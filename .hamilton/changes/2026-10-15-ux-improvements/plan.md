# Plan: UX improvements — warehouse provisioner test, dashboard fixes, cell fixes, search cell jump

## Overview

- Change: `.hamilton/changes/2026-10-15-ux-improvements/`
- Goal: Ship eleven user-reported fixes/features: a warehouse provisioner self-test (backend + UI), six dashboard App/editor fixes (AI panel on the bare app surface, chapter preservation between view and edit, fixed-width editor canvas, deterministic "All" tab ordering, a clearer View-published action, smaller text-panel minimum height), two notebook-cell fixes (clipboard copy from table outputs while a cell editor keeps focus, and schema-driven SQL table/column autocomplete), and search-result deep links that focus the matched notebook cell.
- Branch: `feat/ux-improvements`, based on `origin/main` (includes PR #266 search/favorites). One PR for the whole batch; browser validation happens after the whole-branch review.
- Test: Go — `bash scripts/go-test.sh ./... -count=1` (the `task test` runner; resets the disposable per-worktree DB). Web — `cd web && npm run test:run` (Vitest; `task test:web`). Full-suite scope per task: Go tasks run the Go suite, web tasks run the web suite; every task also runs its focused Verify command and the typecheck/build.
- Build / typecheck: Go — `task build`. Web — `cd web && npx tsc --noEmit && npm run build`.
- Context notes: React + React Query frontend under `web/`; Go API under `internal/`. Dashboard App/editor code lives in `web/src/pages/DashboardPage.tsx` (view surface, published/draft resolution, `?tab=` state), `web/src/pages/DashboardEditorPage.tsx` (editor, local `editorTab` state, react-grid-layout), `web/src/utils/widgetLayout.ts` (min heights), `web/src/utils/dashboardTabs.ts` (tab helpers), `web/src/components/AppShell.tsx` (shell, `bare` mode for the App surface, global agent FAB). Cell code lives in `web/src/components/Cell.tsx` (CodeMirror 6 + Yjs editor) and `web/src/components/OutputRenderer.tsx` (virtualized result table with custom TSV selection and document-level copy handling). Search lives in `internal/search/` + `internal/api/search_handlers.go` and `web/src/pages/SearchPage.tsx` + `web/src/components/SearchPalette.tsx`. Warehouse reconciliation lives in `internal/api/warehouse_sync.go` and `internal/chaccess/`. AGENTS.md notes: `task test:e2e` needs a native API + relay + Vite stack on :5173 (docker-API runs fail specs whose connectors hardcode `localhost`) — e2e spec updates in this plan are validated during final browser validation, not by task runners.
- Quality notes: every task lands one cohesive fix with a testable seam. Pure helpers (`sqlCompletion`, `appLayout`, `dashboardTabs` ordering/param helpers, `widgetLayout` min rows) carry unit tests; component wiring is covered by the existing component suites and the end-of-branch real-browser validation (mandatory per AGENTS.md for UI changes). Deliberate scope decisions recorded in tasks: autocomplete is notebook-cell SQL cells only (the dashboard widget drawer keeps its current editor); the App surface gains the agent FAB/panel only — the search palette stays off the bare surface; the SQL autocomplete is a fresh implementation (the historical feature was lost before merge — see Task 4 notes).

## Global constraints

- Never bypass or loosen auth/ACL checks; new endpoints follow the existing middleware pattern (`RequireRole("admin")` for warehouses) and return HTTP 200 with `{ok:false, error}` for business failures, mirroring `handleTestConnector`.
- The warehouse provisioner test must not disturb managed state: it runs under the existing per-warehouse sync lock, creates only identities inside the `chaccess.IdentifierPrefix(warehouseID)` namespace (suffix `test_<hex>`), and removes them on every path, verifying removal.
- Yjs remains the single source of truth for cell content: no code may make programmatic editor updates persist as user autosaves (reuse the existing `applyingYjsRef` mechanism).
- Dashboard chapter state is the `?tab=` search param, the same name the view page already uses; an invalid or missing id resolves to the first tab (view) / All (editor), matching existing helper behavior.
- Text panels keep `TEXT_PANEL_MIN_H` semantics (source-less `text` widgets); data-backed widgets keep `DATA_WIDGET_MIN_H = 6`. The new value is 2 rows.
- Web styling uses CSS variables (`var(--accent)`, `var(--text-primary)`); no hardcoded colors.
- Each task's commit includes its `progress.md` entry; nothing under `.hamilton/changes/2026-10-15-ux-improvements/` is left uncommitted.

## Tasks

### Task 1: Add the warehouse provisioner test endpoint

- Depends on: none
- Files:
  - Created: `internal/api/warehouse_test_handler.go`, `internal/api/warehouse_test_handler_test.go`
  - Modified: `internal/api/router.go`
- Acceptance:
  - `POST /api/v1/warehouses/{id}/test` (org-admin gated) returns HTTP 200 `{"ok":true,"steps":[...]}` after connecting with the stored provisioner credential, creating a throwaway ClickHouse user, granting the mode's read bundle on up to 3 catalog tables, verifying the grants are visible, dropping the user, and verifying no identity or grant remains.
  - Response shape: `{"ok":bool,"error":string?,"steps":[{"name":string,"ok":bool,"error":string?}]}` with step names `connect`, `create user`, `grant access`, `verify grants`, `remove test user`. Business failures (no provisioner, connection error, DDL error, leftover identity) return 200 with `ok:false` and an error message; unknown/foreign warehouse ids return 404.
  - The test identity is named `chaccess.IdentifierPrefix(warehouseID) + "test_" + <random hex>`, is dropped on every path (including mid-flight failures, using `context.WithoutCancel` for the cleanup context), and no other identity or grant is created, modified, or dropped.
  - A reconcile running concurrently cannot interleave with the test (the test runs inside `withWarehouseSyncLock`); a busy lock returns a clear `ok:false` result rather than an error page.
  - Audit entry `warehouse.provisioner_test` records org, warehouse, admin user, and `{ok, error}`.
- Steps:
  1. Write failing tests in `internal/api/warehouse_test_handler_test.go`, following the request-building pattern of `warehouse_handlers_test.go` and the fixtures in `warehouse_test_fixtures_test.go` (`newWarehouseSyncTestServer`, `setupWarehouseFixture`, `requireClickHouseReachable`). Cover: (a) success — response `ok:true`, every step ok, and afterwards `SELECT count() FROM system.users WHERE name LIKE '<prefix>test_%'` is 0 and no `system.grants` rows remain for the generated name; (b) no provisioner configured; (c) unknown warehouse id → 404. Use admin claims so the handler-level test exercises the logic (route middleware is covered by the router itself).
  2. Implement `handleTestWarehouseProvisioner` in `internal/api/warehouse_test_handler.go`: resolve the warehouse for the caller's org (mirror `handleGetWarehouse`'s lookup and its 404 semantics), load the provisioner config with `loadProvisionerConfig`, then run everything inside `withWarehouseSyncLock`. Bound the whole operation with a ~90s timeout derived from the request context.
  3. Inside the lock: `openWarehouseProvisionerConn` → `chaccess.DetectPrivilegeMode` → `chaccess.LoadCatalogTables` + `chaccess.SanitizeCatalogTables` → pick up to 3 quotable tables ordered by `(database, table)`. If no quotable tables exist, report the `grant access` and `verify grants` steps as `ok:true` with no error (nothing to exercise) and continue with the user create/remove path — the test must not fail on an empty catalog. Build the identity with `chaccess.IdentifierPrefix` + `test_` + a random hex suffix; generate a `[0-9a-f]`-only random password (safe to inline in single quotes); reference quote rules used by `internal/chaccess/ddl.go` (`QuoteIdent` for identities, `QuoteObjectIdent` for `db.table`; skip unquotable objects). Statements: `CREATE USER IF NOT EXISTS <ident> IDENTIFIED WITH sha256_password BY '<pw>' GRANTEES NONE`, one `GRANT <mode.Bundle() joined> ON <db>.<tbl> TO <ident>` per object, `DROP USER IF EXISTS <ident>`.
  4. Verify with parameterized queries: `SELECT count() FROM system.users WHERE name = ?` and, per granted object, `SELECT count() FROM system.grants WHERE user_name = ? AND database = ? AND table = ?` (one row per granted privilege; count > 0 proves the grant is visible). After the drop, re-check that both the user and its grants are gone.
  5. Register the route in `internal/api/router.go` next to the other warehouse routes: `POST /api/v1/warehouses/{id}/test` with `authMW(RequireRole("admin")(...))`, and add the swagger annotation block above the handler (project convention).
  6. Audit with `s.audit.Log` (`warehouse.provisioner_test`), following the field pattern of the nearby warehouse audit entries.
  7. Run the focused test, then the full Go suite, then `task build`.
- Verify: `bash scripts/go-test.sh -run 'TestWarehouseProvisionerTest' ./internal/api/` → all tests pass with output pristine; `bash scripts/go-test.sh ./... -count=1` → full suite green.
- Commit: `feat(warehouse): add provisioner self-test endpoint`

### Task 2: Add the warehouse provisioner test button

- Depends on: Task 1
- Files:
  - Created: `web/src/pages/WarehouseSettingsPage.test.tsx`
  - Modified: `web/src/api/warehouses.ts`, `web/src/pages/WarehouseSettingsPage.tsx`
- Acceptance:
  - The expanded warehouse card shows a "Test provisioner" button next to (or directly under) the provisioner connector select; it is disabled when no provisioner is configured.
  - Clicking it calls `POST /api/v1/warehouses/{id}/test`, shows a spinner while running, then renders `Provisioner test passed` on `ok:true`, or the response `error` (with the first failed step's name when present) in the card's existing error color on `ok:false`/network failure.
  - The previous result is cleared when the selected provisioner changes.
- Steps:
  1. In `web/src/api/warehouses.ts`, add `WarehouseProvisionerTestStep`/`WarehouseProvisionerTestResult` types and `testWarehouseProvisioner(id: string): Promise<WarehouseProvisionerTestResult>` calling `POST /api/v1/warehouses/${id}/test`.
  2. In `WarehouseSettingsPage.tsx`, add the button and result rendering to `WarehouseCard`, following the card's existing inline styles and the page's `useMutation` usage (no query invalidation — the test changes no warehouse row). Use a lucide `Zap`/`Loader2` icon consistent with the page's imports.
  3. Write `web/src/pages/WarehouseSettingsPage.test.tsx` with the MSW setup pattern from `web/src/test/ConnectorsPage.test.tsx`: mock `GET /api/v1/warehouses` (one warehouse with `provisioner_connector_id`), the config/auth endpoints the page needs, and `POST /api/v1/warehouses/:id/test`. Assert success rendering, failure rendering (mock `{ok:false,error:"boom",steps:[{name:"connect",ok:false,error:"dial failed"}]}` → the message is shown), and that the button is disabled for a warehouse without a provisioner.
  4. Run the focused vitest file, then the full web suite, then `npx tsc --noEmit`.
- Verify: `cd web && npx vitest run src/pages/WarehouseSettingsPage.test.tsx` → all pass; `cd web && npm run test:run` → green.
- Commit: `feat(web): add warehouse provisioner test button`

### Task 3: Copy table selections while a cell editor keeps focus

- Depends on: none
- Files:
  - Modified: `web/src/components/OutputRenderer.tsx`, `web/src/test/OutputRenderer.test.tsx`
- Acceptance:
  - With the caret inside a CodeMirror cell editor, a mousedown on a result-table data cell moves DOM focus out of `.cm-editor` (to the document body when nothing else is focusable), so Ctrl/Cmd+C then copies the TSV table selection (or the open detail value) instead of the editor's current line.
  - The behavior comes from a `blur()` on the active `.cm-editor` element inside `beginCellSelection` — the same idiom used in `NotebookPage.tsx`'s keyboard shortcuts — leaving the existing `copy`/`keydown` guards (`target.closest('.cm-editor')`) unchanged so real editor copies are never hijacked.
  - Drag selection, shift-click anchor selection, and the detail panel keep working.
- Steps:
  1. Extend `web/src/test/OutputRenderer.test.tsx`: add a test that appends `<div class="cm-editor"><div tabindex="0"></div></div>` to `document.body`, focuses the inner element, fires `mousedown` on a data cell, and asserts `document.activeElement` no longer sits inside `.cm-editor`. Add a second test that then dispatches the document `copy` event (the file's existing dispatch helper) and asserts the clipboard receives the TSV selection — this is the regression the fix addresses.
  2. In `beginCellSelection` (`OutputRenderer.tsx`), before `e.preventDefault()`, blur the active element when it is inside `.cm-editor`:
     ```ts
     const active = document.activeElement as HTMLElement | null
     if (active?.closest('.cm-editor')) active.blur()
     ```
  3. Run the focused test file, then the full web suite, then `npx tsc --noEmit`.
- Verify: `cd web && npx vitest run src/test/OutputRenderer.test.tsx` → all pass (new test fails before the fix).
- Commit: `fix(web): blur the cell editor when a result-table selection starts`

### Task 4: Schema-driven table/column autocomplete in notebook SQL cells

- Depends on: none
- Notes: This feature existed in the past but its code was lost before merge (only the PR description survived — `d8957e15` describes "SQL autocomplete with table/column completion from schema" and "autocomplete popup theme styling", but neither the commit nor its original PR branch contains any of it; the same loss applies to that block's other frontend bullets like bracket matching and Ctrl+F search). This task implements it fresh with CodeMirror's standard popup.
- Files:
  - Created: `web/src/utils/sqlCompletion.ts`, `web/src/utils/sqlCompletion.test.ts`
  - Modified: `web/package.json`, `web/package-lock.json`, `web/src/components/Cell.tsx`, `web/src/styles/theme.css`
- Acceptance:
  - Typing in a notebook SQL cell shows CodeMirror completion suggestions: keywords always, and tables/columns from the cell's connector schema once loaded (`SELECT * FROM ` → table names; `table.` → that table's columns). Ctrl+Space opens the popup manually; Enter/Tab accept; Escape closes. Cells whose connector resolves to no schema keep keyword completion and never error.
  - The schema is fetched through the shared React Query cache (`connectorSchemaQueryKey`) at most once per connector per session (`staleTime` ~5 minutes), reused across cells, and only when the cell is a SQL cell with a connector.
  - JavaScript cells are untouched; Yjs sync and autosave behavior are unchanged (completions are ordinary editor transactions).
  - The popup is legible in both themes (background/border/selected-option colors from CSS variables).
- Steps:
  1. `cd web && npm install @codemirror/autocomplete@^6.20.3` (already present transitively — this makes it a direct dependency and updates the lockfile).
  2. Add `web/src/utils/sqlCompletion.ts` with `buildSqlCompletionSchema(catalog: ConnectorSchema): Record<string, readonly string[]>`: map each `tables[]` entry to `name → columns.map(c => c.name)`, and additionally add `schema.name → columns` when `table.schema` is non-empty. Skip nothing; empty column lists are fine. Unit-test it in `web/src/utils/sqlCompletion.test.ts` (bare names, qualified names, duplicate names across schemas, empty catalog).
  3. Rework `Cell.tsx`: give `languageExtension(cell, connector, schemaData?)` an optional schema map and return `[sql({ dialect, schema: schemaData }), autocompletion()]` (JS still returns `[]`); add `...completionKeymap` to the cell keymap; hoist the effect-local `langCompartment` to a `useRef<Compartment>` so both the initial mount and the JS lazy import and the new schema effect reconfigure the same compartment.
  4. Add the schema query inside `CodeEditorView`: `useQuery({ queryKey: connectorSchemaQueryKey(connector?.id ?? ''), queryFn: () => getConnectorSchema(connector!.id), enabled: !!connector && cell.language !== 'javascript', staleTime: 5 * 60_000 })`, and a `useEffect` that dispatches `languageCompartment.current.reconfigure(languageExtension(cell, connector, schemaData))` when the schema data, connector id, or language changes (skip JS and skip when the editor view is not mounted). Swallow query errors (no suggestions, no UI change).
  5. Add popup styling in `web/src/styles/theme.css` scoped to `.cm-tooltip-autocomplete` / `.cm-tooltip` (background `var(--bg-primary)`/`var(--bg-secondary)`, border `var(--border)`, text `var(--text-primary)`, selected `var(--accent)` family, monospace font consistent with the editor). Verify in the browser during final validation.
  6. Add a Cell-level regression test only if it can assert real behavior cheaply (e.g. the editor still mounts and typing still works with the query mocked); otherwise the acceptance is the helper test plus final browser validation — do not write a test that asserts implementation trivia.
  7. Run the focused tests, the full web suite, `npx tsc --noEmit`, and `npm run build`.
- Verify: `cd web && npx vitest run src/utils/sqlCompletion.test.ts` → all pass; `cd web && npx tsc --noEmit` → clean.
- Commit: `feat(web): schema-driven SQL autocomplete in notebook cells`

### Task 5: Keep the agent panel reachable on the App surface

- Depends on: none
- Files:
  - Modified: `web/src/components/AppShell.tsx`, `web/src/test/AppShell.test.tsx`
- Acceptance:
  - On `bare` pages (`/dashboards/:id/view`) the AI FAB renders bottom-right exactly as on other pages; opening it shows the global AgentPanel over the app content, and minimize/close restore the FAB.
  - Ctrl/Cmd+J toggles the agent panel on bare pages (the shortcut's `bare` guard is removed because the panel now exists there); the `?` shortcuts modal and Ctrl/Cmd+K search palette remain unavailable on the bare surface (documented in the component comment).
  - Non-bare behavior, the `/chats/:id` exception, and docking/width persistence are unchanged, and every shell hook still runs before the bare branch (rules of hooks).
- Steps:
  1. Extend `web/src/test/AppShell.test.tsx`: render `<AppShell bare>` inside the file's existing router/provider wrapper and assert the `#main-content` landmark plus the FAB button (`title="Open AI Agent (Ctrl+J)"`) is present. Keep the existing non-bare assertions working.
  2. Refactor `AppShell.tsx`: keep all hooks above the branch, extract the FAB + floating panel + minimized bar markup into a single local `agentOverlay` node, return `<><main id="main-content">{children}</main>{agentOverlay}</>` for `bare` and keep the current full markup otherwise (reuse `agentOverlay` there to avoid duplication).
  3. Update the two shortcut effects: the agent toggle loses its `bare` guard (`[isChatPage, bare]` → `[isChatPage]`); the search palette and `?` shortcuts modals keep skipping bare, and the component doc comment reflects the exact remaining exclusions.
  4. Run the focused test, the full web suite, `npx tsc --noEmit`.
- Verify: `cd web && npx vitest run src/test/AppShell.test.tsx` → all pass.
- Commit: `fix(web): keep the agent panel reachable on the App surface`

### Task 6: Preserve the active chapter between dashboard view and edit

- Depends on: none
- Files:
  - Modified: `web/src/utils/dashboardTabs.ts`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/DashboardEditorPage.tsx`
  - Created/extended: unit tests for the new helper (extend `web/src/utils/dashboardTabs.test.ts` if present, otherwise create it)
- Acceptance:
  - Clicking Edit on `/dashboards/:id/view?tab=foo` opens `/dashboards/:id?tab=foo` and the editor starts on chapter `foo`; with no tab param (or All) it starts on All as today.
  - In the editor, switching chapters updates `?tab=` via `replace` (All removes the param); the View link carries the currently active `?tab=` into `/dashboards/:id/view`.
  - Invalid ids and dashboards without tabs fall back to the existing defaults (view: first tab; editor: All) and do not leave a stale `?tab=` in the URL.
  - The view page's existing behavior (tab changes update `?tab=` with replace; Preview draft / View published links preserve params) is unchanged.
- Steps:
  1. Add a pure helper to `web/src/utils/dashboardTabs.ts`, e.g. `tabQueryString(tabId: string | null | undefined): string` returning `''` or `?tab=<encoded id>`, and unit-test it (id, null, empty string).
  2. `DashboardPage.tsx`: the Edit link (currently `<Link to={/dashboards/${id}}>`) appends `tabQueryString(activeTab)`.
  3. `DashboardEditorPage.tsx`: initialize the `editorTab` state from `useSearchParams().get('tab')`; on chip change update both the state and the URL (`setSearchParams` with `{replace: true}`, removing the param for All); make sure the existing "invalid id clears state" effect also clears the URL param; the View link appends `tabQueryString(activeEditorTab)`.
  4. Grep `e2e/` for flows asserting the edit link or editor URL and update only if a selector/URL assertion is invalidated (the existing `?tab=tab-2` assertion in `dashboard-publish.spec.ts` covers view mode and should still hold).
  5. Run the focused unit test, the full web suite, `npx tsc --noEmit`.
- Verify: `cd web && npx vitest run src/utils/dashboardTabs.test.ts` → all pass.
- Commit: `feat(web): preserve the active chapter between dashboard view and edit`

### Task 7: Honor the fixed app width in the dashboard editor

- Depends on: none
- Files:
  - Created: `web/src/utils/appLayout.ts`, `web/src/utils/appLayout.test.ts`
  - Modified: `web/src/components/AppHeader.tsx`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/DashboardEditorPage.tsx`
- Acceptance:
  - With `settings.app.width` `fixed` (the default), the editor's widget canvas is constrained to the same 1200px maximum width, centered, as the view surface; with `full`, it spans the available width as today.
  - The grid measures the constrained width (the ResizeObserver sees the wrapper, so drag/resize coordinates stay correct), and the stacked mobile fallback is constrained identically.
  - `APP_CONTENT_MAX_WIDTH` has a single source of truth, and view/public/editor all use the shared style helper.
- Steps:
  1. Move `APP_CONTENT_MAX_WIDTH = 1200` from `AppHeader.tsx` into a new `web/src/utils/appLayout.ts` and export both the constant and `appContentWidthStyle(width: 'fixed' | 'full' | undefined): React.CSSProperties | undefined` (fixed/undefined → `{ maxWidth: APP_CONTENT_MAX_WIDTH, margin: '0 auto' }`, full → `undefined`). Unit-test the helper in `web/src/utils/appLayout.test.ts`.
  2. Update `AppHeader.tsx` and `DashboardPage.tsx` to import the shared constant/helper instead of their local definitions (behavior identical).
  3. `DashboardEditorPage.tsx`: resolve the app width from the same settings source the grid already uses (`liveDashboard.settings ?? dashboard?.settings`), and merge `appContentWidthStyle(appWidth)` into the widget-canvas wrapper that `layoutRef` measures so both the grid and the stacked fallback are constrained. Do not constrain the editor toolbar.
  4. Run the focused test, the full web suite, `npx tsc --noEmit`.
- Verify: `cd web && npx vitest run src/utils/appLayout.test.ts` → all pass.
- Commit: `feat(web): honor fixed app width in the dashboard editor`

### Task 8: Order editor widgets by chapter, then grid position

- Depends on: none
- Files:
  - Modified: `web/src/utils/dashboardTabs.ts`, `web/src/pages/DashboardEditorPage.tsx`
  - Created/extended: unit tests as in Task 6
- Acceptance:
  - In the editor's All view, widgets render in a deterministic order: chapter order first (widgets with no/invalid `tab` fall into the first chapter's bucket, matching `widgetTabId`'s fallback), then `(layout.row, layout.col)`, with a stable tiebreak so identical coordinates keep a stable order across renders and doc syncs.
  - The ordering is applied to `shownWidgets` (not the stored document), so it only affects rendering/compaction; per-chapter views keep working and the view surface is unchanged.
  - A unit test covers: tab buckets, untabbed widgets, row/col ordering inside a bucket, stability for equal positions, and the no-tabs case.
- Steps:
  1. Add `orderWidgetsForEditor(widgets: Widget[], tabs: DashboardTab[]): Widget[]` to `web/src/utils/dashboardTabs.ts`: non-mutating stable sort by `(tabIndex, layout.row, layout.col)` where `tabIndex = tabs.findIndex(t => t.id === widgetTabId(widget, tabs))` (negative/absent → first bucket via the existing fallback; no tabs → single bucket).
  2. Use it in `DashboardEditorPage.tsx` where `shownWidgets` is computed, wrapped in the existing `useMemo`.
  3. Extend `web/src/utils/dashboardTabs.test.ts` with the cases listed in Acceptance.
  4. Run the focused test, the full web suite, `npx tsc --noEmit`.
- Verify: `cd web && npx vitest run src/utils/dashboardTabs.test.ts` → all pass.
- Commit: `fix(web): order editor widgets by chapter and grid position`

### Task 9: Make the editor's View action say what it shows

- Depends on: Task 6
- Files:
  - Modified: `web/src/pages/DashboardEditorPage.tsx`
- Acceptance:
  - When the dashboard has a published version, the editor header action reads `View published` with a title explaining it opens the published snapshot and that unpublished edits are visible via `Preview draft` on that page. When nothing has been published, it reads `View app` with a title saying the current draft is shown because no version exists yet.
  - The link still navigates to `/dashboards/{id}/view` carrying the active `?tab=` (Task 6).
- Steps:
  1. Read the editor's existing publish-status state (the `v{n}` / `Not published` chip) to derive `hasPublishedVersion`.
  2. Update the View link: label and title per the Acceptance; keep the `Eye` icon and `nav-btn` styling.
  3. Grep `e2e/` for selectors matching the editor's View link and update them if the rename invalidates any (`dashboard-publish.spec.ts`'s `/View published/` assertions target the view page's own link and should not need changes — confirm).
  4. Run the full web suite and `npx tsc --noEmit`.
- Verify: `cd web && npm run test:run` → green; `cd web && npx tsc --noEmit` → clean.
- Commit: `feat(web): label the dashboard editor view action as published`

### Task 10: Smaller minimum height for text panels

- Depends on: none
- Files:
  - Created: `web/src/utils/widgetLayout.test.ts`
  - Modified: `web/src/utils/widgetLayout.ts`, `web/src/pages/PublicDashboardPage.tsx`
- Acceptance:
  - Source-less text panels can be resized down to 2 grid rows (`TEXT_PANEL_MIN_H = 2`), while data-backed widgets keep their 6-row minimum; the first-render height clamp uses the same per-type minimum.
  - `toGridItem` uses a shared min-rows helper, and `PublicDashboardPage`'s hardcoded 6-row minimums (chapter height and widget rows) use the same helper so the published surface matches the editor/view.
  - Unit tests cover: text panel → 2, cell/query-backed widget → 6, `toGridItem` clamping `h` up to the minimum and keeping `maxH: 24`.
- Steps:
  1. In `web/src/utils/widgetLayout.ts`, set `TEXT_PANEL_MIN_H = 2` and add `export function widgetMinRows(w: Widget): number` returning `isTextPanel(w) ? TEXT_PANEL_MIN_H : DATA_WIDGET_MIN_H`; use it in `toGridItem`.
  2. Write `web/src/utils/widgetLayout.test.ts` for the cases above.
  3. In `PublicDashboardPage.tsx`, replace the hardcoded `6` row minimums (chapter height calculation and per-widget rows) with `widgetMinRows(widget)`.
  4. Run the focused test, the full web suite, `npx tsc --noEmit`.
- Verify: `cd web && npx vitest run src/utils/widgetLayout.test.ts` → all pass.
- Commit: `feat(web): smaller minimum height for text panels`

### Task 11: Jump to the matched cell from search results

- Depends on: none
- Files:
  - Modified: `internal/search/search.go`, `internal/search/pg.go`, `internal/api/resource_summaries.go`, `internal/api/search_handlers.go`, `internal/api/search_handlers_test.go` (extend), `web/src/types/index.ts`, `web/src/utils/resources.ts`, `web/src/pages/SearchPage.tsx`, `web/src/components/SearchPalette.tsx`, `web/src/test/SearchPage.test.tsx` (extend), `e2e/search.spec.ts`
  - Created: `web/src/utils/resources.test.ts` (or extend an existing utils test file)
- Acceptance:
  - A notebook content hit carries the id of the matched cell end-to-end: the Postgres FTS query already selects `cell_id`; `search.ContentHit` exposes it, and `resourceSummary` includes `"cell_id"` (omitted when absent, e.g. favorites and dashboard hits).
  - Clicking a notebook content result — in both the results page and the Ctrl/Cmd+K palette — navigates to `/notebooks/{id}#cell-{cell_id}`; the notebook page's existing hash handler scrolls to that cell and flashes it. Favorites, title-only hits, dashboards, and every other type keep their current hrefs (no hash).
  - Dashboard content hits still expose no cell/widget id.
- Steps:
  1. Go tests first: extend `internal/api/search_handlers_test.go` to assert that a content hit's summary carries the matching cell's id (compare against the seeded cell) and that a title-only hit has no `cell_id`.
  2. `internal/search/search.go`: add `CellID string` to `ContentHit` with a doc comment (notebook hits only; empty for dashboard widgets). `internal/search/pg.go`: select `p.cell_id::text` in `notebookContentSQL` and scan it; leave `dashboardContentSQL` without it.
  3. `internal/api/search_handlers.go`: thread the id the same way snippets travel — add the map to `searchTypePage` (e.g. `CellIDs map[string]string`), populate it where `contentSnippets` is populated (after the content gate), pass it through the summary options, and set it on each `resourceSummary`. `internal/api/resource_summaries.go`: add `CellID *string \`json:"cell_id,omitempty"\``.
  4. Web: add `cell_id?: string` to `ResourceSummary`; extend `resourceHref(type, id, cellId?)` so a notebook with a cell id returns `/notebooks/${id}#cell-${cellId}`; update `SearchPage.tsx` and `SearchPalette.tsx` call sites to pass `result.cell_id`.
  5. Web tests: unit-test `resourceHref` (notebook with/without cell id, other types unaffected) and extend `web/src/test/SearchPage.test.tsx` to assert the rendered link's href contains the hash for a notebook content hit.
  6. Update `e2e/search.spec.ts`'s content-hit navigation expectation to the hash URL (keep the assertion order-independent of the exact cell id by matching `/notebooks/${notebookId}#cell-`).
  7. Run the focused Go test, the full Go suite, `task build`; then the focused web test, the full web suite, `npx tsc --noEmit`.
- Verify: `bash scripts/go-test.sh -run 'TestSearch' ./internal/api/` → green; `cd web && npx vitest run src/test/SearchPage.test.tsx` → green.
- Commit: `feat(search): jump to the matched cell from search results`

## Done when

- All eleven tasks are implemented, each with its own commit and a `progress.md` entry reading `Outcome: done`.
- Every task passed a hamilton-review pass; the whole-branch review is clean.
- Go suite, web suite, builds, and typecheck green, and the changed flows validated in a real browser against the dev stack.
- The branch is committed and ready for `hamilton-finish-work` (merge / PR decision left to the user).
