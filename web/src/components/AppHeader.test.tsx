import type { ComponentProps, ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AppHeader } from './AppHeader'
import { formatExecutedAt } from '../utils/formatDateTime'

function renderHeader(
  overrides: Partial<ComponentProps<typeof AppHeader>> = {},
  children?: ReactNode,
) {
  const props: ComponentProps<typeof AppHeader> = {
    title: 'Executive Brief',
    width: 'fixed',
    showRefresh: true,
    showLastUpdated: true,
    lastUpdated: null,
    refreshing: false,
    onRefresh: () => {},
    ...overrides,
  }
  return render(
    <MemoryRouter>
      <AppHeader {...props}>{children}</AppHeader>
    </MemoryRouter>,
  )
}

describe('AppHeader', () => {
  it('renders title and subtitle', () => {
    renderHeader({ subtitle: 'Sales ops' })
    expect(screen.getByRole('heading', { name: 'Executive Brief' })).toBeInTheDocument()
    expect(screen.getByText('Sales ops')).toBeInTheDocument()
  })

  it('hides the subtitle when absent', () => {
    const { container } = renderHeader()
    // Title only: no subtitle line leaks into the masthead.
    expect(container.textContent).toBe('Executive Brief')
  })

  it('fires onRefresh when the refresh button is clicked', () => {
    const onRefresh = vi.fn()
    renderHeader({ onRefresh })
    fireEvent.click(screen.getByRole('button', { name: 'Refresh data' }))
    expect(onRefresh).toHaveBeenCalledTimes(1)
  })

  it('shows a spinner and disables the refresh button while refreshing', () => {
    const { rerender } = render(
      <MemoryRouter>
        <AppHeader
          title="Executive Brief"
          width="fixed"
          showRefresh
          showLastUpdated
          lastUpdated={null}
          refreshing={false}
          onRefresh={() => {}}
        />
      </MemoryRouter>,
    )
    const button = screen.getByRole('button', { name: 'Refresh data' })
    expect(button).toBeEnabled()
    expect(button.querySelector('svg')?.style.animation ?? '').not.toContain('spin')

    rerender(
      <MemoryRouter>
        <AppHeader
          title="Executive Brief"
          width="fixed"
          showRefresh
          showLastUpdated
          lastUpdated={null}
          refreshing
          onRefresh={() => {}}
        />
      </MemoryRouter>,
    )
    expect(button).toBeDisabled()
    expect(button.querySelector('svg')?.style.animation).toContain('spin')
  })

  it('hides the refresh button when showRefresh is false', () => {
    renderHeader({ showRefresh: false })
    expect(screen.queryByRole('button', { name: 'Refresh data' })).toBeNull()
  })

  it('shows Updated {time} when lastUpdated is set and showLastUpdated is not false', () => {
    const at = new Date('2026-10-11T14:30:00Z')
    renderHeader({ lastUpdated: at })
    expect(screen.getByRole('status')).toHaveTextContent(`Updated ${formatExecutedAt(at)}`)
  })

  it('hides freshness when showLastUpdated is false', () => {
    renderHeader({ lastUpdated: new Date(), showLastUpdated: false })
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('shows no Updated label before the first fetch', () => {
    renderHeader()
    expect(screen.queryByText(/Updated/)).toBeNull()
  })

  it('renders children in the actions slot', () => {
    renderHeader({}, <button type="button">Share</button>)
    expect(screen.getByRole('button', { name: 'Share' })).toBeInTheDocument()
  })

  it('renders a router back link when backTo is set', () => {
    renderHeader({ backTo: '/dashboards' })
    const back = screen.getByLabelText('Back to dashboards')
    expect(back).toHaveAttribute('href', '/dashboards')
  })

  it('omits the back link without backTo', () => {
    renderHeader()
    expect(screen.queryByLabelText('Back to dashboards')).toBeNull()
  })

  it('renders the logo before the title block when logoUrl is set', () => {
    const { container } = renderHeader({ logoUrl: '/api/v1/attachments/logo-1?token=abc' })
    const logo = container.querySelector('img')
    if (!logo) throw new Error('logo img not found')
    expect(logo).toHaveAttribute('src', '/api/v1/attachments/logo-1?token=abc')
    // Decorative: the adjacent h1 already announces the app name.
    expect(logo).toHaveAttribute('alt', '')
    expect(logo.style.height).toBe('24px')
    const heading = screen.getByRole('heading', { name: 'Executive Brief' })
    expect(logo.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('renders no logo without logoUrl', () => {
    const { container } = renderHeader()
    expect(container.querySelector('img')).toBeNull()
  })

  it('renders a minimal bar without a heading when the title is empty', () => {
    renderHeader({ title: '', backTo: '/dashboards' }, <button type="button">Share</button>)
    // show_header=false path: no title block, but the escape hatches remain.
    expect(screen.queryByRole('heading')).toBeNull()
    expect(screen.getByLabelText('Back to dashboards')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Share' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refresh data' })).toBeInTheDocument()
  })

  it('constrains content when width is fixed and spans when full', () => {
    const fixed = renderHeader({ width: 'fixed' })
    const fixedContent = fixed.container.firstElementChild?.firstElementChild as HTMLElement
    expect(fixedContent.style.maxWidth).toBe('1200px')
    fixed.unmount()

    const full = renderHeader({ width: 'full' })
    const fullContent = full.container.firstElementChild?.firstElementChild as HTMLElement
    expect(fullContent.style.maxWidth).toBe('')
  })

  it('renders a non-sticky strip with a bottom hairline', () => {
    const { container } = renderHeader()
    const header = container.firstElementChild as HTMLElement
    expect(header.tagName).toBe('HEADER')
    // The .app-header class owns the responsive padding (theme.css); the
    // hairline stays inline with the component.
    expect(header.className).toBe('app-header')
    expect(header.style.borderBottom).toBe('1px solid var(--border-light)')
    expect(header.style.position).not.toBe('sticky')
  })

  it('exposes 36px tap targets on the back and refresh controls', () => {
    renderHeader({ backTo: '/dashboards' })
    const back = screen.getByLabelText('Back to dashboards')
    expect(back.style.width).toBe('36px')
    expect(back.style.height).toBe('36px')
    const refresh = screen.getByRole('button', { name: 'Refresh data' })
    expect(refresh.style.width).toBe('36px')
    expect(refresh.style.height).toBe('36px')
  })
})
