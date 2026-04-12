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
    expect(screen.getByTitle('Home')).toBeDefined()
    expect(screen.getByTitle('Groups')).toBeDefined()
    expect(screen.queryByTitle('Profile')).toBeNull()
  })

  it('never shows Admin badge (feature removed)', () => {
    localStorage.setItem('hnb_sidebar_expanded', 'true')
    renderWithProviders(<Sidebar />)
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
