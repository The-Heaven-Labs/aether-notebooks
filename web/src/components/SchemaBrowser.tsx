import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { PanelHeader } from './PanelHeader'
import { TreeItem } from './TreeItem'

interface SchemaColumn {
  name: string
  type: string
  description?: string
}

interface SchemaTable {
  name: string
  description?: string
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
  const [searchQuery, setSearchQuery] = useState('')

  const schemaUrl = connectorId
    ? `/api/v1/connectors/${connectorId}/schema`
    : ''

  const { data, isLoading, isError } = useQuery({
    queryKey: ['connector-schema', connectorId],
    queryFn: () => api.get<SchemaResponse>(schemaUrl),
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

  const filteredTables = data?.tables?.filter(table =>
    table.name.toLowerCase().includes(searchQuery.toLowerCase())
  ) ?? []

  return (
    <div style={styles.sidebar}>
      <PanelHeader title="Schema Browser" onClose={onClose} closeTitle="Close schema browser" />

      {connectorId && (
        <div style={styles.searchContainer}>
          <input
            type="text"
            placeholder="Filter tables..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                setSearchQuery('')
              }
            }}
            style={styles.searchInput}
          />
        </div>
      )}

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
        ) : filteredTables.length === 0 ? (
          <div style={styles.placeholder}>
            <p style={styles.placeholderText}>
              {searchQuery ? 'No tables match your search' : 'No tables found'}
            </p>
          </div>
        ) : (
          <div style={styles.tree}>
            {filteredTables.map((table) => (
              <TreeItem
                key={table.name}
                name={table.name}
                description={table.description}
                columns={table.columns}
                isExpanded={expandedTables.has(table.name)}
                onToggle={toggleTable}
              />
            ))}
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
    background: 'var(--bg-primary)',
    borderRight: '1px solid var(--border)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
  },
  searchContainer: {
    padding: '8px 12px',
    borderBottom: '1px solid var(--border)',
  },
  searchInput: {
    width: '100%',
    fontSize: 12,
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: '6px 10px',
    color: 'var(--text-primary)',
    outline: 'none',
    boxSizing: 'border-box',
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
    background: 'var(--text-muted)',
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
    color: 'var(--error-full)',
  },
  tree: {
    display: 'flex',
    flexDirection: 'column',
  },
}
