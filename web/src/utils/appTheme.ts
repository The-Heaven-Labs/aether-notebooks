import type { DashboardAppTheme, OrgBranding } from '../types'

/**
 * App-surface theme resolution: turns a dashboard's `settings.app.theme`
 * (with org branding as the fallback layer) into the scoped CSS custom
 * properties the `.app-theme` container consumes.
 *
 * Pure on purpose — no DOM access — so the caller (AppThemeProvider) supplies
 * the current global scheme and applies the result.
 */
export type AppColorScheme = 'light' | 'dark'

export interface ResolvedAppTheme {
  /** The scheme the app surface renders in. */
  scheme: AppColorScheme
  /** CSS custom properties to set on the `.app-theme` container. */
  vars: Record<string, string>
  /** Chart palette override; absent when neither source provides valid colors. */
  chartPalette?: string[]
}

/** WCAG 2.1 AA minimums: normal text vs. background, and UI/large text vs. background. */
const TEXT_CONTRAST_MIN = 4.5
const ACCENT_CONTRAST_MIN = 3

/**
 * The scheme's default canvas / card / text values (theme.css `--bg-primary`,
 * `--bg-card`, `--text-primary`), used as the contrast baseline whenever the
 * theme does not override the corresponding token.
 */
const DEFAULT_CANVAS: Record<AppColorScheme, string> = {
  light: '#f5f5f5',
  dark: '#141414',
}

const DEFAULT_CARD: Record<AppColorScheme, string> = {
  light: '#ffffff',
  dark: '#1c1c1c',
}

const DEFAULT_TEXT: Record<AppColorScheme, string> = {
  light: '#111',
  dark: '#e8e8e8',
}

/** The curated, self-hosted font choices. No third-party font requests. */
const FONT_STACKS: Record<NonNullable<DashboardAppTheme['font_family']>, string> = {
  'dm-sans': "'DM Sans', system-ui, sans-serif",
  'jetbrains-mono': "'JetBrains Mono', ui-monospace, monospace",
  system: 'system-ui, -apple-system, sans-serif',
}

/**
 * Parses a `#rgb` or `#rrggbb` color. Returns null for anything else, so
 * callers can filter invalid overrides instead of emitting broken CSS vars.
 */
export function hexToRgb(hex: string): [number, number, number] | null {
  const match = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim())
  if (!match) return null
  const digits = match[1]
  const channel = (i: number) =>
    parseInt(digits.length === 3 ? digits[i] + digits[i] : digits.slice(i * 2, i * 2 + 2), 16)
  return [channel(0), channel(1), channel(2)]
}

function channelLuminance(value: number): number {
  const c = value / 255
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4)
}

function relativeLuminance([r, g, b]: [number, number, number]): number {
  return 0.2126 * channelLuminance(r) + 0.7152 * channelLuminance(g) + 0.0722 * channelLuminance(b)
}

/**
 * WCAG 2.1 contrast ratio between two colors (1–21, order-independent).
 * Returns null when either color cannot be parsed.
 */
export function contrastRatio(fg: string, bg: string): number | null {
  const fgRgb = hexToRgb(fg)
  const bgRgb = hexToRgb(bg)
  if (!fgRgb || !bgRgb) return null
  const fgLum = relativeLuminance(fgRgb)
  const bgLum = relativeLuminance(bgRgb)
  return (Math.max(fgLum, bgLum) + 0.05) / (Math.min(fgLum, bgLum) + 0.05)
}

function validHex(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return hexToRgb(trimmed) !== null ? trimmed : undefined
}

/** Whether `fg` clears `min` against `bg`. Unparseable pairs count as failures. */
function readable(fg: string, bg: string, min: number): boolean {
  return (contrastRatio(fg, bg) ?? 0) >= min
}

/**
 * The first candidate that is a hex color and clears `min` against the
 * effective background. Failures fall back independently: a dashboard token
 * that fails contrast drops to the next layer (branding, then the stylesheet
 * default) without disturbing the tokens that passed.
 */
function firstContrasting(candidates: Array<string | undefined>, background: string, min: number): string | undefined {
  for (const candidate of candidates) {
    const hex = validHex(candidate)
    if (hex && readable(hex, background, min)) return hex
  }
  return undefined
}

/** Filters a palette down to its hex entries (trimmed); absent/empty results mean "no override". */
function pickPalette(palette: string[] | undefined): string[] | undefined {
  if (!palette) return undefined
  const valid = palette.map(validHex).filter((c): c is string => c !== undefined)
  return valid.length > 0 ? valid : undefined
}

/**
 * Resolves the app surface's theme.
 *
 * Precedence: dashboard tokens > org branding > the stylesheet defaults.
 * Guardrails validate the *effective* pairs, not just the overrides: text
 * (override or scheme default) must clear 4.5:1 against both the canvas and
 * the card it can sit on, and accent must clear 3:1 against the final canvas.
 * Failing tokens fall back independently — a text override that can't be read
 * drops to the scheme default, and a background/surface override the default
 * text can't be read on reverts to the scheme default.
 */
export function resolveAppTheme(
  theme: DashboardAppTheme | null | undefined,
  branding: OrgBranding | null | undefined,
  globalScheme: AppColorScheme,
): ResolvedAppTheme {
  const scheme: AppColorScheme =
    theme?.color_scheme === 'light' || theme?.color_scheme === 'dark' ? theme.color_scheme : globalScheme

  const vars: Record<string, string> = {}

  // background maps to the page canvas (`--bg-primary`); surface maps to the
  // card background (`--bg-card`), matching what Wave 1's app surface uses.
  // `border` deliberately targets only `--border-light` (the card/app-surface
  // hairline token), not the heavier `--border` used by editor controls.
  let background = validHex(theme?.background)
  let surface = validHex(theme?.surface)
  const border = validHex(theme?.border)

  // A text override must be readable on every surface it can sit on: the
  // effective canvas and the effective card (override when it applies, the
  // scheme default otherwise).
  const textOverride = validHex(theme?.text)
  const text =
    textOverride !== undefined &&
    readable(textOverride, background ?? DEFAULT_CANVAS[scheme], TEXT_CONTRAST_MIN) &&
    readable(textOverride, surface ?? DEFAULT_CARD[scheme], TEXT_CONTRAST_MIN)
      ? textOverride
      : undefined

  // No text override survives: the scheme-default text is what renders, so a
  // background/surface override that makes it unreadable falls back too.
  if (!text) {
    const defaultText = DEFAULT_TEXT[scheme]
    if (background && !readable(defaultText, background, TEXT_CONTRAST_MIN)) background = undefined
    if (surface && !readable(defaultText, surface, TEXT_CONTRAST_MIN)) surface = undefined
  }

  if (background) vars['--bg-primary'] = background
  if (surface) vars['--bg-card'] = surface
  if (border) vars['--border-light'] = border
  if (text) vars['--text-primary'] = text

  // Accent is validated against the final canvas, after any reversion above.
  const accent = firstContrasting(
    [theme?.accent, branding?.accent_color],
    background ?? DEFAULT_CANVAS[scheme],
    ACCENT_CONTRAST_MIN,
  )
  if (accent) vars['--accent'] = accent

  const radius = theme?.radius
  if (typeof radius === 'number' && Number.isFinite(radius) && radius >= 0) {
    vars['--app-radius'] = `${radius}px`
  }
  const stack = theme?.font_family ? FONT_STACKS[theme.font_family] : undefined
  if (stack) vars['--app-font'] = stack

  const chartPalette = pickPalette(theme?.chart_palette) ?? pickPalette(branding?.default_chart_palette)

  return chartPalette ? { scheme, vars, chartPalette } : { scheme, vars }
}
