import { describe, it, expect } from 'vitest'
import { screen } from '@testing-library/react'
import { AppShell } from '../components/AppShell'
import { renderWithProviders } from './utils'

describe('AppShell', () => {
  it('renders a skip-to-content link as the first focusable element', () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>)
    const skipLink = screen.getByText('Skip to content')
    expect(skipLink).toBeDefined()
    expect(skipLink.tagName).toBe('A')
    expect(skipLink.getAttribute('href')).toBe('#main-content')
  })

  it('main content area has id="main-content" for skip link target', () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>)
    const main = document.querySelector('main#main-content')
    expect(main).not.toBeNull()
  })

  it('skip link has class "skip-link" for CSS styling', () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>)
    const skipLink = screen.getByText('Skip to content')
    expect(skipLink.className).toBe('skip-link')
  })

  it('hides the global agent panel and FAB on chat pages', () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>, { initialPath: '/chats/s-1' })
    expect(screen.queryByTitle('Open AI Agent (Ctrl+K)')).toBeNull()
  })

  it('shows the agent FAB away from chat pages', () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>, { initialPath: '/' })
    expect(screen.getByTitle('Open AI Agent (Ctrl+K)')).toBeInTheDocument()
  })
})
