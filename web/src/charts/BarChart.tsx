import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle, axisStyle } from './common'
import { AxisConfigPanel } from './AxisConfigPanel'

function BarChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const xAxis = config.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length ? config.yAxis : columns.slice(1, 2)
  const isStacked = config.chartType === 'stacked_bar'

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const option = {
    tooltip: { trigger: 'axis' as const, ...tooltipStyle },
    legend: config.showLegend !== false ? { top: 0, textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    grid: { top: config.showLegend !== false ? 30 : 8, right: 16, bottom: 8, left: 0, containLabel: true },
    xAxis: { type: 'category' as const, data: chartData.map(d => d[xAxis]), ...axisStyle },
    yAxis: { type: 'value' as const, ...axisStyle },
    series: yAxes.map((y, i) => ({
      name: y,
      type: 'bar' as const,
      data: chartData.map(d => d[y]),
      stack: isStacked ? 'a' : undefined,
      itemStyle: {
        color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length],
        borderRadius: [3, 3, 0, 0] as [number, number, number, number],
      },
      label: config.showLabels ? { show: true, position: 'top' as const, fontSize: 10, color: 'var(--text-muted)' } : undefined,
      animation: false,
    })),
  }

  return <EChartsContainer option={option} />
}

function BarConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return <AxisConfigPanel config={config} columns={columns} onChange={onChange} showStack />
}

export const BarChartModule: ChartModule = {
  Component: BarChartComponent,
  ConfigPanel: BarConfigPanel,
  defaultConfig: { chartType: 'bar', showLegend: true, showGrid: true, showLabels: false, skipEmpty: true },
  detectColumns: (columns) => ({ xAxis: columns[0]?.name, yAxis: columns.slice(1, 2).map(c => c.name) }),
  requirements: { minColumns: 2 },
}
