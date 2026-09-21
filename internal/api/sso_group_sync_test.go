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
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/sso"
)

var testMasterKey = crypto.DeriveKey("test-master-key-for-tests-only!")

func TestFindOrCreateGroup_PreservesDisplayName(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	groupID, _, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "aether-analysts")
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx, `UPDATE groups SET display_name=$1 WHERE id=$2`, "Data Analysts", groupID)
	require.NoError(t, err)

	again, created, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "AETHER-ANALYSTS")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, groupID, again)

	var label *string
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `SELECT display_name FROM groups WHERE id=$1`, groupID).Scan(&label))
	require.NotNil(t, label)
	require.Equal(t, "Data Analysts", *label)
}

func TestSyncSSOGroups_CreatesGroups(t *testing.T) {
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
		fmt.Sprintf("groupsync-%d@test.com", time.Now().UnixNano()), "Group Sync",
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
		Name:           "sync-test",
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

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"engineering", "analysts"})

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1 AND g.org_id=$2`,
		userID, orgID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sso_group_memberships WHERE provider_id=$1 AND user_id=$2`,
		provider.ID, userID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"engineering"})

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1 AND g.org_id=$2`,
		userID, orgID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestSyncSSOGroups_PrefixFilter(t *testing.T) {
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
		fmt.Sprintf("prefix-%d@test.com", time.Now().UnixNano()), "Prefix",
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
		Name:           "prefix-test",
		ProviderType:   "oidc",
		ClientID:       "test-client",
		ClientSecret:   "test-secret",
		DiscoveryURL:   "https://example.com/.well-known/openid-configuration",
		AllowedDomains: []string{},
		Scopes:         []string{},
		Enabled:        true,
		AutoSyncGroups: true,
		GroupPrefix:    "aether-",
	})
	require.NoError(t, err)

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID,
		[]string{"aether-engineering", "aether-analysts", "all-employees", "system-admins"})

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1 AND g.org_id=$2`,
		userID, orgID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "only aether- prefixed groups should be synced")
}

func TestSyncSSOGroups_PreservesManualMemberships(t *testing.T) {
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
		fmt.Sprintf("manual-%d@test.com", time.Now().UnixNano()), "Manual",
	).Scan(&userID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, userID,
	)
	require.NoError(t, err)

	var groupID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'manual-group') RETURNING id`,
		orgID,
	).Scan(&groupID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		groupID, userID,
	)
	require.NoError(t, err)

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, testMasterKey, sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           "manual-test",
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

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"engineering"})

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE group_id=$1 AND user_id=$2`,
		groupID, userID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "manual membership should be preserved")
}

func TestSyncSSOGroups_StripGroupPrefix(t *testing.T) {
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
		fmt.Sprintf("strip-%d@test.com", time.Now().UnixNano()), "Strip",
	).Scan(&userID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, userID,
	)
	require.NoError(t, err)

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, testMasterKey, sso.Provider{
		Scope:            "org",
		OrgID:            &orgID,
		Name:             "strip-test",
		ProviderType:     "oidc",
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		DiscoveryURL:     "https://example.com/",
		AllowedDomains:   []string{},
		Scopes:           []string{},
		Enabled:          true,
		AutoSyncGroups:   true,
		GroupPrefix:      "Aether Notebooks: ",
		StripGroupPrefix: true,
	})
	require.NoError(t, err)

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{
		"Aether Notebooks: Area Name",
		"Aether Notebooks: Engineering",
		"all-employees",
		"Aether Notebooks: ",
	})

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
	assert.Equal(t, []string{"Area Name", "Engineering"}, names,
		"prefix should be stripped, unrelated and empty names dropped")
}

func TestSyncSSOGroups_StaleCaseInsensitive(t *testing.T) {
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
		fmt.Sprintf("case-%d@test.com", time.Now().UnixNano()), "Case",
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
		Name:           "case-test",
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

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"Engineering"})

	// Different casing must not churn the membership.
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"engineering"})

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "casing change should not remove the membership")
}

func TestSyncSSOGroups_AuditEvents(t *testing.T) {
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
		fmt.Sprintf("audit-%d@test.com", time.Now().UnixNano()), "Audit",
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
		Name:           "audit-test",
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

	events := func(action string) int {
		t.Helper()
		var n int
		require.NoError(t, s.DB().Pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_logs WHERE org_id=$1 AND action=$2`,
			orgID, action,
		).Scan(&n))
		return n
	}

	// First sync creates the group and adds the member.
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"audit-engineering"})
	assert.Equal(t, 1, events("group.sso.create"), "first sync should emit one create")
	assert.Equal(t, 1, events("group.sso.add_member"), "first sync should emit one add_member")

	// Both events are attributed to the synced user.
	var attributed int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs
		 WHERE org_id=$1 AND user_id=$2 AND action IN ('group.sso.create', 'group.sso.add_member')`,
		orgID, userID,
	).Scan(&attributed)
	require.NoError(t, err)
	assert.Equal(t, 2, attributed, "sync events must record the user who logged in")

	// Re-reporting the same group is a no-op and must not re-emit events.
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"audit-engineering"})
	assert.Equal(t, 1, events("group.sso.create"), "existing group must not emit create again")
	assert.Equal(t, 1, events("group.sso.add_member"), "repeat sync must not emit add_member again")

	// Drop the membership row out from under the tracked group: stale removal
	// finds nothing to delete, so it must not claim a removal.
	_, err = s.DB().Pool.Exec(ctx, `DELETE FROM group_members WHERE user_id=$1`, userID)
	require.NoError(t, err)
	provider.SyncEmptyGroups = true
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, nil)
	assert.Equal(t, 0, events("group.sso.remove_member"), "no remove_member when the row was already absent")

	var tracked int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sso_group_memberships WHERE provider_id=$1 AND user_id=$2`,
		provider.ID, userID,
	).Scan(&tracked)
	require.NoError(t, err)
	assert.Equal(t, 0, tracked, "tracking rows are cleaned even when the membership was already gone")

	// Re-sync, then an authoritative-empty sync removes the membership and
	// emits remove_member attributed to the user.
	provider.SyncEmptyGroups = false
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"audit-engineering"})
	provider.SyncEmptyGroups = true
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, nil)
	assert.Equal(t, 1, events("group.sso.remove_member"), "authoritative empty should emit remove_member")

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs
		 WHERE org_id=$1 AND user_id=$2 AND action='group.sso.remove_member'`,
		orgID, userID,
	).Scan(&attributed)
	require.NoError(t, err)
	assert.Equal(t, 1, attributed, "remove_member must record the user")
}
