import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { SessionHistory } from '../components/SessionHistory'
import { server } from './server'

const OWN_SESSION = {
  id: 's1',
  agent_id: 'a1',
  notebook_id: 'nb1',
  user_id: 'user-1',
  max_turns: 10,
  created_at: '2026-09-13T00:00:00Z',
  first_message: 'revenue question',
  message_count: 2,
  title: null,
  owner_email: 'alice@test.com',
  shared: false,
  can_edit: true,
  share_with_notebook_viewers: false,
}

const SHARED_SESSION = {
  id: 's2',
  agent_id: 'a1',
  notebook_id: 'nb1',
  user_id: 'user-2',
  max_turns: 10,
  created_at: '2026-09-14T00:00:00Z',
  first_message: 'margin question',
  message_count: 3,
  title: 'Margin review',
  owner_email: 'bob@test.com',
  shared: true,
  can_edit: false,
  share_with_notebook_viewers: true,
}

class MockWebSocket {
  static OPEN = 1
  static CLOSED = 3
  url: string
  readyState = MockWebSocket.OPEN
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  sent: string[] = []
  constructor(url: string) {
    this.url = url
  }
  send(data: string) {
    this.sent.push(data)
  }
  close() {
    this.readyState = MockWebSocket.CLOSED
  }
}

const realWebSocket = globalThis.WebSocket

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('aether_token', 'tok-123')
  vi.stubGlobal('WebSocket', MockWebSocket)
})

afterEach(() => {
  vi.stubGlobal('WebSocket', realWebSocket)
})

function mockLists(own: object[], shared: object[]) {
  server.use(
    http.get('/api/v1/agents/:agentId/sessions', () => HttpResponse.json(own)),
    http.get('/api/v1/sessions/shared', () => HttpResponse.json(shared)),
  )
}

describe('SessionHistory compaction rows', () => {
  it('renders a summary block instead of a tool bubble', async () => {
    mockLists([OWN_SESSION], [])
    server.use(
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

describe('SessionHistory sections', () => {
  it('splits own and shared sessions, showing owner and a Shared badge on shared rows', async () => {
    mockLists(
      [OWN_SESSION],
      [SHARED_SESSION, { ...SHARED_SESSION, id: 's3', agent_id: 'a-other', title: 'Other agent chat' }],
    )

    render(<SessionHistory agentId="a1" onBack={() => {}} onResumeSession={() => {}} />)

    expect(await screen.findByText('My sessions')).toBeInTheDocument()
    expect(screen.getByText('Shared with me')).toBeInTheDocument()
    expect(screen.getByText('revenue question')).toBeInTheDocument()
    expect(screen.getByText('Margin review')).toBeInTheDocument()
    expect(screen.getByText('bob@test.com')).toBeInTheDocument()
    expect(screen.getAllByText('Shared')).toHaveLength(1)
    expect(screen.queryByText('Other agent chat')).toBeNull()
    expect(screen.queryByText('alice@test.com')).toBeNull()
  })

  it('resumes an own row through the existing detail view', async () => {
    mockLists([OWN_SESSION], [])
    server.use(
      http.get('/api/v1/sessions/:sessionId/messages', () =>
        HttpResponse.json([
          { id: 'm1', role: 'user', content: 'revenue question', created_at: '2026-09-13T00:00:01Z' },
        ]),
      ),
    )
    const onResumeSession = vi.fn()

    render(<SessionHistory agentId="a1" onBack={() => {}} onResumeSession={onResumeSession} />)
    await userEvent.click(await screen.findByRole('button', { name: /revenue question/ }))
    await userEvent.click(await screen.findByRole('button', { name: /Resume/ }))

    expect(onResumeSession).toHaveBeenCalledTimes(1)
    expect(onResumeSession).toHaveBeenCalledWith(OWN_SESSION)
  })

  it('opens a shared row in the read-only viewer', async () => {
    mockLists([OWN_SESSION], [SHARED_SESSION])
    server.use(
      http.get('/api/v1/sessions/s2', () => HttpResponse.json(SHARED_SESSION)),
      http.get('/api/v1/sessions/s2/messages', () =>
        HttpResponse.json([
          { id: 'm1', session_id: 's2', role: 'user', content: 'margin question', created_at: '2026-09-14T00:00:01Z' },
        ]),
      ),
    )

    render(<SessionHistory agentId="a1" onBack={() => {}} onResumeSession={() => {}} />)
    await userEvent.click(await screen.findByRole('button', { name: /Margin review/ }))

    expect(await screen.findByRole('dialog', { name: 'Shared agent session' })).toBeInTheDocument()
    expect(await screen.findByText('bob@test.com')).toBeInTheDocument()
    expect(await screen.findByText('margin question')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: /Close viewer/ }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Shared agent session' })).toBeNull())
    expect(screen.getByText('Shared with me')).toBeInTheDocument()
  })
})
