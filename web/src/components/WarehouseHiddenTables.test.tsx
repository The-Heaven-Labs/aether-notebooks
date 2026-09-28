import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { WarehouseHiddenTables } from './WarehouseHiddenTables'
import type { Warehouse, WarehouseConnector } from '../api/warehouses'

const CONNECTORS: WarehouseConnector[] = [
  { id: 'c-1', name: 'CH RW', type: 'clickhouse', is_provisioner: true },
]

const WAREHOUSE: Warehouse = {
  id: 'wh-1', org_id: 'org-1', name: 'WH', provisioner_connector_id: 'c-1',
  allow_provisioner_execution: false, hidden_table_patterns: ['_tmp'],
  sync_status: 'ready', sync_error: null, last_synced_at: null,
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
}

const SCHEMA = {
  tables: [
    { schema: 'analytics', name: 'events', columns: [] },
    { schema: 'analytics', name: '_tmp_scratch', columns: [] },
    { schema: 'analytics', name: '_tmp_kept', columns: [] },
  ],
  hidden_tables: 1,
}

beforeEach(() => {
  server.use(
    http.get('/api/v1/connectors/c-1/schema', () => HttpResponse.json(SCHEMA)),
  )
})

describe('WarehouseHiddenTables', () => {
  test('renders patterns and the server-reported hidden count', async () => {
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    expect(await screen.findByText('_tmp')).toBeInTheDocument()
    expect(await screen.findByText(/hides 1 table/)).toBeInTheDocument()
  })

  test('adds a pattern through PUT /warehouses/{id}', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp', '^raw\\.old'] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    fireEvent.change(screen.getByLabelText('Pattern'), { target: { value: '^raw\\.old' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: ['_tmp', '^raw\\.old'] }))
    expect(screen.getByLabelText('Pattern')).toHaveValue('')
  })

  test('removes a pattern through PUT /warehouses/{id}', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: [] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    fireEvent.click(screen.getByRole('button', { name: 'Remove pattern _tmp' }))
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: [] }))
  })

  test('clears the input and does not PUT when adding a duplicate pattern', async () => {
    let puts = 0
    server.use(
      http.put('/api/v1/warehouses/wh-1', () => {
        puts++
        return HttpResponse.json(WAREHOUSE)
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    fireEvent.change(screen.getByLabelText('Pattern'), { target: { value: '_tmp' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(screen.getByLabelText('Pattern')).toHaveValue('')
    expect(puts).toBe(0)
  })

  test('disables Add and sends no PUT for empty input', async () => {
    let puts = 0
    server.use(
      http.put('/api/v1/warehouses/wh-1', () => {
        puts++
        return HttpResponse.json(WAREHOUSE)
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={[]} connectors={CONNECTORS} />,
    )
    const addButton = await screen.findByRole('button', { name: 'Add' })
    expect(addButton).toBeDisabled()
    fireEvent.click(addButton)
    expect(puts).toBe(0)
  })

  test('pluralizes the server-reported hidden count', async () => {
    server.use(
      http.get('/api/v1/connectors/c-1/schema', () =>
        HttpResponse.json({ ...SCHEMA, hidden_tables: 2 }),
      ),
    )
    renderWithProviders(
      <WarehouseHiddenTables
        warehouseId="wh-1"
        patterns={['^analytics\\._tmp', '^analytics\\.events$']}
        connectors={CONNECTORS}
      />,
    )
    expect(await screen.findByText(/hides 2 tables/)).toBeInTheDocument()
  })

  test('reports zero hidden tables when the server reports none', async () => {
    server.use(
      http.get('/api/v1/connectors/c-1/schema', () =>
        HttpResponse.json({ ...SCHEMA, hidden_tables: 0 }),
      ),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={['^raw\\.']} connectors={CONNECTORS} />,
    )
    expect(await screen.findByText(/hides 0 tables/)).toBeInTheDocument()
  })

  test('shows the server validation error for an invalid pattern', async () => {
    server.use(
      http.put('/api/v1/warehouses/wh-1', () =>
        HttpResponse.json({ error: 'invalid pattern "(": error parsing regexp' }, { status: 400 }),
      ),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={[]} connectors={CONNECTORS} />,
    )
    fireEvent.change(await screen.findByLabelText('Pattern'), { target: { value: '(' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findByText(/invalid pattern/)).toBeInTheDocument()
  })
})
