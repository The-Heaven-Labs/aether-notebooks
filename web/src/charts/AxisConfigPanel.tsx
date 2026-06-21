import type React from 'react'
import type { ChartConfig, ChartType } from './types'
import { ALL_CHART_TYPES, CHART_COLORS } from './index'
import { ConfigHint } from './ConfigHint'

interface AxisConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
  showStack?: boolean
  showPieOptions?: boolean
}

export function AxisConfigPanel({ 
  config, columns, onChange, 
  showStack, showPieOptions 
}: AxisConfigPanelProps) {
  return (
    <div style={styles.panel}>
      {/* Chart Type Selector */}
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Chart type</div>
          <select
            aria-label="Chart type"
            style={styles.select}
            value={config.chartType ?? 'bar'}
            onChange={e => onChange({ ...config, chartType: e.target.value as ChartType })}
          >
            {ALL_CHART_TYPES.map(t => (
              <option key={t.value} value={t.value}>{t.symbol} {t.label}</option>
            ))}
          </select>
          <ConfigHint>Visualization style for your data</ConfigHint>
        </div>
      </div>

      {/* Axis-based options */}
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>{showPieOptions ? 'Name column' : 'X axis'}</div>
          <select
            aria-label={showPieOptions ? 'Name column' : 'X axis'}
            style={styles.select}
            value={config.xAxis ?? ''}
            onChange={e => onChange({ ...config, xAxis: e.target.value })}
          >
            {columns.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
          <ConfigHint>{showPieOptions ? 'Column for slice labels' : 'Column for horizontal axis (categories, time, or numeric values)'}</ConfigHint>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>{showPieOptions ? 'Value column' : 'Y axis'} <span style={{ fontWeight: 400, textTransform: 'none' }}>(Ctrl+click multi)</span></div>
          <select
            aria-label={showPieOptions ? 'Value column' : 'Y axis'}
            style={{ ...styles.select, minHeight: 56 }}
            multiple={!showPieOptions}
            value={showPieOptions ? (config.yAxis?.[0] ?? '') : (config.yAxis ?? [])}
            onChange={e => {
              if (showPieOptions) {
                onChange({ ...config, yAxis: [e.target.value] })
              } else {
                const selected = Array.from(e.target.selectedOptions).map(o => o.value)
                onChange({ ...config, yAxis: selected })
              }
            }}
          >
            {columns.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
          <ConfigHint>{showPieOptions ? 'Column for slice sizes' : 'Column(s) for vertical values (Ctrl+click for multiple series)'}</ConfigHint>
        </div>
      </div>
      {showStack && (
        <div style={styles.row}>
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
            <ConfigHint>Stack multiple series on top of each other</ConfigHint>
          </div>
        </div>
      )}

      {/* Title */}
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
          <ConfigHint>Optional text displayed at the top of the chart</ConfigHint>
        </div>
      </div>

      {/* Series Colors */}
      {(config.yAxis?.length ?? 0) > 0 && (
        <div style={styles.row}>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Series colors</div>
            <div style={styles.colorRow}>
              {config.yAxis!.map((seriesName, i) => {
                const defaultColor = CHART_COLORS[i % CHART_COLORS.length]
                const currentColor = config.seriesColors?.[seriesName] ?? defaultColor
                return (
                  <label key={seriesName} style={styles.colorLabel}>
                    <input
                      type="color"
                      value={currentColor}
                      onChange={e => {
                        const newColors = { ...config.seriesColors, [seriesName]: e.target.value }
                        onChange({ ...config, seriesColors: newColors })
                      }}
                      style={styles.colorInput}
                    />
                    <span style={styles.colorText}>{seriesName.substring(0, 8)}</span>
                  </label>
                )
              })}
            </div>
            <ConfigHint>Customize the color for each data series</ConfigHint>
          </div>
        </div>
      )}

      {/* Checkboxes */}
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
      <ConfigHint>Legend identifies each series, Grid shows background lines, Labels show values on data points</ConfigHint>
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
  colorRow: { display: 'flex', gap: 8, flexWrap: 'wrap' },
  colorLabel: { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2, fontSize: 10, color: 'var(--text-muted)' },
  colorInput: { width: 24, height: 24, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', background: 'transparent' },
  colorText: { fontSize: 9, maxWidth: 40, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
}
