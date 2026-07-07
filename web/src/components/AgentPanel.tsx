import { useState, useEffect, useLayoutEffect, useRef, useCallback, memo } from 'react'
import { Send, Loader2, History, Copy, Check, Square, X } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import { useQueryClient } from '@tanstack/react-query'
import { api, getToken } from '../api/client'
import type { Agent, AgentTaskItem, ModelConfig, TokenBreakdown, WSMessage } from '../types/agent'
import { AgentMessageImages } from './AgentMessageImages'
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
  pageContext?: { type: 'notebook' | 'dashboard' | 'files'; id?: string; title?: string }
  width: number
  onResize: (width: number) => void
  onClose: () => void
  onMinimize?: () => void
  onDock?: () => void
  docked?: boolean
}

const WS_URL = (import.meta.env.VITE_WS_URL || 'ws://localhost:8088') + '/api/v1/ws/agents/'
const LAST_AGENT_KEY = 'aether:lastAgentId'
const LAST_SESSION_KEY = 'aether:lastSessionId'
const CHAT_STATE_KEY = 'aether:agentChat:'

interface ChatMessage {
  id?: string
  role: string
  content: string
  reasoning?: string
  params?: string
  result?: string
  images?: string[]
  duration_ms?: number
  created_at?: string
}

interface PendingImage {
  id: string
  blobUrl: string
  filename: string
  uploading: boolean
}

interface AgentChatState {
  agentId: string
  sessionId: string
  messages: ChatMessage[]
  tasks?: AgentTaskItem[]
  totalTokens?: TokenBreakdown
  contextWindow?: number
  lastMessageId?: string
  modelConfigId?: string
}

export function AgentPanel({ notebookId, pageContext, width, onResize, onClose, onMinimize, onDock, docked }: AgentPanelProps) {
  const [agents, setAgents] = useState<Agent[]>([])
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null)
  const [_sessionId, _setSessionId] = useState<string | null>(null)
  const setSessionId = useCallback((sid: string | null) => {
    sessionIdRef.current = sid
    if (sid) localStorage.setItem(LAST_SESSION_KEY, sid)
    _setSessionId(sid)
  }, [])
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const chatStateKey = CHAT_STATE_KEY + '__global__'
  const [tasks, setTasks] = useState<AgentTaskItem[]>([])
  const [sessionTitle, setSessionTitle] = useState<string | null>(null)
  const [input, setInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const [currentStreamingText, setCurrentStreamingText] = useState('')
  const [currentStreamingReasoning, setCurrentStreamingReasoning] = useState('')
  const streamingReasoningRef = useRef('')
  const needsCollapseRef = useRef(false)
  const streamingStartedAt = useRef<string | null>(null)
  const [elapsed, setElapsed] = useState(0)
  const [thinkingOpen, setThinkingOpen] = useState(true)
  const [totalTokens, setTotalTokens] = useState<TokenBreakdown | null>(null)
  const [now, setNow] = useState(Date.now())
  const [subagentView, setSubagentView] = useState<string | null>(() => {
    try { return localStorage.getItem('aether:subagentView') } catch { return null }
  })
  const subagentViewRef = useRef<string | null>(null)
  const [subagentMessages, setSubagentMessages] = useState<ChatMessage[]>([])
  const [subagentTokens, setSubagentTokens] = useState<Record<string, {input: number, output: number}>>({})
  const [subagentLoading, setSubagentLoading] = useState(false)
  const mainScrollRef = useRef<number>(0)
  // Persist subagentView across page refreshes
  useEffect(() => {
    if (subagentView) localStorage.setItem('aether:subagentView', subagentView)
    else localStorage.removeItem('aether:subagentView')
  }, [subagentView])
  const subagentScrollRef = useRef<HTMLDivElement | null>(null)
  const hasPendingTools = messages.some(m => m.role === 'tool' && !m.result)
  // Auto-scroll subagent chat when new messages arrive
  useEffect(() => {
    if (subagentScrollRef.current) {
      subagentScrollRef.current.scrollTop = subagentScrollRef.current.scrollHeight
    }
  }, [subagentMessages])
  const fetchSubagentMessages = async (taskId: string, setter: (msgs: ChatMessage[]) => void, setLoading: (v: boolean) => void) => {
    setLoading(true)
    try {
      const res = await fetch(`/api/v1/agents/subagent/${taskId}/messages`, {
        headers: { Authorization: 'Bearer ' + getToken() }
      })
      if (!res.ok) return
      const data = await res.json()
      setter(data.flatMap((m: any) => {
        const fn = (tc: any) => tc.function || tc
        if (m.role === 'assistant' && m.tool_calls?.length) {
          const entries: ChatMessage[] = []
          if (m.reasoning_content || m.content) {
            const c = m.content || m.reasoning_content || ''
            entries.push({ role: 'assistant', content: c, reasoning: c === m.reasoning_content ? undefined : (m.reasoning_content || undefined), duration_ms: m.duration_ms, created_at: m.created_at })
          }
          return entries
        }
        if (m.role === 'tool') {
          let name = 'tool'
          let params: string | undefined
          let result = m.content || ''
          try { const p = JSON.parse(m.content); if (p.name) { name = p.name; result = p.result || result } } catch {}
          const f = fn(m.tool_calls?.[0])
          if (f?.name) name = f.name
          if (f?.arguments) params = typeof f.arguments === 'string' ? f.arguments : JSON.stringify(f.arguments)
          return [{ role: 'tool', content: name, params, result, duration_ms: m.duration_ms, created_at: m.created_at }]
        }
        const finalContent = m.content || (!m.tool_calls?.length ? (m.reasoning_content || '') : '')
        return [{
          role: m.role,
          content: finalContent,
          reasoning: (finalContent === m.reasoning_content) ? undefined : (m.reasoning_content || undefined),
          duration_ms: m.duration_ms,
          created_at: m.created_at,
        }]
      }))
    } catch {} finally { setLoading(false) }
  }
  useEffect(() => {
    if (!isStreaming || !hasPendingTools) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [isStreaming, hasPendingTools])
  useEffect(() => {
    if (!isStreaming || !streamingStartedAt.current) { setElapsed(0); return }
    const id = setInterval(() => {
      setElapsed(Math.floor((Date.now() - new Date(streamingStartedAt.current!).getTime()) / 1000))
    }, 1000)
    return () => clearInterval(id)
  }, [isStreaming])
  const ts = () => new Date().toISOString()
  const formatElapsed = (s: number) => s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`
  const fmtTime = (iso?: string) => {
    if (!iso) return ''
    const d = new Date(iso)
    return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }
  const costFmt = (compute: (mc: ModelConfig) => number): string => {
    const mc = modelConfigs.find(m => m.id === modelConfigId)
    if (!mc || (!mc.price_per_input_token && !mc.price_per_output_token)) return ''
    const c = compute(mc)
    if (c <= 0) return ''
    return `$${c.toFixed(c < 0.01 ? 6 : 4)}`
  }
  const [contextWindow, setContextWindow] = useState<number>(0)
  const [showTokenDetails, setShowTokenDetails] = useState(false)
  const [reasoningEffort, setReasoningEffort] = useState('')
  const [modelConfigs, setModelConfigs] = useState<ModelConfig[]>([])
  const [modelConfigId, setModelConfigId] = useState('')
  const modelConfigIdRef = useRef('')
  modelConfigIdRef.current = modelConfigId
  const [reasoningEffortOpts, setReasoningEffortOpts] = useState<string[]>([])
  const reasoningEffortRef = useRef('')
  reasoningEffortRef.current = reasoningEffort
  const [autoConfirmTool, setAutoConfirmTool] = useState(true)
  const autoConfirmRef = useRef(true)
  autoConfirmRef.current = autoConfirmTool
  const [pendingConfirm, setPendingConfirm] = useState<{ tool: string; args: string; currentSource?: string } | null>(null)
  const sessionApprovedRef = useRef<Set<string>>(new Set())
  const chatClearedRef = useRef(false)

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
  const sessionIdRef = useRef<string | null>(null)
  const [isLoadingAgents, setIsLoadingAgents] = useState(true)
  const [showHistory, setShowHistory] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showSlashPicker, setShowSlashPicker] = useState(false)
  const [copied, setCopied] = useState(false)
  const [pendingMessages, setPendingMessages] = useState<string[]>([])
  const [pendingImages, setPendingImages] = useState<PendingImage[]>([])
  const pendingImagesRef = useRef<PendingImage[]>([])
  pendingImagesRef.current = pendingImages

  const uploadAgentImage = useCallback(async (file: File, sessionId: string): Promise<string> => {
    const form = new FormData()
    form.append('file', file)
    const res = await fetch(`/api/v1/agent-sessions/${sessionId}/attachments`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${getToken()}` },
      body: form,
    })
    if (!res.ok) throw new Error(`upload failed: ${res.status}`)
    const data = await res.json()
    return data.id as string
  }, [])

  const addPendingImage = useCallback((file: File) => {
    const blobUrl = URL.createObjectURL(file)
    const pid: PendingImage = { id: '', blobUrl, filename: file.name, uploading: true }
    setPendingImages(prev => [...prev, pid])
    // Upload immediately
    const sid = sessionIdRef.current
    if (!sid) {
      setPendingImages(prev => prev.map(p => p.blobUrl === blobUrl ? { ...p, uploading: false } : p))
      return
    }
    uploadAgentImage(file, sid).then(attId => {
      setPendingImages(prev => prev.map(p => p.blobUrl === blobUrl ? { ...p, id: attId, uploading: false } : p))
    }).catch(() => {
      setPendingImages(prev => prev.map(p => p.blobUrl === blobUrl ? { ...p, uploading: false } : p))
    })
  }, [uploadAgentImage])

  const removePendingImage = useCallback((blobUrl: string) => {
    setPendingImages(prev => prev.filter(p => p.blobUrl !== blobUrl))
    URL.revokeObjectURL(blobUrl)
  }, [])

  // Register native paste listener when session is active
  useEffect(() => {
    if (!inputRef.current) return
    const el = inputRef.current
    const handler = (e: Event) => {
      const ce = e as ClipboardEvent
      if (!ce.clipboardData) return
      for (let i = 0; i < ce.clipboardData.items.length; i++) {
        if (ce.clipboardData.items[i].kind === 'file' && ce.clipboardData.items[i].type.startsWith('image/')) {
          ce.preventDefault()
          const file = ce.clipboardData.items[i].getAsFile()
          if (file) { addPendingImage(file); return }
        }
      }
      for (let i = 0; i < ce.clipboardData.files.length; i++) {
        if (ce.clipboardData.files[i].type.startsWith('image/')) {
          ce.preventDefault()
          addPendingImage(ce.clipboardData.files[i])
          return
        }
      }
    }
    el.addEventListener('paste', handler)
    return () => el.removeEventListener('paste', handler)
  }, [_sessionId])

  const handleDrop = useCallback((e: React.DragEvent<HTMLDivElement>) => {
    const files = Array.from(e.dataTransfer.files).filter(f => f.type.startsWith('image/'))
    if (files.length === 0) return
    e.preventDefault()
    for (const file of files) {
      addPendingImage(file)
    }
  }, [addPendingImage])

  const handleDragOver = useCallback((e: React.DragEvent<HTMLDivElement>) => {
    if (Array.from(e.dataTransfer.types).includes('Files')) {
      e.preventDefault()
    }
  }, [])

  const wsRef = useRef<WebSocket | null>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const streamingTextRef = useRef('')
  const reconnectAttemptsRef = useRef(0)
  const processingRef = useRef(false)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const sessionReqIdRef = useRef(0)
  const resizeRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const selectedAgentRef = useRef<Agent | null>(null)
  const lastScrollTimeRef = useRef(0)
  const prevScrollHeightRef = useRef(0)
  const isProgrammaticScrollRef = useRef(false)
  const forceScrollRef = useRef(false)
  const wasAtBottomRef = useRef(false)
  selectedAgentRef.current = selectedAgent

  const queryClient = useQueryClient()

  const scrollIntervalsRef = useRef<Set<ReturnType<typeof setInterval>>>(new Set())
  useEffect(() => {
    return () => {
      for (const id of scrollIntervalsRef.current) clearInterval(id)
      scrollIntervalsRef.current.clear()
    }
  }, [])

  const scrollToCell = useCallback((cellId: string) => {
    let attempts = 0
    const interval = setInterval(() => {
      const el = document.getElementById('cell-' + cellId)
      if (el) {
        clearInterval(interval)
        scrollIntervalsRef.current.delete(interval)
        el.scrollIntoView({ behavior: 'smooth', block: 'center' })
        el.classList.add('cell-flash')
        setTimeout(() => el.classList.remove('cell-flash'), 3000)
      } else if (++attempts >= 50) {
        clearInterval(interval)
        scrollIntervalsRef.current.delete(interval)
      }
    }, 100)
    scrollIntervalsRef.current.add(interval)
  }, [])

  useEffect(() => {
    api.get<Agent[]>('/api/v1/agents')
      .then(setAgents)
      .catch(() => setError('Failed to load agents'))
      .finally(() => setIsLoadingAgents(false))
    api.get<ModelConfig[]>('/api/v1/model-configs')
      .then(setModelConfigs)
      .catch(() => {})
  }, [])

  const MODEL_CONFIG_KEY = 'aether:lastModelConfigId'
  const REASONING_EFFORT_KEY = 'aether:lastReasoningEffort'

  useEffect(() => {
    if (selectedAgent) {
      const params = selectedAgent.model_config_params || {}
      const opts = params['reasoning_effort_options']
      setReasoningEffortOpts(Array.isArray(opts) ? opts as string[] : [])
      const def = params['reasoning_effort']
      const defaultEffort = typeof def === 'string' ? def : ''
      setModelConfigId(selectedAgent.model_config_id ?? '')
      setReasoningEffort(defaultEffort)
      try {
        const savedModelConfig = localStorage.getItem(MODEL_CONFIG_KEY)
        if (savedModelConfig) setModelConfigId(savedModelConfig)
        const savedEffort = localStorage.getItem(REASONING_EFFORT_KEY)
        if (savedEffort) setReasoningEffort(savedEffort)
      } catch { /* ignore */ }
    }
  }, [selectedAgent])

  useEffect(() => {
    return () => {
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
      if (wsRef.current) {
        wsRef.current.onclose = null
        wsRef.current.onerror = null
        wsRef.current.onmessage = null
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [])

  // Send page context to backend when it changes
  useEffect(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN && pageContext) {
      wsRef.current.send(JSON.stringify({
        type: 'set_page_context',
        page_context: { type: pageContext.type, id: pageContext.id || '', title: pageContext.title || '' },
      }))
    }
  }, [pageContext])

  useEffect(() => {
    if (!selectedAgent && agents.length > 0 && !isLoadingAgents) {
      const savedState = loadChatState()
      const lastSessionId = localStorage.getItem(LAST_SESSION_KEY)

      // 1) Full restore: saved state with matching agent + messages
      if (savedState && savedState.messages && savedState.messages.length > 0) {
        const agent = agents.find((a) => a.id === savedState.agentId)
        if (agent) {
          setSelectedAgent(agent)
          setSessionId(savedState.sessionId)
          setMessages(savedState.messages)
          setTasks(savedState.tasks || [])
          if (savedState.totalTokens) setTotalTokens(savedState.totalTokens)
          if (savedState.contextWindow) setContextWindow(savedState.contextWindow)
          forceScrollRef.current = true
          connectWebSocket(savedState.sessionId)
          return
        }
      }

      // 2) Reconnect to last session even without saved messages
      if (lastSessionId && savedState?.agentId) {
        const agent = agents.find((a) => a.id === savedState.agentId)
        if (agent) {
          setSelectedAgent(agent)
          setSessionId(lastSessionId)
          setMessages([])
          forceScrollRef.current = true
          connectWebSocket(lastSessionId)
          return
        }
      }

      // 3) Fallback: start a brand new session
      const lastId = localStorage.getItem(LAST_AGENT_KEY)
      if (lastId) {
        const agent = agents.find((a) => a.id === lastId)
        if (agent) {
          startSession(agent)
        }
      }
    }
    return () => {
      sessionReqIdRef.current++ // invalidate any in-flight startSession
      if (wsRef.current) {
        reconnectTimerRef.current = setTimeout(() => {}, 0)
        try { wsRef.current.close() } catch {}
        wsRef.current = null
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

  const saveChatState = (agentId: string, sessionId: string, msgs: ChatMessage[], tks?: AgentTaskItem[], tok?: TokenBreakdown, cw?: number) => {
    try {
      const lastId = msgs.length > 0 ? msgs[msgs.length - 1].id : undefined
      localStorage.setItem(chatStateKey, JSON.stringify({ agentId, sessionId, messages: msgs, tasks: tks, totalTokens: tok, contextWindow: cw, lastMessageId: lastId, modelConfigId: modelConfigIdRef.current || undefined }))
    } catch { /* ignore */ }
  }

  const clearChatState = () => {
    try { localStorage.removeItem(chatStateKey) } catch { /* ignore */ }
    setSubagentTokens({})
  }

  const connectWebSocket = useCallback((sid: string) => {
    // Close any existing connection, suppressing its reconnect logic
    if (wsRef.current) {
      reconnectTimerRef.current = setTimeout(() => {}, 0)
      try { wsRef.current.close() } catch {}
      wsRef.current = null
    }
    const token = getToken()
    const ws = new WebSocket(WS_URL + sid + '?token=' + token)
    wsRef.current = ws
    reconnectAttemptsRef.current = 0

    ws.onopen = () => {
        const e = reasoningEffortRef.current
        if (e) { ws.send(JSON.stringify({ type: 'set_reasoning_effort', reasoning_effort: e })) }
        if (pageContext) { ws.send(JSON.stringify({ type: 'set_page_context', page_context: { type: pageContext.type, id: pageContext.id || '', title: pageContext.title || '' } })) }
        const savedState = loadChatState()
        const lastId = savedState?.lastMessageId || ''
        ws.send(JSON.stringify({ type: 'reconnect', last_message_id: lastId }))
      }

      ws.onmessage = (event) => {
        const msg: WSMessage = JSON.parse(event.data)
        switch (msg.type) {
          case 'token':
            setIsStreaming(true)
            setCurrentStreamingText((prev) => { const next = prev + msg.data; streamingTextRef.current = next; return next })
            break
          case 'reasoning':
            setIsStreaming(true); appendStreamingReasoning(msg.data); break
          case 'tool_call':
            setMessages((prev) => [...prev, { role: 'tool', content: msg.tool, params: msg.params, reasoning: msg.reasoning || streamingReasoningRef.current || undefined, duration_ms: msg.duration_ms, created_at: ts() }])
            if (streamingReasoningRef.current) { needsCollapseRef.current = true; updateStreamingReasoning('') }
            break
          case 'tool_confirm_required':
            if (autoConfirmRef.current || sessionApprovedRef.current.has(msg.tool_name)) {
              wsRef.current?.send(JSON.stringify({ type: 'tool_confirm', approved: true, content: msg.tool_name }))
            } else {
              setPendingConfirm({ tool: msg.tool_name, args: msg.tool_args, currentSource: (msg as any).current_source || '' })
            }
            break
          case 'tool_result':
            setMessages((prev) => { const updated = [...prev]; for (let i = updated.length - 1; i >= 0; i--) { if (updated[i].role === 'tool' && updated[i].content === msg.tool) { updated[i] = { ...updated[i], params: msg.params, result: msg.error || msg.result, duration_ms: msg.duration_ms }; break } }; return updated }); break
          case 'cell_created':
            if (notebookId) queryClient.invalidateQueries({ queryKey: ['notebook', notebookId] }); scrollToCell(msg.cell_id); break
          case 'cell_output':
            if (notebookId) queryClient.setQueryData(['notebook', notebookId], (old: any) => old ? { ...old, cells: old.cells.map((c: any) => c.id === msg.cell_id ? { ...c, outputs: msg.outputs as any[] } : c) } : old); scrollToCell(msg.cell_id); break
          case 'cell_updated': scrollToCell(msg.cell_id); break
          case 'reconnect_sync': {
            const _fn = (tc: any) => tc?.function || tc
            const serverMsgs: ChatMessage[] = (msg.messages || []).map((m: any) => {
              const base: ChatMessage = { id: m.id, role: m.role, content: m.content || '', images: m.image_ids?.length ? m.image_ids : undefined, created_at: m.created_at }
              if (m.role === 'subagent') {
                const tc = m.tool_calls?.[0]
                base.content = m.content || ''
                base.params = JSON.stringify({ goal: tc?.name || '', status: tc?.arguments?.status || 'completed', error: tc?.arguments?.error || '' })
                base.result = tc?.arguments?.status === 'completed' || tc?.arguments?.status === 'failed' ? JSON.stringify(tc?.arguments?.result || tc?.arguments?.status) : undefined
              } else if (m.tool_calls?.length) {
                const tc = _fn(m.tool_calls[0])
                base.content = tc.name || 'tool'
                base.params = tc.arguments ? (typeof tc.arguments === 'string' ? tc.arguments : JSON.stringify(tc.arguments)) : undefined
                base.result = m.tool_calls[0].result !== undefined ? JSON.stringify(m.tool_calls[0].result) : undefined
                base.role = 'tool'
                base.duration_ms = m.duration_ms
              } else if (m.role === 'tool') {
                base.result = m.content || ''
                base.duration_ms = m.duration_ms
              }
              if (m.duration_ms) base.duration_ms = m.duration_ms
              return base
            })
            if (serverMsgs.length > 0) setMessages(serverMsgs)
            streamingTextRef.current = ''; setCurrentStreamingText('')
            break
          }
          case 'done': {
            setIsStreaming(false); updateStreamingReasoning(''); needsCollapseRef.current = false
            const tk = (msg as any).data?.tokens as TokenBreakdown | undefined
            const dm = tk?.duration_ms
            const finalText = streamingTextRef.current
            if (finalText) { setMessages((prev) => [...prev, { role: 'assistant', content: finalText, reasoning: ((msg as any).data?.reasoning as string) || undefined, duration_ms: dm, created_at: ts() }]); streamingTextRef.current = ''; setCurrentStreamingText('') }
            else if (msg.data && 'content' in msg.data && msg.data.content) { setMessages((prev) => [...prev, { role: 'assistant', content: (msg.data as any).content, reasoning: ((msg.data as any)?.reasoning as string) || undefined, duration_ms: dm, created_at: ts() }]) }
            if (tk && typeof tk.input === 'number') setTotalTokens(prev => ({ input: (prev?.input || 0) + tk.input, output: (prev?.output || 0) + tk.output, reasoning: (prev?.reasoning || 0) + (tk.reasoning || 0), cache_read: (prev?.cache_read || 0) + (tk.cache_read || 0), model_calls: (prev?.model_calls || 0) + (tk.model_calls || 0), system_prompt: (prev?.system_prompt || 0) + (tk.system_prompt || 0), skill_override: (prev?.skill_override || 0) + (tk.skill_override || 0), history: (prev?.history || 0) + (tk.history || 0), user_message: (prev?.user_message || 0) + (tk.user_message || 0), tool_definitions: (prev?.tool_definitions || 0) + (tk.tool_definitions || 0), tool_calls: (prev?.tool_calls || 0) + (tk.tool_calls || 0), tool_results: (prev?.tool_results || 0) + (tk.tool_results || 0), subagent_input: prev?.subagent_input, subagent_output: prev?.subagent_output }))
            setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 50); break
          }
          case 'subagent_message':
            if (subagentViewRef.current === msg.task_id) {
              const fn = (tc: any) => tc?.function || tc
              setSubagentMessages((prev) => {
                const next = [...prev]
                if (msg.role === 'assistant' && msg.tool_calls?.length) {
                  if (msg.reasoning_content || msg.content) {
                    const c = msg.content || msg.reasoning_content || ''
                    next.push({ role: 'assistant', content: c, reasoning: c === msg.reasoning_content ? undefined : (msg.reasoning_content || undefined), duration_ms: msg.duration_ms, created_at: new Date().toISOString() })
                  }
                } else if (msg.role === 'tool') {
                  let name = 'tool'
                  let result = msg.content || ''
                  try { const p = JSON.parse(msg.content); if (p.name) { name = p.name; result = p.result || result } } catch {}
                  const f = fn(msg.tool_calls?.[0])
                  if (f?.name) name = f.name
                  const params = f?.arguments ? (typeof f.arguments === 'string' ? f.arguments : JSON.stringify(f.arguments)) : undefined
                  next.push({ role: 'tool', content: name, params, result, duration_ms: msg.duration_ms, created_at: new Date().toISOString() })
                } else {
                  const c = msg.content || (!msg.tool_calls?.length ? (msg.reasoning_content || '') : '')
                  next.push({ role: msg.role, content: c, reasoning: c === msg.reasoning_content ? undefined : (msg.reasoning_content || undefined), duration_ms: msg.duration_ms, created_at: new Date().toISOString() })
                }
                return next
              })
            }
            break
          case 'subagent_status':
            if (msg.tokens_input || msg.tokens_output) {
              setSubagentTokens(prev => ({ ...prev, [msg.task_id]: { input: msg.tokens_input || 0, output: msg.tokens_output || 0 } }))
              setTotalTokens(prev => ({
                input: prev?.input || 0,
                output: prev?.output || 0,
                reasoning: prev?.reasoning || 0,
                cache_read: prev?.cache_read || 0,
                model_calls: (prev?.model_calls || 0) + 1,
                system_prompt: prev?.system_prompt || 0,
                skill_override: prev?.skill_override || 0,
                history: prev?.history || 0,
                user_message: prev?.user_message || 0,
                tool_definitions: prev?.tool_definitions || 0,
                tool_calls: prev?.tool_calls || 0,
                tool_results: prev?.tool_results || 0,
                subagent_input: (prev?.subagent_input || 0) + (msg.tokens_input || 0),
                subagent_output: (prev?.subagent_output || 0) + (msg.tokens_output || 0),
              }))
            }
            setMessages((prev) => {
              const existing = prev.findIndex(m => m.role === 'subagent' && m.content === msg.task_id)
              const subagentMsg: ChatMessage = {
                role: 'subagent', content: msg.task_id,
                params: JSON.stringify({ goal: msg.goal, status: msg.status, error: msg.error }),
                result: msg.status === 'completed' || msg.status === 'failed' ? JSON.stringify(msg.result || msg.status) : undefined,
                duration_ms: msg.duration_ms,
                created_at: ts(),
              }
              if (existing >= 0) {
                const updated = [...prev]
                updated[existing] = subagentMsg
                return updated
              }
              return [...prev, subagentMsg]
            }); break
          case 'error':
            setMessages((prev) => [...prev, { role: 'assistant', content: 'Error: ' + msg.message, created_at: ts() }]); setIsStreaming(false); updateStreamingReasoning(''); needsCollapseRef.current = false; setTasks((prev) => prev.map((t) => t.status === 'in_progress' ? { ...t, status: 'pending' as const } : t)); setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 50); break
          case 'cancelled':
            setIsStreaming(false); updateStreamingReasoning(''); needsCollapseRef.current = false; setTasks((prev) => prev.map((t) => t.status === 'in_progress' ? { ...t, status: 'pending' as const } : t))
            const ct = streamingTextRef.current; setMessages((prev) => [...prev, { role: 'assistant', content: ct ? ct + '\n\n*[Cancelled]*' : '*[Cancelled]*', created_at: ts() }]); streamingTextRef.current = ''; setCurrentStreamingText(''); break
          case 'slash_result':
            setIsStreaming(false)
            if (msg.command === 'new') { clearChatState(); if (selectedAgentRef.current) { closeWS(); startSession(selectedAgentRef.current) } }
            else if (msg.command === 'summarize' && msg.data) {
              const d = msg.data as { session_id: string; summary: string }
              if (d.session_id) { closeWS(); connectToSession(d.session_id); const sm = [{ role: 'assistant', content: 'Previous session summary:\n\n' + d.summary, created_at: ts() }]; setMessages(sm); if (selectedAgentRef.current) saveChatState(selectedAgentRef.current.id, d.session_id, sm) }
              else { setMessages((prev) => [...prev, { role: 'assistant', content: (msg.data as any).summary ? 'Summary: ' + (msg.data as any).summary : JSON.stringify(msg.data), created_at: ts() }]) }
            }
            else if (msg.data) setMessages((prev) => [...prev, { role: 'assistant', content: JSON.stringify(msg.data, null, 2), created_at: ts() }])
            break
          case 'reconnect_sync': {
            const sm = msg.messages as Array<any>; if (!sm?.length) break
            const conv = sm.map((m: any) => {
              const b: any = { created_at: m.created_at || ts(), images: m.image_ids?.length ? m.image_ids : undefined }
              if (m.role === 'tool') return { ...b, role: 'tool', content: m.tool_call_id || 'tool', params: JSON.stringify(m.tool_calls?.[0]?.arguments || {}), result: m.content }
              if (m.tool_calls?.length) return { ...b, role: 'tool', content: m.tool_calls.map((tc: any) => tc.name).join(', '), params: JSON.stringify(m.tool_calls.map((tc: any) => tc.arguments)), result: undefined }
              return { ...b, role: m.role, content: m.content || '' }
            })
            setMessages(conv); setTasks([])
            const ti = sm.reduce((s: number, m: any) => s + (m.tokens_input || 0), 0); const to = sm.reduce((s: number, m: any) => s + (m.tokens_output || 0), 0); const tr = sm.reduce((s: number, m: any) => s + (m.tokens_reasoning || 0), 0)
            if (ti > 0 || to > 0) setTotalTokens(prev => ({ input: ti, output: to, reasoning: tr, cache_read: 0, model_calls: 0, system_prompt: 0, skill_override: 0, history: 0, user_message: 0, tool_definitions: 0, tool_calls: 0, tool_results: 0, subagent_input: prev?.subagent_input || 0, subagent_output: prev?.subagent_output || 0 }))
            break
          }
          case 'token_update':
            setTotalTokens(prev => { const t = msg.tokens; return { input: t?.input ?? (prev?.input || 0), output: t?.output ?? (prev?.output || 0), reasoning: t?.reasoning ?? (prev?.reasoning || 0), cache_read: t?.cache_read ?? (prev?.cache_read || 0), model_calls: t?.model_calls ?? (prev?.model_calls || 0), system_prompt: t?.system_prompt ?? (prev?.system_prompt || 0), skill_override: t?.skill_override ?? (prev?.skill_override || 0), history: t?.history ?? (prev?.history || 0), user_message: t?.user_message ?? (prev?.user_message || 0), tool_definitions: t?.tool_definitions ?? (prev?.tool_definitions || 0), tool_calls: t?.tool_calls ?? (prev?.tool_calls || 0), tool_results: t?.tool_results ?? (prev?.tool_results || 0), subagent_input: prev?.subagent_input, subagent_output: prev?.subagent_output } }); break
          case 'tasks_updated':
            setTasks((prev) => { const inc = msg.data as AgentTaskItem[]; const m = [...prev]; for (const t of inc) { const idx = m.findIndex((x) => x.id === t.id); if (idx >= 0) m[idx] = { ...m[idx], ...t, ...(t.description ? {} : { description: m[idx].description }) }; else m.push(t) }; return m }); break
        }
      }

    ws.onclose = () => {
      if (reconnectTimerRef.current) return
      wsRef.current = null
      if (reconnectAttemptsRef.current < 5) {
        const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 15000)
        reconnectAttemptsRef.current += 1
        reconnectTimerRef.current = setTimeout(() => { reconnectTimerRef.current = null; connectWebSocket(sid) }, delay)
      } else {
        clearChatState()
        if (selectedAgentRef.current) startSession(selectedAgentRef.current)
      }
    }

    ws.onerror = () => { setError('WebSocket connection failed'); setIsStreaming(false) }
  }, [notebookId, queryClient, scrollToCell])

  const startSession = async (agent: Agent) => {
    const reqId = ++sessionReqIdRef.current
    try {
      const res = await api.post<{ session_id: string; context_window?: number }>('/api/v1/agents/' + agent.id + '/session', {
        notebook_id: notebookId || null,
        page_context: pageContext || (notebookId ? { type: 'notebook', id: notebookId } : null),
      })
      if (reqId !== sessionReqIdRef.current) return // stale — superseded by newer startSession
      setSessionId(res.session_id)
      setSessionTitle(null)
      setSelectedAgent(agent)
      localStorage.setItem(LAST_AGENT_KEY, agent.id)
      setMessages([])
      setTasks([])
      setTotalTokens(null)
      setContextWindow(res.context_window ?? 0)
      connectWebSocket(res.session_id)
    } catch {
      if (reqId !== sessionReqIdRef.current) return
      setError('Failed to start session')
    }
  }

  const connectToSession = (sessionID: string) => {
    setSessionId(sessionID)
    setSessionTitle(null)
    setMessages([])
    setTasks([])
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

    // Optimistic /new: handle before connection check so it can recover
    // from a disconnected state by starting a fresh session.
    if (text.startsWith('/') && text.slice(1).trim().toLowerCase() === 'new') {
      chatClearedRef.current = true
      clearChatState()
      setMessages([])
      setTasks([])
      closeWS()
      if (selectedAgentRef.current) startSession(selectedAgentRef.current)
      return
    }

    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      setError('Not connected. Attempting to reconnect...')
      return
    }

    forceScrollRef.current = true
    setTotalTokens(prev => prev || { input: 0, output: 0, reasoning: 0, cache_read: 0, model_calls: 0, system_prompt: 0, skill_override: 0, history: 0, user_message: 0, tool_definitions: 0, tool_calls: 0, tool_results: 0 })

    // Slash commands must bypass the queue — they need to reach the backend
    // immediately, even while streaming.
    if (text.startsWith('/') && !text.toLowerCase().startsWith('/skill:')) {
      const command = text.slice(1).trim()
      setMessages((prev) => [...prev, { role: 'user', content: text, created_at: ts() }])
      wsRef.current.send(JSON.stringify({ type: 'slash_command', command }))
      return
    }

    if (!skipQueue && (isStreaming || pendingMessages.length > 0)) {
      setPendingMessages((prev) => [...prev, text])
      setMessages((prev) => [...prev, { role: 'user', content: text, created_at: ts() }])
      return
    }

    // Collect pending image IDs
    const images = pendingImagesRef.current.filter(p => p.id && !p.uploading).map(p => p.id)
    if (images.length > 0) {
      // Clean up blob URLs
      pendingImagesRef.current.forEach(p => URL.revokeObjectURL(p.blobUrl))
      setPendingImages([])
    }

    const msg: Record<string, any> = { type: 'message', content: text }
    if (images.length > 0) {
      msg.images = images
    }
    setMessages((prev) => [...prev, { role: 'user', content: text, images, created_at: ts() }])
    wsRef.current.send(JSON.stringify(msg))
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
        if (msg.images?.length) {
          lines.push(...msg.images.map(() => '  _[Image]_'))
        }
      } else if (msg.role === 'assistant') {
        if (msg.reasoning) {
          const dur = msg.duration_ms ? ` (${msg.duration_ms}ms)` : ''
          lines.push(`> **Thinking:${dur}** ${msg.reasoning}`)
        }
        const dur = msg.duration_ms ? ` (${msg.duration_ms}ms)` : ''
        lines.push(`**Assistant:${dur}** ${msg.content}`)
        if (msg.images?.length) {
          lines.push(...msg.images.map(() => '  _[Image]_'))
        }
      } else if (msg.role === 'tool') {
        const dur = msg.duration_ms ? ` (${msg.duration_ms}ms)` : ''
        lines.push(`**Tool: ${msg.content}${dur}**`)
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

  const scrollHandlerRef = useRef<((e: Event) => void) | null>(null)
  const scrollRefCb = useCallback((el: HTMLDivElement | null) => {
    if (messageListRef.current && scrollHandlerRef.current) {
      messageListRef.current.removeEventListener('scroll', scrollHandlerRef.current)
    }
    messageListRef.current = el
    if (el) {
      el.style.setProperty('overflow-anchor', 'none')
      const handler = () => {
        if (isProgrammaticScrollRef.current) {
          isProgrammaticScrollRef.current = false
          return
        }
        lastScrollTimeRef.current = Date.now()
        const el = messageListRef.current
        if (el) {
          const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30
          wasAtBottomRef.current = nearBottom
        } else {
          wasAtBottomRef.current = false
        }
      }
      scrollHandlerRef.current = handler
      el.addEventListener('scroll', handler, { passive: true })
    } else {
      scrollHandlerRef.current = null
    }
  }, [])

  useLayoutEffect(() => {
    const el = messageListRef.current
    if (!el) return
    const now = Date.now()
    const scrollGuard = now - lastScrollTimeRef.current < 200
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30
    if (scrollGuard && !forceScrollRef.current) return
    if (el.scrollHeight < prevScrollHeightRef.current) {
      prevScrollHeightRef.current = el.scrollHeight
      if (wasAtBottomRef.current) {
        isProgrammaticScrollRef.current = true
        el.scrollTop = el.scrollHeight
      }
      wasAtBottomRef.current = Math.max(0, el.scrollHeight - el.clientHeight) <= el.scrollTop + 5
      return
    }
    prevScrollHeightRef.current = el.scrollHeight
    if (!forceScrollRef.current && !nearBottom && !wasAtBottomRef.current) {
      wasAtBottomRef.current = false
      return
    }
    forceScrollRef.current = false
    isProgrammaticScrollRef.current = true
    el.scrollTop = el.scrollHeight
    wasAtBottomRef.current = true
  }, [messages, currentStreamingText, currentStreamingReasoning])

  useEffect(() => {
    if (_sessionId && selectedAgent && messages.length > 0 && !chatClearedRef.current) {
      saveChatState(selectedAgent.id, _sessionId, messages, tasks, totalTokens || undefined, contextWindow)
    }
    return () => {
      if (_sessionId && selectedAgent && messages.length > 0 && !chatClearedRef.current) {
        saveChatState(selectedAgent.id, _sessionId, messages, tasks, totalTokens || undefined, contextWindow)
      }
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

  const MemoizedChatMessage = memo(({ msg }: {
    msg: ChatMessage
  }) => {
    const [thoughtOpen, setThoughtOpen] = useState(true)
    const [toolOpen, setToolOpen] = useState(false)
    const [toolNow, setToolNow] = useState(Date.now())
    useEffect(() => {
      const needsTimer = (msg.role === 'tool' && !msg.result) || (msg.role === 'subagent' && !msg.result)
      if (!needsTimer) return
      const id = setInterval(() => setToolNow(Date.now()), 1000)
      return () => clearInterval(id)
    }, [msg.role, msg.result])
    return (
    <div>
      {msg.reasoning && (
        <div style={{ ...styles.message, ...styles.reasoningMessage, marginBottom: 4 }}>
          <div onClick={() => setThoughtOpen((o) => !o)} style={{ cursor: 'pointer', color: 'var(--text-muted)', fontSize: 11, userSelect: 'none', display: 'flex', alignItems: 'center', gap: 6 }}>
            <span>{thoughtOpen ? '▼' : '▶'} Thinking</span>
            {msg.duration_ms ? <span style={{ opacity: 0.5, fontSize: 10 }}>({msg.duration_ms}ms)</span> : null}
          </div>
          {thoughtOpen && (
            <>
              {msg.created_at && <div style={{ fontSize: 9, color: 'var(--text-muted)', opacity: 0.5, marginBottom: 4 }}>{fmtTime(msg.created_at)}</div>}
              <div style={{ marginTop: 6, whiteSpace: 'pre-wrap' }}>{msg.reasoning}</div>
            </>
          )}
        </div>
      )}
      {msg.role !== 'reasoning' && (
        <div style={{ ...styles.message, ...(msg.role === 'user' ? styles.userMessage : msg.role === 'tool' ? styles.toolMessage : styles.assistantMessage) }}>
          {msg.created_at && (
            <div style={{ fontSize: 9, color: msg.role === 'user' ? 'rgba(255,255,255,0.5)' : 'var(--text-muted)', marginBottom: 4, textAlign: msg.role === 'user' ? 'right' : 'left' }}>
              {fmtTime(msg.created_at)}
            </div>
          )}
          {msg.images && msg.images.length > 0 && (
            <AgentMessageImages images={msg.images} />
          )}
          {msg.role === 'tool' ? (
            <>
              <div onClick={() => setToolOpen((o) => !o)} style={{ cursor: 'pointer', userSelect: 'none', display: 'flex', alignItems: 'center', gap: 4 }}>
                <span style={{ opacity: 0.6, fontSize: 11 }}>{toolOpen ? '▼' : '▶'} TOOL </span>
                <span>{msg.content}</span>
                {!msg.result ? (
                  <span style={{ opacity: 0.6, fontSize: 11, marginLeft: 'auto' }}>
                    <span style={{ display: 'inline-block', animation: 'spin 1s linear infinite', marginRight: 4 }}>●</span>
                    Working…
                    {msg.created_at && ` (${Math.floor((toolNow - new Date(msg.created_at).getTime()) / 1000)}s)`}
                  </span>
                ) : msg.duration_ms ? (
                  <span style={{ opacity: 0.5, fontSize: 10, marginLeft: 'auto' }}>({msg.duration_ms}ms)
                ) : null}
              </div>
              {toolOpen && (
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
              )}
            </>
          ) : msg.role === 'subagent' ?
            (() => {
              let parsedParams: { goal?: string; status?: string; error?: string } = {}
              try { if (msg.params) parsedParams = JSON.parse(msg.params) } catch {}
              return (
              <div onClick={() => { if (msg.content) { const el = document.querySelector('[data-main-scroll]'); mainScrollRef.current = el?.scrollTop || 0; subagentViewRef.current = msg.content; setSubagentView(msg.content); fetchSubagentMessages(msg.content, setSubagentMessages, setSubagentLoading) } }}
                style={{ fontSize: 11, opacity: 0.8, cursor: 'pointer', borderRadius: 4, padding: '2px 4px', border: subagentView === msg.content ? '1px solid var(--accent)' : '1px solid transparent' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                  <span style={{ opacity: 0.5, fontSize: 10 }}>SUBAGENT</span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, opacity: 0.5 }}>{msg.content?.slice(0, 8)}</span>
                  {!msg.result ? (
                    <span style={{ opacity: 0.6, fontSize: 10, marginLeft: 'auto' }}>
                      <span style={{ display: 'inline-block', animation: 'spin 1s linear infinite', marginRight: 4 }}>●</span>
                      Working…
                      {msg.created_at && ` (${Math.floor((toolNow - new Date(msg.created_at).getTime()) / 1000)}s)`}
                    </span>
                  ) : (
                    <span style={{ marginLeft: 'auto', fontSize: 10 }}>
                      {msg.result?.includes('failed') ? '❌ Failed' : '✅ Done'}
                      {msg.duration_ms ? ` (${msg.duration_ms}ms)` : ''}
                    </span>
                  )}
                </div>
                {parsedParams.goal && (
                  <div style={{ marginTop: 4, opacity: 0.6, fontSize: 10, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', maxWidth: '100%' }}>{parsedParams.goal}</div>
                )}
                {msg.result && msg.result !== '"completed"' && msg.result !== '"failed"' && (
                  <div style={{ marginTop: 4, fontSize: 10, maxHeight: 60, overflow: 'auto', opacity: 0.7, whiteSpace: 'pre-wrap' }}>
                    {msg.result.length > 200 ? msg.result.slice(0, 200) + '…' : msg.result}
                  </div>
                )}
                {parsedParams.error && (
                  <div style={{ marginTop: 4, fontSize: 10, color: 'var(--error, #ef4444)', opacity: 0.8 }}>{parsedParams.error}</div>
                )}
              </div>
            )})()
          ) : (
            <div>
              <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]} components={chatMarkdownComponents}>{msg.content}</ReactMarkdown>
              {msg.role === 'assistant' && msg.duration_ms ? (
                <div style={{ fontSize: 9, color: 'var(--text-muted)', opacity: 0.5, marginTop: 4 }}>{msg.duration_ms}ms</div>
              ) : null}
            </div>
          )}
        </div>
      )}
    </div>
  )})
  return (
    <div ref={panelRef} style={{ ...styles.panel, width }}>
      {subagentView ? (
        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 12px', borderBottom: '1px solid var(--border)', flexShrink: 0 }}>
            <button onClick={() => { subagentViewRef.current = null; setSubagentView(null); setSubagentMessages([]); setTimeout(() => { const el = document.querySelector('[data-main-scroll]'); if (el) el.scrollTop = mainScrollRef.current }, 50) }}
              style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', color: 'var(--text-primary)', fontSize: 12, padding: '4px 10px', fontWeight: 500 }}>
              ← Back
            </button>
            <span style={{ flex: 1, fontSize: 13, fontWeight: 600, color: 'var(--text-primary)' }}>Subagent {subagentView.slice(0, 8)}</span>
            {subagentTokens[subagentView] && (
              <span style={{ fontSize: 10, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                {subagentTokens[subagentView].input.toLocaleString()}↑ / {subagentTokens[subagentView].output.toLocaleString()}↓
              </span>
            )}
            <button onClick={() => {
              const lines: string[] = []
              for (const m of subagentMessages) {
                const dur = m.duration_ms ? ` (${m.duration_ms}ms)` : ''
                if (m.role === 'user') {
                  lines.push(`**User:** ${m.content}`)
                } else if (m.role === 'assistant') {
                  if (m.reasoning) lines.push(`> **Thinking:${dur}** ${m.reasoning}`)
                  lines.push(`**Assistant:${dur}** ${m.content}`)
                } else if (m.role === 'tool') {
                  lines.push(`**Tool: ${m.content}${dur}**`)
                  if (m.result) lines.push(`  Result: \`${m.result.length > 500 ? m.result.slice(0, 500) + '...' : m.result}\``)
                }
                lines.push('')
              }
              navigator.clipboard.writeText(lines.join('\n').trim()).then(() => {
                setCopied(true)
                setTimeout(() => setCopied(false), 1500)
              })
            }}
              style={{ background: 'none', border: '1px solid var(--border)', borderRadius: 4, cursor: 'pointer', color: 'var(--text-secondary)', fontSize: 11, padding: '3px 8px' }}>
              {copied ? <Check size={12} style={{ color: 'var(--success, #10b981)' }} /> : 'Copy chat'}
            </button>
          </div>
          <div ref={subagentScrollRef} style={{ flex: 1, overflow: 'auto', padding: 12 }}>
            {subagentLoading && <div style={{ textAlign: 'center', color: 'var(--text-muted)', fontSize: 12, padding: 20 }}>Loading…</div>}
            {subagentMessages.map((m, i) => (
              <MemoizedChatMessage key={i} msg={m} isStreaming={false} now={now} />
            ))}
            {!subagentLoading && subagentMessages.length === 0 && (
              <div style={{ textAlign: 'center', color: 'var(--text-muted)', fontSize: 12, padding: 20 }}>
                This subagent hasn't produced any messages yet.
              </div>
            )}
          </div>
        </div>
      ) : (<>
      <div
        ref={resizeRef}
        style={styles.resizeHandle}
      />
      <PanelHeader
        title={sessionTitle || (selectedAgent ? selectedAgent.name : 'AI Agent')}
        onClose={onClose}
        onMinimize={onMinimize}
        onDock={onDock}
        docked={docked}
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
              const msgs = await api.get<Array<{ role: string; content: string; reasoning_content?: string; image_ids?: string[]; duration_ms?: number; created_at?: string }>>(`/api/v1/sessions/${session.id}/messages`)
              const formatted = msgs.map((m) => ({
                role: m.role,
                content: m.content || '',
                reasoning: m.reasoning_content,
                images: m.image_ids?.length ? m.image_ids : undefined,
                duration_ms: m.duration_ms,
                created_at: m.created_at,
              }))
              setMessages(formatted)
              if (selectedAgent) {
                saveChatState(selectedAgent.id, session.id, formatted, undefined)
              }
            } catch {
              setMessages([])
            }
            setIsStreaming(false)
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
                  try { localStorage.setItem(REASONING_EFFORT_KEY, val) } catch {}
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
            {modelConfigs.length > 0 && (
              <select
                style={{ fontSize: 11, padding: '2px 20px 2px 6px', background: 'var(--bg-primary)', border: '1px solid var(--border)', borderRadius: 4, color: 'var(--text-muted)', cursor: 'pointer', maxWidth: 120 }}
                value={modelConfigId}
                onChange={(e) => {
                  const val = e.target.value
                  setModelConfigId(val)
                  try { localStorage.setItem(MODEL_CONFIG_KEY, val) } catch {}
                  const mc = modelConfigs.find(m => m.id === val)
                  const params = mc?.default_params || {}
                  const opts = params['reasoning_effort_options']
                  const effortOpts: string[] = Array.isArray(opts) ? opts as string[] : []
                  setReasoningEffortOpts(effortOpts)
                  const def = params['reasoning_effort']
                  const defaultEffort = typeof def === 'string' ? def : ''
                  const newEffort = effortOpts.length > 0 && !effortOpts.includes(reasoningEffort) ? defaultEffort : reasoningEffort
                  if (newEffort !== reasoningEffort) {
                    setReasoningEffort(newEffort)
                    try { localStorage.setItem(REASONING_EFFORT_KEY, newEffort) } catch {}
                    wsRef.current?.send(JSON.stringify({ type: 'set_reasoning_effort', reasoning_effort: newEffort }))
                  }
                  wsRef.current?.send(JSON.stringify({ type: 'set_model_config', model_config_id: val }))
                }}
                title="Model"
              >
                {modelConfigs.map(mc => (
                  <option key={mc.id} value={mc.id}>{mc.name}</option>
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
                  {(() => {
                    const si = totalTokens.subagent_input || 0
                    const so = totalTokens.subagent_output || 0
                    const allIn = totalTokens.input + si
                    const allOut = totalTokens.output + so
                    return <>{allIn.toLocaleString()}↑ / {allOut.toLocaleString()}↓</>
                  })()}
                  {(() => {
                    const mc = modelConfigs.find(m => m.id === modelConfigId)
                    if (!mc || (!mc.price_per_input_token && !mc.price_per_output_token)) return null
                    const si = totalTokens.subagent_input || 0
                    const so = totalTokens.subagent_output || 0
                    const cost = ((totalTokens.input + si) * mc.price_per_input_token + (totalTokens.output + so) * mc.price_per_output_token + (totalTokens.cache_read ?? 0) * mc.price_per_cache_read_token) / 1000000
                    return <span style={{ marginLeft: 6, opacity: 0.7, fontSize: 10 }}>${cost < 0.01 ? cost.toFixed(6) : cost.toFixed(4)}</span>
                  })()}
                  {contextWindow > 0 && (
                    <span style={{ marginLeft: 6, opacity: 0.6 }}>
                      ({Math.round((totalTokens.input + totalTokens.output) / contextWindow * 100)}%)
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
                        <span>{totalTokens.input.toLocaleString()} <span style={{ fontSize: 10, opacity: 0.6 }}>{costFmt(mc => mc.price_per_input_token * totalTokens.input / 1000000)}</span></span>
                      </div>
                      {totalTokens.cache_read > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2, paddingLeft: 16 }}>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Cache read</span>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.cache_read.toLocaleString()} <span style={{ fontSize: 10, opacity: 0.6 }}>{costFmt(mc => mc.price_per_cache_read_token * totalTokens.cache_read / 1000000)}</span></span>
                        </div>
                      )}

                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 4 }}>
                        <span style={{ color: 'var(--text-secondary)' }}>Output</span>
                        <span>{totalTokens.output.toLocaleString()} <span style={{ fontSize: 10, opacity: 0.6 }}>{costFmt(mc => mc.price_per_output_token * totalTokens.output / 1000000)}</span></span>
                      </div>
                      {totalTokens.reasoning > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 4, paddingLeft: 16 }}>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>Reasoning</span>
                          <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{totalTokens.reasoning.toLocaleString()}</span>
                        </div>
                      )}

                      {totalTokens.subagent_input ? (
                        <div style={{ borderTop: '1px solid var(--border)', margin: '6px 0', paddingTop: 6 }}>
                          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, marginBottom: 2 }}>
                            <span style={{ color: 'var(--text-secondary)', fontSize: 11 }}>Subagent Input</span>
                            <span style={{ fontSize: 11 }}>{totalTokens.subagent_input.toLocaleString()}</span>
                          </div>
                          <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24 }}>
                            <span style={{ color: 'var(--text-secondary)', fontSize: 11 }}>Subagent Output</span>
                            <span style={{ fontSize: 11 }}>{totalTokens.subagent_output?.toLocaleString()}</span>
                          </div>
                        </div>
                      ) : null}

                      <div style={{ borderTop: '1px solid var(--border)', margin: '6px 0', paddingTop: 6, display: 'flex', justifyContent: 'space-between', gap: 24 }}>
                        <span style={{ color: 'var(--text-secondary)' }}>Total</span>
                        <span style={{ fontWeight: 600 }}>{(() => {
                          const si = totalTokens.subagent_input || 0
                          const so = totalTokens.subagent_output || 0
                          const total = totalTokens.input + totalTokens.output + si + so
                          const mc = modelConfigs.find(m => m.id === modelConfigId)
                          const cost = mc && (mc.price_per_input_token || mc.price_per_output_token)
                            ? ((totalTokens.input + si) * mc.price_per_input_token + (totalTokens.output + so) * mc.price_per_output_token + (totalTokens.cache_read || 0) * mc.price_per_cache_read_token) / 1000000
                            : null
                          return total.toLocaleString() + (cost !== null ? `  $${cost < 0.01 ? cost.toFixed(6) : cost.toFixed(4)}` : '')
                        })()}</span>
                      </div>
                      {totalTokens.model_calls > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, color: 'var(--text-muted)', fontSize: 11, marginTop: 4 }}>
                          <span>Model calls</span>
                          <span>{totalTokens.model_calls}</span>
                        </div>
                      )}
                      {contextWindow > 0 && (
                        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 24, color: 'var(--text-muted)', fontSize: 11 }}>
                          <span>Context window</span>
                          <span>{contextWindow.toLocaleString()} ({Math.round((totalTokens.input + totalTokens.output) / contextWindow * 100)}%)</span>
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

          <div ref={scrollRefCb} data-main-scroll style={styles.messageList}>
            {messages.length === 0 && (
              <div style={styles.emptyState}>
                {notebookId
                  ? 'Ask me anything about this notebook. I can read cells, create new ones, run queries, and make charts.'
                  : 'Ask me anything. I can help with notebooks, queries, analysis, and more.'}
              </div>
            )}
              {messages.map((msg, i) => (
                <MemoizedChatMessage
                  key={i}
                  msg={msg}
                />
              ))}
              {isStreaming && !currentStreamingText && (
                  <details open={thinkingOpen} style={{ ...styles.message, ...styles.reasoningMessage }}>
                    <summary onClick={(e) => { e.preventDefault(); setThinkingOpen((o) => !o) }} style={{ cursor: 'pointer', color: 'var(--text-muted)', fontSize: 11, userSelect: 'none' }}>{thinkingOpen ? '▼' : '▶'} {hasPendingTools ? 'Working' : 'Thinking'} <span style={{ opacity: 0.5 }}>• {formatElapsed(elapsed)}</span></summary>
                    {streamingStartedAt.current && <div style={{ fontSize: 9, color: 'var(--text-muted)', opacity: 0.5, marginBottom: 4 }}>{fmtTime(streamingStartedAt.current)}</div>}
                    <div style={{ marginTop: 6, whiteSpace: 'pre-wrap' }}>
                      {hasPendingTools ? <span style={{ color: 'var(--text-muted)' }}>Waiting for tool result…</span> : (currentStreamingReasoning || <span style={{ color: 'var(--text-muted)' }}>...</span>)}
                    </div>
                  </details>
                )}
              {currentStreamingText && (
              <div style={{ ...styles.message, ...styles.assistantMessage }}>
                {streamingStartedAt.current && <div style={{ fontSize: 9, color: 'var(--text-muted)', opacity: 0.5, marginBottom: 4 }}>{fmtTime(streamingStartedAt.current)} <span style={{ opacity: 0.5 }}>• {formatElapsed(elapsed)}</span></div>}
                <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]} components={chatMarkdownComponents}>{currentStreamingText}</ReactMarkdown>
                <span style={styles.streamingDot} />
              </div>
            )}
            {error && <div style={styles.error}>{error}</div>}
          </div>

          <div>
            {pendingImages.length > 0 && (
              <div style={styles.imagePreviewRow}>
                {pendingImages.map((img, idx) => (
                  <div key={idx} style={styles.imagePreviewItem}>
                    <img src={img.blobUrl} alt={img.filename} style={styles.imagePreviewThumb} />
                    {img.uploading && <div style={styles.imageUploadingOverlay}><Loader2 size={14} style={{ animation: 'spin 1s linear infinite' }} /></div>}
                    <button
                      style={styles.imagePreviewRemove}
                      onClick={() => removePendingImage(img.blobUrl)}
                      title="Remove image"
                    >
                      <X size={12} />
                    </button>
                  </div>
                ))}
              </div>
            )}
            <div
              style={{ ...styles.inputArea, position: 'relative' }}
              onDrop={handleDrop}
              onDragOver={handleDragOver}
            >
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
              placeholder="Message agent... (/ for commands, paste images)"
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
            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 6, flexWrap: 'wrap' }}>
              <button onClick={() => {
                wsRef.current?.send(JSON.stringify({ type: 'tool_confirm', approved: false, content: pendingConfirm.tool }))
                setMessages((prev) => [...prev, { role: 'assistant', content: `⛔ Denied tool call: **${pendingConfirm.tool}**`, created_at: ts() }])
                setPendingConfirm(null)
              }} style={{ padding: '6px 14px', border: '1px solid var(--border)', borderRadius: 4, background: 'none', color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 12 }}>
                Deny
              </button>
              <button onClick={() => {
                wsRef.current?.send(JSON.stringify({ type: 'tool_confirm', approved: true, content: pendingConfirm.tool }))
                setMessages((prev) => [...prev, { role: 'assistant', content: `✅ Approved: **${pendingConfirm.tool}**`, created_at: ts() }])
                setPendingConfirm(null)
              }} style={{ padding: '6px 14px', border: '1px solid var(--border)', borderRadius: 4, background: 'var(--accent)', color: '#fff', cursor: 'pointer', fontWeight: 600, fontSize: 12 }}>
                Approve Once
              </button>
              <button onClick={() => {
                sessionApprovedRef.current.add(pendingConfirm.tool)
                wsRef.current?.send(JSON.stringify({ type: 'tool_confirm', approved: true, content: pendingConfirm.tool }))
                setMessages((prev) => [...prev, { role: 'assistant', content: `✅ Always allow **${pendingConfirm.tool}** for this session`, created_at: ts() }])
                setPendingConfirm(null)
              }} style={{ padding: '6px 14px', border: 'none', borderRadius: 4, background: 'var(--accent)', color: '#fff', cursor: 'pointer', fontWeight: 600, fontSize: 12 }}>
                Always Allow in Session
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
      </>)}
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
    position: 'relative',
    overflow: 'hidden',
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
    flexWrap: 'wrap',
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
    overscrollBehavior: 'contain',
    overflowAnchor: 'none',
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
  imagePreviewRow: {
    display: 'flex',
    gap: 8,
    padding: '8px 12px 0 12px',
    flexWrap: 'wrap' as const,
  },
  imagePreviewItem: {
    position: 'relative' as const,
    width: 64,
    height: 64,
    borderRadius: 6,
    overflow: 'hidden',
    border: '1px solid var(--border)',
  },
  imagePreviewThumb: {
    width: '100%',
    height: '100%',
    objectFit: 'cover' as const,
  },
  imageUploadingOverlay: {
    position: 'absolute' as const,
    inset: 0,
    background: 'rgba(0,0,0,0.4)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  imagePreviewRemove: {
    position: 'absolute' as const,
    top: 2,
    right: 2,
    width: 18,
    height: 18,
    borderRadius: '50%',
    background: 'rgba(0,0,0,0.6)',
    border: 'none',
    color: 'white',
    cursor: 'pointer',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 0,
  },
}
