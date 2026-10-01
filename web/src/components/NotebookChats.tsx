import { useEffect, useState } from 'react'
import { MessageSquare } from 'lucide-react'
import { api } from '../api/client'
import type { AgentSessionListItem } from '../types/agent'
import { PanelHeader } from './PanelHeader'

export interface NotebookChatsProps {
  notebookId: string
  onClose: () => void
  onOpenSession: (session: AgentSessionListItem) => void
  onResumeSession?: (session: AgentSessionListItem) => void
}

function fmtRelative(iso: string): string {
  const date = new Date(iso)
  const diffMin = Math.floor((Date.now() - date.getTime()) / 60000)
  if (diffMin < 1) return 'Just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHour = Math.floor(diffMin / 60)
  if (diffHour < 24) return `${diffHour}h ago`
  const diffDay = Math.floor(diffHour / 24)
  if (diffDay < 7) return `${diffDay}d ago`
  return date.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

export function NotebookChats({ notebookId, onClose, onOpenSession, onResumeSession }: NotebookChatsProps) {
  const [sessions, setSessions] = useState<AgentSessionListItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    api.get<AgentSessionListItem[]>(`/api/v1/notebooks/${notebookId}/sessions`)
      .then((data) => { if (!cancelled) setSessions(data ?? []) })
      .catch(() => { if (!cancelled) setError('Failed to load chats') })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [notebookId])

  // Deliberate decision (docs/plans/2026-09-11-agent-session-sharing-plan.md,
  // Task 12): every row opens the read-only viewer, including owned sessions —
  // their can_edit suppresses the "Shared · Read-only" banner and resuming
  // stays in the agent panel's history. `onResumeSession` is reserved for a
  // future wiring and preferred when a caller provides it for an owned row.
  const openSession = (session: AgentSessionListItem) => {
    if (session.shared === false && onResumeSession) {
      onResumeSession(session)
      return
    }
    onOpenSession(session)
  }

  return (
    <>
      <PanelHeader
        title="Chats"
        onClose={onClose}
        closeTitle="Close chats"
        style={{ borderBottom: '1px solid var(--border)', background: 'var(--bg-primary)' }}
      />
      <div style={styles.body}>
        {loading ? (
          <div style={styles.stateText}>Loading…</div>
        ) : error ? (
          <div style={styles.stateText}>{error}</div>
        ) : sessions.length === 0 ? (
          <div style={styles.stateText}>No chats in this notebook yet</div>
        ) : (
          sessions.map((session) => (
            <button
              key={session.id}
              type="button"
              style={styles.item}
              onClick={() => openSession(session)}
            >
              <MessageSquare size={14} style={{ flexShrink: 0, marginTop: 2 }} />
              <div style={styles.info}>
                <div style={styles.preview}>
                  {session.title || session.first_message || '(empty session)'}
                  {session.shared && <span style={styles.badge}>Shared</span>}
                </div>
                <div style={styles.meta}>
                  {session.owner_email && <span style={styles.owner}>{session.owner_email}</span>}
                  {session.owner_email && <span style={styles.sep}>·</span>}
                  <span>{fmtRelative(session.created_at)}</span>
                  <span style={styles.sep}>·</span>
                  <span>{session.message_count} message{session.message_count === 1 ? '' : 's'}</span>
                </div>
              </div>
            </button>
          ))
        )}
      </div>
    </>
  )
}

const styles: Record<string, React.CSSProperties> = {
  body: { flex: 1, overflowY: 'auto' },
  stateText: { textAlign: 'center', padding: 20, color: 'var(--text-muted)', fontSize: 13 },
  item: {
    display: 'flex',
    gap: 8,
    width: '100%',
    padding: '10px 14px',
    background: 'none',
    border: 'none',
    borderBottom: '1px solid var(--border-light)',
    cursor: 'pointer',
    color: 'var(--text-primary)',
    textAlign: 'left' as const,
  },
  info: { flex: 1, minWidth: 0 },
  preview: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    fontSize: 13,
    fontWeight: 500,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
  },
  badge: {
    flexShrink: 0,
    fontSize: 9,
    fontWeight: 600,
    textTransform: 'uppercase' as const,
    letterSpacing: '0.04em',
    color: 'var(--accent)',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 3,
    padding: '0 4px',
  },
  meta: { display: 'flex', alignItems: 'center', gap: 4, fontSize: 11, color: 'var(--text-muted)', marginTop: 2 },
  owner: { color: 'var(--accent)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' as const },
  sep: { opacity: 0.6 },
}
