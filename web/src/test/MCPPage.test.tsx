import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { MCPPage } from '../pages/MCPPage'
import { renderWithProviders } from './utils'

vi.mock('../components/AppShell', () => ({
  AppShell: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

const mockServers = [
  {
    id: 's-1',
    name: 'Weather MCP',
    type: 'http',
    command: 'https://mcp.example.com',
    args: [],
    headers: { 'X-Env': 'prod', Authorization: '__AETHER_REDACTED__' },
    header_secrets: ['Authorization'],
    created_at: '2026-01-01T00:00:00Z',
  },
]

beforeEach(() => {
  server.use(http.get('/api/v1/mcp-servers', () => HttpResponse.json(mockServers)))
  vi.clearAllMocks()
})

describe('MCPPage', () => {
  test('round-trips masked header secrets on save', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/mcp-servers/:id', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 's-1' })
      }),
    )
    renderWithProviders(<MCPPage />)
    await screen.findByText('Weather MCP')

    fireEvent.click(screen.getByLabelText('Edit MCP server'))
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveAccessibleName('Edit "Weather MCP"')
    expect(screen.getByLabelText('Header 2 value')).toHaveValue('')

    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(putBody).not.toBeNull())
    expect(putBody).toMatchObject({
      headers: { 'X-Env': 'prod', Authorization: '__AETHER_REDACTED__' },
      header_secrets: ['Authorization'],
    })
  })

  test('hides the headers editor for stdio servers', async () => {
    server.use(http.get('/api/v1/mcp-servers', () => HttpResponse.json([
      { ...mockServers[0], type: 'stdio', command: '/usr/bin/mcp' },
    ])))
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/mcp-servers/:id', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 's-1' })
      }),
    )
    renderWithProviders(<MCPPage />)
    await screen.findByText('Weather MCP')

    fireEvent.click(screen.getByLabelText('Edit MCP server'))
    await screen.findByRole('dialog')
    expect(screen.queryByText('Headers')).toBeNull()

    fireEvent.click(screen.getByText('Save'))
    await waitFor(() => expect(putBody).not.toBeNull())
    // The mock row carries stored headers, so their absence proves the UI
    // omits the payload for stdio rather than the data simply being missing.
    expect(putBody).not.toHaveProperty('headers')
    expect(putBody).not.toHaveProperty('header_secrets')
  })

  test('creates an MCP server with custom headers', async () => {
    let postBody: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/mcp-servers', async ({ request }) => {
        postBody = await request.json() as Record<string, unknown>
        return HttpResponse.json({ id: 's-new' }, { status: 201 })
      }),
    )
    renderWithProviders(<MCPPage />)
    await screen.findByText('Weather MCP')

    fireEvent.click(screen.getByText('+ New MCP Server'))
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'New MCP' } })
    fireEvent.change(within(dialog).getByLabelText('Command'), { target: { value: 'https://new.example.com' } })
    fireEvent.click(within(dialog).getByText('+ Add Header'))
    fireEvent.change(within(dialog).getByLabelText('Header 1 name'), { target: { value: 'X-Token' } })
    fireEvent.change(within(dialog).getByLabelText('Header 1 value'), { target: { value: 'abc' } })
    fireEvent.click(within(dialog).getAllByRole('checkbox')[0])
    fireEvent.click(within(dialog).getByText('Create'))

    await waitFor(() => expect(postBody).not.toBeNull())
    expect(postBody).toMatchObject({
      headers: { 'X-Token': 'abc' },
      header_secrets: ['X-Token'],
    })
  })
})
