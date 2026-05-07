import { useState } from 'react'
import type React from 'react'
import { ErrorBanner } from '../components/ErrorBanner'

function finishOnboarding(data: { token: string; user: { name: string; email: string }; org: { name: string } }) {
  localStorage.setItem('hnb_token', data.token)
  localStorage.setItem('hnb_user_name', data.user.name)
  localStorage.setItem('hnb_user_email', data.user.email)
  localStorage.setItem('hnb_org_name', data.org.name)
  localStorage.removeItem('hnb_onboarding_token')
  // Full reload so useAuth re-reads the new token from localStorage
  window.location.href = '/'
}

export function OrgOnboardingPage() {
  const [mode, setMode] = useState<'choose' | 'create' | 'join'>('choose')
  const [orgName, setOrgName] = useState('')
  const [inviteToken, setInviteToken] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const onboardingToken = localStorage.getItem('hnb_onboarding_token') ?? ''

  async function createOrg() {
    if (!orgName.trim()) { setError('Organization name is required'); return }
    setError('')
    setLoading(true)
    try {
      const r = await fetch('/api/v1/auth/org/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${onboardingToken}` },
        body: JSON.stringify({ org_name: orgName }),
      })
      const data = await r.json()
      if (!r.ok) { setError(data.error ?? 'Failed to create org'); return }
      finishOnboarding(data)
    } finally {
      setLoading(false)
    }
  }

  async function joinOrg() {
    if (!inviteToken.trim()) { setError('Invite token is required'); return }
    setError('')
    setLoading(true)
    try {
      const r = await fetch('/api/v1/auth/org/join', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${onboardingToken}` },
        body: JSON.stringify({ invite_token: inviteToken }),
      })
      const data = await r.json()
      if (!r.ok) { setError(data.error ?? 'Failed to join org'); return }
      finishOnboarding(data)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={styles.page}>
      <div style={styles.card}>
        <div style={styles.logo}>▦</div>
        <h1 style={styles.heading}>Welcome to Heaven's Notebooks</h1>
        <p style={styles.sub}>Your account is ready. Now set up your workspace.</p>

        {mode === 'choose' && (
          <div style={styles.choices}>
            <button style={styles.choiceBtn} onClick={() => setMode('create')}>
              <span style={styles.choiceTitle}>Create a new organization</span>
              <span style={styles.choiceSub}>Start fresh with your own workspace</span>
            </button>
            <button style={styles.choiceBtn} onClick={() => setMode('join')}>
              <span style={styles.choiceTitle}>Join an existing organization</span>
              <span style={styles.choiceSub}>Use an invite token to join a team</span>
            </button>
          </div>
        )}

        {mode === 'create' && (
          <div style={styles.form}>
            <label style={styles.label}>Organization name</label>
            <input
              style={styles.input}
              type="text"
              value={orgName}
              onChange={e => setOrgName(e.target.value)}
              placeholder="Acme Analytics"
              autoFocus
            />
            {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}
            <button style={styles.primaryBtn} onClick={createOrg} disabled={loading}>
              {loading ? 'Creating…' : 'Create organization'}
            </button>
            <button style={styles.backBtn} onClick={() => { setMode('choose'); setError('') }}>
              ← Back
            </button>
          </div>
        )}

        {mode === 'join' && (
          <div style={styles.form}>
            <label style={styles.label}>Invite token</label>
            <input
              style={styles.input}
              type="text"
              value={inviteToken}
              onChange={e => setInviteToken(e.target.value)}
              placeholder="Paste your invite token here"
              autoFocus
            />
            {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}
            <button style={styles.primaryBtn} onClick={joinOrg} disabled={loading}>
              {loading ? 'Joining…' : 'Join organization'}
            </button>
            <button style={styles.backBtn} onClick={() => { setMode('choose'); setError('') }}>
              ← Back
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    background: 'var(--bg-secondary)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  card: {
    background: 'var(--bg-primary)',
    borderRadius: 4,
    border: '1px solid var(--border)',
    padding: '40px 36px',
    width: '100%',
    maxWidth: 440,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    textAlign: 'center',
    gap: 0,
  },
  logo: {
    width: 48,
    height: 48,
    background: 'var(--accent)',
    borderRadius: 4,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 24,
    color: 'var(--button-primary-text)',
    marginBottom: 20,
  },
  heading: {
    fontSize: 22,
    fontWeight: 700,
    color: 'var(--text-primary)',
    marginBottom: 8,
    letterSpacing: '-0.2px',
  },
  sub: {
    fontSize: 14,
    color: 'var(--text-muted)',
    marginBottom: 28,
    lineHeight: 1.5,
  },
  choices: {
    width: '100%',
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
  },
  choiceBtn: {
    width: '100%',
    padding: '14px 16px',
    background: 'var(--bg-secondary)',
    border: '1.5px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'flex-start',
    gap: 4,
    textAlign: 'left',
    transition: 'border-color 0.15s',
  },
  choiceTitle: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--text-primary)',
  },
  choiceSub: {
    fontSize: 12,
    color: 'var(--text-muted)',
  },
  form: {
    width: '100%',
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
    textAlign: 'left',
  },
  label: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-secondary)',
  },
  input: {
    padding: '9px 12px',
    borderRadius: 4,
    border: '1.5px solid var(--border)',
    fontSize: 14,
    outline: 'none',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    caretColor: 'var(--text-primary)',
  },
  error: {
    color: 'var(--error)',
    fontSize: 13,
    padding: '8px 12px',
    background: 'var(--error-light)',
    borderRadius: 4,
    border: '1px solid var(--error-border)',
    margin: 0,
  },
  primaryBtn: {
    padding: '11px',
    background: 'var(--accent)',
    color: 'var(--button-primary-text)',
    border: 'none',
    borderRadius: 4,
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
    marginTop: 4,
  },
  backBtn: {
    background: 'none',
    border: 'none',
    color: 'var(--text-muted)',
    fontSize: 13,
    cursor: 'pointer',
    padding: '4px 0',
    textAlign: 'left' as const,
  },
}
