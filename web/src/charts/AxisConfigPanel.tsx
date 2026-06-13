import type React from 'react'
import type { ChartConfig } from './types'

interface AxisConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
  showStack?: boolean
}

export function AxisConfigPanel({ config, columns, onChange, showStack }: AxisConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>X axis</div>
          <select
            aria-label="X axis"
            style={styles.select}
            value={config.xAxis ?? ''}
            onChange={e => onChange({ ...config, xAxis: e.target.value })}
          >
            {columns.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Y axis <span style={{ fontWeight: 400, textTransform: 'none' }}>(Ctrl+click multi)</span></div>
          <select
            aria-label="Y axis"
            style={{ ...styles.select, minHeight: 56 }}
            multiple
            value={config.yAxis ?? []}
            onChange={e => {
              const selected = Array.from(e.target.selectedOptions).map(o => o.value)
              onChange({ ...config, yAxis: selected })
            }}
          >
            {columns.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
        </div>
      </div>
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Title</div>
          <input
            aria-label="Chart title"
            style={styles.input}
            value={config.title ?? ''}
            placeholder="Optional title"
            onChange={e => onChange({ ...config, title: e.target.value })}
          />
        </div>
        {showStack && (
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Stack</div>
            <select
              aria-label="Stack"
              style={styles.select}
              value={config.chartType === 'stacked_bar' ? 'yes' : 'no'}
              onChange={e => onChange({ ...config, chartType: e.target.value === 'yes' ? 'stacked_bar' : 'bar' })}
            >
              <option value="no">No</option>
              <option value="yes">Yes</option>
            </select>
          </div>
        )}
      </div>
      <div style={styles.row}>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showLegend ?? true}
            onChange={e => onChange({ ...config, showLegend: e.target.checked })}
          />
          Legend
        </label>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showGrid ?? true}
            onChange={e => onChange({ ...config, showGrid: e.target.checked })}
          />
          Grid
        </label>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showLabels ?? false}
            onChange={e => onChange({ ...config, showLabels: e.target.checked })}
          />
          Labels
        </label>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  row: { display: 'flex', gap: 10 },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  input: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
}
