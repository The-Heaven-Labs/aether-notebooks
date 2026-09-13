import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps, ChartConfig } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getContrastTextColor, useChartColors, useRowsAsObjects, useAxisColumns, detectAxisColumns, ChartTypeSelect, buildLegend } from './common'
import { ConfigHint } from './ConfigHint'

// Label plumbing shared by the option builder below (and unit-tested):
// when labels are on, every slice gets an explicit visible label — ECharts 6
// hides overlapping pie labels by default (labelLayout.hideOverlap), so we
// disable the hiding and keep repositioning (avoidLabelOverlap) instead.
// Inside labels sit on colored slices, so their color contrasts per-slice.
export function buildPieSeriesLabelConfig(
  config: Pick<ChartConfig, 'showLabels' | 'labelPosition' | 'minShowLabelAngle'>,
  outsideColor: string,
): {
  label: Record<string, unknown>
  labelLine: Record<string, unknown>
  labelLayout: { hideOverlap: boolean }
  avoidLabelOverlap: boolean
  minShowLabelAngle: number
} {
  if (config.showLabels === false) {
    return {
      label: { show: false },
      labelLine: { show: false },
      labelLayout: { hideOverlap: false },
      avoidLabelOverlap: true,
      minShowLabelAngle: 0,
    }
  }
  const position = config.labelPosition ?? 'outside'
  return {
    label: {
      show: true,
      position,
      fontSize: 11,
      color: position === 'inside'
        ? (params: { color?: unknown }) => getContrastTextColor(String(params?.color ?? '#888888'))
        : outsideColor,
    },
    labelLine: position === 'outside'
      ? { show: true, length: 12, length2: 12 }
      : { show: false },
    labelLayout: { hideOverlap: false },
    avoidLabelOverlap: true,
    minShowLabelAngle: config.minShowLabelAngle ?? 0,
  }
}

function PieChartComponent({ data, config }: ChartProps) {
  const { xAxis, yAxes } = useAxisColumns(data, config)
  const chartData = useRowsAsObjects(data)
  const colors = useChartColors()
  const valueKey = yAxes[0] ?? data.columns[1]?.name ?? ''
  const nameKey = config.labelColumn || xAxis
  const isDonut = config.chartType === 'donut'
  const { showLabels, labelPosition, minShowLabelAngle } = config

  const option = useMemo(() => ({
    tooltip: { trigger: 'item' as const, ...getTooltipStyle(), formatter: '{b}: {c} ({d}%)' },
    title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
    legend: buildLegend({ title: config.title, showLegend: config.showLegend }, colors),
    series: [{
      type: 'pie' as const,
      radius: isDonut ? ['40%', '70%'] as [string, string] : ['0%', '70%'] as [string, string],
      center: config.showLegend !== false ? ['40%', config.title ? '58%' : '50%'] as [string, string] : ['50%', '50%'] as [string, string],
      data: chartData.map((d, i) => ({
        name: d[nameKey],
        value: d[valueKey],
        itemStyle: { color: config.seriesColors?.[String(d[nameKey])] ?? CHART_COLORS[i % CHART_COLORS.length] },
      })),
      ...buildPieSeriesLabelConfig({ showLabels, labelPosition, minShowLabelAngle }, colors.text),
      emphasis: { itemStyle: { shadowBlur: 10, shadowColor: 'rgba(0,0,0,0.2)' } },
      roseType: config.roseType || false,
      startAngle: config.startAngle ?? 90,
      padAngle: config.padAngle ?? 0,
    }],
  }), [chartData, nameKey, valueKey, isDonut, config.title, config.seriesColors, config.showLegend, showLabels, labelPosition, minShowLabelAngle, config.roseType, config.startAngle, config.padAngle, colors])

  return <EChartsContainer option={option} showReset />
}

function PieConfigPanel({ config, columns, onChange, data }: ConfigPanelProps) {
  const sliceNames = useMemo(() => {
    if (!data || !config.xAxis || !data.rows || !data.columns) return []
    const colIndex = data.columns.findIndex(c => c.name === config.xAxis)
    if (colIndex < 0) return []
    const seen = new Set<string>()
    const names: string[] = []
    for (const row of data.rows) {
      const val = String(row[colIndex] ?? '')
      if (!seen.has(val)) {
        seen.add(val)
        names.push(val)
      }
    }
    return names
  }, [data, config.xAxis])

  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'pie'} onChange={v => onChange({ ...config, chartType: v as any })} />
      </div>
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

      {/* Title */}
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Title</div>
        <input
          aria-label="Title"
          style={styles.input}
          value={config.title ?? ''}
          placeholder="Chart title"
          onChange={e => onChange({ ...config, title: e.target.value })}
        />
      </div>

      {/* Labels + Legend — always-visible labels when enabled */}
      <div style={styles.row}>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showLabels !== false}
            onChange={e => onChange({ ...config, showLabels: e.target.checked })}
          />
          Show labels
        </label>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showLegend !== false}
            onChange={e => onChange({ ...config, showLegend: e.target.checked })}
          />
          Legend
        </label>
      </div>
      {config.showLabels !== false && (
        <div style={styles.row}>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Label position</div>
            <select
              aria-label="Label position"
              style={styles.select}
              value={config.labelPosition ?? 'outside'}
              onChange={e => onChange({ ...config, labelPosition: e.target.value as 'outside' | 'inside' })}
            >
              <option value="outside">Outside (with guide lines)</option>
              <option value="inside">Inside slices</option>
            </select>
          </div>
          <div style={styles.section}>
            <div style={styles.sectionLabel}>Min label angle</div>
            <input
              aria-label="Min label angle"
              type="number"
              min={0}
              max={90}
              style={styles.input}
              value={config.minShowLabelAngle ?? 0}
              onChange={e => onChange({ ...config, minShowLabelAngle: parseInt(e.target.value) || 0 })}
            />
            <ConfigHint>Slices smaller than this angle (deg) hide their label; 0 shows all</ConfigHint>
          </div>
        </div>
      )}

      {/* Series Colors — one color picker per slice */}
      {sliceNames.length > 0 && (
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Series colors</div>
          <div style={styles.colorRow}>
            {sliceNames.map((name, i) => {
              const defaultColor = CHART_COLORS[i % CHART_COLORS.length]
              const currentColor = config.seriesColors?.[name] ?? defaultColor
              return (
                <label key={name} style={styles.colorLabel}>
                  <input
                    type="color"
                    value={currentColor}
                    onChange={e => {
                      const newColors = { ...config.seriesColors, [name]: e.target.value }
                      onChange({ ...config, seriesColors: newColors })
                    }}
                    style={styles.colorInput}
                  />
                  <span style={styles.colorText}>{name.substring(0, 8)}</span>
                </label>
              )
            })}
          </div>
          <ConfigHint>Customize the color for each slice</ConfigHint>
        </div>
      )}

      <div style={styles.section}>
        <div style={styles.sectionLabel}>Rose type</div>
        <select
          aria-label="Rose type"
          style={styles.select}
          value={config.roseType ?? ''}
          onChange={e => onChange({ ...config, roseType: (e.target.value || undefined) as 'radius' | 'area' | undefined })}
        >
          <option value="">None (plain pie)</option>
          <option value="radius">Radius (rose)</option>
          <option value="area">Area (rose)</option>
        </select>
      </div>
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Start angle</div>
          <input
            aria-label="Start angle"
            type="number"
            min={0}
            max={360}
            style={styles.input}
            value={config.startAngle ?? 90}
            onChange={e => onChange({ ...config, startAngle: parseInt(e.target.value) || 90 })}
          />
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Pad angle</div>
          <input
            aria-label="Pad angle"
            type="number"
            min={0}
            max={30}
            style={styles.input}
            value={config.padAngle ?? 0}
            onChange={e => onChange({ ...config, padAngle: parseInt(e.target.value) || 0 })}
          />
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  section: { display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
  row: { display: 'flex', gap: 10 },
  input: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  colorRow: { display: 'flex', gap: 8, flexWrap: 'wrap' },
  colorLabel: { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2, fontSize: 10, color: 'var(--text-muted)' },
  colorInput: { width: 24, height: 24, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', background: 'transparent' },
  colorText: { fontSize: 9, maxWidth: 40, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
}

export const PieChartModule: ChartModule = {
  Component: PieChartComponent,
  ConfigPanel: PieConfigPanel,
  defaultConfig: { chartType: 'pie', showLegend: true, showLabels: true, labelPosition: 'outside', minShowLabelAngle: 0, skipEmpty: true },
  detectColumns: (columns) => detectAxisColumns(columns),
  requirements: { minColumns: 2 },
}
