import { describe, test, expect, vi } from 'vitest'
import { renderHook, waitFor, act } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse, delay } from 'msw'
import type { ReactNode } from 'react'
import { server } from '../test/server'
import { favoritesFixture, favoriteSummary } from '../test/handlers'
import { FAVORITE_RESOURCE_TYPES } from '../types'
import {
  favoritesQueryKey,
  useFavorites,
  useIsFavorite,
  useToggleFavorite,
  type FavoritesData,
} from './useFavorites'

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  }
  return { qc, Wrapper }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

describe('useFavorites', () => {
  test('parses the grouped listing into per-type id sets', async () => {
    server.use(
      http.get('/api/v1/favorites', () =>
        HttpResponse.json({
          groups: [
            { resource_type: 'notebook', results: [favoriteSummary('notebook', 'nb-1')] },
            {
              resource_type: 'folder',
              results: [favoriteSummary('folder', 'f-1'), favoriteSummary('folder', 'f-2')],
            },
          ],
        }),
      ),
    )
    const { Wrapper } = createWrapper()
    const { result } = renderHook(() => useFavorites(), { wrapper: Wrapper })

    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.ids.notebook.has('nb-1')).toBe(true)
    expect([...result.current.ids.folder].sort()).toEqual(['f-1', 'f-2'])
    // Every favoriteable type carries a set, so consumers never branch on undefined.
    for (const type of FAVORITE_RESOURCE_TYPES) {
      expect(result.current.ids[type]).toBeInstanceOf(Set)
    }
    expect(result.current.ids.tool.size).toBe(0)
    expect(result.current.groups.map((g) => g.resource_type)).toEqual(['notebook', 'folder'])
  })

  test('defaults every type to an empty set, before and after an empty listing', async () => {
    const { Wrapper } = createWrapper()
    const { result } = renderHook(() => useFavorites(), { wrapper: Wrapper })

    expect(result.current.ids.notebook.size).toBe(0)
    await waitFor(() => expect(result.current.isLoading).toBe(false))
    for (const type of FAVORITE_RESOURCE_TYPES) expect(result.current.ids[type].size).toBe(0)
    expect(result.current.groups).toEqual([])
    expect(result.current.isError).toBe(false)
  })
})

describe('useIsFavorite', () => {
  test('reports one resource per instance and shares a single listing fetch', async () => {
    const { handlers, counts } = favoritesFixture([{ type: 'notebook', id: 'nb-a' }])
    server.use(...handlers)
    const { Wrapper } = createWrapper()
    const { result } = renderHook(
      () => ({ a: useIsFavorite('notebook', 'nb-a'), b: useIsFavorite('notebook', 'nb-b') }),
      { wrapper: Wrapper },
    )

    await waitFor(() => expect(result.current.a.isLoading).toBe(false))
    expect(result.current.a.isFavorite).toBe(true)
    expect(result.current.b.isFavorite).toBe(false)
    expect(result.current.b.isLoading).toBe(false)
    // Two observers on the same key still issue exactly one listing request.
    expect(counts.gets).toBe(1)
  })

  test('is loading until the first listing settles', async () => {
    server.use(
      http.get('/api/v1/favorites', async () => {
        await delay(40)
        return HttpResponse.json({
          groups: [{ resource_type: 'notebook', results: [favoriteSummary('notebook', 'nb-a')] }],
        })
      }),
    )
    const { Wrapper } = createWrapper()
    const { result } = renderHook(() => useIsFavorite('notebook', 'nb-a'), { wrapper: Wrapper })

    expect(result.current.isLoading).toBe(true)
    expect(result.current.isFavorite).toBe(false)
    await waitFor(() => expect(result.current.isLoading).toBe(false))
    expect(result.current.isFavorite).toBe(true)
  })

  test('a listing error stops loading and reports the add-only default', async () => {
    server.use(http.get('/api/v1/favorites', () => HttpResponse.json({ error: 'boom' }, { status: 500 })))
    const { Wrapper } = createWrapper()
    const { result } = renderHook(() => useIsFavorite('notebook', 'nb-1'), { wrapper: Wrapper })

    await waitFor(() => expect(result.current.isLoading).toBe(false))
    // Not favorited is not a claim here — the toggle falls back to add-only.
    expect(result.current.isFavorite).toBe(false)
  })

  test('another resource flipping does not re-render this row', async () => {
    const { handlers } = favoritesFixture()
    server.use(...handlers)
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    }

    let rendersA = 0
    const { result: a } = renderHook(() => {
      rendersA += 1
      return useIsFavorite('notebook', 'nb-a')
    }, { wrapper: Wrapper })
    const { result: b } = renderHook(() => useIsFavorite('notebook', 'nb-b'), { wrapper: Wrapper })
    await waitFor(() => expect(a.current.isLoading).toBe(false))
    await waitFor(() => expect(b.current.isLoading).toBe(false))
    const settled = rendersA

    expect(b.current.isFavorite).toBe(false)
    expect(a.current.isFavorite).toBe(false)

    // Flip nb-b exactly the way the optimistic toggle writes the cache; the
    // select must keep nb-a's boolean identity stable for its observer.
    act(() => {
      qc.setQueryData<FavoritesData>(favoritesQueryKey, (data) =>
        data
          ? { groups: data.groups, ids: { ...data.ids, notebook: new Set([...data.ids.notebook, 'nb-b']) } }
          : data,
      )
    })

    await waitFor(() => expect(b.current.isFavorite).toBe(true))
    expect(a.current.isFavorite).toBe(false)
    // The whole point of the narrow selector: nb-a's row never re-rendered.
    expect(rendersA).toBe(settled)
  })
})

describe('useToggleFavorite', () => {
  test('refuses a toggle before the first listing has data, then proceeds once it lands', async () => {
    let putCalls = 0
    const held = new Set<string>()
    const listing = deferred<void>()
    server.use(
      http.get('/api/v1/favorites', async () => {
        await listing.promise
        return HttpResponse.json({
          groups: held.size
            ? [{ resource_type: 'notebook', results: [...held].map((id) => favoriteSummary('notebook', id)) }]
            : [],
        })
      }),
      http.put('/api/v1/favorites/notebook/nb-1', ({ params }) => {
        putCalls += 1
        held.add(params.id as string)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { Wrapper } = createWrapper()
    const { result } = renderHook(
      () => ({ favorite: useIsFavorite('notebook', 'nb-1'), toggle: useToggleFavorite('notebook', 'nb-1') }),
      { wrapper: Wrapper },
    )
    expect(result.current.favorite.isLoading).toBe(true)

    // Before the listing lands the flip would be guesswork: no request.
    act(() => { result.current.toggle.toggle() })
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(putCalls).toBe(0)

    // Once the listing arrives the same toggle goes through.
    await act(async () => {
      listing.resolve()
      await listing.promise
    })
    await waitFor(() => expect(result.current.favorite.isLoading).toBe(false))
    act(() => { result.current.toggle.toggle() })
    await waitFor(() => expect(putCalls).toBe(1))
  })

  test('a successful toggle invalidates both favorites and search', async () => {
    const { handlers } = favoritesFixture()
    server.use(...handlers)
    const { qc, Wrapper } = createWrapper()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(
      () => ({ favorites: useFavorites(), toggle: useToggleFavorite('notebook', 'nb-1') }),
      { wrapper: Wrapper },
    )
    await waitFor(() => expect(result.current.favorites.isLoading).toBe(false))

    act(() => { result.current.toggle.toggle() })
    await waitFor(() => expect(result.current.toggle.isPending).toBe(false))
    expect(invalidate).toHaveBeenCalledWith({ queryKey: favoritesQueryKey })

    // Search invalidation is narrowed to `favorites=1` searches (which render
    // and filter on `is_favorite`); plain searches never refetch. Key shape:
    // `['search', q, types, favorites, limit]` — index 3 is the flag.
    const searchInvalidations = invalidate.mock.calls
      .map(([filters]) => filters)
      .filter((filters) => filters?.queryKey?.[0] === 'search')
    expect(searchInvalidations).toHaveLength(1)
    const predicate = searchInvalidations[0]!.predicate!
    const matches = (key: unknown[]) =>
      predicate({ queryKey: key } as unknown as Parameters<typeof predicate>[0])
    expect(matches(['search', 'rev', undefined, true, 5])).toBe(true)
    expect(matches(['search', 'rev', undefined, false, 5])).toBe(false)
    expect(matches(['search', 'rev'])).toBe(false)
  })

  test('flips the id set optimistically before the request resolves', async () => {
    const put = deferred<void>()
    let putCalls = 0
    const { handlers, hold } = favoritesFixture()
    server.use(...handlers)
    // Registered after the fixture so it matches first (MSW prepends).
    server.use(
      http.put('/api/v1/favorites/notebook/nb-1', async () => {
        putCalls += 1
        await put.promise
        hold('notebook', 'nb-1')
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { Wrapper } = createWrapper()
    const { result } = renderHook(
      () => ({ favorites: useFavorites(), toggle: useToggleFavorite('notebook', 'nb-1') }),
      { wrapper: Wrapper },
    )
    await waitFor(() => expect(result.current.favorites.isLoading).toBe(false))

    act(() => { result.current.toggle.toggle() })
    await waitFor(() => expect(result.current.favorites.ids.notebook.has('nb-1')).toBe(true))
    expect(result.current.toggle.isPending).toBe(true)
    expect(putCalls).toBe(1)

    await act(async () => {
      put.resolve()
      await put.promise
    })
    await waitFor(() => expect(result.current.toggle.isPending).toBe(false))
    // The success invalidation refetched the listing, which now holds the id.
    await waitFor(() => expect(result.current.favorites.ids.notebook.has('nb-1')).toBe(true))
  })

  test('rolls back to the previous set when the request fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const put = deferred<void>()
      server.use(...favoritesFixture().handlers)
      server.use(
        http.put('/api/v1/favorites/notebook/nb-1', async () => {
          await put.promise
          return HttpResponse.json({ error: 'boom' }, { status: 500 })
        }),
      )
      const { Wrapper } = createWrapper()
      const { result } = renderHook(
        () => ({ favorites: useFavorites(), toggle: useToggleFavorite('notebook', 'nb-1') }),
        { wrapper: Wrapper },
      )
      await waitFor(() => expect(result.current.favorites.isLoading).toBe(false))

      act(() => { result.current.toggle.toggle() })
      await waitFor(() => expect(result.current.favorites.ids.notebook.has('nb-1')).toBe(true))

      await act(async () => {
        put.resolve()
        await put.promise
      })
      await waitFor(() => expect(result.current.favorites.ids.notebook.has('nb-1')).toBe(false))
      expect(result.current.toggle.isPending).toBe(false)
      // No toast system: failures revert silently with a console trace.
      expect(consoleError).toHaveBeenCalledWith('Failed to add favorite:', expect.anything())
    } finally {
      consoleError.mockRestore()
    }
  })

  test('removing flips the set and the success invalidation refetches the listing', async () => {
    const { handlers, counts } = favoritesFixture([{ type: 'notebook', id: 'nb-1' }])
    server.use(...handlers)
    const { Wrapper } = createWrapper()
    const { result } = renderHook(
      () => ({ favorites: useFavorites(), toggle: useToggleFavorite('notebook', 'nb-1') }),
      { wrapper: Wrapper },
    )
    await waitFor(() => expect(result.current.favorites.ids.notebook.has('nb-1')).toBe(true))
    expect(counts.gets).toBe(1)

    act(() => { result.current.toggle.toggle() })
    await waitFor(() => expect(result.current.favorites.ids.notebook.has('nb-1')).toBe(false))
    await waitFor(() => expect(counts.gets).toBe(2))
    await waitFor(() => expect(result.current.toggle.isPending).toBe(false))
    expect(result.current.favorites.ids.notebook.has('nb-1')).toBe(false)
  })

  test('ignores a second toggle while one is in flight, across instances', async () => {
    const put = deferred<void>()
    let putCalls = 0
    server.use(...favoritesFixture().handlers)
    server.use(
      http.put('/api/v1/favorites/notebook/nb-1', async () => {
        putCalls += 1
        await put.promise
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { Wrapper } = createWrapper()
    const { result } = renderHook(
      () => ({
        favorites: useFavorites(),
        first: useToggleFavorite('notebook', 'nb-1'),
        second: useToggleFavorite('notebook', 'nb-1'),
      }),
      { wrapper: Wrapper },
    )
    await waitFor(() => expect(result.current.favorites.isLoading).toBe(false))

    act(() => {
      result.current.first.toggle()
      // A same-resource toggle from another surface must not double-fire.
      result.current.second.toggle()
    })
    await waitFor(() => expect(putCalls).toBe(1))
    expect(result.current.favorites.ids.notebook.has('nb-1')).toBe(true)

    await act(async () => {
      put.resolve()
      await put.promise
    })
    await waitFor(() => expect(result.current.first.isPending).toBe(false))
    expect(putCalls).toBe(1)
  })

  test('a failed toggle does not drop a concurrent success on another resource', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const failA = deferred<void>()
      const { handlers, counts, hold } = favoritesFixture()
      server.use(...handlers)
      server.use(
        http.put('/api/v1/favorites/notebook/nb-a', async () => {
          await failA.promise
          return HttpResponse.json({ error: 'boom' }, { status: 500 })
        }),
        http.put('/api/v1/favorites/notebook/nb-b', async () => {
          hold('notebook', 'nb-b')
          return new HttpResponse(null, { status: 204 })
        }),
      )
      const { Wrapper } = createWrapper()
      const { result } = renderHook(
        () => ({
          favorites: useFavorites(),
          a: useToggleFavorite('notebook', 'nb-a'),
          b: useToggleFavorite('notebook', 'nb-b'),
        }),
        { wrapper: Wrapper },
      )
      await waitFor(() => expect(result.current.favorites.isLoading).toBe(false))
      expect(counts.gets).toBe(1)

      act(() => { result.current.a.toggle() })
      act(() => { result.current.b.toggle() })
      // B succeeds; wait until its post-success refetch has landed, so A's
      // rollback runs strictly after B is reconciled with the server.
      await waitFor(() => expect(counts.gets).toBe(2))
      await waitFor(() => expect(result.current.b.isPending).toBe(false))

      await act(async () => {
        failA.resolve()
        await failA.promise
      })
      await waitFor(() => expect(result.current.a.isPending).toBe(false))

      expect(result.current.favorites.ids.notebook.has('nb-a')).toBe(false)
      expect(result.current.favorites.ids.notebook.has('nb-b')).toBe(true)
    } finally {
      consoleError.mockRestore()
    }
  })

  test('two concurrent failures leave neither resource favorited', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const failA = deferred<void>()
      const failB = deferred<void>()
      server.use(...favoritesFixture().handlers)
      server.use(
        http.put('/api/v1/favorites/notebook/nb-a', async () => {
          await failA.promise
          return HttpResponse.json({ error: 'boom' }, { status: 500 })
        }),
        http.put('/api/v1/favorites/notebook/nb-b', async () => {
          await failB.promise
          return HttpResponse.json({ error: 'boom' }, { status: 500 })
        }),
      )
      const { Wrapper } = createWrapper()
      const { result } = renderHook(
        () => ({
          favorites: useFavorites(),
          a: useToggleFavorite('notebook', 'nb-a'),
          b: useToggleFavorite('notebook', 'nb-b'),
        }),
        { wrapper: Wrapper },
      )
      await waitFor(() => expect(result.current.favorites.isLoading).toBe(false))

      act(() => { result.current.a.toggle() })
      act(() => { result.current.b.toggle() })
      await waitFor(() => expect(result.current.favorites.ids.notebook.has('nb-a')).toBe(true))
      await waitFor(() => expect(result.current.favorites.ids.notebook.has('nb-b')).toBe(true))

      // A fails first, then B: B's rollback must not resurrect A.
      await act(async () => {
        failA.resolve()
        await failA.promise
      })
      await waitFor(() => expect(result.current.a.isPending).toBe(false))
      await act(async () => {
        failB.resolve()
        await failB.promise
      })
      await waitFor(() => expect(result.current.b.isPending).toBe(false))

      expect(result.current.favorites.ids.notebook.has('nb-a')).toBe(false)
      expect(result.current.favorites.ids.notebook.has('nb-b')).toBe(false)
    } finally {
      consoleError.mockRestore()
    }
  })
})
