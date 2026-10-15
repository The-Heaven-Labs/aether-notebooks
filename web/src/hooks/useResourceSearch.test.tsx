import { describe, test, expect, beforeEach } from 'vitest'
import type { ReactNode } from 'react'
import { act, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { useResourceSearch } from './useResourceSearch'
import type { ResourceSummary } from '../types'

function createWrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>
  }
  return Wrapper
}

function summary(query: string): ResourceSummary {
  return {
    id: 'nb-1',
    resource_type: 'notebook',
    title: `Notebook for ${query}`,
    subtitle: 'Notebook',
    updated_at: '2026-01-01T00:00:00Z',
    is_favorite: false,
    can_edit: true,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

const searchRequests: string[] = []

beforeEach(() => {
  searchRequests.length = 0
  server.use(
    http.get('/api/v1/search', ({ request }) => {
      searchRequests.push(request.url)
      const q = new URL(request.url).searchParams.get('q') ?? ''
      return HttpResponse.json({
        query: q,
        groups: q
          ? [{ resource_type: 'notebook', has_more: false, results: [summary(q)] }]
          : [],
      })
    }),
  )
})

describe('useResourceSearch', () => {
  test('stays idle for empty and whitespace-only queries', () => {
    const { result, rerender } = renderHook(
      ({ query }: { query: string }) => useResourceSearch({ query }),
      { initialProps: { query: '' }, wrapper: createWrapper() },
    )
    expect(result.current.fetchStatus).toBe('idle')
    expect(result.current.data).toBeUndefined()

    rerender({ query: '   ' })
    expect(result.current.fetchStatus).toBe('idle')
    expect(searchRequests).toHaveLength(0)
  })

  test('fires one request for the trimmed query with the default limit', async () => {
    const { result } = renderHook(
      ({ query }: { query: string }) => useResourceSearch({ query }),
      { initialProps: { query: '  rev  ' }, wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.data).toBeDefined())
    expect(searchRequests).toHaveLength(1)

    const url = new URL(searchRequests[0])
    expect(url.pathname).toBe('/api/v1/search')
    expect(url.searchParams.get('q')).toBe('rev')
    expect(url.searchParams.get('limit')).toBe('5')
    expect(url.searchParams.get('types')).toBeNull()
    expect(url.searchParams.get('favorites')).toBeNull()
    expect(result.current.data?.groups[0].results[0].title).toBe('Notebook for rev')
  })

  test('passes types, favorites, and limit through to the request', async () => {
    const { result } = renderHook(
      () => useResourceSearch({ query: 'rev', types: ['notebook', 'mcp_server'], favorites: true, limit: 20 }),
      { wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.data).toBeDefined())

    const url = new URL(searchRequests[0])
    expect(url.searchParams.get('types')).toBe('notebook,mcp_server')
    expect(url.searchParams.get('favorites')).toBe('1')
    expect(url.searchParams.get('limit')).toBe('20')
  })

  test('whitespace-only differences share one cache entry', async () => {
    const { result, rerender } = renderHook(
      ({ query }: { query: string }) => useResourceSearch({ query }),
      { initialProps: { query: 'rev' }, wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.data).toBeDefined())
    rerender({ query: ' rev ' })
    expect(searchRequests).toHaveLength(1)
  })

  test('keeps the previous results while the next query loads', async () => {
    const second = deferred<void>()
    let calls = 0
    server.use(
      http.get('/api/v1/search', async ({ request }) => {
        calls += 1
        const q = new URL(request.url).searchParams.get('q') ?? ''
        if (calls > 1) await second.promise
        return HttpResponse.json({
          query: q,
          groups: [{ resource_type: 'notebook', has_more: false, results: [summary(q)] }],
        })
      }),
    )

    const { result, rerender } = renderHook(
      ({ query }: { query: string }) => useResourceSearch({ query }),
      { initialProps: { query: 'one' }, wrapper: createWrapper() },
    )
    await waitFor(() => expect(result.current.data?.query).toBe('one'))
    expect(result.current.isPlaceholderData).toBe(false)

    rerender({ query: 'two' })
    await waitFor(() => expect(result.current.isPlaceholderData).toBe(true))
    // The previous page is still rendered while the new one is in flight.
    expect(result.current.data?.query).toBe('one')

    await act(async () => {
      second.resolve()
      await second.promise
    })
    await waitFor(() => expect(result.current.data?.query).toBe('two'))
    expect(result.current.isPlaceholderData).toBe(false)
  })

  test('a stale response cannot overwrite a newer query', async () => {
    const first = deferred<void>()
    server.use(
      http.get('/api/v1/search', async ({ request }) => {
        const q = new URL(request.url).searchParams.get('q') ?? ''
        if (q === 'one') await first.promise
        return HttpResponse.json({
          query: q,
          groups: [{ resource_type: 'notebook', has_more: false, results: [summary(q)] }],
        })
      }),
    )

    const { result, rerender } = renderHook(
      ({ query }: { query: string }) => useResourceSearch({ query }),
      { initialProps: { query: 'one' }, wrapper: createWrapper() },
    )
    // The first request is stuck in flight; the key changes underneath it.
    await waitFor(() => expect(result.current.fetchStatus).toBe('fetching'))
    rerender({ query: 'two' })
    await waitFor(() => expect(result.current.data?.query).toBe('two'))

    // Let the abandoned request finish; it must not clobber the current data.
    await act(async () => {
      first.resolve()
      await first.promise
    })
    await waitFor(() => expect(result.current.data?.query).toBe('two'))
  })
})
