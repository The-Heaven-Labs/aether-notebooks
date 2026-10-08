import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor, within } from '@testing-library/react'
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

// Wait for the ACL query to settle: the Direct access section only renders
// after the loading skeleton.
async function waitForAclLoaded() {
  await screen.findByText('Direct access')
}

/** The add-access composer, scoped so chip queries don't match entry rows. */
async function openComposer() {
  fireEvent.click(await screen.findByRole('button', { name: /add people or groups/i }))
  return await screen.findByRole('group', { name: 'Add access' })
}

/** Picks a subject from the open inline picker. */
async function pickSubject(name: string | RegExp) {
  const option = await screen.findByRole('option', { name })
  fireEvent.mouseDown(option)
}

/** The entry row (direct or inherited) carrying the given subject name. */
function findEntryRow(name: string, inherited = false): HTMLElement | null {
  for (const el of screen.queryAllByText(name)) {
    const row = el.closest('.access-row') as HTMLElement | null
    if (!row) continue
    const isInherited = row.classList.contains('access-row-inherited')
    if (isInherited === inherited) return row
  }
  return null
}

// ── Panel renders ───────────────────────────────────────────────────────────

describe('Panel renders', () => {
  test('shows resource name in header', async () => {
    renderPanel()
    expect(await screen.findByText('Q1 Report')).toBeInTheDocument()
  })

  test('shows resource type badge', async () => {
    renderPanel()
    await screen.findByText('Q1 Report')
    expect(screen.getByText('Notebook')).toBeInTheDocument()
  })

  test('shows existing ACL entries with their granted capabilities', async () => {
    renderPanel()
    await waitForAclLoaded()
    const row = findEntryRow('Alice Admin')
    expect(row).not.toBeNull()
    // Capability chips are visible without any expand interaction.
    expect(within(row!).getByRole('button', { name: 'view' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(row!).getByRole('button', { name: 'edit' })).toHaveAttribute('aria-pressed', 'true')
  })

  test('shows the empty state when nothing is granted', async () => {
    server.use(http.get('/api/v1/acl/notebook/nb-1', () => HttpResponse.json([])))
    renderPanel()
    expect(await screen.findByText('No direct access yet')).toBeInTheDocument()
  })

  test('does not render an inherited section without a parent folder', async () => {
    renderPanel()
    await waitForAclLoaded()
    expect(screen.queryByText(/Inherited from/i)).toBeNull()
  })

  test('renders inherited entries as read-only chips', async () => {
    server.use(
      http.get('/api/v1/acl/folder/f-eng', () => HttpResponse.json(ACL_ENTRIES))
    )
    renderPanel({ parentFolderId: 'f-eng' })
    await waitForAclLoaded()
    expect(await screen.findByText(/Inherited from Engineering/i)).toBeInTheDocument()
    const row = findEntryRow('Alice Admin', true)
    expect(row).not.toBeNull()
    expect(within(row!).getByRole('button', { name: 'view' })).toBeDisabled()
    expect(within(row!).queryByTitle('Remove')).toBeNull()
  })

  test('reports the inherited state when the parent folder has no entries', async () => {
    server.use(http.get('/api/v1/acl/folder/f-eng', () => HttpResponse.json([])))
    renderPanel({ parentFolderId: 'f-eng' })
    expect(await screen.findByText(/Nothing is inherited from this folder/i)).toBeInTheDocument()
  })

  test('read-only callers cannot open the composer or toggle chips', async () => {
    renderPanel({ canEdit: false })
    await waitForAclLoaded()
    expect(screen.queryByRole('button', { name: /add people or groups/i })).toBeNull()
    const row = findEntryRow('Alice Admin')
    expect(row).not.toBeNull()
    expect(within(row!).getByRole('button', { name: 'view' })).toBeDisabled()
    expect(screen.getByText(/don't have permission to change access/i)).toBeInTheDocument()
  })
})

// ── Close ───────────────────────────────────────────────────────────────────

describe('Close', () => {
  test('close button calls onClose', async () => {
    const onClose = vi.fn()
    renderPanel({ onClose })
    await screen.findByText('Q1 Report')
    const closeBtn = screen.getByTitle('Close')
    fireEvent.click(closeBtn)
    expect(onClose).toHaveBeenCalled()
  })

  test('Escape closes the panel when there is nothing to lose', async () => {
    const onClose = vi.fn()
    renderPanel({ onClose })
    await waitForAclLoaded()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
  })

  test('closing with unsaved changes asks before discarding', async () => {
    const onClose = vi.fn()
    renderPanel({ onClose })
    await waitForAclLoaded()
    const row = findEntryRow('Alice Admin')!
    fireEvent.click(within(row).getByRole('button', { name: 'edit' }))
    await screen.findByRole('button', { name: /^Save$/i })

    fireEvent.click(screen.getByTitle('Close'))
    expect(onClose).not.toHaveBeenCalled()
    expect(await screen.findByText('Discard changes?')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }))
    await waitFor(() => expect(screen.queryByText('Discard changes?')).toBeNull())
    expect(onClose).not.toHaveBeenCalled()

    fireEvent.click(screen.getByTitle('Close'))
    fireEvent.click(await screen.findByRole('button', { name: 'Discard changes' }))
    expect(onClose).toHaveBeenCalled()
  })
})

// ── Add entry ───────────────────────────────────────────────────────────────

describe('Add entry', () => {
  test('adding a subject creates a draft row and defaults to view', async () => {
    renderPanel()
    await waitForAclLoaded()
    await openComposer()
    await pickSubject(/Bob Editor/i)

    // Least-privilege default: view preselected, so Add is immediately usable.
    const addBtn = screen.getByRole('button', { name: /^Add$/i })
    expect(addBtn).toBeEnabled()

    fireEvent.click(addBtn)
    expect(await screen.findByRole('button', { name: /^Save$/i })).toBeInTheDocument()
    const row = findEntryRow('Bob Editor')
    expect(row).not.toBeNull()
    expect(within(row!).getByRole('button', { name: 'view' })).toHaveAttribute('aria-pressed', 'true')
  })

  test('Add is disabled when every capability is unselected', async () => {
    renderPanel()
    await waitForAclLoaded()
    await openComposer()
    await pickSubject(/Bob Editor/i)

    const composer = screen.getByRole('group', { name: 'Add access' })
    fireEvent.click(within(composer).getByRole('button', { name: 'view' }))
    expect(screen.getByRole('button', { name: /^Add$/i })).toBeDisabled()
  })

  test('the picker excludes subjects that already have an entry', async () => {
    renderPanel()
    await waitForAclLoaded()
    await openComposer()
    await screen.findByRole('option', { name: /Bob Editor/i })
    expect(screen.queryByRole('option', { name: /Alice Admin/i })).toBeNull()
  })

  test('Escape inside the picker closes only the picker', async () => {
    const onClose = vi.fn()
    renderPanel({ onClose })
    await waitForAclLoaded()
    await openComposer()
    const search = screen.getByRole('combobox', { name: /search people and groups/i })
    fireEvent.keyDown(search, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('option', { name: /Bob Editor/i })).toBeNull())
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  test('saving includes the newly added entry in the PUT payload', async () => {
    let putBody: { entries: Array<{ subject_type: string; subject_id: string; actions: string[] }> } | null = null
    server.use(
      http.put('/api/v1/acl/notebook/nb-1', async ({ request }) => {
        putBody = await request.json() as typeof putBody
        return HttpResponse.json([])
      })
    )
    renderPanel()
    await waitForAclLoaded()
    await openComposer()
    await pickSubject(/Bob Editor/i)
    const composer = screen.getByRole('group', { name: 'Add access' })
    fireEvent.click(within(composer).getByRole('button', { name: 'run' }))
    fireEvent.click(screen.getByRole('button', { name: /^Add$/i }))
    await screen.findByRole('button', { name: /^Save$/i })
    fireEvent.click(screen.getByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    const bob = putBody!.entries.find((e) => e.subject_id === 'user-2')
    expect(bob).toMatchObject({ subject_type: 'user' })
    expect([...bob!.actions].sort()).toEqual(['run', 'view'])
  })
})

// ── Draft mode ──────────────────────────────────────────────────────────────

describe('Draft mode', () => {
  test('Save and Discard buttons not shown when no changes', async () => {
    renderPanel()
    await waitForAclLoaded()
    expect(screen.queryByRole('button', { name: /save/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /discard/i })).toBeNull()
  })

  test('toggling a capability shows Save + Discard buttons', async () => {
    renderPanel()
    await waitForAclLoaded()
    const row = findEntryRow('Alice Admin')!
    fireEvent.click(within(row).getByRole('button', { name: 'edit' }))
    expect(await screen.findByRole('button', { name: /^Save$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /discard/i })).toBeInTheDocument()
  })

  test('Discard resets changes', async () => {
    renderPanel()
    await waitForAclLoaded()
    const row = findEntryRow('Alice Admin')!
    const editChip = within(row).getByRole('button', { name: 'edit' })
    fireEvent.click(editChip)
    await screen.findByRole('button', { name: /discard/i })
    expect(editChip).toHaveAttribute('aria-pressed', 'false')
    fireEvent.click(screen.getByRole('button', { name: /discard/i }))
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /^Save$/i })).toBeNull()
    )
    expect(findEntryRow('Alice Admin')!).toBeInTheDocument()
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
    await waitForAclLoaded()
    const row = findEntryRow('Alice Admin')!
    fireEvent.click(within(row).getByRole('button', { name: 'edit' }))
    await screen.findByRole('button', { name: /^Save$/i })
    fireEvent.click(screen.getByRole('button', { name: /^Save$/i }))
    await waitFor(() => expect(putBody).toBeDefined())
  })
})

// ── Remove entry ────────────────────────────────────────────────────────────

describe('Remove entry', () => {
  test('removing an entry stages the removal in the draft', async () => {
    renderPanel()
    await waitForAclLoaded()
    fireEvent.click(screen.getByTitle('Remove'))
    expect(await screen.findByRole('button', { name: /^Save$/i })).toBeInTheDocument()
    expect(screen.queryByText('Alice Admin')).toBeNull()
  })
})

// ── Correct actions per resource type ───────────────────────────────────────

describe('Action sets', () => {
  test('folder shows the manage capability', async () => {
    server.use(http.get('/api/v1/acl/folder/f-1', () => HttpResponse.json([])))
    renderPanel({ resourceType: 'folder', resourceId: 'f-1', resourceName: 'Docs' })
    await waitForAclLoaded()
    await openComposer()
    await pickSubject(/Bob Editor/i)
    expect(screen.getByRole('button', { name: 'manage' })).toBeInTheDocument()
  })

  test('dashboard does not offer the run capability', async () => {
    server.use(http.get('/api/v1/acl/dashboard/d-1', () => HttpResponse.json([])))
    renderPanel({ resourceType: 'dashboard', resourceId: 'd-1', resourceName: 'Sales' })
    await waitForAclLoaded()
    await openComposer()
    await pickSubject(/Bob Editor/i)
    expect(screen.queryByRole('button', { name: 'run' })).toBeNull()
  })

  test('agent_session offers only the view capability', async () => {
    server.use(http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([])))
    renderPanel({ resourceType: 'agent_session', resourceId: 's-1', resourceName: 'Revenue analysis' })
    await waitForAclLoaded()
    await openComposer()
    await pickSubject(/Bob Editor/i)
    const chips = screen.getByRole('group', { name: 'Add access' }).querySelectorAll('.access-chip')
    expect(chips).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'view' })).toBeInTheDocument()
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

    const row = await waitFor(() => {
      const r = findEntryRow('Bob Editor')
      expect(r).not.toBeNull()
      return r!
    })
    const chip = within(row).getByRole('button', { name: 'view' })
    // Toggle off then on so the draft retains exactly the view action.
    fireEvent.click(chip)
    fireEvent.click(chip)
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
    await waitForAclLoaded()
    const row = findEntryRow('Alice Admin')!
    const chip = within(row).getByRole('button', { name: 'edit' })
    // Toggle off then on to create a draft without changing the action set.
    fireEvent.click(chip)
    fireEvent.click(chip)
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
    const row = await waitFor(() => {
      const r = findEntryRow('Alice Admin')
      expect(r).not.toBeNull()
      return r!
    })
    const chip = within(row).getByRole('button', { name: 'edit' })
    fireEvent.click(chip)
    fireEvent.click(chip)
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody!.entries).toHaveLength(1)
    expect([...putBody!.entries[0].actions].sort()).toEqual(
      ['create', 'delete', 'edit', 'run', 'share', 'view']
    )
  })
})

// ── Agent session notebook link ─────────────────────────────────────────────

describe('Session notebook link', () => {
  const NOTEBOOKS = [
    { id: 'nb-1', title: 'Notebook One' },
    { id: 'nb-2', title: 'Notebook Two' },
  ]

  function renderSessionPanel(overrides?: Partial<Parameters<typeof PermissionsPanel>[0]>) {
    server.use(
      http.get('/api/v1/notebooks', () => HttpResponse.json(NOTEBOOKS)),
      http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([])),
    )
    return renderPanel({
      resourceType: 'agent_session',
      resourceId: 's-1',
      resourceName: 'Revenue analysis',
      ...overrides,
    })
  }

  test('attaching a notebook saves the selection', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    renderSessionPanel({ sessionNotebookLink: { notebookId: null, onSave } })

    const picker = await screen.findByRole('combobox', { name: 'Notebook' })
    await screen.findByRole('option', { name: 'Notebook Two' })
    fireEvent.change(picker, { target: { value: 'nb-2' } })

    await waitFor(() => expect(onSave).toHaveBeenCalledWith('nb-2'))
  })

  test('detaching a notebook saves null', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    renderSessionPanel({ sessionNotebookLink: { notebookId: 'nb-1', onSave } })

    const picker = await screen.findByRole('combobox', { name: 'Notebook' })
    await screen.findByRole('option', { name: 'Notebook One' })
    expect(picker).toHaveValue('nb-1')

    fireEvent.change(picker, { target: { value: '' } })

    await waitFor(() => expect(onSave).toHaveBeenCalledWith(null))
  })

  test('a failed save reverts the picker and shows the error', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('notebook update failed'))
    renderSessionPanel({ sessionNotebookLink: { notebookId: 'nb-1', onSave } })

    const picker = await screen.findByRole('combobox', { name: 'Notebook' })
    await screen.findByRole('option', { name: 'Notebook One' })
    fireEvent.change(picker, { target: { value: '' } })

    await waitFor(() => expect(onSave).toHaveBeenCalledWith(null))
    await waitFor(() => expect(picker).toHaveValue('nb-1'))
    expect(await screen.findByText('notebook update failed')).toBeInTheDocument()
  })

  test('the inheritance toggle appears once a notebook is linked', async () => {
    const onToggle = vi.fn()
    renderSessionPanel({
      sessionNotebookLink: { notebookId: null, onSave: vi.fn() },
      sessionNotebookInheritance: { enabled: false, hasNotebook: false, onToggle },
    })

    // No notebook → no inheritance control.
    expect(screen.queryByText('Anyone who can view this notebook')).toBeNull()

    const picker = await screen.findByRole('combobox', { name: 'Notebook' })
    await screen.findByRole('option', { name: 'Notebook One' })
    fireEvent.change(picker, { target: { value: 'nb-1' } })

    expect(await screen.findByText('Anyone who can view this notebook')).toBeInTheDocument()
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

// ── Session chat link ───────────────────────────────────────────────────────

describe('Session chat link', () => {
  function renderSessionLinkPanel() {
    server.use(
      http.get('/api/v1/acl/agent_session/s-1', () => HttpResponse.json([])),
    )
    return renderPanel({
      resourceType: 'agent_session',
      resourceId: 's-1',
      resourceName: 'Revenue analysis',
    })
  }

  test('shows the absolute chat link', async () => {
    renderSessionLinkPanel()
    const input = await screen.findByLabelText('Chat link')
    expect(input).toHaveValue(`${window.location.origin}/chats/s-1`)
  })

  test('copies the chat link to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    renderSessionLinkPanel()

    fireEvent.click(await screen.findByRole('button', { name: /copy chat link/i }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/chats/s-1`))
    expect(await screen.findByText('Copied')).toBeInTheDocument()
  })

  test('selects the input instead of claiming success when the clipboard write fails', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    Object.assign(navigator, { clipboard: { writeText } })
    renderSessionLinkPanel()

    const input = (await screen.findByLabelText('Chat link')) as HTMLInputElement
    fireEvent.click(await screen.findByRole('button', { name: /copy chat link/i }))
    await waitFor(() => expect(writeText).toHaveBeenCalled())
    expect(screen.queryByText('Copied')).toBeNull()
    expect(input.selectionStart).toBe(0)
    expect(input.selectionEnd).toBe(input.value.length)
  })
})

// ── Pending users ───────────────────────────────────────────────────────────

describe('Pending users', () => {
  test('offers a pending option for an email that matches nobody and adds it as a draft', async () => {
    renderPanel()
    await waitForAclLoaded()
    await openComposer()

    const input = screen.getByRole('combobox', { name: /search people and groups/i })
    fireEvent.change(input, { target: { value: 'Future.User@Example.com' } })

    const option = await screen.findByRole('option', { name: /future\.user@example\.com/i })
    expect(within(option).getByText('Pending — awaiting first login')).toBeInTheDocument()
    fireEvent.mouseDown(option)

    // The composer shows the pending subject and defaults to view.
    expect(await screen.findByText('future.user@example.com')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^Add$/i }))

    // The draft row renders with the pending secondary line and chips.
    const row = findEntryRow('future.user@example.com')
    expect(row).not.toBeNull()
    expect(within(row!).getByText('Pending — awaiting first login')).toBeInTheDocument()
    expect(within(row!).getByRole('button', { name: 'view' })).toHaveAttribute('aria-pressed', 'true')
  })

  test('saving a pending entry PUTs pending_user with the lowercased email', async () => {
    let putBody: { entries: Array<{ subject_type: string; subject_id: string; actions: string[] }> } | null = null
    server.use(
      http.put('/api/v1/acl/notebook/nb-1', async ({ request }) => {
        putBody = (await request.json()) as typeof putBody
        return HttpResponse.json(putBody!.entries.map((e, i) => ({ id: `e-${i}`, ...e })))
      }),
    )
    renderPanel()
    await waitForAclLoaded()
    await openComposer()

    fireEvent.change(screen.getByRole('combobox', { name: /search people and groups/i }), {
      target: { value: 'Future.User@Example.com' },
    })
    fireEvent.mouseDown(await screen.findByRole('option', { name: /future\.user@example\.com/i }))
    fireEvent.click(await screen.findByRole('button', { name: /^Add$/i }))
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    const pending = putBody!.entries.find((e) => e.subject_type === 'pending_user')
    expect(pending).toMatchObject({ subject_type: 'pending_user', subject_id: 'future.user@example.com' })
    expect(pending!.actions).toEqual(['view'])
  })

  test('renders staged pending rows returned by GET and removes them on save', async () => {
    server.use(
      http.get('/api/v1/acl/notebook/nb-1', () => HttpResponse.json([
        ...ACL_ENTRIES,
        {
          id: 'pending-1', org_id: 'org-1', resource_type: 'notebook', resource_id: 'nb-1',
          subject_type: 'pending_user', subject_id: 'future@example.com', pending: true,
          actions: ['view', 'edit'], created_at: '2026-01-01T00:00:00Z',
        },
      ])),
    )
    let putBody: unknown = null
    server.use(
      http.put('/api/v1/acl/notebook/nb-1', async ({ request }) => {
        putBody = await request.json()
        return HttpResponse.json([])
      }),
    )
    renderPanel()
    await waitForAclLoaded()

    const row = findEntryRow('future@example.com')
    expect(row).not.toBeNull()
    expect(within(row!).getByText('Pending — awaiting first login')).toBeInTheDocument()
    fireEvent.click(within(row!).getByTitle('Remove'))
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    const entries = (putBody as { entries: Array<{ subject_type: string }> }).entries
    expect(entries.some((e) => e.subject_type === 'pending_user')).toBe(false)
  })

  test('does not offer pending when the typed email already belongs to a member', async () => {
    renderPanel()
    await waitForAclLoaded()
    await openComposer()

    fireEvent.change(screen.getByRole('combobox', { name: /search people and groups/i }), {
      target: { value: 'alice@test.com' },
    })
    // Alice already has an entry, so she is not offered as an option; the
    // point is that her known email is never offered as pending either.
    expect(await screen.findByText(/No matches for/i)).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /alice admin/i })).toBeNull()
    expect(screen.queryByText(/pending — awaiting first login/i)).toBeNull()
  })
})
