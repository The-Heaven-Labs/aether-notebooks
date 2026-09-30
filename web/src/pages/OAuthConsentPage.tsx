import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ApiError, api, getToken } from '../api/client'

interface ConsentInfo {
  client_name: string
  org_name: string
  scopes: string[]
  scope_descriptions: Record<string, string>
}

// DCR client names are self-asserted, so show the host the code will be sent
// to. Fall back to the raw value when it is not a parseable URL.
function redirectHost(uri: string): string {
  try {
    return new URL(uri).host || uri
  } catch {
    return uri
  }
}

export function OAuthConsentPage() {
  const params = new URLSearchParams(window.location.search)
  const [info, setInfo] = useState<ConsentInfo | null>(null)
  const [fetchError, setFetchError] = useState('')
  const [decisionError, setDecisionError] = useState('')
  const [busy, setBusy] = useState(false)

  const clientId = params.get('client_id') || ''
  const redirectUri = params.get('redirect_uri') || ''
  const scope = params.get('scope') || ''
  const resource = params.get('resource') || ''
  const state = params.get('state') || ''
  const challenge = params.get('code_challenge') || ''
  const challengeMethod = params.get('code_challenge_method') || ''

  const redirectTarget = redirectHost(redirectUri)
  const invalidRequest = !clientId || !redirectUri || !challenge || challengeMethod !== 'S256'
  const signedOut = !invalidRequest && !getToken()
  const error = fetchError || (invalidRequest ? 'Invalid authorization request.' : signedOut ? 'Sign in to Aether first, then reconnect your MCP client.' : '')

  function rememberReturnUrl() {
    sessionStorage.setItem('aether_redirect_after_login', window.location.pathname + window.location.search)
  }

  useEffect(() => {
    if (invalidRequest || signedOut) return
    let cancelled = false
    api
      .get<ConsentInfo>(
        `/api/v1/oauth/consent/info?client_id=${encodeURIComponent(clientId)}&scope=${encodeURIComponent(scope)}&resource=${encodeURIComponent(resource)}`,
      )
      .then((data) => {
        if (!cancelled) setInfo(data)
      })
      .catch((e: unknown) => {
        if (!cancelled) setFetchError(e instanceof ApiError ? e.message : 'Failed to load authorization request')
      })
    return () => {
      cancelled = true
    }
  }, [invalidRequest, signedOut, clientId, scope, resource])

  const decide = async (approve: boolean) => {
    setBusy(true)
    setDecisionError('')
    try {
      const { redirect } = await api.post<{ redirect: string }>('/api/v1/oauth/consent/decision', {
        client_id: clientId,
        redirect_uri: redirectUri,
        scope,
        resource,
        state,
        code_challenge: challenge,
        code_challenge_method: challengeMethod,
        approve,
      })
      if (!redirect) throw new Error('Authorization failed')
      window.location.href = redirect
    } catch (e) {
      setDecisionError(e instanceof ApiError || e instanceof Error ? e.message : 'Authorization failed')
      setBusy(false)
    }
  }

  return (
    <div style={styles.page}>
      <div style={styles.card}>
        {error ? (
          <p style={styles.error} role="alert">
            {error}
            {signedOut && (
              <>
                {' '}
                <Link to="/login" onClick={rememberReturnUrl} style={styles.loginLink}>
                  Sign in
                </Link>
              </>
            )}
          </p>
        ) : !info ? (
          <p style={styles.loading}>Loading…</p>
        ) : (
          <>
            <h1 style={styles.title}>Authorize {info.client_name}</h1>
            <p style={styles.subtitle}>
              {info.client_name} wants to access your organization <strong>{info.org_name}</strong> and
              redirect back to <strong>{redirectTarget}</strong>.
            </p>
            <ul style={styles.scopes}>
              {info.scopes.map((s) => (
                <li key={s} style={styles.scopeItem}>
                  <code style={styles.scopeName}>{s}</code>
                  <span style={styles.scopeDescription}>{info.scope_descriptions[s] ?? s}</span>
                </li>
              ))}
            </ul>
            {decisionError && (
              <p style={styles.decisionError} role="alert">
                {decisionError}
              </p>
            )}
            <div style={styles.actions}>
              <button style={styles.allow} disabled={busy} onClick={() => decide(true)}>
                Allow
              </button>
              <button style={styles.deny} disabled={busy} onClick={() => decide(false)}>
                Deny
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: '100vh',
    background: 'var(--bg-primary)',
    padding: 24,
  },
  card: {
    width: '100%',
    maxWidth: 420,
    background: 'var(--bg-modal)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    padding: '28px 32px',
    boxSizing: 'border-box',
  },
  title: {
    fontSize: 20,
    fontWeight: 700,
    color: 'var(--text-primary)',
    marginBottom: 10,
    letterSpacing: '-0.2px',
  },
  subtitle: {
    fontSize: 14,
    color: 'var(--text-secondary)',
    lineHeight: 1.5,
    marginBottom: 20,
  },
  scopes: {
    listStyle: 'none',
    margin: '0 0 24px',
    padding: 0,
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
  },
  scopeItem: {
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    padding: '10px 12px',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
  },
  scopeName: {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--accent)',
  },
  scopeDescription: {
    fontSize: 13,
    color: 'var(--text-secondary)',
  },
  actions: {
    display: 'flex',
    gap: 10,
  },
  allow: {
    flex: 1,
    padding: '10px',
    background: 'var(--accent)',
    color: '#fff',
    border: 'none',
    borderRadius: 4,
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
  },
  deny: {
    flex: 1,
    padding: '10px',
    background: 'transparent',
    color: 'var(--text-primary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    fontSize: 14,
    fontWeight: 600,
    cursor: 'pointer',
  },
  loading: {
    fontSize: 14,
    color: 'var(--text-muted)',
  },
  error: {
    fontSize: 14,
    color: 'var(--error-text)',
    lineHeight: 1.5,
  },
  loginLink: {
    color: 'var(--accent)',
    fontWeight: 600,
  },
  decisionError: {
    fontSize: 13,
    color: 'var(--error-text)',
    lineHeight: 1.5,
    marginBottom: 12,
  },
}
