import type React from 'react'
import { useEffect, useId, useRef } from 'react'
import { X } from 'lucide-react'

interface Props {
  title: string
  onClose: () => void
  children: React.ReactNode
  minWidth?: number
}

export function Modal({ title, onClose, children, minWidth }: Props) {
  const dialogRef = useRef<HTMLDivElement>(null)
  const titleId = useId()

  // Keep the latest close handler without re-running the mount effect (which
  // would steal focus on every parent render).
  const onCloseRef = useRef(onClose)
  useEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])

  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null
    dialogRef.current?.focus()
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); onCloseRef.current() }
    }
    window.addEventListener('keydown', handler)
    return () => {
      window.removeEventListener('keydown', handler)
      previouslyFocused?.focus?.()
    }
  }, [])

  return (
    <div style={styles.overlay} onClick={onClose}>
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        style={{ ...styles.modal, minWidth: minWidth ?? 400 }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={styles.header}>
          <span id={titleId} style={styles.title}>{title}</span>
          <button style={{ ...styles.close, display: 'flex', alignItems: 'center' }} onClick={onClose} aria-label="Close modal"><X size={14} /></button>
        </div>
        <div>{children}</div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: { position: 'fixed', inset: 0, background: 'var(--bg-overlay)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 },
  modal: { background: 'var(--bg-card)', borderRadius: 4, border: '1px solid var(--border)', boxShadow: 'var(--shadow-md)', maxHeight: '80vh', overflow: 'auto', outline: 'none' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px 20px', borderBottom: '1px solid var(--border)' },
  title: { fontSize: 15, fontWeight: 700, color: 'var(--text-primary)' },
  close: { background: 'transparent', border: 'none', fontSize: 14, cursor: 'pointer', color: 'var(--text-secondary)' },
}
