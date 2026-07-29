import { useState, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Connector } from '../types'
import { AppShell } from '../components/AppShell'
import { Check, X, Loader2, Star, Database } from 'lucide-react'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { FormCard } from '../components/FormCard'
import { StatusBadge } from '../components/StatusBadge'
import { SectionHeader } from '../components/SectionHeader'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { EmptyState } from '../components/EmptyState'
import { ConfirmDialog } from '../components/ConfirmDialog'

type ConnectorType = 'postgres' | 'clickhouse' | 'opensearch'

interface ConnectorForm {
  name: string
  type: ConnectorType
  host: string
  port: string
  database: string
  user: string
  password: string
  ssl_mode: string
  use_tls: boolean
  is_default: boolean
  timeout_seconds: string
  table_allowlist: string
  table_denylist: string
}

const defaultForm = (): ConnectorForm => ({
  name: '', type: 'postgres', host: 'localhost', port: '5432',
  database: '', user: '', password: '', ssl_mode: 'disable',
  use_tls: false, is_default: false, timeout_seconds: '0',
  table_allowlist: '', table_denylist: '',
})

export function ConnectorsPage() {
  useEffect(() => { document.title = "Connectors — Aether Notebooks" }, [])
  const qc = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<ConnectorForm>(defaultForm())
  const [form, setForm] = useState<ConnectorForm>(defaultForm())
  const [testResults, setTestResults] = useState<Record<string, { ok: boolean; error?: string }>>({})
  const [testingIds, setTestingIds] = useState<Record<string, boolean>>({})
  const [createError, setCreateError] = useState<string | null>(null)
  const [editError, setEditError] = useState<string | null>(null)
  const [formTest, setFormTest] = useState<{ ok: boolean; error?: string } | null>(null)
  const [formTesting, setFormTesting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [permissionsTarget, setPermissionsTarget] = useState<{ type: 'connector'; id: string; name: string } | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Connector | null>(null)

  const { data: connectors = [], isLoading } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

  const [autoTested, setAutoTested] = useState(false)

  useEffect(() => {
    if (connectors.length > 0 && !autoTested) {
      setAutoTested(true)
      connectors.forEach(c => testConnector(c.id))
    }
  }, [connectors, autoTested])

  useEffect(() => {
    const editId = searchParams.get('edit')
    if (editId && connectors.length > 0) {
      const c = connectors.find(x => x.id === editId)
      if (c) {
        setEditing(c.id)
        setEditForm({
          name: c.name,
          type: c.type as ConnectorType,
          host: c.config?.host ?? '',
          port: String(c.config?.port ?? 5432),
          database: c.config?.database ?? '',
          user: c.config?.user ?? '',
          password: '',
          ssl_mode: c.config?.ssl_mode ?? 'disable',
          use_tls: c.config?.use_tls ?? false,
          is_default: c.is_default ?? false,
          timeout_seconds: String(c.timeout_seconds ?? 0),
          table_allowlist: (c.table_allowlist ?? []).join('\n'),
          table_denylist: (c.table_denylist ?? []).join('\n'),
        })
        setSearchParams({})
      }
    }
  }, [searchParams, connectors, setSearchParams])

  const updateConnector = useMutation({
    mutationFn: (id: string) => api.put<Connector>(`/api/v1/connectors/${id}`, {
      name: editForm.name,
      timeout_seconds: parseInt(editForm.timeout_seconds) || 0,
      config: {
        host: editForm.host,
        port: parseInt(editForm.port),
        database: editForm.database,
        user: editForm.user,
        ...(editForm.password !== '' ? { password: editForm.password } : {}),
        ssl_mode: editForm.ssl_mode,
        ...(editForm.type === 'opensearch' ? { use_tls: editForm.use_tls } : {}),
      },
      ...(editForm.is_default ? { is_default: true } : {}),
      table_allowlist: editForm.table_allowlist.split('\n').filter(s => s.trim()),
      table_denylist: editForm.table_denylist.split('\n').filter(s => s.trim()),
    }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['connectors'] })
      setEditing(null)
      setEditForm(defaultForm())
      setEditError(null)
    },
    onError: (e: Error) => setEditError(e.message),
  })

  const createConnector = useMutation({
    mutationFn: () => api.post<Connector>('/api/v1/connectors', {
      name: form.name,
      type: form.type,
      is_default: form.is_default,
      timeout_seconds: parseInt(form.timeout_seconds) || 0,
      config: {
        host: form.host,
        port: parseInt(form.port),
        database: form.database,
        user: form.user,
        password: form.password,
        ssl_mode: form.ssl_mode,
        ...(form.type === 'opensearch' ? { use_tls: form.use_tls } : {}),
      },
    }),
    onSuccess: (connector) => {
      qc.invalidateQueries({ queryKey: ['connectors'] })
      if (formTest) setTestResults((prev) => ({ ...prev, [connector.id]: formTest }))
      setCreating(false)
      setForm(defaultForm())
      setFormTest(null)
      setCreateError(null)
    },
    onError: (err: Error) => setCreateError(err.message),
  })

  const deleteConnector = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/connectors/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['connectors'] }),
    onError: (err: Error) => setDeleteError(err.message),
  })

  const setDefault = useMutation({
    mutationFn: (id: string) => api.put(`/api/v1/connectors/${id}/default`, {}),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['connectors'] }),
  })

  const testConnector = async (id: string) => {
    setTestResults((prev) => ({ ...prev, [id]: undefined as unknown as { ok: boolean; error?: string } }))
    setTestingIds((prev) => ({ ...prev, [id]: true }))
    try {
      const result = await api.post<{ ok: boolean; error?: string }>(`/api/v1/connectors/${id}/test`, {})
      setTestResults((prev) => ({ ...prev, [id]: result }))
    } catch (e) {
      setTestResults((prev) => ({ ...prev, [id]: { ok: false, error: String(e) } }))
    } finally {
      setTestingIds((prev) => { const n = { ...prev }; delete n[id]; return n })
    }
  }

  const testFormConnection = async () => {
    setFormTesting(true)
    setFormTest(null)
    try {
      const result = await api.post<{ ok: boolean; error?: string }>('/api/v1/connectors/test', {
        type: form.type,
        config: {
          host: form.host,
          port: parseInt(form.port),
          database: form.database,
          user: form.user,
          password: form.password,
          ssl_mode: form.ssl_mode,
        },
      })
      setFormTest(result)
    } catch {
      setFormTest({ ok: false, error: 'Request failed' })
    } finally {
      setFormTesting(false)
    }
  }

  const setField = (field: keyof ConnectorForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm((f) => ({ ...f, [field]: e.target.value }))

  return (
    <AppShell>
      <div style={styles.body}>
        {!creating && (
          <>
          <SectionHeader title="Connectors" subtitle={connectors.length > 0 ? `${connectors.length} connector${connectors.length !== 1 ? 's' : ''}` : ''}>
            <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Connector</button>
          </SectionHeader>
          <p style={{ fontSize: 13, color: 'var(--text-muted)', marginTop: -16, marginBottom: 24 }}>
            Connect to your databases (PostgreSQL, ClickHouse, OpenSearch) to query data from notebooks.
          </p>
          </>
        )}
        {creating && (
          <FormCard title="New Connector">
            <div style={styles.formGrid}>
              <label style={styles.label}>Name
                <input style={styles.input} value={form.name} onChange={setField('name')} placeholder="My Postgres" />
              </label>
              <label style={styles.label}>Type
                <select style={styles.input} value={form.type} onChange={(e) => setForm((f) => ({
                  ...f, type: e.target.value as ConnectorType,
                  port: e.target.value === 'clickhouse' ? '9000' : e.target.value === 'opensearch' ? '9200' : '5432',
                }))}>
                  <option value="postgres">PostgreSQL</option>
                  <option value="clickhouse">ClickHouse</option>
                  <option value="opensearch">OpenSearch</option>
                </select>
              </label>
              <label style={styles.label}>Host
                <input style={styles.input} value={form.host} onChange={setField('host')} />
              </label>
              <label style={styles.label}>Port
                <input style={styles.input} type="text" value={form.port} onChange={setField('port')} />
              </label>
              {(form.type === 'postgres' || form.type === 'clickhouse') && (
                <label style={styles.label}>Database
                  <input style={styles.input} value={form.database} onChange={setField('database')} />
                </label>
              )}
              <label style={styles.label}>User
                <input style={styles.input} value={form.user} onChange={setField('user')} />
              </label>
              <label style={styles.label}>Password
                <input style={styles.input} type="password" value={form.password} onChange={setField('password')} />
              </label>
              {form.type === 'opensearch' && (
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
                  <input type="checkbox" checked={form.use_tls}
                    onChange={e => setForm(f => ({ ...f, use_tls: e.target.checked }))} />
                  Use TLS (HTTPS)
                </label>
              )}
              {(form.type === 'postgres' || form.type === 'clickhouse') && (
                <label style={styles.label}>SSL Mode
                  <select style={styles.input} value={form.ssl_mode} onChange={setField('ssl_mode')}>
                    <option value="disable">disable</option>
                    <option value="require">require</option>
                    <option value="verify-full">verify-full</option>
                  </select>
                </label>
              )}
              <label style={styles.label}>Query Timeout (s)
                <input style={styles.input} type="text" value={form.timeout_seconds}
                  onChange={(e) => setForm(f => ({ ...f, timeout_seconds: e.target.value }))} placeholder="0 = unlimited" />
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)', gridColumn: '1 / -1' }}>
                <input type="checkbox" checked={form.is_default ?? false}
                  onChange={e => setForm(f => ({ ...f, is_default: e.target.checked }))} />
                Set as default connector for new notebooks
              </label>
            </div>
            <div style={styles.formActions}>
              <button
                type="button"
                style={styles.testBtn}
                onClick={testFormConnection}
                disabled={!form.host || (form.type === 'postgres' && !form.database) || formTesting}
                title={!form.host ? 'Host is required' : (form.type === 'postgres' && !form.database) ? 'Database is required' : undefined}
              >
                {formTesting ? 'Testing…' : 'Test Connection'}
              </button>
              {formTest && (
                <StatusBadge
                  status={formTest.ok ? 'success' : 'error'}
                  label={formTest.ok ? 'Connected' : (formTest.error ?? 'Failed')}
                  icon={formTest.ok ? <Check size={12} /> : <X size={12} />}
                />
              )}
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setCreating(false); setForm(defaultForm()); setFormTest(null) }}>Cancel</button>
              <button
                type="button"
                style={styles.saveBtn}
                onClick={() => createConnector.mutate()}
                disabled={!form.name || !form.host || (form.type === 'postgres' && !form.database) || createConnector.isPending}
                title={!form.name ? 'Name is required' : !form.host ? 'Host is required' : (form.type === 'postgres' && !form.database) ? 'Database is required' : undefined}
              >
                {createConnector.isPending ? 'Creating…' : 'Create'}
              </button>
            </div>
            {createError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{createError}</p>}
          </FormCard>
        )}

        {editing && (
          <FormCard title="Edit Connector">
            <div style={styles.formGrid}>
              <label style={styles.label}>Name
                <input style={styles.input} value={editForm.name} onChange={(e) => setEditForm(f => ({ ...f, name: e.target.value }))} />
              </label>
              <label style={styles.label}>Type
                <select style={styles.input} value={editForm.type} onChange={(e) => setEditForm((f) => ({
                  ...f, type: e.target.value as ConnectorType,
                  port: e.target.value === 'clickhouse' ? '9000' : e.target.value === 'opensearch' ? '9200' : '5432',
                }))}>
                  <option value="postgres">PostgreSQL</option>
                  <option value="clickhouse">ClickHouse</option>
                  <option value="opensearch">OpenSearch</option>
                </select>
              </label>
              <label style={styles.label}>Host
                <input style={styles.input} value={editForm.host} onChange={(e) => setEditForm(f => ({ ...f, host: e.target.value }))} />
              </label>
              <label style={styles.label}>Port
                <input style={styles.input} type="text" value={editForm.port} onChange={(e) => setEditForm(f => ({ ...f, port: e.target.value }))} />
              </label>
              {(editForm.type === 'postgres' || editForm.type === 'clickhouse') && (
                <label style={styles.label}>Database
                  <input style={styles.input} value={editForm.database} onChange={(e) => setEditForm(f => ({ ...f, database: e.target.value }))} />
                </label>
              )}
              <label style={styles.label}>User
                <input style={styles.input} value={editForm.user} onChange={(e) => setEditForm(f => ({ ...f, user: e.target.value }))} />
              </label>
              <label style={styles.label}>Password <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(leave blank to keep current)</span>
                <input style={styles.input} type="password" value={editForm.password} onChange={(e) => setEditForm(f => ({ ...f, password: e.target.value }))} />
              </label>
              {editForm.type === 'opensearch' && (
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
                  <input type="checkbox" checked={editForm.use_tls}
                    onChange={e => setEditForm(f => ({ ...f, use_tls: e.target.checked }))} />
                  Use TLS (HTTPS)
                </label>
              )}
              {(editForm.type === 'postgres' || editForm.type === 'clickhouse') && (
                <label style={styles.label}>SSL Mode
                  <select style={styles.input} value={editForm.ssl_mode} onChange={(e) => setEditForm(f => ({ ...f, ssl_mode: e.target.value }))}>
                    <option value="disable">disable</option>
                    <option value="require">require</option>
                    <option value="verify-full">verify-full</option>
                  </select>
                </label>
              )}
              <label style={styles.label}>Query Timeout (s)
                <input style={styles.input} type="text" value={editForm.timeout_seconds}
                  onChange={(e) => setEditForm(f => ({ ...f, timeout_seconds: e.target.value }))} placeholder="0 = unlimited" />
              </label>
              <label style={{ ...styles.label, gridColumn: '1 / -1' }}>
                Table Allowlist (regex patterns, one per line, empty = all tables)
                <textarea
                  style={{ ...styles.input, minHeight: 60, fontFamily: 'var(--font-mono)', fontSize: 12, resize: 'vertical' }}
                  value={editForm.table_allowlist}
                  onChange={(e) => setEditForm(f => ({ ...f, table_allowlist: e.target.value }))}
                  placeholder="e.g.,^public\\..*\\n^analytics\\..*"
                />
              </label>
              <label style={{ ...styles.label, gridColumn: '1 / -1' }}>
                Table Denylist (regex patterns, one per line)
                <textarea
                  style={{ ...styles.input, minHeight: 60, fontFamily: 'var(--font-mono)', fontSize: 12, resize: 'vertical' }}
                  value={editForm.table_denylist}
                  onChange={(e) => setEditForm(f => ({ ...f, table_denylist: e.target.value }))}
                  placeholder="e.g.,.*_src$\\n^temp\\..*"
                />
              </label>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, color: 'var(--text-secondary)', gridColumn: '1 / -1' }}>
                <input type="checkbox" checked={editForm.is_default ?? false}
                  onChange={e => setEditForm(f => ({ ...f, is_default: e.target.checked }))} />
                Set as default connector for new notebooks
              </label>
            </div>
            <div style={styles.formActions}>
              <span style={{ flex: 1 }} />
              <button type="button" style={styles.cancelBtn} onClick={() => { setEditing(null); setEditForm(defaultForm()); setEditError(null) }}>Cancel</button>
              <button
                type="button"
                style={styles.saveBtn}
                onClick={() => updateConnector.mutate(editing!)}
                disabled={!editForm.name || !editForm.host || (editForm.type === 'postgres' && !editForm.database) || updateConnector.isPending}
                title={!editForm.name ? 'Name is required' : !editForm.host ? 'Host is required' : (editForm.type === 'postgres' && !editForm.database) ? 'Database is required' : undefined}
              >
                {updateConnector.isPending ? 'Saving…' : 'Save'}
              </button>
            </div>
            {editError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{editError}</p>}
          </FormCard>
        )}

        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}
        {connectors.length === 0 && !isLoading ? (
          <EmptyState
            icon={<Database size={28} />}
            title="No connectors yet"
            text="Add a connector to link your databases and start querying."
            action={{ label: '+ New Connector', onClick: () => setCreating(true) }}
          />
        ) : (
          <div style={{ overflowX: 'auto' }}>
          <StyledTable headers={['Name', 'Type', 'Host', 'Database', 'Status', '']}>
            {connectors.map((c) => {
              const test = testResults[c.id]
              return (
                <tr key={c.id} style={rowStyle}>
                  <td style={cellStyle}>
                    <strong>{c.name}</strong>
                    {c.is_default && (
                      <span style={{
                        fontSize: 11,
                        background: 'var(--accent-light)',
                        border: '1px solid var(--accent)',
                        borderRadius: 10,
                        padding: '2px 8px',
                        color: 'var(--accent)',
                        fontWeight: 600,
                        marginLeft: 8,
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: 3,
                      }}>
                        <Star size={10} fill="var(--accent)" />
                        Default
                      </span>
                    )}
                  </td>
                  <td style={cellStyle}><code style={styles.badge}>{c.type}</code></td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                    {c.config?.host ?? '—'}
                  </td>
                  <td style={{ ...cellStyle, fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                    {c.config?.database ?? '—'}
                  </td>
                  <td style={cellStyle}>
                    {testingIds[c.id] ? (
                      <StatusBadge status="neutral" label="Testing…" />
                    ) : test ? (
                      <StatusBadge
                        status={test.ok ? 'success' : 'error'}
                        label={test.ok ? 'Connected' : (test.error ?? 'Failed')}
                        icon={test.ok ? <Check size={12} /> : <X size={12} />}
                      />
                    ) : (
                      <span style={{ fontSize: 11, color: 'var(--text-muted)', fontStyle: 'italic' }}>
                        Unknown — click Test
                      </span>
                    )}
                  </td>
                  <td style={styles.tdActions}>
                    <button type="button" style={styles.actionBtn} onClick={() => testConnector(c.id)} disabled={testingIds[c.id]}>
                      {testingIds[c.id] ? (
                        <><Loader2 size={11} style={{ animation: 'spin 1s linear infinite', marginRight: 4 }} />Testing…</>
                      ) : 'Test'}
                    </button>
                    <button type="button" style={styles.editBtn} onClick={() => {
                      setEditing(c.id)
                      setEditForm({
                        name: c.name,
                        type: c.type as ConnectorType,
                        host: c.config?.host ?? '',
                        port: String(c.config?.port ?? 5432),
                        database: c.config?.database ?? '',
                        user: c.config?.user ?? '',
                        password: '',
                        ssl_mode: c.config?.ssl_mode ?? 'disable',
                        use_tls: c.config?.use_tls ?? false,
                        is_default: c.is_default ?? false,
                        timeout_seconds: String(c.timeout_seconds ?? 0),
                        table_allowlist: (c.table_allowlist ?? []).join('\n'),
                        table_denylist: (c.table_denylist ?? []).join('\n'),
                      })
                    }}>Edit</button>
                    <button type="button" style={styles.actionBtn} onClick={() => setPermissionsTarget({ type: 'connector', id: c.id, name: c.name })}>Permissions</button>
                    {!c.is_default && (
                      <button type="button"
                        title="Set as default connector for new notebooks"
                        style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 4,
                          fontSize: 12, padding: '3px 10px', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 }}
                        onClick={() => setDefault.mutate(c.id)}>
                        Set default
                      </button>
                    )}
                    <button
                      type="button"
                      style={styles.deleteBtn}
                      onClick={() => setDeleteTarget(c)}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              )
            })}
          </StyledTable>
          </div>
        )}
        {permissionsTarget && (
          <PermissionsPanel
            resourceType="connector"
            resourceId={permissionsTarget.id}
            resourceName={permissionsTarget.name}
            resourceOwnerId={connectors.find(c => c.id === permissionsTarget.id)?.created_by}
            onClose={() => setPermissionsTarget(null)}
          />
        )}
      </div>
      <ConfirmDialog
        open={!!deleteTarget}
        title="Delete connector"
        message={`Delete "${deleteTarget?.name}"? It will be moved to trash and automatically deleted after 7 days.`}
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteTarget) deleteConnector.mutate(deleteTarget.id); setDeleteTarget(null) }}
        onCancel={() => setDeleteTarget(null)}
      />
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  newBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  body: { maxWidth: 1100, margin: '0 auto', padding: 'clamp(16px, 4vw, 32px)', width: '100%' },
  formGrid: { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 12, marginBottom: 16 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  formActions: { display: 'flex', gap: 8, justifyContent: 'flex-end' },
  testBtn: { padding: '6px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)', fontWeight: 600 },
  cancelBtn: { padding: '6px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  saveBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  tdActions: { padding: '8px 16px', textAlign: 'right' as const },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--accent-light)', color: 'var(--text-secondary)', padding: '2px 7px', borderRadius: 3 },
  actionBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 },
  editBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--accent)', marginRight: 6 },
  deleteBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'none', cursor: 'pointer', color: 'var(--error-full)' },
}
