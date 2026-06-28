package api_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/sso"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Test OIDC Server ────────────────────────────────────────────────────────

type testOIDCServer struct {
	server     *httptest.Server
	key        *rsa.PrivateKey
	kid        string
	baseURL    string
	sub        string
	email      string
	name       string
	groups     []string
	idTokenHasGroups bool
}

func newTestOIDCServer(t *testing.T, sub, email, name string, groups []string, idTokenHasGroups bool) *testOIDCServer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := &testOIDCServer{
		key:        key,
		kid:        "test-key-1",
		sub:        sub,
		email:      email,
		name:       name,
		groups:     groups,
		idTokenHasGroups: idTokenHasGroups,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", srv.handleDiscovery)
	mux.HandleFunc("/token", srv.handleToken)
	mux.HandleFunc("/userinfo", srv.handleUserInfo)
	mux.HandleFunc("/jwks", srv.handleJWKS)

	srv.server = httptest.NewServer(mux)
	srv.baseURL = srv.server.URL
	t.Cleanup(srv.server.Close)
	return srv
}

func (s *testOIDCServer) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"issuer":                               s.baseURL,
		"authorization_endpoint":               s.baseURL + "/auth",
		"token_endpoint":                       s.baseURL + "/token",
		"userinfo_endpoint":                    s.baseURL + "/userinfo",
		"jwks_uri":                             s.baseURL + "/jwks",
		"response_types_supported":             []string{"code"},
		"subject_types_supported":              []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (s *testOIDCServer) handleJWKS(w http.ResponseWriter, r *http.Request) {
	pub := s.key.Public().(*rsa.PublicKey)
	n := base64URLEncodeBig(pub.N)
	e := base64URLEncodeInt(pub.E)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": s.kid,
				"use": "sig",
				"alg": "RS256",
				"n":   n,
				"e":   e,
			},
		},
	})
}

func (s *testOIDCServer) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":            s.baseURL,
		"sub":            s.sub,
		"aud":            "test-client-id",
		"exp":            float64(now.Add(time.Hour).Unix()),
		"iat":            float64(now.Unix()),
		"auth_time":      float64(now.Unix()),
		"email":          s.email,
		"email_verified": true,
		"name":           s.name,
	}

	if s.idTokenHasGroups && len(s.groups) > 0 {
		groups := make([]any, len(s.groups))
		for i, g := range s.groups {
			groups[i] = g
		}
		claims["groups"] = groups
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid

	idToken, err := token.SignedString(s.key)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": "test-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	})
}

func (s *testOIDCServer) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	info := map[string]any{
		"sub":   s.sub,
		"email": s.email,
		"name":  s.name,
	}
	if len(s.groups) > 0 {
		groups := make([]any, len(s.groups))
		for i, g := range s.groups {
			groups[i] = g
		}
		info["groups"] = groups
	}
	json.NewEncoder(w).Encode(info)
}

func base64URLEncodeBig(n *big.Int) string {
	return base64.RawURLEncoding.EncodeToString(n.Bytes())
}

func base64URLEncodeInt(n int) string {
	return base64.RawURLEncoding.EncodeToString(big.NewInt(int64(n)).Bytes())
}

// ─── SyncSSOGroups Edge Cases ───────────────────────────────────────────────

func TestSyncSSOGroups_EmptyGroups(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	var userID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("empty-%d@test.com", time.Now().UnixNano()), "Empty",
	).Scan(&userID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, userID,
	)
	require.NoError(t, err)

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, testMasterKey, sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           "empty-test",
		ProviderType:   "oidc",
		ClientID:       "test-client",
		ClientSecret:   "test-secret",
		DiscoveryURL:   "https://example.com/",
		AllowedDomains: []string{},
		Scopes:         []string{},
		Enabled:        true,
		AutoSyncGroups: true,
	})
	require.NoError(t, err)

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, nil)

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1 AND g.org_id=$2`,
		userID, orgID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "nil groups should not create any memberships")

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{})

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1 AND g.org_id=$2`,
		userID, orgID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "empty groups should not create any memberships")
}

func TestSyncSSOGroups_CaseInsensitiveMatching(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	// Pre-create an existing group with different case
	var existingID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'Engineering') RETURNING id`,
		orgID,
	).Scan(&existingID)
	require.NoError(t, err)

	// FindOrCreateGroup should return the existing group when queried with different case
	foundID, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "engineering")
	require.NoError(t, err)
	assert.Equal(t, existingID, foundID, "should find existing group case-insensitively")

	// Verify group name is preserved (case from DB)
	var name string
	err = s.DB().Pool.QueryRow(ctx, `SELECT name FROM groups WHERE id=$1`, foundID).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "Engineering", name)
}

func TestFindOrCreateGroup_Existing(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	// Pre-create a group
	var existingID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'my-group') RETURNING id`,
		orgID,
	).Scan(&existingID)
	require.NoError(t, err)

	// Find the same group — should return existing ID
	foundID, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "my-group")
	require.NoError(t, err)
	assert.Equal(t, existingID, foundID, "should return existing group ID")
}

func TestFindOrCreateGroup_CaseInsensitive(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	var existingID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'MyGroup') RETURNING id`,
		orgID,
	).Scan(&existingID)
	require.NoError(t, err)

	// Find with different case
	foundID, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "mygroup")
	require.NoError(t, err)
	assert.Equal(t, existingID, foundID, "should find existing group case-insensitively")
}

func TestFindStaleSSOGroups(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	var userID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("stale-%d@test.com", time.Now().UnixNano()), "Stale",
	).Scan(&userID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, userID,
	)
	require.NoError(t, err)

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, testMasterKey, sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           "stale-test",
		ProviderType:   "oidc",
		ClientID:       "test-client",
		ClientSecret:   "test-secret",
		DiscoveryURL:   "https://example.com/",
		AllowedDomains: []string{},
		Scopes:         []string{},
		Enabled:        true,
		AutoSyncGroups: true,
	})
	require.NoError(t, err)

	// Pre-create groups and SSO-provision the memberships
	var groupA, groupB string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'group-a') RETURNING id`,
		orgID,
	).Scan(&groupA)
	require.NoError(t, err)

	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'group-b') RETURNING id`,
		orgID,
	).Scan(&groupB)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2), ($3, $4)`,
		groupA, userID, groupB, userID,
	)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO sso_group_memberships (provider_id, group_id, user_id) VALUES ($1, $2, $3), ($4, $5, $6)`,
		provider.ID, groupA, userID, provider.ID, groupB, userID,
	)
	require.NoError(t, err)

	// Only group-a is in current groups — group-b should be stale
	stale, err := api.FindStaleSSOGroups(ctx, s.DB().Pool, provider.ID, userID, []string{"group-a"})
	require.NoError(t, err)
	assert.Len(t, stale, 1, "group-b should be stale")
	assert.Equal(t, groupB, stale[0])
}

// ─── OIDC Exchange Tests ────────────────────────────────────────────────────

func TestOIDCExchangeWithGroups(t *testing.T) {
	email := fmt.Sprintf("oidc-%d@test.com", time.Now().UnixNano())
	srv := newTestOIDCServer(t, "user-123", email, "OIDC Test",
		[]string{"engineering", "analysts", "aether-admins"}, true)

	provider, err := auth.NewGenericOIDCProvider(context.Background(),
		"test", srv.baseURL, "test-client-id", "test-client-secret",
		"http://localhost/callback",
		[]string{"openid", "profile", "email", "groups"},
		"groups", false)
	require.NoError(t, err)

	claims, err := provider.Exchange(context.Background(), "test-code")
	require.NoError(t, err)

	assert.Equal(t, email, claims.Email)
	assert.Equal(t, "OIDC Test", claims.Name)
	assert.Equal(t, []string{"engineering", "analysts", "aether-admins"}, claims.Groups)
}

func TestOIDCExchangeGroupsOnlyFromUserInfo(t *testing.T) {
	email := fmt.Sprintf("ui-%d@test.com", time.Now().UnixNano())
	// ID token has NO groups, UserInfo has them
	srv := newTestOIDCServer(t, "user-ui", email, "UI Test",
		[]string{"from-userinfo", "aether-team"}, false)

	provider, err := auth.NewGenericOIDCProvider(context.Background(),
		"test", srv.baseURL, "test-client-id", "test-client-secret",
		"http://localhost/callback",
		[]string{"openid", "profile", "email", "groups"},
		"groups", true)
	require.NoError(t, err)

	claims, err := provider.Exchange(context.Background(), "test-code")
	require.NoError(t, err)

	assert.Equal(t, email, claims.Email)
	assert.Equal(t, []string{"from-userinfo", "aether-team"}, claims.Groups,
		"groups from UserInfo should be returned when ID token has none")
}

func TestOIDCExchangeNoGroups(t *testing.T) {
	email := fmt.Sprintf("nogroups-%d@test.com", time.Now().UnixNano())
	srv := newTestOIDCServer(t, "user-nogroups", email, "No Groups",
		nil, false)

	provider, err := auth.NewGenericOIDCProvider(context.Background(),
		"test", srv.baseURL, "test-client-id", "test-client-secret",
		"http://localhost/callback",
		[]string{"openid", "profile", "email"},
		"groups", false)
	require.NoError(t, err)

	claims, err := provider.Exchange(context.Background(), "test-code")
	require.NoError(t, err)

	assert.Empty(t, claims.Groups, "groups should be empty when not present")
}

func TestOIDCExchangeUserInfoOverridesIDToken(t *testing.T) {
	email := fmt.Sprintf("override-%d@test.com", time.Now().UnixNano())
	// ID token has some groups, UserInfo has a different set — UserInfo should win
	srv := newTestOIDCServer(t, "user-override", email, "Override",
		[]string{"from-userinfo-only"}, true)

	provider, err := auth.NewGenericOIDCProvider(context.Background(),
		"test", srv.baseURL, "test-client-id", "test-client-secret",
		"http://localhost/callback",
		[]string{"openid", "profile", "email", "groups"},
		"groups", true)
	require.NoError(t, err)

	claims, err := provider.Exchange(context.Background(), "test-code")
	require.NoError(t, err)

	// Our test server puts the same groups in both ID token and UserInfo,
	// so they should match
	assert.NotEmpty(t, claims.Groups)
	assert.Equal(t, []string{"from-userinfo-only"}, claims.Groups)
}

// ─── Full Callback Integration Test ───────────────────────────────────────────

func TestFullOIDCCallbackWithGroupSync(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("fulltest-%d@example.com", ts)
	name := fmt.Sprintf("Full Test %d", ts)

	oidcSrv := newTestOIDCServer(t, "full-test-user", email, name,
		[]string{"aether-analysts", "aether-engineering", "all-employees"}, true)

	dbProvider := sso.Provider{
		Scope:          "platform",
		Name:           "Full Test OIDC",
		ProviderType:   "oidc",
		ClientID:       "test-client-id",
		ClientSecret:   "test-secret",
		DiscoveryURL:   oidcSrv.baseURL,
		AllowedDomains: []string{},
		Scopes:         []string{"openid", "profile", "email", "groups"},
		Enabled:        true,
		AutoSyncGroups: true,
		GroupsClaim:    "groups",
		GroupPrefix:    "aether-",
		GetUserInfo:    false,
	}

	created, err := sso.CreateProvider(ctx, s.DB().Pool, s.MasterKey(), dbProvider)
	require.NoError(t, err)

	state := fmt.Sprintf("test-state-%d", time.Now().UnixNano())
	stateKey := fmt.Sprintf("oidc:state:%s", state)
	_, err = s.Cache.Client().SetNX(ctx, stateKey, "1", 10*time.Minute).Result()
	require.NoError(t, err)

	callbackURL := fmt.Sprintf("/api/v1/auth/oidc/%s/callback?code=test-code&state=%s", created.ID, state)
	req := httptest.NewRequest("GET", callbackURL, nil)
	req.Host = "localhost"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Logf("callback returned %d: %s", rec.Code, rec.Body.String())
	}
	require.Equal(t, http.StatusFound, rec.Code, "should redirect to frontend")

	location := rec.Header().Get("Location")
	if strings.HasPrefix(location, "http") {
		u, err := url.Parse(location)
		require.NoError(t, err)
		location = u.RequestURI()
	}
	assert.Contains(t, location, "/login?token=")

	var userID string
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT id FROM users WHERE email=$1`, email,
	).Scan(&userID)
	require.NoError(t, err, "user should have been created")

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1`,
		userID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should have 2 SSO-synced groups (aether- prefix filtered)")

	rows, err := s.DB().Pool.Query(ctx,
		`SELECT g.name FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1 ORDER BY g.name`,
		userID,
	)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	assert.Equal(t, []string{"aether-analysts", "aether-engineering"}, names)
}
