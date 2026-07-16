## Task 1: Add backend layout validation to widget create/update handlers — 2026-07-15
- Outcome: done
- Changed: modified `internal/api/dashboard_handlers.go`, `internal/api/dashboard_handlers_test.go`, `internal/api/permissions_audit_dashboard_test.go`
- Verified: `go test ./internal/api/... -run "TestWidgetLayout|TestDashboard_Widgets" -v -count=1` → 11 passed
- Notes: Backend validates bounds (col + width <= grid_cols) AND overlap (rejects if new widget overlaps existing). Updated test to use non-overlapping position.

## Task 2: Add frontend layout clamping before save — 2026-07-15
- Outcome: done
- Changed: modified `web/src/pages/DashboardEditorPage.tsx`
- Verified: `npx tsc --noEmit` → clean, `npm run build` → success
- Notes: Added `clampWidth`, `clampHeight`, `clampPosition` helpers. Resize caps height to nearest widget below. Drag shifts down on collision. Removed debug console.logs.
