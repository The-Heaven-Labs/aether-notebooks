import { useMemo, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import type { FolderSection } from '../types'

/** Load-more page size (the API default; server clamps to 1–200). */
const FOLDER_SECTION_PAGE_SIZE = 50

export type FolderSectionKind = 'notebook' | 'connector' | 'dashboard'

interface FolderSectionExtras<T> {
  /** Folder these pages belong to ('root' for the root listing). */
  folderKey: string
  /** First-page generation (`dataUpdatedAt`) these pages extend. */
  base: number
  items: T[]
  /**
   * Offset for the next request, advanced by each page's raw item count (not
   * the deduped one) so a skipped duplicate can never shift the window.
   */
  nextOffset: number
  /** Latest server total / has_more; a page's values win over the first page's. */
  total: number | null
  hasMore: boolean | null
  /** A page added no new rows while the server still reported more: stop. */
  stopped: boolean
}

/** Variables carried from the click to the response, immune to re-pointed options. */
interface LoadMoreVars {
  offset: number
  base: number
  folderKey: string
}

export interface FolderSectionPager<T> {
  /** First page plus every loaded extra page, deduped by id. */
  items: T[]
  /** Exact visible total, or null when the server did not report one. */
  total: number | null
  hasMore: boolean
  stopped: boolean
  loading: boolean
  loadMore: () => void
}

/**
 * Accumulates load-more pages for one folder section on top of the first page
 * the listing query returned. Extras are pinned to the folder and to the
 * listing generation they were appended to, so any successful refetch (e.g.
 * after a mutation invalidation) — or a folder switch — drops them instead of
 * mixing stale rows into the first page. Pages are deduped by id, against the
 * first page, earlier pages, and ids repeated within one page.
 */
export function useFolderSectionPager<T extends { id: string }>({
  kind,
  folderID,
  firstPage,
  firstPageTotal,
  firstPageHasMore,
  generation,
  contentsKey,
  onError,
}: {
  kind: FolderSectionKind
  folderID: string | null
  firstPage: T[]
  firstPageTotal: number | null
  firstPageHasMore: boolean
  generation: number
  contentsKey: readonly unknown[]
  onError: (e: Error) => void
}): FolderSectionPager<T> {
  const qc = useQueryClient()
  const folderKey = folderID ?? 'root'
  const [extras, setExtras] = useState<FolderSectionExtras<T> | null>(null)

  // Only extras from the current folder and first-page generation count.
  const active = extras && extras.folderKey === folderKey && extras.base === generation ? extras : null
  // First page plus accumulated extras, kept referentially stable so the
  // section is usable as a lookup source for every loaded row.
  const items = useMemo(
    () => (active ? [...firstPage, ...active.items] : firstPage),
    [active, firstPage],
  )

  // Synchronous in-flight latch: `isPending` flips a tick later, so two clicks
  // dispatched in the same task would otherwise both fire.
  const inFlight = useRef(false)

  const more = useMutation({
    mutationFn: ({ offset }: LoadMoreVars) =>
      api.get<FolderSection<T>>(
        `${folderID ? `/api/v1/folders/${folderID}` : '/api/v1/folders'}/contents` +
          `?type=${kind}&offset=${offset}&limit=${FOLDER_SECTION_PAGE_SIZE}`
      ),
    onError,
    onSuccess: (page, { offset, base, folderKey: requestedFolderKey }) => {
      // React Query re-points these options at the latest render, so the guard
      // must read everything from the variables: the page belongs to the folder
      // it was requested for (the generation alone can collide within a
      // millisecond), and to that folder's first-page generation.
      if (requestedFolderKey !== folderKey) return
      if (qc.getQueryState(contentsKey)?.dataUpdatedAt !== base) return
      setExtras((prev) => {
        const current = prev && prev.folderKey === folderKey && prev.base === base
          ? prev
          : { folderKey, base, items: [], nextOffset: firstPage.length, total: null, hasMore: null, stopped: false }
        const seen = new Set<string>(firstPage.map((item) => item.id))
        for (const item of current.items) seen.add(item.id)
        const fresh = page.items.filter((item) => {
          if (seen.has(item.id)) return false
          seen.add(item.id)
          return true
        })
        return {
          ...current,
          items: [...current.items, ...fresh],
          nextOffset: offset + page.items.length,
          total: page.total,
          hasMore: page.has_more,
          // An empty page while has_more stays true would loop forever: keep
          // what is loaded and stop asking.
          stopped: fresh.length === 0,
        }
      })
    },
  })

  const loadMore = () => {
    if (inFlight.current) return
    inFlight.current = true
    more.mutate(
      { offset: active ? active.nextOffset : firstPage.length, base: generation, folderKey },
      { onSettled: () => { inFlight.current = false } },
    )
  }

  return {
    items,
    total: active && active.total !== null ? active.total : firstPageTotal,
    hasMore: active && active.hasMore !== null ? active.hasMore : firstPageHasMore,
    stopped: active?.stopped ?? false,
    loading: more.isPending,
    loadMore,
  }
}
