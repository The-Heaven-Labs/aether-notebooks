import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
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

const ZERO_USAGE = {
  input: 0,
  output: 0,
  reasoning: 0,
  cache_read: 0,
  model_calls: 0,
  subagent_input: 0,
  subagent_output: 0,
  context_tokens: 0,
  context_window: 0,
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

function baseTokens(overrides: Record<string, unknown> = {}) {
  return {
    input: 0,
    output: 0,
    reasoning: 0,
    cache_read: 0,
    model_calls: 0,
    system_prompt: 0,
    skill_override: 0,
    history: 0,
    user_message: 0,
    tool_definitions: 0,
    tool_calls: 0,
    tool_results: 0,
    ...overrides,
  }
}

function seedSession(state: Record<string, unknown> | null) {
  localStorage.setItem('aether:lastAgentId', 'a1')
  localStorage.setItem('aether:lastSessionId', 's1')
  if (state) localStorage.setItem('aether:agentChat:__global__', JSON.stringify(state))
}

function savedStateWithTokens(tokens: Record<string, unknown>, contextWindow: number) {
  return {
    agentId: 'a1',
    sessionId: 's1',
    messages: [{ id: 'm1', role: 'user', content: 'hello', created_at: '2026-09-14T00:00:00Z' }],
    totalTokens: tokens,
    contextWindow,
  }
}

function emit(sock: MockWebSocket, msg: Record<string, unknown>) {
  act(() => {
    sock.onmessage?.({ data: JSON.stringify(msg) })
  })
}

async function renderPanel(): Promise<MockWebSocket> {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <AgentPanel notebookId="nb-1" width={360} onResize={() => {}} onClose={() => {}} />
    </QueryClientProvider>,
  )
  await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))
  return MockWebSocket.instances[MockWebSocket.instances.length - 1]
}

function rowValue(label: string): string {
  const row = screen.getByText(label).parentElement as HTMLElement
  return row.textContent ?? ''
}

beforeEach(() => {
  localStorage.clear()
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
  server.use(
    http.get('/api/v1/agents', () => HttpResponse.json([AGENT])),
    http.get('/api/v1/model-configs', () => HttpResponse.json([])),
    // Default: no REST snapshot so tests exercise the live event payloads. The
    // resume test overrides this with a real fixture.
    http.get('/api/v1/agents/sessions/:id/usage', () => new HttpResponse(null, { status: 404 })),
  )
})

afterEach(() => {
  vi.stubGlobal('WebSocket', realWebSocket)
})

describe('AgentPanel context-first token meter', () => {
  it('done replaces the turn tokens instead of adding them and keeps context_current', async () => {
    seedSession(savedStateWithTokens(baseTokens({ input: 100, output: 10, model_calls: 1, context_current: 500 }), 1000))
    const ws = await renderPanel()

    emit(ws, { type: 'token_update', tokens: baseTokens({ input: 650, output: 20, model_calls: 1, context_current: 500 }) })
    emit(ws, { type: 'done', data: { tokens: baseTokens({ input: 700, output: 30, model_calls: 1 }) } })

    await waitFor(() => expect(screen.getByText(/700↑ \/ 30↓/)).toBeInTheDocument())
    expect(screen.queryByText(/800↑/)).toBeNull()
    expect(screen.getByText(/\(50%\)/)).toBeInTheDocument()
  })

  it('renders the current-context percent instead of cumulative input+output', async () => {
    seedSession(savedStateWithTokens(baseTokens({ input: 0, output: 0 }), 1000))
    const ws = await renderPanel()

    emit(ws, {
      type: 'token_update',
      tokens: baseTokens({ input: 900, output: 100, model_calls: 1, context_current: 250 }),
      session_usage: { ...ZERO_USAGE, input: 900, output: 100, context_tokens: 250, context_window: 1000 },
    })

    await waitFor(() => expect(screen.getByText(/\(25%\)/)).toBeInTheDocument())
    expect(screen.queryByText(/\(100%\)/)).toBeNull()
  })

  it('applies session_usage from reconnect_sync to the "This session" hover rows', async () => {
    seedSession(savedStateWithTokens(baseTokens({ input: 0, output: 0 }), 0))
    const ws = await renderPanel()

    emit(ws, {
      type: 'reconnect_sync',
      messages: null,
      session_usage: {
        input: 1234,
        output: 567,
        reasoning: 89,
        cache_read: 11,
        model_calls: 3,
        subagent_input: 42,
        subagent_output: 7,
        context_tokens: 500,
        context_window: 1000,
      },
    })

    fireEvent.click(await screen.findByText(/↑/))
    expect(screen.getByText('This session')).toBeInTheDocument()
    expect(rowValue('Input')).toContain((1234).toLocaleString())
    expect(rowValue('Cache read')).toContain('11')
    expect(rowValue('Output')).toContain('567')
    expect(rowValue('Reasoning')).toContain('89')
    expect(rowValue('Subagent Input')).toContain('42')
    expect(rowValue('Subagent Output')).toContain('7')
    expect(rowValue('Model calls')).toContain('3')
    expect(rowValue('Current context')).toContain('(50%)')
  })

  it('seeds sessionUsage from the usage endpoint and updates contextWindow on resume', async () => {
    const usageSpy = vi.fn()
    server.use(
      http.get('/api/v1/agents/sessions/:id/usage', () => {
        usageSpy()
        return HttpResponse.json({
          ...ZERO_USAGE,
          input: 800,
          output: 80,
          reasoning: 8,
          model_calls: 2,
          context_tokens: 500,
          context_window: 2000,
        })
      }),
    )
    seedSession(null)

    await renderPanel()

    await waitFor(() => expect(usageSpy).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByText(/800↑ \/ 80↓/)).toBeInTheDocument())
    expect(screen.getByText(/\(25%\)/)).toBeInTheDocument()
  })
})
