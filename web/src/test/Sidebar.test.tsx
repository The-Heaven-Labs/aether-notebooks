import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from '../components/Sidebar'

beforeEach(() => {
  localStorage.clear()
})

describe('Sidebar', () => {
  it('renders all 5 navigation items', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )
    expect(screen.getByTitle('Notebooks')).toBeDefined()
    expect(screen.getByTitle('Dashboards')).toBeDefined()
    expect(screen.getByTitle('Connectors')).toBeDefined()
    expect(screen.getByTitle('Members')).toBeDefined()
    expect(screen.getByTitle('Audit')).toBeDefined()
  })

  it('persists expanded state to localStorage on toggle', () => {
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>
    )
    const toggle = screen.getByTitle('Expand sidebar')
    fireEvent.click(toggle)
    expect(localStorage.getItem('hnb_sidebar_expanded')).toBe('true')
    fireEvent.click(toggle)
    expect(localStorage.getItem('hnb_sidebar_expanded')).toBe('false')
  })
})
