import type React from 'react'

export const REDACTED_SENTINEL = '__AETHER_REDACTED__'

export interface HeaderRow {
  key: string
  value: string
  sensitive: boolean
}

export const emptyHeaderRow = (): HeaderRow => ({ key: '', value: '', sensitive: false })

/** Map an API header map + secrets list into editable rows. */
export function headersToRows(headers?: Record<string, unknown>, secrets?: string[]): HeaderRow[] {
  const entries = headers ? Object.entries(headers) : []
  if (entries.length === 0) return [emptyHeaderRow()]
  return entries.map(([key, value]) => ({ key, value: String(value), sensitive: secrets?.some(s => s.toLowerCase() === key.toLowerCase()) ?? false }))
}

/**
 * Build the API payload for a rows list. Rows without a key or value are
 * skipped; an unchanged sensitive value round-trips as the server's redaction
 * sentinel so the stored secret is preserved.
 */
export function rowsToPayload(rows: HeaderRow[]): { headers: Record<string, string>; secrets: string[] } {
  const headers: Record<string, string> = {}
  const secrets: string[] = []
  for (const h of rows) {
    if (h.key && h.value) {
      headers[h.key] = h.value
      if (h.sensitive) secrets.push(h.key)
    }
  }
  return { headers, secrets }
}

export function HeadersEditor({ rows, onChange, title = 'Headers', hint, style }: {
  rows: HeaderRow[]
  onChange: (rows: HeaderRow[]) => void
  title?: string
  hint?: string
  style?: React.CSSProperties
}) {
  const update = (i: number, patch: Partial<HeaderRow>) => {
    const next = [...rows]
    next[i] = { ...next[i], ...patch }
    onChange(next)
  }
  const add = () => onChange([...rows, emptyHeaderRow()])
  const remove = (i: number) => onChange(rows.filter((_, idx) => idx !== i))
  const toggleSensitive = (i: number) => {
    const row = rows[i]
    update(i, { sensitive: !row.sensitive, value: row.sensitive ? '' : row.value })
  }

  return (
    <div role="group" aria-label={title} style={{ ...styles.label, ...style }}>
      <span>{title}</span>
      {hint && <span style={styles.hint}>{hint}</span>}
      <div style={styles.rows}>
        {rows.map((h, i) => (
          <div key={i} style={styles.row}>
            <input
              aria-label={`Header ${i + 1} name`}
              style={{ ...styles.input, flex: 1 }}
              placeholder="Key"
              value={h.key}
              onChange={e => update(i, { key: e.target.value })}
            />
            <input
              aria-label={`Header ${i + 1} value`}
              type={h.sensitive ? 'password' : 'text'}
              style={{ ...styles.input, flex: 1 }}
              placeholder={h.value === REDACTED_SENTINEL ? '••••••••' : 'Value'}
              value={h.sensitive && h.value === REDACTED_SENTINEL ? '' : h.value}
              onChange={e => update(i, { value: e.target.value })}
            />
            <label style={styles.sensitiveLabel}>
              <input type="checkbox" checked={h.sensitive} onChange={() => toggleSensitive(i)} />
              sensitive
            </label>
            <button type="button" aria-label={`Remove header ${i + 1}`} style={styles.removeBtn} onClick={() => remove(i)}>×</button>
          </div>
        ))}
        <button type="button" style={styles.addBtn} onClick={add}>+ Add Header</button>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  label: { display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12, fontWeight: 600, color: 'var(--text-secondary)' },
  hint: { fontSize: 10, color: 'var(--text-muted)', fontWeight: 400 },
  rows: { display: 'flex', flexDirection: 'column', gap: 6, marginTop: 4 },
  row: { display: 'flex', gap: 6, alignItems: 'center' },
  input: { padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4, fontSize: 13, fontFamily: 'var(--font-mono)', background: 'var(--bg-input)', color: 'var(--text-primary)', marginTop: 2 },
  sensitiveLabel: { fontSize: 11, display: 'flex', alignItems: 'center', gap: 3, cursor: 'pointer', color: 'var(--text-secondary)', whiteSpace: 'nowrap', fontWeight: 400 },
  removeBtn: { background: 'none', border: 'none', cursor: 'pointer', color: 'var(--error-full)', fontSize: 14, padding: '4px 6px' },
  addBtn: { alignSelf: 'flex-start', padding: '4px 10px', background: 'var(--bg-secondary)', color: 'var(--text-primary)', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12, cursor: 'pointer' },
}
