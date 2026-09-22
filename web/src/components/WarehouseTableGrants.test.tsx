import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { WarehouseTableGrants } from './WarehouseTableGrants'
import type { WarehouseConnector, WarehouseGrant } from '../api/warehouses'

const CONNECTORS: WarehouseConnector[] = [
  { id: 'c-1', name: 'CH RW', type: 'clickhouse', is_provisioner: true },
]

const GRANTS: WarehouseGrant[] = [
  {
    id: 'gr-1', org_id: 'org-1', warehouse_id: 'wh-1', subject_type: 'group', subject_id: 'g-1',
    subject_name: 'Data Team', database: 'analytics', table: 'events',
    created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'gr-2', org_id: 'org-1', warehouse_id: 'wh-1', subject_type: 'user', subject_id: 'user-2',
    subject_name: 'Bob Editor', subject_email: 'bob@test.com', database: 'analytics', table: 'users',
    created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 'gr-3', org_id: 'org-1', warehouse_id: 'wh-1', subject_type: 'everyone', subject_id: 'everyone',
    subject_name: 'Everyone', database: 'raw', table: 'clicks',
    created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
  },
]

const SCHEMA = {
  tables: [
    { schema: 'analytics', name: 'events', columns: [{ name: 'id', type: 'UInt64' }] },
    { schema: 'analytics', name: 'users', columns: [{ name: 'id', type: 'UInt64' }] },
    { schema: 'raw', name: 'clicks', columns: [{ name: 'id', type: 'UInt64' }] },
  ],
}

function renderGrants() {
  return renderWithProviders(<WarehouseTableGrants warehouseId="wh-1" connectors={CONNECTORS} />)
}

beforeEach(() => {
  server.use(
    http.get('/api/v1/warehouses/wh-1/grants', () => HttpResponse.json(GRANTS)),
    http.get('/api/v1/connectors/c-1/schema', () => HttpResponse.json(SCHEMA)),
  )
})

describe('WarehouseTableGrants', () => {
  test('renders grant rows grouped by subject with table chips', async () => {
    renderGrants()
    expect((await screen.findAllByText('Data Team')).length).toBeGreaterThan(0)
    expect((await screen.findAllByText('Bob Editor')).length).toBeGreaterThan(0)
    expect((await screen.findAllByText('Everyone')).length).toBeGreaterThan(0)
    expect(screen.getByText('analytics.events')).toBeInTheDocument()
    expect(screen.getByText('analytics.users')).toBeInTheDocument()
    expect(screen.getByText('raw.clicks')).toBeInTheDocument()
  })

  test('subject picker offers users, groups, and Everyone', async () => {
    renderGrants()
    await screen.findAllByText('Data Team')
    const picker = screen.getByLabelText('Subject')
    expect(picker).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Bob Editor' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Data Team' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'CSIRT' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Everyone' })).toBeInTheDocument()
  })

  test('states grants are not enforced while table permissions are disabled', async () => {
    server.use(
      http.get('/api/v1/auth/config', () =>
        HttpResponse.json({
          registration_disabled: false,
          warehouse_table_permissions_enabled: false,
        }),
      ),
    )
    renderGrants()
    await screen.findAllByText('Data Team')
    expect(
      screen.getByText(/table permissions are disabled, so these grants are not enforced yet/),
    ).toBeInTheDocument()
    expect(screen.queryByText(/ClickHouse enforces these grants/)).toBeNull()
  })

  test('posts a grant for the selected subject and table and refetches grants', async () => {
    let posted: Record<string, unknown> | null = null
    let grantsCalls = 0
    server.use(
      http.get('/api/v1/warehouses/wh-1/grants', () => {
        grantsCalls++
        return HttpResponse.json(GRANTS)
      }),
      http.post('/api/v1/warehouses/wh-1/grants', async ({ request }) => {
        posted = await request.json() as Record<string, unknown>
        return HttpResponse.json(
          {
            id: 'gr-new', org_id: 'org-1', warehouse_id: 'wh-1',
            subject_type: 'group', subject_id: 'g-2', subject_name: 'CSIRT',
            database: 'analytics', table: 'users', created_by: 'user-1',
            created_at: '2026-01-01T00:00:00Z',
          },
          { status: 201 },
        )
      }),
    )
    renderGrants()
    await screen.findAllByText('Data Team')
    expect(grantsCalls).toBe(1)

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'group:g-2' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() =>
      expect(screen.getByLabelText('users')).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByLabelText('users'))
    fireEvent.click(screen.getByText('Add grant'))

    await waitFor(() =>
      expect(posted).toEqual({
        subject_type: 'group',
        subject_id: 'g-2',
        database: 'analytics',
        table: 'users',
      }),
    )
    await waitFor(() => expect(grantsCalls).toBeGreaterThanOrEqual(2))
  })

  test('marks already-granted tables and disables their checkboxes', async () => {
    renderGrants()
    await screen.findAllByText('Data Team')

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'group:g-1' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() => expect(screen.getByLabelText('events')).toBeInTheDocument())

    expect(screen.getByLabelText('events')).toBeChecked()
    expect(screen.getByLabelText('events')).toBeDisabled()
    expect(screen.getByLabelText('users')).not.toBeDisabled()
    expect(screen.getByText('already granted')).toBeInTheDocument()
  })

  test('adds every checked table in one action and reports per-table failures', async () => {
    const posted: Record<string, unknown>[] = []
    server.use(
      http.post('/api/v1/warehouses/wh-1/grants', async ({ request }) => {
        const body = await request.json() as Record<string, unknown>
        posted.push(body)
        if (body.table === 'users') {
          return HttpResponse.json({ error: 'bad table name' }, { status: 400 })
        }
        return HttpResponse.json(
          {
            id: `gr-${posted.length}`, org_id: 'org-1', warehouse_id: 'wh-1',
            subject_type: body.subject_type, subject_id: body.subject_id,
            subject_name: 'Everyone', database: body.database, table: body.table,
            created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
          },
          { status: 201 },
        )
      }),
    )
    renderGrants()
    await screen.findAllByText('Data Team')

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'everyone:everyone' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() => expect(screen.getByLabelText('events')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('events'))
    fireEvent.click(screen.getByLabelText('users'))
    expect(screen.getByText('Add 2 grants')).toBeInTheDocument()
    fireEvent.click(screen.getByText('Add 2 grants'))

    await waitFor(() => expect(posted).toHaveLength(2))
    expect(posted.map((p) => p.table).sort()).toEqual(['events', 'users'])
    expect(await screen.findByText(/1 of 2 grants failed: bad table name/)).toBeInTheDocument()
  })

  test('selects all visible non-granted tables and clears them', async () => {
    renderGrants()
    await screen.findAllByText('Data Team')

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'everyone:everyone' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() => expect(screen.getByLabelText('events')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Select all'))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.getByText('Add 2 grants')).toBeInTheDocument()

    fireEvent.click(screen.getByText('Clear'))
    expect(screen.getByText('0 selected')).toBeInTheDocument()
  })

  test('filters the table checklist', async () => {
    renderGrants()
    await screen.findAllByText('Data Team')

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'everyone:everyone' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() => expect(screen.getByLabelText('events')).toBeInTheDocument())

    fireEvent.change(screen.getByLabelText('Filter tables'), { target: { value: 'user' } })
    expect(screen.queryByLabelText('events')).toBeNull()
    expect(screen.getByLabelText('users')).toBeInTheDocument()
  })

  test('shows the "no service access" warning after a warned grant', async () => {
    server.use(
      http.post('/api/v1/warehouses/wh-1/grants', () =>
        HttpResponse.json(
          {
            id: 'gr-new', org_id: 'org-1', warehouse_id: 'wh-1',
            subject_type: 'group', subject_id: 'g-2', subject_name: 'CSIRT',
            database: 'analytics', table: 'events', created_by: 'user-1',
            created_at: '2026-01-01T00:00:00Z', warning: 'no_service_access',
          },
          { status: 201 },
        ),
      ),
    )
    renderGrants()
    await screen.findAllByText('Data Team')

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'group:g-2' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() =>
      expect(screen.getByLabelText('events')).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByLabelText('events'))
    fireEvent.click(screen.getByText('Add grant'))

    expect((await screen.findAllByText(/no service access/i)).length).toBeGreaterThan(0)
    const link = screen.getByRole('link', { name: 'Manage service access' })
    expect(link).toHaveAttribute('href', '/connectors?permissions=c-1')
  })

  test('clears the warning after a later warning-free grant', async () => {
    let posts = 0
    server.use(
      http.post('/api/v1/warehouses/wh-1/grants', async ({ request }) => {
        posts++
        const body = await request.json() as Record<string, unknown>
        return HttpResponse.json(
          {
            id: `gr-${posts}`, org_id: 'org-1', warehouse_id: 'wh-1',
            subject_type: body.subject_type, subject_id: body.subject_id,
            subject_name: 'CSIRT', database: body.database, table: body.table,
            created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
            warning: posts === 1 ? 'no_service_access' : undefined,
          },
          { status: 201 },
        )
      }),
    )
    renderGrants()
    await screen.findAllByText('Data Team')

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'group:g-2' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() =>
      expect(screen.getByLabelText('events')).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByLabelText('events'))
    fireEvent.click(screen.getByText('Add grant'))
    expect((await screen.findAllByText(/no service access/i)).length).toBeGreaterThan(0)

    fireEvent.click(screen.getByLabelText('users'))
    fireEvent.click(screen.getByText('Add grant'))

    await waitFor(() => expect(screen.queryByText(/no service access/i)).toBeNull())
  })

  test('clears the warning when the last grant of a subject is removed', async () => {
    let currentGrants: WarehouseGrant[] = []
    server.use(
      http.get('/api/v1/warehouses/wh-1/grants', () => HttpResponse.json(currentGrants)),
      http.post('/api/v1/warehouses/wh-1/grants', async ({ request }) => {
        const body = await request.json() as Record<string, unknown>
        const grant: WarehouseGrant = {
          id: 'gr-warned', org_id: 'org-1', warehouse_id: 'wh-1',
          subject_type: body.subject_type as WarehouseGrant['subject_type'],
          subject_id: body.subject_id as string,
          subject_name: 'CSIRT', database: body.database as string, table: body.table as string,
          created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
        }
        currentGrants = [grant]
        return HttpResponse.json({ ...grant, warning: 'no_service_access' }, { status: 201 })
      }),
      http.delete(
        '/api/v1/warehouses/wh-1/grants/:grantId',
        () => {
          currentGrants = []
          return new HttpResponse(null, { status: 204 })
        },
      ),
    )
    renderGrants()
    await screen.findByText('No table grants yet.')

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'group:g-2' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() =>
      expect(screen.getByLabelText('events')).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByLabelText('events'))
    fireEvent.click(screen.getByText('Add grant'))
    expect((await screen.findAllByText(/no service access/i)).length).toBeGreaterThan(0)

    fireEvent.click(await screen.findByTitle('Remove grant'))

    await waitFor(() => expect(screen.queryByText(/no service access/i)).toBeNull())
  })

  test('removes a grant when its chip is dismissed', async () => {
    let deletedId = ''
    server.use(
      http.delete('/api/v1/warehouses/wh-1/grants/:grantId', ({ params }) => {
        deletedId = params.grantId as string
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderGrants()
    await screen.findByText('analytics.events')

    fireEvent.click(screen.getAllByTitle('Remove grant')[0])
    await waitFor(() => expect(deletedId).toBe('gr-1'))
  })

  test('falls back to the default connector when the selected one is unlinked', async () => {
    const twoConnectors: WarehouseConnector[] = [
      { id: 'c-1', name: 'CH RW', type: 'clickhouse', is_provisioner: true },
      { id: 'c-2', name: 'CH RO', type: 'clickhouse', is_provisioner: false },
    ]
    server.use(
      http.get('/api/v1/connectors/c-2/schema', () => HttpResponse.json(SCHEMA)),
    )
    const { rerender } = renderWithProviders(
      <WarehouseTableGrants warehouseId="wh-1" connectors={twoConnectors} />,
    )
    await screen.findAllByText('Data Team')

    // The schema-source picker only exists while several connectors are linked.
    fireEvent.change(screen.getByLabelText('Schema source'), { target: { value: 'c-2' } })
    expect(screen.getByLabelText('Schema source')).toHaveValue('c-2')

    rerender(<WarehouseTableGrants warehouseId="wh-1" connectors={[CONNECTORS[0]]} />)

    await waitFor(() => expect(screen.queryByLabelText('Schema source')).toBeNull())
    expect(screen.getByText(/Tables are browsed from CH RW/)).toBeInTheDocument()
  })

  test('renders the empty state in a neutral secondary color', async () => {
    server.use(http.get('/api/v1/warehouses/wh-1/grants', () => HttpResponse.json([])))
    renderGrants()
    const empty = await screen.findByText('No table grants yet.')
    expect(empty).toHaveStyle('color: var(--text-secondary)')
  })

  test('shows an error state when grants fail to load', async () => {
    server.use(
      http.get('/api/v1/warehouses/wh-1/grants', () =>
        HttpResponse.json({ error: 'query failed' }, { status: 500 }),
      ),
    )
    renderGrants()
    expect(await screen.findByText('Failed to load table grants')).toBeInTheDocument()
  })

  test('shows the API error when creating a grant fails', async () => {
    server.use(
      http.post('/api/v1/warehouses/wh-1/grants', () =>
        HttpResponse.json({ error: 'invalid database name' }, { status: 400 }),
      ),
    )
    renderGrants()
    await screen.findAllByText('Data Team')

    fireEvent.change(screen.getByLabelText('Subject'), { target: { value: 'everyone:everyone' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() =>
      expect(screen.getByLabelText('users')).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByLabelText('users'))
    fireEvent.click(screen.getByText('Add grant'))

    expect(await screen.findByText(/invalid database name/)).toBeInTheDocument()
  })
})
