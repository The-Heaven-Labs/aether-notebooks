import type React from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle } from './common'

function PieChartComponent({ data, config }: ChartProps) {
  const columns = data.columns.map(c => c.name)
  const xAxis = config.xAxis ?? columns[0] ?? ''
  const yAxes = config.yAxis?.length ? config.yAxis : columns.slice(1, 2)
  const valueKey = yAxes[0] ?? columns[1] ?? ''
  const isDonut = config.chartType === 'donut'

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const option = {
    tooltip: { trigger: 'item' as const, ...tooltipStyle, formatter: '{b}: {c} ({d}%)' },
    legend: config.showLegend !== false ? { orient: 'vertical' as const, right: 10, top: 'center', textStyle: { fontSize: 11, color: 'var(--text-muted)' } } : undefined,
    series: [{
      type: 'pie' as const,
      radius: isDonut ? ['40%', '70%'] as [string, string] : ['0%', '70%'] as [string, string],
      center: config.showLegend !== false ? ['40%', '50%'] as [string, string] : ['50%', '50%'] as [string, string],
      data: chartData.map((d, i) => ({
        name: d[xAxis],
        value: d[valueKey],
        itemStyle: { color: config.seriesColors?.[String(d[xAxis])] ?? CHART_COLORS[i % CHART_COLORS.length] },
      })),
      label: config.showLabels !== false ? { fontSize: 11, color: 'var(--text-primary)' } : { show: false },
      emphasis: { itemStyle: { shadowBlur: 10, shadowColor: 'rgba(0,0,0,0.2)' } },
      animation: false,
    }],
  }

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
      </div>
      <label style={styles.checkbox}>
        <input
          type="checkbox"
          checked={config.chartType === 'donut'}
          onChange={e => onChange({ ...config, chartType: e.target.checked ? 'donut' : 'pie' })}
        />
        Donut (ring)
      </label>
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
  detectColumns: (columns) => ({ xAxis: columns[0]?.name, yAxis: columns.slice(1, 2).map(c => c.name) }),
  requirements: { minColumns: 2 },
}
