import type React from 'react'
import { X, ChevronDown, PanelRightOpen, PanelRightClose } from 'lucide-react'

interface Props {
  title: string
  onClose?: () => void
  onMinimize?: () => void
  onDock?: () => void
  docked?: boolean
  closeTitle?: string
  style?: React.CSSProperties
}

export function PanelHeader({ title, onClose, onMinimize, onDock, docked, closeTitle = 'Close', style }: Props) {
  return (
    <div style={{ ...styles.header, ...style }}>
      <span style={styles.title}>{title}</span>
      <div style={{ display: 'flex', gap: 4 }}>
        {onDock && (
          <button
            style={styles.iconBtn}
            onClick={onDock}
            title={docked ? 'Undock panel' : 'Dock to right side'}
          >
            {docked ? <PanelRightOpen size={13} /> : <PanelRightClose size={13} />}
          </button>
        )}
        {onMinimize && (
          <button
            style={styles.iconBtn}
            onClick={onMinimize}
            title="Minimize"
          >
            <ChevronDown size={13} />
          </button>
        )}
        {onClose && (
          <button
            style={styles.iconBtn}
            onClick={onClose}
            title={closeTitle}
          >
            <X size={13} />
          </button>
        )}
      </div>
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
    background: 'var(--bg-primary)',
    flexShrink: 0,
  },
  title: {
    fontSize: 12,
    fontWeight: 600,
    color: 'var(--text-secondary)',
    textTransform: 'uppercase',
    letterSpacing: '0.06em',
  },
  iconBtn: {
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
