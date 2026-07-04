import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, getTooltipStyle, getContrastTextColor, useChartColors, useRowsAsObjects, ChartTypeSelect, CHART_COLORS } from './common'
import { ConfigHint } from './ConfigHint'

function FunnelChartComponent({ data, config }: ChartProps) {
  const chartData = useRowsAsObjects(data)
  const colors = useChartColors()

  const categoryCol = config.categoryColumn || config.xAxis || data.columns[0]?.name || ''
  const valueCol = config.valueColumn || config.yAxis?.[0] || data.columns[1]?.name || ''

  const option = useMemo(() => {
    const funnelData = chartData
      .map((d, i) => {
        const bgColor = config.seriesColors?.[String(d[categoryCol] ?? '')] ?? CHART_COLORS[i % CHART_COLORS.length]
        return {
          name: String(d[categoryCol] ?? ''),
          value: Number(d[valueCol]) || 0,
          itemStyle: { color: bgColor },
          label: { color: getContrastTextColor(bgColor) },
        }
      })
      .filter(d => d.value > 0 || !config.skipEmpty)

    return {
      tooltip: {
        trigger: 'item' as const,
        formatter: '{b}: {c}' + (config.suffix ? ` ${config.suffix}` : ''),
        ...getTooltipStyle(),
      },
      title: config.title ? {
        text: config.title, left: 'center', top: 8,
        textStyle: { fontSize: 14, color: colors.text },
      } : undefined,
      series: [{
        type: 'funnel' as const,
        top: config.title ? 48 : 8,
        bottom: 8,
        left: '15%',
        right: '15%',
        minSize: '15%',
        maxSize: '100%',
        gap: 2,
        sort: config.funnelSort ?? 'descending',
        label: {
          show: config.showLabels !== false,
          position: 'inside' as const,
          fontSize: 11,
          formatter: '{b}: {c}' + (config.suffix ? ` ${config.suffix}` : ''),
        },
        labelLine: { show: false },
        itemStyle: {
          borderColor: colors.bgCard,
          borderWidth: 1,
        },
        data: funnelData,
      }],
    }
  }, [chartData, categoryCol, valueCol, config.title, config.skipEmpty, config.funnelSort, config.showLabels, config.suffix, config.seriesColors, colors])

  return <EChartsContainer option={option} />
}

function FunnelConfigPanel({ config, columns, onChange, data }: ConfigPanelProps) {
  const stages = useMemo(() => {
    if (!data?.rows?.length || !data.columns?.length) return []
    const colIdx = data.columns.findIndex(c => c.name === (config.categoryColumn || config.xAxis))
    if (colIdx < 0) return []
    return [...new Set(data.rows.map(r => String(r[colIdx] ?? '')))].filter(Boolean)
  }, [data, config.categoryColumn, config.xAxis])
  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'funnel'} onChange={v => onChange({ ...config, chartType: v as any })} />
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Category column</div>
        <select
          aria-label="Category column"
          style={styles.select}
          value={config.categoryColumn ?? config.xAxis ?? ''}
          onChange={e => onChange({ ...config, categoryColumn: e.target.value, xAxis: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Labels for each funnel stage</ConfigHint>
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
        <ConfigHint>Numeric column with stage values</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Sort</div>
        <select
          aria-label="Funnel sort order"
          style={styles.select}
          value={config.funnelSort ?? 'descending'}
          onChange={e => onChange({ ...config, funnelSort: e.target.value as any })}
        >
          <option value="descending">Largest first</option>
          <option value="ascending">Smallest first</option>
          <option value="none">Data order</option>
        </select>
        <ConfigHint>Order of funnel stages by size</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Suffix</div>
        <input
          aria-label="Suffix"
          style={styles.input}
          value={config.suffix ?? ''}
          placeholder="e.g. users, %"
          onChange={e => onChange({ ...config, suffix: e.target.value })}
        />
        <ConfigHint>Unit shown in tooltip (e.g. "users", "%")</ConfigHint>
      </div>
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
      <label style={styles.checkbox}>
        <input
          type="checkbox"
          checked={config.showLabels !== false}
          onChange={e => onChange({ ...config, showLabels: e.target.checked })}
        />
        Show labels
      </label>
      {stages.length > 0 && (
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Stage colors</div>
          <div style={s.colorRow}>
            {stages.map((stage, i) => {
              const defaultColor = CHART_COLORS[i % CHART_COLORS.length]
              const currentColor = config.seriesColors?.[stage] ?? defaultColor
              return (
                <label key={stage} style={s.colorLabel}>
                  <input
                    type="color"
                    value={currentColor}
                    onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, [stage]: e.target.value } })}
                    style={s.colorInput}
                  />
                  <span style={s.colorText}>{stage.substring(0, 8)}</span>
                </label>
              )
            })}
          </div>
          <ConfigHint>Color for each funnel stage</ConfigHint>
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  input: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
}

const s: Record<string, React.CSSProperties> = {
  colorRow: { display: 'flex', gap: 6, flexWrap: 'wrap' },
  colorLabel: { display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2, fontSize: 10, color: 'var(--text-muted)' },
  colorInput: { width: 24, height: 24, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', background: 'transparent' },
  colorText: { fontSize: 9, maxWidth: 40, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' },
}

export const FunnelChartModule: ChartModule = {
  Component: FunnelChartComponent,
  ConfigPanel: FunnelConfigPanel,
  defaultConfig: { chartType: 'funnel', showLabels: true, funnelSort: 'descending', skipEmpty: true },
  detectColumns: (columns) => ({
    xAxis: columns[0]?.name,
    categoryColumn: columns[0]?.name,
    yAxis: [columns[1]?.name ?? columns[0]?.name],
    valueColumn: columns[1]?.name ?? columns[0]?.name,
  }),
  requirements: { minColumns: 2 },
}
