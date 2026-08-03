import { render } from '@testing-library/react'
import { ReadOnlyCode } from '../components/ReadOnlyCode'
import { test, expect } from 'vitest'

test('renders read-only SQL source', () => {
  const { container } = render(<ReadOnlyCode source="SELECT 1 FROM foo" language="sql" />)
  const editor = container.querySelector('.cm-editor')
  expect(editor).not.toBeNull()
  expect(editor!.textContent).toContain('SELECT 1 FROM foo')
})

test('re-renders when source changes', () => {
  const { container, rerender } = render(<ReadOnlyCode source="SELECT 1" language="sql" />)
  rerender(<ReadOnlyCode source="SELECT 2" language="sql" />)
  expect(container.querySelector('.cm-editor')!.textContent).toContain('SELECT 2')
})
