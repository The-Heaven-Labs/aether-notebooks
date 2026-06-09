# Wave 1: Notebook Fixes (Items 25, 36)

## Status: ✅ COMPLETE

## Changes Made

### Item 36: New cell created near bottom visually cut off
- **File:** `web/src/pages/NotebookPage.tsx`
- **Change 1:** Added `setTimeout` in `createCell` mutation's `onSuccess` callback that scrolls the newly created cell into view with smooth animation, centered in the viewport.
- **Change 2:** Increased `cellsArea` bottom padding from `64px` to `80px` to prevent cells from being visually cut off at the bottom.

### Item 25: Collapse/Show all buttons
- **File:** `web/src/pages/NotebookPage.tsx`
- **Change 1:** Added `allCollapsed` state variable (boolean, default `false`).
- **Change 2:** Added `toggleCollapseAll` function that:
  - Toggles the `allCollapsed` state
  - Iterates all cells and calls `PUT /api/v1/notebooks/:id/cells/:cellId` with `source_visible` and `cell_collapsed` properties
  - Updates local state optimistically
- **Change 3:** Added "Collapse All" / "Show All" toggle button in the toolbar (before Run All button), using the existing `schemaBtn` style for consistency.

## Commit
```
62f2c03 fix: scroll new cell into view, add collapse/show all toggle (items 25, 36)
```

## Validation
- Diff reviewed — all 5 changes are correct and minimal
- No new dependencies required
- Uses existing API endpoints and styles
- Individual cell expand/collapse still works independently (the toggle just sets all at once)

## Notes
- The `toggleCollapseAll` function uses sequential `await` for each cell API call. For notebooks with many cells, this could be slow. A batch endpoint would be a future optimization but is out of scope for this item.
- The scroll-into-view uses a 100ms delay to ensure the DOM has updated with the new cell before attempting to scroll.
