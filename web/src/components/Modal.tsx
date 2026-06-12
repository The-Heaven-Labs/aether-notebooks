import type React from 'react'
import { useEffect } from 'react'
import { X } from 'lucide-react'

interface Props {
  title: string
  onClose: () => void
  children: React.ReactNode
  minWidth?: number
}

export function Modal({ title, onClose, children, minWidth }: Props) {
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); onClose() }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onClose])

  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={{ ...styles.modal, minWidth: minWidth ?? 400 }} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>{title}</span>
          <button style={{ ...styles.close, display: 'flex', alignItems: 'center' }} onClick={onClose} aria-label="Close modal"><X size={14} /></button>
        </div>
        <div>{children}</div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: { position: 'fixed', inset: 0, background: 'var(--bg-overlay)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 },
  modal: { background: 'var(--bg-card)', borderRadius: 4, border: '1px solid var(--border)', boxShadow: 'var(--shadow-md)', maxHeight: '80vh', overflow: 'auto' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px 20px', borderBottom: '1px solid var(--border)' },
  title: { fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' },
  close: { background: 'transparent', border: 'none', fontSize: 14, cursor: 'pointer', color: 'var(--text-secondary)' },
}
