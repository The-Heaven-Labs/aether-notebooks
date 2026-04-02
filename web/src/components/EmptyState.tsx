import type React from 'react'

interface Props {
  icon?: React.ReactNode
  title: string
  text?: string
  action?: {
    label: string
    onClick: () => void
  }
}

export function EmptyState({ icon, title, text, action }: Props) {
  return (
    <div style={styles.outer}>
      {icon && (
        <div style={styles.iconTile}>{icon}</div>
      )}
      <p style={styles.title}>{title}</p>
      {text && <p style={styles.text}>{text}</p>}
      {action && (
        <button type="button" style={styles.actionBtn} onClick={action.onClick}>
          {action.label}
        </button>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  outer: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    padding: '80px 0',
    gap: 12,
  },
  iconTile: {
    width: 56,
    height: 56,
    background: 'var(--bg-secondary)',
    borderRadius: 14,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: 28,
    color: 'var(--text-muted)',
    marginBottom: 4,
  },
  title: {
    fontSize: 18,
    fontWeight: 700,
    color: 'var(--text-primary)',
    letterSpacing: '-0.2px',
    margin: 0,
  },
  text: {
    fontSize: 14,
    color: 'var(--text-secondary)',
    margin: 0,
  },
  actionBtn: {
    padding: '8px 18px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 7,
    fontSize: 13,
    fontWeight: 600,
    cursor: 'pointer',
    letterSpacing: '0.01em',
  },
}
