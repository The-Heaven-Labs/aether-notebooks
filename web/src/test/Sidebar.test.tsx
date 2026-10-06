import { describe, it, expect, beforeEach } from 'vitest'
import { screen, fireEvent } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { Sidebar } from '../components/Sidebar'
import { renderWithProviders, viewerUser } from './utils'
import { server } from './server'

beforeEach(() => {
  localStorage.clear()
})

describe('Sidebar', () => {
  it('renders nav items without Profile', () => {
    renderWithProviders(<Sidebar />)
    expect(screen.getByTitle('Browse notebooks, dashboards, and connectors organized in folders')).toBeDefined()
    expect(screen.getByTitle('Permission groups for access control')).toBeDefined()
    expect(screen.queryByTitle('Profile')).toBeNull()
  })

  it('never shows Admin badge (feature removed)', () => {
    localStorage.setItem('aether_sidebar_expanded', 'true')
    renderWithProviders(<Sidebar />)
    expect(screen.queryByText('Admin')).toBeNull()
  })

  it('persists expanded state to localStorage on toggle', () => {
    localStorage.setItem('aether_sidebar_expanded', 'true')
    renderWithProviders(<Sidebar />)
    const toggle = screen.getByTitle('Collapse sidebar')
    fireEvent.click(toggle)
    expect(localStorage.getItem('aether_sidebar_expanded')).toBe('false')
  })

  it('active nav link announces current page to screen readers', () => {
    renderWithProviders(<Sidebar />, { initialPath: '/' })
    // The Files link is active at "/" — it should contain a sr-only "(current page)" span
    const filesLink = screen.getByTitle('Browse notebooks, dashboards, and connectors organized in folders')
    const anchor = filesLink.closest('a') || filesLink
    expect(anchor.textContent).toContain('(current page)')
  })

  it('inactive nav links do not announce current page', () => {
    renderWithProviders(<Sidebar />, { initialPath: '/' })
    const dashboardsLink = screen.getByTitle('Visual dashboards built from notebook query results')
    const anchor = dashboardsLink.closest('a') || dashboardsLink
    expect(anchor.textContent).not.toContain('(current page)')
  })
})

const WAREHOUSES_TITLE = 'ClickHouse access namespaces, provisioners, and table grants'

function connectorFixture(type: string) {
  return { id: `c-${type}`, name: type, type, created_at: '2026-01-01T00:00:00Z' }
}

describe('Sidebar Warehouses entry', () => {
  it('hides Warehouses when the org has no ClickHouse connector', async () => {
    server.use(http.get('/api/v1/connectors', () => HttpResponse.json([connectorFixture('postgres')])))
    renderWithProviders(<Sidebar />)
    // Let the connectors query settle before asserting absence.
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByTitle(WAREHOUSES_TITLE)).toBeNull()
  })

  it('shows Warehouses when a ClickHouse connector exists', async () => {
    server.use(
      http.get('/api/v1/connectors', () =>
        HttpResponse.json([connectorFixture('postgres'), connectorFixture('clickhouse')]),
      ),
    )
    renderWithProviders(<Sidebar />)
    expect(await screen.findByTitle(WAREHOUSES_TITLE)).toBeDefined()
  })

  it('hides Warehouses for non-admins even with a ClickHouse connector', async () => {
    server.use(http.get('/api/v1/connectors', () => HttpResponse.json([connectorFixture('clickhouse')])))
    renderWithProviders(<Sidebar />, { user: viewerUser() })
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(screen.queryByTitle(WAREHOUSES_TITLE)).toBeNull()
  })
})
