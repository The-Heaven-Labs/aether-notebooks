import { describe, test, expect, beforeEach, afterEach, vi } from 'vitest'
import { screen, fireEvent, waitFor, act } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import type { ReactNode } from 'react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { NotebookPage } from './NotebookPage'

// The page orchestrates execution and routing; the cell/editor internals are
// irrelevant here, so heavy children are replaced with probe components.
vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))

vi.mock('../components/Cell', () => ({
  Cell: ({ cell, onRun, routing }: {
    cell: { id: string; outputs?: Array<{ type: string; data: unknown }> }
    onRun: (cellId: string) => void
    routing?: { connector_name?: string }
  }) => (
    <div data-testid={`cell-${cell.id}`}>
      <button type="button" onClick={() => onRun(cell.id)}>{`Run ${cell.id}`}</button>
      {routing?.connector_name && <span>{`endpoint ${routing.connector_name}`}</span>}
      {(cell.outputs ?? [])
        .filter((output) => output.type === 'error')
        .map((output, index) => (
          <span key={index}>{String(output.data)}</span>
        ))}
    </div>
  ),
  collabCache: new Map(),
  focusCellEditorEnd: vi.fn(),
  updateCellScroll: vi.fn(),
}))

vi.mock('../components/CollaboratorAvatars', () => ({
  CollaboratorAvatars: () => null,
}))

const CELL_1 = {
  id: 'cell-1', notebook_id: 'nb-1', position: 0, type: 'code' as const, language: 'sql',
  source: 'SELECT 1', outputs: [], source_visible: true, cell_collapsed: false,
}
const CELL_2 = { ...CELL_1, id: 'cell-2', position: 1, connector_id: 'c-2' }

// The notebook's connector (c-1) and the connector assigned to cell-2 (c-2).
const CONNECTORS = [
  {
    id: 'c-1', org_id: 'org-1', name: 'CH RO', type: 'clickhouse',
    warehouse_id: 'wh-fallback', can_use: true, created_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'c-2', org_id: 'org-1', name: 'CH RW', type: 'clickhouse',
    warehouse_id: 'wh-fallback', can_use: true, created_at: '2026-01-01T00:00:00Z',
  },
]

const NOTEBOOK = {
  id: 'nb-1', org_id: 'org-1', title: 'Warehouse Notebook', description: '',
  parameters: [], connector_id: 'c-1', cells: [CELL_1, CELL_2],
  created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  can_edit: true, can_run: true, can_share: true,
}

const ROUTING = {
  warehouse_id: 'wh-1', warehouse_name: 'Analytics WH',
  connector_id: 'c-2', connector_name: 'CH RW', ch_user: 'aether_ab12_u_ef34',
}

function renderNotebook() {
  return renderWithProviders(
    <Routes>
      <Route path="/notebooks/:id" element={<NotebookPage />} />
    </Routes>,
    { initialPath: '/notebooks/nb-1' },
  )
}

// Minimal WebSocket stand-in so tests can push cell_output broadcasts into the
// page's real useNotebookWs handler.
class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  url: string
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  readyState = 0
  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }
  send() {}
  close() {
    this.readyState = 3
  }
}

function dispatchCellOutput(cellId: string, userEmail: string) {
  const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
  if (!ws) throw new Error('no WebSocket instance was created')
  act(() => {
    ws.onmessage?.({
      data: JSON.stringify({ type: 'cell_output', cell_id: cellId, outputs: [], user_email: userEmail }),
    })
  })
}

function executeHandlerWithRouting() {
  return http.post('/api/v1/notebooks/:id/cells/:cellId/execute', async ({ request }) => {
    executeCalls++
    executeBodies.push(await request.json() as Record<string, unknown>)
    return HttpResponse.json({
      outputs: [],
      metrics: { connect_time_ms: 1, query_time_ms: 2, render_time_ms: 3, total_time_ms: 6 },
      routing: ROUTING,
    })
  })
}

// Execute calls captured by the current test; reset in beforeEach.
let executeBodies: Array<Record<string, unknown>> = []
let executeCalls = 0

beforeEach(() => {
  localStorage.clear()
  executeBodies = []
  executeCalls = 0
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
  server.use(
    http.get('/api/v1/notebooks/nb-1', () => HttpResponse.json(NOTEBOOK)),
    http.get('/api/v1/connectors', () => HttpResponse.json(CONNECTORS)),
    http.put('/api/v1/notebooks/:id/cells/:cellId', () => HttpResponse.json({ ok: true })),
    http.post('/api/v1/notebooks/:id/cells/:cellId/execute', async ({ request }) => {
      executeCalls++
      executeBodies.push(await request.json() as Record<string, unknown>)
      return HttpResponse.json({ outputs: [], metrics: { connect_time_ms: 1, query_time_ms: 2, render_time_ms: 3, total_time_ms: 6 } })
    }),
  )
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('NotebookPage warehouse routing', () => {
  test('sends no pinned flag on execute', async () => {
    renderNotebook()
    fireEvent.click(await screen.findByRole('button', { name: 'Run cell-1' }))

    await waitFor(() => expect(executeBodies).toHaveLength(1))
    expect(executeBodies[0]).not.toHaveProperty('pinned')
    expect(executeBodies[0]).toEqual({ parameters: {} })
  })

  test("a runner's own WS cell_output broadcast does not clear the routing chip", async () => {
    localStorage.setItem('aether_token', 'test-token')
    server.use(executeHandlerWithRouting())

    renderNotebook()
    fireEvent.click(await screen.findByRole('button', { name: 'Run cell-1' }))
    expect(await screen.findByText('endpoint CH RW')).toBeInTheDocument()

    await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0))
    dispatchCellOutput('cell-1', 'user-1')

    // The broadcast is the same run over a second transport; the chip stays.
    expect(screen.getByText('endpoint CH RW')).toBeInTheDocument()
  })

  test('a collaborator WS cell_output clears the routing chip', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      localStorage.setItem('aether_token', 'test-token')
      server.use(executeHandlerWithRouting())

      renderNotebook()
      fireEvent.click(await screen.findByRole('button', { name: 'Run cell-1' }))
      expect(await screen.findByText('endpoint CH RW')).toBeInTheDocument()
      await waitFor(() => expect(FakeWebSocket.instances.length).toBeGreaterThan(0))

      // Expire the self-run pending marker so the next broadcast is treated as
      // a collaborator's update.
      await act(async () => { vi.advanceTimersByTime(3100) })
      dispatchCellOutput('cell-1', 'bob@test.com')

      await waitFor(() => expect(screen.queryByText('endpoint CH RW')).toBeNull())
    } finally {
      vi.useRealTimers()
    }
  })

  test('a failed re-run clears the routing chip from the previous run', async () => {
    server.use(executeHandlerWithRouting())
    renderNotebook()

    fireEvent.click(await screen.findByRole('button', { name: 'Run cell-1' }))
    expect(await screen.findByText('endpoint CH RW')).toBeInTheDocument()

    server.use(
      http.post('/api/v1/notebooks/:id/cells/:cellId/execute', () =>
        HttpResponse.json({ error: 'query failed' }, { status: 500 }),
      ),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Run cell-1' }))

    expect(await screen.findByText('query failed')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('endpoint CH RW')).toBeNull())
  })
})
