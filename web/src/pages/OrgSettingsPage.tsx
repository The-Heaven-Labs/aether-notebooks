import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { api } from '../api/client'
import { Loader2, CheckCircle2, XCircle } from 'lucide-react'
import type { SSOProvider, PlatformSSOProvider, SSOSettings } from '../types'

// ─── Provider form state ────────────────────────────────────────────────────

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
