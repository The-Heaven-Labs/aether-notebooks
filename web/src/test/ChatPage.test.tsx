import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { Routes, Route } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { ChatPage } from '../pages/ChatPage'
import { server } from './server'
import { renderWithProviders } from './utils'

const SESSION = {
  id: 's1',
  agent_id: 'a1',
  notebook_id: '',
  user_id: 'u1',
  max_turns: 10,
  title: 'Revenue analysis',
  created_at: '2026-09-29T10:00:00Z',
  owner_email: 'owner@example.com',
  shared: false,
  can_edit: false,
  share_with_notebook_viewers: false,
}

const MESSAGES = [
  { id: 'm1', session_id: 's1', role: 'user', content: 'hello from owner', created_at: '2026-09-29T10:00:01Z' },
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

function renderChatPage(path = '/chats/s1') {
  return renderWithProviders(
    <Routes>
      <Route path="/chats/:id" element={<ChatPage />} />
      <Route path="/" element={<div>Home page marker</div>} />
    </Routes>,
    { initialPath: path },
  )
}

beforeEach(() => {
  localStorage.clear()
  MockWebSocket.instances = []
  vi.stubGlobal('WebSocket', MockWebSocket)
})

afterEach(() => {
  vi.stubGlobal('WebSocket', realWebSocket)
})

describe('ChatPage', () => {
  it('renders the read-only live viewer for a shared session', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => HttpResponse.json(SESSION)),
      http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
    )
    renderChatPage()

    expect(await screen.findByText('hello from owner')).toBeInTheDocument()
    expect(screen.getByText('Shared · Read-only')).toBeInTheDocument()
    expect(screen.queryByPlaceholderText(/Message agent/)).toBeNull()
    await waitFor(() => expect(MockWebSocket.instances.length).toBeGreaterThan(0))
  })

  it('renders the interactive page-mode panel for an editor', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => HttpResponse.json({ ...SESSION, shared: false, can_edit: true })),
      http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
      http.get('/api/v1/agents', () => HttpResponse.json([{
        id: 'a1', org_id: 'org-1', name: 'Test Agent', skill_ids: [], tool_ids: [],
        mcp_server_ids: [], mcp_servers: [], created_by: 'u1',
        created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
      }])),
      http.get('/api/v1/model-configs', () => HttpResponse.json([])),
      http.get('/api/v1/agents/sessions/s1/usage', () => new HttpResponse(null, { status: 404 })),
    )
    renderChatPage()

    expect(await screen.findByPlaceholderText(/Message agent/)).toBeInTheDocument()
    expect(screen.getByTitle('Back')).toBeInTheDocument()
    expect(screen.queryByText('Shared · Read-only')).toBeNull()
  })

  it('shows a no-access state on 403', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => new HttpResponse(null, { status: 403 })),
    )
    renderChatPage()

    expect(await screen.findByText(/don't have access to this chat/i)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /go home/i })).toBeInTheDocument()
  })

  it('shows a not-found state on 404', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => new HttpResponse(null, { status: 404 })),
    )
    renderChatPage()

    expect(await screen.findByText(/Chat not found or has been deleted/i)).toBeInTheDocument()
  })

  it('sends a fresh-tab Back to home instead of doing nothing', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => HttpResponse.json(SESSION)),
      http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
    )
    renderChatPage()
    await screen.findByText('hello from owner')

    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(await screen.findByText('Home page marker')).toBeInTheDocument()
  })

  it('sets the document title from the session title', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => HttpResponse.json(SESSION)),
      http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
    )
    renderChatPage()
    await screen.findByText('hello from owner')

    await waitFor(() => expect(document.title).toBe('Revenue analysis · Aether'))
  })

  it('shows a generic error state on 500', async () => {
    server.use(
      http.get('/api/v1/sessions/s1', () => new HttpResponse(null, { status: 500 })),
    )
    renderChatPage()

    expect(await screen.findByText('Could not load this chat.')).toBeInTheDocument()
  })

  it('keeps the panel mounted while a newly started session resolves', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>((resolve) => { release = resolve })
    let postCalled = false
    let newSessionRequested = false
    server.use(
      http.get('/api/v1/sessions/s1', () => HttpResponse.json({ ...SESSION, can_edit: true })),
      http.get('/api/v1/sessions/s1/messages', () => HttpResponse.json(MESSAGES)),
      http.get('/api/v1/agents', () => HttpResponse.json([{
        id: 'a1', org_id: 'org-1', name: 'Test Agent', skill_ids: [], tool_ids: [],
        mcp_server_ids: [], mcp_servers: [], created_by: 'u1',
        created_at: '2026-09-01T00:00:00Z', updated_at: '2026-09-01T00:00:00Z',
      }])),
      http.get('/api/v1/model-configs', () => HttpResponse.json([])),
      http.get('/api/v1/agents/sessions/s1/usage', () => new HttpResponse(null, { status: 404 })),
      http.post('/api/v1/agents/:id/session', () => {
        postCalled = true
        return HttpResponse.json({ session_id: 's-new' })
      }),
      http.get('/api/v1/sessions/s-new', async () => {
        newSessionRequested = true
        await gate
        return HttpResponse.json({ ...SESSION, id: 's-new', can_edit: true })
      }),
      http.get('/api/v1/sessions/s-new/messages', () => HttpResponse.json([])),
    )
    renderChatPage()
    await screen.findByPlaceholderText(/Message agent/)

    act(() => {
      window.dispatchEvent(new CustomEvent('aether:new-agent-chat'))
    })
    await waitFor(() => expect(postCalled).toBe(true))
    await waitFor(() => expect(newSessionRequested).toBe(true))

    // Flush pending state updates before asserting: if ChatPage cleared the
    // session on route change, the loading state would commit by now.
    await act(async () => {})

    // The new session GET is still gated: the panel must not have been replaced
    // by the loading state. Scope to the page region defensively: AppShell now
    // suppresses its global panel on chat routes, but the page owns this chat.
    expect(screen.queryByText('Loading chat…')).toBeNull()
    const main = document.getElementById('main-content')
    expect(main).not.toBeNull()
    expect(within(main as HTMLElement).getByPlaceholderText(/Message agent/)).toBeInTheDocument()

    act(() => { release() })
  })
})
