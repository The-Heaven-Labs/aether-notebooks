import { useState, useEffect } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { ApiError, api } from '../api/client'
import { ErrorBanner } from '../components/ErrorBanner'

type LoginStep = 'email' | 'password' | 'sso_and_password'

interface SSOProvider {
  id: string
  name: string
  provider_type: string
}

export function LoginPage() {
  const { login, register, loginWithToken } = useAuth()
  const navigate = useNavigate()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [step, setStep] = useState<LoginStep>('email')
  const [email, setEmail] = useState('')
  const [emailInput, setEmailInput] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [probing, setProbing] = useState(false)
  const [ssoProviders, setSsoProviders] = useState<SSOProvider[]>([])

  // Handle OIDC callback: pick up ?token= from the query string
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const token = params.get('token')
    if (token) {
      loginWithToken(token)
      // Fetch supplementary user info (name, email, platform admin flag)
      fetch('/api/v1/users/me', {
        headers: { Authorization: `Bearer ${token}` }
      })
        .then(res => res.json())
        .then(user => {
          if (user.name) localStorage.setItem('hnb_user_name', user.name)
          if (user.email) localStorage.setItem('hnb_user_email', user.email)
          if (user.is_platform_admin) localStorage.setItem('hnb_is_platform_admin', 'true')
        })
        .catch(err => console.warn('[OIDC] Failed to fetch user info:', err))
        .finally(() => {
          window.history.replaceState({}, '', window.location.pathname)
          navigate('/')
        })
    }
  }, [navigate])

  async function handleEmailContinue(e: FormEvent) {
    e.preventDefault()
    setError('')
    setProbing(true)
    try {
      const providers = await api.get<SSOProvider[]>(
        `/api/v1/auth/sso-providers?email=${encodeURIComponent(emailInput)}`
      )
      setSsoProviders(providers)
      setEmail(emailInput)
      setStep(providers.length > 0 ? 'sso_and_password' : 'password')
    } catch (err) {
      // On probe failure, fall through to password login
      setEmail(emailInput)
      setSsoProviders([])
      setStep('password')
    } finally {
      setProbing(false)
    }
  }

  function handleBackToEmail() {
    setStep('email')
    setEmailInput(email)
    setSsoProviders([])
    setError('')
  }

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

  function handleSSOProviderLogin(providerId: string) {
    window.location.href = `/api/v1/auth/oidc/${providerId}`
  }

  // In register mode, skip the email step entirely
  const showEmailStep = mode === 'login' && step === 'email'
  const showPasswordStep = mode === 'register' || step === 'password' || step === 'sso_and_password'
  const showSSOProviders = mode === 'login' && step === 'sso_and_password' && ssoProviders.length > 0

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
              onClick={() => { setMode('login'); setStep('email'); setEmailInput(email); setError('') }}
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

          {/* Step 1: Email input (login mode only) */}
          {showEmailStep && (
            <form onSubmit={handleEmailContinue} style={styles.form}>
              <div style={styles.field}>
                <label style={styles.label}>Email</label>
                <input
                  style={styles.input}
                  type="email"
                  value={emailInput}
                  onChange={(e) => setEmailInput(e.target.value)}
                  required
                  placeholder="you@example.com"
                  autoFocus
                />
              </div>
              {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}
              <button type="submit" style={styles.submit} disabled={probing}>
                {probing ? 'Please wait…' : 'Continue'}
              </button>
            </form>
          )}

          {/* Step 2: SSO providers + password form */}
          {mode === 'login' && (step === 'password' || step === 'sso_and_password') && (
            <>
              {/* Email label with back link */}
              <div style={styles.emailLabel}>
                <span style={styles.emailDisplay}>{email}</span>
                <button
                  type="button"
                  style={styles.backLink}
                  onClick={handleBackToEmail}
                >
                  ← Use different email
                </button>
              </div>

              {/* SSO buttons */}
              {showSSOProviders && (
                <>
                  <div style={styles.ssoList}>
                    {ssoProviders.map(provider => (
                      <button
                        key={provider.id}
                        type="button"
                        style={styles.ssoProviderButton}
                        onClick={() => handleSSOProviderLogin(provider.id)}
                      >
                        Sign in with {provider.name}
                      </button>
                    ))}
                  </div>
                  <div style={styles.divider}>
                    <div style={styles.dividerLine} />
                    <span style={styles.dividerText}>or</span>
                    <div style={styles.dividerLine} />
                  </div>
                </>
              )}
            </>
          )}

          {/* Password form (login step 2 or register) */}
          {showPasswordStep && (
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
                </>
              )}
              <div style={styles.field}>
                <label style={styles.label}>Password</label>
                <input
                  style={styles.input}
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  placeholder="••••••••"
                  autoFocus={ssoProviders.length === 0}
                />
              </div>
              {error && <ErrorBanner message={error} onDismiss={() => setError('')} />}
              <button type="submit" style={styles.submit} disabled={loading}>
                {loading ? 'Please wait…' : mode === 'login' ? 'Sign In' : 'Create Account'}
              </button>
            </form>
          )}
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
    borderRadius: 4,
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
    background: 'var(--bg-modal)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: '28px 32px',
    boxSizing: 'border-box',
  },
  tabs: {
    display: 'flex',
    gap: 4,
    marginBottom: 28,
    padding: 4,
    background: 'var(--bg-secondary)',
    borderRadius: 4,
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
    borderRadius: 4,
    transition: 'all 0.15s',
  },
  tabActive: {
    background: 'var(--bg-elevated)',
    color: 'var(--text-primary)',
    boxShadow: 'none',
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
    borderRadius: 4,
    border: '1px solid var(--border)',
    fontSize: 14,
    outline: 'none',
    background: 'var(--bg-input)',
    color: 'var(--text-primary)',
    transition: 'border-color 0.15s',
  },
  submit: {
    padding: '11px',
    background: 'var(--text-primary)',
    color: 'var(--bg-primary)',
    border: 'none',
    borderRadius: 4,
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
    marginTop: 6,
    letterSpacing: '0.01em',
    transition: 'background 0.15s',
  },
  emailLabel: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: 16,
    padding: '8px 10px',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
  },
  emailDisplay: {
    fontSize: 13,
    color: 'var(--text-primary)',
    fontWeight: 500,
  },
  backLink: {
    background: 'none',
    border: 'none',
    fontSize: 12,
    color: 'var(--accent)',
    cursor: 'pointer',
    padding: 0,
    fontWeight: 500,
  },
  ssoList: {
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
    marginBottom: 4,
  },
  ssoProviderButton: {
    width: '100%',
    padding: '11px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
    letterSpacing: '0.01em',
    transition: 'opacity 0.15s',
  },
  divider: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    margin: '20px 0',
  },
  dividerLine: {
    flex: 1,
    height: 1,
    background: 'var(--border)',
  },
  dividerText: {
    fontSize: 12,
    color: 'var(--text-secondary)',
    flexShrink: 0,
  },
}
