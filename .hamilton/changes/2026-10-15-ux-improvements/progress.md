# Progress: UX improvements — warehouse provisioner test, dashboard fixes, cell fixes, search cell jump

<!-- Newest entries at the bottom. One block per task attempt. -->

## Task 1: Add the warehouse provisioner test endpoint — 2026-10-15

- Outcome: done
- Changed:
  - Created: `internal/api/warehouse_test_handler.go`, `internal/api/warehouse_test_handler_test.go`
  - Modified: `internal/api/router.go`
  - Deleted: none
- Verified:
  - `bash scripts/go-test.sh -run 'TestWarehouseProvisionerTest' ./internal/api/ -count=1` → pass
  - `bash scripts/go-test.sh ./... -count=1` → pass
  - `task build` → pass
- Notes: Review pass changes — cleanup now drops and verifies removal on every path using a detached context and reports failures in the `remove test user` step and overall error; audit write uses a detached context; added cross-org 404 test.

## Task 2: Add the warehouse provisioner test button — 2026-10-15

- Outcome: done
- Changed:
  - Created: `web/src/pages/WarehouseSettingsPage.test.tsx`
  - Modified: `web/src/api/warehouses.ts`, `web/src/pages/WarehouseSettingsPage.tsx`
  - Deleted: none
- Verified:
  - `cd web && npx vitest run src/pages/WarehouseSettingsPage.test.tsx` → pass
  - `cd web && npm run test:run` → pass (1485 passed, 2 skipped)
  - `cd web && npx tsc --noEmit` → clean
- Notes: Added `testWarehouseProvisioner` API helper, a `Test provisioner` button under the provisioner select, and per-card result state that is cleared on provisioner change. Existing `WarehouseSettingsPage.test.tsx` tests were preserved and extended with a new `provisioner test button` describe block.
- Review pass (changes requested): fixed error formatting so the first failed step name is always shown when present; added a stale-guard that tags each test run with the provisioner id it started against and drops results whose tag no longer matches; added regression tests for both findings.

## Task 3: Copy table selections while a cell editor keeps focus — 2026-10-15

- Outcome: done
- Changed:
  - Created: none
  - Modified: `web/src/components/OutputRenderer.tsx`, `web/src/test/OutputRenderer.test.tsx`
  - Deleted: none
- Verified:
  - `cd web && npx vitest run src/test/OutputRenderer.test.tsx` → pass
  - `cd web && npm run test:run` → pass (1487 passed, 2 skipped)
  - `cd web && npx tsc --noEmit` → clean
- Notes: Added a `cm-editor` focus blur at the start of `beginCellSelection`; the existing copy/keydown guards remain intact so real editor copies are still protected. New tests verify that mousedown on a data cell blurs the active editor and that the subsequent document copy event writes the TSV selection.

## Task 4: Schema-driven table/column autocomplete in notebook SQL cells — 2026-10-15

- Outcome: done
- Changed:
  - Created: `web/src/utils/sqlCompletion.ts`, `web/src/utils/sqlCompletion.test.ts`
  - Modified: `web/package.json`, `web/package-lock.json`, `web/src/components/Cell.tsx`, `web/src/styles/theme.css`, `web/src/components/Cell.test.tsx`
  - Deleted: none
- Verified:
  - `cd web && npx vitest run src/utils/sqlCompletion.test.ts` → pass
  - `cd web && npx vitest run src/components/Cell.test.tsx` → pass
  - `cd web && npm run test:run` → pass (1491 passed, 2 skipped)
  - `cd web && npx tsc --noEmit` → clean
  - `cd web && npm run build` → pass
- Notes: Made `@codemirror/autocomplete` a direct dependency and wired it into `Cell.tsx`: `languageExtension` now returns `sql({ schema }) + autocompletion()` for SQL cells and accepts a schema map built by `buildSqlCompletionSchema`; the mount effect uses a `useRef<Compartment>` for the language compartment so initial setup, lazy JS load, and schema-data effects all reconfigure the same compartment. Schema is fetched via the shared `connectorSchemaQueryKey` React Query cache (`staleTime: 5m`) only for SQL cells with a connector; errors are swallowed. Added CSS variable-based autocomplete popup theming in `theme.css`. Updated `Cell.test.tsx` to wrap renders in `renderWithProviders` so the new `useQuery` hook has a `QueryClient`. Also fixed a pre-existing `HttpResponse` generic type error in `web/src/pages/WarehouseSettingsPage.test.tsx` that blocked `npm run build`.
- Review pass (changes requested): (1) Updated the custom `Tab` keymap handler to call `acceptCompletion(view)` first so Tab accepts an open autocomplete suggestion before falling back to inserting indentation. (2) Replaced the temporary `Record<string, any>` type in the Task 2 test-file fix with a concrete response-body type `{ ok: boolean; steps: { name: string; ok: boolean }[] }`.

## Task 5: Keep the agent panel reachable on the App surface — 2026-10-15

- Outcome: done
- Changed:
  - Created: none
  - Modified: `web/src/components/AppShell.tsx`, `web/src/test/AppShell.test.tsx`
  - Deleted: none
- Verified:
  - `cd web && npx vitest run src/test/AppShell.test.tsx` → pass
  - `cd web && npm run test:run` → pass (1495 passed, 2 skipped)
  - `cd web && npx tsc --noEmit` → clean
- Notes: Extracted the global agent FAB + floating panel + minimized bar into a shared `agentOverlay` node in `AppShell.tsx`. The bare branch now renders `<main id="main-content">{children}</main>{agentOverlay}`, while non-bare keeps the full shell and reuses the same overlay. Removed the `bare` guard from the Ctrl/Cmd+J agent toggle effect; the search palette and `?` shortcuts modal remain gated on `bare`. Updated the `bare` prop doc comment to document the exact exclusions. Updated `AppShell.test.tsx` to assert the FAB is present in bare mode, that Ctrl+J opens the panel there, and that Ctrl+K/`?` stay disabled; removed the obsolete "neither chord" test.
- Review pass (changes requested): Implemented bare-specific minimize behavior inside the shared `agentOverlay` node: the FAB condition includes `bare && globalAgentMinimized`, and the minimized-bar condition excludes `bare`, so minimizing on the App surface restores the FAB while non-bare still shows the minimized bar. Added tests for both bare and non-bare minimize behavior.

## Task 6: Preserve the active chapter between dashboard view and edit — 2026-10-15

- Outcome: done
- Changed:
  - Created/extended: `web/src/utils/dashboardTabs.test.ts`, `web/src/pages/DashboardPage.test.tsx`, `web/src/pages/DashboardEditorPage.test.tsx`
  - Modified: `web/src/utils/dashboardTabs.ts`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/DashboardEditorPage.tsx`
  - Deleted: none
- Verified:
  - `cd web && npx vitest run src/utils/dashboardTabs.test.ts src/pages/DashboardPage.test.tsx src/pages/DashboardEditorPage.test.tsx` → pass
  - `cd web && npm run test:run` → pass (1505 passed, 2 skipped)
  - `cd web && npx tsc --noEmit` → clean
  - `cd web && npm run build` → pass
- Notes: Added `tabQueryString` helper and wired it into the view-page Edit link and the editor View link. The editor now initializes `editorTab` from `?tab=`, writes chip changes back to the URL with `replace`, and clears stale/invalid params once the tab list is known. The App health panel jump-to-chapter also updates the URL.
- Review pass (changes requested): (1) Added stale-param cleanup on the view page: invalid or inapplicable `?tab=` is removed with `replace` once the dashboard (and therefore tabs) is known, preserving unrelated params. (2) Added behavior-level tests to both page test files covering the Edit link, editor initialization, chip-to-URL updates, View link, and invalid/no-tab param clearing. (3) Re-review fix: restored the preview-mode toggle test with a tabbed dashboard so both `Preview draft` and `View published` links are asserted to retain the active `?tab=orders` alongside an unrelated param.

## Task 7: Honor the fixed app width in the dashboard editor — 2026-10-15

- Outcome: done
- Changed:
  - Created: `web/src/utils/appLayout.ts`, `web/src/utils/appLayout.test.ts`
  - Modified: `web/src/components/AppHeader.tsx`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/DashboardEditorPage.tsx`, `web/src/pages/PublicDashboardPage.tsx`
  - Deleted: none
- Verified:
  - `cd web && npx vitest run src/utils/appLayout.test.ts` → pass (4 passed)
  - `cd web && npm run test:run` → pass (1509 passed, 2 skipped; +4 from the new appLayout tests over the Task 6 baseline of 1505)
  - `cd web && npx tsc --noEmit` → clean
  - `cd web && npm run build` → pass
- Notes: Moved `APP_CONTENT_MAX_WIDTH` (1200) into the new `web/src/utils/appLayout.ts` as the single source of truth and added the `appContentWidthStyle(width)` helper (fixed/absent → `{ maxWidth: APP_CONTENT_MAX_WIDTH, margin: '0 auto' }`, full → `undefined`), unit-tested in `appLayout.test.ts`. `AppHeader.tsx` and `DashboardPage.tsx` now use the helper instead of their local copies. `DashboardEditorPage.tsx` resolves `appWidth` from `liveDashboard.settings?.app?.width ?? 'fixed'` and merges `appContentWidthStyle(appWidth)` into the widget-canvas wrapper that `layoutRef` measures (the main body `<div>`), so both the desktop `GridLayout` and the stacked `mobileGrid` fallback are constrained and the ResizeObserver measures the capped width; the editor toolbar/sub-header is untouched. Deviation: also updated `PublicDashboardPage.tsx` (not in the plan's Files list) to use the shared helper, because acceptance criterion 3 explicitly requires view/public/editor to all use the shared helper and it was the only remaining consumer of the old `AppHeader` export. No browser validation was performed (the Vite dev server was not running; out of the task's listed verification commands).

## Task 8: Order editor widgets by chapter, then grid position — 2026-10-15

- Outcome: done
- Changed:
  - Created: none
  - Modified: `web/src/utils/dashboardTabs.ts`, `web/src/pages/DashboardEditorPage.tsx`, `web/src/utils/dashboardTabs.test.ts`
  - Deleted: none
- Verified:
  - `cd web && npx vitest run src/utils/dashboardTabs.test.ts` → pass (17 passed; +6 new)
  - `cd web && npm run test:run` → pass (1515 passed, 2 skipped; +6 over the Task 7 baseline of 1509)
  - `cd web && npx tsc --noEmit` → clean
  - `cd web && npm run build` → pass
- Notes: Added `orderWidgetsForEditor(widgets, tabs)` to `dashboardTabs.ts`: a non-mutating stable sort by `(tabIndex, layout.row, layout.col)` where `tabIndex = tabs.findIndex((t) => t.id === widgetTabId(widget, tabs))`, normalized to `0` when negative so untabbed/invalid/no-tab widgets land in the first bucket (single bucket when there are no tabs). `DashboardEditorPage.tsx` wraps `shownWidgets` in the existing `useMemo` and now pipes the tab-filtered list through `orderWidgetsForEditor`; ordering affects only rendering/compaction (the stored document is untouched) and per-chapter views still work. Test cases cover tab buckets, untabbed/empty/unknown tabs, row-then-col ordering inside a bucket, stability for equal positions, the no-tabs single-bucket case, and non-mutation of the input.

## Task 9: Make the editor's View action say what it shows — 2026-10-15

- Outcome: done
- Changed:
  - Created: none
  - Modified: `web/src/pages/DashboardEditorPage.tsx`, `web/src/pages/DashboardEditorPage.test.tsx`
  - Deleted: none
- Verified:
  - `cd web && npx vitest run src/pages/DashboardEditorPage.test.tsx` → pass (39 passed; +2 new)
  - `cd web && npm run test:run` → pass (1517 passed, 2 skipped; +2 over the Task 8 baseline of 1515)
  - `cd web && npx tsc --noEmit` → clean
  - `cd web && npm run build` → pass
- Notes: Derived `hasPublishedVersion = !!latestVersion` from the same `versionsQuery.data?.latest` source that renders the `v{n}` / `Not published` chip, and used it to drive the header View link: `View published` with a title naming the published snapshot and the `Preview draft` route to unpublished edits when a version exists, `View app` with a title explaining the draft is shown because nothing is published yet otherwise. The `Eye` icon and `nav-btn` styling are unchanged and the link still carries the active `?tab=` via `tabQueryString(activeEditorTab)` → `/dashboards/{id}/view`. Added two tests asserting both label+title states; the existing "View link carries the active chapter" test uses the never-published fixture and its `/View/` regex still matches `View app`. Grepped `e2e/`: the sole `View published` link assertion (`e2e/dashboard-publish.spec.ts:486`) targets the view page's own link (`DashboardPage.tsx`, `/view?preview=draft`), not the editor, so no e2e selector changes were required — confirmed.
- Review pass (changes requested): `latestVersion` comes from `versionsQuery`, which is disabled unless `can_edit !== false`, so for a viewer (or before the query resolves) `versionsQuery.data` is undefined and the link always took the never-published branch — telling viewers of a published dashboard "showing the current draft because it has not been published yet" while the destination served the published snapshot. Fixed by treating unknown versions state as neutral: added `versionsKnown = versionsQuery.data !== undefined` and made `viewLinkTitle` `'View the app'` when unknown, keeping the existing published/never-published strings and the unchanged label (`View published`/`View app`) and `?tab=` href wiring. Added a test (`web/src/pages/DashboardEditorPage.test.tsx`) mirroring the viewer test with a `can_edit: false` + `published_version` dashboard: the link's accessible name is `View app`, its `title` does not contain `not been published`, and the versions endpoint is still never fetched. Adapted the existing never-published test to `await waitFor` the resolved title (the never-published wording now only lands once the query resolves, since unknown state is neutral). Verified: `cd web && npx vitest run src/pages/DashboardEditorPage.test.tsx` → 40 passed; `cd web && npm run test:run` → 1518 passed, 2 skipped; `cd web && npx tsc --noEmit` → clean; `cd web && npm run build` → pass.
