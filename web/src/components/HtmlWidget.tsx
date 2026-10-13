import { useMemo } from 'react'
import type { CSSProperties } from 'react'
import DOMPurify from 'dompurify'
import { interpolateHtml } from '../utils/interpolateText'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { useResolvedAppTheme } from './AppThemeProvider'
import type { AppColorScheme } from '../utils/appTheme'
import type { DashboardAppTheme, DashboardVariable, OrgBranding } from '../types'

// The strict allowlist. Deliberately absent: a, form, input, button, script,
// style, iframe, object, embed, link, meta, base, svg (svg can carry scripts).
const ALLOWED_TAGS = [
  'div', 'span', 'p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'dl', 'dt', 'dd',
  'table', 'thead', 'tbody', 'tfoot', 'tr', 'th', 'td', 'caption',
  'img', 'strong', 'b', 'em', 'i', 'u', 's', 'small', 'code', 'pre', 'br', 'hr',
  'blockquote', 'figure', 'figcaption', 'sub', 'sup', 'section', 'header', 'footer',
  'main', 'aside', 'article', 'nav', 'mark', 'kbd', 'samp', 'var', 'abbr', 'time',
]
const ALLOWED_ATTR = [
  'class', 'style', 'title', 'alt', 'src', 'width', 'height', 'colspan', 'rowspan',
  'align', 'valign', 'dir', 'lang', 'scope', 'headers', 'datetime', 'cite', 'start', 'reversed',
]

function uriRegexp(origin: string): RegExp {
  const escaped = origin.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  // data: images, absolute same-origin URLs, or origin-relative paths. The
  // (?!\/) guard blocks protocol-relative URLs (//evil.example/x.png).
  return new RegExp(`^(?:data:image\\/(?:png|jpe?g|gif|webp|svg\\+xml);|${escaped}\\/|\\/(?!\\/))`)
}

/** Sanitizes panel markup; exported for tests. */
export function sanitizePanelHtml(html: string, origin: string): string {
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS,
    ALLOWED_ATTR,
    ALLOWED_URI_REGEXP: uriRegexp(origin),
  })
}

/** Neutralizes the only sequence that can close the injected style element. */
function safeCss(css: string): string {
  return css.replace(/<\/(style)/gi, '<\\/$1')
}

/** Composes the frame document; exported for tests. */
export function buildPanelSrcDoc({ html, css, tokens, scheme, origin }: {
  html: string
  css: string
  tokens: Record<string, string>
  scheme: AppColorScheme
  origin: string
}): string {
  const csp = [
    "default-src 'none'",
    `img-src data: ${origin}`,
    "style-src 'unsafe-inline'",
    `font-src data: ${origin}`,
    "form-action 'none'",
    "base-uri 'none'",
  ].join('; ')
  const tokenCss = Object.entries(tokens).map(([k, v]) => `${k}:${v}`).join(';')
  const canvas = scheme === 'dark' ? '#141414' : '#ffffff'
  const text = scheme === 'dark' ? '#e8e8e8' : '#111111'
  // The frame is its own document, so it inherits none of the app's global
  // scrollbar styling: re-state it here, themed off the panel's text color
  // (guaranteed readable on the canvas by the app-theme guardrails) with a
  // plain gray as the pre-color-mix fallback. `color-scheme` also switches
  // native chrome (scrollbar base, resize corner) with the panel scheme.
  const scrollbars = [
    "*{scrollbar-width:thin;scrollbar-color:#8888 transparent;scrollbar-color:color-mix(in srgb,var(--text-primary,#888) 25%,transparent) transparent}",
    "*::-webkit-scrollbar{width:7px;height:7px}",
    "*::-webkit-scrollbar-track{background:transparent}",
    "*::-webkit-scrollbar-thumb{background:#8888;background:color-mix(in srgb,var(--text-primary,#888) 25%,transparent);border-radius:4px}",
    "*::-webkit-scrollbar-thumb:hover{background:#888a;background:color-mix(in srgb,var(--text-primary,#888) 45%,transparent)}",
  ].join('')
  return [
    '<!doctype html><html><head><meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp}">`,
    `<style>:root{color-scheme:${scheme};${tokenCss}}body{margin:0;background:var(--bg-card,${canvas});color:var(--text-primary,${text});font-family:var(--app-font-family,'DM Sans',system-ui,sans-serif)}${scrollbars}</style>`,
    `<style>${safeCss(css)}</style>`,
    `</head><body>${html}</body></html>`,
  ].join('')
}

interface HtmlWidgetProps {
  html: string
  css: string
  /** Overrides the dashboard variables context; used by tests and the public page. */
  values?: Record<string, unknown>
  variables?: DashboardVariable[]
  /** False when the org switch is off — renders a placeholder instead. */
  enabled?: boolean
  /**
   * Preview fallbacks used when no `AppThemeProvider` wraps the widget (the
   * editor grid and the drawer): the dashboard's `settings.app.theme` /
   * `org_branding`, resolved exactly like the app surface so the preview
   * matches what viewers get without switching modes. Inside a provider the
   * context wins and these are ignored.
   */
  theme?: DashboardAppTheme | null
  branding?: OrgBranding | null
}

export function HtmlWidget({ html, css, values, variables, enabled = true, theme, branding }: HtmlWidgetProps) {
  const context = useDashboardVariables()
  const resolved = useResolvedAppTheme(theme, branding)
  const interpolated = useMemo(
    () => interpolateHtml(html, values ?? context.values, variables ?? context.variables),
    [html, values, variables, context.values, context.variables],
  )
  const srcDoc = useMemo(() => {
    if (!enabled || (!interpolated.trim() && !css.trim())) return ''
    return buildPanelSrcDoc({
      html: sanitizePanelHtml(interpolated, window.location.origin),
      css,
      tokens: resolved.vars,
      scheme: resolved.scheme,
      origin: window.location.origin,
    })
  }, [enabled, interpolated, css, resolved])

  if (!enabled) return <div style={styles.empty}>HTML panels are disabled for this organization.</div>
  if (!srcDoc) return <div style={styles.empty}>Empty HTML panel — edit it in the dashboard editor.</div>
  return (
    <iframe
      title="HTML panel"
      sandbox=""
      referrerPolicy="no-referrer"
      srcDoc={srcDoc}
      style={{ width: '100%', height: '100%', border: 'none', display: 'block' }}
    />
  )
}

const styles: Record<string, CSSProperties> = {
  empty: { padding: 16, fontSize: 13, color: 'var(--text-muted)', fontStyle: 'italic' },
}
