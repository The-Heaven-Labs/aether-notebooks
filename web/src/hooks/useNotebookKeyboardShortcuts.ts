import { useEffect, useRef } from 'react'
import { isAnyDetailActive } from '../components/OutputRenderer'

export interface ShortcutActions {
  runFocusedCell: () => void
  addCellBelow: () => void
  addCellAbove: () => void
  deleteFocusedCell: () => void
  moveFocusDown: () => void
  moveFocusUp: () => void
  moveCellUp: () => void
  moveCellDown: () => void
  convertToMarkdown: () => void
  convertToCode: () => void
  enterEditMode: () => void
  exitEditMode: () => void
  duplicateCell: () => void
  toggleSlideBreak: () => void
}

export function useNotebookKeyboardShortcuts(
  actions: ShortcutActions,
  isEditingCell: boolean
) {
  const lastDRef = useRef<number>(0)
  const actionsRef = useRef(actions)
  actionsRef.current = actions
  const isEditingRef = useRef(isEditingCell)
  isEditingRef.current = isEditingCell

  // Escape must work even when editing (to exit edit mode)
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        actionsRef.current.exitEditMode()
      }
    }
    window.addEventListener('keydown', handler, true) // capture phase to fire before CodeMirror
    return () => window.removeEventListener('keydown', handler, true)
  }, [])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement).tagName
      if (isEditingRef.current || tag === 'INPUT' || tag === 'TEXTAREA' || (e.target as HTMLElement).isContentEditable) return

      const actions = actionsRef.current
      const inDetailPanel = isAnyDetailActive()

      // Enter → enter edit mode
      if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        actions.enterEditMode()
        return
      }

      // Shift+Enter → run cell
      if (e.shiftKey && e.key === 'Enter') { e.preventDefault(); actions.runFocusedCell(); return }

      // Ctrl+Up/Down → move cell up/down (skip if detail panel is open)
      if (!inDetailPanel && (e.ctrlKey || e.metaKey) && e.key === 'ArrowUp') {
        e.preventDefault()
        actions.moveCellUp()
        return
      }
      if (!inDetailPanel && (e.ctrlKey || e.metaKey) && e.key === 'ArrowDown') {
        e.preventDefault()
        actions.moveCellDown()
        return
      }

      // J/ArrowDown → move focus down (skip if detail panel is open)
      if (!inDetailPanel && (e.key === 'j' || e.key === 'ArrowDown')) {
        e.preventDefault()
        actions.moveFocusDown()
        return
      }
      // K/ArrowUp → move focus up (skip if detail panel is open)
      if (!inDetailPanel && (e.key === 'k' || e.key === 'ArrowUp')) {
        e.preventDefault()
        actions.moveFocusUp()
        return
      }

      // B → add cell below
      if (e.key === 'b' || e.key === 'B') { actions.addCellBelow(); return }
      // A → add cell above
      if (e.key === 'a' || e.key === 'A') { actions.addCellAbove(); return }

      // DD → delete cell (double tap)
      if (e.key === 'd' || e.key === 'D') {
        const now = Date.now()
        if (now - lastDRef.current < 500) { actions.deleteFocusedCell(); lastDRef.current = 0 }
        else { lastDRef.current = now }
        return
      }

      // Shift+D → duplicate cell
      if (e.shiftKey && (e.key === 'd' || e.key === 'D')) {
        e.preventDefault()
        actions.duplicateCell()
        return
      }

      // Shift+M → toggle slide break
      if (e.shiftKey && (e.key === 'm' || e.key === 'M')) {
        e.preventDefault()
        actions.toggleSlideBreak()
        return
      }

      // M → convert to markdown
      if (e.key === 'm' || e.key === 'M') { actions.convertToMarkdown(); return }
      // Y → convert to code
      if (e.key === 'y' || e.key === 'Y') { actions.convertToCode(); return }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])
}
