import type { Cell, Notebook, ResultSet } from '../types'
import type { ChartConfig } from '../charts/types'

interface NotebookWithCells extends Notebook {
  cells: Cell[]
}

const CHART_COLORS = ['#6366f1', '#06b6d4', '#10b981', '#f59e0b', '#f43f5e', '#8b5cf6', '#0ea5e9', '#84cc16']

function escapeHtml(str: string): string {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
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
    markLines: obj.markLines as ChartConfig['markLines'],
  } as ChartConfig
}

function colIdx(columns: { name: string }[], name: string): number {
  const idx = columns.findIndex(c => c.name === name)
  return idx >= 0 ? idx : 0
}

const NUMERIC_TYPES = new Set([
  'int', 'int2', 'int4', 'int8', 'bigint', 'smallint', 'serial', 'bigserial',
  'float', 'float4', 'float8', 'double', 'decimal', 'numeric', 'real',
  'int16', 'int32', 'int64', 'int128', 'int256',
  'uint8', 'uint16', 'uint32', 'uint64', 'uint128', 'uint256',
  'float32', 'float64',
])

function isNumericType(type?: string): boolean {
  if (!type) return false
  const base = type.toLowerCase().replace(/\(.*\)/, '').trim()
  if (NUMERIC_TYPES.has(base)) return true
  for (const wrapper of ['nullable', 'lowcardinality']) {
    if (type.toLowerCase().startsWith(wrapper + '(') && type.endsWith(')')) {
      const inner = type.slice(wrapper.length + 1, -1).trim().replace(/\(.*\)/, '').trim()
      if (NUMERIC_TYPES.has(inner)) return true
    }
  }
  return false
}

function themeColors() {
  const dark = document.documentElement.getAttribute('data-theme') === 'dark'
  return {
    text: dark ? '#e8e8e8' : '#111',
    textMuted: dark ? '#888' : '#6e6e6e',
    border: dark ? '#2e2e2e' : '#e8e8e8',
    bgCard: dark ? '#1c1c1c' : '#ffffff',
  }
}

function axisSt(showGrid?: boolean, c?: ReturnType<typeof themeColors>) {
  const col = c ?? themeColors()
  return { axisLine: { show: false }, axisTick: { show: false }, axisLabel: { fontSize: 11, color: col.textMuted }, splitLine: { show: showGrid !== false, lineStyle: { color: col.border, type: 'dashed' as const } } }
}

function buildOption(data: ResultSet, config: ChartConfig): any {
  const c = themeColors()
  const t = config.chartType
  const cols = data.columns
  const rows = data.rows

  // Axis-based: bar, line, area, scatter
  if (['bar', 'line', 'area', 'scatter'].includes(t)) {
    const isSc = t === 'scatter'
    const isAr = t === 'area'
    const isSt = (t === 'bar' && config.barMode === 'stacked') || (t === 'area' && config.areaMode === 'stacked')
    const isBa = t === 'bar' || isSt
    const xKey = config.xAxis ?? cols[0]?.name ?? ''
    const yKeys = config.yAxis?.length ? config.yAxis : cols.filter(c => isNumericType(c.type)).map(c => c.name)
    const gb = config.groupBy
    const xI = colIdx(cols, xKey)
    const gbI = gb ? colIdx(cols, gb) : -1

    let series: any[], xData: any[]
    if (gb && gbI >= 0) {
      const xMap: Record<string, Record<string, any[]>> = {}
      const gOrder: string[] = []
      for (const r of rows) {
        const xV = String(r[xI] ?? ''); const gV = String(r[gbI] ?? '')
        if (!xMap[xV]) xMap[xV] = {}
        if (!xMap[xV][gV]) xMap[xV][gV] = r
        if (!gOrder.includes(gV)) gOrder.push(gV)
      }
      xData = Object.keys(xMap)
      series = []
      for (let gi = 0; gi < gOrder.length; gi++) {
        const g = gOrder[gi]
        for (let yi = 0; yi < yKeys.length; yi++) {
          const yN = yKeys.length > 1 ? `${g} (${yKeys[yi]})` : g
          const si: any = {
            name: yN,
            type: isAr ? 'line' : isSc ? 'scatter' : isBa ? 'bar' : 'line',
            data: isSc ? xData.map(xv => [xv, xMap[xv]?.[g]?.[colIdx(cols, yKeys[yi])] ?? null]) : xData.map(xv => xMap[xv]?.[g]?.[colIdx(cols, yKeys[yi])] ?? null),
            itemStyle: { color: CHART_COLORS[(gi * yKeys.length + yi) % CHART_COLORS.length] },
          }
          if (!isSc) {
            si.symbol = 'circle'; si.symbolSize = 4; si.lineStyle = { width: 2 }
            si.smooth = config.smooth ?? false; si.connectNulls = config.connectNulls ?? false
            if (isAr) si.areaStyle = { opacity: 0.15 }
            if (isSt) si.stack = 'a'
            if (config.showLabels) si.label = { show: true, position: 'top', fontSize: 10, color: c.textMuted }
          } else { si.itemStyle.opacity = 0.8 }
          series.push(si)
        }
      }
    } else {
      xData = rows.map(r => r[xI])
      series = yKeys.map((y: string, i: number) => {
        const si: any = {
          name: y,
          type: isAr ? 'line' : isSc ? 'scatter' : isBa ? 'bar' : 'line',
          data: isSc ? rows.map(r => [r[xI], r[colIdx(cols, y)]]) : rows.map(r => r[colIdx(cols, y)]),
          itemStyle: { color: config.seriesColors?.[y] ?? CHART_COLORS[i % CHART_COLORS.length] },
        }
        if (!isSc) {
          si.symbol = 'circle'; si.symbolSize = 4; si.lineStyle = { width: 2 }
          si.smooth = config.smooth ?? false; si.connectNulls = config.connectNulls ?? false
          if (isAr) si.areaStyle = { opacity: 0.15 }
          if (isSt) si.stack = 'a'
          if (isBa) si.itemStyle = { ...si.itemStyle, borderRadius: [3, 3, 0, 0] }
          if (config.showLabels) si.label = { show: true, position: 'top', fontSize: 10, color: c.textMuted }
        } else { si.itemStyle.opacity = 0.8 }
        return si
      })
    }

    // Reference lines as ghost series
    if (config.markLines?.length) {
      const allY = series.flatMap((s: any) => (s.data ?? []).filter((v: any) => v != null && isFinite(Number(v)))).map(Number)
      const yMin = Math.min(0, ...allY)
      const yMax = Math.max(0, ...allY)
      const xFirst = xData[0]
      const xLast = xData[xData.length - 1]
      for (const ml of config.markLines) {
        series.push({
          tooltip: { show: false },
          type: 'line',
          data: ml.position === 'horizontal'
            ? [[xFirst, parseFloat(ml.value) || 0], [xLast, parseFloat(ml.value) || 0]]
            : [[ml.value, yMin], [ml.value, yMax]],
          symbol: 'none',
          lineStyle: { type: 'dashed', color: ml.color || '#f43f5e', width: 1.5 },
          silent: true,
          z: 10,
        })
      }
    }

    const showLegend = config.showLegend !== false && !(isSc && !config.groupBy && yKeys.length <= 1)
    const as = axisSt(config.showGrid, c)
    return {
      tooltip: { trigger: 'axis' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
      legend: showLegend ? { show: true, top: config.title ? 32 : 0, textStyle: { fontSize: 11, color: c.textMuted } } : { show: false },
      grid: { top: config.title ? 56 : showLegend ? 30 : 8, right: 16, bottom: isSc || config.dataZoom ? 32 : 8, left: 16, containLabel: true },
      dataZoom: isSc || config.dataZoom ? [{ type: 'inside' as const, start: 0, end: 100 }, { type: 'slider' as const, start: 0, end: 100, bottom: 8, textStyle: { fontSize: 10, color: c.textMuted } }] : undefined,
      xAxis: isSc ? { type: 'value' as const, ...as } : { type: 'category' as const, data: xData, ...(isAr ? { boundaryGap: false } : {}), ...as },
      yAxis: { type: 'value' as const, ...as },
      series,
    }
  }

  // Pie / Donut
  if (t === 'pie' || t === 'donut') {
    const nk = config.labelColumn || config.xAxis || cols[0]?.name || ''
    const vk = config.yAxis?.[0] || cols[1]?.name || ''
    const dn = t === 'donut'
    return {
      tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text }, formatter: '{b}: {c} ({d}%)' },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
      legend: config.showLegend !== false ? { show: true, orient: 'vertical' as const, right: 10, top: config.title ? 36 : 'center', textStyle: { fontSize: 11, color: c.textMuted } } : { show: false },
      series: [{
        type: 'pie' as const, radius: dn ? ['40%', '70%'] : ['0%', '70%'],
        center: config.showLegend !== false ? ['40%', config.title ? '58%' : '50%'] : ['50%', '50%'],
        data: rows.map((r, i) => ({ name: r[colIdx(cols, nk)], value: r[colIdx(cols, vk)], itemStyle: { color: config.seriesColors?.[String(r[colIdx(cols, nk)])] ?? CHART_COLORS[i % CHART_COLORS.length] } })),
        label: config.showLabels !== false ? { fontSize: 11, color: c.text } : { show: false },
        emphasis: { itemStyle: { shadowBlur: 10, shadowColor: 'rgba(0,0,0,0.2)' } },
        roseType: config.roseType || false, startAngle: config.startAngle ?? 90, padAngle: config.padAngle ?? 0,
      }],
    }
  }

  // Sankey
  if (t === 'sankey') {
    const sc = config.xAxis ?? cols[0]?.name ?? ''; const tc = config.yAxis?.[0] ?? cols[1]?.name ?? ''; const vc = config.yAxis?.[1] ?? cols[2]?.name ?? ''
    const ns = new Set<string>(); const lks: Array<{ source: string; target: string; value: number }> = []
    for (const r of rows) {
      const s = String(r[colIdx(cols, sc)] ?? ''); const tg = String(r[colIdx(cols, tc)] ?? ''); const v = Number(r[colIdx(cols, vc)] ?? 1)
      if (s && tg && !isNaN(v)) { ns.add(s); ns.add(tg); lks.push({ source: s, target: tg, value: v }) }
    }
    const nds = Array.from(ns).map((n, i) => ({ name: n, itemStyle: { color: config.seriesColors?.[n] ?? CHART_COLORS[i % CHART_COLORS.length] } }))
    return {
      tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
      series: [{ type: 'sankey' as const, layoutIterations: 32, nodeAlign: config.nodeAlign ?? 'justify', nodeWidth: config.nodeWidth ?? 20, nodeGap: config.nodeGap ?? 12, roam: true, data: nds, links: lks, lineStyle: { color: 'gradient' as const, curveness: 0.5, opacity: 0.4 }, label: { fontSize: 11, color: c.text }, emphasis: { focus: 'adjacency' as const } }],
    }
  }

  // Map (scatter fallback on plain axes)
  if (t === 'map') {
    const latC = config.yAxis?.[0] ?? cols.find(c => /^lat/i.test(c.name))?.name ?? ''
    const lonC = config.xAxis ?? cols.find(c => /^lon/i.test(c.name) || /^lng/i.test(c.name))?.name ?? ''
    const lti = colIdx(cols, latC); const lni = colIdx(cols, lonC)
    const pts = rows.map(r => ({ lon: Number(r[lni]), lat: Number(r[lti]) })).filter(d => !isNaN(d.lon) && !isNaN(d.lat))
    if (pts.length === 0) return {}
    return {
      tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
      grid: { top: config.title ? 46 : 20, right: 16, bottom: 8, left: 16, containLabel: true },
      xAxis: { type: 'value' as const, name: lonC || 'Longitude', min: -180, max: 180, ...axisSt(config.showGrid, c) },
      yAxis: { type: 'value' as const, name: latC || 'Latitude', min: -90, max: 90, ...axisSt(config.showGrid, c) },
      series: [{ type: 'scatter' as const, data: pts.map(p => [p.lon, p.lat]), symbolSize: 10, itemStyle: { color: CHART_COLORS[0], opacity: 0.85 } }],
    }
  }

  // Hierarchy tree
  if (t === 'hierarchy_tree') {
    const columns = cols.map(c => c.name)
    const idC = config.idColumn ?? columns[0]; const pC = config.parentIdColumn ?? columns[1]
    const lC = config.labelColumn ?? columns.find(c => !c.toLowerCase().includes('id') && !c.includes('parent') && c !== idC && c !== pC) ?? idC
    const mC = config.metricColumns ?? []; const h = config.layout === 'left-to-right'
    const nm = new Map<string, any>(); const rs2: any[] = []
    rows.forEach((r, i) => {
      const id = String(r[colIdx(cols, idC)] ?? `node-${i}`); const lb = lC ? String(r[colIdx(cols, lC)] ?? id) : id
      const n: any = { name: lb, parentId: String(r[colIdx(cols, pC)] ?? ''), itemStyle: { color: config.seriesColors?.[lb] ?? CHART_COLORS[i % CHART_COLORS.length] } }
      if (mC.length > 0) n.name = `${lb}\n${mC.map((m: string) => `${m}: ${r[colIdx(cols, m)]}`).join(', ')}`
      nm.set(id, n)
    })
    nm.forEach((n, id) => { if (n.parentId && n.parentId !== id && nm.has(n.parentId)) { const p = nm.get(n.parentId); if (!p.children) p.children = []; p.children.push(n) } else { rs2.push(n) } })
    return {
      tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
      series: [{ type: 'tree' as const, data: rs2, orient: h ? 'LR' as const : 'TB' as const, top: config.title ? '16%' : '8%', bottom: mC.length > 0 ? '30%' : '12%', left: `${h ? 15 : 8}%`, right: `${h ? 20 : 8}%`, roam: true, initialTreeDepth: -1, symbolSize: 12, edgeShape: 'curve' as const, label: { position: h ? 'right' as const : 'top' as const, verticalAlign: 'middle' as const, fontSize: 11, color: c.text }, lineStyle: { color: c.border, width: 1.5, curveness: 0.5 }, itemStyle: { borderColor: c.bgCard }, expandAndCollapse: true }],
    }
  }

  // Timeline
  if (t === 'timeline') {
    const columns = cols.map(c => c.name)
    const tiC = config.timeColumn ?? columns[0]; const lbC = config.labelColumn; const gbC = config.groupBy
    const cd = rows.map(r => { const o: Record<string, unknown> = {}; cols.forEach((co, i) => { o[co.name] = r[i] }); return o }).filter(d => d[tiC] != null).sort((a, b) => new Date(String(a[tiC])).getTime() - new Date(String(b[tiC])).getTime())
    const grps = gbC ? [...new Set(cd.map(d => String(d[gbC] ?? 'Unknown')))] : ['Events']
    const sg = grps.length === 1
    if (config.endTimeColumn) {
      const etC = config.endTimeColumn
      return {
        tooltip: { trigger: 'axis' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
        title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
        legend: grps.length > 1 ? { top: config.title ? 30 : 0, textStyle: { fontSize: 11, color: c.textMuted } } : undefined,
        grid: { top: grps.length > 1 ? 56 : 40, right: 16, bottom: 16, left: 16, containLabel: true },
        xAxis: { type: 'time' as const, ...axisSt(config.showGrid, c) },
        yAxis: sg ? { type: 'value' as const, show: false, min: 0, max: 1 } : { type: 'category' as const, data: grps, inverse: true, ...axisSt(config.showGrid, c) },
        series: grps.map((g, gi) => ({
          name: g, type: 'custom' as const,
          renderItem: (_params: unknown, api: any) => {
            const st = api.value(0); const et = api.value(1); const s = api.coord([st, gi]); const e = api.coord([et, gi]); const bh = api.size([0, 1])[1] * 0.6
            return { type: 'rect', shape: { x: s[0], y: s[1] - bh / 2, width: e[0] - s[0], height: bh }, style: { fill: config.seriesColors?.[g] ?? CHART_COLORS[gi % CHART_COLORS.length], opacity: 0.85 } }
          },
          data: cd.filter(d => gbC ? String(d[gbC] ?? 'Unknown') === g : true).map(d => [new Date(String(d[tiC])).getTime(), new Date(String(d[etC])).getTime(), g]),
        })),
      }
    }
    return {
      tooltip: { trigger: 'item' as const, backgroundColor: c.bgCard, borderColor: c.border, textStyle: { fontSize: 12, color: c.text } },
      title: config.title ? { text: config.title, left: 'center', top: 8, textStyle: { fontSize: 14, color: c.text } } : undefined,
      legend: grps.length > 1 ? { top: config.title ? 30 : 0, textStyle: { fontSize: 11, color: c.textMuted } } : undefined,
      grid: { top: 56, right: 16, bottom: 60, left: 16 },
      xAxis: { type: 'time' as const, ...axisSt(config.showGrid, c) },
      yAxis: sg ? { type: 'value' as const, show: false, min: 0, max: 1 } : { type: 'category' as const, data: grps, ...axisSt(config.showGrid, c) },
      series: grps.flatMap((g, gi) => {
        const gd = cd.filter(d => gbC ? String(d[gbC] ?? 'Unknown') === g : true)
        const cl = config.seriesColors?.[g] ?? CHART_COLORS[gi % CHART_COLORS.length]
        return [
          { name: g, type: 'scatter' as const, symbolSize: 14, itemStyle: { color: cl, borderColor: c.text, borderWidth: 1 }, data: gd.filter((_, i) => i % 2 === 0).map(d => [new Date(String(d[tiC])).getTime(), sg ? 0.2 : g, lbC ? d[lbC] : null]) },
          { name: `${g}_bottom`, type: 'scatter' as const, symbolSize: 14, itemStyle: { color: cl, borderColor: c.text, borderWidth: 1 }, data: gd.filter((_, i) => i % 2 === 1).map(d => [new Date(String(d[tiC])).getTime(), sg ? 0.2 : g, lbC ? d[lbC] : null]) },
        ]
      }),
    }
  }

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
      const option = buildOption(rs, chartConfig!)
      if (option && Object.keys(option).length > 0) {
        const optJson = JSON.stringify(option)
        return { html: `<div class="cell-output chart"><div class="chart-wrap" id="cht-${cell.id}" style="width:100%;height:300px;min-height:300px"></div><script type="application/json" data-chart-for="cht-${cell.id}">${optJson}<\/script></div>`, height: 300 }
      }
    } catch {}
  }

  const dbg = `hasCC=${hasChartConfig} vm=${viewMode} ct=${chartConfig?.chartType} ya=${JSON.stringify((chartConfig as any)?.yAxis || [])}`
  return { html: `<div class="cell-output" data-dbg="${escapeHtml(dbg)}">${renderTable(rs)}</div>`, height: 0 }
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
.big-number-body{display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:200px;padding:24px;gap:8px}
.big-number-label{font-size:13px;font-weight:600;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;text-align:center}
.big-number-value{font-size:56px;font-weight:700;color:var(--text-primary);line-height:1.1;letter-spacing:-1.5px;text-align:center;word-break:break-word}
.big-number-prefix{font-size:28px;font-weight:400;color:var(--text-muted);margin-right:4px}
.big-number-suffix{font-size:28px;font-weight:400;color:var(--text-muted);margin-left:4px}
.chart-wrap{width:100%;height:300px;min-height:300px}
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
    var theme = document.documentElement.getAttribute('data-theme');
    chart.setOption(opt, { notMerge: true });
    window.addEventListener('resize', function() { chart.resize(); });
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
