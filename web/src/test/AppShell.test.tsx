import { describe, it, expect, beforeEach } from 'vitest'
import { act, fireEvent, screen } from '@testing-library/react'
import { AppShell } from '../components/AppShell'
import { renderWithProviders } from './utils'

/** Flush React.lazy resolution and any state updates it scheduled. */
async function flushLazy() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

/**
 * Headroom for the lazy palette/agent-panel chunks: under the full parallel
 * suite their first fetch can exceed RTL's 1s default query timeout.
 */
const LAZY_TIMEOUT = { timeout: 5000 }

describe('AppShell', () => {
  beforeEach(() => {
    localStorage.clear()
  })

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
    expect(screen.queryByTitle('Open AI Agent (Ctrl+J)')).toBeNull()
  })

  it('shows the agent FAB away from chat pages', () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>, { initialPath: '/' })
    expect(screen.getByTitle('Open AI Agent (Ctrl+J)')).toBeInTheDocument()
  })

  it('hides the global agent panel on chat pages even when it was open', () => {
    localStorage.setItem('aether:agentDocked:__global__', 'true')
    renderWithProviders(<AppShell><div>Page content</div></AppShell>, { initialPath: '/chats/s-1' })
    expect(screen.queryByTitle('Close agent panel')).toBeNull()
  })

  it('opens the search palette on Ctrl+K without toggling the agent', async () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>)
    fireEvent.keyDown(document, { key: 'k', ctrlKey: true })

    // The palette is lazy-loaded, so the dialog arrives after the chunk loads.
    expect(await screen.findByRole('dialog', { name: 'Search palette' }, LAZY_TIMEOUT)).toBeInTheDocument()
    expect(screen.queryByTitle('Close agent panel')).toBeNull()
  })

  it('opens the search palette on Cmd+K (metaKey)', async () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>)
    fireEvent.keyDown(document, { key: 'k', metaKey: true })
    expect(await screen.findByRole('dialog', { name: 'Search palette' }, LAZY_TIMEOUT)).toBeInTheDocument()
  })

  it('opens the search palette from the top-bar search field', async () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>)

    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    // The palette is lazy-loaded, so the dialog arrives after the chunk loads.
    expect(await screen.findByRole('dialog', { name: 'Search palette' }, LAZY_TIMEOUT)).toBeInTheDocument()
  })

  it('toggles the global agent panel on Ctrl+J', async () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>)

    fireEvent.keyDown(document, { key: 'j', ctrlKey: true })
    expect(await screen.findByTitle('Close agent panel', {}, LAZY_TIMEOUT)).toBeInTheDocument()

    fireEvent.keyDown(document, { key: 'j', ctrlKey: true })
    expect(screen.queryByTitle('Close agent panel')).toBeNull()
  })

  it('ignores Ctrl+K while typing in an input or textarea', async () => {
    renderWithProviders(
      <AppShell>
        <input aria-label="Name" />
        <textarea aria-label="Notes" />
      </AppShell>,
    )
    // fireEvent returns dispatchEvent's result: false once a handler called
    // preventDefault, true when the guarded handler stood down.
    expect(fireEvent.keyDown(screen.getByLabelText('Name'), { key: 'k', ctrlKey: true })).toBe(true)
    expect(fireEvent.keyDown(screen.getByLabelText('Notes'), { key: 'k', ctrlKey: true })).toBe(true)
    await flushLazy()
    expect(screen.queryByRole('dialog', { name: 'Search palette' })).toBeNull()
  })

  it('ignores Ctrl+J while typing in an input or textarea', async () => {
    renderWithProviders(
      <AppShell>
        <input aria-label="Name" />
        <textarea aria-label="Notes" />
      </AppShell>,
    )
    expect(fireEvent.keyDown(screen.getByLabelText('Name'), { key: 'j', ctrlKey: true })).toBe(true)
    expect(fireEvent.keyDown(screen.getByLabelText('Notes'), { key: 'j', ctrlKey: true })).toBe(true)
    await flushLazy()
    expect(screen.queryByTitle('Close agent panel')).toBeNull()
  })

  it('ignores Ctrl+K inside a select', async () => {
    renderWithProviders(
      <AppShell>
        <select aria-label="Connector"><option>one</option></select>
      </AppShell>,
    )
    expect(fireEvent.keyDown(screen.getByLabelText('Connector'), { key: 'k', ctrlKey: true })).toBe(true)
    await flushLazy()
    expect(screen.queryByRole('dialog', { name: 'Search palette' })).toBeNull()
  })

  it('binds neither chord in bare mode', async () => {
    renderWithProviders(<AppShell bare><div>App surface</div></AppShell>)
    // A handled chord would have called preventDefault (false); no handler is
    // bound in bare mode, so both dispatches stay uncancelled.
    expect(fireEvent.keyDown(document, { key: 'k', ctrlKey: true })).toBe(true)
    expect(fireEvent.keyDown(document, { key: 'j', ctrlKey: true })).toBe(true)
    await flushLazy()
    expect(screen.queryByRole('dialog', { name: 'Search palette' })).toBeNull()
    expect(screen.queryByTitle('Close agent panel')).toBeNull()
  })

  it('suppresses Ctrl+J but keeps Ctrl+K enabled on chat pages', async () => {
    renderWithProviders(<AppShell><div>Page content</div></AppShell>, { initialPath: '/chats/s-1' })

    expect(fireEvent.keyDown(document, { key: 'j', ctrlKey: true })).toBe(true)
    await flushLazy()
    expect(screen.queryByTitle('Close agent panel')).toBeNull()

    // Search is global: the palette still opens on /chats.
    fireEvent.keyDown(document, { key: 'k', ctrlKey: true })
    expect(await screen.findByRole('dialog', { name: 'Search palette' }, LAZY_TIMEOUT)).toBeInTheDocument()
  })

  it('renders children in a bare main landmark: no shell chrome', () => {
    renderWithProviders(<AppShell bare><div>App surface</div></AppShell>)
    expect(screen.getByText('App surface')).toBeInTheDocument()
    // The app surface keeps a main landmark but no skip link, shell frame,
    // or agent FAB.
    expect(document.querySelector('main#main-content')).not.toBeNull()
    expect(screen.queryByText('Skip to content')).toBeNull()
    expect(document.querySelector('.app-shell')).toBeNull()
    expect(screen.queryByTitle('Open AI Agent (Ctrl+J)')).toBeNull()
  })
})
