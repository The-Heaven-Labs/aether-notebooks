import { useState, useEffect, memo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import { AgentMessageImages } from './AgentMessageImages'

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
  p: ({ children }: any) => <div style={{ margin: '4px 0', lineHeight: 1.5 }}>{children}</div>,
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

export interface ChatMessage {
  id?: string
  role: string
  content: string
  reasoning?: string
  params?: string
  result?: string
  tool_call_id?: string
  images?: string[]
  duration_ms?: number
  tokens_direct?: number
  tokens_before?: number
  tokens_after?: number
  created_at?: string
}

// Hoisted to module scope to prevent remount on parent re-render (Issue 4)
export function fmtTime(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function formatTokens(n: number): string {
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k'
  return String(n)
}

export const chatStyles: Record<string, React.CSSProperties> = {
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
  compactionMessage: {
    background: 'var(--bg-elevated)',
    border: '1px dashed var(--border)',
    borderRadius: 6,
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
}

export function CompactionDivider({ msg, fmtTime }: { msg: ChatMessage; fmtTime: (iso?: string) => string }) {
  const [open, setOpen] = useState(false)
  return (
    <div style={{ ...chatStyles.message, background: 'var(--bg-elevated)', border: '1px dashed var(--border)', borderRadius: 6, padding: '8px 10px', fontSize: 11 }}>
      <div onClick={() => setOpen(o => !o)} style={{ cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6, userSelect: 'none' }}>
        <span>{open ? '▼' : '▶'} ⚙ Context compacted</span>
        {msg.tokens_before !== undefined && msg.tokens_after !== undefined ? (
          <span style={{ opacity: 0.6, fontSize: 10 }}>{formatTokens(msg.tokens_before)} → {formatTokens(msg.tokens_after)} (~)</span>
        ) : null}
        <span style={{ marginLeft: 'auto', opacity: 0.5, fontSize: 10 }}>{fmtTime(msg.created_at)}</span>
      </div>
      {open && msg.content && (
        <div style={{ marginTop: 6, whiteSpace: 'pre-wrap', fontSize: 11, opacity: 0.9 }}>{msg.content}</div>
      )}
    </div>
  )
}

const MemoizedChatMessage = memo(function MemoizedChatMessageInner({ msg, subagentView, onSubagentSelect }: {
  msg: ChatMessage
  subagentView?: string | null
  onSubagentSelect?: (id: string) => void
}) {
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
        <div style={{ ...chatStyles.message, ...chatStyles.reasoningMessage, marginBottom: 4 }}>
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
        <div style={{ ...chatStyles.message, ...(msg.role === 'user' ? chatStyles.userMessage : msg.role === 'tool' ? chatStyles.toolMessage : msg.role === 'compaction' ? chatStyles.compactionMessage : chatStyles.assistantMessage) }}>
          {msg.created_at && (
            <div style={{ fontSize: 9, color: msg.role === 'user' ? 'rgba(255,255,255,0.5)' : 'var(--text-muted)', marginBottom: 4, textAlign: msg.role === 'user' ? 'right' : 'left' }}>
              {fmtTime(msg.created_at)}
            </div>
          )}
          {msg.images && msg.images.length > 0 && (
            <AgentMessageImages images={msg.images} />
          )}
          {msg.role === 'compaction' ? (
            <CompactionDivider msg={msg} fmtTime={fmtTime} />
          ) : msg.role === 'tool' ? (
            <>
              <div onClick={() => setToolOpen((o) => !o)} style={{ cursor: 'pointer', userSelect: 'none', display: 'flex', alignItems: 'center', gap: 4 }}>
                <span style={{ opacity: 0.6, fontSize: 11 }}>{toolOpen ? '▼' : '▶'} TOOL </span>
                <span>{msg.content}</span>
                {msg.tokens_direct !== undefined && msg.tokens_direct !== null ? (
                  <span style={{ opacity: 0.5, fontSize: 10, marginLeft: 6 }}>{formatTokens(msg.tokens_direct)} tok</span>
                ) : null}
                {!msg.result ? (
                  <span style={{ opacity: 0.6, fontSize: 11, marginLeft: 'auto' }}>
                    <span style={{ display: 'inline-block', animation: 'spin 1s linear infinite', marginRight: 4 }}>●</span>
                    Working…
                    {msg.created_at && ` (${Math.floor((toolNow - new Date(msg.created_at).getTime()) / 1000)}s)`}
                  </span>
                ) : msg.duration_ms ? (
                  <span style={{ opacity: 0.5, fontSize: 10, marginLeft: 'auto' }}>({msg.duration_ms}ms)</span>
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
          ) : msg.role === 'subagent' ? (
            (() => {
              let parsedParams: { goal?: string; status?: string; error?: string } = {}
              try { if (msg.params) parsedParams = JSON.parse(msg.params) } catch {}
              return (
              <div onClick={onSubagentSelect && msg.content ? () => onSubagentSelect(msg.content) : undefined}
                style={{ fontSize: 11, opacity: 0.8, cursor: onSubagentSelect ? 'pointer' : 'default', borderRadius: 4, padding: '2px 4px', border: onSubagentSelect && subagentView === msg.content ? '1px solid var(--accent)' : '1px solid transparent' }}>
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
            <>
              <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]} components={chatMarkdownComponents}>{msg.content}</ReactMarkdown>
              {msg.role === 'assistant' && msg.duration_ms ? (
                <div style={{ fontSize: 9, color: 'var(--text-muted)', opacity: 0.5, marginTop: 4 }}>{msg.duration_ms}ms</div>
              ) : null}
            </>
          )}
        </div>
      )}
    </div>
  )
})

export interface AgentChatTranscriptProps {
  messages: ChatMessage[]
  subagentView?: string | null
  onSubagentSelect?: (id: string) => void
  leading?: React.ReactNode
  emptyState?: React.ReactNode
  trailing?: React.ReactNode
}

export function AgentChatTranscript({ messages, subagentView, onSubagentSelect, leading, emptyState, trailing }: AgentChatTranscriptProps) {
  return (
    <>
      {leading}
      {messages.length === 0 && emptyState}
      {messages.map((msg, i) => (
        <MemoizedChatMessage key={msg.id ?? i} msg={msg} subagentView={subagentView} onSubagentSelect={onSubagentSelect} />
      ))}
      {trailing}
    </>
  )
}
