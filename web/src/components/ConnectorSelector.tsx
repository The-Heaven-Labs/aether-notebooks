import { useEffect, useState } from 'react'
import { Lock, LockOpen } from 'lucide-react'
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
  /** Pin runs in this scope to the selected connector (bypasses preferences). */
  pinned?: boolean
  onTogglePin?: (pinned: boolean) => void
  style?: React.CSSProperties
}

export function ConnectorSelector({
  value,
  onChange,
  placeholder = 'Select connector',
  allowClear = false,
  pinned = false,
  onTogglePin,
  style,
}: ConnectorSelectorProps) {
  const [connectors, setConnectors] = useState<ConnectorItem[]>([])

  useEffect(() => {
    api.get<ConnectorItem[]>('/api/v1/connectors')
      .then(data => setConnectors(data))
      .catch(() => {})
  }, [])

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
        {connectors.map(c => (
          <option key={c.id} value={c.id} disabled={c.can_use === false}>
            {c.name}{c.can_use === false ? ' (view only)' : ''}
          </option>
        ))}
      </select>
      {onTogglePin && (
        <button
          type="button"
          aria-label={pinned ? 'Unpin connector' : 'Pin connector'}
          aria-pressed={pinned}
          title={pinned
            ? 'Pinned: runs use this connector directly, bypassing your service preference'
            : 'Pin runs to this connector'}
          disabled={!value}
          onClick={() => onTogglePin(!pinned)}
          style={{
            ...styles.pinBtn,
            ...(pinned ? styles.pinBtnActive : {}),
            opacity: value ? 1 : 0.4,
            cursor: value ? 'pointer' : 'not-allowed',
          }}
        >
          {pinned ? <Lock size={12} /> : <LockOpen size={12} />}
        </button>
      )}
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
  pinBtn: {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '3px 5px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    color: 'var(--text-muted)',
  },
  pinBtnActive: {
    background: 'var(--accent-light)',
    borderColor: 'var(--accent)',
    color: 'var(--accent)',
  },
}
