import type React from 'react'
import { useEffect, useRef } from 'react'

export interface ConfirmDialogProps {
  open: boolean
  title: string
  message?: string
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean
  confirmDisabled?: boolean
  defaultFocusRef?: React.RefObject<HTMLInputElement | null>
  children?: React.ReactNode
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = 'Confirm',
  cancelLabel = 'Cancel',
  destructive = false,
  confirmDisabled = false,
  defaultFocusRef,
  children,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  const confirmRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (open) {
      // Default focus on Cancel (safer — prevents accidental destructive actions),
      // unless a custom focus target is provided (e.g. a confirmation input).
      ;(defaultFocusRef?.current ?? cancelRef.current)?.focus()
      const handleKey = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
          onCancel()
          return
        }
        // Left/Right arrows to switch between buttons (only while a button is focused,
        // so inputs keep their natural arrow-key behavior)
        if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
          const active = document.activeElement
          if (active === cancelRef.current || active === confirmRef.current) {
            e.preventDefault()
            if (active === cancelRef.current) {
              confirmRef.current?.focus()
            } else {
              cancelRef.current?.focus()
            }
          }
          return
        }
        // Tab trap: cycle between the two buttons
        if (e.key === 'Tab') {
          const active = document.activeElement
          if (e.shiftKey) {
            // Shift+Tab: if on cancel, wrap to confirm
            if (active === cancelRef.current) {
              e.preventDefault()
              confirmRef.current?.focus()
            }
          } else {
            // Tab: if on confirm, wrap to cancel
            if (active === confirmRef.current) {
              e.preventDefault()
              cancelRef.current?.focus()
            }
          }
          return
        }
        // Enter to activate focused button (skipped while the confirm action is disabled)
        if (e.key === 'Enter') {
          const active = document.activeElement
          if (active === cancelRef.current) {
            onCancel()
          } else if (!confirmDisabled) {
            onConfirm()
          }
        }
      }
      document.addEventListener('keydown', handleKey)
      return () => document.removeEventListener('keydown', handleKey)
    }
  }, [open, onCancel, onConfirm, confirmDisabled, defaultFocusRef])

  if (!open) return null

  return (
    <div style={styles.overlay} onClick={onCancel}>
      <div style={styles.modal} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>{title}</span>
        </div>
        {(message || children) && (
          <div style={styles.body}>
            {message && (
              <p style={styles.message}>{message}</p>
            )}
            {children}
          </div>
        )}
        <div style={styles.footer}>
          <button ref={cancelRef} style={styles.cancelBtn} onClick={onCancel}>
            {cancelLabel}
          </button>
          <button
            ref={confirmRef}
            disabled={confirmDisabled}
            style={{
              ...styles.confirmBtn,
              ...(destructive ? styles.confirmBtnDestructive : {}),
              ...(confirmDisabled ? { opacity: 0.6, cursor: 'not-allowed' } : {}),
            }}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  overlay: {
    position: 'fixed',
    inset: 0,
    background: 'var(--bg-overlay)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 1000,
  },
  modal: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: 8,
    boxShadow: 'var(--shadow-md)',
    width: 400,
    maxWidth: '90vw',
    overflow: 'hidden',
  },
  header: {
    padding: '16px 20px 0',
  },
  title: {
    fontSize: 15,
    fontWeight: 700,
    color: 'var(--text-primary)',
  },
  body: {
    padding: '12px 20px 0',
  },
  message: {
    fontSize: 13,
    color: 'var(--text-secondary)',
    lineHeight: 1.5,
    margin: 0,
  },
  footer: {
    display: 'flex',
    justifyContent: 'flex-end',
    gap: 8,
    padding: '16px 20px',
  },
  cancelBtn: {
    padding: '7px 16px',
    fontSize: 13,
    fontWeight: 500,
    border: '1px solid var(--border)',
    borderRadius: 4,
    background: 'var(--bg-secondary)',
    color: 'var(--text-secondary)',
    cursor: 'pointer',
  },
  confirmBtn: {
    padding: '7px 16px',
    fontSize: 13,
    fontWeight: 600,
    border: 'none',
    borderRadius: 4,
    background: 'var(--accent)',
    color: '#fff',
    cursor: 'pointer',
  },
  confirmBtnDestructive: {
    background: 'var(--error-full)',
  },
}
