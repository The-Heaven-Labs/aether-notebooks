import { useState } from 'react'
import type React from 'react'
import type { Output, ResultSet } from '../types'
import { ChartView } from './ChartView'
import { ToggleLeft, Calendar, Clock, Fingerprint, Ban, Binary, Table, BarChart2, Timer, Sigma } from 'lucide-react'

interface Props {
  outputs: Output[]
  fixedView?: 'table' | 'chart'
}

export function OutputRenderer({ outputs, fixedView }: Props) {
  if (!outputs || outputs.length === 0) return null

  return (
    <div style={styles.container}>
      {outputs.map((out, i) => (
        <OutputItem key={i} output={out} fixedView={fixedView} />
      ))}
    </div>
  )
}

function OutputItem({ output, fixedView }: { output: Output; fixedView?: 'table' | 'chart' }) {
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
    return <TableOutput rs={rs} fixedView={fixedView} />
  }

  return null
}

const TYPE_MAP: Record<string, { icon: React.ReactNode; label: string }> = {
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
  datetime: { icon: <Clock size={12} />, label: 'Datetime' },
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
}

function TypeIcon({ type }: { type: string }) {
  const normalized = type.toLowerCase()
  const info = TYPE_MAP[normalized] ?? { icon: '?', label: 'Unknown' }
  return (
    <span title={info.label} style={typeIconStyles.badge}>
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

function TableOutput({ rs, fixedView }: { rs: ResultSet; fixedView?: 'table' | 'chart' }) {
  const [view, setView] = useState<'table' | 'chart'>(fixedView ?? 'table')

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
              onClick={() => setView('table')}
            >
              <Table size={12} /> Table
            </button>
            <button
              style={{ ...styles.viewBtn, ...(view === 'chart' ? styles.viewBtnActive : {}), display: 'flex', alignItems: 'center', gap: 4 }}
              onClick={() => setView('chart')}
            >
              <BarChart2 size={12} /> Chart
            </button>
          </div>
        )}
      </div>

      {view === 'table' ? (
        <div style={styles.tableWrap}>
          <table style={styles.table}>
            <thead>
              <tr>
                {rs.columns.map((col) => (
                  <th key={col.name} style={styles.th}>
                    <span style={styles.colName}>{col.name}</span>
                    <TypeIcon type={col.type} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rs.rows.map((row, i) => (
                <tr key={i}>
                  {(row as unknown[]).map((cell, j) => (
                    <td key={j} style={styles.td}>
                      {cell === null
                        ? <span style={styles.null}>null</span>
                        : typeof cell === 'object'
                          ? <span style={styles.json}>{JSON.stringify(cell)}</span>
                          : String(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        // TODO: pass onConfigChange to persist chart config to backend (PUT /cells/:id output)
        // Currently chart config is session-only (reset on cell re-execution or reload)
        <ChartView rs={rs} />
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {},
  errorWrap: {
    padding: '12px 16px',
    background: '#fdf5f5',
    border: '1px solid #f5d0d0',
    borderRadius: 4,
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
  },
  errorLabel: {
    fontSize: 11,
    fontWeight: 700,
    color: '#9a2828',
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
  },
  error: {
    color: '#9a2828',
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
    maxHeight: 340,
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
}
