import { useMemo } from 'react'
import type { CSSProperties } from 'react'
import DOMPurify from 'dompurify'
import { interpolateHtml } from '../utils/interpolateText'
import { useDashboardVariables } from '../contexts/DashboardVariablesContext'
import { useAppTheme } from './AppThemeProvider'
import type { AppColorScheme } from '../utils/appTheme'
import type { DashboardVariable } from '../types'

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
  return [
    '<!doctype html><html><head><meta charset="utf-8">',
    `<meta http-equiv="Content-Security-Policy" content="${csp}">`,
    `<style>:root{${tokenCss}}body{margin:0;background:var(--bg-card,${canvas});color:var(--text-primary,${text});font-family:var(--app-font-family,'DM Sans',system-ui,sans-serif)}</style>`,
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
}

export function HtmlWidget({ html, css, values, variables, enabled = true }: HtmlWidgetProps) {
  const context = useDashboardVariables()
  const theme = useAppTheme()
  const interpolated = useMemo(
    () => interpolateHtml(html, values ?? context.values, variables ?? context.variables),
    [html, values, variables, context.values, context.variables],
  )
  const srcDoc = useMemo(() => {
    if (!enabled || (!interpolated.trim() && !css.trim())) return ''
    return buildPanelSrcDoc({
      html: sanitizePanelHtml(interpolated, window.location.origin),
      css,
      tokens: theme?.vars ?? {},
      scheme: theme?.scheme ?? 'light',
      origin: window.location.origin,
    })
  }, [enabled, interpolated, css, theme])

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
