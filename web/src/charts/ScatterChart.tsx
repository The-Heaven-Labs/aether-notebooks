import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, useChartColors, useRowsAsObjects, useAxisColumns, detectAxisColumns, buildMarkLineSeries } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function ScatterChartComponent({ data, config }: ChartProps) {
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const colors = useChartColors()
  const hasGroupBy = !!(config.groupBy && chartData.some(row => config.groupBy! in row))

  const option = useMemo(() => {
    const series = hasGroupBy
      ? (() => {
          const groups = [...new Set(chartData.map(d => String(d[config.groupBy!])))]
          return groups.map((group, gi) => ({
            name: group,
            type: 'scatter' as const,
            data: chartData
              .filter(d => String(d[config.groupBy!]) === group)
              .map(d => {
                const vals = [d[xAxis], d[yAxes[0]]]
                if (config.colorColumn || config.sizeColumn) {
                  const third = d[config.colorColumn || config.sizeColumn || '']
                  vals.push(Number(third) || 0)
                }
                return vals
              }),
            symbolSize: config.sizeColumn
              ? (val: number[]) => Math.max(4, Math.min(40, val[2] ?? 8))
              : 8,
            itemStyle: { color: config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length], opacity: 0.8 },
          }))
        })()
      : yAxes.map((y, i) => ({
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
        }))

    const allPts = series.flatMap((s: any) => (s.data ?? []).filter((p: any) => Array.isArray(p) && p.length >= 2))
    const xVals = allPts.map((p: any[]) => Number(p[0])).filter((v: any) => isFinite(v))
    const yVals = allPts.map((p: any[]) => Number(p[1])).filter((v: any) => isFinite(v))
    const xMin = Math.min(...xVals)
    const xMax = Math.max(...xVals)
    const yMin = Math.min(0, ...yVals)
    const yMax = Math.max(0, ...yVals)
    const { series: mlSeries, xAxis: mlXAxis } = buildMarkLineSeries(config.markLines, xVals, yMin, yMax)
    // For scatter's value x-axis, override horizontal line data to use actual x range
    for (const s of mlSeries) {
      if (s.xAxisIndex !== 1 && s.data?.length === 2) {
        const d0 = Array.isArray(s.data[0]) ? s.data[0] : s.data[0]?.value
        const d1 = Array.isArray(s.data[1]) ? s.data[1] : s.data[1]?.value
        if (d0) d0[0] = xMin
        if (d1) d1[0] = xMax
      }
    }

    const baseXAxis = { type: 'value' as const, name: xAxis, ...getAxisStyle(config.showGrid) }

    return {
      tooltip: { ...getTooltipStyle() },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
      legend: config.showLegend !== false && (hasGroupBy ? series.length > 1 : yAxes.length > 1) ? { show: true, top: config.title ? 32 : 0, textStyle: { fontSize: 11, color: colors.textMuted } } : { show: false },
      grid: { top: config.title ? 56 : config.showLegend !== false && (hasGroupBy ? series.length > 1 : yAxes.length > 1) ? 30 : 8, right: 16, bottom: 32, left: 16, containLabel: true },
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
      xAxis: mlXAxis ? [baseXAxis, mlXAxis] : baseXAxis,
      yAxis: { type: 'value' as const, ...getAxisStyle(config.showGrid) },
      series: [...series, ...mlSeries],
    }
  }, [chartData, xAxis, yAxes, hasGroupBy, config.title, config.seriesColors, config.showLegend, config.showGrid, config.colorColumn, config.sizeColumn, config.groupBy, config.markLines, colors])

  return <EChartsContainer option={option} showReset />
}

function ScatterConfigPanel({ config, columns, onChange, data, groupValues }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} data={data} groupValues={groupValues} />
}

export const ScatterChartModule: ChartModule = {
  Component: ScatterChartComponent,
  ConfigPanel: ScatterConfigPanel,
  defaultConfig: { chartType: 'scatter', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
