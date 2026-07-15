# Requirements: dashboard-layout (MODIFIED)

## MODIFIED: Widget layout validation on save

The backend SHALL validate widget layout bounds before persisting to the database.

### Scenarios

**WHEN** a widget is created or updated with `col + width > grid_cols`
**THEN** the request SHALL be rejected with a 400 error describing the out-of-bounds layout

**WHEN** a widget is created or updated and its bounding box overlaps another widget in the same dashboard (same column range + same row range)
**THEN** the request SHALL be rejected with a 400 error describing the overlap

## MODIFIED: Frontend layout clamping on save

The frontend SHALL clamp widget dimensions before sending them to the backend.

### Scenarios

**WHEN** a widget is dragged or resized such that `col + width > grid_cols`
**THEN** the frontend SHALL clamp `width` to `grid_cols - col` before saving

**WHEN** a widget is dragged to a position that overlaps another widget
**THEN** the frontend SHALL shift the dragged widget down (increase `row`) until it no longer overlaps, before saving
