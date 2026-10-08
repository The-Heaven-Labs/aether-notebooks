package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/crypto"
)

func TestCloudCredentialsFromConfig(t *testing.T) {
	creds := cloudCredentialsFromConfig([]byte(`{"cloud_org_id":" org ","cloud_service_id":"svc","cloud_key_id":"key","cloud_key_secret":"secret"}`))
	require.True(t, creds.configured())
	require.Equal(t, "org", creds.OrgID)

	require.False(t, cloudCredentialsFromConfig([]byte(`{"cloud_org_id":"org"}`)).configured())
	require.False(t, cloudCredentialsFromConfig([]byte(`not-json`)).configured())
}

func TestCloudStateCacheExpiry(t *testing.T) {
	cache := newCloudStateCache(25 * time.Millisecond)
	cache.put("conn-a", cloudStateResult{state: cloudServiceState{State: "running"}, checkedAt: time.Now()})
	if _, ok := cache.get("conn-a"); !ok {
		t.Fatal("expected a fresh cache hit")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := cache.get("conn-a"); ok {
		t.Fatal("expected the entry to expire")
	}
}

// cloudStateCredsConfig is a ClickHouse connector config with a complete Cloud
// API credential set.
const cloudStateCredsConfig = `{
	"host": "abc.clickhouse.cloud",
	"port": 8443,
	"user": "default",
	"password": "pw",
	"cloud_org_id": "org-123",
	"cloud_service_id": "svc-456",
	"cloud_key_id": "key-id",
	"cloud_key_secret": "key-secret"
}`

// cloudStateFixture holds a test server plus a seeded org, org-admin user, and
// one connector of the requested type.
type cloudStateFixture struct {
	s           *Server
	orgID       uuid.UUID
	userID      uuid.UUID
	connectorID uuid.UUID
	token       string
}

// newCloudStateFixture seeds an org, an org admin, and a connector whose
// encrypted config is configJSON. It reuses newWarehouseSyncTestServer because
// package-internal tests cannot call setupTestServer from package api_test and
// both need a migrated database; the CH-table-permissions flag it sets is
// irrelevant to this endpoint.
func newCloudStateFixture(t *testing.T, connType, configJSON string) *cloudStateFixture {
	t.Helper()
	s, key := newWarehouseSyncTestServer(t)
	ctx := context.Background()
	suffix := uuid.NewString()

	orgID := uuid.New()
	_, err := s.db.Pool.Exec(ctx, `INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID.String(), "Cloud State Org", "cloud-state-"+suffix)
	require.NoError(t, err)

	userID := uuid.New()
	_, err = s.db.Pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), "cloud-state-"+suffix+"@test.local", "Cloud State User")
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID.String(), userID.String())
	require.NoError(t, err)

	encrypted, err := crypto.Encrypt([]byte(configJSON), key)
	require.NoError(t, err)

	connectorID := uuid.New()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		connectorID.String(), orgID.String(), "Cloud State Connector", connType, encrypted, userID.String())
	require.NoError(t, err)

	token, err := s.jwt.Issue(userID.String(), orgID.String(), "admin")
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, stmt := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`DELETE FROM connectors WHERE id = $1`, connectorID},
			{`DELETE FROM users WHERE id = $1`, userID},
			{`DELETE FROM orgs WHERE id = $1`, orgID},
		} {
			if _, err := s.db.Pool.Exec(cleanupCtx, stmt.sql, stmt.id.String()); err != nil {
				t.Logf("cleanup %s: %v", stmt.sql, err)
			}
		}
	})

	return &cloudStateFixture{s: s, orgID: orgID, userID: userID, connectorID: connectorID, token: token}
}

func (fx *cloudStateFixture) path() string {
	return "/api/v1/connectors/" + fx.connectorID.String() + "/cloud-state"
}

// get drives the real mux + auth middleware (route-registration coverage).
func (fx *cloudStateFixture) get(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return warehouseAPIRequest(t, fx.s, http.MethodGet, fx.path(), fx.token, nil)
}

// setCloudAPIBaseURL points the control-plane base at an httptest server for
// the duration of one test. Tests in this file must not call t.Parallel().
func setCloudAPIBaseURL(t *testing.T, base string) {
	t.Helper()
	previous := cloudAPIBaseURL
	cloudAPIBaseURL = base
	t.Cleanup(func() { cloudAPIBaseURL = previous })
}

// cloudStateMock wraps handler with a request counter and installs it as the
// control-plane base URL.
func cloudStateMock(t *testing.T, handler http.HandlerFunc) *atomic.Int64 {
	t.Helper()
	var hits atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(ts.Close)
	setCloudAPIBaseURL(t, ts.URL)
	return &hits
}

func TestConnectorCloudStateNotConfigured(t *testing.T) {
	for _, tc := range []struct {
		name     string
		connType string
		config   string
	}{
		{"clickhouse without credentials", "clickhouse", `{"host":"abc.clickhouse.cloud","port":8443}`},
		{"clickhouse with partial credentials", "clickhouse", `{"host":"abc.clickhouse.cloud","cloud_org_id":"org-1"}`},
		{"non-clickhouse connector", "postgres", `{"host":"localhost","port":5432,"user":"u","password":"p","database":"d"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newCloudStateFixture(t, tc.connType, tc.config)
			// No upstream server is installed: any control-plane call would hit
			// the production base URL, which this test's response must not do.
			rec := fx.get(t)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.JSONEq(t, `{"configured": false}`, rec.Body.String())
		})
	}
}

func TestConnectorCloudStateSuccessAndCache(t *testing.T) {
	fx := newCloudStateFixture(t, "clickhouse", cloudStateCredsConfig)

	hits := cloudStateMock(t, func(w http.ResponseWriter, r *http.Request) {
		// Wrong path or auth would make this handler answer 401/404 and fail
		// the response assertions below.
		user, pass, ok := r.BasicAuth()
		if !ok || user != "key-id" || pass != "key-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/organizations/org-123/services/svc-456" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"state":              "idle",
				"idleScaling":        true,
				"idleTimeoutMinutes": 15,
			},
		})
	})

	rec := fx.get(t)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Configured         bool    `json:"configured"`
		State              string  `json:"state"`
		IdleScaling        bool    `json:"idle_scaling"`
		IdleTimeoutMinutes float64 `json:"idle_timeout_minutes"`
		CheckedAt          string  `json:"checked_at"`
		Error              string  `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Configured)
	require.Equal(t, "idle", resp.State)
	require.True(t, resp.IdleScaling)
	require.Equal(t, 15.0, resp.IdleTimeoutMinutes)
	require.NotEmpty(t, resp.CheckedAt)
	require.Empty(t, resp.Error)

	// A second read inside the TTL must come from the in-memory cache.
	rec = fx.get(t)
	require.Equal(t, http.StatusOK, rec.Code)
	require.EqualValues(t, 1, hits.Load(), "cached read must not hit the control plane")
}

func TestConnectorCloudStateDedupesInFlightReads(t *testing.T) {
	fx := newCloudStateFixture(t, "clickhouse", cloudStateCredsConfig)

	release := make(chan struct{})
	var hits atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"state": "running", "idleScaling": false, "idleTimeoutMinutes": 0},
		})
	}))
	t.Cleanup(ts.Close)
	setCloudAPIBaseURL(t, ts.URL)

	recs := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, fx.path(), nil)
			req.Header.Set("Authorization", "Bearer "+fx.token)
			req.Header.Set("X-AETHER-Admin-Mode", "true")
			rec := httptest.NewRecorder()
			fx.s.ServeHTTP(rec, req)
			recs[i] = rec
		}(i)
	}

	// Wait until the leader is parked in the control-plane call, then release
	// both requests: the follower must join the leader's flight (or read the
	// cache), never start a second upstream call.
	require.Eventually(t, func() bool { return hits.Load() >= 1 }, time.Second, 5*time.Millisecond)
	close(release)
	wg.Wait()

	require.EqualValues(t, 1, hits.Load(), "both reads must share one control-plane call")
	for i, rec := range recs {
		require.Equal(t, http.StatusOK, rec.Code, "request %d: %s", i, rec.Body.String())
	}
}

func TestConnectorCloudStateUpstreamErrorIsGraceful(t *testing.T) {
	fx := newCloudStateFixture(t, "clickhouse", cloudStateCredsConfig)
	hits := cloudStateMock(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	rec := fx.get(t)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Configured bool   `json:"configured"`
		Error      string `json:"error"`
		State      string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Configured)
	require.Contains(t, resp.Error, "HTTP 500")
	require.Empty(t, resp.State)
	require.NotContains(t, rec.Body.String(), "key-secret", "credentials must never leak")

	// Errors are not cached: the next read retries upstream.
	rec = fx.get(t)
	require.Equal(t, http.StatusOK, rec.Code)
	require.EqualValues(t, 2, hits.Load(), "errors must not be cached")
}

func TestConnectorCloudStateRequiresViewPermission(t *testing.T) {
	fx := newCloudStateFixture(t, "clickhouse", cloudStateCredsConfig)

	// A second org member with no connector ACL must be denied.
	memberID := uuid.New()
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		memberID.String(), "cloud-state-member-"+uuid.NewString()+"@test.local", "Cloud State Member")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = fx.s.db.Pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, memberID.String())
	})
	_, err = fx.s.db.Pool.Exec(context.Background(),
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'editor')`,
		fx.orgID.String(), memberID.String())
	require.NoError(t, err)
	memberToken, err := fx.s.jwt.Issue(memberID.String(), fx.orgID.String(), "editor")
	require.NoError(t, err)

	rec := warehouseAPIRequest(t, fx.s, http.MethodGet, fx.path(), memberToken, nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}
