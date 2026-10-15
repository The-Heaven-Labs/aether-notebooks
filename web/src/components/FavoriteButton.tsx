import { Bookmark } from 'lucide-react'
import { useFavorites, useToggleFavorite } from '../hooks/useFavorites'
import type { FavoriteResourceType } from '../types'

interface FavoriteButtonProps {
  resourceType: FavoriteResourceType
  resourceId: string
  /** Extra classes for placement (e.g. hover-reveal rules on list rows). */
  className?: string
}

/**
 * Bookmark toggle for a catalog resource, built on the shared row-action
 * metrics (26×26, 36 on touch via the coarse-pointer rule). Filled accent
 * while favorited; the optimistic update in `useToggleFavorite` flips every
 * surface reading `useFavorites()` together.
 */
export function FavoriteButton({ resourceType, resourceId, className }: FavoriteButtonProps) {
  const { ids } = useFavorites()
  const { toggle, isPending } = useToggleFavorite(resourceType, resourceId)
  const isFavorite = ids[resourceType].has(resourceId)
  const label = isFavorite ? 'Remove from favorites' : 'Add to favorites'

  return (
    <button
      type="button"
      className={['row-action', className].filter(Boolean).join(' ')}
      // Pin the active ink inline: the generic row-action hover swap to
      // primary ink would read as "about to remove" the bookmark.
      style={isFavorite ? { color: 'var(--accent)' } : undefined}
      aria-pressed={isFavorite}
      aria-label={label}
      title={label}
      disabled={isPending}
      onClick={(e) => {
        // Row clicks navigate; the toggle is its own control.
        e.stopPropagation()
        toggle()
      }}
    >
      <Bookmark size={13} fill={isFavorite ? 'currentColor' : 'none'} />
    </button>
  )
}
