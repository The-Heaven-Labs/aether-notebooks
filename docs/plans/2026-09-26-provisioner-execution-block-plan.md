# Provisioner Connector Execution Block Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prevent users (and agents/MCP) from executing queries through a warehouse's provisioner connector, while the reconcile worker keeps using its stored credential and org admins can optionally opt a warehouse into managed execution through it.

**Architecture:** The warehouse row is the single source of truth: a connector is a provisioner iff `warehouses.provisioner_connector_id = connectors.id`, and a new `warehouses.allow_provisioner_execution` boolean (default false) is the admin override. A fail-closed sentinel (`executor.ErrProvisionerNotExecutable`) is raised in `resolveExecutionTarget` before the kill-switch/stored-credential fallback; the three introspection handlers guard separately with an org-admin exception; service lists, preferences, and connector inventory reflect the block. The reconcile worker's raw clickhouse-go connection is untouched.

**Tech Stack:** Go (net/http ServeMux, pgx), Postgres migrations (Flyway-style, applied at startup), React + TypeScript + React Query + Vitest, sentinel-error testing with `testify/require`, real-DB tests (no mocks).

**Design doc:** `docs/plans/2026-09-26-provisioner-execution-block-design.md`

**Prerequisites:** `task infra:up` (Postgres/Redis/ClickHouse) or the dev Docker stack; Go tests always use `-timeout 3m` (AGENTS.md).

---

## Task 1: Migration + warehouse read model

**Files:**
- Create: `internal/database/migrations/V118__warehouse_allow_provisioner_execution.sql`
- Modify: `internal/api/warehouse_handlers.go:21-32` (warehouseJSON), `:42` (warehouseSelectColumns), `:84-89` (scanWarehouseRow)
- Test: `internal/api/warehouse_provisioner_execution_test.go` (new)

**Step 1: Write the failing test**

Create `internal/api/warehouse_provisioner_execution_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The provisioner execution override defaults to off and is part of the
// warehouse read model the admin UI toggles.
func TestWarehouseDefaultsAllowProvisionerExecutionFalse(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	wh, err := s.loadWarehouseForOrg(context.Background(), fx.orgID.String(), fx.warehouseID)
	require.NoError(t, err)
	require.False(t, wh.AllowProvisionerExecution)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestWarehouseDefaultsAllowProvisionerExecutionFalse -count=1 -timeout 3m -v`
Expected: compile failure (`wh.AllowProvisionerExecution` undefined) — this is the failing state.

**Step 3: Implement**

Create `internal/database/migrations/V118__warehouse_allow_provisioner_execution.sql`:

```sql
-- allow_provisioner_execution lets org admins opt a warehouse's provisioner
-- connector into user execution as a normal managed service. Off by default:
-- the provisioner credential is reserved for the reconcile worker, and user
-- queries must never fall back to it (including when the kill switch is off).
ALTER TABLE warehouses
    ADD COLUMN allow_provisioner_execution BOOLEAN NOT NULL DEFAULT false;
```

In `internal/api/warehouse_handlers.go`, add the field to `warehouseJSON` (after `ProvisionerConnectorID`):

```go
	AllowProvisionerExecution bool                     `json:"allow_provisioner_execution"`
```

Update the column list and scan order consistently:

```go
const warehouseSelectColumns = `id, org_id, name, provisioner_connector_id, allow_provisioner_execution, sync_status, sync_error, last_synced_at, created_at, updated_at`
```

```go
func scanWarehouseRow(row pgx.Row) (warehouseJSON, error) {
	var wh warehouseJSON
	err := row.Scan(&wh.ID, &wh.OrgID, &wh.Name, &wh.ProvisionerConnectorID,
		&wh.AllowProvisionerExecution, &wh.SyncStatus, &wh.SyncError,
		&wh.LastSyncedAt, &wh.CreatedAt, &wh.UpdatedAt)
	return wh, err
}
```

`handleCreateWarehouse` uses `INSERT ... RETURNING warehouseSelectColumns` + `scanWarehouseRow`, so it needs no further change.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api -run TestWarehouseDefaultsAllowProvisionerExecutionFalse -count=1 -timeout 3m -v`
Expected: PASS.

Also run the warehouse handler tests to catch scan-order regressions:
`go test ./internal/api -run 'TestWarehouse' -count=1 -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/database/migrations/V118__warehouse_allow_provisioner_execution.sql internal/api/warehouse_handlers.go internal/api/warehouse_provisioner_execution_test.go
git commit -m "feat: add warehouse allow_provisioner_execution flag"
```

---

## Task 2: Warehouse update endpoint accepts and audits the toggle

**Files:**
- Modify: `internal/api/warehouse_handlers.go:412-583` (updateWarehouseRequest + handleUpdateWarehouse)
- Test: `internal/api/warehouse_provisioner_execution_test.go`

**Step 1: Write the failing test**

Append to `internal/api/warehouse_provisioner_execution_test.go` (add imports `encoding/json`, `net/http`, `net/http/httptest`, `strings`):

```go
func TestUpdateWarehouseTogglesAllowProvisionerExecution(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	body := `{"allow_provisioner_execution": true}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/warehouses/"+fx.warehouseID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var wh warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wh))
	require.True(t, wh.AllowProvisionerExecution)

	var stored bool
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT allow_provisioner_execution FROM warehouses WHERE id = $1`,
		fx.warehouseID.String()).Scan(&stored))
	require.True(t, stored, "the toggle must persist")

	var metaJSON []byte
	require.NoError(t, s.db.Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE action = 'warehouse.update' AND resource_id = $1
		ORDER BY id DESC LIMIT 1`,
		fx.warehouseID.String()).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	require.Equal(t, true, meta["allow_provisioner_execution"])

	// An empty body is still rejected.
	emptyReq := httptest.NewRequest(http.MethodPut,
		"/api/v1/warehouses/"+fx.warehouseID.String(), strings.NewReader(`{}`))
	emptyReq.Header.Set("Content-Type", "application/json")
	emptyReq.Header.Set("Authorization", "Bearer "+token)
	emptyRec := httptest.NewRecorder()
	s.ServeHTTP(emptyRec, emptyReq)
	require.Equal(t, http.StatusBadRequest, emptyRec.Code, emptyRec.Body.String())
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestUpdateWarehouseTogglesAllowProvisionerExecution -count=1 -timeout 3m -v`
Expected: FAIL — the field is dropped/ignored, `wh.AllowProvisionerExecution` stays false and the empty body still returns 200 (400 assertion fails).

**Step 3: Implement**

Extend `updateWarehouseRequest`:

```go
type updateWarehouseRequest struct {
	Name                      json.RawMessage `json:"name"`
	ProvisionerConnectorID    json.RawMessage `json:"provisioner_connector_id"`
	AllowProvisionerExecution *bool           `json:"allow_provisioner_execution,omitempty"`
}
```

In `handleUpdateWarehouse`:

1. Update the empty-body guard:

```go
	if req.Name == nil && req.ProvisionerConnectorID == nil && req.AllowProvisionerExecution == nil {
		writeError(w, http.StatusBadRequest, "at least one field must be provided")
		return
	}
```

2. Load the old value in the `FOR UPDATE` select:

```go
	var oldName string
	var oldProvisioner *uuid.UUID
	var oldAllow bool
	err = tx.QueryRow(ctx, `
		SELECT name, provisioner_connector_id, allow_provisioner_execution FROM warehouses
		WHERE id = $1 AND org_id = $2 FOR UPDATE`,
		warehouseUUID.String(), claims.OrgID).Scan(&oldName, &oldProvisioner, &oldAllow)
```

3. Compute and persist the change:

```go
	changedName := name != nil && *name != oldName
	changedProvisioner := provisionerSet && !uuidPointersEqual(oldProvisioner, provisionerID)
	changedAllow := req.AllowProvisionerExecution != nil && *req.AllowProvisionerExecution != oldAllow
```

```go
	if changedAllow {
		if _, err := tx.Exec(ctx,
			`UPDATE warehouses SET allow_provisioner_execution = $1, updated_at = now() WHERE id = $2 AND org_id = $3`,
			*req.AllowProvisionerExecution, warehouseUUID.String(), claims.OrgID); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
```

4. Include the change in the commit/audit condition. Replace `if changedName || changedProvisioner {` with:

```go
	if changedName || changedProvisioner || changedAllow {
```

and inside the metadata block add:

```go
		if changedAllow {
			meta["allow_provisioner_execution"] = *req.AllowProvisionerExecution
		}
```

Note: this toggle does not require a reconcile — no ClickHouse user, role, or grant changes — so no `enqueueWarehouseSync` call.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api -run TestUpdateWarehouse -count=1 -timeout 3m -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/warehouse_handlers.go internal/api/warehouse_provisioner_execution_test.go
git commit -m "feat: let admins enable provisioner execution per warehouse"
```

---

## Task 3: Core block in execution-target resolution

**Files:**
- Modify: `internal/executor/execution_target.go:16-40` (new sentinel)
- Modify: `internal/api/execution_target.go:23-29` (warehouseService fields), `:51-63` (guard), `:184-205` (load query), `:245-268` (service list — filter comes in Task 7, not here)
- Modify: `internal/api/warehouse_kill_switch_test.go:72-78` (existing test now targets a non-provisioner service)
- Modify: `internal/api/execution_target_test.go:131-137` (same)
- Test: `internal/api/warehouse_provisioner_execution_test.go`

**Step 1: Write the failing tests**

Append to `internal/api/warehouse_provisioner_execution_test.go` (add imports `github.com/the-heaven-labs/aether/internal/chaccess`, `github.com/the-heaven-labs/aether/internal/executor`):

```go
// A provisioner connector must fail closed for user execution, and must never
// be reported as unmanaged (which would mean the stored-credential path).
func TestResolveExecutionTargetBlocksProvisionerByDefault(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	_, err := s.resolveExecutionTarget(context.Background(), fx.userID, fx.connectorID, false)
	require.ErrorIs(t, err, executor.ErrProvisionerNotExecutable)
	require.NotErrorIs(t, err, executor.ErrUnmanagedConnector)
}

// With the override on and managed mode enabled, the provisioner resolves as a
// normal managed service: per-user identity, never the stored credential.
func TestResolveExecutionTargetAllowsProvisionerWhenEnabled(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET sync_status = 'ready', allow_provisioner_execution = true WHERE id = $1`,
		fx.warehouseID.String())
	require.NoError(t, err)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	target, err := s.resolveExecutionTarget(ctx, fx.userID, fx.connectorID, false)
	require.NoError(t, err)
	require.Equal(t, fx.connectorID, target.ConnectorID)
	require.Equal(t, chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID), target.CHUser)
	require.NotEqual(t, "dev", target.Config.Password,
		"the provisioner's stored credential must never reach execution")
}

// The override cannot unblock the kill-switch-off stored-credential fallback.
func TestResolveExecutionTargetBlocksProvisionerWhenKillSwitchOff(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	s.SetCHTablePermissions(false)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`,
		fx.warehouseID.String())
	require.NoError(t, err)

	_, err = s.resolveExecutionTarget(ctx, fx.userID, fx.connectorID, false)
	require.ErrorIs(t, err, executor.ErrProvisionerNotExecutable)
	require.NotErrorIs(t, err, executor.ErrUnmanagedConnector)
}
```

Update the two existing tests that currently resolve the provisioner connector:

`internal/api/warehouse_kill_switch_test.go` (TestKillSwitchOffResolveExecutionTargetIsUnmanaged) — replace the resolve call with a non-provisioner service:

```go
func TestKillSwitchOffResolveExecutionTargetIsUnmanaged(t *testing.T) {
	s, key := newKillSwitchTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	serviceID := insertClickHouseService(t, s, fx.orgID, fx.connectorID, "Kill Switch Service", &fx.warehouseID)

	_, err := s.resolveExecutionTarget(context.Background(), fx.userID, serviceID, false)
	require.ErrorIs(t, err, executor.ErrUnmanagedConnector)
}
```

`internal/api/execution_target_test.go` (TestResolveExecutionTargetRequiresServiceAccess) — resolve a regular service instead of the provisioner:

```go
func TestResolveExecutionTargetRequiresServiceAccess(t *testing.T) {
	fx := setupExecutionTargetFixture(t)

	target, err := fx.resolve(t, fx.connA, false)
	require.ErrorIs(t, err, executor.ErrServiceAccessDenied)
	require.Nil(t, target)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run 'TestResolveExecutionTarget|TestKillSwitchOffResolveExecutionTargetIsUnmanaged' -count=1 -timeout 3m -v`
Expected: the three new tests FAIL (compile error `ErrProvisionerNotExecutable` undefined); existing tests pass once the sentinel exists.

**Step 3: Implement**

`internal/executor/execution_target.go` — add to the sentinel block:

```go
	// ErrProvisionerNotExecutable reports a connector that is its warehouse's
	// provisioner. The provisioner credential is reserved for the reconcile
	// worker; user-facing execution must fail closed instead of falling back
	// to the stored credential. Org admins can opt a warehouse into managed
	// execution through warehouses.allow_provisioner_execution.
	ErrProvisionerNotExecutable = errors.New("warehouse provisioner cannot execute user queries")
```

`internal/api/execution_target.go` — add fields to `warehouseService`:

```go
type warehouseService struct {
	id             uuid.UUID
	name           string
	encrypted      []byte
	maxRows        int
	timeoutSeconds int
	isProvisioner  bool
	// allowProvisionerExecution is the warehouse's admin override; meaningful
	// only when isProvisioner is true.
	allowProvisionerExecution bool
}
```

Extend `loadServiceConnector` (`:184-205`) with a second warehouse join:

```go
func (s *Server) loadServiceConnector(ctx context.Context, connectorID uuid.UUID) (warehouseService, *uuid.UUID, error) {
	var svc warehouseService
	var warehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `
		SELECT c.id, c.name, c.config_encrypted, c.max_rows, c.timeout_seconds, c.warehouse_id,
		       (wprov.id IS NOT NULL) AS is_provisioner,
		       COALESCE(wprov.allow_provisioner_execution, false) AS allow_provisioner_execution
		FROM connectors c
		LEFT JOIN warehouses w ON w.id = c.warehouse_id
		LEFT JOIN warehouses wprov ON wprov.provisioner_connector_id = c.id
		WHERE c.id = $1
		  AND c.deleted_at IS NULL
		  AND c.type = 'clickhouse'
		  AND (c.warehouse_id IS NULL OR w.org_id = c.org_id)`, connectorID.String()).
		Scan(&svc.id, &svc.name, &svc.encrypted, &svc.maxRows, &svc.timeoutSeconds,
			&warehouseID, &svc.isProvisioner, &svc.allowProvisionerExecution)
	if errors.Is(err, pgx.ErrNoRows) {
		s.warnRejectedConnectorLink(ctx, connectorID)
		return warehouseService{}, nil, fmt.Errorf("connector %s: %w: %w",
			connectorID, executor.ErrConnectorNotFound, err)
	}
	if err != nil {
		return warehouseService{}, nil, fmt.Errorf("connector %s: %w", connectorID, err)
	}
	return svc, warehouseID, nil
}
```

Add the guard in `resolveExecutionTarget` immediately after the connector load and **before** the kill-switch early return (`:56`). This is the critical ordering: a provisioner must never reach `ErrUnmanagedConnector` (the stored-credential fallback):

```go
	// A warehouse provisioner is reserved for the reconcile worker. Fail
	// closed before the kill-switch fallback: user execution may only proceed
	// through managed routing when the admin override is on, management is
	// enabled, and the connector's warehouse link is intact.
	if requested.isProvisioner &&
		(!requested.allowProvisionerExecution || !s.warehouseManagementEnabled() || requestedWarehouseID == nil) {
		slog.Warn("blocked execution through warehouse provisioner connector",
			"connector_id", requestedConnectorID.String(), "user_id", userID.String())
		return nil, fmt.Errorf("connector %s: %w", requestedConnectorID, executor.ErrProvisionerNotExecutable)
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api -run 'TestResolveExecutionTarget|TestKillSwitchOff' -count=1 -timeout 3m -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/executor/execution_target.go internal/api/execution_target.go internal/api/warehouse_provisioner_execution_test.go internal/api/warehouse_kill_switch_test.go internal/api/execution_target_test.go
git commit -m "feat: fail closed on provisioner connector execution"
```

---

## Task 4: HTTP cell execution returns 403

**Files:**
- Modify: `internal/api/execute_handlers.go:249-297` (resolution switch)
- Test: `internal/api/warehouse_provisioner_execution_test.go`

**Step 1: Write the failing tests**

Append (add imports `net/http` already added in Task 2):

```go
func TestExecuteCellOnProvisionerReturns403(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	nbID, cellID := seedExecuteWarehouseCell(t, s, fx.orgID, fx.userID, fx.connectorID,
		"SELECT currentUser()", nil)
	grantNotebookRun(t, s, fx.orgID, fx.userID, nbID)

	rec := executeWarehouseCell(t, s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, strings.ToLower(rec.Body.String()), "provisioner")
}

// With the kill switch off the handler would normally fall back to the stored
// credential. The provisioner config points at an unreachable host, so a
// fallback would surface as a gateway error (502) instead of the block.
func TestExecuteCellOnProvisionerKillSwitchOffReturns403(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	s.SetCHTablePermissions(false)
	fx := seedWarehouseFixtureRows(t, s, key)
	pointProvisionerAtUnreachableHost(t, s, key, fx.connectorID)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	nbID, cellID := seedExecuteWarehouseCell(t, s, fx.orgID, fx.userID, fx.connectorID,
		"SELECT currentUser()", nil)
	grantNotebookRun(t, s, fx.orgID, fx.userID, nbID)

	rec := executeWarehouseCell(t, s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, strings.ToLower(rec.Body.String()), "provisioner")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run TestExecuteCellOnProvisioner -count=1 -timeout 3m -v`
Expected: first test FAILS with 500 ("failed to resolve execution target") once the sentinel exists but is unmapped; second test FAILS with 502 (or 500). Both prove the fallback is currently reachable.

**Step 3: Implement**

In `handleExecuteCell`, add a case to the target-error switch (`internal/api/execute_handlers.go:250`), before `ErrUnmanagedConnector`:

```go
		case errors.Is(targetErr, executor.ErrProvisionerNotExecutable):
			writeError(w, http.StatusForbidden,
				"this connector is the warehouse provisioner and cannot run queries; choose another service or ask an admin to enable queries through the provisioner")
			return
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api -run TestExecuteCellOnProvisioner -count=1 -timeout 3m -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/execute_handlers.go internal/api/warehouse_provisioner_execution_test.go
git commit -m "feat: reject cell execution on provisioner connectors"
```

---

## Task 5: Agent and MCP error mapping

**Files:**
- Modify: `internal/agent/execution_target.go:40-61` (error switch)
- Test: `internal/api/warehouse_provisioner_execution_test.go` (drives the real agent tool, matching `TestKillSwitchOffAgentRejectsDeletedConnector`)

**Step 1: Write the failing test**

Append (add imports `github.com/the-heaven-labs/aether/internal/agent`):

```go
// The agent execute_sql tool shares resolveExecutionTarget; it must surface a
// provisioner-specific, actionable error and never open the stored credential.
func TestAgentExecuteSQLBlocksProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)
	ctx := context.Background()

	def, ok := s.agentEngine.GetRegistry().Get("execute_sql")
	require.True(t, ok, "execute_sql must be registered")
	args, err := json.Marshal(map[string]any{
		"connector_id": fx.connectorID.String(),
		"query":        "SELECT currentUser()",
	})
	require.NoError(t, err)

	tc := &agent.ToolContext{
		Context:             ctx,
		UserID:              fx.userID.String(),
		OrgID:               fx.orgID.String(),
		OrgRole:             "admin",
		DB:                  s.db.Pool,
		MasterKey:           s.masterKey,
		ResolveTarget:       s.resolveExecutionTarget,
		ConnPool:            s.connPool,
		CheckPermissionFunc: s.checkPermission,
	}
	_, err = def.Execute(args, tc)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "provisioner")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestAgentExecuteSQLBlocksProvisioner -count=1 -timeout 3m -v`
Expected: FAIL — the default branch wraps the sentinel as `resolve execution target for connector ...: warehouse provisioner cannot execute user queries`; the message contains "provisioner" already, but the user-facing message is unhelpful. More importantly this test locks the mapping in; to see a red state, first check the current output (`require.Contains` may already pass). If it passes before implementation, instead assert the exact actionable text:

```go
	require.Contains(t, err.Error(), "is the warehouse provisioner and cannot run queries")
```

and run again to confirm it fails with the generic wrapper.

**Step 3: Implement**

In `resolveClickHouseTarget` (`internal/agent/execution_target.go`), add a case:

```go
	case errors.Is(err, executor.ErrProvisionerNotExecutable):
		return nil, fmt.Errorf("connector %s is the warehouse provisioner and cannot run queries; choose another connector or ask an admin to enable queries through the provisioner", connectorID)
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api -run TestAgentExecuteSQLBlocksProvisioner -count=1 -timeout 3m -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/agent/execution_target.go internal/api/warehouse_provisioner_execution_test.go
git commit -m "feat: map provisioner block to agent tool errors"
```

---

## Task 6: Introspection endpoints — blocked for non-admins, admins keep access

**Files:**
- Modify: `internal/api/connector_handlers.go` — add helper near `buildExecutor` (`:601`); guard `handleListConnectorDatabases` (`:657`), `handleTestConnector` (`:701`), `handleConnectorSchema` (`:740`)
- Test: `internal/api/warehouse_provisioner_execution_test.go`

**Step 1: Write the failing tests**

Append (imports already include `net/http`, `net/http/httptest`, `strings`):

```go
func connectorIntrospectionRequest(t *testing.T, s *Server, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestProvisionerIntrospectionBlockedForNonAdmin(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "non-admin")
	require.NoError(t, err)
	base := "/api/v1/connectors/" + fx.connectorID.String()

	rec := connectorIntrospectionRequest(t, s, http.MethodGet, base+"/schema", token)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = connectorIntrospectionRequest(t, s, http.MethodGet, base+"/databases", token)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = connectorIntrospectionRequest(t, s, http.MethodPost, base+"/test", token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"ok":false`)
}

// Org admins keep introspection so the warehouse grant-management table
// browser keeps working when the provisioner is the only linked connector.
// The connector config points at an unreachable host: getting past the guard
// surfaces as a gateway error, not a 403.
func TestProvisionerIntrospectionAllowedForOrgAdmin(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	pointProvisionerAtUnreachableHost(t, s, key, fx.connectorID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)
	base := "/api/v1/connectors/" + fx.connectorID.String()

	rec := connectorIntrospectionRequest(t, s, http.MethodGet, base+"/schema", token)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())

	rec = connectorIntrospectionRequest(t, s, http.MethodGet, base+"/databases", token)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())

	rec = connectorIntrospectionRequest(t, s, http.MethodPost, base+"/test", token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"ok":false`)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run TestProvisionerIntrospection -count=1 -timeout 3m -v`
Expected: FAIL — non-admin with `use` currently gets 200/`ok:true` (or 502) instead of 403; admins currently get 403 from the `use` ACL check (no grant), not 502.

**Step 3: Implement**

Add the helper near `buildExecutor` in `internal/api/connector_handlers.go`:

```go
// connectorIsProvisioner reports whether a connector is the provisioner of its
// warehouse. Provisioner connectors are reserved for the reconcile worker:
// stored-credential introspection rejects them for everyone except org admins,
// who need the warehouse grant-management table browser to keep working.
func (s *Server) connectorIsProvisioner(ctx context.Context, connectorID string) (bool, error) {
	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM warehouses WHERE provisioner_connector_id = $1)`,
		connectorID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check provisioner connector %s: %w", connectorID, err)
	}
	return exists, nil
}
```

In each of the three handlers, replace the leading ACL check with the provisioner-aware gate. Example for `handleConnectorSchema` (`:745-749`):

```go
	isProvisioner, err := s.connectorIsProvisioner(ctx, connID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check connector")
		return
	}
	if isProvisioner {
		if claims.Role != "admin" {
			slog.Warn("blocked provisioner connector introspection",
				"connector_id", connID, "user_id", claims.UserID, "path", r.URL.Path)
			writeError(w, http.StatusForbidden,
				"the warehouse provisioner connector is reserved for background provisioning")
			return
		}
	} else {
		allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", connID, "use")
		if err != nil || !allowed {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}
```

Apply the same shape to `handleListConnectorDatabases` (`:662-666`). For `handleTestConnector` (`:706-710`) keep the HTTP 200 convention:

```go
	isProvisioner, err := s.connectorIsProvisioner(ctx, connID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "failed to check connector"})
		return
	}
	if isProvisioner {
		if claims.Role != "admin" {
			slog.Warn("blocked provisioner connector introspection",
				"connector_id", connID, "user_id", claims.UserID, "path", r.URL.Path)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false,
				"error": "the warehouse provisioner connector is reserved for background provisioning"})
			return
		}
	} else {
		allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", connID, "use")
		if err != nil || !allowed {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "insufficient permissions"})
			return
		}
	}
```

`handleConnectorSchema` also calls `touchSchemaSnapshot` later for admin reads; that is unchanged.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api -run TestProvisionerIntrospection -count=1 -timeout 3m -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/connector_handlers.go internal/api/warehouse_provisioner_execution_test.go
git commit -m "feat: gate provisioner introspection to org admins"
```

---

## Task 7: Exclude blocked provisioner from service lists and preferences

**Files:**
- Modify: `internal/api/execution_target.go:245-268` (`listWarehouseServices`)
- Modify: `internal/api/warehouse_grant_handlers.go:774-802` (`handleSetWarehousePreference`)
- Test: `internal/api/warehouse_provisioner_execution_test.go`

**Step 1: Write the failing tests**

Append (add import `github.com/google/uuid` if not already present):

```go
func TestListWarehouseServicesExcludesBlockedProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()
	serviceID := insertClickHouseService(t, s, fx.orgID, fx.connectorID, "Listed Service", &fx.warehouseID)

	services, err := s.listWarehouseServices(ctx, fx.warehouseID, fx.orgID)
	require.NoError(t, err)
	require.Len(t, services, 1)
	require.Equal(t, serviceID, services[0].id)

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)
	services, err = s.listWarehouseServices(ctx, fx.warehouseID, fx.orgID)
	require.NoError(t, err)
	require.Len(t, services, 2)
}

func TestSetWarehousePreferenceRejectsBlockedProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	rec := putPreferenceViaAPI(t, s, token, fx.warehouseID, &fx.connectorID)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)
	rec = putPreferenceViaAPI(t, s, token, fx.warehouseID, &fx.connectorID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run 'TestListWarehouseServicesExcludesBlockedProvisioner|TestSetWarehousePreferenceRejectsBlockedProvisioner' -count=1 -timeout 3m -v`
Expected: FAIL — the list still returns the provisioner while the override is off, and the preference is accepted (200, not 400).

**Step 3: Implement**

`listWarehouseServices` — join the warehouse and filter the provisioner unless allowed:

```go
func (s *Server) listWarehouseServices(ctx context.Context, warehouseID, orgID uuid.UUID) ([]warehouseService, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT c.id, c.name, c.config_encrypted, c.max_rows, c.timeout_seconds
		FROM connectors c
		JOIN warehouses w ON w.id = c.warehouse_id
		WHERE c.warehouse_id = $1 AND c.org_id = $2
		  AND c.type = 'clickhouse' AND c.deleted_at IS NULL
		  AND (w.provisioner_connector_id IS NULL
		       OR w.provisioner_connector_id <> c.id
		       OR w.allow_provisioner_execution)
		ORDER BY c.name ASC, c.id ASC`, warehouseID.String(), orgID.String())
	// ... unchanged scanning body
}
```

`handleSetWarehousePreference` — after the membership `EXISTS` check and before `connectorUseAllowed`:

```go
		var provisionerBlocked bool
		if err := s.db.Pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM warehouses
				WHERE id = $1 AND provisioner_connector_id = $2
				  AND NOT allow_provisioner_execution)`,
			warehouseUUID.String(), connectorID.String()).Scan(&provisionerBlocked); err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		if provisionerBlocked {
			writeError(w, http.StatusBadRequest,
				"the provisioner connector cannot be selected as a service; enable queries through the provisioner in warehouse settings first")
			return
		}
```

This also updates `effective-access` and grant-warning output automatically, since both call `allowedWarehouseServices` → `listWarehouseServices`.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api -run 'TestListWarehouseServices|TestSetWarehousePreference|TestWarehouseEffectiveAccess' -count=1 -timeout 3m -v`
Expected: PASS. If an existing effective-access test asserted the provisioner appears, update it to expect exclusion (each fixture's provisioner is not a service anymore).

**Step 5: Commit**

```bash
git add internal/api/execution_target.go internal/api/warehouse_grant_handlers.go internal/api/warehouse_provisioner_execution_test.go
git commit -m "feat: hide blocked provisioner from services and preferences"
```

---

## Task 8: Connector inventory exposes is_provisioner and effective can_use

**Files:**
- Modify: `internal/api/connector_handlers.go:196-243` (`handleListConnectors`)
- Test: `internal/api/warehouse_provisioner_execution_test.go`

**Step 1: Write the failing test**

Append a named type plus the test:

```go
type listedConnector struct {
	ID            string `json:"id"`
	CanUse        bool   `json:"can_use"`
	IsProvisioner bool   `json:"is_provisioner"`
}

func TestListConnectorsMarksBlockedProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	listConnectors := func() []listedConnector {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/connectors", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out []listedConnector
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}

	find := func(rows []listedConnector) *listedConnector {
		for i := range rows {
			if rows[i].ID == fx.connectorID.String() {
				return &rows[i]
			}
		}
		return nil
	}

	row := find(listConnectors())
	require.NotNil(t, row)
	require.True(t, row.IsProvisioner)
	require.False(t, row.CanUse, "a blocked provisioner must not be offered as usable")

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)

	row = find(listConnectors())
	require.NotNil(t, row)
	require.True(t, row.IsProvisioner)
	require.True(t, row.CanUse, "the override restores managed usability")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api -run TestListConnectorsMarksBlockedProvisioner -count=1 -timeout 3m -v`
Expected: FAIL — `is_provisioner` is always false and `can_use` is true.

**Step 3: Implement**

Change the query and DTO in `handleListConnectors`:

```go
	rows, err := s.db.Pool.Query(ctx,
		`SELECT c.id, c.org_id, c.name, c.type, c.config_encrypted, c.max_rows, c.timeout_seconds, c.is_default, c.created_at, c.updated_at, c.folder_id, c.warehouse_id, c.table_allowlist, c.table_denylist,
		        (wprov.id IS NOT NULL) AS is_provisioner,
		        COALESCE(wprov.allow_provisioner_execution, false) AS allow_provisioner_execution
		 FROM connectors c
		 LEFT JOIN warehouses wprov ON wprov.provisioner_connector_id = c.id
		 WHERE c.org_id = $1 AND c.deleted_at IS NULL ORDER BY c.name ASC`,
		claims.OrgID,
	)
```

```go
	type connectorWithPerms struct {
		models.Connector
		CanUse        bool `json:"can_use"`
		IsProvisioner bool `json:"is_provisioner"`
	}
```

In the loop scan the two extra columns and adjust `can_use`:

```go
		var isProvisioner, allowProvisionerExecution bool
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
			&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.WarehouseID, &c.TableAllowlist, &c.TableDenylist,
			&isProvisioner, &allowProvisionerExecution); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		// Filter by permission: only return connectors user can view
		allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "view")
		if !allowed {
			continue
		}
		canUse, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "use")
		if isProvisioner && (!allowProvisionerExecution || !s.warehouseManagementEnabled()) {
			canUse = false
		}
```

and the append:

```go
		result = append(result, connectorWithPerms{Connector: c, CanUse: canUse, IsProvisioner: isProvisioner})
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api -run TestListConnectorsMarksBlockedProvisioner -count=1 -timeout 3m -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/connector_handlers.go internal/api/warehouse_provisioner_execution_test.go
git commit -m "feat: expose provisioner state in connector listing"
```

---

## Task 9: Frontend — types, warehouse toggle, pickers, badge

**Files:**
- Modify: `web/src/types/index.ts:157-179` (`Connector`)
- Modify: `web/src/api/warehouses.ts:14-26` (`Warehouse`), `:133-138` (`updateWarehouse`)
- Modify: `web/src/pages/WarehouseSettingsPage.tsx` (mutation + `WarehouseCard` toggle)
- Modify: `web/src/components/Cell.tsx:586-590`
- Modify: `web/src/components/ConnectorSelector.tsx:5-10, 59-63`
- Modify: `web/src/pages/ConnectorsPage.tsx:461-481` (badge)
- Test: `web/src/components/Cell.test.tsx`

**Step 1: Write the failing test**

Append to `web/src/components/Cell.test.tsx` (inside a new `describe`):

```tsx
describe('connector picker provisioner handling', () => {
  it('labels and disables a blocked provisioner connector', async () => {
    const cell: CellType = {
      id: 'cell-prov',
      notebook_id: 'nb-1',
      type: 'code',
      language: 'sql',
      source: 'SELECT 1',
      outputs: [],
      position: 0,
      created_at: '',
      updated_at: '',
      source_visible: true,
      cell_collapsed: false,
      connector_id: 'conn-prov',
    }
    render(
      <Suspense fallback={null}>
        <Cell
          cell={cell}
          connectors={[
            { id: 'conn-prov', name: 'Provisioner', type: 'clickhouse', created_at: '', can_use: false, is_provisioner: true },
            { id: 'conn-svc', name: 'Service', type: 'clickhouse', created_at: '', can_use: true, is_provisioner: false },
          ]}
          notebookId="nb-1"
          onRun={vi.fn()}
          onDelete={vi.fn()}
          onSourceChange={vi.fn()}
          onAssignConnector={vi.fn()}
        />
      </Suspense>
    )

    fireEvent.click(await screen.findByTitle('Click to change connector'))
    const option = await screen.findByRole('option', { name: 'Provisioner (provisioner)' })
    expect(option).toBeDisabled()
  })
})
```

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run --project=default src/components/Cell.test.tsx -t "provisioner"`
Expected: FAIL — the option is labelled "(view only)" / `is_provisioner` is not a known prop yet (TS error) and the name does not match.

**Step 3: Implement**

`web/src/types/index.ts` — add to `Connector`:

```ts
  is_provisioner?: boolean
```

`web/src/api/warehouses.ts` — add to `Warehouse`:

```ts
  allow_provisioner_execution: boolean
```

and extend `updateWarehouse`:

```ts
export function updateWarehouse(
  id: string,
  data: { name?: string; provisioner_connector_id?: string | null; allow_provisioner_execution?: boolean },
): Promise<Warehouse> {
  return api.put<Warehouse>(`/api/v1/warehouses/${id}`, data)
}
```

`web/src/pages/WarehouseSettingsPage.tsx`:

1. Add a mutation after `renameMutation`:

```tsx
  const allowProvisionerMutation = useMutation({
    mutationFn: ({ id, allow }: { id: string; allow: boolean }) =>
      updateWarehouse(id, { allow_provisioner_execution: allow }),
    onSuccess: () => {
      invalidateWarehouses()
      setError(null)
    },
    onError: (err: Error) => setError(err.message),
  })
```

2. Pass a handler to each card (in the `warehouses.map`):

```tsx
                onSetAllowProvisionerExecution={(allow) =>
                  allowProvisionerMutation.mutate({ id: warehouse.id, allow })
                }
```

3. Extend `WarehouseCardProps` and the destructured props with `onSetAllowProvisionerExecution: (allow: boolean) => void`.

4. In the expanded body, directly after the provisioner select's field (`:483`), add:

```tsx
              <div style={styles.field}>
                <label style={{ ...styles.label, display: 'flex', alignItems: 'center', gap: 8 }}>
                  <input
                    type="checkbox"
                    aria-label="Allow queries through the provisioner"
                    checked={detail?.allow_provisioner_execution ?? warehouse.allow_provisioner_execution}
                    disabled={!detail?.provisioner_connector_id}
                    onChange={(e) => onSetAllowProvisionerExecution(e.target.checked)}
                  />
                  Allow queries through the provisioner
                </label>
                <span style={styles.hint}>
                  Off by default. The provisioner credential is reserved for background provisioning;
                  when off, notebook and agent queries cannot run through this connector.
                </span>
              </div>
```

`web/src/components/Cell.tsx` (`:586-590`):

```tsx
                {connectors.map((c) => (
                  <option key={c.id} value={c.id} disabled={c.can_use === false}>
                    {c.name}{c.is_provisioner ? ' (provisioner)' : c.can_use === false ? ' (view only)' : ''}
                  </option>
                ))}
```

`web/src/components/ConnectorSelector.tsx` — add `is_provisioner?: boolean` to `ConnectorItem` and the same label logic at `:59-63`.

`web/src/pages/ConnectorsPage.tsx` — after the `is_default` badge block (`:480`), add:

```tsx
                    {c.is_provisioner && (
                      <span style={{
                        fontSize: 11,
                        background: 'var(--bg-hover)',
                        border: '1px solid var(--border)',
                        borderRadius: 10,
                        padding: '2px 8px',
                        color: 'var(--text-secondary)',
                        fontWeight: 600,
                        marginLeft: 8,
                        display: 'inline-flex',
                        alignItems: 'center',
                      }}>
                        Provisioner
                      </span>
                    )}
```

**Step 4: Run tests and typecheck**

Run:
```bash
cd web && npx vitest run --project=default src/components/Cell.test.tsx
cd web && npx tsc --noEmit
```
Expected: PASS (all Cell tests + typecheck). If `--bg-hover` is not a defined theme variable, use `var(--bg-input)` (check `theme.css`).

**Step 5: Commit**

```bash
git add web/src/types/index.ts web/src/api/warehouses.ts web/src/pages/WarehouseSettingsPage.tsx web/src/components/Cell.tsx web/src/components/ConnectorSelector.tsx web/src/pages/ConnectorsPage.tsx web/src/components/Cell.test.tsx
git commit -m "feat: surface provisioner execution block in the UI"
```

---

## Task 10: Opt the RBAC e2e fixture in, regenerate swagger, docs, full verification

**Files:**
- Modify: `internal/api/warehouse_rbac_e2e_test.go:89-107`
- Modify: `internal/api/docs/*` (generated)
- Modify: `AGENTS.md` (warehouse paragraph)

**Step 1: Update the e2e fixture**

`TestWarehouseRBACE2E` runs user queries through the provisioner (it is the warehouse's only connector). After the warehouse is created (`:100`), enable the override through the API:

```go
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+warehouseID.String(), adminToken,
		map[string]any{"allow_provisioner_execution": true})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
```

Place it right after the `require.True(t, recorder.contains(warehouseID), ...)` assertion and before `grantConnectorUse`.

**Step 2: Run the e2e test**

Run: `go test ./internal/api -run TestWarehouseRBACE2E -count=1 -timeout 3m -v`
Expected: PASS (skips if the dev ClickHouse is unreachable).

**Step 3: Regenerate swagger**

```bash
swag init -g cmd/aether-server/main.go -o internal/api/docs
```

**Step 4: Update AGENTS.md**

In the warehouse paragraph ("Everything runs only when `AETHER_CH_TABLE_PERMISSIONS=true` ..."), add one sentence:

> A warehouse's provisioner connector is non-executable for users, agents, and MCP by default (fail-closed even when the kill switch is off); org admins can opt a warehouse into managed execution through it with `warehouses.allow_provisioner_execution`, and keep admin-only access to its `/test|/schema|/databases` introspection. The reconcile worker's raw connection is unaffected.

**Step 5: Full verification**

Run:
```bash
task fmt
go test ./internal/api ./internal/agent ./internal/executor -count=1 -timeout 3m
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
```
Then the full suite: `task check` (fmt + vet + tidy + test).
Expected: all green.

**Step 6: Commit**

```bash
git add internal/api/warehouse_rbac_e2e_test.go internal/api/docs AGENTS.md
git commit -m "test/docs: cover provisioner execution opt-in in e2e and docs"
```

---

## Final checklist

- [ ] Provisioner is blocked on cells, agent tools, MCP, and introspection (non-admin) by default.
- [ ] Kill switch off never falls back to the provisioner credential.
- [ ] Override on + management on routes the provisioner as a normal managed service.
- [ ] Reconcile/drift/drop worker paths untouched (`openWarehouseProvisionerConn`).
- [ ] Service lists, preferences, `can_use`, and pickers reflect the block.
- [ ] `task check`, web build, relay build, and swagger all pass.
- [ ] Feature branch `feat/provisioner-query-block` ready for PR (never commit to `main`).
