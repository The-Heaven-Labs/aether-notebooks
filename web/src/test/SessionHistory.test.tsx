import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { SessionHistory } from '../components/SessionHistory'
import { server } from './server'

describe('SessionHistory compaction rows', () => {
  it('renders a summary block instead of a tool bubble', async () => {
    server.use(
      http.get('/api/v1/agents/:agentId/sessions', () =>
        HttpResponse.json([
          {
            id: 's1',
            created_at: '2026-09-13T00:00:00Z',
            first_message: 'revenue question',
            message_count: 2,
            notebook_id: 'nb1',
            title: null,
          },
        ]),
      ),
      http.get('/api/v1/sessions/:sessionId/messages', () =>
        HttpResponse.json([
          {
            id: 'm1',
            role: 'compaction',
            content: 'The user asked about revenue tables and then about margins.',
            created_at: '2026-09-13T00:00:01Z',
          },
        ]),
      ),
    )

    render(<SessionHistory agentId="a1" onBack={() => {}} onResumeSession={() => {}} />)
    await userEvent.click(await screen.findByRole('button', { name: /revenue question/ }))

    const label = await screen.findByText(/Context compacted/i)
    const summary = screen.getByText(/The user asked about revenue tables/)
    // Label + summary share the summary card as a direct parent; the old
    // generic bubble had no label and rendered the summary through markdown.
    expect(label.parentElement).toBe(summary.parentElement)
    expect(label.parentElement?.textContent).toContain('The user asked about revenue tables and then about margins.')
  })
})
