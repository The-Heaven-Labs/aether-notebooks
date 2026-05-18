import type { CellVersion } from '../types'
import { PanelHeader } from './PanelHeader'

interface Props {
  versions: CellVersion[]
  currentSource: string
  onRestore: (versionId: string) => void
  onClose: () => void
}

export function HistoryPanel({ versions, currentSource, onRestore, onClose }: Props) {
  const fmt = (iso: string) => {
    const d = new Date(iso)
    return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  return (
    <div style={styles.panel}>
      <PanelHeader title="Cell History" onClose={onClose} closeTitle="Close history" style={{ borderBottom: '1px solid var(--border)', position: 'sticky', top: 0, background: 'var(--bg-primary)' }} />
      {versions.length === 0 && <p style={styles.empty}>No history yet</p>}
      {versions.map((v, i) => {
        const isCurrent = v.source === currentSource && i === 0
        return (
          <div key={v.id} style={styles.item}>
            <div style={styles.itemHeader}>
              <span style={styles.ts}>{fmt(v.created_at)}</span>
              {v.user && <span style={styles.user}>{v.user.name}</span>}
              {isCurrent && <span style={styles.currentBadge}>current</span>}
            </div>
            <pre style={styles.preview}>{v.source.slice(0, 120)}{v.source.length > 120 ? '…' : ''}</pre>
            <button style={styles.restoreBtn} onClick={() => onRestore(v.id)}>Restore</button>
          </div>
        )
      })}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { width: 300, height: '100%', borderLeft: '1px solid var(--border)', background: 'var(--bg-primary)', display: 'flex', flexDirection: 'column', flexShrink: 0, overflowY: 'auto' },
  empty: { padding: 16, fontSize: 13, color: 'var(--text-muted)', textAlign: 'center' },
  item: { padding: '10px 14px', borderBottom: '1px solid var(--border-light)' },
  itemHeader: { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 },
  ts: { fontSize: 11, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' },
  user: { fontSize: 11, color: 'var(--accent)', fontFamily: 'var(--font-mono)' },
  currentBadge: { fontSize: 10, background: 'var(--bg-secondary)', color: 'var(--text-secondary)', border: '1px solid var(--border)', borderRadius: 4, padding: '1px 5px', fontWeight: 600 },
  preview: { fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-secondary)', background: 'var(--bg-secondary)', padding: '6px 8px', borderRadius: 4, whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: '0 0 6px' },
  restoreBtn: { fontSize: 11, padding: '3px 8px', background: 'none', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', color: 'var(--text-primary)', fontWeight: 500 },
}
