import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { X, ChevronDown, ChevronRight, Table2, Columns } from 'lucide-react'

interface SchemaColumn {
  name: string
  type: string
}

interface SchemaTable {
  name: string
  columns: SchemaColumn[]
}

interface SchemaResponse {
  tables: SchemaTable[]
}

interface Props {
  connectorId: string | null
  onClose: () => void
}

export function SchemaBrowser({ connectorId, onClose }: Props) {
  const [expandedTables, setExpandedTables] = useState<Set<string>>(new Set())

  const { data, isLoading, isError } = useQuery({
    queryKey: ['connector-schema', connectorId],
    queryFn: () => api.get<SchemaResponse>(`/api/v1/connectors/${connectorId}/schema`),
    enabled: !!connectorId,
  })

  const toggleTable = (tableName: string) => {
    setExpandedTables((prev) => {
      const next = new Set(prev)
      if (next.has(tableName)) {
        next.delete(tableName)
      } else {
        next.add(tableName)
      }
      return next
    })
  }

  return (
    <div style={styles.sidebar}>
      <div style={styles.header}>
        <span style={styles.headerTitle}>Schema Browser</span>
        <button style={{ ...styles.closeBtn, display: 'flex', alignItems: 'center' }} onClick={onClose} title="Close schema browser">
          <X size={13} />
        </button>
      </div>

      <div style={styles.content}>
        {!connectorId ? (
          <div style={styles.placeholder}>
            <span style={styles.placeholderIcon}>🗄️</span>
            <p style={styles.placeholderText}>Select a connector to browse its schema</p>
          </div>
        ) : isLoading ? (
          <div style={styles.loadingArea}>
            <div style={styles.loadingDot} />
            <span style={styles.loadingText}>Loading schema…</span>
          </div>
        ) : isError ? (
          <div style={styles.errorArea}>
            <span style={styles.errorText}>Failed to load schema</span>
          </div>
        ) : !data || !data.tables || data.tables.length === 0 ? (
          <div style={styles.placeholder}>
            <p style={styles.placeholderText}>No tables found</p>
          </div>
        ) : (
          <div style={styles.tree}>
            {data.tables.map((table) => {
              const isExpanded = expandedTables.has(table.name)
              return (
                <div key={table.name} style={styles.tableItem}>
                  <button
                    style={styles.tableRow}
                    onClick={() => toggleTable(table.name)}
                    title={table.name}
                  >
                    <span style={styles.tableChevron}>{isExpanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}</span>
                    <span style={styles.tableIcon}><Table2 size={12} /></span>
                    <span style={styles.tableName}>{table.name}</span>
                    <span style={styles.columnCount}>{table.columns.length}</span>
                  </button>
                  {isExpanded && (
                    <div style={styles.columnList}>
                      {table.columns.map((col) => (
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
            })}
          </div>
        )}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  sidebar: {
    width: 280,
    flexShrink: 0,
    background: 'var(--bg-secondary)',
    borderRight: '1px solid var(--border)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '10px 14px',
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  headerTitle: {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
  },
  closeBtn: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 12,
    padding: '2px 4px',
    borderRadius: 4,
    lineHeight: 1,
  },
  content: {
    flex: 1,
    overflowY: 'auto',
    padding: '8px 0',
  },
  placeholder: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '40px 20px',
    gap: 12,
  },
  placeholderIcon: {
    fontSize: 28,
    opacity: 0.5,
  },
  placeholderText: {
    fontSize: 12,
    color: 'var(--text-muted)',
    textAlign: 'center',
    margin: 0,
    lineHeight: 1.5,
  },
  loadingArea: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    padding: '24px 16px',
  },
  loadingDot: {
    width: 7,
    height: 7,
    borderRadius: '50%',
    background: 'var(--accent)',
    opacity: 0.5,
    flexShrink: 0,
  },
  loadingText: {
    fontSize: 12,
    color: 'var(--text-muted)',
  },
  errorArea: {
    padding: '24px 16px',
  },
  errorText: {
    fontSize: 12,
    color: '#c0392b',
  },
  tree: {
    display: 'flex',
    flexDirection: 'column',
  },
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
