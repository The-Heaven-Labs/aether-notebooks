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
 *  pattern used for matchMedia in these tests). */
function stubNarrowViewport() {
  vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({
    matches: true,
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
    expect(screen.getByText('Search files, dashboards, SQL…')).toBeInTheDocument()
  })

  it('shows the Ctrl K chip off Apple platforms', () => {
    renderWithProviders(<TopBar onOpenSearch={vi.fn()} />)

    const chip = screen.getByText('Ctrl K')
    expect(chip.tagName).toBe('KBD')
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
    expect(screen.queryByText('Search files, dashboards, SQL…')).toBeNull()
    expect(screen.queryByText('Ctrl K')).toBeNull()
  })

  it('opens the palette from the collapsed icon button', () => {
    stubNarrowViewport()
    const onOpenSearch = vi.fn()
    renderWithProviders(<TopBar onOpenSearch={onOpenSearch} />)

    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    expect(onOpenSearch).toHaveBeenCalledTimes(1)
  })
})
