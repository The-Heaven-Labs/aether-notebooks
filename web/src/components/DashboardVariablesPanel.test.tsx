import type { ReactNode } from 'react'
import { describe, test, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { DashboardVariablesPanel } from './DashboardVariablesPanel'
import type { Dashboard } from '../types'

// The panel embeds ConnectorSelector, which reads the shared ['connectors']
// React Query cache.
function renderWithQuery(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

function dashboardWith(variables: Dashboard['settings']['variables']): Dashboard {
  return {
    id: 'd1',
    org_id: 'org-1',
    title: 'Test Dashboard',
    settings: { variables },
    created_by: 'u1',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

beforeEach(() => {
  server.use(
    http.get('/api/v1/connectors', () =>
      HttpResponse.json([{ id: 'conn-1', name: 'Production DB', type: 'postgres' }]),
    ),
  )
})

function capturePuts() {
  const bodies: Array<Record<string, unknown>> = []
  server.use(
    http.put('/api/v1/dashboards/d1', async ({ request }) => {
      bodies.push((await request.json()) as Record<string, unknown>)
      return HttpResponse.json({})
    }),
  )
  return bodies
}

describe('DashboardVariablesPanel', () => {
  test('renders existing variables', () => {
    renderWithQuery(
      <DashboardVariablesPanel
        dashboardId="d1"
        dashboard={dashboardWith([{ name: 'region', type: 'text', default: 'EMEA' }])}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    expect((screen.getByLabelText('Variable name') as HTMLInputElement).value).toBe('region')
    expect((screen.getByLabelText('Variable default') as HTMLInputElement).value).toBe('EMEA')
  })

  test('saves edited variables', async () => {
    const bodies = capturePuts()
    const onSaved = vi.fn()
    renderWithQuery(
      <DashboardVariablesPanel
        dashboardId="d1"
        dashboard={dashboardWith([{ name: 'region', type: 'text', default: 'EMEA' }])}
        onClose={() => {}}
        onSaved={onSaved}
      />,
    )
    fireEvent.change(screen.getByLabelText('Variable default'), { target: { value: 'AMER' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save variables' }))
    await waitFor(() => expect(bodies.length).toBe(1))
    const settings = bodies[0].settings as { variables: Array<Record<string, unknown>> }
    expect(settings.variables).toHaveLength(1)
    expect(settings.variables[0]).toMatchObject({ name: 'region', type: 'text', default: 'AMER' })
    expect(onSaved).toHaveBeenCalled()
  })

  test('rejects invalid and duplicate names without saving', async () => {
    const bodies = capturePuts()
    renderWithQuery(
      <DashboardVariablesPanel
        dashboardId="d1"
        dashboard={dashboardWith([{ name: 'region', type: 'text' }])}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.change(screen.getByLabelText('Variable name'), { target: { value: 'bad name' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save variables' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Invalid variable name')

    fireEvent.change(screen.getByLabelText('Variable name'), { target: { value: 'region' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add variable' }))
    const names = screen.getAllByLabelText('Variable name')
    fireEvent.change(names[1], { target: { value: 'region' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save variables' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Duplicate variable name')
    expect(bodies).toHaveLength(0)
  })

  test('requires connector and SQL for query-backed options', async () => {
    const bodies = capturePuts()
    renderWithQuery(
      <DashboardVariablesPanel
        dashboardId="d1"
        dashboard={dashboardWith([{ name: 'city', type: 'single_select' }])}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.change(screen.getByLabelText('Options mode'), { target: { value: 'query' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save variables' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('needs a connector and SQL')
    expect(bodies).toHaveLength(0)
  })

  test('adds and removes variables', () => {
    renderWithQuery(
      <DashboardVariablesPanel
        dashboardId="d1"
        dashboard={dashboardWith([])}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Add variable' }))
    expect(screen.getAllByLabelText('Variable name')).toHaveLength(1)
    fireEvent.click(screen.getByLabelText('Remove variable'))
    expect(screen.queryByLabelText('Variable name')).toBeNull()
  })

  test('saves the public live toggle', async () => {
    const bodies = capturePuts()
    renderWithQuery(
      <DashboardVariablesPanel
        dashboardId="d1"
        dashboard={dashboardWith([])}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    )
    fireEvent.click(screen.getByLabelText('Public live queries'))
    fireEvent.click(screen.getByRole('button', { name: 'Save variables' }))
    await waitFor(() => expect(bodies.length).toBe(1))
    const settings = bodies[0].settings as Record<string, unknown>
    expect(settings.public_live).toBe(true)
  })

  test('prefills a new variable from the widget drawer', () => {
    renderWithQuery(
      <DashboardVariablesPanel
        dashboardId="d1"
        dashboard={dashboardWith([])}
        onClose={() => {}}
        onSaved={() => {}}
        initialNewName="who"
      />,
    )
    expect((screen.getByLabelText('Variable name') as HTMLInputElement).value).toBe('who')
  })

  test('closes on Escape and on backdrop click', () => {
    const onClose = vi.fn()
    renderWithQuery(
      <DashboardVariablesPanel
        dashboardId="d1"
        dashboard={dashboardWith([])}
        onClose={onClose}
        onSaved={() => {}}
      />,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByTestId('variables-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(2)
  })
})
