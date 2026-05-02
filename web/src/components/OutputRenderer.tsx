import { useState, useRef, useCallback, useEffect } from 'react'
import type React from 'react'
import type { Output, ResultSet } from '../types'
import { ChartView } from './ChartView'
import { ToggleLeft, Calendar, Clock, Fingerprint, Ban, Binary, Table, BarChart2, Timer, Sigma, ChevronUp, ChevronDown, X } from 'lucide-react'

interface Props {
  outputs: Output[]
  fixedView?: 'table' | 'chart'
  cellId?: string
}

export function OutputRenderer({ outputs, fixedView, cellId }: Props) {
  if (!outputs || outputs.length === 0) return null

  return (
    <div style={styles.container}>
      {outputs.map((out, i) => (
        <OutputItem key={i} output={out} fixedView={fixedView} cellId={cellId} />
      ))}
    </div>
  )
}

function OutputItem({ output, fixedView, cellId }: { output: Output; fixedView?: 'table' | 'chart'; cellId?: string }) {
  if (output.type === 'error') {
    return (
      <div style={styles.errorWrap}>
        <span style={styles.errorLabel}>Error</span>
        <pre style={styles.error}>{String(output.data)}</pre>
      </div>
    )
  }

  if (output.type === 'text') {
    return <pre style={styles.text}>{String(output.data)}</pre>
  }

  if (output.type === 'table') {
    const rs = output.data as ResultSet
    if (!rs?.columns?.length) return <p style={styles.empty}>No results returned</p>
    return <TableOutput rs={rs} fixedView={fixedView} cellId={cellId} />
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
const MAX_CELL_DISPLAY = 100

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
}

function isNumericColumn(rs: ResultSet, colIndex: number): boolean {
  const nonNullValues = rs.rows
    .map((row) => (row as unknown[])[colIndex])
    .filter((v) => v !== null && v !== undefined && v !== '')
  if (nonNullValues.length === 0) return false
  return nonNullValues.every((v) => !Number.isNaN(parseFloat(String(v))))
}

function sortRows(rows: unknown[][], colIndex: number, direction: SortDirection, numeric: boolean): unknown[][] {
  if (direction === 'none') return rows
  const sorted = [...rows].sort((a, b) => {
    const av = (a as unknown[])[colIndex]
    const bv = (b as unknown[])[colIndex]
    // nulls last
    if (av === null || av === undefined) return 1
    if (bv === null || bv === undefined) return -1
    let cmp: number
    if (numeric) {
      cmp = parseFloat(String(av)) - parseFloat(String(bv))
    } else {
      cmp = String(av).localeCompare(String(bv))
    }
    return direction === 'asc' ? cmp : -cmp
  })
  return sorted
}

function TableOutput({ rs, fixedView, cellId }: { rs: ResultSet; fixedView?: 'table' | 'chart'; cellId?: string }) {
  const storageKey = cellId ? `hnb_cell_view_${cellId}` : null
  const [view, setView] = useState<'table' | 'chart'>(() => {
    if (fixedView) return fixedView
    if (storageKey) {
      const saved = localStorage.getItem(storageKey)
      if (saved === 'chart' || saved === 'table') return saved
    }
    return 'table'
  })
  const [outputHeight, setOutputHeight] = useState(OUTPUT_DEFAULT_HEIGHT)
  const dragStartY = useRef<number | null>(null)
  const dragStartHeight = useRef<number>(OUTPUT_DEFAULT_HEIGHT)
  const [sort, setSort] = useState<SortState>({ column: null, direction: 'none' })
  const [detail, setDetail] = useState<DetailPanel | null>(null)

  const handleViewChange = (v: 'table' | 'chart') => {
    setView(v)
    if (storageKey) localStorage.setItem(storageKey, v)
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
  const numericSort = sortColIndex >= 0 ? isNumericColumn(rs, sortColIndex) : false
  const displayRows = sortColIndex >= 0 && sort.direction !== 'none'
    ? sortRows(rs.rows, sortColIndex, sort.direction, numericSort)
    : rs.rows

  const openDetail = (rowIndex: number, colIndex: number, value: string) => {
    setDetail({ rowIndex, colIndex, value, colName: rs.columns[colIndex].name })
  }

  const closeDetail = () => setDetail(null)

  const navigateDetail = useCallback((delta: number) => {
    if (!detail) return
    const newRow = detail.rowIndex + delta
    if (newRow < 0 || newRow >= displayRows.length) return
    const rawValue = (displayRows[newRow] as unknown[])[detail.colIndex]
    const value = rawValue === null || rawValue === undefined
      ? ''
      : typeof rawValue === 'object'
        ? JSON.stringify(rawValue)
        : String(rawValue)
    setDetail({ ...detail, rowIndex: newRow, value })
  }, [detail, displayRows])

  useEffect(() => {
    if (!detail) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { closeDetail(); return }
      if (e.key === 'ArrowDown') { e.preventDefault(); navigateDetail(1) }
      if (e.key === 'ArrowUp') { e.preventDefault(); navigateDetail(-1) }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [detail, navigateDetail])

  return (
    <div style={styles.tableSection}>
      <div style={styles.outputBar}>
        <span style={styles.rowCount}>
          {rs.rows.length} row{rs.rows.length !== 1 ? 's' : ''} · {rs.columns.length} columns
        </span>
        {!fixedView && (
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
      </div>

      {view === 'table' ? (
        <div style={{ position: 'relative', display: 'flex' }}>
          <div className="output-scroll-area" style={{ ...styles.tableWrap, maxHeight: outputHeight, flex: 1, minWidth: 0 }}>
            <table style={styles.table}>
              <thead>
                <tr>
                  {rs.columns.map((col) => {
                    const isSorted = sort.column === col.name
                    return (
                      <th
                        key={col.name}
                        style={{ ...styles.th, cursor: 'pointer', userSelect: 'none' }}
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
              <tbody>
                {displayRows.map((row, i) => (
                  <tr key={i} style={detail?.rowIndex === i ? { background: 'var(--bg-secondary)' } : undefined}>
                    {(row as unknown[]).map((cell, j) => {
                      if (cell === null) {
                        return <td key={j} style={styles.td}><span style={styles.null}>null</span></td>
                      }
                      const strValue = typeof cell === 'object' ? JSON.stringify(cell) : String(cell)
                      const isTruncated = strValue.length > MAX_CELL_DISPLAY
                      const displayValue = isTruncated ? strValue.slice(0, MAX_CELL_DISPLAY) + '…' : strValue
                      const isObj = typeof cell === 'object'
                      return (
                        <td key={j} style={styles.td}>
                          {isTruncated ? (
                            <span
                              style={styles.truncatedCell}
                              onClick={() => openDetail(i, j, strValue)}
                              title="Click to view full value"
                            >
                              <span style={isObj ? styles.json : undefined}>{displayValue}</span>
                            </span>
                          ) : (
                            <span style={isObj ? styles.json : undefined}>{strValue}</span>
                          )}
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {detail && (
            <div style={styles.detailPanel}>
              <div style={styles.detailHeader}>
                <div style={styles.detailHeaderLeft}>
                  <span style={styles.detailColName}>{detail.colName}</span>
                  <span style={styles.detailRowLabel}>row {detail.rowIndex + 1} of {displayRows.length}</span>
                </div>
                <div style={{ display: 'flex', gap: 4 }}>
                  <button
                    style={styles.detailNavBtn}
                    onClick={() => navigateDetail(-1)}
                    disabled={detail.rowIndex === 0}
                    title="Previous row (↑)"
                    aria-label="Previous row"
                  >
                    <ChevronUp size={14} />
                  </button>
                  <button
                    style={styles.detailNavBtn}
                    onClick={() => navigateDetail(1)}
                    disabled={detail.rowIndex >= displayRows.length - 1}
                    title="Next row (↓)"
                    aria-label="Next row"
                  >
                    <ChevronDown size={14} />
                  </button>
                  <button style={styles.detailCloseBtn} onClick={closeDetail} aria-label="Close panel">
                    <X size={14} />
                  </button>
                </div>
              </div>
              <div style={styles.detailBody}>
                <pre style={styles.detailValue}>{detail.value}</pre>
              </div>
            </div>
          )}
        </div>
      ) : (
        // TODO: pass onConfigChange to persist chart config to backend (PUT /cells/:id output)
        <ChartView rs={rs} cellId={cellId} />
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
}

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
    opacity: 0.6,
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
  },
  colName: {
    fontWeight: 600,
    color: 'var(--text-primary)',
    fontFamily: 'var(--font-mono)',
    fontSize: 12,
  },
  td: {
    padding: '7px 16px',
    borderBottom: '1px solid var(--border-light)',
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
  truncatedCell: {
    cursor: 'pointer',
    textDecoration: 'underline',
    textDecorationStyle: 'dotted',
    textDecorationColor: 'var(--text-muted)',
    color: 'var(--text-primary)',
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
}
