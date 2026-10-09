import { useId, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { connectorSchemaQueryKey, getConnectorSchema } from '../api/schema'
import {
  updateWarehouse,
  type Warehouse,
  type WarehouseConnector,
} from '../api/warehouses'
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
  const hintId = useId()

  const sourceConnectorId = useMemo(() => {
    const provisioner = connectors.find((c) => c.is_provisioner)
    return provisioner?.id ?? connectors[0]?.id ?? ''
  }, [connectors])

  const { data: schema } = useQuery({
    queryKey: connectorSchemaQueryKey(sourceConnectorId),
    queryFn: () => getConnectorSchema(sourceConnectorId),
    enabled: !!sourceConnectorId,
  })

  // The server computes the count before dropping hidden tables: the response
  // no longer carries them, so the client cannot derive it.
  const hiddenCount = schema?.hidden_tables ?? 0

  const save = useMutation({
    scope: { id: `warehouse-hidden-patterns-${warehouseId}` },
    mutationFn: (next: string[]) => updateWarehouse(warehouseId, { hidden_table_patterns: next }),
    onSuccess: (updated) => {
      setError(null)
      setInput('')
      // The PUT response omits `connectors` (GET-only); merge so the cached
      // detail keeps the linked connectors the settings page renders.
      qc.setQueryData<Warehouse>(['warehouse', warehouseId], (prev) =>
        prev
          ? { ...prev, ...updated, connectors: updated.connectors ?? prev.connectors }
          : updated,
      )
      qc.invalidateQueries({ queryKey: ['warehouse', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouses'] })
      qc.invalidateQueries({ queryKey: ['warehouse-new-tables', warehouseId] })
      for (const connector of connectors) {
        qc.invalidateQueries({ queryKey: connectorSchemaQueryKey(connector.id) })
      }
    },
    onError: (err: Error) => setError(err.message),
  })

  // Enter/Add commits every non-empty line as one batch. Lines are trimmed,
  // empties dropped, and duplicates (against existing patterns and within the
  // batch) ignored — first occurrence wins. All-duplicate input clears the
  // field without a request, matching the single-pattern behavior.
  const addPatterns = () => {
    if (save.isPending) return
    const next = [...patterns]
    for (const line of input.split(/\r?\n/)) {
      const trimmed = line.trim()
      if (trimmed && !next.includes(trimmed)) next.push(trimmed)
    }
    if (next.length === patterns.length) {
      setInput('')
      return
    }
    save.mutate(next)
  }

  const removePattern = (pattern: string) => {
    if (save.isPending) return
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
        <textarea
          aria-label="Pattern"
          aria-describedby={hintId}
          style={styles.textarea}
          placeholder={'e.g. ^analytics\\._tmp\n       ^raw\\.old$'}
          value={input}
          disabled={save.isPending}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              addPatterns()
            }
          }}
        />
        <button
          type="button"
          style={{ ...styles.addBtn, opacity: input.trim() && !save.isPending ? 1 : 0.5 }}
          disabled={!input.trim() || save.isPending}
          onClick={addPatterns}
        >
          {save.isPending ? 'Saving…' : 'Add'}
        </button>
      </div>
      <p id={hintId} style={styles.keyHint}>
        One pattern per line · <kbd style={styles.kbd}>Enter</kbd> adds ·{' '}
        <kbd style={styles.kbd}>Shift+Enter</kbd> for a new line
      </p>

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
  addRow: { display: 'flex', gap: 8, alignItems: 'flex-end', maxWidth: 460 },
  textarea: {
    flex: 1, minHeight: 60, padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 6,
    fontSize: 12, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)',
    color: 'var(--text-primary)', resize: 'vertical', minWidth: 0,
  },
  keyHint: {
    fontSize: 11, color: 'var(--text-muted)', margin: 0, display: 'flex',
    alignItems: 'center', gap: 4, flexWrap: 'wrap',
  },
  kbd: {
    fontFamily: 'var(--font-mono)', fontSize: 10, background: 'var(--bg-secondary)',
    border: '1px solid var(--border)', borderRadius: 3, padding: '1px 5px',
    color: 'var(--text-primary)',
  },
  addBtn: {
    padding: '7px 16px', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none',
    borderRadius: 4, fontSize: 13, fontWeight: 600,
  },
}
