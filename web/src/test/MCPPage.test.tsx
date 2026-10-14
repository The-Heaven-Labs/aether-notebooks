import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
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
    renderWithProviders(<MCPPage />)
    await screen.findByText('Weather MCP')

    fireEvent.click(screen.getByLabelText('Edit MCP server'))
    await screen.findByRole('dialog')
    expect(screen.queryByText('Headers')).toBeNull()
  })
})
