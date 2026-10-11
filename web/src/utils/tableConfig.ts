import type { ValueFormat } from './valueFormat'

export interface TableConfig {
  /** Per-column formats keyed by column name; unlisted columns render raw. */
  columns: Record<string, ValueFormat>
}

const STYLES: ReadonlySet<string> = new Set(['number', 'currency', 'percent'])

// Copies only the recognized ValueFormat keys so a hand-edited or stale config
// can never smuggle junk into the renderer. Currency codes are normalized
// (trim + uppercase) and must look like an ISO 4217 code — anything else is
// dropped rather than handed to Intl, which throws on invalid codes. A format
// that normalizes to no keys at all is dropped along with its column.
function normalizeFormat(raw: unknown): ValueFormat | null {
  if (!raw || typeof raw !== 'object') return null
  const src = raw as Record<string, unknown>
  if (src.style !== undefined && !(typeof src.style === 'string' && STYLES.has(src.style))) return null
  const out: ValueFormat = {}
  if (typeof src.style === 'string') out.style = src.style as ValueFormat['style']
  if (typeof src.currency === 'string') {
    const currency = src.currency.trim().toUpperCase()
    if (/^[A-Z]{3}$/.test(currency)) out.currency = currency
  }
  if (typeof src.decimals === 'number' && Number.isFinite(src.decimals)) out.decimals = src.decimals
  if (typeof src.compact === 'boolean') out.compact = src.compact
  return Object.keys(out).length > 0 ? out : null
}

/**
 * Reads per-column formats out of a widget config's `table` block
 * (`{ table: { columns: [{ name, format }] } }`). Malformed entries are
 * ignored so a hand-edited config degrades to raw rendering instead of
 * crashing the table.
 */
export function tableConfigFrom(config: Record<string, unknown> | undefined): TableConfig {
  const entries: Array<[string, ValueFormat]> = []
  const table = config?.table
  if (!table || typeof table !== 'object') return { columns: {} }
  const raw = (table as Record<string, unknown>).columns
  if (!Array.isArray(raw)) return { columns: {} }
  for (const entry of raw) {
    if (!entry || typeof entry !== 'object') continue
    const { name, format } = entry as Record<string, unknown>
    if (typeof name !== 'string' || name === '') continue
    const normalized = normalizeFormat(format)
    if (!normalized) continue
    entries.push([name, normalized])
  }
  // fromEntries creates own properties even for keys like "__proto__", which
  // assignment to a plain object would route through the prototype setter.
  return { columns: Object.fromEntries(entries) }
}
