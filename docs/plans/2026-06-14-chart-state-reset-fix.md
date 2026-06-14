# Chart State Reset Fix

**Date:** 2026-06-14
**Status:** Implemented & Verified

## Problem

Chart visualizations lose their internal state (zoom, dataZoom slider position, collapse/expand, selections) when the user's mouse leaves the chart area. Any interaction with the notebook outside the chart resets the view to default.

## Root Causes Discovered

### 1. `notMerge: true` in `EChartsContainer`

`web/src/charts/common.tsx:126`: `chart.setOption(option, { notMerge: true })` completely replaces the chart instance, destroying all internal ECharts state (zoom, pan, dataZoom, selections).

**Fix:** Changed default to `notMerge: false` so `setOption()` merges new options with existing chart state.

### 2. Unstable `columns` array references

Every chart module computed `const columns = data.columns.map(c => c.name)` on every render, creating a new array reference. This reference was in the `useMemo` dependency for `chartData`, causing the entire memoization chain to recompute on every render — cascading through `chartData → treeData → option → setOption`.

**Fix:** Wrapped `columns` in `useMemo(() => data.columns.map(c => c.name), [data.columns])` in all 7 chart modules.

### 3. Unstable `metricCols` reference

`web/src/charts/HierarchyTreeChart.tsx:96`: `const metricCols = config.metricColumns ?? []` created a new empty array on every render when no metrics were configured, invalidating the `treeData` useMemo dependency.

**Fix:** Wrapped in `useMemo(() => config.metricColumns ?? [], [config.metricColumns])`.

### 4. `notMerge: false` breaks tree series data replacement

ECharts' merge-by-name behavior for tree series data corrupts the tree when node labels change (e.g., metrics added to names). This caused only one node to appear when config was modified.

**Fix:** Tree chart uses `notMerge={true}` to fully replace series data on config changes. Axis charts continue using `notMerge={false}` for zoom/pan persistence.

### 5. Tree collapsed state not tracked across `setOption`

Tree chart's `expandAndCollapse` state is stored internally by ECharts. When `setOption` replaced the tree data (even with `notMerge: true`), collapsed state was lost.

**Fix:** In `EChartsContainer`, before calling `setOption`, read current collapsed state via `chart.getOption()`, walk the tree nodes to collect collapsed names, then apply them to the new tree data via `applyCollapsedToTree`. This runs regardless of `notMerge` setting.

## Files Modified

| File | Change |
|------|--------|
| `web/src/charts/common.tsx` | `notMerge` default `true` → `false`; added `walkTree`, `applyCollapsedToTree` helpers; collapsed state preservation; `replaceMerge` removed |
| `web/src/charts/BarChart.tsx` | Added `useMemo` for `columns`, `chartData`, and `option` |
| `web/src/charts/LineChart.tsx` | Added `useMemo` for `columns`, `chartData`, and `option` |
| `web/src/charts/AreaChart.tsx` | Added `useMemo` for `columns`, `chartData`, and `option` |
| `web/src/charts/ScatterChart.tsx` | Added `useMemo` for `columns`, `chartData`, and `option` |
| `web/src/charts/PieChart.tsx` | Added `useMemo` for `columns`, `chartData`, and `option` |
| `web/src/charts/TimelineChart.tsx` | Added `useMemo` for `columns`, `chartData`, `groups`, `colors`, and `option`; fixed mutation of memoized array |
| `web/src/charts/HierarchyTreeChart.tsx` | Added `useMemo` for `columns` and `metricCols`; wired `handleChartReady` to capture chart instance; implemented `handleReset` (was no-op); uses `notMerge={true}` |
| `web/src/charts/index.tsx` | Removed D3TreeChart references |
| `web/src/charts/types.ts` | Removed `d3_tree` from ChartType union |

## Files Deleted

| File | Reason |
|------|--------|
| `web/src/charts/D3TreeChart.tsx` | Removed per user request |

## Architecture

### `EChartsContainer` (`common.tsx`)

```
React re-render → option ref unchanged → React.memo prevents re-render → ECharts state preserved
                              ↓ (option changed)
                    useEffect fires
                              ↓
                    collapsed state preservation from chart.getOption()
                              ↓
                    chart.setOption(finalOption, { notMerge })
```

- `React.memo` on `EChartsContainer` prevents re-render when option reference is stable
- Serialization guard (tried but removed due to `JSON.stringify` failures with BigInt)
- Collapsed state preservation always runs when chart exists

### Tree chart (`HierarchyTreeChart.tsx`)

- `notMerge={true}`: replaces series data fully (safe for tree data)
- `walkTree`/`applyCollapsedToTree`: preserve collapsed nodes across updates
- `handleChartReady`: captures chart instance for Reset button
- `handleReset`: dispatches `restore` action then re-applies collapsed state

### Axis charts (Bar, Line, Area, Scatter, Pie, Timeline)

- `notMerge=false` (default): merges new options, preserving zoom/pan/dataZoom
- Memoized columns and options prevent unnecessary setOption calls

## Validation

- Zoom/pan persists after mouse leaves chart (axis charts)
- dataZoom slider persists after mouse leaves chart (timeline chart)
- Tree node collapse/expand persists after mouse leaves chart
- Tree config changes (label, metrics) render all nodes correctly
- Reset view button resets zoom/pan while preserving collapsed state
- TypeScript type check passes (0 errors in changed files)
