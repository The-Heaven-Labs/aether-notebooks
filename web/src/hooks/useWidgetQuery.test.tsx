import { describe, test, expect } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import type { ReactNode } from 'react'
import { server } from '../test/server'
import { useWidgetQuery } from './useWidgetQuery'
import type { Widget } from '../types'

const widget: Widget = {
  id: 'w1',
  dashboard_id: 'd1',
  connector_id: 'conn-1',
  query: 'SELECT 1 AS x',
  language: 'sql',
  type: 'table',
  layout: { row: 0, col: 0, width: 6, height: 6 },
  config: {},
  created_at: '2026-01-01T00:00:00Z',
}

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
}

describe('useWidgetQuery', () => {
  test('posts the widget id and variable values', async () => {
    let received: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/dashboards/d1/execute', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ outputs: [], metrics: {}, cached: false })
      }),
    )
    const { result } = renderHook(
      () => useWidgetQuery({ dashboardId: 'd1', widget, values: { region: 'EMEA' }, enabled: true }),
      { wrapper },
    )
    await waitFor(() => expect(result.current.data).toBeTruthy())
    expect(received).toMatchObject({ widget_id: 'w1', variables: { region: 'EMEA' } })
  })

  test('does not fetch when disabled', () => {
    let calls = 0
    server.use(
      http.post('/api/v1/dashboards/d1/execute', () => {
        calls += 1
        return HttpResponse.json({ outputs: [], metrics: {}, cached: false })
      }),
    )
    const { result } = renderHook(
      () => useWidgetQuery({ dashboardId: 'd1', widget, values: {}, enabled: false }),
      { wrapper },
    )
    expect(result.current.fetchStatus).toBe('idle')
    expect(calls).toBe(0)
  })

  test('refresh refetches with bypass_cache', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ outputs: [], metrics: {}, cached: false })
      }),
    )
    const { result } = renderHook(
      () => useWidgetQuery({ dashboardId: 'd1', widget, values: {}, enabled: true }),
      { wrapper },
    )
    await waitFor(() => expect(bodies.length).toBe(1))
    act(() => { result.current.refresh() })
    await waitFor(() => expect(bodies.length).toBe(2))
    expect(bodies[0].bypass_cache).toBe(false)
    expect(bodies[1].bypass_cache).toBe(true)
  })

  test('carries the viewer connector id in the request body', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ outputs: [], metrics: {}, cached: false })
      }),
    )
    const { result } = renderHook(
      () => useWidgetQuery({ dashboardId: 'd1', widget, values: {}, enabled: true, viewerConnectorId: 'conn-2' }),
      { wrapper },
    )
    await waitFor(() => expect(result.current.data).toBeTruthy())
    expect(bodies[0]).toMatchObject({ connector_id: 'conn-2' })
  })

  test('omits connector_id when no viewer selection is passed', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ outputs: [], metrics: {}, cached: false })
      }),
    )
    const { result } = renderHook(
      () => useWidgetQuery({ dashboardId: 'd1', widget, values: {}, enabled: true }),
      { wrapper },
    )
    await waitFor(() => expect(result.current.data).toBeTruthy())
    expect(bodies[0]).not.toHaveProperty('connector_id')
  })

  test('refetches when the viewer selection changes', async () => {
    const bodies: Array<Record<string, unknown>> = []
    server.use(
      http.post('/api/v1/dashboards/d1/execute', async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>)
        return HttpResponse.json({ outputs: [], metrics: {}, cached: false })
      }),
    )
    let viewer: string | null = null
    const { rerender } = renderHook(
      () => useWidgetQuery({ dashboardId: 'd1', widget, values: {}, enabled: true, viewerConnectorId: viewer }),
      { wrapper },
    )
    await waitFor(() => expect(bodies.length).toBe(1))
    expect(bodies[0]).not.toHaveProperty('connector_id')
    viewer = 'conn-2'
    rerender()
    await waitFor(() => expect(bodies.length).toBe(2))
    expect(bodies[1]).toMatchObject({ connector_id: 'conn-2' })
  })
})
