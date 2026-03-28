import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { Connector } from '../types'
import { AppShell } from '../components/AppShell'
import { Check, X } from 'lucide-react'

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
}

const defaultForm = (): ConnectorForm => ({
  name: '', type: 'postgres', host: 'localhost', port: '5432',
  database: '', user: '', password: '', ssl_mode: 'disable',
})

export function ConnectorsPage() {
  useEffect(() => { document.title = "Connectors — Heaven's Notebooks" }, [])
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [form, setForm] = useState<ConnectorForm>(defaultForm())
  const [testResults, setTestResults] = useState<Record<string, { ok: boolean; error?: string }>>({})
  const [createError, setCreateError] = useState<string | null>(null)
  const [formTest, setFormTest] = useState<{ ok: boolean; error?: string } | null>(null)
  const [formTesting, setFormTesting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const { data: connectors = [] } = useQuery({
    queryKey: ['connectors'],
    queryFn: () => api.get<Connector[]>('/api/v1/connectors'),
  })

  const createConnector = useMutation({
    mutationFn: () => api.post<Connector>('/api/v1/connectors', {
      name: form.name,
      type: form.type,
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

  const testConnector = async (id: string) => {
    try {
      const result = await api.post<{ ok: boolean; error?: string }>(`/api/v1/connectors/${id}/test`, {})
      setTestResults((prev) => ({ ...prev, [id]: result }))
    } catch {
      setTestResults((prev) => ({ ...prev, [id]: { ok: false, error: 'Request failed' } }))
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
          <div style={styles.bodyHeader}>
            <button type="button" style={styles.newBtn} onClick={() => setCreating(true)}>+ New Connector</button>
          </div>
        )}
        {creating && (
          <div style={styles.formCard}>
            <h3 style={styles.formTitle}>New Connector</h3>
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
                <span style={{ fontSize: 12, fontWeight: 600, display: 'inline-flex', alignItems: 'center', gap: 4, color: formTest.ok ? '#2d7d46' : '#c0392b' }}>
                  {formTest.ok ? <><Check size={12} /> Connected</> : <><X size={12} /> {formTest.error ?? 'Failed'}</>}
                </span>
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
          </div>
        )}

        {deleteError && <p style={{ color: 'var(--error)', fontSize: 12 }}>{deleteError}</p>}
        <div style={styles.tableWrap}>
          <table style={styles.table}>
            <thead>
              <tr>
                {['Name', 'Type', 'Host', 'Database', 'Status', ''].map((h) => (
                  <th key={h} style={styles.th}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {connectors.map((c) => {
                const test = testResults[c.id]
                return (
                  <tr key={c.id} style={styles.tr}>
                    <td style={styles.td}><strong>{c.name}</strong></td>
                    <td style={styles.td}><code style={styles.badge}>{c.type}</code></td>
                    <td style={{ ...styles.td, fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                      {c.config?.host ?? '—'}
                    </td>
                    <td style={{ ...styles.td, fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                      {c.config?.database ?? '—'}
                    </td>
                    <td style={styles.td}>
                      {test ? (
                        <span style={{ color: test.ok ? '#2d7d46' : '#c0392b', fontSize: 12, fontWeight: 600, display: 'inline-flex', alignItems: 'center', gap: 3 }}>
                          {test.ok ? <><Check size={12} /> Connected</> : <><X size={12} /> {test.error ?? 'Failed'}</>}
                        </span>
                      ) : (
                        <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>—</span>
                      )}
                    </td>
                    <td style={styles.tdActions}>
                      <button type="button" style={styles.actionBtn} onClick={() => testConnector(c.id)}>Test</button>
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
                  <td colSpan={6} style={{ ...styles.td, textAlign: 'center', color: 'var(--text-muted)', padding: 40 }}>
                    No connectors yet. Add one to connect to your databases.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  newBtn: { padding: '6px 16px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 6, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  bodyHeader: { display: 'flex', justifyContent: 'flex-end', marginBottom: 16 },
  body: { maxWidth: 1100, margin: '0 auto', padding: '32px 40px', width: '100%' },
  formCard: {
    background: 'white',
    border: '1px solid var(--border)',
    borderRadius: 10,
    padding: 24,
    marginBottom: 24,
    boxShadow: 'var(--shadow-sm)',
  },
  formTitle: { margin: '0 0 16px', fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' },
  formGrid: { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 16 },
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 5, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'white', marginTop: 2 },
  formActions: { display: 'flex', gap: 8, justifyContent: 'flex-end' },
  testBtn: { padding: '6px 16px', background: 'transparent', border: '1px solid var(--accent)', borderRadius: 5, fontSize: 13, cursor: 'pointer', color: 'var(--accent)', fontWeight: 600 },
  cancelBtn: { padding: '6px 16px', background: 'transparent', border: '1px solid var(--border)', borderRadius: 5, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  saveBtn: { padding: '6px 16px', background: 'var(--accent)', color: 'white', border: 'none', borderRadius: 5, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  tableWrap: { borderRadius: 10, overflow: 'hidden', border: '1px solid var(--border)', boxShadow: 'var(--shadow-sm)' },
  table: { width: '100%', borderCollapse: 'collapse', background: 'white' },
  th: { padding: '10px 16px', textAlign: 'left', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', letterSpacing: '0.06em', borderBottom: '1px solid var(--border-light)', background: 'var(--bg-secondary)', textTransform: 'uppercase' },
  tr: { borderBottom: '1px solid var(--border-light)' },
  td: { padding: '12px 16px', fontSize: 13, color: 'var(--text-primary)' },
  tdActions: { padding: '8px 16px', textAlign: 'right' as const },
  badge: { fontSize: 11, fontFamily: 'var(--font-mono)', background: 'var(--bg-secondary)', padding: '2px 7px', borderRadius: 3, border: '1px solid var(--border-light)' },
  actionBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid var(--border)', borderRadius: 4, background: 'transparent', cursor: 'pointer', color: 'var(--text-secondary)', marginRight: 6 },
  deleteBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, border: '1px solid transparent', borderRadius: 4, background: 'transparent', cursor: 'pointer', color: '#c0392b' },
}
