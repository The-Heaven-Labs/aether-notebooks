import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { connectorSchemaQueryKey, getConnectorSchema } from '../api/schema'
import { listGrants, updateWarehouse, type WarehouseConnector } from '../api/warehouses'
import { ErrorBanner } from './ErrorBanner'

interface Props {
  warehouseId: string
  patterns: string[]
  connectors?: WarehouseConnector[]
}

export function WarehouseHiddenTables({ warehouseId, patterns, connectors = [] }: Props) {
  const qc = useQueryClient()
  const [input, setInput] = useState('')
  const [error, setError] = useState<string | null>(null)

  const sourceConnectorId = useMemo(() => {
    const provisioner = connectors.find((c) => c.is_provisioner)
    return provisioner?.id ?? connectors[0]?.id ?? ''
  }, [connectors])

  const { data: schema } = useQuery({
    queryKey: connectorSchemaQueryKey(sourceConnectorId),
    queryFn: () => getConnectorSchema(sourceConnectorId),
    enabled: !!sourceConnectorId,
  })

  const { data: grants = [] } = useQuery({
    queryKey: ['warehouse-grants', warehouseId],
    queryFn: () => listGrants(warehouseId),
  })

  const hiddenCount = useMemo(() => {
    const compiled: RegExp[] = []
    for (const pattern of patterns) {
      try {
        compiled.push(new RegExp(pattern))
      } catch {
        // Invalid patterns are rejected by the server; ignore stale data.
      }
    }
    if (compiled.length === 0) return 0
    const granted = new Set(grants.map((g) => `${g.database}.${g.table}`))
    let count = 0
    for (const table of schema?.tables ?? []) {
      const name = table.schema ? `${table.schema}.${table.name}` : table.name
      if (granted.has(name)) continue
      if (compiled.some((re) => re.test(name))) count++
    }
    return count
  }, [patterns, grants, schema])

  const save = useMutation({
    mutationFn: (next: string[]) => updateWarehouse(warehouseId, { hidden_table_patterns: next }),
    onSuccess: () => {
      setError(null)
      setInput('')
      qc.invalidateQueries({ queryKey: ['warehouse', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouses'] })
      qc.invalidateQueries({ queryKey: ['warehouse-new-tables', warehouseId] })
      for (const connector of connectors) {
        qc.invalidateQueries({ queryKey: connectorSchemaQueryKey(connector.id) })
      }
    },
    onError: (err: Error) => setError(err.message),
  })

  const addPattern = () => {
    const trimmed = input.trim()
    if (!trimmed) return
    if (patterns.includes(trimmed)) {
      setInput('')
      return
    }
    save.mutate([...patterns, trimmed])
  }

  const removePattern = (pattern: string) => {
    save.mutate(patterns.filter((p) => p !== pattern))
  }

  return (
    <section style={styles.section} aria-label="Hidden tables">
      <div style={styles.header}>
        <h3 style={styles.title}>Hidden tables</h3>
        <span style={styles.hint}>
          Go regex matched against <code>database.table</code>. Matching tables are hidden from the
          picker and inbox unless already granted.
        </span>
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}

      {patterns.length > 0 ? (
        <div style={styles.chips}>
          {patterns.map((pattern) => (
            <span key={pattern} style={styles.chip}>
              <code style={styles.chipLabel}>{pattern}</code>
              <button
                type="button"
                style={styles.chipRemove}
                title="Remove pattern"
                aria-label={`Remove pattern ${pattern}`}
                disabled={save.isPending}
                onClick={() => removePattern(pattern)}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      ) : (
        <div style={styles.empty}>No hidden patterns.</div>
      )}

      <div style={styles.addRow}>
        <input
          aria-label="Pattern"
          style={styles.input}
          placeholder="e.g. ^analytics\._tmp"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') addPattern()
          }}
        />
        <button
          type="button"
          style={{ ...styles.addBtn, opacity: input.trim() && !save.isPending ? 1 : 0.5 }}
          disabled={!input.trim() || save.isPending}
          onClick={addPattern}
        >
          {save.isPending ? 'Saving…' : 'Add'}
        </button>
      </div>

      {patterns.length > 0 && schema && (
        <p style={styles.hint}>
          {hiddenCount === 1 ? 'hides 1 table' : `hides ${hiddenCount} tables`} in the current schema
          source.
        </p>
      )}
    </section>
  )
}

const styles: Record<string, React.CSSProperties> = {
  section: {
    borderTop: '1px solid var(--border)',
    paddingTop: 16,
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
  },
  header: { display: 'flex', alignItems: 'baseline', gap: 10, flexWrap: 'wrap' },
  title: { margin: 0, fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' },
  hint: { fontSize: 12, color: 'var(--text-muted)', margin: 0 },
  empty: { fontSize: 13, color: 'var(--text-secondary)', fontStyle: 'italic', padding: '4px 0' },
  chips: { display: 'flex', flexWrap: 'wrap', gap: 6 },
  chip: {
    display: 'inline-flex', alignItems: 'center', gap: 4, background: 'var(--bg-secondary)',
    border: '1px solid var(--border)', borderRadius: 10, padding: '1px 4px 1px 8px',
  },
  chipLabel: {
    fontSize: 12, fontFamily: 'var(--font-mono)', color: 'var(--text-primary)',
    overflowWrap: 'anywhere' as const,
  },
  chipRemove: {
    background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)',
    fontSize: 14, lineHeight: 1, padding: '0 3px',
  },
  addRow: { display: 'flex', gap: 8, alignItems: 'center', maxWidth: 460 },
  input: {
    flex: 1, padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4,
    fontSize: 13, background: 'var(--bg-input)', color: 'var(--text-primary)', minWidth: 0,
  },
  addBtn: {
    padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none',
    borderRadius: 4, fontSize: 13, fontWeight: 600,
  },
}
