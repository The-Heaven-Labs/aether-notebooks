import type React from 'react'

interface KbdProps {
  children: React.ReactNode
  /** Placement-only overrides merged over the shared chip styling. */
  style?: React.CSSProperties
  /**
   * `chrome` tones the chip for the always-dark navigation bar (the
   * `.nav-seg`/`.nav-btn` band), where the body tokens nearly vanish; the
   * default body tone is byte-identical for palette/modal callers.
   */
  tone?: 'default' | 'chrome'
}

/**
 * The shared keyboard chip: 11px mono key hints. Used by the shortcuts modal,
 * the search palette's input/footer, and the top-bar search field.
 */
export function Kbd({ children, style, tone = 'default' }: KbdProps) {
  return (
    <kbd style={{ ...styles.kbd, ...(tone === 'chrome' ? styles.chrome : undefined), ...style }}>
      {children}
    </kbd>
  )
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
  chrome: {
    background: 'rgba(255, 255, 255, 0.08)',
    border: '1px solid rgba(255, 255, 255, 0.18)',
    color: 'var(--nav-text)',
  },
}
