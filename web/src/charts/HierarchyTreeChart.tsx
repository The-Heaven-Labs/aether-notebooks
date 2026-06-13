import type React from 'react'
import type { ChartModule, ChartProps, ConfigPanelProps } from './types'
import { EChartsContainer, CHART_COLORS, tooltipStyle } from './common'

interface TreeNode {
  name: string
  children?: TreeNode[]
  itemStyle?: { color: string }
}

// Detect parent-child ID columns
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

// Build tree from flat rows
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
  const columns = data.columns.map(c => c.name)
  const idCol = config.idColumn ?? columns[0]
  const parentCol = config.parentIdColumn ?? columns[1]
  const labelCol = config.labelColumn ?? columns.find(c => !c.includes('id') && !c.includes('parent') && c !== idCol && c !== parentCol) ?? idCol
  const metricCols = config.metricColumns ?? []
  const isHorizontal = config.layout === 'left-to-right'

  const chartData = data.rows.map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const treeData = buildTree(chartData, idCol, parentCol, labelCol, metricCols, config.seriesColors ?? {})

  const option = {
    tooltip: { ...getTooltipStyle(), trigger: 'item' as const },
    series: [{
      type: 'tree' as const,
      data: treeData,
      orient: isHorizontal ? 'LR' as const : 'TB' as const,
      top: isHorizontal ? '5%' : '10%',
      bottom: isHorizontal ? '5%' : '25%',
      left: isHorizontal ? '15%' : '5%',
      right: isHorizontal ? '20%' : '5%',
      symbolSize: 10,
      label: {
        position: isHorizontal ? 'right' as const : 'top' as const,
        verticalAlign: 'middle' as const,
        fontSize: 11,
        color: 'var(--text-primary)',
        formatter: (params: { name: string }) => params.name,
      },
      leaves: {
        label: {
          position: isHorizontal ? 'right' as const : 'bottom' as const,
          verticalAlign: 'middle' as const,
        },
      },
      expandAndCollapse: true,
      animationDuration: 200,
      animationDurationUpdate: 200,
      lineStyle: { color: 'var(--border)', width: 1.5 },
      itemStyle: { borderColor: 'var(--bg-card)' },
    }],
  }

  const height = Math.max(300, treeData.length * 60)
  return <EChartsContainer option={option} height={height} />
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
      </div>
      <div style={styles.section}>
        <div style={styles.sectionLabel}>Metrics <span style={{ fontWeight: 400, textTransform: 'none' }}>(Ctrl+click multi)</span></div>
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
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { padding: '12px 16px', display: 'flex', flexDirection: 'column', gap: 10 },
  row: { display: 'flex', gap: 10 },
  section: { flex: 1, display: 'flex', flexDirection: 'column', gap: 4 },
  sectionLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', textTransform: 'uppercase' as const, letterSpacing: 0.5 },
  select: { fontSize: 12, padding: '4px 8px', background: 'var(--bg-input)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4 },
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
