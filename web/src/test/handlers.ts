import { http, HttpResponse } from 'msw'
import { FAVORITE_RESOURCE_TYPES } from '../types'
import type { FavoriteResourceType, FavoriteResult, Folder, FolderContents } from '../types'

// ── Shared fixtures ──────────────────────────────────────────────────────────

export const FOLDER_ROOT: FolderContents = {
  folders: [
    {
      id: 'f-home', org_id: 'org-1', name: "Alice's Home", is_home: true,
      owner_id: 'user-1', created_by: 'user-1',
      can_edit: true, can_delete: true, can_share: true,
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    },
    {
      id: 'f-eng', org_id: 'org-1', name: 'Engineering', is_home: false,
      created_by: 'user-1',
      can_edit: true, can_delete: true, can_share: true,
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
  totals: { notebooks: 1, connectors: 0, dashboards: 0 },
  has_more: { notebooks: false, connectors: false, dashboards: false },
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
  totals: { notebooks: 1, connectors: 0, dashboards: 0 },
  has_more: { notebooks: false, connectors: false, dashboards: false },
}

export const GROUPS = [
  {
    id: 'g-1', org_id: 'org-1', name: 'Data Team', source: 'manual', member_count: 1,
    created_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'g-2', org_id: 'org-1', name: 'CSIRT', source: 'manual', member_count: 0,
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
  // Public app config: registration + warehouse table permissions flags.
  // Defaults to enforcement on so warehouse copy renders its managed wording.
  http.get('/api/v1/auth/config', () =>
    HttpResponse.json({
      registration_disabled: false,
      warehouse_table_permissions_enabled: true,
    })
  ),

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

  // Home (folder tree sidebar)
  http.get('/api/v1/home', () => HttpResponse.json([
    { id: 'f-home', name: "Alice's Home", is_home: true, owner_id: 'user-1', sub_folders: [] },
  ])),

  // Message of the Day banner (AppShell fetches it on every render)
  http.get('/api/v1/motd', () => HttpResponse.json([])),

  // Org settings
  http.get('/api/v1/org/data-export', () => HttpResponse.json({ data_export_enabled: true })),

  // Folders
  http.get('/api/v1/folders', () => HttpResponse.json(FOLDER_ROOT)),
  // Section pages (load more). The root literal must be registered before the
  // `:id` route: MSW matches in order, so `/folders/contents` would otherwise
  // be captured as folder id "contents". Defaults serve an empty last page.
  http.get('/api/v1/folders/contents', ({ request }) => {
    const url = new URL(request.url)
    return HttpResponse.json({
      items: [], total: 0, has_more: false,
      offset: Number(url.searchParams.get('offset') ?? 0) || 0,
    })
  }),
  http.get('/api/v1/folders/:id', ({ params }) => {
    if (params.id === 'f-eng') return HttpResponse.json(FOLDER_ENG)
    return HttpResponse.json({ folder: null, folders: [], notebooks: [], connectors: [], dashboards: [] })
  }),
  http.get('/api/v1/folders/:id/contents', ({ request }) => {
    const url = new URL(request.url)
    return HttpResponse.json({
      items: [], total: 0, has_more: false,
      offset: Number(url.searchParams.get('offset') ?? 0) || 0,
    })
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
  // ClickHouse Cloud state: default to "not configured" so list tests without
  // cloud credentials render the inference path.
  http.get('/api/v1/connectors/:id/cloud-state', () => HttpResponse.json({ configured: false })),

  // Warehouses
  http.get('/api/v1/warehouses', () => HttpResponse.json([])),

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

  // Favorites: an empty listing by default; toggles are idempotent no-ops.
  // Tests that toggle use `favoritesFixture()` so GET agrees with PUT/DELETE.
  http.get('/api/v1/favorites', () => HttpResponse.json({ groups: [] })),
  http.put('/api/v1/favorites/:type/:id', () => new HttpResponse(null, { status: 204 })),
  http.delete('/api/v1/favorites/:type/:id', () => new HttpResponse(null, { status: 204 })),

  // Recent
  http.get('/api/v1/recent', () => HttpResponse.json([
    { id: 'nb-1', type: 'notebook', name: 'Root Notebook', updated_at: '2026-01-01T00:00:00Z' },
  ])),

  // Members
  http.get('/api/v1/members', () => HttpResponse.json(MEMBERS)),

  // Groups
  http.get('/api/v1/groups', () => HttpResponse.json(GROUPS)),
  http.post('/api/v1/groups', async ({ request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json(
      {
        id: 'g-new', org_id: 'org-1', name: body.name, source: 'manual', member_count: 0,
        created_at: '2026-01-01T00:00:00Z',
      },
      { status: 201 }
    )
  }),
  http.put('/api/v1/groups/:id', async ({ params, request }) => {
    const body = await request.json() as Record<string, unknown>
    return HttpResponse.json({
      id: params.id, org_id: 'org-1', name: body.name, source: 'manual',
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
  http.get('/api/v1/groups/:id/pending-members', () => HttpResponse.json([])),
  http.post('/api/v1/groups/:id/pending-members', async ({ request }) => {
    const body = await request.json() as { emails?: string[] }
    const emails = body.emails ?? []
    return HttpResponse.json({ added: emails.length, skipped: [] })
  }),
  http.delete(
    '/api/v1/groups/:id/pending-members/:email',
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

  // Widget execution: the config drawer previews a query widget on open, so
  // every drawer render with a query widget hits this unless the test
  // overrides it with server.use.
  http.post('/api/v1/dashboards/:id/execute', () =>
    HttpResponse.json({
      outputs: [{
        type: 'table',
        data: {
          columns: [{ name: 'x', type: 'Int32' }],
          rows: [[1]],
          truncated: false,
          rows_included: 1,
          rows_total: 1,
          bytes: 4,
        },
      }],
      metrics: {},
      cached: true,
    }),
  ),
]

// ── Favorites test API ───────────────────────────────────────────────────────

/** Minimal favorites-listing row for one resource id. */
export function favoriteSummary(resourceType: FavoriteResourceType, id: string): FavoriteResult {
  return {
    id,
    resource_type: resourceType,
    title: id,
    subtitle: '',
    updated_at: '2026-01-01T00:00:00Z',
    is_favorite: true,
    can_edit: true,
  }
}

/**
 * Stateful favorites API for tests: GET reflects the ids PUT/DELETE stored, so
 * a toggle's post-success invalidation refetch agrees with the request that
 * preceded it (the stateless defaults above would flap an optimistic state
 * back). Build one per test and register it with
 * `server.use(...fixture.handlers)`.
 */
export function favoritesFixture(initial: Array<{ type: FavoriteResourceType; id: string }> = []) {
  const held = new Map<FavoriteResourceType, Set<string>>()
  const hold = (type: FavoriteResourceType, id: string) => {
    let set = held.get(type)
    if (!set) {
      set = new Set()
      held.set(type, set)
    }
    set.add(id)
  }
  for (const { type, id } of initial) hold(type, id)

  const counts = { gets: 0 }
  const handlers = [
    http.get('/api/v1/favorites', () => {
      counts.gets += 1
      const groups = FAVORITE_RESOURCE_TYPES
        .filter((type) => (held.get(type)?.size ?? 0) > 0)
        .map((type) => ({
          resource_type: type,
          results: [...(held.get(type) ?? [])].map((id) => favoriteSummary(type, id)),
        }))
      return HttpResponse.json({ groups })
    }),
    http.put('/api/v1/favorites/:type/:id', ({ params }) => {
      hold(params.type as FavoriteResourceType, params.id as string)
      return new HttpResponse(null, { status: 204 })
    }),
    http.delete('/api/v1/favorites/:type/:id', ({ params }) => {
      held.get(params.type as FavoriteResourceType)?.delete(params.id as string)
      return new HttpResponse(null, { status: 204 })
    }),
  ]

  return { handlers, counts, held, hold }
}
