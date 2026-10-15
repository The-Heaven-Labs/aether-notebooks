import type React from 'react'

interface KbdProps {
  children: React.ReactNode
  /** Placement-only overrides merged over the shared chip styling. */
  style?: React.CSSProperties
}

/**
 * The shared keyboard chip: 11px mono key hints. Used by the shortcuts modal,
 * the search palette's input/footer, and the top-bar search field.
 */
export function Kbd({ children, style }: KbdProps) {
  return <kbd style={{ ...styles.kbd, ...style }}>{children}</kbd>
}

const styles: Record<string, React.CSSProperties> = {
  kbd: {
    fontFamily: 'var(--font-mono)',
    fontSize: 11,
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 3,
    padding: '2px 6px',
    color: 'var(--text-primary)',
    lineHeight: 1.4,
    whiteSpace: 'nowrap',
  },
}
