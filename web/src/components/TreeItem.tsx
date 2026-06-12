import { ChevronDown, ChevronRight, Table2, Columns } from 'lucide-react'

interface Column {
  name: string
  type: string
  description?: string
}

interface Props {
  name: string
  description?: string
  columns: Column[]
  isExpanded: boolean
  onToggle: (name: string) => void
}

export function TreeItem({ name, description, columns, isExpanded, onToggle }: Props) {
  const tableTitle = description ? `${name} — ${description}` : name
  
  return (
    <div style={styles.tableItem}>
      <button style={styles.tableRow} onClick={() => onToggle(name)} title={tableTitle}>
        <span style={styles.tableChevron}>{isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}</span>
        <span style={styles.tableIcon}><Table2 size={12} /></span>
        <span style={styles.tableName}>{name}</span>
        <span style={styles.columnCount}>{columns.length}</span>
      </button>
      {description && isExpanded && (
        <div style={styles.descriptionRow}>
          <span style={styles.descriptionText}>{description}</span>
        </div>
      )}
      {isExpanded && (
        <div style={styles.columnList}>
          {columns.map((col) => (
            <div key={col.name} style={styles.columnItem}>
              <div style={styles.columnRow}>
                <span style={{ ...styles.columnIcon, display: 'flex', alignItems: 'center' }}><Columns size={12} /></span>
                <span style={styles.columnName}>{col.name}</span>
                <span style={styles.columnType}>{col.type}</span>
              </div>
              {col.description && (
                <div style={styles.columnDescriptionRow}>
                  <span style={styles.columnDescription}>{col.description}</span>
                </div>
              )}
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
    borderRadius: 4,
    padding: '1px 6px',
    flexShrink: 0,
  },
  columnList: {
    display: 'flex',
    flexDirection: 'column',
    paddingBottom: 4,
  },
  descriptionRow: {
    padding: '2px 12px 2px 36px',
  },
  descriptionText: {
    fontSize: 11,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
    lineHeight: 1.4,
  },
  columnItem: {
    display: 'flex',
    flexDirection: 'column',
  },
  columnRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '3px 12px 1px 36px',
  },
  columnDescriptionRow: {
    padding: '0px 12px 2px 36px',
  },
  columnDescription: {
    fontSize: 10,
    color: 'var(--text-muted)',
    fontStyle: 'italic',
    lineHeight: 1.3,
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
