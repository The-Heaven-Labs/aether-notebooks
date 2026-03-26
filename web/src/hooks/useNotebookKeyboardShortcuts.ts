import { useEffect, useRef } from 'react'

export interface ShortcutActions {
  runFocusedCell: () => void
  addCellBelow: () => void
  addCellAbove: () => void
  deleteFocusedCell: () => void
  moveFocusDown: () => void
  moveFocusUp: () => void
  convertToMarkdown: () => void
  convertToCode: () => void
  openShortcutsModal: () => void
}

export function useNotebookKeyboardShortcuts(
  actions: ShortcutActions,
  isEditingCell: boolean
) {
  const lastDRef = useRef<number>(0)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Never fire when user is typing in an input, textarea, or contenteditable
      const tag = (e.target as HTMLElement).tagName
      if (isEditingCell || tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return

      if (e.shiftKey && e.key === 'Enter') { e.preventDefault(); actions.runFocusedCell(); return }
      if (e.key === 'b' || e.key === 'B') { actions.addCellBelow(); return }
      if (e.key === 'a' || e.key === 'A') { actions.addCellAbove(); return }
      if (e.key === 'd' || e.key === 'D') {
        const now = Date.now()
        if (now - lastDRef.current < 500) { actions.deleteFocusedCell(); lastDRef.current = 0 }
        else { lastDRef.current = now }
        return
      }
      if (e.key === 'j' || e.key === 'ArrowDown') { actions.moveFocusDown(); return }
      if (e.key === 'k' || e.key === 'ArrowUp') { actions.moveFocusUp(); return }
      if (e.key === 'm' || e.key === 'M') { actions.convertToMarkdown(); return }
      if (e.key === 'y' || e.key === 'Y') { actions.convertToCode(); return }
      if (e.key === '?') { actions.openShortcutsModal(); return }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [actions, isEditingCell])
}
