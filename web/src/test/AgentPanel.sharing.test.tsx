import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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
  send(data: string) {
    this.sent.push(data)
  }
  close() {
    this.readyState = MockWebSocket.CLOSED
  }
}

const realWebSocket = globalThis.WebSocket

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <AgentPanel notebookId="nb-1" width={360} onResize={() => {}} onClose={() => {}} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  localStorage.clear()
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
  // Boot straight into an existing session (path 2 of the restore effect).
  localStorage.setItem('aether:lastAgentId', 'a1')
  localStorage.setItem('aether:lastSessionId', 's1')
  server.use(
    http.get('/api/v1/agents', () => HttpResponse.json([AGENT])),
    http.get('/api/v1/model-configs', () => HttpResponse.json([])),
    http.get('/api/v1/agents/sessions/:id/usage', () => new HttpResponse(null, { status: 404 })),
    http.get('/api/v1/sessions/s1', () => HttpResponse.json(SESSION)),
    http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json([])),
    http.get('/api/v1/acl/agent_session/s1', () => HttpResponse.json([])),
    http.get('/api/v1/members', () => HttpResponse.json([])),
    http.get('/api/v1/groups', () => HttpResponse.json([])),
    http.get('/api/v1/notebooks', () => HttpResponse.json([{ id: 'nb-1', title: 'Notebook One' }])),
  )
})

afterEach(() => {
  vi.stubGlobal('WebSocket', realWebSocket)
})

describe('AgentPanel session sharing', () => {
  it('opens the permissions dialog from the chat header with the notebook link', async () => {
    renderPanel()
    await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))

    fireEvent.click(await screen.findByRole('button', { name: /Share this session/ }))

    const dialog = await screen.findByRole('dialog', { name: /permissions/i })
    await within(dialog).findByRole('combobox', { name: 'Notebook' })
    expect(within(dialog).getByRole('combobox', { name: 'Notebook' })).toHaveValue('nb-1')
    expect(within(dialog).getByText('Anyone who can view this notebook')).toBeInTheDocument()
  })

  it('starts a fresh session linked to the notebook on the New chat event', async () => {
    const created: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/agents/:id/session', async ({ request }) => {
        created.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ session_id: 's-new' })
      }),
    )
    renderPanel()
    await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))
    created.length = 0 // ignore the initial restore reconnect

    act(() => {
      window.dispatchEvent(new CustomEvent('aether:new-agent-chat'))
    })

    await waitFor(() => expect(created.length).toBe(1))
    expect(created[0].notebook_id).toBe('nb-1')
  })
})
