import { useState, useEffect } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { ApiError, setToken } from '../api/client'

export function LoginPage() {
  const { login, register } = useAuth()
  const navigate = useNavigate()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  // Handle OIDC callback: pick up ?token= from the URL query string
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const token = params.get('token')
    if (token) {
      setToken(token)
      // Clean up the URL and navigate to home
      window.history.replaceState({}, '', window.location.pathname)
      navigate('/')
    }
  }, [navigate])

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      if (mode === 'login') {
        await login(email, password)
        navigate('/')
      } else {
        const onboardingToken = await register(email, password, name)
        if (onboardingToken) {
          navigate('/onboarding')
        } else {
          navigate('/')
        }
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Something went wrong')
    } finally {
      setLoading(false)
    }
  }

  function handleSSOLogin() {
    window.location.href = '/api/v1/auth/oidc/default'
  }

  return (
    <div style={styles.page}>
      {/* Brand panel */}
      <div style={styles.brand}>
        <div style={styles.brandInner}>
          <div style={styles.logoMark}>
            <span style={styles.logoIcon}>▦</span>
          </div>
          <h1 style={styles.brandTitle}>Heaven's<br />Notebooks</h1>
          <p style={styles.brandTagline}>
            Collaborative SQL notebooks<br />built for data teams.
          </p>
          <div style={styles.brandFeatures}>
            <div style={styles.feature}>
              <span style={styles.featureDot} />
              Live collaborative editing
            </div>
            <div style={styles.feature}>
              <span style={styles.featureDot} />
              Multi-database connectors
            </div>
            <div style={styles.feature}>
              <span style={styles.featureDot} />
              Scheduled query runs
            </div>
          </div>
        </div>
      </div>

      {/* Form panel */}
      <div style={styles.formPanel}>
        <div style={styles.formInner}>
          <div style={styles.tabs}>
            <button
              style={{ ...styles.tab, ...(mode === 'login' ? styles.tabActive : {}) }}
              onClick={() => { setMode('login'); setError('') }}
            >
              Sign In
            </button>
            <button
              style={{ ...styles.tab, ...(mode === 'register' ? styles.tabActive : {}) }}
              onClick={() => { setMode('register'); setError('') }}
            >
              Create account
            </button>
          </div>

          <p style={styles.formHeading}>
            {mode === 'login' ? 'Welcome back' : 'Get started free'}
          </p>

          <form onSubmit={handleSubmit} style={styles.form}>
            {mode === 'register' && (
              <>
                <div style={styles.field}>
                  <label style={styles.label}>Your name</label>
                  <input
                    style={styles.input}
                    type="text"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    required
                    placeholder="Jane Doe"
                  />
                </div>
              </>
            )}
            <div style={styles.field}>
              <label style={styles.label}>Email</label>
              <input
                style={styles.input}
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                placeholder="you@example.com"
              />
            </div>
            <div style={styles.field}>
              <label style={styles.label}>Password</label>
              <input
                style={styles.input}
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                placeholder="••••••••"
              />
            </div>
            {error && <p style={styles.error}>{error}</p>}
            <button type="submit" style={styles.submit} disabled={loading}>
              {loading ? 'Please wait…' : mode === 'login' ? 'Sign In' : 'Create Account'}
            </button>
          </form>

          <div style={styles.divider}>
            <span style={styles.dividerText}>or</span>
          </div>

          <button type="button" style={styles.ssoButton} onClick={handleSSOLogin}>
            Sign in with SSO
          </button>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: '100vh',
    display: 'flex',
  },
  brand: {
    flex: '0 0 420px',
    background: 'var(--nav-bg)',
    display: 'flex',
    alignItems: 'center',
    padding: '60px 48px',
  },
  brandInner: {
    display: 'flex',
    flexDirection: 'column',
    gap: 24,
  },
  logoMark: {
    width: 48,
    height: 48,
    background: 'var(--accent)',
    borderRadius: 12,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  logoIcon: {
    fontSize: 24,
    color: 'white',
    lineHeight: 1,
  },
  brandTitle: {
    fontSize: 40,
    fontWeight: 700,
    color: '#f8f6f1',
    lineHeight: 1.15,
    letterSpacing: '-0.5px',
  },
  brandTagline: {
    fontSize: 16,
    color: '#8a8278',
    lineHeight: 1.6,
  },
  brandFeatures: {
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
    marginTop: 8,
  },
  feature: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    fontSize: 14,
    color: '#7a7068',
  },
  featureDot: {
    width: 6,
    height: 6,
    borderRadius: '50%',
    background: 'var(--accent)',
    flexShrink: 0,
  },
  formPanel: {
    flex: 1,
    background: 'var(--bg-primary)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '60px 48px',
  },
  formInner: {
    width: '100%',
    maxWidth: 360,
  },
  tabs: {
    display: 'flex',
    gap: 4,
    marginBottom: 28,
    padding: 4,
    background: 'var(--bg-secondary)',
    borderRadius: 8,
  },
  tab: {
    flex: 1,
    padding: '7px 14px',
    border: 'none',
    background: 'transparent',
    fontSize: 13,
    fontWeight: 500,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
    borderRadius: 6,
    transition: 'all 0.15s',
  },
  tabActive: {
    background: 'white',
    color: 'var(--text-primary)',
    boxShadow: 'var(--shadow-sm)',
  },
  formHeading: {
    fontSize: 22,
    fontWeight: 700,
    color: 'var(--text-primary)',
    marginBottom: 24,
    letterSpacing: '-0.2px',
  },
  form: {
    display: 'flex',
    flexDirection: 'column',
    gap: 14,
  },
  field: {
    display: 'flex',
    flexDirection: 'column',
    gap: 5,
  },
  label: {
    fontSize: 13,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    letterSpacing: '0.01em',
  },
  input: {
    padding: '9px 12px',
    borderRadius: 7,
    border: '1.5px solid var(--border)',
    fontSize: 14,
    outline: 'none',
    background: 'white',
    color: 'var(--text-primary)',
    transition: 'border-color 0.15s',
  },
  error: {
    color: 'var(--error)',
    fontSize: 13,
    padding: '8px 12px',
    background: 'var(--error-light)',
    borderRadius: 6,
    border: '1px solid var(--error-border)',
  },
  submit: {
    padding: '11px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 7,
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
    marginTop: 6,
    letterSpacing: '0.01em',
    transition: 'background 0.15s',
  },
  divider: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    margin: '20px 0',
  },
  dividerText: {
    flex: 1,
    textAlign: 'center' as const,
    fontSize: 12,
    color: 'var(--text-secondary)',
    position: 'relative' as const,
  },
  ssoButton: {
    width: '100%',
    padding: '11px',
    background: 'transparent',
    color: 'var(--text-primary)',
    border: '1.5px solid var(--border)',
    borderRadius: 7,
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
    letterSpacing: '0.01em',
    transition: 'border-color 0.15s, background 0.15s',
  },
}
