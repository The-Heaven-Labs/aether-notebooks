# Charting System Overhaul — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace recharts with ECharts, build a chart registry with per-type modules, and add timeline + hierarchy tree chart types for security analyst use cases.

**Architecture:** Self-contained chart modules under `web/src/charts/`, each exporting a component, config panel, defaults, and column detection. A registry orchestrator looks up modules by chart type. ECharts with tree-shaking for performance.

**Tech Stack:** ECharts 6 (tree-shaken), React 19, TypeScript, echarts-for-react, Storybook, Vitest

**Design doc:** `docs/plans/2026-06-12-charting-overhaul-design.md`

---

## Task 1: Types and ECharts Setup

**Files:**
- Create: `web/src/charts/types.ts`
- Create: `web/src/charts/common.tsx`

### Step 1: Create shared chart types

```ts
// web/src/charts/types.ts
import type React from 'react'
import type { ResultSet } from '../types'

export type ChartType =
  | 'bar' | 'stacked_bar' | 'line' | 'area' | 'scatter'
  | 'pie' | 'donut'
  | 'timeline'
  | 'hierarchy_tree'

export interface ChartConfig {
  chartType: ChartType
  // Axis-based charts (bar, line, area, scatter)
  xAxis?: string
  yAxis?: string[]
  // Timeline
  timeColumn?: string
  endTimeColumn?: string
  labelColumn?: string
  groupBy?: string
  // Hierarchy tree
  idColumn?: string
  parentIdColumn?: string
  metricColumns?: string[]
  layout?: 'top-down' | 'left-to-right'
  // Shared
  title?: string
  showLegend?: boolean
  showGrid?: boolean
  showLabels?: boolean
  skipEmpty?: boolean
  seriesColors?: Record<string, string>
}

export interface ChartProps {
  data: ResultSet
  config: ChartConfig
  height?: number
}

export interface ConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
}

export interface ChartModule {
  Component: React.FC<ChartProps>
  ConfigPanel: React.FC<ConfigPanelProps>
  defaultConfig: Partial<ChartConfig>
  detectColumns: (columns: ResultSet['columns'], rows: ResultSet['rows']) => Partial<ChartConfig>
  requirements: { minColumns: number; needsTime?: boolean; needsParentChild?: boolean }
}
```

### Step 2: Create ECharts wrapper with tree-shaking and theme

```tsx
// web/src/charts/common.tsx
import { memo, useMemo } from 'react'
import ReactEChartsCore from 'echarts-for-react/lib/core'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, ScatterChart, PieChart, TreeChart } from 'echarts/charts'
import {
  GridComponent, TooltipComponent, LegendComponent,
  DataZoomComponent, ToolboxComponent
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

// Register only what we use
echarts.use([
  BarChart, LineChart, ScatterChart, PieChart, TreeChart,
  GridComponent, TooltipComponent, LegendComponent,
  DataZoomComponent, ToolboxComponent,
  CanvasRenderer,
])

export const CHART_COLORS = [
  '#6366f1', '#06b6d4', '#10b981', '#f59e0b',
  '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16',
]

export const tooltipStyle = {
  backgroundColor: 'var(--bg-card)',
  borderColor: 'var(--border)',
  borderRadius: 4,
  textStyle: { fontSize: 12, color: 'var(--text-primary)' },
  extraCssText: 'box-shadow: var(--shadow-md);',
}

export const axisStyle = {
  axisLine: { show: false },
  axisTick: { show: false },
  axisLabel: { fontSize: 11, color: 'var(--text-muted)' },
  splitLine: { lineStyle: { color: 'var(--border)', type: 'dashed' as const } },
}

interface EChartsContainerProps {
  option: echarts.EChartsOption
  height?: number
}

export const EChartsContainer = memo(function EChartsContainer({ option, height = 300 }: EChartsContainerProps) {
  return (
    <ReactEChartsCore
      echarts={echarts}
      option={option}
      style={{ height, width: '100%' }}
      notMerge
      lazyUpdate
    />
  )
})
```

### Step 3: Verify it compiles

Run: `cd web && npx tsc --noEmit`
Expected: No errors

### Step 4: Commit

```bash
cd /home/jesus/Projects/hnb-claude
git add web/src/charts/types.ts web/src/charts/common.tsx
git commit -m "charts: add shared types and ECharts tree-shaking setup"
```

---

## Task 2: Chart Registry Orchestrator

**Files:**
- Create: `web/src/charts/index.tsx`

### Step 1: Create the registry and ChartView orchestrator

```tsx
// web/src/charts/index.tsx
import { useState, useMemo } from 'react'
import type React from 'react'
import { Settings2 } from 'lucide-react'
import type { ResultSet } from '../types'
import type { ChartConfig, ChartModule, ConfigPanelProps } from './types'
import { BarChartModule } from './BarChart'
import { LineChartModule } from './LineChart'
import { AreaChartModule } from './AreaChart'
import { ScatterChartModule } from './ScatterChart'
import { PieChartModule } from './PieChart'
import { TimelineModule } from './TimelineChart'
import { HierarchyTreeModule } from './HierarchyTreeChart'

// Registry
export const CHART_MODULES: Record<string, ChartModule> = {
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

export const ALL_CHART_TYPES = [
  { value: 'bar', label: 'Bar', symbol: '▊▊' },
  { value: 'stacked_bar', label: 'Stack', symbol: '▊≡' },
  { value: 'line', label: 'Line', symbol: '╱╲' },
  { value: 'area', label: 'Area', symbol: '▓' },
  { value: 'scatter', label: 'Scatter', symbol: '·:' },
  { value: 'pie', label: 'Pie', symbol: '◕' },
  { value: 'donut', label: 'Donut', symbol: '◎' },
  { value: 'timeline', label: 'Timeline', symbol: '⏱' },
  { value: 'hierarchy_tree', label: 'Tree', symbol: '🌲' },
] as const

interface ChartViewProps {
  output?: { type: string; data?: ResultSet; config?: Partial<ChartConfig> }
  rs?: ResultSet
  onConfigChange?: (config: ChartConfig) => void
  cellId?: string
}

export function ChartView({ output, rs, onConfigChange, cellId }: ChartViewProps) {
  const data = output?.data ?? (rs ? { columns: rs.columns, rows: rs.rows } : undefined)
  const columns = data?.columns?.map(c => c.name) ?? []

  const cfg = (output?.config ?? {}) as ChartConfig
  const chartType = cfg.chartType ?? 'bar'

  // Load saved config from localStorage (legacy mode)
  const savedConfig = useMemo(() => {
    if (!onConfigChange && cellId) {
      try {
        const saved = localStorage.getItem(`hnb_chart_config_${cellId}`)
        return saved ? (JSON.parse(saved) as ChartConfig) : null
      } catch { /* ignore */ }
    }
    return null
  }, [cellId, onConfigChange])

  const [showConfig, setShowConfig] = useState(false)

  const effectiveConfig: ChartConfig = onConfigChange
    ? cfg
    : { ...cfg, ...savedConfig }

  const handleConfigChange = (newCfg: ChartConfig) => {
    if (onConfigChange) {
      onConfigChange(newCfg)
    } else if (cellId) {
      try {
        localStorage.setItem(`hnb_chart_config_${cellId}`, JSON.stringify(newCfg))
      } catch { /* ignore */ }
      // Force re-render for legacy mode
      setShowConfig(v => v)
    }
  }

  const mod = CHART_MODULES[chartType]
  if (!mod) {
    return <div style={{ padding: 16, color: 'var(--text-muted)' }}>Unknown chart type: {chartType}</div>
  }

  if (!data || columns.length < (mod.requirements.minColumns ?? 2)) {
    return (
      <div style={styles.emptyGuidance}>
        <div style={styles.emptyIcon}>📊</div>
        <p style={styles.emptyTitle}>Not enough data to chart</p>
        <p style={styles.emptyText}>
          This chart needs at least {mod.requirements.minColumns} column(s).
          Run a query that returns the right data, then switch to chart view.
        </p>
      </div>
    )
  }

  return (
    <div style={styles.wrap}>
      <div data-testid="chart-container">
        <mod.Component data={data} config={effectiveConfig} />
      </div>
      <div>
        <button
          style={styles.configBtn}
          onClick={() => setShowConfig(v => !v)}
          aria-label={showConfig ? 'Close chart config' : 'Configure chart'}
        >
          <Settings2 size={13} />
          {showConfig ? ' Close' : ' Configure'}
        </button>
        {showConfig && (
          <mod.ConfigPanel config={effectiveConfig} columns={columns} onChange={handleConfigChange} />
        )}
      </div>
    </div>
  )
}

// Re-export types and defaults
export type { ChartConfig, ChartModule, ChartType } from './types'
export { CHART_COLORS, tooltipStyle, axisStyle, EChartsContainer } from './common'
```

### Step 2: Verify it compiles (chart modules don't exist yet — stub them temporarily)

Run: `cd web && npx tsc --noEmit`
Expected: Errors about missing modules — that's fine, we'll create them next.

### Step 3: Commit

```bash
git add web/src/charts/index.tsx
git commit -m "charts: add registry orchestrator and ChartView"
```

---

## Task 3: Axis-Based Chart Modules (Bar, Line, Area, Scatter, Pie)

**Files:**
- Create: `web/src/charts/BarChart.tsx`
- Create: `web/src/charts/LineChart.tsx`
- Create: `web/src/charts/AreaChart.tsx`
- Create: `web/src/charts/ScatterChart.tsx`
- Create: `web/src/charts/PieChart.tsx`

### Step 1: Create a shared axis-based config panel

Each axis-based chart (bar, line, area, scatter) uses the same panel pattern. Create it once and share.

```tsx
// web/src/charts/AxisConfigPanel.tsx (shared by bar, line, area, scatter)
import type React from 'react'
import type { ChartConfig } from './types'

interface AxisConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
  showStack?: boolean
}

export function AxisConfigPanel({ config, columns, onChange, showStack }: AxisConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>X axis</div>
          <select
            aria-label="X axis"
            style={styles.select}
            value={config.xAxis ?? ''}
            onChange={e => onChange({ ...config, xAxis: e.target.value })}
          >
            {columns.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Y axis <span style={{ fontWeight: 400, textTransform: 'none' }}>(Ctrl+click multi)</span></div>
          <select
            aria-label="Y axis"
            style={{ ...styles.select, minHeight: 56 }}
            multiple
            value={config.yAxis ?? []}
            onChange={e => {
              const selected = Array.from(e.target.selectedOptions).map(o => o.value)
              onChange({ ...config, yAxis: selected })
            }}
          >
            {columns.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        </div>
      </div>
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Title</div>
          <input
            aria-label="Chart title"
            style={styles.input}
            value={config.title ?? ''}
            placeholder="Optional title"
            onChange={e => onChange({ ...config, title: e.target.value })}
          />
        </div>
        {showStack && (
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Stack</div>
            <select
              aria-label="Stack"
              style={styles.select}
              value={config.chartType === 'stacked_bar' ? 'yes' : 'no'}
              onChange={e => onChange({ ...config, chartType: e.target.value === 'yes' ? 'stacked_bar' : 'bar' })}
            >
              <option value="no">No</option>
              <option value="yes">Yes</option>
            </select>
          </div>
        )}
      </div>
      <div style={styles.row}>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showLegend ?? true}
            onChange={e => onChange({ ...config, showLegend: e.target.checked })}
          />
          Legend
        </label>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showGrid ?? true}
            onChange={e => onChange({ ...config, showGrid: e.target.checked })}
          />
          Grid
        </label>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showLabels ?? false}
            onChange={e => onChange({ ...config, showLabels: e.target.checked })}
          />
          Labels
        </label>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  row: { display: 'flex', gap: 10 },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  input: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
}
```

### Step 2: Create BarChart module

```tsx
// web/src/charts/BarChart.tsx
import type React from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle, axisStyle } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function BarChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const xAxis = config.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length ? config.yAxis : columns.slice(1, 2)
  const isStacked = config.chartType === 'stacked_bar'

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const option = {
    tooltip: { trigger: 'axis' as const, ...tooltipStyle },
    legend: config.showLegend !== false ? { top: 0, textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    grid: { top: config.showLegend !== false ? 30 : 8, right: 16, bottom: 8, left: 0, containLabel: true },
    xAxis: { type: 'category' as const, data: chartData.map(d => d[xAxis]), ...axisStyle },
    yAxis: { type: 'value' as const, ...axisStyle },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'bar' as const,
      data: chartData.map(d => d[y]),
      stack: isStacked ? 'a' : undefined,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length], borderRadius: isStacked && i === yAxes.length - 1 ? [3, 3, 0, 0] : [3, 3, 0, 0] },
      label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: 'var(--text-muted)' } : undefined,
      animation: false,
    })),
  }

  return <EChartsContainer option={option} />
}

function BarConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} showStack />
}

export const BarChartModule: ChartModule = {
  Component: BarChartComponent,
  ConfigPanel: BarConfigPanel,
  defaultConfig: { chartType: 'bar', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => ({ xAxis: columns[0]?.name, yAxis: columns.slice(1, 2).map(c => c.name) }),
  requirements: { minColumns: 2 },
}
```

### Step 3: Create LineChart module

```tsx
// web/src/charts/LineChart.tsx
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle, axisStyle } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function LineChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const xAxis = config.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length ? config.yAxis : columns.slice(1, 2)

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const option = {
    tooltip: { trigger: 'axis' as const, ...tooltipStyle },
    legend: config.showLegend !== false ? { top: 0, textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    grid: { top: config.showLegend !== false ? 30 : 8, right: 16, bottom: 8, left: 0, containLabel: true },
    xAxis: { type: 'category' as const, data: chartData.map(d => d[xAxis]), ...axisStyle },
    yAxis: { type: 'value' as const, ...axisStyle },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'line' as const,
      data: chartData.map(d => d[y]),
      smooth: false,
      symbol: 'circle',
      symbolSize: 6,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length] },
      lineStyle: { width: 2 },
      label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: 'var(--text-muted)' } : undefined,
      animation: false,
    })),
  }

  return <EChartsContainer option={option} />
}

function LineConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} />
}

export const LineChartModule: ChartModule = {
  Component: LineChartComponent,
  ConfigPanel: LineConfigPanel,
  defaultConfig: { chartType: 'line', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => ({ xAxis: columns[0]?.name, yAxis: columns.slice(1, 2).map(c => c.name) }),
  requirements: { minColumns: 2 },
}
```

### Step 4: Create AreaChart module

```tsx
// web/src/charts/AreaChart.tsx
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle, axisStyle } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function AreaChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const xAxis = config.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length ? config.yAxis : columns.slice(1, 2)

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const option = {
    tooltip: { trigger: 'axis' as const, ...tooltipStyle },
    legend: config.showLegend !== false ? { top: 0, textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    grid: { top: config.showLegend !== false ? 30 : 8, right: 16, bottom: 8, left: 0, containLabel: true },
    xAxis: { type: 'category' as const, data: chartData.map(d => d[xAxis]), boundaryGap: false, ...axisStyle },
    yAxis: { type: 'value' as const, ...axisStyle },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'line' as const,
      data: chartData.map(d => d[y]),
      smooth: false,
      areaStyle: { opacity: 0.15 },
      symbol: 'none',
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length] },
      lineStyle: { width: 2 },
      animation: false,
    })),
  }

  return <EChartsContainer option={option} />
}

function AreaConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} />
}

export const AreaChartModule: ChartModule = {
  Component: AreaChartComponent,
  ConfigPanel: AreaConfigPanel,
  defaultConfig: { chartType: 'area', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => ({ xAxis: columns[0]?.name, yAxis: columns.slice(1, 2).map(c => c.name) }),
  requirements: { minColumns: 2 },
}
```

### Step 5: Create ScatterChart module

```tsx
// web/src/charts/ScatterChart.tsx
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle, axisStyle } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function ScatterChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const xAxis = config.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length ? config.yAxis : columns.slice(1, 2)

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const option = {
    tooltip: { ...tooltipStyle },
    legend: config.showLegend !== false && yAxes.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    grid: { top: config.showLegend !== false && yAxes.length > 1 ? 30 : 8, right: 16, bottom: 8, left: 0, containLabel: true },
    xAxis: { type: 'value' as const, name: xAxis, ...axisStyle },
    yAxis: { type: 'value' as const, ...axisStyle },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'scatter' as const,
      data: chartData.map(d => [d[xAxis], d[y]]),
      symbolSize: 8,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length], opacity: 0.8 },
      animation: false,
    })),
  }

  return <EChartsContainer option={option} />
}

function ScatterConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} />
}

export const ScatterChartModule: ChartModule = {
  Component: ScatterChartComponent,
  ConfigPanel: ScatterConfigPanel,
  defaultConfig: { chartType: 'scatter', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => ({ xAxis: columns[0]?.name, yAxis: columns.slice(1, 2).map(c => c.name) }),
  requirements: { minColumns: 2 },
}
```

### Step 6: Create PieChart module (handles both pie and donut)

```tsx
// web/src/charts/PieChart.tsx
import type React from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle } from './common'

function PieChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const xAxis = config.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length ? config.yAxis : columns.slice(1, 2)
  const valueKey = yAxes[0] ?? columns[1] ?? ''
  const isDonut = config.chartType === 'donut'

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const option = {
    tooltip: { trigger: 'item' as const, ...tooltipStyle, formatter: '{b}: {c} ({d}%)' },
    legend: config.showLegend !== false ? { orient: 'vertical' as const, right: 10, top: 'center', textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    series: [{
      type: 'pie' as const,
      radius: isDonut ? ['40%', '70%'] : ['0%', '70%'],
      center: config.showLegend !== false ? ['40%', '50%'] : ['50%', '50%'],
      data: chartData.map((d, i) => ({
        name: d[xAxis],
        value: d[valueKey],
        itemStyle: { color: config.seriesColors?.[String(d[xAxis])] ?? CHART_COLORS[i % CHART_COLORS.length] },
      })),
      label: config.showLabels !== false ? { fontSize: 11, color: 'var(--text-primary)' } : { show: false },
      emphasis: { itemStyle: { shadowBlur: 10, shadowColor: 'rgba(0,0,0,0.2)' } },
      animation: false,
    }],
  }

  return <EChartsContainer option={option} />
}

function PieConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Name column</div>
        <select
          aria-label="Name column"
          style={styles.select}
          value={config.xAxis ?? ''}
          onChange={e => onChange({ ...config, xAxis: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Value column</div>
        <select
          aria-label="Value column"
          style={styles.select}
          value={config.yAxis?.[0] ?? ''}
          onChange={e => onChange({ ...config, yAxis: [e.target.value] })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <label style={styles.checkbox}>
        <input
          type="checkbox"
          checked={config.chartType === 'donut'}
          onChange={e => onChange({ ...config, chartType: e.target.checked ? 'donut' : 'pie' })}
        />
        Donut (ring)
      </label>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  section: { display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
}

export const PieChartModule: ChartModule = {
  Component: PieChartComponent,
  ConfigPanel: PieConfigPanel,
  defaultConfig: { chartType: 'pie', showLegend: true, showLabels: true, skipEmpty: true },
  detectColumns: (columns) => ({ xAxis: columns[0]?.name, yAxis: columns.slice(1, 2).map(c => c.name) }),
  requirements: { minColumns: 2 },
}
```

### Step 7: Verify all axis-based modules compile

Run: `cd web && npx tsc --noEmit`
Expected: No errors (assuming Task 1-2 files are in place)

### Step 8: Commit

```bash
git add web/src/charts/BarChart.tsx web/src/charts/LineChart.tsx web/src/charts/AreaChart.tsx web/src/charts/ScatterChart.tsx web/src/charts/PieChart.tsx web/src/charts/AxisConfigPanel.tsx
git commit -m "charts: add axis-based chart modules (bar, line, area, scatter, pie)"
```

---

## Task 4: Timeline Chart Module

**Files:**
- Create: `web/src/charts/TimelineChart.tsx`

### Step 1: Create the timeline chart module

This chart detects time-like columns and renders events on a time axis. Supports both point-in-time events and range-based durations.

```tsx
// web/src/charts/TimelineChart.tsx
import type React from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle, axisStyle } from './common'

// Detect time-like columns from column types or sample values
function isTimeType(colType?: string): boolean {
  if (!colType) return false
  const t = colType.toLowerCase()
  return t.includes('date') || t.includes('time') || t.includes('timestamp') || t === 'ts'
}

function detectTimeColumns(columns: { name: string; type?: string }[]): string[] {
  return columns.filter(c => isTimeType(c.type)).map(c => c.name)
}

function TimelineChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const colTypes = data.columns.reduce<Record<string, string>>((acc, c) => { acc[c.name] = c.type ?? ''; return acc }, {})
  const timeCol = config.timeColumn ?? detectTimeColumns(data.columns)[0] ?? columns[0]
  const endTimeCol = config.endTimeColumn
  const labelCol = config.labelColumn
  const groupByCol = config.groupBy

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  }).filter(d => d[timeCol] != null)

  // Sort by time
  chartData.sort((a, b) => new Date(String(a[timeCol])).getTime() - new Date(String(b[timeCol])).getTime())

  // Build groups if groupBy is set
  const groups = groupByCol
    ? [...new Set(chartData.map(d => String(d[groupByCol] ?? 'Unknown')))]
    : ['Events']

  const isRangeMode = !!endTimeCol

  if (isRangeMode) {
    // Gantt-style range bars
    const option = {
      tooltip: { ...tooltipStyle, trigger: 'axis' as const },
      legend: groups.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
      grid: { top: groups.length > 1 ? 30 : 8, right: 16, bottom: 8, left: 0, containLabel: true },
      xAxis: { type: 'time' as const, ...axisStyle },
      yAxis: { type: 'category' as const, data: groups, inverse: true, ...axisStyle },
      dataZoom: [{ type: 'slider' as const, xAxisIndex: 0, bottom: 0, height: 20 }],
      series: groups.map((group, gi) => ({
        name: group,
        type: 'custom' as const,
        renderItem: (params: any, api: any) => {
          const groupIndex = gi
          const startTime = api.value(0)
          const endTime = api.value(1)
          const start = api.coord([startTime, groupIndex])
          const end = api.coord([endTime, groupIndex])
          const height = api.size([0, 1])[1] * 0.6
          return {
            type: 'rect' as const,
            shape: { x: start[0], y: start[1] - height / 2, width: end[0] - start[0], height },
            style: { fill: CHART_COLORS[gi % CHART_COLORS.length], opacity: 0.8 },
          }
        },
        encode: { x: [0, 1], y: 2 },
        data: chartData
          .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
          .map(d => [new Date(String(d[timeCol])).getTime(), new Date(String(d[endTimeCol!])).getTime(), group]),
        animation: false,
      })),
    }
    return <EChartsContainer option={option} height={Math.max(200, groups.length * 40 + 60)} />
  }

  // Point-in-time events
  const option = {
    tooltip: {
      ...tooltipStyle,
      trigger: 'item' as const,
      formatter: (params: any) => {
        const d = params.data
        const time = new Date(d[0]).toLocaleString()
        const label = labelCol ? `<br/>${labelCol}: ${d[2] ?? ''}` : ''
        return `<b>${time}</b>${label}`
      },
    },
    legend: groups.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    grid: { top: groups.length > 1 ? 30 : 8, right: 16, bottom: 30, left: 0, containLabel: true },
    xAxis: { type: 'time' as const, ...axisStyle },
    yAxis: { type: 'category' as const, data: groups, ...axisStyle, show: groups.length > 1 },
    dataZoom: [{ type: 'slider' as const, xAxisIndex: 0, bottom: 0, height: 20 }],
    series: groups.map((group, gi) => ({
      name: group,
      type: 'scatter' as const,
      symbolSize: 10,
      itemStyle: { color: CHART_COLORS[gi % CHART_COLORS.length] },
      data: chartData
        .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
        .map(d => [new Date(String(d[timeCol])).getTime(), group, labelCol ? d[labelCol] : null]),
      animation: false,
    })),
  }

  return <EChartsContainer option={option} />
}

function TimelineConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  const timeCols = columns.filter(c => {
    // Heuristic: column name or type suggests time
    const lower = c.toLowerCase()
    return lower.includes('time') || lower.includes('date') || lower.includes('timestamp') || lower === 'ts'
  })
  const stringCols = columns // All columns available for label/groupBy

  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Time column</div>
        <select
          aria-label="Time column"
          style={styles.select}
          value={config.timeColumn ?? ''}
          onChange={e => onChange({ ...config, timeColumn: e.target.value })}
        >
          {timeCols.length > 0
            ? timeCols.map(c => <option key={c} value={c}>{c}</option>)
            : columns.map(c => <option key={c} value={c}>{c}</option>)
          }
        </select>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>End time column <span style={{ fontWeight: 400, textTransform: 'none' }}>(optional, for ranges)</span></div>
        <select
          aria-label="End time column"
          style={styles.select}
          value={config.endTimeColumn ?? ''}
          onChange={e => onChange({ ...config, endTimeColumn: e.target.value || undefined })}
        >
          <option value="">None (point events)</option>
          {timeCols.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Label column</div>
        <select
          aria-label="Label column"
          style={styles.select}
          value={config.labelColumn ?? ''}
          onChange={e => onChange({ ...config, labelColumn: e.target.value || undefined })}
        >
          <option value="">None</option>
          {stringCols.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Group by</div>
        <select
          aria-label="Group by"
          style={styles.select}
          value={config.groupBy ?? ''}
          onChange={e => onChange({ ...config, groupBy: e.target.value || undefined })}
        >
          <option value="">No grouping</option>
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  section: { display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
}

export const TimelineModule: ChartModule = {
  Component: TimelineChartComponent,
  ConfigPanel: TimelineConfigPanel,
  defaultConfig: { chartType: 'timeline', showLegend: true, showGrid: true },
  detectColumns: (columns) => {
    const timeCols = columns.filter(c => isTimeType(c.type))
    const firstTextCol = columns.find(c => !isTimeType(c.type) && (!c.type || c.type.includes('text') || c.type.includes('varchar')))
    return {
      timeColumn: timeCols[0]?.name,
      endTimeColumn: timeCols[1]?.name,
      labelColumn: firstTextCol?.name,
    }
  },
  requirements: { minColumns: 2, needsTime: true },
}
```

### Step 2: Verify it compiles

Run: `cd web && npx tsc --noEmit`
Expected: No errors

### Step 3: Commit

```bash
git add web/src/charts/TimelineChart.tsx
git commit -m "charts: add timeline chart with auto-detect time columns"
```

---

## Task 5: Hierarchy Tree Chart Module

**Files:**
- Create: `web/src/charts/HierarchyTreeChart.tsx`

### Step 1: Create the hierarchy tree chart module

This chart detects parent-child ID pairs and renders a tree. Configurable layout direction.

```tsx
// web/src/charts/HierarchyTreeChart.tsx
import type React from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle } from './common'

interface TreeNode {
  name: string
  value?: number
  children?: TreeNode[]
  itemStyle?: { color: string }
}

// Detect parent-child ID columns
function detectParentChild(columns: { name: string; type?: string }[], rows: unknown[][]): { idCol: string; parentCol: string } | null {
  // Find numeric/ID columns
  const idCols = columns.filter(c => {
    const t = (c.type ?? '').toLowerCase()
    return t.includes('int') || t.includes('bigint') || t.includes('id') || t.includes('serial')
  })

  if (idCols.length < 2) return null

  // Check which pair has parent-child relationship
  for (let i = 0; i < idCols.length; i++) {
    for (let j = 0; j < idCols.length; j++) {
      if (i === j) continue
      const childValues = new Set(rows.map(r => r[i]))
      const parentValues = new Set(rows.map(r => r[j]))
      // If parent column's values include all child column values, it's likely the parent
      const childInParent = [...childValues].every(v => parentValues.has(v))
      if (childInParent && childValues.size > 0) {
        return { idCol: columns[i].name, parentCol: columns[j].name }
      }
    }
  }

  // Fallback: first two ID columns
  return { idCol: idCols[0].name, parentCol: idCols[1].name }
}

// Build tree from flat rows
function buildTree(
  rows: Record<string, unknown>[],
  idCol: string,
  parentCol: string,
  labelCol: string,
  metricCols: string[],
  colors: Record<string, string>
): TreeNode[] {
  const nodeMap = new Map<string, TreeNode & { parentId: string }>()
  const roots: TreeNode[] = []

  // Create all nodes
  rows.forEach((row, i) => {
    const id = String(row[idCol] ?? `node-${i}`)
    const label = labelCol ? String(row[labelCol] ?? id) : id
    const node: TreeNode & { parentId: string } = {
      name: label,
      parentId: String(row[parentCol] ?? ''),
      itemStyle: { color: colors[label] ?? CHART_COLORS[i % CHART_COLORS.length] },
    }
    if (metricCols.length > 0) {
      const metrics = metricCols.map(c => `${c}: ${row[c]}`).join(', ')
      node.name = `${label}\n${metrics}`
    }
    nodeMap.set(id, node)
  })

  // Build hierarchy
  nodeMap.forEach((node, id) => {
    if (node.parentId && node.parentId !== id && nodeMap.has(node.parentId)) {
      const parent = nodeMap.get(node.parentId)!
      if (!parent.children) parent.children = []
      parent.children.push(node)
    } else {
      roots.push(node)
    }
  })

  return roots
}

function HierarchyTreeComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const idCol = config.idColumn ?? columns[0]
  const parentCol = config.parentIdColumn ?? columns[1]
  const labelCol = config.labelColumn ?? columns.find(c => !c.includes('id') && !c.includes('parent') && c !== idCol && c !== parentCol) ?? idCol
  const metricCols = config.metricColumns ?? []
  const isHorizontal = config.layout === 'left-to-right'

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const treeData = buildTree(chartData, idCol, parentCol, labelCol, metricCols, config.seriesColors ?? {})

  const option = {
    tooltip: { ...tooltipStyle, trigger: 'item' as const },
    series: [{
      type: 'tree' as const,
      data: treeData,
      orient: isHorizontal ? 'LR' as const : 'TB' as const,
      top: isHorizontal ? '5%' : '10%',
      bottom: isHorizontal ? '5%' : '25%',
      left: isHorizontal ? '15%' : '5%',
      right: isHorizontal ? '20%' : '5%',
      symbolSize: 10,
      label: {
        position: isHorizontal ? 'right' as const : 'top' as const,
        verticalAlign: 'middle' as const,
        fontSize: 11,
        color: 'var(--text-primary)',
        formatter: (params: any) => params.name,
      },
      leaves: {
        label: {
          position: isHorizontal ? 'right' as const : 'bottom' as const,
          verticalAlign: 'middle' as const,
        },
      },
      expandAndCollapse: true,
      animationDuration: 200,
      animationDurationUpdate: 200,
      lineStyle: { color: 'var(--border)', width: 1.5 },
      itemStyle: { borderColor: 'var(--bg-card)' },
    }],
  }

  const height = Math.max(300, treeData.length * 60)
  return <EChartsContainer option={option} height={height} />
}

function HierarchyTreeConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  const idCols = columns.filter(c => {
    const t = c.toLowerCase()
    return t.includes('id') || t.includes('pid') || t.includes('key') || t.includes('code')
  })

  return (
    <div style={styles.panel}>
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>ID column</div>
          <select
            aria-label="ID column"
            style={styles.select}
            value={config.idColumn ?? ''}
            onChange={e => onChange({ ...config, idColumn: e.target.value })}
          >
            {idCols.length > 0
              ? idCols.map(c => <option key={c} value={c}>{c}</option>)
              : columns.map(c => <option key={c} value={c}>{c}</option>)
            }
          </select>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Parent ID column</div>
          <select
            aria-label="Parent ID column"
            style={styles.select}
            value={config.parentIdColumn ?? ''}
            onChange={e => onChange({ ...config, parentIdColumn: e.target.value })}
          >
            {idCols.length > 0
              ? idCols.map(c => <option key={c} value={c}>{c}</option>)
              : columns.map(c => <option key={c} value={c}>{c}</option>)
            }
          </select>
        </div>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Label column</div>
        <select
          aria-label="Label column"
          style={styles.select}
          value={config.labelColumn ?? ''}
          onChange={e => onChange({ ...config, labelColumn: e.target.value || undefined })}
        >
          <option value="">Use ID</option>
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Metrics <span style={{ fontWeight: 400, textTransform: 'none' }}>(Ctrl+click multi)</span></div>
        <select
          aria-label="Metrics"
          style={{ ...styles.select, minHeight: 56 }}
          multiple
          value={config.metricColumns ?? []}
          onChange={e => {
            const selected = Array.from(e.target.selectedOptions).map(o => o.value)
            onChange({ ...config, metricColumns: selected })
          }}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Layout</div>
        <select
          aria-label="Layout direction"
          style={styles.select}
          value={config.layout ?? 'top-down'}
          onChange={e => onChange({ ...config, layout: e.target.value as 'top-down' | 'left-to-right' })}
        >
          <option value="top-down">Top-down</option>
          <option value="left-to-right">Left-to-right</option>
        </select>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  row: { display: 'flex', gap: 10 },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
}

export const HierarchyTreeModule: ChartModule = {
  Component: HierarchyTreeComponent,
  ConfigPanel: HierarchyTreeConfigPanel,
  defaultConfig: { chartType: 'hierarchy_tree', layout: 'top-down' },
  detectColumns: (columns, rows) => {
    const detected = detectParentChild(columns, rows)
    const firstTextCol = columns.find(c => {
      const t = (c.type ?? '').toLowerCase()
      return t.includes('text') || t.includes('varchar') || t.includes('char') || t.includes('name')
    })
    return {
      idColumn: detected?.idCol,
      parentIdColumn: detected?.parentCol,
      labelColumn: firstTextCol?.name,
    }
  },
  requirements: { minColumns: 3, needsParentChild: true },
}
```

### Step 2: Verify it compiles

Run: `cd web && npx tsc --noEmit`
Expected: No errors

### Step 3: Commit

```bash
git add web/src/charts/HierarchyTreeChart.tsx
git commit -m "charts: add hierarchy tree chart with auto-detect parent-child columns"
```

---

## Task 6: Wire into OutputRenderer and Remove Old ChartView

**Files:**
- Modify: `web/src/components/OutputRenderer.tsx` — swap ChartView import
- Delete: `web/src/components/ChartView.tsx`
- Delete: `web/src/components/ChartConfigPanel.tsx`

### Step 1: Update OutputRenderer imports

In `web/src/components/OutputRenderer.tsx`, change:

```tsx
// OLD:
import { ChartView } from './ChartView'
import type { ChartConfig } from './ChartConfigPanel'

// NEW:
import { ChartView } from '../charts'
import type { ChartConfig } from '../charts'
```

### Step 2: Remove old files

```bash
rm web/src/components/ChartView.tsx
rm web/src/components/ChartConfigPanel.tsx
```

### Step 3: Verify compilation

Run: `cd web && npx tsc --noEmit`
Expected: No errors

### Step 4: Commit

```bash
git add -A web/src/
git commit -m "charts: wire new ChartView into OutputRenderer, remove old files"
```

---

## Task 7: Update Go Agent Tools

**Files:**
- Modify: `internal/agent/tools_chart.go`

### Step 1: Extend chart_type enum and add new params

Update the `create_chart` tool:

```go
// In RegisterChartTools, update create_chart Parameters:
Parameters: `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string","enum":["bar","stacked_bar","line","area","scatter","pie","donut","timeline","hierarchy_tree"]},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"title":{"type":"string"},"time_column":{"type":"string"},"end_time_column":{"type":"string"},"label_column":{"type":"string"},"group_by":{"type":"string"},"id_column":{"type":"string"},"parent_id_column":{"type":"string"},"metric_columns":{"type":"array","items":{"type":"string"}},"layout":{"type":"string","enum":["top-down","left-to-right"]}},"required":["cell_id","chart_type"]}`,
```

Update the `update_chart` tool similarly (same params, none required except cell_id).

### Step 2: Update handler structs to unmarshal new fields

In `makeCreateChartHandler`:

```go
var req struct {
    CellID         string   `json:"cell_id"`
    ChartType      string   `json:"chart_type"`
    XColumn        string   `json:"x_column"`
    YColumns       []string `json:"y_columns"`
    Title          string   `json:"title"`
    TimeColumn     string   `json:"time_column"`
    EndTimeColumn  string   `json:"end_time_column"`
    LabelColumn    string   `json:"label_column"`
    GroupBy        string   `json:"group_by"`
    IDColumn       string   `json:"id_column"`
    ParentIDColumn string   `json:"parent_id_column"`
    MetricColumns  []string `json:"metric_columns"`
    Layout         string   `json:"layout"`
}
```

And update `chartConfig` map to include all fields:

```go
chartConfig := map[string]any{
    "chartType":      req.ChartType,
    "xAxis":          req.XColumn,
    "yAxis":          req.YColumns,
    "title":          req.Title,
    "timeColumn":     req.TimeColumn,
    "endTimeColumn":  req.EndTimeColumn,
    "labelColumn":    req.LabelColumn,
    "groupBy":        req.GroupBy,
    "idColumn":       req.IDColumn,
    "parentIdColumn": req.ParentIDColumn,
    "metricColumns":  req.MetricColumns,
    "layout":         req.Layout,
    "created_at":     time.Now().Format(time.RFC3339),
}
```

Do the same for `makeUpdateChartHandler` — add the same new fields to the req struct and merge them into existingConfig (only if non-empty).

### Step 3: Update engine.go chart context hint

```go
// OLD:
ctx += "\nCharts: Use create_chart to turn a cell's table output into a chart (bar, line, scatter, pie). Use update_chart to modify an existing chart's config. The frontend will render the chart automatically from the saved config."

// NEW:
ctx += "\nCharts: Use create_chart to turn a cell's table output into a chart. Types: bar, stacked_bar, line, area, scatter, pie, donut, timeline, hierarchy_tree. For timeline: use time_column, end_time_column (optional), label_column. For hierarchy_tree: use id_column, parent_id_column, label_column. Use update_chart to modify an existing chart's config. The frontend renders automatically from saved config."
```

### Step 4: Verify Go compiles

Run: `cd /home/jesus/Projects/hnb-claude && go build ./...`
Expected: No errors

### Step 5: Commit

```bash
git add internal/agent/tools_chart.go internal/agent/engine.go
git commit -m "agent: extend chart tools with timeline and hierarchy_tree types"
```

---

## Task 8: Remove recharts and Update Dependencies

**Files:**
- Modify: `web/package.json`

### Step 1: Remove recharts

```bash
cd /home/jesus/Projects/hnb-claude/web && npm uninstall recharts
```

### Step 2: Verify build

```bash
npx tsc --noEmit && npx vite build
```

Expected: No errors, clean build

### Step 3: Commit

```bash
cd /home/jesus/Projects/hnb-claude
git add web/package.json web/package-lock.json
git commit -m "deps: remove recharts, standardize on ECharts"
```

---

## Task 9: Update and Create Tests

**Files:**
- Modify: `web/src/test/ChartView.test.tsx`
- Create: `web/src/test/TimelineChart.test.tsx`
- Create: `web/src/test/HierarchyTreeChart.test.tsx`

### Step 1: Rewrite ChartView tests for new architecture

```tsx
// web/src/test/ChartView.test.tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { ChartView } from '../charts'

// ECharts uses ResizeObserver
globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const tableOutput = {
  type: 'table',
  data: {
    columns: [
      { name: 'month', type: 'text' },
      { name: 'revenue', type: 'float' },
    ],
    rows: [['Jan', 1000], ['Feb', 1500], ['Mar', 1200]],
  },
}

test('renders bar chart', () => {
  const config = { chartType: 'bar' as const, xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders line chart', () => {
  const config = { chartType: 'line' as const, xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders pie chart', () => {
  const config = { chartType: 'pie' as const, xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders timeline chart', () => {
  const output = {
    type: 'table',
    data: {
      columns: [
        { name: 'timestamp', type: 'timestamp' },
        { name: 'event', type: 'text' },
      ],
      rows: [['2024-01-01T10:00:00', 'Login'], ['2024-01-01T11:00:00', 'Logout']],
    },
    config: { chartType: 'timeline' as const },
  }
  render(<ChartView output={output} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders hierarchy tree chart', () => {
  const output = {
    type: 'table',
    data: {
      columns: [
        { name: 'pid', type: 'int4' },
        { name: 'ppid', type: 'int4' },
        { name: 'name', type: 'text' },
      ],
      rows: [[1, 0, 'init'], [2, 1, 'ssh'], [3, 1, 'nginx']],
    },
    config: { chartType: 'hierarchy_tree' as const },
  }
  render(<ChartView output={output} />)
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('shows Configure button that toggles config panel', () => {
  const config = { chartType: 'bar' as const, xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} onConfigChange={() => {}} />)
  fireEvent.click(screen.getByRole('button', { name: /configure/i }))
  expect(screen.getByLabelText(/x axis/i)).toBeInTheDocument()
})

test('shows error for unknown chart type', () => {
  const config = { chartType: 'unknown' as any }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(screen.getByText(/unknown chart type/i)).toBeInTheDocument()
})
```

### Step 2: Create Timeline-specific tests

```tsx
// web/src/test/TimelineChart.test.tsx
import { render, screen } from '@testing-library/react'
import { TimelineModule } from '../charts/TimelineChart'

globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const timeData = {
  columns: [
    { name: 'ts', type: 'timestamp' },
    { name: 'msg', type: 'text' },
    { name: 'level', type: 'text' },
  ],
  rows: [
    ['2024-01-01T10:00:00', 'User logged in', 'info'],
    ['2024-01-01T10:05:00', 'Failed login', 'warn'],
    ['2024-01-01T10:10:00', 'User logged out', 'info'],
  ],
}

const rangeData = {
  columns: [
    { name: 'task', type: 'text' },
    { name: 'start', type: 'timestamp' },
    { name: 'end', type: 'timestamp' },
  ],
  rows: [
    ['Build', '2024-01-01T09:00:00', '2024-01-01T09:30:00'],
    ['Test', '2024-01-01T09:30:00', '2024-01-01T10:00:00'],
    ['Deploy', '2024-01-01T10:00:00', '2024-01-01T10:15:00'],
  ],
}

test('renders point-in-time events', () => {
  render(
    <TimelineModule.Component
      data={timeData}
      config={{ chartType: 'timeline', timeColumn: 'ts', labelColumn: 'msg' }}
    />
  )
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('renders range-based events', () => {
  render(
    <TimelineModule.Component
      data={rangeData}
      config={{ chartType: 'timeline', timeColumn: 'start', endTimeColumn: 'end', labelColumn: 'task' }}
    />
  )
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('auto-detects time columns', () => {
  const detected = TimelineModule.detectColumns(timeData.columns, timeData.rows)
  expect(detected.timeColumn).toBe('ts')
})

test('config panel renders time column selector', () => {
  const onChange = vi.fn()
  render(
    <TimelineModule.ConfigPanel
      config={{ chartType: 'timeline' }}
      columns={['ts', 'msg', 'level']}
      onChange={onChange}
    />
  )
  expect(screen.getByLabelText(/time column/i)).toBeInTheDocument()
})
```

### Step 3: Create Hierarchy Tree tests

```tsx
// web/src/test/HierarchyTreeChart.test.tsx
import { render, screen } from '@testing-library/react'
import { HierarchyTreeModule } from '../charts/HierarchyTreeChart'

globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const treeData = {
  columns: [
    { name: 'pid', type: 'int4' },
    { name: 'ppid', type: 'int4' },
    { name: 'name', type: 'text' },
    { name: 'cpu', type: 'float' },
  ],
  rows: [
    [1, 0, 'init', 0.1],
    [2, 1, 'sshd', 0.5],
    [3, 1, 'nginx', 1.2],
    [4, 2, 'sshd-session', 0.3],
  ],
}

test('renders hierarchy tree', () => {
  render(
    <HierarchyTreeModule.Component
      data={treeData}
      config={{
        chartType: 'hierarchy_tree',
        idColumn: 'pid',
        parentIdColumn: 'ppid',
        labelColumn: 'name',
        metricColumns: ['cpu'],
        layout: 'top-down',
      }}
    />
  )
  expect(screen.getByTestId('chart-container')).toBeInTheDocument()
})

test('auto-detects parent-child columns', () => {
  const detected = HierarchyTreeModule.detectColumns(treeData.columns, treeData.rows)
  expect(detected.idColumn).toBe('pid')
  expect(detected.parentIdColumn).toBe('ppid')
})

test('config panel renders layout selector', () => {
  const onChange = vi.fn()
  render(
    <HierarchyTreeModule.ConfigPanel
      config={{ chartType: 'hierarchy_tree' }}
      columns={['pid', 'ppid', 'name', 'cpu']}
      onChange={onChange}
    />
  )
  expect(screen.getByLabelText(/layout/i)).toBeInTheDocument()
  expect(screen.getByLabelText(/id column/i)).toBeInTheDocument()
  expect(screen.getByLabelText(/parent id column/i)).toBeInTheDocument()
})
```

### Step 4: Run all tests

```bash
cd /home/jesus/Projects/hnb-claude/web && npx vitest run
```

Expected: All tests pass

### Step 5: Commit

```bash
git add -A web/src/test/
git commit -m "charts: rewrite and add tests for new chart modules"
```

---

## Task 10: Update Storybook Stories

**Files:**
- Modify: `web/src/components/ChartView.stories.tsx` (or move to `web/src/charts/ChartView.stories.tsx`)
- Delete: `web/src/components/ChartConfigPanel.stories.tsx`

### Step 1: Create stories for new chart system

```tsx
// web/src/charts/ChartView.stories.tsx
import type { Meta, StoryObj } from '@storybook/react-vite'
import { ChartView } from './index'

const meta: Meta<typeof ChartView> = {
  component: ChartView,
  title: 'Charts/ChartView',
}
export default meta
type Story = StoryObj<typeof ChartView>

const monthlyData = {
  columns: [
    { name: 'month', type: 'text' },
    { name: 'revenue', type: 'float8' },
    { name: 'expenses', type: 'float8' },
  ],
  rows: [
    ['Jan', 12000, 9000], ['Feb', 15000, 10500], ['Mar', 11000, 8000],
    ['Apr', 18000, 12000], ['May', 21000, 14000], ['Jun', 19000, 13500],
  ],
}

const categoryData = {
  columns: [
    { name: 'category', type: 'text' },
    { name: 'count', type: 'int4' },
  ],
  rows: [
    ['Engineering', 42], ['Sales', 28], ['Marketing', 19], ['Support', 15], ['Design', 11],
  ],
}

const timelineData = {
  columns: [
    { name: 'timestamp', type: 'timestamp' },
    { name: 'event', type: 'text' },
    { name: 'level', type: 'text' },
  ],
  rows: [
    ['2024-01-15T08:00:00', 'System start', 'info'],
    ['2024-01-15T08:15:00', 'User login', 'info'],
    ['2024-01-15T08:30:00', 'Failed auth', 'warn'],
    ['2024-01-15T09:00:00', 'Query executed', 'info'],
    ['2024-01-15T09:45:00', 'Timeout error', 'error'],
    ['2024-01-15T10:00:00', 'Retry success', 'info'],
  ],
}

const rangeTimelineData = {
  columns: [
    { name: 'task', type: 'text' },
    { name: 'start', type: 'timestamp' },
    { name: 'end', type: 'timestamp' },
  ],
  rows: [
    ['Build', '2024-01-15T09:00:00', '2024-01-15T09:30:00'],
    ['Test', '2024-01-15T09:30:00', '2024-01-15T10:00:00'],
    ['Deploy', '2024-01-15T10:00:00', '2024-01-15T10:15:00'],
  ],
}

const treeData = {
  columns: [
    { name: 'pid', type: 'int4' },
    { name: 'ppid', type: 'int4' },
    { name: 'name', type: 'text' },
    { name: 'cpu', type: 'float' },
  ],
  rows: [
    [1, 0, 'systemd', 0.1],
    [2, 1, 'sshd', 0.5],
    [3, 1, 'nginx', 1.2],
    [4, 2, 'sshd-session', 0.3],
    [5, 3, 'worker', 2.1],
    [6, 3, 'worker', 1.8],
  ],
}

export const Bar: Story = { args: { rs: monthlyData } }
export const StackedBar: Story = { args: { output: { type: 'table', data: monthlyData, config: { chartType: 'stacked_bar', xAxis: 'month', yAxis: ['revenue', 'expenses'] } } } }
export const Line: Story = { args: { output: { type: 'table', data: monthlyData, config: { chartType: 'line', xAxis: 'month', yAxis: ['revenue', 'expenses'] } } } }
export const Area: Story = { args: { output: { type: 'table', data: monthlyData, config: { chartType: 'area', xAxis: 'month', yAxis: ['revenue', 'expenses'] } } } }
export const Scatter: Story = { args: { output: { type: 'table', data: monthlyData, config: { chartType: 'scatter', xAxis: 'month', yAxis: ['revenue'] } } } }
export const Pie: Story = { args: { output: { type: 'table', data: categoryData, config: { chartType: 'pie', xAxis: 'category', yAxis: ['count'] } } } }
export const Donut: Story = { args: { output: { type: 'table', data: categoryData, config: { chartType: 'donut', xAxis: 'category', yAxis: ['count'] } } } }
export const TimelineEvents: Story = { args: { output: { type: 'table', data: timelineData, config: { chartType: 'timeline', timeColumn: 'timestamp', labelColumn: 'event', groupBy: 'level' } } } }
export const TimelineRanges: Story = { args: { output: { type: 'table', data: rangeTimelineData, config: { chartType: 'timeline', timeColumn: 'start', endTimeColumn: 'end', labelColumn: 'task' } } } }
export const HierarchyTree: Story = { args: { output: { type: 'table', data: treeData, config: { chartType: 'hierarchy_tree', idColumn: 'pid', parentIdColumn: 'ppid', labelColumn: 'name', metricColumns: ['cpu'], layout: 'top-down' } } } }
```

### Step 2: Delete old stories

```bash
rm web/src/components/ChartConfigPanel.stories.tsx
rm web/src/components/ChartView.stories.tsx
```

### Step 3: Commit

```bash
git add -A web/src/charts/ web/src/components/
git commit -m "charts: add Storybook stories for all chart types, remove old stories"
```

---

## Task 11: Final Verification

### Step 1: Full TypeScript check

```bash
cd /home/jesus/Projects/hnb-claude/web && npx tsc --noEmit
```

### Step 2: Run all tests

```bash
npx vitest run
```

### Step 3: Build for production

```bash
npx vite build
```

### Step 4: Go build

```bash
cd /home/jesus/Projects/hnb-claude && go build ./...
```

### Step 5: Run Storybook and visually verify

```bash
cd /home/jesus/Projects/hnb-claude/web && npx storybook dev -p 6006
```

Visually check:
- [ ] All existing chart types render correctly (bar, stacked_bar, line, area, scatter, pie, donut)
- [ ] Timeline shows events on time axis with zoom slider
- [ ] Hierarchy tree renders parent-child relationships
- [ ] Config panels show relevant options per chart type
- [ ] Dark theme looks correct

### Step 6: Final commit

```bash
cd /home/jesus/Projects/hnb-claude
git add -A
git commit -m "charts: complete overhaul - ECharts, registry pattern, timeline + hierarchy tree"
```
