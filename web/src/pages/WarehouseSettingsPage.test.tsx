import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { WarehouseSettingsPage } from './WarehouseSettingsPage'
import { renderWithProviders } from '../test/utils'

vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

const WAREHOUSES = [
  {
    id: 'wh-1', org_id: 'org-1', name: 'Analytics', provisioner_connector_id: 'c-1',
    sync_status: 'ready', sync_error: null, last_synced_at: '2026-01-01T00:00:00Z',
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'wh-2', org_id: 'org-1', name: 'Beta', provisioner_connector_id: null,
    sync_status: 'error', sync_error: 'wildcard grant detected', last_synced_at: null,
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  },
]

const DETAIL_CONNECTORS = [
  { id: 'c-1', name: 'CH RW', type: 'clickhouse', is_provisioner: true },
  { id: 'c-3', name: 'CH RO', type: 'clickhouse', is_provisioner: false },
]

const CONNECTORS = [
  { id: 'c-1', name: 'CH RW', type: 'clickhouse', warehouse_id: 'wh-1', config: { host: 'ch-1' }, created_at: '2026-01-01T00:00:00Z' },
  { id: 'c-2', name: 'CH Unlinked', type: 'clickhouse', warehouse_id: null, config: { host: 'ch-2' }, created_at: '2026-01-01T00:00:00Z' },
  { id: 'c-3', name: 'CH RO', type: 'clickhouse', warehouse_id: 'wh-1', config: { host: 'ch-3' }, created_at: '2026-01-01T00:00:00Z' },
]

beforeEach(() => {
  vi.clearAllMocks()
  server.use(
    http.get('/api/v1/warehouses', () => HttpResponse.json(WAREHOUSES)),
    http.get('/api/v1/connectors', () => HttpResponse.json(CONNECTORS)),
    http.get('/api/v1/warehouses/:id', ({ params }) => {
      const warehouse = WAREHOUSES.find((w) => w.id === params.id)
      if (!warehouse) return HttpResponse.json({ error: 'warehouse not found' }, { status: 404 })
      return HttpResponse.json({
        ...warehouse,
        connectors: params.id === 'wh-1' ? DETAIL_CONNECTORS : [],
      })
    }),
    http.get('/api/v1/warehouses/:id/grants', () => HttpResponse.json([])),
    http.get('/api/v1/warehouses/:id/new-tables', ({ params }) =>
      HttpResponse.json({
        warehouse_id: params.id,
        since: '2026-01-01T00:00:00Z',
        truncated: false,
        tables: [],
      }),
    ),
    http.get('/api/v1/warehouses/:id/validation', ({ params }) =>
      HttpResponse.json({
        warehouse_id: params.id,
        truncated: false,
        tables_without_service_access: [],
        service_access_without_tables: [],
      }),
    ),
  )
})

describe('WarehouseSettingsPage', () => {
  test('renders warehouses with their sync status', async () => {
    renderWithProviders(<WarehouseSettingsPage />)
    expect(await screen.findByText('Analytics')).toBeInTheDocument()
    expect(screen.getByText('Beta')).toBeInTheDocument()
    expect(screen.getByText('Ready')).toBeInTheDocument()
    expect(screen.getByText('Error')).toBeInTheDocument()
  })

  test('does not claim enforcement while table permissions are disabled', async () => {
    server.use(
      http.get('/api/v1/auth/config', () =>
        HttpResponse.json({
          registration_disabled: false,
          warehouse_table_permissions_enabled: false,
        }),
      ),
    )
    renderWithProviders(<WarehouseSettingsPage />)
    await screen.findByText('Analytics')
    expect(screen.getByText(/table permissions are currently disabled/)).toBeInTheDocument()
    expect(screen.queryByText(/table grants enforced by ClickHouse/)).toBeNull()

    // The delete confirmation must not promise identity revocation either.
    fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    expect(
      await screen.findByText(/any provisioned identities are left in place/),
    ).toBeInTheDocument()
  })

  test('creates a warehouse from the inline form', async () => {
    let posted: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/warehouses', async ({ request }) => {
        posted = await request.json() as Record<string, unknown>
        return HttpResponse.json(
          {
            id: 'wh-new', org_id: 'org-1', name: 'New Wh', provisioner_connector_id: null,
            sync_status: 'pending', sync_error: null, last_synced_at: null,
            created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
          },
          { status: 201 },
        )
      }),
    )
    renderWithProviders(<WarehouseSettingsPage />)
    await screen.findByText('Analytics')

    fireEvent.click(screen.getByText('+ New Warehouse'))
    fireEvent.change(screen.getByPlaceholderText('Analytics Warehouse'), {
      target: { value: 'New Wh' },
    })
    fireEvent.click(screen.getByText('Create'))

    await waitFor(() =>
      expect(posted).toEqual({ name: 'New Wh', provisioner_connector_id: null }),
    )
  })

  test('expands a warehouse to show provisioner, connectors, and grants', async () => {
    renderWithProviders(<WarehouseSettingsPage />)
    fireEvent.click(await screen.findByText('Analytics'))

    expect(await screen.findByLabelText('Provisioner connector')).toBeInTheDocument()
    expect(screen.getByText('Provisioner')).toBeInTheDocument()
    expect(screen.getAllByText('CH RW').length).toBeGreaterThan(0)
    expect(await screen.findByText('Table grants')).toBeInTheDocument()
    expect(await screen.findByText('No table grants yet.')).toBeInTheDocument()
    expect(await screen.findByText('New tables')).toBeInTheDocument()
    expect(await screen.findByText('No new tables since your last review.')).toBeInTheDocument()
  })

  test('changes the provisioner through the selector', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1/provisioner', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSES[0], provisioner_connector_id: 'c-3' })
      }),
    )
    renderWithProviders(<WarehouseSettingsPage />)
    fireEvent.click(await screen.findByText('Analytics'))

    const select = await screen.findByLabelText('Provisioner connector')
    fireEvent.change(select, { target: { value: 'c-3' } })

    await waitFor(() => expect(putBody).toEqual({ connector_id: 'c-3' }))
  })

  test('renames a warehouse through the inline input', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSES[0], name: 'Analytics 2' })
      }),
    )
    renderWithProviders(<WarehouseSettingsPage />)
    fireEvent.click(await screen.findByText('Analytics'))
    await screen.findByLabelText('Provisioner connector')

    fireEvent.click(screen.getAllByRole('button', { name: 'Rename' })[0])
    const input = screen.getByLabelText('Warehouse name')
    fireEvent.change(input, { target: { value: 'Analytics 2' } })
    fireEvent.keyDown(input, { key: 'Enter' })

    await waitFor(() => expect(putBody).toEqual({ name: 'Analytics 2' }))
    // Enter commits the rename without collapsing the card.
    expect(screen.getByLabelText('Provisioner connector')).toBeInTheDocument()
  })

  test('unlinking a connector refreshes the expanded warehouse', async () => {
    let putBody: unknown = null
    let detailCalls = 0
    server.use(
      http.get('/api/v1/warehouses/:id', ({ params }) => {
        detailCalls++
        const warehouse = WAREHOUSES.find((w) => w.id === params.id)
        if (!warehouse) return HttpResponse.json({ error: 'warehouse not found' }, { status: 404 })
        return HttpResponse.json({
          ...warehouse,
          connectors: params.id === 'wh-1' ? DETAIL_CONNECTORS : [],
        })
      }),
      http.put('/api/v1/connectors/c-3/warehouse', async ({ request }) => {
        putBody = await request.json()
        return HttpResponse.json({ id: 'c-3', warehouse_id: null })
      }),
    )
    renderWithProviders(<WarehouseSettingsPage />)
    fireEvent.click(await screen.findByText('Analytics'))
    await screen.findAllByText('CH RO')

    fireEvent.click(screen.getAllByText('Remove')[1])
    fireEvent.click(await screen.findByRole('button', { name: 'Unlink' }))

    await waitFor(() => expect(putBody).toEqual({ warehouse_id: null }))
    await waitFor(() => expect(detailCalls).toBeGreaterThanOrEqual(2))
  })

  test('adding a grant refreshes the warehouse list', async () => {
    let listCalls = 0
    server.use(
      http.get('/api/v1/warehouses', () => {
        listCalls++
        return HttpResponse.json(WAREHOUSES)
      }),
      http.get('/api/v1/connectors/c-1/schema', () =>
        HttpResponse.json({
          tables: [{ schema: 'analytics', name: 'events', columns: [] }],
        }),
      ),
      http.post('/api/v1/warehouses/wh-1/grants', async ({ request }) => {
        const body = await request.json() as Record<string, unknown>
        return HttpResponse.json(
          {
            id: 'gr-1', org_id: 'org-1', warehouse_id: 'wh-1',
            subject_type: body.subject_type, subject_id: body.subject_id,
            subject_name: 'Everyone', database: body.database, table: body.table,
            created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
          },
          { status: 201 },
        )
      }),
    )
    renderWithProviders(<WarehouseSettingsPage />)
    fireEvent.click(await screen.findByText('Analytics'))
    await screen.findByText('Table grants')
    expect(listCalls).toBe(1)

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'everyone:everyone' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() =>
      expect(screen.getByRole('option', { name: 'events' })).toBeInTheDocument(),
    )
    fireEvent.change(screen.getByLabelText('Table'), { target: { value: 'events' } })
    fireEvent.click(screen.getByText('Add grant'))

    await waitFor(() => expect(listCalls).toBeGreaterThanOrEqual(2))
  })

  test('deleting a warehouse with linked connectors requires force', async () => {
    const deleteUrls: string[] = []
    let listCalls = 0
    server.use(
      http.get('/api/v1/warehouses', () => {
        listCalls++
        return HttpResponse.json(WAREHOUSES)
      }),
      http.delete('/api/v1/warehouses/:id', ({ request }) => {
        deleteUrls.push(request.url)
        if (!request.url.includes('force=true')) {
          return HttpResponse.json(
            {
              error: 'warehouse has 1 linked connector(s); deleting it unlinks them and returns them to shared-credential mode. Re-send with force=true to confirm',
              connector_count: 1,
            },
            { status: 409 },
          )
        }
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderWithProviders(<WarehouseSettingsPage />)
    await screen.findByText('Analytics')
    expect(listCalls).toBe(1)

    fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0])
    const confirmButtons = screen.getAllByRole('button', { name: 'Delete' })
    fireEvent.click(confirmButtons[confirmButtons.length - 1])

    expect(await screen.findByText('Warehouse has linked connectors')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Delete and unlink' }))

    await waitFor(() => expect(deleteUrls.length).toBe(2))
    expect(deleteUrls[0]).not.toContain('force=true')
    expect(deleteUrls[1]).toContain('force=true')
    // The successful force-delete closes the dialog and refreshes the list.
    await waitFor(() =>
      expect(screen.queryByText('Warehouse has linked connectors')).toBeNull(),
    )
    await waitFor(() => expect(listCalls).toBeGreaterThanOrEqual(2))
  })

  test('shows an error banner when the list request fails', async () => {
    server.use(
      http.get('/api/v1/warehouses', () =>
        HttpResponse.json({ error: 'boom' }, { status: 500 }),
      ),
    )
    renderWithProviders(<WarehouseSettingsPage />)
    expect(await screen.findByText('boom')).toBeInTheDocument()
  })
})
