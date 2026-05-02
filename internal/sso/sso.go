package sso

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Provider is the decoded (decrypted) SSO provider record.
type Provider struct {
	ID             string
	Scope          string   // "platform" or "org"
	OrgID          *string  // nil for platform providers
	Name           string
	ProviderType   string
	ClientID       string
	ClientSecret   string // decrypted
	DiscoveryURL   string
	AllowedDomains []string
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// encryptSecret encrypts the client secret and encodes it as hex for text storage.
func encryptSecret(masterKey []byte, secret string) (string, error) {
	enc, err := crypto.Encrypt([]byte(secret), masterKey)
	if err != nil {
		return "", fmt.Errorf("encrypt secret: %w", err)
	}
	return hex.EncodeToString(enc), nil
}

// decryptSecret decodes hex and decrypts the client secret.
func decryptSecret(masterKey []byte, encHex string) (string, error) {
	enc, err := hex.DecodeString(encHex)
	if err != nil {
		return "", fmt.Errorf("decode secret hex: %w", err)
	}
	plain, err := crypto.Decrypt(enc, masterKey)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

// scanProvider scans a provider row (without client_secret_enc); caller must decrypt separately.
func scanProvider(row pgx.Row) (Provider, string, error) {
	var p Provider
	var encSecret string
	err := row.Scan(
		&p.ID, &p.Scope, &p.OrgID, &p.Name, &p.ProviderType,
		&p.ClientID, &encSecret, &p.DiscoveryURL,
		&p.AllowedDomains, &p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return Provider{}, "", err
	}
	if p.AllowedDomains == nil {
		p.AllowedDomains = []string{}
	}
	return p, encSecret, nil
}

const selectProviderCols = `id, scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, created_at, updated_at`

// CreateProvider inserts a new provider, encrypting the client_secret before storing.
func CreateProvider(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, p Provider) (Provider, error) {
	encSecret, err := encryptSecret(masterKey, p.ClientSecret)
	if err != nil {
		return Provider{}, err
	}

	row := pool.QueryRow(ctx,
		`INSERT INTO sso_providers (scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+selectProviderCols,
		p.Scope, p.OrgID, p.Name, p.ProviderType, p.ClientID, encSecret, p.DiscoveryURL, p.AllowedDomains, p.Enabled,
	)

	result, encFromDB, err := scanProvider(row)
	if err != nil {
		return Provider{}, fmt.Errorf("create provider: %w", err)
	}

	result.ClientSecret, err = decryptSecret(masterKey, encFromDB)
	if err != nil {
		return Provider{}, err
	}
	return result, nil
}

// GetProvider fetches and decrypts a single provider by ID.
// Returns pgx.ErrNoRows if not found.
func GetProvider(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, id string) (Provider, error) {
	row := pool.QueryRow(ctx,
		`SELECT `+selectProviderCols+` FROM sso_providers WHERE id=$1`, id,
	)

	p, encSecret, err := scanProvider(row)
	if err != nil {
		return Provider{}, err
	}

	p.ClientSecret, err = decryptSecret(masterKey, encSecret)
	if err != nil {
		return Provider{}, err
	}
	return p, nil
}

// ListPlatformProviders returns all platform-scoped providers.
func ListPlatformProviders(ctx context.Context, pool *pgxpool.Pool, masterKey []byte) ([]Provider, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+selectProviderCols+` FROM sso_providers WHERE scope='platform' ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list platform providers: %w", err)
	}
	defer rows.Close()
	return collectProviders(rows, masterKey)
}

// ListOrgProviders returns org-scoped providers for the given orgID.
func ListOrgProviders(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, orgID string) ([]Provider, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+selectProviderCols+` FROM sso_providers WHERE scope='org' AND org_id=$1 ORDER BY created_at`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("list org providers: %w", err)
	}
	defer rows.Close()
	return collectProviders(rows, masterKey)
}

// collectProviders iterates rows and decrypts each provider.
func collectProviders(rows pgx.Rows, masterKey []byte) ([]Provider, error) {
	var providers []Provider
	for rows.Next() {
		var p Provider
		var encSecret string
		if err := rows.Scan(
			&p.ID, &p.Scope, &p.OrgID, &p.Name, &p.ProviderType,
			&p.ClientID, &encSecret, &p.DiscoveryURL,
			&p.AllowedDomains, &p.Enabled, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		if p.AllowedDomains == nil {
			p.AllowedDomains = []string{}
		}
		var err error
		p.ClientSecret, err = decryptSecret(masterKey, encSecret)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	if providers == nil {
		providers = []Provider{}
	}
	return providers, nil
}

// UpdateProvider updates name, client_id, client_secret, discovery_url, allowed_domains, enabled.
func UpdateProvider(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, p Provider) (Provider, error) {
	encSecret, err := encryptSecret(masterKey, p.ClientSecret)
	if err != nil {
		return Provider{}, err
	}

	row := pool.QueryRow(ctx,
		`UPDATE sso_providers
		 SET name=$1, client_id=$2, client_secret_enc=$3, discovery_url=$4, allowed_domains=$5, enabled=$6, updated_at=now()
		 WHERE id=$7
		 RETURNING `+selectProviderCols,
		p.Name, p.ClientID, encSecret, p.DiscoveryURL, p.AllowedDomains, p.Enabled, p.ID,
	)

	result, encFromDB, err := scanProvider(row)
	if err != nil {
		return Provider{}, fmt.Errorf("update provider: %w", err)
	}

	result.ClientSecret, err = decryptSecret(masterKey, encFromDB)
	if err != nil {
		return Provider{}, err
	}
	return result, nil
}

// DeleteProvider removes a provider by ID.
func DeleteProvider(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	return nil
}

// EnablePlatformProvider adds a row to org_platform_providers (idempotent).
func EnablePlatformProvider(ctx context.Context, pool *pgxpool.Pool, orgID, providerID string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO org_platform_providers (org_id, provider_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		orgID, providerID,
	)
	if err != nil {
		return fmt.Errorf("enable platform provider: %w", err)
	}
	return nil
}

// DisablePlatformProvider removes the row from org_platform_providers.
func DisablePlatformProvider(ctx context.Context, pool *pgxpool.Pool, orgID, providerID string) error {
	_, err := pool.Exec(ctx,
		`DELETE FROM org_platform_providers WHERE org_id=$1 AND provider_id=$2`,
		orgID, providerID,
	)
	if err != nil {
		return fmt.Errorf("disable platform provider: %w", err)
	}
	return nil
}

// ListEnabledProvidersForOrg returns all active providers for an org:
// org-scoped providers + platform providers the org has enabled.
// Only returns providers where enabled=true.
func ListEnabledProvidersForOrg(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, orgID string) ([]Provider, error) {
	rows, err := pool.Query(ctx,
		`SELECT `+selectProviderCols+`
		 FROM sso_providers
		 WHERE enabled=true AND (
		     (scope='org' AND org_id=$1)
		     OR
		     (scope='platform' AND id IN (
		         SELECT provider_id FROM org_platform_providers WHERE org_id=$1
		     ))
		 )
		 ORDER BY created_at`,
		orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled providers for org: %w", err)
	}
	defer rows.Close()
	return collectProviders(rows, masterKey)
}
