import { useState, useEffect } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { api } from '../api/client'
import { Loader2, CheckCircle2, XCircle } from 'lucide-react'
import type { SSOProvider, PlatformSSOProvider, SSOSettings } from '../types'
import { ConfirmDialog } from '../components/ConfirmDialog'

// ─── Provider form state ────────────────────────────────────────────────────

interface ProviderFormValues {
  name: string
  client_id: string
  client_secret: string
  discovery_url: string
  allowed_domains: string
  enabled: boolean
  scopes: string
  groups_claim: string
  group_prefix: string
  auto_sync_groups: boolean
  get_user_info: boolean
}

const emptyForm: ProviderFormValues = {
  name: '',
  client_id: '',
  client_secret: '',
  discovery_url: '',
  allowed_domains: '',
  enabled: true,
  scopes: '',
  groups_claim: 'groups',
  group_prefix: '',
  auto_sync_groups: false,
  get_user_info: false,
}

function providerToForm(p: SSOProvider): ProviderFormValues {
  return {
    name: p.name,
    client_id: p.client_id,
    client_secret: '',
    discovery_url: p.discovery_url,
    allowed_domains: (p.allowed_domains ?? []).join(', '),
    enabled: p.enabled,
    scopes: (p.scopes ?? []).join(', '),
    groups_claim: p.groups_claim ?? 'groups',
    group_prefix: p.group_prefix ?? '',
    auto_sync_groups: p.auto_sync_groups ?? false,
    get_user_info: p.get_user_info ?? false,
  }
}

// ─── ProviderForm component ──────────────────────────────────────────────────

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
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null)
  const [testing, setTesting] = useState(false)

  const set = (field: keyof ProviderFormValues) =>
    (e: React.ChangeEvent<HTMLInputElement>) =>
      setValues(v => ({ ...v, [field]: field === 'enabled' || field === 'auto_sync_groups' || field === 'get_user_info' ? (e.target as HTMLInputElement).checked : e.target.value }))

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
        <label style={formStyles.label}>
          Scopes <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(comma-separated)</span>
          <input style={formStyles.input} value={values.scopes} onChange={set('scopes')} placeholder="openid, profile, email, groups" />
        </label>
        <label style={formStyles.label}>
          Groups Claim
          <input style={formStyles.input} value={values.groups_claim} onChange={set('groups_claim')} placeholder="groups" />
        </label>
        <label style={formStyles.label}>
          Group Prefix <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(only sync groups with this prefix)</span>
          <input style={formStyles.input} value={values.group_prefix} onChange={set('group_prefix')} placeholder="hnb-" />
        </label>
        <label style={{ ...formStyles.label, flexDirection: 'row', alignItems: 'center', gap: 8 }}>
          <input type="checkbox" checked={values.auto_sync_groups} onChange={set('auto_sync_groups')} />
          Auto-sync Groups
        </label>
        <label style={{ ...formStyles.label, flexDirection: 'row', alignItems: 'center', gap: 8 }}>
          <input type="checkbox" checked={values.get_user_info} onChange={set('get_user_info')} />
          Call UserInfo Endpoint
        </label>
      </div>
      {error && <div style={formStyles.error}>{error}</div>}
      {testResult && (
        <div style={{
          display: 'flex', alignItems: 'center', gap: 6,
          marginTop: 12, padding: '8px 12px', borderRadius: 4, fontSize: 12,
          background: testResult.success ? 'var(--success-light, #d1fae5)' : 'var(--error-light, #fef2f2)',
          color: testResult.success ? 'var(--success, #059669)' : 'var(--error-full)',
        }}>
          {testResult.success ? <CheckCircle2 size={14} /> : <XCircle size={14} />}
          {testResult.message}
        </div>
      )}
      <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
        <button
          style={{ ...formStyles.btn, opacity: saving ? 0.6 : 1 }}
          onClick={() => onSave(values)}
          disabled={saving}
        >
          {saving ? 'Saving…' : isEdit ? 'Save Changes' : 'Add Provider'}
        </button>
        <button
          style={{
            ...formStyles.cancelBtn,
            display: 'inline-flex', alignItems: 'center', gap: 4,
            opacity: testing || !values.discovery_url ? 0.6 : 1,
          }}
          onClick={async () => {
            if (!values.discovery_url) return
            setTesting(true)
            setTestResult(null)
            try {
              const result = await api.post<{ success: boolean; error?: string; issuer?: string; provider_info?: Record<string, string> }>('/api/v1/sso/providers/test', {
                discovery_url: values.discovery_url,
                client_id: values.client_id,
                client_secret: values.client_secret,
              })
              if (result.success) {
                setTestResult({ success: true, message: `Connected! Provider: ${result.issuer ?? 'unknown'}` })
              } else {
                setTestResult({ success: false, message: result.error ?? 'Connection failed' })
              }
            } catch (e: unknown) {
              setTestResult({ success: false, message: String(e) })
            } finally {
              setTesting(false)
            }
          }}
          disabled={testing || !values.discovery_url}
        >
          {testing ? <Loader2 size={13} style={{ animation: 'spin 1s linear infinite' }} /> : null}
          Test Connection
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

// ─── Main Page ───────────────────────────────────────────────────────────────

export function OrgSettingsPage() {
  const qc = useQueryClient()

  // ── Platform providers ───────────────────────────────────────────────────
  const { data: platformProviders = [], isLoading: loadingPlatform } = useQuery<PlatformSSOProvider[]>({
    queryKey: ['sso', 'platform-providers'],
    queryFn: () => api.get<{ providers: PlatformSSOProvider[] }>('/api/v1/sso/platform-providers').then(r => r.providers),
  })

  const togglePlatformProvider = useMutation({
    mutationFn: ({ id, enable }: { id: string; enable: boolean }) =>
      enable
        ? api.post(`/api/v1/sso/platform-providers/${id}/enable`, {})
        : api.delete(`/api/v1/sso/platform-providers/${id}/enable`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['sso', 'platform-providers'] }),
  })

  // ── Custom providers ─────────────────────────────────────────────────────
  const { data: customProviders = [], isLoading: loadingCustom } = useQuery<SSOProvider[]>({
    queryKey: ['sso', 'providers'],
    queryFn: () => api.get<{ providers: SSOProvider[] }>('/api/v1/sso/providers').then(r => r.providers),
  })

  const [showAddForm, setShowAddForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null)
  const [formError, setFormError] = useState<string | null>(null)
  const [saveSuccess, setSaveSuccess] = useState<string | null>(null)
  const [sharingEnabled, setSharingEnabled] = useState(true)

  useEffect(() => {
    api.get<{ public_sharing_enabled: boolean }>('/api/v1/org/sharing')
      .then(r => setSharingEnabled(r.public_sharing_enabled))
      .catch(() => {})
  }, [])

  async function handleToggleSharing(enabled: boolean) {
    await api.put('/api/v1/org/sharing', { public_sharing_enabled: enabled })
    setSharingEnabled(enabled)
  }

  const createProvider = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.post('/api/v1/sso/providers', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sso', 'providers'] })
      setShowAddForm(false)
      setFormError(null)
      setSaveSuccess('Provider created successfully')
      setTimeout(() => setSaveSuccess(null), 3000)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const updateProvider = useMutation({
    mutationFn: ({ id, body }: { id: string; body: Record<string, unknown> }) =>
      api.put(`/api/v1/sso/providers/${id}`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sso', 'providers'] })
      setEditingId(null)
      setFormError(null)
      setSaveSuccess('Provider updated successfully')
      setTimeout(() => setSaveSuccess(null), 3000)
    },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const deleteProvider = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/sso/providers/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sso', 'providers'] })
      setConfirmDeleteId(null)
      setSaveSuccess('Provider deleted')
      setTimeout(() => setSaveSuccess(null), 3000)
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
      scopes: values.scopes.split(',').map(s => s.trim()).filter(Boolean),
      groups_claim: values.groups_claim,
      group_prefix: values.group_prefix,
      auto_sync_groups: values.auto_sync_groups,
      get_user_info: values.get_user_info,
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
      scopes: values.scopes.split(',').map(s => s.trim()).filter(Boolean),
      groups_claim: values.groups_claim,
      group_prefix: values.group_prefix,
      auto_sync_groups: values.auto_sync_groups,
      get_user_info: values.get_user_info,
    }
    if (values.client_secret) body.client_secret = values.client_secret
    updateProvider.mutate({ id, body })
  }

  // ── SSO Settings ─────────────────────────────────────────────────────────
  const { data: ssoSettings } = useQuery<SSOSettings>({
    queryKey: ['sso', 'settings'],
    queryFn: () => api.get<SSOSettings>('/api/v1/sso/settings'),
  })

  const updateSettings = useMutation({
    mutationFn: (body: SSOSettings) => api.put('/api/v1/sso/settings', body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['sso', 'settings'] })
      setSaveSuccess('Settings saved')
      setTimeout(() => setSaveSuccess(null), 3000)
    },
    onError: (e: unknown) => {
      console.error('Failed to update settings:', e)
      setFormError(String(e))
    },
  })

// ─── MOTD Section ───────────────────────────────────────────────────────────

interface MOTDMessage {
  id: string
  title: string
  content: string
  priority: number
  visibility: string
  pages: string[]
  show_on_login: boolean
  created_at: string
  expires_at: string | null
}

function MOTDSection() {
  const qc = useQueryClient()
  const [deleteMotdConfirm, setDeleteMotdConfirm] = useState<string | null>(null)
  const [showForm, setShowForm] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [formData, setFormData] = useState({
    title: '', content: '', priority: 0, visibility: 'all',
    show_on_login: false, expires_at: '', pages: [] as string[],
  })
  const [formError, setFormError] = useState<string | null>(null)

  const { data: motds = [], isLoading } = useQuery<MOTDMessage[]>({
    queryKey: ['org', 'motd'],
    queryFn: () => api.get('/api/v1/admin/motd'),
  })

  const createMotd = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.post('/api/v1/admin/motd', body),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['org', 'motd'] }); resetForm() },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const updateMotd = useMutation({
    mutationFn: ({ id, body }: { id: string; body: Record<string, unknown> }) =>
      api.put(`/api/v1/admin/motd/${id}`, body),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['org', 'motd'] }); resetForm() },
    onError: (e: unknown) => setFormError(String(e)),
  })

  const deleteMotd = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/admin/motd/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['org', 'motd'] }),
  })

  function resetForm() {
    setFormData({ title: '', content: '', priority: 0, visibility: 'all', show_on_login: false, expires_at: '', pages: [] })
    setShowForm(false); setEditingId(null); setFormError(null)
  }

  function handleEdit(m: MOTDMessage) {
    setFormData({
      title: m.title || '', content: m.content, priority: m.priority,
      visibility: m.visibility, show_on_login: m.show_on_login,
      expires_at: m.expires_at ? m.expires_at.slice(0, 16) : '', pages: m.pages ?? [],
    })
    setEditingId(m.id); setShowForm(true); setFormError(null)
  }

  function handleSubmit() {
    if (!formData.content.trim()) { setFormError('Content is required'); return }
    const body: Record<string, unknown> = {
      title: formData.title, content: formData.content, priority: formData.priority,
      visibility: formData.visibility, show_on_login: formData.show_on_login, pages: formData.pages,
    }
    if (formData.expires_at) body.expires_at = new Date(formData.expires_at).toISOString()
    if (editingId) { updateMotd.mutate({ id: editingId, body }) } else { createMotd.mutate(body) }
  }

  const pages = ['/'  , '/notebooks', '/connectors', '/dashboards', '/audit', '/members', '/admin', '/groups', '/settings', '/agents', '/models', '/skills', '/mcps']

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
        <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
          {motds.length} message{motds.length !== 1 ? 's' : ''}
        </span>
        {!showForm && (
          <button style={styles.addBtn} onClick={() => { setShowForm(true); setFormError(null) }}>+ Add MOTD</button>
        )}
      </div>

      {showForm && (
        <div style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, padding: 16, marginBottom: 16 }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <label style={formLabel}>
              Title
              <input style={formInput} value={formData.title} onChange={e => setFormData(f => ({ ...f, title: e.target.value }))} placeholder="System Update" />
            </label>
            <label style={formLabel}>
              Content <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(supports Markdown)</span>
              <textarea style={{ ...formInput, minHeight: 80, resize: 'vertical' }} value={formData.content} onChange={e => setFormData(f => ({ ...f, content: e.target.value }))} placeholder="Scheduled maintenance at 3pm..." />
            </label>
            <div style={{ display: 'flex', gap: 12 }}>
              <label style={{ ...formLabel, flex: 1 }}>
                Priority
                <input style={formInput} type="number" value={formData.priority} onChange={e => setFormData(f => ({ ...f, priority: parseInt(e.target.value) || 0 }))} />
              </label>
              <label style={{ ...formLabel, flex: 1 }}>
                Visibility
                <select style={formInput} value={formData.visibility} onChange={e => setFormData(f => ({ ...f, visibility: e.target.value }))}>
                  <option value="all">All pages</option>
                  <option value="specific">Specific pages</option>
                </select>
              </label>
            </div>
            {formData.visibility === 'specific' && (
              <div>
                <div style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 6 }}>Show on pages</div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                  {pages.map(page => (
                    <label key={page} style={{
                      display: 'flex', alignItems: 'center', gap: 4, padding: '4px 8px', borderRadius: 4, cursor: 'pointer',
                      background: formData.pages.includes(page) ? 'var(--accent)' : 'var(--bg-card)',
                      color: formData.pages.includes(page) ? '#fff' : 'var(--text-primary)',
                      fontSize: 12, fontWeight: formData.pages.includes(page) ? 600 : 400, userSelect: 'none',
                    }}>
                      <input type="checkbox" style={{ display: 'none' }} checked={formData.pages.includes(page)}
                        onChange={e => setFormData(f => ({ ...f, pages: e.target.checked ? [...f.pages, page] : f.pages.filter(p => p !== page) }))} />
                      {page === '/' ? 'Home' : page.charAt(1).toUpperCase() + page.slice(2)}
                    </label>
                  ))}
                </div>
              </div>
            )}
            <label style={{ ...formLabel, flexDirection: 'row', alignItems: 'center', gap: 8 }}>
              <input type="checkbox" checked={formData.show_on_login} onChange={e => setFormData(f => ({ ...f, show_on_login: e.target.checked }))} />
              Show on login page
            </label>
            <label style={formLabel}>
              Expires at <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(leave empty for no expiry)</span>
              <input style={formInput} type="datetime-local" value={formData.expires_at} onChange={e => setFormData(f => ({ ...f, expires_at: e.target.value }))} />
            </label>
          </div>
          {formError && <div style={{ color: 'var(--error)', fontSize: 12, marginTop: 8 }}>{formError}</div>}
          <div style={{ display: 'flex', gap: 8, marginTop: 16 }}>
            <button style={{
              padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer',
              opacity: (createMotd.isPending || updateMotd.isPending) ? 0.6 : 1,
            }} onClick={handleSubmit} disabled={createMotd.isPending || updateMotd.isPending}>
              {(createMotd.isPending || updateMotd.isPending) ? 'Saving…' : editingId ? 'Save Changes' : 'Create MOTD'}
            </button>
            <button style={{
              padding: '7px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)',
            }} onClick={resetForm} disabled={createMotd.isPending || updateMotd.isPending}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {isLoading ? (
        <div style={{ fontSize: 13, color: 'var(--text-muted)', padding: 12 }}>Loading…</div>
      ) : motds.length === 0 && !showForm ? (
        <div style={{ fontSize: 13, color: 'var(--text-muted)', padding: 12 }}>No MOTD messages configured.</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {motds.map(m => (
            <div key={m.id} style={styles.row}>
              <div style={styles.rowInfo}>
                <span style={styles.rowName}>{m.title || '(untitled)'}</span>
                <span style={styles.rowMeta}>
                  {m.content.slice(0, 80)}{m.content.length > 80 ? '…' : ''}
                  {' · '}Priority: {m.priority}
                  {m.expires_at && ` · Expires: ${new Date(m.expires_at).toLocaleDateString()}`}
                  {' · '}{m.show_on_login ? 'Login page' : 'App only'}
                  {m.visibility === 'specific' ? ' · Specific pages' : ' · All pages'}
                </span>
              </div>
              <div style={{ display: 'flex', gap: 6 }}>
                <button style={styles.iconBtn} onClick={() => handleEdit(m)}>Edit</button>
                <button style={{ ...styles.iconBtn, color: 'var(--error)', borderColor: 'var(--error)' }}
                  onClick={() => setDeleteMotdConfirm(m.id)} disabled={deleteMotd.isPending}>Delete</button>
              </div>
            </div>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={!!deleteMotdConfirm}
        title="Delete MOTD"
        message="Delete this MOTD message?"
        confirmLabel="Delete"
        destructive
        onConfirm={() => { if (deleteMotdConfirm) deleteMotd.mutate(deleteMotdConfirm); setDeleteMotdConfirm(null) }}
        onCancel={() => setDeleteMotdConfirm(null)}
      />
    </div>
  )
}

const formLabel: React.CSSProperties = {
  display: 'flex', flexDirection: 'column', gap: 4, fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)',
}

const formInput: React.CSSProperties = {
  padding: '7px 10px', fontSize: 13, background: 'var(--bg-input)', color: 'var(--text-primary)',
  border: '1px solid var(--border)', borderRadius: 4, fontFamily: 'var(--font-sans)',
}

  // ── Render ────────────────────────────────────────────────────────────────
  return (
    <AppShell>
      <div style={{ maxWidth: 720, margin: '0 auto', padding: '32px 40px' }}>
        <SectionHeader title="Organization Settings" />

        {saveSuccess && (
          <div style={{ background: '#d4edda', color: '#155724', padding: '10px 16px', borderRadius: 4, marginBottom: 20, fontSize: 13 }}>
            {saveSuccess}
          </div>
        )}

        {/* ── A. Platform Providers ── */}
        <section style={styles.section}>
          <div style={styles.sectionTitle}>Platform Providers</div>
          <p style={styles.sectionDesc}>
            SSO providers configured by the platform administrator. Enable or disable them for your organization.
          </p>
          {loadingPlatform ? (
            <div style={styles.empty}>Loading…</div>
          ) : platformProviders.length === 0 ? (
            <div style={styles.empty}>No platform providers available.</div>
          ) : (
            <div style={styles.list}>
              {platformProviders.map(p => (
                <div key={p.id} style={styles.row}>
                  <div style={styles.rowInfo}>
                    <span style={styles.rowName}>{p.name}</span>
                    <span style={styles.rowMeta}>{p.provider_type}</span>
                  </div>
                  <button
                    style={p.enabled_for_org ? styles.disableBtn : styles.enableBtn}
                    onClick={() =>
                      togglePlatformProvider.mutate({ id: p.id, enable: !p.enabled_for_org })
                    }
                    disabled={togglePlatformProvider.isPending}
                  >
                    {p.enabled_for_org ? 'Disable' : 'Enable'}
                  </button>
                </div>
              ))}
            </div>
          )}
        </section>

        {/* ── B. Custom Providers ── */}
        <section style={styles.section}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <div style={styles.sectionTitle}>Custom SSO Providers</div>
            {!showAddForm && (
              <button style={styles.addBtn} onClick={() => { setShowAddForm(true); setFormError(null) }}>
                + Add Provider
              </button>
            )}
          </div>
          <p style={styles.sectionDesc}>
            OIDC providers managed by your organization.
          </p>

          {showAddForm && (
            <ProviderForm
              initial={emptyForm}
              isEdit={false}
              onSave={handleCreate}
              onCancel={() => { setShowAddForm(false); setFormError(null) }}
              saving={createProvider.isPending}
              error={formError}
            />
          )}

          {loadingCustom ? (
            <div style={styles.empty}>Loading…</div>
          ) : customProviders.length === 0 && !showAddForm ? (
            <div style={styles.empty}>No custom providers yet.</div>
          ) : (
            <div style={styles.list}>
              {customProviders.map(p => (
                <div key={p.id} style={{ ...styles.row, flexDirection: 'column', alignItems: 'stretch', gap: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center' }}>
                    <div style={styles.rowInfo}>
                      <span style={styles.rowName}>{p.name}</span>
                      <span style={styles.rowMeta}>
                        {p.client_id} · {p.discovery_url}
                      </span>
                    </div>
                    <div style={{ display: 'flex', gap: 6, marginLeft: 'auto' }}>
                      <span style={p.enabled ? styles.badgeEnabled : styles.badgeDisabled}>
                        {p.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                      <button
                        style={styles.iconBtn}
                        onClick={() => {
                          setEditingId(editingId === p.id ? null : p.id)
                          setFormError(null)
                        }}
                      >
                        Edit
                      </button>
                      {confirmDeleteId === p.id ? (
                        <>
                          <button
                            style={{ ...styles.iconBtn, color: 'var(--error)', borderColor: 'var(--error)' }}
                            onClick={() => deleteProvider.mutate(p.id)}
                            disabled={deleteProvider.isPending}
                          >
                            Confirm
                          </button>
                          <button style={styles.iconBtn} onClick={() => setConfirmDeleteId(null)}>
                            Cancel
                          </button>
                        </>
                      ) : (
                        <button
                          style={{ ...styles.iconBtn, color: 'var(--error)', borderColor: 'var(--error)' }}
                          onClick={() => setConfirmDeleteId(p.id)}
                        >
                          Delete
                        </button>
                      )}
                    </div>
                  </div>
                  {editingId === p.id && (
                    <ProviderForm
                      initial={providerToForm(p)}
                      isEdit={true}
                      onSave={values => handleUpdate(p.id, values)}
                      onCancel={() => { setEditingId(null); setFormError(null) }}
                      saving={updateProvider.isPending}
                      error={formError}
                    />
                  )}
                </div>
              ))}
            </div>
          )}
        </section>

        {/* ── C. Login Settings ── */}
        <section style={styles.section}>
          <div style={styles.sectionTitle}>Login Settings</div>
          <p style={styles.sectionDesc}>
            Configure how members log in to your organization.
          </p>
          <label style={styles.checkRow}>
            <input
              type="checkbox"
              checked={ssoSettings?.sso_password_login ?? true}
              onChange={e =>
                updateSettings.mutate({ sso_password_login: e.target.checked })
              }
            />
            <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>
              Allow password login when SSO is configured
            </span>
          </label>
        </section>

        {/* ── D. Public Sharing ── */}
        <section style={styles.section}>
          <div style={styles.sectionTitle}>Public Sharing</div>
          <p style={styles.sectionDesc}>
            Allow sharing notebooks and dashboards via public links.
          </p>
          <label style={styles.checkRow}>
            <input
              type="checkbox"
              checked={sharingEnabled}
              onChange={e => handleToggleSharing(e.target.checked)}
            />
            <span style={{ fontSize: 13, color: 'var(--text-primary)' }}>
              Enable public sharing
            </span>
          </label>
        </section>

        {/* ── E. Message of the Day ── */}
        <section style={styles.section}>
          <div style={styles.sectionTitle}>Message of the Day</div>
          <p style={styles.sectionDesc}>
            Banners shown to all users. Higher priority messages appear first.
          </p>
          <MOTDSection />
        </section>
      </div>
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  section: {
    marginTop: 32,
    paddingTop: 24,
    borderTop: '1px solid var(--border-light)',
  },
  sectionTitle: {
    fontSize: 14,
    fontWeight: 700,
    color: 'var(--text-primary)',
    marginBottom: 4,
  },
  sectionDesc: {
    fontSize: 13,
    color: 'var(--text-muted)',
    marginBottom: 16,
    marginTop: 0,
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
  enableBtn: {
    padding: '5px 12px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 12,
    fontWeight: 600,
    cursor: 'pointer',
    flexShrink: 0,
  },
  disableBtn: {
    padding: '5px 12px',
    background: 'transparent',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 12,
    cursor: 'pointer',
    flexShrink: 0,
  },
  addBtn: {
    padding: '5px 12px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 12,
    fontWeight: 600,
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
  },
  badgeEnabled: {
    padding: '2px 8px',
    background: 'var(--success-light, #d1fae5)',
    color: 'var(--success, #059669)',
    borderRadius: 10,
    fontSize: 11,
    fontWeight: 600,
  },
  badgeDisabled: {
    padding: '2px 8px',
    background: 'var(--bg-secondary)',
    color: 'var(--text-muted)',
    borderRadius: 10,
    fontSize: 11,
    fontWeight: 600,
  },
  empty: {
    fontSize: 13,
    color: 'var(--text-muted)',
    padding: '10px 0',
  },
  checkRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    cursor: 'pointer',
  },
}
