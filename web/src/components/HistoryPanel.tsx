import type { CellVersion } from '../types'

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
      <div style={styles.header}>
        <span style={styles.title}>Cell History</span>
        <button style={styles.closeBtn} onClick={onClose} title="Close history">✕</button>
      </div>
      {versions.length === 0 && <p style={styles.empty}>No history yet</p>}
      {versions.map((v, i) => {
        const isCurrent = v.source === currentSource && i === 0
        return (
          <div key={v.id} style={styles.item}>
            <div style={styles.itemHeader}>
              <span style={styles.ts}>{fmt(v.created_at)}</span>
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
  panel: { width: 300, borderLeft: '1px solid var(--border)', background: 'white', display: 'flex', flexDirection: 'column', flexShrink: 0, overflowY: 'auto' },
  header: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 14px', borderBottom: '1px solid var(--border-light)', position: 'sticky', top: 0, background: 'white' },
  title: { fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' },
  closeBtn: { background: 'transparent', border: 'none', cursor: 'pointer', fontSize: 13, color: 'var(--text-muted)' },
  empty: { padding: 16, fontSize: 13, color: 'var(--text-muted)', textAlign: 'center' },
  item: { padding: '10px 14px', borderBottom: '1px solid var(--border-light)' },
  itemHeader: { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 },
  ts: { fontSize: 11, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' },
  currentBadge: { fontSize: 10, background: 'var(--accent-light)', color: 'var(--accent)', borderRadius: 4, padding: '1px 5px', fontWeight: 600 },
  preview: { fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-secondary)', background: 'var(--bg-secondary)', padding: '6px 8px', borderRadius: 4, whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: '0 0 6px' },
  restoreBtn: { fontSize: 11, padding: '3px 8px', background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', color: 'var(--text-secondary)', fontWeight: 500 },
}
