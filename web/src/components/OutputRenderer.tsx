import { useState, useRef, useCallback, useEffect, useMemo, memo } from 'react'
import type React from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { Output, ResultSet, Column } from '../types'
import { ChartView } from '../charts'
import type { ChartConfig } from '../charts'
import { ToggleLeft, Calendar, Clock, Fingerprint, Ban, Binary, Table, BarChart2, Timer, Sigma, ChevronUp, ChevronDown, ChevronLeft, ChevronRight, X, Copy, Check, Download } from 'lucide-react'
import { api } from '../api/client'

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

interface Props {
  outputs: Output[]
  fixedView?: 'table' | 'chart'
  cellId?: string
  chartConfig?: ChartConfig
  onChartConfigChange?: (config: ChartConfig) => void
  hideExport?: boolean
  viewMode?: 'table' | 'chart'
  onViewModeChange?: (viewMode: 'table' | 'chart') => void
  footerExtra?: React.ReactNode
}

export const OutputRenderer = memo(function OutputRenderer({ outputs, fixedView, cellId, chartConfig, onChartConfigChange, hideExport, viewMode, onViewModeChange, footerExtra }: Props) {
  if (!outputs || outputs.length === 0) return null

  return (
    <div style={{ ...styles.container, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      {outputs.map((out, i) => (
        <OutputItem key={i} output={out} fixedView={fixedView} cellId={cellId} chartConfig={chartConfig} onChartConfigChange={onChartConfigChange} hideExport={hideExport} viewMode={viewMode} onViewModeChange={onViewModeChange} footerExtra={footerExtra} />
      ))}
    </div>
  )
})

function OutputItem({ output, fixedView, cellId, chartConfig, onChartConfigChange, hideExport, viewMode, onViewModeChange, footerExtra }: { output: Output; fixedView?: 'table' | 'chart'; cellId?: string; chartConfig?: ChartConfig; onChartConfigChange?: (config: ChartConfig) => void; hideExport?: boolean; viewMode?: 'table' | 'chart'; onViewModeChange?: (viewMode: 'table' | 'chart') => void; footerExtra?: React.ReactNode }) {
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
    if (!rs?.columns?.length) return <p style={styles.empty}>No results returned</p>
    return <TableOutput rs={rs} fixedView={fixedView} cellId={cellId} chartConfig={chartConfig} onChartConfigChange={onChartConfigChange} hideExport={hideExport} viewMode={viewMode} onViewModeChange={onViewModeChange} footerExtra={footerExtra} />
  }

  return null
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
const ROW_NUM_WIDTH = 40

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

const TableOutput = memo(function TableOutput({ rs, fixedView, cellId, chartConfig, onChartConfigChange, hideExport: hideExportProp, viewMode, onViewModeChange, footerExtra }: { rs: ResultSet; fixedView?: 'table' | 'chart'; cellId?: string; chartConfig?: ChartConfig; onChartConfigChange?: (config: ChartConfig) => void; hideExport?: boolean; viewMode?: 'table' | 'chart'; onViewModeChange?: (viewMode: 'table' | 'chart') => void; footerExtra?: React.ReactNode }) {
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

  // Imperative cell highlighting to avoid re-rendering all rows on detail change
  useEffect(() => {
    if (!scrollAreaRef.current) return
    scrollAreaRef.current.querySelectorAll<HTMLElement>('[data-row][data-col]').forEach(el => {
      el.style.background = ''
      el.style.outline = ''
      el.style.outlineOffset = ''
    })
    if (detail && isDetailActive) {
      const cell = scrollAreaRef.current.querySelector<HTMLElement>(
        `[data-row="${detail.rowIndex}"][data-col="${detail.colIndex}"]`,
      )
      if (cell) {
        cell.style.background = 'var(--accent-light)'
        cell.style.outline = '1px solid var(--accent)'
        cell.style.outlineOffset = '-1px'
        activeCellRef.current = cell
      }
    }
  }, [detail, isDetailActive])

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
    estimateSize: () => COL_WIDTH,
    horizontal: true,
    overscan: 4,
  })

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

  const virtualRows = rowVirtualizer.getVirtualItems()
  const virtualColumns = columnVirtualizer.getVirtualItems()

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
                  onClick={() => openDetail(vr.index, vc.index, strValue, cell)}
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
    if (!detail || !isDetailActive) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { closeDetail(); return }
      if (e.key === 'ArrowDown') { e.preventDefault(); navigateDetail(1, 0) }
      if (e.key === 'ArrowUp') { e.preventDefault(); navigateDetail(-1, 0) }
      if (e.key === 'ArrowRight') { e.preventDefault(); navigateDetail(0, 1) }
      if (e.key === 'ArrowLeft') { e.preventDefault(); navigateDetail(0, -1) }
      if (e.key === 'c' && (e.ctrlKey || e.metaKey)) {
        const active = document.activeElement
        if (active?.closest('.cm-editor') ||
            active?.tagName === 'INPUT' ||
            active?.tagName === 'TEXTAREA') {
          return
        }
        e.preventDefault()
        copyDetail()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [detail, isDetailActive, navigateDetail, copyDetail])

  return (
    <div style={{ ...styles.tableSection, flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
      <div style={{ ...styles.outputBar, flexShrink: 0 }}>
        <span style={styles.rowCount}>
          {rs.rows.length} row{rs.rows.length !== 1 ? 's' : ''} · {rs.columns.length} columns
        </span>
        {!fixedView && !hideExport && (
          <div style={styles.exportGroup}>
            <button style={styles.exportBtn} onClick={() => exportCSV(rs)} title="Download as CSV" aria-label="Download as CSV">
              <Download size={12} /> CSV
            </button>
            <button style={styles.exportBtn} onClick={() => exportJSON(rs)} title="Download as JSON" aria-label="Download as JSON">
              <Download size={12} /> JSON
            </button>
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
                  {rs.columns.map((col) => {
                    const isSorted = sort.column === col.name
                    return (
                      <th
                        key={col.name}
                        style={{ ...styles.th, width: COL_WIDTH, cursor: 'pointer', userSelect: 'none' }}
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
          <ChartView rs={rs} onConfigChange={onChartConfigChange} output={{ type: 'table', data: { columns: rs.columns, rows: rs.rows }, config: chartConfig }} />
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
  },
  rowCount: {
    fontSize: 10,
    color: 'var(--text-muted)',
    fontFamily: 'var(--font-mono)',
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
  virtualTd: {
    position: 'absolute',
    borderBottom: '1px solid var(--border-light)',
    padding: 0,
    overflow: 'hidden',
    cursor: 'pointer',
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
