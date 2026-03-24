import { useState, FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../hooks/useAuth'
import { ApiError } from '../api/client'

export function LoginPage() {
  const { login, register } = useAuth()
  const navigate = useNavigate()
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
  const [orgName, setOrgName] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      if (mode === 'login') {
        await login(email, password)
      } else {
        await register(email, password, name, orgName)
      }
      navigate('/')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Something went wrong')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={styles.container}>
      <div style={styles.card}>
        <h1 style={styles.title}>Heaven's Notebooks</h1>
        <p style={styles.subtitle}>Collaborative SQL notebooks for data teams</p>

        <div style={styles.tabs}>
          <button
            style={{ ...styles.tab, ...(mode === 'login' ? styles.tabActive : {}) }}
            onClick={() => setMode('login')}
          >
            Sign In
          </button>
          <button
            style={{ ...styles.tab, ...(mode === 'register' ? styles.tabActive : {}) }}
            onClick={() => setMode('register')}
          >
            Register
          </button>
        </div>

        <form onSubmit={handleSubmit} style={styles.form}>
          {mode === 'register' && (
            <>
              <label style={styles.label}>
                Your Name
                <input
                  style={styles.input}
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                  placeholder="Jane Doe"
                />
              </label>
              <label style={styles.label}>
                Organization Name
                <input
                  style={styles.input}
                  type="text"
                  value={orgName}
                  onChange={(e) => setOrgName(e.target.value)}
                  required
                  placeholder="Acme Analytics"
                />
              </label>
            </>
          )}
          <label style={styles.label}>
            Email
            <input
              style={styles.input}
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              placeholder="you@example.com"
            />
          </label>
          <label style={styles.label}>
            Password
            <input
              style={styles.input}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              placeholder="••••••••"
            />
          </label>
          {error && <p style={styles.error}>{error}</p>}
          <button type="submit" style={styles.submit} disabled={loading}>
            {loading ? 'Please wait…' : mode === 'login' ? 'Sign In' : 'Create Account'}
          </button>
        </form>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    minHeight: '100vh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'var(--bg-primary)',
  },
  card: {
    width: 400,
    padding: '40px',
    background: 'white',
    borderRadius: 12,
    border: '1px solid var(--border)',
    boxShadow: '0 4px 24px rgba(0,0,0,0.06)',
  },
  title: {
    fontSize: 22,
    fontWeight: 700,
    color: 'var(--accent)',
    marginBottom: 4,
  },
  subtitle: {
    fontSize: 14,
    color: 'var(--text-secondary)',
    marginBottom: 28,
  },
  tabs: {
    display: 'flex',
    marginBottom: 24,
    borderBottom: '1px solid var(--border)',
  },
  tab: {
    flex: 1,
    padding: '8px 0',
    border: 'none',
    background: 'none',
    fontSize: 14,
    color: 'var(--text-secondary)',
    cursor: 'pointer',
  },
  tabActive: {
    color: 'var(--accent)',
    borderBottom: '2px solid var(--accent)',
    marginBottom: -1,
    fontWeight: 600,
  },
  form: {
    display: 'flex',
    flexDirection: 'column',
    gap: 16,
  },
  label: {
    display: 'flex',
    flexDirection: 'column',
    gap: 6,
    fontSize: 14,
    fontWeight: 500,
    color: 'var(--text-primary)',
  },
  input: {
    padding: '8px 12px',
    borderRadius: 6,
    border: '1px solid var(--border)',
    fontSize: 14,
    outline: 'none',
    background: 'var(--bg-primary)',
  },
  error: {
    color: 'var(--error)',
    fontSize: 13,
  },
  submit: {
    padding: '10px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
    marginTop: 4,
  },
}
