import type { ReactNode } from 'react'
import { describe, test, expect, beforeEach, afterEach, vi } from 'vitest'
import { screen, act, waitFor } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import type { Awareness } from 'y-protocols/awareness'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { DashboardPage } from './DashboardPage'
import type { Widget } from '../types'

// The page shell, the live-document hook, and the heavy children are replaced
// with probes: this file verifies the REST/doc takeover and presence wiring,
// not the children's internals.

vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))

// A tiny external store standing in for the live document so tests can flip
// sync/connect state and doc content and have the page re-render.
const docStore = vi.hoisted(() => {
  interface DocState {
    synced: boolean
    connected: boolean
    title: string
    settings: Record<string, unknown>
    variables: unknown[]
    widgets: unknown[]
    awareness: unknown
  }
  const EMPTY: DocState = {
    synced: false,
    connected: false,
    title: '',
    settings: {},
    variables: [],
    widgets: [],
    awareness: null,
  }
  const state: DocState = { ...EMPTY }
  let snapshot: DocState = { ...state }
  const listeners = new Set<() => void>()
  return {
    state,
    set(patch: Partial<DocState>) {
      Object.assign(state, patch)
      snapshot = { ...state }
      listeners.forEach((listener) => listener())
    },
    reset() {
      Object.assign(state, EMPTY)
      snapshot = { ...state }
    },
    subscribe(listener: () => void) {
      listeners.add(listener)
      return () => { listeners.delete(listener) }
    },
    getSnapshot() {
      return snapshot
    },
  }
})

vi.mock('../hooks/useDashboardDoc', async () => {
  const { useSyncExternalStore } = await import('react')
  return {
    useDashboardDoc: () => {
      const snapshot = useSyncExternalStore(docStore.subscribe, docStore.getSnapshot, docStore.getSnapshot)
      return {
        ...snapshot,
        updateLayout: () => {},
        setQuery: () => {},
        setConfig: () => {},
        setTitle: () => {},
      }
    },
  }
})

vi.mock('react-grid-layout', () => ({
  GridLayout: ({ children }: { children: ReactNode }) => <div data-testid="grid">{children}</div>,
}))

vi.mock('../components/OutputRenderer', () => ({
  OutputRenderer: ({ outputs }: { outputs: unknown[] }) => (
    <div data-testid="output-renderer" data-count={outputs.length} />
  ),
}))

vi.mock('../components/QueryDataWidget', () => ({
  QueryDataWidget: ({ widget }: { widget: { id: string; query?: string | null } }) => (
    <div data-testid={`query-${widget.id}`}>{widget.query}</div>
  ),
}))

// Reads the real variables context so the test can see which definitions the
// page handed the provider (REST first, doc after sync).
vi.mock('../components/DashboardVariableBar', async () => {
  const { useDashboardVariables } = await import('../contexts/DashboardVariablesContext')
  return {
    DashboardVariableBar: () => {
      const { variables } = useDashboardVariables()
      return <div data-testid="variable-bar">{variables.map(v => v.name).join(',')}</div>
    },
  }
})

vi.mock('../hooks/useDashboardConnector', () => ({
  useDashboardConnector: () => ({ selected: null, suggestion: null, select: () => {}, resolving: false }),
}))

function widget(overrides: Partial<Widget> = {}): Widget {
  return {
    id: 'w1',
    dashboard_id: 'd1',
    notebook_id: 'nb-1',
    cell_id: 'cell-1',
    connector_id: null,
    query: null,
    language: 'sql',
    type: 'table',
    layout: { row: 0, col: 0, width: 6, height: 8 },
    config: { title: 'Rest widget' },
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function queryWidget(overrides: Partial<Widget> = {}): Widget {
  return widget({
    id: 'w2',
    notebook_id: null,
    cell_id: null,
    connector_id: 'c-1',
    query: 'SELECT 2',
    ...overrides,
  })
}

const OUTPUTS = [
  { type: 'table', data: { columns: [{ name: 'x', type: 'String' }], rows: [[1]] } },
  { type: 'text', data: 'ok' },
]

const REST_DASHBOARD = {
  id: 'd1',
  org_id: 'org-1',
  title: 'Rest board',
  settings: {
    grid_cols: 12,
    variables: [{ name: 'region', type: 'text', label: 'Region', default: 'us' }],
  },
  created_by: 'user-1',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  can_edit: true,
  can_view_with_data: true,
  can_share: true,
  widgets: [widget()],
  widgets_data: {
    'cell-1': {
      cell_id: 'cell-1',
      source: 'SELECT 1',
      type: 'table',
      language: 'sql',
      outputs: OUTPUTS,
      updated_at: '2026-01-02T00:00:00Z',
    },
  },
}

function fakeAwareness(states: Array<{ email: string; name: string; color: string }>): Awareness {
  const map = new Map(states.map((user, i) => [i + 1, { user }]))
  return {
    getStates: () => map,
    on: () => {},
    off: () => {},
  } as unknown as Awareness
}

function renderDashboard() {
  return renderWithProviders(
    <Routes>
      <Route path="/dashboards/:id/view" element={<DashboardPage />} />
    </Routes>,
    { initialPath: '/dashboards/d1/view' },
  )
}

// jsdom reports zero element sizes; the page only mounts the grid once it has
// measured a non-zero container width.
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 900 })
  localStorage.clear()
  localStorage.setItem('aether_user_email', 'me@test.com')
  docStore.reset()
  server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json(REST_DASHBOARD)))
})

afterEach(() => {
  delete (HTMLElement.prototype as { clientWidth?: unknown }).clientWidth
})

describe('DashboardPage live document wiring', () => {
  test('renders the REST snapshot until the document syncs, then the doc', async () => {
    renderDashboard()
    expect(await screen.findByText('Rest board')).toBeInTheDocument()
    expect(screen.getByTestId('variable-bar')).toHaveTextContent('region')
    expect(screen.getByTestId('output-renderer')).toHaveAttribute('data-count', '2')
    expect(screen.queryByTestId('query-w2')).not.toBeInTheDocument()

    act(() => {
      docStore.set({
        synced: true,
        connected: true,
        title: 'Doc board',
        settings: { grid_cols: 12 },
        variables: [
          { name: 'region', type: 'text', label: 'Region', default: 'us' },
          { name: 'env', type: 'text', default: 'prod' },
        ],
        widgets: [queryWidget({ id: 'w2', query: 'SELECT 2' })],
      })
    })

    expect(await screen.findByText('Doc board')).toBeInTheDocument()
    expect(screen.queryByText('Rest board')).not.toBeInTheDocument()
    expect(screen.getByTestId('variable-bar')).toHaveTextContent('region,env')
    expect(screen.getByTestId('query-w2')).toHaveTextContent('SELECT 2')
    expect(screen.queryByTestId('output-renderer')).not.toBeInTheDocument()
  })

  test('falls back silently to the REST snapshot when the relay is unavailable', async () => {
    renderDashboard()
    expect(await screen.findByText('Rest board')).toBeInTheDocument()
    expect(screen.getByTestId('output-renderer')).toHaveAttribute('data-count', '2')
    expect(screen.queryByText('Live')).not.toBeInTheDocument()
  })

  test('shows the Live indicator while the relay is connected', async () => {
    renderDashboard()
    await screen.findByText('Rest board')
    expect(screen.queryByText('Live')).not.toBeInTheDocument()

    act(() => { docStore.set({ connected: true }) })
    expect(await screen.findByText('Live')).toBeInTheDocument()

    act(() => { docStore.set({ connected: false }) })
    await waitFor(() => expect(screen.queryByText('Live')).not.toBeInTheDocument())
  })

  test('renders collaborator presence from the document awareness', async () => {
    docStore.set({
      connected: true,
      awareness: fakeAwareness([
        { email: 'bob@test.com', name: 'Bob Smith', color: '#3366cc' },
        { email: 'me@test.com', name: 'Me', color: '#cc3366' },
      ]),
    })
    renderDashboard()

    expect(await screen.findByTitle('Bob Smith')).toHaveTextContent('BS')
    // The viewer's own awareness entry is not rendered as a collaborator.
    expect(screen.queryByTitle('Me')).not.toBeInTheDocument()
  })

  test('keeps REST can_edit and widgets_data after the doc takes over', async () => {
    renderDashboard()
    await screen.findByText('Rest board')

    act(() => {
      docStore.set({
        synced: true,
        connected: true,
        title: 'Doc board',
        settings: { grid_cols: 8 },
        variables: [],
        widgets: [widget()],
      })
    })

    await screen.findByText('Doc board')
    // can_edit is a REST flag, so the editing controls still render; the doc
    // drives their state (the grid column count).
    expect(screen.getByTitle('8 columns')).toHaveAttribute('aria-pressed', 'true')
    // Cell outputs still come from the REST widgets_data payload.
    expect(screen.getByTestId('output-renderer')).toHaveAttribute('data-count', '2')
  })
})
