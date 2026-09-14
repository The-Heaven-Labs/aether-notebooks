import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { CompactionDivider } from './AgentPanel'

describe('CompactionDivider', () => {
  it('renders the formatted before → after counts with the estimate marker', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'summary', tokens_before: 1200, tokens_after: 400 }}
        fmtTime={() => ''}
      />,
    )
    expect(screen.getByText(/1\.2k → 400 \(~\)/)).toBeInTheDocument()
  })

  it('falls back to no counts when either side is missing', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'summary', tokens_before: 1200 }}
        fmtTime={() => ''}
      />,
    )
    expect(screen.queryByText(/→/)).toBeNull()
  })

  it('expands the summary on click', () => {
    render(
      <CompactionDivider
        msg={{ role: 'compaction', content: 'earlier context', tokens_before: 1200, tokens_after: 400 }}
        fmtTime={() => ''}
      />,
    )
    fireEvent.click(screen.getByText(/Context compacted/))
    expect(screen.getByText('earlier context')).toBeInTheDocument()
  })
})
