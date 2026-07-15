# Design: Fix dashboard widget overlap on the editor page

## Context

Widgets are stored with invalid layout positions. The backend accepts any layout values without validation. The frontend's `GridLayout` applies `correctBounds` at render time, but the stored data remains wrong, causing visual overlap.

Verified via DB query on the "Chart Types Showcase Dashboard":
- Widget `2f279698`: `col: 6, width: 12` — extends past grid boundary
- Widgets `412e10d9` + `c3246e76`: both `col: 0, row: 8, width: 6` — exact overlap

## Decision

Two-layer fix: backend rejects invalid layouts, frontend prevents them from being sent.

### Backend: layout validation in `handleCreateWidget` and `handleUpdateWidget`

In `internal/api/dashboard_handlers.go`, after decoding the request body:

1. Fetch the dashboard's `grid_cols` setting (default 12)
2. Validate `col >= 0`, `row >= 0`, `width > 0`, `height > 0`
3. Validate `col + width <= grid_cols` — reject 400 if violated
4. Query existing widgets for the same dashboard and check for bounding-box overlap with the new layout — reject 400 if violated

This is a validation-only change. No new tables, no schema changes.

### Frontend: clamp before save in `DashboardEditorPage.tsx`

In the `onDragStop` and `onResizeStop` callbacks, before calling `saveLayout`:

1. Clamp `item.w` to `gridCols - item.x` if it exceeds the grid
2. After clamping, check for collisions with other widgets in the current layout. If a collision is found, increment `item.y` until no collision exists.

This prevents the frontend from sending invalid layouts that the backend would reject.

## Files changed

- `internal/api/dashboard_handlers.go` — add layout validation to create and update handlers
- `web/src/pages/DashboardEditorPage.tsx` — add clamp logic to `onDragStop` and `onResizeStop`

## Testing strategy

- Backend: add tests in `internal/api/dashboard_handlers_test.go` that verify 400 responses for out-of-bounds and overlapping layouts
- Frontend: manual test — drag a widget past the grid boundary, confirm it clamps; drag a widget onto another, confirm it shifts down
- Regression: `task test:api` and `cd web && npx tsc --noEmit`

## Quality Lens

Validation logic is cohesive (layout bounds checking), has a single reason to change (layout rules), and sits at the correct boundary (API handlers validate input, frontend prevents bad input).
