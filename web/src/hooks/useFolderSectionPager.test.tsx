import { describe, test, expect } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import type { ReactNode } from 'react'
import { server } from '../test/server'
import { api } from '../api/client'
import { useFolderSectionPager } from './useFolderSectionPager'
import type { FolderContents, FolderSection } from '../types'

type FolderNotebook = FolderContents['notebooks'][number]

function nb(id: string, title: string, folderID: string): FolderNotebook {
  return {
    id, org_id: 'org-1', title, description: '',
    created_by: 'user-1', folder_id: folderID,
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  }
}

function folderPage(folderID: string, notebooks: FolderNotebook[], total: number, hasMore: boolean): FolderContents {
  return {
    folder: {
      id: folderID, org_id: 'org-1', name: `Folder ${folderID}`, is_home: false,
      created_by: 'user-1', created_at: '', updated_at: '',
    },
    folders: [],
    notebooks,
    connectors: [],
    dashboards: [],
    totals: { notebooks: total, connectors: 0, dashboards: 0 },
    has_more: { notebooks: hasMore, connectors: false, dashboards: false },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

/** A real folder-contents query (genuine `dataUpdatedAt`) plus the pager under test. */
function useHarness(folderID: string) {
  const contentsKey = ['folder-contents', folderID]
  const query = useQuery<FolderContents>({
    queryKey: contentsKey,
    queryFn: () => api.get<FolderContents>(`/api/v1/folders/${folderID}`),
    // Cached first pages must not refetch on their own, so a test can pin
    // `dataUpdatedAt` deliberately.
    staleTime: Infinity,
  })
  const pager = useFolderSectionPager<FolderNotebook>({
    kind: 'notebook',
    folderID,
    firstPage: query.data?.notebooks ?? [],
    firstPageTotal: query.data?.totals?.notebooks ?? null,
    firstPageHasMore: query.data?.has_more?.notebooks === true,
    generation: query.dataUpdatedAt,
    contentsKey,
    onError: () => {},
  })
  return { query, pager }
}

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  }
  return { qc, Wrapper }
}

describe('useFolderSectionPager', () => {
  test('drops a page whose first-page generation moved on while it was in flight', async () => {
    let page = folderPage('f-a', [nb('nb-1', 'Alpha', 'f-a'), nb('nb-2', 'Bravo', 'f-a')], 3, true)
    const contents = deferred<FolderSection<FolderNotebook>>()
    server.use(
      http.get('/api/v1/folders/f-a', () => HttpResponse.json(page)),
      http.get('/api/v1/folders/f-a/contents', async () => HttpResponse.json(await contents.promise)),
    )
    const { qc, Wrapper } = createWrapper()
    const { result } = renderHook(() => useHarness('f-a'), { wrapper: Wrapper })

    await waitFor(() => expect(result.current.pager.items).toHaveLength(2))
    act(() => { result.current.pager.loadMore() })
    await waitFor(() => expect(result.current.pager.loading).toBe(true))

    // A mutation invalidates the listing; the refetched first page lands while
    // the load-more page is still in flight.
    page = folderPage('f-a', [nb('nb-9', 'Fresh One', 'f-a'), nb('nb-10', 'Fresh Two', 'f-a')], 2, false)
    await act(async () => { await qc.invalidateQueries({ queryKey: ['folder-contents', 'f-a'] }) })
    await waitFor(() => expect(result.current.query.data?.notebooks[0]?.title).toBe('Fresh One'))

    // The old page resolves now: it belongs to the previous generation.
    await act(async () => {
      contents.resolve({
        items: [nb('nb-3', 'Late One', 'f-a'), nb('nb-4', 'Late Two', 'f-a')],
        total: 3, has_more: false, offset: 2,
      })
      await contents.promise
    })

    expect(result.current.pager.items.map((n) => n.title)).toEqual(['Fresh One', 'Fresh Two'])
    expect(result.current.pager.total).toBe(2)
  })

  test('drops a page requested for another folder even when the generations collide', async () => {
    const contents = deferred<FolderSection<FolderNotebook>>()
    server.use(
      http.get('/api/v1/folders/f-a', () => HttpResponse.json(
        folderPage('f-a', [nb('a-1', 'A One', 'f-a'), nb('a-2', 'A Two', 'f-a')], 3, true)
      )),
      http.get('/api/v1/folders/f-a/contents', async () => HttpResponse.json(await contents.promise)),
      http.get('/api/v1/folders/f-b', () => HttpResponse.json(
        folderPage('f-b', [nb('b-1', 'B One', 'f-b')], 1, false)
      )),
    )
    const { qc, Wrapper } = createWrapper()
    const { result, rerender } = renderHook(
      ({ folderID }: { folderID: string }) => useHarness(folderID),
      { wrapper: Wrapper, initialProps: { folderID: 'f-a' } },
    )

    await waitFor(() => expect(result.current.pager.items).toHaveLength(2))
    const generation = result.current.query.dataUpdatedAt

    act(() => { result.current.pager.loadMore() })
    await waitFor(() => expect(result.current.pager.loading).toBe(true))

    // Switch folders, forcing B's first page to share A's click-time generation:
    // this is the same-millisecond collision the folder guard exists for, so a
    // generation-only check would let A's page land in B's section.
    qc.setQueryData(
      ['folder-contents', 'f-b'],
      folderPage('f-b', [nb('b-1', 'B One', 'f-b')], 1, false),
      { updatedAt: generation },
    )
    rerender({ folderID: 'f-b' })
    await waitFor(() => expect(result.current.pager.items.map((n) => n.title)).toEqual(['B One']))

    await act(async () => {
      contents.resolve({
        items: [nb('a-3', 'Late A', 'f-a')],
        total: 3, has_more: false, offset: 2,
      })
      await contents.promise
    })

    expect(result.current.pager.items.map((n) => n.title)).toEqual(['B One'])
  })
})
