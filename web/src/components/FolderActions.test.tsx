import { describe, it, expect, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useFolderActions, useFolderMutations } from './FolderActions'

function hookWrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      {children}
    </QueryClientProvider>
  )
}

describe('useFolderActions', () => {
  it('orders actions Rename, Move to…, Permissions, Delete', () => {
    const { result } = renderHook(() => useFolderActions({}), { wrapper: hookWrapper })
    expect(result.current.actions.map((a) => a.label)).toEqual(['Rename', 'Move to…', 'Permissions', 'Delete'])
    expect(result.current.actions.find((a) => a.key === 'delete')?.danger).toBe(true)
  })

  it('enables everything without flags (legacy sidebar behavior)', () => {
    const { result } = renderHook(() => useFolderActions({}), { wrapper: hookWrapper })
    expect(result.current).toMatchObject({ canRename: true, canDelete: true, canMove: true, canPermissions: true })
    expect(result.current.actions.every((a) => !a.disabled)).toBe(true)
  })

  it('disables rename/move without edit, delete without delete, permissions without share', () => {
    const { result } = renderHook(
      () => useFolderActions({ can_edit: false, can_delete: false, can_share: false }),
      { wrapper: hookWrapper },
    )
    expect(result.current).toMatchObject({ canRename: false, canDelete: false, canMove: false, canPermissions: false })
    expect(result.current.actions.every((a) => a.disabled)).toBe(true)
  })

  it('supports mixed guards (editor: rename+move only)', () => {
    const { result } = renderHook(
      () => useFolderActions({ can_edit: true, can_delete: false, can_share: false }),
      { wrapper: hookWrapper },
    )
    const byKey = Object.fromEntries(result.current.actions.map((a) => [a.key, a.disabled]))
    expect(byKey).toMatchObject({ rename: false, move: false, permissions: true, delete: true })
  })
})

describe('useFolderMutations', () => {
  it('renames via PUT and fires onRenamed', async () => {
    const onRenamed = vi.fn()
    const onError = vi.fn()
    const { result } = renderHook(() => useFolderMutations({ onError, onRenamed }), { wrapper: hookWrapper })
    await act(async () => {
      await result.current.rename.mutateAsync({ id: 'f-eng', name: 'Eng2' })
    })
    expect(onRenamed).toHaveBeenCalled()
    expect(onError).not.toHaveBeenCalled()
  })

  it('deletes via DELETE', async () => {
    const onError = vi.fn()
    const { result } = renderHook(() => useFolderMutations({ onError }), { wrapper: hookWrapper })
    await act(async () => {
      await result.current.remove.mutateAsync('f-eng')
    })
    expect(onError).not.toHaveBeenCalled()
  })
})
