import { useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { api, getToken } from '../api/client'
import { getApiUrl } from '../config'
import { CHART_PALETTE_PRESETS } from '../charts/palettes'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useFocusTrap } from '../hooks/useFocusTrap'
import type { Dashboard, DashboardAppSettings, DashboardAppTheme, OrgBranding } from '../types'

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
  section: {
    fontSize: 12, fontWeight: 700, color: 'var(--text-primary)',
    borderTop: '1px solid var(--border)', paddingTop: 12, marginTop: 4,
  },
  field: { display: 'flex', flexDirection: 'column', gap: 4 },
  label: { fontSize: 11, fontWeight: 600, color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' },
  input: {
    padding: '7px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13,
    background: 'var(--bg-input)', color: 'var(--text-primary)', width: '100%', boxSizing: 'border-box',
  },
  checkboxRow: { display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, color: 'var(--text-secondary)' },
  colorRow: { display: 'flex', alignItems: 'center', gap: 6 },
  swatch: {
    width: 28, height: 28, padding: 0, border: '1px solid var(--border)',
    borderRadius: 4, background: 'none', cursor: 'pointer', flexShrink: 0,
  },
  resetBtn: {
    background: 'none', border: 'none', padding: 0, color: 'var(--text-muted)',
    fontSize: 11, cursor: 'pointer', textDecoration: 'underline', flexShrink: 0,
  },
  radioRow: { display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' },
  radioOption: { display: 'flex', alignItems: 'center', gap: 5, fontSize: 13, color: 'var(--text-secondary)' },
  uploadBtn: {
    padding: '6px 12px', background: 'var(--bg-input)', color: 'var(--text-primary)',
    border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, cursor: 'pointer',
  },
  logoPreview: { display: 'flex', alignItems: 'center', gap: 10 },
  logoImg: {
    maxWidth: 120, maxHeight: 40, objectFit: 'contain',
    border: '1px solid var(--border)', borderRadius: 4, background: 'var(--bg-input)',
  },
  muted: { fontSize: 12, color: 'var(--text-muted)', fontStyle: 'italic' },
  saveBtn: {
    padding: '9px 0', background: 'var(--button-primary-bg)', color: 'var(--button-primary-text)', border: 'none',
    borderRadius: 4, fontSize: 13, fontWeight: 600, cursor: 'pointer',
  },
  error: { fontSize: 12, color: 'var(--danger, #d33)' },
  saved: { fontSize: 12, color: 'var(--success, #2a2)' },
}

type ColorField = 'accent' | 'background' | 'surface' | 'text' | 'border'
const COLOR_FIELDS: ColorField[] = ['accent', 'background', 'surface', 'text', 'border']
const COLOR_LABELS: Record<ColorField, string> = {
  accent: 'Accent', background: 'Background', surface: 'Surface', text: 'Text', border: 'Border',
}

type PalettePreset = 'default' | 'aether' | 'amber' | 'teal' | 'custom'
type LogoMode = 'none' | 'org' | 'upload'

/** The app block as saved: `theme: null` clears a stored theme. */
type DraftApp = Omit<DashboardAppSettings, 'theme'> & { theme?: DashboardAppTheme | null }

const HEX_RE = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i

function isHexColor(value: string): boolean {
  return HEX_RE.test(value)
}

/** `<input type="color">` only accepts 7-char hex; anything else shows black. */
function swatchValue(value: string): string {
  return /^#[0-9a-f]{6}$/i.test(value) ? value : '#000000'
}

/** The preset whose colors match `palette`; unmatched palettes are Custom. */
function presetFor(palette: string[] | undefined): PalettePreset {
  if (!palette?.length) return 'default'
  const match = CHART_PALETTE_PRESETS.find(
    (p) => p.colors && p.colors.length === palette.length && p.colors.every((c, i) => c === palette[i]),
  )
  return (match?.value as PalettePreset) ?? 'custom'
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
 * Theme semantics: only non-empty fields are sent, so a cleared field is
 * dropped rather than saved as `''`; clearing every field of a stored theme
 * sends `theme: null` to clear it. The logo control is dashboard-local: "None"
 * and "Use org logo" both drop `logo_attachment_id` (the surface falls back to
 * the org logo when one exists) while "Upload…" stores the uploaded
 * attachment id.
 *
 * The save merges into the existing app block (logo_attachment_id, layout)
 * and into the dashboard's other settings (variables, public_live, …).
 */
export function AppSettingsPanel({ dashboardId, dashboard, onClose, onSaved }: {
  dashboardId: string
  dashboard: Dashboard & { org_branding?: OrgBranding }
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
  const [colors, setColors] = useState<Record<ColorField, string>>({
    accent: app?.theme?.accent ?? '',
    background: app?.theme?.background ?? '',
    surface: app?.theme?.surface ?? '',
    text: app?.theme?.text ?? '',
    border: app?.theme?.border ?? '',
  })
  const [radius, setRadius] = useState(app?.theme?.radius != null ? String(app.theme.radius) : '')
  const [fontFamily, setFontFamily] = useState<NonNullable<DashboardAppTheme['font_family']> | ''>(app?.theme?.font_family ?? '')
  const [colorScheme, setColorScheme] = useState<NonNullable<DashboardAppTheme['color_scheme']>>(app?.theme?.color_scheme ?? 'auto')
  const initialPreset = presetFor(app?.theme?.chart_palette)
  const [palettePreset, setPalettePreset] = useState<PalettePreset>(initialPreset)
  const [customPalette, setCustomPalette] = useState(
    initialPreset === 'custom' ? (app?.theme?.chart_palette ?? []).join(', ') : '',
  )
  const [logoMode, setLogoMode] = useState<LogoMode>(
    app?.logo_attachment_id ? 'upload' : dashboard.org_branding?.logo_attachment_id ? 'org' : 'none',
  )
  const [logoId, setLogoId] = useState<string | undefined>(app?.logo_attachment_id)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  useEscapeToClose(onClose)
  useFocusTrap(panelRef)

  const setColor = (field: ColorField, value: string) => {
    setSaved(false)
    setColors((prev) => ({ ...prev, [field]: value }))
  }

  /** Builds the theme payload; validation errors abort the save. */
  const draftTheme = (): { theme: DashboardAppTheme | null } | { error: string } => {
    const theme: DashboardAppTheme = {}
    for (const field of COLOR_FIELDS) {
      const value = colors[field].trim()
      if (!value) continue
      if (!isHexColor(value)) return { error: `${COLOR_LABELS[field]} must be a hex color like #6366f1` }
      theme[field] = value
    }
    const trimmedRadius = radius.trim()
    if (trimmedRadius) {
      const n = Number(trimmedRadius)
      if (!Number.isInteger(n) || n < 0 || n > 24) {
        return { error: 'Corner radius must be a whole number between 0 and 24' }
      }
      theme.radius = n
    }
    if (fontFamily) theme.font_family = fontFamily
    if (colorScheme !== 'auto') theme.color_scheme = colorScheme
    if (palettePreset === 'custom') {
      const entries = customPalette.split(',').map((entry) => entry.trim()).filter(Boolean)
      const invalid = entries.find((entry) => !isHexColor(entry))
      if (invalid) return { error: `Chart color "${invalid}" must be a hex color like #6366f1` }
      if (entries.length) theme.chart_palette = entries
    } else {
      const presetColors = CHART_PALETTE_PRESETS.find((p) => p.value === palettePreset)?.colors
      if (presetColors) theme.chart_palette = [...presetColors]
    }
    return { theme: Object.keys(theme).length > 0 ? theme : null }
  }

  const save = async () => {
    setError(null)
    const built = draftTheme()
    if ('error' in built) {
      setError(built.error)
      return
    }
    setSaving(true)
    try {
      // Start from the existing app block so fields this panel doesn't own
      // (layout) survive a save.
      const next: DraftApp = { ...app }
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
      if (logoMode === 'upload' && logoId) next.logo_attachment_id = logoId
      else delete next.logo_attachment_id
      if (built.theme) next.theme = built.theme
      else if (app?.theme) next.theme = null
      else delete next.theme
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

  const uploadLogo = async (file: File) => {
    setError(null)
    setUploading(true)
    try {
      const form = new FormData()
      form.append('file', file)
      const token = getToken()
      const res = await fetch(`${getApiUrl()}/api/v1/dashboards/${dashboardId}/attachments`, {
        method: 'POST',
        headers: token ? { Authorization: `Bearer ${token}` } : {},
        body: form,
      })
      if (!res.ok) throw new Error(`Logo upload failed (${res.status})`)
      const attachment = (await res.json()) as { id: string }
      setLogoId(attachment.id)
      setSaved(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Logo upload failed')
    } finally {
      setUploading(false)
    }
  }

  // The dashboard logo wins; an absent one falls back to the org logo (the
  // same resolution the app surface uses). The authed attachment route needs
  // credentials an <img> cannot send as a header, so the session JWT rides the
  // query string — same construction as the viewer's masthead logo.
  const sessionToken = getToken()
  const dashboardLogoId = logoMode === 'upload' ? logoId : undefined
  const effectiveLogoId = dashboardLogoId || dashboard.org_branding?.logo_attachment_id
  const logoUrl = effectiveLogoId
    ? `${getApiUrl()}/api/v1/attachments/${effectiveLogoId}${sessionToken ? `?token=${encodeURIComponent(sessionToken)}` : ''}`
    : undefined
  const logoSource = dashboardLogoId ? 'Dashboard logo' : effectiveLogoId ? 'Org logo (fallback)' : 'No logo'

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

          <div style={styles.section}>Theme</div>
          {COLOR_FIELDS.map((field) => (
            <div key={field} style={styles.field}>
              <label style={styles.label} htmlFor={`app-theme-${field}`}>{COLOR_LABELS[field]}</label>
              <div style={styles.colorRow}>
                <input
                  type="color"
                  aria-label={`${COLOR_LABELS[field]} picker`}
                  style={styles.swatch}
                  value={swatchValue(colors[field])}
                  onChange={(e) => setColor(field, e.target.value)}
                />
                <input
                  id={`app-theme-${field}`}
                  aria-label={COLOR_LABELS[field]}
                  style={{ ...styles.input, flex: 1 }}
                  value={colors[field]}
                  placeholder="#6366f1"
                  onChange={(e) => setColor(field, e.target.value)}
                />
                <button
                  type="button"
                  style={styles.resetBtn}
                  aria-label={`Reset ${field}`}
                  onClick={() => setColor(field, '')}
                >
                  Reset
                </button>
              </div>
            </div>
          ))}
          <div style={styles.field}>
            <label style={styles.label} htmlFor="app-theme-radius">Corner radius</label>
            <input
              id="app-theme-radius"
              aria-label="Corner radius"
              type="number"
              min={0}
              max={24}
              step={1}
              style={styles.input}
              value={radius}
              placeholder="8"
              onChange={(e) => { setSaved(false); setRadius(e.target.value) }}
            />
          </div>
          <div style={styles.field}>
            <label style={styles.label} htmlFor="app-theme-font">Font family</label>
            <select
              id="app-theme-font"
              aria-label="Font family"
              style={styles.input}
              value={fontFamily}
              onChange={(e) => { setSaved(false); setFontFamily(e.target.value as NonNullable<DashboardAppTheme['font_family']> | '') }}
            >
              <option value="">Default</option>
              <option value="dm-sans">DM Sans</option>
              <option value="jetbrains-mono">JetBrains Mono</option>
              <option value="system">System</option>
            </select>
          </div>
          <div style={styles.field}>
            <label style={styles.label} htmlFor="app-theme-scheme">Color scheme</label>
            <select
              id="app-theme-scheme"
              aria-label="Color scheme"
              style={styles.input}
              value={colorScheme}
              onChange={(e) => { setSaved(false); setColorScheme(e.target.value as NonNullable<DashboardAppTheme['color_scheme']>) }}
            >
              <option value="auto">Auto</option>
              <option value="light">Light</option>
              <option value="dark">Dark</option>
            </select>
          </div>
          <div style={styles.field}>
            <label style={styles.label} htmlFor="app-theme-palette">Chart palette</label>
            <select
              id="app-theme-palette"
              aria-label="Chart palette"
              style={styles.input}
              value={palettePreset}
              onChange={(e) => { setSaved(false); setPalettePreset(e.target.value as PalettePreset) }}
            >
              {CHART_PALETTE_PRESETS.map((preset) => (
                <option key={preset.value} value={preset.value}>{preset.label}</option>
              ))}
            </select>
          </div>
          {palettePreset === 'custom' && (
            <div style={styles.field}>
              <label style={styles.label} htmlFor="app-theme-custom-palette">Custom chart colors</label>
              <input
                id="app-theme-custom-palette"
                aria-label="Custom chart colors"
                style={styles.input}
                value={customPalette}
                placeholder="#6366f1, #06b6d4, #10b981"
                onChange={(e) => { setSaved(false); setCustomPalette(e.target.value) }}
              />
            </div>
          )}

          <div style={styles.section}>Logo</div>
          <div role="radiogroup" aria-label="Logo source" style={styles.radioRow}>
            {([['none', 'None'], ['org', 'Use org logo'], ['upload', 'Upload…']] as const).map(([value, label]) => (
              <label key={value} style={styles.radioOption}>
                <input
                  type="radio"
                  name="app-logo-mode"
                  value={value}
                  checked={logoMode === value}
                  aria-label={label}
                  onChange={() => {
                    setSaved(false)
                    setLogoMode(value)
                    // Leaving "Upload…" drops the draft dashboard logo: the
                    // fallback (or none) is what the surface will render.
                    if (value !== 'upload') setLogoId(undefined)
                  }}
                />
                {label}
              </label>
            ))}
          </div>
          {logoMode === 'upload' && (
            <div style={styles.colorRow}>
              <button
                type="button"
                style={styles.uploadBtn}
                disabled={uploading}
                onClick={() => fileInputRef.current?.click()}
              >
                {uploading ? 'Uploading…' : 'Choose file…'}
              </button>
              <input
                ref={fileInputRef}
                id="app-logo-file"
                aria-label="Logo file"
                type="file"
                accept="image/*"
                style={{ display: 'none' }}
                onChange={(e) => {
                  const file = e.target.files?.[0]
                  e.target.value = ''
                  if (file) void uploadLogo(file)
                }}
              />
            </div>
          )}
          {logoUrl ? (
            <div style={styles.logoPreview}>
              <img src={logoUrl} alt="Logo preview" style={styles.logoImg} />
              <span style={styles.muted}>{logoSource}</span>
            </div>
          ) : (
            <span style={styles.muted}>No logo</span>
          )}
          <span style={styles.muted}>Without a dashboard logo, the org logo is used when one is configured.</span>

          {error && <div style={styles.error} role="alert">{error}</div>}
          {saved && <div style={styles.saved}>App settings saved</div>}

          <button type="button" style={styles.saveBtn} disabled={saving || uploading} onClick={save}>
            {saving ? 'Saving…' : 'Save app settings'}
          </button>
        </div>
      </div>
    </>,
    document.body,
  )
}
