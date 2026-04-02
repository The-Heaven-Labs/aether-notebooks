import type React from 'react'
import { X } from 'lucide-react'

interface Props {
  title: string
  onClose: () => void
  children: React.ReactNode
  minWidth?: number
}

export function Modal({ title, onClose, children, minWidth }: Props) {
  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={{ ...styles.modal, minWidth: minWidth ?? 400 }} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>{title}</span>
          <button style={{ ...styles.close, display: 'flex', alignItems: 'center' }} onClick={onClose}><X size={14} /></button>
        </div>
        <div>{children}</div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: { position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 },
  modal: { background: 'white', borderRadius: 12, boxShadow: '0 8px 32px rgba(0,0,0,0.2)', maxHeight: '80vh', overflow: 'auto' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px 20px', borderBottom: '1px solid var(--border-light)' },
  title: { fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' },
  close: { background: 'transparent', border: 'none', fontSize: 14, cursor: 'pointer', color: 'var(--text-muted)' },
}
