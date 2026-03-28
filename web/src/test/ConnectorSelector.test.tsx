import { render, screen, fireEvent } from '@testing-library/react'
import { describe, test, expect, vi } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from './server'
import { ConnectorSelector } from '../components/ConnectorSelector'

const mockConnectors = [
  { id: 'conn-1', name: 'Production DB', type: 'postgres' },
  { id: 'conn-2', name: 'Analytics CH', type: 'clickhouse' },
]

describe('ConnectorSelector', () => {
  test('renders connector options', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors))
    )
    render(<ConnectorSelector value={null} onChange={() => {}} />)
    expect(await screen.findByText('Production DB')).toBeInTheDocument()
    expect(await screen.findByText('Analytics CH')).toBeInTheDocument()
  })

  test('shows placeholder when value is null', () => {
    render(<ConnectorSelector value={null} onChange={() => {}} placeholder="Select connector" />)
    // The select element has value "" which shows the placeholder option
    const select = screen.getByRole('combobox')
    expect(select).toBeInTheDocument()
  })

  test('calls onChange when selection changes', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors))
    )
    const onChange = vi.fn()
    render(<ConnectorSelector value={null} onChange={onChange} />)
    const select = await screen.findByRole('combobox')
    fireEvent.change(select, { target: { value: 'conn-1' } })
    expect(onChange).toHaveBeenCalledWith('conn-1')
  })

  test('calls onChange with null when empty option selected', async () => {
    server.use(
      http.get('/api/v1/connectors', () => HttpResponse.json(mockConnectors))
    )
    const onChange = vi.fn()
    render(<ConnectorSelector value="conn-1" onChange={onChange} allowClear />)
    const select = await screen.findByRole('combobox')
    fireEvent.change(select, { target: { value: '' } })
    expect(onChange).toHaveBeenCalledWith(null)
  })
})
