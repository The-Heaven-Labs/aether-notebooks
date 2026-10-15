import { screen, fireEvent, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse, delay } from 'msw'
import { HomePage } from '../pages/HomePage'
import type { FolderContents } from '../types'
import { server } from './server'
import { renderWithProviders } from './utils'

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
