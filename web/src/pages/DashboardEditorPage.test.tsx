import type { ReactNode } from 'react'
import { describe, test, expect, beforeEach, afterEach, vi } from 'vitest'
import { screen, fireEvent, waitFor, act } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { DashboardEditorPage } from './DashboardEditorPage'
import type { Widget } from '../types'

// The page shell, the live-document hook, and the heavy children are replaced
// with probes: this file verifies the REST/doc field split and the editing
// gate, not the children's internals (WidgetConfigDrawer and SqlEditor have
// their own tests).

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
  }
  const EMPTY: DocState = {
    synced: false,
    connected: false,
    title: '',
    settings: {},
    variables: [],
    widgets: [],
  }
  const state: DocState = { ...EMPTY }
  let snapshot: DocState = { ...state }
  const listeners = new Set<() => void>()
  const mutators = {
    updateLayout: vi.fn(),
    setQuery: vi.fn(),
    setConfig: vi.fn(),
    setTitle: vi.fn(),
  }
  return {
    state,
    mutators,
    set(patch: Partial<DocState>) {
      Object.assign(state, patch)
      snapshot = { ...state }
      listeners.forEach((listener) => listener())
    },
    reset() {
      Object.assign(state, EMPTY)
      snapshot = { ...state }
      Object.values(mutators).forEach((fn) => fn.mockClear())
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
      useSyncExternalStore(docStore.subscribe, docStore.getSnapshot, docStore.getSnapshot)
      return {
        ...docStore.getSnapshot(),
        awareness: null,
        ...docStore.mutators,
      }
    },
  }
})

// GridLayout is a canvas for the drag/resize stop callbacks: the buttons
// replay a moved/resized layout item through the exact props the page passes.
vi.mock('react-grid-layout', () => ({
  GridLayout: ({ children, layout, dragConfig, resizeConfig, onDragStop, onResizeStop }: {
    children: ReactNode
    layout: Array<{ i: string; x: number; y: number; w: number; h: number }>
    dragConfig?: { enabled?: boolean }
    resizeConfig?: { enabled?: boolean }
    onDragStop?: (layout: unknown[], oldItem: unknown, newItem: unknown, placeholder?: unknown, e?: Event) => void
    onResizeStop?: (layout: unknown[], oldItem: unknown, newItem: unknown) => void
  }) => {
    const move = (patch: { x?: number; y?: number; w?: number; h?: number }) => {
      const next = layout.map(item => (item.i === 'w1' ? { ...item, ...patch } : item))
      return { next, changed: next.find(item => item.i === 'w1') }
    }
    return (
      <div
        data-testid="grid"
        data-drag-enabled={String(dragConfig?.enabled)}
        data-resize-enabled={String(resizeConfig?.enabled)}
      >
        {children}
        <button
          type="button"
          data-testid="drag-stop"
          onClick={() => {
            const { next, changed } = move({ x: 3, y: 4 })
            onDragStop?.(next, null, changed)
          }}
        >
          drag
        </button>
        <button
          type="button"
          data-testid="drag-stop-on-chip"
          onClick={() => {
            const { next, changed } = move({ x: 3, y: 4 })
            onDragStop?.(next, null, changed, null, { clientX: 20, clientY: 30 } as unknown as Event)
          }}
        >
          drag onto chip
        </button>
        <button
          type="button"
          data-testid="resize-stop"
          onClick={() => {
            const { next, changed } = move({ w: 8, h: 10 })
            onResizeStop?.(next, null, changed)
          }}
        >
          resize
        </button>
      </div>
    )
  },
}))

vi.mock('../components/OutputRenderer', () => ({
  OutputRenderer: ({ onChartConfigChange, onChartConfigReset }: {
    onChartConfigChange?: (config: { chartType: string }) => void
    onChartConfigReset?: () => void
  }) => (
    <div data-testid="output-renderer">
      <button type="button" data-testid="chart-config-change" onClick={() => onChartConfigChange?.({ chartType: 'bar' })}>
        config
      </button>
      <button type="button" data-testid="chart-config-reset" onClick={() => onChartConfigReset?.()}>
        reset
      </button>
    </div>
  ),
}))

vi.mock('../components/WidgetConfigDrawer', () => ({
  WidgetConfigDrawer: ({ editingEnabled, docSynced }: { editingEnabled?: boolean; docSynced?: boolean }) => (
    <div data-testid="widget-drawer" data-editing={String(editingEnabled)} data-doc-synced={String(docSynced)} />
  ),
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

const DASHBOARD = {
  id: 'd1',
  org_id: 'org-1',
  title: 'Ops board',
  settings: { grid_cols: 12, variables: [] },
  created_by: 'user-1',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  can_edit: true,
  can_view_with_data: true,
  widgets: [widget()],
}

const V1 = {
  version: 1,
  created_by_email: 'nova@heaven-labs.com',
  created_at: '2026-01-01T00:00:00Z',
  content_hash: 'hash-1',
}

const NO_VERSIONS = {
  versions: [],
  latest: null,
  has_unpublished_changes: true,
  draft_hash: 'draft-hash',
}

const PUBLISHED_VERSIONS = {
  versions: [V1],
  latest: V1,
  has_unpublished_changes: false,
  draft_hash: 'hash-1',
}

const NOTEBOOK = {
  id: 'nb-1',
  org_id: 'org-1',
  title: 'NB',
  description: '',
  parameters: [],
  created_by: 'user-1',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  cells: [{
    id: 'cell-1', notebook_id: 'nb-1', position: 0, type: 'code', language: 'sql',
    source: 'SELECT 1', outputs: [{ type: 'table', data: { columns: [], rows: [] } }],
  }],
}

function renderEditor() {
  return renderWithProviders(
    <Routes>
      <Route path="/dashboards/:id" element={<DashboardEditorPage />} />
    </Routes>,
    { initialPath: '/dashboards/d1' },
  )
}

// jsdom reports zero element sizes; the page only mounts the grid once it has
// measured a non-zero container width.
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 900 })
  docStore.reset()
  server.use(
    http.get('/api/v1/dashboards/d1', () => HttpResponse.json(DASHBOARD)),
    http.get('/api/v1/dashboards/d1/versions', () => HttpResponse.json(NO_VERSIONS)),
    http.get('/api/v1/notebooks/nb-1', () => HttpResponse.json(NOTEBOOK)),
  )
})

afterEach(() => {
  delete (HTMLElement.prototype as { clientWidth?: unknown }).clientWidth
})

describe('DashboardEditorPage live document wiring', () => {
  test('renders REST widgets until the document syncs, then the doc widgets', async () => {
    renderEditor()
    expect(await screen.findByLabelText('Edit Rest widget')).toBeInTheDocument()

    act(() => {
      docStore.set({
        synced: true,
        connected: true,
        title: 'Ops board',
        widgets: [widget({ id: 'w2', config: { title: 'Doc widget' } })],
      })
    })

    expect(await screen.findByLabelText('Edit Doc widget')).toBeInTheDocument()
    expect(screen.queryByLabelText('Edit Rest widget')).not.toBeInTheDocument()
  })

  test('a drag/resize stop writes the layout, tab included, to the document and never PUTs the widget', async () => {
    let widgetPuts = 0
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', () => {
        widgetPuts++
        return new HttpResponse(null, { status: 204 })
      }),
    )
    docStore.set({
      synced: true,
      connected: true,
      title: 'Ops board',
      widgets: [widget({ layout: { row: 0, col: 0, width: 6, height: 8, tab: 'overview' } })],
    })

    renderEditor()
    await screen.findByTestId('grid')
    expect(screen.getByTestId('grid')).toHaveAttribute('data-drag-enabled', 'true')

    fireEvent.click(screen.getByTestId('drag-stop'))
    expect(docStore.mutators.updateLayout).toHaveBeenCalledWith(
      'w1', { row: 4, col: 3, width: 6, height: 8, tab: 'overview' })

    // The mocked store does not echo the doc write back, so the resize still
    // starts from the REST/doc layout the page rendered.
    fireEvent.click(screen.getByTestId('resize-stop'))
    expect(docStore.mutators.updateLayout).toHaveBeenCalledWith(
      'w1', { row: 0, col: 0, width: 8, height: 10, tab: 'overview' })

    expect(widgetPuts).toBe(0)
  })

  test('disconnected: shows the paused banner and disables every mutation', async () => {
    docStore.set({
      synced: true,
      connected: false,
      title: 'Ops board',
      widgets: [widget()],
    })

    renderEditor()
    expect(await screen.findByText('Reconnecting — editing is paused')).toBeInTheDocument()

    // Drag/resize off; a stray stop callback is a no-op.
    await screen.findByTestId('grid')
    expect(screen.getByTestId('grid')).toHaveAttribute('data-drag-enabled', 'false')
    expect(screen.getByTestId('grid')).toHaveAttribute('data-resize-enabled', 'false')
    fireEvent.click(screen.getByTestId('drag-stop'))
    expect(docStore.mutators.updateLayout).not.toHaveBeenCalled()

    // Structural controls are disabled.
    expect(screen.getByRole('button', { name: '+ Add Widget' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Variables' })).toBeDisabled()
    expect(screen.getByLabelText('Remove Rest widget')).toBeDisabled()
    expect(screen.getByTestId('publish-status')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Publish' })).toBeDisabled()

    // The title cannot enter edit mode.
    fireEvent.click(screen.getByText('Ops board'))
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()

    // The drawer opens read-only.
    fireEvent.click(screen.getByLabelText('Edit Rest widget'))
    expect(screen.getByTestId('widget-drawer')).toHaveAttribute('data-editing', 'false')
    expect(screen.getByTestId('widget-drawer')).toHaveAttribute('data-doc-synced', 'true')
  })

  test('title edits debounce to the document instead of a REST rename', async () => {
    let dashboardPuts = 0
    server.use(
      http.put('/api/v1/dashboards/d1', () => {
        dashboardPuts++
        return HttpResponse.json(DASHBOARD)
      }),
    )
    docStore.set({
      synced: true,
      connected: true,
      title: 'Live title',
      widgets: [widget()],
    })

    renderEditor()
    await screen.findByTestId('grid')

    fireEvent.click(screen.getByText('Live title'))
    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'Renamed board' } })

    expect(docStore.mutators.setTitle).toHaveBeenCalledWith('Renamed board')
    expect(dashboardPuts).toBe(0)
  })

  test('chart config edits write the document with the override marker', async () => {
    let widgetPuts = 0
    server.use(
      http.put('/api/v1/dashboards/d1/widgets/w1', () => {
        widgetPuts++
        return new HttpResponse(null, { status: 204 })
      }),
    )
    docStore.set({
      synced: true,
      connected: true,
      title: 'Ops board',
      widgets: [widget()],
    })

    renderEditor()
    await screen.findByTestId('chart-config-change')

    fireEvent.click(screen.getByTestId('chart-config-change'))
    expect(docStore.mutators.setConfig).toHaveBeenCalledWith('w1', expect.objectContaining({
      chartType: 'bar',
      config_edited_from_dashboard: true,
    }))

    fireEvent.click(screen.getByTestId('chart-config-reset'))
    expect(docStore.mutators.setConfig).toHaveBeenCalledWith('w1', {})

    expect(widgetPuts).toBe(0)
  })

  test('structural deletes keep their REST call and invalidation', async () => {
    let deletes = 0
    server.use(
      http.delete('/api/v1/dashboards/d1/widgets/w1', () => {
        deletes++
        return new HttpResponse(null, { status: 204 })
      }),
    )
    docStore.set({
      synced: true,
      connected: true,
      title: 'Ops board',
      widgets: [widget()],
    })

    renderEditor()
    fireEvent.click(await screen.findByLabelText('Remove Rest widget'))
    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))

    await waitFor(() => expect(deletes).toBe(1))
  })
})

const TABS = [
  { id: 'alpha', title: 'Alpha' },
  { id: 'beta', title: 'Beta' },
]

function docWidget(id: string, tab: string, title: string): Widget {
  return widget({ id, layout: { row: 0, col: 0, width: 6, height: 8, tab }, config: { title } })
}

function syncedTabs(): void {
  docStore.set({
    synced: true,
    connected: true,
    title: 'Ops board',
    settings: { tabs: TABS },
    widgets: [
      docWidget('w1', 'alpha', 'Alpha widget'),
      docWidget('w2', 'beta', 'Beta widget'),
    ],
  })
}

/** Stubs the legacy single-element hit test; returns a restoring cleanup. */
function stubElementFromPoint(el: Element): () => void {
  Object.defineProperty(document, 'elementFromPoint', { configurable: true, value: () => el })
  return () => { delete (document as unknown as { elementFromPoint?: unknown }).elementFromPoint }
}

/** Stubs the full hit-test stack, recording the queried points. */
function stubElementsFromPoint(stack: Element[]): { points: Array<[number, number]>; restore: () => void } {
  const points: Array<[number, number]> = []
  Object.defineProperty(document, 'elementsFromPoint', {
    configurable: true,
    value: (x: number, y: number) => { points.push([x, y]); return stack },
  })
  return {
    points,
    restore: () => { delete (document as unknown as { elementsFromPoint?: unknown }).elementsFromPoint },
  }
}

describe('DashboardEditorPage tabs', () => {
  test('without tabs only the add shortcut shows; the filter stays hidden', async () => {
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()
    await screen.findByTestId('grid')

    expect(screen.queryByRole('button', { name: 'All' })).not.toBeInTheDocument()
    // The lone + is the only chapter control so a first tab can be created.
    expect(screen.getByRole('button', { name: 'Add tab' })).toBeInTheDocument()
    expect(screen.getByLabelText('Edit Rest widget')).toBeInTheDocument()
  })

  test('the lone add shortcut shows on mobile layouts too', async () => {
    const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 500 })
    try {
      docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
      renderEditor()
      await screen.findByLabelText('Edit Rest widget')

      expect(screen.getByRole('button', { name: 'Add tab' })).toBeInTheDocument()
    } finally {
      if (original) Object.defineProperty(HTMLElement.prototype, 'clientWidth', original)
      else delete (HTMLElement.prototype as { clientWidth?: unknown }).clientWidth
    }
  })

  test('renders a chip per tab and filters the grid from editor-local state', async () => {
    syncedTabs()
    renderEditor()

    expect(await screen.findByRole('button', { name: 'Alpha' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Beta' })).toBeInTheDocument()
    expect(screen.getByLabelText('Edit Alpha widget')).toBeInTheDocument()
    expect(screen.getByLabelText('Edit Beta widget')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Beta' }))
    expect(screen.queryByLabelText('Edit Alpha widget')).not.toBeInTheDocument()
    expect(screen.getByLabelText('Edit Beta widget')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Beta' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(screen.getByRole('button', { name: 'All' }))
    expect(screen.getByLabelText('Edit Alpha widget')).toBeInTheDocument()
    expect(screen.getByLabelText('Edit Beta widget')).toBeInTheDocument()
  })

  test('a deleted chapter does not re-apply its filter when the id returns', async () => {
    syncedTabs()
    renderEditor()
    fireEvent.click(await screen.findByRole('button', { name: 'Beta' }))
    expect(screen.queryByLabelText('Edit Alpha widget')).not.toBeInTheDocument()

    // Beta is deleted elsewhere: the filter falls back to All immediately.
    act(() => {
      docStore.set({ settings: { tabs: [TABS[0]] } })
    })
    expect(await screen.findByLabelText('Edit Alpha widget')).toBeInTheDocument()

    // A later chapter reusing the id must not resurrect the stale selection.
    act(() => {
      docStore.set({ settings: { tabs: [TABS[0], TABS[1]] } })
    })
    expect(screen.getByLabelText('Edit Alpha widget')).toBeInTheDocument()
    expect(screen.getByLabelText('Edit Beta widget')).toBeInTheDocument()
  })

  test('dropping a card on a tab chip moves it there in a single document write', async () => {
    syncedTabs()
    renderEditor()
    await screen.findByTestId('grid')

    // The dragged card sits above the chip at drop time, so the stack test must
    // skip the top element and find the chip beneath it.
    const { points, restore } = stubElementsFromPoint([
      screen.getByTestId('grid'),
      screen.getByRole('button', { name: 'Beta' }),
    ])
    try {
      fireEvent.click(screen.getByTestId('drag-stop-on-chip'))
    } finally {
      restore()
    }

    expect(points).toEqual([[20, 30]])
    // The final (dropped) position plus the new chapter, written as one layout.
    expect(docStore.mutators.updateLayout).toHaveBeenCalledTimes(1)
    expect(docStore.mutators.updateLayout).toHaveBeenCalledWith(
      'w1', { row: 4, col: 3, width: 6, height: 8, tab: 'beta' })
  })

  test('dropping a card on All clears its chapter without touching the position', async () => {
    syncedTabs()
    renderEditor()
    await screen.findByTestId('grid')

    const restore = stubElementFromPoint(screen.getByRole('button', { name: 'All' }))
    try {
      fireEvent.click(screen.getByTestId('drag-stop-on-chip'))
    } finally {
      restore()
    }

    expect(docStore.mutators.updateLayout).toHaveBeenCalledTimes(1)
    expect(docStore.mutators.updateLayout).toHaveBeenCalledWith(
      'w1', { row: 4, col: 3, width: 6, height: 8, tab: '' })
  })

  test('dropping outside any chip leaves the chapter unchanged', async () => {
    syncedTabs()
    renderEditor()
    await screen.findByTestId('grid')

    const restore = stubElementFromPoint(document.body)
    try {
      fireEvent.click(screen.getByTestId('drag-stop-on-chip'))
    } finally {
      restore()
    }

    // Only the position write happened; the tab write was skipped.
    expect(docStore.mutators.updateLayout).toHaveBeenCalledTimes(1)
    expect(docStore.mutators.updateLayout).toHaveBeenCalledWith(
      'w1', { row: 4, col: 3, width: 6, height: 8, tab: 'alpha' })
  })

  test('the + chip opens the app settings panel at the Tabs section', async () => {
    syncedTabs()
    renderEditor()

    fireEvent.click(await screen.findByRole('button', { name: 'Add tab' }))

    expect(await screen.findByRole('heading', { name: 'Tabs', level: 3 })).toBeInTheDocument()
    expect(screen.getByLabelText('Tab 1 title')).toHaveValue('Alpha')
  })

  test('a widget added while a chapter is active lands in that chapter', async () => {
    let posted: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/dashboards/d1/widgets', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ id: 'w3', dashboard_id: 'd1', type: 'text', layout: {}, config: {} })
      }),
    )
    syncedTabs()
    renderEditor()

    fireEvent.click(await screen.findByRole('button', { name: 'Beta' }))
    fireEvent.click(screen.getByRole('button', { name: '+ Add Widget' }))
    fireEvent.change(await screen.findByLabelText('Widget source'), { target: { value: 'none' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add Widget' }))

    const body = await waitFor(() => {
      expect(posted).not.toBeNull()
      return posted as unknown as Record<string, unknown>
    })
    expect(body).toMatchObject({
      type: 'text',
      layout: { row: 8, col: 0, width: 6, height: 8, tab: 'beta' },
    })
  })

  test('a widget added with All selected carries no tab', async () => {
    let posted: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/dashboards/d1/widgets', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ id: 'w3', dashboard_id: 'd1', type: 'text', layout: {}, config: {} })
      }),
    )
    syncedTabs()
    renderEditor()

    fireEvent.click(await screen.findByRole('button', { name: 'All' }))
    fireEvent.click(screen.getByRole('button', { name: '+ Add Widget' }))
    fireEvent.change(await screen.findByLabelText('Widget source'), { target: { value: 'none' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add Widget' }))

    const body = await waitFor(() => {
      expect(posted).not.toBeNull()
      return posted as unknown as Record<string, unknown>
    })
    expect(body).toMatchObject({ layout: { row: 8, col: 0, width: 6, height: 8 } })
    expect((body.layout as Record<string, unknown>).tab).toBeUndefined()
  })
})

describe('DashboardEditorPage publish controls', () => {
  test('reports Not published when there is no version', async () => {
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()

    const chip = await screen.findByTestId('publish-status')
    expect(chip).toHaveTextContent('Not published')
    expect(screen.queryByTestId('publish-status-dot')).not.toBeInTheDocument()
  })

  test('marks unpublished changes with an amber dot and a title', async () => {
    server.use(
      http.get('/api/v1/dashboards/d1/versions', () =>
        HttpResponse.json({ ...PUBLISHED_VERSIONS, has_unpublished_changes: true }),
      ),
    )
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()

    const chip = await screen.findByTestId('publish-status')
    await waitFor(() => expect(chip).toHaveTextContent('v1'))
    expect(chip).toHaveAttribute('title', 'Unpublished changes')
    expect(screen.getByTestId('publish-status-dot')).toBeInTheDocument()
  })

  test('a draft with blocking warnings shows the dot and the informative title', async () => {
    server.use(
      http.get('/api/v1/dashboards/d1/versions', () =>
        HttpResponse.json({ ...NO_VERSIONS, warnings: ['widget w1: connector not found'] }),
      ),
    )
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()

    const chip = await screen.findByTestId('publish-status')
    // The query only starts once the dashboard (with edit access) has landed;
    // the dot appears when the response does.
    expect(await screen.findByTestId('publish-status-dot')).toBeInTheDocument()
    expect(chip).toHaveTextContent('Not published')
    expect(chip).toHaveAttribute('title', 'Draft cannot be published: widget w1: connector not found')
    expect(chip).toHaveAttribute('aria-label', 'Not published — Draft cannot be published: widget w1: connector not found')
  })

  test('publishes with a busy state, then refreshes the chip and both queries', async () => {
    let published = false
    let publishPosts = 0
    let versionsGets = 0
    let dashboardGets = 0
    let release: (() => void) | undefined
    server.use(
      http.get('/api/v1/dashboards/d1', () => {
        dashboardGets++
        return HttpResponse.json(DASHBOARD)
      }),
      http.get('/api/v1/dashboards/d1/versions', () => {
        versionsGets++
        return HttpResponse.json(published ? PUBLISHED_VERSIONS : NO_VERSIONS)
      }),
      http.post('/api/v1/dashboards/d1/publish', async () => {
        publishPosts++
        await new Promise<void>((resolve) => { release = resolve })
        published = true
        return HttpResponse.json({ version: 1, content_hash: 'hash-1', created_at: '2026-01-01T00:00:00Z' })
      }),
    )
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()

    expect(await screen.findByTestId('publish-status')).toHaveTextContent('Not published')
    await waitFor(() => expect(versionsGets).toBe(1))

    fireEvent.click(screen.getByRole('button', { name: 'Publish' }))
    expect(await screen.findByRole('button', { name: 'Publishing…' })).toBeDisabled()
    expect(publishPosts).toBe(1)

    release?.()
    const chip = await screen.findByTestId('publish-status')
    await waitFor(() => expect(chip).toHaveTextContent('v1'))
    expect(screen.queryByTestId('publish-status-dot')).not.toBeInTheDocument()

    // Both invalidations refetch: the chip flips to v1 and the REST dashboard
    // row is pulled again.
    await waitFor(() => expect(versionsGets).toBeGreaterThan(1))
    await waitFor(() => expect(dashboardGets).toBeGreaterThan(1))
  })

  test('double-clicking Publish sends a single POST', async () => {
    let publishPosts = 0
    let release: (() => void) | undefined
    server.use(
      http.post('/api/v1/dashboards/d1/publish', async () => {
        publishPosts++
        await new Promise<void>((resolve) => { release = resolve })
        return HttpResponse.json({ version: 1, content_hash: 'hash-1', created_at: '2026-01-01T00:00:00Z' })
      }),
    )
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()

    const publish = await screen.findByRole('button', { name: 'Publish' })
    fireEvent.click(publish)
    expect(await screen.findByRole('button', { name: 'Publishing…' })).toBeDisabled()
    fireEvent.click(publish)

    expect(publishPosts).toBe(1)
    release?.()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Publish' })).not.toBeDisabled())
    expect(publishPosts).toBe(1)
  })

  test('a stale publish error clears on a later successful publish', async () => {
    let failNext = true
    server.use(
      http.post('/api/v1/dashboards/d1/publish', () => {
        if (failNext) {
          failNext = false
          return HttpResponse.json({ error: 'publish failed' }, { status: 500 })
        }
        return HttpResponse.json({ version: 1, content_hash: 'hash-1', created_at: '2026-01-01T00:00:00Z' })
      }),
    )
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()

    fireEvent.click(await screen.findByRole('button', { name: 'Publish' }))
    expect(await screen.findByText('publish failed')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Publish' }))
    await waitFor(() => expect(screen.queryByText('publish failed')).not.toBeInTheDocument())
  })

  test('a refused publish surfaces the widget warnings', async () => {
    server.use(
      http.post('/api/v1/dashboards/d1/publish', () =>
        HttpResponse.json(
          { error: 'dashboard document has invalid widgets', warnings: ['widget w1: connector not found'] },
          { status: 422 },
        ),
      ),
    )
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()

    fireEvent.click(await screen.findByRole('button', { name: 'Publish' }))

    expect(await screen.findByText(/dashboard document has invalid widgets/)).toHaveTextContent(
      'widget w1: connector not found',
    )
  })

  test('the status chip opens the version history panel', async () => {
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
    renderEditor()

    fireEvent.click(await screen.findByTestId('publish-status'))
    expect(await screen.findByRole('dialog', { name: 'Version history' })).toBeInTheDocument()
  })

  test('publish controls are hidden on mobile layouts', async () => {
    const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 500 })
    try {
      docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [widget()] })
      renderEditor()
      await screen.findByLabelText('Edit Rest widget')

      expect(screen.queryByTestId('publish-status')).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Publish' })).not.toBeInTheDocument()
    } finally {
      if (original) Object.defineProperty(HTMLElement.prototype, 'clientWidth', original)
      else delete (HTMLElement.prototype as { clientWidth?: unknown }).clientWidth
    }
  })

  test('publish controls show on an empty dashboard at desktop width', async () => {
    server.use(http.get('/api/v1/dashboards/d1', () => HttpResponse.json({ ...DASHBOARD, widgets: [] })))
    docStore.set({ synced: true, connected: true, title: 'Ops board', widgets: [] })
    renderEditor()

    // An empty dashboard renders no grid to measure, so the width must come
    // from the always-rendered body wrapper; otherwise the desktop-only
    // controls are hidden on every brand-new dashboard.
    expect(await screen.findByTestId('publish-status')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Publish' })).toBeInTheDocument()
  })

  test('a viewer (can_edit: false) never fetches versions and sees no publish controls', async () => {
    let dashboardGets = 0
    let versionGets = 0
    server.use(
      http.get('/api/v1/dashboards/d1', () => {
        dashboardGets++
        return HttpResponse.json({ ...DASHBOARD, can_edit: false })
      }),
      http.get('/api/v1/dashboards/d1/versions', () => {
        versionGets++
        return HttpResponse.json(NO_VERSIONS)
      }),
    )
    renderEditor()

    // Wait for the dashboard to land: once edit access is known to be false
    // the versions query must stay disabled, so the counter stays at zero
    // (enabling it while the dashboard is loading fires a 403 + retry).
    expect(await screen.findByText('Ops board')).toBeInTheDocument()
    await waitFor(() => expect(dashboardGets).toBe(1))

    expect(screen.queryByTestId('publish-status')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Publish' })).not.toBeInTheDocument()
    expect(versionGets).toBe(0)
  })
})
