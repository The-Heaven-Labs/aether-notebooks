import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { RoutingPreference } from './RoutingPreference'
import { type WarehouseEffectiveAccess } from '../api/warehouses'

const ACCESS: WarehouseEffectiveAccess = {
  user_id: 'user-1',
  warehouse_id: 'wh-1',
  tables: [{ database: 'analytics', table: 'events' }],
  services: [
    { connector_id: 'c-1', name: 'CH RO', preferred: false },
    { connector_id: 'c-2', name: 'CH RW', preferred: true },
  ],
  preferred_connector_id: 'c-2',
}

const EMPTY_ACCESS: WarehouseEffectiveAccess = {
  user_id: 'user-1',
  warehouse_id: 'wh-1',
  tables: [],
  services: [],
  preferred_connector_id: null,
}

function renderPreference(warehouseName = 'Analytics WH') {
  return renderWithProviders(
    <RoutingPreference warehouseId="wh-1" warehouseName={warehouseName} />,
  )
}

beforeEach(() => {
  server.use(
    http.get('/api/v1/warehouses/wh-1/effective-access', () =>
      HttpResponse.json(ACCESS),
    ),
  )
})

describe('RoutingPreference', () => {
  test('lists only permitted services and marks the current preference', async () => {
    renderPreference()

    expect(await screen.findByRole('option', { name: 'CH RO' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'CH RW (current)' })).toBeInTheDocument()
    expect(screen.getByLabelText('Preferred service for Analytics WH')).toHaveValue('c-2')
    expect(screen.getByRole('option', { name: 'No default' })).toBeInTheDocument()
  })

  test('renders the no-default option without clipping', async () => {
    renderPreference()

    const select = await screen.findByLabelText('Preferred service for Analytics WH')
    expect(screen.getByRole('option', { name: 'No default' })).toBeInTheDocument()
    expect(select).toHaveStyle('max-width: 320px')
  })

  test('saves the chosen service and refetches effective access', async () => {
    let preferred: string | null = 'c-2'
    let puts: Array<Record<string, unknown>> = []
    let gets = 0
    server.use(
      http.get('/api/v1/warehouses/wh-1/effective-access', () => {
        gets++
        return HttpResponse.json({
          ...ACCESS,
          preferred_connector_id: preferred,
          services: ACCESS.services.map((s) => ({
            ...s,
            preferred: s.connector_id === preferred,
          })),
        } satisfies WarehouseEffectiveAccess)
      }),
      http.put('/api/v1/warehouses/wh-1/preference', async ({ request }) => {
        const body = await request.json() as { connector_id: string | null }
        puts = [...puts, body]
        preferred = body.connector_id
        return HttpResponse.json({
          user_id: 'user-1', warehouse_id: 'wh-1', connector_id: preferred,
        })
      }),
    )
    renderPreference()
    await screen.findByRole('option', { name: 'CH RO' })

    fireEvent.change(screen.getByLabelText('Preferred service for Analytics WH'), {
      target: { value: 'c-1' },
    })

    await waitFor(() => expect(puts).toEqual([{ connector_id: 'c-1' }]))
    await waitFor(() =>
      expect(screen.getByLabelText('Preferred service for Analytics WH')).toHaveValue('c-1'),
    )
    expect(gets).toBeGreaterThanOrEqual(2)
  })

  test('clears the preference back to automatic', async () => {
    let puts: Array<Record<string, unknown>> = []
    server.use(
      http.put('/api/v1/warehouses/wh-1/preference', async ({ request }) => {
        const body = await request.json() as { connector_id: string | null }
        puts = [...puts, body]
        return HttpResponse.json({ user_id: 'user-1', warehouse_id: 'wh-1', connector_id: null })
      }),
    )
    renderPreference()
    await screen.findByRole('option', { name: 'CH RO' })

    fireEvent.change(screen.getByLabelText('Preferred service for Analytics WH'), {
      target: { value: '' },
    })

    await waitFor(() => expect(puts).toEqual([{ connector_id: null }]))
  })

  test('shows a disabled empty state when no service is permitted', async () => {
    server.use(
      http.get('/api/v1/warehouses/wh-1/effective-access', () =>
        HttpResponse.json(EMPTY_ACCESS),
      ),
    )
    renderPreference()

    const select = await screen.findByLabelText('Preferred service for Analytics WH')
    expect(select).toBeDisabled()
    expect(screen.getByRole('option', { name: 'No permitted services' })).toBeInTheDocument()
    expect(screen.getByText(/No service access/)).toBeInTheDocument()
  })

  test('surfaces a failed save', async () => {
    server.use(
      http.put('/api/v1/warehouses/wh-1/preference', () =>
        HttpResponse.json({ error: 'no access to the selected service' }, { status: 403 }),
      ),
    )
    renderPreference()
    await screen.findByRole('option', { name: 'CH RO' })

    fireEvent.change(screen.getByLabelText('Preferred service for Analytics WH'), {
      target: { value: 'c-1' },
    })

    expect(await screen.findByText('no access to the selected service')).toBeInTheDocument()
  })

  test('shows a load error state', async () => {
    server.use(
      http.get('/api/v1/warehouses/wh-1/effective-access', () =>
        HttpResponse.json({ error: 'query failed' }, { status: 500 }),
      ),
    )
    renderPreference()

    expect(await screen.findByText('Failed to load services')).toBeInTheDocument()
  })
})

