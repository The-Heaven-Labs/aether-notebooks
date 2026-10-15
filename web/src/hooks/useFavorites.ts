import { useCallback } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { FAVORITE_RESOURCE_TYPES } from '../types'
import type { FavoriteGroup, FavoriteResourceType, FavoritesResponse } from '../types'

/** React Query key for the caller's favorites listing. */
export const favoritesQueryKey = ['favorites'] as const

/** Per-type favorite id sets; every type is present, so consumers never branch. */
export type FavoriteIDs = Record<FavoriteResourceType, Set<string>>

/** Builds the per-type id sets from a grouped favorites listing. */
export function favoriteIDs(groups: readonly FavoriteGroup[]): FavoriteIDs {
  const ids = {} as FavoriteIDs
  for (const type of FAVORITE_RESOURCE_TYPES) ids[type] = new Set()
  for (const group of groups) {
    const set = ids[group.resource_type]
    // A retired type would be skipped server-side too; stay defensive.
    if (!set) continue
    for (const result of group.results) set.add(result.id)
  }
  return ids
}

/**
 * The `['favorites']` cache value: the grouped listing plus its derived id
 * sets. `ids` is built once per fetch, so toggles only flip the sets and the
 * post-success invalidation refreshes the groups.
 */
export interface FavoritesData {
  groups: FavoriteGroup[]
  ids: FavoriteIDs
}

const EMPTY_GROUPS: FavoriteGroup[] = []
/**
 * Shared empty stand-in returned while the listing loads. The record is
 * frozen and its Sets are read-only by convention: internal writes always
 * replace a clone of the toggled type's Set and never mutate these.
 */
const EMPTY_IDS: FavoriteIDs = Object.freeze(favoriteIDs(EMPTY_GROUPS))

export interface FavoritesState {
  groups: FavoriteGroup[]
  ids: FavoriteIDs
  isLoading: boolean
  isError: boolean
}

/**
 * The caller's favorites: one source of truth every bookmark toggle reads and
 * writes. `ids` drives toggle state (fast, per-type set lookups); `groups` is
 * the grouped listing for renderers.
 */
export function useFavorites(): FavoritesState {
  const { data, isLoading, isError } = useQuery<FavoritesData>({
    queryKey: favoritesQueryKey,
    queryFn: async () => {
      const response = await api.get<FavoritesResponse>('/api/v1/favorites')
      return { groups: response.groups, ids: favoriteIDs(response.groups) }
    },
    // Favorites change only through the user's own toggles, which invalidate
    // this key; 60s keeps cross-surface reads cheap without going stale.
    staleTime: 60_000,
  })
  return {
    groups: data?.groups ?? EMPTY_GROUPS,
    ids: data?.ids ?? EMPTY_IDS,
    isLoading,
    isError,
  }
}

/** The favorites URL for one resource. */
function favoriteURL(resourceType: FavoriteResourceType, resourceId: string): string {
  return `/api/v1/favorites/${resourceType}/${encodeURIComponent(resourceId)}`
}

/** Returns `ids` with one resource added or removed (other sets are shared). */
function withFavorite(
  ids: FavoriteIDs,
  resourceType: FavoriteResourceType,
  resourceId: string,
  favorite: boolean,
): FavoriteIDs {
  const next: FavoriteIDs = { ...ids }
  const set = new Set(ids[resourceType])
  if (favorite) set.add(resourceId)
  else set.delete(resourceId)
  next[resourceType] = set
  return next
}

export interface ToggleFavorite {
  /** Flips the resource; a toggle while one is in flight is ignored. */
  toggle: () => void
  isPending: boolean
}

/**
 * Toggle single-flight, keyed by `type:id`: one in-flight toggle per resource
 * across every hook instance (a pending toggle's optimistic state is shared
 * through the cache, so a duplicate from another surface is a no-op).
 */
const togglesInFlight = new Set<string>()

/**
 * Optimistic bookmark toggle for one resource.
 *
 * The `['favorites']` cache flips immediately (id sets only — the groups
 * catch up on the success invalidation), so every surface reading
 * `useFavorites()` flips together. On failure the exact previous cache value
 * is restored and the error is logged; the app has no toast system, so this
 * matches the console-only convention of its other mutations.
 */
export function useToggleFavorite(resourceType: FavoriteResourceType, resourceId: string): ToggleFavorite {
  const queryClient = useQueryClient()
  const resourceKey = `${resourceType}:${resourceId}`

  const { mutate, isPending } = useMutation({
    mutationFn: (isFavorite: boolean) =>
      isFavorite
        ? api.delete<void>(favoriteURL(resourceType, resourceId))
        : api.put<void>(favoriteURL(resourceType, resourceId)),
    onMutate: async (isFavorite) => {
      await queryClient.cancelQueries({ queryKey: favoritesQueryKey })
      queryClient.setQueryData<FavoritesData>(favoritesQueryKey, (data) =>
        data
          ? { groups: data.groups, ids: withFavorite(data.ids, resourceType, resourceId, !isFavorite) }
          : data,
      )
    },
    onError: (err, isFavorite) => {
      // Id-local functional rollback: restore only this resource, so a
      // concurrent toggle on another resource keeps its optimistic or
      // server-confirmed entry (a whole-cache snapshot would drop it).
      queryClient.setQueryData<FavoritesData>(favoritesQueryKey, (data) =>
        data
          ? { groups: data.groups, ids: withFavorite(data.ids, resourceType, resourceId, isFavorite) }
          : data,
      )
      console.error(isFavorite ? 'Failed to remove favorite:' : 'Failed to add favorite:', err)
    },
    onSuccess: () => {
      // Refresh the groups (and confirm the id sets) from the server.
      void queryClient.invalidateQueries({ queryKey: favoritesQueryKey })
    },
    onSettled: () => {
      togglesInFlight.delete(resourceKey)
    },
  })

  const toggle = useCallback(() => {
    if (!resourceId || togglesInFlight.has(resourceKey)) return
    const data = queryClient.getQueryData<FavoritesData>(favoritesQueryKey)
    const isFavorite = data?.ids[resourceType]?.has(resourceId) ?? false
    togglesInFlight.add(resourceKey)
    mutate(isFavorite)
  }, [queryClient, resourceType, resourceId, resourceKey, mutate])

  return { toggle, isPending }
}
