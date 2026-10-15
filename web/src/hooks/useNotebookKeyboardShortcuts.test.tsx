import { describe, test, expect, vi } from 'vitest'
import { fireEvent, renderHook } from '@testing-library/react'
import { useNotebookKeyboardShortcuts, type ShortcutActions } from './useNotebookKeyboardShortcuts'

function makeActions(): ShortcutActions {
  return {
    runFocusedCell: vi.fn(),
    runFocusedCellAndAdvance: vi.fn(),
    addCellBelow: vi.fn(),
    addCellAbove: vi.fn(),
    deleteFocusedCell: vi.fn(),
    moveFocusDown: vi.fn(),
    moveFocusUp: vi.fn(),
    moveCellUp: vi.fn(),
    moveCellDown: vi.fn(),
    convertToMarkdown: vi.fn(),
    convertToCode: vi.fn(),
    enterEditMode: vi.fn(),
    exitEditMode: vi.fn(),
    duplicateCell: vi.fn(),
    toggleSlideBreak: vi.fn(),
  }
}

function renderShortcuts() {
  const actions = makeActions()
  renderHook(() => useNotebookKeyboardShortcuts(actions, false))
  return actions
}

function press(key: string, init: KeyboardEventInit = {}) {
  fireEvent.keyDown(window, { key, ...init })
}

describe('useNotebookKeyboardShortcuts', () => {
  test('bare j/k and arrows move cell focus', () => {
    const a = renderShortcuts()

    press('j')
    press('k')
    press('ArrowDown')
    press('ArrowUp')

    expect(a.moveFocusDown).toHaveBeenCalledTimes(2)
    expect(a.moveFocusUp).toHaveBeenCalledTimes(2)
  })

  test('Ctrl/Cmd+K and Ctrl/Cmd+J never move cell focus', () => {
    const a = renderShortcuts()

    press('k', { ctrlKey: true })
    press('k', { metaKey: true })
    press('j', { ctrlKey: true })
    press('j', { metaKey: true })

    expect(a.moveFocusUp).not.toHaveBeenCalled()
    expect(a.moveFocusDown).not.toHaveBeenCalled()
    // The chords also must not trip any other letter action.
    expect(a.addCellBelow).not.toHaveBeenCalled()
    expect(a.convertToCode).not.toHaveBeenCalled()
  })

  test('Alt + j/k does not move cell focus (Option-letters type glyphs)', () => {
    const a = renderShortcuts()

    press('j', { altKey: true })
    press('k', { altKey: true })

    expect(a.moveFocusDown).not.toHaveBeenCalled()
    expect(a.moveFocusUp).not.toHaveBeenCalled()
  })

  test('Ctrl+Arrow still moves the cell, not the focus', () => {
    const a = renderShortcuts()

    press('ArrowDown', { ctrlKey: true })
    press('ArrowUp', { metaKey: true })

    expect(a.moveCellDown).toHaveBeenCalledTimes(1)
    expect(a.moveCellUp).toHaveBeenCalledTimes(1)
    expect(a.moveFocusDown).not.toHaveBeenCalled()
    expect(a.moveFocusUp).not.toHaveBeenCalled()
  })

  test('Shift+Arrow keeps its plain focus behavior', () => {
    const a = renderShortcuts()

    press('ArrowDown', { shiftKey: true })
    press('ArrowUp', { shiftKey: true })

    expect(a.moveFocusDown).toHaveBeenCalledTimes(1)
    expect(a.moveFocusUp).toHaveBeenCalledTimes(1)
  })
})
