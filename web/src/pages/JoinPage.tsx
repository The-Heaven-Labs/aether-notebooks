import { useState, useEffect } from 'react'
import { useSearchParams, useNavigate } from 'react-router-dom'
import { api } from '../api/client'
import { useAuth } from '../hooks/useAuth'

export function JoinPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const { loginWithToken, isAuthenticated } = useAuth()
  const token = params.get('token')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!token) {
      setError('No invite token provided')
      setLoading(false)
      return
    }

    const joinOrg = async () => {
      try {
        // Check if user is already authenticated
        if (!isAuthenticated) {
          const userRes = await api.get('/api/v1/users/me').catch(() => null)
          if (!userRes) {
            navigate(`/login?redirect=/join?token=${token}`)
            return
          }
        }
        const result = await api.post<{ token: string }>('/api/v1/auth/org/join', {
          invite_link_token: token,
        })
        loginWithToken(result.token)
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
