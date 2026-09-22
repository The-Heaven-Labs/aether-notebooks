import { useState, useRef, useCallback, useEffect, useMemo, memo } from 'react'
import type React from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { Output, ResultSet, Column, OutputStubData } from '../types'
import { ChartView } from '../charts'
import type { ChartConfig } from '../charts'
import { ToggleLeft, Calendar, Clock, Fingerprint, Ban, Binary, Table, BarChart2, Timer, Sigma, ChevronUp, ChevronDown, ChevronLeft, ChevronRight, X, Copy, Check, Download } from 'lucide-react'
import { api, getToken } from '../api/client'
import { getApiUrl } from '../config'

// Streaming download URL for a cell's raw stored outputs. A real navigation is
// used (not fetch+blob) so the payload is streamed to disk rather than
// materialized in the JS heap; the token rides the query string because the
// middleware accepts it for navigations that cannot set an Authorization header.
function outputsDownloadUrl(cellId: string): string {
  const base = getApiUrl()
  const token = getToken()
  const q = token ? `?token=${encodeURIComponent(token)}` : ''
  return `${base}/api/v1/cells/${cellId}/outputs/download${q}`
}

function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`
  return `${(n / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

// Global state to ensure only one detail panel is open at a time across all cells
let activeDetailCellId: string | null = null
const detailListeners = new Set<(cellId: string | null) => void>()

function setActiveDetailCell(cellId: string | null) {
  activeDetailCellId = cellId
  detailListeners.forEach(listener => listener(cellId))
}

/** Returns true if any cell's detail panel is open (for keyboard shortcut gating) */
export function isAnyDetailActive() {
  return activeDetailCellId !== null
}

function useActiveDetailCell() {
  const [isActive, setIsActive] = useState(activeDetailCellId)
  useEffect(() => {
    const listener = (cellId: string | null) => setIsActive(cellId)
    detailListeners.add(listener)
    return () => { detailListeners.delete(listener) }
  }, [])
  return isActive
}

// Selection ownership: only the last table output the user pressed down on
// responds to selection shortcuts (Ctrl/Cmd+A, Escape), mirroring the
// activeDetailCellId mechanism above. Selection state itself stays local to
// each table output.
let activeSelectionTableId: number | null = null
let nextSelectionTableId = 0

export interface CellPos {
  row: number
  col: number
}

export interface CellRange {
  anchor: CellPos
  extent: CellPos
}

export function selectionBounds(range: CellRange): { rowStart: number; rowEnd: number; colStart: number; colEnd: number } {
  return {
    rowStart: Math.min(range.anchor.row, range.extent.row),
    rowEnd: Math.max(range.anchor.row, range.extent.row),
    colStart: Math.min(range.anchor.col, range.extent.col),
    colEnd: Math.max(range.anchor.col, range.extent.col),
  }
}

// TSV cell coercion mirrors exportCSV: null/undefined → empty, objects → JSON.
// Values containing tabs/newlines are copied raw (spreadsheet-app behavior).
export function selectionToTSV(rows: unknown[][], range: CellRange): string {
  const { rowStart, rowEnd, colStart, colEnd } = selectionBounds(range)
  const lines: string[] = []
  for (let r = rowStart; r <= rowEnd; r++) {
    const row = rows[r] as unknown[] | undefined
    const cells: string[] = []
    for (let c = colStart; c <= colEnd; c++) {
      cells.push(clipboardCellText(row?.[c]))
    }
    lines.push(cells.join('\t'))
  }
  return lines.join('\n')
}

function clipboardCellText(cell: unknown): string {
  if (cell === null || cell === undefined) return ''
  return typeof cell === 'object' ? JSON.stringify(cell) : String(cell)
}

interface Props {
  outputs: Output[]
  fixedView?: 'table' | 'chart'
  cellId?: string
  chartConfig?: ChartConfig
  onChartConfigChange?: (config: ChartConfig) => void
  chartConfigOverridden?: boolean
  onChartConfigReset?: () => void
  hideExport?: boolean
  viewMode?: 'table' | 'chart'
  onViewModeChange?: (viewMode: 'table' | 'chart') => void
  footerExtra?: React.ReactNode
}

export const OutputRenderer = memo(function OutputRenderer({ outputs, fixedView, cellId, chartConfig, onChartConfigChange, chartConfigOverridden, onChartConfigReset, hideExport, viewMode, onViewModeChange, footerExtra }: Props) {
  if (!outputs || outputs.length === 0) return null

  return (
    <div style={{ ...styles.container, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      {outputs.map((out, i) => (
        <OutputItem key={i} output={out} fixedView={fixedView} cellId={cellId} chartConfig={chartConfig} onChartConfigChange={onChartConfigChange} chartConfigOverridden={chartConfigOverridden} onChartConfigReset={onChartConfigReset} hideExport={hideExport} viewMode={viewMode} onViewModeChange={onViewModeChange} footerExtra={footerExtra} />
      ))}
    </div>
  )
})

function OutputItem({ output, fixedView, cellId, chartConfig, onChartConfigChange, chartConfigOverridden, onChartConfigReset, hideExport, viewMode, onViewModeChange, footerExtra }: { output: Output; fixedView?: 'table' | 'chart'; cellId?: string; chartConfig?: ChartConfig; onChartConfigChange?: (config: ChartConfig) => void; chartConfigOverridden?: boolean; onChartConfigReset?: () => void; hideExport?: boolean; viewMode?: 'table' | 'chart'; onViewModeChange?: (viewMode: 'table' | 'chart') => void; footerExtra?: React.ReactNode }) {
  if (output.type === 'error') {
    return (
      <div style={styles.errorWrap}>
        <span style={styles.errorLabel}>Error</span>
        <pre style={styles.error}>{typeof output.data === 'string' ? output.data : JSON.stringify(output.data, null, 2)}</pre>
      </div>
    )
  }

  if (output.type === 'text') {
    return <pre style={styles.text}>{String(output.data)}</pre>
  }

  if (output.type === 'table') {
    const rs = output.data as ResultSet
    // Read-path stub: the notebook inline budget stubbed this cell's outputs.
    // No columns/rows were inlined — show the truncation notice and offer the
    // streaming download instead.
    if (rs?.truncated && !rs?.columns?.length) {
      return <TruncatedStub data={rs as unknown as OutputStubData} cellId={cellId} hideExport={hideExport} />
    }
    if (!rs?.columns?.length) return <p style={styles.empty}>No results returned</p>
    return <TableOutput rs={rs} fixedView={fixedView} cellId={cellId} chartConfig={chartConfig} onChartConfigChange={onChartConfigChange} chartConfigOverridden={chartConfigOverridden} onChartConfigReset={onChartConfigReset} hideExport={hideExport} viewMode={viewMode} onViewModeChange={onViewModeChange} footerExtra={footerExtra} />
  }

  return null
}

// TruncatedStub renders the read-path stub for a cell whose outputs were not
// inlined on notebook GET because the inline budget was exhausted. The full
// payload remains available via the streaming download endpoint.
function TruncatedStub({ data, cellId, hideExport }: { data: OutputStubData; cellId?: string; hideExport?: boolean }) {
  return (
    <div style={styles.tableSection}>
      <div style={styles.outputBar}>
        <span style={styles.rowCount}>
          Output truncated — {formatBytes(data.bytes ?? 0)} not inlined
        </span>
        {!hideExport && cellId && (
          <a style={styles.exportBtn} href={outputsDownloadUrl(cellId)} title="Download full result" aria-label="Download full result">
            <Download size={12} /> Download full result
          </a>
        )}
      </div>
    </div>
  )
}

const TYPE_MAP: Record<string, { icon: React.ReactNode; label: string }> = {
  // Generic / Postgres
  string: { icon: 'Aa', label: 'String' },
  varchar: { icon: 'Aa', label: 'String' },
  text: { icon: 'Aa', label: 'String' },
  char: { icon: 'Aa', label: 'String' },
  integer: { icon: '#', label: 'Integer' },
  int: { icon: '#', label: 'Integer' },
  int2: { icon: '#', label: 'Integer' },
  int4: { icon: '#', label: 'Integer' },
  int8: { icon: '#', label: 'Integer' },
  bigint: { icon: '#', label: 'Integer' },
  smallint: { icon: '#', label: 'Integer' },
  float: { icon: '0.1', label: 'Float' },
  float4: { icon: '0.1', label: 'Float' },
  float8: { icon: '0.1', label: 'Float' },
  double: { icon: '0.1', label: 'Float' },
  decimal: { icon: '0.1', label: 'Float' },
  numeric: { icon: '0.1', label: 'Float' },
  real: { icon: '0.1', label: 'Float' },
  boolean: { icon: <ToggleLeft size={12} />, label: 'Boolean' },
  bool: { icon: <ToggleLeft size={12} />, label: 'Boolean' },
  date: { icon: <Calendar size={12} />, label: 'Date' },
  date32: { icon: <Calendar size={12} />, label: 'Date' },
  datetime: { icon: <Clock size={12} />, label: 'Datetime' },
  datetime64: { icon: <Clock size={12} />, label: 'Datetime' },
  timestamp: { icon: <Clock size={12} />, label: 'Datetime' },
  timestamptz: { icon: <Clock size={12} />, label: 'Datetime' },
  'timestamp with time zone': { icon: <Clock size={12} />, label: 'Datetime' },
  time: { icon: <Timer size={12} />, label: 'Time' },
  interval: { icon: <Sigma size={12} />, label: 'Interval' },
  array: { icon: '[]', label: 'Array' },
  json: { icon: '{}', label: 'JSON' },
  jsonb: { icon: '{}', label: 'JSON' },
  uuid: { icon: <Fingerprint size={12} />, label: 'UUID' },
  null: { icon: <Ban size={12} />, label: 'Null' },
  bytes: { icon: <Binary size={12} />, label: 'Bytes' },
  bytea: { icon: <Binary size={12} />, label: 'Bytes' },
  unknown: { icon: '?', label: 'Unknown' },
  // ClickHouse-specific base types (after stripping wrappers/params)
  fixedstring: { icon: 'Aa', label: 'String' },
  enum8: { icon: 'Aa', label: 'String' },
  enum16: { icon: 'Aa', label: 'String' },
  int16: { icon: '#', label: 'Integer' },
  int32: { icon: '#', label: 'Integer' },
  int64: { icon: '#', label: 'Integer' },
  int128: { icon: '#', label: 'Integer' },
  int256: { icon: '#', label: 'Integer' },
  uint8: { icon: '#', label: 'Integer' },
  uint16: { icon: '#', label: 'Integer' },
  uint32: { icon: '#', label: 'Integer' },
  uint64: { icon: '#', label: 'Integer' },
  uint128: { icon: '#', label: 'Integer' },
  uint256: { icon: '#', label: 'Integer' },
  float32: { icon: '0.1', label: 'Float' },
  float64: { icon: '0.1', label: 'Float' },
}

// Strips ClickHouse type wrappers (Nullable, LowCardinality) and parameters
// so "LowCardinality(String)" → "string", "Decimal(10, 2)" → "decimal".
function normalizeTypeName(type: string): string {
  let t = type.trim()
  for (const wrapper of ['Nullable', 'LowCardinality']) {
    if (t.startsWith(wrapper + '(') && t.endsWith(')')) {
      t = t.slice(wrapper.length + 1, -1).trim()
    }
  }
  const parenIdx = t.indexOf('(')
  if (parenIdx !== -1) t = t.slice(0, parenIdx)
  return t.toLowerCase()
}

function TypeIcon({ type }: { type: string }) {
  const normalized = normalizeTypeName(type)
  const info = TYPE_MAP[normalized] ?? { icon: '?', label: 'Unknown' }
  return (
    <span title={`${info.label} (${type})`} style={typeIconStyles.badge}>
      {info.icon}
    </span>
  )
}

const typeIconStyles: Record<string, React.CSSProperties> = {
  badge: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 10,
    fontFamily: 'var(--font-mono)',
    fontWeight: 700,
    color: 'var(--text-muted)',
    background: 'var(--bg-primary)',
    border: '1px solid var(--border-light)',
    borderRadius: 4,
    padding: '1px 5px',
    marginLeft: 6,
    cursor: 'default',
    userSelect: 'none',
  },
}

const OUTPUT_MIN_HEIGHT = 80
const OUTPUT_DEFAULT_HEIGHT = 340

// Virtualized result-table geometry. Rows are windowed vertically and columns
// horizontally via @tanstack/react-virtual so the DOM stays bounded regardless
// of result size (see UPSTREAM_FIX_TABLE_VIRTUALIZATION design).
const ROW_HEIGHT = 32
const COL_WIDTH = 140
const COL_MIN_WIDTH = 60
const COL_MAX_WIDTH = 600
const ROW_NUM_WIDTH = 40
// Pointer travel (px) before a press becomes a drag instead of a click.
const DRAG_THRESHOLD = 4

type SortDirection = 'none' | 'asc' | 'desc'

interface SortState {
  column: string | null
  direction: SortDirection
}

interface DetailPanel {
  rowIndex: number
  colIndex: number
  value: string
  colName: string
  isArray: boolean
  arrayItems: unknown[]
}

const NUMERIC_TYPES = new Set([
  'int2', 'int4', 'int8', 'integer', 'bigint', 'smallint',
  'float', 'float4', 'float8', 'double', 'decimal', 'numeric', 'real',
  'int16', 'int32', 'int64', 'int128', 'int256',
  'uint8', 'uint16', 'uint32', 'uint64', 'uint128', 'uint256',
  'float32', 'float64',
])

const DATE_TYPES = new Set([
  'date', 'date32', 'datetime', 'datetime64',
  'timestamp', 'timestamptz', 'timestamp with time zone', 'time', 'interval',
])

function getColumnSortType(col: Column): 'numeric' | 'date' | 'string' {
  const normalized = normalizeTypeName(col.type)
  if (NUMERIC_TYPES.has(normalized)) return 'numeric'
  if (DATE_TYPES.has(normalized)) return 'date'
  return 'string'
}

function sortRows(rows: unknown[][], colIndex: number, direction: SortDirection, sortType: 'numeric' | 'date' | 'string'): unknown[][] {
  if (direction === 'none') return rows
  const sorted = [...rows].sort((a, b) => {
    const av = (a as unknown[])[colIndex]
    const bv = (b as unknown[])[colIndex]
    // nulls last
    if (av === null || av === undefined) return 1
    if (bv === null || bv === undefined) return -1
    let cmp: number
    if (sortType === 'numeric') {
      cmp = parseFloat(String(av)) - parseFloat(String(bv))
    } else if (sortType === 'date') {
      cmp = new Date(String(av)).getTime() - new Date(String(bv)).getTime()
    } else {
      cmp = String(av).localeCompare(String(bv))
    }
    return direction === 'asc' ? cmp : -cmp
  })
  return sorted
}

function escapeCSV(value: string): string {
  if (value.includes(',') || value.includes('"') || value.includes('\n') || value.includes('\r')) {
    return '"' + value.replace(/"/g, '""') + '"'
  }
  return value
}

function exportCSV(rs: ResultSet): void {
  const header = rs.columns.map(c => escapeCSV(c.name)).join(',')
  const rows = rs.rows.map(row =>
    (row as unknown[]).map(cell => {
      if (cell === null || cell === undefined) return ''
      const str = typeof cell === 'object' ? JSON.stringify(cell) : String(cell)
      return escapeCSV(str)
    }).join(',')
  )
  const csv = [header, ...rows].join('\n')
  const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'query-results.csv'
  a.click()
  URL.revokeObjectURL(url)
}

function exportJSON(rs: ResultSet): void {
  const data = rs.rows.map(row => {
    const obj: Record<string, unknown> = {}
    rs.columns.forEach((col, i) => {
      obj[col.name] = (row as unknown[])[i]
    })
    return obj
  })
  const json = JSON.stringify(data, null, 2)
  const blob = new Blob([json], { type: 'application/json;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'query-results.json'
  a.click()
  URL.revokeObjectURL(url)
}

const TableOutput = memo(function TableOutput({ rs, fixedView, cellId, chartConfig, onChartConfigChange, chartConfigOverridden, onChartConfigReset, hideExport: hideExportProp, viewMode, onViewModeChange, footerExtra }: { rs: ResultSet; fixedView?: 'table' | 'chart'; cellId?: string; chartConfig?: ChartConfig; onChartConfigChange?: (config: ChartConfig) => void; chartConfigOverridden?: boolean; onChartConfigReset?: () => void; hideExport?: boolean; viewMode?: 'table' | 'chart'; onViewModeChange?: (viewMode: 'table' | 'chart') => void; footerExtra?: React.ReactNode }) {
  const storageKey = cellId ? `aether_cell_view_${cellId}` : null
  const hasChartConfig = !!chartConfig?.chartType
  const [dataExportEnabled, setDataExportEnabled] = useState(true)
  useEffect(() => {
    api.get<{ data_export_enabled: boolean }>('/api/v1/org/data-export')
      .then(r => setDataExportEnabled(r.data_export_enabled))
      .catch(() => {})
  }, [])
  const hideExport = hideExportProp || !dataExportEnabled
  const [view, setView] = useState<'table' | 'chart'>(() => {
    if (fixedView) return fixedView
    if (viewMode) return viewMode
    if (hasChartConfig) return 'chart'
    if (storageKey) {
      const saved = localStorage.getItem(storageKey)
      if (saved === 'chart' || saved === 'table') return saved
    }
    return 'table'
  })
  // Sync view when props change (viewMode, chart config added via broadcast)
  useEffect(() => {
    if (viewMode && viewMode !== view) {
      setView(viewMode)
    } else if (!viewMode && hasChartConfig && view === 'table') {
      setView('chart')
    }
  }, [viewMode, hasChartConfig, view])
  const [outputHeight, setOutputHeight] = useState(OUTPUT_DEFAULT_HEIGHT)
  const dragStartY = useRef<number | null>(null)
  const dragStartHeight = useRef<number>(OUTPUT_DEFAULT_HEIGHT)
  const [sort, setSort] = useState<SortState>({ column: null, direction: 'none' })
  const [detail, setDetail] = useState<DetailPanel | null>(null)
  const activeDetailCell = useActiveDetailCell()
  const isDetailActive = cellId ? activeDetailCell === cellId : false
  const activeCellRef = useRef<HTMLElement | null>(null)
  const theadRef = useRef<HTMLTableSectionElement | null>(null)
  const [copied, setCopied] = useState(false)
  const scrollAreaRef = useRef<HTMLDivElement | null>(null)
  const [selection, setSelection] = useState<CellRange | null>(null)
  const [columnWidths, setColumnWidths] = useState<Record<number, number>>({})
  // Set on mouseup after a real drag so the synthesised click does not also
  // open the detail panel.
  const suppressCellClickRef = useRef(false)
  const tableIdRef = useRef(0)
  if (tableIdRef.current === 0) tableIdRef.current = ++nextSelectionTableId

  const copyDetail = useCallback(() => {
    if (!detail) return
    navigator.clipboard.writeText(detail.value).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }, [detail])

  const handleViewChange = (v: 'table' | 'chart') => {
    setView(v)
    if (storageKey) localStorage.setItem(storageKey, v)
    onViewModeChange?.(v)
  }

  const onResizeMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    dragStartY.current = e.clientY
    dragStartHeight.current = outputHeight

    const onMouseMove = (ev: MouseEvent) => {
      if (dragStartY.current === null) return
      const delta = ev.clientY - dragStartY.current
      const newHeight = Math.max(OUTPUT_MIN_HEIGHT, dragStartHeight.current + delta)
      setOutputHeight(newHeight)
    }

    const onMouseUp = () => {
      dragStartY.current = null
      window.removeEventListener('mousemove', onMouseMove)
      window.removeEventListener('mouseup', onMouseUp)
    }

    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', onMouseUp)
  }, [outputHeight])

  const handleColumnClick = (colName: string) => {
    setSort((prev) => {
      if (prev.column !== colName) return { column: colName, direction: 'asc' }
      if (prev.direction === 'asc') return { column: colName, direction: 'desc' }
      if (prev.direction === 'desc') return { column: null, direction: 'none' }
      return { column: colName, direction: 'asc' }
    })
  }

  const sortColIndex = sort.column !== null ? rs.columns.findIndex((c) => c.name === sort.column) : -1
  const sortType = sortColIndex >= 0 ? getColumnSortType(rs.columns[sortColIndex]) : 'string'
  const displayRows = useMemo(
    () => sortColIndex >= 0 && sort.direction !== 'none'
      ? sortRows(rs.rows, sortColIndex, sort.direction, sortType)
      : rs.rows,
    [rs.rows, sortColIndex, sort.direction, sortType],
  )

  const rowVirtualizer = useVirtualizer({
    count: displayRows.length,
    getScrollElement: () => scrollAreaRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
  })
  const columnVirtualizer = useVirtualizer({
    count: rs.columns.length,
    getScrollElement: () => scrollAreaRef.current,
    estimateSize: (index: number) => columnWidths[index] ?? COL_WIDTH,
    horizontal: true,
    overscan: 4,
  })

  const virtualRows = rowVirtualizer.getVirtualItems()
  const virtualColumns = columnVirtualizer.getVirtualItems()
  const virtualRangeKey = `${virtualRows[0]?.index ?? -1}:${virtualRows.length}:${virtualColumns[0]?.index ?? -1}:${virtualColumns.length}`

  // Imperative cell highlighting to avoid re-rendering all rows on detail or
  // selection change. The virtual range key makes the repaint re-run whenever
  // the virtual window changes, so newly mounted cells get their highlight.
  useEffect(() => {
    const area = scrollAreaRef.current
    if (!area) return
    area.querySelectorAll<HTMLElement>('[data-row][data-col]').forEach(el => {
      el.style.background = ''
      el.style.outline = ''
      el.style.outlineOffset = ''
    })
    if (selection) {
      const { rowStart, rowEnd, colStart, colEnd } = selectionBounds(selection)
      area.querySelectorAll<HTMLElement>('[data-row][data-col]').forEach(el => {
        const row = Number(el.dataset.row)
        const col = Number(el.dataset.col)
        if (row >= rowStart && row <= rowEnd && col >= colStart && col <= colEnd) {
          el.style.background = 'var(--accent-light)'
        }
      })
    }
    if (detail && isDetailActive) {
      const cell = area.querySelector<HTMLElement>(
        `[data-row="${detail.rowIndex}"][data-col="${detail.colIndex}"]`,
      )
      if (cell) {
        cell.style.background = 'var(--accent-light)'
        cell.style.outline = '1px solid var(--accent)'
        cell.style.outlineOffset = '-1px'
        activeCellRef.current = cell
      }
    }
  }, [detail, isDetailActive, selection, virtualRangeKey])

  // Row indices address displayRows, so re-sorting or receiving a new result
  // set silently changes what a selection means — drop it instead of letting
  // copy produce the wrong cells.
  const [selectionScope, setSelectionScope] = useState<{ sort: SortState; rows: unknown[][] }>({ sort, rows: rs.rows })
  if (selectionScope.sort !== sort || selectionScope.rows !== rs.rows) {
    setSelectionScope({ sort, rows: rs.rows })
    if (selection) setSelection(null)
  }

  // Re-measure after a width commit, once the virtualizer options reflect the
  // new estimateSize. Doing it in the same tick as the state update would use
  // the previous closure and wipe the resize.
  useEffect(() => {
    columnVirtualizer.measure()
  }, [columnWidths, columnVirtualizer])

  const beginCellSelection = useCallback((e: React.MouseEvent, row: number, col: number) => {
    if (e.button !== 0) return
    // Suppress native text selection while dragging; click still fires.
    e.preventDefault()
    const anchor = e.shiftKey && selection ? selection.anchor : { row, col }
    activeSelectionTableId = tableIdRef.current
    setSelection({ anchor, extent: { row, col } })
    suppressCellClickRef.current = false
    const startX = e.clientX
    const startY = e.clientY
    let moved = false
    const onMouseMove = (ev: MouseEvent) => {
      if (!moved && Math.abs(ev.clientX - startX) < DRAG_THRESHOLD && Math.abs(ev.clientY - startY) < DRAG_THRESHOLD) return
      moved = true
      const el = ev.target
      const td = el instanceof HTMLElement ? el.closest<HTMLElement>('td[data-row][data-col]') : null
      if (!td || !scrollAreaRef.current?.contains(td)) return
      const r = Number(td.dataset.row)
      const c = Number(td.dataset.col)
      if (!Number.isInteger(r) || !Number.isInteger(c)) return
      setSelection({ anchor, extent: { row: r, col: c } })
    }
    const onMouseUp = () => {
      window.removeEventListener('mousemove', onMouseMove)
      window.removeEventListener('mouseup', onMouseUp)
      if (moved) suppressCellClickRef.current = true
    }
    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', onMouseUp)
  }, [selection])

  const onColumnResizeMouseDown = useCallback((e: React.MouseEvent, colIndex: number) => {
    e.preventDefault()
    e.stopPropagation()
    const startX = e.clientX
    const startWidth = columnWidths[colIndex] ?? COL_WIDTH
    let finalWidth = startWidth
    const applyWidth = (width: number) => {
      finalWidth = width
      setColumnWidths(prev => ({ ...prev, [colIndex]: width }))
      columnVirtualizer.resizeItem(colIndex, width)
    }
    const onMouseMove = (ev: MouseEvent) => {
      applyWidth(Math.min(COL_MAX_WIDTH, Math.max(COL_MIN_WIDTH, startWidth + (ev.clientX - startX))))
    }
    const onMouseUp = () => {
      window.removeEventListener('mousemove', onMouseMove)
      window.removeEventListener('mouseup', onMouseUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
      setColumnWidths(prev => ({ ...prev, [colIndex]: finalWidth }))
    }
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', onMouseUp)
  }, [columnWidths, columnVirtualizer])

  const resetColumnWidth = useCallback((colIndex: number) => {
    columnVirtualizer.resizeItem(colIndex, COL_WIDTH)
    setColumnWidths(prev => {
      const next = { ...prev }
      delete next[colIndex]
      return next
    })
  }, [columnVirtualizer])

  // Bring a target row/col into the virtual window (used by detail navigation).
  const scrollToCell = useCallback((row: number, col: number) => {
    if (!scrollAreaRef.current || typeof scrollAreaRef.current.scrollTo !== 'function') return
    rowVirtualizer.scrollToIndex(row, { align: 'auto' })
    columnVirtualizer.scrollToIndex(col, { align: 'auto' })
  }, [rowVirtualizer, columnVirtualizer])

  const openDetail = useCallback((rowIndex: number, colIndex: number, value: string, rawValue: unknown) => {
    const prettyValue = typeof rawValue === 'object' && rawValue !== null
      ? JSON.stringify(rawValue, null, 2)
      : value
    setDetail({
      rowIndex,
      colIndex,
      value: prettyValue,
      colName: rs.columns[colIndex].name,
      isArray: Array.isArray(rawValue),
      arrayItems: Array.isArray(rawValue) ? rawValue : [],
    })
    if (cellId) setActiveDetailCell(cellId)
  }, [cellId, rs.columns])

  const closeDetail = useCallback(() => {
    setDetail(null)
    if (cellId && activeDetailCellId === cellId) setActiveDetailCell(null)
  }, [cellId])

  const navigateDetail = useCallback((rowDelta: number, colDelta: number) => {
    if (!detail) return
    const newRow = detail.rowIndex + rowDelta
    const newCol = detail.colIndex + colDelta
    if (newRow < 0 || newRow >= displayRows.length) return
    if (newCol < 0 || newCol >= rs.columns.length) return
    scrollToCell(newRow, newCol)
    const rawValue = (displayRows[newRow] as unknown[])[newCol]
    const strValue = rawValue === null || rawValue === undefined
      ? ''
      : typeof rawValue === 'object'
        ? JSON.stringify(rawValue)
        : String(rawValue)
    const prettyValue = typeof rawValue === 'object' && rawValue !== null
      ? JSON.stringify(rawValue, null, 2)
      : strValue
    setDetail({
      rowIndex: newRow,
      colIndex: newCol,
      value: prettyValue,
      colName: rs.columns[newCol].name,
      isArray: Array.isArray(rawValue),
      arrayItems: Array.isArray(rawValue) ? rawValue : [],
    })
  }, [detail, displayRows, rs.columns, scrollToCell])

  const handleCellClick = useCallback((e: React.MouseEvent, row: number, col: number, value: string, rawValue: unknown) => {
    if (suppressCellClickRef.current) {
      suppressCellClickRef.current = false
      return
    }
    // Shift+click only extends the selection; it never opens the panel.
    if (e.shiftKey) return
    openDetail(row, col, value, rawValue)
  }, [openDetail])

  const virtualTbody = (
    <tbody style={{ height: rowVirtualizer.getTotalSize(), position: 'relative' }}>
      {virtualRows.map(vr => {
        const row = displayRows[vr.index] as unknown[]
        return (
          <tr
            key={vr.key}
            style={{ position: 'absolute', top: 0, left: 0, transform: `translateY(${vr.start}px)` }}
          >
            <td style={{ ...styles.rowNumTd, position: 'absolute', left: 0, width: ROW_NUM_WIDTH, height: vr.size, padding: 0 }}>
              <span style={styles.rowNumCell}>{vr.index + 1}</span>
            </td>
            {virtualColumns.map(vc => {
              const cell = row[vc.index]
              const isObj = typeof cell === 'object' && cell !== null
              const strValue = cell === null || cell === undefined
                ? ''
                : typeof cell === 'object'
                  ? JSON.stringify(cell)
                  : String(cell)
              return (
                <td
                  key={vc.key}
                  data-row={vr.index}
                  data-col={vc.index}
                  style={{ ...styles.virtualTd, left: ROW_NUM_WIDTH + vc.start, width: vc.size, height: vr.size }}
                  title={strValue}
                  onMouseDown={(e) => beginCellSelection(e, vr.index, vc.index)}
                  onClick={(e) => handleCellClick(e, vr.index, vc.index, strValue, cell)}
                >
                  <span style={isObj ? { ...styles.cellText, ...styles.json } : styles.cellText}>
                    {cell === null ? <span style={styles.null}>null</span> : strValue}
                  </span>
                </td>
              )
            })}
          </tr>
        )
      })}
    </tbody>
  )

  useEffect(() => {
    if (activeCellRef.current) {
      const headerHeight = theadRef.current?.offsetHeight ?? 0
      activeCellRef.current.style.scrollMarginTop = `${headerHeight}px`
      activeCellRef.current.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
    }
  }, [detail?.rowIndex, detail?.colIndex])

  useEffect(() => {
    if (!detail && !selection) return
    const handler = (e: KeyboardEvent) => {
      // Ownership is evaluated per event: another table output may have become
      // the active selection target since this listener was installed.
      const detailActive = !!detail && isDetailActive
      const selectionOwned = !!selection && activeSelectionTableId === tableIdRef.current
      if (!detailActive && !selectionOwned) return
      const target = e.target instanceof HTMLElement ? e.target : null
      if (target?.closest('.cm-editor') || target?.tagName === 'INPUT' || target?.tagName === 'TEXTAREA') return
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'a' && selectionOwned) {
        if (displayRows.length === 0 || rs.columns.length === 0) return
        e.preventDefault()
        setSelection({
          anchor: { row: 0, col: 0 },
          extent: { row: displayRows.length - 1, col: rs.columns.length - 1 },
        })
        return
      }
      if (e.key === 'Escape') {
        if (selectionOwned) setSelection(null)
        if (detailActive) closeDetail()
        return
      }
      if (!detailActive) return
      if (e.key === 'ArrowDown') { e.preventDefault(); navigateDetail(1, 0) }
      if (e.key === 'ArrowUp') { e.preventDefault(); navigateDetail(-1, 0) }
      if (e.key === 'ArrowRight') { e.preventDefault(); navigateDetail(0, 1) }
      if (e.key === 'ArrowLeft') { e.preventDefault(); navigateDetail(0, -1) }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [detail, isDetailActive, selection, displayRows.length, rs.columns.length, navigateDetail, closeDetail])

  useEffect(() => {
    if (!detail && !selection) return
    const onCopy = (e: ClipboardEvent) => {
      const detailActive = !!detail && isDetailActive
      const selectionOwned = !!selection && activeSelectionTableId === tableIdRef.current
      if (!detailActive && !selectionOwned) return
      const target = e.target instanceof HTMLElement ? e.target : null
      if (target?.closest('.cm-editor') || target?.tagName === 'INPUT' || target?.tagName === 'TEXTAREA') return
      const sel = window.getSelection()
      if (sel && !sel.isCollapsed && sel.toString().length > 0) return
      if (selectionOwned) {
        e.preventDefault()
        e.clipboardData?.setData('text/plain', selectionToTSV(displayRows, selection))
        return
      }
      if (detailActive) {
        e.preventDefault()
        e.clipboardData?.setData('text/plain', detail.value)
      }
    }
    document.addEventListener('copy', onCopy)
    return () => document.removeEventListener('copy', onCopy)
  }, [detail, isDetailActive, selection, displayRows])

  useEffect(() => () => {
    if (activeDetailCellId === cellId) setActiveDetailCell(null)
  }, [cellId])

  return (
    <div style={{ ...styles.tableSection, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      <div style={{ ...styles.outputBar, flexShrink: 0 }}>
        <span style={styles.rowCount}>
          {rs.rows.length} row{rs.rows.length !== 1 ? 's' : ''} · {rs.columns.length} columns
          {rs.truncated && (
            <span style={styles.truncatedBadge}>
              Truncated — {rs.rows_included ?? rs.rows.length} {rs.rows_total != null && rs.rows_total > 0 ? `of ${rs.rows_total} ` : ''}rows / {formatBytes(rs.bytes ?? 0)}
            </span>
          )}
        </span>
        {!fixedView && !hideExport && (
          <div style={styles.exportGroup}>
            {rs.truncated && cellId && (
              <a style={styles.exportBtn} href={outputsDownloadUrl(cellId)} title="Download full result" aria-label="Download full result">
                <Download size={12} /> Full result
              </a>
            )}
            <button style={styles.exportBtn} onClick={() => exportCSV(rs)} title="Download as CSV" aria-label="Download as CSV">
              <Download size={12} /> CSV
            </button>
            {!rs.truncated && (cellId ? (
              <a style={styles.exportBtn} href={outputsDownloadUrl(cellId)} title="Download as JSON" aria-label="Download as JSON">
                <Download size={12} /> JSON
              </a>
            ) : (
              <button style={styles.exportBtn} onClick={() => exportJSON(rs)} title="Download as JSON" aria-label="Download as JSON">
                <Download size={12} /> JSON
              </button>
            ))}
          </div>
        )}
        {!fixedView && !hideExport && (
          <div style={styles.viewToggle}>
            <button
              style={{ ...styles.viewBtn, ...(view === 'table' ? styles.viewBtnActive : {}), display: 'flex', alignItems: 'center', gap: 4 }}
              onClick={() => handleViewChange('table')}
            >
              <Table size={12} /> Table
            </button>
            <button
              style={{ ...styles.viewBtn, ...(view === 'chart' ? styles.viewBtnActive : {}), display: 'flex', alignItems: 'center', gap: 4 }}
              onClick={() => handleViewChange('chart')}
            >
              <BarChart2 size={12} /> Chart
            </button>
          </div>
        )}
        {footerExtra}
      </div>

      {view === 'table' ? (
        <div style={{ position: 'relative', display: 'flex', flex: 1, minHeight: 0 }}>
          <div ref={scrollAreaRef} className="output-scroll-area" style={{ ...styles.tableWrap, maxHeight: outputHeight, flex: 1, minWidth: 0 }}>
            <table
              style={{
                ...styles.table,
                width: ROW_NUM_WIDTH + columnVirtualizer.getTotalSize(),
                tableLayout: 'fixed',
                borderCollapse: 'separate',
                borderSpacing: 0,
              }}
            >
              <thead ref={theadRef}>
                <tr>
                  <th style={{ ...styles.th, ...styles.rowNumTh, width: ROW_NUM_WIDTH, cursor: 'default' }}>
                    <span style={styles.colName}>#</span>
                  </th>
                  {rs.columns.map((col, colIndex) => {
                    const isSorted = sort.column === col.name
                    return (
                      <th
                        key={col.name}
                        style={{ ...styles.th, width: columnWidths[colIndex] ?? COL_WIDTH, cursor: 'pointer', userSelect: 'none' }}
                        onClick={() => handleColumnClick(col.name)}
                        title={`Sort by ${col.name}`}
                      >
                        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                          <span style={styles.colName}>{col.name}</span>
                          <TypeIcon type={col.type} />
                          {isSorted && sort.direction === 'asc' && (
                            <ChevronUp size={12} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                          )}
                          {isSorted && sort.direction === 'desc' && (
                            <ChevronDown size={12} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                          )}
                          {!isSorted && (
                            <span style={{ width: 12, flexShrink: 0, opacity: 0 }}><ChevronUp size={12} /></span>
                          )}
                        </span>
                        <span
                          role="separator"
                          aria-label={`Resize column ${col.name}`}
                          className="col-resize-handle"
                          style={styles.colResizeHandle}
                          title="Drag to resize; double-click to reset"
                          onMouseDown={(e) => onColumnResizeMouseDown(e, colIndex)}
                          onClick={(e) => e.stopPropagation()}
                          onDoubleClick={(e) => { e.stopPropagation(); resetColumnWidth(colIndex) }}
                        />
                      </th>
                    )
                  })}
                </tr>
              </thead>
              {virtualTbody}
            </table>
          </div>

          {detail && isDetailActive && (
            <div style={styles.detailPanel}>
              <div style={styles.detailHeader}>
                <div style={styles.detailHeaderLeft}>
                  <span style={styles.detailColName}>{detail.colName}</span>
                  <span style={styles.detailRowLabel}>
                    col {detail.colIndex + 1}/{rs.columns.length} · row {detail.rowIndex + 1}/{displayRows.length}
                  </span>
                </div>
                <div style={{ display: 'flex', gap: 4 }}>
                  <button style={styles.detailNavBtn} onClick={copyDetail} title="Copy value" aria-label="Copy value">
                    {copied ? <Check size={14} style={{ color: 'var(--success, #10b981)' }} /> : <Copy size={14} />}
                  </button>
                  <button style={styles.detailCloseBtn} onClick={closeDetail} aria-label="Close panel">
                    <X size={14} />
                  </button>
                </div>
              </div>
              <div style={styles.detailNav}>
                <div style={styles.detailNavGroup}>
                  <button
                    style={styles.detailNavBtn}
                    onClick={() => navigateDetail(0, -1)}
                    disabled={detail.colIndex === 0}
                    title="Previous column (←)"
                    aria-label="Previous column"
                  >
                    <ChevronLeft size={14} />
                  </button>
                  <button
                    style={styles.detailNavBtn}
                    onClick={() => navigateDetail(0, 1)}
                    disabled={detail.colIndex >= rs.columns.length - 1}
                    title="Next column (→)"
                    aria-label="Next column"
                  >
                    <ChevronRight size={14} />
                  </button>
                </div>
                <div style={styles.detailNavGroup}>
                  <button
                    style={styles.detailNavBtn}
                    onClick={() => navigateDetail(-1, 0)}
                    disabled={detail.rowIndex === 0}
                    title="Previous row (↑)"
                    aria-label="Previous row"
                  >
                    <ChevronUp size={14} />
                  </button>
                  <button
                    style={styles.detailNavBtn}
                    onClick={() => navigateDetail(1, 0)}
                    disabled={detail.rowIndex >= displayRows.length - 1}
                    title="Next row (↓)"
                    aria-label="Next row"
                  >
                    <ChevronDown size={14} />
                  </button>
                </div>
              </div>
              <div style={styles.detailBody}>
                {detail.isArray ? (
                  <table style={styles.arrayTable}>
                    <thead><tr><th style={styles.arrayTableHeader}>Index</th><th style={styles.arrayTableHeader}>Value</th></tr></thead>
                    <tbody>
                      {detail.arrayItems.map((item, idx) => (
                        <tr key={idx}>
                          <td style={styles.arrayIndex}>{idx}</td>
                          <td style={styles.arrayValue}>
                            {typeof item === 'object' && item !== null
                              ? JSON.stringify(item, null, 2)
                              : String(item)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <pre style={styles.detailValue}>{detail.value}</pre>
                )}
              </div>
            </div>
          )}
        </div>
      ) : (
        <div style={{ flex: 1, minHeight: fixedView ? 0 : 300, display: 'flex', flexDirection: 'column' }}>
          <ChartView rs={rs} onConfigChange={onChartConfigChange} chartConfigOverridden={chartConfigOverridden} onChartConfigReset={onChartConfigReset} output={{ type: 'table', data: { columns: rs.columns, rows: rs.rows }, config: chartConfig }} />
        </div>
      )}

      <div
        style={styles.resizeHandle}
        onMouseDown={onResizeMouseDown}
        title="Drag to resize output"
      >
        <span style={styles.resizeGrip} />
      </div>
    </div>
  )
})

const styles: Record<string, React.CSSProperties> = {
  container: {},
  errorWrap: {
    padding: '12px 16px',
    background: 'var(--error-light)',
    border: '1px solid var(--error-border)',
    borderRadius: 4,
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  errorLabel: {
    fontSize: 11,
    fontWeight: 700,
    color: 'var(--error-text)',
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
  },
  error: {
    color: 'var(--error-text)',
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
    whiteSpace: 'pre-wrap',
    margin: 0,
  },
  text: {
    background: 'var(--bg-secondary)',
    padding: '12px 16px',
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
    whiteSpace: 'pre-wrap',
    borderTop: '1px solid var(--border-light)',
    margin: 0,
  },
  empty: {
    color: 'var(--text-muted)',
    fontSize: 13,
    padding: '12px 16px',
    borderTop: '1px solid var(--border-light)',
  },
  tableSection: {
    borderTop: '1px solid var(--border-light)',
  },
  outputBar: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '6px 16px',
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border-light)',
    gap: 8,
    flexWrap: 'wrap',
  },
  exportGroup: {
    display: 'flex',
    gap: 4,
  },
  exportBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    padding: '3px 8px',
    fontSize: 11,
    fontWeight: 500,
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-card)',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
    textDecoration: 'none',
  },
  rowCount: {
    fontSize: 10,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  truncatedBadge: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    marginLeft: 8,
    padding: '2px 8px',
    borderRadius: 10,
    background: 'var(--accent-light, #e0f2fe)',
    color: 'var(--accent, #0369a1)',
    fontSize: 10,
    fontWeight: 600,
    fontFamily: 'var(--font-sans)',
  },
  viewToggle: {
    display: 'flex',
    gap: 2,
    background: 'var(--border-light)',
    padding: 2,
    borderRadius: 4,
  },
  viewBtn: {
    padding: '3px 10px',
    border: '1px solid transparent',
    background: 'none',
    borderRadius: 4,
    fontSize: 12,
    fontWeight: 500,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    fontFamily: 'var(--font-sans)',
  },
  viewBtnActive: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text-primary)',
  },
  tableWrap: {
    overflowX: 'auto',
    overflowY: 'auto',
    overflowAnchor: 'none',
  },
  resizeHandle: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: 8,
    cursor: 'ns-resize',
    background: 'var(--border-light)',
    borderTop: '1px solid var(--border-light)',
    userSelect: 'none',
  },
  resizeGrip: {
    display: 'block',
    width: 28,
    height: 3,
    borderRadius: 2,
    background: 'var(--border)',
  },
  table: {
    width: '100%',
    borderCollapse: 'collapse',
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
  },
  th: {
    padding: '9px 16px',
    textAlign: 'left',
    background: 'var(--bg-card)',
    borderBottom: '1px solid var(--border)',
    whiteSpace: 'nowrap',
    position: 'sticky',
    top: 0,
    zIndex: 2,
  },
  rowNumTh: {
    padding: '9px 8px',
    textAlign: 'center',
    width: 1,
    minWidth: 40,
  },
  rowNumTd: {
    padding: '7px 8px',
    textAlign: 'center',
    borderBottom: '1px solid var(--border-light)',
    width: 1,
    minWidth: 40,
  },
  rowNumCell: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100%',
    boxSizing: 'border-box',
    padding: '7px 8px',
    fontSize: 11,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
    userSelect: 'none',
  },
  colName: {
    fontWeight: 600,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    fontSize: 12,
  },
  colResizeHandle: {
    position: 'absolute',
    top: 0,
    // Keep the hit area inside this th: a handle extending past the right edge
    // overlaps the next (equal z-index, later-in-DOM) th, which wins hit
    // testing and swallows the drag.
    right: 0,
    width: 7,
    height: '100%',
    cursor: 'col-resize',
    zIndex: 3,
    userSelect: 'none',
  },
  virtualTd: {
    position: 'absolute',
    borderBottom: '1px solid var(--border-light)',
    padding: 0,
    overflow: 'hidden',
    cursor: 'pointer',
    userSelect: 'none',
  },
  cellText: {
    display: 'flex',
    alignItems: 'center',
    height: '100%',
    boxSizing: 'border-box',
    padding: '7px 16px',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    color: 'var(--text-primary)',
    fontSize: 13,
  },
  null: {
    color: 'var(--text-muted)',
    fontStyle: 'italic',
  },
  json: {
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    color: 'var(--text-muted)',
  },
  tdActive: {
    background: 'var(--accent-light)',
    outline: '1px solid var(--accent)',
    outlineOffset: -1,
  },
  detailPanel: {
    width: 320,
    flexShrink: 0,
    borderLeft: '1px solid var(--border)',
    background: 'var(--bg-card)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  detailHeader: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 12px',
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-secondary)',
    gap: 8,
  },
  detailHeaderLeft: {
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    minWidth: 0,
  },
  detailColName: {
    fontSize: 12,
    fontWeight: 700,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  detailRowLabel: {
    fontSize: 10,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
  },
  detailNav: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '6px 12px',
    borderBottom: '1px solid var(--border)',
    background: 'var(--bg-secondary)',
  },
  detailNavGroup: {
    display: 'flex',
    gap: 4,
  },
  detailNavBtn: {
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '3px 5px',
    lineHeight: 1,
  },
  detailCloseBtn: {
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '3px 5px',
    lineHeight: 1,
  },
  detailBody: {
    flex: 1,
    overflow: 'auto',
    padding: '12px',
    maxHeight: 400,
  },
  detailValue: {
    margin: 0,
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-primary)',
    whiteSpace: 'pre-wrap',
    wordBreak: 'break-all',
    lineHeight: 1.6,
  },
  arrayTable: {
    width: '100%',
    borderCollapse: 'collapse',
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
  },
  arrayTableHeader: {
    padding: '6px 10px',
    textAlign: 'left',
    borderBottom: '1px solid var(--border)',
    fontWeight: 700,
    color: 'var(--text-muted)',
    fontSize: 10,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.05em',
    position: 'sticky' as const,
    top: 0,
    background: 'var(--bg-card)',
  },
  arrayIndex: {
    padding: '3px 10px',
    color: 'var(--text-muted)',
    fontSize: 11,
    borderBottom: '1px solid var(--border-light)',
    whiteSpace: 'nowrap' as const,
    verticalAlign: 'top' as const,
  },
  arrayValue: {
    padding: '3px 10px',
    color: 'var(--text-primary)',
    fontSize: 12,
    borderBottom: '1px solid var(--border-light)',
    whiteSpace: 'pre-wrap' as const,
    wordBreak: 'break-all' as const,
  },
}
