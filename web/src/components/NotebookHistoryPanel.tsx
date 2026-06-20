import { useState } from 'react'
import { Clock, RotateCcw, Plus, Minus, Edit3, Save } from 'lucide-react'
import { PanelHeader } from './PanelHeader'
import { ConfirmDialog } from './ConfirmDialog'
import type { NotebookSnapshot } from '../types'

interface Props {
  snapshots: NotebookSnapshot[]
  onCreateSnapshot?: (name: string) => Promise<void>
  onRestore: (snapshotId: string) => Promise<void>
  onClose: () => void
  canEdit?: boolean
}

function fmtTime(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  const diffHour = Math.floor(diffMin / 60)

  if (diffMin < 1) return 'Just now'
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHour < 24) return `${diffHour}h ago`
  return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function groupByDate(snapshots: NotebookSnapshot[]): Map<string, NotebookSnapshot[]> {
  const groups = new Map<string, NotebookSnapshot[]>()
  const today = new Date()
  const yesterday = new Date(today)
  yesterday.setDate(yesterday.getDate() - 1)

  for (const snap of snapshots) {
    const d = new Date(snap.created_at)
    let label: string
    if (d.toDateString() === today.toDateString()) {
      label = 'Today'
    } else if (d.toDateString() === yesterday.toDateString()) {
      label = 'Yesterday'
    } else {
      label = d.toLocaleDateString([], { month: 'long', day: 'numeric', year: 'numeric' })
    }
    const group = groups.get(label) || []
    group.push(snap)
    groups.set(label, group)
  }
  return groups
}

export function NotebookHistoryPanel({ snapshots, onCreateSnapshot, onRestore, onClose, canEdit }: Props) {
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [confirmRestore, setConfirmRestore] = useState<string | null>(null)
  const [restoring, setRestoring] = useState(false)
  const [saveName, setSaveName] = useState('')
  const [saving, setSaving] = useState(false)

  const groups = groupByDate(snapshots)

  const handleRestore = async (snapshotId: string) => {
    setRestoring(true)
    try {
      await onRestore(snapshotId)
    } finally {
      setRestoring(false)
      setConfirmRestore(null)
    }
  }

  const handleSave = async () => {
    if (!saveName.trim() || !onCreateSnapshot) return
    setSaving(true)
    try {
      await onCreateSnapshot(saveName.trim())
      setSaveName('')
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <div style={styles.panel}>
        <PanelHeader
          title="Version History"
          onClose={onClose}
          closeTitle="Close version history"
          style={{ borderBottom: '1px solid var(--border)', position: 'sticky', top: 0, background: 'var(--bg-primary)' }}
        />

        {canEdit && onCreateSnapshot && (
          <div style={styles.saveBar}>
            <input
              type="text"
              placeholder="Snapshot name..."
              value={saveName}
              onChange={(e) => setSaveName(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') handleSave() }}
              style={styles.saveInput}
            />
            <button
              style={styles.saveBtn}
              onClick={handleSave}
              disabled={!saveName.trim() || saving}
            >
              <Save size={12} />
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        )}

        <div style={styles.body}>
          {snapshots.length === 0 && (
            <p style={styles.empty}>No version history yet. Snapshots are created automatically when cells are modified, or you can save one manually above.</p>
          )}
          {Array.from(groups.entries()).map(([label, snaps]) => (
            <div key={label}>
              <div style={styles.groupLabel}>{label}</div>
              {snaps.map((snap) => {
                const expanded = expandedId === snap.id
                const changeCount =
                  (snap.changes?.cells_added?.length ?? 0) +
                  (snap.changes?.cells_deleted?.length ?? 0) +
                  (snap.changes?.cells_modified?.length ?? 0) +
                  (snap.changes?.title_changed ? 1 : 0)

                return (
                  <div
                    key={snap.id}
                    style={styles.item}
                    onClick={() => setExpandedId(expanded ? null : snap.id)}
                  >
                    <div style={styles.itemHeader}>
                      <Clock size={12} style={{ color: 'var(--text-muted)', flexShrink: 0 }} />
                      <span style={styles.ts}>{fmtTime(snap.created_at)}</span>
                      {snap.auto && <span style={styles.autoBadge}>auto</span>}
                    </div>
                    <div style={styles.itemName}>{snap.name}</div>
                    <div style={styles.itemMeta}>
                      <span style={styles.user}>{snap.user?.name || 'Unknown'}</span>
                      {changeCount > 0 && (
                        <span style={styles.changeCount}>{changeCount} change{changeCount !== 1 ? 's' : ''}</span>
                      )}
                    </div>

                    {expanded && snap.changes && (
                      <div style={styles.changes}>
                        {snap.changes.title_changed && (
                          <div style={styles.changeRow}>
                            <Edit3 size={11} style={{ flexShrink: 0, marginTop: 2 }} />
                            <span style={{ fontSize: 11, color: 'var(--text-secondary)' }}>
                              Title: <strong>{snap.changes.old_title}</strong> → <strong>{snap.changes.new_title}</strong>
                            </span>
                          </div>
                        )}
                        {snap.changes.cells_added.map((id) => (
                          <div key={id} style={{ ...styles.changeRow, color: 'var(--success)' }}>
                            <Plus size={11} style={{ flexShrink: 0, marginTop: 2 }} />
                            <span style={{ fontSize: 11 }}>Cell added</span>
                          </div>
                        ))}
                        {snap.changes.cells_deleted.map((id) => (
                          <div key={id} style={{ ...styles.changeRow, color: 'var(--danger)' }}>
                            <Minus size={11} style={{ flexShrink: 0, marginTop: 2 }} />
                            <span style={{ fontSize: 11 }}>Cell deleted</span>
                          </div>
                        ))}
                        {snap.changes.cells_modified.map((id) => (
                          <div key={id} style={styles.changeRow}>
                            <Edit3 size={11} style={{ flexShrink: 0, marginTop: 2 }} />
                            <span style={{ fontSize: 11 }}>Cell modified</span>
                          </div>
                        ))}
                        {snap.changes.positions_changed && (
                          <div style={styles.changeRow}>
                            <Edit3 size={11} style={{ flexShrink: 0, marginTop: 2 }} />
                            <span style={{ fontSize: 11 }}>Cells reordered</span>
                          </div>
                        )}
                      </div>
                    )}

                    {expanded && (
                      <button
                        style={styles.restoreBtn}
                        onClick={(e) => { e.stopPropagation(); setConfirmRestore(snap.id) }}
                        disabled={restoring}
                      >
                        <RotateCcw size={11} style={{ marginRight: 4 }} />
                        {restoring ? 'Restoring...' : 'Restore this version'}
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
          ))}
        </div>
      </div>

      <ConfirmDialog
        open={!!confirmRestore}
        title="Restore snapshot"
        message="This will restore the notebook to the state captured in this snapshot. Any changes made since this snapshot will be lost (deleted cells will be removed, modified cells will be reverted)."
        confirmLabel="Restore"
        destructive
        onConfirm={() => confirmRestore && handleRestore(confirmRestore)}
        onCancel={() => setConfirmRestore(null)}
      />
    </>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: { width: 380, height: '100%', borderLeft: '1px solid var(--border)', background: 'var(--bg-primary)', display: 'flex', flexDirection: 'column', flexShrink: 0 },
  saveBar: { display: 'flex', gap: 6, padding: '8px 14px', borderBottom: '1px solid var(--border-light)', background: 'var(--bg-secondary)' },
  saveInput: { flex: 1, fontSize: 12, padding: '5px 8px', border: '1px solid var(--border)', borderRadius: 4, background: 'var(--bg-primary)', color: 'var(--text-primary)', outline: 'none' },
  saveBtn: { fontSize: 11, padding: '5px 10px', background: 'var(--accent)', border: 'none', borderRadius: 4, cursor: 'pointer', color: '#fff', fontWeight: 500, display: 'inline-flex', alignItems: 'center', gap: 4, whiteSpace: 'nowrap' },
  body: { overflowY: 'auto', flex: 1 },
  empty: { padding: 16, fontSize: 13, color: 'var(--text-muted)', textAlign: 'center' },
  groupLabel: { fontSize: 11, fontWeight: 600, color: 'var(--text-muted)', padding: '10px 14px 4px', textTransform: 'uppercase' as const, letterSpacing: '0.5px' },
  item: { padding: '8px 14px', borderBottom: '1px solid var(--border-light)', cursor: 'pointer' },
  itemHeader: { display: 'flex', alignItems: 'center', gap: 6 },
  ts: { fontSize: 11, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' },
  autoBadge: { fontSize: 9, background: 'var(--bg-secondary)', color: 'var(--text-muted)', border: '1px solid var(--border)', borderRadius: 3, padding: '0 4px', fontWeight: 600 },
  itemName: { fontSize: 13, fontWeight: 500, color: 'var(--text-primary)', marginTop: 2, lineHeight: '18px' },
  itemMeta: { display: 'flex', alignItems: 'center', gap: 8, marginTop: 2 },
  user: { fontSize: 11, color: 'var(--accent)' },
  changeCount: { fontSize: 10, color: 'var(--text-muted)', background: 'var(--bg-secondary)', borderRadius: 3, padding: '1px 5px' },
  changes: { marginTop: 6, display: 'flex', flexDirection: 'column', gap: 3, paddingLeft: 4 },
  changeRow: { display: 'flex', alignItems: 'flex-start', gap: 4 },
  restoreBtn: { fontSize: 11, padding: '4px 10px', background: 'var(--accent)', border: 'none', borderRadius: 4, cursor: 'pointer', color: '#fff', fontWeight: 500, marginTop: 8, display: 'inline-flex', alignItems: 'center' },
}
