import { useState, useEffect, useRef, useCallback } from 'react'
import { Bot, Send, Loader2, History } from 'lucide-react'
import { api, getToken } from '../api/client'
import type { Agent, AgentTaskItem, WSMessage } from '../types/agent'
import { PanelHeader } from './PanelHeader'
import { SessionHistory } from './SessionHistory'
import { SlashCommandPicker } from './SlashCommandPicker'
import { TaskList } from './TaskList'

interface AgentPanelProps {
  notebookId: string
  onCellCreated?: (cellId: string, position: number) => void
  onCellScrollTo?: (cellId: string) => void
  onClose: () => void
}

const WS_URL = (import.meta.env.VITE_WS_URL || 'ws://localhost:8080') + '/api/v1/ws/agents/'
const LAST_AGENT_KEY = 'hnb:lastAgentId'

export function AgentPanel({ notebookId, onCellCreated, onCellScrollTo, onClose }: AgentPanelProps) {
  const [agents, setAgents] = useState<Agent[]>([])
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null)
  const [_sessionId, setSessionId] = useState<string | null>(null)
  const [messages, setMessages] = useState<Array<{ role: string; content: string; reasoning?: string | undefined; params?: string; result?: string }>>([])
  const [tasks, setTasks] = useState<AgentTaskItem[]>([])
  const [input, setInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const [currentStreamingText, setCurrentStreamingText] = useState('')
  const [currentStreamingReasoning, setCurrentStreamingReasoning] = useState('')
  const streamingReasoningRef = useRef('')
  const needsCollapseRef = useRef(false)

  const appendStreamingReasoning = (chunk: string) => {
    if (needsCollapseRef.current) {
      updateStreamingReasoning('')
      needsCollapseRef.current = false
    }
    const next = streamingReasoningRef.current + chunk
    streamingReasoningRef.current = next
    setCurrentStreamingReasoning(next)
  }

  const updateStreamingReasoning = (val: string) => {
    streamingReasoningRef.current = val
    setCurrentStreamingReasoning(val)
  }
  const [isLoadingAgents, setIsLoadingAgents] = useState(true)
  const [showHistory, setShowHistory] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showSlashPicker, setShowSlashPicker] = useState(false)
  const [pendingMessages, setPendingMessages] = useState<string[]>([])
  const wsRef = useRef<WebSocket | null>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const streamingTextRef = useRef('')
  const reconnectAttemptsRef = useRef(0)
  const processingRef = useRef(false)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    api.get<Agent[]>('/api/v1/agents')
      .then(setAgents)
      .catch(() => setError('Failed to load agents'))
      .finally(() => setIsLoadingAgents(false))
  }, [])

  useEffect(() => {
    return () => {
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    }
  }, [])

  useEffect(() => {
    if (!selectedAgent && agents.length > 0 && !isLoadingAgents) {
      const lastId = localStorage.getItem(LAST_AGENT_KEY)
      if (lastId) {
        const agent = agents.find((a) => a.id === lastId)
        if (agent) {
          startSession(agent)
        }
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agents, isLoadingAgents])

  const connectWebSocket = useCallback((sid: string) => {
    const token = getToken()
    // Note: JWT is sent as a query param because browsers don't support
    // setting WebSocket headers natively. The token is short-lived (15min).
    // If needed, migrate to first-message auth pattern.
    const ws = new WebSocket(WS_URL + sid + '?token=' + token)
    wsRef.current = ws
    reconnectAttemptsRef.current = 0

    ws.onmessage = (event) => {
      const msg: WSMessage = JSON.parse(event.data)

      switch (msg.type) {
        case 'token':
          setCurrentStreamingText((prev) => {
            const next = prev + msg.data
            streamingTextRef.current = next
            return next
          })
          break
        case 'reasoning':
          appendStreamingReasoning(msg.data)
          break
        case 'tool_call':
          setMessages((prev) => [...prev, { role: 'tool', content: msg.tool, reasoning: streamingReasoningRef.current || undefined }])
          if (streamingReasoningRef.current) {
            needsCollapseRef.current = true
          }
          break
        case 'tool_result':
          setMessages((prev) => {
            const updated = [...prev]
            for (let i = updated.length - 1; i >= 0; i--) {
              if (updated[i].role === 'tool' && updated[i].content === msg.tool) {
                updated[i] = { ...updated[i], params: msg.params, result: msg.error || msg.result }
                break
              }
            }
            return updated
          })
          break
        case 'cell_created':
          onCellCreated?.(msg.cell_id, msg.position)
          onCellScrollTo?.(msg.cell_id)
          break
        case 'done': {
          setIsStreaming(false)
          updateStreamingReasoning('')
          needsCollapseRef.current = false
          const finalText = streamingTextRef.current
          if (finalText) {
            const r = (msg as any).data?.reasoning as string | undefined
            setMessages((prev) => [...prev, { role: 'assistant', content: finalText, reasoning: r || undefined }])
            streamingTextRef.current = ''
            setCurrentStreamingText('')
          } else if (msg.data && 'content' in msg.data && msg.data.content) {
            const r = (msg as any).data?.reasoning as string | undefined
            const c = (msg.data as any).content as string
            setMessages((prev) => [...prev, { role: 'assistant', content: c, reasoning: r || undefined }])
          }
          setTimeout(() => inputRef.current?.focus(), 50)
          setPendingMessages((prev) => {
            if (prev.length > 0 && !processingRef.current && wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
              processingRef.current = true
              const next = prev[0]
              setTimeout(() => {
                setMessages((msgs) => [...msgs, { role: 'user', content: next }])
                wsRef.current?.send(JSON.stringify({ type: 'message', content: next }))
                setIsStreaming(true)
                streamingTextRef.current = ''
                setCurrentStreamingText('')
                processingRef.current = false
              }, 100)
              return prev.slice(1)
            }
            return prev
          })
          break
        }
        case 'error':
          setMessages((prev) => [...prev, { role: 'assistant', content: 'Error: ' + msg.message }])
          setIsStreaming(false)
          break
        case 'slash_result':
          if (msg.command === 'new') {
            setMessages([])
            setSessionId(null)
            setSelectedAgent(null)
          }
          break
        case 'tasks_updated':
          setTasks((prev) => {
            const incoming = msg.data as AgentTaskItem[]
            const merged = [...prev]
            for (const t of incoming) {
              const idx = merged.findIndex((m) => m.id === t.id)
              if (idx >= 0) {
                merged[idx] = { ...merged[idx], ...t }
              } else {
                merged.push(t)
              }
            }
            return merged
          })
          break
      }
    }

    ws.onclose = () => {
      wsRef.current = null
      if (reconnectAttemptsRef.current < 5) {
        const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 15000)
        reconnectAttemptsRef.current += 1
        reconnectTimerRef.current = setTimeout(() => {
          connectWebSocket(sid)
        }, delay)
      }
    }

    ws.onerror = () => {
      setError('WebSocket connection failed')
      setIsStreaming(false)
    }
  }, [onCellCreated, onCellScrollTo])

  const startSession = async (agent: Agent) => {
    try {
      const res = await api.post<{ session_id: string }>('/api/v1/agents/' + agent.id + '/session', {
        notebook_id: notebookId,
      })
      setSessionId(res.session_id)
      setSelectedAgent(agent)
      localStorage.setItem(LAST_AGENT_KEY, agent.id)
      setMessages([])
      connectWebSocket(res.session_id)
    } catch {
      setError('Failed to start session')
    }
  }

  const sendMessage = () => {
    if (!input.trim()) return
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      setError('Not connected. Attempting to reconnect...')
      return
    }

    if (isStreaming || pendingMessages.length > 0) {
      setPendingMessages((prev) => [...prev, input])
      setMessages((prev) => [...prev, { role: 'user', content: input }])
      setInput('')
      return
    }

    if (input.startsWith('/')) {
      const command = input.slice(1)
      wsRef.current.send(JSON.stringify({ type: 'slash_command', command }))
      setInput('')
      return
    }

    setMessages((prev) => [...prev, { role: 'user', content: input }])
    wsRef.current.send(JSON.stringify({ type: 'message', content: input }))
    setInput('')
    setIsStreaming(true)
    streamingTextRef.current = ''
    setCurrentStreamingText('')
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (showSlashPicker) {
        return
      }
      sendMessage()
    }
    if (e.key === ' ' && showSlashPicker) {
      setShowSlashPicker(false)
    }
  }

  useEffect(() => {
    if (messageListRef.current) {
      messageListRef.current.scrollTop = messageListRef.current.scrollHeight
    }
  }, [messages, currentStreamingText])

  useEffect(() => {
    if (selectedAgent) {
      const timer = setTimeout(() => inputRef.current?.focus(), 100)
      return () => clearTimeout(timer)
    }
  }, [selectedAgent])

  return (
    <div style={styles.panel}>
      <PanelHeader
        title={selectedAgent ? selectedAgent.name : 'AI Agent'}
        onClose={onClose}
        closeTitle="Close agent panel"
        style={{ borderBottom: '1px solid var(--border)', flexShrink: 0 }}
      />

      {showHistory && selectedAgent ? (
        <SessionHistory agentId={selectedAgent.id} onBack={() => setShowHistory(false)} />
      ) : !selectedAgent ? (
        <div style={styles.agentSelect}>
          {isLoadingAgents ? (
            <div style={styles.loading}>
              <Loader2 size={20} style={{ animation: 'spin 1s linear infinite' }} />
              <span>Loading agents...</span>
            </div>
          ) : agents.length === 0 ? (
            <div style={styles.empty}>No agents available</div>
          ) : (
            <select
              style={styles.select}
              value=""
              onChange={(e) => {
                const agent = agents.find((a) => a.id === e.target.value)
                if (agent) startSession(agent)
              }}
            >
              <option value="" disabled>Select an agent...</option>
              {agents.map((a) => (
                <option key={a.id} value={a.id}>{a.name}</option>
              ))}
            </select>
          )}
        </div>
      ) : (
        <>
          <div style={styles.agentInfo}>
            <Bot size={14} style={{ color: 'var(--accent)' }} />
            <span style={styles.agentName}>{selectedAgent.name}</span>
            <button
              style={styles.changeAgentBtn}
              onClick={() => {
                wsRef.current?.close()
                setSelectedAgent(null)
                setSessionId(null)
                setMessages([])
              }}
            >
              Change
            </button>
            <button
              style={styles.historyBtn}
              onClick={() => setShowHistory(true)}
              title="View chat history"
            >
              <History size={14} />
            </button>
          </div>

          <TaskList tasks={tasks} />

          <div ref={messageListRef} style={styles.messageList}>
            {messages.length === 0 && (
              <div style={styles.emptyState}>
                Ask me anything about this notebook. I can read cells, create new ones, run queries, and make charts.
              </div>
            )}
             {messages.map((msg, i) => (
               <div key={i}>
                 {msg.reasoning && (
                   <details style={{ ...styles.message, ...styles.reasoningMessage, marginBottom: 4 }}>
                     <summary style={{ cursor: 'pointer', color: 'var(--text-muted)', fontSize: 11 }}>Thinking</summary>
                     <div style={{ marginTop: 6, whiteSpace: 'pre-wrap' }}>{msg.reasoning}</div>
                   </details>
                 )}
                 {msg.role !== 'reasoning' && (
                   <div style={{ ...styles.message, ...(msg.role === 'user' ? styles.userMessage : msg.role === 'tool' ? styles.toolMessage : styles.assistantMessage) }}>
                     {msg.role === 'tool' ? (
                       <details>
                         <summary style={{ cursor: 'pointer', outline: 'none' }}>
                           <span style={{ opacity: 0.6, fontSize: 11 }}>TOOL </span>
                           {msg.content}
                         </summary>
                         <div style={{ marginTop: 6, fontSize: 11 }}>
                           {msg.params && (
                             <div style={{ marginBottom: 4 }}>
                               <span style={{ opacity: 0.5 }}>Params: </span>
                               <code style={{ fontSize: 10 }}>{msg.params}</code>
                             </div>
                           )}
                           {msg.result && (
                             <div>
                               <span style={{ opacity: 0.5 }}>Result: </span>
                               <code style={{ fontSize: 10, whiteSpace: 'pre-wrap' }}>{msg.result.length > 300 ? msg.result.slice(0, 300) + '...' : msg.result}</code>
                             </div>
                           )}
                         </div>
                       </details>
                     ) : (
                       msg.content
                     )}
                   </div>
                 )}
               </div>
             ))}
            {isStreaming && currentStreamingReasoning && (
               <details open style={{ ...styles.message, ...styles.reasoningMessage }}>
                 <summary style={{ cursor: 'pointer', color: 'var(--text-muted)', fontSize: 11 }}>Thinking</summary>
                 <div style={{ marginTop: 6, whiteSpace: 'pre-wrap' }}>{currentStreamingReasoning}</div>
               </details>
             )}
             {isStreaming && currentStreamingText && (
              <div style={{ ...styles.message, ...styles.assistantMessage }}>
                {currentStreamingText}
                <span style={styles.streamingDot} />
              </div>
            )}
            {error && <div style={styles.error}>{error}</div>}
          </div>

          <div style={{ ...styles.inputArea, position: 'relative' }}>
            {showSlashPicker && (
              <SlashCommandPicker
                filter={input}
                onSelect={(cmd) => {
                  setInput(cmd + ' ')
                  setShowSlashPicker(false)
                }}
                onClose={() => setShowSlashPicker(false)}
              />
            )}
            <textarea
              ref={inputRef}
              style={styles.input}
              value={input}
              onChange={(e) => {
                setInput(e.target.value)
                setShowSlashPicker(e.target.value.startsWith('/') && e.target.value.length <= 15)
              }}
              onKeyDown={handleKeyDown}
              placeholder="Message agent... (/ for commands)"
            />
            {pendingMessages.length > 0 && (
              <span style={styles.pendingBadge}>{pendingMessages.length}</span>
            )}
            <button
              style={{ ...styles.sendButton, ...(isStreaming ? styles.sendButtonDisabled : {}) }}
              onClick={sendMessage}
              disabled={!input.trim()}
            >
              {isStreaming ? <Loader2 size={16} style={{ animation: 'spin 1s linear infinite' }} /> : <Send size={16} />}
            </button>
          </div>
        </>
      )}

      <style>{`
        @keyframes spin {
          from { transform: rotate(0deg); }
          to { transform: rotate(360deg); }
        }
      `}</style>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  panel: {
    width: 360,
    height: '100%',
    borderLeft: '1px solid var(--border)',
    background: 'var(--bg-primary)',
    display: 'flex',
    flexDirection: 'column',
    flexShrink: 0,
  },
  agentSelect: {
    padding: 16,
  },
  select: {
    width: '100%',
    padding: '10px 12px',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    color: 'var(--text-primary)',
    fontSize: 14,
    cursor: 'pointer',
  },
  loading: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    padding: 20,
    color: 'var(--text-muted)',
    fontSize: 13,
  },
  empty: {
    textAlign: 'center',
    padding: 20,
    color: 'var(--text-muted)',
    fontSize: 13,
  },
  agentInfo: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '10px 16px',
    borderBottom: '1px solid var(--border-light)',
    background: 'var(--bg-secondary)',
  },
  agentName: {
    flex: 1,
    fontSize: 13,
    fontWeight: 500,
    color: 'var(--text-primary)',
  },
  changeAgentBtn: {
    fontSize: 11,
    padding: '3px 8px',
    background: 'none',
    border: '1px solid var(--border)',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
  },
  historyBtn: {
    padding: '3px 8px',
    background: 'none',
    border: 'none',
    borderRadius: 4,
    cursor: 'pointer',
    color: 'var(--text-secondary)',
    display: 'flex',
    alignItems: 'center',
  },
  messageList: {
    flex: 1,
    overflowY: 'auto',
    padding: 16,
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  emptyState: {
    textAlign: 'center',
    padding: 20,
    color: 'var(--text-muted)',
    fontSize: 13,
    lineHeight: 1.5,
  },
  message: {
    padding: '10px 14px',
    borderRadius: 8,
    fontSize: 14,
    lineHeight: 1.5,
    maxWidth: '85%',
    wordBreak: 'break-word',
  },
  userMessage: {
    background: 'var(--accent)',
    color: 'white',
    alignSelf: 'flex-end',
    borderBottomRightRadius: 2,
  },
  assistantMessage: {
    background: 'var(--bg-secondary)',
    color: 'var(--text-primary)',
    alignSelf: 'flex-start',
    borderBottomLeftRadius: 2,
  },
  toolMessage: {
    background: 'rgba(var(--accent-rgb, 59, 130, 246), 0.1)',
    color: 'var(--text-secondary)',
    alignSelf: 'flex-start',
    fontSize: 12,
    border: '1px solid rgba(var(--accent-rgb, 59, 130, 246), 0.2)',
    borderRadius: 6,
  },
  reasoningMessage: {
    background: 'var(--bg-secondary)',
    color: 'var(--text-secondary)',
    alignSelf: 'flex-start',
    fontSize: 12,
    borderLeft: '2px solid var(--text-muted)',
    borderRadius: 4,
  },
  streamingDot: {
    display: 'inline-block',
    width: 6,
    height: 6,
    borderRadius: '50%',
    background: 'var(--accent)',
    marginLeft: 8,
    animation: 'pulse 1s infinite',
  },
  error: {
    padding: '8px 12px',
    background: 'var(--bg-secondary)',
    border: '1px solid var(--error, #ef4444)',
    borderRadius: 6,
    color: 'var(--error, #ef4444)',
    fontSize: 13,
  },
  inputArea: {
    display: 'flex',
    gap: 8,
    padding: 12,
    borderTop: '1px solid var(--border)',
    background: 'var(--bg-secondary)',
  },
  input: {
    flex: 1,
    padding: '10px 12px',
    background: 'var(--bg-primary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    color: 'var(--text-primary)',
    fontSize: 14,
    resize: 'none',
    minHeight: 40,
    maxHeight: 120,
    fontFamily: 'inherit',
  },
  sendButton: {
    padding: '10px 12px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  sendButtonDisabled: {
    opacity: 0.5,
    cursor: 'not-allowed',
  },
  pendingBadge: {
    position: 'absolute',
    right: 52,
    top: -4,
    background: 'var(--accent)',
    color: 'white',
    fontSize: 10,
    fontWeight: 600,
    minWidth: 16,
    height: 16,
    borderRadius: 8,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '0 4px',
  },
}
