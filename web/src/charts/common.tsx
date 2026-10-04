import type React from 'react'
import { memo, useRef, useEffect, useMemo, useState } from 'react'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, ScatterChart, PieChart, TreeChart, MapChart, SankeyChart, FunnelChart, HeatmapChart } from 'echarts/charts'
import {
  GridComponent, TooltipComponent, LegendComponent, TitleComponent,
  DataZoomComponent, ToolboxComponent, GeoComponent,
  VisualMapComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { LabelLayout } from 'echarts/features'
import type { ResultSet } from '../types'
import type { ChartConfig, MarkLineConfig } from './types'

// Register only what we use
echarts.use([
  BarChart, LineChart, ScatterChart, PieChart, TreeChart, MapChart, SankeyChart, FunnelChart, HeatmapChart,
  GridComponent, TooltipComponent, LegendComponent, TitleComponent,
  DataZoomComponent, ToolboxComponent, GeoComponent,
  VisualMapComponent,
  // Required for series `labelLayout` (pie title-band clamp, timeline
  // hideOverlap): without it ECharts ignores the option entirely.
  LabelLayout,
  CanvasRenderer,
])

const LIGHT_CHART_COLORS = [
  '#6366f1', '#06b6d4', '#10b981', '#f59e0b',
  '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16',
]

const DARK_CHART_COLORS = [
  '#818cf8', '#22d3ee', '#34d399', '#fbbf24',
  '#fb7185', '#a78bfa', '#38bdf8', '#a3e635',
]

function getCurrentPalette(): string[] {
  return isDarkMode() ? DARK_CHART_COLORS : LIGHT_CHART_COLORS
}

export const CHART_COLORS = new Proxy(LIGHT_CHART_COLORS, {
  get(_target, prop) {
    // Symbol reads (e.g. Symbol.toStringTag by React Refresh / DevTools) must
    // not fall into the numeric index conversion below.
    if (typeof prop === 'symbol') return (Reflect as any).get(_target, prop, _target)
    const palette = getCurrentPalette()
    if (prop === 'length') return palette.length
    const idx = Number(prop)
    if (!isNaN(idx)) return palette[idx]
    return (Reflect as any).get(palette, prop, palette)
  },
  has(_target, prop) {
    if (typeof prop === 'symbol') return (Reflect as any).has(_target, prop)
    return prop in getCurrentPalette()
  },
}) as string[]

export const ALL_CHART_TYPES = [
  { value: 'bar', label: 'Bar' },
  { value: 'line', label: 'Line' },
  { value: 'area', label: 'Area' },
  { value: 'scatter', label: 'Scatter' },
  { value: 'pie', label: 'Pie' },
  { value: 'donut', label: 'Donut' },
  { value: 'timeline', label: 'Timeline' },
  { value: 'hierarchy_tree', label: 'Tree' },
  { value: 'big_number', label: 'Big Number' },
  { value: 'map', label: 'Map' },
  { value: 'sankey', label: 'Sankey' },
  { value: 'funnel', label: 'Funnel' },
  { value: 'heatmap', label: 'Heatmap' },
  { value: 'histogram', label: 'Histogram' },
] as const

const iconStyle: React.CSSProperties = {
  width: 18, height: 18,
  verticalAlign: 'middle',
  marginRight: 6,
  flexShrink: 0,
}

function IconBar() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" style={iconStyle}>
      <rect x="2" y="13" width="4" height="5" rx="1" />
      <rect x="8" y="8" width="4" height="10" rx="1" />
      <rect x="14" y="5" width="4" height="13" rx="1" />
    </svg>
  )
}
function IconLine() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
      <polyline points="2,16 6,11 10,14 14,6 18,8" />
    </svg>
  )
}
function IconArea() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" style={iconStyle}>
      <polyline points="2,16 6,11 10,14 14,6 18,8" />
      <polyline points="2,16 2,18 18,18 18,8" fill="currentColor" fillOpacity="0.2" stroke="none" />
    </svg>
  )
}
function IconScatter() {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" stroke="none" style={iconStyle}>
      <circle cx="4" cy="13" r="1.5" />
      <circle cx="9" cy="6" r="1.5" />
      <circle cx="14" cy="12" r="1.5" />
      <circle cx="16" cy="4" r="1.5" />
      <circle cx="7" cy="16" r="1.5" />
    </svg>
  )
}
function IconPie() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" style={iconStyle}>
      <circle cx="10" cy="10" r="7" />
      <path d="M10 10 L10 3 A7 7 0 0 1 16.6 12.8" fill="currentColor" fillOpacity="0.2" />
      <path d="M10 10 L10 3" />
    </svg>
  )
}
function IconDonut() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" style={iconStyle}>
      <circle cx="10" cy="10" r="7" />
      <circle cx="10" cy="10" r="3" />
      <path d="M10 3 A7 7 0 0 1 16.6 12.8" fill="currentColor" fillOpacity="0.15" stroke="none" />
    </svg>
  )
}
function IconTimeline() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" style={iconStyle}>
      <line x1="2" y1="6" x2="18" y2="6" />
      <circle cx="0" cy="0" r="0" />
      <circle cx="8" cy="6" r="2" fill="currentColor" />
      <line x1="8" y1="8" x2="8" y2="15" strokeDasharray="2 1" />
      <rect x="5" y="12" width="6" height="4" rx="1" fill="currentColor" fillOpacity="0.2" />
      <line x1="14" y1="6" x2="14" y2="3" strokeDasharray="2 1" />
      <circle cx="14" cy="3" r="2" fill="currentColor" />
    </svg>
  )
}
function IconTree() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" style={iconStyle}>
      <circle cx="10" cy="3" r="2" fill="currentColor" />
      <line x1="10" y1="5" x2="10" y2="8" />
      <line x1="10" y1="8" x2="5" y2="12" />
      <line x1="10" y1="8" x2="15" y2="12" />
      <circle cx="5" cy="14" r="2" fill="currentColor" fillOpacity="0.2" stroke="currentColor" />
      <circle cx="15" cy="14" r="2" fill="currentColor" fillOpacity="0.2" stroke="currentColor" />
    </svg>
  )
}
function IconBigNumber() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" style={iconStyle}>
      <text x="10" y="15" textAnchor="middle" fontSize="12" fontWeight="700" fill="currentColor" stroke="none">123</text>
    </svg>
  )
}
function IconMap() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" style={iconStyle}>
      <circle cx="10" cy="10" r="7" />
      <ellipse cx="10" cy="10" rx="3" ry="7" />
      <line x1="3" y1="8" x2="17" y2="8" />
      <line x1="3" y1="12" x2="17" y2="12" />
    </svg>
  )
}
function IconSankey() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" style={iconStyle}>
      <rect x="2" y="3" width="4" height="4" rx="1" fill="currentColor" fillOpacity="0.15" />
      <rect x="2" y="12" width="4" height="4" rx="1" fill="currentColor" fillOpacity="0.15" />
      <path d="M6 5 Q10 5 10 7 Q10 9 14 10 Q18 11 18 14" fill="none" />
      <path d="M6 14 Q10 14 10 12 Q10 10 14 10" fill="none" />
      <rect x="14" y="8" width="4" height="4" rx="1" fill="currentColor" fillOpacity="0.15" />
      <rect x="14" y="13" width="4" height="3" rx="1" fill="currentColor" fillOpacity="0.15" />
    </svg>
  )
}
function IconFunnel() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" style={iconStyle}>
      <path d="M2 3 L18 3 L14 10 L6 10 Z" fill="currentColor" fillOpacity="0.15" />
      <path d="M10 10 L10 17" strokeWidth="1.2" />
      <path d="M6 17 L14 17" strokeWidth="1.2" />
    </svg>
  )
}
function IconHeatmap() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1" style={iconStyle}>
      <rect x="2" y="2" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.4" />
      <rect x="8" y="2" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.7" />
      <rect x="14" y="2" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.2" />
      <rect x="2" y="8" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.6" />
      <rect x="8" y="8" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.3" />
      <rect x="14" y="8" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.8" />
      <rect x="2" y="14" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.1" />
      <rect x="8" y="14" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.5" />
      <rect x="14" y="14" width="5" height="5" rx="1" fill="currentColor" fillOpacity="0.9" />
    </svg>
  )
}
function IconHistogram() {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" style={iconStyle}>
      <rect x="2" y="12" width="4" height="6" rx="0" />
      <rect x="6" y="8" width="4" height="10" rx="0" />
      <rect x="10" y="5" width="4" height="13" rx="0" />
      <rect x="14" y="10" width="4" height="8" rx="0" />
    </svg>
  )
}

export const CHART_ICONS: Record<string, React.FC> = {
  bar: IconBar,
  line: IconLine,
  area: IconArea,
  scatter: IconScatter,
  pie: IconPie,
  donut: IconDonut,
  timeline: IconTimeline,
  hierarchy_tree: IconTree,
  big_number: IconBigNumber,
  map: IconMap,
  sankey: IconSankey,
  funnel: IconFunnel,
  heatmap: IconHeatmap,
  histogram: IconHistogram,
}

export function ChartTypeSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  const selected = ALL_CHART_TYPES.find(t => t.value === value)
  const Icon = selected ? CHART_ICONS[value] : null

  return (
    <div ref={ref} style={{ position: 'relative', width: '100%' }}>
      <button
        type="button"
        style={triggerStyle}
        onClick={() => setOpen(!open)}
        aria-label="Chart type"
      >
        {Icon && <Icon />}
        <span style={{ flex: 1, textAlign: 'left', fontSize: 12, color: 'var(--text-primary)' }}>{selected?.label ?? value}</span>
        <svg viewBox="0 0 12 12" width="10" height="10" fill="none" stroke="currentColor" strokeWidth="1.2" style={{ transform: open ? 'rotate(180deg)' : undefined, transition: 'transform 0.15s' }}>
          <polyline points="2,5 6,9 10,5" />
        </svg>
      </button>
      {open && (
        <div style={dropdownStyle}>
          {ALL_CHART_TYPES.map(t => {
            const Icn = CHART_ICONS[t.value]
            return (
              <button
                key={t.value}
                type="button"
                style={{
                  ...itemStyle,
                  background: t.value === value ? 'var(--accent-bg)' : 'transparent',
                  fontWeight: t.value === value ? 600 : 400,
                }}
                onClick={() => { onChange(t.value); setOpen(false) }}
                onMouseEnter={e => (e.currentTarget.style.background = 'var(--bg-secondary)')}
                onMouseLeave={e => (e.currentTarget.style.background = t.value === value ? 'var(--accent-bg)' : 'transparent')}
              >
                {Icn && <Icn />}
                <span style={{ fontSize: 12, color: 'var(--text-primary)' }}>{t.label}</span>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

const triggerStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', gap: 4, width: '100%',
  fontSize: 12, padding: '4px 8px', cursor: 'pointer',
  background: 'var(--bg-input)', color: 'var(--text-primary)',
  border: '1px solid var(--border)', borderRadius: 4,
}
const dropdownStyle: React.CSSProperties = {
  position: 'absolute', zIndex: 100, top: '100%', left: 0, right: 0, marginTop: 2,
  background: 'var(--bg-card)', border: '1px solid var(--border)',
  borderRadius: 4, boxShadow: 'var(--shadow-lg)',
  maxHeight: 240, overflow: 'auto', color: 'var(--text-primary)',
}
const itemStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', gap: 4, width: '100%',
  padding: '5px 8px', border: 'none', cursor: 'pointer',
  background: 'transparent', textAlign: 'left', color: 'var(--text-primary)',
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
  palette?: string[],
  // When true, missing values become 0 instead of null. Opt-in per caller:
  // only stacked charts want this (their cumulative math already treats null
  // as 0, so the band and tooltip should agree).
  coalesceNulls?: boolean,
): { series: any[]; xValues: string[] } {
  const colors = palette ?? (getCurrentPalette())
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
        data: xValues.map(x => xMap.get(x)?.get(group)?.[y] ?? (coalesceNulls ? 0 : null)),
        smooth: false,
        connectNulls: false,
        symbol: 'circle',
        symbolSize: 4,
        lineStyle: { width: (config as any).lineWidth ?? 2 },
        itemStyle: {
          color: config.seriesColors?.[group] ?? colors[gi % colors.length],
        },
      }))
    )

    return { series, xValues }
  }, [chartData, config.xAxis, config.yAxis, config.groupBy, config.seriesColors, colors, coalesceNulls])
}

// Detect dark mode and provide explicit colors for ECharts (canvas doesn't support CSS vars)
function isDarkMode(): boolean {
  return document.documentElement.getAttribute('data-theme') === 'dark'
}

export function getContrastTextColor(bg: string): string {
  const h = bg.replace('#', '')
  if (h.length < 6) return '#111'
  const r = parseInt(h.slice(0, 2), 16)
  const g = parseInt(h.slice(2, 4), 16)
  const b = parseInt(h.slice(4, 6), 16)
  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255
  return luminance > 0.5 ? '#111' : '#fff'
}

export function getChartColors() {
  const dark = isDarkMode()
  return {
    text: dark ? '#e8e8e8' : '#111',
    textMuted: dark ? '#888' : '#6e6e6e',
    border: dark ? '#2e2e2e' : '#e8e8e8',
    bgCard: dark ? '#1c1c1c' : '#ffffff',
    shadow: dark ? 'rgba(0,0,0,0.3)' : 'rgba(0,0,0,0.1)',
    palette: dark ? DARK_CHART_COLORS : LIGHT_CHART_COLORS,
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

// Canvas text can't use CSS variables, so the design system's stacks are
// duplicated here. Titles/labels speak DM Sans; data (axis values, tooltips,
// measurements) speaks JetBrains Mono — the Two Scripts Rule.
export const CHART_FONT_SANS = 'DM Sans, -apple-system, BlinkMacSystemFont, sans-serif'
export const CHART_FONT_MONO = 'JetBrains Mono, Fira Code, ui-monospace, monospace'

export function getTooltipStyle() {
  const c = getChartColors()
  return {
    backgroundColor: c.bgCard,
    borderColor: c.border,
    borderRadius: 4,
    textStyle: { fontSize: 12, color: c.text, fontFamily: CHART_FONT_MONO },
    // Dashboard widgets clip to their card (overflow: hidden), which cut the
    // top off tooltips near the plot's upper edge. Appending to <body> lets
    // the tooltip float free of any container; the z-index keeps it above
    // panels and drawers; max-height bounds pathological tooltips.
    appendToBody: true,
    // Tall tooltips are scrollable, so they must be reachable: let the mouse
    // enter them (pointer-events) and keep them alive while the pointer
    // travels from the chart to the tooltip.
    enterable: true,
    hideDelay: 800,
    // Keep the whole tooltip inside the viewport. Tall tooltips pin to the
    // top-right corner — a cursor-following box can never be caught with the
    // mouse; small ones still follow the cursor.
    position: (
      point: number[],
      _params: unknown,
      _dom: unknown,
      _rect: unknown,
      size: { contentSize: number[]; viewSize: number[] },
    ) => {
      const [mx, my] = point
      const [cw, ch] = size.contentSize
      const [vw, vh] = size.viewSize
      if (ch > 240) {
        const px = Math.max(8, vw - cw - 16)
        const py = Math.max(8, Math.min(60, vh - ch - 8))
        return [px, py]
      }
      let x = mx + 12
      let y = my - ch - 12
      if (y < 8) y = my + 16
      x = Math.max(8, Math.min(x, vw - cw - 8))
      y = Math.max(8, Math.min(y, vh - ch - 8))
      return [x, y]
    },
    extraCssText: `box-shadow: 0 2px 16px ${c.shadow}; z-index: 3000; max-height: 60vh; overflow: auto;`,
  }
}

export function getAxisStyle(showGrid?: boolean) {
  const c = getChartColors()
  return {
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: { fontSize: 11, color: c.textMuted, fontFamily: CHART_FONT_MONO },
    splitLine: { show: showGrid !== false, lineStyle: { color: c.border, type: 'dashed' as const } },
  }
}

// Right-hand plot inset reserved for the docked legend column. The width is
// measured from the series names so long grouped names cannot grow the
// right-anchored legend leftward over the plot; clamping keeps the reserve
// sane for both short and pathological names.
export const LEGEND_COLUMN_MIN = 160
export const LEGEND_COLUMN_MAX = 280

// Below this container width the right-hand legend column leaves almost no
// plot (a 4-row widget can be ~175px wide). Charts collapse the legend to a
// single scrollable row under the plot instead — see narrowChartMedia.
export const NARROW_CHART_WIDTH = 340

// ECharts `media` override for narrow containers. Modules add it to their
// option so the chart itself adapts by its own width, not the viewport's.
// Returns undefined when the legend is disabled (an override must not switch
// a hidden legend back on).
export function narrowChartMedia(showLegend: boolean | undefined): Record<string, unknown>[] | undefined {
  if (showLegend === false) return undefined
  return [{
    query: { maxWidth: NARROW_CHART_WIDTH },
    option: {
      legend: {
        orient: 'horizontal' as const,
        right: 'auto', top: 'auto', left: 'center', bottom: 0,
        width: 'auto', height: 'auto',
      },
      grid: { right: 12, bottom: 30 },
    },
  }]
}

// Fallback average glyph width at fontSize 11 (≈6.2px/char) used when a real
// canvas is unavailable (jsdom/SSR) or yields no measurement.
const CHAR_WIDTH_PER_FONT_PX = 6.2 / 11

let measureContext: CanvasRenderingContext2D | null | undefined

function getMeasureContext(): CanvasRenderingContext2D | null {
  if (measureContext === undefined) {
    try {
      measureContext = typeof document !== 'undefined'
        ? document.createElement('canvas').getContext('2d')
        : null
    } catch {
      measureContext = null
    }
  }
  return measureContext
}

function measureTextWidth(text: string, fontSize: number): number {
  const ctx = getMeasureContext()
  if (ctx) {
    ctx.font = `${fontSize}px "DM Sans", sans-serif`
    const width = ctx.measureText(text).width
    if (width > 0) return width
  }
  return text.length * fontSize * CHAR_WIDTH_PER_FONT_PX
}

// Width of the docked legend column needed for `names`: the widest item text
// at the legend's fontSize (measured, canvas fallback 6.2px/char) + swatch 14
// + gap 6 + `right: 10` offset + pager reserve. Clamped to [MIN, MAX].
export function estimateLegendColumnWidth(names: string[]): number {
  const widest = names.reduce((max, n) => Math.max(max, measureTextWidth(String(n), 11)), 0)
  return Math.min(LEGEND_COLUMN_MAX, Math.max(LEGEND_COLUMN_MIN, widest + 40))
}

// Right-hand plot inset for grid-based charts: the measured legend column
// when the legend is shown, the default plot padding otherwise.
export function legendColumnReserve(showLegend: boolean | undefined, seriesNames: string[]): number {
  return showLegend !== false ? estimateLegendColumnWidth(seriesNames) : 16
}

export interface BuildLegendOptions {
  // Series/slice names rendered as legend items; measured to size the column
  // and truncated to keep the right-anchored box inside it.
  seriesNames?: string[]
  // Chart modules that render the absolute `↺ Reset` button keep the legend
  // box below the top-right corner so the two cannot collide.
  reserveTopRight?: boolean
}

// Explicit height is required for scroll paging: without it ECharts derives an
// auto height and the pager renders but pages to an empty view. The percentages
// keep the legend box on-canvas even for the smallest dashboard widgets
// (reserveTopRight case: top + 0.44H <= H for the ~69px chart area of a 4-row
// widget with a title; the no-title box uses top 34 + 0.50H <= H).
export function buildLegend(
  config: Pick<ChartConfig, 'title' | 'showLegend'>,
  colors: ReturnType<typeof getChartColors>,
  opts?: BuildLegendOptions,
): Record<string, unknown> {
  if (config.showLegend === false) return { show: false }
  const width = estimateLegendColumnWidth(opts?.seriesNames ?? [])
  const maxChars = Math.max(12, Math.floor((width - 40) / 6.2))
  const reserveTopRight = opts?.reserveTopRight === true
  return {
    type: 'scroll',
    orient: 'vertical' as const,
    right: 10,
    top: reserveTopRight ? (config.title ? 36 : 34) : (config.title ? 30 : 8),
    height: reserveTopRight ? (config.title ? '44%' : '50%') : (config.title ? '50%' : '85%'),
    align: 'auto',
    itemWidth: 14,
    itemHeight: 10,
    textStyle: { fontSize: 11, color: colors.textMuted, fontFamily: CHART_FONT_SANS },
    // Keep the box inside the reserved column: truncate to the measured
    // budget; the legend tooltip exposes the full name on hover.
    formatter: (name: string) => {
      const s = String(name)
      return s.length > maxChars ? s.slice(0, maxChars - 1) + '…' : s
    },
    tooltip: { show: true },
    pageIconSize: 10,
    pageIconColor: colors.textMuted,
    pageIconInactiveColor: colors.border,
    pageTextStyle: { color: colors.textMuted, fontSize: 10 },
  }
}

function computeTimeIndex(xData: any[], targetStr: string): number | null {
  const times = xData.map(v => {
    if (typeof v === 'number' && isFinite(v)) return v
    const d = Date.parse(String(v))
    return isNaN(d) ? null : d
  })
  if (times.some(t => t === null)) return null
  const target = Date.parse(targetStr)
  if (isNaN(target)) return null
  const t = times as number[]
  if (target <= t[0]) return 0
  if (target >= t[t.length - 1]) return t.length - 1
  for (let i = 0; i < t.length - 1; i++) {
    if (target >= t[i] && target < t[i + 1]) {
      return i + (target - t[i]) / (t[i + 1] - t[i])
    }
  }
  return null
}

export function buildMarkLineSeries(
  markLines: MarkLineConfig[] | undefined,
  xData: any[],
  yMin: number,
  yMax: number,
): { series: any[]; xAxis?: any } {
  if (!markLines || markLines.length === 0) return { series: [] }

  const mlSeries: any[] = []
  let needsNumAxis = false

  for (const ml of markLines) {
    const val = String(ml.value ?? '')
    const lineColor = ml.color || '#f43f5e'
    const labelColor = getContrastTextColor(lineColor)
    const bgColor = lineColor + 'cc'
    if (ml.position === 'horizontal') {
      const isPureNum = /^-?\d+(\.\d+)?$/.test(val.trim())
      if (!isPureNum) continue
      const y = parseFloat(val)
      const n = xData.length
      needsNumAxis = true
      mlSeries.push({
        type: 'line' as const,
        xAxisIndex: 1,
        symbol: 'circle',
        symbolSize: 0,
        data: [
          [-0.5, y],
          { value: [n - 0.5, y], label: ml.label ? { show: true, formatter: ml.label, fontSize: 11, color: labelColor, backgroundColor: bgColor, padding: [2, 6], borderRadius: 3, position: 'left' as const } : undefined },
        ],
        lineStyle: { type: 'dashed' as const, color: lineColor, width: 1.5 },
        silent: true, z: 10, smooth: false, connectNulls: true,
        tooltip: { show: false },
      })
      continue
    }
    // Vertical
    const isPureNum = /^-?\d+(\.\d+)?$/.test(val.trim())
    const numericVal = isPureNum ? parseFloat(val) : NaN
    const catIdx = xData.indexOf(val)
    const timeIdx = catIdx < 0 ? computeTimeIndex(xData, val) : null

    let xPos: any
    let axisIdx = 0

    if (isPureNum) {
      xPos = numericVal
    } else if (catIdx >= 0) {
      xPos = catIdx
    } else if (timeIdx !== null) {
      xPos = timeIdx
      axisIdx = 1
      needsNumAxis = true
    } else {
      xPos = val
    }

    mlSeries.push({
      type: 'line' as const,
      xAxisIndex: axisIdx,
      symbol: 'circle',
      symbolSize: 0,
      data: [
        [xPos, yMin],
        { value: [xPos, yMax], label: ml.label ? { show: true, formatter: ml.label, fontSize: 11, color: labelColor, backgroundColor: bgColor, padding: [2, 6], borderRadius: 3, position: 'bottom' as const } : undefined },
      ],
      lineStyle: { type: 'dashed' as const, color: lineColor, width: 1.5 },
      silent: true, z: 10, smooth: false, connectNulls: true,
      tooltip: { show: false },
    })
  }

  return {
    series: mlSeries,
    ...(needsNumAxis ? {
      xAxis: {
        type: 'value' as const,
        min: -0.5,
        max: xData.length - 0.5,
        show: false,
        gridIndex: 0,
      }
    } : {}),
  }
}

interface EChartsContainerProps {
  option: echarts.EChartsCoreOption
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

export const EChartsContainer = memo(function EChartsContainer({ option, height, onChartReady, notMerge = true, showReset = false }: EChartsContainerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const wrapperRef = useRef<HTMLDivElement>(null)
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
    // Merge transparent background for theme compatibility and default the
    // chart's text to the product's humanist sans (data text overrides to
    // mono in the shared axis/tooltip helpers).
    const themedOption = {
      textStyle: { fontFamily: CHART_FONT_SANS },
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

    const hasSize = containerRef.current &&
      containerRef.current.clientWidth > 0 &&
      containerRef.current.clientHeight > 0

    if (hasSize) {
      try {
        chartRef.current.setOption(finalOption, { notMerge })
      } catch {
        // option may be incomplete during transitions; resize will retry
      }
    }

    const ro = new ResizeObserver(() => {
      try {
        if (chartRef.current && containerRef.current &&
            containerRef.current.clientHeight > 0) {
          const currentOpt = chartRef.current.getOption() as any
          if (!currentOpt?.series?.length) {
            chartRef.current.setOption(finalOption, { notMerge })
          } else {
            chartRef.current.resize()
          }
        }
      } catch {
        // ignore — ECharts may throw during transition; next observation recovers
      }
    })
    ro.observe(containerRef.current)
    return () => ro.disconnect()
  }, [option])

  // Charts mounted while hidden (display:none) initialize at 0×0 and the
  // ResizeObserver above never sees the reveal. Redraw when the wrapper
  // becomes visible again (live preview variant switching, tabs, panels).
  const optionRef = useRef(option)
  optionRef.current = option
  useEffect(() => {
    const el = wrapperRef.current
    // jsdom (tests) and very old browsers lack IntersectionObserver.
    if (!el || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver((entries) => {
      if (!entries.some((e) => e.isIntersecting)) return
      const chart = chartRef.current
      const container = containerRef.current
      if (!chart || !container || container.clientHeight === 0) return
      try {
        const currentOpt = chart.getOption() as any
        if (!currentOpt?.series?.length) {
          chart.setOption({ ...optionRef.current, backgroundColor: 'transparent' }, { notMerge: true })
        } else {
          chart.resize()
        }
      } catch {
        // ignore — ECharts may throw during transition; next reveal recovers
      }
    })
    io.observe(el)
    return () => io.disconnect()
  }, [])

  useEffect(() => {
    return () => {
      chartRef.current?.dispose()
      chartRef.current = null
    }
  }, [])

  return (
    <div ref={wrapperRef} style={{ position: 'relative', flex: 1, minHeight: 0, ...(height != null ? { height } : {}) }}>
      <div data-testid="chart-container" ref={containerRef} style={{ position: 'absolute', inset: 0 }} />
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
