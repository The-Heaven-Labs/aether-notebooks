import { describe, test, expect, beforeEach, vi } from 'vitest'
import { screen, fireEvent, waitFor, act } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { favoritesFixture } from '../test/handlers'
import { renderWithProviders } from '../test/utils'
import { SearchPalette } from './SearchPalette'
import type { FavoriteResourceType, ResourceSummary, SearchGroup } from '../types'

const navigateMock = vi.hoisted(() => vi.fn())
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => navigateMock }
})

function summary(
  type: FavoriteResourceType,
  id: string,
  title: string,
  extra: Partial<ResourceSummary> = {},
): ResourceSummary {
  return {
    id,
    resource_type: type,
    title,
    subtitle: type,
    updated_at: '2026-01-01T00:00:00Z',
    is_favorite: false,
    can_edit: true,
    ...extra,
  }
}

function group(type: FavoriteResourceType, results: ResourceSummary[]): SearchGroup {
  return { resource_type: type, has_more: false, results }
}

const searchRequests: string[] = []

/** Registers a `/search` handler that serves `groups` and records every URL. */
function useSearchGroups(groups: SearchGroup[]) {
  server.use(
    http.get('/api/v1/search', ({ request }) => {
      searchRequests.push(request.url)
      const q = new URL(request.url).searchParams.get('q') ?? ''
      return HttpResponse.json({ query: q, groups })
    }),
  )
}

function renderPalette(props: Partial<Parameters<typeof SearchPalette>[0]> = {}) {
  const onClose = vi.fn()
  const utils = renderWithProviders(<SearchPalette open onClose={onClose} {...props} />)
  return { onClose, ...utils }
}

function typeQuery(value: string) {
  fireEvent.change(screen.getByLabelText('Search'), { target: { value } })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

beforeEach(() => {
  searchRequests.length = 0
  navigateMock.mockClear()
})

describe('SearchPalette', () => {
  test('debounces rapid typing into exactly one search request', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderPalette()

    fireEvent.change(screen.getByLabelText('Search'), { target: { value: 'r' } })
    fireEvent.change(screen.getByLabelText('Search'), { target: { value: 're' } })
    fireEvent.change(screen.getByLabelText('Search'), { target: { value: 'rev' } })
    expect(searchRequests).toHaveLength(0)

    await waitFor(() => expect(searchRequests).toHaveLength(1))
    await screen.findByRole('option', { name: /Revenue by region/ })
    expect(searchRequests).toHaveLength(1)
    expect(new URL(searchRequests[0]).searchParams.get('q')).toBe('rev')
  })

  test('closing before the debounce settles never fires a request', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    const { onClose, rerender } = renderPalette()
    fireEvent.change(screen.getByLabelText('Search'), { target: { value: 'rev' } })
    rerender(<SearchPalette open={false} onClose={onClose} />)

    // Well past the 150ms debounce: the timer was cancelled with the close.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200))
    })
    expect(searchRequests).toHaveLength(0)
  })

  test('Enter before the debounce settles flushes instead of opening a stale row', async () => {
    server.use(...favoritesFixture([{ type: 'notebook', id: 'nb-fav' }]).handlers)
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderPalette()
    // The empty state's favorite row is the active row until the query lands.
    await screen.findByText('nb-fav')

    const input = screen.getByLabelText('Search')
    fireEvent.change(input, { target: { value: 'rev' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(navigateMock).not.toHaveBeenCalled()

    // The flush kicked the search off; the settled Enter opens the fresh row.
    await screen.findByRole('option', { name: /Revenue by region/ })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(navigateMock).toHaveBeenCalledWith('/notebooks/nb-1')
  })

  test('a second Enter cannot open kept placeholder rows while the flush query loads', async () => {
    const two = deferred<void>()
    server.use(
      http.get('/api/v1/search', async ({ request }) => {
        const q = new URL(request.url).searchParams.get('q') ?? ''
        if (q === 'two') await two.promise
        return HttpResponse.json({
          query: q,
          groups: [group('notebook', [
            q === 'two' ? summary('notebook', 'nb-two', 'Second result') : summary('notebook', 'nb-one', 'First result'),
          ])],
        })
      }),
    )
    renderPalette()
    const input = screen.getByLabelText('Search')
    typeQuery('one')
    await screen.findByRole('option', { name: /First result/ })

    // Flush "two"; its fetch is held open, so "one" stays as placeholder data.
    fireEvent.change(input, { target: { value: 'two' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(screen.getByRole('option', { name: /First result/ })).toBeInTheDocument()

    // A second Enter must not open the kept, stale "one" row.
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(navigateMock).not.toHaveBeenCalled()

    await act(async () => {
      two.resolve()
      await two.promise
    })
    await screen.findByRole('option', { name: /Second result/ })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(navigateMock).toHaveBeenCalledWith('/notebooks/nb-two')
  })

  test('renders groups in API order with highlighted matches and meta', async () => {
    useSearchGroups([
      group('notebook', [
        summary('notebook', 'nb-1', 'Revenue by region', { snippet: 'SELECT revenue FROM sales', folder_path: 'Finance', subtitle: 'Notebook' }),
      ]),
      group('dashboard', [summary('dashboard', 'd-1', 'Revenue dashboard')]),
    ])
    renderPalette()
    typeQuery('revenue')

    await screen.findByRole('option', { name: /Revenue by region/ })
    const headers = screen.getAllByText(/^(Notebooks|Dashboards)$/).map((el) => el.textContent)
    expect(headers).toEqual(['Notebooks', 'Dashboards'])

    // Every occurrence of the term is marked, in titles and snippets alike.
    expect(screen.getAllByText(/revenue/i, { selector: 'mark.search-hit' })).toHaveLength(3)
    expect(screen.getByText(/SELECT/)).toBeInTheDocument()
    expect(screen.getByText('Finance · Notebook')).toBeInTheDocument()
  })

  test('skips result groups carrying an unknown resource type', async () => {
    server.use(
      http.get('/api/v1/search', ({ request }) => {
        const q = new URL(request.url).searchParams.get('q') ?? ''
        return HttpResponse.json({
          query: q,
          groups: [
            // A retired type has no icon and would crash the render.
            { resource_type: 'widget', has_more: false, results: [summary('notebook', 'nb-ghost', 'Ghost row')] },
            group('notebook', [summary('notebook', 'nb-2', 'Real row')]),
          ],
        })
      }),
    )
    renderPalette()
    typeQuery('row')

    expect(await screen.findByRole('option', { name: /Real row/ })).toBeInTheDocument()
    expect(screen.getAllByRole('option')).toHaveLength(2) // result + View all results
    expect(screen.queryByText(/Ghost row/)).toBeNull()
  })

  test('arrows wrap across all rows including View all results', async () => {
    useSearchGroups([
      group('notebook', [summary('notebook', 'nb-1', 'Notebook hit')]),
      group('dashboard', [summary('dashboard', 'd-1', 'Dashboard hit')]),
    ])
    renderPalette()
    typeQuery('hit')
    await screen.findByRole('option', { name: /Notebook hit/ })

    const input = screen.getByLabelText('Search')
    // The listbox carries its own accessible name (the combobox input's label
    // does not carry through aria-labelledby in Chromium's AX tree).
    const listbox = screen.getByRole('listbox', { name: 'Search results' })
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(3) // two results + View all results
    expect(options[2].textContent).toContain('View all results')
    // The combobox input owns the active-descendant pointer (finding E1-2).
    expect(input).toHaveAttribute('role', 'combobox')
    expect(input).toHaveAttribute('aria-expanded', 'true')
    expect(input).toHaveAttribute('aria-controls', listbox.id)
    expect(input).toHaveAttribute('aria-activedescendant', options[0].id)

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(input).toHaveAttribute('aria-activedescendant', options[1].id)
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(input).toHaveAttribute('aria-activedescendant', options[2].id)
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    expect(input).toHaveAttribute('aria-activedescendant', options[0].id)
    fireEvent.keyDown(input, { key: 'ArrowUp' })
    expect(input).toHaveAttribute('aria-activedescendant', options[2].id)

    fireEvent.keyDown(input, { key: 'Home' })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(navigateMock).toHaveBeenLastCalledWith('/notebooks/nb-1')

    fireEvent.keyDown(input, { key: 'End' })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(navigateMock).toHaveBeenLastCalledWith('/search?q=hit')
  })

  const ENTER_TARGETS: Array<[FavoriteResourceType, string, string]> = [
    ['notebook', 'nb-1', '/notebooks/nb-1'],
    ['dashboard', 'd-1', '/dashboards/d-1/view'],
    ['folder', 'f-1', '/?folder=f-1'],
    ['connector', 'c-1', '/connectors'],
    ['agent', 'a-1', '/agents'],
    ['model_config', 'm-1', '/models'],
    ['skill', 's-1', '/skills'],
    ['tool', 't-1', '/tools'],
    ['mcp_server', 'mcp-1', '/mcps'],
  ]

  test.each(ENTER_TARGETS)('Enter opens a %s at %s', async (type, id, href) => {
    useSearchGroups([group(type, [summary(type, id, 'Match me')])])
    renderPalette()
    typeQuery('match')
    await screen.findByRole('option', { name: /Match me/ })

    fireEvent.keyDown(screen.getByLabelText('Search'), { key: 'Enter' })
    expect(navigateMock).toHaveBeenCalledWith(href)
  })

  test('clicking a row opens it and closes the palette', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    const { onClose } = renderPalette()
    typeQuery('rev')

    fireEvent.click(await screen.findByRole('option', { name: /Revenue by region/ }))
    expect(navigateMock).toHaveBeenCalledWith('/notebooks/nb-1')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  test('View all results defers to onOpenResults when wired', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    const onOpenResults = vi.fn()
    renderPalette({ onOpenResults })
    typeQuery('rev')
    await screen.findByRole('option', { name: /Revenue by region/ })

    const input = screen.getByLabelText('Search')
    fireEvent.keyDown(input, { key: 'End' })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(onOpenResults).toHaveBeenCalledWith('rev')
    expect(navigateMock).not.toHaveBeenCalled()
  })

  test('never highlights the View all results row against the query', async () => {
    // "results" would match the synthetic row's own title.
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Playoff results')])])
    renderPalette()
    typeQuery('results')

    const viewAll = await screen.findByRole('option', { name: 'View all results' })
    expect(viewAll.querySelector('mark.search-hit')).toBeNull()
    // The real result still highlights.
    expect(screen.getAllByText('results', { selector: 'mark.search-hit' }).length).toBeGreaterThan(0)
  })

  test('Escape closes the palette', () => {
    const { onClose } = renderPalette()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  test('renders nothing when closed', () => {
    renderPalette({ open: false })
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.queryByLabelText('Search')).toBeNull()
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  test('takes focus into the search input on open', async () => {
    renderPalette()
    await waitFor(() => expect(screen.getByLabelText('Search')).toHaveFocus())
  })

  test('empty query shows Favorites and Recents', async () => {
    server.use(...favoritesFixture([{ type: 'notebook', id: 'nb-fav' }, { type: 'dashboard', id: 'dash-fav' }]).handlers)
    renderPalette()

    expect(await screen.findByText('Favorites')).toBeInTheDocument()
    expect(screen.getByText('Recents')).toBeInTheDocument()
    // favoriteSummary() uses the resource id as the title.
    expect(await screen.findByText('nb-fav')).toBeInTheDocument()
    expect(screen.getByText('dash-fav')).toBeInTheDocument()
    // The default /recent handler serves one notebook.
    expect(await screen.findByText('Root Notebook')).toBeInTheDocument()
    expect(searchRequests).toHaveLength(0)
    // "View all results" belongs to a query, not the empty state.
    expect(screen.queryByText('View all results')).toBeNull()
  })

  test('a query with no results names it', async () => {
    useSearchGroups([])
    renderPalette()
    typeQuery('zzz')
    expect(await screen.findByText('No matches for “zzz”')).toBeInTheDocument()
  })

  test('a failed search offers a retry that recovers', async () => {
    let calls = 0
    server.use(
      http.get('/api/v1/search', ({ request }) => {
        calls += 1
        if (calls === 1) return HttpResponse.json({ error: 'search failed' }, { status: 500 })
        const q = new URL(request.url).searchParams.get('q') ?? ''
        return HttpResponse.json({
          query: q,
          groups: [group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])],
        })
      }),
    )
    renderPalette()
    typeQuery('rev')

    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('option', { name: /Revenue by region/ })).toBeInTheDocument()
    expect(calls).toBe(2)
  })

  test('closes when the scrim is clicked', () => {
    const { onClose } = renderPalette()
    fireEvent.click(screen.getByRole('dialog', { name: 'Search palette' }).parentElement!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
