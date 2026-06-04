import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { GroupsPage } from '../pages/GroupsPage'
import { renderWithProviders, editorUser } from './utils'

// AppShell calls useAuth — already provided by renderWithProviders.
// Mock AppShell to avoid rendering TopBar/Sidebar (reduces noise).
vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

beforeEach(() => {
  vi.clearAllMocks()
})

// ── T8.1 View groups page ───────────────────────────────────────────────────

describe('View groups', () => {
  test('shows all org groups', async () => {
    renderWithProviders(<GroupsPage />)
    expect(await screen.findByText('Data Team')).toBeInTheDocument()
    expect(await screen.findByText('CSIRT')).toBeInTheDocument()
  })

  test('shows member count per group', async () => {
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    // g-1 has member_count: 1 → "1 member" (singular); g-2 has member_count: 0 → "0 members"
    expect(screen.getByText('1 member')).toBeInTheDocument()
    expect(screen.getByText('0 members')).toBeInTheDocument()
  })
})

// ── T8.1 Non-admin cannot see admin controls ────────────────────────────────

describe('Admin gating', () => {
  test('editor does not see New Group button', async () => {
    renderWithProviders(<GroupsPage />, { user: editorUser() })
    await screen.findByText('Data Team')
    expect(screen.queryByText('+ New Group')).toBeNull()
  })

  test('editor does not see Rename or Delete buttons', async () => {
    renderWithProviders(<GroupsPage />, { user: editorUser() })
    await screen.findByText('Data Team')
    expect(screen.queryByText('Rename')).toBeNull()
    expect(screen.queryByText('Delete')).toBeNull()
  })

  test('admin sees New Group button', async () => {
    renderWithProviders(<GroupsPage />) // admin by default
    await screen.findByText('Data Team')
    expect(screen.getByText('+ New Group')).toBeInTheDocument()
  })
})

// ── T8.2 Create group ───────────────────────────────────────────────────────

describe('Create group', () => {
  test('typing name and clicking + New Group calls POST /api/v1/groups', async () => {
    let postedName = ''
    server.use(
      http.post('/api/v1/groups', async ({ request }) => {
        const body = await request.json() as Record<string, unknown>
        postedName = body.name as string
        return HttpResponse.json(
          { id: 'g-new', org_id: 'org-1', name: postedName,
            member_count: 0, created_at: '2026-01-01T00:00:00Z' },
          { status: 201 }
        )
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    const input = screen.getByPlaceholderText('New group name')
    fireEvent.change(input, { target: { value: 'Analytics' } })
    fireEvent.click(screen.getByText('+ New Group'))
    await waitFor(() => expect(postedName).toBe('Analytics'))
  })
})

// ── T8.3 Add member ─────────────────────────────────────────────────────────

describe('Add member', () => {
  test('expanding a group fetches and shows its members', async () => {
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    fireEvent.click(screen.getByText('Data Team'))
    expect(await screen.findByText('Bob Editor')).toBeInTheDocument()
  })

  test('expanding a group with no members shows "No members" message', async () => {
    renderWithProviders(<GroupsPage />)
    await screen.findByText('CSIRT')
    fireEvent.click(screen.getByText('CSIRT'))
    expect(await screen.findByText('No members in this group.')).toBeInTheDocument()
  })

  test('Add button calls POST /api/v1/groups/:id/members', async () => {
    let addedUserId = ''
    server.use(
      http.post('/api/v1/groups/:id/members', async ({ request }) => {
        const body = await request.json() as Record<string, unknown>
        addedUserId = body.user_id as string
        return HttpResponse.json({ user_id: addedUserId }, { status: 201 })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('CSIRT')
    fireEvent.click(screen.getByText('CSIRT'))
    await screen.findByText('No members in this group.')

    // Open MemberDropdown and select Bob Editor (user-2)
    fireEvent.click(screen.getByText('Add member…'))
    fireEvent.mouseDown(await screen.findByRole('option', { name: /Bob Editor/i }))
    fireEvent.click(screen.getByText('Add'))
    await waitFor(() => expect(addedUserId).toBe('user-2'))
  })

  test('error banner shown when add member fails', async () => {
    server.use(
      http.post('/api/v1/groups/:id/members',
        () => HttpResponse.json({ error: 'conflict' }, { status: 409 })
      )
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('CSIRT')
    fireEvent.click(screen.getByText('CSIRT'))
    await screen.findByText('No members in this group.')

    // Open MemberDropdown and select Bob Editor (user-2)
    fireEvent.click(screen.getByText('Add member…'))
    fireEvent.mouseDown(await screen.findByRole('option', { name: /Bob Editor/i }))
    fireEvent.click(screen.getByText('Add'))
    expect(await screen.findByText(/conflict/i)).toBeInTheDocument()
  })
})

// ── T8.4 Remove member ──────────────────────────────────────────────────────

describe('Remove member', () => {
  test('× button calls DELETE /api/v1/groups/:id/members/:uid', async () => {
    let deletedUid = ''
    server.use(
      http.delete('/api/v1/groups/:id/members/:uid', ({ params }) => {
        deletedUid = params.uid as string
        return new HttpResponse(null, { status: 204 })
      })
    )
    // Mock confirm to accept the removal
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    fireEvent.click(screen.getByText('Data Team'))
    await screen.findByText('Bob Editor')
    // The remove button has title="Remove from group" (static title on the component)
    fireEvent.click(screen.getByTitle('Remove from group'))
    await waitFor(() => expect(deletedUid).toBe('user-2'))
    vi.restoreAllMocks()
  })
})

// ── T8.5 Rename group ───────────────────────────────────────────────────────

describe('Rename group', () => {
  test('clicking Rename shows inline input', async () => {
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    // Click the ⋯ menu button (title="Group actions")
    const menuButtons = screen.getAllByTitle('Group actions')
    fireEvent.click(menuButtons[0])
    // Now click Rename in the context menu
    fireEvent.click(screen.getByText('Rename'))
    expect(screen.getByDisplayValue('Data Team')).toBeInTheDocument()
  })

  test('pressing Enter saves the new name via PUT /api/v1/groups/:id', async () => {
    let putName = ''
    server.use(
      http.put('/api/v1/groups/g-1', async ({ request }) => {
        const body = await request.json() as Record<string, unknown>
        putName = body.name as string
        return HttpResponse.json({ id: 'g-1', org_id: 'org-1', name: putName,
          member_count: 1, created_at: '2026-01-01T00:00:00Z' })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    const menuButtons = screen.getAllByTitle('Group actions')
    fireEvent.click(menuButtons[0])
    fireEvent.click(screen.getByText('Rename'))
    const input = screen.getByDisplayValue('Data Team')
    fireEvent.change(input, { target: { value: 'Eng Team' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(putName).toBe('Eng Team'))
  })
})

// ── T8.6 Delete group ───────────────────────────────────────────────────────

describe('Delete group', () => {
  test('clicking Delete calls DELETE /api/v1/groups/:id after confirm', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    let deletedId = ''
    server.use(
      http.delete('/api/v1/groups/:id', ({ params }) => {
        deletedId = params.id as string
        return new HttpResponse(null, { status: 204 })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    // Click the ⋯ menu button
    const menuButtons = screen.getAllByTitle('Group actions')
    fireEvent.click(menuButtons[0])
    // Click Delete in the context menu
    fireEvent.click(screen.getByText('Delete'))
    await waitFor(() => expect(deletedId).toBe('g-1'))
  })

  test('Delete does nothing if user cancels confirm', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    let deleteCalled = false
    server.use(
      http.delete('/api/v1/groups/:id', () => {
        deleteCalled = true
        return new HttpResponse(null, { status: 204 })
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    const menuButtons = screen.getAllByTitle('Group actions')
    fireEvent.click(menuButtons[0])
    fireEvent.click(screen.getByText('Delete'))
    await new Promise(r => setTimeout(r, 50))
    expect(deleteCalled).toBe(false)
  })
})
