import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, detectAxisColumns, isNumericType } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function ScatterChartComponent({ data, config }: ChartProps) {
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
    tooltip: { ...getTooltipStyle() },
    legend: config.showLegend !== false && yAxes.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: getChartColors().textMuted } } : undefined,
    grid: { top: config.showLegend !== false && yAxes.length > 1 ? 30 : 8, right: 16, bottom: 8, left: 16, containLabel: true },
    xAxis: { type: 'value' as const, name: xAxis, ...getAxisStyle() },
    yAxis: { type: 'value' as const, ...getAxisStyle() },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'scatter' as const,
      data: chartData.map(d => [d[xAxis], d[y]]),
      symbolSize: 8,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length], opacity: 0.8 },
      animation: false,
    })),
  }), [chartData, xAxis, yAxes, config.seriesColors, config.showLegend])

  return <EChartsContainer option={option} />
}

function ScatterConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} />
}

export const ScatterChartModule: ChartModule = {
  Component: ScatterChartComponent,
  ConfigPanel: ScatterConfigPanel,
  defaultConfig: { chartType: 'scatter', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
