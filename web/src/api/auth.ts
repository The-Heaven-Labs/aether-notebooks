import { api, setToken, clearToken } from './client'

interface AuthResponse {
  token: string
  user: { name: string; email: string }
  org: { name: string }
}

export async function login(email: string, password: string): Promise<AuthResponse> {
  const res = await api.post<AuthResponse>('/api/v1/auth/login', { email, password })
  setToken(res.token)
  return res
}

export async function register(
  email: string,
  password: string,
  name: string,
  orgName: string,
): Promise<AuthResponse> {
  const res = await api.post<AuthResponse>('/api/v1/auth/register', {
    email,
    password,
    name,
    org_name: orgName,
  })
  setToken(res.token)
  return res
}

export function logout(): void {
  clearToken()
}
