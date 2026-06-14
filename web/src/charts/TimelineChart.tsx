import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, useRowsAsObjects, isTimeType } from './common'

function detectTimeColumns(columns: { name: string; type?: string }[]): string[] {
  return columns.filter(c => isTimeType(c.type)).map(c => c.name)
}

function TimelineChartComponent({ data, config }: ChartProps) {
  const columns = useMemo(() => data.columns.map(c => c.name), [data.columns])
  const timeCol = config.timeColumn ?? detectTimeColumns(data.columns)[0] ?? columns[0]
  const endTimeCol = config.endTimeColumn
  const labelCol = config.labelColumn
  const groupByCol = config.groupBy
  const showLabels = config.showLabels ?? true
  const maxLabelLength = config.maxLabelLength ?? 15
  const colors = useMemo(() => getChartColors(), [])

  const rowsAsObjects = useRowsAsObjects(data)
  const chartData = useMemo(() => {
    const mapped = rowsAsObjects.filter(d => d[timeCol] != null)
    mapped.sort((a, b) => new Date(String(a[timeCol])).getTime() - new Date(String(b[timeCol])).getTime())
    return mapped
  }, [rowsAsObjects, timeCol])

  const groups = useMemo(() => {
    return groupByCol
      ? [...new Set(chartData.map(d => String(d[groupByCol] ?? 'Unknown')))]
      : ['Events']
  }, [chartData, groupByCol])

  const isRangeMode = !!endTimeCol

  const truncateLabel = (label: unknown): string => {
    if (!label) return ''
    const str = String(label)
    return str.length > maxLabelLength ? str.substring(0, maxLabelLength) + '…' : str
  }

  const { option, height } = useMemo(() => {
    if (isRangeMode) {
      return {
        option: {
          tooltip: { ...getTooltipStyle(), trigger: 'axis' as const },
          legend: groups.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
          grid: { top: groups.length > 1 ? 40 : 16, right: 16, bottom: 40, left: 16, containLabel: true },
          xAxis: { type: 'time' as const, ...getAxisStyle() },
          yAxis: { type: 'category' as const, data: groups, inverse: true, ...getAxisStyle() },
          dataZoom: [{ type: 'slider' as const, xAxisIndex: 0, bottom: 0, height: 20 }],
          series: groups.map((group, gi) => ({
            name: group,
            type: 'custom' as const,
            renderItem: (params: Record<string, unknown>, api: { value: (idx: number) => number; coord: (v: number[]) => number[]; size: (v: number[]) => number[] }) => {
              const groupIndex = gi
              const startTime = api.value(0)
              const endTime = api.value(1)
              const start = api.coord([startTime, groupIndex])
              const end = api.coord([endTime, groupIndex])
              const height = api.size([0, 1])[1] * 0.6
              return {
                type: 'rect' as const,
                shape: { x: start[0], y: start[1] - height / 2, width: end[0] - start[0], height },
                style: { fill: config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length], opacity: 0.85 },
              }
            },
            encode: { x: [0, 1], y: 2 },
            data: chartData
              .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
              .map(d => [new Date(String(d[timeCol])).getTime(), new Date(String(d[endTimeCol!])).getTime(), group]),
            animation: false,
          })),
        },
        height: Math.max(200, groups.length * 50 + 80),
      }
    }

    return {
      option: {
        tooltip: {
          ...getTooltipStyle(),
          trigger: 'item' as const,
          formatter: (params: { data: unknown[]; seriesName: string }) => {
            const d = params.data as unknown[]
            const time = new Date(d[0] as number).toLocaleString()
            const label = d[2] ? `<br/><b>${d[2]}</b>` : ''
            const group = groups.length > 1 ? `<br/>Group: ${params.seriesName}` : ''
            return `<b>${time}</b>${label}${group}`
          },
        },
        legend: groups.length > 1 ? { top: 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
        grid: { top: groups.length > 1 ? 40 : 16, right: 80, bottom: 40, left: 16, containLabel: true },
        xAxis: { type: 'time' as const, ...getAxisStyle() },
        yAxis: {
          type: 'category' as const,
          data: groups,
          ...getAxisStyle(),
          show: groups.length > 1,
          axisLabel: { ...getAxisStyle().axisLabel, width: 60, overflow: 'truncate' as const }
        },
        dataZoom: [{ type: 'slider' as const, xAxisIndex: 0, bottom: 0, height: 20 }],
        series: groups.map((group, gi) => ({
          name: group,
          type: 'scatter' as const,
          symbolSize: 14,
          itemStyle: {
            color: config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length],
            borderColor: colors.text,
            borderWidth: 1,
            shadowBlur: 4,
            shadowColor: 'rgba(0,0,0,0.3)'
          },
          label: showLabels ? {
            show: true,
            position: 'top' as const,
            formatter: (params: { data: unknown[] }) => {
              const d = params.data as unknown[]
              return d[2] ? truncateLabel(d[2]) : ''
            },
            fontSize: 10,
            color: colors.textMuted,
            distance: 8,
            overflow: 'truncate' as const,
            ellipsis: '…',
          } : undefined,
          emphasis: {
            scale: 1.5,
            label: { show: true, fontSize: 12, fontWeight: 'bold' as const }
          },
          data: chartData
            .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
            .map(d => [
              new Date(String(d[timeCol])).getTime(),
              group,
              labelCol ? d[labelCol] : null,
            ]),
          animation: false,
        })),
      },
      height: 350,
    }
  }, [chartData, groups, isRangeMode, showLabels, maxLabelLength, timeCol, endTimeCol, labelCol, groupByCol, config.seriesColors, colors])

  return <EChartsContainer option={option} height={height} />
}

function TimelineConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  const showLabels = config.showLabels ?? true
  const timeCols = columns.filter(c => {
    const lower = c.toLowerCase()
    return lower.includes('time') || lower.includes('date') || lower.includes('timestamp') || lower === 'ts'
  })

  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Time column</div>
        <select
          aria-label="Time column"
          style={styles.select}
          value={config.timeColumn ?? ''}
          onChange={e => onChange({ ...config, timeColumn: e.target.value })}
        >
          {timeCols.length > 0
            ? timeCols.map(c => <option key={c} value={c}>{c}</option>)
            : columns.map(c => <option key={c} value={c}>{c}</option>)
          }
        </select>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>End time column <span style={{ fontWeight: 400, textTransform: 'none' }}>(optional, for ranges)</span></div>
        <select
          aria-label="End time column"
          style={styles.select}
          value={config.endTimeColumn ?? ''}
          onChange={e => onChange({ ...config, endTimeColumn: e.target.value || undefined })}
        >
          <option value="">None (point events)</option>
          {timeCols.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
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
      <div style={styles.row}>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showLabels ?? true}
            onChange={e => onChange({ ...config, showLabels: e.target.checked })}
          />
          Show labels
        </label>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showLegend ?? true}
            onChange={e => onChange({ ...config, showLegend: e.target.checked })}
          />
          Legend
        </label>
      </div>
      {showLabels && (
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Max label length</div>
          <select
            aria-label="Max label length"
            style={styles.select}
            value={config.maxLabelLength ?? 15}
            onChange={e => onChange({ ...config, maxLabelLength: Number(e.target.value) })}
          >
            <option value={10}>10 characters</option>
            <option value={15}>15 characters</option>
            <option value={20}>20 characters</option>
            <option value={30}>30 characters</option>
            <option value={50}>50 characters</option>
          </select>
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  row: { display: 'flex', gap: 10 },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
  checkbox: { fontSize: 12, color: 'var(--text-primary)', display: 'flex', alignItems: 'center', gap: 4 },
}

export const TimelineModule: ChartModule = {
  Component: TimelineChartComponent,
  ConfigPanel: TimelineConfigPanel,
  defaultConfig: { chartType: 'timeline', showLegend: true, showGrid: true, showLabels: true },
  detectColumns: (columns) => {
    const timeCols = columns.filter(c => isTimeType(c.type))
    const firstTextCol = columns.find(c => !isTimeType(c.type) && (!c.type || c.type.includes('text') || c.type.includes('varchar')))
    return {
      timeColumn: timeCols[0]?.name,
      endTimeColumn: timeCols[1]?.name,
      labelColumn: firstTextCol?.name,
    }
  },
  requirements: { minColumns: 2, needsTime: true },
}
