# Charting System Overhaul — Design

**Date:** 2026-06-12
**Status:** Approved

## Problem

The current charting system (recharts, 346-line monolithic ChartView) is limited to basic chart types and cannot support security analyst use cases like event timelines and hierarchy trees. Adding new chart types requires modifying a large switch statement. The config panel is one-size-fits-all regardless of chart type.

## Goals

- **Extensibility:** Adding a new chart type = adding one file, no changes to existing code
- **Beauty:** Professional, polished charts using ECharts (already installed but unused)
- **Performance:** Canvas-based rendering handles 10K+ rows (vs recharts SVG struggling at 1-2K)
- **Contextual config:** Each chart type shows only its relevant options
- **Security analyst support:** Timeline and hierarchy tree chart types

## Decision: Drop recharts, standardize on ECharts

| Criteria | recharts | ECharts |
|---|---|---|
| Chart types | 7 (bar, line, area, scatter, pie + stacked/donut variants) | 30+ including timeline, tree, sunburst, sankey, heatmap |
| Rendering | SVG (slows at 1-2K rows) | Canvas (handles 10K+ rows) |
| Themes | Manual CSS | Built-in theming engine |
| Timeline support | None (custom D3 needed) | Native `timeline` component |
| Tree/graph support | None | Native `tree` and `graph` series |
| Bundle | ~200KB | ~800KB full, ~200-300KB gzipped with tree-shaking |

ECharts is the industry standard for analytics dashboards (Grafana, Kibana, Apache Superset).

## Architecture: Chart Registry

### File structure

```
web/src/charts/
  index.ts              ← registry + ChartView orchestrator
  types.ts              ← shared types (ChartConfig, ChartModule, ChartProps)
  BarChart.tsx          ← bar chart module
  LineChart.tsx         ← line chart module
  AreaChart.tsx         ← area chart module
  ScatterChart.tsx      ← scatter chart module
  PieChart.tsx          ← pie/donut chart module
  TimelineChart.tsx     ← NEW: security event timeline
  HierarchyTreeChart.tsx ← NEW: parent-child hierarchy tree
  common.tsx            ← shared ECharts wrapper, theme, tooltip styles
```

### ChartModule interface

Each chart module exports a consistent interface:

```ts
interface ChartModule {
  /** ECharts chart component */
  Component: React.FC<ChartProps>
  /** Contextual config panel — only shows relevant options */
  ConfigPanel: React.FC<ConfigPanelProps>
  /** Default config for this chart type */
  defaultConfig: Partial<ChartConfig>
  /** Auto-detect columns from ResultSet */
  detectColumns: (columns: Column[], rows: unknown[][]) => Partial<ChartConfig>
  /** Column requirements for validation */
  requirements: {
    minColumns: number
    needsTime?: boolean
    needsParentChild?: boolean
  }
}
```

### Registry (`index.ts`)

The registry maps chart type names to modules:

```ts
const CHART_MODULES: Record<string, ChartModule> = {
  bar: BarChartModule,
  stacked_bar: BarChartModule,
  line: LineChartModule,
  area: AreaChartModule,
  scatter: ScatterChartModule,
  pie: PieChartModule,
  donut: PieChartModule,
  timeline: TimelineModule,
  hierarchy_tree: HierarchyTreeModule,
}
```

The main `ChartView` orchestrator:
- Receives data (ResultSet) + config (ChartConfig)
- Looks up `ChartModule` by `chartType`
- Renders `<Module.Component data={...} config={...} />`
- Renders `<Module.ConfigPanel config={...} columns={...} onChange={...} />`
- Handles data normalization, persistence, table/chart toggle

### Shared ECharts wrapper (`common.tsx`)

```ts
function EChartsContainer({ option, height }: { option: EChartsOption; height?: number }) {
  // Renders <ReactEChartsCore /> with theme, responsive sizing
  // Handles dark/light mode via CSS variables
  // Memoizes option to prevent unnecessary re-renders
}
```

Shared theme constants:
- Color palette (matching existing DEFAULT_COLORS)
- Tooltip styling (dark card with border)
- Axis styling (minimal, no axis lines, muted tick labels)
- Font sizes consistent with the app

## Chart Types

### Existing (rewritten on ECharts)

| Type | ECharts config | Notes |
|---|---|---|
| bar | `{ series: [{ type: 'bar' }] }` | |
| stacked_bar | `{ series: [{ type: 'bar', stack: 'a' }] }` | |
| line | `{ series: [{ type: 'line' }] }` | |
| area | `{ series: [{ type: 'line', areaStyle: {} }] }` | |
| scatter | `{ series: [{ type: 'scatter' }] }` | |
| pie | `{ series: [{ type: 'pie' }] }` | |
| donut | `{ series: [{ type: 'pie', radius: ['40%', '70%'] }] }` | |

### NEW: Timeline

**Purpose:** Plot events on a time axis. Shows when things happened.

**Auto-detection logic:**
1. Scan columns for time-like types (Date, DateTime, timestamp, time, ts) or parse sample values
2. If ≥2 time columns → range mode (start/end bars)
3. If 1 time column → event mode (dots on time axis)
4. First text/string column → auto-selected as label

**Data shape examples:**

Point-in-time events:
```sql
SELECT timestamp, event_type, message FROM audit_log
-- → time: timestamp, label: message
```

Range-based (Gantt):
```sql
SELECT process_name, start_time, end_time, status FROM processes
-- → time: start_time, end_time: end_time, label: process_name
```

**Contextual config panel:**

```
┌─ Timeline Config ──────────────────┐
│ Time column        [timestamp   ▾] │
│ End time column    [end_time    ▾] │  ← only shown if 2+ time cols detected
│ Label column       [message     ▾] │
│ Group by           [event_type  ▾] │  ← optional, groups events into lanes
└────────────────────────────────────┘
```

**Rendering:**
- xAxis: time axis with smart tick formatting
- Events: colored scatter dots on time axis (event mode)
- Ranges: horizontal bars spanning start→end (range mode)
- Group by: separate y-axis lanes per group
- Tooltip: shows all fields on hover
- Zoom: built-in dataZoom slider for navigating large time ranges

### NEW: Hierarchy Tree

**Purpose:** Visualize parent-child relationships. Works for processes, org charts, file systems, network topology, dependency trees, etc.

**Auto-detection logic:**
1. Find numeric/ID columns where one column's values are a subset of another's values → that's the child (id), the other is the parent (parent_id)
2. First text column → auto-selected as label
3. Remaining numeric columns → available as metrics

**Data shape example:**

```sql
SELECT pid, ppid, name, cpu, memory FROM processes
-- → id: pid, parent_id: ppid, label: name, metrics: [cpu, memory]
```

```sql
SELECT id, parent_id, name, budget FROM org_units
-- → id: id, parent_id: parent_id, label: name, metrics: [budget]
```

**Contextual config panel:**

```
┌─ Hierarchy Tree Config ────────────┐
│ ID column          [pid         ▾] │
│ Parent ID column   [ppid        ▾] │
│ Label column       [name        ▾] │
│ Metrics            [☑cpu ☐mem]    │  ← multi-select, shown as sub-labels
│ Layout             [Top-down   ▾] │  ← Top-down or Left-to-right
└────────────────────────────────────┘
```

**Rendering:**
- ECharts `tree` series
- Nodes show label + optional metrics (smaller text)
- Expandable/collapsible branches (click to toggle)
- Smooth animation on expand/collapse
- Tooltip on hover with full details
- Layout: top-down (default) or left-to-right
- Leaf nodes with metrics get subtle color coding

## Config Persistence

Same as current — stored in cell `metadata.chart` JSON via agent tools, or localStorage as manual fallback.

```json
{
  "chartType": "timeline",
  "timeColumn": "timestamp",
  "endTimeColumn": null,
  "labelColumn": "message",
  "groupBy": "event_type"
}
```

```json
{
  "chartType": "hierarchy_tree",
  "idColumn": "pid",
  "parentIdColumn": "ppid",
  "labelColumn": "name",
  "metricColumns": ["cpu", "memory"],
  "layout": "top-down"
}
```

## Agent Tool Updates (Go backend)

### `create_chart` tool

Extend `chart_type` enum:
```json
"enum": ["bar","stacked_bar","line","area","scatter","pie","donut","timeline","hierarchy_tree"]
```

Add optional params:
```json
{
  "time_column": { "type": "string" },
  "end_time_column": { "type": "string" },
  "label_column": { "type": "string" },
  "group_by": { "type": "string" },
  "id_column": { "type": "string" },
  "parent_id_column": { "type": "string" },
  "metric_columns": { "type": "array", "items": { "type": "string" } },
  "layout": { "type": "string", "enum": ["top-down", "left-to-right"] }
}
```

### `update_chart` tool

Same new params, all optional (merge with existing config).

## Bundle Optimization

Tree-shake ECharts to load only what's needed:

```ts
import * as echarts from 'echarts/core'
import { BarChart, LineChart, ScatterChart, PieChart, TreeChart } from 'echarts/charts'
import {
  GridComponent, TooltipComponent, LegendComponent,
  DataZoomComponent, ToolboxComponent
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
```

Estimated gzipped: ~200-300KB (comparable to recharts current cost).

## Migration Path

1. Build new `charts/` module alongside existing ChartView
2. Wire OutputRenderer to use new ChartView from registry
3. Update agent tools on Go side (enum + new params)
4. Remove recharts dependency
5. Update tests (ChartView.test.tsx, stories)

## Testing

- **Unit:** Each chart module tested with mock data + config panel interaction
- **Visual:** Storybook stories per chart type
- **E2E:** Agent creates timeline/hierarchy_tree chart → renders correctly in notebook
- **Performance:** Test with 10K+ row datasets, verify smooth rendering

## Files to modify

### New files
- `web/src/charts/index.ts`
- `web/src/charts/types.ts`
- `web/src/charts/common.tsx`
- `web/src/charts/BarChart.tsx`
- `web/src/charts/LineChart.tsx`
- `web/src/charts/AreaChart.tsx`
- `web/src/charts/ScatterChart.tsx`
- `web/src/charts/PieChart.tsx`
- `web/src/charts/TimelineChart.tsx`
- `web/src/charts/HierarchyTreeChart.tsx`

### Modified files
- `web/src/components/OutputRenderer.tsx` — swap ChartView import
- `web/src/types/index.ts` — update ChartConfig type
- `web/package.json` — remove recharts dependency
- `internal/agent/tools_chart.go` — extend enum + new params
- `internal/agent/engine.go` — update chart context hint

### Removed files
- `web/src/components/ChartView.tsx` — replaced by `charts/index.tsx`
- `web/src/components/ChartConfigPanel.tsx` — replaced by per-module config panels
