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

const provisionerConnectors = [
  { id: 'conn-svc', name: 'Service CH', type: 'clickhouse', can_use: true, is_provisioner: false },
  { id: 'conn-prov-allowed', name: 'Allowed provisioner', type: 'clickhouse', can_use: true, is_provisioner: true },
  { id: 'conn-prov-blocked', name: 'Blocked provisioner', type: 'clickhouse', can_use: false, is_provisioner: true },
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

  test('hides a non-executable provisioner from the options', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(provisionerConnectors))
    )
    renderWithQuery(<ConnectorSelector value={null} onChange={() => {}} />)
    expect(await screen.findByRole('option', { name: 'Service CH' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /Blocked provisioner/ })).not.toBeInTheDocument()
  })

  test('keeps an executable provisioner with its label', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(provisionerConnectors))
    )
    renderWithQuery(<ConnectorSelector value={null} onChange={() => {}} />)
    const option = await screen.findByRole('option', { name: 'Allowed provisioner (provisioner)' })
    expect(option).toBeEnabled()
  })

  test('keeps a selected non-executable provisioner as a disabled option', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(provisionerConnectors))
    )
    renderWithQuery(<ConnectorSelector value="conn-prov-blocked" onChange={() => {}} />)
    const option = await screen.findByRole('option', { name: 'Blocked provisioner (provisioner — no access)' })
    expect(option).toBeDisabled()
    // The kept option is what the select displays; it never blanks out.
    expect(screen.getByRole('combobox')).toHaveValue('conn-prov-blocked')
  })
})
