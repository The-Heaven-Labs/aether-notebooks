import { describe, test, expect, beforeEach } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { Routes, Route } from 'react-router-dom'
import { server } from './server'
import { renderWithProviders } from './utils'
import { favoritesFixture } from './handlers'
import { DashboardsPage } from '../pages/DashboardsPage'

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

function renderPage() {
  return renderWithProviders(
    <Routes>
      <Route path="/dashboards" element={<DashboardsPage />} />
      <Route path="/dashboards/:id/view" element={<div>Dashboard opened</div>} />
    </Routes>,
    { initialPath: '/dashboards' },
  )
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
