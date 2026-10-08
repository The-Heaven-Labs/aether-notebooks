import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'

interface ConnectorItem {
  id: string
  name: string
  type: string
  can_use?: boolean
  is_provisioner?: boolean
}

interface ConnectorSelectorProps {
  value: string | null
  onChange: (id: string | null) => void
  placeholder?: string
  allowClear?: boolean
  style?: React.CSSProperties
  /** When set, only connectors of these types render. */
  types?: string[]
}

export function ConnectorSelector({
  value,
  onChange,
  placeholder = 'Select connector',
  allowClear = false,
  style,
  types,
}: ConnectorSelectorProps) {
  // Shares the ['connectors'] query with the sidebar and page queries so the
  // list is fetched once per cache window instead of once per consumer.
  const { data: connectors = [] } = useQuery<ConnectorItem[]>({
    queryKey: ['connectors'],
    queryFn: () => api.get<ConnectorItem[]>('/api/v1/connectors'),
  })
  const visible = types ? connectors.filter(c => types.includes(c.type)) : connectors

  return (
    <span style={styles.wrap}>
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
        {visible.map(c => (
          <option key={c.id} value={c.id} disabled={c.can_use === false}>
            {c.name}
            {c.is_provisioner
              ? (c.can_use === false ? ' (provisioner — no access)' : ' (provisioner)')
              : (c.can_use === false ? ' (view only)' : '')}
          </option>
        ))}
      </select>
    </span>
  )
}

const styles: Record<string, React.CSSProperties> = {
  wrap: {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 4,
    minWidth: 0,
  },
}
