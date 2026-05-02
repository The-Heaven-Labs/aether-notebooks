import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { api } from '../api/client'
import type { SSOProvider } from '../types'

interface Org {
  id: string; name: string; slug: string; member_count: number; created_at: string
}
interface User {
  id: string; email: string; name: string; is_platform_admin: boolean; orgs: string[]
}

// ─── Provider form state ─────────────────────────────────────────────────────

interface ProviderFormValues {
  name: string
  client_id: string
  client_secret: string
  discovery_url: string
  allowed_domains: string
  enabled: boolean
}

const emptyForm: ProviderFormValues = {
  name: '',
  client_id: '',
  client_secret: '',
  discovery_url: '',
  allowed_domains: '',
  enabled: true,
}

function providerToForm(p: SSOProvider): ProviderFormValues {
  return {
    name: p.name,
    client_id: p.client_id,
    client_secret: '',
    discovery_url: p.discovery_url,
    allowed_domains: (p.allowed_domains ?? []).join(', '),
    enabled: p.enabled,
  }
}

// ─── ProviderForm component ───────────────────────────────────────────────────

function ProviderForm({
  initial,
  isEdit,
  onSave,
  onCancel,
  saving,
  error,
}: {
  initial: ProviderFormValues
  isEdit: boolean
  onSave: (values: ProviderFormValues) => void
  onCancel: () => void
  saving: boolean
  error: string | null
}) {
  const [values, setValues] = useState<ProviderFormValues>(initial)

  const set = (field: keyof ProviderFormValues) =>
    (e: React.ChangeEvent<HTMLInputElement>) =>
      setValues(v => ({ ...v, [field]: field === 'enabled' ? (e.target as HTMLInputElement).checked : e.target.value }))

  return (
    <div style={formStyles.container}>
      <div style={formStyles.grid}>
        <label style={formStyles.label}>
          Name
          <input style={formStyles.input} value={values.name} onChange={set('name')} placeholder="My OIDC Provider" />
        </label>
        <label style={formStyles.label}>
          Client ID
          <input style={formStyles.input} value={values.client_id} onChange={set('client_id')} />
        </label>
        <label style={formStyles.label}>
          Client Secret
          <input
            style={formStyles.input}
            type="password"
            value={values.client_secret}
            onChange={set('client_secret')}
            placeholder={isEdit ? '(leave blank to keep existing)' : ''}
          />
        </label>
        <label style={formStyles.label}>
          Discovery URL
          <input
            style={formStyles.input}
            value={values.discovery_url}
            onChange={set('discovery_url')}
            placeholder="https://accounts.example.com/.well-known/openid-configuration"
          />
        </label>
        <label style={formStyles.label}>
          Allowed Domains <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(comma-separated)</span>
          <input
            style={formStyles.input}
            value={values.allowed_domains}
            onChange={set('allowed_domains')}
            placeholder="company.com, company.org"
          />
        </label>
        <label style={{ ...formStyles.label, flexDirection: 'row', alignItems: 'center', gap: 8 }}>
          <input type="checkbox" checked={values.enabled} onChange={set('enabled')} />
          Enabled
        </label>
      </div>
      {error && <div style={formStyles.error}>{error}</div>}
      <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
        <button
          style={{ ...formStyles.btn, opacity: saving ? 0.6 : 1 }}
          onClick={() => onSave(values)}
          disabled={saving}
        >
          {saving ? 'Saving…' : isEdit ? 'Save Changes' : 'Add Provider'}
        </button>
        <button style={formStyles.cancelBtn} onClick={onCancel} disabled={saving}>
          Cancel
        </button>
      </div>
    </div>
  )
}

const formStyles: Record<string, React.CSSProperties> = {
  container: {
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    padding: '16px 20px',
    marginTop: 10,
  },
  grid: { display: 'flex', flexDirection: 'column', gap: 12 },
  label: {
    display: 'flex',
    flexDirection: 'column',
    gap: 4,
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-secondary)',
  },
  input: {
    padding: '7px 10px',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    color: 'var(--text-primary)',
    background: 'var(--bg-input)',
  },
  btn: {
    padding: '7px 16px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
  },
  cancelBtn: {
    padding: '7px 16px',
    background: 'transparent',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 13,
    cursor: 'pointer',
  },
  error: {
    marginTop: 10,
    fontSize: 12,
    color: 'var(--error)',
  },
}

// ─── SSO Providers tab content ───────────────────────────────────────────────

function SSOProvidersTab() {
  const qc = useQueryClient()

  const { data: providers = [], isLoading } = useQuery<SSOProvider[]>({
    queryKey: ['admin', 'sso', 'providers'],
    queryFn: () => api.get('/api/v1/admin/sso/providers'),
  })

  const [showAddForm, setShowAddForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null)
  const [addFormError, setAddFormError] = useState<string | null>(null)
  const [editFormError, setEditFormError] = useState<string | null>(null)
  const [togglingId, setTogglingId] = useState<string | null>(null)

  const createProvider = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.post('/api/v1/admin/sso/providers', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'sso', 'providers'] })
      setShowAddForm(false)
      setAddFormError(null)
    },
    onError: (e: unknown) => setAddFormError(String(e)),
  })

  const updateProvider = useMutation({
    mutationFn: ({ id, body }: { id: string; body: Record<string, unknown> }) =>
      api.put(`/api/v1/admin/sso/providers/${id}`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'sso', 'providers'] })
      setEditingId(null)
      setEditFormError(null)
      setTogglingId(null)
    },
    onError: (e: unknown) => { setEditFormError(String(e)); setTogglingId(null) },
  })

  const deleteProvider = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/admin/sso/providers/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin', 'sso', 'providers'] })
      setConfirmDeleteId(null)
    },
  })

  function handleCreate(values: ProviderFormValues) {
    const body: Record<string, unknown> = {
      name: values.name,
      client_id: values.client_id,
      client_secret: values.client_secret,
      discovery_url: values.discovery_url,
      allowed_domains: values.allowed_domains
        .split(',')
        .map(s => s.trim())
        .filter(Boolean),
      enabled: values.enabled,
    }
    createProvider.mutate(body)
  }

  function handleUpdate(id: string, values: ProviderFormValues) {
    const body: Record<string, unknown> = {
      name: values.name,
      client_id: values.client_id,
      discovery_url: values.discovery_url,
      allowed_domains: values.allowed_domains
        .split(',')
        .map(s => s.trim())
        .filter(Boolean),
      enabled: values.enabled,
    }
    if (values.client_secret) body.client_secret = values.client_secret
    updateProvider.mutate({ id, body })
  }

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
        <p style={ssoStyles.desc}>
          Platform-level OIDC providers available to all organizations.
        </p>
        {!showAddForm && (
          <button style={ssoStyles.addBtn} onClick={() => { setShowAddForm(true); setAddFormError(null) }}>
            + Add Provider
          </button>
        )}
      </div>

      {showAddForm && (
        <ProviderForm
          initial={emptyForm}
          isEdit={false}
          onSave={handleCreate}
          onCancel={() => { setShowAddForm(false); setAddFormError(null) }}
          saving={createProvider.isPending}
          error={addFormError}
        />
      )}

      {isLoading ? (
        <div style={ssoStyles.empty}>Loading…</div>
      ) : providers.length === 0 && !showAddForm ? (
        <div style={ssoStyles.empty}>No platform SSO providers yet.</div>
      ) : (
        <div style={ssoStyles.list}>
          {providers.map(p => (
            <div key={p.id} style={{ ...ssoStyles.row, flexDirection: 'column', alignItems: 'stretch', gap: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center' }}>
                <div style={ssoStyles.rowInfo}>
                  <span style={ssoStyles.rowName}>{p.name}</span>
                  <span style={ssoStyles.rowMeta}>
                    {p.client_id} · {p.discovery_url}
                  </span>
                </div>
                <div style={{ display: 'flex', gap: 6, marginLeft: 'auto' }}>
                  <button
                    style={p.enabled ? ssoStyles.badgeEnabled : ssoStyles.badgeDisabled}
                    onClick={() => { setTogglingId(p.id); updateProvider.mutate({ id: p.id, body: { enabled: !p.enabled } }) }}
                    disabled={togglingId === p.id}
                  >
                    {p.enabled ? 'Enabled' : 'Disabled'}
                  </button>
                  <button
                    style={ssoStyles.iconBtn}
                    onClick={() => {
                      setEditingId(editingId === p.id ? null : p.id)
                      setEditFormError(null)
                    }}
                  >
                    Edit
                  </button>
                  {confirmDeleteId === p.id ? (
                    <>
                      <button
                        style={{ ...ssoStyles.iconBtn, color: 'var(--error)', borderColor: 'var(--error)' }}
                        onClick={() => deleteProvider.mutate(p.id)}
                        disabled={deleteProvider.isPending}
                      >
                        Confirm
                      </button>
                      <button style={ssoStyles.iconBtn} onClick={() => setConfirmDeleteId(null)}>
                        Cancel
                      </button>
                    </>
                  ) : (
                    <button
                      style={{ ...ssoStyles.iconBtn, color: 'var(--error)', borderColor: 'var(--error)' }}
                      onClick={() => setConfirmDeleteId(p.id)}
                    >
                      Delete
                    </button>
                  )}
                </div>
              </div>
              {editingId === p.id && (
                <ProviderForm
                  key={p.id}
                  initial={providerToForm(p)}
                  isEdit={true}
                  onSave={values => handleUpdate(p.id, values)}
                  onCancel={() => { setEditingId(null); setEditFormError(null) }}
                  saving={updateProvider.isPending}
                  error={editFormError}
                />
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

const ssoStyles: Record<string, React.CSSProperties> = {
  desc: {
    fontSize: 13,
    color: 'var(--text-muted)',
    margin: 0,
  },
  addBtn: {
    padding: '6px 14px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    flexShrink: 0,
  },
  empty: {
    fontSize: 13,
    color: 'var(--text-muted)',
    padding: '12px 0',
  },
  list: {
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
  },
  row: {
    display: 'flex',
    alignItems: 'center',
    padding: '10px 14px',
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    gap: 12,
  },
  rowInfo: {
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    flex: 1,
    minWidth: 0,
  },
  rowName: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  rowMeta: {
    fontSize: 12,
    color: 'var(--text-muted)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  badgeEnabled: {
    padding: '2px 8px',
    background: 'var(--success-bg, #d1fae5)',
    color: 'var(--success, #065f46)',
    border: '1px solid var(--success, #065f46)',
    borderRadius: 10,
    fontSize: 11,
    fontWeight: 600,
    flexShrink: 0,
    cursor: 'pointer',
  },
  badgeDisabled: {
    padding: '2px 8px',
    background: 'var(--bg-secondary)',
    color: 'var(--text-muted)',
    border: '1px solid var(--border)',
    borderRadius: 10,
    fontSize: 11,
    fontWeight: 600,
    flexShrink: 0,
    cursor: 'pointer',
  },
  iconBtn: {
    padding: '4px 10px',
    background: 'transparent',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    cursor: 'pointer',
    flexShrink: 0,
  },
}

// ─── Main Page ────────────────────────────────────────────────────────────────

export function AdminPage() {
  useEffect(() => { document.title = "Platform Admin — Heaven's Notebooks" }, [])
  const [tab, setTab] = useState<'orgs' | 'users' | 'sso'>('orgs')
  const [orgs, setOrgs] = useState<Org[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [togglingUserId, setTogglingUserId] = useState<string | null>(null)

  const togglePlatformAdmin = useMutation({
    mutationFn: ({ id, isPlatformAdmin }: { id: string; isPlatformAdmin: boolean }) =>
      api.put(`/api/v1/admin/users/${id}`, { is_platform_admin: isPlatformAdmin }),
    onSuccess: (_data, { id, isPlatformAdmin }) => {
      setUsers(prev => prev.map(u => u.id === id ? { ...u, is_platform_admin: isPlatformAdmin } : u))
      setTogglingUserId(null)
    },
    onError: (_err, { id }) => {
      setTogglingUserId(null)
    },
  })

  useEffect(() => {
    const token = localStorage.getItem('hnb_token')
    const headers: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {}
    fetch('/api/v1/admin/orgs', { headers }).then(r => r.json()).then(d => setOrgs(d.orgs ?? []))
    fetch('/api/v1/admin/users', { headers }).then(r => r.json()).then(d => setUsers(d.users ?? []))
  }, [])

  return (
    <div style={styles.page}>
      <h1 style={styles.title}>Platform Admin</h1>
      <div style={styles.tabs} role="tablist">
        <button
          role="tab"
          aria-selected={tab === 'orgs'}
          style={tab === 'orgs' ? styles.tabActive : styles.tab}
          onClick={() => setTab('orgs')}
        >
          Orgs
        </button>
        <button
          role="tab"
          aria-selected={tab === 'users'}
          style={tab === 'users' ? styles.tabActive : styles.tab}
          onClick={() => setTab('users')}
        >
          Users
        </button>
        <button
          role="tab"
          aria-selected={tab === 'sso'}
          style={tab === 'sso' ? styles.tabActive : styles.tab}
          onClick={() => setTab('sso')}
        >
          SSO Providers
        </button>
      </div>

      {tab === 'orgs' && (
        <table style={styles.table}>
          <thead><tr>
            <th style={styles.th}>Name</th>
            <th style={styles.th}>Slug</th>
            <th style={styles.th}>Members</th>
            <th style={styles.th}>Created</th>
          </tr></thead>
          <tbody>
            {orgs.map(o => (
              <tr key={o.id}>
                <td style={styles.td}>{o.name}</td>
                <td style={styles.td}>{o.slug}</td>
                <td style={styles.td}>{o.member_count}</td>
                <td style={styles.td}>{new Date(o.created_at).toLocaleDateString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {tab === 'users' && (
        <table style={styles.table}>
          <thead><tr>
            <th style={styles.th}>Email</th>
            <th style={styles.th}>Name</th>
            <th style={styles.th}>Platform Admin</th>
            <th style={styles.th}>Orgs</th>
          </tr></thead>
          <tbody>
            {users.map(u => (
              <tr key={u.id}>
                <td style={styles.td}>{u.email}</td>
                <td style={styles.td}>{u.name}</td>
                <td style={styles.td}>
                  <button
                    style={{
                      ...styles.iconBtn,
                      background: u.is_platform_admin ? 'var(--accent)' : 'transparent',
                      color: u.is_platform_admin ? '#fff' : 'var(--text-muted)',
                    }}
                    disabled={togglingUserId === u.id}
                    onClick={() => {
                      setTogglingUserId(u.id)
                      togglePlatformAdmin.mutate({ id: u.id, isPlatformAdmin: !u.is_platform_admin })
                    }}
                  >
                    {u.is_platform_admin ? 'Admin' : 'User'}
                  </button>
                </td>
                <td style={styles.td}>{u.orgs.join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {tab === 'sso' && <SSOProvidersTab />}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: { padding: 24 },
  title: { fontSize: 20, fontWeight: 600, marginBottom: 16, color: 'var(--text-primary)' },
  tabs: { display: 'flex', gap: 2, marginBottom: 16, borderBottom: '1px solid var(--border)' },
  tab: { padding: '8px 16px', background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', fontSize: 14 },
  tabActive: { padding: '8px 16px', background: 'none', border: 'none', borderBottom: '2px solid var(--accent)', cursor: 'pointer', color: 'var(--text-primary)', fontSize: 14, fontWeight: 600 },
  table: { width: '100%', borderCollapse: 'collapse' },
  th: { textAlign: 'left', padding: '8px 12px', fontSize: 12, color: 'var(--text-muted)', borderBottom: '1px solid var(--border)', background: 'var(--bg-card)' },
  td: { padding: '10px 12px', fontSize: 13, color: 'var(--text-primary)', borderBottom: '1px solid var(--border)' },
}
