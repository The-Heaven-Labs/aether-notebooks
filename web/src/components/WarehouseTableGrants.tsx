import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { connectorSchemaQueryKey, getConnectorSchema } from '../api/schema'
import {
  createGrant,
  deleteGrant,
  listGrants,
  type WarehouseConnector,
  type WarehouseGrant,
  type WarehouseGrantCreateResult,
  type WarehouseSubjectType,
} from '../api/warehouses'
import { ErrorBanner } from './ErrorBanner'
import { StyledTable, rowStyle, cellStyle } from './StyledTable'
import { useWarehouseTablePermissions } from '../hooks/useWarehouseTablePermissions'
import { groupLabel } from '../utils/groupLabel'
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
  const tablePermissionsEnabled = useWarehouseTablePermissions()
  const [subjectSelection, setSubjectSelection] = useState('')
  const [connectorId, setConnectorId] = useState('')
  const [database, setDatabase] = useState('')
  const [tableFilter, setTableFilter] = useState('')
  const [checkedTables, setCheckedTables] = useState<Set<string>>(new Set())
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

  // Tables already granted to the currently selected subject in the selected
  // database — rendered as checked + disabled instead of offering a duplicate.
  const grantedTables = useMemo(() => {
    const parsed = parseSubjectKey(subjectSelection)
    const set = new Set<string>()
    if (!parsed) return set
    for (const grant of grants) {
      if (
        grant.subject_type === parsed.subjectType &&
        grant.subject_id === parsed.subjectId &&
        grant.database === database
      ) {
        set.add(grant.table)
      }
    }
    return set
  }, [grants, subjectSelection, database])

  const filteredTables = useMemo(() => {
    const query = tableFilter.trim().toLowerCase()
    if (!query) return tables
    return tables.filter((t) => t.toLowerCase().includes(query))
  }, [tables, tableFilter])

  const pendingTables = useMemo(
    () => Array.from(checkedTables).filter((t) => !grantedTables.has(t)),
    [checkedTables, grantedTables],
  )

  // Switching subject or database invalidates what the checked set refers to.
  const [pickerScope, setPickerScope] = useState('')
  const currentScope = `${subjectSelection}|${database}`
  if (pickerScope !== currentScope) {
    setPickerScope(currentScope)
    if (checkedTables.size > 0) setCheckedTables(new Set())
    if (tableFilter) setTableFilter('')
  }

  const toggleTable = (name: string) => {
    setCheckedTables((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const selectAllVisible = () => {
    setCheckedTables((prev) => {
      const next = new Set(prev)
      for (const t of filteredTables) {
        if (!grantedTables.has(t)) next.add(t)
      }
      return next
    })
  }

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

  const addGrants = useMutation({
    mutationFn: async () => {
      const parsed = parseSubjectKey(subjectSelection)
      if (!parsed) throw new Error('Select a subject')
      if (!database) throw new Error('Select a database')
      const targets = Array.from(checkedTables).filter((t) => !grantedTables.has(t))
      if (targets.length === 0) throw new Error('Select at least one table')
      // The endpoint is idempotent per grant, so allSettled retries are safe
      // and one bad table name does not abort the rest.
      const settled = await Promise.allSettled(
        targets.map((table) =>
          createGrant(warehouseId, {
            subject_type: parsed.subjectType,
            subject_id: parsed.subjectId,
            database,
            table,
          }),
        ),
      )
      const fulfilled = settled
        .filter((r): r is PromiseFulfilledResult<WarehouseGrantCreateResult> => r.status === 'fulfilled')
        .map((r) => r.value)
      const failures = settled
        .filter((r): r is PromiseRejectedResult => r.status === 'rejected')
        .map((r) => (r.reason instanceof Error ? r.reason.message : String(r.reason)))
      return { parsed, fulfilled, failures, requested: targets.length }
    },
    onSuccess: ({ parsed, fulfilled, failures, requested }) => {
      const key = subjectKey(parsed.subjectType, parsed.subjectId)
      if (fulfilled.length > 0) {
        if (fulfilled.some((g) => g.warning)) {
          setWarnings((prev) => ({ ...prev, [key]: true }))
        } else {
          // A warning-free response proves the subject can use a service now.
          clearWarning(key)
        }
      }
      setCheckedTables(new Set())
      invalidateAfterGrantChange()
      if (failures.length > 0) {
        setError(`${failures.length} of ${requested} grants failed: ${failures.join('; ')}`)
      } else {
        setError(null)
      }
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

  const canSubmit = !!subjectSelection && !!database && pendingTables.length > 0 && !addGrants.isPending

  return (
    <section style={styles.section} aria-label="Table grants">
      <div style={styles.header}>
        <h3 style={styles.title}>Table grants</h3>
        <span style={styles.hint}>
          {tablePermissionsEnabled
            ? 'Tables each subject may read in this warehouse. ClickHouse enforces these grants.'
            : 'Tables each subject may read in this warehouse. ClickHouse table permissions are disabled, so these grants are not enforced yet.'}
        </span>
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}

      {warnedSubjects.length > 0 && (
        <div style={styles.warningBanner} role="status">
          No service access: {warnedSubjects.join(', ')} — table grants are saved, but these
          subjects cannot run queries on any warehouse service yet.
          {defaultConnectorId && (
            <>
              {' '}
              <Link style={styles.warningLink} to={`/connectors?permissions=${defaultConnectorId}`}>
                Manage service access
              </Link>
            </>
          )}
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
        <label style={styles.field}>
          <span style={styles.fieldLabel}>Subject</span>
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
                    {groupLabel(g)}
                  </option>
                ))}
            </optgroup>
            <option value={subjectKey('everyone', 'everyone')}>Everyone</option>
          </select>
        </label>

        {connectors.length > 1 && (
          <label style={styles.field}>
            <span style={styles.fieldLabel}>Schema source</span>
            <select
              aria-label="Schema source"
              style={styles.input}
              value={activeConnectorId}
              onChange={(e) => {
                setConnectorId(e.target.value)
                setDatabase('')
              }}
            >
              {connectors.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </label>
        )}

        <label style={styles.field}>
          <span style={styles.fieldLabel}>Database</span>
          <select
            aria-label="Database"
            style={styles.input}
            value={database}
            disabled={!activeConnectorId || schemaLoading}
            onChange={(e) => setDatabase(e.target.value)}
          >
            <option value="">{schemaLoading ? 'Loading…' : 'Database…'}</option>
            {databases.map((db) => (
              <option key={db} value={db}>
                {db}
              </option>
            ))}
          </select>
        </label>

        <button
          type="button"
          style={{ ...styles.addBtn, opacity: canSubmit ? 1 : 0.5, cursor: canSubmit ? 'pointer' : 'not-allowed' }}
          disabled={!canSubmit}
          onClick={() => addGrants.mutate()}
        >
          {addGrants.isPending
            ? 'Adding…'
            : pendingTables.length > 1
              ? `Add ${pendingTables.length} grants`
              : 'Add grant'}
        </button>
      </div>

      <p style={styles.hint}>
        Grants apply to the whole warehouse regardless of service; services are controlled via
        connector permissions (ACL).
        {connectors.length === 1 && ` Tables are browsed from ${connectors[0].name}.`}
      </p>

      {database && (
        <div style={styles.checklistWrap}>
          <div style={styles.checklistHeader}>
            <input
              aria-label="Filter tables"
              style={{ ...styles.input, flex: 1, minWidth: 140 }}
              placeholder={`Filter ${tables.length} tables…`}
              value={tableFilter}
              onChange={(e) => setTableFilter(e.target.value)}
            />
            <button
              type="button"
              style={styles.linkBtn}
              onClick={selectAllVisible}
              disabled={filteredTables.every((t) => grantedTables.has(t))}
            >
              Select all
            </button>
            <button
              type="button"
              style={styles.linkBtn}
              onClick={() => setCheckedTables(new Set())}
              disabled={checkedTables.size === 0}
            >
              Clear
            </button>
            <span style={styles.checkedCount}>{checkedTables.size} selected</span>
          </div>
          <div style={styles.checklist} role="group" aria-label="Tables">
            {filteredTables.map((t) => {
              const granted = grantedTables.has(t)
              return (
                <label
                  key={t}
                  style={granted ? { ...styles.checkItem, ...styles.checkItemGranted } : styles.checkItem}
                >
                  <input
                    type="checkbox"
                    aria-label={t}
                    checked={granted || checkedTables.has(t)}
                    disabled={granted}
                    onChange={() => toggleTable(t)}
                  />
                  <code style={styles.checkTableName}>{t}</code>
                  {granted && <span style={styles.grantedBadge}>already granted</span>}
                </label>
              )
            })}
            {filteredTables.length === 0 && (
              <div style={styles.empty}>No tables match this filter.</div>
            )}
          </div>
        </div>
      )}

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
  empty: { fontSize: 13, color: 'var(--text-secondary)', fontStyle: 'italic', padding: '4px 0' },
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
  warningLink: {
    color: 'var(--warning-text)',
    fontWeight: 600,
    textDecoration: 'underline',
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
    alignItems: 'end',
  },
  field: {
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
    minWidth: 0,
  },
  fieldLabel: {
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-secondary)',
  },
  checklistWrap: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: 8,
    background: 'var(--bg-secondary)',
  },
  checklistHeader: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
  },
  checklist: {
    display: 'flex',
    flexDirection: 'column',
    maxHeight: 220,
    overflowY: 'auto',
    border: '1px solid var(--border-light)',
    borderRadius: 4,
    background: 'var(--bg-card)',
  },
  checkItem: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '4px 10px',
    cursor: 'pointer',
  },
  checkItemGranted: {
    cursor: 'default',
    opacity: 0.75,
  },
  checkTableName: {
    fontSize: 12,
    fontFamily: 'var(--font-mono)',
    color: 'var(--text-primary)',
    overflowWrap: 'anywhere' as const,
  },
  grantedBadge: {
    marginLeft: 'auto',
    fontSize: 10,
    fontWeight: 600,
    color: 'var(--text-muted)',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 10,
    padding: '1px 8px',
    whiteSpace: 'nowrap' as const,
  },
  checkedCount: {
    fontSize: 11,
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap' as const,
  },
  linkBtn: {
    background: 'none',
    border: 'none',
    padding: 0,
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--accent)',
    cursor: 'pointer',
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
