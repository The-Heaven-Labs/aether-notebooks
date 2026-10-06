import type React from 'react'
import { Modal } from './Modal'

interface Props {
  title: string
  onClose: () => void
  /** Control focused when the dialog opens; usually the first field. */
  initialFocusRef?: React.RefObject<HTMLElement | null>
  /** Field-grid width; defaults to min(760px, 92vw). */
  width?: number | string
  error?: string | null
  /** Extra controls rendered before Cancel/Submit (e.g. Test Connection). */
  footerExtra?: React.ReactNode
  submitLabel: string
  /** Label while the submit mutation is pending; defaults to "<submitLabel>…". */
  pendingLabel?: string
  pending?: boolean
  submitDisabled?: boolean
  submitTitle?: string
  onSubmit: () => void
  children: React.ReactNode
}

/**
 * Create/edit dialog shell shared by the resource catalog pages: a two-column
 * field grid over a sticky Cancel/Save footer, on top of the standard Modal.
 */
export function FormModal({
  title,
  onClose,
  initialFocusRef,
  width = 'min(760px, 92vw)',
  error,
  footerExtra,
  submitLabel,
  pendingLabel,
  pending = false,
  submitDisabled = false,
  submitTitle,
  onSubmit,
  children,
}: Props) {
  return (
    <Modal title={title} onClose={onClose} initialFocusRef={initialFocusRef}>
      <div className="form-modal-grid" style={{ width }}>
        {children}
      </div>
      <div className="form-modal-footer">
        {error && <p className="form-modal-error">{error}</p>}
        <div className="form-modal-actions">
          {footerExtra}
          {footerExtra && <span style={{ flex: 1 }} />}
          <button type="button" className="form-modal-btn" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="form-modal-btn form-modal-btn--primary"
            onClick={onSubmit}
            disabled={submitDisabled || pending}
            title={submitTitle}
          >
            {pending ? (pendingLabel ?? `${submitLabel}…`) : submitLabel}
          </button>
        </div>
      </div>
    </Modal>
  )
}
