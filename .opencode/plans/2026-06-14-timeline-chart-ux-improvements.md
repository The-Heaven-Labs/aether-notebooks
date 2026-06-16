# Timeline Chart UX Improvements — Design

**Date:** 2026-06-14
**Status:** Implemented

## Problem

The timeline chart type (ECharts-based, point-event and range/Gantt modes) had several UX issues:

1. **Nodes too far from time axis** — Point-event mode used a `category` y-axis, placing events in the vertical center of the chart.
2. **No direct zoom/pan** — Only a small dataZoom slider at the bottom; no mouse wheel zoom or click-drag pan.
3. **Label overlap** — When events have close timestamps, their labels (all positioned `top`) collide horizontally.
4. **No visual sequence flow** — No way to visually track chronological progression (no connector lines, no time deltas in tooltip).

## Solution Summary

- **Value y-axis** for single-group mode: events at `y: 0.2` with `min: 0, max: 1`, placing dots near the time axis with room for labels above and below.
- **Mouse wheel zoom + drag pan** via `type: 'inside'` dataZoom (invisible, handles direct interaction). Slider reduced to 8px as visual indicator.
- **Label stagger**: Each group renders as TWO scatter series — even-indexed events get `position: 'top'`, odd-indexed get `position: 'bottom'`. Labels alternate above/below dots.
- **Overlap hiding**: `labelLayout: { hideOverlap: true }` hides colliding labels (tooltip shows full info).
- **Connector lines**: Dashed line series at `y: 0.2` connecting consecutive events in single-group mode.
- **Time deltas**: Tooltip shows `"Δ 2m 34s"` from previous event, or `"Sequence start"` for the first.
- **Config panel**: Two new checkboxes — "Show connectors" and "Show time deltas" (both default on).

## Files Changed

| File | Changes |
|---|---|
| `web/src/charts/types.ts` | Added `showConnectors?: boolean` and `showTimeDeltas?: boolean` to `ChartConfig` |
| `web/src/charts/TimelineChart.tsx` | Full rewrite of layout, interaction, labels, connectors, tooltip, config panel |
| `web/src/test/TimelineChart.test.tsx` | Added 3 new tests (7 total) |

## Technical Details

### Layout (single-group point-event mode)

- **y-axis**: `type: 'value'`, `show: false`, `min: 0`, `max: 1`. Events at `y: 0.2`.
- **Grid**: `{ top: 50, right: 16, bottom: 60, left: 16 }` — explicit bounds, no `containLabel` (avoids ECharts auto-scaling issues).
- **Chart height**: 320px (was 350px).

### Layout (range/Gantt mode)

- **y-axis**: `type: 'category'` with lane groups.
- **Per-lane height**: 36px (was 50px).
- **Grid**: `{ top: 30, right: 16, bottom: 16, left: 16, containLabel: true }`.

### Interaction

```typescript
dataZoom: [
  { type: 'inside', xAxisIndex: 0 },          // mouse wheel zoom + drag pan
  { type: 'slider', xAxisIndex: 0, bottom: 0, height: 8 },  // visual indicator
]
```

### Label Stagger (two-series approach)

Each group renders as two scatter series via `flatMap`:
- Series 1: even-indexed events, `position: 'top'`, `distance: 16`
- Series 2: odd-indexed events, `position: 'bottom'`, `distance: 16`
- Both use `labelLayout: { hideOverlap: true }` for collision management.

### Connector Lines

Dashed line series at `y: 0.2` (same level as dots), rendered behind scatter dots (`z: 1` vs `z: 2`). Only in single-group mode. `silent: true` to avoid tooltip interference.

### Time Deltas

Tooltip formatter computes delta from previous event in the same group/lane by finding the event's index in the sorted data array. Uses `formatDuration()` helper for human-readable output (e.g., "2m 34s", "1h 5m 30s").

## Testing

7 tests in `web/src/test/TimelineChart.test.tsx`:
1. Renders point-in-time events
2. Renders range-based events
3. Auto-detects time columns
4. Config panel renders time column selector
5. Renders with connectors and time deltas enabled
6. Config panel shows connector and time delta toggles
7. Renders in single-group layout (value y-axis)
