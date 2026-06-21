import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, useRowsAsObjects, useAxisColumns, detectAxisColumns } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function ScatterChartComponent({ data, config }: ChartProps) {
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const colors = useMemo(() => getChartColors(), [])

  const option = useMemo(() => ({
    tooltip: { ...getTooltipStyle() },
    legend: config.showLegend !== false && yAxes.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
    grid: { top: config.showLegend !== false && yAxes.length > 1 ? 30 : 8, right: 16, bottom: 8, left: 16, containLabel: true },
    xAxis: { type: 'value' as const, name: xAxis, ...getAxisStyle(config.showGrid) },
    yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'scatter' as const,
      data: chartData.map(d => [d[xAxis], d[y]]),
      symbolSize: 8,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length], opacity: 0.8 },
      animation: false,
    })),
  }), [chartData, xAxis, yAxes, config.seriesColors, config.showLegend, config.showGrid, colors])

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
