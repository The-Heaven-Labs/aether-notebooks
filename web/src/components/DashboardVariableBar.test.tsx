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

function renderBar(
  variables: DashboardVariable[],
  options: { allVariables?: DashboardVariable[]; initialEntries?: string[] } = {},
) {
  return render(
    <MemoryRouter initialEntries={options.initialEntries ?? ['/dash/d1']}>
      <DashboardVariablesProvider dashboardId="d1" variables={variables} allVariables={options.allVariables}>
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

    // The dropdown opens on the trigger and toggles checkboxes without closing.
    fireEvent.click(screen.getByLabelText('Regions'))
    fireEvent.click(screen.getByRole('option', { name: 'AMER' }))
    expect(screen.getByTestId('probe-regions')).toHaveTextContent('["AMER"]')
    expect(screen.getByRole('option', { name: 'AMER' })).toHaveAttribute('aria-selected', 'true')
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
    // The trigger disables while options reload; open it once they arrive.
    const trigger = screen.getByLabelText('City')
    await waitFor(() => expect(trigger).not.toBeDisabled(), { timeout: 3000 })
    fireEvent.click(trigger)
    await waitFor(() => expect(screen.getByRole('option', { name: 'Paris' })).toBeInTheDocument(), { timeout: 3000 })
  })

  test('empty multi_select warns and can select all', () => {
    renderBar([{ name: 'regions', label: 'Regions', type: 'multi_select', options: staticOptions }])
    expect(screen.getByText(/Nothing selected/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Select all' }))
    expect(screen.getByTestId('probe-regions')).toHaveTextContent('["EMEA","AMER"]')
  })

  test('Reset filters appears for non-default values and restores defaults', () => {
    renderBar([{ name: 'region', label: 'Region', type: 'text', default: 'EMEA' }])
    expect(screen.queryByRole('button', { name: /Reset filters/ })).not.toBeInTheDocument()
    const input = screen.getByLabelText('Region')
    fireEvent.change(input, { target: { value: 'AMER' } })
    fireEvent.blur(input)
    expect(screen.getByTestId('probe-region')).toHaveTextContent('"AMER"')
    fireEvent.click(screen.getByRole('button', { name: /Reset filters/ }))
    expect(screen.getByTestId('probe-region')).toHaveTextContent('"EMEA"')
    expect(screen.queryByRole('button', { name: /Reset filters/ })).not.toBeInTheDocument()
  })

  test('hidden filters pinned in the URL raise the other-chapters hint', () => {
    const visible: DashboardVariable[] = [{ name: 'overview_region', label: 'Overview region', type: 'text' }]
    const all: DashboardVariable[] = [
      ...visible,
      { name: 'orders_status', label: 'Orders status', type: 'text', default: 'open' },
    ]
    renderBar(visible, { allVariables: all, initialEntries: ['/dash/d1?orders_status=closed'] })

    expect(screen.getByText('1 filter set in other chapters')).toBeInTheDocument()
  })

  test('the hint stays away when no hidden filter is pinned or both filters are visible', () => {
    const visible: DashboardVariable[] = [{ name: 'overview_region', label: 'Overview region', type: 'text' }]
    const all: DashboardVariable[] = [
      ...visible,
      { name: 'orders_status', label: 'Orders status', type: 'text', default: 'open' },
    ]
    // A pin on a visible filter is not "in another chapter".
    renderBar(visible, { allVariables: all, initialEntries: ['/dash/d1?overview_region=eu'] })
    expect(screen.queryByText(/set in other chapters/)).not.toBeInTheDocument()

    // Unpinned hidden filters do not count either.
    renderBar(visible, { allVariables: all, initialEntries: ['/dash/d1'] })
    expect(screen.queryByText(/set in other chapters/)).not.toBeInTheDocument()
  })

  test('the hint pluralizes for two hidden pins', () => {
    const visible: DashboardVariable[] = [{ name: 'overview_region', label: 'Overview region', type: 'text' }]
    const all: DashboardVariable[] = [
      ...visible,
      { name: 'orders_status', label: 'Orders status', type: 'text' },
      { name: 'orders_tier', label: 'Orders tier', type: 'text' },
    ]
    renderBar(visible, { allVariables: all, initialEntries: ['/dash/d1?orders_status=closed&orders_tier=gold'] })

    expect(screen.getByText('2 filters set in other chapters')).toBeInTheDocument()
  })
})
