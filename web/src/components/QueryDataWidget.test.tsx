import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent } from '@testing-library/react'
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

function mockExecute(rows: unknown[][]) {
  server.use(
    http.post('/api/v1/dashboards/d1/execute', () =>
      HttpResponse.json({
        outputs: [{ type: 'table', data: { columns: [{ name: 'x', type: 'String' }], rows } }],
        metrics: {},
        cached: false,
      }),
    ),
  )
}

function renderWidget(variables: DashboardVariable[], initialPath: string) {
  return renderWithProviders(
    <DashboardVariablesProvider dashboardId="d1" variables={variables} storageId="qdw-test">
      <QueryDataWidget dashboardId="d1" widget={widget} canViewWithData />
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
