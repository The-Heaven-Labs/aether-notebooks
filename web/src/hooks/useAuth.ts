import { useState, useCallback, useContext, createContext } from 'react'
import { login as apiLogin, logout as apiLogout, register as apiRegister } from '../api/auth'

function parseJwt(token: string): Record<string, unknown> | null {
  try {
    const payload = token.split('.')[1]
    return JSON.parse(atob(payload))
  } catch {
    return null
  }
}

function getStoredUser() {
  const token = localStorage.getItem('hnb_token')
  if (!token) return null
  const claims = parseJwt(token)
  if (!claims) return null
  // Check expiry
  if (claims.exp && (claims.exp as number) * 1000 < Date.now()) {
    localStorage.removeItem('hnb_token')
    return null
  }
  return {
    user_id: claims.sub as string,
    org_id: claims.org_id as string,
    role: claims.role as string,
  }
}

type User = { user_id: string; org_id: string; role: string } | null

interface AuthContextValue {
  user: User
  login: (email: string, password: string) => Promise<void>
  /** Returns onboarding_token if account was created without an org, null otherwise */
  register: (email: string, password: string, name: string) => Promise<string | null>
  logout: () => void
  isAuthenticated: boolean
}

export const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuthProvider(): AuthContextValue {
  const [user, setUser] = useState<User>(getStoredUser)

  const login = useCallback(async (email: string, password: string) => {
    const resp = await apiLogin(email, password)
    localStorage.setItem('hnb_user_name', resp.user.name)
    localStorage.setItem('hnb_user_email', resp.user.email)
    localStorage.setItem('hnb_org_name', resp.org.name)
    const claims = parseJwt(resp.token)
    if (claims) {
      setUser({ user_id: claims.sub as string, org_id: claims.org_id as string, role: claims.role as string })
    }
  }, [])

  const register = useCallback(async (email: string, password: string, name: string): Promise<string | null> => {
    const resp = await apiRegister(email, password, name)
    if (resp.onboarding_token) {
      localStorage.setItem('hnb_onboarding_token', resp.onboarding_token)
      return resp.onboarding_token
    }
    if (resp.token && resp.user && resp.org) {
      const { token, user, org } = resp as Required<typeof resp>
      localStorage.setItem('hnb_user_name', user.name)
      localStorage.setItem('hnb_user_email', user.email)
      localStorage.setItem('hnb_org_name', org.name)
      const claims = parseJwt(token)
      if (claims) {
        setUser({ user_id: claims.sub as string, org_id: claims.org_id as string, role: claims.role as string })
      }
    }
    return null
  }, [])

  const logout = useCallback(() => {
    apiLogout()
    localStorage.removeItem('hnb_user_name')
    localStorage.removeItem('hnb_user_email')
    localStorage.removeItem('hnb_org_name')
    setUser(null)
  }, [])

  return { user, login, register, logout, isAuthenticated: !!user }
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
