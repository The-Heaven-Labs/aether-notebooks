import { useRef, useCallback, useMemo } from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, getChartColors, useRowsAsObjects, walkTree, applyCollapsedToTree } from './common'
import type { ECharts } from 'echarts/core'
import { ConfigHint } from './ConfigHint'

interface TreeNode {
  name: string
  children?: TreeNode[]
  itemStyle?: { color: string }
  collapsed?: boolean
}

function detectParentChild(columns: { name: string; type?: string }[], rows: unknown[][]): { idCol: string; parentCol: string } | null {
  const idCols = columns.filter(c => {
    const t = (c.type ?? '').toLowerCase()
    return t.includes('int') || t.includes('bigint') || t.includes('id') || t.includes('serial')
  })

  if (idCols.length < 2) return null

  for (let i = 0; i < idCols.length; i++) {
    for (let j = 0; j < idCols.length; j++) {
      if (i === j) continue
      const childValues = new Set(rows.map(r => r[i]))
      const parentValues = new Set(rows.map(r => r[j]))
      const childInParent = [...childValues].every(v => parentValues.has(v))
      if (childInParent && childValues.size > 0) {
        return { idCol: columns[columns.indexOf(idCols[i])].name, parentCol: columns[columns.indexOf(idCols[j])].name }
      }
    }
  }

  return { idCol: idCols[0].name, parentCol: idCols[1].name }
}

function buildTree(
  rows: Record<string, unknown>[],
  idCol: string,
  parentCol: string,
  labelCol: string,
  metricCols: string[],
  colors: Record<string, string>,
): TreeNode[] {
  const nodeMap = new Map<string, TreeNode & { parentId: string }>()
  const roots: TreeNode[] = []

  rows.forEach((row, i) => {
    const id = String(row[idCol] ?? `node-${i}`)
    const label = labelCol ? String(row[labelCol] ?? id) : id
    const node: TreeNode & { parentId: string } = {
      name: label,
      parentId: String(row[parentCol] ?? ''),
      itemStyle: { color: colors[label] ?? CHART_COLORS[i % CHART_COLORS.length] },
    }
    if (metricCols.length > 0) {
      const metrics = metricCols.map(c => `${c}: ${row[c]}`).join(', ')
      node.name = `${label}\n${metrics}`
    }
    nodeMap.set(id, node)
  })

  nodeMap.forEach((node, id) => {
    if (node.parentId && node.parentId !== id && nodeMap.has(node.parentId)) {
      const parent = nodeMap.get(node.parentId)!
      if (!parent.children) parent.children = []
      parent.children.push(node)
    } else {
      roots.push(node)
    }
  })

  return roots
}

function HierarchyTreeComponent({ data, config }: ChartProps) {
  const chartColors = useMemo(() => getChartColors(), [])
  const chartInstance = useRef<echarts.ECharts | null>(null)
  const handleChartReady = useCallback((chart: echarts.ECharts) => {
    chartInstance.current = chart
  }, [])
  const handleReset = useCallback(() => {
    const chart = chartInstance.current
    if (!chart) return
    try {
      const opt = chart.getOption() as any
      const series = opt?.series?.[0]
      if (series?.type === 'tree' && series.data) {
        const collapsed = new Set<string>()
        const data = Array.isArray(series.data) ? series.data : [series.data]
        walkTree(data, n => { if (n.collapsed) collapsed.add(n.name) })

        chart.dispatchAction({ type: 'restore' })

        if (collapsed.size > 0) {
          const after = chart.getOption() as any
          const afterSeries = after?.series?.[0]
          if (afterSeries?.type === 'tree' && afterSeries.data) {
            chart.setOption({
              series: [{ ...afterSeries, data: applyCollapsedToTree(afterSeries.data, collapsed) }],
            }, { notMerge: false })
          }
        }
      }
    } catch { /* ignore */ }
  }, [])

  const columns = useMemo(() => data.columns.map(c => c.name), [data.columns])
  const idCol = config.idColumn ?? columns[0]
  const parentCol = config.parentIdColumn ?? columns[1]
  const labelCol = config.labelColumn ?? columns.find(c => !c.includes('id') && !c.includes('parent') && c !== idCol && c !== parentCol) ?? idCol
  const metricCols = useMemo(() => config.metricColumns ?? [], [config.metricColumns])
  const isHorizontal = config.layout === 'left-to-right'
  const nodeSpacing = config.nodeSpacing ?? 50

  const chartData = useRowsAsObjects(data)

  const treeData = useMemo(
    () => buildTree(chartData, idCol, parentCol, labelCol, metricCols, config.seriesColors ?? {}),
    [chartData, idCol, parentCol, labelCol, metricCols, config.seriesColors]
  )
  const treeDataWithState = treeData
  const rowCount = data.rows.length
  const hasMetrics = metricCols.length > 0

  const horizontalMargin = isHorizontal ? 15 : Math.max(5, 25 - nodeSpacing / 5)
  const rightMargin = isHorizontal ? 20 : Math.max(5, 25 - nodeSpacing / 5)
  const bottomMargin = isHorizontal ? '8%' : (hasMetrics ? '30%' : '12%')

  const option = useMemo(() => ({
    tooltip: { show: false },
    series: [{
      type: 'tree' as const,
      data: treeDataWithState,
      orient: isHorizontal ? 'LR' as const : 'TB' as const,
      top: '8%',
      bottom: bottomMargin,
      left: `${horizontalMargin}%`,
      right: `${rightMargin}%`,
      roam: true,
      initialTreeDepth: -1,
      symbolSize: 12,
      edgeShape: 'curve' as const,
      label: {
        position: isHorizontal ? 'right' as const : 'top' as const,
        verticalAlign: 'middle' as const,
        fontSize: 11,
        color: chartColors.text,
        formatter: (params: { name: string }) => params.name,
        overflow: 'truncate' as const,
        width: 100,
      },
      leaves: {
        label: {
          position: isHorizontal ? 'right' as const : 'bottom' as const,
          verticalAlign: 'middle' as const,
          fontSize: 11,
          color: chartColors.text,
          overflow: 'truncate' as const,
          width: 120,
        },
      },
      expandAndCollapse: true,
      animationDuration: 200,
      animationDurationUpdate: 200,
      lineStyle: { color: chartColors.border, width: 1.5, curveness: 0.5 },
      itemStyle: { borderColor: chartColors.bgCard },
    }],
  }), [treeDataWithState, isHorizontal, horizontalMargin, rightMargin, bottomMargin])

  const height = isHorizontal ? 500 : Math.min(600, 250 + rowCount * (nodeSpacing * 0.6))

  return (
    <div style={{ position: 'relative' }}>
      <EChartsContainer option={option} height={height} onChartReady={handleChartReady} notMerge={true} showReset />
      <button
        type="button"
        onClick={handleReset}
        style={styles.resetButton}
        title="Reset zoom and pan"
      >
        ⟲ Reset view
      </button>
    </div>
  )
}

function HierarchyTreeConfigPanel({ config, columns, onChange }: ConfigPanelProps) {
  const idCols = columns.filter(c => {
    const t = c.toLowerCase()
    return t.includes('id') || t.includes('pid') || t.includes('key') || t.includes('code')
  })

  return (
    <div style={styles.panel}>
      <div style={styles.row}>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>ID column</div>
          <select
            aria-label="ID column"
            style={styles.select}
            value={config.idColumn ?? ''}
            onChange={e => onChange({ ...config, idColumn: e.target.value })}
          >
            {idCols.length > 0
              ? idCols.map(c => <option key={c} value={c}>{c}</option>)
              : columns.map(c => <option key={c} value={c}>{c}</option>)
            }
          </select>
          <ConfigHint>Unique identifier for each node</ConfigHint>
        </div>
        <div style={styles.section}>
          <div style={styles.sectionLabel}>Parent ID column</div>
          <select
            aria-label="Parent ID column"
            style={styles.select}
            value={config.parentIdColumn ?? ''}
            onChange={e => onChange({ ...config, parentIdColumn: e.target.value })}
          >
            {idCols.length > 0
              ? idCols.map(c => <option key={c} value={c}>{c}</option>)
              : columns.map(c => <option key={c} value={c}>{c}</option>)
            }
          </select>
          <ConfigHint>References each node's parent (builds the tree)</ConfigHint>
        </div>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Label column</div>
        <select
          aria-label="Label column"
          style={styles.select}
          value={config.labelColumn ?? ''}
          onChange={e => onChange({ ...config, labelColumn: e.target.value || undefined })}
        >
          <option value="">Use ID</option>
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
        <ConfigHint>Display text for nodes (uses ID if not set)</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Metrics <span style={{ fontWeight: 400, textTransform: 'none' }}>(Ctrl+click multi)</span></div>
        <div style={{ position: 'relative' }}>
          <select
            aria-label="Metrics"
            style={{ ...styles.select, minHeight: 56 }}
            multiple
            value={config.metricColumns ?? []}
            onChange={e => {
              const selected = Array.from(e.target.selectedOptions).map(o => o.value)
              onChange({ ...config, metricColumns: selected })
            }}
          >
            {columns.map(c => <option key={c} value={c}>{c}</option>)}
          </select>
          {(config.metricColumns ?? []).length > 0 && (
            <button
              type="button"
              onClick={() => onChange({ ...config, metricColumns: [] })}
              style={styles.clearButton}
              title="Clear all metrics"
            >
              ✕ Clear
            </button>
          )}
        </div>
        <ConfigHint>Numeric columns to display as node values</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Layout</div>
        <select
          aria-label="Layout direction"
          style={styles.select}
          value={config.layout ?? 'top-down'}
          onChange={e => onChange({ ...config, layout: e.target.value as 'top-down' | 'left-to-right' })}
        >
          <option value="top-down">Top-down</option>
          <option value="left-to-right">Left-to-right</option>
        </select>
        <ConfigHint>Tree orientation direction</ConfigHint>
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Horizontal spacing</div>
        <select
          aria-label="Horizontal spacing"
          style={styles.select}
          value={String(config.nodeSpacing ?? 50)}
          onChange={e => {
            const val = Number(e.target.value)
            const newConfig = { ...config, nodeSpacing: val }
            onChange(newConfig)
          }}
        >
          <option value="20">Tight</option>
          <option value="35">Compact</option>
          <option value="50">Normal</option>
          <option value="70">Wide</option>
          <option value="100">Very wide</option>
        </select>
        <ConfigHint>Distance between sibling nodes</ConfigHint>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  row: { display: 'flex', gap: 10 },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4, width: '100%', boxSizing: 'border-box' as const },
  clearButton: { position: 'absolute', top: 4, right: 4, fontSize: 10, padding: '2px 6px', background: 'var(--bg-hover)', color: 'var(--text-muted)', border: '1px solid var(--border)', borderRadius: 3, cursor: 'pointer', lineHeight: 1 },
  resetButton: { position: 'absolute', bottom: 8, right: 8, fontSize: 11, padding: '4px 10px', background: 'var(--bg-card)', color: 'var(--text-muted)', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', boxShadow: 'var(--shadow-sm)' },
}

export const HierarchyTreeModule: ChartModule = {
  Component: HierarchyTreeComponent,
  ConfigPanel: HierarchyTreeConfigPanel,
  defaultConfig: { chartType: 'hierarchy_tree', layout: 'top-down' },
  detectColumns: (columns, rows) => {
    const detected = detectParentChild(columns, rows)
    const firstTextCol = columns.find(c => {
      const t = (c.type ?? '').toLowerCase()
      return t.includes('text') || t.includes('varchar') || t.includes('char') || t.includes('name')
    })
    return {
      idColumn: detected?.idCol,
      parentIdColumn: detected?.parentCol,
      labelColumn: firstTextCol?.name,
    }
  },
  requirements: { minColumns: 3, needsParentChild: true },
}
