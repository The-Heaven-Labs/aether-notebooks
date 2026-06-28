import { useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, getTooltipStyle, getChartColors, useChartColors, CHART_COLORS, useRowsAsObjects, ChartTypeSelect } from './common'
import { ConfigHint } from './ConfigHint'

function SankeyChartComponent({ data, config }: ChartProps) {
  const chartData = useRowsAsObjects(data)
  const colors = useChartColors()

  const sourceCol = config.xAxis ?? data.columns[0]?.name ?? ''
  const targetCol = config.yAxis?.[0] ?? data.columns[1]?.name ?? ''
  const valueCol = config.yAxis?.[1] ?? data.columns[2]?.name ?? ''

  const { nodes, links } = useMemo(() => {
    const nodeSet = new Set<string>()
    const rawLinks: Array<{ source: string; target: string; value: number }> = []

    for (const row of chartData) {
      const src = String(row[sourceCol] ?? '')
      const tgt = String(row[targetCol] ?? '')
      const val = Number(row[valueCol] ?? 1)
      if (!src || !tgt || isNaN(val)) continue
      nodeSet.add(src)
      nodeSet.add(tgt)
      rawLinks.push({ source: src, target: tgt, value: val })
    }

    // Sankey requires DAG — remove cycles by topological sort, dropping back-edges
    const allNodes = Array.from(nodeSet)
    const adj: Record<string, string[]> = {}
    for (const n of allNodes) adj[n] = []
    for (const l of rawLinks) adj[l.source]?.push(l.target)

    const visited = new Set<string>()
    const inStack = new Set<string>()
    const backEdges = new Set<string>()
    const order: string[] = []

    function dfs(n: string) {
      if (visited.has(n)) return
      inStack.add(n)
      for (const next of adj[n] ?? []) {
        if (inStack.has(next)) {
          backEdges.add(`${n}->${next}`)
        } else if (!visited.has(next)) {
          dfs(next)
        }
      }
      inStack.delete(n)
      visited.add(n)
      order.push(n)
    }
    for (const n of allNodes) dfs(n)

    const linkList = rawLinks.filter(l => !backEdges.has(`${l.source}->${l.target}`))

    const nodeColors: Record<string, string> = {}
    let ci = 0
    for (const name of nodeSet) {
      const custom = config.seriesColors?.[name]
      if (custom) nodeColors[name] = custom
      else {
        nodeColors[name] = CHART_COLORS[ci % CHART_COLORS.length]
        ci++
      }
    }

    return {
      nodes: Array.from(nodeSet).map(name => ({
        name,
        itemStyle: { color: nodeColors[name] },
      })),
      links: linkList,
    }
  }, [chartData, sourceCol, targetCol, valueCol, config.seriesColors])

  const option = useMemo(() => ({
    tooltip: {
      trigger: 'item' as const,
      ...getTooltipStyle(),
      formatter: (p: any) => {
        if (p.dataType === 'edge') {
          return `${p.data.source} → ${p.data.target}: ${p.data.value}`
        }
        return `${p.name}`
      },
    },
    title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: colors.text } } : undefined,
    series: [{
      type: 'sankey' as const,
      layoutIterations: 32,
      nodeAlign: config.nodeAlign ?? 'justify',
      nodeWidth: config.nodeWidth ?? 20,
      nodeGap: config.nodeGap ?? 12,
      roam: true,
      data: nodes,
      links,
      lineStyle: {
        color: 'gradient' as const,
        curveness: 0.5,
        opacity: 0.4,
      },
      label: {
        fontSize: 11,
        color: colors.text,
      },
      emphasis: {
        focus: 'adjacency' as const,
      },
    }],
  }), [nodes, links, colors, config.title, config.nodeWidth, config.nodeGap, config.nodeAlign])

  return <EChartsContainer option={option} showReset />
}

function SankeyConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  return (
    <div style={styles.panel}>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Chart type</div>
        <ChartTypeSelect value={config.chartType ?? 'sankey'} onChange={v => onChange({ ...config, chartType: v as any })} />
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Source column</div>
        <select
          aria-label="Source column"
          style={styles.select}
          value={config.xAxis ?? ''}
          onChange={e => onChange({ ...config, xAxis: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Column for flow origin (left side nodes)</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Target column</div>
        <select
          aria-label="Target column"
          style={styles.select}
          value={config.yAxis?.[0] ?? ''}
          onChange={e => onChange({ ...config, yAxis: [e.target.value, config.yAxis?.[1] ?? ''].filter(Boolean) })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Column for flow destination (right side nodes)</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Value column</div>
        <select
          aria-label="Value column"
          style={styles.select}
          value={config.yAxis?.[1] ?? ''}
          onChange={e => onChange({ ...config, yAxis: [config.yAxis?.[0] ?? '', e.target.value].filter(Boolean) })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Numeric column for flow width (volume, count, etc.)</ConfigHint>
      </div>
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Node width</div>
          <select
            aria-label="Node width"
            style={styles.select}
            value={String(config.nodeWidth ?? 20)}
            onChange={e => onChange({ ...config, nodeWidth: Number(e.target.value) })}
          >
            <option value="10">Thin</option>
            <option value="20">Normal</option>
            <option value="30">Thick</option>
            <option value="50">Wide</option>
          </select>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Node gap</div>
          <select
            aria-label="Node gap"
            style={styles.select}
            value={String(config.nodeGap ?? 12)}
            onChange={e => onChange({ ...config, nodeGap: Number(e.target.value) })}
          >
            <option value="4">Tight</option>
            <option value="12">Normal</option>
            <option value="24">Wide</option>
            <option value="40">Extra</option>
          </select>
        </div>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Node alignment</div>
        <select
          aria-label="Node alignment"
          style={styles.select}
          value={config.nodeAlign ?? 'justify'}
          onChange={e => onChange({ ...config, nodeAlign: e.target.value as any })}
        >
          <option value="justify">Justify</option>
          <option value="left">Left</option>
          <option value="right">Right</option>
        </select>
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
}

export const SankeyChartModule: ChartModule = {
  Component: SankeyChartComponent,
  ConfigPanel: SankeyConfigPanel,
  defaultConfig: { chartType: 'sankey', showLegend: false, showGrid: false, showLabels: false },
  detectColumns: (columns) => ({
    xAxis: columns[0]?.name,
    yAxis: columns.slice(1, 3).map(c => c.name),
  }),
  requirements: { minColumns: 3 },
}
