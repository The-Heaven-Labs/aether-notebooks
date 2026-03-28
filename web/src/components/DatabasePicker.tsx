import { useEffect, useState } from 'react'
import { api } from '../api/client'

interface DatabasePickerProps {
  connectorId: string
  value: string | null
  onChange: (db: string) => void
}

export function DatabasePicker({ connectorId, value, onChange }: DatabasePickerProps) {
  const [databases, setDatabases] = useState<string[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!connectorId) return
    setLoading(true)
    api.get<{ databases: string[] }>(`/api/v1/connectors/${connectorId}/databases`)
      .then(data => setDatabases(data.databases ?? []))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [connectorId])

  if (loading) return <div style={{ fontSize: 11, color: 'var(--text-muted)', padding: '6px 12px' }}>Loading databases…</div>
  if (databases.length === 0) return null

  return (
    <div style={{ padding: '6px 12px', borderBottom: '1px solid var(--border)' }}>
      <label style={{ fontSize: 10, color: 'var(--text-muted)', display: 'block', marginBottom: 3, fontWeight: 600, letterSpacing: '0.06em', textTransform: 'uppercase' }}>Database</label>
      <select
        style={{
          width: '100%',
          fontSize: 12,
          background: 'var(--bg-primary)',
          border: '1px solid var(--border)',
          borderRadius: 4,
          padding: '3px 6px',
          color: 'var(--text-primary)',
          outline: 'none',
        }}
        value={value ?? ''}
        onChange={e => onChange(e.target.value)}
      >
        <option value="">— select database —</option>
        {databases.map(db => (
          <option key={db} value={db}>{db}</option>
        ))}
      </select>
    </div>
  )
}
