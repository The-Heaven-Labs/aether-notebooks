package sso_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/sso"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testMasterKey = crypto.DeriveKey("test-master-key-for-tests-only!")

func setupTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// createTestOrg inserts a minimal org and returns its ID.
func createTestOrg(t *testing.T, db *database.DB) string {
	t.Helper()
	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id=$1`, orgID)
	})
	return orgID
}

func makePlatformProvider(name string) sso.Provider {
	return sso.Provider{
		Scope:          "platform",
		Name:           name,
		ProviderType:   "oidc",
		ClientID:       "client-id-" + name,
		ClientSecret:   "super-secret-" + name,
		DiscoveryURL:   "https://accounts.example.com/.well-known/openid-configuration",
		AllowedDomains: []string{"example.com"},
		Scopes:         []string{},
		Enabled:        true,
	}
}

func TestCreateAndGetProvider(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	input := makePlatformProvider("create-test")
	created, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, input)
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, created.ID)
	})

	assert.Equal(t, input.Scope, created.Scope)
	assert.Equal(t, input.Name, created.Name)
	assert.Equal(t, input.ClientID, created.ClientID)
	assert.Equal(t, input.ClientSecret, created.ClientSecret, "decrypted secret should match original")
	assert.Equal(t, input.DiscoveryURL, created.DiscoveryURL)
	assert.Equal(t, input.AllowedDomains, created.AllowedDomains)
	assert.True(t, created.Enabled)
	assert.Nil(t, created.OrgID)

	got, err := sso.GetProvider(ctx, db.Pool, testMasterKey, created.ID, "")
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, input.ClientSecret, got.ClientSecret)
	assert.Equal(t, input.AllowedDomains, got.AllowedDomains)
}

func TestListPlatformProviders(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	// Clean stale providers that may have been created by the running API server
	// with a different master key, to avoid decryption failures.
	db.Pool.Exec(ctx, `DELETE FROM sso_providers`)

	p1, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, makePlatformProvider("list-p1"))
	require.NoError(t, err)
	p2, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, makePlatformProvider("list-p2"))
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id IN ($1, $2)`, p1.ID, p2.ID)
	})

	list, err := sso.ListPlatformProviders(ctx, db.Pool, testMasterKey)
	require.NoError(t, err)

	// Filter to our test providers in case other tests left rows.
	found := 0
	for _, p := range list {
		if p.ID == p1.ID || p.ID == p2.ID {
			found++
		}
	}
	assert.Equal(t, 2, found)
}

func TestUpdateProvider(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	created, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, makePlatformProvider("update-test"))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, created.ID)
	})

	created.Name = "Updated Name"
	created.ClientSecret = "new-secret-value"
	updated, err := sso.UpdateProvider(ctx, db.Pool, testMasterKey, created)
	require.NoError(t, err)

	assert.Equal(t, "Updated Name", updated.Name)
	assert.Equal(t, "new-secret-value", updated.ClientSecret)

	got, err := sso.GetProvider(ctx, db.Pool, testMasterKey, created.ID, "")
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", got.Name)
	assert.Equal(t, "new-secret-value", got.ClientSecret)
}

func TestDeleteProvider(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	created, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, makePlatformProvider("delete-test"))
	require.NoError(t, err)

	err = sso.DeleteProvider(ctx, db.Pool, created.ID)
	require.NoError(t, err)

	_, err = sso.GetProvider(ctx, db.Pool, testMasterKey, created.ID, "")
	assert.ErrorIs(t, err, pgx.ErrNoRows, "GetProvider after delete should return ErrNoRows")
}

func TestEnableDisablePlatformProvider(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	orgID := createTestOrg(t, db)

	provider, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, makePlatformProvider("enable-disable-test"))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, provider.ID)
	})

	err = sso.EnablePlatformProvider(ctx, db.Pool, orgID, provider.ID)
	require.NoError(t, err)

	var count int
	err = db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM org_platform_providers WHERE org_id=$1 AND provider_id=$2`,
		orgID, provider.ID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Idempotent — second call should not error.
	err = sso.EnablePlatformProvider(ctx, db.Pool, orgID, provider.ID)
	require.NoError(t, err)

	err = sso.DisablePlatformProvider(ctx, db.Pool, orgID, provider.ID)
	require.NoError(t, err)

	err = db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM org_platform_providers WHERE org_id=$1 AND provider_id=$2`,
		orgID, provider.ID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCreateProviderWithGroupSettings(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	input := sso.Provider{
		Scope:          "platform",
		Name:           "groups-test",
		ProviderType:   "oidc",
		ClientID:       "groups-client",
		ClientSecret:   "groups-secret",
		DiscoveryURL:   "https://accounts.example.com/.well-known/openid-configuration",
		AllowedDomains: []string{},
		Enabled:        true,
		Scopes:         []string{"openid", "profile", "email", "groups"},
		GroupsClaim:    "custom_groups",
		GroupPrefix:    "aether-",
		AutoSyncGroups: true,
		GetUserInfo:    true,
	}

	created, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, input)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, created.ID)
	})

	assert.Equal(t, input.Scopes, created.Scopes)
	assert.Equal(t, input.GroupsClaim, created.GroupsClaim)
	assert.Equal(t, input.GroupPrefix, created.GroupPrefix)
	assert.Equal(t, input.AutoSyncGroups, created.AutoSyncGroups)
	assert.Equal(t, input.GetUserInfo, created.GetUserInfo)

	got, err := sso.GetProvider(ctx, db.Pool, testMasterKey, created.ID, "")
	require.NoError(t, err)
	assert.Equal(t, input.Scopes, got.Scopes)
	assert.Equal(t, input.GroupsClaim, got.GroupsClaim)
	assert.Equal(t, input.GroupPrefix, got.GroupPrefix)
	assert.Equal(t, input.AutoSyncGroups, got.AutoSyncGroups)
	assert.Equal(t, input.GetUserInfo, got.GetUserInfo)
}

func TestListEnabledProvidersForOrg(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	orgID := createTestOrg(t, db)

	// Create org-scoped provider.
	orgProvider, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           "Org Provider",
		ProviderType:   "oidc",
		ClientID:       "org-client-id",
		ClientSecret:   "org-secret",
		DiscoveryURL:   "https://org.example.com/.well-known/openid-configuration",
		AllowedDomains: []string{},
		Scopes:         []string{},
		Enabled:        true,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, orgProvider.ID)
	})

	// Create platform provider and enable for org.
	platformProvider, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, makePlatformProvider("list-enabled-test"))
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, platformProvider.ID)
	})

	err = sso.EnablePlatformProvider(ctx, db.Pool, orgID, platformProvider.ID)
	require.NoError(t, err)

	list, err := sso.ListEnabledProvidersForOrg(ctx, db.Pool, testMasterKey, orgID)
	require.NoError(t, err)

	ids := make(map[string]bool)
	for _, p := range list {
		ids[p.ID] = true
	}
	assert.True(t, ids[orgProvider.ID], "org-scoped provider should appear")
	assert.True(t, ids[platformProvider.ID], "enabled platform provider should appear")
}
