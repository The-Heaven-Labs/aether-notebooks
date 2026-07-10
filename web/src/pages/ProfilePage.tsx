import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { ErrorBanner } from '../components/ErrorBanner'
import { Check, Copy, Trash2, Plus, AlertTriangle, Shield } from 'lucide-react'
import { api } from '../api/client'
import { useAuth } from '../hooks/useAuth'
import { ConfirmDialog } from '../components/ConfirmDialog'

interface UserProfile {
  id: string
  email: string
  name: string
  status?: string
  theme: string
}

interface ApiToken {
  id: string
  name: string
  last_used_at: string | null
  expires_at: string | null
  created_at: string
}

interface CreatedToken {
  id: string
  name: string
  token: string
  expires_at: string | null
  created_at: string
}

export function ProfilePage() {
  const { user: authUser } = useAuth()
  const qc = useQueryClient()
  const { data: user } = useQuery<UserProfile>({
    queryKey: ['profile'],
    queryFn: () => api.get('/api/v1/users/me'),
  })

  const { data: myGroups = [] } = useQuery<Array<{ id: string; name: string }>>({
    queryKey: ['groups', 'mine'],
    queryFn: () => api.get('/api/v1/groups?member=me'),
  })

  const { data: tokens = [], refetch: refetchTokens } = useQuery<ApiToken[]>({
    queryKey: ['api-tokens'],
    queryFn: () => api.get('/api/v1/tokens'),
  })

  const createToken = useMutation({
    mutationFn: (body: { name: string; expires_at?: string }) => api.post<CreatedToken>('/api/v1/tokens', body),
    onSuccess: (data) => {
      setCreatedToken(data)
      setNewTokenName('')
      setTokenExpiry('never')
      setShowTokenForm(false)
      refetchTokens()
    },
  })

  const deleteToken = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/tokens/${id}`),
    onSuccess: () => refetchTokens(),
  })

  const [name, setName] = useState('')
  const [status, setStatus] = useState('')
  const [theme, setTheme] = useState<'light' | 'dark'>(
    (localStorage.getItem('aether_theme') ?? 'dark') as 'light' | 'dark'
  )
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const [adminMode, setAdminMode] = useState(() => localStorage.getItem('aether_admin_mode') !== 'false')

  // Token management state
  const [showTokenForm, setShowTokenForm] = useState(false)
  const [newTokenName, setNewTokenName] = useState('')
  const [tokenExpiry, setTokenExpiry] = useState<'never' | '7d' | '30d' | '90d' | '1y'>('never')
  const [createdToken, setCreatedToken] = useState<CreatedToken | null>(null)
  const [tokenCopied, setTokenCopied] = useState(false)
  const [revokeTokenConfirm, setRevokeTokenConfirm] = useState<ApiToken | null>(null)

  const timeoutRef = useRef<number | null>(null)

  useEffect(() => {
    return () => {
      if (timeoutRef.current !== null) clearTimeout(timeoutRef.current)
    }
  }, [])

  useEffect(() => {
    if (user) {
      setName(user.name)
      setStatus(user.status ?? '')
    }
  }, [user])

  const update = useMutation({
    mutationFn: (patch: Partial<UserProfile>) => api.put('/api/v1/users/me', patch),
    onMutate: () => setSaveStatus('saving'),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['profile'] })
      setSaveStatus('saved')
      timeoutRef.current = window.setTimeout(() => setSaveStatus('idle'), 2500)
    },
    onError: () => {
      setSaveStatus('error')
      timeoutRef.current = window.setTimeout(() => setSaveStatus('idle'), 3000)
    },
  })

  const handleCreateToken = () => {
    if (!newTokenName.trim()) return
    const body: { name: string; expires_at?: string } = { name: newTokenName.trim() }
    if (tokenExpiry !== 'never') {
      const now = new Date()
      const days = tokenExpiry === '7d' ? 7 : tokenExpiry === '30d' ? 30 : tokenExpiry === '90d' ? 90 : 365
      now.setDate(now.getDate() + days)
      body.expires_at = now.toISOString()
    }
    createToken.mutate(body)
  }

  const handleThemeToggle = (newTheme: 'light' | 'dark') => {
    setTheme(newTheme)
    localStorage.setItem('aether_theme', newTheme)
    document.documentElement.setAttribute('data-theme', newTheme)
    update.mutate({ theme: newTheme })
  }

  const handleSave = () => {
    const patch: Partial<UserProfile> = {}
    if (name !== user?.name) patch.name = name
    if (status !== (user?.status ?? '')) patch.status = status || undefined
    if (Object.keys(patch).length > 0) update.mutate(patch)
  }

  return (
    <AppShell>
      <div style={{ maxWidth: 600, margin: '0 auto', padding: '32px 40px' }}>
        <SectionHeader title="Profile" />
        <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
          <label style={styles.label}>
            Name
            <input style={styles.input} value={name}
              onChange={e => setName(e.target.value)} />
          </label>
            <label style={styles.label}>
            Status <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(optional)</span>
            <input style={styles.input} value={status} placeholder="e.g. On vacation"
              onChange={e => setStatus(e.target.value)} maxLength={100} />
            <span style={{
              fontSize: 11,
              color: status.length > 90 ? 'var(--error)' : 'var(--text-muted)',
              textAlign: 'right',
              marginTop: 2,
              display: 'block',
            }}>
              {status.length}/100
            </span>
          </label>
          <label style={styles.label}>
            Email
            <input style={{ ...styles.input, color: 'var(--text-muted)', cursor: 'default' }}
              value={user?.email ?? ''} readOnly />
          </label>
          <div style={styles.label}>
            Theme
            <div style={{ display: 'flex', gap: 8, marginTop: 6 }}>
              {(['light', 'dark'] as const).map(t => (
                <button key={t} type="button"
                  style={theme === t ? styles.themeActive : styles.themeBtn}
                  onClick={() => handleThemeToggle(t)}>
                  {t === 'light' ? 'Light' : 'Dark'}
                </button>
              ))}
            </div>
          </div>
          {authUser?.role === 'admin' && (
            <div style={styles.label}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                <Shield size={14} style={{ color: 'var(--text-muted)' }} />
                Admin Mode
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                {(['on', 'off'] as const).map(v => (
                  <button key={v} type="button"
                    style={adminMode === (v === 'on') ? styles.themeActive : styles.themeBtn}
                    onClick={() => { setAdminMode(v === 'on'); localStorage.setItem('aether_admin_mode', String(v === 'on')) }}>
                    {v === 'on' ? 'ON' : 'OFF'}
                  </button>
                ))}
              </div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 4 }}>
                When OFF, your admin role is disabled and you respect ACLs like regular users.
              </div>
            </div>
          )}
          {saveStatus === 'saved' && (
            <ErrorBanner message="Profile updated successfully" variant="info" onDismiss={() => setSaveStatus('idle')} />
          )}
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 8 }}>
            <button
              type="button"
              style={{
                ...styles.saveBtn,
                opacity: saveStatus === 'saving' ? 0.6 : 1,
                background: saveStatus === 'saved' ? 'var(--success)' : 'var(--accent)',
                transition: 'background 0.3s',
              }}
              onClick={handleSave}
              disabled={saveStatus === 'saving'}
            >
              {saveStatus === 'saving' ? 'Saving…' : 'Save'}
            </button>
            {saveStatus === 'saved' && (
              <span style={{ fontSize: 13, color: 'var(--success)', fontWeight: 500, display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                <Check size={14} /> Saved
              </span>
            )}
            {saveStatus === 'error' && (
              <span style={{ fontSize: 13, color: 'var(--error)', fontWeight: 500 }}>Save failed</span>
            )}
          </div>
          <div style={{ marginTop: 32, borderTop: '1px solid var(--border-light)', paddingTop: 24 }}>
            <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 12 }}>My Groups</div>
            {myGroups.length === 0 ? (
              <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>Not a member of any group.</div>
            ) : (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {myGroups.map(g => (
                  <span key={g.id} style={styles.groupTag}>{g.name}</span>
                ))}
              </div>
            )}
          </div>

          {/* Personal Access Tokens */}
          <div style={{ marginTop: 32, borderTop: '1px solid var(--border-light)', paddingTop: 24 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
              <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' }}>Personal Access Tokens</div>
              <button
                type="button"
                style={styles.tokenCreateBtn}
                onClick={() => { setShowTokenForm(true); setCreatedToken(null) }}
              >
                <Plus size={12} /> Create Token
              </button>
            </div>

            {/* Newly created token — show ONCE */}
            {createdToken && (
              <div style={styles.tokenAlert}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 8 }}>
                  <AlertTriangle size={14} style={{ color: 'var(--warning, #f59e0b)', flexShrink: 0 }} />
                  <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-primary)' }}>
                    Copy your token now — you won't see it again!
                  </span>
                </div>
                <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <input
                    style={{ ...styles.input, flex: 1, fontFamily: 'var(--font-mono)', fontSize: 12 }}
                    value={createdToken.token}
                    readOnly
                    onClick={e => (e.target as HTMLInputElement).select()}
                  />
                  <button
                    type="button"
                    style={styles.tokenCopyBtn}
                    onClick={() => {
                      navigator.clipboard.writeText(createdToken.token)
                      setTokenCopied(true)
                      setTimeout(() => setTokenCopied(false), 2000)
                    }}
                  >
                    {tokenCopied ? <><Check size={12} /> Copied</> : <><Copy size={12} /> Copy</>}
                  </button>
                </div>
              </div>
            )}

            {/* Create token form */}
            {showTokenForm && !createdToken && (
              <div style={styles.tokenForm}>
                <input
                  style={styles.input}
                  placeholder="Token name (e.g. CI pipeline)"
                  value={newTokenName}
                  onChange={e => setNewTokenName(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' && newTokenName.trim()) handleCreateToken()
                    if (e.key === 'Escape') setShowTokenForm(false)
                  }}
                  autoFocus
                />
                <label style={{ fontSize: 11, color: 'var(--text-secondary)', marginTop: 4 }}>
                  Expires
                  <select
                    style={{ ...styles.input, marginTop: 4, fontSize: 12, padding: '4px 8px' }}
                    value={tokenExpiry}
                    onChange={e => setTokenExpiry(e.target.value as typeof tokenExpiry)}
                  >
                    <option value="never">Never</option>
                    <option value="7d">7 days</option>
                    <option value="30d">30 days</option>
                    <option value="90d">90 days</option>
                    <option value="1y">1 year</option>
                  </select>
                </label>
                <div style={{ display: 'flex', gap: 8 }}>
                  <button
                    type="button"
                    style={styles.tokenCreateBtn}
                    disabled={!newTokenName.trim() || createToken.isPending}
                    title={!newTokenName.trim() ? 'Token name is required' : undefined}
                    onClick={handleCreateToken}
                  >
                    {createToken.isPending ? 'Creating…' : 'Create'}
                  </button>
                  <button
                    type="button"
                    style={styles.tokenCancelBtn}
                    onClick={() => { setShowTokenForm(false); setTokenExpiry('never') }}
                  >
                    Cancel
                  </button>
                </div>
              </div>
            )}

            {/* Token list */}
            {tokens.length === 0 && !showTokenForm ? (
              <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>No personal access tokens.</div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 8 }}>
                {tokens.map(t => (
                  <div key={t.id} style={styles.tokenRow}>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ fontSize: 13, fontWeight: 500, color: 'var(--text-primary)' }}>{t.name}</div>
                      <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>
                        Created {new Date(t.created_at).toLocaleDateString()}
                        {t.last_used_at && <> · Last used {new Date(t.last_used_at).toLocaleDateString()}</>}
                        {t.expires_at && <> · Expires {new Date(t.expires_at).toLocaleDateString()}</>}
                      </div>
                    </div>
                    <button
                      type="button"
                      style={styles.tokenDeleteBtn}
                      title="Revoke token"
                      onClick={() => setRevokeTokenConfirm(t)}
                      disabled={deleteToken.isPending}
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={!!revokeTokenConfirm}
        title="Revoke token"
        message={`Revoke token "${revokeTokenConfirm?.name}"? This action cannot be undone.`}
        confirmLabel="Revoke"
        destructive
        onConfirm={() => { if (revokeTokenConfirm) deleteToken.mutate(revokeTokenConfirm.id); setRevokeTokenConfirm(null) }}
        onCancel={() => setRevokeTokenConfirm(null)}
      />
    </AppShell>
  )
}

const styles: Record<string, React.CSSProperties> = {
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  input: { padding: '8px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 14, color: 'var(--text-primary)', background: 'var(--bg-input)' },
  saveBtn: { padding: '7px 18px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer' },
  themeBtn: { padding: '6px 16px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, cursor: 'pointer', color: 'var(--text-secondary)' },
  themeActive: { padding: '6px 16px', background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, fontSize: 13, cursor: 'pointer' },
  groupTag: { padding: '3px 10px', background: 'var(--accent-light)', color: 'var(--accent)', borderRadius: 12, fontSize: 12, fontWeight: 500 },
  tokenCreateBtn: { display: 'inline-flex', alignItems: 'center', gap: 4, padding: '4px 10px', fontSize: 11, fontWeight: 600, background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer' },
  tokenCancelBtn: { padding: '4px 10px', fontSize: 11, fontWeight: 600, background: 'none', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', color: 'var(--text-secondary)' },
  tokenCopyBtn: { display: 'inline-flex', alignItems: 'center', gap: 4, padding: '6px 12px', fontSize: 11, fontWeight: 600, background: 'var(--accent)', color: '#fff', border: 'none', borderRadius: 4, cursor: 'pointer', whiteSpace: 'nowrap' as const },
  tokenAlert: { padding: 12, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, marginBottom: 12 },
  tokenForm: { display: 'flex', flexDirection: 'column' as const, gap: 8, padding: 12, background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, marginBottom: 8 },
  tokenRow: { display: 'flex', alignItems: 'center', gap: 12, padding: '8px 0', borderBottom: '1px solid var(--border-light)' },
  tokenDeleteBtn: { display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '4px 6px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', color: 'var(--error, #ef4444)' },
}
