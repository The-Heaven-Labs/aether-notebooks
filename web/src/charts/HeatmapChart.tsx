import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, getTooltipStyle, getContrastTextColor, useChartColors, useRowsAsObjects, isNumericType, ChartTypeSelect } from './common'
import { ConfigHint } from './ConfigHint'

function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '')
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}

function rgbToHex(r: number, g: number, b: number): string {
  return '#' + [r, g, b].map(c => Math.round(c).toString(16).padStart(2, '0')).join('')
}

function interpolateColor(low: string, high: string, steps: number): string[] {
  const [r1, g1, b1] = hexToRgb(low)
  const [r2, g2, b2] = hexToRgb(high)
  const colors: string[] = []
  for (let i = 0; i < steps; i++) {
    const t = i / (steps - 1)
    colors.push(rgbToHex(r1 + (r2 - r1) * t, g1 + (g2 - g1) * t, b1 + (b2 - b1) * t))
  }
  return colors
}

function HeatmapChartComponent({ data, config }: ChartProps) {
  const chartData = useRowsAsObjects(data)
  const colors = useChartColors()

  const xCol = config.xAxis || data.columns[0]?.name || ''
  const yCol = config.yAxisColumn || data.columns[1]?.name || xCol
  const valueCol = config.valueColumn || config.yAxis?.[0] || data.columns[2]?.name || data.columns[1]?.name || ''

  const option = useMemo(() => {
    const xValues = [...new Set(chartData.map(d => String(d[xCol] ?? '')))].slice(0, 50)
    const yValues = [...new Set(chartData.map(d => String(d[yCol] ?? '')))].slice(0, 50)

    const valueMap = new Map<string, number>()
    let minVal = Infinity
    let maxVal = -Infinity
    for (const row of chartData) {
      const key = `${row[xCol]}|${row[yCol]}`
      const val = Number(row[valueCol])
      if (!isNaN(val)) {
        valueMap.set(key, val)
        if (val < minVal) minVal = val
        if (val > maxVal) maxVal = val
      }
    }

    const lowColor = config.seriesColors?._low ?? '#f0f9ff'
    const highColor = config.seriesColors?._high ?? '#0369a1'

    const heatData: ({ value: [number, number, number]; label: { color: string } } | undefined)[] = []
    for (const row of chartData) {
      const xIdx = xValues.indexOf(String(row[xCol] ?? ''))
      const yIdx = yValues.indexOf(String(row[yCol] ?? ''))
      const val = valueMap.get(`${row[xCol]}|${row[yCol]}`)
      if (xIdx >= 0 && yIdx >= 0 && val != null) {
        const t = maxVal !== minVal ? (val - minVal) / (maxVal - minVal) : 0.5
        const cellColors = interpolateColor(lowColor, highColor, 100)
        const cellColor = cellColors[Math.round(t * 99)]
        heatData.push({ value: [xIdx, yIdx, val], label: { color: getContrastTextColor(cellColor) } })
      }
    }

    const gradientColors = interpolateColor(lowColor, highColor, 7)

    return {
      tooltip: {
        position: 'top' as const,
        ...getTooltipStyle(),
      },
      title: config.title ? {
        text: config.title, left: 'center', top: 8,
        textStyle: { fontSize: 14, color: colors.text },
      } : undefined,
      grid: {
        top: config.title ? 56 : 8,
        right: 16,
        bottom: 72,
        left: 80,
        containLabel: true,
      },
      xAxis: {
        type: 'category' as const,
        data: xValues,
        splitArea: { show: true },
        axisLabel: {
          fontSize: 10,
          color: colors.textMuted,
          rotate: xValues.length > 8 ? 45 : 0,
        },
        axisLine: { show: false },
        axisTick: { show: false },
      },
      yAxis: {
        type: 'category' as const,
        data: yValues,
        splitArea: { show: true },
        axisLabel: { fontSize: 10, color: colors.textMuted },
        axisLine: { show: false },
        axisTick: { show: false },
      },
      visualMap: {
        min: minVal !== Infinity ? minVal : 0,
        max: maxVal !== -Infinity ? maxVal : 1,
        calculable: true,
        orient: 'horizontal' as const,
        left: 'center',
        bottom: 0,
        textStyle: { fontSize: 10, color: colors.textMuted },
        inRange: {
          color: gradientColors,
        },
      },
      series: [{
        type: 'heatmap' as const,
        data: heatData,
        label: {
          show: config.showLabels,
          fontSize: 9,
        },
        emphasis: {
          itemStyle: {
            shadowBlur: 10,
            shadowColor: 'rgba(0, 0, 0, 0.3)',
          },
        },
      }],
    }
  }, [chartData, xCol, yCol, valueCol, config.title, config.showLabels, config.seriesColors, colors])

  return <EChartsContainer option={option} />
}

function HeatmapConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'heatmap'} onChange={v => onChange({ ...config, chartType: v as any })} />
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>X-axis column</div>
        <select
          aria-label="X-axis column"
          style={styles.select}
          value={config.xAxis ?? ''}
          onChange={e => onChange({ ...config, xAxis: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Categories shown on the horizontal axis</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Y-axis column</div>
        <select
          aria-label="Y-axis column"
          style={styles.select}
          value={config.yAxisColumn ?? ''}
          onChange={e => onChange({ ...config, yAxisColumn: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Categories shown on the vertical axis</ConfigHint>
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
        <ConfigHint>Numeric column that determines cell color intensity</ConfigHint>
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
        Show values
      </label>
      <ConfigHint>Display numeric values inside each cell</ConfigHint>
      <div style={{ ...styles.row, gap: 8 }}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Low color</div>
          <input
            type="color"
            value={config.seriesColors?._low ?? '#f0f9ff'}
            onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, _low: e.target.value } })}
            style={s.colorInput}
          />
          <ConfigHint>Color for lowest values</ConfigHint>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>High color</div>
          <input
            type="color"
            value={config.seriesColors?._high ?? '#0369a1'}
            onChange={e => onChange({ ...config, seriesColors: { ...config.seriesColors, _high: e.target.value } })}
            style={s.colorInput}
          />
          <ConfigHint>Color for highest values</ConfigHint>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  row: { display: 'flex', gap: 10 },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  input: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
}

const s: Record<string, React.CSSProperties> = {
  colorInput: { width: 32, height: 32, padding: 0, border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', background: 'none' },
}

export const HeatmapChartModule: ChartModule = {
  Component: HeatmapChartComponent,
  ConfigPanel: HeatmapConfigPanel,
  defaultConfig: { chartType: 'heatmap', showLabels: false, skipEmpty: true },
  detectColumns: (columns) => {
    const textCols = columns.filter(c => {
      const t = (c.type ?? '').toLowerCase()
      return t.includes('text') || t.includes('varchar') || t.includes('char') || !c.type
    })
    const numCols = columns.filter(c => isNumericType(c.type))
    return {
      xAxis: textCols[0]?.name ?? columns[0]?.name,
      yAxisColumn: textCols[1]?.name ?? textCols[0]?.name ?? columns[1]?.name ?? columns[0]?.name,
      yAxis: [numCols[0]?.name ?? columns[2]?.name ?? columns[1]?.name ?? columns[0]?.name],
      valueColumn: numCols[0]?.name ?? columns[2]?.name ?? columns[1]?.name ?? columns[0]?.name,
    }
  },
  requirements: { minColumns: 3 },
}
