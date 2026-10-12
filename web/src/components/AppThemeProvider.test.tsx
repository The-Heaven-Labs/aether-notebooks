import { describe, expect, it, afterEach } from 'vitest'
import { act, render, screen, waitFor } from '@testing-library/react'
import { AppThemeProvider } from './AppThemeProvider'

afterEach(() => {
  document.documentElement.removeAttribute('data-theme')
})

function containerFor(text: string): HTMLElement {
  const el = screen.getByText(text).closest('.app-theme')
  if (!(el instanceof HTMLElement)) throw new Error('.app-theme container not found')
  return el
}

describe('AppThemeProvider', () => {
  it('renders the resolved scheme and vars on a .app-theme container with children inside', () => {
    render(
      <AppThemeProvider
        theme={{
          accent: '#7c6faa',
          background: '#ffffff',
          surface: '#fafafa',
          text: '#111111',
          border: '#e0e0e0',
          radius: 12,
          font_family: 'dm-sans',
        }}
        branding={null}
      >
        <span>app child</span>
      </AppThemeProvider>,
    )

    const container = containerFor('app child')
    expect(container).toHaveAttribute('data-app-scheme', 'light')
    expect(container.style.getPropertyValue('--accent')).toBe('#7c6faa')
    expect(container.style.getPropertyValue('--bg-primary')).toBe('#ffffff')
    expect(container.style.getPropertyValue('--bg-card')).toBe('#fafafa')
    expect(container.style.getPropertyValue('--text-primary')).toBe('#111111')
    expect(container.style.getPropertyValue('--border-light')).toBe('#e0e0e0')
    expect(container.style.getPropertyValue('--app-radius')).toBe('12px')
    // The chosen font restyles explicit `var(--font-sans)` consumers too.
    expect(container.style.getPropertyValue('--app-font')).toBe("'DM Sans', system-ui, sans-serif")
    expect(container.style.getPropertyValue('--font-sans')).toBe("'DM Sans', system-ui, sans-serif")
  })

  it('renders the container with no overrides when neither source defines a theme', () => {
    render(
      <AppThemeProvider>
        <span>plain</span>
      </AppThemeProvider>,
    )
    const container = containerFor('plain')
    expect(container).toHaveAttribute('data-app-scheme', 'light')
    expect(container.style.getPropertyValue('--accent')).toBe('')
    expect(container.style.getPropertyValue('--bg-primary')).toBe('')
  })

  it('auto follows the global data-theme scheme', () => {
    document.documentElement.setAttribute('data-theme', 'dark')
    render(
      <AppThemeProvider theme={{ color_scheme: 'auto' }}>
        <span>auto</span>
      </AppThemeProvider>,
    )
    expect(containerFor('auto')).toHaveAttribute('data-app-scheme', 'dark')
  })

  it('re-resolves when the global scheme flips while mounted', async () => {
    render(
      <AppThemeProvider>
        <span>live</span>
      </AppThemeProvider>,
    )
    expect(containerFor('live')).toHaveAttribute('data-app-scheme', 'light')

    act(() => {
      document.documentElement.setAttribute('data-theme', 'dark')
    })
    await waitFor(() => expect(containerFor('live')).toHaveAttribute('data-app-scheme', 'dark'))
  })

  it('lets a forced scheme win over the global one', () => {
    document.documentElement.setAttribute('data-theme', 'light')
    render(
      <AppThemeProvider theme={{ color_scheme: 'dark' }}>
        <span>forced</span>
      </AppThemeProvider>,
    )
    expect(containerFor('forced')).toHaveAttribute('data-app-scheme', 'dark')
  })

  it('falls back to org branding for the accent', () => {
    render(
      <AppThemeProvider branding={{ accent_color: '#123456' }}>
        <span>branded</span>
      </AppThemeProvider>,
    )
    expect(containerFor('branded').style.getPropertyValue('--accent')).toBe('#123456')
  })

  it('lets the dashboard accent win over the branding accent', () => {
    render(
      <AppThemeProvider theme={{ accent: '#7c6faa' }} branding={{ accent_color: '#123456' }}>
        <span>precedence</span>
      </AppThemeProvider>,
    )
    expect(containerFor('precedence').style.getPropertyValue('--accent')).toBe('#7c6faa')
  })
})
