import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'

export type FolderActionKey = 'rename' | 'move' | 'permissions' | 'delete'

export interface FolderAction {
  key: FolderActionKey
  label: string
  disabled?: boolean
  danger?: boolean
}

export interface FolderPerms {
  can_edit?: boolean
  can_delete?: boolean
  can_share?: boolean
}

// Canonical folder action list shared by the main view context menu and the
// folder-tree sidebar menu (previously two hand-rolled, diverging menus).
// Ordered: Rename, Move to…, Permissions, Delete.
//
// Guards come from the folder's own permission flags. Flags absent (e.g. home
// folders, which the tree API returns without them) default to permissive to
// preserve the sidebar's historical behavior — the backend still enforces on
// mutation and surfaces 403s through onError.
export function useFolderActions(folder: FolderPerms): {
  canRename: boolean
  canDelete: boolean
  canMove: boolean
  canPermissions: boolean
  actions: FolderAction[]
} {
  const canRename = folder.can_edit ?? true
  const canDelete = folder.can_delete ?? true
  // Move issues PUT /folders (edit permission); Permissions needs share scope.
  const canMove = folder.can_edit ?? true
  const canPermissions = folder.can_share ?? true
  return {
    canRename,
    canDelete,
    canMove,
    canPermissions,
    actions: [
      { key: 'rename', label: 'Rename', disabled: !canRename },
      { key: 'move', label: 'Move to…', disabled: !canMove },
      { key: 'permissions', label: 'Permissions', disabled: !canPermissions },
      { key: 'delete', label: 'Delete', disabled: !canDelete, danger: true },
    ],
  }
}

export function useFolderMutations(opts?: { onError?: (e: Error) => void; onRenamed?: () => void }) {
  const qc = useQueryClient()
  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ['folder-contents'] })
    qc.invalidateQueries({ queryKey: ['folder-tree-root'] })
    qc.invalidateQueries({ queryKey: ['folder-home'] })
  }
  const rename = useMutation({
    mutationFn: ({ id, name }: { id: string; name: string }) => api.put(`/api/v1/folders/${id}`, { name }),
    onSuccess: () => {
      invalidate()
      opts?.onRenamed?.()
    },
    onError: (e: Error) => opts?.onError?.(e),
  })
  const remove = useMutation({
    mutationFn: (id: string) => api.delete(`/api/v1/folders/${id}?force=true`),
    onSuccess: invalidate,
    onError: (e: Error) => opts?.onError?.(e),
  })
  return { rename, remove }
}
