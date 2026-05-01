import type React from 'react'

export interface ChartConfig {
  chartType: 'bar' | 'stacked_bar' | 'line' | 'area' | 'scatter' | 'pie' | 'donut'
  xAxis: string
  yAxis: string[]
  title?: string
  showLegend?: boolean
  showGrid?: boolean
  showLabels?: boolean
  seriesColors?: Record<string, string>
}

interface ChartConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
}

const CHART_TYPES: { value: ChartConfig['chartType']; label: string; symbol: string }[] = [
  { value: 'bar',         label: 'Bar',     symbol: '▊▊' },
  { value: 'stacked_bar', label: 'Stack',   symbol: '▊≡' },
  { value: 'line',        label: 'Line',    symbol: '╱╲' },
  { value: 'area',        label: 'Area',    symbol: '◣◢' },
  { value: 'scatter',     label: 'Scatter', symbol: '⠿⠿' },
  { value: 'pie',         label: 'Pie',     symbol: '◕' },
  { value: 'donut',       label: 'Donut',   symbol: '◎' },
]

export const DEFAULT_COLORS = [
  '#6366f1', // indigo
  '#06b6d4', // cyan
  '#10b981', // emerald
  '#f59e0b', // amber
  '#f43f5e', // rose
  '#8b5cf6', // violet
  '#0ea5e9', // sky
  '#84cc16', // lime
]

const PRESET_SWATCHES = DEFAULT_COLORS

export function ChartConfigPanel({ config, columns, onChange }: ChartConfigPanelProps) {
  return (
    <div style={styles.panel}>
      {/* Chart type — visual tile picker */}
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <div style={styles.typeGrid}>
          {CHART_TYPES.map(ct => (
            <button
              key={ct.value}
              type="button"
              title={ct.label}
              onClick={() => onChange({ ...config, chartType: ct.value })}
              style={{
                ...styles.typeBtn,
                ...(config.chartType === ct.value ? styles.typeBtnActive : {}),
              }}
            >
              <span style={{ fontSize: 14, lineHeight: 1 }}>{ct.symbol}</span>
              <span style={{ fontSize: 11, marginTop: 2, color: 'var(--text-primary)' }}>{ct.label}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Axes */}
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>X axis</div>
          <select
            aria-label="X axis"
            style={styles.select}
            value={config.xAxis}
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

      {/* Per-series color pickers */}
      {config.yAxis?.length > 0 && (
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Series colors</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {config.yAxis.map((series, i) => {
              const color = config.seriesColors?.[series] ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length]
              const applyColor = (val: string) => onChange({
                ...config,
                seriesColors: { ...config.seriesColors, [series]: val },
              })
              return (
                <div key={series}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, cursor: 'pointer' }}>
                    <input
                      type="color"
                      key={color}
                      defaultValue={color}
                      onBlur={e => applyColor(e.target.value)}
                      style={{ width: 26, height: 20, padding: 1, border: '1px solid var(--border)', borderRadius: 3, cursor: 'pointer', background: 'none' }}
                    />
                    <span style={{ color: 'var(--text-secondary)' }}>{series}</span>
                  </label>
                  <div style={{ display: 'flex', gap: 4, marginTop: 4, marginLeft: 2 }}>
                    {PRESET_SWATCHES.map(swatch => (
                      <button
                        key={swatch}
                        type="button"
                        title={swatch}
                        onClick={() => applyColor(swatch)}
                        style={{
                          width: 14,
                          height: 14,
                          borderRadius: '50%',
                          background: swatch,
                          border: color === swatch ? '2px solid var(--text-primary)' : '2px solid transparent',
                          padding: 0,
                          cursor: 'pointer',
                          outline: 'none',
                          flexShrink: 0,
                        }}
                      />
                    ))}
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* Display toggles */}
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Display</div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          {([
            { key: 'showGrid',   label: 'Grid lines',  def: true },
            { key: 'showLegend', label: 'Legend',       def: true },
            { key: 'showLabels', label: 'Data labels',  def: false },
          ] as const).map(opt => (
            <label key={opt.key} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={(config[opt.key] as boolean | undefined) ?? opt.def}
                onChange={e => onChange({ ...config, [opt.key]: e.target.checked })}
                style={{ width: 14, height: 14, accentColor: 'var(--accent)', cursor: 'pointer' }}
              />
              <span style={{ color: 'var(--text-secondary)' }}>{opt.label}</span>
            </label>
          ))}
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: {
    borderTop: '1px solid var(--border)',
    padding: '12px 14px',
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
    background: 'var(--bg-secondary)',
  },
  row: { display: 'flex', gap: 12 },
  section: { display: 'flex', flexDirection: 'column', gap: 5, flex: 1 },
  sectionLabel: {
    fontSize: 10, fontWeight: 700, color: 'var(--text-muted)',
    textTransform: 'uppercase', letterSpacing: '0.07em',
  },
  typeGrid: { display: 'flex', flexWrap: 'wrap', gap: 4 },
  typeBtn: {
    display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
    width: 50, height: 40, border: '1px solid var(--border)', borderRadius: 4,
    background: 'var(--bg-card)', cursor: 'pointer', color: 'var(--text-muted)',
    padding: '4px 2px', fontFamily: 'var(--font-sans)',
  },
  typeBtnActive: {
    background: 'var(--accent-light)', borderColor: 'var(--accent)', color: 'var(--accent)',
  },
  select: {
    fontSize: 12, border: '1px solid var(--border)', borderRadius: 4,
    padding: '4px 6px', background: 'var(--bg-input)', color: 'var(--text-primary)',
    width: '100%',
  },
}
