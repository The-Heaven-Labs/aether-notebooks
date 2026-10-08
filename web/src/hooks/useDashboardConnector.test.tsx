import type { ReactNode } from 'react'
import { act, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { beforeEach, describe, expect, it } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { useDashboardConnector } from './useDashboardConnector'

// The hook reads the shared ['connectors'] cache through useQueryClient.
function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  }
}

function clickhouseConnector(id: string, warehouseID: string) {
  return {
    id,
    name: `CH ${id}`,
    type: 'clickhouse',
    warehouse_id: warehouseID,
    can_use: true,
    created_at: '2026-01-01T00:00:00Z',
  }
}

function accessBody(
  warehouseID: string,
  preferred: string | null,
  serviceIDs: string[] = preferred ? [preferred] : [],
) {
  return {
    user_id: 'user-1',
    warehouse_id: warehouseID,
    tables: [],
    services: serviceIDs.map(id => ({
      connector_id: id,
      name: `Service ${id}`,
      preferred: id === preferred,
    })),
    preferred_connector_id: preferred,
  }
}

describe('useDashboardConnector', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('starts empty and persists the viewer choice per user+dashboard', async () => {
    const { result, rerender } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.resolving).toBe(false))
    expect(result.current.selected).toBeNull()
    act(() => { result.current.select('conn-2') })
    rerender()
    expect(result.current.selected).toBe('conn-2')
    expect(localStorage.getItem('aether_dash_connector:user-1:dash-1')).toBe('conn-2')
  })

  it('reads the persisted choice for the same user+dashboard', async () => {
    localStorage.setItem('aether_dash_connector:user-1:dash-1', 'conn-2')
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.resolving).toBe(false))
    expect(result.current.selected).toBe('conn-2')
  })

  it('removes the stored choice when cleared', async () => {
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.resolving).toBe(false))
    act(() => { result.current.select('conn-2') })
    expect(localStorage.getItem('aether_dash_connector:user-1:dash-1')).toBe('conn-2')
    act(() => { result.current.select(null) })
    expect(result.current.selected).toBeNull()
    expect(localStorage.getItem('aether_dash_connector:user-1:dash-1')).toBeNull()
  })

  it('re-reads the persisted choice when the dashboard changes', async () => {
    localStorage.setItem('aether_dash_connector:user-1:dash-2', 'conn-7')
    const { result, rerender } = renderHook(
      ({ dashboardID }) => useDashboardConnector(dashboardID, 'user-1'),
      { initialProps: { dashboardID: 'dash-1' }, wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.resolving).toBe(false))
    expect(result.current.selected).toBeNull()
    rerender({ dashboardID: 'dash-2' })
    expect(result.current.selected).toBe('conn-7')
  })

  it('does not fetch and is not resolving without a user id', () => {
    let calls = 0
    server.use(
      http.get('/api/v1/connectors', () => {
        calls += 1
        return HttpResponse.json([])
      }),
    )
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', ''),
      { wrapper: createWrapper() },
    )
    expect(result.current.resolving).toBe(false)
    expect(calls).toBe(0)
  })

  it('exposes the sole warehouse preference as the initial suggestion', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([clickhouseConnector('conn-9', 'w1')])),
      http.get('/api/v1/warehouses/w1/effective-access', () =>
        HttpResponse.json(accessBody('w1', 'conn-9')),
      ),
    )
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    expect(result.current.resolving).toBe(true)
    await waitFor(() => expect(result.current.suggestion).toBe('conn-9'))
    expect(result.current.resolving).toBe(false)
  })

  it('suggests the live preference when only one of several warehouses has one', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          clickhouseConnector('conn-9', 'w1'),
          clickhouseConnector('conn-7', 'w2'),
        ]),
      ),
      http.get('/api/v1/warehouses/w1/effective-access', () =>
        HttpResponse.json(accessBody('w1', 'conn-9')),
      ),
      http.get('/api/v1/warehouses/w2/effective-access', () =>
        HttpResponse.json(accessBody('w2', null)),
      ),
    )
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.suggestion).toBe('conn-9'))
  })

  it('gives no suggestion when several warehouses each have a live preference', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          clickhouseConnector('conn-9', 'w1'),
          clickhouseConnector('conn-7', 'w2'),
        ]),
      ),
      http.get('/api/v1/warehouses/w1/effective-access', () =>
        HttpResponse.json(accessBody('w1', 'conn-9')),
      ),
      http.get('/api/v1/warehouses/w2/effective-access', () =>
        HttpResponse.json(accessBody('w2', 'conn-7')),
      ),
    )
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.resolving).toBe(false))
    expect(result.current.suggestion).toBeNull()
  })

  it('gives no suggestion when no warehouse has a preference', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([clickhouseConnector('conn-9', 'w1')])),
      http.get('/api/v1/warehouses/w1/effective-access', () =>
        HttpResponse.json(accessBody('w1', null)),
      ),
    )
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.resolving).toBe(false))
    expect(result.current.suggestion).toBeNull()
  })

  it('ignores a stale preference that no longer names a granted service', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([clickhouseConnector('conn-9', 'w1')])),
      http.get('/api/v1/warehouses/w1/effective-access', () =>
        HttpResponse.json(accessBody('w1', 'conn-9', [])),
      ),
    )
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.resolving).toBe(false))
    expect(result.current.suggestion).toBeNull()
  })

  it('excludes non-ClickHouse and unusable connectors from discovery', async () => {
    const accessCalls: string[] = []
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          clickhouseConnector('conn-9', 'w1'),
          { ...clickhouseConnector('conn-x', 'w2'), type: 'postgres' },
          { ...clickhouseConnector('conn-y', 'w3'), can_use: false },
        ]),
      ),
      http.get('/api/v1/warehouses/w1/effective-access', () => {
        accessCalls.push('w1')
        return HttpResponse.json(accessBody('w1', 'conn-9'))
      }),
      http.get('/api/v1/warehouses/w2/effective-access', () => {
        accessCalls.push('w2')
        return HttpResponse.json(accessBody('w2', 'conn-x'))
      }),
      http.get('/api/v1/warehouses/w3/effective-access', () => {
        accessCalls.push('w3')
        return HttpResponse.json(accessBody('w3', 'conn-y'))
      }),
    )
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.suggestion).toBe('conn-9'))
    expect(accessCalls).toEqual(['w1'])
  })

  it('clears a previous suggestion when the user changes', async () => {
    let preference: string | null = 'conn-9'
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([clickhouseConnector('conn-9', 'w1')])),
      http.get('/api/v1/warehouses/w1/effective-access', () =>
        HttpResponse.json(accessBody('w1', preference)),
      ),
    )
    const { result, rerender } = renderHook(
      ({ userID }) => useDashboardConnector('dash-1', userID),
      { initialProps: { userID: 'user-1' }, wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.suggestion).toBe('conn-9'))
    preference = null
    rerender({ userID: 'user-2' })
    expect(result.current.resolving).toBe(true)
    expect(result.current.suggestion).toBeNull()
    await waitFor(() => expect(result.current.resolving).toBe(false))
    expect(result.current.suggestion).toBeNull()
  })

  it('stops resolving without a suggestion when the connectors fetch fails', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json({ error: 'boom' }, { status: 500 })),
    )
    const { result } = renderHook(
      () => useDashboardConnector('dash-1', 'user-1'),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.resolving).toBe(false))
    expect(result.current.suggestion).toBeNull()
  })
})
