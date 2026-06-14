import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, useRowsAsObjects, useAxisColumns, detectAxisColumns } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function BarChartComponent({ data, config }: ChartProps) {
  const columns = useMemo(() => data.columns.map(c => c.name), [data.columns])
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const isStacked = config.chartType === 'stacked_bar'
  const colors = useMemo(() => getChartColors(), [])

  const option = useMemo(() => ({
    tooltip: { trigger: 'axis' as const, ...getTooltipStyle() },
    legend: config.showLegend !== false ? { top: 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
    grid: { top: config.showLegend !== false ? 30 : 8, right: 16, bottom: 8, left: 16, containLabel: true },
    xAxis: { type: 'category' as const, data: chartData.map(d => d[xAxis]), ...getAxisStyle() },
    yAxis: { type: 'value' as const, ...getAxisStyle() },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'bar' as const,
      data: chartData.map(d => d[y]),
      stack: isStacked ? 'a' : undefined,
      itemStyle: {
        color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length],
        borderRadius: [3, 3, 0, 0] as [number, number, number, number],
      },
      label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
      animation: false,
    })),
  }), [chartData, xAxis, yAxes, isStacked, config.seriesColors, config.showLegend, config.showLabels, colors])

  return <EChartsContainer option={option} />
}

function BarConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  const isPie = config.chartType === 'pie' || config.chartType === 'donut'
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} showStack={!isPie} showPieOptions={isPie} />
}

export const BarChartModule: ChartModule = {
  Component: BarChartComponent,
  ConfigPanel: BarConfigPanel,
  defaultConfig: { chartType: 'bar', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
