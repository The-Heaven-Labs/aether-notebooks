import type { ReactNode } from 'react'
import { describe, test, expect, beforeEach, afterEach, vi } from 'vitest'
import { screen, act, waitFor, fireEvent } from '@testing-library/react'
import { Route, Routes, useLocation } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import type { Awareness } from 'y-protocols/awareness'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { DashboardPage } from './DashboardPage'
import { AUTO_RERUN_DEBOUNCE_MS } from '../utils/dashboardRunSignature'
import type { DashboardVariable, Widget } from '../types'

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

// Captures the options each render passes to the hook so tests can assert the
// page's connect/edit intent (published mode must not connect).
const docHookOptions = vi.hoisted(() => ({
  calls: [] as Array<{ enabled?: boolean; connect?: boolean } | undefined>,
}))

vi.mock('../hooks/useDashboardDoc', async () => {
  const { useSyncExternalStore } = await import('react')
  return {
    useDashboardDoc: (_id: string, options?: { enabled?: boolean; connect?: boolean }) => {
      docHookOptions.calls.push(options)
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

// Registered cache-eligible re-run handles, keyed by widget id. The page calls
// the handle registered for a widget when a live definition change fires; the
// probe maps that call onto the test's spy for the widget.
const rerunSpies = vi.hoisted(() => new Map<string, () => void>())
// Registered (cache-bypassing) refresh handles, keyed by widget id — what the
// masthead's Refresh action invokes across every mounted widget.
const refreshSpies = vi.hoisted(() => new Map<string, () => void>())

vi.mock('../components/QueryDataWidget', async () => {
  const { useEffect } = await import('react')
  function QueryDataWidgetProbe({ widget, published, registerRefresher, registerRerunner }: {
    widget: { id: string; query?: string | null }
    published?: boolean
    registerRefresher?: (refresh: () => Promise<unknown>) => (() => void) | void
    registerRerunner?: (rerun: () => Promise<unknown>) => (() => void) | void
  }) {
    useEffect(
      () => registerRefresher?.(() => {
        refreshSpies.get(widget.id)?.()
        return Promise.resolve()
      }),
      [registerRefresher, widget.id],
    )
    useEffect(
      () => registerRerunner?.(() => {
        rerunSpies.get(widget.id)?.()
        return Promise.resolve()
      }),
      [registerRerunner, widget.id],
    )
    return <div data-testid={`query-${widget.id}`} data-published={published ? 'true' : 'false'}>{widget.query}</div>
  }
  return { QueryDataWidget: QueryDataWidgetProbe }
})

// Reads the real variables context so the test can see which definitions the
// page handed the provider (REST first, doc after sync) and how many hidden
// (other-chapter) filters the scoping pass reports.
vi.mock('../components/DashboardVariableBar', async () => {
  const { useDashboardVariables } = await import('../contexts/DashboardVariablesContext')
  return {
    DashboardVariableBar: () => {
      const { variables, hiddenFilterCount } = useDashboardVariables()
      return (
        <div data-testid="variable-bar">
          {variables.map(v => v.name).join(',')}
          {hiddenFilterCount > 0 && (
            <span data-testid="other-chapters-hint">
              {hiddenFilterCount === 1 ? '1 filter set in other chapters' : `${hiddenFilterCount} filters set in other chapters`}
            </span>
          )}
        </div>
      )
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

// Published mode: the response carries the snapshot identity, so the page
// renders this REST payload and must not merge the live draft document.
const PUBLISHED_DASHBOARD = {
  ...REST_DASHBOARD,
  title: 'Published board',
  published_version: { version: 1, created_at: '2026-10-10T00:00:00Z' },
  widgets: [widget(), queryWidget()],
}

// A draft whose widget set mirrors the published one, so mode switches can be
// compared widget-for-widget.
const DRAFT_DASHBOARD = { ...REST_DASHBOARD, widgets: [widget(), queryWidget()] }

function fakeAwareness(states: Array<{ email: string; name: string; color: string }>): Awareness {
  const map = new Map(states.map((user, i) => [i + 1, { user }]))
  return {
    getStates: () => map,
    on: () => {},
    off: () => {},
  } as unknown as Awareness
}

// Exposes the router's current location so tests can assert URL writes
// (`?tab=`) without reaching into router internals.
function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname + location.search}</div>
}

function renderDashboard(initialPath = '/dashboards/d1/view') {
  return renderWithProviders(
    <>
      <LocationProbe />
      <Routes>
        <Route path="/dashboards/:id/view" element={<DashboardPage />} />
      </Routes>
    </>,
    { initialPath },
  )
}

// jsdom reports zero element sizes; the page only mounts the grid once it has
// measured a non-zero container width.
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 900 })
  localStorage.clear()
  localStorage.setItem('aether_user_email', 'me@test.com')
  docStore.reset()
  docHookOptions.calls.length = 0
  rerunSpies.clear()
  refreshSpies.clear()
  server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json(REST_DASHBOARD)))
})

afterEach(() => {
  delete (HTMLElement.prototype as { clientWidth?: unknown }).clientWidth
  // The auto re-run tests flip to fake timers; make sure a failing assertion
  // cannot leak them into the next test.
  vi.useRealTimers()
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

  test('wraps the surface in the resolved app theme and renders the dashboard logo', async () => {
    localStorage.setItem('aether_token', 'test-token')
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      settings: {
        ...REST_DASHBOARD.settings,
        app: { theme: { color_scheme: 'dark' }, logo_attachment_id: 'logo-1' },
      },
      org_branding: { logo_attachment_id: 'org-logo' },
    })))
    renderDashboard()
    await screen.findByText('Rest board')

    const themeContainer = document.querySelector<HTMLElement>('.app-theme')
    if (!themeContainer) throw new Error('.app-theme container not found')
    expect(themeContainer).toHaveAttribute('data-app-scheme', 'dark')

    // The dashboard logo wins over the org fallback, and the URL carries the
    // session token: an <img> cannot send an Authorization header. The alt is
    // decorative (the h1 announces the name).
    const logo = document.querySelector<HTMLImageElement>('.app-theme img')
    if (!logo) throw new Error('logo img not found')
    expect(logo).toHaveAttribute('src', '/api/v1/attachments/logo-1?token=test-token')
    expect(logo).toHaveAttribute('alt', '')
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
    // can_edit is a REST flag, so the editing controls still render. The app
    // surface keeps the grid column selector in the editor; the canEdit-gated
    // control on the masthead is the refresh cadence.
    expect(screen.getByLabelText('Auto-refresh interval')).toBeInTheDocument()
    expect(screen.queryByTitle('8 columns')).not.toBeInTheDocument()
    // Cell outputs still come from the REST widgets_data payload.
    expect(screen.getByTestId('output-renderer')).toHaveAttribute('data-count', '2')
  })

  test('the empty app explains what viewers will see', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      settings: {},
      widgets: [],
    })))
    renderDashboard()

    expect(await screen.findByText('This app has no widgets yet')).toBeInTheDocument()
    expect(screen.getByText(/viewers see them here as chapters, filters, and readings/)).toBeInTheDocument()
  })
})

describe('DashboardPage live auto re-run', () => {
  const REGION_VAR: DashboardVariable = { name: 'region', type: 'text', label: 'Region', default: 'us' }

  async function advance(ms: number) {
    await act(async () => { await vi.advanceTimersByTimeAsync(ms) })
  }

  // Renders under real timers until the REST snapshot is on screen, then
  // flips to fake timers for the takeover: `findBy`/`waitFor` cannot run
  // under fake timers in this setup, but the debounce windows must be
  // deterministic. The takeover advance also proves the first sync seeds
  // baselines instead of scheduling runs.
  async function takeoverDoc(
    widgets: Widget[],
    variables: unknown[] = [],
    settings: Record<string, unknown> = { grid_cols: 12 },
  ) {
    renderDashboard()
    await screen.findByText('Rest board')
    vi.useFakeTimers()
    act(() => {
      docStore.set({ synced: true, connected: true, title: 'Doc board', settings, variables, widgets })
    })
    await advance(AUTO_RERUN_DEBOUNCE_MS + 50)
  }

  test('the first sync seeds baselines without re-running widgets', async () => {
    const spy = vi.fn()
    rerunSpies.set('w2', spy)
    await takeoverDoc([queryWidget({ id: 'w2', query: 'SELECT 2' })])
    // takeoverDoc already advanced past the debounce window.
    expect(spy).not.toHaveBeenCalled()
  })

  test('typing SQL coalesces into one re-run after the debounce', async () => {
    const spy = vi.fn()
    rerunSpies.set('w2', spy)
    await takeoverDoc([queryWidget({ id: 'w2', query: 'SELECT 2' })])

    act(() => { docStore.set({ widgets: [queryWidget({ id: 'w2', query: 'SELECT 2 -- a' })] }) })
    act(() => { docStore.set({ widgets: [queryWidget({ id: 'w2', query: 'SELECT 2 -- ab' })] }) })
    await advance(AUTO_RERUN_DEBOUNCE_MS - 100)
    expect(spy).not.toHaveBeenCalled()

    await advance(200)
    expect(spy).toHaveBeenCalledTimes(1)
  })

  test('a layout-only change re-renders without running', async () => {
    const spy = vi.fn()
    rerunSpies.set('w2', spy)
    await takeoverDoc([queryWidget({ id: 'w2', query: 'SELECT 2' })])

    act(() => {
      docStore.set({
        widgets: [queryWidget({ id: 'w2', query: 'SELECT 2', layout: { row: 4, col: 4, width: 6, height: 8 } })],
      })
    })
    await advance(AUTO_RERUN_DEBOUNCE_MS + 50)
    expect(spy).not.toHaveBeenCalled()
  })

  test('a connector change re-runs the widget after the debounce', async () => {
    const spy = vi.fn()
    rerunSpies.set('w2', spy)
    await takeoverDoc([queryWidget({ id: 'w2', connector_id: 'c-1', query: 'SELECT 2' })])

    act(() => { docStore.set({ widgets: [queryWidget({ id: 'w2', connector_id: 'c-2', query: 'SELECT 2' })] }) })
    await advance(AUTO_RERUN_DEBOUNCE_MS + 50)
    expect(spy).toHaveBeenCalledTimes(1)
  })

  test('a variable default change re-runs only widgets referencing that token', async () => {
    const w2Spy = vi.fn()
    const w3Spy = vi.fn()
    rerunSpies.set('w2', w2Spy)
    rerunSpies.set('w3', w3Spy)
    await takeoverDoc(
      [
        queryWidget({ id: 'w2', query: 'SELECT * FROM t WHERE r = {{region}}' }),
        queryWidget({ id: 'w3', query: 'SELECT 1' }),
      ],
      [REGION_VAR],
    )

    act(() => { docStore.set({ variables: [{ ...REGION_VAR, default: 'eu' }] }) })
    await advance(AUTO_RERUN_DEBOUNCE_MS + 50)
    expect(w2Spy).toHaveBeenCalledTimes(1)
    expect(w3Spy).not.toHaveBeenCalled()
  })

  test('a cell reference change invalidates the dashboard without executing', async () => {
    let dashboardGets = 0
    server.use(http.get('/api/v1/dashboards/d1', () => {
      dashboardGets += 1
      return HttpResponse.json(REST_DASHBOARD)
    }))
    const spy = vi.fn()
    rerunSpies.set('w2', spy)
    await takeoverDoc([
      widget({ id: 'w1', notebook_id: 'nb-1', cell_id: 'cell-1' }),
      queryWidget({ id: 'w2', query: 'SELECT 2' }),
    ])
    expect(dashboardGets).toBe(1)

    act(() => {
      docStore.set({
        widgets: [
          widget({ id: 'w1', notebook_id: 'nb-2', cell_id: 'cell-1' }),
          queryWidget({ id: 'w2', query: 'SELECT 2' }),
        ],
      })
    })
    await advance(AUTO_RERUN_DEBOUNCE_MS + 50)
    expect(dashboardGets).toBe(2)
    expect(spy).not.toHaveBeenCalled()
  })

  test('a doc update does not restart the auto-refresh interval', async () => {
    let dashboardGets = 0
    server.use(http.get('/api/v1/dashboards/d1', () => {
      dashboardGets += 1
      return HttpResponse.json(REST_DASHBOARD)
    }))
    await takeoverDoc(
      [queryWidget({ id: 'w2', query: 'SELECT 2' })],
      [],
      { grid_cols: 12, auto_refresh_seconds: 30 },
    )
    expect(dashboardGets).toBe(1)

    // 20s in (plus the takeover advance): the interval has not fired.
    await advance(20_000)
    expect(dashboardGets).toBe(1)

    // A layout-only doc update mid-interval must not push the next run out:
    // with a restarted interval it would fire at ~52s, not at the 30s mark.
    act(() => {
      docStore.set({
        widgets: [queryWidget({ id: 'w2', query: 'SELECT 2', layout: { row: 4, col: 4, width: 6, height: 8 } })],
      })
    })
    await advance(7_500)
    expect(dashboardGets).toBe(1)

    await advance(1_000)
    expect(dashboardGets).toBe(2)
  })
})

describe('DashboardPage chapter tabs', () => {
  const TABS = [
    { id: 'overview', title: 'Overview' },
    { id: 'orders', title: 'Orders' },
  ]

  function tabWidget(id: string, tab: string): Widget {
    return queryWidget({
      id,
      query: `SELECT '${id}'`,
      layout: { row: 0, col: tab === 'overview' ? 0 : 6, width: 6, height: 8, tab },
    })
  }

  beforeEach(() => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      settings: { ...REST_DASHBOARD.settings, tabs: TABS },
      widgets: [tabWidget('w2', 'overview'), tabWidget('w3', 'orders')],
    })))
  })

  test('renders the tab bar and shows only the active chapter, switching by click', async () => {
    renderDashboard()
    await screen.findByRole('tab', { name: 'Overview' })

    expect(screen.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true')
    // One grid per chapter; both stay mounted (refreshers registered) and only
    // the active one is visible.
    expect(screen.getAllByTestId('grid')).toHaveLength(2)
    expect(screen.getByTestId('query-w2')).toBeVisible()
    expect(screen.getByTestId('query-w3')).not.toBeVisible()
    expect(screen.getByTestId('location')).toHaveTextContent('/dashboards/d1/view')
    // The visible chapter panel is linked to its tab.
    expect(screen.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', 'app-tab-overview')

    fireEvent.click(screen.getByRole('tab', { name: 'Orders' }))

    expect(screen.getByRole('tab', { name: 'Orders' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByTestId('query-w2')).not.toBeVisible()
    expect(screen.getByTestId('query-w3')).toBeVisible()
    expect(screen.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', 'app-tab-orders')
    expect(screen.getByTestId('location')).toHaveTextContent('/dashboards/d1/view?tab=orders')
  })

  test('an unknown ?tab falls back to the first chapter', async () => {
    renderDashboard('/dashboards/d1/view?tab=nope')
    await screen.findByRole('tab', { name: 'Overview' })

    expect(screen.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByTestId('query-w2')).toBeVisible()
    expect(screen.getByTestId('query-w3')).not.toBeVisible()
  })

  test('shows a quiet placeholder for a chapter with no widgets', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      settings: { ...REST_DASHBOARD.settings, tabs: TABS },
      widgets: [tabWidget('w2', 'overview')],
    })))
    renderDashboard()
    await screen.findByRole('tab', { name: 'Overview' })

    const placeholder = screen.getByText('Nothing in this chapter yet.')
    expect(placeholder).not.toBeVisible()

    fireEvent.click(screen.getByRole('tab', { name: 'Orders' }))
    expect(placeholder).toBeVisible()
    // The empty chapter renders no grid of its own.
    expect(screen.getAllByTestId('grid')).toHaveLength(1)
  })

  test('the refresh action runs the hidden chapter’s widgets too', async () => {
    const overviewRefresh = vi.fn()
    const ordersRefresh = vi.fn()
    refreshSpies.set('w2', overviewRefresh)
    refreshSpies.set('w3', ordersRefresh)

    renderDashboard()
    await screen.findByRole('tab', { name: 'Overview' })
    expect(screen.getByTestId('query-w3')).not.toBeVisible()

    fireEvent.click(screen.getByRole('button', { name: 'Refresh data' }))

    await waitFor(() => expect(ordersRefresh).toHaveBeenCalledTimes(1))
    expect(overviewRefresh).toHaveBeenCalledTimes(1)
  })
})

describe('DashboardPage filter scoping', () => {
  const TABS = [
    { id: 'overview', title: 'Overview' },
    { id: 'orders', title: 'Orders' },
  ]

  const SCOPED_VARIABLES: DashboardVariable[] = [
    {
      name: 'overview_region',
      label: 'Overview region',
      type: 'text',
      default: 'us',
      options: { mode: 'query', query: { connector_id: 'c1', sql: 'SELECT 1' } },
    },
    {
      name: 'orders_status',
      label: 'Orders status',
      type: 'text',
      default: 'open',
      options: { mode: 'query', query: { connector_id: 'c1', sql: 'SELECT 2' } },
    },
  ]

  function scopedDashboard(filterScope?: string) {
    return {
      ...REST_DASHBOARD,
      settings: {
        ...REST_DASHBOARD.settings,
        variables: SCOPED_VARIABLES,
        tabs: TABS,
        ...(filterScope ? { app: { filter_scope: filterScope } } : {}),
      },
      widgets: [
        queryWidget({
          id: 'w2',
          query: 'SELECT * FROM t WHERE r = {{overview_region}}',
          layout: { row: 0, col: 0, width: 6, height: 8, tab: 'overview' },
        }),
        queryWidget({
          id: 'w3',
          query: 'SELECT * FROM t WHERE s = {{orders_status}}',
          layout: { row: 0, col: 6, width: 6, height: 8, tab: 'orders' },
        }),
      ],
    }
  }

  function mockScopedDashboard(filterScope?: string) {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json(scopedDashboard(filterScope))))
  }

  test('chapter A shows only its variables and switching chapters re-scopes the strip', async () => {
    mockScopedDashboard('tab')
    renderDashboard()
    await screen.findByRole('tab', { name: 'Overview' })

    expect(screen.getByTestId('variable-bar')).toHaveTextContent('overview_region')
    expect(screen.getByTestId('variable-bar')).not.toHaveTextContent('orders_status')

    fireEvent.click(screen.getByRole('tab', { name: 'Orders' }))
    expect(screen.getByTestId('variable-bar')).toHaveTextContent('orders_status')
    expect(screen.getByTestId('variable-bar')).not.toHaveTextContent('overview_region')
  })

  test('a hidden filter pinned in the URL raises the other-chapters hint', async () => {
    mockScopedDashboard('tab')
    renderDashboard('/dashboards/d1/view?orders_status=closed')
    await screen.findByRole('tab', { name: 'Overview' })

    expect(await screen.findByTestId('other-chapters-hint')).toHaveTextContent('1 filter set in other chapters')
  })

  test('a pin on the visible chapter’s own filter raises no hint', async () => {
    mockScopedDashboard('tab')
    renderDashboard('/dashboards/d1/view?overview_region=eu')
    await screen.findByRole('tab', { name: 'Overview' })

    expect(screen.getByTestId('variable-bar')).toHaveTextContent('overview_region')
    expect(screen.queryByTestId('other-chapters-hint')).not.toBeInTheDocument()
  })

  test('options load only for visible variables; switching chapters loads the newly visible ones', async () => {
    const requested: string[] = []
    server.use(
      http.get('/api/v1/dashboards/d1', () => HttpResponse.json(scopedDashboard('tab'))),
      http.post('/api/v1/dashboards/d1/variables/:name/options', ({ params }) => {
        requested.push(String(params.name))
        return HttpResponse.json({ options: [] })
      }),
    )
    renderDashboard()
    await screen.findByRole('tab', { name: 'Overview' })

    await waitFor(() => expect(requested).toContain('overview_region'), { timeout: 3000 })
    expect(requested).not.toContain('orders_status')

    fireEvent.click(screen.getByRole('tab', { name: 'Orders' }))
    await waitFor(() => expect(requested).toContain('orders_status'), { timeout: 3000 })
  })

  test('filter_scope absent keeps every filter visible', async () => {
    mockScopedDashboard()
    renderDashboard()
    await screen.findByRole('tab', { name: 'Overview' })

    expect(screen.getByTestId('variable-bar')).toHaveTextContent('overview_region')
    expect(screen.getByTestId('variable-bar')).toHaveTextContent('orders_status')
    expect(screen.queryByTestId('other-chapters-hint')).not.toBeInTheDocument()
  })
})

describe('DashboardPage published mode', () => {
  test('a published dashboard renders the REST snapshot, never the live doc, and runs published', async () => {
    const urls: string[] = []
    server.use(http.get('/api/v1/dashboards/d1', ({ request }) => {
      urls.push(request.url)
      return HttpResponse.json(PUBLISHED_DASHBOARD)
    }))
    renderDashboard()
    await screen.findByText('Published board')

    // The viewer fetch asks for the published snapshot explicitly.
    expect(new URL(urls[0]).searchParams.get('published')).toBe('1')
    // The REST widget list paints: the cell widget's inlined outputs and the
    // query probe.
    expect(screen.getByTestId('output-renderer')).toHaveAttribute('data-count', '2')
    expect(screen.getByTestId('query-w2')).toHaveTextContent('SELECT 2')
    // Execute is threaded with published: true.
    expect(screen.getByTestId('query-w2')).toHaveAttribute('data-published', 'true')
    // The hook is told not to connect (no draft subscription, no mutators) —
    // including while the response was still loading, so the page never even
    // attempts the draft document before knowing the mode.
    expect(docHookOptions.calls.at(-1)).toMatchObject({ connect: false, enabled: false })
    expect(docHookOptions.calls.every((c) => c?.connect === false)).toBe(true)
    // Auto-refresh persists settings — the snapshot in this mode — so its
    // control is draft-only; the interval itself still runs from the
    // snapshot's cadence.
    expect(screen.queryByLabelText('Auto-refresh interval')).not.toBeInTheDocument()

    // A synced live document must not merge into the published view.
    act(() => {
      docStore.set({
        synced: true,
        connected: true,
        title: 'Doc board',
        settings: { grid_cols: 8 },
        variables: [],
        widgets: [queryWidget({ id: 'w2', query: 'SELECT 2 -- draft' })],
      })
    })
    expect(screen.queryByText('Doc board')).not.toBeInTheDocument()
    expect(screen.queryByText('Live')).not.toBeInTheDocument()
    expect(screen.getByTestId('query-w2')).toHaveTextContent('SELECT 2')
    expect(screen.getByTestId('variable-bar')).toHaveTextContent('region')
  })

  test('an editor viewing the published app gets a Preview draft link', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json(PUBLISHED_DASHBOARD)))
    renderDashboard()
    await screen.findByText('Published board')

    const link = screen.getByRole('link', { name: /Preview draft/ })
    expect(link).toHaveAttribute('href', '/dashboards/d1/view?preview=draft')
    expect(screen.queryByText('Draft preview')).not.toBeInTheDocument()
  })

  test('the Preview draft toggle switches to the plain draft fetch and back', async () => {
    const urls: string[] = []
    server.use(http.get('/api/v1/dashboards/d1', ({ request }) => {
      urls.push(request.url)
      const published = new URL(request.url).searchParams.get('published') === '1'
      return HttpResponse.json(published ? PUBLISHED_DASHBOARD : DRAFT_DASHBOARD)
    }))
    renderDashboard()
    await screen.findByText('Published board')

    fireEvent.click(screen.getByRole('link', { name: /Preview draft/ }))

    expect(await screen.findByText('Draft preview')).toBeInTheDocument()
    expect(await screen.findByText('Rest board')).toBeInTheDocument()
    expect(new URL(urls.at(-1)!).searchParams.has('published')).toBe(false)
    expect(screen.getByTestId('query-w2')).toHaveAttribute('data-published', 'false')

    fireEvent.click(screen.getByRole('link', { name: /View published/ }))
    await waitFor(() => expect(screen.queryByText('Draft preview')).not.toBeInTheDocument())
    expect(await screen.findByText('Published board')).toBeInTheDocument()
    expect(screen.getByTestId('query-w2')).toHaveAttribute('data-published', 'true')
  })

  test('an editor previewing the draft gets the plain fetch, the live doc, and the badge', async () => {
    const urls: string[] = []
    server.use(http.get('/api/v1/dashboards/d1', ({ request }) => {
      urls.push(request.url)
      return HttpResponse.json(DRAFT_DASHBOARD)
    }))
    renderDashboard('/dashboards/d1/view?preview=draft')
    await screen.findByText('Rest board')

    expect(urls.every(u => !new URL(u).searchParams.has('published'))).toBe(true)
    expect(screen.getByText('Draft preview')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /View published/ }))
      .toHaveAttribute('href', '/dashboards/d1/view')
    expect(screen.queryByRole('link', { name: /Preview draft/ })).not.toBeInTheDocument()
    expect(screen.getByTestId('query-w2')).toHaveAttribute('data-published', 'false')
    // The connect gate waits for the response: false while loading, true once
    // the draft is known.
    expect(docHookOptions.calls[0]).toMatchObject({ connect: false })
    expect(docHookOptions.calls.at(-1)).toMatchObject({ connect: true })
    // The draft surface keeps the auto-refresh editing control.
    expect(screen.getByLabelText('Auto-refresh interval')).toBeInTheDocument()

    // Draft preview keeps the live-document handover.
    act(() => {
      docStore.set({
        synced: true,
        connected: true,
        title: 'Doc board',
        settings: { grid_cols: 12 },
        variables: [],
        widgets: [queryWidget({ id: 'w2', query: 'SELECT 2 -- draft' })],
      })
    })
    expect(await screen.findByText('Doc board')).toBeInTheDocument()
    expect(screen.getByText('Draft preview')).toBeInTheDocument()
    expect(screen.getByTestId('query-w2')).toHaveTextContent('SELECT 2 -- draft')
  })

  test('preview=draft without edit falls back to the published snapshot', async () => {
    const urls: string[] = []
    server.use(http.get('/api/v1/dashboards/d1', ({ request }) => {
      urls.push(request.url)
      return HttpResponse.json({ ...PUBLISHED_DASHBOARD, can_edit: false })
    }))
    renderDashboard('/dashboards/d1/view?preview=draft')
    await screen.findByText('Published board')

    // The request is plain; the server serves the snapshot to non-editors.
    expect(new URL(urls[0]).searchParams.has('published')).toBe(false)
    expect(screen.queryByText('Draft preview')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /Preview draft/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /View published/ })).not.toBeInTheDocument()
    expect(screen.getByTestId('query-w2')).toHaveAttribute('data-published', 'true')

    act(() => { docStore.set({ synced: true, connected: true, title: 'Doc board' }) })
    expect(screen.queryByText('Doc board')).not.toBeInTheDocument()
    expect(screen.queryByText('Live')).not.toBeInTheDocument()
  })

  test('without a published version the viewer keeps the live-document behavior', async () => {
    const urls: string[] = []
    server.use(http.get('/api/v1/dashboards/d1', ({ request }) => {
      urls.push(request.url)
      return HttpResponse.json(REST_DASHBOARD)
    }))
    renderDashboard()
    await screen.findByText('Rest board')

    // The request still asks for the published view; the server falls back to
    // the draft when the dashboard has never been published.
    expect(new URL(urls[0]).searchParams.get('published')).toBe('1')
    expect(docHookOptions.calls.at(-1)).toMatchObject({ connect: true })

    act(() => {
      docStore.set({
        synced: true,
        connected: true,
        title: 'Doc board',
        settings: { grid_cols: 12 },
        variables: [],
        widgets: [queryWidget({ id: 'w2', query: 'SELECT 2' })],
      })
    })
    expect(await screen.findByText('Doc board')).toBeInTheDocument()
    expect(screen.getByTestId('query-w2')).toHaveAttribute('data-published', 'false')
    expect(screen.getByText('Live')).toBeInTheDocument()
  })

  test('a published view renders no collaborator presence or live indicator', async () => {
    docStore.set({
      connected: true,
      awareness: fakeAwareness([{ email: 'bob@test.com', name: 'Bob Smith', color: '#3366cc' }]),
    })
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json(PUBLISHED_DASHBOARD)))
    renderDashboard()
    await screen.findByText('Published board')

    expect(screen.queryByTitle('Bob Smith')).not.toBeInTheDocument()
    expect(screen.queryByText('Live')).not.toBeInTheDocument()
  })

  test('the mode toggle preserves other query params', async () => {
    server.use(http.get('/api/v1/dashboards/d1', ({ request }) => {
      const published = new URL(request.url).searchParams.get('published') === '1'
      return HttpResponse.json(published ? PUBLISHED_DASHBOARD : DRAFT_DASHBOARD)
    }))
    renderDashboard('/dashboards/d1/view?tab=orders')
    await screen.findByText('Published board')

    expect(screen.getByRole('link', { name: /Preview draft/ }))
      .toHaveAttribute('href', '/dashboards/d1/view?tab=orders&preview=draft')

    fireEvent.click(screen.getByRole('link', { name: /Preview draft/ }))
    expect(await screen.findByText('Draft preview')).toBeInTheDocument()
    // The way back keeps the active chapter too.
    expect(screen.getByRole('link', { name: /View published/ }))
      .toHaveAttribute('href', '/dashboards/d1/view?tab=orders')
  })
})

describe('html panels', () => {
  function htmlPanel() {
    return widget({
      id: 'w-html',
      notebook_id: null,
      cell_id: null,
      type: 'html',
      config: { html: '<p>hi</p>', css: '' },
    })
  }

  test('renders source-less html widgets in a sandboxed frame', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      widgets: [htmlPanel()],
      widgets_data: {},
    })))
    renderDashboard()
    await screen.findByText('Rest board')

    const frame = await screen.findByTitle('HTML panel')
    expect(frame.tagName).toBe('IFRAME')
    expect(frame).toHaveAttribute('sandbox', '')
    expect(frame.getAttribute('srcdoc')).toContain('<p>hi</p>')
  })

  test('renders the placeholder when the org switch is off', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      html_panels_enabled: false,
      widgets: [htmlPanel()],
      widgets_data: {},
    })))
    renderDashboard()
    await screen.findByText('Rest board')

    expect(await screen.findByText('HTML panels are disabled for this organization.')).toBeInTheDocument()
    expect(screen.queryByTitle('HTML panel')).not.toBeInTheDocument()
  })
})

describe('custom components', () => {
  function customWidget(overrides: Partial<Widget> = {}) {
    return widget({
      id: 'w-custom',
      notebook_id: null,
      cell_id: null,
      connector_id: null,
      query: null,
      type: 'custom',
      config: { html: '<p>x</p>', css: '', js: '1', bindings: [] },
      ...overrides,
    })
  }

  test('renders a custom widget in an allow-scripts sandboxed frame when enabled', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      custom_code_enabled: true,
      widgets: [customWidget()],
      widgets_data: {},
    })))
    renderDashboard()
    await screen.findByText('Rest board')

    const frame = await screen.findByTitle('Custom component')
    expect(frame.tagName).toBe('IFRAME')
    expect(frame).toHaveAttribute('sandbox', 'allow-scripts')
    expect(frame.getAttribute('srcdoc')).toContain('<p>x</p>')
  })

  test('renders the placeholder when the org switch is off', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      custom_code_enabled: false,
      widgets: [customWidget()],
      widgets_data: {},
    })))
    renderDashboard()
    await screen.findByText('Rest board')

    expect(await screen.findByText('Custom code is disabled for this organization.')).toBeInTheDocument()
    expect(screen.queryByTitle('Custom component')).not.toBeInTheDocument()
  })

  test('layout custom renders only the first custom component, hiding data sources', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({
      ...REST_DASHBOARD,
      custom_code_enabled: true,
      settings: { ...REST_DASHBOARD.settings, app: { layout: 'custom' } },
      widgets: [customWidget(), widget()],
    })))
    renderDashboard()
    await screen.findByText('Rest board')

    expect(await screen.findByTitle('Custom component')).toBeInTheDocument()
    // The cell widget is a data source on the custom page, not a rendered card.
    expect(screen.queryByTestId('output-renderer')).not.toBeInTheDocument()
  })
})
