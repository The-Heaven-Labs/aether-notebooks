import { http, HttpResponse } from 'msw'

export const handlers = [
  http.get('/api/v1/notebooks', () =>
    HttpResponse.json([
      { id: 'nb-1', org_id: 'org-1', title: 'Test Notebook', description: '', parameters: [], created_by: 'u1', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z' },
    ])
  ),
  http.get('/api/v1/connectors', () => HttpResponse.json([])),
  http.get('/api/v1/members', () => HttpResponse.json([])),
  http.get('/api/v1/audit', () => HttpResponse.json([])),
  http.get('/api/v1/dashboards', () => HttpResponse.json([])),
]
