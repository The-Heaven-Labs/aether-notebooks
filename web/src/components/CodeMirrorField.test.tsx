import { describe, test, expect, vi } from 'vitest'
import { render, waitFor, act } from '@testing-library/react'
import { EditorView } from '@codemirror/view'
import { CodeMirrorField } from './CodeMirrorField'
import type { CodeMirrorFieldProps } from './CodeMirrorField'

const editorEl = () => document.querySelector('.cm-editor') as HTMLElement | null
const view = () => EditorView.findFromDOM(editorEl()!)!

async function renderField(props: CodeMirrorFieldProps) {
  const result = render(<CodeMirrorField {...props} />)
  // The language grammar lazy-imports; wait for the editor to mount.
  await waitFor(() => expect(editorEl()).toBeTruthy())
  return result
}

describe('CodeMirrorField', () => {
  test('renders the given value in a CodeMirror editor', async () => {
    await renderField({ label: 'Component HTML', language: 'html', value: '<div>x</div>', onSave: () => {} })
    expect(view().state.doc.toString()).toBe('<div>x</div>')
  })

  test('debounces typing and calls onSave with the draft', async () => {
    const onSave = vi.fn()
    await renderField({ label: 'Component JS', language: 'javascript', value: '', onSave })
    act(() => {
      view().dispatch({ changes: { from: 0, insert: 'aether.requestData(1)' } })
    })
    expect(onSave).not.toHaveBeenCalled()
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1), { timeout: 2000 })
    expect(onSave).toHaveBeenCalledWith('aether.requestData(1)')
  })

  test('flushes a pending edit on unmount', async () => {
    const onSave = vi.fn()
    const { unmount } = await renderField({ label: 'Component CSS', language: 'css', value: '', onSave })
    act(() => {
      view().dispatch({ changes: { from: 0, insert: '.x{color:red}' } })
    })
    unmount()
    expect(onSave).toHaveBeenCalledWith('.x{color:red}')
  })
})
