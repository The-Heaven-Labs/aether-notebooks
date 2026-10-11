import { describe, test, expect, beforeEach, afterEach, vi } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { DashboardVariablesProvider } from '../contexts/DashboardVariablesContext'
import { QueryDataWidget } from './QueryDataWidget'
import type { DashboardVariable, Widget } from '../types'

const widget: Widget = {
  id: 'w1',
  dashboard_id: 'd1',
  connector_id: 'c1',
  query: 'SELECT x FROM t',
  language: 'sql',
  type: 'table',
  layout: { row: 0, col: 0, width: 6, height: 6 },
  config: {},
  created_at: '2026-01-01T00:00:00Z',
}

function mockExecute(rows: unknown[][], column: { name: string; type: string } = { name: 'x', type: 'String' }) {
  server.use(
    http.post('/api/v1/dashboards/d1/execute', () =>
      HttpResponse.json({
        outputs: [{ type: 'table', data: { columns: [column], rows } }],
        metrics: {},
        cached: false,
      }),
    ),
  )
}

function renderWidget(variables: DashboardVariable[], initialPath: string, w: Widget = widget) {
  return renderWithProviders(
    <DashboardVariablesProvider dashboardId="d1" variables={variables} storageId="qdw-test">
      <QueryDataWidget dashboardId="d1" widget={w} canViewWithData />
    </DashboardVariablesProvider>,
    { initialPath },
  )
}

beforeEach(() => {
  localStorage.clear()
})

describe('QueryDataWidget zero states', () => {
  test('an empty multi_select explains the exclusion even when the query returns rows', async () => {
    mockExecute([[1]])
    renderWidget([{ name: 'sel', type: 'multi_select', label: 'Selection', default: ['A'] }], '/d1?sel=')
    expect(await screen.findByText('No rows match the current filters')).toBeInTheDocument()
    expect(screen.getByText(/Nothing is selected for Selection/)).toBeInTheDocument()
  })

  test('zero rows with an active filter names the filters and offers a reset', async () => {
    mockExecute([])
    renderWidget([{ name: 'q', type: 'text', default: '' }], '/d1?q=zzz')
    expect(await screen.findByText('No rows match the current filters')).toBeInTheDocument()
    expect(screen.getByText('q=zzz')).toBeInTheDocument()
    // Resetting the filter leaves a genuinely empty result, which must read
    // as "no data" rather than as a filter problem.
    fireEvent.click(screen.getByRole('button', { name: 'Reset filters' }))
    expect(await screen.findByText('No data returned')).toBeInTheDocument()
  })

  test('zero rows with no active filter reads as no data', async () => {
    mockExecute([])
    renderWidget([], '/d1')
    expect(await screen.findByText('No data returned')).toBeInTheDocument()
  })

  test('rows with no active filter render the widget normally', async () => {
    mockExecute([[1]])
    renderWidget([], '/d1')
    expect(await screen.findByText('1 row · 1 column')).toBeInTheDocument()
  })
})

describe('QueryDataWidget onFetched', () => {
  function renderWithOnFetched(onFetched: () => void) {
    return renderWithProviders(
      <DashboardVariablesProvider dashboardId="d1" variables={[]} storageId="qdw-test">
        <QueryDataWidget dashboardId="d1" widget={widget} canViewWithData onFetched={onFetched} />
      </DashboardVariablesProvider>,
      { initialPath: '/d1' },
    )
  }

  test('fires on the first fetch and again on an identical refetch', async () => {
    mockExecute([[1]])
    const onFetched = vi.fn()
    renderWithOnFetched(onFetched)
    expect(await screen.findByText('1 row · 1 column')).toBeInTheDocument()
    expect(onFetched).toHaveBeenCalledTimes(1)

    // Same rows again: TanStack's structural sharing keeps the data reference,
    // so the data effect cannot re-fire — the refresh wrapper must report.
    fireEvent.click(screen.getByRole('button', { name: 'Refresh widget data' }))
    await waitFor(() => expect(onFetched).toHaveBeenCalledTimes(2))
  })

  test('does not report freshness when the refetch fails', async () => {
    mockExecute([[1]])
    const onFetched = vi.fn()
    renderWithOnFetched(onFetched)
    expect(await screen.findByText('1 row · 1 column')).toBeInTheDocument()
    expect(onFetched).toHaveBeenCalledTimes(1)

    server.use(
      http.post('/api/v1/dashboards/d1/execute', () => new HttpResponse(null, { status: 500 })),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Refresh widget data' }))
    // The failure surfaces as the widget's error state; freshness stays put.
    expect(await screen.findByRole('button', { name: 'Retry' })).toBeInTheDocument()
    expect(onFetched).toHaveBeenCalledTimes(1)
  })
})

describe('QueryDataWidget metric widgets', () => {
  const metricWidget: Widget = {
    ...widget,
    type: 'metric',
    config: { metric: { value_column: 'v', aggregation: 'none' } },
  }

  test('a metric widget renders the computed value instead of a table', async () => {
    mockExecute([[42]], { name: 'v', type: 'Int32' })
    renderWidget([], '/d1', metricWidget)
    // The reading is the metric group, not a table cell that happens to say 42.
    expect(await screen.findByRole('group', { name: '42' })).toBeInTheDocument()
    expect(screen.queryByText(/1 row · 1 column/)).not.toBeInTheDocument()
    // The refresh/freshness footer stays available on metric widgets.
    expect(screen.getByRole('button', { name: 'Refresh widget data' })).toBeInTheDocument()
  })

  test('a metric widget with zero rows still reads as no data', async () => {
    mockExecute([], { name: 'v', type: 'Int32' })
    renderWidget([], '/d1', metricWidget)
    expect(await screen.findByText('No data returned')).toBeInTheDocument()
  })
})

describe('QueryDataWidget table column formats', () => {
  // jsdom reports zero element sizes, which makes @tanstack/react-virtual treat
  // the table viewport as empty and render no cells.
  beforeEach(() => {
    Object.defineProperty(HTMLElement.prototype, 'offsetHeight', { configurable: true, get: () => 300 })
    Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get: () => 800 })
  })
  afterEach(() => {
    delete (HTMLElement.prototype as { offsetHeight?: unknown }).offsetHeight
    delete (HTMLElement.prototype as { offsetWidth?: unknown }).offsetWidth
  })

  test('formats table cells from the widget config', async () => {
    mockExecute([[1200]], { name: 'revenue', type: 'Float64' })
    renderWidget([], '/d1', {
      ...widget,
      config: { table: { columns: [{ name: 'revenue', format: { style: 'currency', currency: 'USD', decimals: 0 } }] } },
    })
    expect(await screen.findByText('$1,200')).toBeInTheDocument()
  })
})
