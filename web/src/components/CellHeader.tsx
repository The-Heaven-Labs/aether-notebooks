import { useState } from 'react'
import type { Cell } from '../types'
import { Link2 } from 'lucide-react'

interface Props {
  cell: Cell
  onUpdateCell: (updates: Partial<Pick<Cell, 'title' | 'description' | 'slug'>>) => void
  referencedByCount?: number
}

export function CellHeader({ cell, onUpdateCell, referencedByCount = 0 }: Props) {
  const [editingSlug, setEditingSlug] = useState(false)
  const [slugDraft, setSlugDraft] = useState(cell.slug ?? '')
  const [slugError, setSlugError] = useState('')

  const hasHeader = cell.title || cell.description || cell.slug

  if (!hasHeader && referencedByCount === 0) return null

  return (
    <div style={styles.header}>
      <div style={styles.titleRow}>
        {cell.title !== undefined && (
          <input
            style={styles.titleInput}
            value={cell.title}
            onChange={(e) => onUpdateCell({ title: e.target.value })}
            placeholder="Cell title…"
          />
        )}
        <div style={styles.slugArea}>
          {editingSlug ? (
            <input
              style={styles.slugInput}
              value={slugDraft}
              onChange={(e) => setSlugDraft(e.target.value.replace(/[^a-z0-9_]/g, '_'))}
              onBlur={() => {
                setEditingSlug(false)
                setSlugError('')
                onUpdateCell({ slug: slugDraft || undefined })
              }}
              onKeyDown={(e) => { if (e.key === 'Enter') e.currentTarget.blur() }}
              autoFocus
            />
          ) : (
            <button
              style={styles.slugBadge}
              onClick={() => { setSlugDraft(cell.slug ?? ''); setEditingSlug(true) }}
              title="Click to edit cell slug (used in {{slug}} references)"
            >
              {cell.slug ? `{{${cell.slug}}}` : '+ slug'}
            </button>
          )}
          {slugError && <span style={styles.slugError}>{slugError}</span>}
          {referencedByCount > 0 && (
            <span style={{ ...styles.refBadge, display: 'inline-flex', alignItems: 'center', gap: 2 }} title={`Referenced by ${referencedByCount} cell(s)`}>
              <Link2 size={11} />{referencedByCount}
            </span>
          )}
        </div>
      </div>
      {cell.description !== undefined && (
        <input
          style={styles.descInput}
          value={cell.description}
          onChange={(e) => onUpdateCell({ description: e.target.value })}
          placeholder="Cell description…"
        />
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  header: { padding: '6px 16px 4px', borderBottom: '1px solid var(--border-light)', background: 'var(--bg-primary)' },
  titleRow: { display: 'flex', alignItems: 'center', gap: 8 },
  titleInput: { flex: 1, border: 'none', outline: 'none', fontSize: 13, fontWeight: 600, color: 'var(--text-primary)', background: 'transparent', fontFamily: 'var(--font-sans)' },
  slugArea: { display: 'flex', alignItems: 'center', gap: 6 },
  slugBadge: { fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)', background: 'var(--bg-secondary)', border: '1px solid var(--border-light)', borderRadius: 4, padding: '2px 6px', cursor: 'pointer' },
  slugInput: { fontSize: 11, fontFamily: 'var(--font-mono)', padding: '2px 6px', border: '1px solid var(--accent)', borderRadius: 4, outline: 'none', width: 120 },
  slugError: { fontSize: 11, color: 'var(--error)' },
  refBadge: { fontSize: 10, color: 'var(--accent)', background: 'var(--accent-light)', borderRadius: 4, padding: '1px 5px', cursor: 'default' },
  descInput: { width: '100%', border: 'none', outline: 'none', fontSize: 12, color: 'var(--text-secondary)', background: 'transparent', fontFamily: 'var(--font-sans)', marginTop: 2 },
}
