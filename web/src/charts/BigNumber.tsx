import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { ChartTypeSelect, CHART_COLORS } from './common'
import { ConfigHint } from './ConfigHint'

function formatNumber(value: unknown, decimalPlaces?: number): string {
  if (typeof value === 'number') {
    return value.toLocaleString(undefined, {
      minimumFractionDigits: decimalPlaces ?? 0,
      maximumFractionDigits: decimalPlaces ?? 2,
    })
  }
  if (value == null) return '—'
  return String(value)
}

function getColor(skipEmpty?: boolean): string {
  return skipEmpty ? 'var(--text-muted)' : 'var(--text-primary)'
}

function BigNumberComponent({ data, config }: ChartProps) {
  const col = config.valueColumn || data.columns[0]?.name || ''
  const row = data.rows[0]
  const value = row ? row[data.columns.findIndex(c => c.name === col)] : null

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      height: '100%',
      minHeight: 200,
      padding: 24,
      gap: config.label ? 8 : 0,
    }}>
      {config.label && (
        <div style={{
          fontSize: 13,
          fontWeight: 600,
          color: 'var(--text-muted)',
          textTransform: 'uppercase',
          letterSpacing: 0.5,
          textAlign: 'center',
        }}>
          {config.label}
        </div>
      )}
      <div style={{
        fontSize: 56,
        fontWeight: 700,
        color: config.seriesColors?.value ?? getColor(config.skipEmpty),
        lineHeight: 1.1,
        letterSpacing: '-1.5px',
        textAlign: 'center',
        wordBreak: 'break-word',
      }}>
        {config.prefix && <span style={{ fontSize: 28, fontWeight: 400, color: 'var(--text-muted)', marginRight: 4 }}>{config.prefix}</span>}
        {formatNumber(value, config.decimalPlaces)}
        {config.suffix && <span style={{ fontSize: 28, fontWeight: 400, color: 'var(--text-muted)', marginLeft: 4 }}>{config.suffix}</span>}
      </div>
    </div>
  )
}

function BigNumberConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'big_number'} onChange={v => onChange({ ...config, chartType: v as any })} />
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Value column</div>
        <select
          aria-label="Value column"
          style={styles.select}
          value={config.valueColumn ?? config.yAxis?.[0] ?? ''}
          onChange={e => onChange({ ...config, valueColumn: e.target.value, yAxis: [e.target.value] })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Numeric column to display as the main value</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Label</div>
        <input
          aria-label="Label"
          style={styles.input}
          value={config.label ?? ''}
          placeholder="e.g. Total Revenue"
          onChange={e => onChange({ ...config, label: e.target.value })}
        />
        <ConfigHint>Text shown above the number (e.g. "Total Revenue")</ConfigHint>
      </div>
      <div style={{ ...styles.row, gap: 8 }}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Prefix</div>
          <input
            aria-label="Prefix"
            style={styles.input}
            value={config.prefix ?? ''}
            placeholder="$"
            onChange={e => onChange({ ...config, prefix: e.target.value })}
          />
          <ConfigHint>Text before the number (e.g. "$")</ConfigHint>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Suffix</div>
          <input
            aria-label="Suffix"
            style={styles.input}
            value={config.suffix ?? ''}
            placeholder="USD"
            onChange={e => onChange({ ...config, suffix: e.target.value })}
          />
          <ConfigHint>Text after the number (e.g. "USD", "%")</ConfigHint>
        </div>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Decimal places</div>
        <input
          aria-label="Decimal places"
          style={styles.input}
          type="number"
          min={0}
          max={10}
          value={config.decimalPlaces ?? 0}
          onChange={e => onChange({ ...config, decimalPlaces: parseInt(e.target.value) || 0 })}
        />
        <ConfigHint>Number of decimal digits (0 = whole numbers)</ConfigHint>
      </div>
      <label style={styles.checkbox}>
        <input
          type="checkbox"
          checked={!config.skipEmpty}
          onChange={e => onChange({ ...config, skipEmpty: !e.target.checked })}
        />
        Show empty state
      </label>
      <ConfigHint>Display a placeholder when no data is available</ConfigHint>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Color</div>
        <input
          type="color"
          value={config.seriesColors?.value ?? CHART_COLORS[0]}
          onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, value: e.target.value } })}
          style={styles.colorInput}
        />
        <ConfigHint>Text color for the number value</ConfigHint>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  row: { display: 'flex' },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  input: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
  colorInput: { width: 32, height: 32, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', background: 'none' },
}

export const BigNumberModule: ChartModule = {
  Component: BigNumberComponent,
  ConfigPanel: BigNumberConfigPanel,
  defaultConfig: { chartType: 'big_number', decimalPlaces: 0, skipEmpty: true, showLegend: false, showGrid: false, showLabels: false },
  detectColumns: (columns) => ({ valueColumn: columns[0]?.name, yAxis: [columns[0]?.name] }),
  requirements: { minColumns: 1 },
}
