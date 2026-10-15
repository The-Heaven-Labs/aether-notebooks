import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { ToolsPage } from '../pages/ToolsPage'
import { renderWithProviders } from './utils'

vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

beforeEach(() => {
  server.use(
    http.get('/api/v1/tools', () => HttpResponse.json([])),
    http.get('/api/v1/connectors', () => HttpResponse.json([])),
  )
  vi.clearAllMocks()
})

describe('ToolsPage', () => {
  test('creates a webhook tool with custom headers and secrets', async () => {
    let postBody: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/tools', async ({ request }) => {
        postBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 't-new' }, { status: 201 })
      }),
    )
    renderWithProviders(<ToolsPage />)
    await screen.findByText('No tools yet')

    // The section header and the empty state both render a "+ New Tool" button.
    fireEvent.click(screen.getAllByText('+ New Tool')[0])
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Token Hook' } })
    fireEvent.change(within(dialog).getByLabelText('URL'), { target: { value: 'https://example.com/hook' } })
    fireEvent.change(within(dialog).getByLabelText('Header 1 name'), { target: { value: 'X-Token' } })
    fireEvent.change(within(dialog).getByLabelText('Header 1 value'), { target: { value: 'abc' } })
    // The sensitive checkbox precedes the confirmation checkbox in the form.
    fireEvent.click(within(dialog).getAllByRole('checkbox')[0])
    fireEvent.click(within(dialog).getByText('Create'))

    await waitFor(() => expect(postBody).not.toBeNull())
    expect(postBody).toMatchObject({
      config: { headers: { 'X-Token': 'abc' }, secrets: ['X-Token'] },
    })
  })

  test('round-trips a masked header secret on save', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.get('/api/v1/tools', () => HttpResponse.json([
        {
          id: 't-1',
          org_id: 'org-1',
          name: 'Token Hook',
          description: '',
          type: 'webhook',
          schema: { type: 'object', properties: {} },
          config: {
            url: 'https://example.com/hook',
            method: 'POST',
            headers: { 'X-Mode': 'live', 'X-Api-Key': '__AETHER_REDACTED__' },
            secrets: ['X-Api-Key'],
          },
          require_confirmation: false,
          created_by: 'user-1',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ])),
      http.put('/api/v1/tools/:id', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 't-1' })
      }),
    )
    renderWithProviders(<ToolsPage />)
    await screen.findByText('Token Hook')

    fireEvent.click(screen.getByLabelText('Edit tool'))
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('Edit "Token Hook"')

    const secretInput = within(dialog).getByLabelText('Header 2 value') as HTMLInputElement
    expect(secretInput.value).toBe('')
    expect(secretInput.type).toBe('password')

    fireEvent.click(within(dialog).getByText('Save'))
    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody).toMatchObject({
      config: {
        headers: { 'X-Mode': 'live', 'X-Api-Key': '__AETHER_REDACTED__' },
        secrets: ['X-Api-Key'],
      },
    })
  })

  test('hides a blocked provisioner from the sql_query connector picker', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([
        { id: 'conn-svc', name: 'Service CH', type: 'clickhouse', can_use: true, is_provisioner: false, created_at: '2026-01-01T00:00:00Z' },
        { id: 'conn-prov', name: 'Blocked provisioner', type: 'clickhouse', can_use: false, is_provisioner: true, created_at: '2026-01-01T00:00:00Z' },
      ])),
    )
    renderWithProviders(<ToolsPage />)
    await screen.findByText('No tools yet')

    fireEvent.click(screen.getAllByText('+ New Tool')[0])
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Type'), { target: { value: 'sql_query' } })

    expect(within(dialog).queryByRole('option', { name: /Blocked provisioner/ })).not.toBeInTheDocument()
    expect(within(dialog).getByRole('option', { name: 'Service CH (clickhouse)' })).toBeInTheDocument()
  })

  test('a tool configured with a blocked provisioner is not listed and shows the placeholder', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json([
        { id: 'conn-svc', name: 'Service CH', type: 'clickhouse', can_use: true, is_provisioner: false, created_at: '2026-01-01T00:00:00Z' },
        { id: 'conn-prov', name: 'Blocked provisioner', type: 'clickhouse', can_use: false, is_provisioner: true, created_at: '2026-01-01T00:00:00Z' },
      ])),
      http.get('/api/v1/tools', () => HttpResponse.json([
        {
          id: 't-sql',
          org_id: 'org-1',
          name: 'Provisioned Query',
          description: '',
          type: 'sql_query',
          schema: { type: 'object', properties: {} },
          config: { connector_id: 'conn-prov', query: 'SELECT 1' },
          require_confirmation: false,
          created_by: 'user-1',
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        },
      ])),
    )
    renderWithProviders(<ToolsPage />)
    await screen.findByText('Provisioned Query')

    fireEvent.click(screen.getByLabelText('Edit tool'))
    const dialog = await screen.findByRole('dialog')

    const picker = within(dialog).getByLabelText('Connector') as HTMLSelectElement
    // The stored reference is hidden, so the select falls back to the
    // placeholder instead of a disabled option.
    expect(picker.value).toBe('')
    expect(within(dialog).queryByRole('option', { name: /Blocked provisioner/ })).not.toBeInTheDocument()
    expect(within(dialog).getByRole('option', { name: 'Service CH (clickhouse)' })).toBeInTheDocument()
  })
})
