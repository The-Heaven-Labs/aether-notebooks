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
})
