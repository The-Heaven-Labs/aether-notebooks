import { api, setToken, clearToken } from './client'

export async function login(email: string, password: string): Promise<string> {
  const res = await api.post<{ token: string }>('/api/v1/auth/login', { email, password })
  setToken(res.token)
  return res.token
}

export async function register(
  email: string,
  password: string,
  name: string,
  orgName: string,
): Promise<string> {
  const res = await api.post<{ token: string }>('/api/v1/auth/register', {
    email,
    password,
    name,
    org_name: orgName,
  })
  setToken(res.token)
  return res.token
}

export function logout(): void {
  clearToken()
}
