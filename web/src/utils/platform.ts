/**
 * True on Apple platforms, where keyboard shortcuts render with ⌘ (⌘K) —
 * false everywhere else (Ctrl K). Also false when no global navigator exists
 * (SSR), matching the plain-text fallback.
 */
export function isApplePlatform(): boolean {
  if (typeof navigator === 'undefined') return false
  return /Mac|iPhone|iPad|iPod/.test(navigator.userAgent)
}
