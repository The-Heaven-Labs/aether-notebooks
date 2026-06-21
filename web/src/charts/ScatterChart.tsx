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
    title: config.title ? { text: config.title, left: 'center', textStyle: { fontSize: 14, color: colors.text } } : undefined,
    legend: config.showLegend !== false && yAxes.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
    grid: { top: config.showLegend !== false && yAxes.length > 1 ? 30 : 8, right: 16, bottom: 32, left: 16, containLabel: true },
    visualMap: config.colorColumn ? {
      dimension: 2,
      min: 0,
      max: 100,
      calculable: true,
      inRange: { color: ['#6366f1', '#06b6d4', '#10b981', '#f59e0b', '#f43f5e'] },
      textStyle: { color: colors.textMuted, fontSize: 10 },
    } : undefined,
    dataZoom: [
      { type: 'inside' as const, start: 0, end: 100 },
      { type: 'slider' as const, start: 0, end: 100, bottom: 8, height: 20, borderColor: colors.border, textStyle: { fontSize: 10, color: colors.textMuted } },
    ],
    xAxis: { type: 'value' as const, name: xAxis, ...getAxisStyle(config.showGrid) },
    yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'scatter' as const,
      data: chartData.map(d => {
        const vals = [d[xAxis], d[y]]
        if (config.colorColumn || config.sizeColumn) {
          const third = d[config.colorColumn || config.sizeColumn || '']
          vals.push(Number(third) || 0)
        }
        return vals
      }),
      symbolSize: config.sizeColumn
        ? (val: number[]) => Math.max(4, Math.min(40, val[2] ?? 8))
        : 8,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length], opacity: 0.8 },
    })),
  }), [chartData, xAxis, yAxes, config.title, config.seriesColors, config.showLegend, config.showGrid, config.colorColumn, config.sizeColumn, colors])

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
