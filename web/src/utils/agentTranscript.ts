// TranscriptMessage mirrors the ChatMessage shape rendered by AgentPanel.
// It is defined structurally so both the panel and tests can use these helpers
// without importing the component.
export interface TranscriptMessage {
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

interface ServerToolCall {
  id?: string
  name?: string
  arguments?: unknown
  result?: unknown
  error?: string
  duration_ms?: number
}

function stringifyArg(v: unknown): string | undefined {
  if (v === undefined || v === null) return undefined
  return typeof v === 'string' ? v : JSON.stringify(v)
}

// mapServerMessagesToChat converts reconnect_sync rows (authoritative DB state)
// into renderable transcript entries. Rows are typed as any: the wire carries
// roles beyond the AgentMessage union (e.g. 'subagent') and legacy shapes.
//
// Tool calls persisted on assistant messages render as one entry per tool_call
// (name + params + result). Separate role='tool' rows are folded into their
// assistant call when the IDs match — this also heals pre-fix history, where
// results only exist on the tool rows — and skipped otherwise to avoid doubled,
// garbled entries.
export function mapServerMessagesToChat(serverMsgs: any[] | null | undefined): TranscriptMessage[] {
  if (!serverMsgs) return []
  // Index orphan tool rows by tool_call_id for the legacy join.
  const toolRowsByCallId = new Map<string, string>()
  for (const m of serverMsgs) {
    if (m.role === 'tool' && m.tool_call_id && m.content) {
      if (!toolRowsByCallId.has(m.tool_call_id)) toolRowsByCallId.set(m.tool_call_id, m.content)
    }
  }
  const renderedCallIds = new Set<string>()
  const out: TranscriptMessage[] = []
  for (const m of serverMsgs) {
    const base: TranscriptMessage = {
      id: m.id,
      role: m.role,
      content: m.content || '',
      reasoning: (m as any).reasoning_content || undefined,
      images: (m as any).image_ids?.length ? (m as any).image_ids : undefined,
      created_at: (m as any).created_at,
    }
    if ((m as any).duration_ms) base.duration_ms = (m as any).duration_ms
    if ((m as any).tokens_direct !== undefined) base.tokens_direct = (m as any).tokens_direct
    if (m.role === 'subagent') {
      const tc = ((m.tool_calls?.[0] as any)?.function || m.tool_calls?.[0]) as any
      base.content = m.content || ''
      base.params = JSON.stringify({ goal: tc?.name || '', status: tc?.arguments?.status || 'completed', error: tc?.arguments?.error || '' })
      base.result = tc?.arguments?.status === 'completed' || tc?.arguments?.status === 'failed'
        ? JSON.stringify(tc?.arguments?.result || tc?.arguments?.status)
        : undefined
      out.push(base)
    } else if (m.tool_calls?.length) {
      for (const rawTc of m.tool_calls) {
        const tc = ((rawTc as any)?.function || rawTc) as ServerToolCall
        const entry: TranscriptMessage = {
          ...base,
          id: tc.id ? `${m.id}:${tc.id}` : m.id,
          content: tc.name || 'tool',
          params: stringifyArg(tc.arguments),
          role: 'tool',
          tool_call_id: tc.id,
          duration_ms: tc.duration_ms ?? base.duration_ms,
        }
        if (tc.result !== undefined) {
          entry.result = typeof tc.result === 'string' ? tc.result : JSON.stringify(tc.result)
        } else if (tc.id && toolRowsByCallId.has(tc.id)) {
          entry.result = toolRowsByCallId.get(tc.id)
        }
        if (tc.id) renderedCallIds.add(tc.id)
        out.push(entry)
      }
    } else if (m.role === 'tool') {
      // Orphan tool row: skip when its assistant call already rendered it,
      // otherwise render the payload as the result (never as the tool name).
      if (m.tool_call_id && renderedCallIds.has(m.tool_call_id)) continue
      // Note: assistant rows come before their tool rows in created_at order,
      // but be defensive — check the whole index, not just rendered ones.
      if (m.tool_call_id && toolRowsByCallId.has(m.tool_call_id)) {
        const accounted = serverMsgs.some(
          (o) => o.tool_calls?.some((c: any) => (c?.function || c)?.id === m.tool_call_id),
        )
        if (accounted) continue
      }
      out.push({ ...base, role: 'tool', content: 'tool', result: m.content || '', tool_call_id: m.tool_call_id })
    } else {
      if (m.role === 'compaction') {
        if ((m as any).tokens_direct) base.tokens_before = (m as any).tokens_direct
        base.content = m.content || ''
      }
      out.push(base)
    }
  }
  return out
}

export interface ToolResultEvent {
  tool: string
  tool_call_id?: string
  params?: string
  result?: string
  error?: string
  duration_ms?: number
  tokens_direct?: number
}

// applyToolResult correlates a live tool_result event to its tool entry.
// Events carry tool_call_id (F3); matching by ID is exact. Events without an
// ID (compat window) fall back to the previous name-matching against the most
// recent pending entry. Returns the input array unchanged when nothing matches.
export function applyToolResult(messages: TranscriptMessage[], evt: ToolResultEvent): TranscriptMessage[] {
  let idx = -1
  if (evt.tool_call_id) {
    idx = messages.findIndex((m) => m.role === 'tool' && m.tool_call_id === evt.tool_call_id)
  }
  if (idx < 0) {
    for (let i = messages.length - 1; i >= 0; i--) {
      if (messages[i].role === 'tool' && !messages[i].result && messages[i].content === evt.tool) {
        idx = i
        break
      }
    }
  }
  if (idx < 0) return messages
  const updated = [...messages]
  updated[idx] = {
    ...updated[idx],
    params: evt.params,
    result: evt.error || evt.result,
    duration_ms: evt.duration_ms,
    tokens_direct: evt.tokens_direct,
  }
  return updated
}

// oldestPendingToolAgeMs returns the age of the longest-waiting result-less
// tool entry, or null when no tool is pending. The staleness watchdog uses it
// to detect tool_result events lost mid-connection.
export function oldestPendingToolAgeMs(messages: TranscriptMessage[], nowMs: number): number | null {
  let oldest: number | null = null
  for (const m of messages) {
    if (m.role !== 'tool' || m.result) continue
    if (!m.created_at) return Number.POSITIVE_INFINITY
    const age = nowMs - new Date(m.created_at).getTime()
    if (oldest === null || age > oldest) oldest = age
  }
  return oldest
}

// applySteeringMessage folds an engine steering event into the transcript.
// The sender already appended the text optimistically at send time, and
// reconnect_sync is authoritative — so this is a no-op when an identical user
// message is already present (prevents live duplicates), and appends in stream
// order otherwise (this is how other viewers sharing the session see it).
export function applySteeringMessage(messages: TranscriptMessage[], content: string): TranscriptMessage[] {
  for (const m of messages) {
    if (m.role === 'user' && m.content === content) return messages
  }
  return [...messages, { role: 'user', content, created_at: new Date().toISOString() }]
}
