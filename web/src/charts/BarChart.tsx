import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, useChartColors, useRowsAsObjects, useAxisColumns, useGroupBySeries, detectAxisColumns, buildMarkLineSeries } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function BarChartComponent({ data, config }: ChartProps) {
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const isStacked = config.barMode === 'stacked'
  const isHorizontal = config.barMode === 'horizontal'
  const colors = useChartColors()

  const { series: groupSeries, xValues } = useGroupBySeries(chartData, { ...config, xAxis, yAxis: yAxes }, colors.palette)
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
          label: config.showLabels ? { show: true, position: isHorizontal ? 'right' as const : 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
          itemStyle: { ...s.itemStyle, borderRadius: isHorizontal ? [0, 3, 3, 0] as [number, number, number, number] : [3, 3, 0, 0] as [number, number, number, number] },
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
            borderRadius: isHorizontal ? [0, 3, 3, 0] as [number, number, number, number] : [3, 3, 0, 0] as [number, number, number, number],
          },
          label: config.showLabels ? { show: true, position: isHorizontal ? 'right' as const : 'top' as const, fontSize: 10, color: colors.textMuted } : undefined,
        }))

    // Reference lines as ghost series
    const allY = series.flatMap((s: any) => (s.data ?? []).filter((v: any) => v != null && isFinite(Number(v)))).map(Number)
    const yMin = Math.min(0, ...allY)
    const yMax = Math.max(0, ...allY)
    const { series: mlSeries, xAxis: mlXAxis } = buildMarkLineSeries(config.markLines, effectiveXData, yMin, yMax)

    const grid = isHorizontal
      ? { top: config.title ? 56 : config.showLegend !== false ? 30 : 8, right: 16, bottom: config.dataZoom ? 42 : 8, left: 16, containLabel: true }
      : { top: config.title ? 56 : config.showLegend !== false ? 30 : 8, right: 16, bottom: config.dataZoom ? 32 : 8, left: 16, containLabel: true }

    const catAxis = { type: 'category' as const, data: effectiveXData, ...getAxisStyle(config.showGrid) }
    const valAxis = { type: config.logScale ? 'log' as const : 'value' as const, ...getAxisStyle(config.showGrid) }

    return {
      tooltip: { trigger: isHorizontal ? 'axis' as const : 'axis' as const, axisPointer: isHorizontal ? { type: 'shadow' as const } : undefined, ...getTooltipStyle() },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
      legend: config.showLegend !== false ? { show: true, top: config.title ? 32 : 0, textStyle: { fontSize: 11, color: colors.textMuted } } : { show: false },
      grid,
      dataZoom: config.dataZoom ? [
        { type: 'inside' as const, start: 0, end: 100, ...(isHorizontal ? { yAxisIndex: 0 } : {}) },
        { type: 'slider' as const, start: 0, end: 100, bottom: 8, height: 20, borderColor: colors.border, textStyle: { fontSize: 10, color: colors.textMuted } },
      ] : undefined,
      xAxis: mlXAxis ? [isHorizontal ? valAxis : catAxis, mlXAxis].filter(Boolean) : (isHorizontal ? valAxis : catAxis),
      yAxis: isHorizontal ? catAxis : valAxis,
      series: [...series, ...mlSeries],
    }
  }, [chartData, xAxis, yAxes, isStacked, isHorizontal, hasGroupBy, groupSeries, xValues, config.title, config.seriesColors, config.showLegend, config.showLabels, config.showGrid, config.dataZoom, config.barWidth, config.barCategoryGap, config.markLines, config.logScale, colors])

  return <EChartsContainer option={option} showReset />
}

function BarConfigPanel({ config, columns, onChange, data, groupValues }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} data={data} groupValues={groupValues} />
}

export const BarChartModule: ChartModule = {
  Component: BarChartComponent,
  ConfigPanel: BarConfigPanel,
  defaultConfig: { chartType: 'bar', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
