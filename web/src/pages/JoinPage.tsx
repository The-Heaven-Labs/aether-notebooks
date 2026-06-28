import { useState, useEffect } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { api, setToken } from '../api/client'
import { useAuth } from '../hooks/useAuth'

export function JoinPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { loginWithToken, isAuthenticated } = useAuth()
  const token = params.get('token')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    // If no token in URL, check sessionStorage for a pending join token
    // (set before redirecting to login from a previous visit)
    const effectiveToken = token || sessionStorage.getItem('aether_pending_join_token')
    if (!effectiveToken) {
      setError('No invite token provided')
      setLoading(false)
      return
    }

    const joinOrg = async () => {
      try {
        // Check if user is already authenticated
        // Also check for onboarding_token which is set after registration
        const onboardingToken = localStorage.getItem('aether_onboarding_token')
        if (!isAuthenticated && !onboardingToken) {
          const userRes = await api.get('/api/v1/users/me').catch(() => null)
          if (!userRes) {
            // Store the token in sessionStorage so it survives the login redirect
            sessionStorage.setItem('aether_pending_join_token', effectiveToken)
            navigate('/login')
            return
          }
        }
        // If we have an onboarding token but no auth token, set it for the API call
        if (onboardingToken && !isAuthenticated) {
          setToken(onboardingToken)
        }
        const result = await api.post<{ token: string }>('/api/v1/auth/org/join', {
          invite_link_token: effectiveToken,
        })
        loginWithToken(result.token)
        // Clear the pending token after successful join
        sessionStorage.removeItem('aether_pending_join_token')
        localStorage.removeItem('aether_onboarding_token')
        navigate('/')
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to join organization')
        setLoading(false)
      }
    }

    joinOrg()
  }, [token, navigate, loginWithToken, isAuthenticated])

  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '100vh', background: 'var(--bg-primary)' }}>
      <div style={{ textAlign: 'center', padding: 40 }}>
        {loading && (
          <>
            <div style={{ fontSize: 18, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 8 }}>Joining organization...</div>
            <div style={{ fontSize: 14, color: 'var(--text-muted)' }}>Please wait while we set up your account.</div>
          </>
        )}
        {error && (
          <>
            <div style={{ fontSize: 18, fontWeight: 600, color: 'var(--error-full)', marginBottom: 8 }}>Unable to join</div>
            <div style={{ fontSize: 14, color: 'var(--text-muted)', marginBottom: 16 }}>{error}</div>
            <a href="/login" style={{ color: 'var(--accent)', fontSize: 14 }}>Back to login</a>
          </>
        )}
      </div>
    </div>
  )
}
