package api

import (
	"context"
	"fmt"
	"log"

	"github.com/heavenlabs/hnb/internal/sso"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedPlatformAdmin promotes the given email to platform admin if the user exists.
// Returns true if a user was promoted, false if the user doesn't exist yet.
// The caller should call SetPlatformAdminEmail so the promotion happens at
// registration instead if the user doesn't exist.
func SeedPlatformAdmin(ctx context.Context, pool *pgxpool.Pool, email string) (bool, error) {
	if email == "" {
		return false, nil
	}
	tag, err := pool.Exec(ctx,
		`UPDATE users SET is_platform_admin=true WHERE email=$1`, email)
	if err != nil {
		return false, fmt.Errorf("seed platform admin: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// SeedDevSSOProviders creates the dev Keycloak SSO provider if no providers exist.
// Only runs in dev (when master key is the dev default).
func SeedDevSSOProviders(ctx context.Context, pool *pgxpool.Pool, masterKey []byte) {
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM sso_providers`).Scan(&count); err != nil || count > 0 {
		return
	}

	keycloakProvider := sso.Provider{
		Name:         "Keycloak (Dev)",
		ProviderType: "oidc",
		ClientID:     "hnb-dev",
		ClientSecret: "hnb-dev-keycloak-secret",
		DiscoveryURL: "http://localhost:5557/realms/hnb-dev/.well-known/openid-configuration",
		AllowedDomains: []string{"hnb-dev.test"},
		Enabled:      true,
		Scopes:       []string{"openid", "profile", "email", "groups"},
		GroupsClaim:  "groups",
		AutoSyncGroups: true,
		GetUserInfo:  true,
		Scope:        "platform",
	}

	if _, err := sso.CreateProvider(ctx, pool, masterKey, keycloakProvider); err != nil {
		log.Printf("warning: failed to seed dev SSO provider: %v", err)
		return
	}
	log.Println("seeded dev SSO provider (Keycloak)")
}
