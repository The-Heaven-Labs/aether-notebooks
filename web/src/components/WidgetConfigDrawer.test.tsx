import type { ReactNode } from 'react'
import { describe, test, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EditorView } from '@codemirror/view'
import * as Y from 'yjs'
import { http, HttpResponse, delay } from 'msw'
import { server } from '../test/server'
import { WidgetConfigDrawer, mergeChartConfigInto } from './WidgetConfigDrawer'
import { WIDGET_OVERRIDE_FLAG } from '../charts/widgetChartConfig'
import type { ChartConfig } from '../charts/types'
import { getDashboardCollab, releaseDashboardCollab } from './dashboardCollabRuntime'
import type { Dashboard, Widget } from '../types'

// A fake Hocuspocus provider with a real awareness instance: the drawer's SQL
// binding (y-codemirror) needs a working awareness, and the tests drive the
// shared document directly. No socket is ever opened.
vi.mock('@hocuspocus/provider', async () => {
  const { Awareness } = await import('y-protocols/awareness')
  type Listener = (payload?: unknown) => void
  class FakeHocuspocusProvider {
    readonly doc: import('yjs').Doc
    readonly awareness: InstanceType<typeof Awareness>
    destroyed = false
    private readonly listeners = new Map<string, Set<Listener>>()
    constructor(configuration: { document?: unknown }) {
      this.doc = configuration.document as import('yjs').Doc
      this.awareness = new Awareness(this.doc)
    }
    on(event: string, listener: Listener): void {
      let set = this.listeners.get(event)
      if (!set) {
        set = new Set()
        this.listeners.set(event, set)
      }
      set.add(listener)
    }
    off(event: string, listener: Listener): void {
      this.listeners.get(event)?.delete(listener)
    }
    destroy(): void {
      this.destroyed = true
      this.awareness.destroy()
    }
  }
  return { HocuspocusProvider: FakeHocuspocusProvider }
})

// The drawer embeds ConnectorSelector, which reads the shared ['connectors']
// React Query cache.
function renderWithQuery(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

const dashboard: Dashboard = {
  id: 'd1',
  org_id: 'org-1',
  title: 'Test Dashboard',
  settings: { variables: [] },
  created_by: 'u1',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
}

const tabbedDashboard: Dashboard = {
  ...dashboard,
  settings: {
    variables: [],
    tabs: [
      { id: 'overview', title: 'Overview' },
      { id: 'details', title: 'Details' },
    ],
  },
}

function queryWidget(overrides: Partial<Widget> = {}): Widget {
  return {
    id: 'w1',
    dashboard_id: 'd1',
    connector_id: 'conn-1',
    query: 'SELECT 1 AS x',
    language: 'sql',
    type: 'table',
    layout: { row: 0, col: 0, width: 6, height: 6 },
    config: {},
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function savedConfigs(bodies: Array<Record<string, unknown>>): Array<Record<string, unknown>> {
  return bodies
    .map(b => b.config)
    .filter((c): c is Record<string, unknown> => typeof c === 'object' && c !== null)
}


const tableOutput = {
  type: 'table',
  data: {
    columns: [{ name: 'x', type: 'Int32' }],
    rows: [[42]],
    truncated: false,
    rows_included: 1,
    rows_total: 1,
    bytes: 10,
  },
}

// jsdom reports zero element sizes, which makes @tanstack/react-virtual treat
// the preview viewport as empty and render no rows.
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get: () => 300 })
  Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get: () => 800 })
  server.use(
    http.get('/api/v1/connectors', () =>
      HttpResponse.json([
        { id: 'conn-1', name: 'Production DB', type: 'postgres' },
        { id: 'conn-2', name: 'Analytics CH', type: 'clickhouse' },
      ]),
    ),
  )
})

afterEach(() => {
  delete (HTMLElement.prototype as { offsetHeight?: unknown }).offsetHeight
  delete (HTMLElement.prototype as { offsetWidth?: unknown }).offsetWidth
  // Drop any dashboard document a test acquired so the shared runtime cache
  // never leaks between tests.
  releaseDashboardCollab('d1')
})

/** The shared query text of a seeded widget. */
function widgetQueryText(doc: Y.Doc, id: string): Y.Text {
  const wm = doc.getMap('widgets').get(id) as Y.Map<unknown>
  return wm.get('query') as Y.Text
}

/** Seeds a widget (with its query text) into a shared dashboard document. */
function seedWidgetDoc(doc: Y.Doc, id: string, query: string): void {
  doc.transact(() => {
    const wm = new Y.Map<unknown>()
    wm.set('type', 'table')
    wm.set('language', 'sql')
    wm.set('connector_id', 'conn-1')
    wm.set('notebook_id', '')
    wm.set('cell_id', '')
    wm.set('config', '{}')
    const layout = new Y.Map<unknown>()
    layout.set('row', 0)
    layout.set('col', 0)
    layout.set('width', 6)
    layout.set('height', 6)
    wm.set('layout', layout)
    wm.set('query', new Y.Text(query))
    doc.getMap('widgets').set(id, wm)
  })
}

describe('WidgetConfigDrawer', () => {
  test('renders the SQL source editor and connector selector', () => {
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    // The drawer portals to document.body, so query it there rather than in the render container.
    expect(document.querySelector('.cm-editor')!.textContent).toContain('SELECT 1 AS x')
    expect(screen.getByLabelText('Select connector')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Run/ })).toBeInTheDocument()
  })

  test('runs the widget and renders a preview', async () => {
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ outputs: [tableOutput], metrics: { query_time_ms: 3 }, cached: false }),
      ),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    expect(await screen.findByText('42')).toBeInTheDocument()
  })

  test('previews on open without clicking Run, and offers Configure chart', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', async ({ request }) => {
        bodies.push(await request.json() as Record<string, unknown>)
        return HttpResponse.json({ outputs: [tableOutput], metrics: {}, cached: true })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget({ type: 'chart' })} onClose={() => {}} onSaved={() => {}} />,
    )
    // The chart preview is a lazy-loaded chunk; under full-suite load the
    // import can take longer than the 1s default wait.
    expect(await screen.findByRole('button', { name: 'Configure chart' }, { timeout: 5000 })).toBeInTheDocument()
    expect(bodies).toHaveLength(1)
    // The open preview goes through the cache; Run is the fresh fetch.
    expect(bodies[0].bypass_cache).toBe(false)
  })

  test('Run bypasses the cache after the open preview', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', async ({ request }) => {
        bodies.push(await request.json() as Record<string, unknown>)
        return HttpResponse.json({ outputs: [tableOutput], metrics: {}, cached: false })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    await waitFor(() => expect(bodies).toHaveLength(1))
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    await waitFor(() => expect(bodies).toHaveLength(2))
    expect(bodies[0].bypass_cache).toBe(false)
    expect(bodies[1].bypass_cache).toBe(true)
  })

  test('does not preview on open without view_with_data', async () => {
    let calls = 0
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () => {
        calls++
        return HttpResponse.json({ outputs: [tableOutput], metrics: {}, cached: false })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={{ ...dashboard, can_view_with_data: false }} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    await new Promise(r => setTimeout(r, 50))
    expect(calls).toBe(0)
    expect(screen.getByRole('button', { name: /Run/ })).toBeDisabled()
  })

  test('shows a Run error from the execute endpoint', async () => {
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ error: 'Query timed out' }, { status: 422 }),
      ),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Query timed out')
  })

  test('offers to define detected variables that are not yet configured', async () => {
    const onDefineVariable = vi.fn()
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ query: 'SELECT {{who}} AS greeting' })}
        onClose={() => {}}
        onSaved={() => {}}
        onDefineVariable={onDefineVariable}
      />,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Define variable' }))
    expect(onDefineVariable).toHaveBeenCalledWith('who')
  })

  test('date_range variables make their _start/_end tokens defined', async () => {
    const onDefineVariable = vi.fn()
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={{ ...dashboard, settings: { variables: [{ name: 'd', type: 'date_range', default: ['2026-01-01', '2026-01-31'] }] } }}
        widget={queryWidget({ query: 'SELECT count() FROM t WHERE ts >= {{d_start}} AND ts < {{d_end}}' })}
        onClose={() => {}}
        onSaved={() => {}}
        onDefineVariable={onDefineVariable}
      />,
    )
    expect(await screen.findByText('d_start')).toBeInTheDocument()
    expect(screen.getByText('d_end')).toBeInTheDocument()
    // The derived tokens belong to the date_range variable — nothing to define.
    expect(screen.queryByRole('button', { name: 'Define variable' })).not.toBeInTheDocument()
  })

  test('defining an unknown _start token suggests the base date_range variable', async () => {
    const onDefineVariable = vi.fn()
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ query: 'SELECT count() FROM t WHERE ts >= {{window_start}}' })}
        onClose={() => {}}
        onSaved={() => {}}
        onDefineVariable={onDefineVariable}
      />,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Define variable' }))
    expect(onDefineVariable).toHaveBeenCalledWith('window', 'date_range')
  })

  test('saves a widget type change', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    fireEvent.change(screen.getByLabelText('Widget type'), { target: { value: 'chart' } })
    await waitFor(() => expect(bodies).toContainEqual({ type: 'chart' }))
  })

  test('converts a cell widget to a query widget', async () => {
    let converted = false
    server.use(
      http.post('/api/v1/dashboards/d1/widgets/w2/convert-to-query', () => {
        converted = true
        return HttpResponse.json({ widget: {}, variables: [] })
      }),
    )
    const onSaved = vi.fn()
    const cellWidget = queryWidget({
      id: 'w2',
      connector_id: null,
      query: null,
      notebook_id: 'nb-1',
      cell_id: 'cell-1',
    })
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={cellWidget} onClose={() => {}} onSaved={onSaved} />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Convert to query widget' }))
    await waitFor(() => expect(converted).toBe(true))
    expect(onSaved).toHaveBeenCalled()
  })

  test('debounces the connector over REST without resending the SQL source', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    // Wait for the connector list to load: changing a controlled select to an
    // option that does not exist yet is a no-op.
    await screen.findByRole('option', { name: 'Analytics CH' })
    fireEvent.change(screen.getByLabelText('Select connector'), { target: { value: 'conn-2' } })
    await waitFor(
      () => expect(bodies).toContainEqual({ connector_id: 'conn-2' }),
      { timeout: 3000 },
    )
    // Only the changed field is sent; the server merges it into the widget.
    expect(bodies.every(body => !('query' in body))).toBe(true)
  })

  test('debounces local SQL saves over REST when no document is bound', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    const view = EditorView.findFromDOM(document.querySelector('.cm-editor') as HTMLElement)
    expect(view).not.toBeNull()
    act(() => {
      view!.dispatch({ changes: { from: view!.state.doc.length, insert: ' -- edited' } })
    })
    await waitFor(
      () => expect(bodies).toContainEqual({ query: 'SELECT 1 AS x -- edited' }),
      { timeout: 3000 },
    )
  })

  test('closes on Escape and on backdrop click', () => {
    const onClose = vi.fn()
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={onClose} onSaved={() => {}} />,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByTestId('widget-drawer-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  test('ignores Escape when closeOnEscape is false', () => {
    const onClose = vi.fn()
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget()}
        onClose={onClose}
        onSaved={() => {}}
        closeOnEscape={false}
      />,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })

  test('disables Run without view_with_data and explains why', () => {
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={{ ...dashboard, can_view_with_data: false }}
        widget={queryWidget()}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /Run/ })).toBeDisabled()
    expect(screen.getByRole('note')).toHaveTextContent('view_with_data')
  })

  test('binds the SQL editor to the shared query text for co-editing', async () => {
    const entry = getDashboardCollab('d1')
    entry.synced = true
    seedWidgetDoc(entry.doc, 'w1', 'SELECT 99 AS shared')

    const putBodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        putBodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )

    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget()}
        docSynced
        editingEnabled
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )

    // The shared text wins over the (stale) widget prop once the binding
    // attaches (the dynamic import chain can be slow under full-suite load).
    await waitFor(
      () => expect(document.querySelector('.cm-editor')!.textContent).toContain('SELECT 99 AS shared'),
      { timeout: 5000 },
    )

    // Editor → shared text: a character-level edit through the CodeMirror view.
    const view = EditorView.findFromDOM(document.querySelector('.cm-editor') as HTMLElement)
    expect(view).not.toBeNull()
    act(() => {
      view!.dispatch({ changes: { from: view!.state.doc.length, insert: ' -- typed' } })
    })
    await waitFor(() => {
      expect(widgetQueryText(entry.doc, 'w1').toString()).toBe('SELECT 99 AS shared -- typed')
    }, { timeout: 5000 })

    // Shared text → editor: a remote collaborator's edit.
    act(() => {
      entry.doc.transact(() => {
        widgetQueryText(entry.doc, 'w1').insert(0, 'REMOTE ')
      })
    })
    await waitFor(() => {
      expect(document.querySelector('.cm-editor')!.textContent).toContain('REMOTE SELECT 99 AS shared -- typed')
    }, { timeout: 5000 })

    // Doc-bound query text is persisted by the relay, never by a REST PUT.
    expect(putBodies).toEqual([])
  })

  test('sends only the connector over REST while the query lives in the document', async () => {
    const entry = getDashboardCollab('d1')
    entry.synced = true
    seedWidgetDoc(entry.doc, 'w1', 'SELECT 1 AS x')

    const putBodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        putBodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const onSaved = vi.fn()
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget()}
        docSynced
        editingEnabled
        onClose={() => {}}
        onSaved={onSaved}
      />,
    )

    await screen.findByRole('option', { name: 'Analytics CH' })
    fireEvent.change(screen.getByLabelText('Select connector'), { target: { value: 'conn-2' } })
    await waitFor(
      () => expect(putBodies).toContainEqual({ connector_id: 'conn-2' }),
      { timeout: 3000 },
    )
    // Sending the bound query would clobber concurrent co-edits.
    expect(putBodies.every(body => !('query' in body))).toBe(true)
    // Doc-sourced updates never invalidate the REST cache.
    expect(onSaved).not.toHaveBeenCalled()
  })

  test('disables every mutation while editing is paused', async () => {
    const entry = getDashboardCollab('d1')
    entry.synced = true
    seedWidgetDoc(entry.doc, 'w1', 'SELECT 1 AS x')

    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget()}
        docSynced
        editingEnabled={false}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )

    await screen.findByRole('option', { name: 'Analytics CH' })
    expect(screen.getByLabelText('Select connector')).toBeDisabled()
    expect(screen.getByLabelText('Widget type')).toBeDisabled()
    // Run is a read-only preview and stays available.
    expect(screen.getByRole('button', { name: /Run/ })).toBeEnabled()

    await waitFor(() => {
      const view = EditorView.findFromDOM(document.querySelector('.cm-editor') as HTMLElement)
      expect(view?.contentDOM.getAttribute('contenteditable')).toBe('false')
    }, { timeout: 5000 })
  })

  test('disables convert-to-query while editing is paused', () => {
    const cellWidget = queryWidget({ id: 'w2', connector_id: null, query: null, notebook_id: 'nb-1', cell_id: 'cell-1' })
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={cellWidget}
        docSynced
        editingEnabled={false}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: 'Convert to query widget' })).toBeDisabled()
  })

  test('saves the metric value column and aggregation', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    fireEvent.change(screen.getByLabelText('Widget type'), { target: { value: 'metric' } })
    fireEvent.change(await screen.findByLabelText('Value column'), { target: { value: 'x' } })
    fireEvent.change(screen.getByLabelText('Aggregation'), { target: { value: 'sum' } })
    // The value column commits after the 600ms debounce; both fields must end
    // up in the same metric block (each save merges the previous one).
    await waitFor(
      () => expect(savedConfigs(bodies).at(-1)?.metric).toEqual({ value_column: 'x', aggregation: 'sum' }),
      { timeout: 3000 },
    )
  })

  test('metric fields carry accessible field help from the shared source', async () => {
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget({ type: 'metric', config: { metric: { value_column: 'x' } } })} onClose={() => {}} onSaved={() => {}} />,
    )

    const trigger = await screen.findByRole('button', { name: 'Help: Value column' })
    trigger.focus()
    expect(trigger).toHaveFocus()
    fireEvent.click(trigger)
    expect(screen.getByRole('tooltip')).toHaveTextContent('The numeric column the metric computes over.')
    expect(screen.getByRole('tooltip')).toHaveTextContent('revenue')
  })

  test('renders the metric preview and column suggestions after a run', async () => {
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ outputs: [tableOutput], metrics: { query_time_ms: 3 }, cached: false }),
      ),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ type: 'metric', config: { metric: { value_column: 'x', aggregation: 'none' } } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    // MetricWidget replaces the table fallback so authors see the reading.
    expect(await screen.findByRole('group', { name: '42' })).toBeInTheDocument()
    expect(document.querySelector('#metric-value-columns option[value="x"]')).not.toBeNull()
  })

  test('editing the text markdown saves config.text.markdown', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ connector_id: null, query: null, type: 'text' })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.change(screen.getByLabelText('Markdown'), { target: { value: '## Hello' } })
    await waitFor(
      () => expect(savedConfigs(bodies).at(-1)?.text).toEqual({ markdown: '## Hello' }),
      { timeout: 3000 },
    )
  })

  test('a source-less text widget authors markdown without the cell convert affordance', () => {
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ connector_id: null, query: null, type: 'text' })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect(screen.queryByRole('button', { name: 'Convert to query widget' })).not.toBeInTheDocument()
    expect(screen.queryByText(/Notebook cell widget/)).not.toBeInTheDocument()
    expect(screen.getByLabelText('Markdown')).toBeInTheDocument()
  })

  test('changing the appearance background saves config.appearance.background', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    fireEvent.change(screen.getByLabelText('Background'), { target: { value: 'transparent' } })
    await waitFor(() => {
      const appearance = savedConfigs(bodies).at(-1)?.appearance as Record<string, unknown> | undefined
      expect(appearance?.background).toBe('transparent')
    })
  })

  test('disables the border checkbox on transparent backgrounds', () => {
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ config: { appearance: { background: 'transparent' } } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect(screen.getByLabelText('Border')).toBeDisabled()
    expect(screen.getByText(/never draw a border/i)).toBeInTheDocument()
  })

  test('serializes config saves so a slow write cannot clobber a newer one', async () => {
    const applied: Array<Record<string, unknown>> = []
    const arrivals: number[] = []
    let inFlight = 0
    let overlapped = false
    let requests = 0
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        requests++
        arrivals.push(requests)
        inFlight++
        if (inFlight > 1) overlapped = true
        const body = (await request.json()) as Record<string, unknown>
        if (requests === 1) await delay(200)
        // The server replaces `config` wholesale, so a write lands when its
        // response is produced: later completions overwrite earlier ones.
        applied.push(body.config as Record<string, unknown>)
        inFlight--
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ type: 'metric' })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    // Save 1 (slow) then save 2 (fast) before save 1 resolves.
    fireEvent.change(screen.getByLabelText('Aggregation'), { target: { value: 'sum' } })
    fireEvent.change(screen.getByLabelText('Background'), { target: { value: 'transparent' } })
    await waitFor(() => expect(applied).toHaveLength(2))
    expect(arrivals).toEqual([1, 2])
    // Serialized: save 2 was not in flight while save 1 was, so it executed
    // with the freshest merged config and its write is the one that stuck.
    expect(overlapped).toBe(false)
    const finalConfig = applied.at(-1)!
    expect(finalConfig.metric).toMatchObject({ aggregation: 'sum' })
    expect(finalConfig.appearance).toMatchObject({ background: 'transparent' })
  })

  test('flushes a pending debounced edit when the drawer unmounts', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { unmount } = renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ type: 'metric' })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.change(screen.getByLabelText('Value column'), { target: { value: 'x' } })
    // Unmount inside the 600ms debounce window: the draft must still be saved.
    unmount()
    await waitFor(() => expect(savedConfigs(bodies).at(-1)?.metric).toEqual({ value_column: 'x' }))
  })

  test('metric config survives a subsequent appearance change', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ config: { metric: { value_column: 'x', aggregation: 'none' } } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.change(screen.getByLabelText('Background'), { target: { value: 'transparent' } })
    await waitFor(() => {
      const last = savedConfigs(bodies).at(-1)
      expect(last?.metric).toEqual({ value_column: 'x', aggregation: 'none' })
      expect((last?.appearance as Record<string, unknown> | undefined)?.background).toBe('transparent')
    })
  })

  test('a cell widget chart save keeps appearance and metric blocks', () => {
    const chart = { chartType: 'bar', xAxis: 'x' } as ChartConfig
    const cell = mergeChartConfigInto(
      { appearance: { background: 'transparent' }, metric: { value_column: 'x' } },
      chart,
      false,
    )
    expect(cell).toMatchObject({
      appearance: { background: 'transparent' },
      metric: { value_column: 'x' },
      chartType: 'bar',
      xAxis: 'x',
      [WIDGET_OVERRIDE_FLAG]: true,
    })
    // Query widgets store the chart config raw, without the cell marker.
    const query = mergeChartConfigInto({ appearance: { background: 'transparent' } }, chart, true)
    expect(query.appearance).toEqual({ background: 'transparent' })
    expect(query[WIDGET_OVERRIDE_FLAG]).toBeUndefined()
  })

  test('hints to run before table columns can be configured', () => {
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    expect(screen.getByText('Run the query to configure columns.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Format x')).not.toBeInTheDocument()
  })

  test('saves a table column format from the run result', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ outputs: [tableOutput], metrics: { query_time_ms: 3 }, cached: false }),
      ),
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    fireEvent.change(await screen.findByLabelText('Format x'), { target: { value: 'currency' } })
    await waitFor(() =>
      expect(savedConfigs(bodies).at(-1)?.table).toEqual({ columns: [{ name: 'x', format: { style: 'currency' } }] }),
    )
    // Currency and decimals controls appear once a style is chosen.
    expect(screen.getByLabelText('Currency for x')).toBeInTheDocument()
    expect(screen.getByLabelText('Decimals for x')).toBeInTheDocument()
  })

  test('setting a column back to Default removes it from the saved config', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ outputs: [tableOutput], metrics: { query_time_ms: 3 }, cached: false }),
      ),
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ config: { table: { columns: [{ name: 'x', format: { style: 'currency', currency: 'USD' } }] } } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    expect(await screen.findByLabelText('Format x')).toHaveValue('currency')
    fireEvent.change(screen.getByLabelText('Format x'), { target: { value: 'default' } })
    // With no formats left the whole table block is dropped, not persisted as
    // { columns: [] }.
    await waitFor(() => {
      const last = savedConfigs(bodies).at(-1)
      expect(last).toBeDefined()
      expect(last?.table).toBeUndefined()
    })
  })

  test('normalizes the currency code when saving a table column format', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ outputs: [tableOutput], metrics: { query_time_ms: 3 }, cached: false }),
      ),
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ config: { table: { columns: [{ name: 'x', format: { style: 'currency' } }] } } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    fireEvent.change(await screen.findByLabelText('Currency for x'), { target: { value: ' eur ' } })
    await waitFor(
      () =>
        expect(savedConfigs(bodies).at(-1)?.table).toEqual({
          columns: [{ name: 'x', format: { style: 'currency', currency: 'EUR' } }],
        }),
      { timeout: 3000 },
    )
  })

  test('switching a column away from currency clears the currency code', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ outputs: [tableOutput], metrics: { query_time_ms: 3 }, cached: false }),
      ),
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ config: { table: { columns: [{ name: 'x', format: { style: 'currency', currency: 'USD' } }] } } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    fireEvent.change(await screen.findByLabelText('Format x'), { target: { value: 'number' } })
    await waitFor(() =>
      expect(savedConfigs(bodies).at(-1)?.table).toEqual({ columns: [{ name: 'x', format: { style: 'number' } }] }),
    )
  })

  test('preserves sibling keys under config.table when saving column formats', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ outputs: [tableOutput], metrics: { query_time_ms: 3 }, cached: false }),
      ),
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ config: { table: { keep: 'me' } } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    fireEvent.change(await screen.findByLabelText('Format x'), { target: { value: 'percent' } })
    await waitFor(() =>
      expect(savedConfigs(bodies).at(-1)?.table).toEqual({
        keep: 'me',
        columns: [{ name: 'x', format: { style: 'percent' } }],
      }),
    )
  })

  test('omits the table columns section for cell widgets', () => {
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ id: 'w2', connector_id: null, query: null, notebook_id: 'nb-1', cell_id: 'cell-1' })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect(screen.queryByText('Table columns')).not.toBeInTheDocument()
    expect(screen.queryByText('Run the query to configure columns.')).not.toBeInTheDocument()
    // The convert affordance for cell widgets remains.
    expect(screen.getByRole('button', { name: 'Convert to query widget' })).toBeInTheDocument()
  })

  test('currency and decimals edits merge into the column format', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () =>
        HttpResponse.json({ outputs: [tableOutput], metrics: { query_time_ms: 3 }, cached: false }),
      ),
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={dashboard}
        widget={queryWidget({ config: { table: { columns: [{ name: 'x', format: { style: 'currency' } }] } } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Run/ }))
    fireEvent.change(await screen.findByLabelText('Currency for x'), { target: { value: 'EUR' } })
    fireEvent.change(screen.getByLabelText('Decimals for x'), { target: { value: '2' } })
    await waitFor(
      () =>
        expect(savedConfigs(bodies).at(-1)?.table).toEqual({
          columns: [{ name: 'x', format: { style: 'currency', currency: 'EUR', decimals: 2 } }],
        }),
      { timeout: 3000 },
    )
  })

  test('omits the Placement section when the dashboard has no tabs', () => {
    renderWithQuery(
      <WidgetConfigDrawer dashboardId="d1" dashboard={dashboard} widget={queryWidget()} onClose={() => {}} onSaved={() => {}} />,
    )
    expect(screen.queryByLabelText('Tab')).not.toBeInTheDocument()
    expect(screen.queryByText('Placement')).not.toBeInTheDocument()
  })

  test('renders the Placement select with the tabs and the current placement', () => {
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={tabbedDashboard}
        widget={queryWidget({ layout: { row: 0, col: 0, width: 6, height: 6, tab: 'details' } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect(screen.getByText('Placement')).toBeInTheDocument()
    const select = screen.getByLabelText('Placement') as HTMLSelectElement
    expect(select.value).toBe('details')
    expect(screen.getByRole('option', { name: 'First tab (default)' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Overview' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Details' })).toBeInTheDocument()
  })

  test('an unset placement reads as the first-tab default', () => {
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={tabbedDashboard}
        widget={queryWidget()}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect((screen.getByLabelText('Placement') as HTMLSelectElement).value).toBe('')
  })

  test('a placement naming a deleted tab reads as the first-tab default', () => {
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={tabbedDashboard}
        widget={queryWidget({ layout: { row: 0, col: 0, width: 6, height: 6, tab: 'gone' } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect((screen.getByLabelText('Placement') as HTMLSelectElement).value).toBe('')
  })

  test('saving the tab placement writes the shared document when bound', async () => {
    const entry = getDashboardCollab('d1')
    entry.synced = true
    seedWidgetDoc(entry.doc, 'w1', 'SELECT 99 AS shared')
    const wm = entry.doc.getMap('widgets').get('w1') as Y.Map<unknown>
    const lm = wm.get('layout') as Y.Map<unknown>
    lm.set('tab', 'overview')

    const putBodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        putBodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )

    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={tabbedDashboard}
        widget={queryWidget({ layout: { row: 0, col: 0, width: 6, height: 6, tab: 'overview' } })}
        docSynced
        editingEnabled
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )

    // The doc binding is looked up through a dynamic import; wait for the
    // shared text to win over the (stale) widget prop before changing the tab,
    // so the save goes through the document path rather than the REST fallback.
    await waitFor(
      () => expect(document.querySelector('.cm-editor')!.textContent).toContain('SELECT 99 AS shared'),
      { timeout: 5000 },
    )
    fireEvent.change(screen.getByLabelText('Placement'), { target: { value: 'details' } })

    await waitFor(() => expect(lm.get('tab')).toBe('details'), { timeout: 5000 })
    // The grid fields survive the tab-only write.
    expect(lm.get('col')).toBe(0)
    expect(lm.get('width')).toBe(6)
    expect(lm.get('height')).toBe(6)
    // Doc-bound writes are persisted by the relay, never by a REST PUT.
    expect(putBodies).toEqual([])
  })

  test('saving the tab placement falls back to the widget PUT without a document', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const onSaved = vi.fn()
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={tabbedDashboard}
        widget={queryWidget({ layout: { row: 1, col: 2, width: 6, height: 6, tab: 'overview' } })}
        onClose={() => {}}
        onSaved={onSaved}
      />,
    )
    fireEvent.change(screen.getByLabelText('Placement'), { target: { value: 'details' } })
    await waitFor(() =>
      expect(bodies).toContainEqual({ layout: { row: 1, col: 2, width: 6, height: 6, tab: 'details' } }),
    )
    expect(onSaved).toHaveBeenCalled()
  })

  test('clearing the placement writes an empty tab', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithQuery(
      <WidgetConfigDrawer
        dashboardId="d1"
        dashboard={tabbedDashboard}
        widget={queryWidget({ layout: { row: 0, col: 0, width: 6, height: 6, tab: 'overview' } })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.change(screen.getByLabelText('Placement'), { target: { value: '' } })
    await waitFor(() =>
      expect(bodies).toContainEqual({ layout: { row: 0, col: 0, width: 6, height: 6, tab: '' } }),
    )
  })
})
