import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import {
  createGrant,
  getWarehouseValidation,
  listNewTables,
  type WarehouseNewTable,
  type WarehouseSubjectType,
  type WarehouseValidation,
  type WarehouseValidationSubject,
} from '../api/warehouses'
import { ErrorBanner } from './ErrorBanner'
import { StyledTable, rowStyle, cellStyle } from './StyledTable'
import { groupLabel } from '../utils/groupLabel'
import type { Group, Member } from '../types'

interface Props {
  warehouseId: string
}

const SUBJECT_TYPES: WarehouseSubjectType[] = ['user', 'group', 'everyone']

function isSubjectType(value: string): value is WarehouseSubjectType {
  return (SUBJECT_TYPES as string[]).includes(value)
}

function parseSubjectKey(key: string): { subjectType: WarehouseSubjectType; subjectId: string } | null {
  const separator = key.indexOf(':')
  if (separator < 0) return null
  const subjectType = key.slice(0, separator)
  if (!isSubjectType(subjectType)) return null
  return { subjectType, subjectId: key.slice(separator + 1) }
}

function tableKey(table: WarehouseNewTable): string {
  return `${table.database}.${table.table}`
}

/**
 * Review surface for tables observed on the warehouse's services that have no
 * grant yet, plus the validation warnings for half-configured subjects: table
 * grants without service access and service access without table grants.
 */
export function NewTablesInbox({ warehouseId }: Props) {
  const qc = useQueryClient()
  const [selections, setSelections] = useState<Record<string, string>>({})
  const [error, setError] = useState<string | null>(null)

  const {
    data: inbox,
    isLoading,
    isError,
  } = useQuery({
    queryKey: ['warehouse-new-tables', warehouseId],
    queryFn: () => listNewTables(warehouseId),
  })

  const { data: validation } = useQuery({
    queryKey: ['warehouse-validation', warehouseId],
    queryFn: () => getWarehouseValidation(warehouseId),
  })

  const { data: members = [] } = useQuery({
    queryKey: ['members'],
    queryFn: () => api.get<Member[]>('/api/v1/members'),
  })

  const { data: groups = [] } = useQuery({
    queryKey: ['groups'],
    queryFn: () => api.get<Group[]>('/api/v1/groups'),
  })

  const memberNames = useMemo(() => {
    const map = new Map<string, string>()
    for (const m of members) map.set(m.user_id, m.name || m.email)
    return map
  }, [members])

  const groupNames = useMemo(() => {
    const map = new Map<string, string>()
    for (const g of groups) map.set(g.id, groupLabel(g))
    return map
  }, [groups])

  function warningLabel(subject: WarehouseValidationSubject): string {
    if (subject.subject_type === 'everyone') return 'Everyone'
    if (subject.subject_type === 'user') {
      return memberNames.get(subject.subject_id) ?? subject.subject_name ?? subject.subject_id
    }
    return groupNames.get(subject.subject_id) ?? subject.subject_name ?? subject.subject_id
  }

  const addGrant = useMutation({
    mutationFn: ({ table, selection }: { table: WarehouseNewTable; selection: string }) => {
      const parsed = parseSubjectKey(selection)
      if (!parsed) throw new Error('Select a subject')
      return createGrant(warehouseId, {
        subject_type: parsed.subjectType,
        subject_id: parsed.subjectId,
        database: table.database,
        table: table.table,
      })
    },
    onSuccess: (_, { table }) => {
      const key = tableKey(table)
      setSelections((prev) => {
        if (!(key in prev)) return prev
        const next = { ...prev }
        delete next[key]
        return next
      })
      qc.invalidateQueries({ queryKey: ['warehouse-new-tables', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouse-grants', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouse-validation', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouses'] })
      qc.invalidateQueries({ queryKey: ['warehouse', warehouseId] })
      setError(null)
    },
    onError: (err: Error) => setError(err.message),
  })

  const warned = hasValidationWarnings(validation)
  const pendingKey = addGrant.isPending && addGrant.variables
    ? tableKey(addGrant.variables.table)
    : null

  return (
    <section style={styles.section} aria-label="New tables">
      <div style={styles.header}>
        <h3 style={styles.title}>New tables</h3>
        <span style={styles.hint}>
          {inbox
            ? `Observed since your last grant review on ${formatTimestamp(inbox.since)}.`
            : 'Tables observed on this warehouse that have no grants yet.'}
        </span>
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}

      {warned && (
        <div style={styles.warningBanner} role="status">
          {validation?.service_access_without_tables.map((subject) => (
            <div key={`svc-${subject.subject_type}-${subject.subject_id}`}>
              <strong>{warningLabel(subject)}</strong> can connect
              {subject.services && subject.services.length > 0
                ? ` on ${subject.services.join(', ')}`
                : ''}{' '}
              but holds no table grants — every query fails.
            </div>
          ))}
          {validation?.tables_without_service_access.map((subject) => (
            <div key={`tbl-${subject.subject_type}-${subject.subject_id}`}>
              <strong>{warningLabel(subject)}</strong> holds table grants
              {subject.tables && subject.tables.length > 0
                ? ` (${subject.tables.join(', ')})`
                : ''}{' '}
              but cannot use any warehouse service.
            </div>
          ))}
        </div>
      )}

      {validation?.truncated && (
        <div style={styles.truncatedNote} role="status">
          Some validation warnings may be missing because the list is truncated. Resolve these and
          reload to see the rest.
        </div>
      )}

      {isLoading ? (
        <div style={styles.loading}>Loading new tables…</div>
      ) : isError ? (
        <div style={styles.errorText}>Failed to load new tables</div>
      ) : (inbox?.tables.length ?? 0) === 0 ? (
        <div style={styles.empty}>No new tables since your last review.</div>
      ) : (
        <>
          <div style={styles.tableWrap}>
            <StyledTable headers={['Table', 'First seen', 'Grant to', '']}>
              {inbox?.tables.map((table) => {
                const key = tableKey(table)
                const selection = selections[key] ?? ''
                const pending = pendingKey === key
                return (
                  <tr key={key} style={rowStyle}>
                    <td style={cellStyle}>
                      <code style={styles.tableName}>
                        {table.database}.{table.table}
                      </code>
                    </td>
                    <td style={styles.mutedCell}>{formatTimestamp(table.first_seen_at)}</td>
                    <td style={styles.selectCell}>
                      <select
                        aria-label={`Subject for ${key}`}
                        style={styles.input}
                        value={selection}
                        onChange={(e) =>
                          setSelections((prev) => ({ ...prev, [key]: e.target.value }))
                        }
                      >
                        <option value="">Select subject…</option>
                        <optgroup label="Users">
                          {members.map((m) => (
                            <option key={m.user_id} value={`user:${m.user_id}`}>
                              {m.name || m.email}
                            </option>
                          ))}
                        </optgroup>
                        <optgroup label="Groups">
                          {groups
                            .filter((g) => !/^everyone$/i.test(g.name))
                            .map((g) => (
                              <option key={g.id} value={`group:${g.id}`}>
                                {groupLabel(g)}
                              </option>
                            ))}
                        </optgroup>
                        <option value="everyone:everyone">Everyone</option>
                      </select>
                    </td>
                    <td style={styles.actionCell}>
                      <button
                        type="button"
                        aria-label={`Add grant for ${key}`}
                        style={{
                          ...styles.addBtn,
                          opacity: selection && !pending ? 1 : 0.5,
                          cursor: selection && !pending ? 'pointer' : 'not-allowed',
                        }}
                        disabled={!selection || pending}
                        onClick={() => addGrant.mutate({ table, selection })}
                      >
                        {pending ? 'Adding…' : 'Add grant'}
                      </button>
                    </td>
                  </tr>
                )
              })}
            </StyledTable>
          </div>
          {inbox?.truncated && (
            <div style={styles.truncatedNote} role="status">
              Showing the first {inbox.tables.length} new tables. Granting a table advances the
              review cutoff, so use the API&apos;s since parameter to list the remainder.
            </div>
          )}
        </>
      )}
    </section>
  )
}

function hasValidationWarnings(validation: WarehouseValidation | undefined): boolean {
  if (!validation) return false
  return (
    validation.service_access_without_tables.length > 0 ||
    validation.tables_without_service_access.length > 0
  )
}

function formatTimestamp(value: string): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

const styles: Record<string, React.CSSProperties> = {
  section: {
    borderTop: '1px solid var(--border)',
    paddingTop: 16,
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
  },
  header: {
    display: 'flex',
    alignItems: 'baseline',
    gap: 10,
  },
  title: {
    margin: 0,
    fontSize: 14,
    fontWeight: 700,
    color: 'var(--text-primary)',
  },
  hint: {
    fontSize: 12,
    color: 'var(--text-muted)',
    margin: 0,
  },
  loading: { fontSize: 13, color: 'var(--text-muted)', padding: '4px 0' },
  errorText: { fontSize: 13, color: 'var(--error-full)' },
  empty: { fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic', padding: '4px 0' },
  tableWrap: { overflowX: 'auto' },
  tableName: {
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-primary)',
    overflowWrap: 'anywhere' as const,
  },
  mutedCell: {
    padding: '12px 16px',
    fontSize: 12,
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap' as const,
  },
  selectCell: { padding: '8px 16px', minWidth: 180 },
  actionCell: { padding: '8px 16px', width: 1, whiteSpace: 'nowrap' as const },
  warningBanner: {
    fontSize: 12,
    color: 'var(--warning-text)',
    background: 'var(--warning-light)',
    border: '1px solid var(--warning-border)',
    borderRadius: 4,
    padding: '6px 10px',
    lineHeight: 1.6,
  },
  truncatedNote: {
    fontSize: 12,
    color: 'var(--text-muted)',
    border: '1px dashed var(--border)',
    borderRadius: 4,
    padding: '5px 10px',
    lineHeight: 1.5,
  },
  input: {
    padding: '6px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    minWidth: 0,
    width: '100%',
  },
  addBtn: {
    padding: '6px 14px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 12,
    fontWeight: 600,
  },
}
