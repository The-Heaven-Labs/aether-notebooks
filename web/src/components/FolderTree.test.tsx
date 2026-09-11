import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { renderWithProviders } from '../test/utils'
import { server } from '../test/server'
import { FolderTree } from './FolderTree'

beforeEach(() => {
  localStorage.clear()
})

async function openTreeMenu() {
  renderWithProviders(
    <FolderTree
      onSelectFolder={vi.fn()}
      selectedFolderId={null}
      onMoveFolder={vi.fn()}
      onPermissionsFolder={vi.fn()}
      onRenameFolder={vi.fn()}
      onDeleteFolder={vi.fn()}
    />,
  )
  await screen.findByText('Engineering')
  // ⋯ appears on hover
  fireEvent.mouseOver(screen.getByText('Engineering'))
  fireEvent.click(screen.getByTitle('More options'))
}

describe('FolderTree menu', () => {
  it('renders all four shared actions and fires callbacks', async () => {
    const onRename = vi.fn()
    const onDelete = vi.fn()
    const onMove = vi.fn()
    const onPermissions = vi.fn()
    renderWithProviders(
      <FolderTree
        onSelectFolder={vi.fn()}
        selectedFolderId={null}
        onMoveFolder={onMove}
        onPermissionsFolder={onPermissions}
        onRenameFolder={onRename}
        onDeleteFolder={onDelete}
      />,
    )
    await screen.findByText('Engineering')
    fireEvent.mouseOver(screen.getByText('Engineering'))
    fireEvent.click(screen.getByTitle('More options'))
    for (const label of ['Rename', 'Move to…', 'Permissions', 'Delete']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
    fireEvent.click(screen.getByText('Rename'))
    expect(onRename).toHaveBeenCalledWith(expect.objectContaining({ id: 'f-eng' }))
    expect(onMove).not.toHaveBeenCalled()
  })

  it('delete fires with the folder', async () => {
    const onDelete = vi.fn()
    renderWithProviders(
      <FolderTree
        onSelectFolder={vi.fn()}
        selectedFolderId={null}
        onRenameFolder={vi.fn()}
        onDeleteFolder={onDelete}
      />,
    )
    await screen.findByText('Engineering')
    fireEvent.mouseOver(screen.getByText('Engineering'))
    fireEvent.click(screen.getByTitle('More options'))
    // Only rename/delete handlers provided → only those render
    expect(screen.queryByText('Move to…')).toBeNull()
    fireEvent.click(screen.getByText('Delete'))
    expect(onDelete).toHaveBeenCalledWith(expect.objectContaining({ id: 'f-eng' }))
  })

  it('disables guarded actions without permission', async () => {
    server.use(
      http.get('/api/v1/folders', () =>
        HttpResponse.json({
          folders: [
            {
              id: 'f-ro', org_id: 'org-1', name: 'ReadOnly', is_home: false,
              created_by: 'user-1', can_edit: false, can_delete: false, can_share: false,
              created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
            },
          ],
          notebooks: [], connectors: [], dashboards: [],
        }),
      ),
    )
    const onRename = vi.fn()
    renderWithProviders(
      <FolderTree
        onSelectFolder={vi.fn()}
        selectedFolderId={null}
        onRenameFolder={onRename}
        onDeleteFolder={vi.fn()}
      />,
    )
    await screen.findByText('ReadOnly')
    fireEvent.mouseOver(screen.getByText('ReadOnly'))
    fireEvent.click(screen.getByTitle('More options'))
    const renameBtn = screen.getByText('Rename').closest('button')!
    expect(renameBtn.disabled).toBe(true)
    fireEvent.click(screen.getByText('Delete'))
    await waitFor(() => expect(onRename).not.toHaveBeenCalled())
  })

  it('shares the action order with the main view', async () => {
    await openTreeMenu()
    const menu = document.querySelector('[data-folder-menu]')!
    const labels = Array.from(menu.querySelectorAll('button')).map((b) => b.textContent)
    expect(labels).toEqual(['Rename', 'Move to…', 'Permissions', 'Delete'])
  })
})
