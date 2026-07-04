import { describe, it, expect, beforeEach } from 'vitest'
import { screen, fireEvent } from '@testing-library/react'
import { Sidebar } from '../components/Sidebar'
import { renderWithProviders } from './utils'

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
