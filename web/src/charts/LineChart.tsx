import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, useRowsAsObjects, useAxisColumns, detectAxisColumns } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function LineChartComponent({ data, config }: ChartProps) {
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const colors = useMemo(() => getChartColors(), [])

  const option = useMemo(() => ({
    tooltip: { trigger: 'axis' as const, ...getTooltipStyle() },
    title: config.title ? { text: config.title, left: 'center', textStyle: { fontSize: 14, color: colors.text } } : undefined,
    legend: config.showLegend !== false ? { top: 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
    grid: { top: config.showLegend !== false ? 30 : 8, right: 16, bottom: config.dataZoom ? 32 : 8, left: 16, containLabel: true },
    dataZoom: config.dataZoom ? [
      { type: 'inside' as const, start: 0, end: 100 },
      { type: 'slider' as const, start: 0, end: 100, bottom: 8, height: 20, borderColor: colors.border, textStyle: { fontSize: 10, color: colors.textMuted } },
    ] : undefined,
    xAxis: { type: 'category' as const, data: chartData.map(d => d[xAxis]), ...getAxisStyle(config.showGrid) },
    yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'line' as const,
      data: chartData.map(d => d[y]),
      smooth: config.smooth ?? false,
      connectNulls: config.connectNulls ?? false,
      symbol: 'circle',
      symbolSize: 6,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length] },
      lineStyle: { width: 2 },
      label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
    })),
  }), [chartData, xAxis, yAxes, config.title, config.seriesColors, config.showLegend, config.showLabels, config.showGrid, config.dataZoom, config.smooth, config.connectNulls, colors])

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
