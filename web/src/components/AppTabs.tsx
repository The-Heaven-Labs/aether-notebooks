import { useRef } from 'react'
import type { CSSProperties, KeyboardEvent } from 'react'
import type { DashboardTab } from '../types'

interface AppTabsProps {
  /** The dashboard's chapters; an empty list renders no bar at all. */
  tabs: DashboardTab[]
  /** The selected tab id; unknown or absent falls back to the first tab. */
  activeId: string | null
  onChange: (id: string) => void
}

/** DOM id of a chapter tab's button (the panel's `aria-labelledby`). */
export function appTabButtonId(tabId: string): string {
  return `app-tab-${tabId}`
}

/** DOM id of a chapter's panel container (rendered by the pages). */
export function appTabPanelId(tabId: string): string {
  return `app-tabpanel-${tabId}`
}

/**
 * The App surface's chapter tablist: a quiet ruled row under the masthead,
 * 13px DM Sans, 36px tap targets, hairline underline on the active chapter.
 * Automatic-activation keyboard nav (ArrowLeft / ArrowRight, wrapping, plus
 * Home / End) keeps selection and focus in lockstep; only the selected tab is
 * in the tab order. Each tab carries the ids that link it to its panel.
 */
export function AppTabs({ tabs, activeId, onChange }: AppTabsProps) {
  const buttonsRef = useRef<Array<HTMLButtonElement | null>>([])
  if (tabs.length === 0) return null

  const selectedIndex = Math.max(0, tabs.findIndex((t) => t.id === activeId))

  const select = (index: number) => {
    const tab = tabs[index]
    if (!tab) return
    onChange(tab.id)
    buttonsRef.current[index]?.focus()
  }

  const handleKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    switch (event.key) {
      case 'ArrowRight':
        event.preventDefault()
        select((index + 1) % tabs.length)
        break
      case 'ArrowLeft':
        event.preventDefault()
        select((index - 1 + tabs.length) % tabs.length)
        break
      case 'Home':
        event.preventDefault()
        select(0)
        break
      case 'End':
        event.preventDefault()
        select(tabs.length - 1)
        break
      default:
        break
    }
  }

  return (
    <div role="tablist" aria-label="Dashboard tabs" style={styles.tablist}>
      {tabs.map((tab, index) => {
        const selected = index === selectedIndex
        return (
          <button
            key={tab.id}
            ref={(el) => { buttonsRef.current[index] = el }}
            id={appTabButtonId(tab.id)}
            type="button"
            role="tab"
            aria-selected={selected}
            aria-controls={appTabPanelId(tab.id)}
            tabIndex={selected ? 0 : -1}
            style={selected ? { ...styles.tab, ...styles.tabActive } : styles.tab}
            onClick={() => onChange(tab.id)}
            onKeyDown={(event) => handleKeyDown(event, index)}
          >
            {tab.title}
          </button>
        )
      })}
    </div>
  )
}

/** Quiet placeholder for a chapter that has no widgets yet. */
export function EmptyChapter() {
  return <p style={styles.emptyChapter}>Nothing in this chapter yet.</p>
}

const styles: Record<string, CSSProperties> = {
  // The chapter rule: the tablist owns the hairline; the active tab's
  // indicator is drawn inside its own box (see tabActive) so nothing bleeds
  // out of the scroll container. `overflowY` is pinned because a one-axis
  // `auto` computes the other axis to `auto` as well, which would show a
  // vertical scrollbar for any bleed.
  tablist: {
    display: 'flex',
    alignItems: 'stretch',
    gap: 24,
    borderBottom: '1px solid var(--border-light)',
    overflowX: 'auto',
    overflowY: 'hidden',
  },
  tab: {
    display: 'inline-flex',
    alignItems: 'center',
    minHeight: 36,
    padding: '0 0 9px',
    fontFamily: 'var(--font-sans)',
    fontSize: 13,
    lineHeight: 1.2,
    fontWeight: 500,
    color: 'var(--text-muted)',
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    whiteSpace: 'nowrap',
  },
  tabActive: {
    color: 'var(--text-primary)',
    fontWeight: 600,
    // A 1px accent rule on the tab's inner bottom edge, flush with the
    // tablist's hairline. An inset shadow (not a border) keeps the indicator
    // inside the box; a negative-margin border would be clipped by the
    // scroll container.
    boxShadow: 'inset 0 -1px 0 0 var(--accent)',
  },
  emptyChapter: {
    margin: 0,
    padding: '24px 0',
    fontSize: 13,
    fontStyle: 'italic',
    color: 'var(--text-muted)',
  },
}
