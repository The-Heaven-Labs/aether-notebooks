import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { TopBar } from '../components/TopBar'
import { isApplePlatform } from '../utils/platform'
import { renderWithProviders } from './utils'

// The top bar only needs the platform check for the kbd chord; the rest of the
// module is pure. Mocking it lets each test pin Apple vs. everyone else.
vi.mock('../utils/platform', () => ({ isApplePlatform: vi.fn(() => false) }))

/** Pin the viewport below the 768px collapse breakpoint (the shared stub
 *  pattern used for matchMedia in these tests). Only max-width queries match,
 *  so a future pointer/reduced-motion consumer can't silently flip. */
function stubNarrowViewport() {
  vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({
    matches: query.includes('max-width'),
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })))
}

describe('TopBar search field', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.mocked(isApplePlatform).mockReturnValue(false)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders a field-styled button with the placeholder and an accessible name', () => {
    renderWithProviders(<TopBar onOpenSearch={vi.fn()} />)

    const field = screen.getByRole('button', { name: 'Search' })
    expect(field.tagName).toBe('BUTTON')
    expect(field).toHaveAttribute('aria-keyshortcuts', 'Control+K Meta+K')
    expect(screen.getByText('Search files, dashboards, SQL…')).toBeInTheDocument()
  })

  it('shows the Ctrl K chip off Apple platforms', () => {
    renderWithProviders(<TopBar onOpenSearch={vi.fn()} />)

    const chip = screen.getByText('Ctrl K')
    expect(chip.tagName).toBe('KBD')
    // Chrome tone: the always-dark bar would nearly swallow the body-token cap.
    expect(chip).toHaveStyle({ backgroundColor: 'rgba(255, 255, 255, 0.08)' })
    expect(chip.getAttribute('style')).toContain('var(--nav-text)')
    expect(screen.queryByText('⌘K')).toBeNull()
  })

  it('shows the ⌘K chip on Apple platforms', () => {
    vi.mocked(isApplePlatform).mockReturnValue(true)
    renderWithProviders(<TopBar onOpenSearch={vi.fn()} />)

    expect(screen.getByText('⌘K')).toBeInTheDocument()
    expect(screen.queryByText('Ctrl K')).toBeNull()
  })

  it('opens the palette on click', async () => {
    const onOpenSearch = vi.fn()
    renderWithProviders(<TopBar onOpenSearch={onOpenSearch} />)

    await userEvent.click(screen.getByRole('button', { name: 'Search' }))
    expect(onOpenSearch).toHaveBeenCalledTimes(1)
  })

  it('activates with Enter and Space', async () => {
    const onOpenSearch = vi.fn()
    renderWithProviders(<TopBar onOpenSearch={onOpenSearch} />)

    const field = screen.getByRole('button', { name: 'Search' })
    field.focus()
    await userEvent.keyboard('{Enter}')
    await userEvent.keyboard('[Space]')
    expect(onOpenSearch).toHaveBeenCalledTimes(2)
  })

  it('renders no field when no opener is wired', () => {
    renderWithProviders(<TopBar />)

    expect(screen.queryByRole('button', { name: 'Search' })).toBeNull()
    expect(screen.queryByText('Search files, dashboards, SQL…')).toBeNull()
  })

  it('collapses to an icon-only button below 768px', () => {
    stubNarrowViewport()
    renderWithProviders(<TopBar onOpenSearch={vi.fn()} />)

    const field = screen.getByRole('button', { name: 'Search' })
    expect(field.tagName).toBe('BUTTON')
    expect(field).toHaveAttribute('aria-keyshortcuts', 'Control+K Meta+K')
    expect(screen.queryByText('Search files, dashboards, SQL…')).toBeNull()
    expect(screen.queryByText('Ctrl K')).toBeNull()
  })

  it('names the Ctrl K chord in the collapsed button tooltip off Apple platforms', () => {
    stubNarrowViewport()
    renderWithProviders(<TopBar onOpenSearch={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Search' }))
      .toHaveAttribute('title', 'Search (Ctrl K)')
  })

  it('names the ⌘K chord in the collapsed button tooltip on Apple platforms', () => {
    vi.mocked(isApplePlatform).mockReturnValue(true)
    stubNarrowViewport()
    renderWithProviders(<TopBar onOpenSearch={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Search' }))
      .toHaveAttribute('title', 'Search (⌘K)')
  })

  it('keeps a long org name on one line in the narrow bar', () => {
    stubNarrowViewport()
    localStorage.setItem('aether_org_name', 'A Very Long Organization Name')
    renderWithProviders(<TopBar onOpenSearch={vi.fn()} />)

    const orgName = screen.getByText('A Very Long Organization Name')
    expect(orgName).toHaveStyle({
      maxWidth: '160px',
      overflow: 'hidden',
      textOverflow: 'ellipsis',
      whiteSpace: 'nowrap',
    })
    // The cluster itself can shrink so the ellipsis kicks in before wrapping,
    // while the avatar holds its fixed 30px size.
    expect(orgName.parentElement).toHaveStyle({ minWidth: '0px' })
    expect(screen.getByRole('button', { name: 'Profile menu' })).toHaveStyle({ flexShrink: '0' })
  })

  it('opens the palette from the collapsed icon button', () => {
    stubNarrowViewport()
    const onOpenSearch = vi.fn()
    renderWithProviders(<TopBar onOpenSearch={onOpenSearch} />)

    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    expect(onOpenSearch).toHaveBeenCalledTimes(1)
  })
})
