import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, useChartColors, useRowsAsObjects, useAxisColumns, useGroupBySeries, detectAxisColumns } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function BarChartComponent({ data, config }: ChartProps) {
  const columns = useMemo(() => data.columns.map(c => c.name), [data.columns])
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const isStacked = config.chartType === 'stacked_bar'
  const colors = useChartColors()

  const { series: groupSeries, xValues } = useGroupBySeries(chartData, { ...config, xAxis, yAxis: yAxes })
  const hasGroupBy = !!(config.groupBy && chartData.some(row => config.groupBy! in row))

  const option = useMemo(() => {
    const effectiveXData = hasGroupBy ? xValues : chartData.map(d => d[xAxis])

    const series = hasGroupBy
      ? groupSeries.map(s => ({
          ...s,
          type: 'bar' as const,
          stack: isStacked ? 'a' : undefined,
          barWidth: config.barWidth,
          barCategoryGap: config.barCategoryGap,
          label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
          itemStyle: { ...s.itemStyle, borderRadius: [3, 3, 0, 0] as [number, number, number, number] },
        }))
      : yAxes.map((y, i) => ({
          name: y,
          type: 'bar' as const,
          data: chartData.map(d => d[y]),
          stack: isStacked ? 'a' : undefined,
          barWidth: config.barWidth,
          barCategoryGap: config.barCategoryGap,
          itemStyle: {
            color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length],
            borderRadius: [3, 3, 0, 0] as [number, number, number, number],
          },
          label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
        }))

    return {
      tooltip: { trigger: 'axis' as const, ...getTooltipStyle() },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
      legend: config.showLegend !== false ? { show: true, top: config.title ? 32 : 0, textStyle: { fontSize: 11, color: colors.textMuted } } : { show: false },
      grid: { top: config.title ? 56 : config.showLegend !== false ? 30 : 8, right: 16, bottom: config.dataZoom ? 32 : 8, left: 16, containLabel: true },
      dataZoom: config.dataZoom ? [
        { type: 'inside' as const, start: 0, end: 100 },
        { type: 'slider' as const, start: 0, end: 100, bottom: 8, height: 20, borderColor: colors.border, textStyle: { fontSize: 10, color: colors.textMuted } },
      ] : undefined,
      xAxis: { type: 'category' as const, data: effectiveXData, ...getAxisStyle(config.showGrid) },
      yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
      series,
    }
  }, [chartData, xAxis, yAxes, isStacked, hasGroupBy, groupSeries, xValues, config.title, config.seriesColors, config.showLegend, config.showLabels, config.showGrid, config.dataZoom, config.barWidth, config.barCategoryGap, colors])

  return <EChartsContainer option={option} showReset />
}

function BarConfigPanel({ config, columns, onChange, data, groupValues }: ConfigPanelProps) {
  const isPie = config.chartType === 'pie' || config.chartType === 'donut'
  const isBarType = config.chartType === 'bar' || config.chartType === 'stacked_bar'
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} showStack={isBarType} showPieOptions={isPie} data={data} groupValues={groupValues} />
}

export const BarChartModule: ChartModule = {
  Component: BarChartComponent,
  ConfigPanel: BarConfigPanel,
  defaultConfig: { chartType: 'bar', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
