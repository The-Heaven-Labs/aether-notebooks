package api_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/sso"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testMasterKey = crypto.DeriveKey("test-master-key-for-tests-only!")

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
		GroupPrefix:    "hnb-",
	})
	require.NoError(t, err)

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID,
		[]string{"hnb-engineering", "hnb-analysts", "all-employees", "system-admins"})

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1 AND g.org_id=$2`,
		userID, orgID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "only hnb- prefixed groups should be synced")
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
