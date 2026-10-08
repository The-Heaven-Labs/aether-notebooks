package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// TestPendingGrantTablesExist pins the V127 schema: both staging tables exist
// so materialization and the handlers can rely on them.
func TestPendingGrantTablesExist(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	for _, table := range []string{"pending_acl_entries", "pending_warehouse_table_grants"} {
		var exists bool
		require.NoError(t, db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)`,
			table).Scan(&exists))
		require.True(t, exists, "%s must exist", table)
	}
}

// ─── Fixture helpers ─────────────────────────────────────────────────────────

func stagePendingACL(t *testing.T, s *api.Server, orgID, resourceType, resourceID, email string, actions []string) {
	t.Helper()
	_, err := s.DB().Pool.Exec(context.Background(), `
		INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions)
		VALUES ($1, $2, $3, $4, $5)`, orgID, resourceType, resourceID, email, actions)
	require.NoError(t, err)
}

func stagePendingWarehouseGrant(t *testing.T, s *api.Server, orgID, warehouseID, email, database, table string) {
	t.Helper()
	_, err := s.DB().Pool.Exec(context.Background(), `
		INSERT INTO pending_warehouse_table_grants (org_id, warehouse_id, email, database_name, table_name)
		VALUES ($1, $2, $3, $4, $5)`, orgID, warehouseID, email, database, table)
	require.NoError(t, err)
}

func applyPendingAccessInTx(t *testing.T, s *api.Server, orgID, userID, email string) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.DB().Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, api.ApplyPendingAccess(ctx, tx, orgID, userID, email))
	require.NoError(t, tx.Commit(ctx))
}

func aclActionsFor(t *testing.T, s *api.Server, resourceType, resourceID, subjectID string) []string {
	t.Helper()
	var actions []string
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(), `
		SELECT actions FROM acl_entries
		WHERE resource_type = $1 AND resource_id = $2::uuid
		  AND subject_type = 'user' AND subject_id = $3`,
		resourceType, resourceID, subjectID).Scan(&actions))
	return actions
}

func countPendingACLRows(t *testing.T, s *api.Server, orgID, email string) int {
	t.Helper()
	var n int
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pending_acl_entries WHERE org_id = $1 AND lower(email) = lower($2)`,
		orgID, email).Scan(&n))
	return n
}

// ─── ApplyPendingAccess ──────────────────────────────────────────────────────

func TestApplyPendingAccessMaterializesAndConsumes(t *testing.T) {
	s := setupTestServer(t)
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-user-%d@example.com", time.Now().UnixNano()))
	nbID := uuid.NewString()

	// Staged with mixed case; materialized with a different case.
	stagePendingACL(t, s, orgID, "notebook", nbID, "Alice@Example.com", []string{"view", "edit"})

	applyPendingAccessInTx(t, s, orgID, userID, "alice@example.com")

	require.ElementsMatch(t, []string{"view", "edit"}, aclActionsFor(t, s, "notebook", nbID, userID))
	require.Zero(t, countPendingACLRows(t, s, orgID, "alice@example.com"), "staged rows must be consumed")
}

func TestApplyPendingAccessOrgIsolation(t *testing.T) {
	s := setupTestServer(t)
	orgA := pendingTestOrg(t, s)
	orgB := pendingTestOrg(t, s)
	userA := pendingTestUser(t, s, fmt.Sprintf("pua-iso-%d@example.com", time.Now().UnixNano()))
	nbB := uuid.NewString()

	stagePendingACL(t, s, orgB, "notebook", nbB, "shared@example.com", []string{"view"})

	applyPendingAccessInTx(t, s, orgA, userA, "shared@example.com")

	var count int
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM acl_entries
		WHERE resource_id = $1::uuid AND subject_type = 'user' AND subject_id = $2`,
		nbB, userA).Scan(&count))
	require.Zero(t, count, "org A's join must not materialize org B's staged row")
	require.Equal(t, 1, countPendingACLRows(t, s, orgB, "shared@example.com"), "org B's staged row must remain")
}

func TestApplyPendingAccessIsIdempotent(t *testing.T) {
	s := setupTestServer(t)
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-idem-%d@example.com", time.Now().UnixNano()))
	nbID := uuid.NewString()

	stagePendingACL(t, s, orgID, "notebook", nbID, "idem@example.com", []string{"view"})

	applyPendingAccessInTx(t, s, orgID, userID, "idem@example.com")
	applyPendingAccessInTx(t, s, orgID, userID, "idem@example.com") // nothing staged the second time

	var count int
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM acl_entries
		WHERE resource_id = $1::uuid AND subject_id = $2`, nbID, userID).Scan(&count))
	require.Equal(t, 1, count, "a second materialization must not duplicate rows")
	require.Zero(t, countPendingACLRows(t, s, orgID, "idem@example.com"))
}

func TestApplyPendingAccessUnionsExistingDirectEntry(t *testing.T) {
	s := setupTestServer(t)
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-union-%d@example.com", time.Now().UnixNano()))
	nbID := uuid.NewString()

	_, err := s.DB().Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'notebook', $2, 'user', $3, ARRAY['view','delete'])`, orgID, nbID, userID)
	require.NoError(t, err)
	stagePendingACL(t, s, orgID, "notebook", nbID, "union@example.com", []string{"view", "edit"})

	applyPendingAccessInTx(t, s, orgID, userID, "union@example.com")

	require.ElementsMatch(t, []string{"view", "delete", "edit"}, aclActionsFor(t, s, "notebook", nbID, userID),
		"staged actions must be unioned into the existing direct entry, never dropped")
}

func TestApplyPendingAccessWarehouseGrantsDedupe(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-wh-%d@example.com", time.Now().UnixNano()))
	whID := uuid.NewString()

	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO warehouses (id, org_id, name) VALUES ($1, $2, 'Pending WH')`, whID, orgID)
	require.NoError(t, err)

	// Existing real grant for the same user + table, plus staged duplicates.
	_, err = s.DB().Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, 'analytics', 'events')`, orgID, whID, userID)
	require.NoError(t, err)
	stagePendingWarehouseGrant(t, s, orgID, whID, "Dedupe@Example.com", "analytics", "events")
	stagePendingWarehouseGrant(t, s, orgID, whID, "dedupe@example.com", "analytics", "users")

	applyPendingAccessInTx(t, s, orgID, userID, "dedupe@example.com")

	var grants int
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM warehouse_table_grants WHERE warehouse_id = $1 AND subject_id = $2`,
		whID, userID).Scan(&grants))
	require.Equal(t, 2, grants, "the duplicate grant dedupes and the new table materializes")

	var pending int
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, whID).Scan(&pending))
	require.Zero(t, pending, "staged grant rows must be consumed")
}

func TestApplyPendingAccessConstrainsSessionRowsToView(t *testing.T) {
	s := setupTestServer(t)
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-session-%d@example.com", time.Now().UnixNano()))
	sessionID := uuid.NewString()

	// A staged session row that was somehow written with extra actions must not
	// materialize them: non-owner session subjects are read-only.
	stagePendingACL(t, s, orgID, "agent_session", sessionID, "viewer@example.com", []string{"view", "edit", "delete"})

	applyPendingAccessInTx(t, s, orgID, userID, "viewer@example.com")

	require.Equal(t, []string{"view"}, aclActionsFor(t, s, "agent_session", sessionID, userID))
}

// TestApplyPendingAccessRespectsCallerTransactionRollback pins the
// caller-owns-the-transaction contract: ApplyPendingAccess must never commit
// on its own, so rolling back the caller's transaction undoes the whole
// materialization and leaves the staged rows in place.
func TestApplyPendingAccessRespectsCallerTransactionRollback(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-rollback-%d@example.com", time.Now().UnixNano()))
	nbID := uuid.NewString()

	stagePendingACL(t, s, orgID, "notebook", nbID, "rollback@example.com", []string{"view"})

	tx, err := s.DB().Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, api.ApplyPendingAccess(ctx, tx, orgID, userID, "rollback@example.com"))
	require.NoError(t, tx.Rollback(ctx))

	var count int
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM acl_entries
		WHERE resource_id = $1::uuid AND subject_type = 'user' AND subject_id = $2`,
		nbID, userID).Scan(&count))
	require.Zero(t, count, "a rolled-back materialization must not leave acl_entries rows")

	require.Equal(t, 1, countPendingACLRows(t, s, orgID, "rollback@example.com"),
		"a rolled-back materialization must leave the staged row in place")
}

// TestApplyPendingAccessSessionUnionConstrainedToView exercises the defensive
// session constraint through the union path: a staged view must never
// downgrade an owner entry, and staged extra actions must never widen an
// existing view-only entry.
func TestApplyPendingAccessSessionUnionConstrainedToView(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-session-union-%d@example.com", time.Now().UnixNano()))

	// Matches sessionOwnerActions in session_sharing.go.
	ownerActions := []string{"view", "edit", "share", "delete", "admin"}
	ownerSessionID := uuid.NewString()
	_, err := s.DB().Pool.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'agent_session', $2, 'user', $3, $4)`, orgID, ownerSessionID, userID, ownerActions)
	require.NoError(t, err)
	stagePendingACL(t, s, orgID, "agent_session", ownerSessionID, "owner@example.com", []string{"view"})

	viewerSessionID := uuid.NewString()
	_, err = s.DB().Pool.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'agent_session', $2, 'user', $3, ARRAY['view'])`, orgID, viewerSessionID, userID)
	require.NoError(t, err)
	stagePendingACL(t, s, orgID, "agent_session", viewerSessionID, "viewer@example.com", []string{"view", "edit", "delete"})

	applyPendingAccessInTx(t, s, orgID, userID, "owner@example.com")
	applyPendingAccessInTx(t, s, orgID, userID, "viewer@example.com")

	require.ElementsMatch(t, ownerActions, aclActionsFor(t, s, "agent_session", ownerSessionID, userID),
		"a staged view must not downgrade the owner entry")
	require.Equal(t, []string{"view"}, aclActionsFor(t, s, "agent_session", viewerSessionID, userID),
		"staged extra actions must not widen an existing view-only session entry")
}

// TestApplyPendingAccessWarehouseOrgIsolation is the warehouse counterpart of
// TestApplyPendingAccessOrgIsolation: org A's join must neither materialize
// nor consume org B's staged grant for the same email.
func TestApplyPendingAccessWarehouseOrgIsolation(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgA := pendingTestOrg(t, s)
	orgB := pendingTestOrg(t, s)
	userA := pendingTestUser(t, s, fmt.Sprintf("pua-wh-iso-%d@example.com", time.Now().UnixNano()))
	whB := uuid.NewString()

	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO warehouses (id, org_id, name) VALUES ($1, $2, 'Pending WH B')`, whB, orgB)
	require.NoError(t, err)
	stagePendingWarehouseGrant(t, s, orgB, whB, "shared-wh@example.com", "analytics", "events")

	applyPendingAccessInTx(t, s, orgA, userA, "shared-wh@example.com")

	var grants int
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM warehouse_table_grants WHERE warehouse_id = $1 AND subject_id = $2`,
		whB, userA).Scan(&grants))
	require.Zero(t, grants, "org A's join must not materialize org B's staged warehouse grant")

	var pending int
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, whB).Scan(&pending))
	require.Equal(t, 1, pending, "org B's staged grant must remain")
}

// registerOnly registers a user and returns the onboarding token without
// creating an org, mirroring the first half of testhelpers.registerAndGetToken.
func registerOnly(t *testing.T, srv *api.Server, email string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": "pass123", "name": "Future Member"})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	token, _ := resp["onboarding_token"].(string)
	require.NotEmpty(t, token)
	return token
}

// TestOrgJoinMaterializesPendingAccess drives the real invite-redemption join
// path end to end: the staged ACL must become a real entry inside the join
// transaction, the staged row must be consumed, and the success audit must be
// written.
func TestOrgJoinMaterializesPendingAccess(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ctx := context.Background()

	adminEmail := fmt.Sprintf("pending-join-admin-%d@example.com", time.Now().UnixNano())
	memberEmail := fmt.Sprintf("pending-join-member-%d@example.com", time.Now().UnixNano())
	adminToken := registerAndGetToken(t, srv, adminEmail, "Pending Join Org")
	memberOnboarding := registerOnly(t, srv, memberEmail)

	// Resolve the admin's user + org.
	var adminID, orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, adminEmail).Scan(&adminID))
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT org_id FROM org_members WHERE user_id = $1`, adminID).Scan(&orgID))

	// Admin invites the future member by email (the invite endpoint only
	// accepts role "admin").
	inviteBody, _ := json.Marshal(map[string]string{"email": memberEmail, "role": "admin"})
	inviteReq := httptest.NewRequest("POST", "/api/v1/members/invite", bytes.NewReader(inviteBody))
	inviteReq.Header.Set("Content-Type", "application/json")
	inviteReq.Header.Set("Authorization", "Bearer "+adminToken)
	inviteRec := httptest.NewRecorder()
	srv.ServeHTTP(inviteRec, inviteReq)
	require.Equal(t, http.StatusCreated, inviteRec.Code, inviteRec.Body.String())
	var inviteResp map[string]string
	require.NoError(t, json.NewDecoder(inviteRec.Body).Decode(&inviteResp))
	inviteToken := inviteResp["token"]
	require.NotEmpty(t, inviteToken)

	// Stage a pending ACL for the future member's email. Staged in a different
	// case to exercise the lower(email) matching through the real join path.
	// (The plan text staged "Member@Example.com", which can never match this
	// test's generated member address.)
	nbID := createNotebook(t, srv, adminToken, "Pending Join NB")
	stagePendingACL(t, srv, orgID, "notebook", nbID, strings.ToUpper(memberEmail), []string{"view", "edit"})

	// The member redeems the invite with their onboarding token.
	joinBody, _ := json.Marshal(map[string]string{"invite_token": inviteToken})
	joinReq := httptest.NewRequest("POST", "/api/v1/auth/org/join", bytes.NewReader(joinBody))
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+memberOnboarding)
	joinRec := httptest.NewRecorder()
	srv.ServeHTTP(joinRec, joinReq)
	require.Equal(t, http.StatusOK, joinRec.Code, joinRec.Body.String())

	// Materialized as a real user entry; the staged row is consumed.
	var memberID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT id FROM users WHERE lower(email) = lower($1)`, memberEmail).Scan(&memberID))
	require.ElementsMatch(t, []string{"view", "edit"}, aclActionsFor(t, srv, "notebook", nbID, memberID))
	require.Zero(t, countPendingACLRows(t, srv, orgID, memberEmail))

	// Success audit carries the materialization.
	var audits int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'acl.pending_materialize'`,
		orgID).Scan(&audits))
	require.Equal(t, 1, audits)
}
