import { useState } from 'react'
import type React from 'react'
import {
  BarChart, Bar, LabelList, LineChart, Line, AreaChart, Area,
  ScatterChart, Scatter, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer,
} from 'recharts'
import { Settings2 } from 'lucide-react'
import { ChartConfigPanel, DEFAULT_COLORS } from './ChartConfigPanel'
import type { ChartConfig } from './ChartConfigPanel'
import type { ResultSet } from '../types'

interface OutputShape {
  type: string
  data?: { columns: { name: string; type?: string }[]; rows: unknown[][] }
  config?: unknown
}

interface ChartViewProps {
  /** New API: pass a full output object with optional config */
  output?: OutputShape
  /** Legacy API: pass a ResultSet directly (used by OutputRenderer) */
  rs?: ResultSet
  onConfigChange?: (config: ChartConfig) => void
  /** When provided and no onConfigChange, config is persisted to localStorage */
  cellId?: string
}

export function ChartView({ output, rs, onConfigChange, cellId }: ChartViewProps) {
  // Normalise data source — either from output.data or from legacy rs prop
  const data = output?.data ?? (rs ? { columns: rs.columns, rows: rs.rows } : undefined)
  const columns = data?.columns?.map(c => c.name) ?? []

  const cfg = (output?.config ?? {}) as ChartConfig
  const xAxis = cfg.xAxis || columns[0] || ''
  const yAxes = cfg.yAxis?.length ? cfg.yAxis : columns.slice(1, 2)

  const savedConfig = (() => {
    if (!onConfigChange && cellId) {
      try {
        const saved = localStorage.getItem(`hnb_chart_config_${cellId}`)
        return saved ? (JSON.parse(saved) as ChartConfig) : null
      } catch { /* ignore */ }
    }
    return null
  })()

  const [showConfig, setShowConfig] = useState(false)

  // Local state for legacy (rs-only) mode where no onConfigChange is provided
  const [localChartType, setLocalChartType] = useState<ChartConfig['chartType']>(() =>
    savedConfig?.chartType ?? cfg.chartType ?? 'bar'
  )
  const [localXAxis, setLocalXAxis] = useState<string>(() =>
    savedConfig?.xAxis ?? xAxis
  )
  const [localYAxes, setLocalYAxes] = useState<string[]>(() =>
    savedConfig?.yAxis ?? yAxes
  )
  const [localShowLegend, setLocalShowLegend] = useState<boolean | undefined>(() =>
    savedConfig?.showLegend
  )
  const [localShowGrid, setLocalShowGrid] = useState<boolean | undefined>(() =>
    savedConfig?.showGrid
  )
  const [localShowLabels, setLocalShowLabels] = useState<boolean | undefined>(() =>
    savedConfig?.showLabels
  )
  const [localSeriesColors, setLocalSeriesColors] = useState<Record<string, string> | undefined>(() =>
    savedConfig?.seriesColors
  )
  const [localTitle, setLocalTitle] = useState<string | undefined>(() =>
    savedConfig?.title
  )

  const effectiveConfig: ChartConfig = onConfigChange
    ? cfg
    : {
        chartType: localChartType,
        xAxis: localXAxis,
        yAxis: localYAxes,
        showLegend: localShowLegend,
        showGrid: localShowGrid,
        showLabels: localShowLabels,
        seriesColors: localSeriesColors,
        title: localTitle,
      }

  const getColor = (series: string, index: number): string =>
    effectiveConfig.seriesColors?.[series] ?? DEFAULT_COLORS[index % DEFAULT_COLORS.length]

  const effectiveXAxis = effectiveConfig.xAxis || columns[0] || ''
  const effectiveYAxes = effectiveConfig.yAxis?.length ? effectiveConfig.yAxis : columns.slice(1, 2)

  const handleConfigChange = (newCfg: ChartConfig) => {
    if (onConfigChange) {
      onConfigChange(newCfg)
    } else {
      setLocalChartType(newCfg.chartType)
      setLocalXAxis(newCfg.xAxis)
      setLocalYAxes(newCfg.yAxis)
      setLocalShowLegend(newCfg.showLegend)
      setLocalShowGrid(newCfg.showGrid)
      setLocalShowLabels(newCfg.showLabels)
      setLocalSeriesColors(newCfg.seriesColors)
      setLocalTitle(newCfg.title)
      if (cellId) {
        try {
          localStorage.setItem(`hnb_chart_config_${cellId}`, JSON.stringify(newCfg))
        } catch { /* ignore */ }
      }
    }
  }

  const chartData = (data?.rows ?? []).map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = (row as unknown[])[i] })
    return obj
  })

  if (columns.length < 2) {
    return <p style={styles.empty}>Need at least 2 columns to chart</p>
  }

  const showLegend = effectiveConfig.showLegend ?? true
  const showGrid = effectiveConfig.showGrid ?? true
  const showLabels = effectiveConfig.showLabels ?? false

  const   tooltipStyle: React.CSSProperties = {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    color: 'var(--text-primary)',
    boxShadow: 'var(--shadow-md)',
  }
  const legendStyle: React.CSSProperties = {
    fontSize: 12,
    color: 'var(--text-muted)',
  }

  const renderChart = () => {
    const commonProps = { data: chartData, margin: { top: 8, right: 16, bottom: 8, left: 0 } }
    switch (effectiveConfig.chartType ?? 'bar') {
      case 'bar':
        return (
          <BarChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="0" stroke="var(--border)" vertical={false} />}
            <XAxis dataKey={effectiveXAxis} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} width={40} />
            <Tooltip contentStyle={tooltipStyle} cursor={{ fill: 'var(--border)', opacity: 0.4 }} />
            {showLegend && <Legend wrapperStyle={legendStyle} />}
            {effectiveYAxes.map((y, i) => (
              <Bar key={y} dataKey={y} fill={getColor(y, i)} radius={[3, 3, 0, 0]}>
                {showLabels && (
                  <LabelList dataKey={y} position="top" style={{ fontSize: 10, fill: 'var(--text-muted)' }} />
                )}
              </Bar>
            ))}
          </BarChart>
        )
      case 'stacked_bar':
        return (
          <BarChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="0" stroke="var(--border)" vertical={false} />}
            <XAxis dataKey={effectiveXAxis} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} width={40} />
            <Tooltip contentStyle={tooltipStyle} cursor={{ fill: 'var(--border)', opacity: 0.4 }} />
            {showLegend && <Legend wrapperStyle={legendStyle} />}
            {effectiveYAxes.map((y, i) => (
              <Bar key={y} dataKey={y} stackId="a" fill={getColor(y, i)} radius={i === effectiveYAxes.length - 1 ? [3, 3, 0, 0] : [0, 0, 0, 0]}>
                {showLabels && (
                  <LabelList dataKey={y} position="top" style={{ fontSize: 10, fill: 'var(--text-muted)' }} />
                )}
              </Bar>
            ))}
          </BarChart>
        )
      case 'line':
        return (
          <LineChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="0" stroke="var(--border)" vertical={false} />}
            <XAxis dataKey={effectiveXAxis} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} width={40} />
            <Tooltip contentStyle={tooltipStyle} />
            {showLegend && <Legend wrapperStyle={legendStyle} />}
            {effectiveYAxes.map((y, i) => (
              <Line key={y} type="monotone" dataKey={y} stroke={getColor(y, i)} strokeWidth={2} dot={{ r: 3, strokeWidth: 0 }} activeDot={{ r: 5 }}>
                {showLabels && (
                  <LabelList dataKey={y} position="top" style={{ fontSize: 10, fill: 'var(--text-muted)' }} />
                )}
              </Line>
            ))}
          </LineChart>
        )
      case 'area':
        return (
          <AreaChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="0" stroke="var(--border)" vertical={false} />}
            <XAxis dataKey={effectiveXAxis} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} width={40} />
            <Tooltip contentStyle={tooltipStyle} />
            {showLegend && <Legend wrapperStyle={legendStyle} />}
            {effectiveYAxes.map((y, i) => (
              <Area key={y} type="monotone" dataKey={y} stroke={getColor(y, i)} strokeWidth={2} fill={getColor(y, i)} fillOpacity={0.15} dot={false} activeDot={{ r: 5 }}>
                {showLabels && (
                  <LabelList dataKey={y} position="top" style={{ fontSize: 10, fill: 'var(--text-muted)' }} />
                )}
              </Area>
            ))}
          </AreaChart>
        )
      case 'pie':
      case 'donut': {
        const innerRadius = effectiveConfig.chartType === 'donut' ? '55%' : 0
        return (
          <PieChart>
            <Pie
              data={chartData}
              dataKey={effectiveYAxes[0] ?? ''}
              nameKey={effectiveXAxis}
              cx="50%"
              cy="50%"
              outerRadius="75%"
              innerRadius={innerRadius}
              paddingAngle={2}
            >
              {chartData.map((entry, i) => {
                const label = String((entry as Record<string, unknown>)[effectiveXAxis] ?? i)
                return <Cell key={i} fill={effectiveConfig.seriesColors?.[label] ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length]} stroke="none" />
              })}
            </Pie>
            <Tooltip contentStyle={tooltipStyle} />
            {showLegend && <Legend wrapperStyle={legendStyle} />}
          </PieChart>
        )
      }
      case 'scatter':
        return (
          <ScatterChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="0" stroke="var(--border)" />}
            <XAxis dataKey={effectiveXAxis} name={effectiveXAxis} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} />
            <YAxis dataKey={effectiveYAxes[0]} name={effectiveYAxes[0]} tick={{ fontSize: 11, fill: 'var(--text-muted)' }} axisLine={false} tickLine={false} width={40} />
            <Tooltip contentStyle={tooltipStyle} cursor={{ strokeDasharray: '3 3', stroke: 'var(--border)' }} />
            <Scatter data={chartData} fill={getColor(effectiveYAxes[0] ?? '', 0)} />
          </ScatterChart>
        )
      default:
        return <div style={{ color: 'var(--text-muted)', padding: 16 }}>Unknown chart type</div>
    }
  }

  return (
    <div style={styles.wrap}>
      <div data-testid="chart-container">
        <ResponsiveContainer width="100%" height={300}>
          {renderChart()}
        </ResponsiveContainer>
      </div>
      <div>
        <button
          style={styles.configBtn}
          onClick={() => setShowConfig(v => !v)}
          aria-label={showConfig ? 'Close chart config' : 'Configure chart'}
        >
          <Settings2 size={13} />
          {showConfig ? ' Close' : ' Configure'}
        </button>
        {showConfig && (
          <ChartConfigPanel
            config={effectiveConfig}
            columns={columns}
            onChange={handleConfigChange}
          />
        )}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  wrap: {
    width: '100%',
    padding: '12px 16px 4px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
  },
  configBtn: {
    fontSize: 11,
    color: 'var(--text-muted)',
    padding: '4px 12px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    gap: 4,
  },
  empty: {
    padding: '16px',
    color: 'var(--text-muted)',
    fontSize: 13,
    textAlign: 'center',
  },
}
