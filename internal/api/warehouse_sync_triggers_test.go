package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/sso"
)

// recordingWarehouseSyncer records every warehouse ID enqueued by the server.
type recordingWarehouseSyncer struct {
	mu       sync.Mutex
	enqueued []uuid.UUID
}

func (r *recordingWarehouseSyncer) Enqueue(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueued = append(r.enqueued, id)
}

func (r *recordingWarehouseSyncer) ids() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]uuid.UUID(nil), r.enqueued...)
}

var _ api.WarehouseSyncerForTest = (*recordingWarehouseSyncer)(nil)

// seedWarehouseMembershipFixture creates an org with one user, one group, one
// warehouse, and a grant for the group, all with random IDs so repeated runs
// never collide.
func seedWarehouseMembershipFixture(t *testing.T, s *api.Server) (orgID, userID, groupID, warehouseID uuid.UUID, email string) {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()

	orgID, userID, groupID, warehouseID = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	email = "wh-enqueue-" + suffix + "@test.local"

	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID.String(), "Warehouse Enqueue Org", "wh-enqueue-"+suffix)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), email, "Warehouse Enqueue User")
	require.NoError(t, err)
	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'non-admin')`,
		orgID.String(), userID.String())
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
		groupID.String(), orgID.String(), "Warehouse Enqueue Group")
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO warehouses (id, org_id, name) VALUES ($1, $2, $3)`,
		warehouseID.String(), orgID.String(), "Warehouse Enqueue WH")
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'group', $3, 'analytics', 'events')`,
		orgID.String(), warehouseID.String(), groupID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, stmt := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`DELETE FROM warehouses WHERE id = $1`, warehouseID},
			{`DELETE FROM groups WHERE id = $1`, groupID},
			{`DELETE FROM users WHERE id = $1`, userID},
			{`DELETE FROM orgs WHERE id = $1`, orgID},
		} {
			if _, err := s.DB().Pool.Exec(cleanupCtx, stmt.sql, stmt.id.String()); err != nil {
				t.Logf("cleanup %s: %v", stmt.sql, err)
			}
		}
	})

	return orgID, userID, groupID, warehouseID, email
}

func TestMembershipChangeEnqueuesWarehouseSync(t *testing.T) {
	s := setupTestServer(t)
	orgID, userID, groupID, warehouseID, _ := seedWarehouseMembershipFixture(t, s)

	rec := &recordingWarehouseSyncer{}
	s.SetWarehouseSyncerForTest(rec)

	// Add the member through the router as an org admin.
	addBody, _ := json.Marshal(map[string]string{"user_id": userID.String()})
	addReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/groups/"+groupID.String()+"/members", bytes.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	withAdminClaims(addReq, orgID.String())
	addRec := httptest.NewRecorder()
	s.ServeHTTP(addRec, addReq)
	require.Equal(t, http.StatusCreated, addRec.Code, addRec.Body.String())
	require.Contains(t, rec.ids(), warehouseID,
		"adding a member must enqueue the group's grant warehouses")

	// Removing the member must enqueue again.
	delReq := httptest.NewRequest(http.MethodDelete,
		"/api/v1/groups/"+groupID.String()+"/members/"+userID.String(), nil)
	withAdminClaims(delReq, orgID.String())
	delRec := httptest.NewRecorder()
	s.ServeHTTP(delRec, delReq)
	require.Equal(t, http.StatusNoContent, delRec.Code, delRec.Body.String())

	ids := rec.ids()
	require.Len(t, ids, 2, "add and remove must each enqueue once")
	require.Contains(t, ids, warehouseID)
}

// TestOrgJoinEnqueuesWarehouseSyncForUser exercises the user-scoped trigger
// end to end: an invite redemption that materializes a pending group
// membership must reconcile both the everyone-granted warehouse and the
// warehouse granted to the just-materialized group.
func TestOrgJoinEnqueuesWarehouseSyncForUser(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	suffix := uuid.NewString()

	orgID, userID, groupID := uuid.New(), uuid.New(), uuid.New()
	email := "join-enqueue-" + suffix + "@test.local"

	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID.String(), "Join Enqueue Org", "join-enqueue-"+suffix)
	require.NoError(t, err)
	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), email, "Join Enqueue User")
	require.NoError(t, err)
	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
		groupID.String(), orgID.String(), "Join Enqueue Group")
	require.NoError(t, err)

	everyoneWH, groupWH := uuid.New(), uuid.New()
	for _, wh := range []uuid.UUID{everyoneWH, groupWH} {
		_, err = s.DB().Pool.Exec(ctx,
			`INSERT INTO warehouses (id, org_id, name) VALUES ($1, $2, $3)`,
			wh.String(), orgID.String(), "Join Enqueue WH "+wh.String()[:8])
		require.NoError(t, err)
	}
	_, err = s.DB().Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'everyone', 'everyone', 'analytics', 'all_events'),
		       ($1, $3, 'group', $4, 'analytics', 'events')`,
		orgID.String(), everyoneWH.String(), groupWH.String(), groupID.String())
	require.NoError(t, err)

	// Stage the group membership so the join materializes it.
	_, err = s.DB().Pool.Exec(ctx, `
		INSERT INTO pending_group_members (org_id, group_id, email) VALUES ($1, $2, $3)`,
		orgID.String(), groupID.String(), email)
	require.NoError(t, err)

	inviteToken := uuid.NewString()
	_, err = s.DB().Pool.Exec(ctx, `
		INSERT INTO org_invites (org_id, email, role, token, expires_at, created_by)
		VALUES ($1, $2, 'non-admin', $3, now() + interval '1 hour', $4)`,
		orgID.String(), email, inviteToken, userID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, stmt := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`DELETE FROM warehouses WHERE id = $1`, everyoneWH},
			{`DELETE FROM warehouses WHERE id = $1`, groupWH},
			{`DELETE FROM groups WHERE id = $1`, groupID},
			{`DELETE FROM orgs WHERE id = $1`, orgID},
			{`DELETE FROM users WHERE id = $1`, userID},
		} {
			if _, err := s.DB().Pool.Exec(cleanupCtx, stmt.sql, stmt.id.String()); err != nil {
				t.Logf("cleanup %s: %v", stmt.sql, err)
			}
		}
	})

	rec := &recordingWarehouseSyncer{}
	s.SetWarehouseSyncerForTest(rec)

	onboarding, err := testJWT.IssueOnboarding(userID.String(), false)
	require.NoError(t, err)
	body, _ := json.Marshal(map[string]string{"invite_token": inviteToken})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/org/join", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+onboarding)
	res := httptest.NewRecorder()
	s.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	ids := rec.ids()
	require.Contains(t, ids, everyoneWH,
		"everyone-granted warehouse must be enqueued for the new member")
	require.Contains(t, ids, groupWH,
		"warehouse granted to a materialized pending group must be enqueued")
}

// TestPendingMemberAddEnqueuesWarehouseSync covers the direct-add branch of
// POST /groups/{id}/pending-members: when the email already belongs to an org
// member the handler inserts into group_members immediately, which changes
// warehouse access just like the regular membership endpoint.
func TestPendingMemberAddEnqueuesWarehouseSync(t *testing.T) {
	s := setupTestServer(t)
	orgID, _, groupID, warehouseID, email := seedWarehouseMembershipFixture(t, s)

	rec := &recordingWarehouseSyncer{}
	s.SetWarehouseSyncerForTest(rec)

	body, _ := json.Marshal(map[string]any{"emails": []string{email}})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/groups/"+groupID.String()+"/pending-members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAdminClaims(req, orgID.String())
	res := httptest.NewRecorder()
	s.ServeHTTP(res, req)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	require.Contains(t, rec.ids(), warehouseID,
		"directly adding an existing org member to a pending group must enqueue the group's grant warehouses")
}

// TestSSOGroupRemovalEnqueuesWarehouseSync drives the full OIDC callback twice:
// the first login syncs the user into aether-analysts (granted a warehouse),
// the second login reports a different IdP group set, so the stale membership
// is removed. The warehouse that lost its only granting group must still be
// enqueued even though the user no longer resolves to that group.
func TestSSOGroupRemovalEnqueuesWarehouseSync(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("sso-removal-%d@example.com", ts)
	name := fmt.Sprintf("SSO Removal %d", ts)

	oidcSrv := newTestOIDCServer(t, "sso-removal-user", email, name,
		[]string{"aether-analysts"}, true)

	created, err := sso.CreateProvider(ctx, s.DB().Pool, s.MasterKey(), sso.Provider{
		Scope:          "platform",
		Name:           "SSO Removal OIDC",
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
	})
	require.NoError(t, err)

	login := func() {
		state := fmt.Sprintf("test-state-%d", time.Now().UnixNano())
		_, err := s.Cache.Client().SetNX(ctx, "oidc:state:"+state, "1", 10*time.Minute).Result()
		require.NoError(t, err)
		req := httptest.NewRequest("GET",
			fmt.Sprintf("/api/v1/auth/oidc/%s/callback?code=test-code&state=%s", created.ID, state), nil)
		req.Host = "localhost"
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	}

	// First login provisions the user and creates the granted group.
	login()

	var userID, orgID, groupID string
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `
		SELECT u.id, om.org_id, g.id
		FROM users u
		JOIN org_members om ON om.user_id = u.id
		JOIN groups g ON g.org_id = om.org_id AND g.name = 'aether-analysts'
		WHERE u.email = $1`, email).Scan(&userID, &orgID, &groupID))

	warehouseID := uuid.New()
	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO warehouses (id, org_id, name) VALUES ($1, $2, $3)`,
		warehouseID.String(), orgID, "SSO Removal WH")
	require.NoError(t, err)
	_, err = s.DB().Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'group', $3, 'analytics', 'events')`,
		orgID, warehouseID.String(), groupID)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, stmt := range []struct {
			sql string
			id  string
		}{
			{`DELETE FROM warehouses WHERE id = $1`, warehouseID.String()},
			{`DELETE FROM sso_providers WHERE id = $1`, created.ID},
			{`DELETE FROM orgs WHERE id = $1`, orgID},
			{`DELETE FROM users WHERE id = $1`, userID},
		} {
			if _, err := s.DB().Pool.Exec(cleanupCtx, stmt.sql, stmt.id); err != nil {
				t.Logf("cleanup %s: %v", stmt.sql, err)
			}
		}
	})

	rec := &recordingWarehouseSyncer{}
	s.SetWarehouseSyncerForTest(rec)

	// Second login: the IdP reports only aether-engineering, so aether-analysts
	// is stale and its membership is removed.
	oidcSrv.groups = []string{"aether-engineering"}
	login()

	require.Contains(t, rec.ids(), warehouseID,
		"removing the user's only granting group must enqueue the warehouse that lost the grant")
}
