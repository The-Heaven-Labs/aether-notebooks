import type { ComponentProps } from 'react'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { AgentPanel } from '../components/AgentPanel'
import { server } from './server'

const AGENT = {
  id: 'a1',
  org_id: 'org-1',
  name: 'Test Agent',
  skill_ids: [],
  tool_ids: [],
  mcp_server_ids: [],
  mcp_servers: [],
  created_by: 'u1',
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
}

const SESSION = {
  id: 's1',
  agent_id: 'a1',
  notebook_id: 'nb-1',
  user_id: 'u1',
  max_turns: 10,
  title: 'Revenue chat',
  created_at: '2026-09-14T00:00:00Z',
  owner_email: 'alice@test.com',
  shared: false,
  can_edit: true,
  share_with_notebook_viewers: false,
}

const MESSAGES = [
  { id: 'm1', session_id: 's1', role: 'user', content: 'hello from owner', created_at: '2026-09-14T00:00:01Z' },
]

class MockWebSocket {
  static instances: MockWebSocket[] = []
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
    MockWebSocket.instances.push(this)
  }
  send(data: string) { this.sent.push(data) }
  close() { this.readyState = MockWebSocket.CLOSED }
}

const realWebSocket = globalThis.WebSocket

function renderPagePanel(extra: Partial<ComponentProps<typeof AgentPanel>> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <AgentPanel variant="page" initialSessionId="s1" onClose={() => {}} {...extra} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
  // A different localStorage session must NOT win over initialSessionId.
  localStorage.setItem('aether:lastAgentId', 'a1')
  localStorage.setItem('aether:lastSessionId', 's-other')
  server.use(
    http.get('/api/v1/agents', () => HttpResponse.json([AGENT])),
    http.get('/api/v1/model-configs', () => HttpResponse.json([])),
    http.get('/api/v1/agents/sessions/s1/usage', () => new HttpResponse(null, { status: 404 })),
    http.get('/api/v1/sessions/s1', () => HttpResponse.json(SESSION)),
    http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
  )
})

afterEach(() => {
  vi.stubGlobal('WebSocket', realWebSocket)
})

describe('AgentPanel page mode', () => {
  it('opens initialSessionId instead of the localStorage session', async () => {
    renderPagePanel()

    expect(await screen.findByText('hello from owner')).toBeInTheDocument()
    await waitFor(() => expect(MockWebSocket.instances.length).toBe(1))
    expect(MockWebSocket.instances[0].url).toContain('/api/v1/ws/agents/s1')
    expect(MockWebSocket.instances.some((i) => i.url.includes('s-other'))).toBe(false)
    expect(localStorage.getItem('aether:lastSessionId')).toBe('s1')
  })

  it('shows the failure when the route session cannot be opened', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => new HttpResponse(null, { status: 403 })),
    )
    renderPagePanel()

    expect(await screen.findByText('Failed to open session')).toBeInTheDocument()
  })

  it('renders full-page chrome: no resize/dock/minimize, a Back affordance', async () => {
    renderPagePanel({ onMinimize: vi.fn(), onDock: vi.fn() })
    await screen.findByText('hello from owner')

    expect(screen.queryByTitle('Minimize')).toBeNull()
    expect(screen.queryByTitle('Dock to right side')).toBeNull()
    expect(screen.queryByTitle('Undock panel')).toBeNull()
    expect(screen.getByTitle('Back')).toBeInTheDocument()
  })

  it('reports a newly started session so the page can update the URL', async () => {
    const onSessionChange = vi.fn()
    server.use(
      http.post('/api/v1/agents/:id/session', () => HttpResponse.json({ session_id: 's-new' })),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <AgentPanel variant="page" initialSessionId="s1" onClose={() => {}} onSessionChange={onSessionChange} />
      </QueryClientProvider>,
    )
    await screen.findByText('hello from owner')

    expect(onSessionChange).toHaveBeenNthCalledWith(1, 's1')

    act(() => {
      window.dispatchEvent(new CustomEvent('aether:new-agent-chat'))
    })

    await waitFor(() => expect(onSessionChange).toHaveBeenLastCalledWith('s-new'))
  })

  it('reports the forked session from a summarize slash result', async () => {
    const onSessionChange = vi.fn()
    renderPagePanel({ onSessionChange })
    await screen.findByText('hello from owner')

    const ws = MockWebSocket.instances[MockWebSocket.instances.length - 1]
    act(() => {
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'slash_result',
          command: 'summarize',
          data: { session_id: 's-fork', summary: 'sum' },
        }),
      })
    })

    await waitFor(() => expect(onSessionChange).toHaveBeenCalledWith('s-fork'))
  })

  it('does not connect or navigate after unmount while the transcript is loading', async () => {
    const onSessionChange = vi.fn()
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => { release = resolve })
    let messagesRequested = false
    server.use(
      http.get('/api/v1/sessions/s1/messages', async () => {
        messagesRequested = true
        await gate
        return HttpResponse.json(MESSAGES)
      }),
    )
    const { unmount } = renderPagePanel({ onSessionChange })
    await waitFor(() => expect(messagesRequested).toBe(true))

    unmount()
    release()
    await new Promise((resolve) => setTimeout(resolve, 25))

    expect(onSessionChange).not.toHaveBeenCalled()
    expect(MockWebSocket.instances).toHaveLength(0)
  })

  it('reconnects after the resumed session WebSocket drops', async () => {
    renderPagePanel()
    await screen.findByText('hello from owner')
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(1))

    const first = MockWebSocket.instances[0]
    act(() => { first.onclose?.() })

    // The first retry is scheduled after ~1s. Before the per-socket suppression
    // fix, the shared sentinel suppressed this reconnect entirely.
    await waitFor(() => expect(MockWebSocket.instances).toHaveLength(2), { timeout: 3000 })
    expect(MockWebSocket.instances[1].url).toContain('/api/v1/ws/agents/s1')
  })
})
