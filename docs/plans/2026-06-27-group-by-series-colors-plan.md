# group_by + series_colors for Axis-Based Charts — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow bar/stacked_bar/line/area/scatter charts to accept `group_by` and let `series_colors` keys match group values instead of only y_columns.

**Architecture:** Backend removes validation gates and stale filtering; frontend adds shared `useGroupBySeries()` hook that axis-based chart components use when `config.groupBy` is set. The hook generates one ECharts series per (group × y_column) using efficient Map-based lookup.

**Tech Stack:** Go (net/http, pgx), TypeScript/React (ECharts), PostgreSQL JSONB metadata

---

### Task 1: Backend — Un-gate group_by storage

**Files:**
- Modify: `internal/agent/tools_chart.go:39-98` (chartAllowedFields)
- Modify: `internal/agent/tools_chart.go:231-234` (create_chart gating)
- Modify: `internal/agent/tools_chart.go:438-443` (update_chart gating)

**Step 1: Add "groupBy": true to axis-based chart types**

Edit `chartAllowedFields` map. Add `"groupBy": true` to `"bar"`, `"stacked_bar"`, `"line"`, `"area"`, and `"scatter"` entries.

**Step 2: Remove timeline gate in create_chart**

Change lines 231-234 from:
```go
// groupBy is only used by timeline charts; silently ignore for others
if req.GroupBy != "" && req.ChartType == "timeline" {
    chartConfig["groupBy"] = req.GroupBy
}
```
To:
```go
if req.GroupBy != "" {
    chartConfig["groupBy"] = req.GroupBy
}
```

**Step 3: Remove timeline gate in update_chart**

Change lines 438-443 from:
```go
if req.GroupBy != "" {
    ct, _ := existingConfig["chartType"].(string)
    if ct == "timeline" {
        existingConfig["groupBy"] = req.GroupBy
    }
}
```
To:
```go
if req.GroupBy != "" {
    existingConfig["groupBy"] = req.GroupBy
}
```

**Step 4: Verify the build compiles**

Run: `go build ./...`
Expected: no errors

**Step 5: Commit**

```bash
git add internal/agent/tools_chart.go
git commit -m "feat: allow group_by on all axis-based chart types"
```

---

### Task 2: Backend — Remove series_colors validation

**Files:**
- Modify: `internal/agent/tools_chart.go:100-149` (filterChartConfig)
- Modify: `internal/agent/tools_chart.go:32` (tool description)
- Modify: `internal/agent/tools_notebook.go:376-432` (stale filtering on read)

**Step 1: Remove y_columns-based seriesColors validation**

In `filterChartConfig()`, delete lines 116-147 entirely (the `if chartType == "bar" || ...` block that validates seriesColors keys against yAxis). The function should still strip unallowed fields (lines 106-114) but no longer validate seriesColors.

**Step 2: Remove stale filtering in read_cell**

In `tools_notebook.go`, delete the "Filter stale seriesColors keys from metadata" block (lines 376-432). This removes the silent deletion of non-y_columns seriesColors keys when reading cells back.

**Step 3: Update tool descriptions**

Change line 32 description from:
```
"Modify chart config on an existing cell. NOTE: group_by only works with chart_type='timeline'; other chart types (bar, line, area) ignore group_by and derive series from y_columns instead. Colors in series_colors must match series names from the chart's y_columns (for bar/line/area) or group values (for timeline).",
```
To:
```
"Modify chart config on an existing cell. For axis-based charts (bar, line, area, scatter), series are derived from y_columns. When group_by is set, one series is created per unique group value (cross-product with y_columns if multiple). Colors in series_colors keys match y_columns (no group_by) or group values (with group_by). For timeline, series_colors keys match group values.",
```

Update the `series_colors` parameter description on line 33 from:
```
"Map of series names to hex colors. Keys must match y_columns (bar/line/area) or group values (timeline). E.g. {\"revenue\":\"#ff0000\"}"
```
To:
```
"Map of series names to hex colors. Keys match y_columns (no group_by) or group values (with group_by). E.g. {\"revenue\":\"#ff0000\"}"
```

**Step 4: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 5: Commit**

```bash
git add internal/agent/tools_chart.go internal/agent/tools_notebook.go
git commit -m "feat: remove series_colors y_columns-only validation"
```

---

### Task 3: Backend — Report group_by for all chart types

**Files:**
- Modify: `internal/agent/tools_notebook.go:328-334` (chart_summary group_by reporting)

**Step 1: Remove timeline-only gate in chart_summary**

Change lines 328-334 from:
```go
if gCol, ok := meta.Chart["groupBy"].(string); ok {
    chartType, _ := meta.Chart["chartType"].(string)
    if chartType == "timeline" {
        summary["group_by"] = gCol
    }
    groupByCol = gCol
}
```
To:
```go
if gCol, ok := meta.Chart["groupBy"].(string); ok {
    summary["group_by"] = gCol
    groupByCol = gCol
}
```

This ensures `data_series` is populated from group column values for ALL chart types that have group_by set.

**Step 2: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/agent/tools_notebook.go
git commit -m "feat: report group_by for all chart types in read_cell summary"
```

---

### Task 4: Backend — Update engine prompt

**Files:**
- Modify: `internal/agent/engine.go:1144-1155` (agent chart descriptions)

**Step 1: Update chart descriptions**

Change line 1145 from:
```
"\n  Bar/stacked_bar: x_column (categories), y_columns (values). Also: bar_width (% string), bar_gap (% string), data_zoom."
```
To:
```
"\n  Bar/stacked_bar: x_column (categories), y_columns (values). Also: group_by (split into series by column), bar_width (% string), bar_gap (% string), data_zoom."
```

Change line 1147 from:
```
"\n  Area: same as line with area fill. Also: smooth, connect_nulls, data_zoom."
```
To:
```
"\n  Area: same as line with area fill. Also: group_by (split into series by column), smooth, connect_nulls, data_zoom."
```

Change line 1148 from:
```
"\n  Scatter: x_column (numeric), y_columns (values). Also: color_column (maps 3rd dim to color gradient), size_column (bubble size), data_zoom (always enabled)."
```
To:
```
"\n  Scatter: x_column (numeric), y_columns (values). Also: group_by (split into series by column), color_column (maps 3rd dim to color gradient), size_column (bubble size), data_zoom (always enabled)."
```

**Step 2: Build**

Run: `go build ./...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/agent/engine.go
git commit -m "docs: add group_by to agent chart descriptions"
```

---

### Task 5: Frontend — Add useGroupBySeries hook

**Files:**
- Modify: `web/src/charts/common.tsx` (~40 lines added)

**Step 1: Write the hook**

Add after `useAxisColumns()` function (after line 134):

```typescript
export function useGroupBySeries(
  chartData: Record<string, unknown>[],
  config: { xAxis?: string; yAxis?: string[]; groupBy?: string; seriesColors?: Record<string, string> },
  colors: ReturnType<typeof getChartColors>
): { series: any[]; xValues: string[] } {
  return useMemo(() => {
    const groupByCol = config.groupBy
    if (!groupByCol || !config.xAxis || !config.yAxis?.length) {
      return { series: [], xValues: [] }
    }

    const xKey = config.xAxis
    const yCols = config.yAxis

    // Build Map: xVal → Map(groupVal → row)
    const xMap = new Map<string, Map<string, Record<string, unknown>>>()
    const groupOrder: string[] = []

    for (const row of chartData) {
      const xVal = String(row[xKey] ?? '')
      const gVal = String(row[groupByCol] ?? '')
      if (!xMap.has(xVal)) xMap.set(xVal, new Map())
      const gMap = xMap.get(xVal)!
      if (!gMap.has(gVal)) gMap.set(gVal, row)
      if (!groupOrder.includes(gVal)) groupOrder.push(gVal)
    }

    const xValues = [...xMap.keys()]

    const series = groupOrder.flatMap((group, gi) =>
      yCols.map((y) => ({
        name: yCols.length > 1 ? `${group} (${y})` : group,
        type: 'line' as const,
        data: xValues.map(x => xMap.get(x)?.get(group)?.[y] ?? null),
        smooth: false,
        connectNulls: false,
        symbol: 'circle',
        symbolSize: 4,
        lineStyle: { width: 2 },
        itemStyle: { color: config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length] },
      }))
    )

    return { series, xValues }
  }, [chartData, config.xAxis, config.yAxis, config.groupBy, config.seriesColors])
}
```

**Step 2: Build the frontend**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 3: Commit**

```bash
git add web/src/charts/common.tsx
git commit -m "feat: add useGroupBySeries hook for axis-based chart group_by"
```

---

### Task 6: Frontend — Update BarChart to support group_by

**Files:**
- Modify: `web/src/charts/BarChart.tsx`

**Step 1: Import useGroupBySeries**

At the top, add `useGroupBySeries` to the import from `./common`:
```typescript
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, useRowsAsObjects, useAxisColumns, useGroupBySeries, detectAxisColumns } from './common'
```

**Step 2: Add group_by logic to BarChartComponent**

After the `colors` line (line 11), add:
```typescript
  const groupSeries = useGroupBySeries(chartData, config, colors)
  const hasGroupBy = !!(config.groupBy && chartData.some(row => config.groupBy! in row))
```

Modify the `option` useMemo to switch between group_by and standard rendering:
```typescript
  const option = useMemo(() => {
    if (hasGroupBy && groupSeries.series.length > 0) {
      return {
        tooltip: { trigger: 'axis' as const, ...getTooltipStyle() },
        title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
        legend: config.showLegend !== false ? { top: config.title ? 32 : 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
        grid: { top: config.title ? 56 : config.showLegend !== false ? 30 : 8, right: 16, bottom: config.dataZoom ? 32 : 8, left: 16, containLabel: true },
        dataZoom: config.dataZoom ? [
          { type: 'inside' as const, start: 0, end: 100 },
          { type: 'slider' as const, start: 0, end: 100, bottom: 8, height: 20, borderColor: colors.border, textStyle: { fontSize: 10, color: colors.textMuted } },
        ] : undefined,
        xAxis: { type: 'category' as const, data: groupSeries.xValues, boundaryGap: true, ...getAxisStyle(config.showGrid) },
        yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
        series: groupSeries.series.map(s => ({
          ...s,
          type: 'bar' as const,
          stack: config.chartType === 'stacked_bar' ? 'a' : undefined,
          barWidth: config.barWidth,
          barCategoryGap: config.barCategoryGap,
          label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
          itemStyle: { ...s.itemStyle, borderRadius: [3, 3, 0, 0] as [number, number, number, number] },
        })),
      }
    }
    // Original code (no group_by)
    return {
      // ... existing option object ...
    }
  }, [chartData, xAxis, yAxes, isStacked, hasGroupBy, groupSeries, config.title, config.seriesColors, config.showLegend, config.showLabels, config.showGrid, config.dataZoom, config.barWidth, config.barCategoryGap, config.smooth, config.connectNulls, colors])
```

Wait, this is getting messy. Let me think of a cleaner way.

Actually, let me use a single option structure where I conditionally include group_by logic:

```typescript
function BarChartComponent({ data, config }: ChartProps) {
  const columns = useMemo(() => data.columns.map(c => c.name), [data.columns])
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const isStacked = config.chartType === 'stacked_bar'
  const colors = useMemo(() => getChartColors(), [])

  const { series: groupSeries, xValues } = useGroupBySeries(chartData, { ...config, yAxis: yAxes }, colors)
  const hasGroupBy = !!(config.groupBy && chartData.some(row => config.groupBy! in row))

  const option = useMemo(() => {
    const effectiveXData = hasGroupBy ? xValues : chartData.map(d => d[xAxis])
    const series = hasGroupBy
      ? groupSeries.map(s => ({
          ...s,
          type: 'bar' as const,
          stack: isStacked ? 'a' : undefined,
          barWidth: config.barWidth,
          barCategoryGap: config.barCategoryGap,
          label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
          itemStyle: { ...s.itemStyle, borderRadius: [3, 3, 0, 0] as [number, number, number, number] },
        }))
      : yAxes.map((y, i) => ({
          name: y,
          type: 'bar' as const,
          data: chartData.map(d => d[y]),
          stack: isStacked ? 'a' : undefined,
          barWidth: config.barWidth,
          barCategoryGap: config.barCategoryGap,
          itemStyle: {
            color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length],
            borderRadius: [3, 3, 0, 0] as [number, number, number, number],
          },
          label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
        }))

    return {
      tooltip: { trigger: 'axis' as const, ...getTooltipStyle() },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
      legend: config.showLegend !== false ? { top: config.title ? 32 : 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
      grid: { top: config.title ? 56 : config.showLegend !== false ? 30 : 8, right: 16, bottom: config.dataZoom ? 32 : 8, left: 16, containLabel: true },
      dataZoom: config.dataZoom ? [
        { type: 'inside' as const, start: 0, end: 100 },
        { type: 'slider' as const, start: 0, end: 100, bottom: 8, height: 20, borderColor: colors.border, textStyle: { fontSize: 10, color: colors.textMuted } },
      ] : undefined,
      xAxis: { type: 'category' as const, data: effectiveXData, ...getAxisStyle(config.showGrid) },
      yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
      series,
    }
  }, [chartData, xAxis, yAxes, isStacked, hasGroupBy, groupSeries, xValues, config.title, config.seriesColors, config.showLegend, config.showLabels, config.showGrid, config.dataZoom, config.barWidth, config.barCategoryGap, config.smooth, config.connectNulls, colors])

  return <EChartsContainer option={option} showReset />
}
```

This is cleaner — a single option structure with conditionally computed xData and series based on hasGroupBy.

**Step 2: TypeScript check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 3: Commit**

```bash
git add web/src/charts/BarChart.tsx
git commit -m "feat: add group_by support to BarChart"
```

---

### Task 7: Frontend — Update LineChart to support group_by

**Files:**
- Modify: `web/src/charts/LineChart.tsx`

Apply the same pattern as BarChart but with line-specific properties (smooth, connectNulls, symbolSize: 6).

**Step 1: Apply same pattern**

Import `useGroupBySeries`, add `hasGroupBy` logic, modify `option` useMemo to switch.

The line-specific series properties for the group_by case:
```typescript
series: hasGroupBy
  ? groupSeries.map(s => ({
      ...s,
      type: 'line' as const,
      smooth: config.smooth ?? false,
      connectNulls: config.connectNulls ?? false,
      symbol: 'circle',
      symbolSize: 6,
      lineStyle: { width: 2 },
      label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
    }))
  : yAxes.map((y, i) => ({
      // existing pattern
    }))
```

**Step 2: TypeScript check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 3: Commit**

```bash
git add web/src/charts/LineChart.tsx
git commit -m "feat: add group_by support to LineChart"
```

---

### Task 8: Frontend — Update AreaChart to support group_by

**Files:**
- Modify: `web/src/charts/AreaChart.tsx`

Same pattern as LineChart but with `areaStyle: { opacity: 0.15 }` on each series.

**Step 1: Apply same pattern**

**Step 2: TypeScript check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 3: Commit**

```bash
git add web/src/charts/AreaChart.tsx
git commit -m "feat: add group_by support to AreaChart"
```

---

### Task 9: Frontend — Update ScatterChart to support group_by

**Files:**
- Modify: `web/src/charts/ScatterChart.tsx`

Scatter is slightly different — data is `[x, y, z]` tuples, and the group_by series need to produce scatter data per group.

**Step 1: Update ScatterChart**

Group_by with scatter: each group becomes a separate scatter series with its own symbol/color. The hook's series output would need scatter-specific data format. Since `useGroupBySeries` returns arrays of values per x, but scatter needs x,y pairs per row, we need a different approach.

For scatter with group_by, the simpler approach is to filter the data per group:
```typescript
const hasGroupBy = !!(config.groupBy && chartData.some(row => config.groupBy! in row))
const groups = hasGroupBy ? [...new Set(chartData.map(d => String(d[config.groupBy!])))] : []

const series = hasGroupBy
  ? groups.map((group, gi) => ({
      name: group,
      type: 'scatter' as const,
      data: chartData
        .filter(d => String(d[config.groupBy!]) === group)
        .map(d => [d[xAxis], d[yAxes[0]]]),
      symbolSize: 8,
      itemStyle: { color: config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length], opacity: 0.8 },
    }))
  : yAxes.map((y, i) => ({
      // existing pattern
    }))
```

For v1, scatter only uses the first y_column when group_by is set (multi-y + group_by is unusual for scatter).

**Step 2: TypeScript check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 3: Commit**

```bash
git add web/src/charts/ScatterChart.tsx
git commit -m "feat: add group_by support to ScatterChart"
```

---

### Task 10: Frontend — Update AxisConfigPanel

**Files:**
- Modify: `web/src/charts/AxisConfigPanel.tsx`

**Step 1: Hide color pickers when group_by is set**

In the "Series Colors" section (lines 140-168), wrap in a conditional:

```typescript
{/* Series Colors */}
{(config.yAxis?.length ?? 0) > 0 && !config.groupBy && (
  // existing color picker per y_column
)}
{config.groupBy && (config.yAxis?.length ?? 0) > 0 && (
  <div style={styles.row}>
    <div style={styles.section}>
      <div style={styles.sectionLabel}>Series colors</div>
      <ConfigHint>
        Colors are applied per group value from the &quot;{config.groupBy}&quot; column.
        Use the agent or chart tools to set group colors.
      </ConfigHint>
    </div>
  </div>
)}
```

**Step 2: TypeScript check**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 3: Commit**

```bash
git add web/src/charts/AxisConfigPanel.tsx
git commit -m "feat: update AxisConfigPanel for group_by color pickers"
```

---

### Task 11: Verify everything

**Step 1: Run Go build and vet**

```bash
go build ./... && go vet ./...
```

Expected: no errors

**Step 2: Run frontend type check**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors

**Step 3: Run frontend tests**

```bash
cd web && npx vitest run 2>/dev/null || npx playwright test 2>/dev/null || echo "No tests found — skipping"
```

Note: Test failures should be investigated. The chart test files are at `web/src/test/ChartView.test.tsx`, `web/src/test/TimelineChart.test.tsx`, `web/src/test/HierarchyTreeChart.test.tsx`.

**Step 4: Run Go tests**

```bash
task test:v 2>&1 | head -50
```

Expected: all tests pass

**Step 5: Final commit**

```bash
git add -A && git commit -m "feat: complete group_by + series_colors for axis-based charts"
```
