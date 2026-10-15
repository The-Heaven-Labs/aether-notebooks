import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
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
  const { pathname, search } = useLocation()
  return <div data-testid="location">{pathname}{search}</div>
}

/** Pushes/restores history entries the way the browser back button would. */
function HistoryProbe() {
  const navigate = useNavigate()
  return (
    <>
      <button type="button" onClick={() => navigate(-1)}>probe back</button>
      <button type="button" onClick={() => navigate(1)}>probe forward</button>
      <button type="button" onClick={() => navigate('/search?q=other')}>probe other</button>
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
})
