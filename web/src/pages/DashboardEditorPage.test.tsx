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
    onDragStop?: (layout: unknown[], oldItem: unknown, newItem: unknown) => void
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

  test('a drag/resize stop writes the layout to the document and never PUTs the widget', async () => {
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
    await screen.findByTestId('grid')
    expect(screen.getByTestId('grid')).toHaveAttribute('data-drag-enabled', 'true')

    fireEvent.click(screen.getByTestId('drag-stop'))
    expect(docStore.mutators.updateLayout).toHaveBeenCalledWith('w1', { row: 4, col: 3, width: 6, height: 8 })

    // The mocked store does not echo the doc write back, so the resize still
    // starts from the REST/doc layout the page rendered.
    fireEvent.click(screen.getByTestId('resize-stop'))
    expect(docStore.mutators.updateLayout).toHaveBeenCalledWith('w1', { row: 0, col: 0, width: 8, height: 10 })

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
