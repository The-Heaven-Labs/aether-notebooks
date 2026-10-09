import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor, render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
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
    expect(screen.getByLabelText('Pattern')).toHaveValue('(')
  })

  test('merges the PUT response into the cached warehouse without dropping connectors', async () => {
    let puts = 0
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    // Distinguishing pre-mutation state: empty patterns, connectors present.
    qc.setQueryData<Warehouse>(['warehouse', 'wh-1'], {
      ...WAREHOUSE,
      hidden_table_patterns: [],
      connectors: CONNECTORS,
    })
    server.use(
      http.put('/api/v1/warehouses/wh-1', () => {
        puts++
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp'] })
      }),
    )
    render(
      <QueryClientProvider client={qc}>
        <WarehouseHiddenTables warehouseId="wh-1" patterns={[]} connectors={CONNECTORS} />
      </QueryClientProvider>,
    )
    fireEvent.change(await screen.findByLabelText('Pattern'), { target: { value: '_tmp' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(puts).toBe(1))

    const cached = qc.getQueryData<Warehouse>(['warehouse', 'wh-1'])
    // The new patterns prove the PUT response was merged into the cache...
    expect(cached?.hidden_table_patterns).toEqual(['_tmp'])
    // ...and the connectors prove the merge kept the GET-only field.
    expect(cached?.connectors).toEqual(CONNECTORS)
  })

  test('adds all non-empty lines in one PUT on Enter', async () => {
    let putBody: Record<string, unknown> | null = null
    let puts = 0
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        puts++
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp', '^raw\\.old', '^analytics\\.x'] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    const field = screen.getByLabelText('Pattern')
    fireEvent.change(field, { target: { value: '^raw\\.old\n\n  ^analytics\\.x  ' } })
    fireEvent.keyDown(field, { key: 'Enter' })
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: ['_tmp', '^raw\\.old', '^analytics\\.x'] }))
    expect(puts).toBe(1)
    expect(field).toHaveValue('')
  })

  test('dedupes lines against existing patterns and within the batch', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp', 'foo', 'bar'] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    fireEvent.change(screen.getByLabelText('Pattern'), { target: { value: '_tmp\nfoo\nfoo\n\nbar\n' } })
    fireEvent.keyDown(screen.getByLabelText('Pattern'), { key: 'Enter' })
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: ['_tmp', 'foo', 'bar'] }))
  })

  test('clears without a PUT when every line is already present', async () => {
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
    const field = screen.getByLabelText('Pattern')
    fireEvent.change(field, { target: { value: '_tmp\n_tmp' } })
    fireEvent.keyDown(field, { key: 'Enter' })
    expect(puts).toBe(0)
    expect(field).toHaveValue('')
  })

  test('Shift+Enter keeps the text and sends no request', async () => {
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
    const field = screen.getByLabelText('Pattern')
    fireEvent.change(field, { target: { value: '^a' } })
    fireEvent.keyDown(field, { key: 'Enter', shiftKey: true })
    expect(puts).toBe(0)
    expect(field).toHaveValue('^a')
  })

  test('does not intercept paste — lines stay editable until Enter', async () => {
    let puts = 0
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        puts++
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp', '^a', '^b'] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    const field = screen.getByLabelText('Pattern')
    // The component must not turn a paste into an immediate request.
    fireEvent.paste(field, { clipboardData: { getData: () => '^a\n^b' } })
    expect(puts).toBe(0)
    // Simulate what the browser would then place in the textarea.
    fireEvent.change(field, { target: { value: '^a\n^b' } })
    fireEvent.keyDown(field, { key: 'Enter' })
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: ['_tmp', '^a', '^b'] }))
  })

  test('pressing Enter in an empty field sends no request', async () => {
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
    const field = await screen.findByLabelText('Pattern')
    fireEvent.keyDown(field, { key: 'Enter' })
    expect(puts).toBe(0)
  })
})
