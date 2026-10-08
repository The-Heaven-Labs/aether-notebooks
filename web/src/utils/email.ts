/** True when the value looks like an email address typed for a not-yet-registered user. */
export function looksLikeEmail(value: string): boolean {
  const v = value.trim()
  const at = v.indexOf('@')
  return at > 0 && at < v.length - 1 && !/\s/.test(v)
}

/** Canonical email spelling used by the pending-subject APIs (the server lowercases too). */
export function normalizeEmail(value: string): string {
  return value.trim().toLowerCase()
}
