import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ArrowLeft, Share2, X } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import { api, ApiError, getToken } from '../api/client'
import { getWsUrl } from '../config'
import type { AgentMessage, AgentSession, WSMessage } from '../types/agent'
import { mapServerMessagesToChat, mapSubagentMessage, mapSubagentMessages, applyToolResult, applySteeringMessage } from '../utils/agentTranscript'
import { AgentChatTranscript, chatMarkdownComponents, chatStyles } from './AgentChatTranscript'
import type { ChatMessage } from './AgentChatTranscript'
import { PermissionsPanel } from './PermissionsPanel'

const WS_URL = getWsUrl() + '/api/v1/ws/agents/'

export interface SessionViewerProps {
  sessionId: string
  /** Optional session summary (owner_email, shared, can_edit, title) from a
   * listing endpoint; the GET /sessions body does not always carry them. */
  session?: AgentSession | null
  onClose?: () => void
  /** Full-page presentation: region semantics and a Back affordance. */
  page?: boolean
  /** Owner-only share override; when absent the Share button opens the
   * built-in PermissionsPanel for the session. */
  onShare?: () => void
}

interface RetryNotice {
  attempt: number
  max: number
  error: string
}

// SessionViewer renders a shared agent session read-only: the same transcript
// as AgentPanel (via the shared reducers), live WS events, and no controls.
// It never sends mutating frames; tool confirmations and questions are ignored.
export function SessionViewer({ sessionId, session: sessionSummary, onClose, page, onShare }: SessionViewerProps) {
  const [fetchedSession, setFetchedSession] = useState<AgentSession | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [isStreaming, setIsStreaming] = useState(false)
  const [currentStreamingText, setCurrentStreamingText] = useState('')
  const [currentStreamingReasoning, setCurrentStreamingReasoning] = useState('')
  const [retryNotice, setRetryNotice] = useState<RetryNotice | null>(null)
  const [connected, setConnected] = useState(false)
  const [disconnected, setDisconnected] = useState(false)
  const [subagentView, setSubagentView] = useState<string | null>(null)
  const [subagentMessages, setSubagentMessages] = useState<ChatMessage[]>([])
  const [subagentLoading, setSubagentLoading] = useState(false)
  const [subagentError, setSubagentError] = useState<string | null>(null)
  const [showPermissions, setShowPermissions] = useState(false)
  // Optimistic override for share_with_notebook_viewers so the toggle reflects
  // the PATCH result without refetching the session.
  const [inheritOverride, setInheritOverride] = useState<boolean | null>(null)
  // Optimistic override for the notebook link (attach/detach).
  const [notebookOverride, setNotebookOverride] = useState<string | null | undefined>(undefined)

  const wsRef = useRef<WebSocket | null>(null)
  const reconnectAttemptsRef = useRef(0)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const suppressReconnectRef = useRef(false)
  const streamingTextRef = useRef('')
  const streamingReasoningRef = useRef('')
  const lastSeqRef = useRef(0)
  const lastResyncRequestRef = useRef(0)
  const lastMessageIdRef = useRef('')
  const syncedRef = useRef(false)
  const subagentViewRef = useRef<string | null>(null)
  const scrollRef = useRef<HTMLDivElement | null>(null)

  const session = useMemo<AgentSession | null>(() => {
    if (!sessionSummary && !fetchedSession) return null
    const merged = { ...(sessionSummary ?? {}), ...(fetchedSession ?? {}) } as AgentSession
    if (inheritOverride !== null) merged.share_with_notebook_viewers = inheritOverride
    if (notebookOverride !== undefined) merged.notebook_id = notebookOverride ?? ''
    return merged
  }, [sessionSummary, fetchedSession, inheritOverride, notebookOverride])

  // Session switches must not leak the previous transcript/stream state.
  useEffect(() => {
    setFetchedSession(null)
    setMessages([])
    setSubagentView(null)
    subagentViewRef.current = null
    setSubagentMessages([])
    setSubagentError(null)
    streamingTextRef.current = ''
    streamingReasoningRef.current = ''
    setCurrentStreamingText('')
    setCurrentStreamingReasoning('')
    lastSeqRef.current = 0
    lastMessageIdRef.current = ''
    lastResyncRequestRef.current = 0
    setRetryNotice(null)
    setConnected(false)
    setDisconnected(false)
    syncedRef.current = false
    setShowPermissions(false)
    setInheritOverride(null)
    setNotebookOverride(undefined)
  }, [sessionId])

  // The REST fetch seeds the transcript; the WS reconnect_sync is authoritative
  // and replaces it when the connection opens. A late REST response must never
  // clobber or regress what reconnect_sync already applied.
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    Promise.all([
      api.get<AgentSession>(`/api/v1/sessions/${sessionId}`, { signal: controller.signal }),
      api.get<AgentMessage[]>(`/api/v1/sessions/${sessionId}/messages`, { signal: controller.signal }),
    ])
      .then(([sess, rows]) => {
        if (controller.signal.aborted) return
        setFetchedSession(sess)
        if (syncedRef.current) return
        setMessages(mapServerMessagesToChat(rows))
        if (rows?.length) lastMessageIdRef.current = rows[rows.length - 1]?.id ?? ''
      })
      .catch((err) => {
        if (controller.signal.aborted) return
        const message = err instanceof ApiError
          ? (err.status === 401 || err.status === 403
            ? `You do not have access to this session (HTTP ${err.status})`
            : `Failed to load session (HTTP ${err.status})`)
          : 'Failed to load session'
        setError(message)
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [sessionId])

  // Sends a throttled `reconnect` to reconcile against the authoritative DB
  // state (used by the resync marker). This is the only frame the viewer sends.
  const requestResync = useCallback(() => {
    const now = Date.now()
    if (now - lastResyncRequestRef.current < 30000) return
    lastResyncRequestRef.current = now
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'reconnect', last_message_id: lastMessageIdRef.current }))
    }
  }, [])

  const openSubagent = useCallback(async (taskId: string) => {
    subagentViewRef.current = taskId
    setSubagentView(taskId)
    setSubagentMessages([])
    setSubagentError(null)
    setSubagentLoading(true)
    try {
      const data = await api.get<Array<Record<string, unknown>>>(`/api/v1/agents/subagent/${taskId}/messages`)
      if (subagentViewRef.current !== taskId) return
      setSubagentMessages(mapSubagentMessages(data))
    } catch (err) {
      if (subagentViewRef.current !== taskId) return
      const status = err instanceof ApiError ? ` (HTTP ${err.status})` : ''
      setSubagentError(`Failed to load subagent messages${status}`)
    } finally {
      if (subagentViewRef.current === taskId) setSubagentLoading(false)
    }
  }, [])

  const closeSubagent = useCallback(() => {
    subagentViewRef.current = null
    setSubagentView(null)
    setSubagentMessages([])
    setSubagentError(null)
  }, [])

  // Owner-only: flips notebook-viewer inheritance and mirrors the server's
  // response into local state. Errors propagate to the panel for inline display.
  const handleToggleInheritance = useCallback(async (next: boolean) => {
    const res = await api.patch<{ share_with_notebook_viewers?: boolean }>(
      `/api/v1/sessions/${sessionId}`,
      { share_with_notebook_viewers: next },
    )
    setInheritOverride(
      typeof res.share_with_notebook_viewers === 'boolean' ? res.share_with_notebook_viewers : next,
    )
  }, [sessionId])

  // Owner-only: attaches or detaches the session's notebook. The server clears
  // inheritance on detach, so the same response keeps both fields in sync.
  const handleNotebookChange = useCallback(async (next: string | null) => {
    const res = await api.patch<{ notebook_id?: string | null; share_with_notebook_viewers?: boolean }>(
      `/api/v1/sessions/${sessionId}`,
      { notebook_id: next },
    )
    setNotebookOverride(res.notebook_id ?? null)
    setInheritOverride(res.share_with_notebook_viewers === true)
  }, [sessionId])

  const connectWebSocket = useCallback(() => {
    suppressReconnectRef.current = false
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
    if (wsRef.current) {
      try { wsRef.current.close() } catch {}
      wsRef.current = null
    }
    const token = getToken()
    const adminParam = localStorage.getItem('aether_admin_mode') === 'true' ? '&admin_mode=true' : ''
    const ws = new WebSocket(WS_URL + sessionId + '?token=' + token + adminParam)
    wsRef.current = ws
    reconnectAttemptsRef.current = 0
    // A fresh connection restarts the resumable-stream clock: seqs are scoped
    // to the server's stream epoch, so a restart must not drop every event for
    // the lifetime of this connection.
    lastSeqRef.current = 0

    ws.onopen = () => {
      setConnected(true)
      setDisconnected(false)
      // Read-only connect: reconcile only, never set_* or an auth frame.
      ws.send(JSON.stringify({ type: 'reconnect', last_message_id: lastMessageIdRef.current }))
    }

    ws.onmessage = (event) => {
      const msg: WSMessage = JSON.parse(event.data)
      // Resumable stream: drop replayed/duplicated events. reconnect_sync
      // carries no seq and always applies; it jumps the stream clock forward.
      const seq = (msg as { seq?: unknown }).seq
      if (typeof seq === 'number') {
        if (seq <= lastSeqRef.current) return
        lastSeqRef.current = seq
      }
      switch (msg.type) {
        case 'token':
          setIsStreaming(true)
          setRetryNotice(null)
          setCurrentStreamingText((prev) => {
            const next = prev + msg.data
            streamingTextRef.current = next
            return next
          })
          break
        case 'reasoning':
          setIsStreaming(true)
          setCurrentStreamingReasoning((prev) => {
            const next = prev + msg.data
            streamingReasoningRef.current = next
            return next
          })
          break
        case 'tool_call':
          setMessages((prev) => [...prev, {
            id: crypto.randomUUID(),
            role: 'tool',
            content: msg.tool,
            tool_call_id: msg.tool_call_id,
            params: msg.params,
            reasoning: msg.reasoning || streamingReasoningRef.current || undefined,
            duration_ms: msg.duration_ms,
            created_at: new Date().toISOString(),
          }])
          if (streamingReasoningRef.current) {
            streamingReasoningRef.current = ''
            setCurrentStreamingReasoning('')
          }
          break
        case 'tool_result':
          setMessages((prev) => applyToolResult(prev, {
            tool: msg.tool,
            tool_call_id: msg.tool_call_id,
            params: msg.params,
            result: msg.result,
            error: msg.error,
            duration_ms: msg.duration_ms,
            tokens_direct: msg.tokens_direct,
          }))
          break
        case 'steering':
          if (msg.content) setMessages((prev) => applySteeringMessage(prev, msg.content))
          break
        case 'reconnect_sync': {
          syncedRef.current = true
          const rows = msg.messages
          const serverMsgs = mapServerMessagesToChat(rows)
          if (serverMsgs.length > 0) setMessages(serverMsgs)
          if (rows?.length) lastMessageIdRef.current = rows[rows.length - 1]?.id ?? lastMessageIdRef.current
          if (typeof msg.server_seq === 'number' && msg.server_seq > lastSeqRef.current) {
            lastSeqRef.current = msg.server_seq
          }
          streamingTextRef.current = ''
          setCurrentStreamingText('')
          streamingReasoningRef.current = ''
          setCurrentStreamingReasoning('')
          setRetryNotice(null)
          setIsStreaming(!!msg.running)
          break
        }
        case 'context_compacted':
          setMessages((prev) => [...prev, {
            id: crypto.randomUUID(),
            role: 'compaction',
            content: msg.summary,
            tokens_before: msg.tokens?.input ?? 0,
            tokens_after: msg.tokens?.context_current,
            created_at: new Date().toISOString(),
          }])
          break
        case 'done': {
          setIsStreaming(false)
          setRetryNotice(null)
          const durationMs = msg.data?.tokens?.duration_ms
          const finalText = streamingTextRef.current
          const finalReasoning = msg.data?.reasoning || streamingReasoningRef.current || undefined
          streamingTextRef.current = ''
          setCurrentStreamingText('')
          streamingReasoningRef.current = ''
          setCurrentStreamingReasoning('')
          if (finalText) {
            setMessages((prev) => [...prev, { id: crypto.randomUUID(), role: 'assistant', content: finalText, reasoning: finalReasoning, duration_ms: durationMs, created_at: new Date().toISOString() }])
          } else if (msg.data?.content) {
            setMessages((prev) => [...prev, { id: crypto.randomUUID(), role: 'assistant', content: msg.data?.content ?? '', reasoning: finalReasoning, duration_ms: durationMs, created_at: new Date().toISOString() }])
          }
          break
        }
        case 'subagent_status':
          setMessages((prev) => {
            const existing = prev.findIndex((m) => m.role === 'subagent' && m.content === msg.task_id)
            const entry: ChatMessage = {
              id: crypto.randomUUID(),
              role: 'subagent',
              content: msg.task_id,
              params: JSON.stringify({ goal: msg.goal, status: msg.status, error: msg.error }),
              result: msg.status === 'completed' || msg.status === 'failed' ? JSON.stringify(msg.result || msg.status) : undefined,
              duration_ms: msg.duration_ms,
              created_at: new Date().toISOString(),
            }
            if (existing >= 0) {
              const updated = [...prev]
              updated[existing] = entry
              return updated
            }
            return [...prev, entry]
          })
          break
        case 'subagent_message':
          if (subagentViewRef.current === msg.task_id) {
            const entry = mapSubagentMessage({ ...msg, created_at: (msg as { created_at?: string }).created_at || new Date().toISOString() })
            if (entry) {
              const mapped: ChatMessage = entry
              setSubagentMessages((prev) => [...prev, mapped])
            }
          }
          break
        case 'error':
          // Keep any partially streamed text, then append the failure: the
          // resync below reconciles against the persisted DB state.
          setIsStreaming(false)
          setRetryNotice(null)
          setMessages((prev) => {
            const next = [...prev]
            if (streamingTextRef.current) {
              next.push({ id: crypto.randomUUID(), role: 'assistant', content: streamingTextRef.current, created_at: new Date().toISOString() })
              streamingTextRef.current = ''
              setCurrentStreamingText('')
            }
            next.push({ id: crypto.randomUUID(), role: 'assistant', content: 'Error: ' + msg.message, created_at: new Date().toISOString() })
            return next
          })
          streamingReasoningRef.current = ''
          setCurrentStreamingReasoning('')
          requestResync()
          break
        case 'cancelled': {
          // Mirror AgentPanel: a cancelled turn keeps its partial text and
          // closes with a marker instead of silently dropping the stream.
          setIsStreaming(false)
          const cancelledText = streamingTextRef.current
          setMessages((prev) => [...prev, {
            id: crypto.randomUUID(),
            role: 'assistant',
            content: cancelledText ? cancelledText + '\n\n*[Cancelled]*' : '*[Cancelled]*',
            created_at: new Date().toISOString(),
          }])
          streamingTextRef.current = ''
          setCurrentStreamingText('')
          streamingReasoningRef.current = ''
          setCurrentStreamingReasoning('')
          break
        }
        case 'resync':
          requestResync()
          break
        case 'llm_retry':
          setRetryNotice({ attempt: msg.attempt ?? 0, max: msg.max_attempts ?? 0, error: msg.error ?? '' })
          break
        case 'tool_confirm_required':
        case 'question':
          // Read-only viewer: the session owner resolves these. Never reply.
          break
      }
    }

    ws.onclose = () => {
      setConnected(false)
      if (suppressReconnectRef.current) return
      wsRef.current = null
      if (reconnectAttemptsRef.current < 5) {
        const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 15000)
        reconnectAttemptsRef.current += 1
        reconnectTimerRef.current = setTimeout(() => {
          reconnectTimerRef.current = null
          connectWebSocket()
        }, delay)
      } else {
        setDisconnected(true)
      }
    }

    ws.onerror = () => {
      setConnected(false)
      setDisconnected(true)
    }
  }, [sessionId, requestResync])

  useEffect(() => {
    connectWebSocket()
    return () => {
      suppressReconnectRef.current = true
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current)
        reconnectTimerRef.current = null
      }
      if (wsRef.current) {
        wsRef.current.onopen = null
        wsRef.current.onclose = null
        wsRef.current.onerror = null
        wsRef.current.onmessage = null
        try { wsRef.current.close() } catch {}
        wsRef.current = null
      }
    }
  }, [connectWebSocket])

  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages, currentStreamingText, subagentMessages, subagentView])

  const title = session?.title || 'Agent session'
  const ownerEmail = session?.owner_email
  const canEdit = session?.can_edit === true

  return (
    <div
      style={styles.panel}
      role={page ? 'region' : 'dialog'}
      aria-labelledby={page ? 'session-viewer-title' : undefined}
      aria-label={page ? undefined : 'Shared agent session'}
    >
      <div style={styles.header}>
        <div style={styles.headerText}>
          {page ? (
            <h1 id="session-viewer-title" style={{ ...styles.title, margin: 0 }} title={title}>{title}</h1>
          ) : (
            <div style={styles.title} title={title}>{title}</div>
          )}
          {ownerEmail && <div style={styles.owner} title={ownerEmail}>{ownerEmail}</div>}
        </div>
        {connected ? (
          <span style={styles.live} title="Live updates">
            <span style={styles.liveDot} /> Live
          </span>
        ) : disconnected ? (
          <span style={styles.disconnected} title="Live updates stopped">
            <span style={styles.disconnectedDot} /> Disconnected
          </span>
        ) : null}
        {canEdit && (
          <button
            type="button"
            style={styles.shareBtn}
            onClick={() => { if (onShare) onShare(); else setShowPermissions(true) }}
            title="Share this session"
          >
            <Share2 size={13} /> Share
          </button>
        )}
        {onClose && (
          <button type="button" style={styles.closeBtn} onClick={onClose} title={page ? 'Back' : 'Close viewer'} aria-label={page ? 'Back' : 'Close viewer'}>
            {page ? <ArrowLeft size={14} /> : <X size={14} />}
          </button>
        )}
      </div>

      {!canEdit && <div style={styles.banner}>Shared · Read-only</div>}

      {subagentView ? (
        <>
          <div style={styles.subHeader}>
            <button type="button" style={styles.backBtn} onClick={closeSubagent}>
              <ArrowLeft size={12} /> Back
            </button>
            <span style={styles.subTitle}>Subagent {subagentView.slice(0, 8)}</span>
          </div>
          <div ref={scrollRef} style={styles.messageList}>
            {subagentError && <div style={styles.error}>{subagentError}</div>}
            <AgentChatTranscript
              messages={subagentMessages}
              leading={subagentLoading ? <div style={styles.loading}>Loading…</div> : undefined}
              emptyState={!subagentLoading && !subagentError ? (
                <div style={styles.empty}>This subagent hasn't produced any messages yet.</div>
              ) : undefined}
            />
          </div>
        </>
      ) : (
        <div ref={scrollRef} style={styles.messageList}>
          {error && <div style={styles.error}>{error}</div>}
          {loading ? (
            <div style={styles.loading}>Loading session…</div>
          ) : (
            <AgentChatTranscript
              messages={messages}
              onSubagentSelect={openSubagent}
              emptyState={error ? undefined : <div style={styles.empty}>No messages in this session yet.</div>}
              trailing={<>
                {isStreaming && !currentStreamingText && currentStreamingReasoning && (
                  <div data-testid="streaming-reasoning" style={{ ...chatStyles.message, ...chatStyles.reasoningMessage }}>
                    <div style={{ color: 'var(--text-muted)', fontSize: 11 }}>Thinking…</div>
                    <div style={{ marginTop: 6, whiteSpace: 'pre-wrap' }}>{currentStreamingReasoning}</div>
                  </div>
                )}
                {currentStreamingText && (
                  <div data-testid="streaming-text" style={{ ...chatStyles.message, ...chatStyles.assistantMessage }}>
                    <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]} components={chatMarkdownComponents}>{currentStreamingText}</ReactMarkdown>
                  </div>
                )}
                {retryNotice && (
                  <div style={styles.retry}>
                    Model call failed{retryNotice.error ? `: ${retryNotice.error.slice(0, 120)}` : ''} — retrying ({retryNotice.attempt}/{retryNotice.max || 3})…
                  </div>
                )}
              </>}
            />
          )}
        </div>
      )}

      {showPermissions && canEdit && session && (
        <PermissionsPanel
          resourceType="agent_session"
          resourceId={sessionId}
          resourceName={title}
          resourceOwnerId={session.user_id}
          canEdit={canEdit}
          sessionNotebookLink={{
            notebookId: session.notebook_id || null,
            onSave: handleNotebookChange,
          }}
          sessionNotebookInheritance={{
            enabled: session.share_with_notebook_viewers === true,
            hasNotebook: !!session.notebook_id,
            onToggle: handleToggleInheritance,
          }}
          onClose={() => setShowPermissions(false)}
        />
      )}
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: {
    display: 'flex',
    flexDirection: 'column',
    height: '100%',
    minHeight: 0,
    background: 'var(--bg-primary)',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '10px 14px',
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  headerText: { flex: 1, minWidth: 0 },
  title: {
    fontSize: 14,
    fontWeight: 600,
    color: 'var(--text-primary)',
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  owner: {
    fontSize: 11,
    color: 'var(--text-muted)',
    marginTop: 2,
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  live: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 10,
    color: 'var(--text-muted)',
    whiteSpace: 'nowrap',
  },
  liveDot: {
    display: 'inline-block',
    width: 6,
    height: 6,
    borderRadius: '50%',
    background: 'var(--success, #10b981)',
  },
  disconnected: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 10,
    color: 'var(--warning, #f59e0b)',
    whiteSpace: 'nowrap',
  },
  disconnectedDot: {
    display: 'inline-block',
    width: 6,
    height: 6,
    borderRadius: '50%',
    background: 'var(--warning, #f59e0b)',
  },
  shareBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    fontSize: 12,
    padding: '4px 10px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  closeBtn: {
    display: 'flex',
    alignItems: 'center',
    padding: 4,
    background: 'none',
    border: 'none',
    cursor: 'pointer',
    color: 'var(--text-muted)',
  },
  banner: {
    padding: '6px 14px',
    fontSize: 11,
    color: 'var(--text-muted)',
    background: 'var(--bg-secondary)',
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  messageList: {
    flex: 1,
    overflowY: 'auto',
    padding: 12,
    display: 'flex',
    flexDirection: 'column',
    gap: 8,
    minHeight: 0,
  },
  subHeader: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '8px 12px',
    borderBottom: '1px solid var(--border)',
    flexShrink: 0,
  },
  backBtn: {
    display: 'flex',
    alignItems: 'center',
    gap: 4,
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-primary)',
    fontSize: 12,
    padding: '4px 10px',
  },
  subTitle: { flex: 1, fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' },
  loading: { textAlign: 'center', padding: 20, color: 'var(--text-muted)', fontSize: 13 },
  empty: { textAlign: 'center', padding: 20, color: 'var(--text-muted)', fontSize: 13 },
  error: {
    padding: '8px 12px',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--error, #ef4444)',
    borderRadius: 6,
    color: 'var(--error, #ef4444)',
    fontSize: 13,
  },
  retry: { fontSize: 11, color: 'var(--warning, #f59e0b)', opacity: 0.9, margin: '4px 2px' },
}
