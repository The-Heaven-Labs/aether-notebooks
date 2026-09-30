import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { SessionViewer } from './SessionViewer'
import { server } from '../test/server'

const SESSION = {
  id: 's1',
  agent_id: 'a1',
  notebook_id: 'nb-1',
  user_id: 'u1',
  max_turns: 10,
  title: 'Revenue analysis',
  created_at: '2026-09-29T10:00:00Z',
  owner_email: 'owner@example.com',
  shared: true,
  can_edit: false,
  share_with_notebook_viewers: true,
}

const MESSAGES = [
  { id: 'm1', session_id: 's1', role: 'user', content: 'hello from owner', created_at: '2026-09-29T10:00:01Z' },
  { id: 'm2', session_id: 's1', role: 'assistant', content: 'Hi there', created_at: '2026-09-29T10:00:02Z' },
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
  send(data: string) {
    this.sent.push(data)
  }
  close() {
    this.readyState = MockWebSocket.CLOSED
  }
}

const realWebSocket = globalThis.WebSocket

function emit(sock: MockWebSocket, msg: Record<string, unknown>) {
  act(() => {
    sock.onmessage?.({ data: JSON.stringify(msg) })
  })
}

async function renderViewer(props: { onShare?: () => void } = {}): Promise<MockWebSocket> {
  render(<SessionViewer sessionId="s1" {...props} />)
  await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))
  return MockWebSocket.instances[MockWebSocket.instances.length - 1]
}

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('aether_token', 'tok-123')
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
  server.use(
    http.get('/api/v1/sessions/:id', () => HttpResponse.json(SESSION)),
    http.get('/api/v1/sessions/:id/messages', () => HttpResponse.json(MESSAGES)),
  )
})

afterEach(() => {
  vi.stubGlobal('WebSocket', realWebSocket)
})

describe('SessionViewer', () => {
  it('fetches the session and messages, renders the transcript and header, and shows no composer', async () => {
    await renderViewer()

    expect(await screen.findByText('hello from owner')).toBeInTheDocument()
    expect(screen.getByText('Hi there')).toBeInTheDocument()
    expect(screen.getByText('Revenue analysis')).toBeInTheDocument()
    expect(screen.getByText('owner@example.com')).toBeInTheDocument()
    expect(screen.getByText('Shared · Read-only')).toBeInTheDocument()

    expect(screen.queryByRole('textbox')).toBeNull()
    expect(screen.queryByPlaceholderText(/Message agent/)).toBeNull()
    expect(screen.queryByTitle('Cancel (Esc)')).toBeNull()
    expect(screen.queryByRole('button', { name: /share/i })).toBeNull()
  })

  it('connects read-only: token + admin_mode query params, only reconnect on open', async () => {
    localStorage.setItem('aether_admin_mode', 'true')
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    expect(ws.url).toContain('/api/v1/ws/agents/s1')
    expect(ws.url).toContain('token=tok-123')
    expect(ws.url).toContain('admin_mode=true')

    act(() => {
      ws.onopen?.()
    })
    expect(ws.sent).toHaveLength(1)
    expect(JSON.parse(ws.sent[0])).toEqual({ type: 'reconnect', last_message_id: 'm2' })
  })

  it('appends live tokens and finalizes them into an assistant message on done', async () => {
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'token', data: 'Live ' })
    emit(ws, { type: 'token', data: 'output' })
    expect(await screen.findByText(/Live output/)).toBeInTheDocument()

    emit(ws, { type: 'done', data: { tokens: { input: 10, output: 5 } } })
    expect(await screen.findByText(/Live output/)).toBeInTheDocument()
  })

  it('ignores tool_confirm_required and question events', async () => {
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'tool_confirm_required', tool_name: 'update_cell', tool_args: '{"source":"x"}', current_source: 'y' })
    emit(ws, { type: 'question', question: 'Pick one', options: ['a', 'b'], allow_custom: true })

    expect(screen.queryByText(/Confirm Tool Call/)).toBeNull()
    expect(screen.queryByText('Pick one')).toBeNull()
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(ws.sent.some((f) => f.includes('tool_confirm') || f.includes('question_answer'))).toBe(false)
  })

  it('shows a Share button only for owners who wired a handler', async () => {
    const onShare = vi.fn()
    server.use(http.get('/api/v1/sessions/:id', () => HttpResponse.json({ ...SESSION, can_edit: true, shared: false })))
    await renderViewer({ onShare })

    const share = await screen.findByRole('button', { name: /share/i })
    fireEvent.click(share)
    expect(onShare).toHaveBeenCalledTimes(1)
  })

  it('loads subagent detail read-only when a subagent row is clicked', async () => {
    server.use(
      http.get('/api/v1/agents/subagent/:taskId/messages', () =>
        HttpResponse.json([
          { role: 'user', content: 'subagent goal', created_at: '2026-09-29T10:01:00Z' },
          { role: 'assistant', content: 'subagent answer', created_at: '2026-09-29T10:01:01Z' },
        ]),
      ),
    )
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'subagent_status', task_id: 'task-1', status: 'running', goal: 'do a thing' })
    fireEvent.click(await screen.findByText('SUBAGENT'))

    expect(await screen.findByText('subagent answer')).toBeInTheDocument()
    expect(screen.getByText(/Subagent task-1/)).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).toBeNull()
  })
})
