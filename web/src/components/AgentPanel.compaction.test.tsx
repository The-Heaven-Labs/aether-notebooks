import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CompactionDivider } from './AgentPanel'

describe('CompactionDivider', () => {
  it('renders a labeled marker with compact before → after counts', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'summary', tokens_before: 1200, tokens_after: 400 }}
        fmtTime={() => ''}
      />,
    )
    expect(screen.getByText(/Context auto-compacted/)).toBeInTheDocument()
    expect(screen.getByText(/1\.2k → ~400/)).toBeInTheDocument()
  })

  it('renders the label alone when counts are missing (legacy rows)', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'summary', tokens_before: 1200 }}
        fmtTime={() => ''}
      />,
    )
    expect(screen.getByText(/Context auto-compacted/)).toBeInTheDocument()
    expect(screen.queryByText(/→/)).toBeNull()
  })

  it('expands to the summary, exact counts, and timestamp', () => {
    render(
      <CompactionDivider
        msg={{
          role: 'compaction',
          content: 'earlier context',
          tokens_before: 1200,
          tokens_after: 400,
          created_at: '2026-09-14T00:01:00Z',
        }}
        fmtTime={(iso) => `t:${iso}`}
      />,
    )
    fireEvent.click(screen.getByRole('button', { expanded: false }))
    expect(screen.getByText('earlier context')).toBeInTheDocument()
    expect(screen.getByText(/1,200 tokens \(actual\) → ~400 tokens \(estimated\)/)).toBeInTheDocument()
    expect(screen.getByText('t:2026-09-14T00:01:00Z')).toBeInTheDocument()
  })

  it('is toggleable and keyboard-operable through a native button', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'summary', tokens_before: 1200, tokens_after: 400 }}
        fmtTime={() => ''}
      />,
    )
    const pill = screen.getByRole('button', { expanded: false })
    expect(pill.tagName).toBe('BUTTON')
    fireEvent.click(pill)
    expect(screen.getByRole('button', { expanded: true })).toBeInTheDocument()
  })
})
