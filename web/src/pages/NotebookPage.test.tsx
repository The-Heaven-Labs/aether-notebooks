import { describe, test, expect, beforeEach, vi } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
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

// Connectors deliberately carry a different warehouse_id than the 409 payload:
// the choice flow must trust the 409, not the connectors-list fallback.
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

// Execute calls captured by the current test; reset in beforeEach.
let executeBodies: Array<Record<string, unknown>> = []
let executeCalls = 0

beforeEach(() => {
  localStorage.clear()
  executeBodies = []
  executeCalls = 0
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

describe('NotebookPage warehouse routing', () => {
  test('409 prompts for a service, saves the 409 warehouse preference, and re-runs', async () => {
    const preferenceBodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/notebooks/:id/cells/:cellId/execute', async ({ request }) => {
        executeCalls++
        executeBodies.push(await request.json() as Record<string, unknown>)
        if (executeCalls === 1) {
          return HttpResponse.json(
            {
              error: 'service_choice_required',
              warehouse_id: 'wh-1',
              services: [
                { connector_id: 'c-1', name: 'CH RO' },
                { connector_id: 'c-2', name: 'CH RW' },
              ],
            },
            { status: 409 },
          )
        }
        return HttpResponse.json({
          outputs: [],
          metrics: { connect_time_ms: 1, query_time_ms: 2, render_time_ms: 3, total_time_ms: 6 },
          routing: ROUTING,
        })
      }),
      http.put('/api/v1/warehouses/wh-1/preference', async ({ request }) => {
        preferenceBodies.push(await request.json() as Record<string, unknown>)
        return HttpResponse.json({ user_id: 'user-1', warehouse_id: 'wh-1', connector_id: 'c-2' })
      }),
    )

    renderNotebook()
    fireEvent.click(await screen.findByRole('button', { name: 'Run cell-1' }))

    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'CH RO' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'CH RW' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'CH RW' }))

    await waitFor(() => expect(preferenceBodies).toEqual([{ connector_id: 'c-2' }]))
    await waitFor(() => expect(executeCalls).toBe(2))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(await screen.findByText('endpoint CH RW')).toBeInTheDocument()
  })

  test('keeps the dialog open and reports the failure when the re-run fails', async () => {
    server.use(
      http.post('/api/v1/notebooks/:id/cells/:cellId/execute', async ({ request }) => {
        executeCalls++
        executeBodies.push(await request.json() as Record<string, unknown>)
        if (executeCalls === 1) {
          return HttpResponse.json(
            {
              error: 'service_choice_required',
              warehouse_id: 'wh-1',
              services: [{ connector_id: 'c-2', name: 'CH RW' }],
            },
            { status: 409 },
          )
        }
        return HttpResponse.json({ error: 'query failed' }, { status: 500 })
      }),
      http.put('/api/v1/warehouses/wh-1/preference', () =>
        HttpResponse.json({ user_id: 'user-1', warehouse_id: 'wh-1', connector_id: 'c-2' }),
      ),
    )

    renderNotebook()
    fireEvent.click(await screen.findByRole('button', { name: 'Run cell-1' }))
    fireEvent.click(await screen.findByRole('button', { name: 'CH RW' }))

    expect(
      await screen.findByText(/preference was saved, but the query did not run/i),
    ).toBeInTheDocument()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  test('pin persists per user and notebook and only applies to the notebook connector', async () => {
    const first = renderNotebook()

    const pin = await screen.findByRole('button', { name: 'Pin connector' })
    await waitFor(() => expect(pin).toBeEnabled())
    fireEvent.click(pin)

    await waitFor(() => expect(screen.getByRole('button', { name: 'Unpin connector' })).toBeInTheDocument())
    expect(localStorage.getItem('aether_notebook_pin:user-1:nb-1')).toBe('true')

    // cell-1 inherits the notebook connector: the pin applies.
    fireEvent.click(screen.getByRole('button', { name: 'Run cell-1' }))
    await waitFor(() => expect(executeBodies).toHaveLength(1))
    expect(executeBodies[0]).toMatchObject({ pinned: true })

    // cell-2 has its own connector: the notebook pin does not apply.
    fireEvent.click(screen.getByRole('button', { name: 'Run cell-2' }))
    await waitFor(() => expect(executeBodies).toHaveLength(2))
    expect(executeBodies[1].pinned).toBeUndefined()

    // Remounting re-reads the persisted pin.
    first.unmount()
    renderNotebook()
    expect(await screen.findByRole('button', { name: 'Unpin connector' })).toBeInTheDocument()
  })

  test('a 409 whose error is not service_choice_required is shown as a cell error', async () => {
    server.use(
      http.post('/api/v1/notebooks/:id/cells/:cellId/execute', () =>
        HttpResponse.json(
          { error: 'some_other_conflict', services: [{ connector_id: 'c-1', name: 'CH RO' }] },
          { status: 409 },
        ),
      ),
    )

    renderNotebook()
    fireEvent.click(await screen.findByRole('button', { name: 'Run cell-1' }))

    // The non-routing 409 falls through to the normal cell error surface.
    expect(await screen.findByText('some_other_conflict')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
