import { memo, useRef, useEffect } from 'react'
import * as echarts from 'echarts/core'
import { BarChart, LineChart, ScatterChart, PieChart, TreeChart } from 'echarts/charts'
import {
  GridComponent, TooltipComponent, LegendComponent,
  DataZoomComponent, ToolboxComponent,
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
  
  // Check base type (strip params like Decimal(10,2))
  const base = t.replace(/\(.*\)/, '').trim()
  if (NUMERIC_TYPES.has(base)) return true
  
  // Handle wrappers like Nullable(Int64), LowCardinality(Float64)
  for (const wrapper of ['nullable', 'lowcardinality']) {
    if (t.startsWith(wrapper + '(') && t.endsWith(')')) {
      const inner = t.slice(wrapper.length + 1, -1).trim()
      // Check inner type (may have its own params)
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
  const containerRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<echarts.ECharts | null>(null)

  useEffect(() => {
    if (!containerRef.current) return
    if (!chartRef.current) {
      chartRef.current = echarts.init(containerRef.current, undefined, {
        renderer: 'canvas',
      })
    }
    // Merge transparent background for theme compatibility
    const themedOption = {
      ...option,
      backgroundColor: 'transparent',
    }
    chartRef.current.setOption(themedOption, { notMerge: true })

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

  return <div data-testid="chart-container" ref={containerRef} style={{ height, width: '100%' }} />
})
