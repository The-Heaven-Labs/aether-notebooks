import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, useRowsAsObjects, useAxisColumns, detectAxisColumns } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function AreaChartComponent({ data, config }: ChartProps) {
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const colors = useMemo(() => getChartColors(), [])

  const option = useMemo(() => ({
    tooltip: { trigger: 'axis' as const, ...getTooltipStyle() },
    legend: config.showLegend !== false ? { top: 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
    grid: { top: config.showLegend !== false ? 30 : 8, right: 16, bottom: 8, left: 16, containLabel: true },
    xAxis: { type: 'category' as const, data: chartData.map(d => d[xAxis]), boundaryGap: false, ...getAxisStyle(config.showGrid) },
    yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'line' as const,
      data: chartData.map(d => d[y]),
      smooth: false,
      areaStyle: { opacity: 0.15 },
      symbol: 'circle',
      symbolSize: 4,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length] },
      lineStyle: { width: 2 },
      label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
      animation: false,
    })),
  }), [chartData, xAxis, yAxes, config.seriesColors, config.showLegend, config.showLabels, config.showGrid, colors])

  return <EChartsContainer option={option} />
}

function AreaConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} />
}

export const AreaChartModule: ChartModule = {
  Component: AreaChartComponent,
  ConfigPanel: AreaConfigPanel,
  defaultConfig: { chartType: 'area', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
