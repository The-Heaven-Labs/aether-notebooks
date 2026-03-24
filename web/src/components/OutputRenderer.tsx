import { Output, ResultSet } from '../types'

interface Props {
  outputs: Output[]
}

export function OutputRenderer({ outputs }: Props) {
  if (!outputs || outputs.length === 0) return null

  return (
    <div style={styles.container}>
      {outputs.map((out, i) => (
        <OutputItem key={i} output={out} />
      ))}
    </div>
  )
}

function OutputItem({ output }: { output: Output }) {
  if (output.type === 'error') {
    return (
      <pre style={styles.error}>{String(output.data)}</pre>
    )
  }

  if (output.type === 'text') {
    return <pre style={styles.text}>{String(output.data)}</pre>
  }

  if (output.type === 'table') {
    const rs = output.data as ResultSet
    if (!rs?.columns?.length) return <p style={styles.empty}>No results</p>
    return <TableView rs={rs} />
  }

  return null
}

function TableView({ rs }: { rs: ResultSet }) {
  return (
    <div style={styles.tableWrapper}>
      <table style={styles.table}>
        <thead>
          <tr>
            {rs.columns.map((col) => (
              <th key={col.name} style={styles.th}>
                {col.name}
                <span style={styles.colType}>{col.type}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rs.rows.map((row, i) => (
            <tr key={i} style={i % 2 === 0 ? {} : styles.rowAlt}>
              {(row as unknown[]).map((cell, j) => (
                <td key={j} style={styles.td}>
                  {cell === null ? <span style={styles.null}>null</span> : String(cell)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      <p style={styles.rowCount}>{rs.rows.length} rows</p>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: { marginTop: 8 },
  error: {
    background: '#fdf0f0',
    color: 'var(--error)',
    padding: '10px 14px',
    borderRadius: 6,
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
    whiteSpace: 'pre-wrap',
  },
  text: {
    background: 'var(--bg-secondary)',
    padding: '10px 14px',
    borderRadius: 6,
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
    whiteSpace: 'pre-wrap',
  },
  empty: { color: 'var(--text-secondary)', fontSize: 13, padding: '8px 0' },
  tableWrapper: { overflowX: 'auto', borderRadius: 6, border: '1px solid var(--border)' },
  table: { width: '100%', borderCollapse: 'collapse', fontSize: 13 },
  th: {
    padding: '8px 12px',
    textAlign: 'left',
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border)',
    fontWeight: 600,
    whiteSpace: 'nowrap',
  },
  colType: {
    marginLeft: 6,
    fontSize: 11,
    color: 'var(--text-secondary)',
    fontWeight: 400,
  },
  td: { padding: '6px 12px', borderBottom: '1px solid #f0ede8' },
  rowAlt: { background: '#faf9f7' },
  null: { color: 'var(--text-secondary)', fontStyle: 'italic' },
  rowCount: { padding: '6px 12px', fontSize: 12, color: 'var(--text-secondary)', background: 'var(--bg-secondary)' },
}
