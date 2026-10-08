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

  test('renders a dash when a connector has no database', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          {
            id: 'c-ch', name: 'CH No DB', type: 'clickhouse', is_default: false,
            config: { host: 'localhost', port: 9000, database: '' },
            created_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
    )
    renderWithProviders(<ConnectorsPage />)
    const row = (await screen.findByText('CH No DB')).closest('tr')
    expect(row).not.toBeNull()
    expect(within(row as HTMLElement).getByText('—')).toBeInTheDocument()
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

    fireEvent.click(await screen.findByLabelText('Link to warehouse'))
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
            allow_provisioner_execution: false, hidden_table_patterns: [],
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

    const linkButton = await screen.findByLabelText('Link to warehouse')
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

  test('creates a Databricks connector with PAT config (T-DBX)', async () => {
    let postBody: { config?: Record<string, unknown> } | null = null
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([])),
      http.post('/api/v1/connectors', async ({ request }) => {
        postBody = (await request.json()) as { config?: Record<string, unknown> }
        return HttpResponse.json(
          { id: 'c-dbx', name: 'DBX', type: 'databricks', config: {}, created_at: '2026-01-01T00:00:00Z' },
          { status: 201 },
        )
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByText('+ New Connector'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'DBX' } })
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'databricks' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'dbc-x.cloud.databricks.com' } })
    fireEvent.change(screen.getByLabelText('HTTP Path'), { target: { value: '/sql/1.0/warehouses/abc' } })
    fireEvent.change(screen.getByLabelText('Token'), { target: { value: 'dapi-123' } })
    fireEvent.click(screen.getByText('Create'))

    await waitFor(() =>
      expect(postBody?.config).toEqual({
        host: 'dbc-x.cloud.databricks.com',
        http_path: '/sql/1.0/warehouses/abc',
        auth_type: 'pat',
        catalog: '',
        schema: '',
        token: 'dapi-123',
      }),
    )
  })

  test('creates a Databricks connector with OAuth M2M config (T-DBX-2)', async () => {
    let postBody: { config?: Record<string, unknown> } | null = null
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([])),
      http.post('/api/v1/connectors', async ({ request }) => {
        postBody = (await request.json()) as { config?: Record<string, unknown> }
        return HttpResponse.json(
          { id: 'c-dbx2', name: 'DBX M2M', type: 'databricks', config: {}, created_at: '2026-01-01T00:00:00Z' },
          { status: 201 },
        )
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByText('+ New Connector'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'DBX M2M' } })
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'databricks' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'dbc-x.cloud.databricks.com' } })
    fireEvent.change(screen.getByLabelText('HTTP Path'), { target: { value: '/sql/1.0/warehouses/abc' } })
    fireEvent.change(screen.getByLabelText('Auth Type'), { target: { value: 'oauth_m2m' } })
    fireEvent.change(screen.getByLabelText('Client ID'), { target: { value: 'sp-123' } })
    fireEvent.change(screen.getByLabelText('Client Secret'), { target: { value: 'secret-456' } })
    fireEvent.click(screen.getByText('Create'))

    await waitFor(() =>
      expect(postBody?.config).toEqual({
        host: 'dbc-x.cloud.databricks.com',
        http_path: '/sql/1.0/warehouses/abc',
        auth_type: 'oauth_m2m',
        catalog: '',
        schema: '',
        client_id: 'sp-123',
        client_secret: 'secret-456',
      }),
    )
  })

  test('editing a Databricks connector omits blank secrets on save (T-DBX-3)', async () => {
    let putBody: { config?: Record<string, unknown> } | null = null
    const stored = {
      id: 'c-dbx-edit', name: 'DBX Edit', type: 'databricks', is_default: false,
      config: {
        host: 'dbc-x.cloud.databricks.com', http_path: '/sql/1.0/warehouses/abc',
        auth_type: 'pat', token: '***', catalog: 'main', schema: 'sales',
      },
      created_at: '2026-01-01T00:00:00Z',
    }
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([stored])),
      http.post('/api/v1/connectors/:id/test', () => HttpResponse.json({ ok: true })),
      http.put('/api/v1/connectors/c-dbx-edit', async ({ request }) => {
        putBody = (await request.json()) as { config?: Record<string, unknown> }
        return HttpResponse.json({ ...stored, config: { ...stored.config, catalog: 'analytics' } })
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByLabelText('Edit connector'))
    // Blank secret is allowed while the auth type is unchanged.
    expect(screen.getByText('Save')).not.toBeDisabled()
    fireEvent.change(screen.getByLabelText('Catalog'), { target: { value: 'analytics' } })
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() =>
      expect(putBody?.config).toEqual({
        host: 'dbc-x.cloud.databricks.com',
        http_path: '/sql/1.0/warehouses/abc',
        auth_type: 'pat',
        catalog: 'analytics',
        schema: 'sales',
      }),
    )
  })

  test('create Databricks connector stays disabled until required fields are set (T-DBX-4)', async () => {
    server.use(http.get('/api/v1/connectors', () => HttpResponse.json([])))
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByText('+ New Connector'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'DBX' } })
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'databricks' } })
    expect(screen.getByText('Create')).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'dbc-x.cloud.databricks.com' } })
    fireEvent.change(screen.getByLabelText('HTTP Path'), { target: { value: '/sql/1.0/warehouses/abc' } })
    expect(screen.getByText('Create')).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Token'), { target: { value: 'dapi-123' } })
    expect(screen.getByText('Create')).not.toBeDisabled()
  })

  test('switching auth type on edit requires the new secret (T-DBX-5)', async () => {
    const storedM2M = {
      id: 'c-dbx-m2m', name: 'DBX M2M', type: 'databricks', is_default: false,
      config: {
        host: 'dbc-x.cloud.databricks.com', http_path: '/sql/1.0/warehouses/abc',
        auth_type: 'oauth_m2m', client_id: 'sp-1', client_secret: '***', catalog: '', schema: '',
      },
      created_at: '2026-01-01T00:00:00Z',
    }
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([storedM2M])),
      http.post('/api/v1/connectors/:id/test', () => HttpResponse.json({ ok: true })),
    )
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByLabelText('Edit connector'))
    expect(screen.getByText('Save')).not.toBeDisabled()
    fireEvent.change(screen.getByLabelText('Auth Type'), { target: { value: 'pat' } })
    expect(screen.getByText('Save')).toBeDisabled()
    fireEvent.change(screen.getByLabelText(/^Token/), { target: { value: 'dapi-new' } })
    expect(screen.getByText('Save')).not.toBeDisabled()
  })

  test('edit opens a dialog named for the connector, focuses the name field, and restores focus on close', async () => {
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Staging DB')
    expect(screen.queryByRole('dialog')).toBeNull()

    const editButton = screen.getAllByLabelText('Edit connector')[1]
    editButton.focus()
    fireEvent.click(editButton)

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('Edit "Staging DB"')
    expect(screen.getByLabelText('Name')).toHaveFocus()
    // Editing form only exists inside the dialog, so a click far down the table
    // can never render it out of view.
    expect(screen.queryByText('Edit Connector')).toBeNull()

    fireEvent.click(screen.getByText('Cancel'))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(editButton).toHaveFocus()
  })

  test('new connector opens a dialog and Escape returns focus to the trigger', async () => {
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Prod DB')
    const newButton = screen.getByText('+ New Connector')
    newButton.focus()
    fireEvent.click(newButton)

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('New Connector')
    expect(screen.getByLabelText('Name')).toHaveFocus()

    fireEvent.keyDown(window, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(newButton).toHaveFocus()
  })

  test('does not probe connectors on mount (zero /test requests)', async () => {
    const testSpy = vi.fn()
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors)),
      http.post('/api/v1/connectors/:id/test', () => {
        testSpy()
        return HttpResponse.json({ ok: true })
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Prod DB')
    await screen.findByText('Staging DB')
    // Give any buggy effect a beat to fire before asserting.
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(testSpy).not.toHaveBeenCalled()
  })

  test('renders the persisted health status without probing', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([
          {
            id: 'c-failed', name: 'Broken DB', type: 'postgres', config: {}, created_at: '2026-10-08T08:00:00Z',
            last_success_at: '2026-10-08T08:00:00Z',
            last_failure_at: '2026-10-08T09:00:00Z',
            last_error: 'dial tcp 10.0.0.5:5432: connection refused',
          },
          {
            id: 'c-recovered', name: 'Recovered DB', type: 'postgres', config: {}, created_at: '2026-10-08T08:00:00Z',
            last_success_at: '2026-10-08T10:00:00Z',
            last_failure_at: '2026-10-08T09:00:00Z',
          },
          {
            id: 'c-new', name: 'Fresh DB', type: 'postgres', config: {}, created_at: '2026-10-08T08:00:00Z',
          },
        ]),
      ),
    )
    renderWithProviders(<ConnectorsPage />)

    const failed = await screen.findByText(/^Failed · /)
    expect(failed).toHaveAttribute('title', 'dial tcp 10.0.0.5:5432: connection refused')
    expect(await screen.findByText(/^Connected · used /)).toBeInTheDocument()
    expect(screen.getByText('Never used — click Test')).toBeInTheDocument()
  })

  test('manual Test refreshes the persisted status', async () => {
    let connectors: Record<string, unknown>[] = [
      { id: 'c-1', name: 'Prod DB', type: 'postgres', config: {}, created_at: '2026-10-08T08:00:00Z' },
    ]
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(connectors)),
      http.post('/api/v1/connectors/c-1/test', () => {
        connectors = [{ ...connectors[0], last_success_at: new Date().toISOString() }]
        return HttpResponse.json({ ok: true })
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Never used — click Test')

    fireEvent.click(screen.getByLabelText('Test connection'))
    expect(screen.getByText('Testing…')).toBeInTheDocument()
    expect(await screen.findByText(/^Connected · used /)).toBeInTheDocument()
  })

  test('creates a ClickHouse connector with idle timeout and Cloud API config (CH-CLOUD-1)', async () => {
    let postBody: { config?: Record<string, unknown> } | null = null
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([])),
      http.post('/api/v1/connectors', async ({ request }) => {
        postBody = (await request.json()) as { config?: Record<string, unknown> }
        return HttpResponse.json(
          { id: 'c-ch', name: 'CH', type: 'clickhouse', config: {}, created_at: '2026-01-01T00:00:00Z' },
          { status: 201 },
        )
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByText('+ New Connector'))
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'CH' } })
    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'clickhouse' } })
    fireEvent.change(screen.getByLabelText('Host'), { target: { value: 'abc.clickhouse.cloud' } })
    fireEvent.change(screen.getByLabelText('Idle timeout (minutes)'), { target: { value: '30' } })
    fireEvent.change(screen.getByLabelText('Organization ID'), { target: { value: 'org-1' } })
    fireEvent.change(screen.getByLabelText('Service ID'), { target: { value: 'svc-1' } })
    fireEvent.change(screen.getByLabelText('Key ID'), { target: { value: 'key-1' } })
    fireEvent.change(screen.getByLabelText('Key Secret'), { target: { value: 'super-secret' } })
    fireEvent.click(screen.getByText('Create'))

    await waitFor(() =>
      expect(postBody?.config).toEqual({
        host: 'abc.clickhouse.cloud',
        port: 9000,
        database: '',
        user: '',
        ssl_mode: 'disable',
        idle_timeout_minutes: 30,
        cloud_org_id: 'org-1',
        cloud_service_id: 'svc-1',
        cloud_key_id: 'key-1',
        cloud_key_secret: 'super-secret',
        password: '',
      }),
    )
  })

  test('editing ClickHouse keeps the stored Cloud key secret when left blank (CH-CLOUD-2)', async () => {
    let putBody: { config?: Record<string, unknown> } | null = null
    const stored = {
      id: 'c-ch-edit', name: 'CH Edit', type: 'clickhouse', is_default: false,
      config: {
        host: 'abc.clickhouse.cloud', port: 8443, database: '', user: 'default',
        ssl_mode: 'require', idle_timeout_minutes: 15,
        cloud_org_id: 'org-1', cloud_service_id: 'svc-1',
        cloud_key_id: 'key-1', cloud_key_secret: '***',
      },
      created_at: '2026-01-01T00:00:00Z',
    }
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([stored])),
      http.get('/api/v1/connectors/:id/cloud-state', () =>
        HttpResponse.json({ configured: true, state: 'running', checked_at: new Date().toISOString() }),
      ),
      http.put('/api/v1/connectors/c-ch-edit', async ({ request }) => {
        putBody = (await request.json()) as { config?: Record<string, unknown> }
        return HttpResponse.json({ ...stored, config: { ...stored.config, idle_timeout_minutes: 45 } })
      }),
    )
    renderWithProviders(<ConnectorsPage />)
    fireEvent.click(await screen.findByLabelText('Edit connector'))
    // The stored secret comes back masked and must never be pre-filled.
    expect((screen.getByLabelText(/Key Secret/) as HTMLInputElement).value).toBe('')
    fireEvent.change(screen.getByLabelText('Idle timeout (minutes)'), { target: { value: '45' } })
    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody!.config).toMatchObject({
      idle_timeout_minutes: 45,
      cloud_org_id: 'org-1',
      cloud_service_id: 'svc-1',
      cloud_key_id: 'key-1',
    })
    expect(putBody!.config).not.toHaveProperty('cloud_key_secret')
  })
})
