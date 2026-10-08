/**
 * Compact relative time for persisted connector health labels
 * (e.g. "12m ago", "1h ago", "3d ago"). Returns '' for missing or invalid
 * input so callers can decide the fallback copy. `now` is an optional test
 * seam; production callers use the default.
 */
export function formatRelativeTime(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return ''
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  // Clamp future timestamps (clock skew) to "just now" instead of negative ages.
  const seconds = Math.max(0, Math.round((now - then) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  return `${Math.floor(hours / 24)}d ago`
}
