# Warehouse Routing — Selection Wins: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the selected connector the execution target everywhere (cells, dashboard widgets, agents, scheduled runs), demote `warehouse_service_preferences` to default-seeding only, and fail closed with an enriched 403 when the selected service is not usable.

**Architecture:** One semantic flip in `resolveExecutionTarget` (shared by all execution paths), a new `ServiceAccessDeniedError` carrying the allowed-services list, an optional viewer-selected connector for dashboard widget execution (same-warehouse precedence), and preference seeding at notebook creation. No migrations.

**Tech Stack:** Go (net/http, pgx/v5, testify), React + TypeScript (vitest, @tanstack/react-query).

**Design:** see `docs/plans/2026-10-07-warehouse-routing-selection-wins-design.md` (same repo).

**Working repo:** upstream clone of `github.com/The-Heaven-Labs/aether-notebooks` (references at v0.64.0). All paths below are relative to that clone root.

---

### Task 0: Workspace setup and green baseline

**Files:** none (setup only)

- [ ] **Step 1: Clone/fork and branch**

```bash
git clone git@github.com:<your-fork>/aether-notebooks.git && cd aether-notebooks
git checkout -b feat/warehouse-routing-selection-wins
```

- [ ] **Step 2: Baseline green**

Run: `go build ./... && go test ./internal/... 2>&1 | tail -5`
Expected: `ok` for all packages. If anything is red before your changes, stop and investigate.
Run: `cd web && npm ci && npm run build && npx vitest run --project=default 2>&1 | tail -5`
Expected: build succeeds, tests pass.

- [ ] **Step 3: Commit (empty, marks baseline)**

```bash
git commit --allow-empty -m "chore: baseline for warehouse routing selection-wins"
```

---

### Task 1: `executor.ServiceAccessDeniedError`

**Files:**
- Modify: `internal/executor/execution_target.go`
- Test: `internal/executor/execution_target_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

```go
package executor

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestServiceAccessDeniedErrorWrapsSentinel(t *testing.T) {
	wh := uuid.New()
	e := &ServiceAccessDeniedError{
		WarehouseID: wh,
		Allowed:     []ServiceChoice{{ConnectorID: uuid.New(), Name: "Service A"}},
	}
	require.ErrorIs(t, e, ErrServiceAccessDenied)
	require.Contains(t, e.Error(), "Service A")
	require.Equal(t, wh, e.WarehouseID)
	require.Len(t, e.Allowed, 1)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run TestServiceAccessDeniedError -v`
Expected: FAIL — `undefined: ServiceAccessDeniedError`.

- [ ] **Step 3: Implement**

In `internal/executor/execution_target.go`, directly below `ServiceChoiceError` (line ~63), add:

```go
// ServiceAccessDeniedError reports that the requested connector cannot serve
// the acting user: they hold no `use` grant on it. It carries the services
// the user MAY use in the same warehouse so callers can render an actionable
// message instead of a bare denial.
type ServiceAccessDeniedError struct {
	WarehouseID uuid.UUID
	Allowed     []ServiceChoice
}

func (e *ServiceAccessDeniedError) Error() string {
	names := make([]string, 0, len(e.Allowed))
	for _, svc := range e.Allowed {
		names = append(names, svc.Name)
	}
	return fmt.Sprintf("no access to the requested service; permitted services in warehouse %s: %s",
		e.WarehouseID, strings.Join(names, ", "))
}

// Unwrap makes errors.Is(err, ErrServiceAccessDenied) true for every caller
// that only needs the sentinel.
func (e *ServiceAccessDeniedError) Unwrap() error { return ErrServiceAccessDenied }
```

Add `"strings"` to the import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/executor/ -run TestServiceAccessDeniedError -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/executor/execution_target.go internal/executor/execution_target_test.go
git commit -m "feat(executor): ServiceAccessDeniedError with allowed-services list"
```

---

### Task 2: `resolveExecutionTarget` — selection wins

**Files:**
- Modify: `internal/api/execution_target.go:62-147`
- Test: `internal/api/execution_target_test.go`

The fixture (`setupExecutionTargetFixture`, line 35) provides `connA`, `connB` in one ready warehouse, plus `grantUse`, `revokeUse`, `prefer`, `resolve` helpers.

- [ ] **Step 1: Rewrite the routing tests (failing)**

Delete `TestResolveExecutionTargetUsesPreference` (line 139), `TestResolveExecutionTargetFallsBackToSoleService` (150), `TestResolveExecutionTargetHonorsPin` (161), `TestResolveExecutionTargetAmbiguousWithoutPreference` (182), and `TestResolveExecutionTargetCarriesRoutedServiceLimits` (254). Replace with:

```go
func TestResolveExecutionTargetRunsSelectedConnector(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)

	target, err := fx.resolve(t, fx.connA, false)
	require.NoError(t, err)
	require.Equal(t, fx.connA, target.ConnectorID)
	require.Equal(t, fx.warehouseID, target.WarehouseID)
}

func TestResolveExecutionTargetPreferenceIgnored(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB) // preference no longer routes

	target, err := fx.resolve(t, fx.connA, false)
	require.NoError(t, err)
	require.Equal(t, fx.connA, target.ConnectorID)
}

func TestResolveExecutionTargetSelectedConnectorDenied(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA) // other service is usable — still denied

	_, err := fx.resolve(t, fx.connB, false)
	var denied *executor.ServiceAccessDeniedError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, fx.warehouseID, denied.WarehouseID)
	require.Len(t, denied.Allowed, 1)
	require.Equal(t, "Service A", denied.Allowed[0].Name)
}

func TestResolveExecutionTargetNoAccessAnywhere(t *testing.T) {
	fx := setupExecutionTargetFixture(t)

	_, err := fx.resolve(t, fx.connA, false)
	require.ErrorIs(t, err, executor.ErrServiceAccessDenied)
}

func TestResolveExecutionTargetCarriesSelectedServiceLimits(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET max_rows = 123, timeout_seconds = 45 WHERE id = $1`, fx.connA.String())
	require.NoError(t, err)

	target, err := fx.resolve(t, fx.connA, false)
	require.NoError(t, err)
	require.Equal(t, 123, target.MaxRows)
	require.Equal(t, 45, target.TimeoutSeconds)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestResolveExecutionTarget' -count=1`
Expected: FAIL — preference routing still returns `connB` / `ServiceChoiceError`.

- [ ] **Step 3: Implement the flip**

In `internal/api/execution_target.go`, replace the body of `resolveExecutionTarget` from the `if pinned {` block (line 112) through the `default:` 409 return (line 146) with:

```go
	allowed, _, err := s.allowedWarehouseServices(ctx, userID, orgID, role, warehouseID)
	if err != nil {
		return nil, err
	}

	// Selection wins: the requested connector is the execution target when the
	// user holds `use` on it. The preference never re-routes a run.
	allowedOnRequested := false
	for _, svc := range allowed {
		if svc.id == requested.id {
			allowedOnRequested = true
			break
		}
	}
	if !allowedOnRequested {
		choices := make([]executor.ServiceChoice, 0, len(allowed))
		for _, svc := range allowed {
			choices = append(choices, executor.ServiceChoice{ConnectorID: svc.id, Name: svc.name})
		}
		return nil, &executor.ServiceAccessDeniedError{WarehouseID: warehouseID, Allowed: choices}
	}
	return s.buildExecutionTarget(warehouseID, warehouseName, orgID, userID, requested)
```

Delete the now-unused `pinned` parameter check but **keep the `pinned bool` signature** (callers still pass it; it is ignored). Update the doc comment (lines 35-61) to state: the requested connector is always the target when `use` is granted; preference is advisory only; `ErrServiceChoiceRequired` is no longer returned.

Keep `allowedWarehouseServices` unchanged — it still resolves the effective preference for effective-access reporting (and the allowed list reuses the stale-preference filtering, so a revoked service never appears in the denial payload).

- [ ] **Step 4: Run the full resolver suite**

Run: `go test ./internal/api/ -run 'TestResolveExecutionTarget' -count=1 -v`
Expected: PASS for all, including the untouched guards (unmanaged, not-ready, soft-deleted, cross-org, provisioner, non-ClickHouse, non-member).

- [ ] **Step 5: Commit**

```bash
git add internal/api/execution_target.go internal/api/execution_target_test.go
git commit -m "feat(api): warehouse routing selection-wins in resolveExecutionTarget"
```

---

### Task 3: HTTP error mapping — enriched 403, drop 409

**Files:**
- Modify: `internal/api/query_runner.go:145-180` (`writeOpenQueryError`)
- Modify: `internal/api/execute_handlers.go:349-362` (add `writeServiceAccessDenied`; update swagger comment)
- Modify: `internal/api/dashboard_query_handlers.go:71-81` (swagger comment)
- Test: `internal/api/execute_warehouse_test.go`

- [ ] **Step 1: Write the failing test** (append to `execute_warehouse_test.go`)

```go
func TestExecuteCellSelectedServiceDeniedPayload(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)

	rec := fx.executeCellOn(t, fx.connB) // helper below posts the execute request

	require.Equal(t, http.StatusForbidden, rec.Code)
	var body struct {
		Error       string            `json:"error"`
		WarehouseID string            `json:"warehouse_id"`
		Services    []map[string]any  `json:"services"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "service_access_denied", body.Error)
	require.Equal(t, fx.warehouseID.String(), body.WarehouseID)
	require.Len(t, body.Services, 1)
	require.Equal(t, "Service A", body.Services[0]["name"])
}
```

Add the helper to the same file's fixture type (mirror how existing tests in this file POST execute requests; if the file builds requests inline instead, inline it here the same way):

```go
func (fx *executionTargetFixture) executeCellOn(t *testing.T, connectorID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	nbID := createTestNotebookForConnector(t, fx.s, fx.orgID, fx.userID, connectorID)
	cellID := createTestSQLCell(t, fx.s, nbID, "SELECT 1")
	req := httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(`{}`))
	req.SetPathValue("notebook_id", nbID)
	req.SetPathValue("cell_id", cellID)
	rec := httptest.NewRecorder()
	fx.s.handleExecuteCell(rec, withClaims(req, fx.userID, fx.orgID))
	return rec
}
```

If `createTestNotebookForConnector` / `createTestSQLCell` / `withClaims` helpers already exist under different names in this test file, use those instead — match the exact pattern of `TestExecuteCellRoutesByServiceAccessNotCellConnector` (line 270), which this test replaces.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run TestExecuteCellSelectedServiceDeniedPayload -count=1`
Expected: FAIL — current body is plain `no permitted service in warehouse`.

- [ ] **Step 3: Implement the writer**

In `execute_handlers.go`, next to `writeServiceChoiceRequired` (line 349):

```go
func writeServiceAccessDenied(w http.ResponseWriter, e *executor.ServiceAccessDeniedError) {
	services := make([]map[string]string, 0, len(e.Allowed))
	for _, svc := range e.Allowed {
		services = append(services, map[string]string{
			"connector_id": svc.ConnectorID.String(),
			"name":         svc.Name,
		})
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error":        "service_access_denied",
		"warehouse_id": e.WarehouseID.String(),
		"services":     services,
	})
}
```

In `writeOpenQueryError` (`query_runner.go`), replace the `errQueryServiceDenied` case (lines 163-169) with:

```go
	case errors.Is(err, errQueryServiceDenied):
		var denied *executor.ServiceAccessDeniedError
		if errors.As(err, &denied) {
			writeServiceAccessDenied(w, denied)
			return
		}
		writeError(w, http.StatusForbidden, "no permitted service in warehouse")
```

Delete the `queryServiceChoiceError` case and `writeServiceChoiceRequired` only after Task 6 removes the last producer (`resolveExecutionTarget` no longer returns `ServiceChoiceError`; `openQuery` maps it — remove that `errors.As` branch and the `queryServiceChoiceError` type). Update both swagger `@Description` lines: drop the 409 sentence, add `403 service_access_denied with warehouse_id and services`.

- [ ] **Step 4: Verify**

Run: `go build ./... && go test ./internal/api/ -run 'TestExecuteCell' -count=1`
Expected: build OK; the new test passes; remaining 409-dependent tests are handled in Task 5.

- [ ] **Step 5: Commit**

```bash
git add internal/api/query_runner.go internal/api/execute_handlers.go internal/api/dashboard_query_handlers.go internal/api/execute_warehouse_test.go
git commit -m "feat(api): enriched service_access_denied 403 payload"
```

---

### Task 4: Agent resolver classification

**Files:**
- Modify: `internal/agent/execution_target.go:22-70`
- Test: `internal/agent/warehouse_identity_test.go`

- [ ] **Step 1: Rewrite the failing tests**

Find the cases asserting `ServiceChoiceRequired` / preference routing (search `ErrServiceChoiceRequired` in `warehouse_identity_test.go`). Replace with:

```go
func TestAgentResolveSelectedConnectorDeniedListsServices(t *testing.T) {
	tc := newIdentityFixture(t) // reuse the file's existing ToolContext fixture constructor
	tc.ResolveTarget = func(_ context.Context, u, c uuid.UUID, _ bool) (*executor.ExecutionTarget, error) {
		return nil, &executor.ServiceAccessDeniedError{
			WarehouseID: uuid.New(),
			Allowed:     []executor.ServiceChoice{{ConnectorID: uuid.New(), Name: "Service A"}},
		}
	}

	_, err := resolveClickHouseTarget(tc, fixtureConnectorID) // the fixture's connector id
	require.ErrorContains(t, err, "Service A")
}
```

(Keep the file's existing fixture constructor/names — mirror any neighboring test that injects `ResolveTarget`, e.g. the one at line 218.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/agent/ -run TestAgentResolveSelectedConnectorDenied -count=1`
Expected: FAIL — current message is "you do not have permission to use any service in the warehouse".

- [ ] **Step 3: Implement**

In `internal/agent/execution_target.go`, replace the `ErrServiceAccessDenied` case (line ~44) with:

```go
	case errors.Is(err, executor.ErrServiceAccessDenied):
		var denied *executor.ServiceAccessDeniedError
		if errors.As(err, &denied) && len(denied.Allowed) > 0 {
			names := make([]string, 0, len(denied.Allowed))
			for _, svc := range denied.Allowed {
				names = append(names, svc.Name)
			}
			return nil, fmt.Errorf("no access to connector %s; permitted services in this warehouse: %s", connectorID, strings.Join(names, ", "))
		}
		return nil, fmt.Errorf("you do not have permission to use connector %s", connectorID)
```

Delete the `ErrServiceChoiceRequired` case (lines ~47-56) — the resolver never returns it after Task 2.

- [ ] **Step 4: Verify**

Run: `go test ./internal/agent/ -count=1`
Expected: PASS (all agent tests; the `pinned=false` argument at line 35 still compiles — it is ignored).

- [ ] **Step 5: Commit**

```bash
git add internal/agent/execution_target.go internal/agent/warehouse_identity_test.go
git commit -m "feat(agent): actionable denial message for selection-wins routing"
```

---

### Task 5: Rewrite cell-execution tests to the new semantics

**Files:**
- Test: `internal/api/execute_warehouse_test.go`

These tests assert the old routing contract. Rewrite each in place (same fixtures, inverted expectations). Names change to describe the new contract.

- [ ] **Step 1: Rewrite the tests**

| Old test (line) | New name | New assertion core |
|---|---|---|
| `TestExecuteCellRoutesByServiceAccessNotCellConnector` (270) | `TestExecuteCellRunsOnCellConnector` | grant `use` on cell connector only; `routing.connector_id` == cell connector |
| `TestExecuteCellManagedNoServiceAccessDenied` (303) | `TestExecuteCellSelectedServiceDenied` | grant on `connA`, cell on `connB` → 403 `service_access_denied` (covered in Task 3) |
| `TestExecuteCellServiceChoiceRequired` (388) | *(delete)* | 409 no longer exists |
| `TestExecuteCellPinnedConnectorBypassesPreference` (422) | *(delete)* | pin is meaningless; behavior identical unpinned |
| `TestExecuteCellPreferenceRoutesAndLogsRoutedService` (501) | `TestExecuteCellPreferenceDoesNotRoute` | prefer `connB`, run cell on `connA` → `routing.connector_id` == `connA`; execution log records `connA` |
| `TestExecuteCellAppliesRoutedServiceLimits` (543) | `TestExecuteCellAppliesSelectedServiceLimits` | limits (max_rows/timeout) come from the cell's connector |

Pattern for the first rewrite (others follow the same shape, reusing each old test's fixtures and request plumbing):

```go
func TestExecuteCellRunsOnCellConnector(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA) // only the cell's connector is usable

	resp := fx.executeCellOn(t, fx.connA) // helper from Task 3
	require.Equal(t, http.StatusOK, resp.Code)

	var body struct {
		Routing map[string]any `json:"routing"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Equal(t, fx.connA.String(), body.Routing["connector_id"])
}
```

Also update `TestExecuteCellUsesPerUserIdentity` (164) only if it asserted preference-routed identity — the identity formula (`chaccess.UserIdent(warehouseID, orgID, userID)`) is unchanged; only the serving connector differs.

- [ ] **Step 2: Verify**

Run: `go test ./internal/api/ -run 'TestExecuteCell' -count=1 -v`
Expected: PASS, no references to `service_choice_required` remain in the file (`rg -c "service_choice_required|ServiceChoiceError" internal/api/execute_warehouse_test.go` → 0 matches).

- [ ] **Step 3: Commit**

```bash
git add internal/api/execute_warehouse_test.go
git commit -m "test(api): cell execution asserts selection-wins routing"
```

---

### Task 6: Dashboard widgets — viewer-selected connector (backend)

**Files:**
- Modify: `internal/api/dashboard_query_handlers.go:25-31` (request), `:85-186` (handler), `:59` (`dashboardQueryParams`)
- Test: `internal/api/dashboard_query_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `dashboard_query_test.go` (reuse the file's existing widget/dashboard fixture helpers; the two cases below describe the full contract):

```go
func TestDashboardQueryViewerConnectorSameWarehouseWins(t *testing.T) {
	fx := newDashboardQueryFixture(t) // file's existing fixture: dashboard + widget on connW (warehouse W)
	fx.grantUse(t, fx.widgetConn)
	fx.grantUse(t, fx.otherConnW) // second service in the SAME warehouse

	resp := fx.executeQuery(t, map[string]any{
		"widget_id":    fx.widgetID,
		"connector_id": fx.otherConnW.String(), // viewer selection
	})
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, fx.otherConnW.String(), resp.routingConnectorID())
}

func TestDashboardQueryViewerConnectorCrossWarehouseIgnored(t *testing.T) {
	fx := newDashboardQueryFixture(t)
	fx.grantUse(t, fx.widgetConn)
	fx.grantUse(t, fx.otherWarehouseConn) // different warehouse

	resp := fx.executeQuery(t, map[string]any{
		"widget_id":    fx.widgetID,
		"connector_id": fx.otherWarehouseConn.String(),
	})
	require.Equal(t, http.StatusOK, resp.Code)
	require.Equal(t, fx.widgetConn.String(), resp.routingConnectorID()) // widget's own connector served
}
```

If the file's fixture differs, adapt the two tests to it — the assertions (same-warehouse override serves; cross-warehouse ignored) are the contract.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run TestDashboardQueryViewerConnector -count=1`
Expected: FAIL — `connector_id` is not read from the request.

- [ ] **Step 3: Implement**

In `dashboard_query_handlers.go`:

```go
type dashboardExecuteRequest struct {
	WidgetID       string         `json:"widget_id"`
	ConnectorID    string         `json:"connector_id,omitempty"` // viewer's dashboard selector
	Variables      map[string]any `json:"variables,omitempty"`
	BypassCache    bool           `json:"bypass_cache,omitempty"`
}
```

Add a resolver next to `loadQueryWidget` (line 724):

```go
// resolveWidgetConnector applies the viewer's dashboard-selector choice:
// it serves the widget only when it lives in the same warehouse as the
// widget's saved connector; otherwise the widget's own connector wins
// (cross-warehouse selections cannot redirect a widget at tables that
// do not exist on the selected service).
func (s *Server) resolveWidgetConnector(ctx context.Context, orgID, widgetConnectorID, viewerConnectorID string) (string, error) {
	if viewerConnectorID == "" || viewerConnectorID == widgetConnectorID {
		return widgetConnectorID, nil
	}
	var whW, whV *string
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT warehouse_id FROM connectors WHERE id=$1 AND org_id=$2 AND deleted_at IS NULL`,
		widgetConnectorID, orgID).Scan(&whW); err != nil {
		return "", err
	}
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT warehouse_id FROM connectors WHERE id=$1 AND org_id=$2 AND deleted_at IS NULL`,
		viewerConnectorID, orgID).Scan(&whV); err != nil {
		return "", err // unknown viewer selection: fall through, widget connector serves
	}
	if whW != nil && whV != nil && *whW == *whV {
		return viewerConnectorID, nil
	}
	return widgetConnectorID, nil
}
```

In the execute handler (`handleDashboardQuery`, line ~85), after decoding the request and loading the widget, resolve before `runDashboardQuery`:

```go
	servedConnector, err := s.resolveWidgetConnector(ctx, claims.OrgID, w.ConnectorID, req.ConnectorID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
```

…pass `servedConnector` into the `dashboardQueryParams.ConnectorID` at line 144 (keep `pinned=false` — Task 2 already made it irrelevant). Wire the same resolution into the variable-options handler (line ~240) so schema-driven dropdowns agree with execution.

Update the swagger `@Param request body` line: `widget_id, connector_id, variables, bypass_cache`.

- [ ] **Step 4: Verify**

Run: `go test ./internal/api/ -run 'TestDashboardQuery' -count=1 -v`
Expected: PASS — both new tests and all pre-existing widget tests.

- [ ] **Step 5: Commit**

```bash
git add internal/api/dashboard_query_handlers.go internal/api/dashboard_query_test.go
git commit -m "feat(api): viewer-selected dashboard connector with same-warehouse precedence"
```

---

### Task 7: Notebook creation — preference seeding

**Files:**
- Modify: `internal/api/notebook_handlers.go:43-110` (`handleCreateNotebook`)
- Test: `internal/api/notebook_handlers_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCreateNotebookSeedsConnectorFromSolePreference(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)

	nb := fx.createNotebook(t) // helper: POST /notebooks {title} as fx.userID; reuse the file's create-notebook test plumbing
	require.Equal(t, fx.connB.String(), nb.ConnectorID)
}

func TestCreateNotebookAmbiguousPreferenceLeavesConnectorEmpty(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	// two usable services, no preference recorded

	nb := fx.createNotebook(t)
	require.Empty(t, nb.ConnectorID)
}

func TestCreateNotebookExplicitConnectorBeatsPreference(t *testing.T) {
	fx := setupExecutionTargetFixture(t)
	fx.grantUse(t, fx.connA)
	fx.grantUse(t, fx.connB)
	fx.prefer(t, fx.connB)

	nb := fx.createNotebookWithConnector(t, fx.connA)
	require.Equal(t, fx.connA.String(), nb.ConnectorID)
}
```

Adapt `createNotebook*` helpers to whatever request plumbing `notebook_handlers_test.go` already uses for `handleCreateNotebook`; the assertions above are the contract.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run TestCreateNotebook -count=1`
Expected: FAIL — connector stays empty or falls back to the org default.

- [ ] **Step 3: Implement**

In `handleCreateNotebook`, replace the existing `is_default` fallback block (lines ~95-109) with preference-first seeding:

```go
	if nb.ConnectorID == "" {
		// Seed from the creator's warehouse preference when it resolves to
		// exactly one live, granted service; otherwise fall back to the org
		// default connector. Existing notebooks are never re-seeded.
		var prefIDs []string
		rows, err := s.db.Pool.Query(ctx, `
			SELECT p.connector_id
			FROM warehouse_service_preferences p
			JOIN connectors c ON c.id = p.connector_id
			JOIN warehouses w ON w.id = c.warehouse_id AND w.org_id = c.org_id AND w.sync_status = 'ready'
			WHERE p.user_id = $1 AND c.org_id = $1 AND c.deleted_at IS NULL AND c.type = 'clickhouse'
			  AND EXISTS (
			    SELECT 1 FROM acl_entries a
			    WHERE a.org_id = $1 AND a.resource_type = 'connector' AND a.resource_id = c.id
			      AND a.subject_type = 'user' AND a.subject_id = p.user_id AND 'use' = ANY(a.actions)
			  )
			ORDER BY p.connector_id`,
			claims.OrgID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id string
				if serr := rows.Scan(&id); serr == nil {
					prefIDs = append(prefIDs, id)
				}
			}
		}
		switch {
		case len(prefIDs) == 1: // exactly one live preference — seed it
			_, _ = s.db.Pool.Exec(ctx, `UPDATE notebooks SET connector_id=$1 WHERE id=$2`, prefIDs[0], nb.ID)
			nb.ConnectorID = prefIDs[0]
		default: // zero, ambiguous (multiple warehouses), or lookup failure
			var defaultID string
			if derr := s.db.Pool.QueryRow(ctx,
				`SELECT id FROM connectors WHERE org_id=$1 AND is_default=true LIMIT 1`,
				claims.OrgID).Scan(&defaultID); derr == nil {
				_, _ = s.db.Pool.Exec(ctx, `UPDATE notebooks SET connector_id=$1 WHERE id=$2`, defaultID, nb.ID)
				nb.ConnectorID = defaultID
			}
		}
	}
```

The `EXISTS` guard reuses the ACL convention with a direct user grant; extend it with the same group/folder predicates `checkPermission` uses if direct-only is insufficient (match `grantConnectorUse`'s grant path). The "exactly one" rule is structural: `warehouse_service_preferences` is PK `(user_id, warehouse_id)`, so a creator with preferences in two warehouses yields two rows and the `default` branch applies.

- [ ] **Step 4: Verify**

Run: `go test ./internal/api/ -run 'TestCreateNotebook' -count=1 -v && go test ./internal/api/ -count=1`
Expected: PASS; full api package green.

- [ ] **Step 5: Commit**

```bash
git add internal/api/notebook_handlers.go internal/api/notebook_handlers_test.go
git commit -m "feat(api): seed new-notebook connector from sole warehouse preference"
```

---

### Task 8: Frontend — remove the notebook pin

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx:216-233, 1037-1043, 1055, 1452-1458`
- Modify: `web/src/components/ConnectorSelector.tsx`
- Test: `web/src/pages/NotebookPage.test.tsx`, `web/src/test/ConnectorSelector.test.tsx`

- [ ] **Step 1: Update the failing tests**

In `NotebookPage.test.tsx`: delete tests asserting pin behavior (search `pinned`); add:

```tsx
it('sends no pinned flag on execute', async () => {
  const post = vi.spyOn(api, 'post').mockResolvedValueOnce({ outputs: [], metrics: {} })
  renderNotebook() // file's existing render helper
  await runFirstCell()
  expect(post).toHaveBeenCalledWith(expect.stringContaining('/execute'), expect.not.objectContaining({ pinned: expect.anything() }))
})
```

In `ConnectorSelector.test.tsx`: delete the pin-toggle tests (search `pinned` / `Pin connector`).

- [ ] **Step 2: Run to verify failure**

Run: `cd web && npx vitest run --project=default src/pages/NotebookPage.test.tsx src/test/ConnectorSelector.test.tsx`
Expected: FAIL (pin tests still pass against old code / new test fails).

- [ ] **Step 3: Implement**

`NotebookPage.tsx`: delete the `pinState`/`toggleNotebookPin` block (lines 216-233), the `pinApplies` computation (1037-1043), and change the execute POST (1055) to:

```tsx
{ parameters: params },
```

Remove the `pinned` prop from the `ConnectorSelector` usage (1452-1458). `ConnectorSelector.tsx`: remove `pinned`/`onTogglePin` props, the lock button (lines 70-88), and the `Lock, LockOpen` import. Update `ExecuteRouting` handling — no change needed; the footer stays.

- [ ] **Step 4: Verify**

Run: `cd web && npx vitest run --project=default src/pages/NotebookPage.test.tsx src/test/ConnectorSelector.test.tsx && npm run build`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/NotebookPage.tsx web/src/components/ConnectorSelector.tsx web/src/pages/NotebookPage.test.tsx web/src/test/ConnectorSelector.test.tsx
git commit -m "feat(web): remove notebook connector pin — selection always wins"
```

---

### Task 9: Frontend — dashboard viewer connector selector

**Files:**
- Create: `web/src/hooks/useDashboardConnector.ts`
- Modify: `web/src/hooks/useWidgetQuery.ts`, `web/src/pages/DashboardPage.tsx`
- Test: `web/src/hooks/useDashboardConnector.test.tsx`, `web/src/hooks/useWidgetQuery.test.tsx`

- [ ] **Step 1: Write the failing hook test**

```tsx
import { renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

const effectiveAccess = vi.fn()
const listWarehouses = vi.fn()
vi.mock('../api/warehouses', () => ({
  effectiveAccess: (...a: unknown[]) => effectiveAccess(...a),
  listWarehouses: (...a: unknown[]) => listWarehouses(...a),
}))

describe('useDashboardConnector', () => {
  it('starts empty and persists the viewer choice per user+dashboard', () => {
    const { result, rerender } = renderHook(() => useDashboardConnector('dash-1', 'user-1'))
    expect(result.current.selected).toBeNull()
    result.current.select('conn-2')
    rerender()
    expect(JSON.parse(localStorage.getItem('aether_dash_connector:user-1:dash-1')!)).toBe('conn-2')
  })

  it('exposes the sole warehouse preference as the initial suggestion', async () => {
    listWarehouses.mockResolvedValue([{ id: 'w1', name: 'CSIRT_SIEM' }])
    effectiveAccess.mockResolvedValue({
      services: [{ connector_id: 'conn-9', name: 'Service A', preferred: true, tables: [] }],
      preferred_connector_id: 'conn-9',
      tables: [],
    })
    const { result } = renderHook(() => useDashboardConnector('dash-1', 'user-1'))
    await waitFor(() => expect(result.current.suggestion).toBe('conn-9'))
  })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `cd web && npx vitest run --project=default src/hooks/useDashboardConnector.test.tsx`
Expected: FAIL — module does not exist.

- [ ] **Step 3: Implement the hook**

```tsx
import { useCallback, useEffect, useState } from 'react'
import { effectiveAccess, listWarehouses } from '../api/warehouses'

function storageKey(userID: string, dashboardID: string) {
  return `aether_dash_connector:${userID}:${dashboardID}`
}

/**
 * Per-viewer dashboard connector selection (the notebook pin's successor):
 * localStorage per user+dashboard. The viewer's warehouse preference is the
 * initial suggestion when the org has exactly one warehouse and its
 * preference still names a granted service (design rule D7's unambiguity).
 */
export function useDashboardConnector(dashboardID: string, userID: string) {
  const key = storageKey(userID, dashboardID)
  const [selected, setSelected] = useState<string | null>(() => {
    try { return localStorage.getItem(key) } catch { return null }
  })
  const [suggestion, setSuggestion] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const warehouses = await listWarehouses()
        if (cancelled || warehouses.length !== 1) return
        const access = await effectiveAccess(warehouses[0].id)
        const preferred = access.preferred_connector_id
        const stillGranted = access.services.some((s) => s.connector_id === preferred)
        if (!cancelled && stillGranted) setSuggestion(preferred)
      } catch { /* warehouses unreadable — no suggestion */ }
    })()
    return () => { cancelled = true }
  }, [])

  const select = useCallback((connectorID: string | null) => {
    setSelected(connectorID)
    try {
      if (connectorID == null) localStorage.removeItem(key)
      else localStorage.setItem(key, connectorID)
    } catch { /* private mode */ }
  }, [key])

  return { selected, suggestion, select }
}
```

The response shapes come from the real API types (`WarehouseEffectiveAccess.services[].connector_id`, `.preferred_connector_id` — `web/src/api/warehouses.ts:75-83`). Update the Step 1 test's mock to match (`effectiveAccess` resolves `{ services: [{ connector_id: 'conn-9', name: 'X', preferred: true, tables: [] }], preferred_connector_id: 'conn-9', tables: [] }`).

- [ ] **Step 4: Wire into `useWidgetQuery` and `DashboardPage`**

`useWidgetQuery.ts` — accept and forward the viewer selection:

```ts
export function useWidgetQuery(opts: {
  dashboardId: string
  widget: Widget
  values: Record<string, unknown>
  enabled: boolean
  viewerConnectorId?: string | null
  endpointBase?: string
}) {
  // ...
  return api.post<WidgetQueryResult>(
    `${endpointBase}/execute`,
    {
      widget_id: opts.widget.id,
      connector_id: opts.viewerConnectorId ?? undefined,
      variables: opts.values,
      bypass_cache: bypass,
    },
    { signal },
  )
```

Also add `connector_id` to the `queryKey` so a selection change refetches: `queryKey: ['widget-query', endpointBase, opts.widget.id, opts.viewerConnectorId ?? '', canonicalValues(opts.values)]`.

`DashboardPage.tsx`: render the `ConnectorSelector` in the header (ClickHouse connectors only — filter `c.type === 'clickhouse'`), initialize from `useDashboardConnector(dashboardID, user.id)` with the suggestion shown as placeholder, and pass `viewerConnectorId={selected ?? undefined}` into each widget's `useWidgetQuery`. Extend `useWidgetQuery.test.tsx`:

```tsx
it('forwards the viewer connector id', async () => {
  // existing mock plumbing from this file's first test
  // assert the POST body contains connector_id: 'conn-2'
})
```

- [ ] **Step 5: Verify**

Run: `cd web && npx vitest run --project=default src/hooks && npm run build`
Expected: PASS + clean build.

- [ ] **Step 6: Commit**

```bash
git add web/src/hooks/useDashboardConnector.ts web/src/hooks/useDashboardConnector.test.tsx web/src/hooks/useWidgetQuery.ts web/src/hooks/useWidgetQuery.test.tsx web/src/pages/DashboardPage.tsx
git commit -m "feat(web): per-viewer dashboard connector selector with preference default"
```

---

### Task 10: Full gates and upstream PR

**Files:** docs touched only if swagger comments drifted.

- [ ] **Step 1: Whole-suite gates**

Run: `gofmt -l internal/ | wc -l && go vet ./... && go test ./internal/... -count=1 2>&1 | tail -3`
Expected: `0`, no vet findings, all packages `ok`.
Run: `rg -n "service_choice_required" internal/ web/src/ | wc -l`
Expected: only the UI's service-choice *preference picker* references if any remain (RoutingPreference.tsx is the preference UI, not the 409 flow) — the execute/widget 409 path must be gone.
Run: `cd web && npm run build && npx vitest run --project=default`
Expected: build clean, tests pass.

- [ ] **Step 2: PR**

```bash
git push -u origin feat/warehouse-routing-selection-wins
```

Open the PR against `The-Heaven-Labs/aether-notebooks:main` with: the design doc (`docs/plans/2026-10-07-warehouse-routing-selection-wins-design.md`) linked, the benchmark table quoted, and the breaking-change notes (409 removal; `pinned` deprecated; widget exec gains optional `connector_id`).

- [ ] **Step 3: Downstream record**

After the upstream release that ships this: in the iFood repo bump `resources/aether-notebooks/VERSION`, run `task sync-migrations` (no new migrations expected), and update this plan's doc + `CHANGELOG.md` with the shipped PR/release links.
