# Proposal: Fix dashboard widget overlap on the editor page

## Problem

When opening the dashboard editor, widgets in the right column are stacked on top of each other, overlapping visually. Dragging them does not fix the layout.

## Root cause

The backend does not validate widget layout bounds on create or update. Widgets are stored with invalid positions:
- A widget at `col: 6` with `width: 12` extends to column 17, past the 12-column grid
- Two widgets at `col: 0, row: 8` with `width: 6` occupy the exact same grid cells

The frontend's `GridLayout` applies `correctBounds` at render time, but the stored data is wrong, causing the overlap.

## What changes

1. **Backend validation** — reject widget create/update requests where the layout would extend past the grid's column count or overlap another widget in the same dashboard
2. **Frontend correction** — clamp widget widths to `grid_cols - col` before saving, and shift overlapping widgets down to avoid collisions

## Capabilities

| Capability | Status | Notes |
|---|---|---|
| `dashboard-layout` | MODIFIED | Backend validation + frontend clamping |
