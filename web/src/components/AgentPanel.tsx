import { useState, useEffect, useRef, useCallback } from 'react'
import { Send, Loader2, History, Copy, Check, Square } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api, getToken } from '../api/client'
import type { Agent, AgentTaskItem, TokenBreakdown, WSMessage } from '../types/agent'
import { PanelHeader } from './PanelHeader'
import { SessionHistory } from './SessionHistory'
import { SlashCommandPicker } from './SlashCommandPicker'
import { TaskList } from './TaskList'

const headingSizes: Record<number, number> = { 1: 16, 2: 15, 3: 14, 4: 13 }
const headingStyle = (level: number): React.CSSProperties => ({
  margin: `${level <= 2 ? 12 : 8}px 0 ${level <= 2 ? 6 : 4}px 0`,
  fontWeight: 600,
  fontSize: headingSizes[level] || 14,
  lineHeight: 1.3,
})

export const chatMarkdownComponents = {
  h1: ({ children }: any) => <h1 style={headingStyle(1)}>{children}</h1>,
  h2: ({ children }: any) => <h2 style={headingStyle(2)}>{children}</h2>,
  h3: ({ children }: any) => <h3 style={headingStyle(3)}>{children}</h3>,
  h4: ({ children }: any) => <h4 style={headingStyle(4)}>{children}</h4>,
  p: ({ children }: any) => <p style={{ margin: '4px 0', lineHeight: 1.5 }}>{children}</p>,
  ul: ({ children }: any) => <ul style={{ margin: '4px 0', paddingLeft: 20 }}>{children}</ul>,
  ol: ({ children }: any) => <ol style={{ margin: '4px 0', paddingLeft: 20 }}>{children}</ol>,
  li: ({ children }: any) => <li style={{ margin: '2px 0' }}>{children}</li>,
  hr: () => <hr style={{ margin: '8px 0', border: 'none', borderTop: '1px solid var(--border)' }} />,
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
  messages: Array<{ role: string; content: string; reasoning?: string; params?: string; result?: string; created_at?: string }>
  tasks?: AgentTaskItem[]
  totalTokens?: TokenBreakdown
  maxTokens?: number
}

export function AgentPanel({ notebookId, width, onResize, onCellCreated, onCellOutput, onCellScrollTo, onClose, onMinimize }: AgentPanelProps) {
  const [agents, setAgents] = useState<Agent[]>([])
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null)
  const [_sessionId, setSessionId] = useState<string | null>(null)
  const [messages, setMessages] = useState<Array<{ role: string; content: string; reasoning?: string; params?: string; result?: string; created_at?: string }>>([])
  const chatStateKey = CHAT_STATE_KEY + (notebookId || '__global__')
  const [tasks, setTasks] = useState<AgentTaskItem[]>([])
  const [sessionTitle, setSessionTitle] = useState<string | null>(null)
  const [input, setInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const [currentStreamingText, setCurrentStreamingText] = useState('')
  const [currentStreamingReasoning, setCurrentStreamingReasoning] = useState('')
  const streamingReasoningRef = useRef('')
  const needsCollapseRef = useRef(false)
  const streamingStartedAt = useRef<string | null>(null)
  const [totalTokens, setTotalTokens] = useState<TokenBreakdown | null>(null)
  const ts = () => new Date().toISOString()
  const fmtTime = (iso?: string) => {
    if (!iso) return ''
    const d = new Date(iso)
    return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }
  const [maxTokens, setMaxTokens] = useState<number>(0)
  const [showTokenDetails, setShowTokenDetails] = useState(false)
  const [reasoningEffort, setReasoningEffort] = useState('')
  const [reasoningEffortOpts, setReasoningEffortOpts] = useState<string[]>([])
  const reasoningEffortRef = useRef('')
  reasoningEffortRef.current = reasoningEffort
  const [autoConfirmTool, setAutoConfirmTool] = useState(true)
  const autoConfirmRef = useRef(true)
  autoConfirmRef.current = autoConfirmTool
  const [pendingConfirm, setPendingConfirm] = useState<{ tool: string; args: string; currentSource?: string } | null>(null)

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
  const userScrolledAwayRef = useRef(false)
  selectedAgentRef.current = selectedAgent

  useEffect(() => {
    api.get<Agent[]>('/api/v1/agents')
      .then(setAgents)
      .catch(() => setError('Failed to load agents'))
      .finally(() => setIsLoadingAgents(false))
  }, [])

  useEffect(() => {
    if (selectedAgent) {
      const params = selectedAgent.model_config_params || {}
      const opts = params['reasoning_effort_options']
      setReasoningEffortOpts(Array.isArray(opts) ? opts as string[] : [])
      const def = params['reasoning_effort']
      const defaultEffort = typeof def === 'string' ? def : ''
      setReasoningEffort(defaultEffort)
    }
  }, [selectedAgent])

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
          setTasks(savedState.tasks || [])
          if (savedState.totalTokens) setTotalTokens(savedState.totalTokens)
          if (savedState.maxTokens) setMaxTokens(savedState.maxTokens)
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

  const saveChatState = (agentId: string, sessionId: string, msgs: Array<{ role: string; content: string; reasoning?: string; params?: string; result?: string; created_at?: string }>, tks?: AgentTaskItem[], tok?: TokenBreakdown, mTok?: number) => {
    try {
      localStorage.setItem(chatStateKey, JSON.stringify({ agentId, sessionId, messages: msgs, tasks: tks, totalTokens: tok, maxTokens: mTok }))
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

    ws.onopen = () => {
      const e = reasoningEffortRef.current
      if (e) {
        ws.send(JSON.stringify({ type: 'set_reasoning_effort', reasoning_effort: e }))
      }
    }

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
          setMessages((prev) => [...prev, { role: 'tool', content: msg.tool, reasoning: msg.reasoning || streamingReasoningRef.current || undefined, created_at: ts() }])
          if (streamingReasoningRef.current) {
            needsCollapseRef.current = true
            streamingReasoningRef.current = ''
          }
          break
        case 'tool_confirm_required':
          if (autoConfirmRef.current) {
            wsRef.current?.send(JSON.stringify({ type: 'tool_confirm', approved: true, content: msg.tool_name }))
          } else {
            setPendingConfirm({ tool: msg.tool_name, args: msg.tool_args, currentSource: (msg as any).current_source || '' })
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
            setMessages((prev) => [...prev, { role: 'assistant', content: finalText, reasoning: r || undefined, created_at: ts() }])
            streamingTextRef.current = ''
            setCurrentStreamingText('')
          } else if (msg.data && 'content' in msg.data && msg.data.content) {
            const r = (msg as any).data?.reasoning as string | undefined
            const c = (msg.data as any).content as string
            setMessages((prev) => [...prev, { role: 'assistant', content: c, reasoning: r || undefined, created_at: ts() }])
          }
          const tk = (msg as any).data?.tokens as TokenBreakdown | undefined
          if (tk && typeof tk.input === 'number') {
            setTotalTokens(prev => ({
              input: (prev?.input || 0) + tk.input,
              output: (prev?.output || 0) + tk.output,
              reasoning: (prev?.reasoning || 0) + (tk.reasoning || 0),
              system_prompt: (prev?.system_prompt || 0) + (tk.system_prompt || 0),
              skill_override: (prev?.skill_override || 0) + (tk.skill_override || 0),
              history: (prev?.history || 0) + (tk.history || 0),
              user_message: (prev?.user_message || 0) + (tk.user_message || 0),
              tool_definitions: (prev?.tool_definitions || 0) + (tk.tool_definitions || 0),
              tool_calls: (prev?.tool_calls || 0) + (tk.tool_calls || 0),
              tool_results: (prev?.tool_results || 0) + (tk.tool_results || 0),
            }))
          }
          setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 50)
          break
        }
        case 'error':
          setMessages((prev) => [...prev, { role: 'assistant', content: 'Error: ' + msg.message, created_at: ts() }])
          setIsStreaming(false)
          updateStreamingReasoning('')
          needsCollapseRef.current = false
          setTasks((prev) => prev.map((t) => t.status === 'in_progress' ? { ...t, status: 'pending' as const } : t))
          setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 50)
          break
        case 'cancelled':
          setIsStreaming(false)
          updateStreamingReasoning('')
          needsCollapseRef.current = false
          setTasks((prev) => prev.map((t) => t.status === 'in_progress' ? { ...t, status: 'pending' as const } : t))
          const cancelledText = streamingTextRef.current
          if (cancelledText) {
            setMessages((prev) => [...prev, { role: 'assistant', content: cancelledText + '\n\n*[Cancelled]*', created_at: ts() }])
          } else {
            setMessages((prev) => [...prev, { role: 'assistant', content: '*[Cancelled]*', created_at: ts() }])
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
              const summaryMsgs = [{ role: 'assistant', content: 'Previous session summary:\n\n' + data.summary, created_at: ts() }]
              setMessages(summaryMsgs)
              if (selectedAgentRef.current) {
                saveChatState(selectedAgentRef.current.id, data.session_id, summaryMsgs, undefined)
              }
            } else {
              const s = (msg.data as any).summary
              setMessages((prev) => [...prev, { role: 'assistant', content: s ? 'Summary: ' + s : JSON.stringify(msg.data), created_at: ts() }])
            }
          } else if (msg.data) {
            setMessages((prev) => [...prev, { role: 'assistant', content: JSON.stringify(msg.data, null, 2), created_at: ts() }])
          }
          break
        case 'reconnect_sync': {
          const serverMsgs = msg.messages as Array<{ role: string; content?: string; tool_calls?: Array<{ id: string; name: string; arguments: Record<string, unknown> }>; tool_call_id?: string; tokens_input?: number; tokens_output?: number; created_at?: string }>
          if (serverMsgs.length === 0) break
          const converted = serverMsgs.map((m) => {
            const base = { created_at: m.created_at || ts() }
            if (m.role === 'tool') {
              return { ...base, role: 'tool' as const, content: m.tool_call_id || 'tool', params: JSON.stringify(m.tool_calls?.[0]?.arguments || {}), result: m.content }
            }
            if (m.tool_calls && m.tool_calls.length > 0) {
              return { ...base, role: 'tool' as const, content: m.tool_calls.map(tc => tc.name).join(', '), params: JSON.stringify(m.tool_calls.map(tc => tc.arguments)), result: undefined }
            }
            return { ...base, role: m.role as 'user' | 'assistant' | 'tool', content: m.content || '' }
          })
          setMessages(converted)
          setTasks([])
          const ti = serverMsgs.reduce((sum, m) => sum + ((m as any).tokens_input || 0), 0)
          const to = serverMsgs.reduce((sum, m) => sum + ((m as any).tokens_output || 0), 0)
          const tr = serverMsgs.reduce((sum, m) => sum + ((m as any).tokens_reasoning || 0), 0)
          if (ti > 0 || to > 0) setTotalTokens({ input: ti, output: to, reasoning: tr, cache_read: 0, model_calls: 0, system_prompt: 0, skill_override: 0, history: 0, user_message: 0, tool_definitions: 0, tool_calls: 0, tool_results: 0 })
          break
        }
        case 'tasks_updated':
          setTasks((prev) => {
            const incoming = msg.data as AgentTaskItem[]
            const merged = [...prev]
            for (const t of incoming) {
              const idx = merged.findIndex((m) => m.id === t.id)
              if (idx >= 0) {
                merged[idx] = { ...merged[idx], ...t, ...(t.description ? {} : { description: merged[idx].description }) }
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
      const res = await api.post<{ session_id: string; max_tokens: number }>('/api/v1/agents/' + agent.id + '/session', {
        notebook_id: notebookId || null,
      })
      setSessionId(res.session_id)
      setSessionTitle(null)
      setSelectedAgent(agent)
      localStorage.setItem(LAST_AGENT_KEY, agent.id)
      setMessages([])
      setTotalTokens(null)
      setMaxTokens(res.max_tokens)
      saveChatState(agent.id, res.session_id, [], undefined, undefined, res.max_tokens)
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
      saveChatState(selectedAgent.id, sessionID, [], undefined)
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
      setMessages((prev) => [...prev, { role: 'user', content: text, created_at: ts() }])
      return
    }

    // /skill: prefix is handled as a regular message by the backend engine,
    // not as a slash command. Only send actual slash commands (/new, /summarize, etc.)
    // as slash_command type.
    if (text.startsWith('/') && !text.toLowerCase().startsWith('/skill:')) {
      const command = text.slice(1).trim()
      setMessages((prev) => [...prev, { role: 'user', content: text, created_at: ts() }])
      setIsStreaming(true)
      streamingStartedAt.current = ts()
      wsRef.current.send(JSON.stringify({ type: 'slash_command', command }))
      return
    }

    setMessages((prev) => [...prev, { role: 'user', content: text, created_at: ts() }])
    wsRef.current.send(JSON.stringify({ type: 'message', content: text }))
    setIsStreaming(true)
    streamingStartedAt.current = ts()
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
    const el = messageListRef.current
    if (!el) return
    const onScroll = () => {
      userScrolledAwayRef.current = el.scrollHeight - el.scrollTop - el.clientHeight > 80
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  }, [])

  useEffect(() => {
    const el = messageListRef.current
    if (el && !userScrolledAwayRef.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [messages, currentStreamingText, currentStreamingReasoning])

  useEffect(() => {
    if (_sessionId && selectedAgent && messages.length > 0) {
      saveChatState(selectedAgent.id, _sessionId, messages, tasks, totalTokens || undefined, maxTokens)
    }
  }, [messages, tasks, _sessionId, selectedAgent])

  useEffect(() => {
    if (isStreaming || pendingMessages.length === 0 || processingRef.current) return
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return

    processingRef.current = true
    const next = pendingMessages[0]
    const timer = setTimeout(() => {
      wsRef.current?.send(JSON.stringify({ type: 'message', content: next }))
      setPendingMessages((prev) => prev.slice(1))
      setIsStreaming(true)
      streamingStartedAt.current = ts()
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
        const newWidth = Math.max(280, Math.min(960, startWidth + delta))
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
              const msgs = await api.get<Array<{ role: string; content: string; reasoning_content?: string; created_at?: string }>>(`/api/v1/sessions/${session.id}/messages`)
              const formatted = msgs.map((m) => ({
                role: m.role,
                content: m.content || '',
                reasoning: m.reasoning_content,
                created_at: m.created_at,
              }))
              setMessages(formatted)
              if (selectedAgent) {
                saveChatState(selectedAgent.id, session.id, formatted, undefined)
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
            <div style={{ position: 'relative', width: '100%' }}>
              <select
                className="agent-select"
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
              <svg style={{ position: 'absolute', right: 10, top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none', color: 'var(--text-muted)' }} width={12} height={12} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                <path d="M6 9l6 6 6-6" />
              </svg>
            </div>
          )}
        </div>
      ) : (
        <>
          <div style={styles.agentInfo}>
            {reasoningEffortOpts.length > 0 && (
              <select
                className="agent-select"
                value={reasoningEffort}
                onChange={(e) => {
                  const val = e.target.value
                  setReasoningEffort(val)
                  wsRef.current?.send(JSON.stringify({ type: 'set_reasoning_effort', reasoning_effort: val }))
                }}
                style={{ fontSize: 11, padding: '2px 20px 2px 6px', background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-muted)', cursor: 'pointer' }}
                title="Reasoning effort"
              >
                <option value="">Effort: Default</option>
                {reasoningEffortOpts.map(o => (
                  <option key={o} value={o}>{o}</option>
                ))}
              </select>
            )}
            <label style={{ fontSize: 10, color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: 3, cursor: 'pointer', whiteSpace: 'nowrap' }} title="Auto-accept all tool calls">
              <input type="checkbox" checked={autoConfirmTool} onChange={e => setAutoConfirmTool(e.target.checked)} style={{ margin: 0 }} />
              Auto-Approve
            </label>
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
            {totalTokens && (
              <span style={{ position: 'relative' }}>
                <span
                  onClick={() => setShowTokenDetails(v => !v)}
                  style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 8, whiteSpace: 'nowrap', cursor: 'pointer', borderBottom: '1px dashed var(--text-muted)' }}
                >
                  {totalTokens.input.toLocaleString()}↑ / {totalTokens.output.toLocaleString()}↓
                  {maxTokens > 0 && (
                    <span style={{ marginLeft: 6, opacity: 0.6 }}>
                      ({Math.round((totalTokens.input + totalTokens.output) / maxTokens * 100)}%)
                    </span>
                  )}
                </span>
                {showTokenDetails && (
                  <>
                    <div
                      style={{ position: 'fixed', inset: 0, zIndex: 999 }}
                      onClick={() => setShowTokenDetails(false)}
                    />
                    <div style={{
                      position: 'absolute', top: '100%', right: 0, zIndex: 1000,
                      background: 'var(--bg-primary)', border: '1px solid var(--border)',
                      borderRadius: 8, padding: 12, minWidth: 280, marginTop: 4,
                      fontSize: 12, boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
                    }}>
                      <div style={{ fontWeight: 600, marginBottom: 8, color: 'var(--text-primary)' }}>Token Usage</div>

                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 4 }}>
                        <span style={{ color: 'var(--text-secondary)' }}>Input</span>
                        <span>{totalTokens.input.toLocaleString()}</span>
                      </div>
                      {totalTokens.cache_read > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2, paddingLeft: 16 }}>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Cache read</span>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.cache_read.toLocaleString()}</span>
                        </div>
                      )}

                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 4 }}>
                        <span style={{ color: 'var(--text-secondary)' }}>Output</span>
                        <span>{totalTokens.output.toLocaleString()}</span>
                      </div>
                      {totalTokens.reasoning > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 4, paddingLeft: 16 }}>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Reasoning</span>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.reasoning.toLocaleString()}</span>
                        </div>
                      )}

                      <div style={{ borderTop: '1px solid var(--border)', margin: '6px 0', paddingTop: 6, display: 'flex', justifyContent: 'space-between', gap: 24 }}>
                        <span style={{ color: 'var(--text-secondary)' }}>Total</span>
                        <span style={{ fontWeight: 600 }}>{(totalTokens.input + totalTokens.output).toLocaleString()}</span>
                      </div>
                      {totalTokens.model_calls > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, color: 'var(--text-muted)', fontSize: 11, marginTop: 4 }}>
                          <span>Model calls</span>
                          <span>{totalTokens.model_calls}</span>
                        </div>
                      )}
                      {maxTokens > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, color: 'var(--text-muted)', fontSize: 11 }}>
                          <span>Budget</span>
                          <span>{maxTokens.toLocaleString()} ({Math.round((totalTokens.input + totalTokens.output) / maxTokens * 100)}%)</span>
                        </div>
                      )}

                      {(totalTokens.system_prompt > 0 || totalTokens.tool_definitions > 0 || totalTokens.history > 0) && (
                        <div style={{ borderTop: '1px dashed var(--border)', margin: '6px 0', paddingTop: 6 }}>
                          <div style={{ color: 'var(--text-muted)', fontSize: 10, marginBottom: 4 }}>Estimated (tiktoken)</div>
                          {totalTokens.system_prompt > 0 && (
                            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2 }}>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>System prompt</span>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.system_prompt.toLocaleString()}</span>
                            </div>
                          )}
                          {totalTokens.skill_override > 0 && (
                            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2 }}>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Skill override</span>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.skill_override.toLocaleString()}</span>
                            </div>
                          )}
                          {totalTokens.history > 0 && (
                            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2 }}>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>History</span>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.history.toLocaleString()}</span>
                            </div>
                          )}
                          {totalTokens.user_message > 0 && (
                            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2 }}>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>User message</span>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.user_message.toLocaleString()}</span>
                            </div>
                          )}
                          {totalTokens.tool_definitions > 0 && (
                            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2 }}>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Tool definitions</span>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.tool_definitions.toLocaleString()}</span>
                            </div>
                          )}
                          {totalTokens.tool_calls > 0 && (
                            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2 }}>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Tool calls</span>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.tool_calls.toLocaleString()}</span>
                            </div>
                          )}
                          {totalTokens.tool_results > 0 && (
                            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2 }}>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Tool results</span>
                              <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.tool_results.toLocaleString()}</span>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  </>
                )}
              </span>
            )}
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
                      <details open={!isStreaming && i === messages.findLastIndex(m => !!m.reasoning)} style={{ ...styles.message, ...styles.reasoningMessage, marginBottom: 4 }}>
                        <summary style={{ cursor: 'pointer', color: 'var(--text-muted)', fontSize: 11 }}>Thinking</summary>
                        {msg.created_at && <div style={{ fontSize: 9, color: 'var(--text-muted)', opacity: 0.5, marginBottom: 4 }}>{fmtTime(msg.created_at)}</div>}
                        <div style={{ marginTop: 6, whiteSpace: 'pre-wrap' }}>{msg.reasoning}</div>
                      </details>
                    )}
                 {msg.role !== 'reasoning' && (
                     <div style={{ ...styles.message, ...(msg.role === 'user' ? styles.userMessage : msg.role === 'tool' ? styles.toolMessage : styles.assistantMessage) }}>
                      {msg.created_at && (
                        <div style={{ fontSize: 9, color: msg.role === 'user' ? 'rgba(255,255,255,0.5)' : 'var(--text-muted)', marginBottom: 4, textAlign: msg.role === 'user' ? 'right' : 'left' }}>
                          {fmtTime(msg.created_at)}
                        </div>
                      )}
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
              {isStreaming && !currentStreamingText && (
                  <details open style={{ ...styles.message, ...styles.reasoningMessage }}>
                    <summary style={{ cursor: 'pointer', color: 'var(--text-muted)', fontSize: 11 }}>Thinking</summary>
                    {streamingStartedAt.current && <div style={{ fontSize: 9, color: 'var(--text-muted)', opacity: 0.5, marginBottom: 4 }}>{fmtTime(streamingStartedAt.current)}</div>}
                    <div style={{ marginTop: 6, whiteSpace: 'pre-wrap' }}>
                      {currentStreamingReasoning || <span style={{ color: 'var(--text-muted)' }}>...</span>}
                    </div>
                  </details>
                )}
              {isStreaming && currentStreamingText && (
              <div style={{ ...styles.message, ...styles.assistantMessage }}>
                {streamingStartedAt.current && <div style={{ fontSize: 9, color: 'var(--text-muted)', opacity: 0.5, marginBottom: 4 }}>{fmtTime(streamingStartedAt.current)}</div>}
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

      {pendingConfirm && (
        <div style={{ position: 'absolute', inset: 0, zIndex: 100, background: 'rgba(0,0,0,0.4)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
          <div style={{ background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 8, padding: 16, maxWidth: '92%', minWidth: 300, fontSize: 13, maxHeight: '80%', display: 'flex', flexDirection: 'column' }}>
            <div style={{ fontWeight: 600, marginBottom: 8, color: 'var(--text-primary)' }}>Confirm Tool Call</div>
            <div style={{ marginBottom: 8, padding: '6px 8px', background: 'var(--bg-secondary)', borderRadius: 4, fontSize: 12, fontWeight: 600, color: 'var(--accent)' }}>{pendingConfirm.tool}</div>
            {pendingConfirm.currentSource && pendingConfirm.tool === 'update_cell' ? (() => {
              const newSource = (() => { try { return JSON.parse(pendingConfirm.args)?.source || '' } catch { return '' } })()
              const diff = computeDiff(pendingConfirm.currentSource, newSource)
              return (
                <div style={diffStyles.block}>
                  {diff.map((d, i) => (
                    <div key={i} style={{ ...diffStyles.line, ...(d.type === 'ctx' ? diffStyles.ctx : {}) }}>
                      <span style={diffStyles.num}>{d.num || ''}</span>
                      <span style={{ flex: 1, whiteSpace: 'pre' }}>
                        {d.charSpans ? d.charSpans.map((s, si) => (
                          <span key={si} style={{
                            ...(s.type === 'add' ? { ...diffStyles.addText, ...diffStyles.addBg } : {}),
                            ...(s.type === 'del' ? { ...diffStyles.delText, ...diffStyles.delBg } : {}),
                            borderRadius: 2, padding: '0 1px',
                          }}>{s.text}</span>
                        )) : d.line}
                      </span>
                    </div>
                  ))}
                </div>
              )
            })() : pendingConfirm.args && formatToolArgs(pendingConfirm.args)}
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 4 }}>
              <button onClick={() => {
                wsRef.current?.send(JSON.stringify({ type: 'tool_confirm', approved: false, content: pendingConfirm.tool }))
                setMessages((prev) => [...prev, { role: 'assistant', content: `⛔ Denied tool call: **${pendingConfirm.tool}**`, created_at: ts() }])
                setPendingConfirm(null)
              }} style={{ padding: '6px 14px', border: '1px solid var(--border)', borderRadius: 4, background: 'none', color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 }}>
                Deny
              </button>
              <button onClick={() => {
                wsRef.current?.send(JSON.stringify({ type: 'tool_confirm', approved: true, content: pendingConfirm.tool }))
                setMessages((prev) => [...prev, { role: 'assistant', content: `✅ Approved tool call: **${pendingConfirm.tool}**`, created_at: ts() }])
                setPendingConfirm(null)
              }} style={{ padding: '6px 14px', border: 'none', borderRadius: 4, background: 'var(--accent)', color: '#fff', cursor: 'pointer', fontWeight: 600, fontSize: 12 }}>
                Approve
              </button>
            </div>
          </div>
        </div>
      )}

      <style>{`
        @keyframes spin {
          from { transform: rotate(0deg); }
          to { transform: rotate(360deg); }
        }
        .agent-select { -webkit-appearance: none; -moz-appearance: none; appearance: none; }
      `}</style>
    </div>
  )
}

function formatToolArgs(args: string) {
  let parsed: Record<string, unknown> | null = null
  try { parsed = JSON.parse(args) } catch {}
  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    return (
      <div style={{ marginBottom: 8 }}>
        {Object.entries(parsed).map(([key, val]) => (
          <div key={key} style={{ display: 'flex', gap: 8, marginBottom: 3, fontSize: 12 }}>
            <span style={{ color: 'var(--text-muted)', minWidth: 80, flexShrink: 0 }}>{key}</span>
            <span style={{ color: 'var(--text-primary)', wordBreak: 'break-word' }}>{typeof val === 'string' ? val : JSON.stringify(val)}</span>
          </div>
        ))}
      </div>
    )
  }
  return <div style={{ marginBottom: 8, fontSize: 11, color: 'var(--text-muted)', maxHeight: 100, overflow: 'auto', background: 'var(--bg-secondary)', padding: 6, borderRadius: 4, whiteSpace: 'pre-wrap' }}>{args}</div>
}

type CharSpan = { text: string; type: 'same' | 'add' | 'del' }
function charDiff(oldStr: string, newStr: string): CharSpan[] {
  let i = 0
  while (i < oldStr.length && i < newStr.length && oldStr[i] === newStr[i]) i++
  const prefix = oldStr.slice(0, i)
  let j = 0
  while (j < oldStr.length - i && j < newStr.length - i && oldStr[oldStr.length - 1 - j] === newStr[newStr.length - 1 - j]) j++
  const suffix = oldStr.slice(oldStr.length - j)
  const oldMid = oldStr.slice(i, oldStr.length - j)
  const newMid = newStr.slice(i, newStr.length - j)
  const spans: CharSpan[] = []
  if (prefix) spans.push({ text: prefix, type: 'same' })
  if (oldMid) spans.push({ text: oldMid, type: 'del' })
  if (newMid) spans.push({ text: newMid, type: 'add' })
  if (suffix) spans.push({ text: suffix, type: 'same' })
  return spans
}

type DiffLine = { type: 'ctx' | 'changed'; line: string; num?: number; charSpans?: CharSpan[] }
function computeDiff(oldText: string, newText: string): DiffLine[] {
  const oldLines = oldText.split('\n')
  const newLines = newText.split('\n')
  const result: DiffLine[] = []
  let i = 0, j = 0
  while (i < oldLines.length || j < newLines.length) {
    if (i < oldLines.length && j < newLines.length && oldLines[i] === newLines[j]) {
      result.push({ type: 'ctx', line: oldLines[i], num: i + 1 })
      i++; j++
    } else if (j < newLines.length && i < oldLines.length) {
      result.push({ type: 'changed', line: newLines[j], num: j + 1, charSpans: charDiff(oldLines[i], newLines[j]) })
      i++; j++
    } else if (j < newLines.length) {
      result.push({ type: 'changed', line: newLines[j], num: j + 1, charSpans: [{ text: newLines[j], type: 'add' }] })
      j++
    } else if (i < oldLines.length) {
      result.push({ type: 'changed', line: oldLines[i], num: i + 1, charSpans: [{ text: oldLines[i], type: 'del' }] })
      i++
    }
  }
  return result
}

const diffStyles = {
  block: { background: 'var(--bg-secondary)', borderRadius: 4, fontSize: 10, fontFamily: 'var(--font-mono)', maxHeight: 200, overflow: 'auto', marginBottom: 8, whiteSpace: 'pre' },
  line: { display: 'flex', alignItems: 'flex-start', padding: '1px 4px', lineHeight: '16px' },
  ctx: { color: 'var(--text-muted)' },
  addBg: { background: 'rgba(34,197,94,0.15)' },
  delBg: { background: 'rgba(239,68,68,0.15)' },
  addText: { color: '#22c55e' },
  delText: { color: '#ef4444' },
  num: { width: 28, flexShrink: 0, textAlign: 'right' as const, paddingRight: 6, opacity: 0.6 },
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
    padding: '10px 32px 10px 12px',
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
