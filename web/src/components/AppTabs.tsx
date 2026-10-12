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

/**
 * The App surface's chapter tablist: a quiet ruled row under the masthead,
 * 13px DM Sans, 36px tap targets, hairline underline on the active chapter.
 * Automatic-activation keyboard nav (ArrowLeft / ArrowRight, wrapping) keeps
 * selection and focus in lockstep; only the selected tab is in the tab order.
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
    if (event.key === 'ArrowRight') {
      event.preventDefault()
      select((index + 1) % tabs.length)
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault()
      select((index - 1 + tabs.length) % tabs.length)
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
            type="button"
            role="tab"
            aria-selected={selected}
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

const styles: Record<string, CSSProperties> = {
  // The chapter rule: the tablist owns the hairline; the tabs' negative
  // margin-bottom pulls their own underline onto it so the active chapter
  // reads as a break in the rule, not a floating bar.
  tablist: {
    display: 'flex',
    alignItems: 'stretch',
    gap: 24,
    borderBottom: '1px solid var(--border-light)',
    overflowX: 'auto',
  },
  tab: {
    display: 'inline-flex',
    alignItems: 'center',
    minHeight: 36,
    marginBottom: -1,
    padding: '0 0 9px',
    fontFamily: 'var(--font-sans)',
    fontSize: 13,
    lineHeight: 1.2,
    fontWeight: 500,
    color: 'var(--text-muted)',
    background: 'none',
    border: 'none',
    // Longhands, not the `borderBottom` shorthand: React's style diff only
    // writes changed keys, and clearing a shorthand-set longhand on the
    // previously active tab would drop its color back to currentcolor.
    borderBottomWidth: 1,
    borderBottomStyle: 'solid',
    borderBottomColor: 'transparent',
    cursor: 'pointer',
    whiteSpace: 'nowrap',
  },
  tabActive: {
    color: 'var(--text-primary)',
    fontWeight: 600,
    borderBottomColor: 'var(--accent)',
  },
}
