package api_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/sso"
)

// ─── Fixture helpers ─────────────────────────────────────────────────────────

func pendingTestOrg(t *testing.T, s *api.Server) string {
	t.Helper()
	slug := fmt.Sprintf("pgm-org-%d", time.Now().UnixNano())
	var orgID string
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(),
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&orgID))
	return orgID
}

func pendingTestUser(t *testing.T, s *api.Server, email string) string {
	t.Helper()
	var userID string
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(),
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`, email, email).Scan(&userID))
	return userID
}

func pendingTestGroup(t *testing.T, s *api.Server, orgID, name string) string {
	t.Helper()
	var groupID string
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(),
		`INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, name).Scan(&groupID))
	return groupID
}

func stagePending(t *testing.T, s *api.Server, orgID, groupID, email, createdBy string) {
	t.Helper()
	_, err := s.DB().Pool.Exec(context.Background(),
		`INSERT INTO pending_group_members (org_id, group_id, email, created_by) VALUES ($1, $2, $3, $4)`,
		orgID, groupID, email, createdBy)
	require.NoError(t, err)
}

func groupMemberCount(t *testing.T, s *api.Server, groupID, userID string) int {
	t.Helper()
	var n int
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID).Scan(&n))
	return n
}

func pendingRowCount(t *testing.T, s *api.Server, orgID, email string) int {
	t.Helper()
	var n int
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_group_members WHERE org_id = $1 AND lower(email) = lower($2)`, orgID, email).Scan(&n))
	return n
}

// ─── ApplyPendingGroups unit tests ───────────────────────────────────────────

func TestApplyPendingGroupsMaterializesAndConsumes(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgID := pendingTestOrg(t, s)
	adminID := pendingTestUser(t, s, fmt.Sprintf("pgm-admin-%d@example.com", time.Now().UnixNano()))
	userID := pendingTestUser(t, s, fmt.Sprintf("pgm-user-%d@example.com", time.Now().UnixNano()))
	groupID := pendingTestGroup(t, s, orgID, "Analysts")

	// Staged with mixed case; materialized with a different case.
	stagePending(t, s, orgID, groupID, "Alice@Example.com", adminID)

	tx, err := s.DB().Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, api.ApplyPendingGroups(ctx, tx, orgID, userID, "alice@example.com"))
	require.NoError(t, tx.Commit(ctx))

	assert.Equal(t, 1, groupMemberCount(t, s, groupID, userID), "membership should be materialized")
	assert.Equal(t, 0, pendingRowCount(t, s, orgID, "alice@example.com"), "pending row should be consumed")
}

func TestApplyPendingGroupsIdempotentWhenAlreadyMember(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgID := pendingTestOrg(t, s)
	adminID := pendingTestUser(t, s, fmt.Sprintf("pgm-admin-%d@example.com", time.Now().UnixNano()))
	userID := pendingTestUser(t, s, fmt.Sprintf("pgm-member-%d@example.com", time.Now().UnixNano()))
	groupID := pendingTestGroup(t, s, orgID, "Existing")

	// User is already a member and a pending row exists anyway.
	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`, groupID, userID)
	require.NoError(t, err)
	stagePending(t, s, orgID, groupID, "Member@Example.com", adminID)

	tx, err := s.DB().Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, api.ApplyPendingGroups(ctx, tx, orgID, userID, "member@example.com"))
	require.NoError(t, tx.Commit(ctx))

	assert.Equal(t, 1, groupMemberCount(t, s, groupID, userID), "no duplicate membership row")
	assert.Equal(t, 0, pendingRowCount(t, s, orgID, "member@example.com"), "pending row still consumed")
}

func TestApplyPendingGroupsOrgIsolation(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgA := pendingTestOrg(t, s)
	orgB := pendingTestOrg(t, s)
	adminA := pendingTestUser(t, s, fmt.Sprintf("pgm-admin-a-%d@example.com", time.Now().UnixNano()))
	userA := pendingTestUser(t, s, fmt.Sprintf("pgm-user-a-%d@example.com", time.Now().UnixNano()))
	groupB := pendingTestGroup(t, s, orgB, "Org B Group")

	// Staged in org B for the same email.
	const email = "shared@example.com"
	stagePending(t, s, orgB, groupB, email, adminA)

	tx, err := s.DB().Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, api.ApplyPendingGroups(ctx, tx, orgA, userA, email))
	require.NoError(t, tx.Commit(ctx))

	assert.Equal(t, 0, groupMemberCount(t, s, groupB, userA), "org A join must not consume org B's pending row")
	assert.Equal(t, 1, pendingRowCount(t, s, orgB, email), "org B's pending row must remain")
}

// TestPreProvisionedMembershipSurvivesSSOSync verifies that a pre-provisioned
// membership is admin-curated: SyncSSOGroups removes only rows it tracks in
// sso_group_memberships, so the staged membership persists across IdP syncs.
func TestPreProvisionedMembershipSurvivesSSOSync(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgID := pendingTestOrg(t, s)
	adminID := pendingTestUser(t, s, fmt.Sprintf("pgm-sync-admin-%d@example.com", time.Now().UnixNano()))
	userID := pendingTestUser(t, s, fmt.Sprintf("pgm-sync-user-%d@example.com", time.Now().UnixNano()))
	preProvisioned := pendingTestGroup(t, s, orgID, "PreProvisioned")

	stagePending(t, s, orgID, preProvisioned, "sync@example.com", adminID)
	tx, err := s.DB().Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, api.ApplyPendingGroups(ctx, tx, orgID, userID, "sync@example.com"))
	require.NoError(t, tx.Commit(ctx))
	require.Equal(t, 1, groupMemberCount(t, s, preProvisioned, userID))

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, s.MasterKey(), sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           "pgm-sync-provider",
		ProviderType:   "oidc",
		ClientID:       "test-client",
		ClientSecret:   "test-secret",
		DiscoveryURL:   "https://example.com/.well-known/openid-configuration",
		AllowedDomains: []string{},
		Scopes:         []string{},
		Enabled:        true,
		AutoSyncGroups: true,
	})
	require.NoError(t, err)

	// IdP reports only an unrelated group — the pre-provisioned one is absent.
	api.SyncSSOGroups(ctx, s.DB().Pool, audit.NewLogger(s.DB()), provider, orgID, userID, []string{"idp-only-group"})

	assert.Equal(t, 1, groupMemberCount(t, s, preProvisioned, userID),
		"pre-provisioned membership must survive IdP sync")
}
