import { useState } from 'react'
import type React from 'react'
import { Database } from 'lucide-react'

export interface ConnectorCardItem {
  id: string
  name: string
  type: string
  is_default: boolean
  host?: string
  port?: number
  database?: string
  user?: string
}

interface Props {
  connectors: ConnectorCardItem[]
  isAdmin?: boolean
  onEdit?: (id: string) => void
  onDelete?: (id: string) => void
  onSetDefault?: (id: string) => void
}

export function ConnectorsVariantCard({ connectors, isAdmin, onEdit, onDelete, onSetDefault }: Props) {
  const [expandedId, setExpandedId] = useState<string | null>(null)

  if (connectors.length === 0) {
    return <div style={s.empty}>No connectors yet.</div>
  }

  return (
    <div style={s.list}>
      {connectors.map((c) => {
        const isExpanded = expandedId === c.id
        const hasDetails = c.host || c.port || c.database || c.user
        return (
          <div key={c.id} style={s.card}>
            <div style={s.row}>
              <button
                style={s.expandBtn}
                onClick={() => setExpandedId(isExpanded ? null : c.id)}
                aria-expanded={isExpanded}
                disabled={!hasDetails}
              >
                <span style={{ ...s.chevron, transform: isExpanded ? 'rotate(90deg)' : 'none', opacity: hasDetails ? 1 : 0.2 }}>▶</span>
                <Database size={14} style={{ color: 'var(--accent)', flexShrink: 0 }} />
                <span style={s.name}>{c.name}</span>
                <span style={s.typeBadge}>{c.type}</span>
                {c.is_default && <span style={s.defaultBadge}>default</span>}
              </button>
              {isAdmin && (
                <div style={s.actions}>
                  <button style={s.actionBtn} onClick={() => onEdit?.(c.id)}>Edit</button>
                  {!c.is_default && (
                    <button style={s.actionBtn} onClick={() => onSetDefault?.(c.id)}>Set default</button>
                  )}
                  <button style={s.deleteBtn} onClick={() => onDelete?.(c.id)}>Delete</button>
                </div>
              )}
            </div>
            {isExpanded && hasDetails && (
              <div style={s.body}>
                <div style={s.detailGrid}>
                  {c.host && <><span style={s.detailLabel}>Host</span><span style={s.detailValue}>{c.host}</span></>}
                  {c.port && <><span style={s.detailLabel}>Port</span><span style={s.detailValue}>{c.port}</span></>}
                  {c.database && <><span style={s.detailLabel}>Database</span><span style={s.detailValue}>{c.database}</span></>}
                  {c.user && <><span style={s.detailLabel}>User</span><span style={s.detailValue}>{c.user}</span></>}
                </div>
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

const s: Record<string, React.CSSProperties> = {
  list: { display: 'flex', flexDirection: 'column', gap: 8 },
  empty: { padding: '40px 0', textAlign: 'center', color: 'var(--text-muted)', fontSize: 13 },
  card: { border: '1px solid var(--border)', borderRadius: 6, overflow: 'hidden' },
  row: {
    display: 'flex', alignItems: 'center', justifyContent: 'space-between',
    padding: '10px 14px', background: 'var(--bg-primary)',
  },
  expandBtn: {
    display: 'flex', alignItems: 'center', gap: 8,
    background: 'transparent', border: 'none', cursor: 'pointer',
    padding: 0, flex: 1, textAlign: 'left' as const, minWidth: 0,
  },
  chevron: { fontSize: 10, color: 'var(--text-secondary)', transition: 'transform 0.15s ease', flexShrink: 0 },
  name: { fontSize: 14, fontWeight: 600, color: 'var(--text-primary)' },
  typeBadge: {
    fontSize: 10, fontWeight: 700, borderRadius: 3, padding: '1px 6px',
    background: 'var(--accent-light)', color: 'var(--text-primary)',
    textTransform: 'uppercase' as const, letterSpacing: '0.04em', flexShrink: 0,
  },
  defaultBadge: {
    fontSize: 10, fontWeight: 600, borderRadius: 3, padding: '1px 6px',
    background: 'var(--success-light)', color: 'var(--success)', flexShrink: 0,
  },
  actions: { display: 'flex', gap: 6, flexShrink: 0 },
  actionBtn: {
    padding: '4px 10px', fontSize: 12, fontWeight: 500,
    border: '1px solid var(--border)', borderRadius: 4,
    background: 'transparent', cursor: 'pointer', color: 'inherit',
  },
  deleteBtn: {
    padding: '4px 10px', fontSize: 12, fontWeight: 500,
    border: '1px solid transparent', borderRadius: 4,
    background: 'transparent', cursor: 'pointer', color: 'var(--error-text)',
  },
  body: { borderTop: '1px solid var(--border)', padding: '12px 16px', background: 'var(--bg-secondary)' },
  detailGrid: { display: 'grid', gridTemplateColumns: 'max-content 1fr', gap: '4px 16px' },
  detailLabel: {
    fontSize: 11, fontWeight: 600, color: 'var(--text-muted)',
    textTransform: 'uppercase' as const, letterSpacing: '0.05em',
    alignSelf: 'center',
  },
  detailValue: { fontSize: 13, color: 'var(--text-primary)', fontFamily: 'var(--font-mono)' },
}
