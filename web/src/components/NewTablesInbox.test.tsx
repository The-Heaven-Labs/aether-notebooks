import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { NewTablesInbox } from './NewTablesInbox'

const TABLES = [
  { database: 'analytics', table: 'events', first_seen_at: '2026-01-02T00:00:00Z' },
  { database: 'raw', table: 'clicks', first_seen_at: '2026-01-01T00:00:00Z' },
]

const EMPTY_VALIDATION = {
  warehouse_id: 'wh-1',
  truncated: false,
  tables_without_service_access: [],
  service_access_without_tables: [],
}

function renderInbox() {
  return renderWithProviders(<NewTablesInbox warehouseId="wh-1" />)
}

beforeEach(() => {
  server.use(
    http.get('/api/v1/warehouses/wh-1/new-tables', () =>
      HttpResponse.json({ warehouse_id: 'wh-1', since: '2025-12-31T00:00:00Z', tables: TABLES }),
    ),
    http.get('/api/v1/warehouses/wh-1/validation', () => HttpResponse.json(EMPTY_VALIDATION)),
  )
})

describe('NewTablesInbox', () => {
  test('renders new table rows with a subject picker and Add grant action', async () => {
    renderInbox()
    expect(await screen.findByText('analytics.events')).toBeInTheDocument()
    expect(screen.getByText('raw.clicks')).toBeInTheDocument()
    expect(screen.getByLabelText('Subject for analytics.events')).toBeInTheDocument()
    expect(screen.getAllByText('Add grant')).toHaveLength(2)
    expect(screen.getAllByRole('option', { name: 'Alice Admin' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('option', { name: 'Data Team' }).length).toBeGreaterThan(0)
    expect(screen.getAllByRole('option', { name: 'Everyone' }).length).toBeGreaterThan(0)
  })

  test('grants the selected table to the selected subject and refetches', async () => {
    let posted: Record<string, unknown> | null = null
    let inboxCalls = 0
    server.use(
      http.get('/api/v1/warehouses/wh-1/new-tables', () => {
        inboxCalls++
        return HttpResponse.json({ warehouse_id: 'wh-1', since: '2025-12-31T00:00:00Z', tables: TABLES })
      }),
      http.post('/api/v1/warehouses/wh-1/grants', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(
          {
            id: 'gr-new', org_id: 'org-1', warehouse_id: 'wh-1',
            subject_type: 'group', subject_id: 'g-1', subject_name: 'Data Team',
            database: 'analytics', table: 'events', created_by: 'user-1',
            created_at: '2026-01-01T00:00:00Z',
          },
          { status: 201 },
        )
      }),
    )
    renderInbox()
    await screen.findByText('analytics.events')
    expect(inboxCalls).toBe(1)

    fireEvent.change(screen.getByLabelText('Subject for analytics.events'), {
      target: { value: 'group:g-1' },
    })
    fireEvent.click(screen.getAllByText('Add grant')[0])

    await waitFor(() =>
      expect(posted).toEqual({
        subject_type: 'group',
        subject_id: 'g-1',
        database: 'analytics',
        table: 'events',
      }),
    )
    await waitFor(() => expect(inboxCalls).toBeGreaterThanOrEqual(2))
  })

  test('does not grant until a subject is selected', async () => {
    let posts = 0
    server.use(
      http.post('/api/v1/warehouses/wh-1/grants', () => {
        posts++
        return new HttpResponse(null, { status: 500 })
      }),
    )
    renderInbox()
    await screen.findByText('analytics.events')

    expect(screen.getAllByText('Add grant')[0]).toBeDisabled()
    fireEvent.click(screen.getAllByText('Add grant')[0])
    expect(posts).toBe(0)
  })

  test('shows validation warnings for half-configured subjects', async () => {
    server.use(
      http.get('/api/v1/warehouses/wh-1/validation', () =>
        HttpResponse.json({
          warehouse_id: 'wh-1',
          truncated: false,
          tables_without_service_access: [
            {
              subject_type: 'group',
              subject_id: 'g-1',
              subject_name: 'Data Team',
              tables: ['analytics.events'],
            },
          ],
          service_access_without_tables: [
            {
              subject_type: 'user',
              subject_id: 'user-2',
              subject_name: 'Bob Editor',
              services: ['CH RW'],
            },
          ],
        }),
      ),
    )
    renderInbox()
    await screen.findByText('analytics.events')

    expect(await screen.findByText(/holds table grants/)).toBeInTheDocument()
    expect(screen.getByText(/cannot use any warehouse service/)).toBeInTheDocument()
    expect(screen.getByText(/can connect/)).toBeInTheDocument()
    expect(screen.getByText(/every query fails/)).toBeInTheDocument()
    expect(screen.getAllByText('Data Team').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Bob Editor').length).toBeGreaterThan(0)
  })

  test('shows an empty state when there are no new tables', async () => {
    server.use(
      http.get('/api/v1/warehouses/wh-1/new-tables', () =>
        HttpResponse.json({ warehouse_id: 'wh-1', since: '2026-01-01T00:00:00Z', tables: [] }),
      ),
    )
    renderInbox()
    expect(await screen.findByText('No new tables since your last review.')).toBeInTheDocument()
  })

  test('shows the API error when creating a grant fails', async () => {
    server.use(
      http.post('/api/v1/warehouses/wh-1/grants', () =>
        HttpResponse.json({ error: 'invalid database name' }, { status: 400 }),
      ),
    )
    renderInbox()
    await screen.findByText('analytics.events')

    fireEvent.change(screen.getByLabelText('Subject for analytics.events'), {
      target: { value: 'everyone:everyone' },
    })
    fireEvent.click(screen.getAllByText('Add grant')[0])

    expect(await screen.findByText('invalid database name')).toBeInTheDocument()
  })
})
