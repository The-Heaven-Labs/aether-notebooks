/** True when the value looks like an email address typed for a not-yet-registered user. */
export function looksLikeEmail(value: string): boolean {
  // Check format/control characters on the raw value: trim() strips U+FEFF
  // (BOM), which the server's Cf check rejects, so trimming first would let it through.
  if (/[\p{Cf}\p{Cc}]/u.test(value)) return false
  return /^[^\s@]+@[^\s@]+$/.test(value.trim())
}

/** Canonical email spelling used by the pending-subject APIs (the server lowercases too). */
export function normalizeEmail(value: string): string {
  return value.trim().toLowerCase()
}
