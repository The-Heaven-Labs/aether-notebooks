import { useState, useEffect, useRef } from 'react'
import type { FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Eye, EyeOff } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { useAuth } from '../hooks/useAuth'
import { ApiError, api } from '../api/client'
import { ErrorBanner } from '../components/ErrorBanner'

type LoginStep = 'email' | 'password' | 'sso_and_password'

interface SSOProvider {
  id: string
  name: string
  provider_type: string
}

function getPasswordStrength(pw: string): { score: number; label: string; color: string } {
  let score = 0
  if (pw.length >= 8) score++
  if (/[A-Z]/.test(pw)) score++
  if (/[a-z]/.test(pw)) score++
  if (/[0-9]/.test(pw)) score++
  if (/[^A-Za-z0-9]/.test(pw)) score++
  if (score <= 1) return { score, label: 'Weak', color: 'var(--error)' }
  if (score <= 3) return { score, label: 'Fair', color: 'var(--warning)' }
  return { score, label: 'Strong', color: 'var(--success)' }
}

export function LoginPage() {
  const { login, register, loginWithToken } = useAuth()
  const navigate = useNavigate()
  const passwordRef = useRef<HTMLInputElement>(null)
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
  const [showPassword, setShowPassword] = useState(false)
  const [loginMotds, setLoginMotds] = useState<Array<{id: string; title: string; content: string}>>([])
  const [registrationDisabled, setRegistrationDisabled] = useState(false)

  useEffect(() => {
    api.get<Array<{id: string; title: string; content: string}>>('/api/v1/public/motd')
      .then(setLoginMotds)
      .catch(() => {})
    api.get<{ registration_disabled: boolean }>('/api/v1/auth/config')
      .then(c => setRegistrationDisabled(c.registration_disabled))
      .catch(() => {})
  }, [])

  // Focus password field when it becomes visible and no SSO providers
  const showPasswordStep = mode === 'register' || step === 'password' || step === 'sso_and_password'

  useEffect(() => {
    if (showPasswordStep && ssoProviders.length === 0) {
      passwordRef.current?.focus()
    }
  }, [showPasswordStep, ssoProviders.length])

  function redirectAfterLogin() {
    const redirectTo = sessionStorage.getItem('aether_redirect_after_login')
    sessionStorage.removeItem('aether_redirect_after_login')
    return redirectTo || '/'
  }

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
          if (user.name) localStorage.setItem('aether_user_name', user.name)
          if (user.email) localStorage.setItem('aether_user_email', user.email)
          if (user.is_platform_admin) localStorage.setItem('aether_is_platform_admin', 'true')
        })
        .catch(err => console.warn('[OIDC] Failed to fetch user info:', err))
        .finally(() => {
          window.history.replaceState({}, '', window.location.pathname)
          navigate(redirectAfterLogin())
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
        // Check if there's a pending join token from an invite link
        const pendingJoinToken = sessionStorage.getItem('aether_pending_join_token')
        if (pendingJoinToken) {
          navigate('/join')
        } else {
          navigate(redirectAfterLogin())
        }
      } else {
        const onboardingToken = await register(email, password, name)
        // Check if there's a pending join token from an invite link
        const pendingJoinToken = sessionStorage.getItem('aether_pending_join_token')
        if (pendingJoinToken) {
          navigate('/join')
        } else if (onboardingToken) {
          navigate('/onboarding')
        } else {
          navigate(redirectAfterLogin())
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
  const showSSOProviders = mode === 'login' && step === 'sso_and_password' && ssoProviders.length > 0

  return (
    <div style={styles.page}>
      <a
        href="#login-form"
        style={styles.skipLink}
        onFocus={(e) => { e.currentTarget.style.top = '0' }}
        onBlur={(e) => { e.currentTarget.style.top = '-40px' }}
      >
        Skip to form
      </a>
      {/* Brand panel */}
      <div style={styles.brand}>
        <div style={styles.brandInner}>
          <div style={styles.logoMark}>
            <span style={styles.logoIcon}>▦</span>
          </div>
          <h1 style={styles.brandTitle}>Aether<br />Notebooks</h1>
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
      <div id="login-form" style={styles.formPanel}>
        <div style={styles.formInner}>
          <div style={styles.tabs}>
            <button
              style={{ ...styles.tab, ...(mode === 'login' ? styles.tabActive : {}) }}
              onClick={() => { setMode('login'); setStep('email'); setEmailInput(email); setError('') }}
            >
              Sign In
            </button>
            {!registrationDisabled && (
              <button
                style={{ ...styles.tab, ...(mode === 'register' ? styles.tabActive : {}) }}
                onClick={() => { setMode('register'); setError('') }}
              >
                Create account
              </button>
            )}
          </div>

          {loginMotds.length > 0 && (
            <div style={{ marginBottom: 16 }}>
              {loginMotds.map(motd => (
                <div key={motd.id} style={{
                  background: 'var(--warning-light)',
                  border: '1px solid var(--warning-border)',
                  borderLeft: '3px solid var(--accent)',
                  borderRadius: 4,
                  padding: '12px 16px',
                  marginBottom: loginMotds.length > 1 ? 8 : 0,
                  fontSize: 13,
                  color: 'var(--text-primary)',
                  lineHeight: 1.5,
                }}>
                  {motd.title && <strong style={{ marginRight: 8, fontWeight: 600 }}>{motd.title}:</strong>}
                  <ReactMarkdown remarkPlugins={[remarkGfm]}>{motd.content}</ReactMarkdown>
                </div>
              ))}
            </div>
          )}

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
                {probing ? 'Please wait…' : 'Sign In'}
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
                <div style={{ position: 'relative' }}>
                  <input
                    ref={passwordRef}
                    style={{ ...styles.input, paddingRight: 36 }}
                    type={showPassword ? 'text' : 'password'}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                    placeholder="••••••••"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword(v => !v)}
                    style={styles.passwordToggle}
                    tabIndex={-1}
                    aria-label={showPassword ? 'Hide password' : 'Show password'}
                  >
                    {showPassword ? <EyeOff size={14} /> : <Eye size={14} />}
                  </button>
                </div>
                {mode === 'register' && password.length > 0 && (
                  <div style={styles.strengthRow}>
                    <div style={styles.strengthBarBg}>
                      <div style={{
                        ...styles.strengthBarFill,
                        width: `${(getPasswordStrength(password).score / 5) * 100}%`,
                        background: getPasswordStrength(password).color,
                      }} />
                    </div>
                    <span style={{ ...styles.strengthLabel, color: getPasswordStrength(password).color }}>
                      {getPasswordStrength(password).label}
                    </span>
                  </div>
                )}
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
    position: 'relative',
  },
  skipLink: {
    position: 'absolute',
    top: -40,
    left: 0,
    background: 'var(--accent)',
    color: '#fff',
    padding: '8px 16px',
    zIndex: 100,
    fontSize: 13,
    borderRadius: '0 0 4px 0',
    transition: 'top 0.15s',
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
  passwordToggle: {
    position: 'absolute',
    right: 8,
    top: '50%',
    transform: 'translateY(-50%)',
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    padding: 4,
    display: 'flex',
    alignItems: 'center',
  },
  strengthRow: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    marginTop: 2,
  },
  strengthBarBg: {
    flex: 1,
    height: 4,
    background: 'var(--border)',
    borderRadius: 2,
    overflow: 'hidden',
  },
  strengthBarFill: {
    height: '100%',
    borderRadius: 2,
    transition: 'width 0.2s, background 0.2s',
  },
  strengthLabel: {
    fontSize: 11,
    fontWeight: 600,
    flexShrink: 0,
  },
}
