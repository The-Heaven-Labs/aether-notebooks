import { useEffect } from 'react'
import { AlertTriangle } from 'lucide-react'

interface ConfirmModalProps {
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  onConfirm: () => void
  onCancel: () => void
  destructive?: boolean
}

export function ConfirmModal({ title, message, confirmLabel = 'Discard', cancelLabel = 'Cancel', onConfirm, onCancel, destructive }: ConfirmModalProps) {
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onCancel])

  return (
    <div style={styles.overlay} onClick={onCancel}>
      <div style={styles.modal} onClick={e => e.stopPropagation()}>
        <div style={styles.iconWrap}>
          <AlertTriangle size={24} style={{ color: destructive ? 'var(--error, #ef4444)' : 'var(--accent)' }} />
        </div>
        <div style={styles.title}>{title}</div>
        <div style={styles.message}>{message}</div>
        <div style={styles.actions}>
          <button style={styles.cancelBtn} onClick={onCancel}>{cancelLabel}</button>
          <button style={{ ...styles.confirmBtn, ...(destructive ? styles.destructiveBtn : {}) }} onClick={onConfirm}>{confirmLabel}</button>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)',
    zIndex: 2000, display: 'flex', alignItems: 'center', justifyContent: 'center',
  },
  modal: {
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    padding: 24,
    width: 360,
    maxWidth: '90vw',
    boxShadow: '0 8px 32px rgba(0,0,0,0.3)',
    display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 12,
  },
  iconWrap: {
    width: 48, height: 48, borderRadius: '50%',
    display: 'flex', alignItems: 'center', justifyContent: 'center',
    background: 'var(--bg-secondary)',
  },
  title: { fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' },
  message: { fontSize: 13, color: 'var(--text-muted)', textAlign: 'center', lineHeight: 1.4 },
  actions: { display: 'flex', gap: 8, marginTop: 8 },
  cancelBtn: {
    fontSize: 12, padding: '6px 16px',
    background: 'var(--bg-input)', color: 'var(--text-primary)',
    border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer',
  },
  confirmBtn: {
    fontSize: 12, padding: '6px 16px',
    background: 'var(--accent)', color: '#fff',
    border: 'none', borderRadius: 4, cursor: 'pointer', fontWeight: 500,
  },
  destructiveBtn: {
    background: 'var(--error, #ef4444)',
  },
}
