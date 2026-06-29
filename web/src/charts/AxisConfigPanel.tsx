import { useMemo } from 'react'
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
  data?: { columns: { name: string; type?: string }[]; rows: unknown[][] }
  groupValues?: string[]
}

function useGroupValues(config: ChartConfig, columns: string[], data?: { columns: { name: string; type?: string }[]; rows: unknown[][] }): string[] {
  return useMemo(() => {
    if (!config.groupBy) return []
    const colIndex = data
      ? data.columns.findIndex(c => c.name === config.groupBy)
      : columns.indexOf(config.groupBy)
    if (colIndex < 0) return []
    const seen = new Set<string>()
    const groups: string[] = []
    if (data) {
      for (const row of data.rows) {
        const val = String(row[colIndex] ?? '')
        if (val && !seen.has(val)) {
          seen.add(val)
          groups.push(val)
        }
      }
    }
    return groups
  }, [config.groupBy, data, columns])
}

function detectSeriesNames(config: ChartConfig, columns: string[], data?: { columns: { name: string; type?: string }[]; rows: unknown[][] }): string[] {
  if (config.yAxis?.length) return config.yAxis
  const exclude = new Set([config.xAxis, config.groupBy].filter(Boolean))
  // Try type-based detection from data.columns
  if (data?.columns?.length) {
    const numericTypes = new Set([
      'int','int2','int4','int8','int16','int32','int64','int128','int256',
      'uint8','uint16','uint32','uint64','uint128','uint256',
      'float','float4','float8','float32','float64','double',
      'decimal','numeric','real','bigint','smallint','serial','bigserial','number',
    ])
    function isNum(t: string): boolean {
      const base = t.replace(/\(.*\)/, '').trim().toLowerCase()
      if (numericTypes.has(base)) return true
      const inner = t.replace(/^(nullable|lowcardinality)\(/i, '').replace(/\)$/, '').trim()
      return inner !== t && isNum(inner)
    }
    const detected = data.columns
      .filter(c => c.type && isNum(c.type))
      .map(c => c.name)
      .filter(n => !exclude.has(n))
    if (detected.length > 0) return detected
  }
  // Fallback: use columns string array (always available as prop)
  return columns.filter(n => !exclude.has(n))
}

export function AxisConfigPanel({ 
  config, columns, onChange, 
  showStack, showPieOptions, data, groupValues: groupValuesProp
}: AxisConfigPanelProps) {
  const localGroupValues = useGroupValues(config, columns, data)
  const groupValues = (groupValuesProp?.length ? groupValuesProp : localGroupValues) ?? []
  const seriesNames = detectSeriesNames(config, columns, data)
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
              value={config.chartType === 'stacked_bar' || config.chartType === 'stacked_area' ? 'yes' : 'no'}
              onChange={e => {
                const baseType = config.chartType === 'stacked_bar' ? 'bar' : config.chartType === 'stacked_area' ? 'area' : config.chartType
                const stackedType = baseType === 'bar' ? 'stacked_bar' : baseType === 'area' ? 'stacked_area' : baseType
                onChange({ ...config, chartType: e.target.value === 'yes' ? stackedType : baseType })
              }}
            >
              <option value="no">No</option>
              <option value="yes">Yes</option>
            </select>
            <ConfigHint>Stack multiple series on top of each other</ConfigHint>
          </div>
        </div>
      )}
      {(config.chartType === 'bar' || config.chartType === 'stacked_bar') ? (
        <div style={styles.row}>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Bar width</div>
            <select
              aria-label="Bar width"
              style={styles.select}
              value={config.barWidth ?? ''}
              onChange={e => onChange({ ...config, barWidth: e.target.value || undefined })}
            >
              <option value="">Auto</option>
              <option value="30%">Narrow</option>
              <option value="50%">Normal</option>
              <option value="70%">Wide</option>
              <option value="90%">Max</option>
            </select>
          </div>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Category gap</div>
            <select
              aria-label="Category gap"
              style={styles.select}
              value={config.barCategoryGap ?? ''}
              onChange={e => onChange({ ...config, barCategoryGap: e.target.value || undefined })}
            >
              <option value="">Default</option>
              <option value="10%">Tight</option>
              <option value="30%">Normal</option>
              <option value="60%">Wide</option>
              <option value="100%">Extra</option>
            </select>
          </div>
        </div>
      ) : undefined}

      {/* Group By */}
      {!showPieOptions && (
        <div style={styles.row}>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Group by</div>
            <select
              aria-label="Group by"
              style={styles.select}
              value={config.groupBy ?? ''}
              onChange={e => onChange({ ...config, groupBy: e.target.value || undefined })}
            >
              <option value="">— None —</option>
              {columns.filter(c => c !== config.xAxis && !(config.yAxis ?? []).includes(c)).map(c => (
                <option key={c} value={c}>{c}</option>
              ))}
            </select>
            <ConfigHint>Split into separate series per value in this column</ConfigHint>
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

      {/* Series Colors — without group_by, show per-y_column */}
      {seriesNames.length > 0 && !config.groupBy && (
        <div style={styles.row}>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Series colors</div>
            <div style={styles.colorRow}>
              {seriesNames.map((seriesName, i) => {
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
      {/* Series Colors — with group_by, show per-group-value */}
      {config.groupBy && groupValues.length > 0 && (
        <div style={styles.row}>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Series colors</div>
            <div style={styles.colorRow}>
              {groupValues.map((group: string, i: number) => {
                const defaultColor = CHART_COLORS[i % CHART_COLORS.length]
                const currentColor = config.seriesColors?.[group] ?? defaultColor
                return (
                  <label key={group} style={styles.colorLabel}>
                    <input
                      type="color"
                      value={currentColor}
                      onChange={e => {
                        const newColors = { ...config.seriesColors, [group]: e.target.value }
                        onChange({ ...config, seriesColors: newColors })
                      }}
                      style={styles.colorInput}
                    />
                    <span style={styles.colorText}>{group.substring(0, 8)}</span>
                  </label>
                )
              })}
            </div>
            <ConfigHint>Colors by group value from the &quot;{config.groupBy}&quot; column</ConfigHint>
          </div>
        </div>
      )}

      {config.chartType === 'scatter' && (
        <div style={styles.row}>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Color by (optional)</div>
            <select
              aria-label="Color by"
              style={styles.select}
              value={config.colorColumn ?? ''}
              onChange={e => onChange({ ...config, colorColumn: e.target.value || undefined })}
            >
              <option value="">None</option>
              {columns.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
            <ConfigHint>Column to map to color gradient</ConfigHint>
          </div>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Size by (optional)</div>
            <select
              aria-label="Size by"
              style={styles.select}
              value={config.sizeColumn ?? ''}
              onChange={e => onChange({ ...config, sizeColumn: e.target.value || undefined })}
            >
              <option value="">None</option>
              {columns.map(c => <option key={c} value={c}>{c}</option>)}
            </select>
            <ConfigHint>Column to control bubble size</ConfigHint>
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
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.dataZoom ?? false}
            onChange={e => onChange({ ...config, dataZoom: e.target.checked })}
          />
          Zoom
        </label>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.smooth ?? false}
            onChange={e => onChange({ ...config, smooth: e.target.checked })}
          />
          Smooth
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
