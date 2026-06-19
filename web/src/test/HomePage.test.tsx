import { screen, fireEvent, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { HomePage } from '../pages/HomePage'
import { server } from './server'
import { renderWithProviders } from './utils'

// Mock PermissionsPanel to avoid real API calls
vi.mock('../components/PermissionsPanel', () => ({
  PermissionsPanel: ({ onClose }: { onClose: () => void }) => (
    <div data-testid="permissions-panel">
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
    expect(await screen.findByText('Files')).toBeInTheDocument()
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
  test('clicking Rename shows inline input pre-filled with current name', async () => {
    renderWithProviders(<HomePage />)
    await screen.findAllByText('Engineering')
    const menuBtns = screen.getAllByTitle('More options')
    // Engineering is the second folder (index 1)
    fireEvent.click(menuBtns[1])
    fireEvent.click(screen.getByText('Rename'))
    const input = screen.getByDisplayValue('Engineering')
    expect(input).toBeInTheDocument()
  })

  test('Escape cancels rename', async () => {
    renderWithProviders(<HomePage />)
    await screen.findAllByText('Engineering')
    const menuBtns = screen.getAllByTitle('More options')
    fireEvent.click(menuBtns[1])
    fireEvent.click(screen.getByText('Rename'))
    const input = screen.getByDisplayValue('Engineering')
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
    await screen.findAllByText('Engineering')
    const menuBtns = screen.getAllByTitle('More options')
    fireEvent.click(menuBtns[1])
    fireEvent.click(screen.getByText('Rename'))
    const input = screen.getByDisplayValue('Engineering')
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
    renderWithProviders(<HomePage />)
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
    renderWithProviders(<HomePage />)
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
