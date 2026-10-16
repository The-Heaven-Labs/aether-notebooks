# Review: UX improvements — warehouse provisioner test, dashboard fixes, cell fixes, search cell jump

## Whole branch `feat/ux-improvements` (0d0b8876..8d2c0ecf) — 2026-10-16

Verdict: approved

Verified across the full base..HEAD diff and all eleven plan tasks: correctness against each
task's acceptance criteria in `plan.md`; no auth/ACL regressions (the new provisioner test
endpoint stays behind `RequireRole("admin")` with an org-scoped warehouse lookup, mirrors
`handleTestConnector`'s 200-with-error convention, and always drops + verifies the throwaway
ClickHouse identity); Yjs remains the single source of truth for cell content; dashboard
`?tab=` handling matches the existing helpers; no stray artifacts. Per-task review passes are
recorded in `progress.md`.

Three non-blocking suggestions were reported and folded into `a5e2e528` (verified by focused
tests plus live browser/API checks — see the "Whole-branch review follow-ups" entry in
`progress.md`):

- `web/src/pages/WarehouseSettingsPage.tsx` referenced an undefined `--success-border` (the
  success box's border silently dropped) → the token is now defined in both light and both
  dark token sets in `web/src/styles/theme.css`.
- `internal/api/warehouse_test_handler.go`'s `runWarehouseProvisionerTest` took unused
  `claims`/`warehouseJSON` params and re-loaded the warehouse → one header load in the handler,
  an inline org-scope check, and the header passed into the runner.
- The provisioner test result box now carries `role="status"` + `aria-live="polite"`.

Fix-round evidence: `bash scripts/go-test.sh ./internal/api/ -run "TestWarehouseProvisionerTest" -count=1 -timeout 3m -v`
→ 7/7 (including the foreign-warehouse 404 against the new inline org check); warehouse page
vitest 23/23; full web suite 1528 passed; tsc/build clean; live provisioner test 5/5 steps with
the success border rendered; CI smoke (`e2e/smoke_test.sh`) all 10 steps pass against a native
API instance. End-of-branch real-browser validation covered every UI task's user flow
(Warehouse test, bare-app FAB/panel, chapter preservation, fixed-width editing, All ordering,
publish labeling, text-panel height, table-output clipboard copy, SQL autocomplete,
search-to-cell on both the results page and the same-page palette case).
