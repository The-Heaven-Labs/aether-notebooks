import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle, axisStyle, detectAxisColumns } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function ScatterChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const xAxis = config.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length ? config.yAxis : columns.slice(1, 2)

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const option = {
    tooltip: { ...tooltipStyle },
    legend: config.showLegend !== false && yAxes.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    grid: { top: config.showLegend !== false && yAxes.length > 1 ? 30 : 8, right: 16, bottom: 8, left: 16, containLabel: true },
    xAxis: { type: 'value' as const, name: xAxis, ...axisStyle },
    yAxis: { type: 'value' as const, ...axisStyle },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'scatter' as const,
      data: chartData.map(d => [d[xAxis], d[y]]),
      symbolSize: 8,
      itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length], opacity: 0.8 },
      animation: false,
    })),
  }

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
