import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { connectorSchemaQueryKey, getConnectorSchema } from '../api/schema'
import {
  createGrant,
  deleteGrant,
  listGrants,
  type WarehouseConnector,
  type WarehouseGrant,
  type WarehouseSubjectType,
} from '../api/warehouses'
import { ErrorBanner } from './ErrorBanner'
import { StyledTable, rowStyle, cellStyle } from './StyledTable'
import type { Group, Member } from '../types'

interface Props {
  warehouseId: string
  connectors?: WarehouseConnector[]
}

interface SubjectRow {
  key: string
  subjectType: WarehouseSubjectType
  subjectId: string
  label: string
  detail?: string
  grants: WarehouseGrant[]
}

const SUBJECT_TYPES: WarehouseSubjectType[] = ['user', 'group', 'everyone']

function isSubjectType(value: string): value is WarehouseSubjectType {
  return (SUBJECT_TYPES as string[]).includes(value)
}

function subjectKey(subjectType: WarehouseSubjectType, subjectId: string): string {
  return `${subjectType}:${subjectId}`
}

function parseSubjectKey(key: string): { subjectType: WarehouseSubjectType; subjectId: string } | null {
  const separator = key.indexOf(':')
  if (separator < 0) return null
  const subjectType = key.slice(0, separator)
  if (!isSubjectType(subjectType)) return null
  return { subjectType, subjectId: key.slice(separator + 1) }
}

export function WarehouseTableGrants({ warehouseId, connectors = [] }: Props) {
  const qc = useQueryClient()
  const [subjectSelection, setSubjectSelection] = useState('')
  const [connectorId, setConnectorId] = useState('')
  const [database, setDatabase] = useState('')
  const [table, setTable] = useState('')
  const [warnings, setWarnings] = useState<Record<string, boolean>>({})
  const [error, setError] = useState<string | null>(null)

  const { data: grants = [], isLoading, isError } = useQuery({
    queryKey: ['warehouse-grants', warehouseId],
    queryFn: () => listGrants(warehouseId),
  })

  const { data: members = [] } = useQuery({
    queryKey: ['members'],
    queryFn: () => api.get<Member[]>('/api/v1/members'),
  })

  const { data: groups = [] } = useQuery({
    queryKey: ['groups'],
    queryFn: () => api.get<Group[]>('/api/v1/groups'),
  })

  const defaultConnectorId = useMemo(() => {
    const provisioner = connectors.find((c) => c.is_provisioner)
    return provisioner?.id ?? connectors[0]?.id ?? ''
  }, [connectors])

  // A manually selected connector that is no longer linked falls back to the
  // provisioner/default instead of a dead connector.
  const selectionLinked = !connectorId || connectors.some((c) => c.id === connectorId)
  const activeConnectorId = (selectionLinked && connectorId) || defaultConnectorId

  // Clear object pickers when the effective connector changes (including when
  // the manual selection was unlinked). This is the React "adjust state while
  // rendering" pattern, not an effect.
  const [lastConnectorId, setLastConnectorId] = useState(activeConnectorId)
  if (lastConnectorId !== activeConnectorId) {
    setLastConnectorId(activeConnectorId)
    setDatabase('')
    setTable('')
    if (!selectionLinked) setConnectorId('')
  }

  const { data: schema, isLoading: schemaLoading } = useQuery({
    queryKey: connectorSchemaQueryKey(activeConnectorId),
    queryFn: () => getConnectorSchema(activeConnectorId),
    enabled: !!activeConnectorId,
  })

  const databases = useMemo(() => {
    const seen = new Set<string>()
    for (const t of schema?.tables ?? []) {
      if (t.schema) seen.add(t.schema)
    }
    return Array.from(seen).sort()
  }, [schema])

  const tables = useMemo(
    () => (schema?.tables ?? []).filter((t) => t.schema === database).map((t) => t.name).sort(),
    [schema, database],
  )

  const memberNames = useMemo(() => {
    const map = new Map<string, string>()
    for (const m of members) map.set(m.user_id, m.name || m.email)
    return map
  }, [members])

  const groupNames = useMemo(() => {
    const map = new Map<string, string>()
    for (const g of groups) map.set(g.id, g.name)
    return map
  }, [groups])

  function subjectLabel(
    subjectType: WarehouseSubjectType,
    subjectId: string,
    grant?: WarehouseGrant,
  ): string {
    if (subjectType === 'everyone') return 'Everyone'
    if (subjectType === 'user') {
      return memberNames.get(subjectId) ?? grant?.subject_name ?? grant?.subject_email ?? subjectId
    }
    return groupNames.get(subjectId) ?? grant?.subject_name ?? subjectId
  }

  const rows = useMemo<SubjectRow[]>(() => {
    const byKey = new Map<string, SubjectRow>()
    for (const grant of grants) {
      const key = subjectKey(grant.subject_type, grant.subject_id)
      let row = byKey.get(key)
      if (!row) {
        const label =
          grant.subject_type === 'everyone'
            ? 'Everyone'
            : grant.subject_type === 'user'
              ? memberNames.get(grant.subject_id) ??
                grant.subject_name ??
                grant.subject_email ??
                grant.subject_id
              : groupNames.get(grant.subject_id) ?? grant.subject_name ?? grant.subject_id
        row = {
          key,
          subjectType: grant.subject_type,
          subjectId: grant.subject_id,
          label,
          detail: grant.subject_type === 'user' ? grant.subject_email : undefined,
          grants: [],
        }
        byKey.set(key, row)
      }
      row.grants.push(grant)
    }
    return Array.from(byKey.values())
  }, [grants, memberNames, groupNames])

  const invalidateAfterGrantChange = () => {
    qc.invalidateQueries({ queryKey: ['warehouse-grants', warehouseId] })
    qc.invalidateQueries({ queryKey: ['warehouse-new-tables', warehouseId] })
    qc.invalidateQueries({ queryKey: ['warehouse-validation', warehouseId] })
    qc.invalidateQueries({ queryKey: ['warehouses'] })
    qc.invalidateQueries({ queryKey: ['warehouse', warehouseId] })
  }

  const clearWarning = (key: string) => {
    setWarnings((prev) => {
      if (!(key in prev)) return prev
      const next = { ...prev }
      delete next[key]
      return next
    })
  }

  const addGrant = useMutation({
    mutationFn: () => {
      const parsed = parseSubjectKey(subjectSelection)
      if (!parsed) throw new Error('Select a subject')
      if (!database || !table) throw new Error('Select a database and table')
      return createGrant(warehouseId, {
        subject_type: parsed.subjectType,
        subject_id: parsed.subjectId,
        database,
        table,
      })
    },
    onSuccess: (grant) => {
      const key = subjectKey(grant.subject_type, grant.subject_id)
      if (grant.warning) {
        setWarnings((prev) => ({ ...prev, [key]: true }))
      } else {
        // A warning-free response proves the subject can use a service now.
        clearWarning(key)
      }
      setTable('')
      invalidateAfterGrantChange()
      setError(null)
    },
    onError: (err: Error) => setError(err.message),
  })

  const removeGrant = useMutation({
    mutationFn: (grant: WarehouseGrant) => deleteGrant(warehouseId, grant.id),
    onSuccess: (_, grant) => {
      const key = subjectKey(grant.subject_type, grant.subject_id)
      const remaining = grants.filter(
        (g) =>
          g.subject_type === grant.subject_type &&
          g.subject_id === grant.subject_id &&
          g.id !== grant.id,
      )
      if (remaining.length === 0) clearWarning(key)
      invalidateAfterGrantChange()
      setError(null)
    },
    onError: (err: Error) => setError(err.message),
  })

  const warnedSubjects = Object.keys(warnings).map((key) => {
    const parsed = parseSubjectKey(key)
    if (!parsed) return key
    return subjectLabel(parsed.subjectType, parsed.subjectId)
  })

  const canSubmit = !!subjectSelection && !!database && !!table && !addGrant.isPending

  return (
    <section style={styles.section} aria-label="Table grants">
      <div style={styles.header}>
        <h3 style={styles.title}>Table grants</h3>
        <span style={styles.hint}>
          Tables each subject may read in this warehouse. ClickHouse enforces these grants.
        </span>
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}

      {warnedSubjects.length > 0 && (
        <div style={styles.warningBanner} role="status">
          No service access: {warnedSubjects.join(', ')} — table grants are saved, but these
          subjects cannot run queries on any warehouse service yet.
        </div>
      )}

      {isLoading ? (
        <div style={styles.loading}>Loading grants…</div>
      ) : isError ? (
        <div style={styles.errorText}>Failed to load table grants</div>
      ) : rows.length === 0 ? (
        <div style={styles.empty}>No table grants yet.</div>
      ) : (
        <div style={styles.tableWrap}>
          <StyledTable headers={['Subject', 'Tables', '']}>
            {rows.map((row) => (
              <tr key={row.key} style={rowStyle}>
                <td style={cellStyle}>
                  <span style={{ fontWeight: 600 }}>{row.label}</span>
                  {row.detail && <span style={styles.detail}>{row.detail}</span>}
                  {warnings[row.key] && (
                    <span
                      style={styles.warningBadge}
                      title="This subject cannot use any of the warehouse's services yet"
                    >
                      No service access
                    </span>
                  )}
                </td>
                <td style={cellStyle}>
                  <div style={styles.chips}>
                    {row.grants.map((grant) => (
                      <span key={grant.id} style={styles.chip}>
                        <code style={styles.chipLabel}>
                          {grant.database}.{grant.table}
                        </code>
                        <button
                          type="button"
                          style={styles.chipRemove}
                          title="Remove grant"
                          aria-label={`Remove grant ${grant.database}.${grant.table}`}
                          disabled={removeGrant.isPending}
                          onClick={() => removeGrant.mutate(grant)}
                        >
                          ×
                        </button>
                      </span>
                    ))}
                  </div>
                </td>
                <td style={styles.cellRight} />
              </tr>
            ))}
          </StyledTable>
        </div>
      )}

      <div style={styles.addForm}>
        <select
          aria-label="Subject"
          style={styles.input}
          value={subjectSelection}
          onChange={(e) => setSubjectSelection(e.target.value)}
        >
          <option value="">Select subject…</option>
          <optgroup label="Users">
            {members.map((m) => (
              <option key={m.user_id} value={subjectKey('user', m.user_id)}>
                {m.name || m.email}
              </option>
            ))}
          </optgroup>
          <optgroup label="Groups">
            {groups
              .filter((g) => !/^everyone$/i.test(g.name))
              .map((g) => (
                <option key={g.id} value={subjectKey('group', g.id)}>
                  {g.name}
                </option>
              ))}
          </optgroup>
          <option value={subjectKey('everyone', 'everyone')}>Everyone</option>
        </select>

        <select
          aria-label="Connector"
          style={styles.input}
          value={activeConnectorId}
          onChange={(e) => {
            setConnectorId(e.target.value)
            setDatabase('')
            setTable('')
          }}
        >
          {connectors.length === 0 && <option value="">No connector linked</option>}
          {connectors.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </select>

        <select
          aria-label="Database"
          style={styles.input}
          value={database}
          disabled={!activeConnectorId || schemaLoading}
          onChange={(e) => {
            setDatabase(e.target.value)
            setTable('')
          }}
        >
          <option value="">{schemaLoading ? 'Loading…' : 'Database…'}</option>
          {databases.map((db) => (
            <option key={db} value={db}>
              {db}
            </option>
          ))}
        </select>

        <select
          aria-label="Table"
          style={styles.input}
          value={table}
          disabled={!database}
          onChange={(e) => setTable(e.target.value)}
        >
          <option value="">Table…</option>
          {tables.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>

        <button
          type="button"
          style={{ ...styles.addBtn, opacity: canSubmit ? 1 : 0.5, cursor: canSubmit ? 'pointer' : 'not-allowed' }}
          disabled={!canSubmit}
          onClick={() => addGrant.mutate()}
        >
          {addGrant.isPending ? 'Adding…' : 'Add grant'}
        </button>
      </div>
      {connectors.length === 0 && (
        <p style={styles.hint}>
          Link a ClickHouse connector to this warehouse to pick tables from its schema.
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
  tableWrap: { marginBottom: 12, overflowX: 'auto' },
  warningBanner: {
    fontSize: 12,
    color: 'var(--warning-text)',
    background: 'var(--warning-light)',
    border: '1px solid var(--warning-border)',
    borderRadius: 4,
    padding: '6px 10px',
    lineHeight: 1.5,
  },
  warningBadge: {
    display: 'inline-block',
    marginLeft: 8,
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--warning-text)',
    background: 'var(--warning-light)',
    border: '1px solid var(--warning-border)',
    borderRadius: 10,
    padding: '1px 8px',
    verticalAlign: 'middle',
  },
  detail: {
    marginLeft: 8,
    fontSize: 12,
    color: 'var(--text-muted)',
  },
  chips: {
    display: 'flex',
    flexWrap: 'wrap',
    gap: 6,
  },
  chip: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 10,
    padding: '1px 4px 1px 8px',
  },
  chipLabel: {
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-primary)',
    overflowWrap: 'anywhere' as const,
  },
  chipRemove: {
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 14,
    lineHeight: 1,
    padding: '0 3px',
  },
  cellRight: {
    padding: '12px 16px',
    width: 1,
  },
  addForm: {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))',
    gap: 8,
    alignItems: 'center',
  },
  input: {
    padding: '6px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    minWidth: 0,
  },
  addBtn: {
    padding: '7px 16px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
  },
}
