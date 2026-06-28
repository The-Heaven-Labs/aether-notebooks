import type React from 'react'
import { memo, useRef, useEffect, useMemo, useState } from 'react'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, ScatterChart, PieChart, TreeChart, MapChart, SankeyChart } from 'echarts/charts'
import {
  GridComponent, TooltipComponent, LegendComponent, TitleComponent,
  DataZoomComponent, ToolboxComponent, GeoComponent,
  VisualMapComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { ResultSet } from '../types'

// Register only what we use
echarts.use([
  BarChart, LineChart, ScatterChart, PieChart, TreeChart, MapChart, SankeyChart,
  GridComponent, TooltipComponent, LegendComponent, TitleComponent,
  DataZoomComponent, ToolboxComponent, GeoComponent,
  VisualMapComponent,
  CanvasRenderer,
])

export const CHART_COLORS = [
  '#6366f1', '#06b6d4', '#10b981', '#f59e0b',
  '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16',
]

export const ALL_CHART_TYPES = [
  { value: 'bar', label: 'Bar', symbol: '▊▊' },
  { value: 'stacked_bar', label: 'Stack', symbol: '▊≡' },
  { value: 'line', label: 'Line', symbol: '╱╲' },
  { value: 'area', label: 'Area', symbol: '▓' },
  { value: 'stacked_area', label: 'Stack Area', symbol: '▓≡' },
  { value: 'scatter', label: 'Scatter', symbol: '·:' },
  { value: 'pie', label: 'Pie', symbol: '◕' },
  { value: 'donut', label: 'Donut', symbol: '◎' },
  { value: 'timeline', label: 'Timeline', symbol: '⏱' },
  { value: 'hierarchy_tree', label: 'Tree', symbol: '🌲' },
  { value: 'big_number', label: 'Big Number', symbol: '123' },
  { value: 'map', label: 'Map', symbol: '🌍' },
  { value: 'sankey', label: 'Sankey', symbol: '⇄' },
] as const

export function ChartTypeSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <select
      aria-label="Chart type"
      style={{ fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4, width: '100%' }}
      value={value}
      onChange={e => onChange(e.target.value)}
    >
      {ALL_CHART_TYPES.map(t => (
        <option key={t.value} value={t.value}>{t.symbol} {t.label}</option>
      ))}
    </select>
  )
}

// Detect numeric column types
const NUMERIC_TYPES = new Set([
  'int', 'int2', 'int4', 'int8', 'bigint', 'smallint', 'serial', 'bigserial',
  'float', 'float4', 'float8', 'double', 'decimal', 'numeric', 'real',
  'int16', 'int32', 'int64', 'int128', 'int256',
  'uint8', 'uint16', 'uint32', 'uint64', 'uint128', 'uint256',
  'float32', 'float64',
])

export function isNumericType(type?: string): boolean {
  if (!type) return false
  const t = type.toLowerCase()
  
  const base = t.replace(/\(.*\)/, '').trim()
  if (NUMERIC_TYPES.has(base)) return true
  
  for (const wrapper of ['nullable', 'lowcardinality']) {
    if (t.startsWith(wrapper + '(') && t.endsWith(')')) {
      const inner = t.slice(wrapper.length + 1, -1).trim()
      const innerBase = inner.replace(/\(.*\)/, '').trim()
      if (NUMERIC_TYPES.has(innerBase)) return true
    }
  }
  return false
}

// Auto-detect: first text-like column for xAxis, first numeric columns for yAxis
export function detectAxisColumns(columns: { name: string; type?: string }[]): { xAxis?: string; yAxis?: string[] } {
  const textCol = columns.find(c => {
    const t = (c.type ?? '').toLowerCase()
    return t.includes('text') || t.includes('varchar') || t.includes('char') || t.includes('date') || t.includes('time') || !c.type
  })
  const numericCols = columns.filter(c => isNumericType(c.type)).map(c => c.name)
  return {
    xAxis: textCol?.name ?? columns[0]?.name,
    yAxis: numericCols.length > 0 ? numericCols : columns.slice(1, 2).map(c => c.name),
  }
}

export function isTimeType(colType?: string): boolean {
  if (!colType) return false
  const t = colType.toLowerCase()
  return t.includes('date') || t.includes('time') || t.includes('timestamp') || t === 'ts'
}

// Shareable hook: maps rows to Record objects (used by every chart component)
export function useRowsAsObjects(data: ResultSet): Record<string, unknown>[] {
  const columns = useMemo(() => data.columns.map(c => c.name), [data.columns])
  return useMemo(
    () => data.rows.map(row => {
      const obj: Record<string, unknown> = {}
      columns.forEach((col, i) => { obj[col] = row[i] })
      return obj
    }),
    [data.rows, columns]
  )
}

// Shareable hook: resolves axis columns with smart defaults for axis-based charts
export function useAxisColumns(
  data: ResultSet,
  config: { xAxis?: string; yAxis?: string[] }
): { xAxis: string; yAxes: string[] } {
  const columns = useMemo(() => data.columns.map(c => c.name), [data.columns])
  return useMemo(() => {
    const detected = (!config.xAxis && !config.yAxis?.length)
      ? detectAxisColumns(data.columns)
      : { xAxis: config.xAxis, yAxis: config.yAxis }

    const xAxis = config.xAxis ?? detected.xAxis ?? columns[0] ?? ''
    const yAxes = config.yAxis?.length
      ? config.yAxis
      : (detected.yAxis?.length
        ? detected.yAxis
        : columns.filter((_, i) => i > 0 && isNumericType(data.columns[i]?.type)).slice(0, 1))
    return { xAxis, yAxes }
  }, [data.columns, data.rows, config.xAxis, config.yAxis, columns])
}

export function useGroupBySeries(
  chartData: Record<string, unknown>[],
  config: { xAxis?: string; yAxis?: string[]; groupBy?: string; seriesColors?: Record<string, string> },
): { series: any[]; xValues: string[] } {
  return useMemo(() => {
    const groupByCol = config.groupBy
    if (!groupByCol || !config.xAxis || !config.yAxis?.length || chartData.length === 0) {
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
        itemStyle: {
          color: config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length],
        },
      }))
    )

    return { series, xValues }
  }, [chartData, config.xAxis, config.yAxis, config.groupBy, config.seriesColors])
}

// Detect dark mode and provide explicit colors for ECharts (canvas doesn't support CSS vars)
function isDarkMode(): boolean {
  return document.documentElement.getAttribute('data-theme') === 'dark'
}

export function getChartColors() {
  const dark = isDarkMode()
  return {
    text: dark ? '#e8e8e8' : '#111',
    textMuted: dark ? '#888' : '#6e6e6e',
    border: dark ? '#2e2e2e' : '#e8e8e8',
    bgCard: dark ? '#1c1c1c' : '#ffffff',
    shadow: dark ? 'rgba(0,0,0,0.3)' : 'rgba(0,0,0,0.1)',
  }
}

export function useChartColors() {
  const [colors, setColors] = useState(getChartColors)

  useEffect(() => {
    const el = document.documentElement
    const update = () => setColors(getChartColors())
    const observer = new MutationObserver(update)
    observer.observe(el, { attributes: true, attributeFilter: ['data-theme'] })
    return () => observer.disconnect()
  }, [])

  return colors
}

export function getTooltipStyle() {
  const c = getChartColors()
  return {
    backgroundColor: c.bgCard,
    borderColor: c.border,
    borderRadius: 4,
    textStyle: { fontSize: 12, color: c.text },
    extraCssText: `box-shadow: 0 2px 16px ${c.shadow};`,
  }
}

export function getAxisStyle(showGrid?: boolean) {
  const c = getChartColors()
  return {
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: { fontSize: 11, color: c.textMuted },
    splitLine: { show: showGrid !== false, lineStyle: { color: c.border, type: 'dashed' as const } },
  }
}

interface EChartsContainerProps {
  option: echarts.EChartsOption
  height?: number
  onChartReady?: (chart: echarts.ECharts) => void
  notMerge?: boolean
  showReset?: boolean
}

// Walk tree nodes to collect collapsed names or apply state
export function walkTree(nodes: any[], fn: (node: any) => void) {
  for (const node of nodes) {
    fn(node)
    if (node.children) walkTree(node.children, fn)
  }
}

export function applyCollapsedToTree(data: any, collapsed: Set<string>): any {
  if (Array.isArray(data)) {
    return data.map(n => applyCollapsedToTree(n, collapsed))
  }
  const node = { ...data }
  if (collapsed.has(data.name)) {
    node.collapsed = true
  } else {
    delete node.collapsed
  }
  if (data.children) {
    node.children = applyCollapsedToTree(data.children, collapsed)
  }
  return node
}

export const EChartsContainer = memo(function EChartsContainer({ option, height = 300, onChartReady, notMerge = true, showReset = false }: EChartsContainerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)

  const handleReset = () => {
    chartRef.current?.dispatchAction({ type: 'restore' })
  }

  useEffect(() => {
    if (!containerRef.current) return
    if (!chartRef.current) {
      chartRef.current = echarts.init(containerRef.current, undefined, {
        renderer: 'canvas',
      })
      onChartReady?.(chartRef.current)
    }
    // Merge transparent background for theme compatibility
    const themedOption = {
      ...option,
      backgroundColor: 'transparent',
    }

    // Preserve tree node collapsed state across updates
    let finalOption: any = themedOption
    if (chartRef.current) {
      try {
        const currentOpt = chartRef.current.getOption() as any
        const curSeries = currentOpt?.series?.[0]
        const newSeries = (themedOption as any)?.series?.[0]
        if (curSeries?.type === 'tree' && newSeries?.type === 'tree' && curSeries.data && newSeries.data) {
          const collapsed = new Set<string>()
          const curData = Array.isArray(curSeries.data) ? curSeries.data : [curSeries.data]
          walkTree(curData, n => { if (n.collapsed) collapsed.add(n.name) })
          if (collapsed.size > 0) {
            finalOption = {
              ...themedOption,
              series: [{ ...newSeries, data: applyCollapsedToTree(newSeries.data, collapsed) }],
            }
          }
        }
      } catch { /* ignore — best-effort preservation */ }
    }

    chartRef.current.setOption(finalOption, { notMerge })

    const ro = new ResizeObserver(() => {
      chartRef.current?.resize()
    })
    ro.observe(containerRef.current)
    return () => ro.disconnect()
  }, [option])

  useEffect(() => {
    return () => {
      chartRef.current?.dispose()
      chartRef.current = null
    }
  }, [])

  return (
    <div style={{ position: 'relative' }}>
      <div data-testid="chart-container" ref={containerRef} style={{ height, width: '100%' }} />
      {showReset && (
        <button
          onClick={handleReset}
          style={resetBtnStyle}
          title="Reset zoom/pan"
          aria-label="Reset zoom and pan to initial view"
        >
          ↺ Reset
        </button>
      )}
    </div>
  )
})

const resetBtnStyle: React.CSSProperties = {
  position: 'absolute',
  top: 8,
  right: 8,
  fontSize: 11,
  padding: '4px 10px',
  background: 'var(--bg-card)',
  color: 'var(--text-muted)',
  border: '1px solid var(--border)',
  borderRadius: 4,
  cursor: 'pointer',
  zIndex: 10,
  transition: 'background 0.15s, color 0.15s',
}
