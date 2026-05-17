import { useState, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Connector } from '../types'
import { AppShell } from '../components/AppShell'
import { Check, X } from 'lucide-react'
import { StyledTable, rowStyle, cellStyle } from '../components/StyledTable'
import { FormCard } from '../components/FormCard'
import { StatusBadge } from '../components/StatusBadge'
import { SectionHeader } from '../components/SectionHeader'

type ConnectorType = 'postgres' | 'clickhouse'

interface ConnectorForm {
  name: string
  type: ConnectorType
  host: string
  port: string
  database: string
  user: string
  password: string
  ssl_mode: string
  is_default: boolean
}

const defaultForm = (): ConnectorForm => ({
  name: '', type: 'postgres', host: 'localhost', port: '5432',
  database: '', user: '', password: '', ssl_mode: 'disable', is_default: false,
})

export function ConnectorsPage() {
  useEffect(() => { document.title = "Connectors — Heaven's Notebooks" }, [])
  const qc = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [editForm, setEditForm] = useState<ConnectorForm>(defaultForm())
  const [form, setForm] = useState<ConnectorForm>(defaultForm())
  const [testResults, setTestResults] = useState<Record<string, { ok: boolean; error?: string }>>({})
  const [testingId, setTestingId] = useState<string | null>(null)
  const [createError, setCreateError] = useState<string | null>(null)
  const [editError, setEditError] = useState<string | null>(null)
  const [formTest, setFormTest] = useState<{ ok: boolean; error?: string } | null>(null)
  const [formTesting, setFormTesting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const { data: connectors = [] } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

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
          is_default: c.is_default ?? false,
        })
        setSearchParams({})
      }
    }
  }, [searchParams, connectors, setSearchParams])

  const updateConnector = useMutation({
    mutationFn: (id: string) => api.put<Connector>(`/api/v1/connectors/${id}`, {
      name: editForm.name,
      config: {
        host: editForm.host,
        port: parseInt(editForm.port),
        database: editForm.database,
        user: editForm.user,
        ...(editForm.password !== '' ? { password: editForm.password } : {}),
        ssl_mode: editForm.ssl_mode,
      },
      ...(editForm.is_default ? { is_default: true } : {}),
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
      config: {
        host: form.host,
        port: parseInt(form.port),
        database: form.database,
        user: form.user,
        password: form.password,
        ssl_mode: form.ssl_mode,
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
    setTestingId(id)
    try {
      const result = await api.post<{ ok: boolean; error?: string }>(`/api/v1/connectors/${id}/test`, {})
      setTestResults((prev) => ({ ...prev, [id]: result }))
    } catch {
      setTestResults((prev) => ({ ...prev, [id]: { ok: false, error: 'Request failed' } }))
    } finally {
      setTestingId(null)
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
          <SectionHeader title="Connectors" subtitle={connectors.length > 0 ? `${connectors.length} connector${connectors.length !== 1 ? 's' : ''}` : ''}>
            <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Connector</button>
          </SectionHeader>
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
                  port: e.target.value === 'clickhouse' ? '9000' : '5432',
                }))}>
                  <option value="postgres">PostgreSQL</option>
                  <option value="clickhouse">ClickHouse</option>
                </select>
              </label>
              <label style={styles.label}>Host
                <input style={styles.input} value={form.host} onChange={setField('host')} />
              </label>
              <label style={styles.label}>Port
                <input style={styles.input} type="number" min={1} max={65535} value={form.port} onChange={setField('port')} />
              </label>
              <label style={styles.label}>Database
                <input style={styles.input} value={form.database} onChange={setField('database')} />
              </label>
              <label style={styles.label}>User
                <input style={styles.input} value={form.user} onChange={setField('user')} />
              </label>
              <label style={styles.label}>Password
                <input style={styles.input} type="password" value={form.password} onChange={setField('password')} />
              </label>
              <label style={styles.label}>SSL Mode
                <select style={styles.input} value={form.ssl_mode} onChange={setField('ssl_mode')}>
                  <option value="disable">disable</option>
                  <option value="require">require</option>
                  <option value="verify-full">verify-full</option>
                </select>
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
                disabled={!form.host || !form.database || formTesting}
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
                disabled={!form.name || !form.host || !form.database || createConnector.isPending}
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
                  port: e.target.value === 'clickhouse' ? '9000' : '5432',
                }))}>
                  <option value="postgres">PostgreSQL</option>
                  <option value="clickhouse">ClickHouse</option>
                </select>
              </label>
              <label style={styles.label}>Host
                <input style={styles.input} value={editForm.host} onChange={(e) => setEditForm(f => ({ ...f, host: e.target.value }))} />
              </label>
              <label style={styles.label}>Port
                <input style={styles.input} type="number" min={1} max={65535} value={editForm.port} onChange={(e) => setEditForm(f => ({ ...f, port: e.target.value }))} />
              </label>
              <label style={styles.label}>Database
                <input style={styles.input} value={editForm.database} onChange={(e) => setEditForm(f => ({ ...f, database: e.target.value }))} />
              </label>
              <label style={styles.label}>User
                <input style={styles.input} value={editForm.user} onChange={(e) => setEditForm(f => ({ ...f, user: e.target.value }))} />
              </label>
              <label style={styles.label}>Password <span style={{ fontWeight: 400, color: 'var(--text-muted)' }}>(leave blank to keep current)</span>
                <input style={styles.input} type="password" value={editForm.password} onChange={(e) => setEditForm(f => ({ ...f, password: e.target.value }))} />
              </label>
              <label style={styles.label}>SSL Mode
                <select style={styles.input} value={editForm.ssl_mode} onChange={(e) => setEditForm(f => ({ ...f, ssl_mode: e.target.value }))}>
                  <option value="disable">disable</option>
                  <option value="require">require</option>
                  <option value="verify-full">verify-full</option>
                </select>
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
                disabled={!editForm.name || !editForm.host || !editForm.database || updateConnector.isPending}
              >
                {updateConnector.isPending ? 'Saving…' : 'Save'}
              </button>
            </div>
            {editError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{editError}</p>}
          </FormCard>
        )}

        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}
        <StyledTable headers={['Name', 'Type', 'Host', 'Database', 'Status', '']}>
          {connectors.map((c) => {
            const test = testResults[c.id]
            return (
              <tr key={c.id} style={rowStyle}>
                <td style={cellStyle}>
                  <strong>{c.name}</strong>
                  {c.is_default && (
                    <span style={{ fontSize: 11, background: 'var(--accent-light)', border: '1px solid var(--border)',
                      borderRadius: 3, padding: '1px 6px', color: 'var(--text-secondary)', fontFamily: 'var(--font-mono)',
                      marginLeft: 8 }}>
                      default
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
                  {test ? (
                    <StatusBadge
                      status={test.ok ? 'success' : 'error'}
                      label={test.ok ? 'Connected' : (test.error ?? 'Failed')}
                      icon={test.ok ? <Check size={12} /> : <X size={12} />}
                    />
                  ) : (
                    <StatusBadge status="neutral" label="—" />
                  )}
                </td>
                <td style={styles.tdActions}>
                  <button type="button" style={styles.actionBtn} onClick={() => testConnector(c.id)} disabled={testingId === c.id}>
                    {testingId === c.id ? 'Testing…' : 'Test'}
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
                      is_default: c.is_default ?? false,
                    })
                  }}>Edit</button>
                  {!c.is_default && (
                    <button type="button"
                      style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 4,
                        fontSize: 12, padding: '3px 10px', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 }}
                      onClick={() => setDefault.mutate(c.id)}>
                      Set default
                    </button>
                  )}
                  <button
                    type="button"
                    style={styles.deleteBtn}
                    onClick={() => { if (confirm(`Delete "${c.name}"?`)) deleteConnector.mutate(c.id) }}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            )
          })}
          {connectors.length === 0 && (
            <tr>
              <td colSpan={6} style={{ ...cellStyle, textAlign: 'center', color: 'var(--text-muted)', padding: 40 }}>
                No connectors yet. Add one to connect to your databases.
              </td>
            </tr>
          )}
        </StyledTable>
      </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  newBtn: { padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  formGrid: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 16 },
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
