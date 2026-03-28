import { useState } from 'react'
import type React from 'react'
import {
  BarChart, Bar, LineChart, Line, AreaChart, Area,
  ScatterChart, Scatter, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer,
} from 'recharts'
import { ChartConfigPanel } from './ChartConfigPanel'
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
}

const COLORS = ['#6366f1', '#22d3ee', '#f59e0b', '#10b981', '#ef4444', '#8b5cf6', '#ec4899']

export function ChartView({ output, rs, onConfigChange }: ChartViewProps) {
  // Normalise data source — either from output.data or from legacy rs prop
  const data = output?.data ?? (rs ? { columns: rs.columns, rows: rs.rows } : undefined)
  const columns = data?.columns?.map(c => c.name) ?? []

  const cfg = (output?.config ?? {}) as ChartConfig
  const xAxis = cfg.xAxis || columns[0] || ''
  const yAxes = cfg.yAxis?.length ? cfg.yAxis : columns.slice(1, 2)

  const [showConfig, setShowConfig] = useState(false)

  // Local state for legacy (rs-only) mode where no onConfigChange is provided
  const [localChartType, setLocalChartType] = useState<ChartConfig['chartType']>(cfg.chartType ?? 'bar')
  const [localXAxis, setLocalXAxis] = useState(xAxis)
  const [localYAxes, setLocalYAxes] = useState<string[]>(yAxes)

  const effectiveConfig: ChartConfig = onConfigChange
    ? cfg
    : { chartType: localChartType, xAxis: localXAxis, yAxis: localYAxes }

  const effectiveXAxis = effectiveConfig.xAxis || columns[0] || ''
  const effectiveYAxes = effectiveConfig.yAxis?.length ? effectiveConfig.yAxis : columns.slice(1, 2)

  const handleConfigChange = (newCfg: ChartConfig) => {
    if (onConfigChange) {
      onConfigChange(newCfg)
    } else {
      setLocalChartType(newCfg.chartType)
      setLocalXAxis(newCfg.xAxis)
      setLocalYAxes(newCfg.yAxis)
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

  const renderChart = () => {
    const commonProps = { data: chartData, margin: { top: 8, right: 16, bottom: 8, left: 0 } }
    switch (effectiveConfig.chartType ?? 'bar') {
      case 'bar':
        return (
          <BarChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={effectiveXAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip />
            {showLegend && <Legend />}
            {effectiveYAxes.map((y, i) => <Bar key={y} dataKey={y} fill={COLORS[i % COLORS.length]} />)}
          </BarChart>
        )
      case 'stacked_bar':
        return (
          <BarChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={effectiveXAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip />
            {showLegend && <Legend />}
            {effectiveYAxes.map((y, i) => <Bar key={y} dataKey={y} stackId="a" fill={COLORS[i % COLORS.length]} />)}
          </BarChart>
        )
      case 'line':
        return (
          <LineChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={effectiveXAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip />
            {showLegend && <Legend />}
            {effectiveYAxes.map((y, i) => (
              <Line key={y} type="monotone" dataKey={y} stroke={COLORS[i % COLORS.length]} dot={false} />
            ))}
          </LineChart>
        )
      case 'area':
        return (
          <AreaChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={effectiveXAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip />
            {showLegend && <Legend />}
            {effectiveYAxes.map((y, i) => (
              <Area key={y} type="monotone" dataKey={y} stroke={COLORS[i % COLORS.length]} fill={COLORS[i % COLORS.length]} fillOpacity={0.3} />
            ))}
          </AreaChart>
        )
      case 'pie':
      case 'donut': {
        const innerRadius = effectiveConfig.chartType === 'donut' ? '60%' : 0
        return (
          <PieChart>
            <Pie
              data={chartData}
              dataKey={effectiveYAxes[0] ?? ''}
              nameKey={effectiveXAxis}
              cx="50%"
              cy="50%"
              outerRadius="80%"
              innerRadius={innerRadius}
            >
              {chartData.map((_, i) => <Cell key={i} fill={COLORS[i % COLORS.length]} />)}
            </Pie>
            <Tooltip />
            {showLegend && <Legend />}
          </PieChart>
        )
      }
      case 'scatter':
        return (
          <ScatterChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={effectiveXAxis} name={effectiveXAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis dataKey={effectiveYAxes[0]} name={effectiveYAxes[0]} tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip />
            <Scatter data={chartData} fill={COLORS[0]} />
          </ScatterChart>
        )
      default:
        return <div style={{ color: '#888', padding: 16 }}>Unknown chart type</div>
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
          aria-label={showConfig ? 'Hide configure' : 'Configure'}
        >
          {showConfig ? 'Hide' : 'Configure'}
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
    borderTop: '1px solid var(--border-light)',
    background: 'white',
  },
  configBtn: {
    fontSize: 11,
    color: 'var(--text-muted)',
    padding: '4px 12px',
    background: 'none',
    border: 'none',
    cursor: 'pointer',
  },
  empty: {
    padding: '16px',
    color: 'var(--text-muted)',
    fontSize: 13,
    textAlign: 'center',
  },
}
