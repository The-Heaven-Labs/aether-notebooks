import { getApiUrl } from '../config'
const BASE_URL = getApiUrl()

export function getToken(): string | null {
  return localStorage.getItem('aether_token')
}

export function setToken(token: string): void {
  localStorage.setItem('aether_token', token)
}

export function clearToken(): void {
  localStorage.removeItem('aether_token')
}

export class ApiError extends Error {
  status: number
  /** Parsed JSON error body, when the server sent one (e.g. 403 service_access_denied). */
  body: unknown
  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.status = status
    this.body = body
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  options?: { binary?: boolean; signal?: AbortSignal },
): Promise<T> {
  const headers: Record<string, string> = {}

  if (body && !options?.binary) {
    headers['Content-Type'] = 'application/json'
  }

  const token = getToken()
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  if (localStorage.getItem('aether_admin_mode') === 'true') {
    headers['X-AETHER-Admin-Mode'] = 'true'
  }

  const res = await fetch(BASE_URL + path, {
    method,
    headers,
    body: body ? (options?.binary ? (body as BodyInit) : JSON.stringify(body)) : undefined,
    signal: options?.signal,
  })

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new ApiError(res.status, err.error || res.statusText, err)
  }

  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  get: <T>(path: string, options?: { signal?: AbortSignal }) => request<T>('GET', path, undefined, options),
  post: <T>(path: string, body?: unknown, options?: { signal?: AbortSignal }) => request<T>('POST', path, body, options),
  put: <T>(path: string, body?: unknown, options?: { signal?: AbortSignal }) => request<T>('PUT', path, body, options),
  patch: <T>(path: string, body?: unknown, options?: { signal?: AbortSignal }) => request<T>('PATCH', path, body, options),
  delete: <T>(path: string, options?: { signal?: AbortSignal }) => request<T>('DELETE', path, undefined, options),
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
