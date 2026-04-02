import type React from 'react'
import { X } from 'lucide-react'

interface Props {
  title: string
  onClose?: () => void
  closeTitle?: string
  style?: React.CSSProperties
}

export function PanelHeader({ title, onClose, closeTitle = 'Close', style }: Props) {
  return (
    <div style={{ ...styles.header, ...style }}>
      <span style={styles.title}>{title}</span>
      {onClose && (
        <button
          style={styles.closeBtn}
          onClick={onClose}
          title={closeTitle}
        >
          <X size={13} />
        </button>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  header: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '10px 14px',
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  title: {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
  },
  closeBtn: {
    display: 'flex',
    alignItems: 'center',
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
    fontSize: 12,
    padding: '2px 4px',
    borderRadius: 4,
    lineHeight: 1,
  },
}
