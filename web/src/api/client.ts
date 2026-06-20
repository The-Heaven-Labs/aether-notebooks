const BASE_URL = import.meta.env.VITE_API_URL || ''

export function getToken(): string | null {
  return localStorage.getItem('hnb_token')
}

export function setToken(token: string): void {
  localStorage.setItem('hnb_token', token)
}

export function clearToken(): void {
  localStorage.removeItem('hnb_token')
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  options?: { binary?: boolean },
): Promise<T> {
  const headers: Record<string, string> = {}

  if (body && !options?.binary) {
    headers['Content-Type'] = 'application/json'
  }

  const token = getToken()
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  if (localStorage.getItem('hnb_admin_mode') === 'false') {
    headers['X-HNB-Admin-Mode'] = 'false'
  }

  const res = await fetch(BASE_URL + path, {
    method,
    headers,
    body: body ? (options?.binary ? (body as BodyInit) : JSON.stringify(body)) : undefined,
  })

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new ApiError(res.status, err.error || res.statusText)
  }

  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  delete: <T>(path: string) => request<T>('DELETE', path),
}

export const toolsApi = {
  list: (): Promise<Tool[]> => api.get('/api/v1/tools'),
  get: (id: string): Promise<Tool> => api.get(`/api/v1/tools/${id}`),
  create: (data: Partial<Tool>): Promise<{ id: string }> => api.post('/api/v1/tools', data),
  update: (id: string, data: Partial<Tool>): Promise<void> => api.put(`/api/v1/tools/${id}`, data),
  delete: (id: string): Promise<void> => api.delete(`/api/v1/tools/${id}`),
  test: (id: string): Promise<{ status: number; body?: string; result?: any }> => api.post(`/api/v1/tools/${id}/test`),
}

import type { Tool } from '../types/agent'
