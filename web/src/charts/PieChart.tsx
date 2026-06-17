import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getChartColors, useRowsAsObjects, useAxisColumns, detectAxisColumns } from './common'
import { ConfigHint } from './ConfigHint'

function PieChartComponent({ data, config }: ChartProps) {
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const colors = useMemo(() => getChartColors(), [])
  const valueKey = yAxes[0] ?? data.columns[1]?.name ?? ''
  const isDonut = config.chartType === 'donut'

  const option = useMemo(() => ({
    tooltip: { trigger: 'item' as const, ...getTooltipStyle(), formatter: '{b}: {c} ({d}%)' },
    legend: config.showLegend !== false ? { orient: 'vertical' as const, right: 10, top: 'center', textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
    series: [{
      type: 'pie' as const,
      radius: isDonut ? ['40%', '70%'] as [string, string] : ['0%', '70%'] as [string, string],
      center: config.showLegend !== false ? ['40%', '50%'] as [string, string] : ['50%', '50%'] as [string, string],
      data: chartData.map((d, i) => ({
        name: d[xAxis],
        value: d[valueKey],
        itemStyle: { color: config.seriesColors?.[String(d[xAxis])] ?? CHART_COLORS[i % CHART_COLORS.length] },
      })),
      label: config.showLabels !== false ? { fontSize: 11, color: colors.text } : { show: false },
      emphasis: { itemStyle: { shadowBlur: 10, shadowColor: 'rgba(0,0,0,0.2)' } },
      animation: false,
    }],
  }), [chartData, xAxis, valueKey, isDonut, config.seriesColors, config.showLegend, config.showLabels, colors])

  return <EChartsContainer option={option} />
}

function PieConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Name column</div>
        <select
          aria-label="Name column"
          style={styles.select}
          value={config.xAxis ?? ''}
          onChange={e => onChange({ ...config, xAxis: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Column for slice labels (categories)</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Value column</div>
        <select
          aria-label="Value column"
          style={styles.select}
          value={config.yAxis?.[0] ?? ''}
          onChange={e => onChange({ ...config, yAxis: [e.target.value] })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Column for slice sizes (numeric values)</ConfigHint>
      </div>
      <label style={styles.checkbox}>
        <input
          type="checkbox"
          checked={config.chartType === 'donut'}
          onChange={e => onChange({ ...config, chartType: e.target.checked ? 'donut' : 'pie' })}
        />
        Donut (ring)
      </label>
      <ConfigHint>Show as a ring chart with a hole in the center</ConfigHint>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  section: { display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
}

export const PieChartModule: ChartModule = {
  Component: PieChartComponent,
  ConfigPanel: PieConfigPanel,
  defaultConfig: { chartType: 'pie', showLegend: true, showLabels: true, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
