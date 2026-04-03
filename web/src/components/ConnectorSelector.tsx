import { useEffect, useState } from 'react'
import { api } from '../api/client'

interface Connector {
  id: string
  name: string
  type: string
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
  const [connectors, setConnectors] = useState<Connector[]>([])

  useEffect(() => {
    api.get<Connector[]>('/api/v1/connectors')
      .then(data => setConnectors(data))
      .catch(() => {})
  }, [])

  return (
    <select
      style={{
        border: '1px solid #ddd',
        borderRadius: 4,
        padding: '4px 8px',
        fontSize: 12,
        fontFamily: 'var(--font-mono)',
        background: '#fff',
        color: '#333',
        outline: 'none',
        ...style,
      }}
      value={value ?? ''}
      onChange={e => onChange(e.target.value || null)}
    >
      <option value="">{allowClear && value ? '— None —' : placeholder}</option>
      {connectors.map(c => (
        <option key={c.id} value={c.id}>
          {c.name}
        </option>
      ))}
    </select>
  )
}
