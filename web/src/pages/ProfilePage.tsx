import { useState, useEffect, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { AppShell } from '../components/AppShell'
import { SectionHeader } from '../components/SectionHeader'
import { ErrorBanner } from '../components/ErrorBanner'
import { Check } from 'lucide-react'
import { api } from '../api/client'

interface UserProfile {
  id: string
  email: string
  name: string
  status?: string
  theme: string
}

export function ProfilePage() {
  const qc = useQueryClient()
  const { data: user } = useQuery<UserProfile>({
    queryKey: ['profile'],
    queryFn: () => api.get('/api/v1/users/me'),
  })

  const { data: myGroups = [] } = useQuery<Array<{ id: string; name: string }>>({
    queryKey: ['groups', 'mine'],
    queryFn: () => api.get('/api/v1/groups?member=me'),
  })

  const [name, setName] = useState('')
  const [status, setStatus] = useState('')
  const [theme, setTheme] = useState<'light' | 'dark'>(
    (localStorage.getItem('hnb_theme') ?? 'light') as 'light' | 'dark'
  )
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')

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

  const handleThemeToggle = (newTheme: 'light' | 'dark') => {
    setTheme(newTheme)
    localStorage.setItem('hnb_theme', newTheme)
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
        </div>
      </div>
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
}
