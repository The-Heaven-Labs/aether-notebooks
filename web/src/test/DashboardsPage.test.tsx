import { describe, test, expect, beforeEach } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { Routes, Route, useLocation } from 'react-router-dom'
import { server } from './server'
import { renderWithProviders } from './utils'
import { favoritesFixture, favoriteSummary } from './handlers'
import { DashboardsPage } from '../pages/DashboardsPage'
import type { FavoriteResourceType } from '../types'

const dashboards = [
  {
    id: 'd-1', org_id: 'org-1', title: 'Revenue Board', settings: {},
    created_by: 'user-1', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-02T00:00:00Z',
  },
  {
    id: 'd-2', org_id: 'org-1', title: 'Ops Board', settings: {},
    created_by: 'user-1', created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-02T00:00:00Z',
  },
]

beforeEach(() => {
  // The page persists the layout choice; pin it per test.
  localStorage.setItem('aether_dashboards_layout', 'list')
  server.use(http.get('/api/v1/dashboards', () => HttpResponse.json(dashboards)))
})

function renderPage(initialPath = '/dashboards') {
  return renderWithProviders(
    <>
      <Routes>
        <Route path="/dashboards" element={<DashboardsPage />} />
        <Route path="/dashboards/:id/view" element={<div>Dashboard opened</div>} />
      </Routes>
      <LocationProbe />
    </>,
    { initialPath },
  )
}

/** Renders the URL so the chip's `?fav=1` sync can be asserted. */
function LocationProbe() {
  const { pathname, search } = useLocation()
  return <div data-testid="location">{pathname}{search}</div>
}

function locationText(): string {
  return screen.getByTestId('location').textContent ?? ''
}

describe('DashboardsPage favorites (F2)', () => {
  test('list rows render the seeded bookmark from the shared listing', async () => {
    const { handlers, counts } = favoritesFixture([{ type: 'dashboard', id: 'd-1' }])
    server.use(...handlers)
    renderPage()

    const row = (await screen.findByText('Revenue Board')).closest('.favorite-host') as HTMLElement
    expect(row).not.toBeNull()
    expect(await within(row).findByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true')
    // One listing shared by both rows.
    await waitFor(() => expect(counts.gets).toBe(1))
  })

  test('toggling a list row flips optimistically without navigating', async () => {
    const { handlers } = favoritesFixture()
    server.use(...handlers)
    renderPage()

    const row = (await screen.findByText('Ops Board')).closest('.favorite-host') as HTMLElement
    await userEvent.click(within(row).getByRole('button', { name: 'Add to favorites' }))
    await waitFor(() =>
      expect(within(row).getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true'),
    )
    expect(screen.queryByText('Dashboard opened')).not.toBeInTheDocument()
  })

  test('grid cards keep the bookmark always visible and toggle without navigating', async () => {
    const { handlers } = favoritesFixture()
    server.use(...handlers)
    renderPage()

    await screen.findByText('Revenue Board')
    await userEvent.click(screen.getByRole('button', { name: 'Switch to grid view' }))

    const card = screen.getByText('Revenue Board').closest('.card-hover') as HTMLElement
    const button = within(card).getByRole('button', { name: 'Add to favorites' })
    // No hover reveal on cards: no `favorite-reveal` class to hide behind.
    expect(button).not.toHaveClass('favorite-reveal')

    await userEvent.click(button)
    await waitFor(() =>
      expect(within(card).getByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true'),
    )
    expect(screen.queryByText('Dashboard opened')).not.toBeInTheDocument()
  })
})

describe('DashboardsPage favorites filter (F3)', () => {
  test('the chip filters rows through the favorites id set and syncs ?fav=1', async () => {
    const { handlers } = favoritesFixture([{ type: 'dashboard', id: 'd-1' }])
    server.use(...handlers)
    renderPage()

    expect(await screen.findByText('Revenue Board')).toBeInTheDocument()
    expect(screen.getByText('Ops Board')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Favorites' }))

    expect(screen.queryByText('Ops Board')).not.toBeInTheDocument()
    expect(screen.getByText('Revenue Board')).toBeInTheDocument()
    expect(locationText()).toBe('/dashboards?fav=1')

    await userEvent.click(screen.getByRole('button', { name: 'Favorites' }))
    expect(await screen.findByText('Ops Board')).toBeInTheDocument()
    expect(locationText()).toBe('/dashboards')
  })

  test('a ?fav=1 deep link filters on first paint', async () => {
    const { handlers } = favoritesFixture([{ type: 'dashboard', id: 'd-2' }])
    server.use(...handlers)
    renderPage('/dashboards?fav=1')

    expect(await screen.findByText('Ops Board')).toBeInTheDocument()
    expect(screen.queryByText('Revenue Board')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Favorites' })).toHaveAttribute('aria-pressed', 'true')
  })

  test('grid cards filter too', async () => {
    localStorage.setItem('aether_dashboards_layout', 'grid')
    const { handlers } = favoritesFixture([{ type: 'dashboard', id: 'd-1' }])
    server.use(...handlers)
    renderPage('/dashboards?fav=1')

    expect(await screen.findByText('Revenue Board')).toBeInTheDocument()
    expect(screen.queryByText('Ops Board')).not.toBeInTheDocument()
    // The surviving row is still a grid card.
    expect(screen.getByText('Revenue Board').closest('.card-hover')).not.toBeNull()
  })

  test('empty state when nothing is favorited', async () => {
    const { handlers } = favoritesFixture()
    server.use(...handlers)
    renderPage('/dashboards?fav=1')

    expect(await screen.findByText('No favorite dashboards')).toBeInTheDocument()
    expect(screen.getByText('Bookmark a dashboard with the bookmark icon to see it here.')).toBeInTheDocument()
  })

  test('a failed favorites listing shows the error panel with a retry that reports its fetch', async () => {
    let gets = 0
    let release: () => void = () => {}
    const parked = new Promise<void>((resolve) => { release = resolve })
    server.use(
      http.get('/api/v1/favorites', async () => {
        gets += 1
        if (gets === 1) return HttpResponse.json({ error: 'boom' }, { status: 500 })
        await parked
        return HttpResponse.json({ groups: [] })
      }),
    )
    renderPage('/dashboards?fav=1')

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent("Couldn't load favorites.")
    const retry = within(alert).getByRole('button', { name: 'Retry' })

    await userEvent.click(retry)
    // Pending feedback: the button disables and relabels while retrying.
    await waitFor(() => expect(retry).toBeDisabled())
    expect(retry).toHaveTextContent('Retrying…')

    release()
    // Recovery: the retried listing lands and the filtered empty state shows.
    expect(await screen.findByText('No favorite dashboards')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  test('a failed refetch keeps the cached filtered rows', async () => {
    let gets = 0
    server.use(
      http.get('/api/v1/favorites', () => {
        gets += 1
        if (gets === 1) {
          return HttpResponse.json({
            groups: [{
              resource_type: 'dashboard',
              results: [favoriteSummary('dashboard', 'd-1'), favoriteSummary('dashboard', 'd-2')],
            }],
          })
        }
        return HttpResponse.json({ error: 'boom' }, { status: 500 })
      }),
      http.delete('/api/v1/favorites/:type/:id', () => new HttpResponse(null, { status: 204 })),
    )
    renderPage('/dashboards?fav=1')

    expect(await screen.findByText('Revenue Board')).toBeInTheDocument()
    expect(screen.getByText('Ops Board')).toBeInTheDocument()

    // Un-favorite one: the success invalidation refetches, the refetch fails.
    const row = screen.getByText('Revenue Board').closest('.favorite-host') as HTMLElement
    await userEvent.click(within(row).getByRole('button', { name: 'Remove from favorites' }))
    await waitFor(() => expect(gets).toBe(2))

    // Cached id sets still filter; no error panel replaces the rows.
    expect(screen.queryByText('Revenue Board')).not.toBeInTheDocument()
    expect(screen.getByText('Ops Board')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  test('un-favoriting removes the row while the request is still pending', async () => {
    const { handlers, held } = favoritesFixture([{ type: 'dashboard', id: 'd-1' }])
    let release: () => void = () => {}
    const parked = new Promise<void>((resolve) => { release = resolve })
    server.use(
      http.delete('/api/v1/favorites/:type/:id', async ({ params }) => {
        await parked
        held.get(params.type as FavoriteResourceType)?.delete(params.id as string)
        return new HttpResponse(null, { status: 204 })
      }),
      ...handlers,
    )
    renderPage('/dashboards?fav=1')

    const row = (await screen.findByText('Revenue Board')).closest('.favorite-host') as HTMLElement
    await userEvent.click(within(row).getByRole('button', { name: 'Remove from favorites' }))

    expect(screen.queryByText('Revenue Board')).not.toBeInTheDocument()
    expect(await screen.findByText('No favorite dashboards')).toBeInTheDocument()

    release()
    await waitFor(() => expect(held.get('dashboard')?.has('d-1')).toBe(false))
  })
})
