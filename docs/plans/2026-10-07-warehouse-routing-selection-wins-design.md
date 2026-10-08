# Design: Warehouse Routing — Selection Wins

- **Date:** 2026-10-07
- **Status:** Approved (design validated section-by-section)
- **Target:** upstream [The-Heaven-Labs/aether-notebooks](https://github.com/The-Heaven-Labs/aether-notebooks) (references against v0.64.0, commit `03c52e8`)
- **Deliverable:** design + implementation docs; the change lands upstream, then is consumed here via the usual `VERSION` bump.

## 1. Problem

A notebook cell shows **"ClickHouse Kong"** in its connector selector, but the run footer reports **"ran on CSIRT SIEM Exploration · CSIRT_SIEM"** — the query executed on a different service than the one displayed. This is the current *unpinned warehouse routing* behavior: the selected connector is only a warehouse hint; the user's per-warehouse routing preference (or the sole allowed service) silently serves the run. The user cannot tell where their query ran without reading the footer, and the selector lies about execution.

## 2. Current behavior (upstream v0.64.0)

- Warehouses group ClickHouse connectors sharing one access namespace (`migrations/V107__warehouses.sql`).
- `resolveExecutionTarget` (`internal/api/execution_target.go:35`) — unpinned runs: allowed services = warehouse connectors with `use` ACL for the user; the stored preference (`warehouse_service_preferences`, `V109`) wins; a sole allowed service is used; several without a preference → `409 service_choice_required`.
- `openQuery` (`internal/api/query_runner.go:49`) deliberately skips the `use` check on the *requested* connector for managed warehouse connectors — "access is enforced against the service that actually serves the run" (`query_runner.go:57-69`).
- `pinned=true` dials the selected connector directly, still requiring `use` on it (`internal/api/execute_handlers.go:20-22`).
- Callers: cells pass `req.Pinned` (`execute_handlers.go:114`); dashboard query widgets hardcode `pinned=false` (`internal/api/dashboard_query_handlers.go:745`); agents/MCP resolve with `pinned=false` (`internal/agent/execution_target.go:35`).
- The UI renders the `routing` response object as the footer chip (`internal/api/execute_handlers.go:302-308`, `web/src/components/Cell.tsx:887`).
- Notebook pin is per-user localStorage state (`web/src/pages/NotebookPage.tsx:216-233`); the selector disables connectors without `use` (`web/src/components/ConnectorSelector.tsx:61`).

## 3. Goals / Non-goals

**Goals**
- What you select is what executes: cell/notebook/dashboard selection is the execution target, everywhere.
- Warehouse preference stops being a routing override; it becomes a *default-seeding* mechanism only.
- Fail closed with an actionable error when the selected service is not usable by the acting user.
- One routing semantic for all surfaces: notebook cells, dashboards, agents/MCP, scheduled runs.

**Non-goals**
- Per-user connector state inside a shared notebook (collaborators keep shared state, industry-standard).
- Changing the warehouse/per-user ClickHouse identity model (`buildExecutionTarget` is untouched).
- Touching unmanaged connectors, the kill-switch legacy path, or the provisioner guard.

## 4. Industry benchmark

| Tool | Where the selection lives | Change visibility | Access gap behavior |
|---|---|---|---|
| Databricks | Notebook compute/SQL-warehouse attachment is shared notebook state; notebook `CAN RUN` implicitly grants compute access | Everyone | Fail-open via implicit permission inheritance |
| Snowflake | Worksheet context (role + warehouse) "is preserved for future sessions and is shared with all users of the same worksheet" | Everyone | Fail-closed ("run provided they have the worksheet role"); escape hatch: duplicate & run with own role |
| BigQuery | None (serverless); execution always as the viewer's own identity | N/A | Per-user IAM |
| Deepnote / Hex / Count | Connection definition shared at project level; execution via per-user credentials | Selection shared, identity per-user | Fail-closed on missing credentials |

Takeaways adopted here: artifact-level selection is the norm (Databricks, Snowflake); nobody silently re-routes to a different service than displayed; per-user dimension = execution identity + defaults for *new* artifacts only. This design follows the Snowflake pattern (shared selection, fail-closed) with aether's existing per-user ClickHouse identity, and goes one step further than Snowflake dashboards with a per-viewer dashboard selector.

## 5. Decisions

| # | Decision |
|---|---|
| D1 | **Cells/notebooks: selection wins.** The selected connector (cell connector, else notebook connector — existing frontend precedence) is the execution target. |
| D2 | **Dashboards: per-viewer connector selector** (UI state, per user+dashboard, like the notebook pin). Widget runs use the viewer's selection; the widget's saved connector is no longer the primary target. |
| D3 | **Cross-warehouse exception:** a viewer's dashboard selection only applies to widgets whose saved connector is in the *same warehouse*; widgets targeting another warehouse keep their saved connector (a selection can't redirect a widget at tables that don't exist on the selected service). |
| D4 | **Agents/MCP: the SQL tool's configured connector is the execution target.** `tools.config.connector_id` is already explicit and already `use`-checked unconditionally (`internal/agent/tools_sql.go:192`); notebook linkage is prompt context only (`internal/agent/engine.go:1905`). |
| D5 | **Fail closed with enriched 403** when the selected connector lacks `use` — no silent fallback, no re-routing. |
| D6 | **Notebook connector is shared state** (unchanged). If User 2 changes it, User 1 sees the change — same as Databricks/Snowflake. Escape hatch: per-cell connectors. |
| D7 | **Preference seeds new notebooks at creation** (persisted as `notebooks.connector_id`): seed only when the creator has exactly one live preference in the org; zero or several → leave unset. Existing notebooks are never re-seeded. |
| D8 | **Scheduled runs follow selection** (same server-side resolver; no separate mode). |
| D9 | **No rollout flag.** The semantic flip ships unconditionally. |

## 6. Detailed design

### 6.1 Backend routing (`resolveExecutionTarget`)

New rule order (`internal/api/execution_target.go`):

1. Load the requested connector with existing guards (non-deleted, ClickHouse, cross-org rejection) — unchanged.
2. Provisioner guard — unchanged, fails closed before everything.
3. Kill switch off → `ErrUnmanagedConnector` → legacy stored-credential path — unchanged.
4. Warehouse resolution from the requested connector — unchanged (needed for the per-user identity).
5. **The `use` ACL check moves from the served service to the requested connector.** No grant → `ErrServiceAccessDenied` with an enriched payload listing the user's allowed services in that warehouse.
6. Execute on the requested connector via `buildExecutionTarget` — unchanged (per-user warehouse identity; stored credential never used).

Consequences:
- `pinned` is accepted, ignored, documented deprecated (everything is pinned now).
- `409 service_choice_required` disappears from execution; `allowedWarehouseServices` survives for effective-access reporting and default seeding.
- Agents/MCP, widgets, and scheduled runs inherit the semantics through the shared resolver — no separate code paths.

### 6.2 Notebook frontend

- Existing notebooks: no change on open — the selector keeps showing the stored `connector_id`; cells with their own connector keep it. No per-user injection.
- The padlock pin toggle is removed (selection always wins); the frontend stops sending `pinned`. Backend keeps parsing it for older clients.
- New notebooks: creation request resolves seeding per D7 (server-side; explicit `connector_id` in the create request always wins).
- The "ran on" footer stays — now it confirms the selected service served the run.

### 6.3 Dashboard selector + widget execution

- Per-viewer `ConnectorSelector` on the dashboard header; state in `localStorage` keyed per user+dashboard; ClickHouse connectors only (non-ClickHouse widgets always run their saved connector).
- Default on open: viewer's warehouse preference — exactly one live preference → preselect; zero/several → empty until picked (same unambiguity rule as D7).
- Widget execution endpoint gains an optional viewer-selected `connector_id`. Server-side precedence:
  1. Viewer selection and widget's saved connector in the **same warehouse** → viewer's selection serves (with the global `use` check on it).
  2. Different warehouse → widget's saved connector wins (D3).
  3. No viewer selection → widget's saved connector.

### 6.4 Error handling & compatibility

- Enriched fail-closed 403: payload carries `warehouse_id` + allowed services (reusing the `ServiceChoiceError.Allowed` shape). Identical payload from cells, widgets, agents, scheduled runs.
- `409` removed from execution paths; preference CRUD and `/warehouses/{id}/effective-access` unchanged (they power defaults/selections, not routing).
- Rolling deploy is safe: old frontend + new backend yields new semantics immediately (intended); widgets without a viewer selection behave as saved-connector runs.
- `MaxRows`/`timeout` now come from the selected service (`buildExecutionTarget`) — document the shift; users selecting an idling service accept its wake-up latency.

### 6.5 Data model

**No migrations.** `warehouse_service_preferences` keeps its schema and meaning; only its consumers change. Existing notebooks keep their stored connector; seeding touches creation only.

## 7. Testing

**Go**
- `execution_target_test.go`: rewrite unpinned cases — selection-wins; enriched 403 (`use` missing, allowed services listed); cross-org rejection; provisioner guard; kill-switch unmanaged fallback; warehouse-not-ready.
- `execute_warehouse_test.go`: routing response fields (connector now = requested connector).
- Widget handlers: viewer-connector precedence, cross-warehouse override, no-selection fallback, non-ClickHouse passthrough.
- Agent `warehouse_identity_test.go`: configured connector served directly.
- Notebook creation: seeding with 0/1/N preferences; revoked/stale preference ignored; explicit connector wins.

**Web**
- `NotebookPage.test.tsx`: lock toggle removed; footer unchanged.
- Dashboard selector: preference preselect (1/N cases), per-viewer persistence, request carries viewer connector.
- `ConnectorSelector.test.tsx`: view-only options remain disabled.

## 8. Deliverables & process

1. Upstream PR (this design + implementation plan attached), following the repo's `UPSTREAM_FIX_*`/`UPSTREAM_FEATURE_*` precedent.
2. On upstream release: bump `resources/aether-notebooks/VERSION` ≥ release, `task sync-migrations` (no new migrations expected), changelog entry.
3. Downstream docs: update this record with the shipped PR/release; no local forks.

## 9. References

- Upstream v0.64.0: `internal/api/execution_target.go`, `internal/api/query_runner.go`, `internal/api/execute_handlers.go`, `internal/api/dashboard_query_handlers.go`, `internal/agent/execution_target.go`, `internal/agent/tools_sql.go`, `web/src/pages/NotebookPage.tsx`, `web/src/components/ConnectorSelector.tsx`, `web/src/components/RoutingPreference.tsx`, `migrations/V107__warehouses.sql`, `migrations/V109__warehouse_service_preferences.sql`.
- Benchmark: [Databricks notebook compute](https://docs.databricks.com/en/notebooks/notebook-compute.html), [Snowsight worksheets](https://docs.snowflake.com/en/user-guide/ui-snowsight-worksheets).
