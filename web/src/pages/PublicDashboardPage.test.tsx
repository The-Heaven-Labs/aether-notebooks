import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent } from '@testing-library/react'
import { Route, Routes, useLocation } from 'react-router-dom'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { PublicDashboardPage } from './PublicDashboardPage'

// The public surface renders through the share token; the logo streams from
// the token-scoped attachment route (no session credentials exist here).

const PUBLIC_TOKEN = 'pub-token-1'

function publicDashboard(overrides: Record<string, unknown> = {}) {
  return {
    id: 'd1',
    org_id: 'org-1',
    title: 'Quarterly Review',
    settings: {},
    created_by: 'user-1',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    widgets: [],
    widgets_data: {},
    ...overrides,
  }
}

function mockPublicDashboard(payload: Record<string, unknown>) {
  server.use(http.get(`/api/v1/public/${PUBLIC_TOKEN}`, () => HttpResponse.json(payload)))
}

// Exposes the router's current location so tests can assert that switching
// tabs keeps the token route and only sets the `tab` search param.
function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location">{location.pathname + location.search}</div>
}

function renderPublicPage(initialPath = `/public/dashboards/${PUBLIC_TOKEN}`) {
  return renderWithProviders(
    <>
      <LocationProbe />
      <Routes>
        <Route path="/public/dashboards/:token" element={<PublicDashboardPage />} />
      </Routes>
    </>,
    { initialPath },
  )
}

function logoImage(): HTMLImageElement | null {
  return document.querySelector<HTMLImageElement>('.app-theme img')
}

describe('PublicDashboardPage app surface', () => {
  test('renders the token-scoped dashboard logo inside the themed surface', async () => {
    mockPublicDashboard(publicDashboard({
      settings: { app: { title: 'Executive Brief', logo_attachment_id: 'logo-1' } },
      org_branding: { logo_attachment_id: 'org-logo' },
    }))
    renderPublicPage()
    expect(await screen.findByRole('heading', { name: 'Executive Brief' })).toBeInTheDocument()

    const themeContainer = document.querySelector('.app-theme')
    if (!themeContainer) throw new Error('.app-theme container not found')
    expect(themeContainer).toContainElement(screen.getByRole('heading', { name: 'Executive Brief' }))

    // The dashboard logo wins over the org fallback and streams through the
    // share token.
    const logo = logoImage()
    if (!logo) throw new Error('logo img not found')
    expect(logo).toHaveAttribute('src', `/api/v1/public/${PUBLIC_TOKEN}/attachments/logo-1`)
    expect(logo).toHaveAttribute('alt', '')
  })

  test('falls back to the org logo when the dashboard has none', async () => {
    mockPublicDashboard(publicDashboard({
      org_branding: { logo_attachment_id: 'org-logo' },
    }))
    renderPublicPage()
    expect(await screen.findByRole('heading', { name: 'Quarterly Review' })).toBeInTheDocument()

    const logo = logoImage()
    if (!logo) throw new Error('logo img not found')
    expect(logo).toHaveAttribute('src', `/api/v1/public/${PUBLIC_TOKEN}/attachments/org-logo`)
  })

  test('renders no logo when neither source sets one', async () => {
    mockPublicDashboard(publicDashboard())
    renderPublicPage()
    expect(await screen.findByRole('heading', { name: 'Quarterly Review' })).toBeInTheDocument()
    expect(logoImage()).toBeNull()
    // No org app name: the footer keeps the platform attribution.
    expect(screen.getByText('Powered by Aether')).toBeInTheDocument()
  })

  test('uses the org app name in the footer attribution when set', async () => {
    mockPublicDashboard(publicDashboard({ org_branding: { app_name: 'Heaven Labs' } }))
    renderPublicPage()
    expect(await screen.findByRole('heading', { name: 'Quarterly Review' })).toBeInTheDocument()

    expect(screen.getByText('Powered by Heaven Labs')).toBeInTheDocument()
    expect(screen.queryByText('Powered by Aether')).not.toBeInTheDocument()
  })
})

describe('PublicDashboardPage chapter tabs', () => {
  const TABS = [
    { id: 'overview', title: 'Overview' },
    { id: 'orders', title: 'Orders' },
  ]

  function panelWidget(id: string, tabId: string, label: string, col: number): Record<string, unknown> {
    return {
      id,
      dashboard_id: 'd1',
      notebook_id: null,
      cell_id: null,
      connector_id: null,
      query: null,
      language: 'sql',
      type: 'text',
      layout: { row: 0, col, width: 6, height: 6, tab: tabId },
      config: { text: { markdown: `${label} panel` } },
      created_at: '2026-01-01T00:00:00Z',
    }
  }

  beforeEach(() => {
    mockPublicDashboard(publicDashboard({
      settings: { tabs: TABS },
      widgets: [panelWidget('w1', 'overview', 'Overview', 0), panelWidget('w2', 'orders', 'Orders', 6)],
    }))
  })

  test('renders the tab bar, hides the inactive chapter, and keeps the token route on switch', async () => {
    renderPublicPage()
    await screen.findByRole('tab', { name: 'Overview' })

    // Both chapters stay mounted (their data loads in one pass); only the
    // active one is visible.
    expect(screen.getByText('Overview panel')).toBeVisible()
    expect(screen.getByText('Orders panel')).not.toBeVisible()
    expect(screen.getByTestId('location')).toHaveTextContent(`/public/dashboards/${PUBLIC_TOKEN}`)

    fireEvent.click(screen.getByRole('tab', { name: 'Orders' }))

    expect(screen.getByRole('tab', { name: 'Orders' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('Overview panel')).not.toBeVisible()
    expect(screen.getByText('Orders panel')).toBeVisible()
    // The token route is intact; only the `tab` param was set.
    expect(screen.getByTestId('location')).toHaveTextContent(`/public/dashboards/${PUBLIC_TOKEN}?tab=orders`)
  })

  test('an unknown ?tab falls back to the first chapter', async () => {
    renderPublicPage(`/public/dashboards/${PUBLIC_TOKEN}?tab=nope`)
    await screen.findByRole('tab', { name: 'Overview' })

    expect(screen.getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('Overview panel')).toBeVisible()
    expect(screen.getByText('Orders panel')).not.toBeVisible()
  })
})
