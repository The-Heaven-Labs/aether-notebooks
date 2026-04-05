import { describe, test, expect, vi, beforeEach } from 'vitest'
import { screen } from '@testing-library/react'
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
    expect(screen.getByText('default')).toBeInTheDocument()
  })

  test('only one connector has default badge (T4.2)', async () => {
    renderWithProviders(<ConnectorsPage />)
    await screen.findByText('Prod DB')
    const badges = screen.getAllByText('default')
    expect(badges).toHaveLength(1)
  })
})
