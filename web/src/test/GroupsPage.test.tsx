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
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    fireEvent.click(screen.getByText('Data Team'))
    await screen.findByText('Bob Editor')
    // Click the remove button which opens ConfirmDialog
    fireEvent.click(screen.getByTitle('Remove from group'))
    // Click the confirm button in the ConfirmDialog
    fireEvent.click(screen.getByRole('button', { name: /remove/i }))
    await waitFor(() => expect(deletedUid).toBe('user-2'))
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

// ── T8.7 Pre-provisioned (pending) members ──────────────────────────────────

describe('Pending members', () => {
  test('shows pending rows with a badge and removes by email', async () => {
    let deletedEmail = ''
    server.use(
      http.get('/api/v1/groups/g-1/pending-members', () => HttpResponse.json([
        { email: 'future@example.com', created_at: '2026-01-01T00:00:00Z' },
      ])),
      http.delete('/api/v1/groups/:id/pending-members/:email', ({ params }) => {
        deletedEmail = params.email as string
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    fireEvent.click(screen.getByText('Data Team'))
    expect(await screen.findByText('future@example.com')).toBeInTheDocument()
    expect(screen.getByText('Pending — awaiting first login')).toBeInTheDocument()

    fireEvent.click(screen.getByTitle('Remove pending member'))
    fireEvent.click(screen.getByRole('button', { name: /remove/i }))
    await waitFor(() => expect(deletedEmail).toBe('future@example.com'))
  })

  test('dropdown offers adding an unmatched email as pending', async () => {
    let posted: string[] = []
    server.use(
      http.post('/api/v1/groups/:id/pending-members', async ({ request }) => {
        const body = await request.json() as { emails: string[] }
        posted = body.emails
        return HttpResponse.json({ added: body.emails.length, skipped: [] })
      }),
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('CSIRT')
    fireEvent.click(screen.getByText('CSIRT'))
    await screen.findByText('No members in this group.')

    fireEvent.click(screen.getByText('Add member…'))
    fireEvent.change(screen.getByPlaceholderText('Search…'), { target: { value: 'newbie@example.com' } })
    fireEvent.mouseDown(await screen.findByText('Add newbie@example.com as pending member'))
    await waitFor(() => expect(posted).toEqual(['newbie@example.com']))
  })

  test('bulk paste stages multiple emails and reports skips', async () => {
    let posted: string[] = []
    server.use(
      http.post('/api/v1/groups/:id/pending-members', async ({ request }) => {
        const body = await request.json() as { emails: string[] }
        posted = body.emails
        return HttpResponse.json({
          added: 2,
          skipped: [{ email: 'dup@example.com', reason: 'already_pending' }],
        })
      }),
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('CSIRT')
    fireEvent.click(screen.getByText('CSIRT'))
    await screen.findByText('No members in this group.')

    fireEvent.click(screen.getByText('Paste emails'))
    fireEvent.change(screen.getByLabelText('Emails to pre-provision'), {
      target: { value: 'a@example.com, b@example.com\ndup@example.com' },
    })
    fireEvent.click(screen.getByText('Add emails'))

    await waitFor(() => expect(posted).toEqual(['a@example.com', 'b@example.com', 'dup@example.com']))
    expect(await screen.findByText('2 added, 1 skipped: already pending')).toBeInTheDocument()
  })
})

// ── T8.6 Delete group ───────────────────────────────────────────────────────

describe('Delete group', () => {
  test('clicking Delete calls DELETE /api/v1/groups/:id after confirm', async () => {
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
    // Click the confirm button in the ConfirmDialog
    fireEvent.click(screen.getByRole('button', { name: /delete/i }))
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

// ── T8.8 Group display names ────────────────────────────────────────────────

describe('Display names', () => {
  test('listing renders the label when set and the name otherwise', async () => {
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([
        { id: 'g-1', org_id: 'org-1', name: 'aether-analysts',
          display_name: 'Data Analysts', member_count: 1, created_at: '2026-01-01T00:00:00Z' },
        { id: 'g-2', org_id: 'org-1', name: 'CSIRT',
          display_name: null, member_count: 0, created_at: '2026-01-01T00:00:00Z' },
      ])),
    )
    renderWithProviders(<GroupsPage />)
    const label = await screen.findByText('Data Analysts')
    expect(label).toHaveAttribute('title', 'aether-analysts')
    expect(screen.getByText('CSIRT')).toBeInTheDocument()
    expect(screen.queryByText('aether-analysts')).toBeNull()
  })

  test('creating with a display name sends it in the POST payload', async () => {
    let posted: Record<string, unknown> = {}
    server.use(
      http.post('/api/v1/groups', async ({ request }) => {
        posted = await request.json() as Record<string, unknown>
        return HttpResponse.json(
          { id: 'g-new', org_id: 'org-1', name: posted.name,
            display_name: posted.display_name, member_count: 0, created_at: '2026-01-01T00:00:00Z' },
          { status: 201 }
        )
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Data Team')
    fireEvent.change(screen.getByPlaceholderText('New group name'), { target: { value: 'Analytics' } })
    fireEvent.change(screen.getByLabelText('New group display name'), { target: { value: 'Data Analysts' } })
    fireEvent.click(screen.getByText('+ New Group'))
    await waitFor(() => expect(posted).toEqual({ name: 'Analytics', display_name: 'Data Analysts' }))
  })

  test('renaming sends name and display_name, and clearing sends an empty label', async () => {
    let putBody: Record<string, unknown> = {}
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([
        { id: 'g-1', org_id: 'org-1', name: 'Data Team',
          display_name: 'Core Data', member_count: 1, created_at: '2026-01-01T00:00:00Z' },
        { id: 'g-2', org_id: 'org-1', name: 'CSIRT',
          display_name: null, member_count: 0, created_at: '2026-01-01T00:00:00Z' },
      ])),
      http.put('/api/v1/groups/g-1', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json(
          { id: 'g-1', org_id: 'org-1', name: putBody.name,
            display_name: putBody.display_name, member_count: 1, created_at: '2026-01-01T00:00:00Z' }
        )
      })
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Core Data')

    // Setting a label sends it alongside the identity name.
    fireEvent.click(screen.getAllByTitle('Group actions')[0])
    fireEvent.click(screen.getByText('Rename'))
    const labelInput = screen.getByLabelText('Group display name')
    expect(labelInput).toHaveValue('Core Data')
    fireEvent.change(labelInput, { target: { value: 'Core Data 2' } })
    fireEvent.keyDown(labelInput, { key: 'Enter' })
    await waitFor(() => expect(putBody).toEqual({ name: 'Data Team', display_name: 'Core Data 2' }))

    // Clearing the label sends an explicit blank.
    fireEvent.click(screen.getAllByTitle('Group actions')[0])
    fireEvent.click(screen.getByText('Rename'))
    const clearInput = screen.getByLabelText('Group display name')
    fireEvent.change(clearInput, { target: { value: '' } })
    fireEvent.keyDown(clearInput, { key: 'Enter' })
    await waitFor(() => expect(putBody).toEqual({ name: 'Data Team', display_name: '' }))
  })

  test('the Everyone group exposes no label control', async () => {
    server.use(
      http.get('/api/v1/groups', () => HttpResponse.json([
        { id: 'g-everyone', org_id: 'org-1', name: 'Everyone',
          display_name: 'All Staff', member_count: 3, created_at: '2026-01-01T00:00:00Z' },
      ])),
    )
    renderWithProviders(<GroupsPage />)
    await screen.findByText('Everyone')
    expect(screen.getByText('System')).toBeInTheDocument()
    expect(screen.queryByTitle('Group actions')).toBeNull()
    expect(screen.queryByLabelText('Group display name')).toBeNull()
    expect(screen.queryByText('All Staff')).toBeNull()
  })
})
