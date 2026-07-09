package api

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/sso"
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
		Name:           "Keycloak (Dev)",
		ProviderType:   "oidc",
		ClientID:       "aether-dev",
		ClientSecret:   "aether-dev-keycloak-secret",
		DiscoveryURL:   "http://localhost:5557/realms/aether-dev",
		AllowedDomains: []string{"aether-dev.test"},
		Enabled:        true,
		Scopes:         []string{"openid", "profile", "email"},
		GroupsClaim:    "groups",
		AutoSyncGroups: true,
		GetUserInfo:    true,
		Scope:          "platform",
	}

	if _, err := sso.CreateProvider(ctx, pool, masterKey, keycloakProvider); err != nil {
		slog.Warn("failed to seed dev SSO provider", "error", err)
		return
	}
	slog.Info("seeded dev SSO provider (Keycloak)")
}

// SeedDevAuditS3Config creates the dev audit S3 config pointing to Garage if none exists.
func SeedDevAuditS3Config(ctx context.Context, pool *pgxpool.Pool) {
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM platform_audit_s3_config`).Scan(&count); err != nil || count > 0 {
		return
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO platform_audit_s3_config (endpoint, region, bucket, access_key, secret_key, use_role, batch_size, flush_interval_secs, enabled, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())`,
		"http://aether-garage:3900", "us-east-1", "aether-audit-logs",
		"GKacc5e39c17a68ea60adf92db", "557351ff6f3e11eb8a1810af9647f9af9d3b0ce2e5786d2b186d327f091ab65c",
		false, 100, 60, true,
	)
	if err != nil {
		slog.Warn("failed to seed dev audit S3 config", "error", err)
		return
	}
	slog.Info("seeded dev audit S3 config (Garage)")
}


