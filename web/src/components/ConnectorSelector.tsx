import { useEffect, useState } from 'react'
import { api } from '../api/client'

interface ConnectorItem {
  id: string
  name: string
  type: string
  can_use?: boolean
}

interface ConnectorSelectorProps {
  value: string | null
  onChange: (id: string | null) => void
  placeholder?: string
  allowClear?: boolean
  style?: React.CSSProperties
}

export function ConnectorSelector({
  value,
  onChange,
  placeholder = 'Select connector',
  allowClear = false,
  style,
}: ConnectorSelectorProps) {
  const [connectors, setConnectors] = useState<ConnectorItem[]>([])

  useEffect(() => {
    api.get<ConnectorItem[]>('/api/v1/connectors')
      .then(data => setConnectors(data))
      .catch(() => {})
  }, [])

  return (
    <select
      aria-label={placeholder}
      style={{
        border: '1px solid var(--border)',
        borderRadius: 4,
        padding: '4px 8px',
        fontSize: 12,
        fontFamily: 'var(--font-mono)',
        background: 'var(--bg-input)',
        color: 'var(--text-primary)',
        outline: 'none',
        ...style,
      }}
      value={value ?? ''}
      onChange={e => onChange(e.target.value || null)}
    >
      <option value="" disabled={!allowClear || !value}>{allowClear && value ? 'Clear selection' : placeholder}</option>
      {connectors.map(c => (
        <option key={c.id} value={c.id} disabled={c.can_use === false}>
          {c.name}{c.can_use === false ? ' (view only)' : ''}
        </option>
      ))}
    </select>
  )
}
