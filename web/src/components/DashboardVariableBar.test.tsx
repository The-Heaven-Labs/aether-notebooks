import { describe, test, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { DashboardVariablesProvider, useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { DashboardVariableBar } from './DashboardVariableBar'
import type { DashboardVariable } from '../types'

function Probe({ name }: { name: string }) {
  const { values } = useDashboardVariables()
  return <span data-testid={`probe-${name}`}>{JSON.stringify(values[name])}</span>
}

function renderBar(variables: DashboardVariable[]) {
  return render(
    <MemoryRouter initialEntries={['/dash/d1']}>
      <DashboardVariablesProvider dashboardId="d1" variables={variables}>
        <DashboardVariableBar />
        {variables.map((v) => <Probe key={v.name} name={v.name} />)}
      </DashboardVariablesProvider>
    </MemoryRouter>,
  )
}

const staticOptions = {
  mode: 'static' as const,
  values: [
    { label: 'EMEA', value: 'EMEA' },
    { label: 'AMER', value: 'AMER' },
  ],
}

beforeEach(() => {
  localStorage.clear()
})

describe('DashboardVariableBar', () => {
  test('renders a control for every variable type', () => {
    const variables: DashboardVariable[] = [
      { name: 'note', label: 'Note', type: 'text' },
      { name: 'count', label: 'Count', type: 'number' },
      { name: 'active', label: 'Active', type: 'boolean', default: false },
      { name: 'on_date', label: 'On date', type: 'date' },
      { name: 'period', label: 'Period', type: 'date_range' },
      { name: 'region', label: 'Region', type: 'single_select', options: staticOptions },
      { name: 'regions', label: 'Regions', type: 'multi_select', options: staticOptions },
    ]
    renderBar(variables)

    expect(screen.getByLabelText('Note')).toBeInTheDocument()
    expect(screen.getByLabelText('Count')).toBeInTheDocument()
    expect(screen.getByLabelText('Active')).toBeInTheDocument()
    expect(screen.getByLabelText('On date')).toBeInTheDocument()
    expect(screen.getByLabelText('Period start')).toBeInTheDocument()
    expect(screen.getByLabelText('Period end')).toBeInTheDocument()
    expect(screen.getByLabelText('Region')).toBeInTheDocument()
    expect(screen.getByLabelText('Regions')).toBeInTheDocument()
  })

  test('text commits on blur and number commits immediately', () => {
    renderBar([
      { name: 'note', label: 'Note', type: 'text', default: '' },
      { name: 'count', label: 'Count', type: 'number' },
    ])

    const note = screen.getByLabelText('Note')
    fireEvent.change(note, { target: { value: 'hello' } })
    fireEvent.blur(note)
    expect(screen.getByTestId('probe-note')).toHaveTextContent('"hello"')

    fireEvent.change(screen.getByLabelText('Count'), { target: { value: '5' } })
    expect(screen.getByTestId('probe-count')).toHaveTextContent('5')
  })

  test('boolean and multi select report new values', () => {
    renderBar([
      { name: 'active', label: 'Active', type: 'boolean', default: false },
      { name: 'regions', label: 'Regions', type: 'multi_select', options: staticOptions },
    ])

    fireEvent.click(screen.getByLabelText('Active'))
    expect(screen.getByTestId('probe-active')).toHaveTextContent('true')

    const multi = screen.getByLabelText('Regions') as HTMLSelectElement
    const option = Array.from(multi.options).find((o) => o.value === 'AMER')!
    option.selected = true
    fireEvent.change(multi)
    expect(screen.getByTestId('probe-regions')).toHaveTextContent('["AMER"]')
  })

  test('shows query option errors with a working retry', async () => {
    let calls = 0
    server.use(
      http.post('/api/v1/dashboards/d1/variables/city/options', () => {
        calls += 1
        if (calls === 1) return HttpResponse.json({ error: 'boom' }, { status: 500 })
        return HttpResponse.json({ options: [{ label: 'Paris', value: 'paris' }] })
      }),
    )
    renderBar([
      {
        name: 'city',
        label: 'City',
        type: 'single_select',
        options: {
          mode: 'query',
          query: { connector_id: 'conn-1', sql: 'SELECT label, value FROM cities' },
          label_column: 'label',
          value_column: 'value',
        },
      },
    ])

    expect(await screen.findByRole('alert', {}, { timeout: 3000 })).toHaveTextContent('boom')
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => expect(screen.getByRole('option', { name: 'Paris' })).toBeInTheDocument(), { timeout: 3000 })
  })
})
