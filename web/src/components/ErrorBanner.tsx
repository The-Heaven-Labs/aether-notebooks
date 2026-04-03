import { X } from 'lucide-react'

interface Props {
  message: string
  onDismiss?: () => void
  variant?: 'error' | 'warning' | 'info'
}

export function ErrorBanner({ message, onDismiss, variant = 'error' }: Props) {
  const variantStyles = {
    error: {
      background: 'var(--error-light)',
      border: '1px solid var(--error-border)',
      color: 'var(--error-full)',
    },
    warning: {
      background: '#fff8e6',
      border: '1px solid #f5d78e',
      color: '#8a6d3b',
    },
    info: {
      background: 'var(--accent-light)',
      border: '1px solid var(--accent)',
      color: 'var(--accent)',
    },
  }

  const style = variantStyles[variant]

  return (
    <div style={{ ...styles.banner, ...style }}>
      <span style={styles.message}>{message}</span>
      {onDismiss && (
        <button style={styles.dismiss} onClick={onDismiss} aria-label="Dismiss">
          <X size={14} />
        </button>
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  banner: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    padding: '8px 12px',
    borderRadius: 4,
    fontSize: 13,
    fontFamily: 'var(--font-sans)',
  },
  message: {
    flex: 1,
  },
  dismiss: {
    display: 'flex',
    alignItems: 'center',
    background: 'transparent',
    border: 'none',
    cursor: 'pointer',
    padding: 0,
    color: 'inherit',
    opacity: 0.7,
    transition: 'opacity 0.15s',
  },
}