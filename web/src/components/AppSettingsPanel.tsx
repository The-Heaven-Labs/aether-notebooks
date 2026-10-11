import { useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { api } from '../api/client'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useFocusTrap } from '../hooks/useFocusTrap'
import type { Dashboard, DashboardAppSettings } from '../types'

const styles: Record<string, React.CSSProperties> = {
  // The panel covers the top bar (z-index 1550/1600), so it must sit above it:
  // otherwise the top bar hides the header and intercepts clicks on the close button.
  backdrop: {
    position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.3)', zIndex: 1700,
  },
  panel: {
    position: 'fixed', top: 0, right: 0, bottom: 0, width: 420, maxWidth: '100vw',
    background: 'var(--bg-card)', borderLeft: '1px solid var(--border)',
    display: 'flex', flexDirection: 'column', zIndex: 1701,
    boxShadow: 'var(--shadow-lg, -4px 0 16px rgba(0,0,0,0.2))',
  },
  header: {
    display: 'flex', alignItems: 'center', justifyContent: 'space-between',
    padding: '14px 16px', borderBottom: '1px solid var(--border)', flexShrink: 0,
  },
  title: { fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' },
  close: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)', padding: '2px 4px', display: 'flex', alignItems: 'center' },
  body: { padding: 16, display: 'flex', flexDirection: 'column', gap: 14, overflowY: 'auto', flex: 1 },
  field: { display: 'flex', flexDirection: 'column', gap: 4 },
  label: { fontSize: 11, fontWeight: 600, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' },
  input: {
    padding: '7px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13,
    background: 'var(--bg-input)', color: 'var(--text-primary)', width: '100%', boxSizing: 'border-box',
  },
  checkboxRow: { display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, color: 'var(--text-secondary)' },
  muted: { fontSize: 12, color: 'var(--text-muted)', fontStyle: 'italic' },
  saveBtn: {
    padding: '9px 0', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none',
    borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer',
  },
  error: { fontSize: 12, color: 'var(--danger, #d33)' },
  saved: { fontSize: 12, color: 'var(--success, #2a2)' },
}

/**
 * Editor drawer for the App surface configuration (`settings.app`).
 *
 * Toggle semantics: the backend stores `show_header` / `show_refresh` /
 * `show_last_updated` as `*bool` where absent means "shown". The checkboxes
 * render that effective state (absent → checked) and a save always writes
 * explicit booleans, so after one save the tri-state is materialized as a
 * plain on/off choice. Title and subtitle are trimmed and omitted when empty
 * so the stored settings stay clean — the viewer falls back to the dashboard
 * title and hides an absent subtitle.
 *
 * The save merges into the existing app block (logo_attachment_id, layout)
 * and into the dashboard's other settings (variables, public_live, …).
 */
export function AppSettingsPanel({ dashboardId, dashboard, onClose, onSaved }: {
  dashboardId: string
  dashboard: Dashboard
  onClose: () => void
  onSaved: () => void
}) {
  const app = dashboard.settings?.app
  const [title, setTitle] = useState(app?.title ?? '')
  const [subtitle, setSubtitle] = useState(app?.subtitle ?? '')
  const [width, setWidth] = useState<'fixed' | 'full'>(app?.width ?? 'fixed')
  const [showHeader, setShowHeader] = useState(app?.show_header !== false)
  const [showRefresh, setShowRefresh] = useState(app?.show_refresh !== false)
  const [showLastUpdated, setShowLastUpdated] = useState(app?.show_last_updated !== false)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const panelRef = useRef<HTMLDivElement>(null)

  useEscapeToClose(onClose)
  useFocusTrap(panelRef)

  const save = async () => {
    setSaving(true)
    setError(null)
    try {
      // Start from the existing app block so fields this panel doesn't own
      // (logo_attachment_id, layout) survive a save.
      const next: DashboardAppSettings = { ...app }
      const trimmedTitle = title.trim()
      if (trimmedTitle) next.title = trimmedTitle
      else delete next.title
      const trimmedSubtitle = subtitle.trim()
      if (trimmedSubtitle) next.subtitle = trimmedSubtitle
      else delete next.subtitle
      next.width = width
      next.show_header = showHeader
      next.show_refresh = showRefresh
      next.show_last_updated = showLastUpdated
      await api.put(`/api/v1/dashboards/${dashboardId}`, {
        settings: { ...dashboard.settings, app: next },
      })
      setSaved(true)
      onSaved()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save app settings')
    } finally {
      setSaving(false)
    }
  }

  return createPortal(
    <>
      <div data-testid="app-settings-backdrop" style={styles.backdrop} onClick={onClose} aria-hidden="true" />
      <div ref={panelRef} style={styles.panel} role="dialog" aria-modal="true" aria-label="App settings" tabIndex={-1}>
        <div style={styles.header}>
          <span style={styles.title}>App settings</span>
          <button type="button" style={styles.close} onClick={onClose} aria-label="Close app settings">
            <X size={15} />
          </button>
        </div>
        <div style={styles.body}>
          <div style={styles.field}>
            <label style={styles.label} htmlFor="app-title">App title</label>
            <input
              id="app-title"
              aria-label="App title"
              style={styles.input}
              value={title}
              placeholder={dashboard.title}
              onChange={(e) => { setSaved(false); setTitle(e.target.value) }}
            />
          </div>
          <div style={styles.field}>
            <label style={styles.label} htmlFor="app-subtitle">Subtitle</label>
            <input
              id="app-subtitle"
              aria-label="Subtitle"
              style={styles.input}
              value={subtitle}
              placeholder="Sales ops · updated hourly"
              onChange={(e) => { setSaved(false); setSubtitle(e.target.value) }}
            />
          </div>
          <div style={styles.field}>
            <label style={styles.label} htmlFor="app-width">Width</label>
            <select
              id="app-width"
              aria-label="Width"
              style={styles.input}
              value={width}
              onChange={(e) => { setSaved(false); setWidth(e.target.value as 'fixed' | 'full') }}
            >
              <option value="fixed">Fixed</option>
              <option value="full">Full width</option>
            </select>
          </div>
          <div style={styles.checkboxRow}>
            <input
              id="app-show-header"
              aria-label="Show header"
              type="checkbox"
              checked={showHeader}
              onChange={(e) => { setSaved(false); setShowHeader(e.target.checked) }}
            />
            <label htmlFor="app-show-header">Show header</label>
          </div>
          <div style={styles.checkboxRow}>
            <input
              id="app-show-refresh"
              aria-label="Show refresh"
              type="checkbox"
              checked={showRefresh}
              onChange={(e) => { setSaved(false); setShowRefresh(e.target.checked) }}
            />
            <label htmlFor="app-show-refresh">Show refresh</label>
          </div>
          <div style={styles.checkboxRow}>
            <input
              id="app-show-last-updated"
              aria-label="Show last updated"
              type="checkbox"
              checked={showLastUpdated}
              onChange={(e) => { setSaved(false); setShowLastUpdated(e.target.checked) }}
            />
            <label htmlFor="app-show-last-updated">Show last updated</label>
          </div>

          <span style={styles.muted}>Logo and fonts arrive with org branding (next wave).</span>

          {error && <div style={styles.error} role="alert">{error}</div>}
          {saved && <div style={styles.saved}>App settings saved</div>}

          <button type="button" style={styles.saveBtn} disabled={saving} onClick={save}>
            {saving ? 'Saving…' : 'Save app settings'}
          </button>
        </div>
      </div>
    </>,
    document.body,
  )
}
