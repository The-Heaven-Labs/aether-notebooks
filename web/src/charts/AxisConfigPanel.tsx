import type React from 'react'
import type { ChartConfig, ChartType } from './types'
import { ALL_CHART_TYPES, CHART_COLORS } from './index'

interface AxisConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
  showStack?: boolean
  showPieOptions?: boolean
  showTimelineOptions?: boolean
  showTreeOptions?: boolean
}

export function AxisConfigPanel({ 
  config, columns, onChange, 
  showStack, showPieOptions, showTimelineOptions, showTreeOptions 
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
        </div>
      </div>

      {/* Timeline-specific options */}
      {showTimelineOptions && (
        <>
          <div style={styles.row}>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Time column</div>
              <select
                aria-label="Time column"
                style={styles.select}
                value={config.timeColumn ?? ''}
                onChange={e => onChange({ ...config, timeColumn: e.target.value })}
              >
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>End time column <span style={{ fontWeight: 400, textTransform: 'none' }}>(optional)</span></div>
              <select
                aria-label="End time column"
                style={styles.select}
                value={config.endTimeColumn ?? ''}
                onChange={e => onChange({ ...config, endTimeColumn: e.target.value || undefined })}
              >
                <option value="">None (point events)</option>
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
          </div>
          <div style={styles.row}>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Label column</div>
              <select
                aria-label="Label column"
                style={styles.select}
                value={config.labelColumn ?? ''}
                onChange={e => onChange({ ...config, labelColumn: e.target.value || undefined })}
              >
                <option value="">None</option>
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Group by</div>
              <select
                aria-label="Group by"
                style={styles.select}
                value={config.groupBy ?? ''}
                onChange={e => onChange({ ...config, groupBy: e.target.value || undefined })}
              >
                <option value="">No grouping</option>
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
          </div>
        </>
      )}

      {/* Tree-specific options */}
      {showTreeOptions && (
        <>
          <div style={styles.row}>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>ID column</div>
              <select
                aria-label="ID column"
                style={styles.select}
                value={config.idColumn ?? ''}
                onChange={e => onChange({ ...config, idColumn: e.target.value })}
              >
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Parent ID column</div>
              <select
                aria-label="Parent ID column"
                style={styles.select}
                value={config.parentIdColumn ?? ''}
                onChange={e => onChange({ ...config, parentIdColumn: e.target.value })}
              >
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
          </div>
          <div style={styles.row}>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Label column</div>
              <select
                aria-label="Label column"
                style={styles.select}
                value={config.labelColumn ?? ''}
                onChange={e => onChange({ ...config, labelColumn: e.target.value || undefined })}
              >
                <option value="">Use ID</option>
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Layout</div>
              <select
                aria-label="Layout direction"
                style={styles.select}
                value={config.layout ?? 'top-down'}
                onChange={e => onChange({ ...config, layout: e.target.value as 'top-down' | 'left-to-right' })}
              >
                <option value="top-down">Top-down</option>
                <option value="left-to-right">Left-to-right</option>
              </select>
            </div>
          </div>
          <div style={styles.row}>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>Metrics <span style={{ fontWeight: 400, textTransform: 'none' }}>(Ctrl+click multi)</span></div>
              <select
                aria-label="Metrics"
                style={{ ...styles.select, minHeight: 56 }}
                multiple
                value={config.metricColumns ?? []}
                onChange={e => {
                  const selected = Array.from(e.target.selectedOptions).map(o => o.value)
                  onChange({ ...config, metricColumns: selected })
                }}
              >
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
          </div>
        </>
      )}

      {/* Axis-based options (bar, line, area, scatter, pie) */}
      {!showTimelineOptions && !showTreeOptions && (
        <>
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
            </div>
            <div style={styles.section}>
              <div style={styles.sectionLabel}>{showPieOptions ? 'Value column' : 'Y axis'} <span style={{ fontWeight: 400, textTransform: 'none' }}>(Ctrl+click multi)</span></div>
              <select
                aria-label={showPieOptions ? 'Value column' : 'Y axis'}
                style={{ ...styles.select, minHeight: 56 }}
                multiple={!!showPieOptions}
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
              </div>
            </div>
          )}
        </>
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
        </div>
      </div>

      {/* Series Colors */}
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Series colors</div>
          <div style={styles.colorRow}>
            {CHART_COLORS.slice(0, 6).map((defaultColor, i) => {
              const seriesName = config.yAxis?.[i] ?? `Series ${i + 1}`
              const currentColor = config.seriesColors?.[seriesName] ?? defaultColor
              return (
                <label key={i} style={styles.colorLabel}>
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
        </div>
      </div>

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
  colorInput: { width: 24, height: 24, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer' },
  colorText: { fontSize: 9, maxWidth: 40, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
}
