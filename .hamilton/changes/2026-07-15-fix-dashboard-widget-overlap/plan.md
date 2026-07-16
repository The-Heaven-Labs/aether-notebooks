# Plan: Fix dashboard widget overlap on the editor page

## Overview

- Change: `.hamilton/changes/2026-07-15-fix-dashboard-widget-overlap/`
- Goal: Prevent widgets from being saved with invalid layout positions (extending past grid boundary or overlapping other widgets), fixing the visual overlap bug on the dashboard editor page.
- Test: `task test:api` (Go backend tests)
- Build / typecheck: `cd web && npx tsc --noEmit` (frontend TypeScript)
- Context notes: The backend currently accepts any layout values without validation. The frontend's `GridLayout` applies `correctBounds` at render time, but the stored data remains wrong. Two-layer fix: backend rejects invalid layouts, frontend prevents them from being sent.
- Quality notes: Backend validation is a single cohesive unit (layout bounds checking, one reason to change). Frontend clamp is a separate cohesive unit (prevents bad input). No structural smells — each task lands one cohesive unit that can be tested in isolation.

## Tasks

### Task 1: Add backend layout validation to widget create/update handlers

- Depends on: none
- Files:
  - Created: none
  - Modified: `internal/api/dashboard_handlers.go`, `internal/api/dashboard_handlers_test.go`
  - Deleted: none
- Acceptance:
  - A widget create request with `col + width > grid_cols` returns 400 with error message "layout exceeds grid columns"
  - A widget create request with `col < 0` or `row < 0` or `width <= 0` or `height <= 0` returns 400 with error message "invalid layout dimensions"
  - A widget create request that overlaps an existing widget in the same dashboard returns 400 with error message "widget overlaps existing widget"
  - A widget update request with the same invalid layouts returns 400
  - A valid widget create/update request succeeds (no regression)
- Steps:
  1. In `internal/api/dashboard_handlers.go`, add a `validateWidgetLayout` method on `*Server` that takes the dashboard ID, the new layout, and an optional exclude widget ID (for updates). It should:
     - Query the dashboard's `settings->>'grid_cols'` (default 12)
     - Check `col >= 0`, `row >= 0`, `width > 0`, `height > 0`
     - Check `col + width <= grid_cols`
     - Query existing widgets for the dashboard and check for bounding-box overlap (excluding the widget being updated)
     - Return a descriptive error string if any check fails, nil if valid
  2. In `handleAddWidget`, call `validateWidgetLayout` after decoding the request and before the INSERT. Return 400 on error.
  3. In `handleUpdateWidget`, call `validateWidgetLayout` after decoding the request and before the UPDATE. Return 400 on error.
  4. In `dashboard_handlers_test.go`, add tests:
     - `TestWidgetLayoutValidation_OutOfBounds`: create a dashboard, add a widget with `col: 6, width: 12` (exceeds 12-column grid) → expect 400
     - `TestWidgetLayoutValidation_NegativeValues`: create a dashboard, add a widget with `col: -1` → expect 400
     - `TestWidgetLayoutValidation_Overlap`: create a dashboard, add a widget at `col: 0, row: 0, width: 6, height: 8`, then add another at `col: 0, row: 0, width: 6, height: 8` → expect 400
     - `TestWidgetLayoutValidation_Valid`: create a dashboard, add a widget at `col: 0, row: 0, width: 6, height: 8`, then add another at `col: 6, row: 0, width: 6, height: 8` → expect 201 (no overlap)
  5. Run `task test:api` — all tests pass
- Verify: `task test:api` → all tests pass, including the new validation tests
- Commit: `fix: validate widget layout bounds on create/update to prevent overlap`

### Task 2: Add frontend layout clamping before save

- Depends on: Task 1
- Files:
  - Created: none
  - Modified: `web/src/pages/DashboardEditorPage.tsx`
  - Deleted: none
- Acceptance:
  - When a widget is dragged such that `col + width > grid_cols`, the width is clamped to `grid_cols - col` before saving
  - When a widget is dragged to a position that overlaps another widget, the widget is shifted down (row incremented) until it no longer overlaps, before saving
  - When a widget is resized such that `col + width > grid_cols`, the width is clamped before saving
  - The existing swap-on-drag behavior is preserved (no regression)
- Steps:
  1. In `DashboardEditorPage.tsx`, add a `clampLayout` helper function that takes a `LayoutItem`, the current `gridCols`, and the full `layout` array. It should:
     - Clamp `item.w` to `gridCols - item.x` if `item.x + item.w > gridCols`
     - Clamp `item.h` to `item.h` (no change needed for height, maxH is handled by the grid)
     - Check for collisions with other items in the layout. If a collision is found, increment `item.y` until no collision exists.
     - Return the clamped item
  2. In `saveLayout`, before calling `api.put`, apply `clampLayout` to the item
  3. In `onDragStop`, in the "no swap" branch (the `else` clause), apply `clampLayout` to the settled item before calling `saveLayout`
  4. In `onResizeStop`, apply `clampLayout` to the settled item before calling `saveLayout`
  5. Run `cd web && npx tsc --noEmit` — no type errors
  6. Run `cd web && npm run build` — build succeeds
- Verify: `cd web && npx tsc --noEmit && npm run build` → no errors
- Commit: `fix: clamp widget layout before save to prevent backend rejection`
