import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import { DatabasePicker } from './DatabasePicker'
import { PanelHeader } from './PanelHeader'
import { TreeItem } from './TreeItem'
import type { Connector } from '../types'

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
  connector?: Connector | null
  onClose: () => void
}

export function SchemaBrowser({ connectorId, connector, onClose }: Props) {
  const [expandedTables, setExpandedTables] = useState<Set<string>>(new Set())
  const [activeDatabase, setActiveDatabase] = useState<string | null>(null)

  const hasFixedDatabase = !!connector?.config?.database
  const schemaUrl = connectorId
    ? activeDatabase && !hasFixedDatabase
      ? `/api/v1/connectors/${connectorId}/schema?database=${encodeURIComponent(activeDatabase)}`
      : `/api/v1/connectors/${connectorId}/schema`
    : ''

  const { data, isLoading, isError } = useQuery({
    queryKey: ['connector-schema', connectorId, activeDatabase],
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

  return (
    <div style={styles.sidebar}>
      <PanelHeader title="Schema Browser" onClose={onClose} closeTitle="Close schema browser" />

      {connectorId && !hasFixedDatabase && (
        <DatabasePicker
          connectorId={connectorId}
          value={activeDatabase}
          onChange={setActiveDatabase}
        />
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
        ) : !data || !data.tables || data.tables.length === 0 ? (
          <div style={styles.placeholder}>
            <p style={styles.placeholderText}>No tables found</p>
          </div>
        ) : (
          <div style={styles.tree}>
            {data.tables.map((table) => (
              <TreeItem
                key={table.name}
                name={table.name}
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
    background: 'var(--bg-secondary)',
    borderRight: '1px solid var(--border)',
    display: 'flex',
    flexDirection: 'column',
    overflow: 'hidden',
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
    color: 'var(--error-full)',
  },
  tree: {
    display: 'flex',
    flexDirection: 'column',
  },
}
