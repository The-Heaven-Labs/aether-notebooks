import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
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

function renderPagePanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <AgentPanel variant="page" initialSessionId="s1" onClose={() => {}} />
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
    await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))
    const ws = MockWebSocket.instances[MockWebSocket.instances.length - 1]
    expect(ws.url).toContain('/api/v1/ws/agents/s1')
    expect(ws.url).not.toContain('s-other')
    expect(localStorage.getItem('aether:lastSessionId')).toBe('s1')
  })
})
