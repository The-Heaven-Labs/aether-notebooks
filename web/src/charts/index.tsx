import { useState, useMemo } from 'react'
import type React from 'react'
import { Settings2 } from 'lucide-react'
import type { ResultSet } from '../types'
import type { ChartConfig, ChartModule } from './types'
import { ChartConfigModal } from './ChartConfigModal'
import { BarChartModule } from './BarChart'
import { LineChartModule } from './LineChart'
import { AreaChartModule } from './AreaChart'
import { ScatterChartModule } from './ScatterChart'
import { PieChartModule } from './PieChart'
import { TimelineModule } from './TimelineChart'
import { HierarchyTreeModule } from './HierarchyTreeChart'
import { BigNumberModule } from './BigNumber'
import { MapChartModule } from './MapChart'
import { SankeyChartModule } from './SankeyChart'
import { FunnelChartModule } from './FunnelChart'
import { HeatmapChartModule } from './HeatmapChart'
import { HistogramChartModule } from './HistogramChart'
// Registry
export const CHART_MODULES: Record<string, ChartModule> = {
  bar: BarChartModule,
  line: LineChartModule,
  area: AreaChartModule,
  scatter: ScatterChartModule,
  pie: PieChartModule,
  donut: PieChartModule,
  timeline: TimelineModule,
  hierarchy_tree: HierarchyTreeModule,
  big_number: BigNumberModule,
  map: MapChartModule,
  sankey: SankeyChartModule,
  funnel: FunnelChartModule,
  heatmap: HeatmapChartModule,
  histogram: HistogramChartModule,
}

export { ALL_CHART_TYPES } from './common'

interface ChartViewProps {
  output?: { type: string; data?: ResultSet; config?: Partial<ChartConfig> }
  rs?: ResultSet
  onConfigChange?: (config: ChartConfig) => void
}

export function ChartView({ output, rs, onConfigChange }: ChartViewProps) {
  const data = output?.data ?? (rs ? { columns: rs.columns, rows: rs.rows } : undefined)
  const columns = data?.columns?.map(c => c.name) ?? []

  const cfg = (output?.config ?? {}) as ChartConfig
  // Migrate legacy stacked_bar/horizontal_bar → bar + barMode
  const rawType = (cfg as any).chartType as string | undefined
  if (rawType === 'stacked_bar' || rawType === 'horizontal_bar') {
    cfg.barMode = rawType === 'stacked_bar' ? 'stacked' : 'horizontal'
    cfg.chartType = 'bar'
  }
  // Migrate legacy stacked_area → area + areaMode
  if (rawType === 'stacked_area') {
    cfg.areaMode = 'stacked'
    cfg.chartType = 'area'
  }
  const chartType = cfg.chartType ?? 'bar'

  // Compute group values from data for the config panel (data may not propagate as a prop)
  const groupValues = useMemo(() => {
    const gb = cfg.groupBy
    if (!data?.columns?.length || !gb) return []
    const colIndex = data.columns.findIndex(c => c.name === gb)
    if (colIndex < 0) return []
    const seen = new Set<string>()
    const vals: string[] = []
    for (const row of data.rows) {
      const v = String(row[colIndex] ?? '')
      if (v && !seen.has(v)) { seen.add(v); vals.push(v) }
    }
    return vals
  }, [data, cfg.groupBy])

  const [showConfig, setShowConfig] = useState(false)

  const handleConfigChange = (newCfg: ChartConfig) => {
    if (onConfigChange) {
      onConfigChange(newCfg)
    }
  }

  const handleSaveConfig = (cfg: ChartConfig) => {
    handleConfigChange(cfg)
    setShowConfig(false)
  }

  const mod = CHART_MODULES[chartType]
  if (!mod) {
    return <div style={{ padding: 16, color: 'var(--text-muted)' }}>Unknown chart type: {chartType}</div>
  }

  const effectiveConfig: ChartConfig = { ...mod.defaultConfig, ...cfg }

  if (!data || columns.length < (mod.requirements.minColumns ?? 2)) {
    return (
      <div style={styles.wrap}>
        <div style={styles.emptyGuidance}>
          <div style={styles.emptyIcon}>📊</div>
          <p style={styles.emptyTitle}>Not enough data to chart</p>
          <p style={styles.emptyText}>
            This chart needs at least {mod.requirements.minColumns} column(s).
            Switch to a different chart type or run a query with more columns.
          </p>
        </div>
        {onConfigChange && (
          <div>
            <button
              style={styles.configBtn}
              onClick={() => setShowConfig(true)}
              aria-label="Configure chart"
            >
              <Settings2 size={13} /> Configure
            </button>
            {showConfig && (
              <ChartConfigModal
                config={effectiveConfig}
                columns={columns}
                data={data}
                groupValues={groupValues}
                onSave={handleSaveConfig}
                onClose={() => setShowConfig(false)}
                mod={mod}
              />
            )}
          </div>
        )}
      </div>
    )
  }

  return (
    <div style={styles.wrap}>
      <mod.Component data={data} config={effectiveConfig} />
      {onConfigChange && (
        <div>
          <button
            style={styles.configBtn}
            onClick={() => setShowConfig(true)}
            aria-label="Configure chart"
          >
            <Settings2 size={13} /> Configure
          </button>
          {showConfig && (
            <ChartConfigModal
              config={effectiveConfig}
              columns={columns}
              data={data}
              groupValues={groupValues}
              onSave={handleSaveConfig}
              onClose={() => setShowConfig(false)}
              mod={mod}
            />
          )}
        </div>
      )}
    </div>
  )
}

// Re-export types and defaults
export type { ChartConfig, ChartModule, ChartType, MarkLineConfig } from './types'
export { CHART_COLORS, EChartsContainer } from './common'

const styles: Record<string, React.CSSProperties> = {
  wrap: { display: 'flex', flexDirection: 'column', height: '100%' },
  emptyGuidance: { padding: 24, textAlign: 'center' as const, color: 'var(--text-muted)' },
  emptyIcon: { fontSize: 32, marginBottom: 8 },
  emptyTitle: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)', margin: '0 0 4px' },
  emptyText: { fontSize: 12, margin: 0 },
  configBtn: {
    display: 'inline-flex', alignItems: 'center', gap: 4,
    fontSize: 11, padding: '4px 8px', marginTop: 8, marginLeft: 8,
    background: 'var(--bg-input)', color: 'var(--text-muted)',
    border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer',
  },
}
