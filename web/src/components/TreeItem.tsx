import { ChevronDown, ChevronRight, Table2, Columns } from 'lucide-react'

interface Column {
  name: string
  type: string
}

interface Props {
  name: string
  columns: Column[]
  isExpanded: boolean
  onToggle: (name: string) => void
}

export function TreeItem({ name, columns, isExpanded, onToggle }: Props) {
  return (
    <div style={styles.tableItem}>
      <button style={styles.tableRow} onClick={() => onToggle(name)} title={name}>
        <span style={styles.tableChevron}>{isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}</span>
        <span style={styles.tableIcon}><Table2 size={12} /></span>
        <span style={styles.tableName}>{name}</span>
        <span style={styles.columnCount}>{columns.length}</span>
      </button>
      {isExpanded && (
        <div style={styles.columnList}>
          {columns.map((col) => (
            <div key={col.name} style={styles.columnRow}>
              <span style={{ ...styles.columnIcon, display: 'flex', alignItems: 'center' }}><Columns size={12} /></span>
              <span style={styles.columnName}>{col.name}</span>
              <span style={styles.columnType}>{col.type}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  tableItem: {
    display: 'flex',
    flexDirection: 'column',
  },
  tableRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '5px 12px',
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    width: '100%',
    textAlign: 'left',
    color: 'var(--text-secondary)',
    borderRadius: 0,
  },
  tableChevron: {
    fontSize: 10,
    color: 'var(--text-muted)',
    flexShrink: 0,
    width: 10,
  },
  tableIcon: {
    fontSize: 12,
    color: 'var(--accent)',
    flexShrink: 0,
  },
  tableName: {
    fontSize: 13,
    fontFamily: 'var(--font-mono)',
    fontWeight: 500,
    flex: 1,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  columnCount: {
    fontSize: 10,
    color: 'var(--text-muted)',
    background: 'var(--border)',
    borderRadius: 10,
    padding: '1px 6px',
    flexShrink: 0,
  },
  columnList: {
    display: 'flex',
    flexDirection: 'column',
    paddingBottom: 4,
  },
  columnRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '3px 12px 3px 36px',
  },
  columnIcon: {
    fontSize: 10,
    color: 'var(--text-muted)',
    flexShrink: 0,
  },
  columnName: {
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-secondary)',
    flex: 1,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  columnType: {
    fontSize: 11,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-muted)',
    flexShrink: 0,
    textTransform: 'uppercase',
  },
}
