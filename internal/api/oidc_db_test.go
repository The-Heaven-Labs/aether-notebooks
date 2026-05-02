package api_test

import (
	"context"
	"testing"

	"github.com/heavenlabs/hnb/internal/sso"
)

func TestOIDCProviderLoadedFromDB(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	// Insert a provider directly into the DB
	provider := sso.Provider{
		Scope:          "platform",
		Name:           "Test OIDC",
		ProviderType:   "oidc",
		ClientID:       "test-client-id",
		ClientSecret:   "test-secret",
		DiscoveryURL:   "https://example.com/.well-known/openid-configuration",
		AllowedDomains: []string{"example.com"},
		Enabled:        true,
	}

	key := s.MasterKey()
	created, err := sso.CreateProvider(ctx, s.DB().Pool, key, provider)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// Load via GetCachedProvider — should hit DB on first call
	loaded, err := sso.GetCachedProvider(ctx, s.DB().Pool, s.Cache.Client(), key, created.ID)
	if err != nil {
		t.Fatalf("GetCachedProvider: %v", err)
	}
	if loaded.ClientSecret != "test-secret" {
		t.Errorf("expected secret %q, got %q", "test-secret", loaded.ClientSecret)
	}

	// Second call should hit Redis cache
	loaded2, err := sso.GetCachedProvider(ctx, s.DB().Pool, s.Cache.Client(), key, created.ID)
	if err != nil {
		t.Fatalf("GetCachedProvider (cached): %v", err)
	}
	if loaded2.ClientID != loaded.ClientID {
		t.Error("cached result differs from DB result")
	}
}
