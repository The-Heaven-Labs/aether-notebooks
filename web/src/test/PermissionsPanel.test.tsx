import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { PermissionsPanel } from '../components/PermissionsPanel'
import { renderWithProviders } from './utils'
import { ACL_ENTRIES } from './handlers'

const MEMBERS_WITH_USER_ID = [
  { user_id: 'user-1', name: 'Alice Admin', email: 'alice@test.com', role: 'admin' },
  { user_id: 'user-2', name: 'Bob Editor', email: 'bob@test.com', role: 'editor' },
]

beforeEach(() => {
  vi.clearAllMocks()
  // Override members handler so component can resolve subject names by id
  server.use(
    http.get('/api/v1/members', () => HttpResponse.json(MEMBERS_WITH_USER_ID))
  )
})

function renderPanel(overrides?: Partial<Parameters<typeof PermissionsPanel>[0]>) {
  const props = {
    resourceType: 'notebook' as const,
    resourceId: 'nb-1',
    resourceName: 'Q1 Report',
    onClose: vi.fn(),
    ...overrides,
  }
  return renderWithProviders(<PermissionsPanel {...props} />)
}

// Wait for ACL to finish loading (Add button appears when not loading)
async function waitForAclLoaded() {
  await screen.findByRole('button', { name: /^Add$/i })
}

// ── T7.1 Panel renders correctly ────────────────────────────────────────────

describe('Panel renders', () => {
  test('shows resource name in header', async () => {
    renderPanel()
    expect(await screen.findByText('Q1 Report')).toBeInTheDocument()
  })

  test('shows resource type badge', async () => {
    renderPanel()
    await screen.findByText('Q1 Report')
    // typeBadge text content is 'notebook' (CSS uppercases it visually)
    expect(screen.getByText('notebook')).toBeInTheDocument()
  })

  test('shows existing ACL entries', async () => {
    renderPanel()
    // ACL_ENTRIES has one entry with subject_id 'user-1' (Alice Admin)
    // Use getAllByText since name may appear in both the entry span and the select option
    const matches = await screen.findAllByText('Alice Admin')
    expect(matches.length).toBeGreaterThan(0)
  })

  test('shows action checkboxes for notebook type', async () => {
    renderPanel()
    // Wait for ACL to load so entry rows + add-row are rendered
    await waitForAclLoaded()
    expect(screen.getAllByRole('checkbox').length).toBeGreaterThan(0)
  })

  test('shows "No inherited permissions" when no parent folder', async () => {
    renderPanel() // no parentFolderId
    await screen.findByText(/No inherited permissions/i)
  })

  test('shows inheritance count when parent folder provided', async () => {
    server.use(
      http.get('/api/v1/acl/folder/f-eng', () =>
        HttpResponse.json([ACL_ENTRIES[0], ACL_ENTRIES[0]]) // 2 entries
      )
    )
    renderPanel({ parentFolderId: 'f-eng' })
    expect(await screen.findByText(/Inheriting 2 permissions/i)).toBeInTheDocument()
  })
})

// ── T7.1 Close button ───────────────────────────────────────────────────────

describe('Close', () => {
  test('close button calls onClose', async () => {
    const onClose = vi.fn()
    renderPanel({ onClose })
    await screen.findByText('Q1 Report')
    const closeBtn = screen.getByTitle('Close')
    fireEvent.click(closeBtn)
    expect(onClose).toHaveBeenCalled()
  })
})

// ── T7.2 Add ACL entry ──────────────────────────────────────────────────────

describe('Add entry', () => {
  test('Add button is disabled when no subject selected', async () => {
    server.use(
      http.get('/api/v1/acl/notebook/nb-1', () => HttpResponse.json([]))
    )
    renderPanel()
    // Wait for ACL to finish loading (Add button appears)
    const addBtn = await screen.findByRole('button', { name: /^Add$/i })
    expect(addBtn).toBeDisabled()
  })
})

// ── T7.2 Draft mode ─────────────────────────────────────────────────────────

describe('Draft mode', () => {
  test('Save and Discard buttons not shown when no changes', async () => {
    renderPanel()
    await waitForAclLoaded()
    expect(screen.queryByRole('button', { name: /save/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /discard/i })).toBeNull()
  })

  test('toggling a checkbox shows Save + Discard buttons', async () => {
    renderPanel()
    await screen.findAllByText('Alice Admin')
    await waitForAclLoaded()
    const entryRow = screen.getAllByText('Alice Admin')[0].closest('[tabIndex="0"]')
    if (entryRow) fireEvent.click(entryRow)
    const expandedCheckboxes = entryRow?.querySelectorAll('input[type="checkbox"]') || []
    if (expandedCheckboxes.length > 0) {
      fireEvent.click(expandedCheckboxes[0])
    }
    expect(await screen.findByRole('button', { name: /save/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /discard/i })).toBeInTheDocument()
  })

  test('Discard resets changes', async () => {
    renderPanel()
    await screen.findAllByText('Alice Admin')
    await waitForAclLoaded()
    const entryRow = screen.getAllByText('Alice Admin')[0].closest('[tabIndex="0"]')
    if (entryRow) fireEvent.click(entryRow)
    const expandedCheckboxes = entryRow?.querySelectorAll('input[type="checkbox"]') || []
    if (expandedCheckboxes.length > 0) {
      fireEvent.click(expandedCheckboxes[0])
    }
    await screen.findByRole('button', { name: /discard/i })
    fireEvent.click(screen.getByRole('button', { name: /discard/i }))
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /save/i })).toBeNull()
    )
  })

  test('Save calls PUT /api/v1/acl/notebook/nb-1', async () => {
    let putBody: unknown
    server.use(
      http.put('/api/v1/acl/notebook/nb-1', async ({ request }) => {
        putBody = await request.json()
        return HttpResponse.json([])
      })
    )
    renderPanel()
    await screen.findAllByText('Alice Admin')
    await waitForAclLoaded()
    const entryRow = screen.getAllByText('Alice Admin')[0].closest('[tabIndex="0"]')
    if (entryRow) fireEvent.click(entryRow)
    const expandedCheckboxes = entryRow?.querySelectorAll('input[type="checkbox"]') || []
    if (expandedCheckboxes.length > 0) {
      fireEvent.click(expandedCheckboxes[0])
    }
    await screen.findByRole('button', { name: /save/i })
    fireEvent.click(screen.getByRole('button', { name: /save/i }))
    await waitFor(() => expect(putBody).toBeDefined())
  })
})

// ── Remove entry ────────────────────────────────────────────────────────────

describe('Remove entry', () => {
  test('remove button adds entry to draft for removal', async () => {
    renderPanel()
    await screen.findAllByText('Alice Admin')
    const entryRow = screen.getAllByText('Alice Admin')[0].closest('[tabIndex="0"]')
    if (entryRow) fireEvent.click(entryRow)
    await waitFor(() => {
      const btns = screen.queryAllByTitle('Remove')
      expect(btns.length).toBeGreaterThan(0)
    })
    const removeBtn = screen.getByTitle('Remove')
    fireEvent.click(removeBtn)
    expect(await screen.findByRole('button', { name: /save/i })).toBeInTheDocument()
  })
})

// ── Correct actions per resource type ───────────────────────────────────────

describe('Action sets', () => {
  test('folder shows manage action', async () => {
    server.use(
      http.get('/api/v1/acl/folder/f-1', () => HttpResponse.json([]))
    )
    renderPanel({ resourceType: 'folder', resourceId: 'f-1', resourceName: 'Docs' })
    // Wait for ACL to load (Add button signals end of loading state)
    await screen.findByRole('button', { name: /^Add$/i })
    expect(screen.getByText('manage')).toBeInTheDocument()
  })

  test('dashboard does not show run action', async () => {
    server.use(
      http.get('/api/v1/acl/dashboard/d-1', () => HttpResponse.json([]))
    )
    renderPanel({ resourceType: 'dashboard', resourceId: 'd-1', resourceName: 'Sales' })
    await screen.findByRole('button', { name: /^Add$/i })
    expect(screen.queryByText('run')).toBeNull()
  })

  test('agent_session offers only the view action', async () => {
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([]))
    )
    renderPanel({ resourceType: 'agent_session', resourceId: 's-1', resourceName: 'Revenue analysis' })
    await screen.findByRole('button', { name: /^Add$/i })

    expect(screen.getByText('view')).toBeInTheDocument()
    expect(screen.getAllByRole('checkbox')).toHaveLength(1)
    expect(screen.queryByText('edit')).toBeNull()
    expect(screen.queryByText('share')).toBeNull()
    expect(screen.queryByText('delete')).toBeNull()
    expect(screen.queryByText('run')).toBeNull()
  })

  test('agent_session Save PUTs view-only entries', async () => {
    let putPath = ''
    let putBody: { entries: Array<{ subject_type: string; subject_id: string; actions: string[] }> } | null = null
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () =>
        HttpResponse.json([
          {
            id: 'acl-9', org_id: 'org-1', resource_type: 'agent_session', resource_id: 's-1',
            subject_type: 'user', subject_id: 'user-2', actions: ['view'],
            created_at: '2026-01-01T00:00:00Z',
          },
        ])
      ),
      http.put('/api/v1/acl/agent_session/s-1', async ({ request }) => {
        putPath = '/api/v1/acl/agent_session/s-1'
        putBody = await request.json() as typeof putBody
        return HttpResponse.json([])
      })
    )
    renderPanel({ resourceType: 'agent_session', resourceId: 's-1', resourceName: 'Revenue analysis' })

    const name = await screen.findByText('Bob Editor')
    const entryRow = name.closest('[tabIndex="0"]')
    expect(entryRow).not.toBeNull()
    fireEvent.click(entryRow!)
    const checkbox = entryRow!.querySelector<HTMLInputElement>('input[type="checkbox"]')
    expect(checkbox).not.toBeNull()
    // Toggle off then on so the draft retains exactly the view action.
    fireEvent.click(checkbox!)
    fireEvent.click(checkbox!)
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putPath).toBe('/api/v1/acl/agent_session/s-1')
    expect(putBody!.entries).toHaveLength(1)
    expect(putBody!.entries[0]).toMatchObject({
      subject_type: 'user', subject_id: 'user-2', actions: ['view'],
    })
  })

  test('agent_session constrains every entry to view-only actions in the PUT payload', async () => {
    let putBody: { entries: Array<{ subject_type: string; subject_id: string; actions: string[] }> } | null = null
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () =>
        HttpResponse.json([
          {
            id: 'acl-owner', org_id: 'org-1', resource_type: 'agent_session', resource_id: 's-1',
            subject_type: 'user', subject_id: 'user-1',
            actions: ['view', 'edit', 'share', 'delete', 'admin'],
            created_at: '2026-01-01T00:00:00Z',
          },
          {
            id: 'acl-bob', org_id: 'org-1', resource_type: 'agent_session', resource_id: 's-1',
            subject_type: 'user', subject_id: 'user-2', actions: ['view'],
            created_at: '2026-01-01T00:00:00Z',
          },
        ])
      ),
      http.put('/api/v1/acl/agent_session/s-1', async ({ request }) => {
        putBody = await request.json() as typeof putBody
        return HttpResponse.json([])
      })
    )
    renderPanel({ resourceType: 'agent_session', resourceId: 's-1', resourceName: 'Revenue analysis' })

    const name = await screen.findByText('Bob Editor')
    const entryRow = name.closest('[tabIndex="0"]')
    expect(entryRow).not.toBeNull()
    fireEvent.click(entryRow!)
    const checkbox = entryRow!.querySelector<HTMLInputElement>('input[type="checkbox"]')
    expect(checkbox).not.toBeNull()
    // Toggle off then on so the draft retains exactly the view action.
    fireEvent.click(checkbox!)
    fireEvent.click(checkbox!)
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    const entries = putBody!.entries
    expect(entries).toHaveLength(2)
    const owner = entries.find((e) => e.subject_id === 'user-1')
    expect(owner).toBeDefined()
    expect(owner!.actions).toEqual(['view'])
    const share = entries.find((e) => e.subject_id === 'user-2')
    expect(share).toBeDefined()
    expect(share!.actions).toEqual(['view'])
  })

  test('notebook keeps all of its allowed actions in the PUT payload', async () => {
    let putBody: { entries: Array<{ subject_type: string; subject_id: string; actions: string[] }> } | null = null
    server.use(
      http.put('/api/v1/acl/notebook/nb-1', async ({ request }) => {
        putBody = await request.json() as typeof putBody
        return HttpResponse.json([])
      })
    )
    renderPanel()
    await screen.findAllByText('Alice Admin')
    const entryRow = screen.getAllByText('Alice Admin')[0].closest('[tabIndex="0"]')
    expect(entryRow).not.toBeNull()
    fireEvent.click(entryRow!)
    const checkboxes = entryRow!.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')
    expect(checkboxes.length).toBeGreaterThan(0)
    // Toggle off then on to create a draft without changing the action set.
    fireEvent.click(checkboxes[0])
    fireEvent.click(checkboxes[0])
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody!.entries).toHaveLength(1)
    expect([...putBody!.entries[0].actions].sort()).toEqual(
      ['delete', 'edit', 'run', 'share', 'view']
    )
  })

  test('notebook preserves the unrendered create action on save', async () => {
    let putBody: { entries: Array<{ subject_type: string; subject_id: string; actions: string[] }> } | null = null
    server.use(
      http.get('/api/v1/acl/notebook/nb-1', () =>
        HttpResponse.json([
          {
            id: 'acl-owner', org_id: 'org-1', resource_type: 'notebook', resource_id: 'nb-1',
            subject_type: 'user', subject_id: 'user-1',
            actions: ['view', 'run', 'edit', 'share', 'delete', 'create'],
            created_at: '2026-01-01T00:00:00Z',
          },
        ])
      ),
      http.put('/api/v1/acl/notebook/nb-1', async ({ request }) => {
        putBody = await request.json() as typeof putBody
        return HttpResponse.json([])
      })
    )
    renderPanel()
    const name = await screen.findByText('Alice Admin')
    const entryRow = name.closest('[tabIndex="0"]')
    expect(entryRow).not.toBeNull()
    fireEvent.click(entryRow!)
    const checkbox = entryRow!.querySelector<HTMLInputElement>('input[type="checkbox"]')
    expect(checkbox).not.toBeNull()
    // Toggle off then on to create a draft without changing the action set.
    fireEvent.click(checkbox!)
    fireEvent.click(checkbox!)
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody!.entries).toHaveLength(1)
    expect([...putBody!.entries[0].actions].sort()).toEqual(
      ['create', 'delete', 'edit', 'run', 'share', 'view']
    )
  })
})

// ── Session notebook inheritance toggle ─────────────────────────────────────

describe('Session notebook inheritance', () => {
  test('renders the owner toggle and calls onToggle with the next state', async () => {
    const onToggle = vi.fn().mockResolvedValue(undefined)
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([]))
    )
    renderPanel({
      resourceType: 'agent_session',
      resourceId: 's-1',
      resourceName: 'Revenue analysis',
      sessionNotebookInheritance: { enabled: false, hasNotebook: true, onToggle },
    })

    const checkbox = await screen.findByRole('checkbox', { name: /Anyone who can view this notebook/i })
    expect(checkbox).not.toBeChecked()
    fireEvent.click(checkbox)
    await waitFor(() => expect(onToggle).toHaveBeenCalledWith(true))
  })

  test('shows the checked state from the props', async () => {
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([]))
    )
    renderPanel({
      resourceType: 'agent_session',
      resourceId: 's-1',
      resourceName: 'Revenue analysis',
      sessionNotebookInheritance: { enabled: true, hasNotebook: true, onToggle: vi.fn().mockResolvedValue(undefined) },
    })

    const checkbox = await screen.findByRole('checkbox', { name: /Anyone who can view this notebook/i })
    expect(checkbox).toBeChecked()
  })

  test('surfaces a failed toggle inline without closing the panel', async () => {
    const onToggle = vi.fn().mockRejectedValue(new Error('toggle failed'))
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([]))
    )
    renderPanel({
      resourceType: 'agent_session',
      resourceId: 's-1',
      resourceName: 'Revenue analysis',
      sessionNotebookInheritance: { enabled: false, hasNotebook: true, onToggle },
    })

    const checkbox = await screen.findByRole('checkbox', { name: /Anyone who can view this notebook/i })
    fireEvent.click(checkbox)

    expect(await screen.findByText('toggle failed')).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  test('hides the toggle when the session has no notebook', async () => {
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([]))
    )
    renderPanel({
      resourceType: 'agent_session',
      resourceId: 's-1',
      resourceName: 'Revenue analysis',
      sessionNotebookInheritance: { enabled: false, hasNotebook: false, onToggle: vi.fn() },
    })

    await waitForAclLoaded()
    expect(screen.queryByText(/Anyone who can view this notebook/i)).toBeNull()
  })
})
