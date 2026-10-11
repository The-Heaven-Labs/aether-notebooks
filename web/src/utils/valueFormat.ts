export interface ValueFormat {
  style?: 'number' | 'currency' | 'percent'
  currency?: string
  decimals?: number
  compact?: boolean
}

// Formatters are immutable and reused per render across table cells, so
// instances are cached by their resolved options. Failures (invalid currency
// codes) are never cached — the key is only written after construction.
const formatterCache = new Map<string, Intl.NumberFormat>()

function getFormatter(opts: Intl.NumberFormatOptions): Intl.NumberFormat {
  const key = JSON.stringify(opts)
  let formatter = formatterCache.get(key)
  if (!formatter) {
    formatter = new Intl.NumberFormat('en-US', opts)
    formatterCache.set(key, formatter)
  }
  return formatter
}

/**
 * Shared value formatter for metric widgets and table columns.
 * Percent values render as-is with a suffix (12.5 → "12.5%"), unlike Intl's
 * style:'percent' which multiplies by 100 — widget authors write their SQL
 * values directly. Strings pass through untouched and empty values become an
 * em dash so cells never collapse to blank. Fraction digits are clamped to
 * Intl's [0, 20] range; non-finite values fall back to the defaults so a raw
 * config input (e.g. an empty number field) can never throw a RangeError.
 * An invalid currency code (e.g. "EURO") throws during formatter construction;
 * that degrades to plain number formatting instead of blanking the render.
 */
export function formatValue(value: number | string | null | undefined, format?: ValueFormat): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'string') return value
  const rawDecimals = format?.decimals
  const decimals = rawDecimals === undefined || !Number.isFinite(rawDecimals)
    ? undefined
    : Math.min(20, Math.max(0, Math.trunc(rawDecimals)))
  const opts: Intl.NumberFormatOptions = {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals ?? 2,
  }
  if (format?.style === 'currency') {
    opts.style = 'currency'
    opts.currency = format.currency || 'USD'
  }
  // Compact is independent of style: Intl natively supports compact currency
  // ("$1.3M"), and the drawer exposes the two options separately.
  if (format?.compact) {
    opts.notation = 'compact'
    opts.maximumFractionDigits = decimals ?? 1
  }
  let formatted: string
  try {
    formatted = getFormatter(opts).format(value)
  } catch {
    // Plain number formatting with the same fraction-digit behavior, without
    // the currency style.
    formatted = getFormatter({
      minimumFractionDigits: decimals,
      maximumFractionDigits: decimals ?? 2,
    }).format(value)
  }
  return format?.style === 'percent' ? `${formatted}%` : formatted
}
