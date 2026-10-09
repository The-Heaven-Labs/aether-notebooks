# ClickHouse Cloud Idle State Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

> **Dependency — read first.** This plan **assumes the connector-health plan has
> landed** on this branch (`docs/plans/2026-10-08-connector-health-plan.md`,
> design §5.2). Inference reads `connectors.last_success_at`, and the frontend
> Status cell must already render the persisted connector-health badge that this
> plan extends. Verify before Task 1:
>
> ```bash
> grep -n "last_success_at" internal/models/connector.go internal/api/connector_handlers.go
> grep -n "last_success_at" web/src/types/index.ts web/src/pages/ConnectorsPage.tsx
> ```
>
> If either command finds nothing, **stop** — land the connector-health plan
> first. If the health plan named its relative-time helper differently than
> expected, that does not matter: this plan's inference copy uses its own
> `formatRelativeAgo` helper (see Task 4) and never imports the health plan's
> helper.

**Goal:** Surface ClickHouse Cloud service idle state on the Connectors page — exactly, via optional per-connector Cloud API credentials, and probabilistically, via recorded query activity — without waking idle services.

**Architecture:** A new `GET /api/v1/connectors/{id}/cloud-state` endpoint decrypts optional Cloud API credentials out of the connector's encrypted config, reads the fixed `https://api.clickhouse.cloud` control-plane service resource (basic auth, 5 s timeout, cached ~45 s with in-flight dedupe), and returns `{"configured": false}`, an exact state, or a 200 with an error so the page degrades gracefully. The frontend adds an optional "ClickHouse Cloud API" form group plus an idle-timeout field, and renders a second status chip per ClickHouse connector: exact state when configured, `last_success_at`-based "likely idle" inference for `*.clickhouse.cloud` hosts otherwise. No migration: everything lives in the encrypted connector config.

**Tech Stack:** Go net/http ServeMux + pgx; React 18 + TypeScript + Vitest; no migration (config stored in `connectors.config_encrypted`).

---

## Design notes (where implementation choices refine the design doc)

The design (§5.3, D10–D11) is authoritative; these notes record where this plan pins down details it leaves open:

1. **Intended wording**: The design lists `running` / `idle` / `awaking` / `stopped` / `degraded` / `failed`; the ClickHouse Cloud API also returns `starting` / `provisioning`. This plan maps those extra states to neutral labels so the catch-all never prints a raw machine value for a real state.
2. **Single-flight crate**: the design says "in-flight dedupe per connector". This plan uses `golang.org/x/sync/singleflight`, which is already a required (indirect) module at `v0.22.0` with full `go.sum` entries. `go mod tidy` only promotes it to a direct requirement — no new dependency is introduced.
3. **Where the cache lives**: a `cloudStateCache` field on `Server` (initialized in `NewServer`), not a package-level map. Each test server gets a fresh cache and no test can leak state into another; the two things that must be package-level test seams are `cloudAPIBaseURL` and the shared `cloudStateFlight` group.
4. **Only successes are cached.** An upstream 401/500 is retried on the next poll so fixing credentials takes effect immediately rather than after the TTL. Config edits on a cached connector are visible within ≤45 s; acceptable for a status display.
5. **`ConfigSchema` is not a validation mechanism.** `internal/executor/driver.go`'s `ConfigSchema()` is documented as "for validation + future frontend form rendering" but is only consumed by `secretFieldSet` in the API layer today. The new `cloud_*` keys need no driver-schema entries: the driver's `json.Unmarshal` ignores unknown keys, the form is hand-built, and masking is done by extending `secretFieldSet`'s conservative default list exactly as the design prescribes. (Adding them to `ClickHouseDriver.ConfigSchema` would be inert documentation; this plan deliberately does not, to avoid a second place that must stay in sync.)
6. **Cloud-state reads never touch connector health.** D7 records only data-plane connection outcomes (`openQuery`, tests, introspection, agent execution). `GET /cloud-state` must **not** call `recordConnectorSuccess`/`recordConnectorFailure` — a control-plane read says nothing about whether the data plane is reachable.
7. **No audit, no rate limit, no flags.** The endpoint is a read that reveals no secret; it is gated only by connector `view`. The 45 s cache plus 60 s frontend poll bounds upstream traffic.
8. **Clearing cloud credentials**: non-secret `cloud_*` fields round-trip verbatim like `host`/`port`, so blanking them disables the feature; a blank `cloud_key_secret` on edit means "keep stored" (same contract as `password`), leaving an orphaned secret in the encrypted config until a new one is set. Harmless and consistent with existing secret handling.

**Preflight (run once, from the worktree root):**

```bash
# Real Postgres + Redis for the Go tests. Skips already-running services.
task infra:up

# The worktree has no web/node_modules — install once before Task 4.
cd web && npm ci && cd ..
```

All Go test commands below include the dev-DB env inline and `-timeout 3m` (per AGENTS.md). Frontend commands run from `web/`.

---

### Task 1: Mask the Cloud API key secret in connector config

**Files:**
- Modify: `internal/api/connector_handlers.go` — `secretFieldSet` (add `cloud_key_secret` to the conservative default key list)
- Test: `internal/api/connector_config_test.go` — add `TestSecretFieldSetIncludesClickHouseCloudKey`, `TestMaskedConnectorConfigClickHouseCloudSecret`
- Test: `internal/api/connector_handlers_test.go` — add `TestClickHouseCloudSecretMaskedAndPreservedOnUpdate`

**Step 1: Write the failing tests.**

Append to `internal/api/connector_config_test.go` (package `api`; existing imports `encoding/json`, `strings`, `testing`, `crypto`, `models` already cover this):

```go
func TestSecretFieldSetIncludesClickHouseCloudKey(t *testing.T) {
	keys := secretFieldSet(models.ConnectorClickHouse)
	if !keys["cloud_key_secret"] {
		t.Fatalf("expected cloud_key_secret in secret field set, got %v", keys)
	}
}

func TestMaskedConnectorConfigClickHouseCloudSecret(t *testing.T) {
	s := &Server{masterKey: []byte("0123456789abcdef0123456789abcdef")}
	enc, err := crypto.Encrypt([]byte(`{"host":"abc.clickhouse.cloud","cloud_org_id":"org-1","cloud_service_id":"svc-1","cloud_key_id":"key-1","cloud_key_secret":"super-secret"}`), s.masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	out := s.maskedConnectorConfig(models.ConnectorClickHouse, enc)
	var cfg map[string]any
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg["cloud_key_secret"] != "***" {
		t.Fatalf("expected masked cloud_key_secret, got %v", cfg["cloud_key_secret"])
	}
	if cfg["cloud_org_id"] != "org-1" || cfg["cloud_key_id"] != "key-1" {
		t.Fatalf("expected non-secret Cloud fields preserved, got %v", cfg)
	}
}
```

Append to `internal/api/connector_handlers_test.go` (package `api_test`). Add `"context"` and `"github.com/the-heaven-labs/aether/internal/crypto"` to that file's import block first:

```go
// The Cloud API key secret must be masked in every response and survive an
// update that leaves the edit form's secret field blank — proven by decrypting
// the stored config, since the API never returns the real value.
func TestClickHouseCloudSecretMaskedAndPreservedOnUpdate(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("ch-cloud-%d@example.com", ts), "CH Cloud Org")

	body, _ := json.Marshal(map[string]interface{}{
		"name": "CH Cloud", "type": "clickhouse",
		"config": map[string]interface{}{
			"host": "abc.clickhouse.cloud", "port": 8443, "user": "default", "password": "pw",
			"cloud_org_id": "org-1", "cloud_service_id": "svc-1",
			"cloud_key_id": "key-1", "cloud_key_secret": "super-secret",
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&created)
	connID := created["id"].(string)
	cfg := created["config"].(map[string]interface{})
	if cfg["cloud_key_secret"] != "***" {
		t.Fatalf("expected cloud_key_secret masked, got %v", cfg["cloud_key_secret"])
	}
	if cfg["cloud_org_id"] != "org-1" {
		t.Fatalf("expected cloud_org_id preserved, got %v", cfg["cloud_org_id"])
	}

	// Save with a blank secret and a changed non-secret field: the stored
	// secret must survive while the org id updates.
	upd, _ := json.Marshal(map[string]interface{}{
		"config": map[string]interface{}{"cloud_key_secret": "", "cloud_org_id": "org-2"},
	})
	req = httptest.NewRequest("PUT", "/api/v1/connectors/"+connID, bytes.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	var encrypted []byte
	if err := srv.DB().Pool.QueryRow(context.Background(),
		`SELECT config_encrypted FROM connectors WHERE id = $1`, connID).Scan(&encrypted); err != nil {
		t.Fatalf("load stored config: %v", err)
	}
	plain, err := crypto.Decrypt(encrypted, srv.MasterKey())
	if err != nil {
		t.Fatalf("decrypt stored config: %v", err)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal(plain, &stored); err != nil {
		t.Fatalf("parse stored config: %v", err)
	}
	if stored["cloud_key_secret"] != "super-secret" {
		t.Fatalf("stored cloud secret was clobbered: %v", stored["cloud_key_secret"])
	}
	if stored["cloud_org_id"] != "org-2" {
		t.Fatalf("stored cloud_org_id not updated: %v", stored["cloud_org_id"])
	}
}
```

**Step 2: Run the tests to confirm they fail.**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestSecretFieldSetIncludesClickHouseCloudKey|TestMaskedConnectorConfigClickHouseCloudSecret|TestClickHouseCloudSecretMaskedAndPreservedOnUpdate' -count=1 -timeout 3m
```

Expected: `TestSecretFieldSetIncludesClickHouseCloudKey` fails (`cloud_key_secret` missing), `TestMaskedConnectorConfigClickHouseCloudSecret` fails (`cloud_key_secret` unmasked), and the round-trip fails the masked assertion.

**Step 3: Implement (minimal).**

In `internal/api/connector_handlers.go`, change `secretFieldSet`'s first line to include `cloud_key_secret`:

```go
func secretFieldSet(connType models.ConnectorType) map[string]bool {
	keys := map[string]bool{"password": true, "token": true, "client_secret": true, "cloud_key_secret": true}
	if d, ok := executor.GetDriver(connType); ok {
		for _, f := range d.ConfigSchema().Fields {
			if f.Secret {
				keys[strings.ToLower(f.Name)] = true
			}
		}
	}
	return keys
}
```

Nothing else changes: `maskedConnectorConfig` masks every key in this set, and `mergeConnectorConfig` skips blank/`"***"` values for the same set (the existing update flow already calls `secretFieldSet(connType)`).

**Step 4: Run the tests to confirm they pass.**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestSecretFieldSetIncludesClickHouseCloudKey|TestMaskedConnectorConfigClickHouseCloudSecret|TestClickHouseCloudSecretMaskedAndPreservedOnUpdate' -count=1 -timeout 3m
```

Expected: `ok github.com/the-heaven-labs/aether/internal/api`. Then `task fmt`.

**Step 5: Commit.**

```bash
git add internal/api/connector_handlers.go internal/api/connector_config_test.go internal/api/connector_handlers_test.go
git commit -m "feat(connectors): mask ClickHouse Cloud API key secret in connector config"
```

---

### Task 2: Cloud-state fetch core (credentials, cache, single-flight)

**Files:**
- Create: `internal/api/connector_cloud_state.go` — `cloudAPIBaseURL`, `cloudCredentialsFromConfig`, `cloudStateCredentials`, `cloudServiceState`, `cloudStateResult`, `cloudStateCache`, `newCloudStateCache`, `cloudStateFlight`, `fetchCloudServiceState` (the handler is added in Task 3)
- Test: `internal/api/connector_cloud_state_test.go` — `TestCloudCredentialsFromConfig`, `TestCloudStateCacheExpiry`

**Step 1: Write the failing tests.**

Create `internal/api/connector_cloud_state_test.go`:

```go
package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloudCredentialsFromConfig(t *testing.T) {
	creds := cloudCredentialsFromConfig([]byte(`{"cloud_org_id":" org ","cloud_service_id":"svc","cloud_key_id":"key","cloud_key_secret":"secret"}`))
	require.True(t, creds.configured())
	require.Equal(t, "org", creds.OrgID)

	require.False(t, cloudCredentialsFromConfig([]byte(`{"cloud_org_id":"org"}`)).configured())
	require.False(t, cloudCredentialsFromConfig([]byte(`not-json`)).configured())
}

func TestCloudStateCacheExpiry(t *testing.T) {
	cache := newCloudStateCache(25 * time.Millisecond)
	cache.put("conn-a", cloudStateResult{state: cloudServiceState{State: "running"}, checkedAt: time.Now()})
	if _, ok := cache.get("conn-a"); !ok {
		t.Fatal("expected a fresh cache hit")
	}
	time.Sleep(40 * time.Millisecond)
	if _, ok := cache.get("conn-a"); ok {
		t.Fatal("expected the entry to expire")
	}
}
```

**Step 2: Run to confirm the failure.**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestCloudCredentialsFromConfig|TestCloudStateCacheExpiry' -count=1 -timeout 3m
```

Expected: compile failure — `undefined: cloudCredentialsFromConfig`, `undefined: newCloudStateCache`, etc.

**Step 3: Implement the core file.**

Create `internal/api/connector_cloud_state.go` (exactly this content; Task 3 appends the handler and its imports):

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// cloudAPIBaseURL is the ClickHouse Cloud control-plane API root. It is a
// package variable so tests can point it at an httptest server. Production
// code never accepts a user-supplied URL, so there is no SSRF surface.
var cloudAPIBaseURL = "https://api.clickhouse.cloud"

// cloudStateHTTPClient bounds every control-plane read. The 5s timeout is far
// above normal latency but keeps a stuck upstream from holding a request open.
var cloudStateHTTPClient = &http.Client{Timeout: cloudStateRequestTimeout}

const (
	// cloudStateRequestTimeout caps one control-plane call.
	cloudStateRequestTimeout = 5 * time.Second
	// cloudStateCacheTTL bounds how stale a served state can be. Control-plane
	// reads never wake the service, so a short cache is about API traffic, not
	// correctness.
	cloudStateCacheTTL = 45 * time.Second
	// cloudStateResponseMaxBytes caps how much of an upstream response body is
	// read (the service resource is a few KB).
	cloudStateResponseMaxBytes = 1 << 20
)

// cloudStateCredentials are the optional per-connector ClickHouse Cloud API
// credentials. The zero value means "not configured".
type cloudStateCredentials struct {
	OrgID     string
	ServiceID string
	KeyID     string
	KeySecret string
}

// configured reports whether all four credential fields are present.
func (c cloudStateCredentials) configured() bool {
	return c.OrgID != "" && c.ServiceID != "" && c.KeyID != "" && c.KeySecret != ""
}

// cloudServiceState is the subset of the ClickHouse Cloud service resource the
// status chip needs; the JSON tags mirror the control-plane field names.
type cloudServiceState struct {
	State              string  `json:"state"`
	IdleScaling        bool    `json:"idleScaling"`
	IdleTimeoutMinutes float64 `json:"idleTimeoutMinutes"`
}

// cloudStateResult pairs a fetched state with the time it was fetched, so the
// cache can age it out.
type cloudStateResult struct {
	state     cloudServiceState
	checkedAt time.Time
}

// cloudCredentialsFromConfig extracts Cloud API credentials from the decrypted
// connector config. Malformed JSON degrades to "not configured" instead of
// failing the request.
func cloudCredentialsFromConfig(raw []byte) cloudStateCredentials {
	var cfg struct {
		CloudOrgID     string `json:"cloud_org_id"`
		CloudServiceID string `json:"cloud_service_id"`
		CloudKeyID     string `json:"cloud_key_id"`
		CloudKeySecret string `json:"cloud_key_secret"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cloudStateCredentials{}
	}
	return cloudStateCredentials{
		OrgID:     strings.TrimSpace(cfg.CloudOrgID),
		ServiceID: strings.TrimSpace(cfg.CloudServiceID),
		KeyID:     strings.TrimSpace(cfg.CloudKeyID),
		KeySecret: cfg.CloudKeySecret,
	}
}

// cloudStateCache is a small per-connector TTL cache for successful
// control-plane reads. Errors are deliberately not cached so a credential fix
// is visible on the next poll.
type cloudStateCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cloudStateResult
}

func newCloudStateCache(ttl time.Duration) *cloudStateCache {
	return &cloudStateCache{ttl: ttl, entries: make(map[string]cloudStateResult)}
}

func (c *cloudStateCache) get(connectorID string) (cloudStateResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[connectorID]
	if !ok {
		return cloudStateResult{}, false
	}
	if time.Since(entry.checkedAt) > c.ttl {
		delete(c.entries, connectorID)
		return cloudStateResult{}, false
	}
	return entry, true
}

func (c *cloudStateCache) put(connectorID string, result cloudStateResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[connectorID] = result
}

// cloudStateFlight dedupes concurrent control-plane reads for the same
// connector. The cache in front handles the steady state; this handles bursts
// (e.g. several page loads in one polling window).
var cloudStateFlight singleflight.Group

// fetchCloudServiceState reads one service from the ClickHouse Cloud
// control-plane API. It authenticates with the API key pair and never wakes
// the service. Errors carry no credential material: they are built from
// transport and status information only.
func fetchCloudServiceState(ctx context.Context, creds cloudStateCredentials) (cloudServiceState, error) {
	ctx, cancel := context.WithTimeout(ctx, cloudStateRequestTimeout)
	defer cancel()

	endpoint := fmt.Sprintf("%s/v1/organizations/%s/services/%s",
		strings.TrimRight(cloudAPIBaseURL, "/"),
		url.PathEscape(creds.OrgID), url.PathEscape(creds.ServiceID),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return cloudServiceState{}, fmt.Errorf("build clickhouse cloud request: %w", err)
	}
	req.SetBasicAuth(creds.KeyID, creds.KeySecret)

	resp, err := cloudStateHTTPClient.Do(req)
	if err != nil {
		return cloudServiceState{}, fmt.Errorf("clickhouse cloud request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return cloudServiceState{}, fmt.Errorf("clickhouse cloud API returned HTTP %d", resp.StatusCode)
	}

	var body struct {
		Result cloudServiceState `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, cloudStateResponseMaxBytes)).Decode(&body); err != nil {
		return cloudServiceState{}, fmt.Errorf("invalid clickhouse cloud response: %w", err)
	}
	if strings.TrimSpace(body.Result.State) == "" {
		return cloudServiceState{}, fmt.Errorf("clickhouse cloud response carried no service state")
	}
	return body.Result, nil
}
```

**Step 4: Run to confirm the tests pass.**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestCloudCredentialsFromConfig|TestCloudStateCacheExpiry' -count=1 -timeout 3m
```

Expected: `ok github.com/the-heaven-labs/aether/internal/api`. Then `task fmt`.

**Step 5: Commit.**

```bash
git add internal/api/connector_cloud_state.go internal/api/connector_cloud_state_test.go
git commit -m "feat(connectors): add ClickHouse Cloud state fetch core with TTL cache and single-flight"
```

---

### Task 3: `GET /api/v1/connectors/{id}/cloud-state` handler + route

**Files:**
- Modify: `internal/api/connector_cloud_state.go` — append `cloudStateResponse`, `handleConnectorCloudState`, `cachedCloudServiceState`; add `crypto` + `models` imports
- Modify: `internal/api/router.go` — `Server` struct field `cloudStateCache`, `NewServer` initialization, route registration
- Test: `internal/api/connector_cloud_state_test.go` — fixture + `TestConnectorCloudStateNotConfigured`, `TestConnectorCloudStateSuccessAndCache`, `TestConnectorCloudStateDedupesInFlightReads`, `TestConnectorCloudStateUpstreamErrorIsGraceful`, `TestConnectorCloudStateRequiresViewPermission`

**Step 1: Write the failing tests.**

Extend `internal/api/connector_cloud_state_test.go` with the fixture and handler tests below. Extend the import block to exactly:

```go
import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/crypto"
)
```

Append:

```go
// cloudStateCredsConfig is a ClickHouse connector config with a complete Cloud
// API credential set.
const cloudStateCredsConfig = `{
	"host": "abc.clickhouse.cloud",
	"port": 8443,
	"user": "default",
	"password": "pw",
	"cloud_org_id": "org-123",
	"cloud_service_id": "svc-456",
	"cloud_key_id": "key-id",
	"cloud_key_secret": "key-secret"
}`

// cloudStateFixture holds a test server plus a seeded org, org-admin user, and
// one connector of the requested type.
type cloudStateFixture struct {
	s           *Server
	orgID       uuid.UUID
	userID      uuid.UUID
	connectorID uuid.UUID
	token       string
}

// newCloudStateFixture seeds an org, an org admin, and a connector whose
// encrypted config is configJSON. It reuses newWarehouseSyncTestServer because
// package-internal tests cannot call setupTestServer from package api_test and
// both need a migrated database; the CH-table-permissions flag it sets is
// irrelevant to this endpoint.
func newCloudStateFixture(t *testing.T, connType, configJSON string) *cloudStateFixture {
	t.Helper()
	s, key := newWarehouseSyncTestServer(t)
	ctx := context.Background()
	suffix := uuid.NewString()

	orgID := uuid.New()
	_, err := s.db.Pool.Exec(ctx, `INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID.String(), "Cloud State Org", "cloud-state-"+suffix)
	require.NoError(t, err)

	userID := uuid.New()
	_, err = s.db.Pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), "cloud-state-"+suffix+"@test.local", "Cloud State User")
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID.String(), userID.String())
	require.NoError(t, err)

	encrypted, err := crypto.Encrypt([]byte(configJSON), key)
	require.NoError(t, err)

	connectorID := uuid.New()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		connectorID.String(), orgID.String(), "Cloud State Connector", connType, encrypted, userID.String())
	require.NoError(t, err)

	token, err := s.jwt.Issue(userID.String(), orgID.String(), "admin")
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, stmt := range []struct {
			sql string
			id  uuid.UUID
		}{
			{`DELETE FROM connectors WHERE id = $1`, connectorID},
			{`DELETE FROM users WHERE id = $1`, userID},
			{`DELETE FROM orgs WHERE id = $1`, orgID},
		} {
			if _, err := s.db.Pool.Exec(cleanupCtx, stmt.sql, stmt.id.String()); err != nil {
				t.Logf("cleanup %s: %v", stmt.sql, err)
			}
		}
	})

	return &cloudStateFixture{s: s, orgID: orgID, userID: userID, connectorID: connectorID, token: token}
}

func (fx *cloudStateFixture) path() string {
	return "/api/v1/connectors/" + fx.connectorID.String() + "/cloud-state"
}

// get drives the real mux + auth middleware (route-registration coverage).
func (fx *cloudStateFixture) get(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return warehouseAPIRequest(t, fx.s, http.MethodGet, fx.path(), fx.token, nil)
}

// setCloudAPIBaseURL points the control-plane base at an httptest server for
// the duration of one test. Tests in this file must not call t.Parallel().
func setCloudAPIBaseURL(t *testing.T, base string) {
	t.Helper()
	previous := cloudAPIBaseURL
	cloudAPIBaseURL = base
	t.Cleanup(func() { cloudAPIBaseURL = previous })
}

// cloudStateMock wraps handler with a request counter and installs it as the
// control-plane base URL.
func cloudStateMock(t *testing.T, handler http.HandlerFunc) *atomic.Int64 {
	t.Helper()
	var hits atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(ts.Close)
	setCloudAPIBaseURL(t, ts.URL)
	return &hits
}

func TestConnectorCloudStateNotConfigured(t *testing.T) {
	for _, tc := range []struct {
		name     string
		connType string
		config   string
	}{
		{"clickhouse without credentials", "clickhouse", `{"host":"abc.clickhouse.cloud","port":8443}`},
		{"clickhouse with partial credentials", "clickhouse", `{"host":"abc.clickhouse.cloud","cloud_org_id":"org-1"}`},
		{"non-clickhouse connector", "postgres", `{"host":"localhost","port":5432,"user":"u","password":"p","database":"d"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newCloudStateFixture(t, tc.connType, tc.config)
			// No upstream server is installed: any control-plane call would hit
			// the production base URL, which this test's response must not do.
			rec := fx.get(t)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.JSONEq(t, `{"configured": false}`, rec.Body.String())
		})
	}
}

func TestConnectorCloudStateSuccessAndCache(t *testing.T) {
	fx := newCloudStateFixture(t, "clickhouse", cloudStateCredsConfig)

	hits := cloudStateMock(t, func(w http.ResponseWriter, r *http.Request) {
		// Wrong path or auth would make this handler answer 401/404 and fail
		// the response assertions below.
		user, pass, ok := r.BasicAuth()
		if !ok || user != "key-id" || pass != "key-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/organizations/org-123/services/svc-456" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"state":              "idle",
				"idleScaling":        true,
				"idleTimeoutMinutes": 15,
			},
		})
	})

	rec := fx.get(t)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Configured         bool    `json:"configured"`
		State              string  `json:"state"`
		IdleScaling        bool    `json:"idle_scaling"`
		IdleTimeoutMinutes float64 `json:"idle_timeout_minutes"`
		CheckedAt          string  `json:"checked_at"`
		Error              string  `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Configured)
	require.Equal(t, "idle", resp.State)
	require.True(t, resp.IdleScaling)
	require.Equal(t, 15.0, resp.IdleTimeoutMinutes)
	require.NotEmpty(t, resp.CheckedAt)
	require.Empty(t, resp.Error)

	// A second read inside the TTL must come from the in-memory cache.
	rec = fx.get(t)
	require.Equal(t, http.StatusOK, rec.Code)
	require.EqualValues(t, 1, hits.Load(), "cached read must not hit the control plane")
}

func TestConnectorCloudStateDedupesInFlightReads(t *testing.T) {
	fx := newCloudStateFixture(t, "clickhouse", cloudStateCredsConfig)

	release := make(chan struct{})
	var hits atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{"state": "running", "idleScaling": false, "idleTimeoutMinutes": 0},
		})
	}))
	t.Cleanup(ts.Close)
	setCloudAPIBaseURL(t, ts.URL)

	recs := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, fx.path(), nil)
			req.Header.Set("Authorization", "Bearer "+fx.token)
			req.Header.Set("X-AETHER-Admin-Mode", "true")
			rec := httptest.NewRecorder()
			fx.s.ServeHTTP(rec, req)
			recs[i] = rec
		}(i)
	}

	// Wait until the leader is parked in the control-plane call, then release
	// both requests: the follower must join the leader's flight (or read the
	// cache), never start a second upstream call.
	require.Eventually(t, func() bool { return hits.Load() >= 1 }, time.Second, 5*time.Millisecond)
	close(release)
	wg.Wait()

	require.EqualValues(t, 1, hits.Load(), "both reads must share one control-plane call")
	for i, rec := range recs {
		require.Equal(t, http.StatusOK, rec.Code, "request %d: %s", i, rec.Body.String())
	}
}

func TestConnectorCloudStateUpstreamErrorIsGraceful(t *testing.T) {
	fx := newCloudStateFixture(t, "clickhouse", cloudStateCredsConfig)
	hits := cloudStateMock(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	rec := fx.get(t)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Configured bool   `json:"configured"`
		Error      string `json:"error"`
		State      string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Configured)
	require.Contains(t, resp.Error, "HTTP 500")
	require.Empty(t, resp.State)
	require.NotContains(t, rec.Body.String(), "key-secret", "credentials must never leak")

	// Errors are not cached: the next read retries upstream.
	rec = fx.get(t)
	require.Equal(t, http.StatusOK, rec.Code)
	require.EqualValues(t, 2, hits.Load(), "errors must not be cached")
}

func TestConnectorCloudStateRequiresViewPermission(t *testing.T) {
	fx := newCloudStateFixture(t, "clickhouse", cloudStateCredsConfig)

	// A second org member with no connector ACL must be denied.
	memberID := uuid.New()
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		memberID.String(), "cloud-state-member-"+uuid.NewString()+"@test.local", "Cloud State Member")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = fx.s.db.Pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, memberID.String())
	})
	_, err = fx.s.db.Pool.Exec(context.Background(),
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'editor')`,
		fx.orgID.String(), memberID.String())
	require.NoError(t, err)
	memberToken, err := fx.s.jwt.Issue(memberID.String(), fx.orgID.String(), "editor")
	require.NoError(t, err)

	rec := warehouseAPIRequest(t, fx.s, http.MethodGet, fx.path(), memberToken, nil)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}
```

**Step 2: Run to confirm the failure.**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestConnectorCloudState' -count=1 -timeout 3m
```

Expected: compile failure — `undefined: handleConnectorCloudState`, `s.cloudStateCache` undefined, etc.

**Step 3: Implement the handler, route, and Server wiring.**

**3a.** Append to `internal/api/connector_cloud_state.go` and extend its import block with `"github.com/the-heaven-labs/aether/internal/crypto"` and `"github.com/the-heaven-labs/aether/internal/models"`:

```go
// cloudStateResponse is the wire shape for GET /connectors/{id}/cloud-state.
// `configured` separates "no Cloud API credentials" from "configured but the
// read failed"; failures stay HTTP 200 so the page degrades into the inferred
// state instead of showing a request error.
type cloudStateResponse struct {
	Configured         bool     `json:"configured"`
	State              string   `json:"state,omitempty"`
	IdleScaling        *bool    `json:"idle_scaling,omitempty"`
	IdleTimeoutMinutes *float64 `json:"idle_timeout_minutes,omitempty"`
	CheckedAt          string   `json:"checked_at,omitempty"`
	Error              string   `json:"error,omitempty"`
}

// @Summary Get ClickHouse Cloud service state
// @Description Returns the ClickHouse Cloud control-plane state for a ClickHouse connector with Cloud API credentials. Non-ClickHouse connectors and connectors without credentials return 200 with {"configured": false}; upstream failures return 200 with an error field. Reading state never wakes an idle service.
// @Tags connectors
// @Produce json
// @Param id path string true "Connector ID"
// @Success 200 {object} map[string]interface{}
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id}/cloud-state [get]
func (s *Server) handleConnectorCloudState(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	connType, configEnc, err := s.loadConnectorRow(ctx, connID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	if connType != models.ConnectorClickHouse {
		writeJSON(w, http.StatusOK, cloudStateResponse{Configured: false})
		return
	}

	plain, err := crypto.Decrypt(configEnc, s.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load connector config")
		return
	}
	creds := cloudCredentialsFromConfig(plain)
	if !creds.configured() {
		writeJSON(w, http.StatusOK, cloudStateResponse{Configured: false})
		return
	}

	result, err := s.cachedCloudServiceState(ctx, connID, creds)
	if err != nil {
		// The upstream error carries no credential material: fetchCloudServiceState
		// builds messages from transport and status information only.
		writeJSON(w, http.StatusOK, cloudStateResponse{Configured: true, Error: err.Error()})
		return
	}
	idleScaling := result.state.IdleScaling
	idleTimeout := result.state.IdleTimeoutMinutes
	writeJSON(w, http.StatusOK, cloudStateResponse{
		Configured:         true,
		State:              result.state.State,
		IdleScaling:        &idleScaling,
		IdleTimeoutMinutes: &idleTimeout,
		CheckedAt:          result.checkedAt.UTC().Format(time.RFC3339),
	})
}

// cachedCloudServiceState serves the in-memory TTL cache, falling back to one
// deduplicated control-plane read per connector.
func (s *Server) cachedCloudServiceState(ctx context.Context, connID string, creds cloudStateCredentials) (cloudStateResult, error) {
	if result, ok := s.cloudStateCache.get(connID); ok {
		return result, nil
	}
	value, err, _ := cloudStateFlight.Do(connID, func() (any, error) {
		// The flight window may have filled the cache while this call waited.
		if result, ok := s.cloudStateCache.get(connID); ok {
			return result, nil
		}
		state, err := fetchCloudServiceState(ctx, creds)
		if err != nil {
			return cloudStateResult{}, err
		}
		result := cloudStateResult{state: state, checkedAt: time.Now()}
		s.cloudStateCache.put(connID, result)
		return result, nil
	})
	if err != nil {
		return cloudStateResult{}, err
	}
	return value.(cloudStateResult), nil
}
```

**3b.** In `internal/api/router.go`, add the cache field to `Server` (place it after the `oauth *oauth.Service // OAuth AS storage/logic` line):

```go
	// cloudStateCache holds short-lived ClickHouse Cloud control-plane reads
	// keyed by connector ID (connector_cloud_state.go).
	cloudStateCache *cloudStateCache
```

**3c.** In `NewServer`'s struct literal, after `warehouseReconcileInterval: config.DefaultWarehouseReconcileInterval,`:

```go
		cloudStateCache:              newCloudStateCache(cloudStateCacheTTL),
```

**3d.** In `s.routes()`, after the line registering `POST /api/v1/connectors/{id}/test`:

```go
	s.mux.Handle("GET /api/v1/connectors/{id}/cloud-state", authMW(s.requirePermission("connector", "id", "view")(http.HandlerFunc(s.handleConnectorCloudState))))
```

**3e.** Promote the now-direct dependency: `go mod tidy && go mod verify`.

**Step 4: Run to confirm everything passes.**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestConnectorCloudState|TestCloudCredentialsFromConfig|TestCloudStateCacheExpiry' -count=1 -timeout 3m
```

Expected: `ok github.com/the-heaven-labs/aether/internal/api` with all subtests passing. Also run `go build ./...` to catch router wiring mistakes, then `task fmt`.

**Step 5: Commit.**

```bash
git add internal/api/connector_cloud_state.go internal/api/connector_cloud_state_test.go internal/api/router.go go.mod go.sum
git commit -m "feat(connectors): expose GET /connectors/{id}/cloud-state"
```

---

### Task 4: Frontend types and cloud-state helpers

**Files:**
- Modify: `web/src/types/index.ts` — `Connector.config` keys; new `ConnectorCloudState` interface
- Create: `web/src/utils/cloudState.ts` — `cloudStateView`, `isClickHouseCloudHost`, `parseIdleTimeoutMinutes`, `connectorIdleTimeoutMinutes`, `formatRelativeAgo`, `inferIdleState`
- Test: `web/src/utils/cloudState.test.ts`
- Run: `cd web && npm ci` (once, before anything else in this task)

**Step 0/dependency check:** confirm the connector-health plan landed:

```bash
grep -n "last_success_at" web/src/types/index.ts
```

It must find the field on `Connector`. If it does not, stop — land the connector-health plan first.

**Step 1: Write the failing test.**

Create `web/src/utils/cloudState.test.ts`:

```ts
import { describe, expect, test } from 'vitest'
import {
  DEFAULT_IDLE_TIMEOUT_MINUTES,
  cloudStateView,
  connectorIdleTimeoutMinutes,
  formatRelativeAgo,
  inferIdleState,
  isClickHouseCloudHost,
  parseIdleTimeoutMinutes,
} from './cloudState'
import type { Connector } from '../types'

const connector = (config: Connector['config'], lastSuccessAt?: string | null): Connector => ({
  id: 'c-1', name: 'CH', type: 'clickhouse', created_at: '2026-01-01T00:00:00Z',
  config, last_success_at: lastSuccessAt,
})

describe('cloudStateView', () => {
  test.each([
    ['running', 'success', 'Running'],
    ['idle', 'neutral', 'Idle — wakes on next query'],
    ['awaking', 'neutral', 'Awaking…'],
    ['stopped', 'neutral', 'Stopped'],
    ['degraded', 'error', 'Degraded'],
    ['failed', 'error', 'Failed'],
  ])('maps %s', (state, status, label) => {
    expect(cloudStateView(state)).toEqual({ status, label })
  })

  test('falls back for unknown and missing states', () => {
    expect(cloudStateView('paused')).toEqual({ status: 'neutral', label: 'paused' })
    expect(cloudStateView(undefined)).toEqual({ status: 'neutral', label: 'Unknown' })
  })
})

describe('isClickHouseCloudHost', () => {
  test('matches ClickHouse Cloud hosts only', () => {
    expect(isClickHouseCloudHost('abc.clickhouse.cloud')).toBe(true)
    expect(isClickHouseCloudHost('ABC.ClickHouse.Cloud')).toBe(true)
    expect(isClickHouseCloudHost('evilclickhouse.cloud')).toBe(false)
    expect(isClickHouseCloudHost('localhost')).toBe(false)
    expect(isClickHouseCloudHost(undefined)).toBe(false)
  })
})

describe('parseIdleTimeoutMinutes', () => {
  test('defaults to 15 for empty, invalid, and non-positive values', () => {
    expect(parseIdleTimeoutMinutes('')).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
    expect(parseIdleTimeoutMinutes('abc')).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
    expect(parseIdleTimeoutMinutes('0')).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
    expect(parseIdleTimeoutMinutes('-5')).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
    expect(parseIdleTimeoutMinutes('30')).toBe(30)
  })
})

describe('connectorIdleTimeoutMinutes', () => {
  test('prefers stored config and falls back to 15', () => {
    expect(connectorIdleTimeoutMinutes(connector({ idle_timeout_minutes: 45 }))).toBe(45)
    expect(connectorIdleTimeoutMinutes(connector({}))).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
  })
})

describe('formatRelativeAgo', () => {
  test('formats sub-minute, minutes, hours, and days', () => {
    expect(formatRelativeAgo(30_000)).toBe('just now')
    expect(formatRelativeAgo(5 * 60_000)).toBe('5m ago')
    expect(formatRelativeAgo(125 * 60_000)).toBe('2h 5m ago')
    expect(formatRelativeAgo(3 * 24 * 60 * 60_000)).toBe('3d ago')
  })
})

describe('inferIdleState', () => {
  const now = new Date('2026-10-08T12:00:00Z')

  test('returns null for non-Cloud hosts', () => {
    expect(inferIdleState({ host: 'localhost', lastSuccessAt: '2026-10-08T11:00:00Z', idleTimeoutMinutes: 15, now })).toBeNull()
    expect(inferIdleState({ host: undefined, lastSuccessAt: null, idleTimeoutMinutes: 15, now })).toBeNull()
  })

  test('reports likely idle past the timeout', () => {
    const result = inferIdleState({
      host: 'abc.clickhouse.cloud',
      lastSuccessAt: '2026-10-08T11:20:00Z', // 40m ago
      idleTimeoutMinutes: 15,
      now,
    })
    expect(result).toEqual({ kind: 'likely_idle', idleTimeoutMinutes: 15, lastActivityAgeMs: 40 * 60_000 })
  })

  test('reports active within the timeout', () => {
    expect(inferIdleState({
      host: 'abc.clickhouse.cloud',
      lastSuccessAt: '2026-10-08T11:55:00Z',
      idleTimeoutMinutes: 15,
      now,
    })).toEqual({ kind: 'active', idleTimeoutMinutes: 15, lastActivityAgeMs: 5 * 60_000 })
  })

  test('reports unknown with no recorded activity', () => {
    expect(inferIdleState({ host: 'abc.clickhouse.cloud', lastSuccessAt: null, idleTimeoutMinutes: 15, now }))
      .toEqual({ kind: 'unknown', idleTimeoutMinutes: 15 })
  })

  test('falls back to the default timeout for bad values', () => {
    const result = inferIdleState({
      host: 'abc.clickhouse.cloud',
      lastSuccessAt: '2026-10-08T11:00:00Z',
      idleTimeoutMinutes: 0,
      now,
    })
    expect(result?.idleTimeoutMinutes).toBe(DEFAULT_IDLE_TIMEOUT_MINUTES)
  })
})
```

**Step 2: Run to confirm the failure.**

```bash
cd web && npm run test:run -- src/utils/cloudState.test.ts
```

Expected: suite fails to load — `Failed to resolve import "./cloudState"`.

**Step 3: Implement.**

Add to `web/src/types/index.ts` inside `Connector.config` (after `schema?: string`):

```ts
    /** Manual idle threshold (minutes) for ClickHouse Cloud inference; absent = 15. */
    idle_timeout_minutes?: number
    cloud_org_id?: string
    cloud_service_id?: string
    cloud_key_id?: string
    /** Returned as "***" once stored; never a real secret. */
    cloud_key_secret?: string
```

And after the closing brace of `Connector`, add:

```ts
/** GET /api/v1/connectors/{id}/cloud-state response. */
export interface ConnectorCloudState {
  configured: boolean
  state?: string
  idle_scaling?: boolean
  idle_timeout_minutes?: number
  checked_at?: string
  error?: string
}
```

Create `web/src/utils/cloudState.ts`:

```ts
import type { Connector } from '../types'

export const DEFAULT_IDLE_TIMEOUT_MINUTES = 15
export const CLICKHOUSE_CLOUD_HOST_SUFFIX = '.clickhouse.cloud'

export type CloudStateStatus = 'success' | 'error' | 'neutral'

export interface CloudStateView {
  status: CloudStateStatus
  label: string
}

/** Maps a ClickHouse Cloud service state onto chip copy and styling. */
export function cloudStateView(state: string | undefined): CloudStateView {
  switch (state?.trim().toLowerCase()) {
    case 'running':
      return { status: 'success', label: 'Running' }
    case 'idle':
      return { status: 'neutral', label: 'Idle — wakes on next query' }
    case 'awaking':
      return { status: 'neutral', label: 'Awaking…' }
    case 'starting':
      return { status: 'neutral', label: 'Starting…' }
    case 'provisioning':
      return { status: 'neutral', label: 'Provisioning…' }
    case 'stopped':
      return { status: 'neutral', label: 'Stopped' }
    case 'degraded':
      return { status: 'error', label: 'Degraded' }
    case 'failed':
      return { status: 'error', label: 'Failed' }
    default:
      return { status: 'neutral', label: state?.trim() ? state.trim() : 'Unknown' }
  }
}

/** True for hosts managed by ClickHouse Cloud, where idle inference applies. */
export function isClickHouseCloudHost(host: string | undefined): boolean {
  if (!host) return false
  return host.trim().toLowerCase().endsWith(CLICKHOUSE_CLOUD_HOST_SUFFIX)
}

/** Parses the manual idle-timeout form value; empty/invalid falls back to 15. */
export function parseIdleTimeoutMinutes(value: string): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : DEFAULT_IDLE_TIMEOUT_MINUTES
}

/** Reads the stored idle timeout, defaulting to 15 when absent/invalid. */
export function connectorIdleTimeoutMinutes(connector: Connector): number {
  const stored = connector.config?.idle_timeout_minutes
  return typeof stored === 'number' && stored > 0 ? stored : DEFAULT_IDLE_TIMEOUT_MINUTES
}

/** Compact "ago" copy for durations in ms: "just now", "5m ago", "2h 5m ago", "3d ago". */
export function formatRelativeAgo(ms: number): string {
  if (!Number.isFinite(ms) || ms < 60_000) return 'just now'
  const minutes = Math.floor(ms / 60_000)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  const restMinutes = minutes % 60
  if (hours < 24) return restMinutes > 0 ? `${hours}h ${restMinutes}m ago` : `${hours}h ago`
  const days = Math.floor(hours / 24)
  const restHours = hours % 24
  return restHours > 0 ? `${days}d ${restHours}h ago` : `${days}d ago`
}

export type IdleInferenceKind = 'likely_idle' | 'active' | 'unknown'

export interface IdleInference {
  kind: IdleInferenceKind
  idleTimeoutMinutes: number
  /** Age of the last recorded activity at evaluation time, when known. */
  lastActivityAgeMs?: number
}

/**
 * Probabilistic idle inference for ClickHouse Cloud hosts without Cloud API
 * credentials. Returns null for non-Cloud hosts (nothing to infer) so callers
 * render no line at all. Wording stays probabilistic on purpose: activity is
 * only what Aether has observed, not the service's real state.
 */
export function inferIdleState(opts: {
  host: string | undefined
  lastSuccessAt: string | null | undefined
  idleTimeoutMinutes: number
  now?: Date
}): IdleInference | null {
  if (!isClickHouseCloudHost(opts.host)) return null
  const timeout = opts.idleTimeoutMinutes > 0 ? opts.idleTimeoutMinutes : DEFAULT_IDLE_TIMEOUT_MINUTES
  if (!opts.lastSuccessAt) return { kind: 'unknown', idleTimeoutMinutes: timeout }
  const last = new Date(opts.lastSuccessAt)
  if (Number.isNaN(last.getTime())) return { kind: 'unknown', idleTimeoutMinutes: timeout }
  const ageMs = (opts.now ?? new Date()).getTime() - last.getTime()
  if (ageMs > timeout * 60_000) {
    return { kind: 'likely_idle', idleTimeoutMinutes: timeout, lastActivityAgeMs: ageMs }
  }
  return { kind: 'active', idleTimeoutMinutes: timeout, lastActivityAgeMs: ageMs }
}
```

**Step 4: Run to confirm the tests pass.**

```bash
cd web && npm run test:run -- src/utils/cloudState.test.ts
cd web && npx tsc --noEmit
```

Expected: all `cloudState` tests pass and TypeScript is clean. (If `last_success_at` is missing from `Connector`, tsc fails here — that is the dependency check firing.)

**Step 5: Commit.**

```bash
git add web/src/types/index.ts web/src/utils/cloudState.ts web/src/utils/cloudState.test.ts
git commit -m "feat(connectors): add cloud-state types and idle-inference helpers"
```

---

### Task 5: Connector form — idle timeout + ClickHouse Cloud API group

**Files:**
- Modify: `web/src/pages/ConnectorsPage.tsx` — `ConnectorForm`, `defaultForm`, `buildConnectorConfig`, `formFromConnector`, new `ClickHouseCloudFields`, both FormModal bodies; import `parseIdleTimeoutMinutes`
- Test: `web/src/test/ConnectorsPage.test.tsx` — `creates a ClickHouse connector with idle timeout and Cloud API config (CH-CLOUD-1)`, `editing ClickHouse keeps the stored Cloud key secret when left blank (CH-CLOUD-2)`

**Step 1: Write the failing tests.**

Append inside the existing `describe('ConnectorsPage', ...)` block in `web/src/test/ConnectorsPage.test.tsx`:

```tsx
  test('creates a ClickHouse connector with idle timeout and Cloud API config (CH-CLOUD-1)', async () => {
    let postBody: { config?: Record<string, unknown> } | null = null
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([])),
      http.post('/api/v1/connectors', async ({ request }) => {
        postBody = (await request.json()) as { config?: Record<string, unknown> }
        return HttpResponse.json(
          { id: 'c-ch', name: 'CH', type: 'clickhouse', config: {}, created_at: '2026-01-01T00:00:00Z' },
          { status: 201 },
        )
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByText('+ New Connector'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'CH' } })
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'clickhouse' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'abc.clickhouse.cloud' } })
    fireEvent.change(screen.getByLabelText('Idle timeout (minutes)'), { target: { value: '30' } })
    fireEvent.change(screen.getByLabelText('Organization ID'), { target: { value: 'org-1' } })
    fireEvent.change(screen.getByLabelText('Service ID'), { target: { value: 'svc-1' } })
    fireEvent.change(screen.getByLabelText('Key ID'), { target: { value: 'key-1' } })
    fireEvent.change(screen.getByLabelText('Key Secret'), { target: { value: 'super-secret' } })
    fireEvent.click(screen.getByText('Create'))

    await waitFor(() =>
      expect(postBody?.config).toEqual({
        host: 'abc.clickhouse.cloud',
        port: 9000,
        database: '',
        user: '',
        ssl_mode: 'disable',
        idle_timeout_minutes: 30,
        cloud_org_id: 'org-1',
        cloud_service_id: 'svc-1',
        cloud_key_id: 'key-1',
        cloud_key_secret: 'super-secret',
        password: '',
      }),
    )
  })

  test('editing ClickHouse keeps the stored Cloud key secret when left blank (CH-CLOUD-2)', async () => {
    let putBody: { config?: Record<string, unknown> } | null = null
    const stored = {
      id: 'c-ch-edit', name: 'CH Edit', type: 'clickhouse', is_default: false,
      config: {
        host: 'abc.clickhouse.cloud', port: 8443, database: '', user: 'default',
        ssl_mode: 'require', idle_timeout_minutes: 15,
        cloud_org_id: 'org-1', cloud_service_id: 'svc-1',
        cloud_key_id: 'key-1', cloud_key_secret: '***',
      },
      created_at: '2026-01-01T00:00:00Z',
    }
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([stored])),
      http.get('/api/v1/connectors/:id/cloud-state', () =>
        HttpResponse.json({ configured: true, state: 'running', checked_at: new Date().toISOString() }),
      ),
      http.put('/api/v1/connectors/c-ch-edit', async ({ request }) => {
        putBody = (await request.json()) as { config?: Record<string, unknown> }
        return HttpResponse.json({ ...stored, config: { ...stored.config, idle_timeout_minutes: 45 } })
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByLabelText('Edit connector'))
    // The stored secret comes back masked and must never be pre-filled.
    expect((screen.getByLabelText(/Key Secret/) as HTMLInputElement).value).toBe('')
    fireEvent.change(screen.getByLabelText('Idle timeout (minutes)'), { target: { value: '45' } })
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody!.config).toMatchObject({
      idle_timeout_minutes: 45,
      cloud_org_id: 'org-1',
      cloud_service_id: 'svc-1',
      cloud_key_id: 'key-1',
    })
    expect(putBody!.config).not.toHaveProperty('cloud_key_secret')
  })
```

**Step 2: Run to confirm the failures.**

```bash
cd web && npm run test:run -- src/test/ConnectorsPage.test.tsx
```

Expected: `Unable to find a label with the text of: Idle timeout (minutes)` (and `Organization ID` / `Key Secret`), while pre-existing tests still pass.

**Step 3: Implement.**

**3a.** Import the parser in `web/src/pages/ConnectorsPage.tsx` (with the other local imports):

```tsx
import { parseIdleTimeoutMinutes } from '../utils/cloudState'
```

**3b.** Extend `ConnectorForm` (after `table_denylist: string`):

```tsx
  idle_timeout_minutes: string
  cloud_org_id: string
  cloud_service_id: string
  cloud_key_id: string
  cloud_key_secret: string
```

**3c.** Extend `defaultForm` (after `table_allowlist: '', table_denylist: '',`):

```tsx
  idle_timeout_minutes: '', cloud_org_id: '', cloud_service_id: '', cloud_key_id: '', cloud_key_secret: '',
```

**3d.** In `buildConnectorConfig`, insert a ClickHouse branch before the final `if (!forUpdate || f.password !== '') cfg.password = f.password` line:

```tsx
  if (f.type === 'clickhouse') {
    // The manual idle threshold always round-trips (empty = the 15m default).
    cfg.idle_timeout_minutes = parseIdleTimeoutMinutes(f.idle_timeout_minutes)
    // Non-secret Cloud API fields round-trip verbatim (blank clears access);
    // the secret is omitted on edit when blank so the server keeps the stored one.
    cfg.cloud_org_id = f.cloud_org_id.trim()
    cfg.cloud_service_id = f.cloud_service_id.trim()
    cfg.cloud_key_id = f.cloud_key_id.trim()
    if (!forUpdate || f.cloud_key_secret !== '') cfg.cloud_key_secret = f.cloud_key_secret
  }
```

**3e.** In `formFromConnector`, add to the returned object (after `table_denylist:`):

```tsx
    idle_timeout_minutes: c.config?.idle_timeout_minutes != null ? String(c.config.idle_timeout_minutes) : '',
    cloud_org_id: c.config?.cloud_org_id ?? '',
    cloud_service_id: c.config?.cloud_service_id ?? '',
    cloud_key_id: c.config?.cloud_key_id ?? '',
    cloud_key_secret: '',
```

**3f.** Add the `ClickHouseCloudFields` component right after `DatabricksFields`:

```tsx
function ClickHouseCloudFields({ form, setForm, isEdit }: {
  form: ConnectorForm
  setForm: React.Dispatch<React.SetStateAction<ConnectorForm>>
  isEdit?: boolean
}) {
  const set = (field: keyof ConnectorForm) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))
  return (
    <>
      <label style={styles.label}>Idle timeout (minutes)
        <input
          style={styles.input}
          type="number"
          min="1"
          value={form.idle_timeout_minutes}
          onChange={set('idle_timeout_minutes')}
          placeholder="15"
        />
      </label>
      <details style={{ gridColumn: '1 / -1', fontSize: 13 }}>
        <summary style={{ cursor: 'pointer', fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>
          ClickHouse Cloud API (optional)
        </summary>
        <p style={{ fontSize: 12, color: 'var(--text-muted)', margin: '8px 0', lineHeight: 1.5 }}>
          Add an API key to show the service's exact state (running, idle, stopped). Aether reads
          the control plane, which never wakes an idle service. Without a key, idle state is
          inferred from recorded query activity using the timeout above.
        </p>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
          <label style={styles.label}>Organization ID
            <input style={styles.input} value={form.cloud_org_id} onChange={set('cloud_org_id')} />
          </label>
          <label style={styles.label}>Service ID
            <input style={styles.input} value={form.cloud_service_id} onChange={set('cloud_service_id')} />
          </label>
          <label style={styles.label}>Key ID
            <input style={styles.input} value={form.cloud_key_id} onChange={set('cloud_key_id')} />
          </label>
          <label style={styles.label}>
            Key Secret{isEdit ? ' (leave blank to keep current)' : ''}
            <input style={styles.input} type="password" value={form.cloud_key_secret} onChange={set('cloud_key_secret')} />
          </label>
        </div>
      </details>
    </>
  )
}
```

**3g.** In the **create** modal, immediately after `{form.type === 'databricks' && <DatabricksFields form={form} setForm={setForm} />}`, add:

```tsx
              {form.type === 'clickhouse' && <ClickHouseCloudFields form={form} setForm={setForm} />}
```

**3h.** In the **edit** modal, immediately after `{editForm.type === 'databricks' && <DatabricksFields form={editForm} setForm={setEditForm} isEdit />}`, add:

```tsx
              {editForm.type === 'clickhouse' && <ClickHouseCloudFields form={editForm} setForm={setEditForm} isEdit />}
```

**Step 4: Run to confirm the tests pass.**

```bash
cd web && npm run test:run -- src/test/ConnectorsPage.test.tsx
cd web && npx tsc --noEmit
```

Expected: both new tests pass alongside the existing suite; tsc clean.

**Step 5: Commit.**

```bash
git add web/src/pages/ConnectorsPage.tsx web/src/test/ConnectorsPage.test.tsx
git commit -m "feat(connectors): add ClickHouse Cloud API fields to the connector form"
```

---

### Task 6: Status chip + idle inference on the Connectors page

**Files:**
- Modify: `web/src/pages/ConnectorsPage.tsx` — new `ClickHouseCloudStatus` component, Status-cell insertion, imports, cloud-state query invalidation after save
- Modify: `web/src/test/handlers.ts` — default MSW handler for `/api/v1/connectors/:id/cloud-state`
- Test: `web/src/test/ConnectorsPage.test.tsx` — chip and inference tests

**Step 1: Write the failing tests.**

First add the default handler so every existing test that renders a ClickHouse row has a defined response (the suite runs with `onUnhandledRequest: 'error'`). In `web/src/test/handlers.ts`, next to the other Connectors handlers:

```ts
  // ClickHouse Cloud state: default to "not configured" so list tests without
  // cloud credentials render the inference path.
  http.get('/api/v1/connectors/:id/cloud-state', () => HttpResponse.json({ configured: false })),
```

Then append inside the `describe('ConnectorsPage', ...)` block in `web/src/test/ConnectorsPage.test.tsx`:

```tsx
  test('renders the exact Cloud state chip for a configured ClickHouse connector (CH-CLOUD-3)', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          {
            id: 'c-ch-idle', name: 'CH Idle', type: 'clickhouse', is_default: false,
            config: {
              host: 'abc.clickhouse.cloud',
              cloud_org_id: 'org-1', cloud_service_id: 'svc-1',
              cloud_key_id: 'key-1', cloud_key_secret: '***',
            },
            last_success_at: '2026-01-01T00:00:00Z',
            created_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
      http.get('/api/v1/connectors/:id/cloud-state', () =>
        HttpResponse.json({
          configured: true, state: 'idle', idle_scaling: true,
          idle_timeout_minutes: 15, checked_at: new Date().toISOString(),
        }),
      ),
    )
    renderWithProviders(<ConnectorsPage />)
    expect(await screen.findByText('Idle — wakes on next query')).toBeInTheDocument()
    // The exact chip wins over inference: no "likely idle" copy.
    expect(screen.queryByText(/Likely idle/)).toBeNull()
  })

  test('shows a muted unavailable chip when Cloud state fails (CH-CLOUD-4)', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          {
            id: 'c-ch-err', name: 'CH Err', type: 'clickhouse', is_default: false,
            config: {
              host: 'abc.clickhouse.cloud',
              cloud_org_id: 'org-1', cloud_service_id: 'svc-1',
              cloud_key_id: 'key-1', cloud_key_secret: '***',
            },
            created_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
      http.get('/api/v1/connectors/:id/cloud-state', () =>
        HttpResponse.json({ configured: true, error: 'clickhouse cloud API returned HTTP 403' }),
      ),
    )
    renderWithProviders(<ConnectorsPage />)
    const chip = await screen.findByText('Cloud state unavailable')
    expect(chip).toHaveAttribute('title', 'clickhouse cloud API returned HTTP 403')
  })

  test('infers likely idle for an unconfigured ClickHouse Cloud host (CH-CLOUD-5)', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          {
            id: 'c-ch-infer', name: 'CH Infer', type: 'clickhouse', is_default: false,
            config: { host: 'abc.clickhouse.cloud', idle_timeout_minutes: 15 },
            last_success_at: new Date(Date.now() - 40 * 60_000).toISOString(),
            created_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
      http.get('/api/v1/connectors/:id/cloud-state', () => HttpResponse.json({ configured: false })),
    )
    renderWithProviders(<ConnectorsPage />)
    expect(await screen.findByText(/Likely idle — last activity .* ago \(idle timeout 15m\)/)).toBeInTheDocument()
  })

  test('shows idle state unknown for a never-used unconfigured ClickHouse Cloud host (CH-CLOUD-6)', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          {
            id: 'c-ch-new', name: 'CH New', type: 'clickhouse', is_default: false,
            config: { host: 'abc.clickhouse.cloud' },
            last_success_at: null,
            created_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
      http.get('/api/v1/connectors/:id/cloud-state', () => HttpResponse.json({ configured: false })),
    )
    renderWithProviders(<ConnectorsPage />)
    expect(await screen.findByText('Idle state unknown')).toBeInTheDocument()
  })

  test('renders no idle inference for non-Cloud ClickHouse hosts (CH-CLOUD-7)', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          {
            id: 'c-ch-self', name: 'CH Self-hosted', type: 'clickhouse', is_default: false,
            config: { host: 'ch.internal.example.com' },
            last_success_at: '2020-01-01T00:00:00Z',
            created_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
      http.get('/api/v1/connectors/:id/cloud-state', () => HttpResponse.json({ configured: false })),
    )
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('CH Self-hosted')
    expect(screen.queryByText(/Likely idle|Idle state unknown|Active recently/)).toBeNull()
  })
```

**Step 2: Run to confirm the failures.**

```bash
cd web && npm run test:run -- src/test/ConnectorsPage.test.tsx
```

Expected: CH-CLOUD-3..7 fail on missing chip/inference text (`Unable to find an element with the text: Idle — wakes on next query`, etc.). The CH-CLOUD-2 test from Task 5 keeps passing once the `:id/cloud-state` routes are mocked.

**Step 3: Implement.**

**3a.** Update the imports in `web/src/pages/ConnectorsPage.tsx`:

```tsx
import type { Connector, ConnectorCloudState } from '../types'
```

and add:

```tsx
import {
  cloudStateView,
  connectorIdleTimeoutMinutes,
  formatRelativeAgo,
  inferIdleState,
} from '../utils/cloudState'
```

**3b.** Add the component (place it right after `ClickHouseCloudFields`):

```tsx
/** Second status line for ClickHouse connectors: exact Cloud control-plane
 * state when credentials are configured, probabilistic activity inference
 * otherwise. Control-plane reads never wake the service; the query polls one
 * connector at a time while the page stays open. */
function ClickHouseCloudStatus({ connector }: { connector: Connector }) {
  const { data, isError } = useQuery({
    queryKey: ['connector-cloud-state', connector.id],
    queryFn: () => api.get<ConnectorCloudState>(`/api/v1/connectors/${connector.id}/cloud-state`),
    staleTime: 30_000,
    refetchInterval: 60_000,
    retry: false,
  })

  const lineStyle: React.CSSProperties = {
    fontSize: 11,
    color: 'var(--text-muted)',
    marginTop: 4,
    fontStyle: 'italic',
  }

  const configuredHint = Boolean(
    connector.config?.cloud_org_id &&
    connector.config?.cloud_service_id &&
    connector.config?.cloud_key_id &&
    connector.config?.cloud_key_secret,
  )

  if (data?.configured && data.error) {
    return (
      <div style={lineStyle}>
        <StatusBadge status="neutral" label="Cloud state unavailable" title={data.error} />
      </div>
    )
  }
  if (isError) {
    return (
      <div style={lineStyle}>
        <StatusBadge status="neutral" label="Cloud state unavailable" title="Could not load Cloud state from Aether" />
      </div>
    )
  }
  if (data?.configured && data.state) {
    const view = cloudStateView(data.state)
    const scaling = data.idle_scaling ? 'Idle scaling on' : 'Idle scaling off'
    const timeout = data.idle_timeout_minutes ? `, timeout ${data.idle_timeout_minutes}m` : ''
    const checked = data.checked_at
      ? ` · checked ${formatRelativeAgo(Date.now() - new Date(data.checked_at).getTime())}`
      : ''
    return (
      <div style={lineStyle}>
        <StatusBadge status={view.status} label={view.label} title={`${scaling}${timeout}${checked}`} />
      </div>
    )
  }
  if (!data && configuredHint) {
    // Credentials exist but the first response has not arrived: don't flash an
    // inference line the exact state is about to replace.
    return null
  }

  const inference = inferIdleState({
    host: connector.config?.host,
    lastSuccessAt: connector.last_success_at,
    idleTimeoutMinutes: connectorIdleTimeoutMinutes(connector),
  })
  if (!inference) return null
  if (inference.kind === 'likely_idle') {
    return (
      <div style={lineStyle}>
        {`Likely idle — last activity ${formatRelativeAgo(inference.lastActivityAgeMs ?? 0)} (idle timeout ${inference.idleTimeoutMinutes}m)`}
      </div>
    )
  }
  if (inference.kind === 'active') {
    return <div style={lineStyle}>Active recently</div>
  }
  return <div style={lineStyle}>Idle state unknown</div>
}
```

**3c.** Insert the chip into each row's **Status** cell. Locate the `<td>` that renders the connector-health badge (the cell the connector-health plan rewrote, formerly the `testingIds`/`testResults` cell) and add, as the last child immediately before `</td>`:

```tsx
                    {c.type === 'clickhouse' && <ClickHouseCloudStatus connector={c} />}
```

Do not remove or reorder the health badge: health describes Aether↔database reachability, the cloud chip describes the ClickHouse Cloud control plane, and the two are intentionally distinct.

**3d.** In the `updateConnector` mutation's `onSuccess`, invalidate the cloud-state queries so new credentials refresh the chip immediately:

```tsx
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['connectors'] })
      qc.invalidateQueries({ queryKey: ['connector-cloud-state'] })
      closeEdit()
    },
```

**Step 4: Run to confirm the tests pass.**

```bash
cd web && npm run test:run -- src/test/ConnectorsPage.test.tsx
cd web && npm run test:run -- src/utils/cloudState.test.ts
cd web && npx tsc --noEmit
```

Expected: full ConnectorsPage suite green (including all pre-existing tests — the default MSW handler is what keeps them green), cloudState suite green, tsc clean.

**Step 5: Commit.**

```bash
git add web/src/pages/ConnectorsPage.tsx web/src/test/handlers.ts web/src/test/ConnectorsPage.test.tsx
git commit -m "feat(connectors): show ClickHouse Cloud state chip and idle inference"
```

---

### Task 7: Docs, broad verification, and real-browser validation

**Files:**
- Modify: `AGENTS.md` — add the ClickHouse Cloud idle-state paragraph
- No source changes expected; fix regressions if the broad runs expose any

**Step 1: Document the feature.**

In `AGENTS.md`, immediately after the paragraph `**Connector credentials** are AES-encrypted using ...` (~line 295), add:

```markdown
**ClickHouse Cloud idle state**: ClickHouse connectors carry an optional manual idle threshold `idle_timeout_minutes` (default 15 when absent) and optional ClickHouse Cloud API credentials (`cloud_org_id`, `cloud_service_id`, `cloud_key_id`, `cloud_key_secret` — masked as `***` and merge-preserved when an edit leaves it blank) inside their encrypted config. `GET /api/v1/connectors/{id}/cloud-state` (connector `view`) reads the control-plane service state (`running`/`idle`/`awaking`/`stopped`/`degraded`/`failed`) from the fixed host `https://api.clickhouse.cloud` with basic auth and a 5 s timeout; results are cached in memory ~45 s with in-flight dedupe per connector, and control-plane reads never wake an idle service. Without credentials, the Connectors page infers "likely idle" for `*.clickhouse.cloud` hosts when `last_success_at` is older than `idle_timeout_minutes` (activity-based, probabilistic).
```

**Step 2: Go verification.**

```bash
task fmt
go vet ./...
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/... -count=1 -timeout 3m
```

Expected: `go vet` clean; `internal/api` package green. If anything regressed, fix it before continuing. (For a PR, AGENTS.md additionally calls for `task check`; it is heavier than needed for this feature but is the repo's gate.)

**Step 3: Frontend verification.**

```bash
cd web && npx tsc --noEmit
cd web && npm run test:run
cd web && npm run build
```

Expected: tsc clean, all non-storybook unit tests green, production build succeeds (this catches the stricter `tsc -b`).

**Step 4: Real-browser validation with agent-browser (mandatory per AGENTS.md).**

```bash
docker compose -f docker-compose.dev.yml up -d
docker compose -f docker-compose.dev.yml ps
agent-browser open http://localhost:5173/connectors
agent-browser snapshot -i
```

Log in as `nova@heaven-labs.com` / `nova123` if prompted, then validate:

1. **Form fields**: New Connector → Type `ClickHouse` → "Idle timeout (minutes)" is visible with placeholder `15`; the "ClickHouse Cloud API (optional)" group expands and shows Organization ID / Service ID / Key ID / Key Secret.
2. **No-credential inference**: create a connector with host `abc.clickhouse.cloud`, no Cloud API fields → the Status column shows `Idle state unknown` (no `last_success_at` yet) rather than any exact-state chip.
3. **Graceful degradation**: edit that connector, fill the Cloud API fields with dummy values, Save → the chip switches to `Cloud state unavailable` with the upstream error in its tooltip; no error toast, no console error. Edit again → Key Secret is blank (stored, not echoed).
4. **Polling traffic**: open the browser network log; confirm `GET /api/v1/connectors/{id}/cloud-state` fires for ClickHouse rows (roughly once per minute) and that **no** `POST /connectors/{id}/test` fires from page load (that behavior belongs to the connector-health plan and must already be gone).

```bash
agent-browser screenshot
agent-browser errors
```

Upload/attach the screenshot to the PR and paste the console output summary. Exact states (`Running` / `Idle — wakes on next query` / `Stopped`) require real ClickHouse Cloud credentials; the mock-backed handler tests from Task 3 and the MSW tests from Task 6 cover those paths when real credentials are not available.

**Step 5: Commit.**

```bash
git add AGENTS.md
git commit -m "docs(connectors): document ClickHouse Cloud state endpoint and config keys"
```

If Step 2–4 required source fixes, commit those with their own focused messages before the docs commit.

---

## Compatibility notes (design §7, D10–D12)

- **Additive only.** The new route, response fields, and config keys change no existing behavior; older clients never see the new keys unless the form writes them.
- **No feature flags.** Cloud API support is opt-in by configuration; without credentials or egress the endpoint returns `{"configured": false}` and the page falls back to inference.
- **No migration.** Everything is stored in `connectors.config_encrypted`; nothing reads `config_encrypted` as a typed struct except the new credentials parser, which ignores unknown keys.
- **Security.** The upstream host is fixed (no SSRF), credentials are AES-encrypted at rest, `cloud_key_secret` is masked as `***` in every response, the secret is never logged, and upstream errors contain only transport/status text.
- **Known trade-offs** (all accepted in the design): config edits are visible within ≤45 s (cache TTL); a cancelled leader request can surface to single-flight followers as one failed poll (next poll retries); cache entries are evicted lazily on read, bounded by the number of ClickHouse connectors queried per process.
