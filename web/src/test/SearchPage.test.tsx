import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { useLocation, useNavigate } from 'react-router-dom'
import { server } from './server'
import { favoritesFixture } from './handlers'
import { renderWithProviders } from './utils'
import { SearchPage } from '../pages/SearchPage'
import type { FavoriteResourceType, ResourceSummary, SearchGroup } from '../types'

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

function group(type: FavoriteResourceType, results: ResourceSummary[], hasMore = false): SearchGroup {
  return { resource_type: type, has_more: hasMore, results }
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

/** Renders the search URL state so tests can assert on it. */
function LocationProbe() {
  const { pathname, search, key } = useLocation()
  return (
    <>
      <div data-testid="location">{pathname}{search}</div>
      <div data-testid="location-key">{key}</div>
    </>
  )
}

/** Pushes/restores history entries the way the browser back button would. */
function HistoryProbe() {
  const navigate = useNavigate()
  const { pathname, search } = useLocation()
  return (
    <>
      <button type="button" onClick={() => navigate(-1)}>probe back</button>
      <button type="button" onClick={() => navigate(1)}>probe forward</button>
      <button type="button" onClick={() => navigate('/search?q=other')}>probe other</button>
      {/* Pushes a duplicate of the current URL, like a repeated navigation. */}
      <button type="button" onClick={() => navigate(pathname + search)}>probe same</button>
    </>
  )
}

function renderSearch(initialPath = '/search') {
  return renderWithProviders(
    <>
      <SearchPage />
      <LocationProbe />
      <HistoryProbe />
    </>,
    { initialPath },
  )
}

function locationSearch(): string {
  return screen.getByTestId('location').textContent ?? ''
}

function locationKey(): string {
  return screen.getByTestId('location-key').textContent ?? ''
}

function searchInput(): HTMLInputElement {
  return screen.getByRole('textbox', { name: 'Search' })
}

function typeQuery(value: string) {
  fireEvent.change(searchInput(), { target: { value } })
}

function lastRequest(): URLSearchParams {
  return new URL(searchRequests[searchRequests.length - 1]).searchParams
}

beforeEach(() => {
  searchRequests.length = 0
})

describe('SearchPage', () => {
  test('deep link fetches at limit=20 and renders groups with highlights', async () => {
    useSearchGroups([
      group('notebook', [
        summary('notebook', 'nb-1', 'Revenue by region', {
          snippet: 'SELECT revenue FROM sales',
          folder_path: 'Finance',
          subtitle: 'Notebook',
        }),
      ]),
    ])
    renderSearch('/search?q=revenue')

    const link = await screen.findByRole('link', { name: /Revenue by region/ })
    expect(link).toHaveAttribute('href', '/notebooks/nb-1')
    expect(screen.getByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()

    await waitFor(() => expect(searchRequests).toHaveLength(1))
    expect(lastRequest().get('q')).toBe('revenue')
    expect(lastRequest().get('limit')).toBe('20')

    // Highlighting marks every occurrence of the term in title and snippet.
    expect(screen.getAllByText(/revenue/i, { selector: 'mark.search-hit' })).toHaveLength(2)
    // The snippet is split by its mark; the surviving segments still render.
    expect(screen.getByText(/SELECT/)).toBeInTheDocument()
    expect(screen.getByText('Finance · Notebook')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('1 result')
    // No group reported more matches.
    expect(screen.queryByText('More matches — refine your search')).toBeNull()
  })

  test('a notebook content hit links to the matched cell hash', async () => {
    useSearchGroups([
      group('notebook', [
        summary('notebook', 'nb-1', 'Revenue by region', {
          snippet: 'SELECT revenue FROM sales',
          cell_id: 'a1b2c3',
        }),
        summary('dashboard', 'd-1', 'Revenue dashboard', { cell_id: 'ignored' }),
      ]),
    ])
    renderSearch('/search?q=revenue')

    const link = await screen.findByRole('link', { name: /Revenue by region/ })
    expect(link).toHaveAttribute('href', '/notebooks/nb-1#cell-a1b2c3')
    // A cell id on a non-notebook type is ignored: only notebooks deep-link.
    expect(screen.getByRole('link', { name: /Revenue dashboard/ }))
      .toHaveAttribute('href', '/dashboards/d-1/view')
  })

  test('typing debounces into the URL and refetches once', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderSearch()
    expect(searchInput()).toHaveValue('')

    typeQuery('r')
    typeQuery('re')
    typeQuery('rev')
    expect(searchRequests).toHaveLength(0)

    await waitFor(() => expect(searchRequests).toHaveLength(1))
    expect(locationSearch()).toContain('q=rev')
    expect(lastRequest().get('q')).toBe('rev')
    expect(await screen.findByRole('link', { name: /Revenue by region/ })).toBeInTheDocument()
  })

  test('a type chip filters the request and All clears it', async () => {
    useSearchGroups([
      group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')]),
      group('dashboard', [summary('dashboard', 'd-1', 'Revenue dashboard')]),
    ])
    renderSearch('/search?q=revenue')
    await screen.findByRole('link', { name: /Revenue by region/ })

    fireEvent.click(screen.getByRole('button', { name: 'Dashboards' }))
    await waitFor(() => expect(lastRequest().get('types')).toBe('dashboard'))
    expect(locationSearch()).toContain('type=dashboard')
    expect(screen.getByRole('button', { name: 'Dashboards' })).toHaveAttribute('aria-pressed', 'true')

    // All clears the filter. The unfiltered key is still cached, so assert the
    // URL/chip state rather than a request that React Query rightly skips.
    fireEvent.click(screen.getByRole('button', { name: 'All' }))
    await waitFor(() => expect(locationSearch()).not.toContain('type=dashboard'))
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true')
    expect(lastRequest().get('types')).toBe('dashboard') // request 1 was unfiltered
    expect(new URL(searchRequests[0]).searchParams.get('types')).toBeNull()
  })

  test('the Favorites chip sends favorites=1 and toggles back off', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderSearch('/search?q=revenue')
    await screen.findByRole('link', { name: /Revenue by region/ })

    const chip = screen.getByRole('button', { name: 'Favorites' })
    expect(chip).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(chip)
    await waitFor(() => expect(lastRequest().get('favorites')).toBe('1'))
    expect(locationSearch()).toContain('favorites=1')
    expect(screen.getByRole('button', { name: 'Favorites' })).toHaveAttribute('aria-pressed', 'true')

    // Toggling back off restores the unfiltered URL state (that request is
    // still cached, so the assertion is on the URL and chip).
    fireEvent.click(screen.getByRole('button', { name: 'Favorites' }))
    await waitFor(() => expect(locationSearch()).not.toContain('favorites=1'))
    expect(screen.getByRole('button', { name: 'Favorites' })).toHaveAttribute('aria-pressed', 'false')
  })

  test('type chips render only for types with results', async () => {
    useSearchGroups([
      group('folder', [summary('folder', 'f-1', 'Finance')]),
      group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')]),
    ])
    renderSearch('/search?q=fin')
    await screen.findByRole('link', { name: /Finance/ })

    expect(screen.getByRole('button', { name: 'All' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Folders' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Notebooks' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Favorites' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Dashboards' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Connectors' })).toBeNull()
  })

  test('the has_more line appears only when a group reports more', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')], true)])
    renderSearch('/search?q=revenue')
    await screen.findByRole('link', { name: /Revenue by region/ })

    expect(screen.getByText('More matches — refine your search')).toBeInTheDocument()
    // The count carries the truncation: at least one more match exists.
    expect(screen.getByRole('status')).toHaveTextContent('1+ results')
  })

  test('no results names the query', async () => {
    useSearchGroups([])
    renderSearch('/search?q=zzz')

    expect(await screen.findByText('No matches for “zzz”')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('No results')
  })

  test('a failed search offers a retry that recovers', async () => {
    let calls = 0
    server.use(
      http.get('/api/v1/search', () => {
        calls += 1
        if (calls === 1) return HttpResponse.json({ error: 'search failed' }, { status: 500 })
        return HttpResponse.json({
          query: 'rev',
          groups: [group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])],
        })
      }),
    )
    renderSearch('/search?q=rev')

    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }))
    expect(await screen.findByRole('link', { name: /Revenue by region/ })).toBeInTheDocument()
    expect(calls).toBe(2)
  })

  test('an empty query shows the orientation panel and focuses the input', async () => {
    renderSearch()

    expect(await screen.findByText(
      'Search across notebooks, dashboards, folders, connectors, agents, models, skills, tools, and MCP servers.',
    )).toBeInTheDocument()
    expect(screen.getByText('Start typing to see results.')).toBeInTheDocument()
    await waitFor(() => expect(searchInput()).toHaveFocus())
    // No query, no request, and no filter chrome.
    expect(searchRequests).toHaveLength(0)
    expect(screen.queryByRole('button', { name: 'All' })).toBeNull()
  })

  test('favorite toggling works from a result row without navigating', async () => {
    server.use(...favoritesFixture().handlers)
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderSearch('/search?q=rev')

    await userEvent.click(await screen.findByRole('button', { name: 'Add to favorites' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Remove from favorites' }))
        .toHaveAttribute('aria-pressed', 'true'),
    )
    // The row is a link; the toggle is its own control.
    expect(locationSearch()).toBe('/search?q=rev')
  })

  test('deep-linked filter params seed the chips and the request', async () => {
    server.use(...favoritesFixture([{ type: 'notebook', id: 'nb-1' }]).handlers)
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderSearch('/search?q=rev&type=notebook&favorites=1')
    await screen.findByRole('link', { name: /Revenue by region/ })

    await waitFor(() => expect(searchRequests).toHaveLength(1))
    expect(lastRequest().get('types')).toBe('notebook')
    expect(lastRequest().get('favorites')).toBe('1')
    expect(screen.getByRole('button', { name: 'Notebooks' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Favorites' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'false')
  })

  test('back/forward restores the chip state', async () => {
    useSearchGroups([
      group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')]),
      group('dashboard', [summary('dashboard', 'd-1', 'Revenue dashboard')]),
    ])
    renderSearch('/search?q=revenue')
    await screen.findByRole('link', { name: /Revenue by region/ })

    fireEvent.click(screen.getByRole('button', { name: 'Dashboards' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Dashboards' })).toHaveAttribute('aria-pressed', 'true'))

    fireEvent.click(screen.getByRole('button', { name: 'probe back' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'true'))
    expect(screen.getByRole('button', { name: 'Dashboards' })).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(screen.getByRole('button', { name: 'probe forward' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Dashboards' })).toHaveAttribute('aria-pressed', 'true'))
  })

  test('back restores the query into the input', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderSearch()

    typeQuery('rev')
    await waitFor(() => expect(locationSearch()).toContain('q=rev'))

    fireEvent.click(screen.getByRole('button', { name: 'probe other' }))
    await waitFor(() => expect(searchInput()).toHaveValue('other'))

    fireEvent.click(screen.getByRole('button', { name: 'probe back' }))
    await waitFor(() => expect(searchInput()).toHaveValue('rev'))
  })

  test('Back within the debounce window restores the destination and discards the draft', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderSearch('/search?q=revenue')
    await screen.findByRole('link', { name: /Revenue by region/ })

    // A chip pushes a second entry; its `q` equals the committed value.
    fireEvent.click(screen.getByRole('button', { name: 'Favorites' }))
    await waitFor(() => expect(locationSearch()).toBe('/search?q=revenue&favorites=1'))

    // The exact race: typing a draft and Backing in the same task. The
    // destination's `q` is unchanged, so only a location-keyed import can see
    // that the URL — not the draft — is now the truth.
    typeQuery('raced')
    fireEvent.click(screen.getByRole('button', { name: 'probe back' }))
    await waitFor(() => expect(locationSearch()).toBe('/search?q=revenue'))
    expect(searchInput()).toHaveValue('revenue')
    expect(screen.getByRole('button', { name: 'Favorites' })).toHaveAttribute('aria-pressed', 'false')

    // Well past the debounce: the pending timer was cancelled with the import,
    // so Back is not cancelled by a late `?q=raced` write.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300))
    })
    expect(locationSearch()).toBe('/search?q=revenue')
    expect(searchInput()).toHaveValue('revenue')
  })

  test('typing rewrites the history entry while chips push new ones', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderSearch()
    typeQuery('rev')
    await waitFor(() => expect(locationSearch()).toBe('/search?q=rev'))

    // Replace-style typing: the pristine entry was rewritten, so going Back
    // has no earlier search state to reveal.
    fireEvent.click(screen.getByRole('button', { name: 'probe back' }))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50))
    })
    expect(locationSearch()).toBe('/search?q=rev')
    expect(searchInput()).toHaveValue('rev')

    // Chips push: Back returns to the unfiltered URL.
    fireEvent.click(screen.getByRole('button', { name: 'Notebooks' }))
    await waitFor(() => expect(locationSearch()).toContain('type=notebook'))
    fireEvent.click(screen.getByRole('button', { name: 'probe back' }))
    await waitFor(() => expect(locationSearch()).toBe('/search?q=rev'))
    expect(screen.getByRole('button', { name: 'Notebooks' })).toHaveAttribute('aria-pressed', 'false')
  })

  test('the active type chip renders even when the type has no results', async () => {
    useSearchGroups([])
    renderSearch('/search?q=zzz&type=notebook')
    await screen.findByText('No matches for “zzz”')

    expect(screen.getByRole('button', { name: 'Notebooks' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'All' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Favorites' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Dashboards' })).toBeNull()
  })

  test('a filter with no query is a no-results state, not the orientation panel', async () => {
    renderSearch('/search?type=notebook')

    expect(await screen.findByText('No results')).toBeInTheDocument()
    expect(screen.getByText('Type a query to search, or clear the filters above.')).toBeInTheDocument()
    expect(screen.queryByText('Search the workspace')).toBeNull()
    // Nothing ran, so the live region is mounted but silent.
    expect(screen.getByRole('status').textContent).toBe('')
    expect(screen.getByRole('button', { name: 'Notebooks' })).toHaveAttribute('aria-pressed', 'true')
    expect(searchRequests).toHaveLength(0)
  })

  test('a same-URL pop restores the destination and discards the draft', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    renderSearch('/search?q=revenue')
    await screen.findByRole('link', { name: /Revenue by region/ })
    const originalKey = locationKey()

    // A duplicate entry for the identical URL: the search string never
    // changes, so only the location entry distinguishes the two.
    fireEvent.click(screen.getByRole('button', { name: 'probe same' }))
    await waitFor(() => expect(locationKey()).not.toBe(originalKey))
    expect(locationSearch()).toBe('/search?q=revenue')

    // Same race as the chip-entry test, but with an identical search string:
    // a search-string-gated import cannot see this navigation at all.
    typeQuery('raced2')
    fireEvent.click(screen.getByRole('button', { name: 'probe back' }))
    await waitFor(() => expect(locationKey()).toBe(originalKey))
    expect(searchInput()).toHaveValue('revenue')

    // Well past the debounce: the pending timer was cancelled with the import,
    // so Back is not cancelled by a late `?q=raced2` write.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300))
    })
    expect(locationSearch()).toBe('/search?q=revenue')
    expect(searchInput()).toHaveValue('revenue')
  })

  test('clicking the active chip never pushes a duplicate history entry', async () => {
    useSearchGroups([
      group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')]),
      group('dashboard', [summary('dashboard', 'd-1', 'Revenue dashboard')]),
    ])
    renderSearch('/search?q=revenue')
    await screen.findByRole('link', { name: /Revenue by region/ })

    // Already active: "All" is a no-op, not a same-URL push.
    const pristineKey = locationKey()
    fireEvent.click(screen.getByRole('button', { name: 'All' }))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50))
    })
    expect(locationKey()).toBe(pristineKey)

    // A real filter changes the entry; re-clicking the active chip is a no-op.
    fireEvent.click(screen.getByRole('button', { name: 'Dashboards' }))
    await waitFor(() => expect(locationKey()).not.toBe(pristineKey))
    const filteredKey = locationKey()
    fireEvent.click(screen.getByRole('button', { name: 'Dashboards' }))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50))
    })
    expect(locationKey()).toBe(filteredKey)
    expect(locationSearch()).toBe('/search?q=revenue&type=dashboard')
  })

  test('unmounting before the debounce settles leaves no stray navigation', async () => {
    useSearchGroups([group('notebook', [summary('notebook', 'nb-1', 'Revenue by region')])])
    const { rerender } = renderWithProviders(
      <>
        <SearchPage />
        <LocationProbe />
      </>,
      { initialPath: '/search' },
    )
    typeQuery('rev')
    rerender(<LocationProbe />)

    // The timer's cleanup runs on unmount: nothing may navigate after it.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300))
    })
    expect(locationSearch()).toBe('/search')
    expect(searchRequests).toHaveLength(0)
  })

  test('an over-long query renders the specific error and the input caps its length', async () => {
    server.use(
      http.get('/api/v1/search', () => HttpResponse.json({ error: 'query too long' }, { status: 400 })),
    )
    renderSearch(`/search?q=${'x'.repeat(210)}`)

    expect(await screen.findByRole('alert')).toHaveTextContent('Query is too long — 200 characters max.')
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    expect(searchInput()).toHaveAttribute('maxlength', '200')
  })

  test('un-favoriting under the Favorites filter hides the row and count immediately', async () => {
    server.use(...favoritesFixture([{ type: 'notebook', id: 'nb-1' }]).handlers)
    useSearchGroups([
      group('notebook', [summary('notebook', 'nb-1', 'Revenue by region', { is_favorite: true })]),
    ])
    renderSearch('/search?q=rev&favorites=1')

    expect(await screen.findByRole('link', { name: /Revenue by region/ })).toBeInTheDocument()
    await screen.findByRole('button', { name: 'Remove from favorites' })
    expect(screen.getByRole('status')).toHaveTextContent('1 result')

    // The optimistic id-set flip drops the row and the count, before the
    // search response itself has refreshed.
    await userEvent.click(screen.getByRole('button', { name: 'Remove from favorites' }))
    await waitFor(() => expect(screen.queryByRole('link', { name: /Revenue by region/ })).toBeNull())
    expect(screen.getByRole('status')).toHaveTextContent('No results')
  })
})
