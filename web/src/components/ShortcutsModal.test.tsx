import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ShortcutsModal } from './ShortcutsModal'

describe('ShortcutsModal', () => {
  it('lists the search and agent chords with the shared Kbd chip', () => {
    render(<ShortcutsModal onClose={vi.fn()} />)

    const searchKey = screen.getByText('Ctrl/Cmd+K')
    const agentKey = screen.getByText('Ctrl/Cmd+J')
    expect(searchKey.closest('tr')).toHaveTextContent('Search')
    expect(agentKey.closest('tr')).toHaveTextContent('Open AI agent')

    // The shared Kbd chip, not the old per-modal inline <kbd> styles.
    expect(searchKey.tagName).toBe('KBD')
    expect(agentKey.tagName).toBe('KBD')

    // The old Ctrl+K = agent binding is gone.
    expect(screen.queryByText('Ctrl+K')).toBeNull()
  })

  it('still renders caller-supplied extra shortcuts alongside the globals', () => {
    render(
      <ShortcutsModal
        onClose={vi.fn()}
        extraShortcuts={[{ key: 'G G', action: 'Jump to top' }]}
      />,
    )
    expect(screen.getByText('G G').tagName).toBe('KBD')
    expect(screen.getByText('Jump to top')).toBeInTheDocument()
    expect(screen.getByText('Ctrl/Cmd+K')).toBeInTheDocument()
  })
})
