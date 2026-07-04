import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, useChartColors, useRowsAsObjects, isNumericType, ChartTypeSelect } from './common'
import { ConfigHint } from './ConfigHint'

function computeHistogram(values: number[], binCount: number): { bins: string[]; counts: number[] } {
  if (values.length === 0) return { bins: [], counts: [] }

  const min = Math.min(...values)
  const max = Math.max(...values)

  if (min === max) {
    return { bins: [`${min}`], counts: [values.length] }
  }

  const binWidth = (max - min) / binCount
  const counts = new Array(binCount).fill(0)

  for (const v of values) {
    const idx = Math.min(Math.floor((v - min) / binWidth), binCount - 1)
    counts[idx]++
  }

  const bins: string[] = []
  for (let i = 0; i < binCount; i++) {
    const lo = min + i * binWidth
    const hi = min + (i + 1) * binWidth
    const loStr = Number.isInteger(lo) ? lo.toString() : lo.toFixed(1)
    const hiStr = Number.isInteger(hi) ? hi.toString() : hi.toFixed(1)
    bins.push(`${loStr}–${hiStr}`)
  }

  return { bins, counts }
}

function HistogramChartComponent({ data, config }: ChartProps) {
  const chartData = useRowsAsObjects(data)
  const colors = useChartColors()

  const valueCol = config.valueColumn || config.yAxis?.[0] || data.columns[0]?.name || ''
  const binCount = config.binCount ?? Math.min(20, Math.max(5, Math.ceil(Math.sqrt(chartData.length))))

  const option = useMemo(() => {
    const values = chartData
      .map(d => Number(d[valueCol]))
      .filter(v => !isNaN(v))

    const { bins, counts } = computeHistogram(values, binCount)

    return {
      tooltip: {
        trigger: 'axis' as const,
        axisPointer: { type: 'shadow' as const },
        formatter: (params: any) => {
          const p = Array.isArray(params) ? params[0] : params
          return `${p.name}<br/>Frequency: <b>${p.value}</b>`
        },
        ...getTooltipStyle(),
      },
      title: config.title ? {
        text: config.title, left: 'center', top: 8,
        textStyle: { fontSize: 14, color: colors.text },
      } : undefined,
      legend: { show: false },
      grid: {
        top: config.title ? 56 : 8,
        right: 16,
        bottom: bins.length > 8 ? 48 : 8,
        left: 16,
        containLabel: true,
      },
      xAxis: {
        type: 'category' as const,
        data: bins,
        ...getAxisStyle(config.showGrid),
        axisLabel: {
          fontSize: 10,
          color: colors.textMuted,
          rotate: bins.length > 8 ? 45 : 0,
        },
      },
      yAxis: {
        type: 'value' as const,
        name: 'Frequency',
        nameTextStyle: { fontSize: 11, color: colors.textMuted },
        ...getAxisStyle(config.showGrid),
      },
      series: [{
        type: 'bar' as const,
        data: counts,
        barMaxWidth: 40,
        itemStyle: {
          color: config.seriesColors?.[valueCol] ?? CHART_COLORS[0],
          borderRadius: [3, 3, 0, 0] as [number, number, number, number],
        },
        label: config.showLabels ? {
          show: true,
          position: 'top' as const,
          fontSize: 10,
          color: colors.textMuted,
        } : undefined,
      }],
    }
  }, [chartData, valueCol, binCount, config.title, config.showLabels, config.showGrid, config.seriesColors, colors])

  return <EChartsContainer option={option} />
}

function HistogramConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'histogram'} onChange={v => onChange({ ...config, chartType: v as any })} />
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
        <ConfigHint>Numeric column to compute frequency distribution for</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Number of bins</div>
        <input
          aria-label="Bin count"
          style={styles.input}
          type="number"
          min={2}
          max={100}
          value={config.binCount ?? ''}
          placeholder="Auto"
          onChange={e => {
            const v = parseInt(e.target.value)
            onChange({ ...config, binCount: isNaN(v) ? undefined : v })
          }}
        />
        <ConfigHint>Number of histogram bins (auto if empty)</ConfigHint>
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
          checked={config.showLabels ?? false}
          onChange={e => onChange({ ...config, showLabels: e.target.checked })}
        />
        Show bar values
      </label>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Bar color</div>
        <input
          type="color"
          value={config.seriesColors?.[config.valueColumn || 'bar'] ?? CHART_COLORS[0]}
          onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, [config.valueColumn || 'bar']: e.target.value } })}
          style={s.colorInput}
        />
      </div>
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
  colorInput: { width: 32, height: 32, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', background: 'none' },
}

export const HistogramChartModule: ChartModule = {
  Component: HistogramChartComponent,
  ConfigPanel: HistogramConfigPanel,
  defaultConfig: { chartType: 'histogram', showLabels: false, showLegend: false, showGrid: true, skipEmpty: true },
  detectColumns: (columns) => {
    const numCols = columns.filter(c => isNumericType(c.type))
    return {
      valueColumn: numCols[0]?.name ?? columns[0]?.name,
      yAxis: [numCols[0]?.name ?? columns[0]?.name],
    }
  },
  requirements: { minColumns: 1 },
}
