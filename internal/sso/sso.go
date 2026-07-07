package sso

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Provider is the decoded (decrypted) SSO provider record.
type Provider struct {
	ID             string
	Scope          string  // "platform" or "org"
	OrgID          *string // nil for platform providers
	Name           string
	ProviderType   string
	ClientID       string
	ClientSecret   string // decrypted
	DiscoveryURL   string
	AllowedDomains []string
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Scopes         []string `json:"scopes"`
	GroupsClaim    string   `json:"groups_claim"`
	GroupPrefix    string   `json:"group_prefix"`
	AutoSyncGroups bool     `json:"auto_sync_groups"`
	GetUserInfo    bool     `json:"get_user_info"`
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
		&p.AllowedDomains, &p.Enabled, &p.Scopes, &p.GroupsClaim, &p.GroupPrefix, &p.AutoSyncGroups, &p.GetUserInfo,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return Provider{}, "", err
	}
	if p.AllowedDomains == nil {
		p.AllowedDomains = []string{}
	}
	if p.Scopes == nil {
		p.Scopes = []string{}
	}
	return p, encSecret, nil
}

const selectProviderCols = `id, scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, scopes, groups_claim, group_prefix, auto_sync_groups, get_user_info, created_at, updated_at`

// CreateProvider inserts a new provider, encrypting the client_secret before storing.
func CreateProvider(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, p Provider) (Provider, error) {
	encSecret, err := encryptSecret(masterKey, p.ClientSecret)
	if err != nil {
		return Provider{}, err
	}

	if p.AllowedDomains == nil {
		p.AllowedDomains = []string{}
	}
	if p.Scopes == nil {
		p.Scopes = []string{}
	}

	row := pool.QueryRow(ctx,
		`INSERT INTO sso_providers (scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, scopes, groups_claim, group_prefix, auto_sync_groups, get_user_info)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING `+selectProviderCols,
		p.Scope, p.OrgID, p.Name, p.ProviderType, p.ClientID, encSecret, p.DiscoveryURL, p.AllowedDomains, p.Enabled,
		p.Scopes, p.GroupsClaim, p.GroupPrefix, p.AutoSyncGroups, p.GetUserInfo,
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
func GetProvider(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, id, orgID string) (Provider, error) {
	var row pgx.Row
	if orgID != "" {
		row = pool.QueryRow(ctx,
			`SELECT `+selectProviderCols+` FROM sso_providers WHERE id=$1 AND (org_id IS NULL OR org_id=$2)`, id, orgID,
		)
	} else {
		row = pool.QueryRow(ctx,
			`SELECT `+selectProviderCols+` FROM sso_providers WHERE id=$1`, id,
		)
	}

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

// GetCachedProvider loads a provider from Redis cache (60s TTL) or falls back to DB.
// Cache key: sso:provider:{id}
// If redisClient is nil (e.g. test environments without Redis), falls through to DB directly.
func GetCachedProvider(ctx context.Context, pool *pgxpool.Pool, redisClient *redis.Client, masterKey []byte, id, orgID string) (Provider, error) {
	key := "sso:provider:" + id

	if redisClient != nil {
		val, err := redisClient.Get(ctx, key).Bytes()
		if err == nil {
			var p Provider
			if jsonErr := json.Unmarshal(val, &p); jsonErr == nil {
				return p, nil
			}
		}
		// On redis.Nil or any other error, fall through to DB.
	}

	p, err := GetProvider(ctx, pool, masterKey, id, orgID)
	if err != nil {
		return Provider{}, err
	}

	if redisClient != nil {
		if data, jsonErr := json.Marshal(p); jsonErr == nil {
			// Ignore cache-set errors — non-fatal.
			redisClient.Set(ctx, key, data, 60*time.Second)
		}
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
			&p.AllowedDomains, &p.Enabled, &p.Scopes, &p.GroupsClaim, &p.GroupPrefix, &p.AutoSyncGroups, &p.GetUserInfo,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		if p.AllowedDomains == nil {
			p.AllowedDomains = []string{}
		}
		if p.Scopes == nil {
			p.Scopes = []string{}
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
// If p.ClientSecret is empty, the stored secret is preserved.
func UpdateProvider(ctx context.Context, pool *pgxpool.Pool, masterKey []byte, p Provider) (Provider, error) {
	if p.AllowedDomains == nil {
		p.AllowedDomains = []string{}
	}
	if p.Scopes == nil {
		p.Scopes = []string{}
	}

	var encSecret string
	var err error

	if p.ClientSecret != "" {
		encSecret, err = encryptSecret(masterKey, p.ClientSecret)
		if err != nil {
			return Provider{}, err
		}
	} else {
		var existingEnc string
		err := pool.QueryRow(ctx, `SELECT client_secret_enc FROM sso_providers WHERE id=$1`, p.ID).Scan(&existingEnc)
		if err != nil {
			return Provider{}, fmt.Errorf("update provider: failed to read existing secret: %w", err)
		}
		encSecret = existingEnc
	}

	row := pool.QueryRow(ctx,
		`UPDATE sso_providers
		 SET name=$1, client_id=$2, client_secret_enc=$3, discovery_url=$4, allowed_domains=$5, enabled=$6,
		     scopes=$7, groups_claim=$8, group_prefix=$9, auto_sync_groups=$10, get_user_info=$11,
		     updated_at=now()
		 WHERE id=$12
		 RETURNING `+selectProviderCols,
		p.Name, p.ClientID, encSecret, p.DiscoveryURL, p.AllowedDomains, p.Enabled,
		p.Scopes, p.GroupsClaim, p.GroupPrefix, p.AutoSyncGroups, p.GetUserInfo, p.ID,
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

// ProbeResult is the public-safe view of a provider returned by the probe endpoint.
// It never includes client_secret, discovery_url, or org_id.
type ProbeResult struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProviderType string `json:"provider_type"`
}

// ListProvidersByDomain returns active providers across all orgs whose allowed_domains contains domain.
// Never returns client_secret.
func ListProvidersByDomain(ctx context.Context, pool *pgxpool.Pool, domain string) ([]ProbeResult, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name, provider_type
		 FROM sso_providers
		 WHERE enabled = true
		   AND $1 = ANY(allowed_domains)`,
		domain,
	)
	if err != nil {
		return nil, fmt.Errorf("list providers by domain: %w", err)
	}
	defer rows.Close()

	var results []ProbeResult
	for rows.Next() {
		var r ProbeResult
		if err := rows.Scan(&r.ID, &r.Name, &r.ProviderType); err != nil {
			return nil, fmt.Errorf("scan probe result: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	if results == nil {
		results = []ProbeResult{}
	}
	return results, nil
}

// ListProvidersByDomainForOrg returns providers for a specific org whose allowed_domains contains domain.
// Includes both org-scoped providers and platform providers enabled for that org.
func ListProvidersByDomainForOrg(ctx context.Context, pool *pgxpool.Pool, domain, orgID string) ([]ProbeResult, error) {
	rows, err := pool.Query(ctx,
		`SELECT sp.id, sp.name, sp.provider_type
		 FROM sso_providers sp
		 WHERE sp.enabled = true
		   AND $1 = ANY(sp.allowed_domains)
		   AND (
		     (sp.scope = 'org' AND sp.org_id = $2)
		     OR
		     (sp.scope = 'platform' AND EXISTS (
		       SELECT 1 FROM org_platform_providers opp
		       WHERE opp.provider_id = sp.id AND opp.org_id = $2
		     ))
		   )`,
		domain, orgID,
	)
	if err != nil {
		return nil, fmt.Errorf("list providers by domain for org: %w", err)
	}
	defer rows.Close()

	var results []ProbeResult
	for rows.Next() {
		var r ProbeResult
		if err := rows.Scan(&r.ID, &r.Name, &r.ProviderType); err != nil {
			return nil, fmt.Errorf("scan probe result: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	if results == nil {
		results = []ProbeResult{}
	}
	return results, nil
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
