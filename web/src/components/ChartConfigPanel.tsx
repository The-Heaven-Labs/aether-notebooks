import type React from 'react'

export interface ChartConfig {
  chartType: 'bar' | 'stacked_bar' | 'line' | 'area' | 'scatter' | 'pie' | 'donut'
  xAxis: string
  yAxis: string[]
  title?: string
  showLegend?: boolean
  showGrid?: boolean
}

interface ChartConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
}

export function ChartConfigPanel({ config, columns, onChange }: ChartConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.field}>
        <label htmlFor="chart-type" style={styles.label}>Chart type</label>
        <select
          id="chart-type"
          style={styles.select}
          value={config.chartType}
          onChange={e => onChange({ ...config, chartType: e.target.value as ChartConfig['chartType'] })}
        >
          {(['bar', 'stacked_bar', 'line', 'area', 'scatter', 'pie', 'donut'] as const).map(t => (
            <option key={t} value={t}>{t.replace('_', ' ')}</option>
          ))}
        </select>
      </div>
      <div style={styles.field}>
        <label htmlFor="chart-x-axis" style={styles.label}>X axis</label>
        <select
          id="chart-x-axis"
          style={styles.select}
          value={config.xAxis}
          onChange={e => onChange({ ...config, xAxis: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <div style={{ ...styles.field, gridColumn: '1 / -1' }}>
        <label htmlFor="chart-y-axis" style={styles.label}>Y axis</label>
        <select
          id="chart-y-axis"
          multiple
          style={styles.select}
          value={config.yAxis}
          onChange={e => {
            const selected = Array.from(e.target.selectedOptions).map(o => o.value)
            onChange({ ...config, yAxis: selected })
          }}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { borderTop: '1px solid var(--border)', padding: '10px 12px', display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 },
  field: { display: 'flex', flexDirection: 'column', gap: 4 },
  label: { fontSize: 11, color: 'var(--text-muted)', fontWeight: 500 },
  select: { fontSize: 12, border: '1px solid var(--border)', borderRadius: 4, padding: '3px 6px', background: 'var(--bg-primary)', color: 'var(--text-primary)' },
}
