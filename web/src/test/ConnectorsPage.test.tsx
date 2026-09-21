import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { ConnectorsPage } from '../pages/ConnectorsPage'
import { renderWithProviders } from './utils'

vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

const mockConnectors = [
  {
    id: 'c-1',
    name: 'Prod DB',
    type: 'postgres',
    is_default: true,
    config: { host: 'localhost', port: 5432, database: 'prod', user: 'user' },
    created_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'c-2',
    name: 'Staging DB',
    type: 'postgres',
    is_default: false,
    config: { host: 'localhost', port: 5432, database: 'staging', user: 'user' },
    created_at: '2026-01-01T00:00:00Z',
  },
]

beforeEach(() => {
  server.use(http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors)))
  vi.clearAllMocks()
})

describe('ConnectorsPage', () => {
  test('shows list of connectors', async () => {
    renderWithProviders(<ConnectorsPage />)
    expect(await screen.findByText('Prod DB')).toBeInTheDocument()
    expect(await screen.findByText('Staging DB')).toBeInTheDocument()
  })

  test('shows default badge on the default connector (T4.1)', async () => {
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Prod DB')
    expect(screen.getByText('Default')).toBeInTheDocument()
  })

  test('only one connector has default badge (T4.2)', async () => {
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Prod DB')
    const badges = screen.getAllByText('Default')
    expect(badges).toHaveLength(1)
  })

  test('shows an access-mode badge for managed and unmanaged connectors', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          { id: 'c-managed', name: 'Managed CH', type: 'clickhouse', warehouse_id: 'wh-1', config: {}, created_at: '2026-01-01T00:00:00Z' },
          { id: 'c-unmanaged', name: 'Shared CH', type: 'clickhouse', warehouse_id: null, config: {}, created_at: '2026-01-01T00:00:00Z' },
        ]),
      ),
    )
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Managed CH')
    expect(screen.getByText('Managed — table grants enforced')).toBeInTheDocument()
    expect(screen.getByText('Shared credential — not table-scoped')).toBeInTheDocument()
  })

  test('softens the managed badge while table permissions are disabled', async () => {
    server.use(
      http.get('/api/v1/auth/config', () =>
        HttpResponse.json({
          registration_disabled: false,
          warehouse_table_permissions_enabled: false,
        }),
      ),
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          { id: 'c-managed', name: 'Managed CH', type: 'clickhouse', warehouse_id: 'wh-1', config: {}, created_at: '2026-01-01T00:00:00Z' },
        ]),
      ),
    )
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Managed CH')
    expect(screen.getByText('Managed — table grants off')).toBeInTheDocument()
    expect(screen.queryByText('Managed — table grants enforced')).toBeNull()
  })

  test('link dialog does not promise provisioning while table permissions are disabled', async () => {
    server.use(
      http.get('/api/v1/auth/config', () =>
        HttpResponse.json({
          registration_disabled: false,
          warehouse_table_permissions_enabled: false,
        }),
      ),
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          { id: 'c-ch', name: 'CH RW', type: 'clickhouse', warehouse_id: null, config: {}, created_at: '2026-01-01T00:00:00Z' },
        ]),
      ),
    )
    renderWithProviders(<ConnectorsPage />)

    fireEvent.click(await screen.findByText('Link to warehouse'))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/AETHER_CH_TABLE_PERMISSIONS/)).toBeInTheDocument()
    expect(within(dialog).queryByText(/Provisioning begins immediately/)).toBeNull()
    expect(within(dialog).queryByText(/table grants are enforced by the warehouse/)).toBeNull()
  })

  test('links a clickhouse connector to a warehouse through the confirmation dialog', async () => {
    let putBody: unknown = null
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          { id: 'c-ch', name: 'CH RW', type: 'clickhouse', warehouse_id: null, config: {}, created_at: '2026-01-01T00:00:00Z' },
        ]),
      ),
      http.get('/api/v1/warehouses', () =>
        HttpResponse.json([
          {
            id: 'wh-1', org_id: 'org-1', name: 'Analytics', provisioner_connector_id: 'c-ch',
            sync_status: 'pending', sync_error: null, last_synced_at: null,
            created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
      http.put('/api/v1/connectors/c-ch/warehouse', async ({ request }) => {
        putBody = await request.json()
        return HttpResponse.json({ id: 'c-ch', warehouse_id: 'wh-1' })
      }),
    )
    renderWithProviders(<ConnectorsPage />)

    const linkButton = await screen.findByText('Link to warehouse')
    linkButton.focus()
    fireEvent.click(linkButton)
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('Link connector to warehouse')
    expect(dialog).toHaveFocus()
    fireEvent.change(screen.getByLabelText('Warehouse'), { target: { value: 'wh-1' } })
    fireEvent.click(screen.getByText('Link connector'))

    await waitFor(() => expect(putBody).toEqual({ warehouse_id: 'wh-1' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    // Focus returns to the control that opened the dialog.
    expect(linkButton).toHaveFocus()
  })
})
