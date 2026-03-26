import { useState, useCallback } from 'react'
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

export function useAuth() {
  const [user, setUser] = useState(getStoredUser)

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

  const register = useCallback(async (email: string, password: string, name: string, orgName: string) => {
    const resp = await apiRegister(email, password, name, orgName)
    localStorage.setItem('hnb_user_name', resp.user.name)
    localStorage.setItem('hnb_user_email', resp.user.email)
    localStorage.setItem('hnb_org_name', resp.org.name)
    const claims = parseJwt(resp.token)
    if (claims) {
      setUser({ user_id: claims.sub as string, org_id: claims.org_id as string, role: claims.role as string })
    }
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
