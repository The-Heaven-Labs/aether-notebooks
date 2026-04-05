import { describe, it, expect, beforeEach } from 'vitest'
import { screen, fireEvent } from '@testing-library/react'
import { Sidebar } from '../components/Sidebar'
import { renderWithProviders, editorUser } from './utils'

beforeEach(() => {
  localStorage.clear()
})

describe('Sidebar', () => {
  it('renders all nav items including Groups and Profile', () => {
    renderWithProviders(<Sidebar />)
    expect(screen.getByTitle('Notebooks')).toBeDefined()
    expect(screen.getByTitle('Dashboards')).toBeDefined()
    expect(screen.getByTitle('Connectors')).toBeDefined()
    expect(screen.getByTitle('Members')).toBeDefined()
    expect(screen.getByTitle('Groups')).toBeDefined()
    expect(screen.getByTitle('Audit')).toBeDefined()
    expect(screen.getByTitle('Profile')).toBeDefined()
  })

  it('shows Admin badge on Groups link for admin users when expanded', () => {
    localStorage.setItem('hnb_sidebar_expanded', 'true')
    renderWithProviders(<Sidebar />) // default user is admin
    expect(screen.getByText('Admin')).toBeDefined()
  })

  it('does not show Admin badge for non-admin users', () => {
    localStorage.setItem('hnb_sidebar_expanded', 'true')
    renderWithProviders(<Sidebar />, { user: editorUser() })
    expect(screen.queryByText('Admin')).toBeNull()
  })

  it('persists expanded state to localStorage on toggle', () => {
    localStorage.setItem('hnb_sidebar_expanded', 'true')
    renderWithProviders(<Sidebar />)
    const toggle = screen.getByTitle('Collapse sidebar')
    fireEvent.click(toggle)
    expect(localStorage.getItem('hnb_sidebar_expanded')).toBe('false')
  })
})
