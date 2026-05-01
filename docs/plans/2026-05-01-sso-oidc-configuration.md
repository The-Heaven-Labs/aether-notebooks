# SSO / OIDC Configuration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow platform admins and org admins to configure OIDC SSO providers at runtime via UI, replacing the current startup-config-only approach.

**Architecture:** Single `sso_providers` table scoped to `platform` or `org`; org admins enable platform providers via a join table. Redis (already in infra) becomes required for multi-pod correctness: OAuth2 state tokens, provider cache, and probe rate limiting all move there. Login page becomes email-first with domain-based provider lookup to prevent email enumeration.

**Tech Stack:** Go + pgx (existing), `github.com/redis/go-redis/v9` (new), React + TanStack Query (existing). Design doc: `docs/plans/2026-05-01-sso-oidc-configuration-design.md`.

---

### Task 1: Add Redis dependency

**Files:**
- Modify: `go.mod`
- Create: `internal/cache/cache.go`
- Create: `internal/cache/cache_test.go`

**Step 1: Add the go-redis module**

```bash
go get github.com/redis/go-redis/v9
go mod tidy
```

Expected: `go.mod` now lists `github.com/redis/go-redis/v9`.

**Step 2: Write a failing test**

`internal/cache/cache_test.go`:
```go
package cache_test

import (
	"context"
	"testing"
	"github.com/heavenlabs/hnb/internal/cache"
)

func TestPing(t *testing.T) {
	c, err := cache.New("redis://localhost:6379")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer c.Close()
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
```

Run: `go test ./internal/cache/... -v`
Expected: FAIL — package does not exist yet.

**Step 3: Implement the cache package**

`internal/cache/cache.go`:
```go
package cache

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
)

type Cache struct {
	client *redis.Client
}

func New(redisURL string) (*Cache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &Cache{client: redis.NewClient(opts)}, nil
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	return c.client.Close()
}

func (c *Cache) Client() *redis.Client {
	return c.client
}
```

**Step 4: Run test**

```bash
task infra:up
go test ./internal/cache/... -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/cache/ go.mod go.sum
git commit -m "feat: add Redis cache package"
```

---

### Task 2: Database migration — SSO tables

**Files:**
- Create: `internal/database/migrations/013_sso_providers.sql`

**Step 1: Write the migration**

```sql
-- 013_sso_providers.sql

CREATE TABLE sso_providers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope           TEXT NOT NULL CHECK (scope IN ('platform', 'org')),
    org_id          UUID REFERENCES orgs(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    provider_type   TEXT NOT NULL DEFAULT 'oidc',
    client_id       TEXT NOT NULL,
    client_secret_enc TEXT NOT NULL,
    discovery_url   TEXT NOT NULL,
    allowed_domains TEXT[] NOT NULL DEFAULT '{}',
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT sso_scope_org CHECK (
        (scope = 'platform' AND org_id IS NULL) OR
        (scope = 'org' AND org_id IS NOT NULL)
    )
);

CREATE INDEX idx_sso_providers_org ON sso_providers(org_id) WHERE org_id IS NOT NULL;
CREATE INDEX idx_sso_providers_scope ON sso_providers(scope);
CREATE INDEX idx_sso_providers_domains ON sso_providers USING gin(allowed_domains);

CREATE TABLE org_platform_providers (
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    PRIMARY KEY (org_id, provider_id)
);

ALTER TABLE orgs ADD COLUMN sso_password_login BOOLEAN NOT NULL DEFAULT TRUE;
```

**Step 2: Verify migration runs**

```bash
task dev &   # starts server which auto-runs migrations
# check logs for "migrations applied" without error
```

**Step 3: Commit**

```bash
git add internal/database/migrations/013_sso_providers.sql
git commit -m "feat: migration 013 — sso_providers and org_platform_providers tables"
```

---

### Task 3: Wire Redis into Server

**Files:**
- Modify: `internal/api/router.go`
- Modify: `cmd/hnb-server/main.go`

**Step 1: Add Redis field to Server**

In `internal/api/router.go`, update the `Server` struct and constructor:

```go
import "github.com/heavenlabs/hnb/internal/cache"

type Server struct {
	db            *database.DB
	jwt           *auth.JWTIssuer
	audit         *audit.Logger
	masterKey     []byte
	cache         *cache.Cache          // new
	hub           *Hub
	mux           *http.ServeMux
	oidcProviders map[string]auth.OIDCProvider
	attachmentDir string
}

func NewServer(db *database.DB, jwt *auth.JWTIssuer, auditLogger *audit.Logger, masterKey []byte, c *cache.Cache, oidcProviders map[string]auth.OIDCProvider) *Server {
	s := &Server{
		db:            db,
		jwt:           jwt,
		audit:         auditLogger,
		masterKey:     masterKey,
		cache:         c,
		hub:           NewHub(),
		mux:           http.NewServeMux(),
		oidcProviders: oidcProviders,
	}
	s.routes()
	return s
}
```

**Step 2: Wire Redis in main.go**

In `cmd/hnb-server/main.go`, after `db.Migrate`:

```go
import "github.com/heavenlabs/hnb/internal/cache"

// Connect to Redis
redisCache, err := cache.New(cfg.RedisURL)
if err != nil {
    log.Fatalf("redis: %v", err)
}
if err := redisCache.Ping(ctx); err != nil {
    log.Fatalf("redis ping: %v", err)
}
defer redisCache.Close()
log.Println("redis connected")

// Then pass to NewServer:
srv := api.NewServer(db, jwtIssuer, auditLogger, masterKey, redisCache, nil)
```

**Step 3: Fix any test compilation breakage**

Tests that call `api.NewServer` need the new `cache` parameter. In `internal/api/testhelpers_test.go`, pass `nil` for the cache:

```go
srv := api.NewServer(db, jwtIssuer, auditLogger, masterKey, nil, nil)
```

**Step 4: Build check**

```bash
go build ./...
```
Expected: SUCCESS.

**Step 5: Commit**

```bash
git add internal/api/router.go cmd/hnb-server/main.go internal/api/testhelpers_test.go
git commit -m "feat: wire Redis cache into Server"
```

---

### Task 4: Migrate OAuth2 state store to Redis

**Files:**
- Modify: `internal/api/oidc_handlers.go`

**Step 1: Write a failing test**

In `internal/api/oidc_handlers_test.go` (create if needed):
```go
func TestOIDCStateRoundTrip(t *testing.T) {
	// Two separate servers sharing one Redis — simulates multi-pod
	s1 := setupTestServer(t)
	s2 := setupTestServer(t)

	// Issue state on s1
	state, err := generateState()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s1.SetOIDCState(ctx, state); err != nil {
		t.Fatalf("set: %v", err)
	}

	// Consume on s2 — must succeed
	if !s2.ConsumeOIDCState(ctx, state) {
		t.Error("expected state to be consumable on a different server instance")
	}

	// Second consume must fail (single-use)
	if s1.ConsumeOIDCState(ctx, state) {
		t.Error("state should not be consumable twice")
	}
}
```

Run: `go test ./internal/api/... -run TestOIDCState -v`
Expected: FAIL — methods do not exist.

> Note: this test will need `setupTestServer` to provide a real Redis. Update `testhelpers_test.go` to create a real `cache.New("redis://localhost:6379")` instance. The test suite already requires infra (`task test` starts it).

**Step 2: Replace globalStateStore with Redis methods on Server**

In `internal/api/oidc_handlers.go`, remove `oidcStateStore`, `globalStateStore`, and update:

```go
const oidcStateTTL = 10 * time.Minute

func (s *Server) SetOIDCState(ctx context.Context, state string) error {
	key := "oidc:state:" + state
	return s.cache.Client().Set(ctx, key, "1", oidcStateTTL).Err()
}

func (s *Server) ConsumeOIDCState(ctx context.Context, state string) bool {
	key := "oidc:state:" + state
	val, err := s.cache.Client().GetDel(ctx, key).Result()
	return err == nil && val == "1"
}
```

Update `handleOIDCLogin` to call `s.SetOIDCState(ctx, state)` and `handleOIDCCallback` to call `s.ConsumeOIDCState(ctx, state)`.

Add a nil-cache guard for tests that pass `nil`:
```go
func (s *Server) SetOIDCState(ctx context.Context, state string) error {
	if s.cache == nil {
		return nil // tests without Redis skip state validation
	}
	// ...
}
```

**Step 3: Run test**

```bash
task infra:up
go test ./internal/api/... -run TestOIDCState -v
```
Expected: PASS.

**Step 4: Commit**

```bash
git add internal/api/oidc_handlers.go internal/api/oidc_handlers_test.go internal/api/testhelpers_test.go
git commit -m "feat: migrate OAuth2 state store from in-memory to Redis"
```

---

### Task 5: SSO provider DB helpers

**Files:**
- Create: `internal/sso/sso.go`
- Create: `internal/sso/sso_test.go`

**Step 1: Write failing tests**

`internal/sso/sso_test.go`:
```go
package sso_test

import (
	"context"
	"testing"
	"github.com/heavenlabs/hnb/internal/sso"
	// use a real DB from testhelpers pattern
)

func TestCreateAndGetPlatformProvider(t *testing.T) {
	// setup: real DB, real masterKey
	store := sso.NewStore(db, masterKey)
	ctx := context.Background()

	id, err := store.CreateProvider(ctx, sso.Provider{
		Scope:          "platform",
		Name:           "Test Google",
		ClientID:       "client-id",
		ClientSecret:   "secret",
		DiscoveryURL:   "https://accounts.google.com",
		AllowedDomains: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	p, err := store.GetProvider(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Name != "Test Google" {
		t.Errorf("name mismatch: got %q", p.Name)
	}
	if p.ClientSecret != "secret" {
		t.Error("client secret should round-trip through encryption")
	}
}

func TestDomainLookup(t *testing.T) {
	store := sso.NewStore(db, masterKey)
	ctx := context.Background()

	// Create platform provider for example.com, enable for org
	id, _ := store.CreateProvider(ctx, sso.Provider{
		Scope: "platform", Name: "Corp SSO",
		ClientID: "cid", ClientSecret: "cs",
		DiscoveryURL:   "https://sso.example.com",
		AllowedDomains: []string{"example.com"},
	})
	_ = store.EnablePlatformProvider(ctx, orgID, id)

	providers, err := store.ProvidersForDomain(ctx, "example.com")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(providers))
	}
	if providers[0].ID != id {
		t.Error("wrong provider returned")
	}
}
```

Run: `go test ./internal/sso/... -v`
Expected: FAIL — package does not exist.

**Step 2: Implement the SSO store**

`internal/sso/sso.go`:
```go
package sso

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/heavenlabs/hnb/internal/crypto"
	"github.com/heavenlabs/hnb/internal/database"
)

type Provider struct {
	ID             string
	Scope          string   // "platform" or "org"
	OrgID          string   // empty for platform scope
	Name           string
	ProviderType   string
	ClientID       string
	ClientSecret   string   // plaintext; encrypted at rest
	DiscoveryURL   string
	AllowedDomains []string
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ProbeResult is what the unauthenticated probe endpoint returns — no secrets.
type ProbeResult struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProviderType string `json:"provider_type"`
}

type Store struct {
	db        *database.DB
	masterKey []byte
}

func NewStore(db *database.DB, masterKey []byte) *Store {
	return &Store{db: db, masterKey: masterKey}
}

func (s *Store) encryptSecret(plain string) (string, error) {
	enc, err := crypto.Encrypt([]byte(plain), s.masterKey)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(enc), nil
}

func (s *Store) decryptSecret(enc string) (string, error) {
	raw, err := hex.DecodeString(enc)
	if err != nil {
		return "", err
	}
	plain, err := crypto.Decrypt(raw, s.masterKey)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Store) CreateProvider(ctx context.Context, p Provider) (string, error) {
	enc, err := s.encryptSecret(p.ClientSecret)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}
	if p.ProviderType == "" {
		p.ProviderType = "oidc"
	}
	orgID := nilIfEmpty(p.OrgID)
	var id string
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO sso_providers
		 (scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		p.Scope, orgID, p.Name, p.ProviderType, p.ClientID, enc, p.DiscoveryURL, p.AllowedDomains, true,
	).Scan(&id)
	return id, err
}

func (s *Store) GetProvider(ctx context.Context, id string) (*Provider, error) {
	return s.scanProvider(s.db.Pool.QueryRow(ctx,
		`SELECT id, scope, COALESCE(org_id::text,''), name, provider_type,
		        client_id, client_secret_enc, discovery_url, allowed_domains, enabled, created_at, updated_at
		 FROM sso_providers WHERE id = $1`, id))
}

func (s *Store) scanProvider(row interface{ Scan(...any) error }) (*Provider, error) {
	var p Provider
	var enc string
	if err := row.Scan(&p.ID, &p.Scope, &p.OrgID, &p.Name, &p.ProviderType,
		&p.ClientID, &enc, &p.DiscoveryURL, &p.AllowedDomains, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	secret, err := s.decryptSecret(enc)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	p.ClientSecret = secret
	return &p, nil
}

func (s *Store) ListPlatformProviders(ctx context.Context) ([]Provider, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, scope, COALESCE(org_id::text,''), name, provider_type,
		        client_id, client_secret_enc, discovery_url, allowed_domains, enabled, created_at, updated_at
		 FROM sso_providers WHERE scope = 'platform' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		p, err := s.scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) ListOrgProviders(ctx context.Context, orgID string) ([]Provider, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, scope, COALESCE(org_id::text,''), name, provider_type,
		        client_id, client_secret_enc, discovery_url, allowed_domains, enabled, created_at, updated_at
		 FROM sso_providers
		 WHERE scope = 'org' AND org_id = $1
		 UNION ALL
		 SELECT sp.id, sp.scope, COALESCE(sp.org_id::text,''), sp.name, sp.provider_type,
		        sp.client_id, sp.client_secret_enc, sp.discovery_url, sp.allowed_domains, sp.enabled, sp.created_at, sp.updated_at
		 FROM sso_providers sp
		 JOIN org_platform_providers opp ON opp.provider_id = sp.id AND opp.org_id = $1
		 WHERE sp.scope = 'platform' AND sp.enabled = TRUE
		 ORDER BY created_at`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		p, err := s.scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProvider(ctx context.Context, id string, p Provider) error {
	enc, err := s.encryptSecret(p.ClientSecret)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	_, err = s.db.Pool.Exec(ctx,
		`UPDATE sso_providers SET name=$1, client_id=$2, client_secret_enc=$3,
		 discovery_url=$4, allowed_domains=$5, enabled=$6, updated_at=NOW()
		 WHERE id=$7`,
		p.Name, p.ClientID, enc, p.DiscoveryURL, p.AllowedDomains, p.Enabled, id)
	return err
}

func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id = $1`, id)
	return err
}

func (s *Store) EnablePlatformProvider(ctx context.Context, orgID, providerID string) error {
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO org_platform_providers (org_id, provider_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		orgID, providerID)
	return err
}

func (s *Store) DisablePlatformProvider(ctx context.Context, orgID, providerID string) error {
	_, err := s.db.Pool.Exec(ctx,
		`DELETE FROM org_platform_providers WHERE org_id=$1 AND provider_id=$2`,
		orgID, providerID)
	return err
}

// ProvidersForDomain returns lightweight probe results for a given email domain.
// Does NOT decrypt or return secrets.
func (s *Store) ProvidersForDomain(ctx context.Context, domain string) ([]ProbeResult, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT DISTINCT sp.id, sp.name, sp.provider_type
		 FROM sso_providers sp
		 LEFT JOIN org_platform_providers opp ON opp.provider_id = sp.id
		 WHERE sp.enabled = TRUE
		   AND $1 = ANY(sp.allowed_domains)
		   AND (sp.scope = 'platform' OR sp.scope = 'org')
		 ORDER BY sp.name`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProbeResult
	for rows.Next() {
		var r ProbeResult
		if err := rows.Scan(&r.ID, &r.Name, &r.ProviderType); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateOrgPasswordLogin sets whether password login is available when SSO is configured.
func (s *Store) UpdateOrgPasswordLogin(ctx context.Context, orgID string, allowed bool) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE orgs SET sso_password_login=$1 WHERE id=$2`, allowed, orgID)
	return err
}

// OrgPasswordLogin returns the sso_password_login setting for an org.
func (s *Store) OrgPasswordLogin(ctx context.Context, orgID string) (bool, error) {
	var v bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT sso_password_login FROM orgs WHERE id=$1`, orgID).Scan(&v)
	return v, err
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

**Step 3: Run tests**

```bash
task infra:up
go test ./internal/sso/... -v
```
Expected: PASS.

**Step 4: Commit**

```bash
git add internal/sso/
git commit -m "feat: SSO provider store with encryption and domain lookup"
```

---

### Task 6: Platform admin API handlers

**Files:**
- Create: `internal/api/sso_admin_handlers.go`
- Create: `internal/api/sso_admin_handlers_test.go`
- Modify: `internal/api/router.go`

**Step 1: Write failing tests**

`internal/api/sso_admin_handlers_test.go`:
```go
func TestAdminSSOCRUD(t *testing.T) {
	srv, token := setupTestServerWithPlatformAdmin(t)

	// Create
	body := `{"name":"Test","client_id":"cid","client_secret":"secret",
	           "discovery_url":"https://issuer.example","allowed_domains":["example.com"]}`
	resp := doRequest(t, srv, "POST", "/api/v1/admin/sso/providers", token, body)
	assert.Equal(t, 201, resp.Code)
	var created map[string]any
	json.Unmarshal(resp.Body.Bytes(), &created)
	id := created["id"].(string)

	// List
	resp = doRequest(t, srv, "GET", "/api/v1/admin/sso/providers", token, "")
	assert.Equal(t, 200, resp.Code)

	// Update
	resp = doRequest(t, srv, "PUT", "/api/v1/admin/sso/providers/"+id, token,
		`{"name":"Updated","client_id":"cid","client_secret":"secret",
		  "discovery_url":"https://issuer.example","allowed_domains":["example.com"],"enabled":true}`)
	assert.Equal(t, 200, resp.Code)

	// Non-admin returns 403
	_, regularToken := setupTestServerMember(t, "viewer")
	resp = doRequest(t, srv, "GET", "/api/v1/admin/sso/providers", regularToken, "")
	assert.Equal(t, 403, resp.Code)

	// Delete
	resp = doRequest(t, srv, "DELETE", "/api/v1/admin/sso/providers/"+id, token, "")
	assert.Equal(t, 204, resp.Code)
}
```

Run: `go test ./internal/api/... -run TestAdminSSO -v`
Expected: FAIL — routes do not exist.

**Step 2: Implement handlers**

`internal/api/sso_admin_handlers.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"github.com/heavenlabs/hnb/internal/sso"
)

func (s *Server) ssoStore() *sso.Store {
	return sso.NewStore(s.db, s.masterKey)
}

func (s *Server) handleAdminListSSOProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.ssoStore().ListPlatformProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleAdminCreateSSOProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string   `json:"name"`
		ClientID       string   `json:"client_id"`
		ClientSecret   string   `json:"client_secret"`
		DiscoveryURL   string   `json:"discovery_url"`
		AllowedDomains []string `json:"allowed_domains"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name == "" || req.ClientID == "" || req.ClientSecret == "" || req.DiscoveryURL == "" {
		writeError(w, http.StatusBadRequest, "name, client_id, client_secret, discovery_url required")
		return
	}
	id, err := s.ssoStore().CreateProvider(r.Context(), sso.Provider{
		Scope: "platform", Name: req.Name,
		ClientID: req.ClientID, ClientSecret: req.ClientSecret,
		DiscoveryURL: req.DiscoveryURL, AllowedDomains: req.AllowedDomains,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	s.invalidateSSOCache(r.Context(), "")
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleAdminUpdateSSOProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name           string   `json:"name"`
		ClientID       string   `json:"client_id"`
		ClientSecret   string   `json:"client_secret"`
		DiscoveryURL   string   `json:"discovery_url"`
		AllowedDomains []string `json:"allowed_domains"`
		Enabled        bool     `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	err := s.ssoStore().UpdateProvider(r.Context(), id, sso.Provider{
		Name: req.Name, ClientID: req.ClientID, ClientSecret: req.ClientSecret,
		DiscoveryURL: req.DiscoveryURL, AllowedDomains: req.AllowedDomains, Enabled: req.Enabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	s.invalidateSSOCache(r.Context(), "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAdminDeleteSSOProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.ssoStore().DeleteProvider(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.invalidateSSOCache(r.Context(), "")
	w.WriteHeader(http.StatusNoContent)
}

// invalidateSSOCache deletes the Redis cache key for an org (empty string = all platform keys).
func (s *Server) invalidateSSOCache(ctx context.Context, orgID string) {
	if s.cache == nil {
		return
	}
	if orgID != "" {
		s.cache.Client().Del(ctx, "sso:providers:"+orgID)
	}
	// Platform changes affect all orgs — flush by pattern (acceptable for low-frequency admin op)
	keys, _ := s.cache.Client().Keys(ctx, "sso:providers:*").Result()
	if len(keys) > 0 {
		s.cache.Client().Del(ctx, keys...)
	}
}
```

**Step 3: Register routes in `router.go`**

```go
// SSO — platform admin
s.mux.Handle("GET /api/v1/admin/sso/providers",        authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListSSOProviders))))
s.mux.Handle("POST /api/v1/admin/sso/providers",       authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminCreateSSOProvider))))
s.mux.Handle("PUT /api/v1/admin/sso/providers/{id}",   authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminUpdateSSOProvider))))
s.mux.Handle("DELETE /api/v1/admin/sso/providers/{id}",authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminDeleteSSOProvider))))
```

**Step 4: Run tests**

```bash
go test ./internal/api/... -run TestAdminSSO -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/sso_admin_handlers.go internal/api/sso_admin_handlers_test.go internal/api/router.go
git commit -m "feat: platform admin SSO provider CRUD endpoints"
```

---

### Task 7: Org admin SSO handlers

**Files:**
- Create: `internal/api/sso_org_handlers.go`
- Create: `internal/api/sso_org_handlers_test.go`
- Modify: `internal/api/router.go`

**Step 1: Write failing tests**

```go
func TestOrgSSOCRUD(t *testing.T) {
	srv, adminToken, orgID := setupTestServerWithOrgAdmin(t)

	// Create org provider
	body := `{"name":"My Okta","client_id":"cid","client_secret":"secret",
	           "discovery_url":"https://my.okta.com","allowed_domains":["myco.com"]}`
	resp := doRequest(t, srv, "POST", "/api/v1/sso/providers", adminToken, body)
	assert.Equal(t, 201, resp.Code)

	// List shows org provider
	resp = doRequest(t, srv, "GET", "/api/v1/sso/providers", adminToken, "")
	assert.Equal(t, 200, resp.Code)

	// Editor cannot manage SSO
	_, editorToken := addOrgMember(t, srv, orgID, "editor")
	resp = doRequest(t, srv, "POST", "/api/v1/sso/providers", editorToken, body)
	assert.Equal(t, 403, resp.Code)

	// Update password login setting
	resp = doRequest(t, srv, "PUT", "/api/v1/sso/settings", adminToken,
		`{"sso_password_login":false}`)
	assert.Equal(t, 200, resp.Code)
}
```

Run: `go test ./internal/api/... -run TestOrgSSO -v`
Expected: FAIL.

**Step 2: Implement handlers**

`internal/api/sso_org_handlers.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"github.com/heavenlabs/hnb/internal/sso"
)

func (s *Server) handleOrgListSSOProviders(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	providers, err := s.ssoStore().ListOrgProviders(r.Context(), claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	// Strip client secrets from response
	type safeProvider struct {
		ID             string   `json:"id"`
		Scope          string   `json:"scope"`
		Name           string   `json:"name"`
		ProviderType   string   `json:"provider_type"`
		ClientID       string   `json:"client_id"`
		DiscoveryURL   string   `json:"discovery_url"`
		AllowedDomains []string `json:"allowed_domains"`
		Enabled        bool     `json:"enabled"`
	}
	safe := make([]safeProvider, len(providers))
	for i, p := range providers {
		safe[i] = safeProvider{ID: p.ID, Scope: p.Scope, Name: p.Name, ProviderType: p.ProviderType,
			ClientID: p.ClientID, DiscoveryURL: p.DiscoveryURL, AllowedDomains: p.AllowedDomains, Enabled: p.Enabled}
	}
	writeJSON(w, http.StatusOK, safe)
}

func (s *Server) handleOrgCreateSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Name           string   `json:"name"`
		ClientID       string   `json:"client_id"`
		ClientSecret   string   `json:"client_secret"`
		DiscoveryURL   string   `json:"discovery_url"`
		AllowedDomains []string `json:"allowed_domains"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name == "" || req.ClientID == "" || req.ClientSecret == "" || req.DiscoveryURL == "" {
		writeError(w, http.StatusBadRequest, "name, client_id, client_secret, discovery_url required")
		return
	}
	id, err := s.ssoStore().CreateProvider(r.Context(), sso.Provider{
		Scope: "org", OrgID: claims.OrgID, Name: req.Name,
		ClientID: req.ClientID, ClientSecret: req.ClientSecret,
		DiscoveryURL: req.DiscoveryURL, AllowedDomains: req.AllowedDomains,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create failed")
		return
	}
	s.invalidateSSOCache(r.Context(), claims.OrgID)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleOrgUpdateSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	// Verify provider belongs to this org
	p, err := s.ssoStore().GetProvider(r.Context(), id)
	if err != nil || p.OrgID != claims.OrgID || p.Scope != "org" {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	var req struct {
		Name           string   `json:"name"`
		ClientID       string   `json:"client_id"`
		ClientSecret   string   `json:"client_secret"`
		DiscoveryURL   string   `json:"discovery_url"`
		AllowedDomains []string `json:"allowed_domains"`
		Enabled        bool     `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.ssoStore().UpdateProvider(r.Context(), id, sso.Provider{
		Name: req.Name, ClientID: req.ClientID, ClientSecret: req.ClientSecret,
		DiscoveryURL: req.DiscoveryURL, AllowedDomains: req.AllowedDomains, Enabled: req.Enabled,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	s.invalidateSSOCache(r.Context(), claims.OrgID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOrgDeleteSSOProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	p, err := s.ssoStore().GetProvider(r.Context(), id)
	if err != nil || p.OrgID != claims.OrgID || p.Scope != "org" {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	if err := s.ssoStore().DeleteProvider(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.invalidateSSOCache(r.Context(), claims.OrgID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOrgListPlatformProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.ssoStore().ListPlatformProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleOrgEnablePlatformProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	if err := s.ssoStore().EnablePlatformProvider(r.Context(), claims.OrgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "enable failed")
		return
	}
	s.invalidateSSOCache(r.Context(), claims.OrgID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOrgDisablePlatformProvider(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	if err := s.ssoStore().DisablePlatformProvider(r.Context(), claims.OrgID, id); err != nil {
		writeError(w, http.StatusInternalServerError, "disable failed")
		return
	}
	s.invalidateSSOCache(r.Context(), claims.OrgID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOrgUpdateSSOSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		SSOPasswordLogin bool `json:"sso_password_login"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.ssoStore().UpdateOrgPasswordLogin(r.Context(), claims.OrgID, req.SSOPasswordLogin); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

**Step 3: Register routes**

In `router.go`, inside `routes()`:
```go
// SSO — org admin
orgAdminMW := func(h http.Handler) http.Handler { return authMW(RequireRole("admin")(h)) }
s.mux.Handle("GET /api/v1/sso/providers",                          orgAdminMW(http.HandlerFunc(s.handleOrgListSSOProviders)))
s.mux.Handle("POST /api/v1/sso/providers",                         orgAdminMW(http.HandlerFunc(s.handleOrgCreateSSOProvider)))
s.mux.Handle("PUT /api/v1/sso/providers/{id}",                     orgAdminMW(http.HandlerFunc(s.handleOrgUpdateSSOProvider)))
s.mux.Handle("DELETE /api/v1/sso/providers/{id}",                  orgAdminMW(http.HandlerFunc(s.handleOrgDeleteSSOProvider)))
s.mux.Handle("GET /api/v1/sso/platform-providers",                 orgAdminMW(http.HandlerFunc(s.handleOrgListPlatformProviders)))
s.mux.Handle("POST /api/v1/sso/platform-providers/{id}/enable",    orgAdminMW(http.HandlerFunc(s.handleOrgEnablePlatformProvider)))
s.mux.Handle("DELETE /api/v1/sso/platform-providers/{id}/enable",  orgAdminMW(http.HandlerFunc(s.handleOrgDisablePlatformProvider)))
s.mux.Handle("PUT /api/v1/sso/settings",                           orgAdminMW(http.HandlerFunc(s.handleOrgUpdateSSOSettings)))
```

**Step 4: Run tests**

```bash
go test ./internal/api/... -run TestOrgSSO -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/sso_org_handlers.go internal/api/sso_org_handlers_test.go internal/api/router.go
git commit -m "feat: org admin SSO provider management endpoints"
```

---

### Task 8: Probe endpoint + rate limiting

**Files:**
- Create: `internal/api/sso_probe_handlers.go`
- Create: `internal/api/sso_probe_handlers_test.go`
- Modify: `internal/api/router.go`

**Step 1: Write failing tests**

```go
func TestSSOProbe(t *testing.T) {
	srv, _ := setupTestServerWithPlatformAdmin(t)

	// No providers — returns empty list (not an error)
	resp := doRequest(t, srv, "GET", "/api/v1/auth/sso-providers?email=alice@unknown.com", "", "")
	assert.Equal(t, 200, resp.Code)
	var body []any
	json.Unmarshal(resp.Body.Bytes(), &body)
	assert.Empty(t, body)

	// Add a platform provider for example.com
	// ... (setup helpers) ...

	// Known domain returns provider
	resp = doRequest(t, srv, "GET", "/api/v1/auth/sso-providers?email=alice@example.com", "", "")
	assert.Equal(t, 200, resp.Code)
	json.Unmarshal(resp.Body.Bytes(), &body)
	assert.Len(t, body, 1)

	// Registered vs unregistered at known domain — same response (enumeration prevention)
	resp2 := doRequest(t, srv, "GET", "/api/v1/auth/sso-providers?email=nobody@example.com", "", "")
	assert.Equal(t, resp.Body.String(), resp2.Body.String())
}

func TestSSOProbeRateLimit(t *testing.T) {
	srv, _ := setupTestServer(t)
	// Fire 21 requests from same IP
	for i := 0; i < 21; i++ {
		resp := doRequestWithIP(t, srv, "GET", "/api/v1/auth/sso-providers?email=x@y.com", "192.0.2.1")
		if i < 20 {
			assert.Equal(t, 200, resp.Code)
		} else {
			assert.Equal(t, 429, resp.Code)
		}
	}
}
```

Run: `go test ./internal/api/... -run TestSSOProbe -v`
Expected: FAIL.

**Step 2: Implement probe handler with caching and rate limiting**

`internal/api/sso_probe_handlers.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/heavenlabs/hnb/internal/sso"
)

const (
	ssoProviderCacheTTL = 60 * time.Second
	ssoRateLimitWindow  = time.Minute
	ssoRateLimitMax     = 20
)

func (s *Server) handleSSOProbe(w http.ResponseWriter, r *http.Request) {
	// Rate limit by IP
	ip := clientIP(r)
	if s.cache != nil && !s.checkSSOProbeRateLimit(r.Context(), ip) {
		writeError(w, http.StatusTooManyRequests, "too many requests")
		return
	}

	email := r.URL.Query().Get("email")
	domain := emailDomain(email)

	if domain == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	// Check cache
	if s.cache != nil {
		cacheKey := "sso:probe:" + domain
		cached, err := s.cache.Client().Get(r.Context(), cacheKey).Bytes()
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(cached)
			return
		}
	}

	providers, err := s.ssoStore().ProvidersForDomain(r.Context(), domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if providers == nil {
		providers = []sso.ProbeResult{}
	}

	data, _ := json.Marshal(providers)

	// Cache the result
	if s.cache != nil {
		s.cache.Client().Set(r.Context(), "sso:probe:"+domain, data, ssoProviderCacheTTL)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *Server) checkSSOProbeRateLimit(ctx context.Context, ip string) bool {
	key := "ratelimit:sso-probe:" + ip
	count, err := s.cache.Client().Incr(ctx, key).Result()
	if err != nil {
		return true // fail open if Redis has issues
	}
	if count == 1 {
		s.cache.Client().Expire(ctx, key, ssoRateLimitWindow)
	}
	return count <= ssoRateLimitMax
}
```

Note: `emailDomain` is already defined in `auth_handlers.go` — reuse it.

**Step 3: Register route** (unauthenticated — no middleware)

```go
s.mux.HandleFunc("GET /api/v1/auth/sso-providers", s.handleSSOProbe)
```

**Step 4: Run tests**

```bash
go test ./internal/api/... -run TestSSOProbe -v
```
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/sso_probe_handlers.go internal/api/sso_probe_handlers_test.go internal/api/router.go
git commit -m "feat: SSO probe endpoint with Redis cache and rate limiting"
```

---

### Task 9: Update OIDC redirect/callback to load from DB

**Files:**
- Modify: `internal/api/oidc_handlers.go`

**Step 1: Write a failing test**

```go
func TestOIDCLoginWithDBProvider(t *testing.T) {
	srv, adminToken, _ := setupTestServerWithOrgAdmin(t)

	// Create a provider in the DB
	body := `{"name":"Test","client_id":"cid","client_secret":"secret",
	          "discovery_url":"https://accounts.google.com","allowed_domains":["co.com"]}`
	resp := doRequest(t, srv, "POST", "/api/v1/sso/providers", adminToken, body)
	assert.Equal(t, 201, resp.Code)
	var created map[string]string
	json.Unmarshal(resp.Body.Bytes(), &created)
	id := created["id"]

	// Login redirect should work using the DB provider ID
	resp = doRequest(t, srv, "GET", "/api/v1/auth/oidc/"+id, "", "")
	// Expect a redirect (302) — the discovery URL lookup will fail in test but route should resolve
	assert.NotEqual(t, 404, resp.Code)
}
```

Run: `go test ./internal/api/... -run TestOIDCLoginWithDB -v`
Expected: FAIL or 404 (provider lookup misses DB).

**Step 2: Update `handleOIDCLogin` and `handleOIDCCallback`**

Replace `s.oidcProviders[providerName]` map lookup with DB lookup + in-memory `GenericOIDCProvider` construction:

```go
func (s *Server) loadOIDCProvider(ctx context.Context, providerID string) (auth.OIDCProvider, error) {
	p, err := s.ssoStore().GetProvider(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %w", err)
	}
	redirectURL := s.oidcRedirectURL(providerID)
	return auth.NewGenericOIDCProvider(ctx, p.Name, p.DiscoveryURL, p.ClientID, p.ClientSecret, redirectURL, nil)
}

func (s *Server) oidcRedirectURL(providerID string) string {
	// derive from request in handler; accept a base URL config or fall back to relative
	return "/api/v1/auth/oidc/" + providerID + "/callback"
}
```

Update both handlers to call `s.loadOIDCProvider(ctx, providerID)` instead of the map lookup. Keep `s.oidcProviders` map as a fallback for backward-compat with startup config (nil check: if not in DB, try map).

**Step 3: Build + run tests**

```bash
go build ./...
go test ./internal/api/... -v
```
Expected: all tests PASS.

**Step 4: Commit**

```bash
git add internal/api/oidc_handlers.go
git commit -m "feat: OIDC handlers load provider config from DB instead of startup map"
```

---

### Task 10: Frontend — Login page email-first flow

**Files:**
- Modify: `web/src/pages/LoginPage.tsx`

**Step 1: Plan the state machine**

Three steps:
1. `email` — email field + Continue
2. `choosing` — probe returned; show SSO buttons + optional password form
3. `password` — password-only (no SSO for this domain)

Probe returns `[]{ id, name, provider_type }` and also need to fetch `sso_password_login` for the org. Since the probe is domain-based and we don't know the org yet, include `password_login_allowed` in the probe response.

**Step 2: Update the probe handler to include password_login_allowed**

Back in `internal/api/sso_probe_handlers.go`, change the response to:
```go
type probeResponse struct {
	Providers          []sso.ProbeResult `json:"providers"`
	PasswordLoginAllowed bool            `json:"password_login_allowed"`
}
```

For the `password_login_allowed` field: query `orgs.sso_password_login` for any org whose provider matched. If no providers matched (empty list), always return `true`.

**Step 3: Update LoginPage.tsx**

Replace the current single-step form with a two-step flow:

```tsx
type Step = 'email' | 'providers'

interface SSOProvider { id: string; name: string; provider_type: string }
interface ProbeResult { providers: SSOProvider[]; password_login_allowed: boolean }

export function LoginPage() {
  const [step, setStep] = useState<Step>('email')
  const [email, setEmail] = useState('')
  const [probeResult, setProbeResult] = useState<ProbeResult | null>(null)
  const [probePending, setProbePending] = useState(false)
  // ... existing password/name/error state

  async function handleEmailContinue(e: FormEvent) {
    e.preventDefault()
    setProbePending(true)
    try {
      const res = await fetch(`/api/v1/auth/sso-providers?email=${encodeURIComponent(email)}`)
      const data: ProbeResult = await res.json()
      setProbeResult(data)
      setStep('providers')
    } finally {
      setProbePending(false)
    }
  }

  // Step 1: email entry
  if (step === 'email') {
    return (
      // ... brand panel + form panel with just the email field and Continue button
    )
  }

  // Step 2: show SSO buttons and/or password form
  return (
    // SSO buttons if providers.length > 0
    // divider "or" if both SSO and password shown
    // password form if password_login_allowed or providers.length === 0
    // "← Back" link to return to email step
  )
}
```

Key UX details:
- SSO button label: `Sign in with {provider.name}`
- SSO button click: `window.location.href = '/api/v1/auth/oidc/' + provider.id`
- Password form pre-fills the email field (user typed it already)
- "← Back" clears `probeResult` and returns to email step
- Keep the existing Register tab (email-first only applies to Sign In)

**Step 4: Verify in browser**

```bash
task dev:web
```

Test: type an email with no providers → should show password form. Type an unregistered email at a domain with providers → should show SSO buttons (same as registered email — enumeration prevention verified visually).

**Step 5: Commit**

```bash
git add web/src/pages/LoginPage.tsx
git commit -m "feat: login page email-first flow with SSO provider probe"
```

---

### Task 11: Frontend — /settings page (org SSO management)

**Files:**
- Create: `web/src/pages/OrgSettingsPage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/TopBar.tsx`

**Step 1: Create OrgSettingsPage**

Three sections on a single scrollable page:

**Platform providers section:**
```tsx
// Fetches GET /api/v1/sso/platform-providers
// For each: show name, discovery_url, and an enable/disable toggle
// Toggle calls POST or DELETE /api/v1/sso/platform-providers/{id}/enable
```

**Custom providers section:**
```tsx
// Fetches GET /api/v1/sso/providers (filtered to scope=org)
// "Add provider" button opens an inline form:
//   name, discovery_url, client_id, client_secret (masked), allowed_domains (comma-separated)
// Each existing provider has Edit and Delete buttons
// PUT /api/v1/sso/providers/{id} on save; DELETE on delete
```

**Login settings section:**
```tsx
// Fetches current setting from GET /api/v1/sso/settings (add this endpoint — returns { sso_password_login })
// Toggle: "Allow password login when SSO is configured"
// PUT /api/v1/sso/settings on change
```

Use `useQuery` / `useMutation` from TanStack Query. Follow the same card layout pattern used in the audit page and profile page.

**Step 2: Register route in App.tsx**

```tsx
import { OrgSettingsPage } from './pages/OrgSettingsPage'
// Inside router:
<Route path="/settings" element={<OrgSettingsPage />} />
```

**Step 3: Add settings link in TopBar.tsx**

Show "Settings" link in the profile dropdown for org admins (role === 'admin'):

```tsx
const role = localStorage.getItem('hnb_org_role') ?? ''
// In dropdown:
{role === 'admin' && (
  <Link to="/settings" style={styles.dropdownLink} onClick={() => setOpen(false)}>
    Organisation settings
  </Link>
)}
```

Note: `hnb_org_role` needs to be stored at login time — add it to `LoginPage.tsx` (already stores `hnb_user_name`, `hnb_user_email` etc.; add `role` from the login response).

**Step 4: Verify in browser**

```bash
task dev:web
```

Test as org admin: TopBar dropdown shows "Organisation settings" → navigates to `/settings` → all three sections visible → can add a provider and toggle enable.

**Step 5: Commit**

```bash
git add web/src/pages/OrgSettingsPage.tsx web/src/App.tsx web/src/components/TopBar.tsx web/src/pages/LoginPage.tsx
git commit -m "feat: org settings page for SSO management"
```

---

### Task 12: Frontend — /admin SSO tab

**Files:**
- Modify: `web/src/pages/AdminPage.tsx`

**Step 1: Add SSO tab to AdminPage**

The existing AdminPage has tabs for Orgs and Users. Add a third tab: **SSO Providers**.

```tsx
type Tab = 'orgs' | 'users' | 'sso'
const [tab, setTab] = useState<Tab>('orgs')
```

SSO tab content:
- Fetches `GET /api/v1/admin/sso/providers`
- Table: Name, Discovery URL, Allowed Domains, Enabled toggle, Edit button, Delete button
- "Add provider" button opens an inline form with the same fields as the org settings page
- Enabled toggle calls `PUT /api/v1/admin/sso/providers/{id}` with `enabled` flipped
- Delete calls `DELETE /api/v1/admin/sso/providers/{id}` with a confirmation dialog

Use the same table component (`StyledTable`) used by the audit page for visual consistency.

**Step 2: Verify in browser**

```bash
task dev:web
```

Log in as platform admin → TopBar shows "Admin" link → `/admin` → SSO tab visible → create a platform provider → verify it appears.

**Step 3: Commit**

```bash
git add web/src/pages/AdminPage.tsx
git commit -m "feat: platform admin SSO providers tab"
```

---

### Task 13: Add GET /api/v1/sso/settings endpoint

**Files:**
- Modify: `internal/api/sso_org_handlers.go`
- Modify: `internal/api/router.go`

The `OrgSettingsPage` needs to read the current `sso_password_login` value.

**Step 1: Add handler**

In `sso_org_handlers.go`:
```go
func (s *Server) handleOrgGetSSOSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	allowed, err := s.ssoStore().OrgPasswordLogin(r.Context(), claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sso_password_login": allowed})
}
```

**Step 2: Register route**

```go
s.mux.Handle("GET /api/v1/sso/settings", orgAdminMW(http.HandlerFunc(s.handleOrgGetSSOSettings)))
```

**Step 3: Build + run all tests**

```bash
go build ./...
go test ./... 2>&1 | grep -E "FAIL|ok"
```
Expected: all packages pass (ClickHouse test will fail if no CH running — pre-existing).

**Step 4: Final commit**

```bash
git add internal/api/sso_org_handlers.go internal/api/router.go
git commit -m "feat: GET /api/v1/sso/settings endpoint"
```
