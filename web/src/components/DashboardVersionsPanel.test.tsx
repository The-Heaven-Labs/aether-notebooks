import { describe, test, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { DashboardVersionsPanel } from './DashboardVersionsPanel'
import type { DashboardVersionSummary, DashboardVersionsResponse } from '../types'

// Relative timestamps are computed at module load and the assertions run
// milliseconds later, so the rounding buckets ("2m ago") stay stable.
function version(n: number, overrides: Partial<DashboardVersionSummary> = {}): DashboardVersionSummary {
  return {
    version: n,
    created_by_email: `publisher${n}@heaven-labs.com`,
    created_at: new Date(Date.now() - n * 60_000).toISOString(),
    content_hash: `hash-${n}`,
    ...overrides,
  }
}

const V2 = version(2)
const V1 = version(1, { created_at: new Date(Date.now() - 3 * 60 * 60_000).toISOString() })

const PUBLISHED: DashboardVersionsResponse = {
  versions: [V2, V1],
  latest: V2,
  has_unpublished_changes: true,
  draft_hash: 'draft-hash',
}

const EMPTY: DashboardVersionsResponse = {
  versions: [],
  latest: null,
  has_unpublished_changes: true,
  draft_hash: 'draft-hash',
}

beforeEach(() => {
  server.use(http.get('/api/v1/dashboards/d1/versions', () => HttpResponse.json(EMPTY)))
})

/** The panel owns its query and invalidation, so the test client is local. */
function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const invalidate = vi.spyOn(qc, 'invalidateQueries')
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={qc}>
      <DashboardVersionsPanel dashboardId="d1" onClose={onClose} />
    </QueryClientProvider>,
  )
  return { qc, invalidate, onClose }
}

function withVersions(response: DashboardVersionsResponse) {
  server.use(http.get('/api/v1/dashboards/d1/versions', () => HttpResponse.json(response)))
}

describe('DashboardVersionsPanel', () => {
  test('lists versions newest first with publisher, relative time, and the Published marker', async () => {
    withVersions(PUBLISHED)
    renderPanel()

    expect(await screen.findByText('v2')).toBeInTheDocument()
    expect(screen.getByText('v1')).toBeInTheDocument()
    expect(screen.getByText('publisher2@heaven-labs.com')).toBeInTheDocument()
    expect(screen.getByText('publisher1@heaven-labs.com')).toBeInTheDocument()
    expect(screen.getByText('2m ago')).toBeInTheDocument()
    expect(screen.getByText('3h ago')).toBeInTheDocument()

    const rows = screen.getAllByTestId('version-row')
    expect(rows).toHaveLength(2)
    expect(within(rows[0]).getByText('Published')).toBeInTheDocument()
    expect(within(rows[1]).queryByText('Published')).not.toBeInTheDocument()
  })

  test('explains that restoring copies into the draft and publishing stays explicit', async () => {
    withVersions(PUBLISHED)
    renderPanel()

    expect(
      await screen.findByText('Restoring copies this version into the draft — publish to make it live.'),
    ).toBeInTheDocument()
  })

  test('shows an empty state when nothing has been published', async () => {
    renderPanel()

    expect(await screen.findByText(/No versions published yet/)).toBeInTheDocument()
  })

  test('restore asks for confirmation and cancel posts nothing', async () => {
    const restores: string[] = []
    server.use(
      http.get('/api/v1/dashboards/d1/versions', () => HttpResponse.json(PUBLISHED)),
      http.post('/api/v1/dashboards/d1/versions/:version/restore', ({ params }) => {
        restores.push(String(params.version))
        return HttpResponse.json({ restored: Number(params.version) })
      }),
    )
    renderPanel()

    fireEvent.click(await screen.findByRole('button', { name: 'Restore v1' }))
    expect(await screen.findByRole('dialog', { name: 'Restore version v1?' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Restore version v1?' })).not.toBeInTheDocument(),
    )
    expect(restores).toEqual([])
  })

  test('confirming restore POSTs the version and invalidates both queries', async () => {
    const restores: string[] = []
    server.use(
      http.get('/api/v1/dashboards/d1/versions', () => HttpResponse.json(PUBLISHED)),
      http.post('/api/v1/dashboards/d1/versions/:version/restore', ({ params }) => {
        restores.push(String(params.version))
        return HttpResponse.json({ restored: Number(params.version) })
      }),
    )
    const { invalidate } = renderPanel()

    fireEvent.click(await screen.findByRole('button', { name: 'Restore v1' }))
    fireEvent.click(screen.getByRole('button', { name: 'Restore' }))

    await waitFor(() => expect(restores).toEqual(['1']))
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['dashboard-versions', 'd1'] })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['dashboard', 'd1'] })
    expect(await screen.findByText('Restored v1 into the draft.')).toBeInTheDocument()
  })

  test('a failed restore surfaces the error and does not invalidate', async () => {
    server.use(
      http.get('/api/v1/dashboards/d1/versions', () => HttpResponse.json(PUBLISHED)),
      http.post('/api/v1/dashboards/d1/versions/:version/restore', () =>
        HttpResponse.json({ error: 'restore failed' }, { status: 500 }),
      ),
    )
    const { invalidate } = renderPanel()

    fireEvent.click(await screen.findByRole('button', { name: 'Restore v1' }))
    fireEvent.click(screen.getByRole('button', { name: 'Restore' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('restore failed')
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ['dashboard-versions', 'd1'] })
  })

  test('Escape closes the confirmation without closing the panel', async () => {
    withVersions(PUBLISHED)
    const { onClose } = renderPanel()

    fireEvent.click(await screen.findByRole('button', { name: 'Restore v1' }))
    expect(await screen.findByRole('dialog', { name: 'Restore version v1?' })).toBeInTheDocument()

    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Restore version v1?' })).not.toBeInTheDocument(),
    )
    expect(onClose).not.toHaveBeenCalled()
  })

  test('closes on Escape and on backdrop click', async () => {
    withVersions(PUBLISHED)
    const { onClose } = renderPanel()
    await screen.findByText('v2')

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByTestId('dashboard-versions-backdrop'))
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  test('surfaces the draft warnings that block publishing', async () => {
    withVersions({ ...PUBLISHED, warnings: ['widget w1: connector not found'] })
    renderPanel()

    expect(await screen.findByText('widget w1: connector not found')).toBeInTheDocument()
    expect(screen.getByText(/cannot be published/)).toBeInTheDocument()
  })

  test('an in-flight restore shows progress and disables every Restore button', async () => {
    let release: (() => void) | undefined
    server.use(
      http.get('/api/v1/dashboards/d1/versions', () => HttpResponse.json(PUBLISHED)),
      http.post('/api/v1/dashboards/d1/versions/:version/restore', async () => {
        await new Promise<void>((resolve) => { release = resolve })
        return HttpResponse.json({ restored: 1 })
      }),
    )
    renderPanel()

    fireEvent.click(await screen.findByRole('button', { name: 'Restore v1' }))
    fireEvent.click(screen.getByRole('button', { name: 'Restore' }))

    expect(await screen.findByText('Restoring v1…')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Restore v1' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Restore v2' })).toBeDisabled()

    release?.()
    expect(await screen.findByText('Restored v1 into the draft.')).toBeInTheDocument()
  })

  test('closing the confirmation returns focus to the triggering Restore button', async () => {
    withVersions(PUBLISHED)
    renderPanel()

    fireEvent.click(await screen.findByRole('button', { name: 'Restore v1' }))
    expect(await screen.findByRole('dialog', { name: 'Restore version v1?' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Restore version v1?' })).not.toBeInTheDocument(),
    )
    expect(screen.getByRole('button', { name: 'Restore v1' })).toHaveFocus()
  })

  test('a load failure shows only the error, not the empty state', async () => {
    server.use(
      http.get('/api/v1/dashboards/d1/versions', () =>
        HttpResponse.json({ error: 'failed to load versions' }, { status: 500 }),
      ),
    )
    renderPanel()

    expect(await screen.findByRole('alert')).toHaveTextContent('failed to load versions')
    expect(screen.queryByText(/No versions published yet/)).not.toBeInTheDocument()
  })
})
