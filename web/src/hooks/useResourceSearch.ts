import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { FavoriteResourceType, SearchResponse } from '../types'

/**
 * The API's maximum `q` length, in runes. Every search input caps at the same
 * bound so an over-long paste cannot dead-end on a 400.
 */
export const MAX_SEARCH_QUERY_LENGTH = 200
/** Copy for the over-long-query rejection (HTTP 400), shared by both inputs. */
export const SEARCH_QUERY_TOO_LONG_MESSAGE = 'Query is too long — 200 characters max.'

export interface ResourceSearchParams {
  query: string
  /** Resource types to search; omit for every searchable type. */
  types?: FavoriteResourceType[]
  /** Restrict results to the caller's favorites (`favorites=1`). */
  favorites?: boolean
  /** Results per type; the API defaults to 5 and caps at 20. */
  limit?: number
}

/**
 * React Query key for a resource search. The query is trimmed, so whitespace
 * variants share one cache entry.
 */
export function resourceSearchQueryKey(
  query: string,
  types?: FavoriteResourceType[],
  favorites = false,
  limit = 5,
) {
  return ['search', query.trim(), types, favorites, limit] as const
}

/**
 * Catalog search (`GET /api/v1/search`) as the caller. Stays idle for an empty
 * or whitespace-only query, keeps the previous results while a new debounced
 * query loads, and aborts the in-flight request when the key changes.
 */
export function useResourceSearch({ query, types, favorites = false, limit = 5 }: ResourceSearchParams) {
  const trimmed = query.trim()
  return useQuery<SearchResponse>({
    queryKey: resourceSearchQueryKey(trimmed, types, favorites, limit),
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams({ q: trimmed, limit: String(limit) })
      if (types?.length) params.set('types', types.join(','))
      if (favorites) params.set('favorites', '1')
      return api.get<SearchResponse>(`/api/v1/search?${params.toString()}`, { signal })
    },
    enabled: trimmed.length >= 1,
    placeholderData: keepPreviousData,
    staleTime: 10_000,
  })
}
