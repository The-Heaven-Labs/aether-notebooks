/**
 * Widget appearance vocabulary stored in `widget.config.appearance`.
 *
 * The defaults reproduce today's widget card exactly, so an absent or
 * malformed block changes nothing until an author opts in.
 */
export interface WidgetAppearance {
  background: 'card' | 'transparent'
  border: boolean
  padding: 'none' | 'sm' | 'md'
  align: 'left' | 'center' | 'right'
}

const DEFAULTS: WidgetAppearance = { background: 'card', border: true, padding: 'none', align: 'left' }

/**
 * Reads a widget's appearance out of its config. Invalid or missing fields fall
 * back per-field to the defaults, and a non-object `appearance` block is
 * ignored entirely.
 */
export function appearanceFrom(config: Record<string, unknown> | undefined): WidgetAppearance {
  const raw = config?.appearance
  if (!raw || typeof raw !== 'object') return { ...DEFAULTS }
  const a = raw as Record<string, unknown>
  const pick = <T extends string>(v: unknown, allowed: readonly T[], fallback: T): T =>
    typeof v === 'string' && (allowed as readonly string[]).includes(v) ? (v as T) : fallback
  return {
    background: pick(a.background, ['card', 'transparent'] as const, DEFAULTS.background),
    border: typeof a.border === 'boolean' ? a.border : DEFAULTS.border,
    padding: pick(a.padding, ['none', 'sm', 'md'] as const, DEFAULTS.padding),
    align: pick(a.align, ['left', 'center', 'right'] as const, DEFAULTS.align),
  }
}
