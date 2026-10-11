import type { ReactNode } from 'react'
import { describe, test, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EditorView } from '@codemirror/view'
import * as Y from 'yjs'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { WidgetConfigDrawer } from './WidgetConfigDrawer'
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
})
