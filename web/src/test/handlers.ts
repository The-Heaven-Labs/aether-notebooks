import { http, HttpResponse } from 'msw'
import type { Folder, FolderContents } from '../types'

// ── Shared fixtures ──────────────────────────────────────────────────────────

export const FOLDER_ROOT: FolderContents = {
  folders: [
    {
      id: 'f-home', org_id: 'org-1', name: "Alice's Home", is_home: true,
      owner_id: 'user-1', created_by: 'user-1',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'f-eng', org_id: 'org-1', name: 'Engineering', is_home: false,
      created_by: 'user-1',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    },
  ],
  notebooks: [
    {
      id: 'nb-1', org_id: 'org-1', title: 'Root Notebook', description: '',
      parameters: [], created_by: 'user-1',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    },
  ],
  connectors: [],
  dashboards: [],
}

export const FOLDER_ENG: FolderContents = {
  folder: {
    id: 'f-eng', org_id: 'org-1', name: 'Engineering', is_home: false,
    created_by: 'user-1',
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  },
  folders: [],
  notebooks: [
    {
      id: 'nb-2', org_id: 'org-1', title: 'Q1 Report', description: '',
      parameters: [], created_by: 'user-1', folder_id: 'f-eng',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    },
  ],
  connectors: [],
  dashboards: [],
}

export const GROUPS = [
  {
    id: 'g-1', org_id: 'org-1', name: 'Data Team', member_count: 1,
    created_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'g-2', org_id: 'org-1', name: 'CSIRT', member_count: 0,
    created_at: '2026-01-01T00:00:00Z',
  },
]

export const GROUP_MEMBERS = [
  { user_id: 'user-2', name: 'Bob Editor', email: 'bob@test.com' },
]

export const MEMBERS = [
  {
    user_id: 'user-1', name: 'Alice Admin', email: 'alice@test.com',
    role: 'admin', joined_at: '2026-01-01T00:00:00Z',
  },
  {
    user_id: 'user-2', name: 'Bob Editor', email: 'bob@test.com',
    role: 'editor', joined_at: '2026-01-01T00:00:00Z',
  },
]

export const ACL_ENTRIES = [
  {
    id: 'acl-1', org_id: 'org-1', resource_type: 'notebook', resource_id: 'nb-1',
    subject_type: 'user' as const, subject_id: 'user-1',
    actions: ['view', 'edit', 'delete', 'share', 'run'],
    created_at: '2026-01-01T00:00:00Z',
  },
]

// ── Handlers ────────────────────────────────────────────────────────────────

export const handlers = [
  // Notebooks
  http.get('/api/v1/notebooks', () => HttpResponse.json([])),
  http.get('/api/v1/notebooks/:id', ({ params }) =>
    HttpResponse.json({
      id: params.id, org_id: 'org-1', title: 'Test Notebook',
      description: '', parameters: [], cells: [],
      created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    })
  ),
  http.post('/api/v1/notebooks', async ({ request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json(
      {
        id: 'nb-new', org_id: 'org-1', title: body.title ?? 'Untitled',
        description: '', parameters: [], created_by: 'user-1',
        created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
      },
      { status: 201 }
    )
  }),
  http.put('/api/v1/notebooks/:id', async ({ params, request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json({
      id: params.id, org_id: 'org-1', title: body.title ?? 'Updated Notebook',
      description: '', parameters: [], created_by: 'user-1',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    })
  }),

  // Folders
  http.get('/api/v1/folders', () => HttpResponse.json(FOLDER_ROOT)),
  http.get('/api/v1/folders/:id', ({ params }) => {
    if (params.id === 'f-eng') return HttpResponse.json(FOLDER_ENG)
    return HttpResponse.json({ folder: null, folders: [], notebooks: [], connectors: [], dashboards: [] })
  }),
  http.get('/api/v1/folders/:id/ancestors', ({ params }) => {
    if (params.id === 'f-eng')
      return HttpResponse.json([{ id: 'f-eng', name: 'Engineering' }])
    return HttpResponse.json([])
  }),
  http.post('/api/v1/folders', async ({ request }) => {
    const body = await request.json() as Record<string, unknown>
    const folder: Folder = {
      id: 'f-new', org_id: 'org-1', name: body.name as string,
      is_home: false, created_by: 'user-1',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    }
    return HttpResponse.json(folder, { status: 201 })
  }),
  http.put('/api/v1/folders/:id', async ({ params, request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json({
      id: params.id, org_id: 'org-1',
      name: body.name ?? 'Engineering', is_home: false,
      created_by: 'user-1',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    })
  }),
  http.delete('/api/v1/folders/:id', () => new HttpResponse(null, { status: 204 })),

  // Connectors
  http.get('/api/v1/connectors', () => HttpResponse.json([])),
  http.put('/api/v1/connectors/:id', async ({ params, request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json({ id: params.id, ...body })
  }),

  // Dashboards
  http.get('/api/v1/dashboards', () => HttpResponse.json([])),
  http.post('/api/v1/dashboards', async ({ request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json(
      {
        id: 'dash-new', org_id: 'org-1', title: body.title ?? 'Untitled',
        created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      },
      { status: 201 }
    )
  }),

  // Members
  http.get('/api/v1/members', () => HttpResponse.json(MEMBERS)),

  // Groups
  http.get('/api/v1/groups', () => HttpResponse.json(GROUPS)),
  http.post('/api/v1/groups', async ({ request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json(
      {
        id: 'g-new', org_id: 'org-1', name: body.name, member_count: 0,
        created_at: '2026-01-01T00:00:00Z',
      },
      { status: 201 }
    )
  }),
  http.put('/api/v1/groups/:id', async ({ params, request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json({
      id: params.id, org_id: 'org-1', name: body.name,
      member_count: 0, created_at: '2026-01-01T00:00:00Z',
    })
  }),
  http.delete('/api/v1/groups/:id', () => new HttpResponse(null, { status: 204 })),
  http.get('/api/v1/groups/:id/members', ({ params }) => {
    if (params.id === 'g-1') return HttpResponse.json(GROUP_MEMBERS)
    return HttpResponse.json([])
  }),
  http.post('/api/v1/groups/:id/members', async ({ request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json({ user_id: body.user_id }, { status: 201 })
  }),
  http.delete(
    '/api/v1/groups/:id/members/:uid',
    () => new HttpResponse(null, { status: 204 })
  ),

  // ACL
  http.get('/api/v1/acl/:type/:id', () => HttpResponse.json(ACL_ENTRIES)),
  http.put('/api/v1/acl/:type/:id', async ({ request }) => {
    const entries = await request.json()
    return HttpResponse.json(entries)
  }),

  // Audit
  http.get('/api/v1/audit', () => HttpResponse.json([])),
]
