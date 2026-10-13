import { describe, test, expect, beforeEach, vi } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { AppHealthPanel } from './AppHealthPanel'
import type { ReviewReport } from './AppHealthPanel'

const REPORT: ReviewReport = {
  dashboard_id: 'd1',
  count: 3,
  view_url: '/dashboards/d1/view',
  preview_url: '/dashboards/d1/view?preview=draft',
  findings: [
    {
      severity: 'warning', category: 'layout', chapter_id: 'orders',
      message: 'chapter "orders" has no widgets', fix_tool: 'set_dashboard_layout',
    },
    {
      severity: 'info', category: 'content', chapter_id: 'overview', widget_id: 'w1',
      message: 'metric w1 has no label — viewers see the column name', fix_tool: 'set_widget_config',
    },
    {
      severity: 'error', category: 'layout', chapter_id: 'overview', widget_id: 'w1',
      message: 'widgets w1 and w2 overlap in chapter "overview"', fix_tool: 'set_dashboard_layout',
    },
  ],
}

beforeEach(() => {
  // jsdom has no scrollIntoView.
  Element.prototype.scrollIntoView = vi.fn() as unknown as typeof Element.prototype.scrollIntoView
})

describe('AppHealthPanel', () => {
  test('groups findings by severity and names the fix per row', async () => {
    server.use(http.get('/api/v1/dashboards/d1/review', () => HttpResponse.json(REPORT)))
    renderWithProviders(<AppHealthPanel dashboardId="d1" onClose={() => {}} />)

    expect(await screen.findByText('Errors (1)')).toBeInTheDocument()
    expect(screen.getByText('Warnings (1)')).toBeInTheDocument()
    expect(screen.getByText('Notes (1)')).toBeInTheDocument()
    expect(screen.getByText(/has no widgets/)).toBeInTheDocument()
    expect(screen.getByText('Configure the widget')).toBeInTheDocument()
    expect(screen.getAllByText('Arrange the layout')).toHaveLength(2)
    // Chapter scope is named on the row.
    expect(screen.getByText('orders')).toBeInTheDocument()
  })

  test('clicking a finding scrolls to, focuses, and highlights its widget', async () => {
    server.use(http.get('/api/v1/dashboards/d1/review', () => HttpResponse.json(REPORT)))
    renderWithProviders(
      <>
        <div id="dash-widget-w1" tabIndex={-1} data-testid="widget-w1" />
        <AppHealthPanel dashboardId="d1" onClose={() => {}} />
      </>,
    )

    const row = await screen.findByRole('button', { name: /no label/ })
    fireEvent.click(row)

    const target = screen.getByTestId('widget-w1')
    expect(Element.prototype.scrollIntoView).toHaveBeenCalled()
    expect(document.activeElement).toBe(target)
    expect(target).toHaveClass('dash-widget-highlight')
  })

  test('a widget in an inactive chapter asks the editor to show it, then jumps', async () => {
    server.use(http.get('/api/v1/dashboards/d1/review', () => HttpResponse.json(REPORT)))
    const onJumpToChapter = vi.fn((chapterId: string) => {
      // Simulate the editor mounting that chapter's widget.
      const el = document.createElement('div')
      el.id = 'dash-widget-w1'
      el.tabIndex = -1
      document.body.appendChild(el)
      expect(chapterId).toBe('overview')
    })
    renderWithProviders(
      <AppHealthPanel dashboardId="d1" onJumpToChapter={onJumpToChapter} onClose={() => {}} />,
    )

    const row = await screen.findByRole('button', { name: /no label/ })
    fireEvent.click(row)

    expect(onJumpToChapter).toHaveBeenCalledWith('overview')
    await waitFor(() =>
      expect(document.getElementById('dash-widget-w1')?.classList.contains('dash-widget-highlight')).toBe(true))
  })

  test('chapter-only findings are not clickable', async () => {
    server.use(http.get('/api/v1/dashboards/d1/review', () => HttpResponse.json(REPORT)))
    renderWithProviders(<AppHealthPanel dashboardId="d1" onClose={() => {}} />)

    await screen.findByText('Errors (1)')
    expect(screen.queryByRole('button', { name: /has no widgets/ })).not.toBeInTheDocument()
  })

  test('the empty state reads "No issues found"', async () => {
    server.use(http.get('/api/v1/dashboards/d1/review', () =>
      HttpResponse.json({ ...REPORT, count: 0, findings: [] })))
    renderWithProviders(<AppHealthPanel dashboardId="d1" onClose={() => {}} />)

    expect(await screen.findByText('No issues found')).toBeInTheDocument()
  })

  test('refetches when the panel opens again', async () => {
    let requests = 0
    server.use(http.get('/api/v1/dashboards/d1/review', () => {
      requests += 1
      return HttpResponse.json({ ...REPORT, count: 0, findings: [] })
    }))
    const first = renderWithProviders(<AppHealthPanel dashboardId="d1" onClose={() => {}} />)
    await screen.findByText('No issues found')
    expect(requests).toBe(1)
    first.unmount()

    renderWithProviders(<AppHealthPanel dashboardId="d1" onClose={() => {}} />)
    await waitFor(() => expect(requests).toBe(2))
  })

  test('surfaces a request error', async () => {
    server.use(http.get('/api/v1/dashboards/d1/review', () =>
      HttpResponse.json({ error: 'boom' }, { status: 500 })))
    renderWithProviders(<AppHealthPanel dashboardId="d1" onClose={() => {}} />)

    expect(await screen.findByRole('alert')).toHaveTextContent(/boom|failed/i)
  })
})
