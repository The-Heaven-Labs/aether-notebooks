import { describe, test, expect, beforeEach } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import type { ReactNode } from 'react'
import { server } from '../test/server'
import { DashboardVariablesProvider, useDashboardVariables } from './DashboardVariablesContext'
import type { DashboardVariable } from '../types'

function renderWithVariables(variables: DashboardVariable[], initialPath: string, dashboardId = 'd1', viewerConnectorId?: string | null) {
  return renderHook(() => useDashboardVariables(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <MemoryRouter initialEntries={[initialPath]}>
        <DashboardVariablesProvider dashboardId={dashboardId} variables={variables} viewerConnectorId={viewerConnectorId}>
          {children}
        </DashboardVariablesProvider>
      </MemoryRouter>
    ),
  })
}

const countryVar: DashboardVariable = { name: 'country', type: 'text', default: 'US' }

const cityVar: DashboardVariable = {
  name: 'city',
  type: 'single_select',
  depends_on: ['country'],
  options: {
    mode: 'query',
    query: { connector_id: 'conn-1', sql: 'SELECT label, value FROM cities' },
    label_column: 'label',
    value_column: 'value',
  },
}

beforeEach(() => {
  localStorage.clear()
})

describe('DashboardVariablesContext', () => {
  test('applies variable defaults', () => {
    const { result } = renderWithVariables([{ name: 'region', type: 'text', default: 'EMEA' }], '/dash/d1')
    expect(result.current.values.region).toBe('EMEA')
  })

  test('URL value wins over localStorage', () => {
    localStorage.setItem('aether_dashvars_d1', JSON.stringify({ region: 'LOCAL' }))
    const { result } = renderWithVariables(
      [{ name: 'region', type: 'text', default: 'EMEA' }],
      '/dash/d1?region=API',
    )
    expect(result.current.values.region).toBe('API')
  })

  test('setValue persists to localStorage', () => {
    const { result } = renderWithVariables([{ name: 'region', type: 'text', default: 'EMEA' }], '/dash/d1')
    act(() => {
      result.current.setValue('region', 'AMER')
    })
    expect(result.current.values.region).toBe('AMER')
    expect(JSON.parse(localStorage.getItem('aether_dashvars_d1') ?? '{}')).toMatchObject({
      region: 'AMER',
    })
  })

  test('loads query-backed options with parent values', async () => {
    const received: { variables?: Record<string, unknown> } = {}
    server.use(
      http.post('/api/v1/dashboards/d1/variables/city/options', async ({ request }) => {
        Object.assign(received, (await request.json()) as { variables?: Record<string, unknown> })
        return HttpResponse.json({ options: [{ label: 'Paris', value: 'paris' }] })
      }),
    )
    const { result } = renderWithVariables([countryVar, cityVar], '/dash/d1')

    await waitFor(
      () => expect(result.current.optionState.city?.options).toEqual([{ label: 'Paris', value: 'paris' }]),
      { timeout: 3000 },
    )
    expect(received.variables).toMatchObject({ country: 'US' })
  })

  test('options request carries the viewer connector selection', async () => {
    const received: { connector_id?: string } = {}
    server.use(
      http.post('/api/v1/dashboards/d1/variables/city/options', async ({ request }) => {
        Object.assign(received, (await request.json()) as { connector_id?: string })
        return HttpResponse.json({ options: [{ label: 'Paris', value: 'paris' }] })
      }),
    )
    const { result } = renderWithVariables([countryVar, cityVar], '/dash/d1', 'd1', 'conn-2')

    await waitFor(
      () => expect(result.current.optionState.city?.options).toEqual([{ label: 'Paris', value: 'paris' }]),
      { timeout: 3000 },
    )
    expect(received.connector_id).toBe('conn-2')
  })

  test('options request omits connector_id without a viewer selection', async () => {
    const received: { connector_id?: string } = {}
    server.use(
      http.post('/api/v1/dashboards/d1/variables/city/options', async ({ request }) => {
        Object.assign(received, (await request.json()) as { connector_id?: string })
        return HttpResponse.json({ options: [{ label: 'Paris', value: 'paris' }] })
      }),
    )
    const { result } = renderWithVariables([countryVar, cityVar], '/dash/d1')

    await waitFor(
      () => expect(result.current.optionState.city?.options).toEqual([{ label: 'Paris', value: 'paris' }]),
      { timeout: 3000 },
    )
    expect(received).not.toHaveProperty('connector_id')
  })

  test('reloads query-backed options when the viewer connector changes', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/variables/city/options', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ options: [{ label: 'Paris', value: 'paris' }] })
      }),
    )
    let connector: string | null = null
    const wrapper = ({ children }: { children: ReactNode }) => (
      <MemoryRouter initialEntries={['/dash/d1']}>
        <DashboardVariablesProvider dashboardId="d1" variables={[countryVar, cityVar]} viewerConnectorId={connector}>
          {children}
        </DashboardVariablesProvider>
      </MemoryRouter>
    )
    const { rerender } = renderHook(() => useDashboardVariables(), { wrapper })

    await waitFor(() => expect(bodies.length).toBe(1), { timeout: 3000 })
    expect(bodies[0]).not.toHaveProperty('connector_id')
    connector = 'conn-2'
    rerender()
    await waitFor(() => expect(bodies.length).toBe(2), { timeout: 3000 })
    expect(bodies[1].connector_id).toBe('conn-2')
  })

  test('exposes loading and error state for failed option queries', async () => {
    server.use(
      http.post('/api/v1/dashboards/d1/variables/city/options', async () => {
        await new Promise((resolve) => setTimeout(resolve, 400))
        return HttpResponse.json({ error: 'boom' }, { status: 500 })
      }),
    )
    const { result } = renderWithVariables([countryVar, cityVar], '/dash/d1')

    await waitFor(() => expect(result.current.optionState.city?.loading).toBe(true), { timeout: 3000 })
    await waitFor(() => expect(result.current.optionState.city?.error).toBeTruthy(), { timeout: 3000 })
    expect(result.current.optionState.city?.loading).toBe(false)
  })

  test('seeds static options immediately', () => {
    const regionVar: DashboardVariable = {
      name: 'region',
      type: 'single_select',
      options: {
        mode: 'static',
        values: [
          { label: 'EMEA', value: 'EMEA' },
          { label: 'AMER', value: 'AMER' },
        ],
      },
    }
    const { result } = renderWithVariables([regionVar], '/dash/d1')
    expect(result.current.optionState.region?.options).toHaveLength(2)
    expect(result.current.optionState.region?.loading).toBe(false)
  })
})
