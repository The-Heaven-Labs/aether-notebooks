import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { useDashboardConnector } from './useDashboardConnector'

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

  it('starts empty and persists the viewer choice per user+dashboard', () => {
    const { result, rerender } = renderHook(() => useDashboardConnector('dash-1', 'user-1'))
    expect(result.current.selected).toBeNull()
    act(() => { result.current.select('conn-2') })
    rerender()
    expect(result.current.selected).toBe('conn-2')
    expect(localStorage.getItem('aether_dash_connector:user-1:dash-1')).toBe('conn-2')
  })

  it('reads the persisted choice for the same user+dashboard', () => {
    localStorage.setItem('aether_dash_connector:user-1:dash-1', 'conn-2')
    const { result } = renderHook(() => useDashboardConnector('dash-1', 'user-1'))
    expect(result.current.selected).toBe('conn-2')
  })

  it('re-reads the persisted choice when the dashboard changes', () => {
    localStorage.setItem('aether_dash_connector:user-1:dash-2', 'conn-7')
    const { result, rerender } = renderHook(
      ({ dashboardID }) => useDashboardConnector(dashboardID, 'user-1'),
      { initialProps: { dashboardID: 'dash-1' } },
    )
    expect(result.current.selected).toBeNull()
    rerender({ dashboardID: 'dash-2' })
    expect(result.current.selected).toBe('conn-7')
  })

  it('does not fetch without a user id', () => {
    let calls = 0
    server.use(
      http.get('/api/v1/connectors', () => {
        calls += 1
        return HttpResponse.json([])
      }),
    )
    renderHook(() => useDashboardConnector('dash-1', ''))
    expect(calls).toBe(0)
  })

  it('exposes the sole warehouse preference as the initial suggestion', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([clickhouseConnector('conn-9', 'w1')])),
      http.get('/api/v1/warehouses/w1/effective-access', () =>
        HttpResponse.json(accessBody('w1', 'conn-9')),
      ),
    )
    const { result } = renderHook(() => useDashboardConnector('dash-1', 'user-1'))
    await waitFor(() => expect(result.current.suggestion).toBe('conn-9'))
  })

  it('gives no suggestion when several warehouses each have a live preference', async () => {
    let accessCalls = 0
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          clickhouseConnector('conn-9', 'w1'),
          clickhouseConnector('conn-7', 'w2'),
        ]),
      ),
      http.get('/api/v1/warehouses/w1/effective-access', () => {
        accessCalls += 1
        return HttpResponse.json(accessBody('w1', 'conn-9'))
      }),
      http.get('/api/v1/warehouses/w2/effective-access', () => {
        accessCalls += 1
        return HttpResponse.json(accessBody('w2', 'conn-7'))
      }),
    )
    const { result } = renderHook(() => useDashboardConnector('dash-1', 'user-1'))
    await waitFor(() => expect(accessCalls).toBe(2))
    expect(result.current.suggestion).toBeNull()
  })

  it('gives no suggestion when no warehouse has a preference', async () => {
    let accessCalls = 0
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([clickhouseConnector('conn-9', 'w1')])),
      http.get('/api/v1/warehouses/w1/effective-access', () => {
        accessCalls += 1
        return HttpResponse.json(accessBody('w1', null))
      }),
    )
    const { result } = renderHook(() => useDashboardConnector('dash-1', 'user-1'))
    await waitFor(() => expect(accessCalls).toBe(1))
    expect(result.current.suggestion).toBeNull()
  })

  it('ignores a stale preference that no longer names a granted service', async () => {
    let accessCalls = 0
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([clickhouseConnector('conn-9', 'w1')])),
      http.get('/api/v1/warehouses/w1/effective-access', () => {
        accessCalls += 1
        return HttpResponse.json(accessBody('w1', 'conn-9', []))
      }),
    )
    const { result } = renderHook(() => useDashboardConnector('dash-1', 'user-1'))
    await waitFor(() => expect(accessCalls).toBe(1))
    expect(result.current.suggestion).toBeNull()
  })
})
