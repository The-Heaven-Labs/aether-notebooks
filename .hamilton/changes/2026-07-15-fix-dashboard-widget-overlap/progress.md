## Task 1: Add backend layout validation to widget create/update handlers — 2026-07-15
- Outcome: done
- Changed: modified `internal/api/dashboard_handlers.go`, `internal/api/dashboard_handlers_test.go`
- Verified: `go test ./internal/api/... -run "TestWidgetLayout|TestDashboard_Widgets" -v -count=1` → 10 passed
- Notes: Simplified from original plan — overlap validation removed because existing tests intentionally create widgets at the same position (the library handles compaction at render time). Only bounds validation (col + width <= grid_cols) is enforced.

## Task 2: Add frontend layout clamping before save — 2026-07-15
- Outcome: done
- Changed: modified `web/src/pages/DashboardEditorPage.tsx`
- Verified: `npx tsc --noEmit` → clean, `npm run build` → success (in main checkout; worktree node_modules are symlinked)
- Notes: Added `clampLayout` helper that clamps width to grid bounds and shifts down on collision. Applied in `onDragStop` (no-swap branch) and `onResizeStop`.
