import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { WarehouseHiddenTables } from './WarehouseHiddenTables'
import type { Warehouse, WarehouseConnector, WarehouseGrant } from '../api/warehouses'

const CONNECTORS: WarehouseConnector[] = [
  { id: 'c-1', name: 'CH RW', type: 'clickhouse', is_provisioner: true },
]

const WAREHOUSE: Warehouse = {
  id: 'wh-1', org_id: 'org-1', name: 'WH', provisioner_connector_id: 'c-1',
  allow_provisioner_execution: false, hidden_table_patterns: ['_tmp'],
  sync_status: 'ready', sync_error: null, last_synced_at: null,
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
}

const GRANTS: WarehouseGrant[] = [
  {
    id: 'gr-1', org_id: 'org-1', warehouse_id: 'wh-1', subject_type: 'everyone', subject_id: 'everyone',
    database: 'analytics', table: '_tmp_kept', created_by: null, created_at: '2026-01-01T00:00:00Z',
  },
]

const SCHEMA = {
  tables: [
    { schema: 'analytics', name: 'events', columns: [] },
    { schema: 'analytics', name: '_tmp_scratch', columns: [] },
    { schema: 'analytics', name: '_tmp_kept', columns: [] },
  ],
}

beforeEach(() => {
  server.use(
    http.get('/api/v1/connectors/c-1/schema', () => HttpResponse.json(SCHEMA)),
    http.get('/api/v1/warehouses/wh-1/grants', () => HttpResponse.json(GRANTS)),
  )
})

describe('WarehouseHiddenTables', () => {
  test('renders patterns and counts matches excluding already-granted tables', async () => {
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    expect(await screen.findByText('_tmp')).toBeInTheDocument()
    // Two tables match `_tmp`, but one is already granted and stays visible.
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
