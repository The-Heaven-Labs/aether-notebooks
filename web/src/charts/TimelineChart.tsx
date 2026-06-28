import { useMemo, useCallback } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getTooltipStyle, getAxisStyle, getChartColors, useChartColors, useRowsAsObjects, isTimeType, ChartTypeSelect } from './common'
import { ConfigHint } from './ConfigHint'

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
  const colors = useChartColors()

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

  const truncateLabel = useCallback((label: unknown): string => {
    if (!label) return ''
    const str = String(label)
    return str.length > maxLabelLength ? str.substring(0, maxLabelLength) + '…' : str
  }, [maxLabelLength])

  function formatDuration(ms: number): string {
    const seconds = Math.floor(ms / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    if (hours > 0) return `${hours}h ${minutes % 60}m ${seconds % 60}s`
    if (minutes > 0) return `${minutes}m ${seconds % 60}s`
    return `${seconds}s`
  }

  const { option, height } = useMemo(() => {
    if (isRangeMode) {
      return {
        option: {
          tooltip: { ...getTooltipStyle(), trigger: 'axis' as const },
          title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
          legend: groups.length > 1 ? { top: config.title ? 30 : 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
          grid: { top: config.title ? (groups.length > 1 ? 56 : 46) : groups.length > 1 ? 30 : 12, right: 16, bottom: 16, left: 16, containLabel: true },
          xAxis: { type: 'time' as const, ...getAxisStyle() },
          yAxis: { type: 'category' as const, data: groups, inverse: true, ...getAxisStyle(), splitLine: { show: config.showGrid !== false } },
          dataZoom: [{
            type: 'slider' as const,
            xAxisIndex: 0,
            bottom: 0,
            height: 8,
            zoomOnMouseWheel: true,
            moveOnMouseMove: true,
          }],
          series: groups.map((group, gi) => ({
            name: group,
            type: 'custom' as const,
            renderItem: (params: Record<string, unknown>, api: { value: (idx: number) => number; coord: (v: number[]) => number[]; size: (v: number[]) => number[] }) => {
              const groupIndex = gi
              const startTime = api.value(0)
              const endTime = api.value(1)
              const start = api.coord([startTime, groupIndex])
              const end = api.coord([endTime, groupIndex])
              const barHeight = api.size([0, 1])[1] * 0.6
              return {
                type: 'rect' as const,
                shape: { x: start[0], y: start[1] - barHeight / 2, width: end[0] - start[0], height: barHeight },
                style: { fill: config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length], opacity: 0.85 },
              }
            },
            encode: { x: [0, 1], y: 2 },
            data: chartData
              .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
              .map(d => [new Date(String(d[timeCol])).getTime(), new Date(String(d[endTimeCol!])).getTime(), group]),
          })),
        },
        height: Math.max(200, groups.length * 36 + 60),
      }
    }

    const singleGroup = groups.length === 1

    const yAxisConfig = singleGroup ? {
      yAxis: {
        type: 'value' as const,
        show: false,
        min: 0,
        max: 1,
        splitLine: { show: config.showGrid !== false },
      }
    } : {
      yAxis: {
        type: 'category' as const,
        data: groups,
        ...getAxisStyle(),
        show: true,
        axisLabel: { ...getAxisStyle().axisLabel, width: 60, overflow: 'truncate' as const },
        splitLine: { show: config.showGrid !== false },
      }
    }

    const gridTop = singleGroup
      ? (config.title ? 76 : 50)
      : config.title
        ? (groups.length > 1 ? 66 : 46)
        : (groups.length > 1 ? 40 : 12)
    const gridConfig = singleGroup
      ? { top: gridTop, right: 16, bottom: 60, left: 16 }
      : { top: gridTop, right: 16, bottom: 16, left: 16, containLabel: true }

    const dataZoomConfig = [
      { type: 'inside' as const, xAxisIndex: 0 },
      { type: 'slider' as const, xAxisIndex: 0, bottom: 0, height: 8 },
    ]

    const connectorSeries = (config.showConnectors !== false && singleGroup)
      ? [{
          name: '__connector',
          type: 'line' as const,
          data: chartData
            .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === groups[0] : true)
            .map(d => [new Date(String(d[timeCol])).getTime(), 0.2]),
          lineStyle: { color: colors.textMuted, width: 1, type: 'dashed' as const, opacity: 0.25 },
          symbol: 'none',
          silent: true,
          z: 1,
        }]
      : []

    const chartHeight = singleGroup ? 320 : 350

    return {
      option: {
        tooltip: {
          ...getTooltipStyle(),
          trigger: 'item' as const,
          formatter: (params: { data: unknown[]; seriesName: string; dataIndex: number }) => {
            const raw = params.data as unknown[]
            const pointTime = raw[0] as number
            const time = new Date(pointTime).toLocaleString()
            const label = raw[2] ? `<br/><b>${raw[2]}</b>` : ''
            const groupInfo = groups.length > 1 ? `<br/>Group: ${params.seriesName}` : ''

            let delta = ''
            if (config.showTimeDeltas !== false) {
              const seriesGroupName = singleGroup ? groups[0] : params.seriesName
              const dataForGroup = chartData
                .filter(row => groupByCol ? String(row[groupByCol] ?? 'Unknown') === seriesGroupName : true)
                .sort((a, b) => new Date(String(a[timeCol])).getTime() - new Date(String(b[timeCol])).getTime())
              const eventIdx = dataForGroup.findIndex(row =>
                new Date(String(row[timeCol])).getTime() === pointTime
              )
              if (eventIdx > 0) {
                const prevTime = new Date(String(dataForGroup[eventIdx - 1][timeCol])).getTime()
                const diff = pointTime - prevTime
                if (diff > 0) {
                  delta = `<br/><span style="color:#888">Δ ${formatDuration(diff)}</span>`
                }
              } else {
                delta = `<br/><span style="color:#888">Sequence start</span>`
              }
            }

            return `<b>${time}</b>${label}${groupInfo}${delta}`
          },
        },
        title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
        legend: groups.length > 1 ? { top: config.title ? 30 : 0, textStyle: { fontSize: 11, color: colors.textMuted } } : undefined,
        grid: gridConfig,
        xAxis: { type: 'time' as const, ...getAxisStyle() },
        ...yAxisConfig,
        dataZoom: dataZoomConfig,
        series: [
          ...connectorSeries,
          ...groups.flatMap((group, gi) => {
            const groupData = chartData
              .filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
            const color = config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length]
            const itemStyle = {
              color,
              borderColor: colors.text,
              borderWidth: 1,
              shadowBlur: 4,
              shadowColor: 'rgba(0,0,0,0.3)',
            }
            const makeSeries = (
              labelPos: 'top' | 'bottom',
              dataFilter: (d: Record<string, unknown>, i: number) => boolean,
            ) => ({
              name: group,
              type: 'scatter' as const,
              symbolSize: 14,
              itemStyle,
              label: showLabels ? {
                show: true,
                position: labelPos,
                formatter: (p: { data: unknown[] }) => {
                  const row = p.data as unknown[]
                  return row[2] ? truncateLabel(row[2]) : ''
                },
                fontSize: 11,
                color: colors.textMuted,
                distance: 16,
                overflow: 'truncate' as const,
                ellipsis: '…',
              } : undefined,
              labelLayout: showLabels ? { hideOverlap: true } : undefined,
              emphasis: {
                scale: 1.5,
                label: { show: true, fontSize: 12, fontWeight: 'bold' as const },
              },
              data: groupData
                .filter(dataFilter)
                .map(d => [
                  new Date(String(d[timeCol])).getTime(),
                  singleGroup ? 0.2 : group,
                  labelCol ? d[labelCol] : null,
                ]),
              z: 2,
            })
            return [
              makeSeries('top', (_, i) => i % 2 === 0),
              makeSeries('bottom', (_, i) => i % 2 === 1),
            ]
          }),
        ],
      },
      height: chartHeight,
    }
  }, [chartData, groups, isRangeMode, showLabels, timeCol, endTimeCol, labelCol, groupByCol, config.title, config.seriesColors, config.showConnectors, config.showTimeDeltas, config.showGrid, colors, truncateLabel])

  return <EChartsContainer option={option} height={height} notMerge showReset />
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
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'timeline'} onChange={v => onChange({ ...config, chartType: v as any })} />
      </div>
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
        <ConfigHint>Column containing event timestamps</ConfigHint>
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
          {timeCols.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Optional column for event end times (enables range bars)</ConfigHint>
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
        <ConfigHint>Column for event text labels</ConfigHint>
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
        <ConfigHint>Column to group events into parallel swim lanes</ConfigHint>
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
          <ConfigHint>Truncate long labels to this character count</ConfigHint>
        </div>
      )}
      <div style={styles.row}>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showConnectors ?? true}
            onChange={e => onChange({ ...config, showConnectors: e.target.checked })}
          />
          Show connectors
        </label>
        <label style={styles.checkbox}>
          <input
            type="checkbox"
            checked={config.showTimeDeltas ?? true}
            onChange={e => onChange({ ...config, showTimeDeltas: e.target.checked })}
          />
          Show time deltas
        </label>
      </div>
      <ConfigHint>Connectors draw lines between related events, Time deltas show time differences</ConfigHint>
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
  defaultConfig: { chartType: 'timeline', showLegend: true, showGrid: true, showLabels: true, showConnectors: true, showTimeDeltas: true },
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
