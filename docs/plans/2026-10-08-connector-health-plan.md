# Connector Health Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Persist one connection-outcome timeline per connector (`last_success_at`, `last_failure_at`, `last_error`), record it from every execution path, and make the Connectors page render that persisted status with **zero** data-plane probes on load.

**Architecture:** Migration V128 adds three nullable columns to `connectors`. A new `internal/api/connector_health.go` exposes `recordConnectorSuccess` (debounced ~30 s) and `recordConnectorFailure` (immediate, error text truncated to 500 chars), both detached from the request/deadline context via `context.WithoutCancel` + a short internal timeout. Recording sites are the shared choke points: `openQuery` (HTTP cells, dashboards, public dashboards), `handleExecuteCell`, `runDashboardQuery`, `handleTestConnector`, `handleConnectorSchema`, `handleListConnectorDatabases`, and the agent path via a new `ToolContext.RecordConnectorActivity` callback wired from `router.go` and `mcp.go` (also propagated through `Engine` and subagents like `ResolveTarget`). The frontend deletes the auto-test-on-load effect and derives status from the persisted fields.

**Tech Stack:** Go net/http ServeMux + pgx; React 18 + TypeScript + Vitest + MSW; Postgres migrations (Flyway-style, applied at startup).

---

## Background

Design: `docs/plans/2026-10-08-pending-grants-and-connector-health-design.md` §5.2 + decisions D6–D9.

- **D6** — Connector health is one outcome timeline: `last_success_at`, `last_failure_at`, `last_error`. Status is derived **on read**: failure newer than success → Failed; success exists → Connected; else Never used.
- **D7** — Only connection-level failures are recorded: explicit test failure; connect/dial failure while opening a run (all execution paths). SQL/semantic errors never flip status. Successes: completed runs (cell, dashboard incl. public, agent/MCP), successful tests, successful schema/databases introspection.
- **D8** — Success writes are debounced (~30 s per connector); failure writes are immediate.
- **D9** — No automatic tests on page load. The row action "Test connection" is the only data-plane trigger from the Connectors page; its outcome persists.
- **D12** — No rollout flags; ships unconditionally.

**Compatibility & rollout:** all changes are additive. `V128` only adds nullable columns (`ADD COLUMN IF NOT EXISTS`), so rollback is a no-op for old binaries and existing rows read as "Never used". The API adds optional JSON fields (`last_success_at`, `last_failure_at`, `last_error`) that older clients ignore. The `/connectors/{id}/test` endpoint keeps its `{ok, error}` contract and now also persists the outcome — a behavioral addition, not a break. The only intentional behavior change is the Connectors page no longer probing on load (idle databases stay idle, which is the point of the feature).

### Design notes (divergences from the design doc / prompt, verified against the code)

1. **`RecordConnectorActivity` signature.** The design doc writes `ToolContext.RecordConnectorActivity func(connectorID string, ok bool, errMsg string)`. This plan uses `func(ctx context.Context, orgID, connectorID string, ok bool, errMsg string)` because (a) the recorder is org-scoped and the `Engine` is constructed once per server and shared across orgs, so the org must ride the call; (b) it matches the existing `ResolveTarget` callback convention (`ctx` first, wired as a method value `s.recordConnectorActivity`).
2. **Handler name.** The prompt says `handleConnectorDatabases`; the real handler is `handleListConnectorDatabases` (`internal/api/connector_handlers.go`). This plan uses the real name.
3. **Lazily-dialed drivers.** `PostgresDriver.NewExecutor` parses config and creates a lazy pool — it does not dial. A dead Postgres server therefore surfaces at `Execute`/`Schema`/`TestConnection` time, not at `NewExecutor` time. Per D7, `Execute` errors are never recorded; the explicit Test action (which pings) and introspection `buildExecutor` failures cover those connectors. This is intentional, not a hole to "fix" in this plan.
4. **Migration path.** `AGENTS.md` says `migrations/`; the real directory is `internal/database/migrations/`. The new file is `internal/database/migrations/V128__connector_health.sql`.

## Environment prep (once)

```bash
task infra:up   # Postgres, Redis, ClickHouse on localhost
```

All Go test commands must carry `-timeout 3m` and this env prefix:

```
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379
```

Tests hit the shared dev database; every test below registers a unique email/org, so runs are repeatable.

---

### Task 1: Persist connector health columns (migration + model + read paths)

**Files:**
- Create: `internal/database/migrations/V128__connector_health.sql`
- Modify: `internal/models/connector.go` (`Connector` struct)
- Modify: `internal/api/connector_handlers.go` (`handleGetConnector`, `handleListConnectors`, `handleUpdateConnector` reload SELECT)
- Test: `internal/api/connector_health_test.go` (new)

**Step 1: Write the failing test** — create `internal/api/connector_health_test.go`:

```go
package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/database"
)

// updateConnectorHealth writes the health columns directly so read paths can
// be asserted independently of the recording call sites.
func updateConnectorHealth(t *testing.T, db *database.DB, connID string, lastSuccess, lastFailure *time.Time, lastError string) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(),
		`UPDATE connectors SET last_success_at = $2, last_failure_at = $3, last_error = $4 WHERE id = $1`,
		connID, lastSuccess, lastFailure, lastError)
	require.NoError(t, err)
}

// getConnectorJSON fetches a connector through the API and decodes the full
// response body, so nil (omitted) health fields are observable as absent keys.
func getConnectorJSON(t *testing.T, srv *api.Server, token, connID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/connectors/"+connID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	return out
}

func TestConnectorHealthFieldsRoundTrip(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-health-%d@example.com", ts), "Conn Health Org")
	connID := createConnector(t, srv, token)

	success := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	failure := time.Now().UTC().Truncate(time.Microsecond)
	updateConnectorHealth(t, db, connID, &success, &failure, "dial tcp: connection refused")

	// GET returns the timeline.
	got := getConnectorJSON(t, srv, token, connID)
	successStr, ok := got["last_success_at"].(string)
	require.True(t, ok, "last_success_at must be present: %v", got)
	parsedSuccess, err := time.Parse(time.RFC3339Nano, successStr)
	require.NoError(t, err)
	require.WithinDuration(t, success, parsedSuccess, time.Millisecond)
	require.Equal(t, "dial tcp: connection refused", got["last_error"])

	// List returns the same fields.
	req := httptest.NewRequest("GET", "/api/v1/connectors", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list []map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&list))
	var found map[string]any
	for _, c := range list {
		if c["id"] == connID {
			found = c
			break
		}
	}
	require.NotNil(t, found, "connector %s missing from list", connID)
	require.Equal(t, "dial tcp: connection refused", found["last_error"])
	failureStr, ok := found["last_failure_at"].(string)
	require.True(t, ok, "last_failure_at must be present: %v", found)
	parsedFailure, err := time.Parse(time.RFC3339Nano, failureStr)
	require.NoError(t, err)
	require.WithinDuration(t, failure, parsedFailure, time.Millisecond)
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run TestConnectorHealthFieldsRoundTrip -count=1 -timeout 3m
```

Expected failure: `updateConnectorHealth` dies with `ERROR: column "last_success_at" of relation "connectors" does not exist (SQLSTATE 42703)`.

**Step 3: Write minimal implementation**

3a. Create `internal/database/migrations/V128__connector_health.sql`:

```sql
-- Connector health: one outcome timeline per connector so the Connectors page
-- can render persisted status without probing data planes on load (D6-D9).
-- Additive and nullable; existing rows start as "Never used".
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_success_at TIMESTAMPTZ;
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_failure_at TIMESTAMPTZ;
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_error TEXT;
```

3b. `internal/models/connector.go` — add after `DeletedAt`:

```go
	// Connector health timeline (D6): status is derived by comparing
	// last_failure_at with last_success_at; last_error carries the most recent
	// connection-level failure message.
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
```

3c. `internal/api/connector_handlers.go` — `handleGetConnector` SELECT (add columns + scan):

```go
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, is_default, created_at, updated_at, folder_id, warehouse_id, table_allowlist, table_denylist,
		        last_success_at, last_failure_at, COALESCE(last_error, '')
		 FROM connectors WHERE id=$1 AND org_id=$2`,
		id, claims.OrgID,
	).Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
		&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.WarehouseID, &c.TableAllowlist, &c.TableDenylist,
		&c.LastSuccessAt, &c.LastFailureAt, &c.LastError)
```

The `COALESCE(last_error, '')` is required: `LastError` is a plain `string` and pgx cannot scan `NULL` into it. Apply the same pattern to every SELECT that feeds `models.Connector`.

3d. `handleListConnectors` SELECT:

```go
	rows, err := s.db.Pool.Query(ctx,
		`SELECT c.id, c.org_id, c.name, c.type, c.config_encrypted, c.max_rows, c.timeout_seconds, c.is_default, c.created_at, c.updated_at, c.folder_id, c.warehouse_id, c.table_allowlist, c.table_denylist,
		        c.last_success_at, c.last_failure_at, COALESCE(c.last_error, ''),
		        EXISTS (SELECT 1 FROM warehouses wp WHERE wp.provisioner_connector_id = c.id AND wp.org_id = c.org_id) AS is_provisioner,
		        CASE WHEN w.provisioner_connector_id = c.id THEN w.allow_provisioner_execution ELSE false END AS allow_provisioner_execution
		 FROM connectors c
		 LEFT JOIN warehouses w ON w.id = c.warehouse_id
		 WHERE c.org_id = $1 AND c.deleted_at IS NULL ORDER BY c.name ASC`,
		claims.OrgID,
	)
```

and extend the scan (before the two provisioner booleans):

```go
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
			&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.WarehouseID, &c.TableAllowlist, &c.TableDenylist,
			&c.LastSuccessAt, &c.LastFailureAt, &c.LastError,
			&isProvisioner, &allowProvisionerExecution); err != nil {
```

3e. `handleUpdateConnector` final reload SELECT:

```go
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, is_default, created_at, updated_at, folder_id, warehouse_id, table_allowlist, table_denylist,
		        last_success_at, last_failure_at, COALESCE(last_error, '')
		 FROM connectors WHERE id=$1`,
		id,
	).Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
		&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.WarehouseID, &c.TableAllowlist, &c.TableDenylist,
		&c.LastSuccessAt, &c.LastFailureAt, &c.LastError)
```

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run TestConnectorHealthFieldsRoundTrip -count=1 -timeout 3m
```

Expected: `ok github.com/the-heaven-labs/aether/internal/api`.

**Step 5: Commit**

```bash
git add internal/database/migrations/V128__connector_health.sql internal/models/connector.go internal/api/connector_handlers.go internal/api/connector_health_test.go
git commit -m "feat(connectors): persist connector health columns and expose them in list/get"
```

---

### Task 2: Health recorder + record on the explicit Test endpoint

**Files:**
- Create: `internal/api/connector_health.go`
- Modify: `internal/api/connector_handlers.go` (`handleTestConnector` only; `handleTestConnectorConfig` stays untouched)
- Test: `internal/api/connector_health_test.go` (append)

**Step 1: Write the failing test** — append to `internal/api/connector_health_test.go`, and add `"bytes"` to its import block:

```go
// createConnectorWithConfig posts a postgres connector with the given raw
// config and returns its id.
func createConnectorWithConfig(t *testing.T, srv *api.Server, token, name string, config map[string]any) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "type": "postgres", "config": config})
	req := httptest.NewRequest("POST", "/api/v1/connectors", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp["id"].(string)
}

// testConnectorEndpoint calls POST /connectors/{id}/test and returns the body.
func testConnectorEndpoint(t *testing.T, srv *api.Server, token, connID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/connectors/"+connID+"/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&out))
	return out
}

func TestConnectorHealthRecordedOnTestEndpoint(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-test-health-%d@example.com", ts), "Conn Test Health Org")

	// Success: the helper connector points at the dev database.
	goodID := createConnector(t, srv, token)
	resp := testConnectorEndpoint(t, srv, token, goodID)
	require.Equal(t, true, resp["ok"], resp)
	good := getConnectorJSON(t, srv, token, goodID)
	require.NotNil(t, good["last_success_at"], "a successful test must persist last_success_at")
	require.Nil(t, good["last_failure_at"], "a successful test must not set last_failure_at")

	// Failure: nothing listens on port 1, so TestConnection's ping is refused.
	badID := createConnectorWithConfig(t, srv, token, "Broken DB", map[string]any{
		"host": "127.0.0.1", "port": 1, "user": "x", "password": "x", "database": "x",
	})
	resp = testConnectorEndpoint(t, srv, token, badID)
	require.Equal(t, false, resp["ok"], resp)
	bad := getConnectorJSON(t, srv, token, badID)
	require.NotNil(t, bad["last_failure_at"], "a failed test must persist last_failure_at")
	require.NotEmpty(t, bad["last_error"], "a failed test must persist the error text")
	require.Nil(t, bad["last_success_at"], "a failed test must not set last_success_at")
}

func TestConnectorHealthSuccessDebounced(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-debounce-%d@example.com", ts), "Conn Debounce Org")
	connID := createConnector(t, srv, token)

	require.Equal(t, true, testConnectorEndpoint(t, srv, token, connID)["ok"])
	first := getConnectorJSON(t, srv, token, connID)["last_success_at"]
	require.NotNil(t, first, "the first test must persist a success timestamp")

	// Without the debounce, the second write would land on a later timestamp
	// (Postgres NOW() has microsecond resolution).
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, true, testConnectorEndpoint(t, srv, token, connID)["ok"])
	second := getConnectorJSON(t, srv, token, connID)["last_success_at"]

	require.Equal(t, first, second, "success writes within the ~30s window must be debounced")
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestConnectorHealthRecordedOnTestEndpoint|TestConnectorHealthSuccessDebounced' -count=1 -timeout 3m
```

Expected failure: `TestConnectorHealthRecordedOnTestEndpoint` fails on `a successful test must persist last_success_at`, and `TestConnectorHealthSuccessDebounced` fails on `the first test must persist a success timestamp` (nothing records yet).

**Step 3: Write minimal implementation**

3a. Create `internal/api/connector_health.go`:

```go
package api

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// connectorHealthWriteTimeout bounds the detached health write. Recording is
// best-effort: a slow or unavailable database must never block or fail the
// request that triggered it.
const connectorHealthWriteTimeout = 2 * time.Second

// maxConnectorErrorChars bounds the persisted last_error text.
const maxConnectorErrorChars = 500

// recordConnectorSuccess stamps last_success_at, debounced to at most one
// write per 30 seconds per connector (D8). The write is detached from ctx so a
// cancelled or timed-out execution context cannot skip it.
func (s *Server) recordConnectorSuccess(ctx context.Context, orgID, connectorID string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectorHealthWriteTimeout)
	defer cancel()
	if _, err := s.db.Pool.Exec(writeCtx,
		`UPDATE connectors SET last_success_at = NOW()
		 WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
		   AND (last_success_at IS NULL OR last_success_at < NOW() - INTERVAL '30 seconds')`,
		connectorID, orgID,
	); err != nil {
		slog.Warn("connector health: record success", "connector_id", connectorID, "error", err)
	}
}

// recordConnectorFailure stamps last_failure_at and last_error immediately
// (D8). last_success_at is left alone: status derivation compares the two
// timestamps on read (D6).
func (s *Server) recordConnectorFailure(ctx context.Context, orgID, connectorID string, failure error) {
	if failure == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectorHealthWriteTimeout)
	defer cancel()
	if _, err := s.db.Pool.Exec(writeCtx,
		`UPDATE connectors SET last_failure_at = NOW(), last_error = $3
		 WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		connectorID, orgID, truncateConnectorError(failure.Error()),
	); err != nil {
		slog.Warn("connector health: record failure", "connector_id", connectorID, "error", err)
	}
}

// recordConnectorActivity is the agent/MCP-facing adapter: one callback, two
// outcomes. ok=true debounces a success write; ok=false is an immediate
// failure write. Wired onto agent.Engine and fulfilled for MCP by mcp.go.
func (s *Server) recordConnectorActivity(ctx context.Context, orgID, connectorID string, ok bool, errMsg string) {
	if ok {
		s.recordConnectorSuccess(ctx, orgID, connectorID)
		return
	}
	if errMsg == "" {
		errMsg = "connection failed"
	}
	s.recordConnectorFailure(ctx, orgID, connectorID, errors.New(errMsg))
}

// truncateConnectorError caps persisted error text without splitting a
// multibyte rune.
func truncateConnectorError(msg string) string {
	runes := []rune(msg)
	if len(runes) <= maxConnectorErrorChars {
		return msg
	}
	return string(runes[:maxConnectorErrorChars])
}
```

3b. `internal/api/connector_handlers.go` — in `handleTestConnector`, replace the tail:

```go
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		s.recordConnectorFailure(ctx, claims.OrgID, connID, err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "failed to connect"})
		return
	}
	defer exec.Close()

	if err := exec.TestConnection(ctx); err != nil {
		s.recordConnectorFailure(ctx, claims.OrgID, connID, err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "connection failed"})
		return
	}
	s.recordConnectorSuccess(ctx, claims.OrgID, connID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
```

Do **not** touch `handleTestConnectorConfig` (`POST /connectors/test`): it tests an unsaved config, so there is no connector row to attribute the outcome to — it records nothing by design.

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestConnectorHealth' -count=1 -timeout 3m
```

Expected: all `TestConnectorHealth*` pass.

**Step 5: Commit**

```bash
git add internal/api/connector_health.go internal/api/connector_handlers.go internal/api/connector_health_test.go
git commit -m "feat(connectors): record health from the test endpoint with debounced successes"
```

---

### Task 3: Record connect failures in `openQuery` and successes after cell/dashboard execution

**Files:**
- Modify: `internal/api/query_runner.go` (`openQuery`, the three `errQueryConnectFailed` returns)
- Modify: `internal/api/execute_handlers.go` (`handleExecuteCell`, after the `Execute` error block)
- Modify: `internal/api/dashboard_query_handlers.go` (`runDashboardQuery`, after the `Execute` error block)
- Test: `internal/api/connector_health_test.go` (append)

**Step 1: Write the failing test** — append to `internal/api/connector_health_test.go`:

```go
// executeCell runs a cell through the shared HTTP endpoint.
func executeCell(t *testing.T, srv *api.Server, token, nbID, cellID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/cells/"+cellID+"/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestConnectorHealthRecordsCellExecutionSuccess(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-cell-ok-%d@example.com", ts), "Conn Cell OK Org")
	connID := createConnector(t, srv, token)
	nbID := createNotebook(t, srv, token, "Health NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1 AS result", connID)

	rec := executeCell(t, srv, token, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got := getConnectorJSON(t, srv, token, connID)
	require.NotNil(t, got["last_success_at"], "a completed run must persist last_success_at")
	require.Nil(t, got["last_failure_at"], "a completed run must not set last_failure_at")
}

func TestConnectorHealthRecordsCellConnectFailure(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-cell-fail-%d@example.com", ts), "Conn Cell Fail Org")

	// A postgres config without host/database can never construct an executor,
	// exercising openQuery's driver.NewExecutor connect-failure branch.
	connID := createConnectorWithConfig(t, srv, token, "No Host", map[string]any{})
	nbID := createNotebook(t, srv, token, "Broken NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", connID)

	rec := executeCell(t, srv, token, nbID, cellID)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())

	got := getConnectorJSON(t, srv, token, connID)
	require.NotNil(t, got["last_failure_at"], "a connect failure must persist last_failure_at")
	require.NotEmpty(t, got["last_error"], "a connect failure must persist the error text")
	require.Nil(t, got["last_success_at"], "a failed run must not set last_success_at")
}

func TestConnectorHealthRecordsDashboardExecutionSuccess(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-dash-ok-%d@example.com", ts), "Conn Dash OK Org")
	connID := createConnector(t, srv, token)
	dashID := createDashWithSettings(t, srv, token, nil)
	widgetID := addQueryWidget(t, srv, token, dashID, connID, "SELECT 1 AS one")

	rec := executeDashboardWidget(t, srv, token, dashID, map[string]any{"widget_id": widgetID})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got := getConnectorJSON(t, srv, token, connID)
	require.NotNil(t, got["last_success_at"], "a completed dashboard run must persist last_success_at")
}
```

Note: `createDashWithSettings`, `addQueryWidget`, and `executeDashboardWidget` already exist in `internal/api/dashboard_query_test.go` (same `api_test` package). The public dashboard path shares `runDashboardQuery`, so it is covered by the same change.

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestConnectorHealthRecords' -count=1 -timeout 3m
```

Expected failure: the three new tests fail on the missing `last_success_at` / `last_failure_at` assertions (Task 2 only records from the test endpoint).

**Step 3: Write minimal implementation**

3a. `internal/api/query_runner.go` — the three connect-failure returns in `openQuery`:

```go
		case targetErr == nil:
			conn, release, getErr := s.connPool.Get(target.Endpoint, target.CHUser, target.Config)
			if getErr != nil {
				s.recordConnectorFailure(ctx, orgID, connectorID, getErr)
				return nil, errQueryConnectFailed
			}
```

```go
		case errors.Is(targetErr, executor.ErrUnmanagedConnector):
			out.Exec, err = driver.NewExecutor(plain)
			if err != nil {
				s.recordConnectorFailure(ctx, orgID, connectorID, err)
				return nil, errQueryConnectFailed
			}
```

```go
	default:
		out.Exec, err = driver.NewExecutor(plain)
		if err != nil {
			s.recordConnectorFailure(ctx, orgID, connectorID, err)
			return nil, errQueryConnectFailed
		}
	}
```

3b. `internal/api/execute_handlers.go` — in `handleExecuteCell`, immediately after the `if err != nil { ... return }` block following `exec.Execute` (i.e. right before `queryTime := time.Since(queryStart).Milliseconds()`):

```go
	// Persisted connector health: a completed run is the success signal (D7).
	// SQL/semantic errors handled above never flip status.
	s.recordConnectorSuccess(ctx, claims.OrgID, opened.DialedConnectorID)
```

3c. `internal/api/dashboard_query_handlers.go` — in `runDashboardQuery`, after the `Execute` error return:

```go
	queryStart := time.Now()
	result, err := opened.Exec.Execute(execCtx, p.SQL, nil, executor.OutputLimits{MaxBytes: maxBytes, MaxRows: maxRows})
	queryMS := time.Since(queryStart).Milliseconds()
	if err != nil {
		return nil, mapDashboardExecError(err)
	}
	// Persisted connector health: a completed run is the success signal (D7).
	s.recordConnectorSuccess(ctx, p.OrgID, p.ConnectorID)
```

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestConnectorHealth' -count=1 -timeout 3m
```

Expected: all `TestConnectorHealth*` tests (Tasks 1–3) pass.

**Step 5: Commit**

```bash
git add internal/api/query_runner.go internal/api/execute_handlers.go internal/api/dashboard_query_handlers.go internal/api/connector_health_test.go
git commit -m "feat(connectors): record connect failures and run successes across HTTP execution paths"
```

---

### Task 4: Record schema/databases introspection outcomes

**Files:**
- Modify: `internal/api/connector_handlers.go` (`handleConnectorSchema`, `handleListConnectorDatabases`)
- Test: `internal/api/connector_health_test.go` (append)

**Step 1: Write the failing test** — append to `internal/api/connector_health_test.go`:

```go
func TestConnectorHealthRecordsSchemaIntrospection(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-schema-%d@example.com", ts), "Conn Schema Org")
	connID := createConnector(t, srv, token)

	req := httptest.NewRequest("GET", "/api/v1/connectors/"+connID+"/schema", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got := getConnectorJSON(t, srv, token, connID)
	require.NotNil(t, got["last_success_at"], "successful introspection must persist last_success_at")
}

func TestConnectorHealthRecordsDatabasesConnectFailure(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()
	token := registerAndGetToken(t, srv, fmt.Sprintf("conn-dbs-fail-%d@example.com", ts), "Conn DBs Fail Org")

	// Empty config: buildExecutor cannot construct a postgres executor.
	connID := createConnectorWithConfig(t, srv, token, "No Host", map[string]any{})

	req := httptest.NewRequest("GET", "/api/v1/connectors/"+connID+"/databases", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())

	got := getConnectorJSON(t, srv, token, connID)
	require.NotNil(t, got["last_failure_at"], "a buildExecutor failure must persist last_failure_at")
	require.NotEmpty(t, got["last_error"])
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestConnectorHealthRecordsSchemaIntrospection|TestConnectorHealthRecordsDatabasesConnectFailure' -count=1 -timeout 3m
```

Expected failure: `successful introspection must persist last_success_at` and `a buildExecutor failure must persist last_failure_at`.

**Step 3: Write minimal implementation**

3a. `handleConnectorSchema`:

```go
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		s.recordConnectorFailure(ctx, claims.OrgID, connID, err)
		writeError(w, http.StatusBadGateway, "failed to connect")
		return
	}
	defer exec.Close()

	schema, err := exec.Schema(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "schema fetch failed")
		return
	}
	// A successful catalog read is a success signal (D7). Query errors above
	// stay unrecorded.
	s.recordConnectorSuccess(ctx, claims.OrgID, connID)
```

3b. `handleListConnectorDatabases`:

```go
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		s.recordConnectorFailure(ctx, claims.OrgID, connID, err)
		writeError(w, http.StatusBadGateway, "failed to connect")
		return
	}
	defer exec.Close()

	dbs, err := exec.Databases(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to list databases")
		return
	}
	s.recordConnectorSuccess(ctx, claims.OrgID, connID)
	if dbs == nil {
		dbs = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"databases": dbs})
```

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ -run 'TestConnectorHealth' -count=1 -timeout 3m
```

Expected: all `TestConnectorHealth*` tests pass.

**Step 5: Commit**

```bash
git add internal/api/connector_handlers.go internal/api/connector_health_test.go
git commit -m "feat(connectors): record schema and databases introspection outcomes"
```

---

### Task 5: Record agent/MCP execution outcomes via `ToolContext.RecordConnectorActivity`

**Files:**
- Modify: `internal/agent/types.go` (`ToolContext` field + helper methods)
- Modify: `internal/agent/engine.go` (`Engine` field + `ToolContext` construction)
- Modify: `internal/agent/subagent.go` (both `ToolContext` constructions)
- Modify: `internal/agent/execution_target.go` (two connect-failure sites in `openAgentExecutor`)
- Modify: `internal/agent/tools_sql.go` (`executeAgentSQL` success)
- Modify: `internal/agent/tools_notebook.go` (`executeCell` — the handler behind `run_cell` — success site)
- Modify: `internal/api/router.go` (wire `s.agentEngine.RecordConnectorActivity = s.recordConnectorActivity`)
- Modify: `internal/api/mcp.go` (`mcpToolContext` wiring)
- Test: `internal/agent/connector_health_test.go` (new)

> **Design note:** the design doc's signature `func(connectorID string, ok bool, errMsg string)` is widened to `func(ctx context.Context, orgID, connectorID string, ok bool, errMsg string)` so the single `Engine`-level callback can be assigned directly from the org-scoped `s.recordConnectorActivity` and matches the existing `ResolveTarget` convention (`ctx` first).

**Step 1: Write the failing test** — create `internal/agent/connector_health_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

type connectorHealthCall struct {
	orgID       string
	connectorID string
	ok          bool
	errMsg      string
}

type connectorHealthCapture struct {
	calls []connectorHealthCall
}

func (c *connectorHealthCapture) record(_ context.Context, orgID, connectorID string, ok bool, errMsg string) {
	c.calls = append(c.calls, connectorHealthCall{orgID: orgID, connectorID: connectorID, ok: ok, errMsg: errMsg})
}

func (c *connectorHealthCapture) last(t *testing.T) connectorHealthCall {
	t.Helper()
	require.NotEmpty(t, c.calls, "expected a recorded connector activity")
	return c.calls[len(c.calls)-1]
}

func TestExecuteSQLRecordsConnectorHealthOnSuccess(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createSQLTestPGConnector(t, db, orgID, userID)

	capture := &connectorHealthCapture{}
	ctx := &ToolContext{
		Context:                 context.Background(),
		UserID:                  userID,
		OrgID:                   orgID,
		OrgRole:                 "admin",
		DB:                      db.Pool,
		MasterKey:               masterKey,
		CheckPermissionFunc:     allowAllPermissions,
		RecordConnectorActivity: capture.record,
	}

	handler := makeExecuteSQLHandler(db.Pool)
	runExecuteSQL(t, handler, ctx, map[string]any{"connector_id": connID, "query": "SELECT 1"})

	last := capture.last(t)
	require.Equal(t, orgID, last.orgID)
	require.Equal(t, connID, last.connectorID)
	require.True(t, last.ok)
	require.Empty(t, last.errMsg)
}

func TestOpenAgentExecutorRecordsConnectorHealthOnConnectFailure(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createIdentityTestCHConnector(t, db, orgID, userID, storedCredentialCfg())

	capture := &connectorHealthCapture{}
	pool := executor.NewConnPool(executor.PoolConfig{
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			return nil, errors.New("dial tcp 10.0.0.5:9000: connect: connection refused")
		},
	})
	ctx := &ToolContext{
		Context:   context.Background(),
		UserID:    userID,
		OrgID:     orgID,
		OrgRole:   "admin",
		DB:        db.Pool,
		MasterKey: masterKey,
		ResolveTarget: func(context.Context, uuid.UUID, uuid.UUID, bool) (*executor.ExecutionTarget, error) {
			return &executor.ExecutionTarget{
				Endpoint: "warehouse.invalid:9000",
				CHUser:   "aether_test_wh_u_fail",
				Config: models.ConnectorConfig{
					Host: "warehouse.invalid", Port: 9000, User: "aether_test_wh_u_fail",
					Password: "derived", Database: "analytics",
				},
			}, nil
		},
		ConnPool:                pool,
		CheckPermissionFunc:     allowAllPermissions,
		RecordConnectorActivity: capture.record,
	}

	handler := makeExecuteSQLHandler(db.Pool)
	raw, err := json.Marshal(map[string]any{"connector_id": connID, "query": "SELECT 1"})
	require.NoError(t, err)
	_, err = handler(raw, ctx)
	require.ErrorContains(t, err, "connect to warehouse")

	last := capture.last(t)
	require.Equal(t, orgID, last.orgID)
	require.Equal(t, connID, last.connectorID)
	require.False(t, last.ok)
	require.Contains(t, last.errMsg, "connection refused")
}

func TestRunCellRecordsConnectorHealthOnSuccess(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	connID, masterKey := createSQLTestPGConnector(t, db, orgID, userID)
	nbID := createTestNotebook(t, db, orgID, userID)

	cellID := uuid.New().String()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO cells (id, notebook_id, type, language, connector_id, source, position, "limit", created_at, updated_at)
		VALUES ($1, $2, 'code', 'sql', $3, 'SELECT 1 AS x', 0, 1000, NOW(), NOW())
	`, cellID, nbID, connID)
	require.NoError(t, err)

	capture := &connectorHealthCapture{}
	ctx := &ToolContext{
		Context:                 context.Background(),
		UserID:                  userID,
		OrgID:                   orgID,
		OrgRole:                 "admin",
		NotebookID:              nbID,
		DB:                      db.Pool,
		MasterKey:               masterKey,
		CheckPermissionFunc:     allowAllPermissions,
		RecordConnectorActivity: capture.record,
	}

	reg := NewToolRegistry()
	RegisterNotebookTools(reg, db.Pool)
	runCellDef, ok := reg.Get("run_cell")
	require.True(t, ok)

	args, _ := json.Marshal(map[string]any{"cell_id": cellID})
	_, err = runCellDef.Handler(args, ctx)
	require.NoError(t, err)

	last := capture.last(t)
	require.Equal(t, orgID, last.orgID)
	require.Equal(t, connID, last.connectorID)
	require.True(t, last.ok)
	require.Empty(t, last.errMsg)
}
```

(The helpers `setupEngineTestDB`, `createEngineTestOrgAndUser`, `createTestNotebook`, `createSQLTestPGConnector`, `createIdentityTestCHConnector`, `storedCredentialCfg`, `allowAllPermissions`, and `runExecuteSQL` all live in existing `package agent` test files.) A test file with `package agent` can coexist with the existing `package agent_test` files in the same directory.

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/agent/ -run 'TestExecuteSQLRecordsConnectorHealthOnSuccess|TestOpenAgentExecutorRecordsConnectorHealthOnConnectFailure|TestRunCellRecordsConnectorHealthOnSuccess' -count=1 -timeout 3m
```

Expected failure: compile error — `unknown field RecordConnectorActivity in struct literal of type ToolContext` (the field does not exist yet).

**Step 3: Write minimal implementation**

3a. `internal/agent/types.go` — add to `ToolContext`, after the `ConnPool` field:

```go
	// RecordConnectorActivity reports a connection-level success or failure
	// for connector health (the api.Server implementation is the same
	// recorder HTTP uses; wired by the API server like ResolveTarget).
	// ok=true debounces a success write, ok=false records an immediate
	// failure with errMsg. Nil-safe via the helpers below so bare test
	// contexts (tests, tool handlers invoked outside a server) simply skip
	// recording.
	RecordConnectorActivity func(ctx context.Context, orgID, connectorID string, ok bool, errMsg string)
```

Add the helpers right after the `CheckPermission` method:

```go
// RecordConnectorSuccess reports a completed execution against connectorID.
func (tc *ToolContext) RecordConnectorSuccess(connectorID string) {
	if tc.RecordConnectorActivity == nil {
		return
	}
	tc.RecordConnectorActivity(tc.Context, tc.OrgID, connectorID, true, "")
}

// RecordConnectorFailure reports a connection-level failure for connectorID.
func (tc *ToolContext) RecordConnectorFailure(connectorID string, failure error) {
	if tc.RecordConnectorActivity == nil {
		return
	}
	msg := ""
	if failure != nil {
		msg = failure.Error()
	}
	tc.RecordConnectorActivity(tc.Context, tc.OrgID, connectorID, false, msg)
}
```

3b. `internal/agent/engine.go` — `Engine` struct, after `CheckPermissionFunc`:

```go
	// RecordConnectorActivity reports per-connector health from agent-driven
	// runs; wired by the API server like the resolvers above.
	RecordConnectorActivity func(ctx context.Context, orgID, connectorID string, ok bool, errMsg string)
```

And in the `ToolContext` construction inside the engine loop, after `CheckPermissionFunc:  e.CheckPermissionFunc,`:

```go
				RecordConnectorActivity: e.RecordConnectorActivity,
```

3c. `internal/agent/subagent.go` — add the same line (`RecordConnectorActivity: e.RecordConnectorActivity,`) to **both** `ToolContext` literals (in `runSubagent` and `runSubagentLoop`), directly after each `CheckPermissionFunc: e.CheckPermissionFunc,`.

3d. `internal/agent/execution_target.go` — record at the two connect sites in `openAgentExecutor`:

```go
			conn, release, err := tc.ConnPool.Get(target.Endpoint, target.CHUser, target.Config)
			if err != nil {
				tc.RecordConnectorFailure(connectorID, err)
				return nil, nil, fmt.Errorf("connect to warehouse for connector %s: %w", connectorID, err)
			}
```

```go
	exec, err := driver.NewExecutor(plain)
	if err != nil {
		tc.RecordConnectorFailure(connectorID, err)
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	return exec, nil, nil
```

3e. `internal/agent/tools_sql.go` — in `executeAgentSQL`, after the successful `Execute`:

```go
	result, err := exec.Execute(execCtx, query, params, executor.OutputLimits{MaxBytes: maxBytes, MaxRows: maxRows})
	if err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}
	tc.RecordConnectorSuccess(connectorID)

	return &sqlExecutionResult{ResultSet: result, ExecutionID: executionID}, nil
```

3f. `internal/agent/tools_notebook.go` — in `executeCell` (the handler behind the `run_cell` tool), immediately after the `if err != nil { ... return res, nil }` block (before `totalTimeMs := time.Since(startTime).Milliseconds()`):

```go
	ctx.RecordConnectorSuccess(*cell.ConnectorID)
```

3g. `internal/api/router.go` — after `s.agentEngine.CheckPermissionFunc = s.checkPermission`:

```go
	s.agentEngine.RecordConnectorActivity = s.recordConnectorActivity
```

3h. `internal/api/mcp.go` — in `mcpToolContext`, after `CheckPermissionFunc: s.checkPermission,`:

```go
		RecordConnectorActivity: s.recordConnectorActivity,
```

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/agent/ ./internal/api/ -run 'ConnectorHealth' -count=1 -timeout 3m
```

Expected: all connector-health tests in both packages pass.

**Step 5: Commit**

```bash
git add internal/agent/types.go internal/agent/engine.go internal/agent/subagent.go internal/agent/execution_target.go internal/agent/tools_sql.go internal/agent/tools_notebook.go internal/api/router.go internal/api/mcp.go internal/agent/connector_health_test.go
git commit -m "feat(connectors): record agent/MCP connection outcomes via ToolContext.RecordConnectorActivity"
```

---

### Task 6: Frontend — relative-time helper

**Files:**
- Create: `web/src/utils/formatRelativeTime.ts`
- Test: `web/src/utils/formatRelativeTime.test.ts`

**Step 0 (setup):** the worktree has no `web/node_modules`:

```bash
cd web && npm ci
```

**Step 1: Write the failing test** — create `web/src/utils/formatRelativeTime.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { formatRelativeTime } from './formatRelativeTime'

const NOW = Date.parse('2026-10-08T12:00:00Z')
const at = (secondsAgo: number) => new Date(NOW - secondsAgo * 1000).toISOString()

describe('formatRelativeTime', () => {
  it('returns an empty string for missing or invalid timestamps', () => {
    expect(formatRelativeTime(null, NOW)).toBe('')
    expect(formatRelativeTime(undefined, NOW)).toBe('')
    expect(formatRelativeTime('not-a-date', NOW)).toBe('')
  })

  it('renders recent instants as just now', () => {
    expect(formatRelativeTime(at(5), NOW)).toBe('just now')
    expect(formatRelativeTime(at(59), NOW)).toBe('just now')
  })

  it('renders minutes, hours, and days', () => {
    expect(formatRelativeTime(at(60), NOW)).toBe('1m ago')
    expect(formatRelativeTime(at(12 * 60), NOW)).toBe('12m ago')
    expect(formatRelativeTime(at(32 * 60), NOW)).toBe('32m ago')
    expect(formatRelativeTime(at(60 * 60), NOW)).toBe('1h ago')
    expect(formatRelativeTime(at(25 * 60 * 60), NOW)).toBe('1d ago')
  })

  it('clamps future timestamps to just now', () => {
    expect(formatRelativeTime(new Date(NOW + 5000).toISOString(), NOW)).toBe('just now')
  })
})
```

**Step 2: Run test to verify it fails**

```bash
cd web && npm run test:run -- formatRelativeTime.test.ts
```

Expected failure: module `./formatRelativeTime` cannot be resolved.

**Step 3: Write minimal implementation** — create `web/src/utils/formatRelativeTime.ts`:

```ts
/**
 * Compact relative time for persisted connector health labels
 * (e.g. "12m ago", "1h ago", "3d ago"). Returns '' for missing or invalid
 * input so callers can decide the fallback copy. `now` is an optional test
 * seam; production callers use the default.
 */
export function formatRelativeTime(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return ''
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  // Clamp future timestamps (clock skew) to "just now" instead of negative ages.
  const seconds = Math.max(0, Math.round((now - then) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}
```

**Step 4: Run test to verify it passes**

```bash
cd web && npm run test:run -- formatRelativeTime.test.ts
```

Expected: `4 passed`.

**Step 5: Commit**

```bash
git add web/src/utils/formatRelativeTime.ts web/src/utils/formatRelativeTime.test.ts
git commit -m "feat(web): add relative-time helper for connector health labels"
```

---

### Task 7: Connectors page — kill auto-test on load, render persisted status

**Files:**
- Modify: `web/src/types/index.ts` (`Connector` interface)
- Modify: `web/src/pages/ConnectorsPage.tsx`
- Test: `web/src/test/ConnectorsPage.test.tsx` (append)

**Step 1: Write the failing test** — append to `web/src/test/ConnectorsPage.test.tsx` (inside the existing `describe('ConnectorsPage', ...)`, before the closing `})`):

```tsx
  test('does not probe connectors on mount (zero /test requests)', async () => {
    const testSpy = vi.fn()
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors)),
      http.post('/api/v1/connectors/:id/test', () => {
        testSpy()
        return HttpResponse.json({ ok: true })
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Prod DB')
    await screen.findByText('Staging DB')
    // Give any buggy effect a beat to fire before asserting.
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(testSpy).not.toHaveBeenCalled()
  })

  test('renders the persisted health status without probing', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          {
            id: 'c-failed', name: 'Broken DB', type: 'postgres', config: {}, created_at: '2026-10-08T08:00:00Z',
            last_success_at: '2026-10-08T08:00:00Z',
            last_failure_at: '2026-10-08T09:00:00Z',
            last_error: 'dial tcp 10.0.0.5:5432: connection refused',
          },
          {
            id: 'c-recovered', name: 'Recovered DB', type: 'postgres', config: {}, created_at: '2026-10-08T08:00:00Z',
            last_success_at: '2026-10-08T10:00:00Z',
            last_failure_at: '2026-10-08T09:00:00Z',
          },
          {
            id: 'c-new', name: 'Fresh DB', type: 'postgres', config: {}, created_at: '2026-10-08T08:00:00Z',
          },
        ]),
      ),
    )
    renderWithProviders(<ConnectorsPage />)

    const failed = await screen.findByText(/^Failed · /)
    expect(failed).toHaveAttribute('title', 'dial tcp 10.0.0.5:5432: connection refused')
    expect(await screen.findByText(/^Connected · used /)).toBeInTheDocument()
    expect(screen.getByText('Never used — click Test')).toBeInTheDocument()
  })

  test('manual Test refreshes the persisted status', async () => {
    let connectors: Record<string, unknown>[] = [
      { id: 'c-1', name: 'Prod DB', type: 'postgres', config: {}, created_at: '2026-10-08T08:00:00Z' },
    ]
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(connectors)),
      http.post('/api/v1/connectors/c-1/test', () => {
        connectors = [{ ...connectors[0], last_success_at: new Date().toISOString() }]
        return HttpResponse.json({ ok: true })
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Never used — click Test')

    fireEvent.click(screen.getByLabelText('Test connection'))
    expect(screen.getByText('Testing…')).toBeInTheDocument()
    expect(await screen.findByText(/^Connected · used /)).toBeInTheDocument()
  })
```

**Step 2: Run test to verify it fails**

```bash
cd web && npm run test:run -- ConnectorsPage
```

Expected failures:
- "does not probe connectors on mount": `testSpy` was called (the auto-test effect fires `POST /test` for both connectors).
- "renders the persisted health status": `Unable to find an element with the text: /^Failed · /` — the page still renders `Unknown — click Test` from ephemeral state.
- "manual Test refreshes the persisted status": `Unable to find text: Never used — click Test`.

**Step 3: Write minimal implementation**

3a. `web/src/types/index.ts` — add to `Connector` (after `table_denylist`):

```ts
  /** Persisted connector health (V128): failure newer than success => Failed. */
  last_success_at?: string | null
  last_failure_at?: string | null
  /** Most recent connection-level failure message. */
  last_error?: string
```

3b. `web/src/pages/ConnectorsPage.tsx`:

- Add the import near the other utils/components:

```tsx
import { formatRelativeTime } from '../utils/formatRelativeTime'
```

- Delete the `testResults` state (line: `const [testResults, setTestResults] = useState<Record<string, { ok: boolean; error?: string }>>({})`).
- Delete the `autoTested` state, the `autoTestCancelled` ref, and both effects associated with them (the cleanup effect and the 3-worker probe effect). Keep the `edit` deep-link effect, the `permissions` deep-link effect, and the `document.title` effect.
- In `createConnector`'s `onSuccess`, delete the line `if (formTest) setTestResults((prev) => ({ ...prev, [connector.id]: formTest }))` (the unsaved-config test records nothing, so it must not be attributed to the new connector). The `formTest` state used by the create modal stays.
- Replace `testConnector` with a version that persists server-side and refetches:

```tsx
  const testConnector = async (id: string) => {
    setTestingIds((prev) => ({ ...prev, [id]: true }))
    try {
      await api.post<{ ok: boolean; error?: string }>(`/api/v1/connectors/${id}/test`, {})
    } catch {
      // The outcome is persisted server-side; the refetch below surfaces it.
      // A network failure leaves the last known persisted state in place.
    } finally {
      await qc.invalidateQueries({ queryKey: ['connectors'] })
      setTestingIds((prev) => { const n = { ...prev }; delete n[id]; return n })
    }
  }
```

- Add the status derivation helper at module scope (above `export function ConnectorsPage`, next to the other helpers):

```tsx
/** Derives the persisted health badge from the connector's outcome timeline
 * (D6): a failure newer than the last success wins; otherwise a success means
 * Connected; otherwise the connector has never been exercised. */
function connectorHealth(c: Connector): { status: 'success' | 'error' | 'neutral'; label: string; title?: string } {
  const failed = !!c.last_failure_at && (!c.last_success_at || new Date(c.last_failure_at) > new Date(c.last_success_at))
  if (failed) {
    const when = formatRelativeTime(c.last_failure_at)
    return { status: 'error', label: when ? `Failed · ${when}` : 'Failed', title: c.last_error || undefined }
  }
  if (c.last_success_at) {
    const when = formatRelativeTime(c.last_success_at)
    return { status: 'success', label: when ? `Connected · used ${when}` : 'Connected' }
  }
  return { status: 'neutral', label: 'Never used — click Test' }
}
```

- In the list map, replace `const test = testResults[c.id]` with `const health = connectorHealth(c)` and replace the Status cell body:

```tsx
                  <td style={cellStyle}>
                    {testingIds[c.id] ? (
                      <StatusBadge status="neutral" label="Testing…" />
                    ) : health.status === 'neutral' ? (
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>
                        {health.label}
                      </span>
                    ) : (
                      <StatusBadge
                        status={health.status}
                        label={health.label}
                        title={health.title}
                        icon={health.status === 'success' ? <Check size={12} /> : <X size={12} />}
                      />
                    )}
                  </td>
```

Everything else on the page (create/edit modals, `formTest`/`formTesting`, Test row action, warehouse link flow) stays as is.

**Step 4: Run test to verify it passes**

```bash
cd web && npm run test:run -- ConnectorsPage
cd web && npx tsc --noEmit
```

Expected: all `ConnectorsPage` tests pass (existing + 3 new) and no TypeScript errors.

**Step 5: Commit**

```bash
git add web/src/types/index.ts web/src/pages/ConnectorsPage.tsx web/src/test/ConnectorsPage.test.tsx
git commit -m "feat(web): render persisted connector health and stop probing on page load"
```

---

### Task 8: Update AGENTS.md and run full verification

**Files:**
- Modify: `AGENTS.md` (root of the worktree)

**Step 1: Edit the docs**

In `AGENTS.md`, after the paragraph that starts `**Connector credentials** are AES-encrypted …`, insert:

```markdown
**Connector health**: `connectors.last_success_at` / `last_failure_at` / `last_error` (V128) persist one outcome timeline per connector; status is derived on read (failure newer than success → Failed; success exists → Connected; else Never used). Only connection-level outcomes are recorded: the explicit `POST /connectors/{id}/test` action, connect/dial failures while opening a run in every execution path (HTTP cells, dashboards including public, agent/MCP via `ToolContext.RecordConnectorActivity`), successful schema/databases introspection, and completed runs. Success writes are debounced ~30 s per connector (`internal/api/connector_health.go`); failure writes are immediate and truncate the message to 500 chars. SQL/semantic errors never flip status. The Connectors page performs zero data-plane probes on load — the row action "Test connection" is the only trigger, and its outcome is persisted and refetched.
```

**Step 2: Broader verification**

```bash
task fmt
task vet
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test ./internal/api/ ./internal/agent/ -count=1 -timeout 3m
cd web && npx tsc --noEmit
cd web && npm run build
```

If the full suite is feasible on this machine, run `task check` instead of the two focused Go commands (it runs `fmt + vet + tidy + test`). The relay is untouched by this plan; `cd relay && npm run build` is only needed if its dependencies are installed and a full pre-PR pass is desired.

**Step 3: Real-browser validation (mandatory for UI changes, per AGENTS.md)**

```bash
docker compose -f docker-compose.dev.yml up -d
agent-browser open http://localhost:5173/connectors
agent-browser snapshot -i
agent-browser screenshot
agent-browser errors
```

Verify:
1. On page load, the API must receive **zero** `POST /api/v1/connectors/*/test` requests. Watch `docker compose -f docker-compose.dev.yml logs -f api` while reloading the page — only `GET /connectors` should appear.
2. Rows show `Connected · used …`, `Failed · …` (hover shows the stored `last_error`), or `Never used — click Test` from persisted data.
3. Click a row's "Test connection": the button spins, "Testing…" shows, and the row refreshes to the newly persisted status (broken connectors → `Failed · just now`).
4. Reload the page: statuses persist and no probes fire.
5. `agent-browser errors` reports no page errors.

**Step 4: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document connector health persistence and no-probe page load"
```

---

## Test/verification summary

| Layer | Coverage |
|---|---|
| Migration + model | `TestConnectorHealthFieldsRoundTrip` — list/get return the timeline; columns exist after V128. |
| Recorder | `TestConnectorHealthRecordedOnTestEndpoint` (success + failure), `TestConnectorHealthSuccessDebounced` (two quick successes keep one timestamp). |
| HTTP execution | `TestConnectorHealthRecordsCellExecutionSuccess`, `TestConnectorHealthRecordsCellConnectFailure` (502 + failure row), `TestConnectorHealthRecordsDashboardExecutionSuccess` (covers public dashboards via the shared `runDashboardQuery`). |
| Introspection | `TestConnectorHealthRecordsSchemaIntrospection` (success), `TestConnectorHealthRecordsDatabasesConnectFailure` (buildExecutor failure). |
| Agent/MCP | `TestExecuteSQLRecordsConnectorHealthOnSuccess`, `TestOpenAgentExecutorRecordsConnectorHealthOnConnectFailure`, `TestRunCellRecordsConnectorHealthOnSuccess`. MCP inherits the same callback through `mcpToolContext`. |
| Frontend | `formatRelativeTime` unit tests; `ConnectorsPage` mount-silence spy test, derived-status rendering (incl. `last_error` tooltip), and manual-Test refresh. |
| E2E | Real-browser pass per AGENTS.md (no probes on load, persisted statuses, Test refresh). |
