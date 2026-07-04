import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, useChartColors, useRowsAsObjects, useAxisColumns, useGroupBySeries, detectAxisColumns } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function AreaChartComponent({ data, config }: ChartProps) {
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const colors = useChartColors()
  const { series: groupSeries, xValues } = useGroupBySeries(chartData, { ...config, xAxis, yAxis: yAxes })
  const hasGroupBy = !!(config.groupBy && chartData.some(row => config.groupBy! in row))
  const isStacked = config.areaMode === 'stacked'

  const option = useMemo(() => {
    const effectiveXData = hasGroupBy ? xValues : chartData.map(d => d[xAxis])

    const series = hasGroupBy
      ? groupSeries.map(s => ({
          ...s,
          type: 'line' as const,
          stack: isStacked ? 'a' : undefined,
          smooth: config.smooth ?? false,
          connectNulls: config.connectNulls ?? false,
          areaStyle: { opacity: 0.15 },
          symbol: 'circle',
          symbolSize: 4,
          lineStyle: { width: 2 },
          label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
        }))
      : yAxes.map((y, i) => ({
          name: y,
          type: 'line' as const,
          stack: isStacked ? 'a' : undefined,
          data: chartData.map(d => d[y]),
          smooth: config.smooth ?? false,
          connectNulls: config.connectNulls ?? false,
          areaStyle: { opacity: 0.15 },
          symbol: 'circle',
          symbolSize: 4,
          itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length] },
          lineStyle: { width: 2 },
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
      xAxis: { type: 'category' as const, data: effectiveXData, boundaryGap: false, ...getAxisStyle(config.showGrid) },
      yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
      series,
    }
  }, [chartData, xAxis, yAxes, hasGroupBy, groupSeries, xValues, config.title, config.seriesColors, config.showLegend, config.showLabels, config.showGrid, config.connectNulls, config.dataZoom, config.smooth, colors])
  // title is used in grid.top calculation above

  return <EChartsContainer option={option} showReset />
}

function AreaConfigPanel({ config, columns, onChange, data, groupValues }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} showStack showPieOptions={false} data={data} groupValues={groupValues} />
}

export const AreaChartModule: ChartModule = {
  Component: AreaChartComponent,
  ConfigPanel: AreaConfigPanel,
  defaultConfig: { chartType: 'area', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
