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
      chartRef.current = echarts.init(containerRef.current)
    }
    chartRef.current.setOption(option, { notMerge: true })
  }, [option])

  useEffect(() => {
    return () => {
      chartRef.current?.dispose()
      chartRef.current = null
    }
  }, [])

  return <div data-testid="chart-container" ref={containerRef} style={{ height, width: '100%' }} />
})
