import type React from 'react'
import { Loader2 } from 'lucide-react'
import { FavoriteButton } from './FavoriteButton'
import type { FavoriteResourceType } from '../types'

/** Header cell for a table's action column (visually empty, announced to AT). */
export function RowActionsHeader() {
  return <span className="sr-only">Actions</span>
}

interface RowActionsCellProps {
  children: React.ReactNode
  /**
   * When set, renders the bookmark toggle immediately left of the action
   * grid — outside it, because the grid packs exactly two controls per line
   * (the row-actions CSS reserves 75px) and a third control inside would
   * wrap to a second row. The cell widens by the bookmark's 26px so the
   * reserved column never shifts when the toggle is hover-revealed.
   */
  favorite?: { resourceType: FavoriteResourceType; resourceId: string }
}

/** Right-aligned action column for a resource table row. */
export function RowActionsCell({ children, favorite }: RowActionsCellProps) {
  const grid = <div className="row-actions-grid">{children}</div>
  if (!favorite) return <td className="row-actions-cell">{grid}</td>
  return (
    <td className="row-actions-cell row-actions-cell--favorite">
      <div className="row-actions-cluster">
        <FavoriteButton
          resourceType={favorite.resourceType}
          resourceId={favorite.resourceId}
          className="favorite-reveal"
        />
        {grid}
      </div>
    </td>
  )
}

interface RowActionProps {
  /** Accessible name; also the tooltip unless `title` overrides it. */
  label: string
  icon: React.ReactNode
  onClick: () => void
  /** Marks the primary row action (usually Edit) with the accent color. */
  accent?: boolean
  /** Red hover treatment for destructive actions. */
  danger?: boolean
  disabled?: boolean
  /** Swaps the icon for a spinner while an async action runs. */
  spinning?: boolean
  /** Longer tooltip when the accessible name needs to stay short. */
  title?: string
}

/** Icon-only row action, the catalog pages' shared table action button. */
export function RowAction({ label, icon, onClick, accent, danger, disabled, spinning, title }: RowActionProps) {
  const className = [
    'row-action',
    accent ? 'row-action--accent' : '',
    danger ? 'row-action--danger' : '',
  ].filter(Boolean).join(' ')
  return (
    <button
      type="button"
      className={className}
      title={title ?? label}
      aria-label={label}
      onClick={onClick}
      disabled={disabled}
    >
      {spinning ? <Loader2 size={13} className="row-action-spin" /> : icon}
    </button>
  )
}

/** Full-width flex break that keeps row actions in right-aligned pairs. */
export function RowActionsBreak() {
  return <span className="row-actions-break" aria-hidden="true" />
}
