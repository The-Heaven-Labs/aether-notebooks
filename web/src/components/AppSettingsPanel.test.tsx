import { describe, test, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { AppSettingsPanel } from './AppSettingsPanel'
import type { Dashboard } from '../types'

function dashboardWith(settings: Dashboard['settings']): Dashboard {
  return {
    id: 'd1',
    org_id: 'org-1',
    title: 'Test Dashboard',
    settings,
    created_by: 'u1',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

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

function renderPanel(dashboard: Dashboard, onSaved = vi.fn(), onClose = vi.fn()) {
  render(
    <AppSettingsPanel
      dashboardId="d1"
      dashboard={dashboard}
      onClose={onClose}
      onSaved={onSaved}
    />,
  )
  return { onSaved, onClose }
}

describe('AppSettingsPanel', () => {
  test('renders current app settings', () => {
    renderPanel(dashboardWith({
      app: {
        title: 'Q3 Revenue',
        subtitle: 'Sales ops · updated hourly',
        width: 'full',
        show_header: false,
        show_refresh: true,
        show_last_updated: false,
      },
    }))
    expect((screen.getByLabelText('App title') as HTMLInputElement).value).toBe('Q3 Revenue')
    expect((screen.getByLabelText('Subtitle') as HTMLInputElement).value).toBe('Sales ops · updated hourly')
    expect((screen.getByLabelText('Width') as HTMLSelectElement).value).toBe('full')
    expect((screen.getByLabelText('Show header') as HTMLInputElement).checked).toBe(false)
    expect((screen.getByLabelText('Show refresh') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('Show last updated') as HTMLInputElement).checked).toBe(false)
    expect(screen.getByText('Logo and fonts arrive with org branding (next wave).')).toBeInTheDocument()
  })

  test('unset toggles default to shown and the width to fixed', () => {
    renderPanel(dashboardWith({}))
    expect((screen.getByLabelText('Show header') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('Show refresh') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('Show last updated') as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText('Width') as HTMLSelectElement).value).toBe('fixed')
  })

  test('app title placeholder falls back to the dashboard title', () => {
    renderPanel(dashboardWith({}))
    expect((screen.getByLabelText('App title') as HTMLInputElement).placeholder).toBe('Test Dashboard')
  })

  test('saves explicit booleans and preserves sibling settings', async () => {
    const bodies = capturePuts()
    const { onSaved } = renderPanel(dashboardWith({
      public_live: true,
      variables: [{ name: 'region', type: 'text' }],
      app: { logo_attachment_id: 'att-1' },
    }))

    fireEvent.change(screen.getByLabelText('App title'), { target: { value: 'Q3 Revenue' } })
    fireEvent.change(screen.getByLabelText('Subtitle'), { target: { value: 'Sales ops' } })
    fireEvent.change(screen.getByLabelText('Width'), { target: { value: 'full' } })
    fireEvent.click(screen.getByLabelText('Show refresh'))
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    expect(bodies[0].settings).toEqual({
      public_live: true,
      variables: [{ name: 'region', type: 'text' }],
      app: {
        logo_attachment_id: 'att-1',
        title: 'Q3 Revenue',
        subtitle: 'Sales ops',
        width: 'full',
        show_header: true,
        show_refresh: false,
        show_last_updated: true,
      },
    })
    expect(onSaved).toHaveBeenCalledTimes(1)
  })

  test('clearing title and subtitle omits them from the payload', async () => {
    const bodies = capturePuts()
    renderPanel(dashboardWith({
      app: { title: 'Old title', subtitle: 'Old subtitle', logo_attachment_id: 'att-1' },
    }))

    fireEvent.change(screen.getByLabelText('App title'), { target: { value: '  ' } })
    fireEvent.change(screen.getByLabelText('Subtitle'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))

    await waitFor(() => expect(bodies.length).toBe(1))
    const app = (bodies[0].settings as Record<string, unknown>).app as Record<string, unknown>
    expect(app).toEqual({
      logo_attachment_id: 'att-1',
      width: 'fixed',
      show_header: true,
      show_refresh: true,
      show_last_updated: true,
    })
    expect('title' in app).toBe(false)
    expect('subtitle' in app).toBe(false)
  })

  test('shows the server error when saving fails', async () => {
    server.use(
      http.put('/api/v1/dashboards/d1', () =>
        HttpResponse.json({ error: 'app title must be at most 120 characters' }, { status: 400 }),
      ),
    )
    renderPanel(dashboardWith({}))
    fireEvent.click(screen.getByRole('button', { name: 'Save app settings' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('app title must be at most 120 characters')
  })

  test('closes on Escape and on backdrop click', () => {
    const { onClose } = renderPanel(dashboardWith({}))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByTestId('app-settings-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(2)
  })
})
