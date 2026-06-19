import { useState, useEffect } from 'react'
import { ArrowLeft, MessageSquare, Play, Edit2 } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api } from '../api/client'
import { chatMarkdownComponents } from './AgentPanel'

interface SessionSummary {
  id: string
  created_at: string
  first_message: string
  message_count: number
  notebook_id: string
  title: string | null
}

interface SessionMessage {
  id: string
  role: string
  content?: string
  tool_calls?: any
  reasoning_content?: string
  created_at: string
}

interface SessionHistoryProps {
  agentId: string
  onBack: () => void
  onResumeSession: (session: SessionSummary) => void
}

export function SessionHistory({ agentId, onBack, onResumeSession }: SessionHistoryProps) {
  const [sessions, setSessions] = useState<SessionSummary[]>([])
  const [selectedSession, setSelectedSession] = useState<SessionSummary | null>(null)
  const [messages, setMessages] = useState<SessionMessage[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingMessages, setLoadingMessages] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editingTitle, setEditingTitle] = useState<string | null>(null)
  const [editValue, setEditValue] = useState('')

  useEffect(() => {
    api.get<SessionSummary[]>(`/api/v1/agents/${agentId}/sessions`)
      .then(setSessions)
      .catch(() => setError('Failed to load sessions'))
      .finally(() => setLoading(false))
  }, [agentId])

  const loadSession = async (session: SessionSummary) => {
    setSelectedSession(session)
    setLoadingMessages(true)
    setError(null)
    try {
      const msgs = await api.get<SessionMessage[]>(`/api/v1/sessions/${session.id}/messages`)
      setMessages(msgs)
    } catch {
      setError('Failed to load messages')
    } finally {
      setLoadingMessages(false)
    }
  }

  const handleSaveTitle = async (sessionId: string, title: string) => {
    const trimmed = title.trim()
    try {
      await api.patch(`/api/v1/sessions/${sessionId}/title`, { title: trimmed || null })
      setSessions(prev => prev.map(s => s.id === sessionId ? { ...s, title: trimmed || null } : s))
      setEditingTitle(null)
    } catch {
      setError('Failed to update title')
    }
  }

  if (selectedSession) {
    return (
      <>
        {error && <div style={styles.error}>{error}</div>}
        <div style={styles.sessionHeader}>
          <button onClick={() => setSelectedSession(null)} style={styles.backBtn}>
            <ArrowLeft size={14} /> Back to history
          </button>
          <button onClick={() => onResumeSession(selectedSession)} style={styles.resumeBtn}>
            <Play size={12} /> Resume
          </button>
          <span style={styles.sessionDate}>
            {new Date(selectedSession.created_at).toLocaleDateString()}
          </span>
        </div>
        <div style={styles.messageList}>
          {loadingMessages ? (
            <div style={styles.loadingText}>Loading...</div>
          ) : messages.length === 0 ? (
            <div style={styles.loadingText}>No messages</div>
          ) : (
            messages.map((msg) => (
              <div key={msg.id} style={{
                ...styles.historyMessage,
                ...(msg.role === 'user' ? styles.userBubble : msg.role === 'assistant' ? styles.assistantBubble : styles.toolBubble),
              }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={chatMarkdownComponents}>{msg.content || (msg.tool_calls ? 'Tool calls' : '(empty)')}</ReactMarkdown>
              </div>
            ))
          )}
        </div>
      </>
    )
  }

  return (
    <>
      {error && <div style={styles.error}>{error}</div>}
      <div style={styles.sessionHeader}>
        <button onClick={onBack} style={styles.backBtn}>
          <ArrowLeft size={14} /> Back to chat
        </button>
        <span style={styles.headerTitle}>Chat History</span>
      </div>
      <div style={styles.sessionList}>
        {loading ? (
          <div style={styles.loadingText}>Loading...</div>
        ) : sessions.length === 0 ? (
          <div style={styles.loadingText}>No past sessions</div>
        ) : (
          sessions.map((s) => (
            <button key={s.id} style={styles.sessionItem} onClick={() => !editingTitle && loadSession(s)}>
              <MessageSquare size={14} style={{ flexShrink: 0, marginTop: 2 }} />
              <div style={styles.sessionInfo}>
                <div style={styles.sessionPreview} onClick={(e) => {
                  e.stopPropagation()
                  setEditingTitle(s.id)
                  setEditValue(s.title || s.first_message || '')
                }}>
                  {editingTitle === s.id ? (
                    <input
                      style={styles.titleInput}
                      value={editValue}
                      onChange={(e) => setEditValue(e.target.value)}
                      onBlur={() => handleSaveTitle(s.id, editValue)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') handleSaveTitle(s.id, editValue)
                        if (e.key === 'Escape') setEditingTitle(null)
                      }}
                      autoFocus
                      maxLength={50}
                    />
                  ) : (
                    <>
                      {s.title || s.first_message || '(empty session)'}
                      <Edit2 size={12} style={styles.editIcon} />
                    </>
                  )}
                </div>
                <div style={styles.sessionMeta}>
                  {new Date(s.created_at).toLocaleDateString()} · {s.message_count} messages
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
  sessionHeader: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '10px 16px',
    borderBottom: '1px solid var(--border-light)',
    background: 'var(--bg-secondary)',
  },
  backBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 12,
    padding: '4px 8px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  resumeBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 12,
    padding: '4px 10px',
    background: 'var(--accent)',
    border: 'none',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'white',
    fontWeight: 500,
  },
  headerTitle: { fontSize: 12, fontWeight: 500, color: 'var(--text-muted)' },
  sessionDate: { fontSize: 12, color: 'var(--text-muted)', marginLeft: 'auto' },
  sessionList: { flex: 1, overflowY: 'auto', padding: 8 },
  sessionItem: {
    display: 'flex',
    gap: 8,
    width: '100%',
    padding: '10px 8px',
    background: 'none',
    border: 'none',
    borderBottom: '1px solid var(--border-light)',
    cursor: 'pointer',
    color: 'var(--text-primary)',
    textAlign: 'left' as const,
  },
  sessionInfo: { flex: 1, minWidth: 0 },
  sessionPreview: {
    fontSize: 13,
    fontWeight: 500,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap' as const,
  },
  sessionMeta: { fontSize: 11, color: 'var(--text-muted)', marginTop: 2 },
  titleInput: {
    width: '100%',
    fontSize: 13,
    fontWeight: 500,
    background: 'var(--bg-primary)',
    border: '1px solid var(--accent)',
    borderRadius: 3,
    padding: '2px 4px',
    color: 'var(--text-primary)',
    outline: 'none',
  },
  editIcon: {
    marginLeft: 4,
    opacity: 0.3,
    color: 'var(--text-muted)',
  },
  messageList: { flex: 1, overflowY: 'auto', padding: 16, display: 'flex', flexDirection: 'column', gap: 8 },
  historyMessage: {
    padding: '8px 12px', borderRadius: 6, fontSize: 13, lineHeight: 1.4,
    maxWidth: '85%', wordBreak: 'break-word' as const,
  },
  userBubble: { background: 'var(--accent)', color: 'white', alignSelf: 'flex-end' },
  assistantBubble: { background: 'var(--bg-secondary)', color: 'var(--text-primary)', alignSelf: 'flex-start' },
  toolBubble: { background: 'rgba(var(--accent-rgb, 59, 130, 246), 0.1)', color: 'var(--text-secondary)', alignSelf: 'flex-start', fontSize: 11 },
  loadingText: { textAlign: 'center', padding: 20, color: 'var(--text-muted)', fontSize: 13 },
  error: { padding: '8px 12px', background: 'var(--bg-secondary)', border: '1px solid var(--error, #ef4444)', borderRadius: 6, color: 'var(--error, #ef4444)', fontSize: 13, margin: 8 },
}
