import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, detectAxisColumns, isNumericType } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function LineChartComponent({ data, config }: ChartProps) {
  const columns = useMemo(() => data.columns.map(c => c.name), [data.columns])

  // Smart defaults: detect numeric columns for yAxis if not configured
  const detected = (!config.xAxis && !config.yAxis?.length) 
    ? detectAxisColumns(data.columns) 
    : { xAxis: config.xAxis, yAxis: config.yAxis }
  
  const xAxis = config.xAxis ?? detected.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length 
    ? config.yAxis 
    : (detected.yAxis?.length ? detected.yAxis : columns.filter((_, i) => i > 0 && isNumericType(data.columns[i]?.type)).slice(0, 1))

  const chartData = useMemo(() => {
    return data.rows.map(row => {
      const obj: Record<string, unknown> = {}
      columns.forEach((col, i) => { obj[col] = row[i] })
      return obj
    })
  }, [data.rows, columns])

  const option = useMemo(() => ({
    tooltip: { trigger: 'axis' as const, ...getTooltipStyle() },
    legend: config.showLegend !== false ? { top: 0, textStyle: { fontSize: 11, color: getChartColors().textMuted } } : undefined,
    grid: { top: config.showLegend !== false ? 30 : 8, right: 16, bottom: 8, left: 16, containLabel: true },
    xAxis: { type: 'category' as const, data: chartData.map(d => d[xAxis]), ...getAxisStyle() },
    yAxis: { type: 'value' as const, ...getAxisStyle() },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'line' as const,
      data: chartData.map(d => d[y]),
      smooth: false,
      symbol: 'circle',
      symbolSize: 6,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length] },
      lineStyle: { width: 2 },
      label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: getChartColors().textMuted } : undefined,
      animation: false,
    })),
  }), [chartData, xAxis, yAxes, config.seriesColors, config.showLegend, config.showLabels])

  return <EChartsContainer option={option} />
}

function LineConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} />
}

export const LineChartModule: ChartModule = {
  Component: LineChartComponent,
  ConfigPanel: LineConfigPanel,
  defaultConfig: { chartType: 'line', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
