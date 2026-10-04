/**
 * Formats an instant for dashboard "Executed at" labels: local date and time
 * with an explicit timezone name (e.g. "04/10/2026 20:15:32 GMT-3").
 *
 * The browser renders in the viewer's own timezone; naming it removes the
 * ambiguity when dashboards are shared across regions.
 */
export function formatExecutedAt(value: Date | string | number): string {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const day = date.toLocaleDateString([], { year: 'numeric', month: '2-digit', day: '2-digit' })
  const time = date.toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    // Numeric offset ("GMT-3") rather than an abbreviation ("BRT"), so the
    // label reads the same for every viewer.
    timeZoneName: 'shortOffset',
  })
  return `${day} ${time}`
}
