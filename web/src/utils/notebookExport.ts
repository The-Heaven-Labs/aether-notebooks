import type { Cell, Notebook, ResultSet, ChartConfig } from '../types'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

const CHART_COLORS = ['#6366f1', '#06b6d4', '#10b981', '#f59e0b', '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16']

function escapeHtml(str: string): string {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#039;')
}



function normalizeChartConfig(raw: unknown): ChartConfig | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const obj = raw as Record<string, unknown>
  return {
    chartType: (obj.chartType ?? obj.type) as ChartConfig['chartType'] | undefined,
    xAxis: (obj.xAxis ?? obj.x_column) as string | undefined,
    yAxis: (obj.yAxis ?? obj.y_columns) as string[] | undefined,
    title: obj.title as string | undefined,
    showLegend: obj.showLegend as boolean | undefined,
    showGrid: obj.showGrid as boolean | undefined,
    showLabels: obj.showLabels as boolean | undefined,
    skipEmpty: obj.skipEmpty as boolean | undefined,
    seriesColors: obj.seriesColors as Record<string, string> | undefined,
    groupBy: obj.groupBy as string | undefined,
    labelColumn: obj.labelColumn as string | undefined,
    valueColumn: obj.valueColumn as string | undefined,
    decimalPlaces: obj.decimalPlaces as number | undefined,
    prefix: obj.prefix as string | undefined,
    suffix: obj.suffix as string | undefined,
    label: obj.label as string | undefined,
    roseType: obj.roseType as 'radius' | 'area' | undefined,
    startAngle: obj.startAngle as number | undefined,
    padAngle: obj.padAngle as number | undefined,
    smooth: obj.smooth as boolean | undefined,
    connectNulls: obj.connectNulls as boolean | undefined,
    dataZoom: obj.dataZoom as boolean | undefined,
    barWidth: obj.barWidth as string | undefined,
    barCategoryGap: obj.barCategoryGap as string | undefined,
    colorColumn: obj.colorColumn as string | undefined,
    sizeColumn: obj.sizeColumn as string | undefined,
    timeColumn: obj.timeColumn as string | undefined,
    endTimeColumn: obj.endTimeColumn as string | undefined,
    maxLabelLength: obj.maxLabelLength as number | undefined,
    showConnectors: obj.showConnectors as boolean | undefined,
    showTimeDeltas: obj.showTimeDeltas as boolean | undefined,
    idColumn: obj.idColumn as string | undefined,
    parentIdColumn: obj.parentIdColumn as string | undefined,
    metricColumns: obj.metricColumns as string[] | undefined,
    layout: obj.layout as 'top-down' | 'left-to-right' | undefined,
    nodeAlign: obj.nodeAlign as string | undefined,
    nodeWidth: obj.nodeWidth as number | undefined,
    nodeGap: obj.nodeGap as number | undefined,
  } as ChartConfig
}

function colIdx(columns: { name: string }[], name: string): number {
  const idx = columns.findIndex(c => c.name === name)
  return idx >= 0 ? idx : 0
}

function themeColors() {
  const dark = document.documentElement.getAttribute('data-theme') === 'dark'
  return {
    text: dark ? '#e8e8e8' : '#111',
    textMuted: dark ? '#888' : '#6e6e6e',
    border: dark ? '#2e2e2e' : '#e8e8e8',
    bgCard: dark ? '#1c1c1c' : '#ffffff',
    shadow: dark ? 'rgba(0,0,0,0.3)' : 'rgba(0,0,0,0.1)',
  }
}

function axisStyle() {
  const c = themeColors()
  return { axisLine: { show: false }, axisTick: { show: false }, axisLabel: { fontSize: 11, color: c.textMuted }, splitLine: { show: true, lineStyle: { color: c.border, type: 'dashed' as const } } }
}

function tooltipStyle() {
  const c = themeColors()
  return { backgroundColor: c.bgCard, borderColor: c.border, borderRadius: 4, textStyle: { fontSize: 12, color: c.text }, extraCssText: `box-shadow: 0 2px 16px ${c.shadow}` }
}

function buildAxisOption(data: ResultSet, config: ChartConfig): any {
  const c = themeColors()
  const isScatter = config.chartType === 'scatter'
  const isArea = config.chartType === 'area'
  const isStacked = config.chartType === 'stacked_bar' || config.chartType === 'stacked_area'
  const isBar = config.chartType === 'bar' || isStacked
  const xKey = config.xAxis ?? data.columns[0]?.name ?? ''
  const yKeys = config.yAxis?.length ? config.yAxis : data.columns.slice(1, 2).map(c => c.name)
  const gb = config.groupBy
  const xIdx = colIdx(data.columns, xKey)
  const gbIdx = gb ? colIdx(data.columns, gb) : -1

  let series: any[], xData: any[]

  if (gb && gbIdx >= 0) {
    const xMap: Record<string, Record<string, any[]>> = {}
    const groupOrder: string[] = []
    for (const r of data.rows) {
      const xVal = String(r[xIdx] ?? '')
      const gVal = String(r[gbIdx] ?? '')
      if (!xMap[xVal]) xMap[xVal] = {}
      if (!xMap[xVal][gVal]) xMap[xVal][gVal] = r
      if (!groupOrder.includes(gVal)) groupOrder.push(gVal)
    }
    xData = Object.keys(xMap)
    series = []
    for (let gi = 0; gi < groupOrder.length; gi++) {
      const g = groupOrder[gi]
      for (let yi = 0; yi < yKeys.length; yi++) {
        const yName = yKeys.length > 1 ? `${g} (${yKeys[yi]})` : g
        const sData = xData.map(xv => xMap[xv]?.[g]?.[colIdx(data.columns, yKeys[yi])] ?? null)
        const s: any = {
          name: yName,
          type: isArea ? 'line' : isScatter ? 'scatter' : isBar ? 'bar' : 'line',
          data: isScatter ? sData.map((v, vi) => [xData[vi], v]) : sData,
          itemStyle: { color: CHART_COLORS[(gi * yKeys.length + yi) % CHART_COLORS.length] },
        }
        if (isScatter) { s.itemStyle = { ...s.itemStyle, opacity: 0.8 } }
        else {
          s.symbol = 'circle'; s.symbolSize = 4; s.lineStyle = { width: 2 }
          s.smooth = config.smooth ?? false; s.connectNulls = config.connectNulls ?? false
          if (isArea) s.areaStyle = { opacity: 0.15 }
          if (isStacked) s.stack = 'a'
          if (config.showLabels) s.label = { show: true, position: 'top', fontSize: 10, color: c.textMuted }
        }
        series.push(s)
      }
    }
  } else {
    xData = data.rows.map(r => r[xIdx])
    series = yKeys.map((y, i) => {
      const s: any = {
        name: y,
        type: isArea ? 'line' : isScatter ? 'scatter' : isBar ? 'bar' : 'line',
        data: isScatter ? data.rows.map(r => [r[xIdx], r[colIdx(data.columns, y)]]) : data.rows.map(r => r[colIdx(data.columns, y)]),
        itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length] },
      }
      if (isScatter) { s.itemStyle = { ...s.itemStyle, opacity: 0.8 } }
      else {
        s.symbol = 'circle'; s.symbolSize = 4; s.lineStyle = { width: 2 }
        s.smooth = config.smooth ?? false; s.connectNulls = config.connectNulls ?? false
        if (isArea) s.areaStyle = { opacity: 0.15 }
        if (isStacked) s.stack = 'a'
        if (isBar) s.itemStyle = { ...s.itemStyle, borderRadius: [3, 3, 0, 0] }
        if (config.showLabels) s.label = { show: true, position: 'top', fontSize: 10, color: c.textMuted }
      }
      return s
    })
  }

  const showLegend = config.showLegend !== false && !(isScatter && !config.groupBy && yKeys.length <= 1)
  const as = axisStyle()

  return {
    tooltip: { trigger: 'axis' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
    title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
    legend: showLegend ? { show: true, top: config.title ? 32 : 0, textStyle: { fontSize: 11, color: c.textMuted } } : { show: false },
    grid: { top: config.title ? 56 : showLegend ? 30 : 8, right: 16, bottom: isScatter || config.dataZoom ? 32 : 8, left: 16, containLabel: true },
    dataZoom: isScatter || config.dataZoom ? [{ type: 'inside' as const, start: 0, end: 100 }, { type: 'slider' as const, backgroundColor: c.bgCard, borderColor: c.border, start: 0, end: 100, bottom: 8, textStyle: { fontSize: 10, color: c.textMuted } }] : undefined,
    xAxis: isScatter ? { type: 'value' as const, ...as } : { type: 'category' as const, data: xData, ...(isArea ? { boundaryGap: false } : {}), ...as },
    yAxis: { type: 'value' as const, ...as },
    series,
  }
}

function buildPieOption(data: ResultSet, config: ChartConfig): any {
  const c = themeColors()
  const nameKey = config.labelColumn || config.xAxis || data.columns[0]?.name || ''
  const valueKey = config.yAxis?.[0] || data.columns[1]?.name || ''
  const isDonut = config.chartType === 'donut'
  return {
    tooltip: { trigger: 'item' as const, formatter: '{b}: {c} ({d}%)', backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
    title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
    legend: config.showLegend !== false ? { show: true, orient: 'vertical' as const, right: 10, top: config.title ? 36 : 'center', textStyle: { fontSize: 11, color: c.textMuted } } : { show: false },
    series: [{
      type: 'pie' as const, radius: isDonut ? ['40%', '70%'] : ['0%', '70%'],
      center: config.showLegend !== false ? ['40%', config.title ? '58%' : '50%'] : ['50%', '50%'],
      data: data.rows.map((r, i) => ({ name: r[colIdx(data.columns, nameKey)], value: r[colIdx(data.columns, valueKey)], itemStyle: { color: config.seriesColors?.[String(r[colIdx(data.columns, nameKey)])] ?? CHART_COLORS[i % CHART_COLORS.length] } })),
      label: config.showLabels !== false ? { fontSize: 11, color: c.text } : { show: false },
      emphasis: { itemStyle: { shadowBlur: 10, shadowColor: c.shadow } },
      roseType: config.roseType || false, startAngle: config.startAngle ?? 90, padAngle: config.padAngle ?? 0,
    }],
  }
}

function buildSankeyOption(data: ResultSet, config: ChartConfig): any {
  const c = themeColors()
  const sourceCol = config.xAxis ?? data.columns[0]?.name ?? ''
  const targetCol = config.yAxis?.[0] ?? data.columns[1]?.name ?? ''
  const valueCol = config.yAxis?.[1] ?? data.columns[2]?.name ?? ''
  const nodeSet = new Set<string>()
  const links: Array<{ source: string; target: string; value: number }> = []
  for (const row of data.rows) {
    const src = String(row[colIdx(data.columns, sourceCol)] ?? '')
    const tgt = String(row[colIdx(data.columns, targetCol)] ?? '')
    const val = Number(row[colIdx(data.columns, valueCol)] ?? 1)
    if (src && tgt && !isNaN(val)) { nodeSet.add(src); nodeSet.add(tgt); links.push({ source: src, target: tgt, value: val }) }
  }
  const nodes = Array.from(nodeSet).map((name, i) => ({ name, itemStyle: { color: config.seriesColors?.[name] ?? CHART_COLORS[i % CHART_COLORS.length] } }))
  return {
    tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
    title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
    series: [{ type: 'sankey' as const, layoutIterations: 32, nodeAlign: config.nodeAlign ?? 'justify', nodeWidth: config.nodeWidth ?? 20, nodeGap: config.nodeGap ?? 12, roam: true, data: nodes, links, lineStyle: { color: 'gradient' as const, curveness: 0.5, opacity: 0.4 }, label: { fontSize: 11, color: c.text }, emphasis: { focus: 'adjacency' as const } }],
  }
}

function buildMapOption(data: ResultSet, config: ChartConfig): any {
  const c = themeColors()
  const as = axisStyle()
  const latCol = config.yAxis?.[0] ?? data.columns.find(c => /^lat/i.test(c.name))?.name ?? ''
  const lonCol = config.xAxis ?? data.columns.find(c => /^lon/i.test(c.name) || /^lng/i.test(c.name))?.name ?? ''
  const labelCol = config.labelColumn ?? ''
  const lti = colIdx(data.columns, latCol)
  const lni = colIdx(data.columns, lonCol)
  const lbi = labelCol ? colIdx(data.columns, labelCol) : -1
  const pts = data.rows.map(r => ({ lon: Number(r[lni]), lat: Number(r[lti]), name: lbi >= 0 ? String(r[lbi] ?? '') : '' })).filter(d => !isNaN(d.lon) && !isNaN(d.lat))
  if (pts.length === 0) return {}
  return {
    tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
    title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
    grid: { top: config.title ? 46 : 20, right: 16, bottom: 8, left: 16, containLabel: true },
    xAxis: { type: 'value' as const, name: lonCol || 'Longitude', min: -180, max: 180, ...as },
    yAxis: { type: 'value' as const, name: latCol || 'Latitude', min: -90, max: 90, ...as },
    series: [{ type: 'scatter' as const, data: pts.map(p => [p.lon, p.lat]), symbolSize: 10, itemStyle: { color: CHART_COLORS[0], opacity: 0.85 } }],
  }
}

function buildTreeOption(data: ResultSet, config: ChartConfig): any {
  const c = themeColors()
  const columns = data.columns.map(c => c.name)
  const idCol = config.idColumn ?? columns[0]
  const parentCol = config.parentIdColumn ?? columns[1]
  const labelCol = config.labelColumn ?? columns.find(c => !c.toLowerCase().includes('id') && !c.includes('parent') && c !== idCol && c !== parentCol) ?? idCol
  const metricCols = config.metricColumns ?? []
  const isHorizontal = config.layout === 'left-to-right'
  const nodeMap = new Map<string, any>()
  const roots: any[] = []
  data.rows.forEach((row, i) => {
    const id = String(row[colIdx(data.columns, idCol)] ?? `node-${i}`)
    const label = labelCol ? String(row[colIdx(data.columns, labelCol)] ?? id) : id
    const node: any = { name: label, parentId: String(row[colIdx(data.columns, parentCol)] ?? ''), itemStyle: { color: config.seriesColors?.[label] ?? CHART_COLORS[i % CHART_COLORS.length] } }
    if (metricCols.length > 0) node.name = `${label}\n${metricCols.map(mc => `${mc}: ${row[colIdx(data.columns, mc)]}`).join(', ')}`
    nodeMap.set(id, node)
  })
  nodeMap.forEach((node, id) => {
    if (node.parentId && node.parentId !== id && nodeMap.has(node.parentId)) {
      const parent = nodeMap.get(node.parentId)
      if (!parent.children) parent.children = []
      parent.children.push(node)
    } else { roots.push(node) }
  })
  return {
    tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
    title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
    series: [{ type: 'tree' as const, data: roots, orient: isHorizontal ? 'LR' as const : 'TB' as const, top: config.title ? '16%' : '8%', bottom: metricCols.length > 0 ? '30%' : '12%', left: `${isHorizontal ? 15 : 8}%`, right: `${isHorizontal ? 20 : 8}%`, roam: true, initialTreeDepth: -1, symbolSize: 12, edgeShape: 'curve' as const, label: { position: isHorizontal ? 'right' as const : 'top' as const, verticalAlign: 'middle' as const, fontSize: 11, color: c.text }, lineStyle: { color: c.border, width: 1.5, curveness: 0.5 }, itemStyle: { borderColor: c.bgCard }, expandAndCollapse: true }],
  }
}

function buildTimelineOption(data: ResultSet, config: ChartConfig): any {
  const c = themeColors()
  const columns = data.columns.map(c => c.name)
  const timeCol = config.timeColumn ?? columns[0]
  const labelCol = config.labelColumn
  const groupByCol = config.groupBy
  const chartData = data.rows.map(r => {
    const o: Record<string, unknown> = {}; data.columns.forEach((c, i) => { o[c.name] = r[i] }); return o
  }).filter(d => d[timeCol] != null).sort((a, b) => new Date(String(a[timeCol])).getTime() - new Date(String(b[timeCol])).getTime())

  const groups = groupByCol ? [...new Set(chartData.map(d => String(d[groupByCol] ?? 'Unknown')))] : ['Events']
  const singleGroup = groups.length === 1
  const isRange = !!config.endTimeColumn

  if (isRange) {
    const etCol = config.endTimeColumn!
    return {
      tooltip: { trigger: 'axis' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
      legend: groups.length > 1 ? { top: config.title ? 30 : 0, textStyle: { fontSize: 11, color: c.textMuted } } : undefined,
      grid: { top: groups.length > 1 ? 56 : 40, right: 16, bottom: 16, left: 16, containLabel: true },
      xAxis: { type: 'time' as const, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { fontSize: 11, color: c.textMuted } },
      yAxis: singleGroup ? { type: 'value' as const, show: false, min: 0, max: 1 } : { type: 'category' as const, data: groups, inverse: true, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { fontSize: 11, color: c.textMuted }, splitLine: { show: config.showGrid !== false, lineStyle: { color: c.border, type: 'dashed' as const } } },
      series: groups.map((group, gi) => ({
        name: group, type: 'scatter' as const, symbolSize: 14, itemStyle: { color: config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length] },
        data: chartData.filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true).map(d => {
          const st = new Date(String(d[timeCol])).getTime()
          const et = new Date(String(d[etCol])).getTime()
          return [st, singleGroup ? 0.2 : gi, et]
        }),
        encode: { x: [0, 2], y: 1 },
      })),
    }
  }

  return {
    tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
    title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
    legend: groups.length > 1 ? { top: config.title ? 30 : 0, textStyle: { fontSize: 11, color: c.textMuted } } : undefined,
    grid: { top: 56, right: 16, bottom: 60, left: 16 },
    xAxis: { type: 'time' as const, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { fontSize: 11, color: c.textMuted } },
    yAxis: singleGroup ? { type: 'value' as const, show: false, min: 0, max: 1 } : { type: 'category' as const, data: groups, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { fontSize: 11, color: c.textMuted }, splitLine: { show: config.showGrid !== false, lineStyle: { color: c.border, type: 'dashed' as const } } },
    series: groups.flatMap((group, gi) => {
      const groupData = chartData.filter(d => groupByCol ? String(d[groupByCol] ?? 'Unknown') === group : true)
      const cl = config.seriesColors?.[group] ?? CHART_COLORS[gi % CHART_COLORS.length]
      return [
        { name: group, type: 'scatter' as const, symbolSize: 14, itemStyle: { color: cl, borderColor: c.text, borderWidth: 1 }, data: groupData.filter((_, i) => i % 2 === 0).map(d => [new Date(String(d[timeCol])).getTime(), singleGroup ? 0.2 : group, labelCol ? d[labelCol] : null]) },
        { name: `${group}_bottom`, type: 'scatter' as const, symbolSize: 14, itemStyle: { color: cl, borderColor: c.text, borderWidth: 1 }, data: groupData.filter((_, i) => i % 2 === 1).map(d => [new Date(String(d[timeCol])).getTime(), singleGroup ? 0.2 : group, labelCol ? d[labelCol] : null]) },
      ]
    }),
  }
}

function buildChartOption(data: ResultSet, config: ChartConfig): any {
  const t = config.chartType
  if (['bar', 'stacked_bar', 'line', 'area', 'scatter'].includes(t)) return buildAxisOption(data, config)
  if (['pie', 'donut'].includes(t)) return buildPieOption(data, config)
  if (t === 'sankey') return buildSankeyOption(data, config)
  if (t === 'map') return buildMapOption(data, config)
  if (t === 'hierarchy_tree') return buildTreeOption(data, config)
  if (t === 'timeline') return buildTimelineOption(data, config)
  return {}
}

function renderTable(rs: ResultSet): string {
  if (!rs?.columns?.length || !rs?.rows?.length) return ''
  const headers = rs.columns.map(c => `<th>${escapeHtml(c.name)}</th>`).join('')
  const rows = rs.rows.map(r => `<tr>${r.map(v => v == null ? '<td><span class="null">NULL</span></td>' : `<td>${escapeHtml(String(v))}</td>`).join('')}</tr>`).join('')
  return `<div class="output-bar">${rs.rows.length.toLocaleString()} rows</div><div class="table-wrap"><table class="output-table"><thead><tr>${headers}</tr></thead><tbody>${rows}</tbody></table></div>`
}

function formatNumber(value: unknown, decimalPlaces?: number): string {
  if (typeof value === 'number') return value.toLocaleString(undefined, { minimumFractionDigits: decimalPlaces ?? 0, maximumFractionDigits: decimalPlaces ?? 2 })
  if (value == null) return '<span class="null">—</span>'
  return String(value)
}

function renderOutputs(cell: Cell): { html: string; height: number } {
  if (cell.outputs_hidden) return { html: '', height: 0 }

  const tableOut = cell.outputs?.find(o => o.type === 'table')
  const errorOut = cell.outputs?.find(o => o.type === 'error')
  const textOut = cell.outputs?.find(o => o.type === 'text')

  if (errorOut) {
    const msg = typeof errorOut.data === 'string' ? errorOut.data : JSON.stringify(errorOut.data)
    return { html: `<div class="cell-output error"><div class="error-label">Error</div><pre>${escapeHtml(msg)}</pre></div>`, height: 0 }
  }
  if (textOut) {
    return { html: `<div class="cell-output text"><pre>${escapeHtml(typeof textOut.data === 'string' ? textOut.data : JSON.stringify(textOut.data))}</pre></div>`, height: 0 }
  }

  const chartConfig = normalizeChartConfig(cell.metadata?.chart)
  const viewMode = cell.metadata?.viewMode as string | undefined
  const hasChartConfig = !!(chartConfig?.chartType)
  const rs = tableOut?.data as ResultSet | undefined

  if (!rs) return { html: '', height: 0 }

  if (hasChartConfig && viewMode !== 'table') {
    if (chartConfig!.chartType === 'big_number') {
      const col = chartConfig!.valueColumn ?? rs.columns[0]?.name ?? ''
      const colIdx = rs.columns.findIndex(c => c.name === col)
      const value = colIdx >= 0 && rs.rows[0] ? rs.rows[0][colIdx] : null
      return { html: `<div class="cell-output big-number"><div class="big-number-body">${chartConfig!.label ? `<div class="big-number-label">${escapeHtml(chartConfig!.label)}</div>` : ''}<div class="big-number-value">${chartConfig!.prefix ? `<span class="big-number-prefix">${escapeHtml(chartConfig!.prefix)}</span>` : ''}${formatNumber(value, chartConfig!.decimalPlaces)}${chartConfig!.suffix ? `<span class="big-number-suffix">${escapeHtml(chartConfig!.suffix)}</span>` : ''}</div></div></div>`, height: 0 }
    }
    try {
      const option = buildChartOption(rs, chartConfig!)
      if (option && Object.keys(option).length > 0) {
        const optId = `chart-${cell.id}`
        const optJson = JSON.stringify(option)
        return { html: `<div class="cell-output chart"><div class="chart-wrap" id="${optId}" style="width:100%;height:300px;min-height:300px"></div><script type="application/json" data-chart-for="${optId}">${optJson}<\/script></div>`, height: 300 }
      }
    } catch {}
  }

  return { html: `<div class="cell-output">${renderTable(rs)}</div>`, height: 0 }
}

function renderCell(cell: Cell, index: number): string {
  const isCode = cell.type === 'code'
  const accent = isCode ? 'var(--accent)' : 'var(--success)'

  if (cell.cell_collapsed) {
    return `<div class="cell collapsed" style="border-left:3px solid ${accent}">
      <div class="cell-meta">
        <span class="cell-num">${index + 1}</span>
        <span class="cell-tag ${isCode ? 'tag-code' : 'tag-md'}">${isCode ? 'SQL' : 'MD'}</span>
        ${cell.title ? `<span class="cell-title">${escapeHtml(cell.title)}</span>` : ''}
      </div>
    </div>`
  }

  let body = ''
  if (isCode && cell.source_visible !== false && cell.source) {
    body += `<div class="cell-source"><pre><code>${escapeHtml(cell.source)}</code></pre></div>`
  }
  if (!isCode && cell.source) {
    body += `<div class="markdown-body"><script type="text/plain">${cell.source.replace(/<\/script>/gi, '<\\/script>')}<\/script></div>`
  }
  const { html } = renderOutputs(cell)
  body += html

  return `<div class="cell" style="border-left:3px solid ${accent}">
    <div class="cell-meta">
      <span class="cell-num">${index + 1}</span>
      <span class="cell-tag ${isCode ? 'tag-code' : 'tag-md'}">${isCode ? 'SQL' : 'MD'}</span>
      ${cell.title ? `<span class="cell-title">${escapeHtml(cell.title)}</span>` : ''}
    </div>
    ${body}
  </div>`
}

const INLINED_CSS = `
:root, [data-theme="light"] {
  --bg-primary: #f5f5f5; --bg-card: #ffffff; --bg-cell-code: #f7f7f7;
  --border: #e8e8e8; --border-light: #efefef;
  --text-primary: #111; --text-secondary: #555; --text-muted: #6e6e6e;
  --accent: #7c6faa; --accent-hover: #6a5e96; --success: #2e7d32;
  --font-sans: 'DM Sans', -apple-system, BlinkMacSystemFont, sans-serif;
  --font-mono: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
  --selected: #e8e4f5; --null-color: #aaa;
}
[data-theme="dark"] {
  --bg-primary: #141414; --bg-card: #1c1c1c; --bg-cell-code: #1e1e1e;
  --border: #2e2e2e; --border-light: #242424;
  --text-primary: #e8e8e8; --text-secondary: #aaa; --text-muted: #888;
  --accent: #9b8fc4; --accent-hover: #b8a9d8; --success: #4caf74;
  --selected: #2a2740; --null-color: #666;
}
* { margin:0;padding:0;box-sizing:border-box; }
body { font-family:var(--font-sans);background:var(--bg-primary);color:var(--text-primary);font-size:14px;line-height:1.5; }
.notebook { max-width:960px;margin:0 auto;padding:24px 16px; }
.header { margin-bottom:24px; }
.title { font-size:28px;font-weight:700;margin:0 0 6px; }
.description { font-size:14px;color:var(--text-secondary);margin:8px 0; }
.cells { display:flex;flex-direction:column;gap:8px; }
.cell { background:var(--bg-card);border-radius:4px;border:1px solid var(--border);overflow:hidden; }
.cell.collapsed { padding:8px 12px; }
.cell-meta { display:flex;align-items:center;gap:8px;padding:6px 12px;border-bottom:1px solid var(--border-light);background:var(--bg-primary);font-size:12px; }
.cell.collapsed .cell-meta { border-bottom:none;background:transparent;padding:0; }
.cell-num { color:var(--text-muted);font-size:11px;min-width:16px;font-weight:500; }
.cell-tag { font-size:10px;font-weight:600;padding:1px 6px;border-radius:3px;text-transform:uppercase;letter-spacing:0.3px; }
.tag-code { background:var(--accent);color:#fff; }
.tag-md { background:var(--success);color:#fff; }
.cell-title { color:var(--text-secondary);font-size:12px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap; }
.cell-source { background:var(--bg-cell-code);padding:12px 16px;border-bottom:1px solid var(--border-light);overflow-x:auto; }
.cell-source pre { font-family:var(--font-mono);font-size:13px;line-height:1.5;white-space:pre; }
.cell-source code { font-family:var(--font-mono); }
.output-bar { font-size:11px;color:var(--text-muted);padding:6px 12px;background:var(--bg-primary);border-bottom:1px solid var(--border-light); }
.table-wrap { overflow-x:auto; }
.output-table { width:100%;border-collapse:collapse;font-size:13px; }
.output-table th { background:var(--bg-primary);font-weight:600;text-align:left;padding:6px 10px;border-bottom:2px solid var(--border);color:var(--text-secondary);font-size:11px;white-space:nowrap; }
.output-table td { padding:5px 10px;border-bottom:1px solid var(--border-light);white-space:nowrap; }
.output-table tbody tr:hover { background:var(--selected); }
.cell-output.error { padding:12px 16px; }
.error-label { font-size:11px;font-weight:600;color:var(--error);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:6px; }
.cell-output.error pre { font-family:var(--font-mono);font-size:12px;white-space:pre-wrap; }
.cell-output.text { padding:12px 16px; }
.cell-output.text pre { font-family:var(--font-mono);font-size:12px;color:var(--text-secondary);white-space:pre-wrap; }
.null { color:var(--null-color);font-style:italic; }
.markdown-body { padding:12px 16px; }
.markdown-body h1,.markdown-body h2,.markdown-body h3,.markdown-body h4 { margin:12px 0 6px;color:var(--text-primary); }
.markdown-body h1{font-size:22px}.markdown-body h2{font-size:18px}.markdown-body h3{font-size:16px}
.markdown-body p{margin:6px 0}.markdown-body ul,.markdown-body ol{margin:6px 0;padding-left:20px}
.markdown-body code{font-family:var(--font-mono);background:var(--bg-cell-code);padding:1px 4px;border-radius:3px;font-size:13px}
.markdown-body pre code{display:block;padding:8px 12px;overflow-x:auto}
.markdown-body blockquote{border-left:3px solid var(--accent);padding-left:12px;color:var(--text-secondary);margin:6px 0}
.markdown-body table{border-collapse:collapse;margin:8px 0;font-size:13px}
.markdown-body th,.markdown-body td{border:1px solid var(--border);padding:4px 8px;text-align:left}
.markdown-body th{background:var(--bg-primary);font-weight:600}
.markdown-body img{max-width:100%}
.chart-wrap{width:100%;height:300px;min-height:300px}
.big-number-body{display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:200px;padding:24px;gap:8px}
.big-number-label{font-size:13px;font-weight:600;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;text-align:center}
.big-number-value{font-size:56px;font-weight:700;color:var(--text-primary);line-height:1.1;letter-spacing:-1.5px;text-align:center;word-break:break-word}
.big-number-prefix{font-size:28px;font-weight:400;color:var(--text-muted);margin-right:4px}
.big-number-suffix{font-size:28px;font-weight:400;color:var(--text-muted);margin-left:4px}
`

const CHART_INIT_SCRIPT = `
(function(){
if (typeof echarts === 'undefined') return;
document.querySelectorAll('script[data-chart-for]').forEach(function(script) {
  try {
    var id = script.getAttribute('data-chart-for');
    var el = document.getElementById(id);
    if (!el) return;
    var opt = JSON.parse(script.textContent || '');
    var chart = echarts.init(el);
    chart.setOption(opt, { notMerge: true });
  } catch(e) { console.error('Chart:', e); }
});
})();
`

function generateHTML(notebook: NotebookWithCells): string {
  const cellsHtml = notebook.cells.map((cell, i) => renderCell(cell, i)).join('\n')
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark'

  return `<!DOCTYPE html>
<html data-theme="${isDark ? 'dark' : 'light'}">
<head>
<meta charset="utf-8">
<title>${escapeHtml(notebook.title)}</title>
<style>${INLINED_CSS}<\/style>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700&family=JetBrains+Mono&display=swap" rel="stylesheet">
</head>
<body>
<div class="notebook">
<div class="header">
<h1 class="title">${escapeHtml(notebook.title)}</h1>
${notebook.description ? `<p class="description">${escapeHtml(notebook.description)}</p>` : ''}
</div>
<div class="cells">
${cellsHtml}
</div>
</div>
<script src="https://cdn.jsdelivr.net/npm/echarts@5/dist/echarts.min.js"><\/script>
<script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"><\/script>
<script>${CHART_INIT_SCRIPT}<\/script>
<script>
document.querySelectorAll('.markdown-body script[type="text/plain"]').forEach(function(el) {
  try { el.parentElement.innerHTML = marked.parse(el.textContent || '') } catch(e) { console.error(e) }
})
<\/script>
</body>
</html>`
}

export function exportNotebookHTML(notebook: NotebookWithCells): void {
  const html = generateHTML(notebook)
  const blob = new Blob([html], { type: 'text/html;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${notebook.title.replace(/[^a-zA-Z0-9_-]/g, '_')}.html`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  setTimeout(() => URL.revokeObjectURL(url), 5000)
}
