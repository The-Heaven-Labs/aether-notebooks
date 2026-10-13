import { useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { ArrowDown, ArrowUp, X } from 'lucide-react'
import { api, getToken } from '../api/client'
import { getApiUrl } from '../config'
import { CHART_PALETTE_PRESETS, MAX_PALETTE_COLORS } from '../charts/palettes'
import type { ChartPalettePresetValue } from '../charts/palettes'
import { useEscapeToClose } from '../hooks/useEscapeToClose'
import { useFocusTrap } from '../hooks/useFocusTrap'
import type { Dashboard, DashboardAppSettings, DashboardAppTheme, DashboardTab, OrgBranding, Widget } from '../types'

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
  tabRow: { display: 'flex', alignItems: 'center', gap: 6 },
  iconBtn: {
    display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
    width: 26, height: 26, padding: 0, flexShrink: 0,
    background: 'var(--bg-input)', color: 'var(--text-secondary)',
    border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer',
  },
  addTabBtn: {
    alignSelf: 'flex-start', padding: '6px 12px',
    background: 'var(--bg-input)', color: 'var(--text-primary)',
    border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, cursor: 'pointer',
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

type LogoMode = 'none' | 'org' | 'upload'

/** The app block as saved: `theme: null` clears a stored theme. */
type DraftApp = Omit<DashboardAppSettings, 'theme'> & { theme?: DashboardAppTheme | null }

const HEX_RE = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i

function isHexColor(value: string): boolean {
  return HEX_RE.test(value)
}

/** `<input type="color">` only accepts 7-char hex; `#rgb` expands, anything else shows black. */
function swatchValue(value: string): string {
  const trimmed = value.trim()
  if (/^#[0-9a-f]{6}$/i.test(trimmed)) return trimmed.toLowerCase()
  const short = /^#([0-9a-f])([0-9a-f])([0-9a-f])$/i.exec(trimmed)
  if (short) return `#${short[1]}${short[1]}${short[2]}${short[2]}${short[3]}${short[3]}`.toLowerCase()
  return '#000000'
}

/** The preset whose colors match `palette`; unmatched palettes are Custom. */
function presetFor(palette: string[] | undefined): ChartPalettePresetValue {
  if (!palette?.length) return 'default'
  const match = CHART_PALETTE_PRESETS.find(
    (p) => 'colors' in p && p.colors.length === palette.length && p.colors.every((c, i) => c === palette[i]),
  )
  return match?.value ?? 'custom'
}

/** Server-side bound on `settings.tabs` (validateDashboardTabs). */
const MAX_DASHBOARD_TABS = 10

/** Tab ids mirror the server's `^[a-z0-9][a-z0-9-]*$` charset and 64-char cap. */
function slugifyTabTitle(title: string): string {
  const slug = title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
  return slug || 'tab'
}

/** `slug`, `slug-2`, … until the id is free; trimmed so the 64-char cap holds. */
function uniqueTabId(slug: string, taken: string[]): string {
  const existing = new Set(taken)
  if (!existing.has(slug)) return slug
  let n = 1
  let id = slug
  while (existing.has(id)) {
    n++
    const suffix = `-${n}`
    id = `${slug.slice(0, 64 - suffix.length)}${suffix}`
  }
  return id
}

/**
 * Clears a removed tab's placement on its widgets after the settings save
 * that dropped the tab succeeds. A live dashboard document takes the write
 * directly (one Yjs transaction per widget); without one the same write goes
 * over REST. Failures are non-fatal: a widget whose tab no longer exists
 * falls back to the first tab anyway.
 */
async function clearWidgetTabs(dashboardId: string, affected: Widget[]): Promise<void> {
  if (affected.length === 0) return
  try {
    const mod = await import('./dashboardCollabRuntime')
    const entry = mod.peekDashboardCollab(dashboardId)
    if (entry) {
      for (const widget of affected) mod.setWidgetTab(entry, widget.id, '')
      return
    }
  } catch { /* fall through to REST */ }
  await Promise.all(affected.map((widget) =>
    api.put(`/api/v1/dashboards/${dashboardId}/widgets/${widget.id}`, {
      layout: { ...widget.layout, tab: '' },
    }).catch(() => { /* the tab is removed either way; the widget falls back */ }),
  ))
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
 * sends `theme: null` to clear it. Custom chart palettes are capped at
 * `MAX_PALETTE_COLORS` client-side, mirroring the server's validator. The logo
 * control is dashboard-local: "None" and "Use org logo" both drop
 * `logo_attachment_id` (the surface falls back to the org logo when one exists)
 * while "Upload…" stores the uploaded attachment id. The logo radios are
 * disabled while an upload is in flight so a resolving request can never
 * resurrect a logo the user just cleared.
 *
 * The save writes only the keys this panel owns — the app block
 * (title/subtitle/width/toggles/theme/logo_attachment_id/layout) and
 * `tabs`; the server shallow-merges settings, so unrelated keys
 * (variables, public_live, …) keep their stored values.
 *
 * Tabs (`settings.tabs`) are edited as local draft rows — title, order and
 * membership commit with the same Save button as everything else. Removing a
 * row is draft-only: placements for dropped tabs are cleared after the save
 * that actually drops them succeeds (see clearWidgetTabs), so closing without
 * saving leaves the dashboard untouched. New tab ids are slugged from the
 * default title (`tab-1`, …) and stay stable across renames: widgets
 * reference the id, not the title.
 */
export function AppSettingsPanel({ dashboardId, dashboard, widgets = [], initialSection, onClose, onSaved }: {
  dashboardId: string
  dashboard: Dashboard & { org_branding?: OrgBranding }
  /** The dashboard's live widgets; used to clear a deleted tab's placements. */
  widgets?: Widget[]
  /** Opens with the Tabs section revealed and focused (the editor strip's +). */
  initialSection?: 'tabs'
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
  const [palettePreset, setPalettePreset] = useState<ChartPalettePresetValue>(initialPreset)
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
  const [tabs, setTabs] = useState<DashboardTab[]>(() => dashboard.settings?.tabs ?? [])
  const fileInputRef = useRef<HTMLInputElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const tabsFocusRef = useRef<HTMLInputElement>(null)
  const addTabBtnRef = useRef<HTMLButtonElement>(null)

  useEscapeToClose(onClose)
  // The editor strip's + opens this panel to add/arrange chapters: the focus
  // trap lands on the Tabs section's first control instead of the panel.
  useFocusTrap(
    panelRef,
    true,
    initialSection === 'tabs' ? (tabs.length > 0 ? tabsFocusRef : addTabBtnRef) : undefined,
  )

  const addTab = () => {
    if (tabs.length >= MAX_DASHBOARD_TABS) return
    setSaved(false)
    const titles = new Set(tabs.map((t) => t.title))
    let n = 1
    while (titles.has(`Tab ${n}`)) n++
    const title = `Tab ${n}`
    const id = uniqueTabId(slugifyTabTitle(title), tabs.map((t) => t.id))
    setTabs((prev) => [...prev, { id, title }])
  }

  const renameTab = (index: number, title: string) => {
    setSaved(false)
    setTabs((prev) => prev.map((tab, i) => (i === index ? { ...tab, title } : tab)))
  }

  const moveTab = (index: number, delta: -1 | 1) => {
    setSaved(false)
    setTabs((prev) => {
      const target = index + delta
      if (target < 0 || target >= prev.length) return prev
      const next = [...prev]
      ;[next[index], next[target]] = [next[target], next[index]]
      return next
    })
  }

  const removeTab = (index: number) => {
    if (!tabs[index]) return
    setSaved(false)
    setTabs((prev) => prev.filter((_, i) => i !== index))
  }

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
      if (entries.length > MAX_PALETTE_COLORS) {
        return { error: `Chart palettes support at most ${MAX_PALETTE_COLORS} colors` }
      }
      if (entries.length) theme.chart_palette = entries
    } else {
      const preset = CHART_PALETTE_PRESETS.find((p) => p.value === palettePreset)
      if (preset && 'colors' in preset) theme.chart_palette = [...preset.colors]
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
    // Tabs present in the stored settings but absent from the draft are the
    // ones this save drops; their widgets' placements are cleared only once
    // the save succeeds, so closing without saving changes nothing.
    const droppedTabs = (dashboard.settings?.tabs ?? [])
      .filter((stored) => !tabs.some((tab) => tab.id === stored.id))
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
        // Only the keys this panel owns are sent: the server merges settings
        // shallowly, so anything omitted keeps its stored value. `tabs` must
        // always ride along for chapter removals to apply.
        settings: { app: next, tabs },
      })
      setSaved(true)
      onSaved()
      if (droppedTabs.length > 0) {
        void clearWidgetTabs(dashboardId, widgets.filter((w) =>
          w.layout?.tab != null && droppedTabs.some((tab) => tab.id === w.layout.tab)))
      }
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
      const attachment = await api.upload<{ id: string }>(
        `/api/v1/dashboards/${dashboardId}/attachments`,
        form,
      )
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

          <div style={styles.section} role="heading" aria-level={3}>Tabs</div>
          {tabs.map((tab, index) => (
            <div key={tab.id} style={styles.tabRow}>
              <input
                ref={index === 0 ? tabsFocusRef : undefined}
                aria-label={`Tab ${index + 1} title`}
                style={styles.input}
                maxLength={60}
                value={tab.title}
                placeholder={`Tab ${index + 1}`}
                onChange={(e) => renameTab(index, e.target.value)}
              />
              <button
                type="button"
                style={styles.iconBtn}
                aria-label={`Move ${tab.title} up`}
                disabled={index === 0}
                onClick={() => moveTab(index, -1)}
              >
                <ArrowUp size={13} />
              </button>
              <button
                type="button"
                style={styles.iconBtn}
                aria-label={`Move ${tab.title} down`}
                disabled={index === tabs.length - 1}
                onClick={() => moveTab(index, 1)}
              >
                <ArrowDown size={13} />
              </button>
              <button
                type="button"
                style={styles.iconBtn}
                aria-label={`Remove ${tab.title}`}
                onClick={() => removeTab(index)}
              >
                <X size={13} />
              </button>
            </div>
          ))}
          <button
            ref={addTabBtnRef}
            type="button"
            style={{ ...styles.addTabBtn, ...(tabs.length >= MAX_DASHBOARD_TABS ? { opacity: 0.5, cursor: 'not-allowed' } : {}) }}
            disabled={tabs.length >= MAX_DASHBOARD_TABS}
            title={tabs.length >= MAX_DASHBOARD_TABS ? `At most ${MAX_DASHBOARD_TABS} tabs` : 'Add a tab'}
            onClick={addTab}
          >
            Add tab
          </button>
          {tabs.length === 0 && (
            <span style={styles.muted}>
              Tabs chapter the app surface: assign widgets from a widget's Placement
              section, or drag a widget onto a tab chip in the editor.
            </span>
          )}

          <div style={styles.section} role="heading" aria-level={3}>Theme</div>
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
              onChange={(e) => { setSaved(false); setPalettePreset(e.target.value as ChartPalettePresetValue) }}
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

          <div style={styles.section} role="heading" aria-level={3}>Logo</div>
          {/* Disabled while an upload is in flight: switching modes mid-upload
              would let the resolving request resurrect a logo the user just
              cleared — the radio is the only path that drops the draft id. */}
          <div role="radiogroup" aria-label="Logo source" style={styles.radioRow}>
            {([['none', 'None'], ['org', 'Use org logo'], ['upload', 'Upload…']] as const).map(([value, label]) => (
              <label key={value} style={styles.radioOption}>
                <input
                  type="radio"
                  name="app-logo-mode"
                  value={value}
                  checked={logoMode === value}
                  aria-label={label}
                  disabled={uploading}
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
