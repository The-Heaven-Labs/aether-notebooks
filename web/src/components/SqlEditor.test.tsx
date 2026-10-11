import { describe, test, expect, beforeEach, vi } from 'vitest'
import { render, waitFor } from '@testing-library/react'
import { SqlEditor } from './SqlEditor'
import type { DashboardCollab } from './dashboardCollabRuntime'

// The binding module resolves after a short delay so the read-only window
// before y-codemirror attaches can be observed. `attachThrows` simulates a
// failed bind (a failed dynamic import is not portable to arrange in jsdom).
const editorMock = vi.hoisted(() => ({
  attachThrows: false,
  attach: undefined as unknown as ReturnType<typeof vi.fn>,
}))

vi.mock('./dashboardCollabEditor', async () => {
  await new Promise((resolve) => setTimeout(resolve, 25))
  const attach = vi.fn(() => {
    if (editorMock.attachThrows) throw new Error('bind failed')
    return () => {}
  })
  editorMock.attach = attach
  return { attachDashboardCollabToEditor: attach }
})

const fakeCollab = { doc: {}, provider: { awareness: {} }, synced: true } as unknown as DashboardCollab
const binding = { collab: fakeCollab, widgetId: 'w1' }
const content = () => document.querySelector('.cm-content') as HTMLElement

describe('SqlEditor shared-document binding gate', () => {
  beforeEach(() => {
    editorMock.attachThrows = false
  })

  test('keeps the editor read-only until the binding settles', async () => {
    render(<SqlEditor value="SELECT 1" onChange={() => {}} collab={binding} />)

    // The binding module has not resolved yet: a keystroke here would be
    // reverted when y-codemirror adopts the shared text, so typing is blocked.
    expect(content().getAttribute('contenteditable')).toBe('false')

    await waitFor(() => expect(content().getAttribute('contenteditable')).toBe('true'))
    expect(editorMock.attach).toHaveBeenCalledTimes(1)
  })

  test('releases the editor and reports when the binding cannot be applied', async () => {
    editorMock.attachThrows = true
    const onCollabUnavailable = vi.fn()
    render(
      <SqlEditor value="SELECT 1" onChange={() => {}} collab={binding} onCollabUnavailable={onCollabUnavailable} />,
    )

    // The caller drops its doc binding; the editor must stay usable locally.
    await waitFor(() => expect(onCollabUnavailable).toHaveBeenCalledTimes(1))
    expect(content().getAttribute('contenteditable')).toBe('true')
  })

  test('is editable immediately without a shared document', () => {
    render(<SqlEditor value="SELECT 1" onChange={() => {}} />)
    expect(content().getAttribute('contenteditable')).toBe('true')
  })
})
