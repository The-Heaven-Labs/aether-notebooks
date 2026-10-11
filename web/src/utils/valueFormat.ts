export interface ValueFormat {
  style?: 'number' | 'currency' | 'percent'
  currency?: string
  decimals?: number
  compact?: boolean
}

/**
 * Shared value formatter for metric widgets and table columns.
 * Percent values render as-is with a suffix (12.5 → "12.5%"), unlike Intl's
 * style:'percent' which multiplies by 100 — widget authors write their SQL
 * values directly. Strings pass through untouched and empty values become an
 * em dash so cells never collapse to blank.
 */
export function formatValue(value: number | string | null | undefined, format?: ValueFormat): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'string') return value
  const decimals = format?.decimals
  const opts: Intl.NumberFormatOptions = {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals ?? 2,
  }
  if (format?.style === 'currency') {
    opts.style = 'currency'
    opts.currency = format.currency || 'USD'
  } else if (format?.compact) {
    opts.notation = 'compact'
    opts.maximumFractionDigits = decimals ?? 1
  }
  const formatted = new Intl.NumberFormat('en-US', opts).format(value)
  return format?.style === 'percent' ? `${formatted}%` : formatted
}
