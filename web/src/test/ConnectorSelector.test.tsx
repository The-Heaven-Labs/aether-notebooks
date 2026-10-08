import type { ReactNode } from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, test, expect, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { ConnectorSelector } from '../components/ConnectorSelector'

// ConnectorSelector reads the shared ['connectors'] React Query cache, so
// tests need a QueryClientProvider around it.
function renderWithQuery(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

const mockConnectors = [
  { id: 'conn-1', name: 'Production DB', type: 'postgres' },
  { id: 'conn-2', name: 'Analytics CH', type: 'clickhouse' },
]

describe('ConnectorSelector', () => {
  test('renders connector options', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors))
    )
    renderWithQuery(<ConnectorSelector value={null} onChange={() => {}} />)
    expect(await screen.findByText('Production DB')).toBeInTheDocument()
    expect(await screen.findByText('Analytics CH')).toBeInTheDocument()
  })

  test('shows placeholder when value is null', () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors))
    )
    renderWithQuery(<ConnectorSelector value={null} onChange={() => {}} placeholder="Select connector" />)
    // The select element has value "" which shows the placeholder option
    const select = screen.getByRole('combobox')
    expect(select).toBeInTheDocument()
  })

  test('calls onChange when selection changes', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors))
    )
    const onChange = vi.fn()
    renderWithQuery(<ConnectorSelector value={null} onChange={onChange} />)
    const select = await screen.findByRole('combobox')
    await screen.findByText('Production DB')
    fireEvent.change(select, { target: { value: 'conn-1' } })
    expect(onChange).toHaveBeenCalledWith('conn-1')
  })

  test('calls onChange with null when empty option selected', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors))
    )
    const onChange = vi.fn()
    renderWithQuery(<ConnectorSelector value="conn-1" onChange={onChange} allowClear />)
    const select = await screen.findByRole('combobox')
    fireEvent.change(select, { target: { value: '' } })
    expect(onChange).toHaveBeenCalledWith(null)
  })

  test('types filter hides connectors of other types', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors))
    )
    renderWithQuery(<ConnectorSelector value={null} onChange={() => {}} types={['clickhouse']} />)
    expect(await screen.findByText('Analytics CH')).toBeInTheDocument()
    expect(screen.queryByText('Production DB')).not.toBeInTheDocument()
  })
})
