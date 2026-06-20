import { useState, useEffect, useRef, useCallback } from 'react'
import { Send, Loader2, History, Copy, Check, Square } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api, getToken } from '../api/client'
import type { Agent, AgentTaskItem, WSMessage } from '../types/agent'
import { PanelHeader } from './PanelHeader'
import { SessionHistory } from './SessionHistory'
import { SlashCommandPicker } from './SlashCommandPicker'
import { TaskList } from './TaskList'

export const chatMarkdownComponents = {
  table: ({ children }: any) => (
    <div style={{ overflowX: 'auto', margin: '4px 0' }}>
      <table style={{ borderCollapse: 'collapse', width: '100%', fontSize: 12 }}>{children}</table>
    </div>
  ),
  th: ({ children }: any) => (
    <th style={{ border: '1px solid var(--border)', padding: '6px 8px', textAlign: 'left', fontWeight: 600, background: 'var(--bg-elevated)' }}>
      {children}
    </th>
  ),
  td: ({ children }: any) => (
    <td style={{ border: '1px solid var(--border)', padding: '4px 8px' }}>{children}</td>
  ),
  code: ({ className, children, ...props }: any) => {
    const isInline = !className
    return isInline ? (
      <code style={{ background: 'var(--bg-elevated)', padding: '1px 4px', borderRadius: 3, fontSize: 11 }} {...props}>{children}</code>
    ) : (
      <code style={{ display: 'block', background: 'var(--bg-elevated)', padding: 8, borderRadius: 4, fontSize: 11, whiteSpace: 'pre-wrap', overflowX: 'auto' }} {...props}>{children}</code>
    )
  },
  pre: ({ children }: any) => <>{children}</>,
}

interface AgentPanelProps {
  notebookId?: string
  width: number
  onResize: (width: number) => void
  onCellCreated?: (cellId: string, position: number) => void
  onCellOutput?: (cellId: string, outputs: Array<{ type: string; data: unknown }>) => void
  onCellScrollTo?: (cellId: string) => void
  onClose: () => void
  onMinimize?: () => void
}

const WS_URL = (import.meta.env.VITE_WS_URL || 'ws://localhost:8080') + '/api/v1/ws/agents/'
const LAST_AGENT_KEY = 'hnb:lastAgentId'
const CHAT_STATE_KEY = 'hnb:agentChat:'

interface AgentChatState {
  agentId: string
  sessionId: string
  messages: Array<{ role: string; content: string; reasoning?: string; params?: string; result?: string }>
}

export function AgentPanel({ notebookId, width, onResize, onCellCreated, onCellOutput, onCellScrollTo, onClose, onMinimize }: AgentPanelProps) {
  const [agents, setAgents] = useState<Agent[]>([])
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null)
  const [_sessionId, setSessionId] = useState<string | null>(null)
  const [messages, setMessages] = useState<Array<{ role: string; content: string; reasoning?: string | undefined; params?: string; result?: string }>>([])
  const chatStateKey = CHAT_STATE_KEY + (notebookId || '__global__')
  const [tasks, setTasks] = useState<AgentTaskItem[]>([])
  const [sessionTitle, setSessionTitle] = useState<string | null>(null)
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
  const [copied, setCopied] = useState(false)
  const [pendingMessages, setPendingMessages] = useState<string[]>([])
  const wsRef = useRef<WebSocket | null>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const streamingTextRef = useRef('')
  const reconnectAttemptsRef = useRef(0)
  const processingRef = useRef(false)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const resizeRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const selectedAgentRef = useRef<Agent | null>(null)
  selectedAgentRef.current = selectedAgent

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
      const savedState = loadChatState()
      if (savedState) {
        const agent = agents.find((a) => a.id === savedState.agentId)
        if (agent) {
          setSelectedAgent(agent)
          setSessionId(savedState.sessionId)
          setMessages(savedState.messages)
          connectWebSocket(savedState.sessionId)
          return
        }
      }
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

  const loadChatState = (): AgentChatState | null => {
    try {
      const raw = localStorage.getItem(chatStateKey)
      if (raw) return JSON.parse(raw)
    } catch { /* ignore */ }
    return null
  }

  const saveChatState = (agentId: string, sessionId: string, msgs: Array<{ role: string; content: string; reasoning?: string; params?: string; result?: string }>) => {
    try {
      localStorage.setItem(chatStateKey, JSON.stringify({ agentId, sessionId, messages: msgs }))
    } catch { /* ignore */ }
  }

  const clearChatState = () => {
    try { localStorage.removeItem(chatStateKey) } catch { /* ignore */ }
  }

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
            streamingReasoningRef.current = ''
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
        case 'cell_output':
          onCellOutput?.(msg.cell_id, msg.outputs)
          onCellScrollTo?.(msg.cell_id)
          break
        case 'cell_updated':
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
          setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 50)
          break
        }
        case 'error':
          setMessages((prev) => [...prev, { role: 'assistant', content: 'Error: ' + msg.message }])
          setIsStreaming(false)
          updateStreamingReasoning('')
          needsCollapseRef.current = false
          setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 50)
          break
        case 'cancelled':
          setIsStreaming(false)
          updateStreamingReasoning('')
          needsCollapseRef.current = false
          const cancelledText = streamingTextRef.current
          if (cancelledText) {
            setMessages((prev) => [...prev, { role: 'assistant', content: cancelledText + '\n\n*[Cancelled]*' }])
          } else {
            setMessages((prev) => [...prev, { role: 'assistant', content: '*[Cancelled]*' }])
          }
          streamingTextRef.current = ''
          setCurrentStreamingText('')
          setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 50)
          break
        case 'slash_result':
          setIsStreaming(false)
          if (msg.command === 'new') {
            if (selectedAgentRef.current) {
              closeWS()
              startSession(selectedAgentRef.current)
            }
          } else if (msg.command === 'summarize' && msg.data) {
            const data = msg.data as { session_id: string; summary: string }
            if (data.session_id) {
              closeWS()
              connectToSession(data.session_id)
              const summaryMsgs = [{ role: 'assistant', content: 'Previous session summary:\n\n' + data.summary }]
              setMessages(summaryMsgs)
              if (selectedAgentRef.current) {
                saveChatState(selectedAgentRef.current.id, data.session_id, summaryMsgs)
              }
            } else {
              const s = (msg.data as any).summary
              setMessages((prev) => [...prev, { role: 'assistant', content: s ? 'Summary: ' + s : JSON.stringify(msg.data) }])
            }
          } else if (msg.data) {
            setMessages((prev) => [...prev, { role: 'assistant', content: JSON.stringify(msg.data, null, 2) }])
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
      if (reconnectTimerRef.current) return // closed intentionally, skip reconnect
      wsRef.current = null
      if (reconnectAttemptsRef.current < 5) {
        const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 15000)
        reconnectAttemptsRef.current += 1
        reconnectTimerRef.current = setTimeout(() => {
          reconnectTimerRef.current = null
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
        notebook_id: notebookId || null,
      })
      setSessionId(res.session_id)
      setSessionTitle(null)
      setSelectedAgent(agent)
      localStorage.setItem(LAST_AGENT_KEY, agent.id)
      setMessages([])
      saveChatState(agent.id, res.session_id, [])
      connectWebSocket(res.session_id)
    } catch {
      setError('Failed to start session')
    }
  }

  const connectToSession = (sessionID: string) => {
    setSessionId(sessionID)
    setSessionTitle(null)
    setMessages([])
    if (selectedAgent) {
      saveChatState(selectedAgent.id, sessionID, [])
    }
    connectWebSocket(sessionID)
  }

  const closeWS = () => {
    if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    reconnectTimerRef.current = setTimeout(() => {}, 0) // non-null sentinel to suppress reconnect
    wsRef.current?.close()
    wsRef.current = null
  }

  const cancelExecution = () => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ type: 'cancel' }))
    }
  }

  const sendText = (text: string, skipQueue = false) => {
    if (!text.trim()) return
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      setError('Not connected. Attempting to reconnect...')
      return
    }

    if (!skipQueue && (isStreaming || pendingMessages.length > 0)) {
      setPendingMessages((prev) => [...prev, text])
      setMessages((prev) => [...prev, { role: 'user', content: text }])
      return
    }

    // /skill: prefix is handled as a regular message by the backend engine,
    // not as a slash command. Only send actual slash commands (/new, /summarize, etc.)
    // as slash_command type.
    if (text.startsWith('/') && !text.toLowerCase().startsWith('/skill:')) {
      const command = text.slice(1).trim()
      setMessages((prev) => [...prev, { role: 'user', content: text }])
      setIsStreaming(true)
      wsRef.current.send(JSON.stringify({ type: 'slash_command', command }))
      return
    }

    setMessages((prev) => [...prev, { role: 'user', content: text }])
    wsRef.current.send(JSON.stringify({ type: 'message', content: text }))
    setIsStreaming(true)
    streamingTextRef.current = ''
    setCurrentStreamingText('')
  }

  const sendMessage = () => {
    const text = input
    setInput('')
    sendText(text)
  }

  const copyAsMarkdown = () => {
    if (messages.length === 0) return
    const lines: string[] = []
    for (const msg of messages) {
      if (msg.role === 'user') {
        lines.push(`**User:** ${msg.content}`)
      } else if (msg.role === 'assistant') {
        if (msg.reasoning) {
          lines.push(`> **Thinking:** ${msg.reasoning}`)
        }
        lines.push(`**Assistant:** ${msg.content}`)
      } else if (msg.role === 'tool') {
        if (msg.reasoning) {
          lines.push(`> **Thinking:** ${msg.reasoning}`)
        }
        lines.push(`**Tool: ${msg.content}**`)
        if (msg.params) lines.push(`  Params: \`${msg.params}\``)
        if (msg.result) lines.push(`  Result: \`${msg.result.length > 500 ? msg.result.slice(0, 500) + '...' : msg.result}\``)
      }
      lines.push('')
    }
    navigator.clipboard.writeText(lines.join('\n').trim()).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape' && isStreaming) {
      e.preventDefault()
      cancelExecution()
      return
    }
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
    if (_sessionId && selectedAgent && messages.length > 0) {
      saveChatState(selectedAgent.id, _sessionId, messages)
    }
  }, [messages, _sessionId, selectedAgent])

  useEffect(() => {
    if (isStreaming || pendingMessages.length === 0 || processingRef.current) return
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return

    processingRef.current = true
    const next = pendingMessages[0]
    const timer = setTimeout(() => {
      wsRef.current?.send(JSON.stringify({ type: 'message', content: next }))
      setPendingMessages((prev) => prev.slice(1))
      setIsStreaming(true)
      streamingTextRef.current = ''
      setCurrentStreamingText('')
      processingRef.current = false
    }, 100)

    return () => {
      clearTimeout(timer)
      processingRef.current = false
    }
  }, [isStreaming, pendingMessages])

  useEffect(() => {
    if (selectedAgent) {
      const timer = setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 100)
      return () => clearTimeout(timer)
    }
  }, [selectedAgent])

  useEffect(() => {
    if (!_sessionId) return
    setSessionTitle(null)
    api.get<any>(`/api/v1/sessions/${_sessionId}`)
      .then((session) => {
        if (session.title) {
          setSessionTitle(session.title)
        }
      })
      .catch(() => {})
  }, [_sessionId])

  useEffect(() => {
    const handle = resizeRef.current
    if (!handle) return

    let startX = 0
    let startWidth = 0

    const onMouseDown = (e: MouseEvent) => {
      e.preventDefault()
      startX = e.clientX
      startWidth = width
      document.body.style.cursor = 'col-resize'
      document.body.style.userSelect = 'none'

      const onMouseMove = (e: MouseEvent) => {
        const delta = startX - e.clientX
        const newWidth = Math.max(280, Math.min(600, startWidth + delta))
        onResize(newWidth)
      }

      const onMouseUp = () => {
        document.body.style.cursor = ''
        document.body.style.userSelect = ''
        document.removeEventListener('mousemove', onMouseMove)
        document.removeEventListener('mouseup', onMouseUp)
      }

      document.addEventListener('mousemove', onMouseMove)
      document.addEventListener('mouseup', onMouseUp)
    }

    handle.addEventListener('mousedown', onMouseDown)
    return () => handle.removeEventListener('mousedown', onMouseDown)
  }, [width, onResize])

  return (
    <div ref={panelRef} style={{ ...styles.panel, width }}>
      <div
        ref={resizeRef}
        style={styles.resizeHandle}
      />
      <PanelHeader
        title={sessionTitle || (selectedAgent ? selectedAgent.name : 'AI Agent')}
        onClose={onClose}
        onMinimize={onMinimize}
        closeTitle="Close agent panel"
        style={{ borderBottom: '1px solid var(--border)', flexShrink: 0 }}
      />

      {showHistory && selectedAgent ? (
        <SessionHistory
          agentId={selectedAgent.id}
          onBack={() => setShowHistory(false)}
          onResumeSession={async (session) => {
            closeWS()
            setSessionId(session.id)
            setShowHistory(false)
            try {
              const msgs = await api.get<Array<{ role: string; content: string; reasoning_content?: string }>>(`/api/v1/sessions/${session.id}/messages`)
              const formatted = msgs.map((m) => ({
                role: m.role,
                content: m.content || '',
                reasoning: m.reasoning_content,
              }))
              setMessages(formatted)
              if (selectedAgent) {
                saveChatState(selectedAgent.id, session.id, formatted)
              }
            } catch {
              setMessages([])
            }
            connectWebSocket(session.id)
          }}
        />
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
            <button
              style={styles.changeAgentBtn}
              onClick={() => {
                closeWS()
                setSelectedAgent(null)
                setSessionId(null)
                setMessages([])
                clearChatState()
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
            <button
              style={styles.historyBtn}
              onClick={copyAsMarkdown}
              title="Copy conversation as markdown"
              disabled={messages.length === 0}
            >
              {copied ? <Check size={14} style={{ color: 'var(--success, #10b981)' }} /> : <Copy size={14} />}
            </button>
          </div>

          <TaskList tasks={tasks} />

          <div ref={messageListRef} style={styles.messageList}>
            {messages.length === 0 && (
              <div style={styles.emptyState}>
                {notebookId
                  ? 'Ask me anything about this notebook. I can read cells, create new ones, run queries, and make charts.'
                  : 'Ask me anything. I can help with notebooks, queries, analysis, and more.'}
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
                        <ReactMarkdown remarkPlugins={[remarkGfm]} components={chatMarkdownComponents}>{msg.content}</ReactMarkdown>
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
              {isStreaming && !currentStreamingReasoning && !currentStreamingText && (
                <div style={{ ...styles.message, ...styles.assistantMessage, display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Loader2 size={14} style={{ animation: 'spin 1s linear infinite', color: 'var(--text-muted)' }} />
                  <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>Processing...</span>
                </div>
              )}
              {isStreaming && currentStreamingText && (
              <div style={{ ...styles.message, ...styles.assistantMessage }}>
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={chatMarkdownComponents}>{currentStreamingText}</ReactMarkdown>
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
                  setShowSlashPicker(false)
                  setInput('')
                  sendText(cmd, true)
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
                const val = e.target.value
                // Show picker for any / command, allow longer inputs for /skill: autocomplete
                const isSkillCommand = val.toLowerCase().startsWith('/skill:')
                setShowSlashPicker(val.startsWith('/') && (isSkillCommand || val.length <= 15))
              }}
              onKeyDown={handleKeyDown}
              placeholder="Message agent... (/ for commands)"
            />
            {pendingMessages.length > 0 && (
              <span style={styles.pendingBadge}>{pendingMessages.length}</span>
            )}
            {isStreaming ? (
              <button
                style={styles.cancelButton}
                onClick={cancelExecution}
                title="Cancel (Esc)"
              >
                <Square size={16} />
              </button>
            ) : (
              <button
                style={{ ...styles.sendButton, ...(!input.trim() ? styles.sendButtonDisabled : {}) }}
                onClick={sendMessage}
                disabled={!input.trim()}
              >
                <Send size={16} />
              </button>
            )}
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
    borderLeft: '1px solid var(--border)',
    background: 'var(--bg-primary)',
    display: 'flex',
    flexDirection: 'column',
    flexShrink: 0,
    height: '100%',
    minHeight: 0,
    overflow: 'hidden',
    position: 'relative',
  },
  resizeHandle: {
    position: 'absolute',
    left: -3,
    top: 0,
    bottom: 0,
    width: 6,
    cursor: 'col-resize',
    zIndex: 10,
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
    padding: '8px 16px',
    borderBottom: '1px solid var(--border-light)',
    background: 'var(--bg-secondary)',
    justifyContent: 'flex-end',
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
    flex: 1,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
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
  cancelButton: {
    padding: '10px 12px',
    background: 'var(--error, #ef4444)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
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
