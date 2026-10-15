import { screen, fireEvent, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse, delay } from 'msw'
import { Routes, Route, useLocation } from 'react-router-dom'
import { HomePage } from '../pages/HomePage'
import type { FavoriteResourceType, FolderContents, ResourceSummary } from '../types'
import { server } from './server'
import { renderWithProviders } from './utils'
import { favoritesFixture } from './handlers'

// Mock PermissionsPanel to avoid real API calls; the lookup props are exposed
// as data attributes so tests can assert what the panel would receive.
vi.mock('../components/PermissionsPanel', () => ({
  PermissionsPanel: ({ onClose, resourceOwnerId, canEdit }: {
    onClose: () => void
    resourceOwnerId?: string
    canEdit?: boolean
  }) => (
    <div
      data-testid="permissions-panel"
      data-owner={resourceOwnerId ?? ''}
      data-can-edit={canEdit === undefined ? '' : String(canEdit)}
    >
      <button onClick={onClose}>Close</button>
    </div>
  ),
}))

// ── Root view (T2.1) ──────────────────────────────────────────────────────────

describe('Root view (T2.1)', () => {
  beforeEach(() => {
    server.use(http.get('/api/v1/home', () => HttpResponse.json([])))
  })

  test('shows "Files" breadcrumb at root', async () => {
    renderWithProviders(<HomePage />)
    const matches = await screen.findAllByText('Files')
    expect(matches.length).toBeGreaterThan(0)
  })

  test('shows folders from root contents', async () => {
    renderWithProviders(<HomePage />)
    const engMatches = await screen.findAllByText('Engineering')
    expect(engMatches.length).toBeGreaterThan(0)
  })

  test('shows notebooks from root contents', async () => {
    renderWithProviders(<HomePage />)
    const matches = await screen.findAllByText('Root Notebook')
    expect(matches.length).toBeGreaterThan(0)
  })

  test('shows New Folder button but not New Notebook/New Dashboard at root', async () => {
    renderWithProviders(<HomePage />)
    expect(await screen.findByText('+ New Folder')).toBeInTheDocument()
    expect(screen.queryByText('+ New Notebook')).not.toBeInTheDocument()
    expect(screen.queryByText('+ New Dashboard')).not.toBeInTheDocument()
  })
})

// ── Create folder (T2.2) ──────────────────────────────────────────────────────

describe('Create folder (T2.2)', () => {
  test('inline form appears after clicking New Folder button', async () => {
    renderWithProviders(<HomePage />)
    const btn = await screen.findByText('+ New Folder')
    fireEvent.click(btn)
    expect(screen.getByPlaceholderText('Folder name…')).toBeInTheDocument()
  })

  test('Cancel button hides the create form', async () => {
    renderWithProviders(<HomePage />)
    fireEvent.click(await screen.findByText('+ New Folder'))
    const input = screen.getByPlaceholderText('Folder name…')
    fireEvent.click(screen.getByText('Cancel'))
    expect(input).not.toBeInTheDocument()
  })

  test('Escape key hides the create form', async () => {
    renderWithProviders(<HomePage />)
    fireEvent.click(await screen.findByText('+ New Folder'))
    const input = screen.getByPlaceholderText('Folder name…')
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(input).not.toBeInTheDocument()
  })

  test('submitting calls POST /api/v1/folders with the typed name', async () => {
    let capturedBody: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/folders', async ({ request }) => {
        capturedBody = await request.json() as Record<string, unknown>
        return HttpResponse.json(
          { id: 'f-new', org_id: 'org-1', name: capturedBody.name, is_home: false, created_by: 'user-1', created_at: '', updated_at: '' },
          { status: 201 }
        )
      })
    )
    renderWithProviders(<HomePage />)
    fireEvent.click(await screen.findByText('+ New Folder'))
    const input = screen.getByPlaceholderText('Folder name…')
    await userEvent.type(input, 'My New Folder')
    fireEvent.click(screen.getByText('Create'))
    await waitFor(() => expect(capturedBody).not.toBeNull())
    const captured = capturedBody!
    expect(captured.name).toBe('My New Folder')
  })
})

// ── Folder navigation (T2.3) ──────────────────────────────────────────────────

describe('Folder navigation (T2.3)', () => {
  test('clicking a folder navigates into it and shows its contents', async () => {
    renderWithProviders(<HomePage />)
    const engMatches = await screen.findAllByText('Engineering')
    // Click the last match (main content area, not sidebar)
    fireEvent.click(engMatches[engMatches.length - 1])
    expect(await screen.findByText('Q1 Report')).toBeInTheDocument()
  })

  test('sub-folder view shows ancestor in breadcrumb', async () => {
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-eng' })
    const engMatches = await screen.findAllByText('Engineering')
    expect(engMatches.length).toBeGreaterThan(0)
    // The breadcrumb shows "Files" — may appear in sidebar + breadcrumb
    const filesMatches = screen.getAllByText('Files')
    expect(filesMatches.length).toBeGreaterThan(0)
  })

  test('clicking Files breadcrumb returns to root', async () => {
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-eng' })
    await screen.findByText('Q1 Report')
    // Click the Files breadcrumb link (first match)
    const filesMatches = screen.getAllByText('Files')
    fireEvent.click(filesMatches[0])
    const matches = await screen.findAllByText('Root Notebook')
    expect(matches.length).toBeGreaterThan(0)
  })
})

// ── Empty state ───────────────────────────────────────────────────────────────

describe('Empty state', () => {
  test('shows empty state when folder has no contents', async () => {
    server.use(
      http.get('/api/v1/folders/:id', ({ params }) => {
        if (params.id === 'f-empty') {
          return HttpResponse.json({
            folder: { id: 'f-empty', org_id: 'org-1', name: 'Empty Folder', is_home: false, created_by: 'user-1', created_at: '', updated_at: '' },
            folders: [],
            notebooks: [],
            connectors: [],
            dashboards: [],
          })
        }
        return HttpResponse.json({ folder: null, folders: [], notebooks: [], connectors: [], dashboards: [] })
      }),
      http.get('/api/v1/folders/:id/ancestors', ({ params }) => {
        if (params.id === 'f-empty')
          return HttpResponse.json([{ id: 'f-empty', name: 'Empty Folder' }])
        return HttpResponse.json([])
      })
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-empty' })
    expect(await screen.findByText('This folder is empty')).toBeInTheDocument()
  })
})

// ── Context menu (T2.6) ───────────────────────────────────────────────────────

describe('Context menu (T2.6)', () => {
  test('⋯ button opens context menu with Rename, Move to…, Permissions, Delete items', async () => {
    renderWithProviders(<HomePage />)
    await screen.findAllByText("Alice's Home")
    // ⋯ buttons appear for items in the main content area
    const menuBtns = screen.getAllByTitle('More options')
    expect(menuBtns.length).toBeGreaterThan(0)
    fireEvent.click(menuBtns[0])
    expect(screen.getByText('Rename')).toBeInTheDocument()
    expect(screen.getByText('Move to…')).toBeInTheDocument()
    expect(screen.getByText('Permissions')).toBeInTheDocument()
    expect(screen.getByText('Delete')).toBeInTheDocument()
  })

  test('clicking outside closes context menu', async () => {
    renderWithProviders(<HomePage />)
    await screen.findAllByText("Alice's Home")
    const menuBtns = screen.getAllByTitle('More options')
    fireEvent.click(menuBtns[0])
    expect(screen.getByText('Rename')).toBeInTheDocument()
    // Simulate clicking outside by firing mousedown on document
    fireEvent.mouseDown(document.body)
    await waitFor(() => expect(screen.queryByText('Rename')).not.toBeInTheDocument())
  })
})

// ── Rename (T2.7) ─────────────────────────────────────────────────────────────

describe('Rename (T2.7)', () => {
  beforeEach(() => {
    server.use(http.get('/api/v1/home', () => HttpResponse.json([])))
  })

  async function openRenameForFolder(folderName: string) {
    await screen.findAllByText(folderName)
    const allBtns = screen.getAllByTitle('More options')
    const menuBtn = Array.from(allBtns).find(b => b.parentElement?.textContent?.includes(folderName)) ?? allBtns[0]
    fireEvent.click(menuBtn)
    fireEvent.click(screen.getByText('Rename'))
    const input = screen.getByDisplayValue(folderName)
    expect(input).toBeInTheDocument()
    return input
  }

  test('clicking Rename shows inline input pre-filled with current name', async () => {
    renderWithProviders(<HomePage />)
    await openRenameForFolder('Engineering')
  })

  test('Escape cancels rename', async () => {
    renderWithProviders(<HomePage />)
    await screen.findAllByText('Engineering')
    const input = await openRenameForFolder('Engineering')
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(screen.queryByDisplayValue('Engineering')).not.toBeInTheDocument()
    const engMatches = screen.getAllByText('Engineering')
    expect(engMatches.length).toBeGreaterThan(0)
  })

  test('Enter triggers PUT /api/v1/folders/:id with new name', async () => {
    let capturedBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/folders/:id', async ({ request }) => {
        capturedBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 'f-eng', org_id: 'org-1', name: capturedBody.name, is_home: false, created_by: 'user-1', created_at: '', updated_at: '' })
      })
    )
    renderWithProviders(<HomePage />)
    const input = await openRenameForFolder('Engineering')
    await userEvent.clear(input)
    await userEvent.type(input, 'New Name')
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(capturedBody).not.toBeNull())
    const captured = capturedBody!
    expect(captured.name).toBe('New Name')
  })
})

// ── New Dashboard (T2.9) ──────────────────────────────────────────────────────

describe('New Dashboard (T2.9)', () => {
  test('inline form appears after clicking New Dashboard button', async () => {
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-eng' })
    fireEvent.click(await screen.findByText('+ New Dashboard'))
    expect(screen.getByPlaceholderText('Dashboard title…')).toBeInTheDocument()
  })

  test('submitting calls POST /api/v1/dashboards', async () => {
    let capturedBody: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/dashboards', async ({ request }) => {
        capturedBody = await request.json() as Record<string, unknown>
        return HttpResponse.json(
          { id: 'dash-new', org_id: 'org-1', title: capturedBody.title, created_by: 'user-1', created_at: '', updated_at: '' },
          { status: 201 }
        )
      })
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-eng' })
    fireEvent.click(await screen.findByText('+ New Dashboard'))
    const input = screen.getByPlaceholderText('Dashboard title…')
    await userEvent.type(input, 'Sales Dashboard')
    fireEvent.click(screen.getByText('Create'))
    await waitFor(() => expect(capturedBody).not.toBeNull())
    const captured = capturedBody!
    expect(captured.title).toBe('Sales Dashboard')
  })
})

// ── Permissions panel (T7.1) ──────────────────────────────────────────────────

describe('Permissions panel (T7.1)', () => {
  test('clicking Permissions in context menu opens PermissionsPanel', async () => {
    renderWithProviders(<HomePage />)
    await screen.findAllByText("Alice's Home")
    const menuBtns = screen.getAllByTitle('More options')
    fireEvent.click(menuBtns[0])
    fireEvent.click(screen.getByText('Permissions'))
    expect(await screen.findByTestId('permissions-panel')).toBeInTheDocument()
  })

  test('closing PermissionsPanel removes it from DOM', async () => {
    renderWithProviders(<HomePage />)
    await screen.findAllByText("Alice's Home")
    const menuBtns = screen.getAllByTitle('More options')
    fireEvent.click(menuBtns[0])
    fireEvent.click(screen.getByText('Permissions'))
    const panel = await screen.findByTestId('permissions-panel')
    expect(panel).toBeInTheDocument()
    fireEvent.click(screen.getByText('Close'))
    await waitFor(() => expect(screen.queryByTestId('permissions-panel')).not.toBeInTheDocument())
  })
})

// ── Folder section pagination (B3) ────────────────────────────────────────────

type FolderNotebook = FolderContents['notebooks'][number]

const PAGE_FOLDER = 'f-page'

function nb(id: string, title: string): FolderNotebook {
  return {
    id, org_id: 'org-1', title, description: '',
    created_by: 'user-1', folder_id: PAGE_FOLDER,
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  }
}

function pageNotebooks(count: number, startIndex: number): FolderNotebook[] {
  return Array.from({ length: count }, (_, i) =>
    nb(`nb-page-${startIndex + i}`, `Paged Notebook ${startIndex + i + 1}`)
  )
}

function folderPage(notebooks: FolderNotebook[], total: number, hasMore: boolean): FolderContents {
  return {
    folder: {
      id: PAGE_FOLDER, org_id: 'org-1', name: 'Paginated', is_home: false,
      created_by: 'user-1',
      created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
    },
    folders: [],
    notebooks,
    connectors: [],
    dashboards: [],
    totals: { notebooks: total, connectors: 0, dashboards: 0 },
    has_more: { notebooks: hasMore, connectors: false, dashboards: false },
  }
}

describe('Folder section pagination (B3)', () => {
  test('counter shows loaded of total and Load more pages from the loaded offset', { timeout: 15000 }, async () => {
    const requests: string[] = []
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage(pageNotebooks(50, 0), 120, true))),
      http.get('/api/v1/folders/f-page/contents', ({ request }) => {
        const url = new URL(request.url)
        requests.push(url.search)
        const offset = Number(url.searchParams.get('offset'))
        const items = pageNotebooks(Math.min(50, 120 - offset), offset)
        return HttpResponse.json({
          items,
          total: 120,
          has_more: offset + items.length < 120,
          offset,
        })
      }),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    expect(await screen.findByText('50 of 120')).toBeInTheDocument()
    expect(screen.getByText('Paged Notebook 1')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Load more notebooks' }))
    expect(await screen.findByText('100 of 120')).toBeInTheDocument()
    expect(screen.getByText('Paged Notebook 51')).toBeInTheDocument()

    expect(requests).toHaveLength(1)
    expect(requests[0]).toContain('type=notebook')
    expect(requests[0]).toContain('offset=50')
    expect(requests[0]).toContain('limit=50')

    // The second click continues from the new offset; the last page turns the
    // counter into the plain count and removes the button.
    fireEvent.click(screen.getByRole('button', { name: 'Load more notebooks' }))
    expect(await screen.findByText('120')).toBeInTheDocument()
    expect(screen.queryByText('100 of 120')).not.toBeInTheDocument()
    expect(requests).toHaveLength(2)
    expect(requests[1]).toContain('offset=100')
    expect(screen.queryByRole('button', { name: /^Load more/ })).not.toBeInTheDocument()
  })

  test('a page that repeats a loaded id does not duplicate the row', async () => {
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([
        nb('nb-1', 'Alpha'), nb('nb-2', 'Bravo'), nb('nb-3', 'Charlie'),
      ], 5, true))),
      http.get('/api/v1/folders/f-page/contents', () => HttpResponse.json({
        // `Charlie` repeats the first page; `Delta` repeats within the page.
        items: [nb('nb-3', 'Charlie'), nb('nb-4', 'Delta'), nb('nb-4', 'Delta'), nb('nb-5', 'Echo')],
        total: 5, has_more: false, offset: 3,
      })),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    expect(await screen.findByText('3 of 5')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Load more notebooks' }))

    expect(await screen.findByText('Delta')).toBeInTheDocument()
    expect(screen.getByText('Echo')).toBeInTheDocument()
    expect(screen.getAllByText('Charlie')).toHaveLength(1)
    expect(screen.getAllByText('Delta')).toHaveLength(1)
    // Everything is loaded: plain count, no "N of M", no button.
    await waitFor(() => expect(screen.queryByText('3 of 5')).not.toBeInTheDocument())
    expect(screen.getByText('5')).toBeInTheDocument()
  })

  test('a page without a total hides the counter instead of inventing a plain count', async () => {
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([
        nb('nb-1', 'Alpha'), nb('nb-2', 'Bravo'),
      ], 4, true))),
      // Legacy envelope: neither total nor has_more is reported.
      http.get('/api/v1/folders/f-page/contents', () => HttpResponse.json({
        items: [nb('nb-3', 'Charlie')],
        offset: 2,
      })),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    expect(await screen.findByText('2 of 4')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Load more notebooks' }))

    expect(await screen.findByText('Charlie')).toBeInTheDocument()
    // Unknown total must degrade to no counter at all — not a bare "3".
    await waitFor(() => expect(screen.queryByText('2 of 4')).not.toBeInTheDocument())
    expect(screen.queryByText('3')).not.toBeInTheDocument()
    // Unknown has_more also hides the button.
    expect(screen.queryByRole('button', { name: /^Load more/ })).not.toBeInTheDocument()
  })

  test('zero new items with has_more still true stops paging and keeps rows', async () => {
    let pageCalls = 0
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([
        nb('nb-1', 'Alpha'), nb('nb-2', 'Bravo'),
      ], 10, true))),
      http.get('/api/v1/folders/f-page/contents', () => {
        pageCalls++
        return HttpResponse.json({ items: [], total: 10, has_more: true, offset: 2 })
      }),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    expect(await screen.findByText('2 of 10')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Load more notebooks' }))

    await waitFor(() => expect(screen.queryByRole('button', { name: /^Load more/ })).not.toBeInTheDocument())
    expect(pageCalls).toBe(1)
    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Bravo')).toBeInTheDocument()
    expect(screen.getByText('2 of 10')).toBeInTheDocument()
  })

  test('Load more is disabled and single-flight while a page is in flight', async () => {
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([
        nb('nb-1', 'Alpha'), nb('nb-2', 'Bravo'),
      ], 4, true))),
      http.get('/api/v1/folders/f-page/contents', async () => {
        await delay(120)
        return HttpResponse.json({
          items: [nb('nb-3', 'Charlie'), nb('nb-4', 'Delta')],
          total: 4, has_more: false, offset: 2,
        })
      }),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    fireEvent.click(await screen.findByRole('button', { name: 'Load more notebooks' }))
    const busy = screen.getByRole('button', { name: 'Load more notebooks' })
    await waitFor(() => expect(busy).toBeDisabled())
    expect(busy).toHaveAttribute('aria-busy', 'true')
    expect(busy).toHaveTextContent('Loading…')

    expect(await screen.findByText('Delta')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Load more/ })).not.toBeInTheDocument()
  })

  test('shows the plain count when everything is loaded', async () => {
    server.use(
      http.get('/api/v1/folders/f-full', () => HttpResponse.json({
        ...folderPage([nb('nb-1', 'Solo'), nb('nb-2', 'Duo')], 2, false),
        folder: { id: 'f-full', org_id: 'org-1', name: 'Full', is_home: false, created_by: 'user-1', created_at: '', updated_at: '' },
      })),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-full' })

    expect(await screen.findByText('Solo')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.queryByText('2 of 2')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Load more/ })).not.toBeInTheDocument()
  })

  test('legacy payloads without totals/has_more show no counter and no button', async () => {
    server.use(
      http.get('/api/v1/folders/f-legacy', () => HttpResponse.json({
        folder: { id: 'f-legacy', org_id: 'org-1', name: 'Legacy', is_home: false, created_by: 'user-1', created_at: '', updated_at: '' },
        folders: [],
        notebooks: [nb('nb-legacy', 'Legacy Notebook')],
        connectors: [],
        dashboards: [],
      })),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-legacy' })

    expect(await screen.findByText('Legacy Notebook')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Load more/ })).not.toBeInTheDocument()
    expect(screen.queryByText(/\d+ of \d+/)).not.toBeInTheDocument()
    // The header carries the label alone — no plain count either.
    expect(screen.getByText('Notebooks').parentElement?.textContent).toBe('Notebooks')
  })

  test('filter hint appears only while filtering with unloaded rows', async () => {
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage(pageNotebooks(2, 0), 10, true))),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    expect(await screen.findByText('2 of 10')).toBeInTheDocument()
    expect(screen.queryByText(/Filtering loaded items/)).not.toBeInTheDocument()

    const filter = screen.getByLabelText('Search files')
    fireEvent.change(filter, { target: { value: 'Paged' } })
    expect(screen.getByText(/Filtering loaded items/)).toBeInTheDocument()
    // The top bar renders its own Ctrl K chip too, so scope to the hint line.
    expect(within(screen.getByText(/Filtering loaded items/)).getByText('Ctrl K')).toBeInTheDocument()

    fireEvent.change(filter, { target: { value: '' } })
    expect(screen.queryByText(/Filtering loaded items/)).not.toBeInTheDocument()
  })

  test('no filter hint when every row is loaded', async () => {
    server.use(
      http.get('/api/v1/folders/f-full2', () => HttpResponse.json({
        ...folderPage(pageNotebooks(2, 0), 2, false),
        folder: { id: 'f-full2', org_id: 'org-1', name: 'Full', is_home: false, created_by: 'user-1', created_at: '', updated_at: '' },
      })),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-full2' })

    expect(await screen.findByText('2')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Search files'), { target: { value: 'Paged' } })
    expect(screen.queryByText(/Filtering loaded items/)).not.toBeInTheDocument()
  })

  test('a successful refetch drops loaded extras (first page wins)', async () => {
    let pageOne = folderPage([nb('nb-1', 'Alpha'), nb('nb-2', 'Bravo')], 4, true)
    server.use(
      http.get('/api/v1/home', () => HttpResponse.json([])),
      http.get('/api/v1/folders', () => HttpResponse.json({
        folders: [{
          id: PAGE_FOLDER, org_id: 'org-1', name: 'Paginated', is_home: false,
          created_by: 'user-1',
          created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
        }],
        notebooks: [], connectors: [], dashboards: [],
        totals: { notebooks: 0, connectors: 0, dashboards: 0 },
        has_more: { notebooks: false, connectors: false, dashboards: false },
      })),
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(pageOne)),
      http.get('/api/v1/folders/f-page/contents', () => HttpResponse.json({
        items: [nb('nb-3', 'Charlie'), nb('nb-4', 'Delta')],
        total: 4, has_more: false, offset: 2,
      })),
    )
    renderWithProviders(<HomePage />)

    // Enter the folder and load its second page.
    fireEvent.click(await screen.findByText('Paginated'))
    expect(await screen.findByText('2 of 4')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Load more notebooks' }))
    expect(await screen.findByText('Charlie')).toBeInTheDocument()

    // The first page changes server-side; leaving and re-entering refetches it.
    pageOne = folderPage([nb('nb-new-1', 'Replacement One'), nb('nb-new-2', 'Replacement Two')], 2, false)
    fireEvent.click(screen.getAllByText('Files')[0])
    fireEvent.click(await screen.findByText('Paginated'))

    expect(await screen.findByText('Replacement One')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('Charlie')).not.toBeInTheDocument())
    await waitFor(() => expect(screen.queryByText('Alpha')).not.toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /^Load more/ })).not.toBeInTheDocument()
  })

  test('pages connector sections with the singular query type and keeps the button focusable', async () => {
    const requests: string[] = []
    server.use(
      http.get('/api/v1/folders/f-conn', () => HttpResponse.json({
        folder: { id: 'f-conn', org_id: 'org-1', name: 'Conn', is_home: false, created_by: 'user-1', created_at: '', updated_at: '' },
        folders: [],
        notebooks: [],
        connectors: [
          { id: 'c-1', name: 'Warehouse', type: 'clickhouse', created_by: 'user-1', created_at: '', updated_at: '' },
        ],
        dashboards: [],
        totals: { notebooks: 0, connectors: 2, dashboards: 0 },
        has_more: { notebooks: false, connectors: true, dashboards: false },
      })),
      http.get('/api/v1/folders/f-conn/contents', ({ request }) => {
        const url = new URL(request.url)
        requests.push(url.search)
        return HttpResponse.json({
          items: [{ id: 'c-2', name: 'Second Source', type: 'postgres', created_by: 'user-1', created_at: '', updated_at: '' }],
          total: 2, has_more: false, offset: 1,
        })
      }),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-conn' })

    expect(await screen.findByText('1 of 2')).toBeInTheDocument()
    const loadMore = screen.getByRole('button', { name: 'Load more connectors' })
    loadMore.focus()
    expect(loadMore).toHaveFocus()
    fireEvent.click(loadMore)

    expect(await screen.findByText('Second Source')).toBeInTheDocument()
    expect(requests[0]).toContain('type=connector')
    expect(requests[0]).toContain('offset=1')
    expect(requests[0]).toContain('limit=50')
  })

  test('extra-page rows resolve permissions lookups like first-page rows', async () => {
    const extraRow = { ...nb('nb-extra', 'Extra Row'), created_by: 'user-2', can_share: true }
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([
        { ...nb('nb-1', 'Alpha'), can_share: false },
      ], 2, true))),
      http.get('/api/v1/folders/f-page/contents', () => HttpResponse.json({
        items: [extraRow], total: 2, has_more: false, offset: 1,
      })),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    fireEvent.click(await screen.findByRole('button', { name: 'Load more notebooks' }))
    await screen.findByText('Extra Row')

    const menuBtn = screen.getAllByTitle('More options')
      .find(b => b.closest('.file-list-item')?.textContent?.includes('Extra Row'))
    fireEvent.click(menuBtn!)
    fireEvent.click(screen.getByText('Permissions'))

    // The panel must see the row's real owner/can_share, not undefined.
    const panel = await screen.findByTestId('permissions-panel')
    expect(panel.getAttribute('data-owner')).toBe('user-2')
    expect(panel.getAttribute('data-can-edit')).toBe('true')
  })

  test('an in-place invalidation drops loaded extras (mutation refetch)', async () => {
    let pageOne = folderPage([
      { ...nb('nb-1', 'Alpha'), can_edit: true },
      { ...nb('nb-2', 'Bravo'), can_edit: true },
    ], 4, true)
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(pageOne)),
      http.get('/api/v1/folders/f-page/contents', () => HttpResponse.json({
        items: [nb('nb-3', 'Charlie'), nb('nb-4', 'Delta')],
        total: 4, has_more: false, offset: 2,
      })),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    expect(await screen.findByText('2 of 4')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Load more notebooks' }))
    expect(await screen.findByText('Charlie')).toBeInTheDocument()

    // Rename Alpha in place: renameNotebook invalidates ['folder-contents'], and
    // the refetched first page no longer contains the extra rows.
    pageOne = folderPage([nb('nb-1', 'Alpha Renamed'), nb('nb-new', 'Echo')], 2, false)
    const renameBtn = screen.getAllByTitle('More options')
      .find(b => b.closest('.file-list-item')?.textContent?.includes('Alpha'))
    fireEvent.click(renameBtn!)
    fireEvent.click(screen.getByText('Rename'))
    const input = screen.getByDisplayValue('Alpha')
    await userEvent.clear(input)
    await userEvent.type(input, 'Alpha Renamed')
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(await screen.findByText('Echo')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('Charlie')).not.toBeInTheDocument())
    expect(screen.queryByText('Delta')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Load more/ })).not.toBeInTheDocument()
  })

  test('a double-click on Load more issues exactly one request', async () => {
    let pageCalls = 0
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([
        nb('nb-1', 'Alpha'), nb('nb-2', 'Bravo'),
      ], 4, true))),
      http.get('/api/v1/folders/f-page/contents', async () => {
        pageCalls++
        await delay(100)
        return HttpResponse.json({
          items: [nb('nb-3', 'Charlie'), nb('nb-4', 'Delta')],
          total: 4, has_more: false, offset: 2,
        })
      }),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    const loadMore = await screen.findByRole('button', { name: 'Load more notebooks' })
    // Two clicks in one task, before the pending re-render lands.
    fireEvent.click(loadMore)
    fireEvent.click(loadMore)

    expect(await screen.findByText('Delta')).toBeInTheDocument()
    expect(pageCalls).toBe(1)
  })

  test('root listing pages through /folders/contents', async () => {
    const requests: string[] = []
    server.use(
      http.get('/api/v1/home', () => HttpResponse.json([])),
      http.get('/api/v1/folders', () => HttpResponse.json({
        folders: [],
        notebooks: [nb('nb-root-1', 'Root Paged 1')],
        connectors: [], dashboards: [],
        totals: { notebooks: 2, connectors: 0, dashboards: 0 },
        has_more: { notebooks: true, connectors: false, dashboards: false },
      })),
      http.get('/api/v1/folders/contents', ({ request }) => {
        const url = new URL(request.url)
        requests.push(url.search)
        return HttpResponse.json({
          items: [nb('nb-root-2', 'Root Paged 2')],
          total: 2, has_more: false, offset: 1,
        })
      }),
    )
    renderWithProviders(<HomePage />)

    expect(await screen.findByText('1 of 2')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Load more notebooks' }))

    expect(await screen.findByText('Root Paged 2')).toBeInTheDocument()
    expect(requests).toHaveLength(1)
    expect(requests[0]).toContain('type=notebook')
    expect(requests[0]).toContain('offset=1')
    expect(requests[0]).toContain('limit=50')
  })
})

// ── Favorites toggles (F2) ────────────────────────────────────────────────────

describe('Favorites toggles (F2)', () => {
  test('bookmark state comes from the shared listing and every row type carries a toggle', async () => {
    const { handlers, counts } = favoritesFixture([
      { type: 'notebook', id: 'nb-1' },
      { type: 'folder', id: 'f-fav' },
    ])
    server.use(
      ...handlers,
      http.get('/api/v1/folders/f-page', () => HttpResponse.json({
        ...folderPage([nb('nb-1', 'Alpha'), nb('nb-2', 'Bravo')], 2, false),
        folders: [{
          id: 'f-fav', org_id: 'org-1', name: 'Favorites Folder', is_home: false,
          created_by: 'user-1', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
        }],
        connectors: [{
          id: 'c-1', name: 'Warehouse', type: 'clickhouse', created_by: 'user-1',
          created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
        }],
        dashboards: [{
          id: 'd-1', title: 'Ops Board', created_by: 'user-1',
          created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
        }],
        totals: { notebooks: 2, connectors: 1, dashboards: 1 },
      })),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    // The seeded notebook row renders filled straight from the listing.
    const notebookRow = (await screen.findByText('Alpha')).closest('.file-list-item') as HTMLElement
    expect(await within(notebookRow).findByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true')

    const folderNameMatches = screen.getAllByText('Favorites Folder')
    const folderCard = folderNameMatches[folderNameMatches.length - 1].closest('.file-list-item') as HTMLElement
    // Grid cards have no hover column: the bookmark is always visible there.
    expect(within(folderCard).getByRole('button', { name: 'Remove from favorites' })).not.toHaveClass('favorite-reveal')

    const connectorRow = screen.getByText('Warehouse').closest('.file-list-item') as HTMLElement
    expect(within(connectorRow).getByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()
    const dashboardRow = screen.getByText('Ops Board').closest('.file-list-item') as HTMLElement
    expect(within(dashboardRow).getByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()

    // Four toggle surfaces, one shared listing request.
    await waitFor(() => expect(counts.gets).toBe(1))
  })

  test('toggling a notebook row flips optimistically, never navigates, and a second click clears it', async () => {
    const { handlers } = favoritesFixture()
    server.use(
      ...handlers,
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([nb('nb-1', 'Alpha')], 1, false))),
    )
    renderWithProviders(
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/notebooks/:id" element={<div>Notebook opened</div>} />
      </Routes>,
      { initialPath: '/?folder=f-page' },
    )

    const row = (await screen.findByText('Alpha')).closest('.file-list-item') as HTMLElement
    await userEvent.click(within(row).getByRole('button', { name: 'Add to favorites' }))
    await waitFor(() =>
      expect(within(row).getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true'),
    )
    // The toggle is its own control: the row link never fires.
    expect(screen.queryByText('Notebook opened')).not.toBeInTheDocument()

    const active = within(row).getByRole('button', { name: 'Remove from favorites' })
    await waitFor(() => expect(active).toBeEnabled())
    await userEvent.click(active)
    await waitFor(() =>
      expect(within(row).getByRole('button', { name: 'Add to favorites' })).toHaveAttribute('aria-pressed', 'false'),
    )
    expect(screen.queryByText('Notebook opened')).not.toBeInTheDocument()
  })

  test('row bookmarks disable until the favorites listing arrives', async () => {
    let release: () => void = () => {}
    const parked = new Promise<void>((resolve) => { release = resolve })
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([nb('nb-1', 'Alpha')], 1, false))),
      http.get('/api/v1/favorites', async () => {
        await parked
        return HttpResponse.json({ groups: [] })
      }),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    const row = (await screen.findByText('Alpha')).closest('.file-list-item') as HTMLElement
    const button = within(row).getByRole('button', { name: 'Add to favorites' })
    expect(button).toBeDisabled()

    release()
    await waitFor(() => expect(button).toBeEnabled())
  })

  test('the bookmark column renders hidden-but-reserved when inactive', async () => {
    server.use(
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([nb('nb-1', 'Alpha')], 1, false))),
    )
    renderWithProviders(<HomePage />, { initialPath: '/?folder=f-page' })

    const row = (await screen.findByText('Alpha')).closest('.file-list-item') as HTMLElement
    expect(row.className).toContain('favorite-host')

    const button = within(row).getByRole('button', { name: 'Add to favorites' })
    // Hidden by opacity (theme.css `.favorite-reveal`), never `display: none`,
    // so its 26px column stays reserved and revealing cannot shift the row.
    expect(button.className).toContain('favorite-reveal')
    // A sibling of the row link (never inside it), immediately left of the ⋯.
    expect(button.closest('a')).toBeNull()
    const menu = within(row).getByTitle('More options')
    expect(button.compareDocumentPosition(menu) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})

// ── Favorites view (F3) ───────────────────────────────────────────────────────

/** Renders the URL so `?fav=1` round-trips can be asserted. */
function LocationProbe() {
  const { pathname, search } = useLocation()
  return <div data-testid="location">{pathname}{search}</div>
}

function locationText(): string {
  return screen.getByTestId('location').textContent ?? ''
}

function renderHome(initialPath = '/') {
  return renderWithProviders(
    <>
      <HomePage />
      <LocationProbe />
    </>,
    { initialPath },
  )
}

/** One favorites-listing row with a human title and folder path. */
function favoriteRow(
  type: FavoriteResourceType,
  id: string,
  title: string,
  extra: Partial<ResourceSummary> = {},
): ResourceSummary {
  return {
    id,
    resource_type: type,
    title,
    subtitle: type,
    updated_at: '2026-01-01T00:00:00Z',
    is_favorite: true,
    can_edit: true,
    ...extra,
  }
}

describe('Favorites view (F3)', () => {
  test('the chip toggles the flat favorites view and ?fav=1 round-trips through the URL', async () => {
    const { handlers } = favoritesFixture([{ type: 'notebook', id: 'nb-1' }])
    server.use(
      ...handlers,
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([nb('nb-1', 'Alpha')], 1, false))),
    )
    renderHome('/?folder=f-page')

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(locationText()).toBe('/?folder=f-page')

    fireEvent.click(screen.getByRole('button', { name: 'Favorites' }))
    // The folder param rides along so leaving the view returns to the folder.
    await waitFor(() => expect(locationText()).toBe('/?folder=f-page&fav=1'))
    // The folder listing is replaced by the flat favorites list.
    expect(screen.queryByText('Alpha')).not.toBeInTheDocument()
    expect(await screen.findByText('nb-1')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Favorites' }))
    await waitFor(() => expect(locationText()).toBe('/?folder=f-page'))
    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(screen.queryByText('nb-1')).not.toBeInTheDocument()
  })

  test('renders grouped rows with folder paths and per-type links in canonical type order', async () => {
    server.use(
      http.get('/api/v1/favorites', () => HttpResponse.json({
        groups: [
          {
            resource_type: 'notebook',
            results: [favoriteRow('notebook', 'nb-1', 'Revenue Notes', { folder_path: 'Engineering / Metrics' })],
          },
          {
            resource_type: 'folder',
            results: [favoriteRow('folder', 'f-fav', 'Shared Reports')],
          },
        ],
      })),
    )
    renderHome('/?fav=1')

    // The server sent notebooks first; the view renders the canonical order.
    const headers = await screen.findAllByRole('heading', { level: 3 })
    expect(headers.map((h) => h.textContent)).toEqual(['Folders · 1', 'Notebooks · 1'])

    expect(screen.getByText('Revenue Notes')).toBeInTheDocument()
    expect(screen.getByText('Engineering / Metrics')).toBeInTheDocument()
    expect(screen.getByText('Revenue Notes').closest('a')).toHaveAttribute('href', '/notebooks/nb-1')
    expect(screen.getByText('Shared Reports').closest('a')).toHaveAttribute('href', '/?folder=f-fav')
  })

  test('empty listing shows the bookmark hint, never a blank page', async () => {
    const { handlers } = favoritesFixture()
    server.use(...handlers)
    renderHome('/?fav=1')

    expect(await screen.findByText('No favorites yet')).toBeInTheDocument()
    expect(screen.getByText('Bookmark anything with the bookmark icon.')).toBeInTheDocument()
  })

  test('shows skeleton rows while the favorites listing loads', async () => {
    let release: () => void = () => {}
    const parked = new Promise<void>((resolve) => { release = resolve })
    server.use(
      http.get('/api/v1/favorites', async () => {
        await parked
        return HttpResponse.json({ groups: [] })
      }),
    )
    const { container } = renderHome('/?fav=1')

    await waitFor(() => expect(container.querySelector('[style*="skeleton-pulse"]')).not.toBeNull())
    expect(screen.queryByText('No favorites yet')).not.toBeInTheDocument()

    release()
    expect(await screen.findByText('No favorites yet')).toBeInTheDocument()
    expect(container.querySelector('[style*="skeleton-pulse"]')).toBeNull()
  })

  test('a failed favorites listing shows the error state instead of blanking', async () => {
    server.use(
      http.get('/api/v1/favorites', () => HttpResponse.json({ error: 'boom' }, { status: 500 })),
    )
    renderHome('/?fav=1')

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent("Couldn't load favorites.")
    expect(screen.queryByText('No favorites yet')).not.toBeInTheDocument()
  })

  test('a failed refetch keeps the cached (optimistically filtered) favorites rendered', async () => {
    let gets = 0
    server.use(
      http.get('/api/v1/favorites', () => {
        gets += 1
        if (gets === 1) {
          return HttpResponse.json({
            groups: [{
              resource_type: 'notebook',
              results: [
                favoriteRow('notebook', 'nb-1', 'Alpha Notes'),
                favoriteRow('notebook', 'nb-2', 'Bravo Notes'),
              ],
            }],
          })
        }
        return HttpResponse.json({ error: 'boom' }, { status: 500 })
      }),
      http.delete('/api/v1/favorites/:type/:id', () => new HttpResponse(null, { status: 204 })),
    )
    renderHome('/?fav=1')

    expect(await screen.findByText('Alpha Notes')).toBeInTheDocument()
    expect(screen.getByText('Bravo Notes')).toBeInTheDocument()

    // Un-favorite one: the success invalidation refetches, the refetch fails.
    const row = screen.getByText('Alpha Notes').closest('.favorite-host') as HTMLElement
    await userEvent.click(within(row).getByRole('button', { name: 'Remove from favorites' }))
    await waitFor(() => expect(gets).toBe(2))

    // The cached listing survives: the removal sticks and the sibling row
    // stays, instead of an error panel replacing what we demonstrably have.
    expect(screen.queryByText('Alpha Notes')).not.toBeInTheDocument()
    expect(screen.getByText('Bravo Notes')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByText('No favorites yet')).not.toBeInTheDocument()
  })

  test('the local name filter narrows the loaded favorites and shows the hint', async () => {
    const { handlers } = favoritesFixture([
      { type: 'notebook', id: 'nb-1' },
      { type: 'notebook', id: 'nb-2' },
    ])
    server.use(...handlers)
    renderHome('/?fav=1')

    expect(await screen.findByText('nb-1')).toBeInTheDocument()
    expect(screen.getByText('nb-2')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Search files'), { target: { value: 'nb-2' } })
    expect(screen.queryByText('nb-1')).not.toBeInTheDocument()
    expect(screen.getByText('nb-2')).toBeInTheDocument()
    expect(screen.getByText(/Filtering loaded items/)).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Search files'), { target: { value: '' } })
    expect(screen.getByText('nb-1')).toBeInTheDocument()
  })

  test('un-favoriting a row drops it immediately, before the request settles', async () => {
    const { handlers, held } = favoritesFixture([{ type: 'notebook', id: 'nb-1' }])
    let release: () => void = () => {}
    const parked = new Promise<void>((resolve) => { release = resolve })
    server.use(
      http.delete('/api/v1/favorites/:type/:id', async ({ params }) => {
        await parked
        held.get(params.type as FavoriteResourceType)?.delete(params.id as string)
        return new HttpResponse(null, { status: 204 })
      }),
      ...handlers,
    )
    renderHome('/?fav=1')

    const row = (await screen.findByText('nb-1')).closest('.favorite-host') as HTMLElement
    await userEvent.click(within(row).getByRole('button', { name: 'Remove from favorites' }))

    // The DELETE is still parked; the optimistic id-set flip still drops it.
    expect(await screen.findByText('No favorites yet')).toBeInTheDocument()
    expect(screen.queryByText('nb-1')).not.toBeInTheDocument()

    release()
    await waitFor(() => expect(held.get('notebook')?.has('nb-1')).toBe(false))
    // The refetched listing agrees: nothing comes back.
    expect(screen.getByText('No favorites yet')).toBeInTheDocument()
  })

  test('the folder view is untouched while the chip is off', async () => {
    const { handlers } = favoritesFixture([{ type: 'notebook', id: 'nb-9' }])
    server.use(
      ...handlers,
      http.get('/api/v1/folders/f-page', () => HttpResponse.json(folderPage([nb('nb-1', 'Alpha')], 1, false))),
    )
    renderHome('/?folder=f-page')

    expect(await screen.findByText('Alpha')).toBeInTheDocument()
    expect(screen.getByText('Notebooks')).toBeInTheDocument()
    // No favorites-view chrome, and favorites from elsewhere are not listed.
    expect(screen.queryByRole('heading', { level: 3 })).not.toBeInTheDocument()
    expect(screen.queryByText('nb-9')).not.toBeInTheDocument()
    expect(locationText()).toBe('/?folder=f-page')
  })
})
