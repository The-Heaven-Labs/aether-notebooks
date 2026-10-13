import { useRef, useState } from 'react'
import { Info } from 'lucide-react'
import type { FieldHelp } from '../app-builder/fieldHelp'

const styles: Record<string, React.CSSProperties> = {
  wrap: { position: 'relative', display: 'inline-flex', alignItems: 'center' },
  trigger: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
    width: 16, height: 16, padding: 0, marginLeft: 4,
    background: 'none', border: 'none', cursor: 'pointer',
    color: 'var(--text-muted)', borderRadius: 3, flexShrink: 0,
  },
  popover: {
    position: 'absolute', top: '100%', left: 0, marginTop: 4, zIndex: 1750,
    width: 260, maxWidth: '80vw', padding: '8px 10px',
    background: 'var(--bg-card)', border: '1px solid var(--border)',
    borderRadius: 4, boxShadow: 'var(--shadow-lg, 0 4px 16px rgba(0,0,0,0.2))',
    fontSize: 12, lineHeight: 1.5, color: 'var(--text-secondary)',
    display: 'flex', flexDirection: 'column', gap: 4,
  },
  popoverLabel: { fontSize: 11, fontWeight: 700, color: 'var(--text-primary)' },
  example: {
    fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-primary)',
    background: 'var(--bg-input)', border: '1px solid var(--border-light)',
    borderRadius: 3, padding: '2px 4px', whiteSpace: 'pre-wrap', wordBreak: 'break-word',
  },
}

/**
 * Accessible field-help trigger: a focusable button that opens a small popover
 * on click or Enter/Space and closes on Escape or blur. It never depends on
 * hover, so keyboard and touch reach the same help, and while open the trigger
 * describes itself with the popover (`aria-describedby`).
 */
export function HelpTip({ help, className }: { help: FieldHelp; className?: string }) {
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLSpanElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const popoverId = `helptip-${help.label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`

  return (
    <span ref={wrapRef} style={styles.wrap} className={className}>
      <button
        ref={triggerRef}
        type="button"
        style={styles.trigger}
        aria-label={`Help: ${help.label}`}
        aria-expanded={open}
        aria-describedby={open ? popoverId : undefined}
        onClick={() => setOpen((v) => !v)}
        onKeyDown={(e) => {
          // Buttons activate on Enter/Space via a synthesized click; handle
          // the keys explicitly and cancel the native activation so the
          // toggle runs exactly once in every environment.
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            setOpen((v) => !v)
          }
          if (e.key === 'Escape') {
            setOpen(false)
            triggerRef.current?.focus()
          }
        }}
        onBlur={(e) => {
          // Focus moving inside the popover keeps it open; leaving the whole
          // help closes it.
          if (!wrapRef.current?.contains(e.relatedTarget as Node | null)) setOpen(false)
        }}
      >
        <Info size={12} aria-hidden="true" />
      </button>
      {open && (
        <span role="tooltip" id={popoverId} style={styles.popover}>
          <span style={styles.popoverLabel}>{help.label}</span>
          <span>{help.hint}</span>
          {help.example && <code style={styles.example}>{help.example}</code>}
        </span>
      )}
    </span>
  )
}
