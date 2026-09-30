import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { NotebookChats } from './NotebookChats'
import type { AgentSessionListItem } from '../types/agent'
import { server } from '../test/server'

const DAY_MS = 24 * 60 * 60 * 1000

function listingRow(overrides: Partial<AgentSessionListItem> = {}): AgentSessionListItem {
  return {
    id: 's-1',
    agent_id: 'a-1',
    notebook_id: 'nb-1',
    user_id: 'user-1',
    max_turns: 10,
    title: 'Revenue analysis',
    created_at: new Date(Date.now() - 3 * DAY_MS).toISOString(),
    owner_email: 'alice@test.com',
    first_message: 'how did revenue move?',
    message_count: 4,
    shared: false,
    can_edit: true,
    share_with_notebook_viewers: false,
    ...overrides,
  }
}

function mockSessions(rows: AgentSessionListItem[]) {
  server.use(http.get('/api/v1/notebooks/nb-1/sessions', () => HttpResponse.json(rows)))
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('NotebookChats', () => {
  it('lists title/first message, owner, message count, relative date and the Shared badge', async () => {
    mockSessions([
      listingRow(),
      listingRow({
        id: 's-2',
        user_id: 'user-2',
        title: null,
        first_message: 'what changed last week?',
        owner_email: 'bob@test.com',
        message_count: 2,
        shared: true,
        can_edit: false,
        share_with_notebook_viewers: true,
        created_at: new Date(Date.now() - DAY_MS).toISOString(),
      }),
    ])

    render(<NotebookChats notebookId="nb-1" onClose={vi.fn()} onOpenSession={vi.fn()} />)

    expect(await screen.findByText('Revenue analysis')).toBeInTheDocument()
    expect(screen.getByText('alice@test.com')).toBeInTheDocument()
    expect(screen.getByText(/4 messages/)).toBeInTheDocument()
    expect(screen.getByText(/3d ago/)).toBeInTheDocument()

    expect(screen.getByText('what changed last week?')).toBeInTheDocument()
    expect(screen.getByText('bob@test.com')).toBeInTheDocument()
    expect(screen.getByText(/2 messages/)).toBeInTheDocument()

    expect(screen.getAllByText('Shared')).toHaveLength(1)
  })

  it('opens a shared row in the page-level viewer with the listing row', async () => {
    const shared = listingRow({
      id: 's-2', user_id: 'user-2', title: 'Bob session', owner_email: 'bob@test.com',
      shared: true, can_edit: false,
    })
    mockSessions([shared])
    const onOpenSession = vi.fn()

    render(<NotebookChats notebookId="nb-1" onClose={vi.fn()} onOpenSession={onOpenSession} />)
    await userEvent.click(await screen.findByRole('button', { name: /Bob session/ }))

    expect(onOpenSession).toHaveBeenCalledTimes(1)
    expect(onOpenSession).toHaveBeenCalledWith(shared)
  })

  it('opens an owned row in the viewer when no resume path is wired', async () => {
    const own = listingRow()
    mockSessions([own])
    const onOpenSession = vi.fn()

    render(<NotebookChats notebookId="nb-1" onClose={vi.fn()} onOpenSession={onOpenSession} />)
    await userEvent.click(await screen.findByRole('button', { name: /Revenue analysis/ }))

    expect(onOpenSession).toHaveBeenCalledTimes(1)
    expect(onOpenSession).toHaveBeenCalledWith(own)
  })

  it('resumes an owned row when onResumeSession is provided', async () => {
    const own = listingRow()
    mockSessions([own, listingRow({ id: 's-2', user_id: 'user-2', title: 'Shared session', shared: true, can_edit: false })])
    const onOpenSession = vi.fn()
    const onResumeSession = vi.fn()

    render(
      <NotebookChats
        notebookId="nb-1"
        onClose={vi.fn()}
        onOpenSession={onOpenSession}
        onResumeSession={onResumeSession}
      />,
    )
    await userEvent.click(await screen.findByRole('button', { name: /Revenue analysis/ }))

    expect(onResumeSession).toHaveBeenCalledTimes(1)
    expect(onResumeSession).toHaveBeenCalledWith(own)
    expect(onOpenSession).not.toHaveBeenCalled()
  })

  it('does not resume a row whose shared flag is omitted', async () => {
    const unknown = listingRow({ shared: undefined, can_edit: undefined })
    mockSessions([unknown])
    const onOpenSession = vi.fn()
    const onResumeSession = vi.fn()

    render(
      <NotebookChats
        notebookId="nb-1"
        onClose={vi.fn()}
        onOpenSession={onOpenSession}
        onResumeSession={onResumeSession}
      />,
    )
    await userEvent.click(await screen.findByRole('button', { name: /Revenue analysis/ }))

    expect(onOpenSession).toHaveBeenCalledWith(unknown)
    expect(onResumeSession).not.toHaveBeenCalled()
  })

  it('shows the empty state when the notebook has no visible chats', async () => {
    mockSessions([])

    render(<NotebookChats notebookId="nb-1" onClose={vi.fn()} onOpenSession={vi.fn()} />)

    expect(await screen.findByText('No chats in this notebook yet')).toBeInTheDocument()
  })

  it('shows an error state when the listing fails', async () => {
    server.use(
      http.get('/api/v1/notebooks/nb-1/sessions', () =>
        HttpResponse.json({ error: 'boom' }, { status: 500 }),
      ),
    )

    render(<NotebookChats notebookId="nb-1" onClose={vi.fn()} onOpenSession={vi.fn()} />)

    expect(await screen.findByText('Failed to load chats')).toBeInTheDocument()
  })
})
