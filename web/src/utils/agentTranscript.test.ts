import { describe, it, expect } from 'vitest'
import { mapServerMessagesToChat, applyToolResult, oldestPendingToolAgeMs, applySteeringMessage } from '../utils/agentTranscript'

const callId = 'call-1'

function assistantWithCall(overrides: Record<string, unknown> = {}) {
  return {
    id: 'msg-a',
    role: 'assistant',
    content: '',
    tool_calls: [{ id: callId, name: 'run_cell', arguments: { cell_id: 'c1' }, ...((overrides as any).toolCall ?? {}) }],
    created_at: '2026-09-11T00:00:00Z',
    ...overrides,
  } as any
}

describe('mapServerMessagesToChat', () => {
  it('renders completed tools from persisted tool_calls (no phantom Working…)', () => {
    const msgs = mapServerMessagesToChat([
      assistantWithCall({ toolCall: { result: { cell_id: 'c1', status: 'completed' }, duration_ms: 120 } }),
    ])
    expect(msgs).toHaveLength(1)
    expect(msgs[0].role).toBe('tool')
    expect(msgs[0].content).toBe('run_cell')
    expect(msgs[0].result).toContain('completed')
    expect(msgs[0].tool_call_id).toBe(callId)
    expect(msgs[0].duration_ms).toBe(120)
  })

  it('renders one entry per tool_call in a multi-call message', () => {
    const msgs = mapServerMessagesToChat([
      {
        id: 'msg-a',
        role: 'assistant',
        content: '',
        tool_calls: [
          { id: 'c1', name: 'list_cells', arguments: {} },
          { id: 'c2', name: 'run_cell', arguments: { cell_id: 'x' }, result: { status: 'completed' } },
        ],
        created_at: '2026-09-11T00:00:00Z',
      } as any,
    ])
    expect(msgs).toHaveLength(2)
    expect(msgs[0].content).toBe('list_cells')
    expect(msgs[0].result).toBeUndefined()
    expect(msgs[1].content).toBe('run_cell')
    expect(msgs[1].result).toContain('completed')
  })

  it('folds matching role=tool rows into their assistant call (no doubles)', () => {
    const msgs = mapServerMessagesToChat([
      assistantWithCall({ toolCall: { result: { status: 'completed' } } }),
      { id: 'msg-t', role: 'tool', content: '{"status":"completed"}', tool_call_id: callId, created_at: '2026-09-11T00:00:01Z' } as any,
    ])
    expect(msgs).toHaveLength(1)
    expect(msgs[0].content).toBe('run_cell')
  })

  it('heals legacy history by joining tool rows onto result-less calls', () => {
    const msgs = mapServerMessagesToChat([
      assistantWithCall(),
      { id: 'msg-t', role: 'tool', content: '{"status":"completed"}', tool_call_id: callId, created_at: '2026-09-11T00:00:01Z' } as any,
    ])
    expect(msgs).toHaveLength(1)
    expect(msgs[0].result).toBe('{"status":"completed"}')
  })

  it('renders orphan tool rows as results, never as garbled names', () => {
    const msgs = mapServerMessagesToChat([
      { id: 'msg-t', role: 'tool', content: '{"status":"completed"}', tool_call_id: 'orphan', created_at: '2026-09-11T00:00:01Z' } as any,
    ])
    expect(msgs).toHaveLength(1)
    expect(msgs[0].content).toBe('tool')
    expect(msgs[0].result).toBe('{"status":"completed"}')
  })

  it('passes plain messages through untouched', () => {
    const msgs = mapServerMessagesToChat([
      { id: 'u1', role: 'user', content: 'hi', created_at: '2026-09-11T00:00:00Z' } as any,
      { id: 'a1', role: 'assistant', content: 'hello', created_at: '2026-09-11T00:00:01Z' } as any,
    ])
    expect(msgs.map((m) => m.content)).toEqual(['hi', 'hello'])
  })

  it('maps compaction rows to before/after token counts', () => {
    const msgs = mapServerMessagesToChat([
      {
        id: 'c1',
        role: 'compaction',
        content: 'summary of earlier turns',
        tokens_direct: 1200,
        tokens_after: 400,
        created_at: '2026-09-11T00:00:02Z',
      },
    ])
    expect(msgs).toHaveLength(1)
    expect(msgs[0].role).toBe('compaction')
    expect(msgs[0].tokens_before).toBe(1200)
    expect(msgs[0].tokens_after).toBe(400)
    expect(msgs[0].content).toBe('summary of earlier turns')
  })
})

describe('applyToolResult', () => {
  const pending = (content: string, id?: string) => ({ role: 'tool', content, tool_call_id: id, created_at: '2026-09-11T00:00:00Z' })

  it('correlates by tool_call_id even with a wrong name in the same turn', () => {
    const msgs = [pending('run_cell', 'id-1'), pending('run_cell', 'id-2')]
    const out = applyToolResult(msgs, { tool: 'list_cells', tool_call_id: 'id-2', result: '{"ok":true}' })
    expect(out[0].result).toBeUndefined()
    expect(out[1].result).toBe('{"ok":true}')
  })

  it('falls back to name matching for events without an ID', () => {
    const msgs = [pending('run_cell')]
    const out = applyToolResult(msgs, { tool: 'run_cell', result: 'done' })
    expect(out[0].result).toBe('done')
  })

  it('prefers the most recent pending entry on name fallback', () => {
    const msgs = [pending('run_cell'), pending('run_cell')]
    const out = applyToolResult(msgs, { tool: 'run_cell', result: 'second' })
    expect(out[0].result).toBeUndefined()
    expect(out[1].result).toBe('second')
  })

  it('returns the input unchanged when nothing matches', () => {
    const msgs = [pending('run_cell', 'id-1')]
    expect(applyToolResult(msgs, { tool: 'other', tool_call_id: 'nope', result: 'x' })).toBe(msgs)
  })

  it('surfaces error over result', () => {
    const out = applyToolResult([pending('run_cell', 'id-1')], { tool: 'run_cell', tool_call_id: 'id-1', result: '', error: 'boom' })
    expect(out[0].result).toBe('boom')
  })
})

describe('oldestPendingToolAgeMs', () => {
  it('returns null when nothing is pending', () => {
    expect(oldestPendingToolAgeMs([{ role: 'tool', content: 'x', result: 'y' }], 1000)).toBeNull()
    expect(oldestPendingToolAgeMs([], 1000)).toBeNull()
  })

  it('returns the oldest pending age', () => {
    const now = new Date('2026-09-11T00:02:00Z').getTime()
    const msgs = [
      { role: 'tool', content: 'a', created_at: '2026-09-11T00:01:30Z' },
      { role: 'tool', content: 'b', created_at: '2026-09-11T00:00:00Z' },
    ]
    expect(oldestPendingToolAgeMs(msgs, now)).toBe(120000)
  })
})

describe('applySteeringMessage', () => {
  it('appends steered text for viewers without an optimistic entry', () => {
    const msgs = [{ role: 'assistant', content: 'working on it' }]
    const out = applySteeringMessage(msgs, 'actually use the other table')
    expect(out).toHaveLength(2)
    expect(out[1]).toMatchObject({ role: 'user', content: 'actually use the other table' })
  })

  it('dedupes against the sender optimistic entry', () => {
    const msgs = [
      { role: 'user', content: 'actually use the other table' },
      { role: 'tool', content: 'run_cell' },
    ]
    expect(applySteeringMessage(msgs, 'actually use the other table')).toBe(msgs)
  })

  it('does not drop distinct follow-ups', () => {
    const msgs = [{ role: 'user', content: 'first steer' }]
    const out = applySteeringMessage(msgs, 'second steer')
    expect(out).toHaveLength(2)
  })
})
