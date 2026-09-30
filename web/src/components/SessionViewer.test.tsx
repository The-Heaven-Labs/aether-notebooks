import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { SessionViewer } from './SessionViewer'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'

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
  renderWithProviders(<SessionViewer sessionId="s1" {...props} />)
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

  it('hides the Shared · Read-only banner for a session the viewer owns', async () => {
    server.use(http.get('/api/v1/sessions/:id', () => HttpResponse.json({ ...SESSION, shared: false, can_edit: true })))
    await renderViewer()

    expect(await screen.findByText('hello from owner')).toBeInTheDocument()
    expect(screen.queryByText('Shared · Read-only')).toBeNull()
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
    expect(await screen.findByTestId('streaming-text')).toBeInTheDocument()
    expect(screen.getByText(/Live output/)).toBeInTheDocument()

    emit(ws, { type: 'done', data: { tokens: { input: 10, output: 5, duration_ms: 42 } } })
    await waitFor(() => expect(screen.queryByTestId('streaming-text')).toBeNull())
    expect(screen.getByText(/Live output/)).toBeInTheDocument()
    expect(screen.getByText('42ms')).toBeInTheDocument()
  })

  it('keeps partial text and appends the error on error', async () => {
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'token', data: 'partial answer' })
    expect(await screen.findByTestId('streaming-text')).toBeInTheDocument()

    emit(ws, { type: 'error', message: 'model failed' })
    await waitFor(() => expect(screen.queryByTestId('streaming-text')).toBeNull())
    expect(screen.getByText(/partial answer/)).toBeInTheDocument()
    expect(screen.getByText(/Error: model failed/)).toBeInTheDocument()
  })

  it('keeps partial text and appends the cancelled marker on cancelled', async () => {
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'token', data: 'half done' })
    expect(await screen.findByTestId('streaming-text')).toBeInTheDocument()

    emit(ws, { type: 'cancelled' })
    await waitFor(() => expect(screen.queryByTestId('streaming-text')).toBeNull())
    expect(screen.getByText(/half done/)).toBeInTheDocument()
    expect(screen.getByText(/\[Cancelled\]/)).toBeInTheDocument()
  })

  it('ignores tool_confirm_required and question events', async () => {
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'tool_confirm_required', tool_name: 'update_cell', tool_args: '{"source":"x"}', current_source: 'y' })
    emit(ws, { type: 'question', question: 'Pick one', options: ['a', 'b'], allow_custom: true })

    expect(screen.queryByText(/Confirm Tool Call/)).toBeNull()
    expect(screen.queryByText('Pick one')).toBeNull()
    expect(screen.getByRole('dialog', { name: 'Shared agent session' })).toBeInTheDocument()
    expect(ws.sent.some((f) => f.includes('tool_confirm') || f.includes('question_answer'))).toBe(false)
  })

  it('shows a Share button for owners and invokes the handler when provided', async () => {
    const onShare = vi.fn()
    server.use(http.get('/api/v1/sessions/:id', () => HttpResponse.json({ ...SESSION, can_edit: true, shared: false })))
    await renderViewer({ onShare })

    const share = await screen.findByRole('button', { name: /share/i })
    fireEvent.click(share)
    expect(onShare).toHaveBeenCalledTimes(1)
  })

  it('opens the sharing panel for the owner and PATCHes notebook inheritance', async () => {
    let patchBody: unknown
    server.use(
      http.get('/api/v1/sessions/:id', () => HttpResponse.json({
        ...SESSION, shared: false, can_edit: true, share_with_notebook_viewers: false,
      })),
      http.get('/api/v1/acl/agent_session/s1', () => HttpResponse.json([])),
      http.patch('/api/v1/sessions/s1', async ({ request }) => {
        patchBody = await request.json()
        return HttpResponse.json({ title: 'Revenue analysis', share_with_notebook_viewers: true })
      }),
    )
    await renderViewer()

    fireEvent.click(await screen.findByRole('button', { name: /share/i }))
    expect(await screen.findByRole('dialog', { name: /permissions/i })).toBeInTheDocument()
    const checkbox = await screen.findByRole('checkbox', { name: /Anyone who can view this notebook/i })
    expect(checkbox).not.toBeChecked()

    fireEvent.click(checkbox)
    await waitFor(() => expect(patchBody).toEqual({ share_with_notebook_viewers: true }))
    await waitFor(() => expect(checkbox).toBeChecked())
  })

  it('hides the notebook inheritance toggle when the session has no notebook', async () => {
    server.use(
      http.get('/api/v1/sessions/:id', () => HttpResponse.json({
        ...SESSION, notebook_id: '', shared: false, can_edit: true, share_with_notebook_viewers: false,
      })),
      http.get('/api/v1/acl/agent_session/s1', () => HttpResponse.json([])),
    )
    await renderViewer()

    fireEvent.click(await screen.findByRole('button', { name: /share/i }))
    expect(await screen.findByRole('dialog', { name: /permissions/i })).toBeInTheDocument()
    expect(screen.queryByText(/Anyone who can view this notebook/i)).toBeNull()
  })

  it('merges the session summary prop into the fetched body for the owner header and Share button', async () => {
    const onShare = vi.fn()
    server.use(
      http.get('/api/v1/sessions/:id', () => HttpResponse.json({
        id: 's1',
        agent_id: 'a1',
        notebook_id: 'nb-1',
        user_id: 'u1',
        max_turns: 10,
        created_at: '2026-09-29T10:00:00Z',
      })),
    )
    renderWithProviders(
      <SessionViewer
        sessionId="s1"
        session={{ ...SESSION, title: 'Summary title', shared: false, can_edit: true }}
        onShare={onShare}
      />,
    )

    expect(await screen.findByText('Summary title')).toBeInTheDocument()
    expect(screen.getByText('owner@example.com')).toBeInTheDocument()
    const share = await screen.findByRole('button', { name: /share/i })
    fireEvent.click(share)
    expect(onShare).toHaveBeenCalledTimes(1)
  })

  it('does not let a late REST seed clobber a reconnect_sync', async () => {
    const releases: { session?: () => void; messages?: () => void } = {}
    server.use(
      http.get('/api/v1/sessions/:id', async () => {
        await new Promise<void>((resolve) => { releases.session = resolve })
        return HttpResponse.json(SESSION)
      }),
      http.get('/api/v1/sessions/:id/messages', async () => {
        await new Promise<void>((resolve) => { releases.messages = resolve })
        return HttpResponse.json(MESSAGES)
      }),
    )
    const ws = await renderViewer()
    await waitFor(() => expect(releases.session).toBeDefined())
    await waitFor(() => expect(releases.messages).toBeDefined())

    emit(ws, {
      type: 'reconnect_sync',
      messages: [{ id: 'm9', session_id: 's1', role: 'assistant', content: 'authoritative', created_at: '2026-09-29T10:05:00Z' }],
    })

    await act(async () => {
      releases.session?.()
      releases.messages?.()
    })
    await waitFor(() => expect(screen.queryByText('Loading session…')).toBeNull())
    expect(screen.getByText('authoritative')).toBeInTheDocument()
    expect(screen.queryByText('hello from owner')).toBeNull()
  })

  it('shows the HTTP status and no empty state when access is denied', async () => {
    server.use(
      http.get('/api/v1/sessions/:id', () => HttpResponse.json({ error: 'insufficient permissions' }, { status: 403 })),
    )
    renderWithProviders(<SessionViewer sessionId="s1" />)

    expect(await screen.findByText('You do not have access to this session (HTTP 403)')).toBeInTheDocument()
    expect(screen.queryByText(/No messages in this session yet/)).toBeNull()
  })

  it('shows the HTTP status and no empty state when the session fetch fails', async () => {
    server.use(
      http.get('/api/v1/sessions/:id', () => HttpResponse.json({ error: 'boom' }, { status: 500 })),
    )
    renderWithProviders(<SessionViewer sessionId="s1" />)

    expect(await screen.findByText('Failed to load session (HTTP 500)')).toBeInTheDocument()
    expect(screen.queryByText(/No messages in this session yet/)).toBeNull()
  })

  it('shows a disconnected state on a socket error and recovers on open', async () => {
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    act(() => { ws.onopen?.() })
    expect(screen.getByText('Live')).toBeInTheDocument()

    act(() => { ws.onerror?.() })
    expect(screen.getByText('Disconnected')).toBeInTheDocument()
    expect(screen.queryByText('Live')).toBeNull()

    act(() => { ws.onopen?.() })
    expect(screen.getByText('Live')).toBeInTheDocument()
    expect(screen.queryByText('Disconnected')).toBeNull()
  })

  it('shows a disconnected state when reconnects are exhausted', async () => {
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    vi.useFakeTimers()
    try {
      // The close handler schedules a retry while attempts < 5 and only flags
      // the disconnected state once the budget is exhausted.
      for (let i = 0; i < 6; i++) {
        act(() => { ws.onclose?.() })
      }
      expect(screen.getByText('Disconnected')).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('restarts the seq clock on a new connection so restarted streams apply', async () => {
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'token', data: 'before restart', seq: 5 })
    expect(await screen.findByText(/before restart/)).toBeInTheDocument()

    vi.useFakeTimers()
    try {
      act(() => { ws.onclose?.() })
      act(() => { vi.advanceTimersByTime(1000) })
    } finally {
      vi.useRealTimers()
    }
    const next = MockWebSocket.instances[MockWebSocket.instances.length - 1]
    expect(next).not.toBe(ws)

    // A restarted server stream starts from a lower seq; it must not be
    // dropped by the previous connection's clock.
    emit(next, { type: 'token', data: 'after restart', seq: 1 })
    expect(await screen.findByText(/after restart/)).toBeInTheDocument()
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

  it('surfaces a failed subagent fetch instead of the empty state', async () => {
    server.use(
      http.get('/api/v1/agents/subagent/:taskId/messages', () =>
        HttpResponse.json({ error: 'forbidden' }, { status: 403 }),
      ),
    )
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'subagent_status', task_id: 'task-1', status: 'running', goal: 'do a thing' })
    fireEvent.click(await screen.findByText('SUBAGENT'))

    expect(await screen.findByText('Failed to load subagent messages (HTTP 403)')).toBeInTheDocument()
    expect(screen.queryByText(/hasn't produced any messages yet/)).toBeNull()
  })

  it('discards a stale subagent response after switching to another subagent', async () => {
    const releases: { first?: () => void } = {}
    server.use(
      http.get('/api/v1/agents/subagent/:taskId/messages', async ({ params }) => {
        if (params.taskId === 'task-1') {
          await new Promise<void>((resolve) => { releases.first = resolve })
          return HttpResponse.json([{ role: 'assistant', content: 'stale answer', created_at: '2026-09-29T10:01:00Z' }])
        }
        return HttpResponse.json([{ role: 'assistant', content: 'fresh answer', created_at: '2026-09-29T10:02:00Z' }])
      }),
    )
    const ws = await renderViewer()
    await screen.findByText('hello from owner')

    emit(ws, { type: 'subagent_status', task_id: 'task-1', status: 'running', goal: 'first' })
    emit(ws, { type: 'subagent_status', task_id: 'task-2', status: 'running', goal: 'second' })

    fireEvent.click((await screen.findAllByText('SUBAGENT'))[0])
    await waitFor(() => expect(releases.first).toBeDefined())
    fireEvent.click(screen.getByRole('button', { name: /back/i }))

    fireEvent.click((await screen.findAllByText('SUBAGENT'))[1])
    expect(await screen.findByText('fresh answer')).toBeInTheDocument()

    await act(async () => { releases.first?.() })
    expect(screen.getByText('fresh answer')).toBeInTheDocument()
    expect(screen.queryByText('stale answer')).toBeNull()
  })
})
