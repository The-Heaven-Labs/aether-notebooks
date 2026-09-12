# Agent Chat UX Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Auto-collapse thinking blocks (latest open only, manual overrides while scrolled up, reset at live bottom) and make subagent detail genuinely live (fix view restoration, reconnect, persistence, token streaming, scroll UX).

**Architecture:** Extract the transcript renderer out of `AgentPanel.tsx` so main chat, subagent detail, and the read-only session viewer share it. Lift thinking open-state into a pure helper + controlled props. Fix the subagent view-ref desync and persist running tasks in the parent transcript; emit task-tagged token/reasoning events; reuse a stick-to-bottom hook with a new-message pill.

**Tech Stack:** React + TypeScript, Vitest + React Testing Library, Go agent engine, existing WS protocol.

**Design doc:** `docs/plans/2026-09-11-agent-chat-ux-design.md`

**Dependency:** the tool-timeouts plan, for `NoTimeout` classification used by the watchdog task.

---

### Task 1: Extract `AgentChatTranscript`

**Files:**
- Create: `web/src/components/AgentChatTranscript.tsx`
- Modify: `web/src/components/AgentPanel.tsx`
- Test: `web/src/components/AgentChatTranscript.test.tsx`

**Step 1:** Move `ChatMessage`, `MemoizedChatMessage`, `chatMarkdownComponents`, and the message list rendering from `AgentPanel.tsx` into `AgentChatTranscript.tsx`, exported:

```tsx
export interface ChatMessage { ... } // moved as-is
export interface AgentChatTranscriptProps {
  messages: ChatMessage[]
  streamingText?: string
  streamingReasoning?: string
  isStreaming?: boolean
  thinking: ThinkingController
  onSubagentSelect?: (taskId: string) => void
}
export function AgentChatTranscript(props: AgentChatTranscriptProps) { ... }
export const MemoizedChatMessage = memo(ChatMessageView)
```

`AgentPanel` keeps all WS/data logic and renders `<AgentChatTranscript ... />` for the main list; the subagent view renders the same component with subagent data.

**Step 2:** `cd web && npx tsc --noEmit` — must be clean; existing UI must render identically (no behavior change yet). Add a smoke RTL test that renders two user/assistant messages.

**Step 3: Commit:** `refactor(web): extract AgentChatTranscript from AgentPanel`

---

### Task 2: Thinking state helper

**Files:**
- Create: `web/src/utils/thinking.ts`
- Test: `web/src/utils/thinking.test.ts`

**Step 1: Failing test**

```ts
import { describe, it, expect } from 'vitest'
import { latestReasoningId, resolveThinkingOpen } from './thinking'

const msgs = [{ id: 'a', reasoning: 'x' }, { id: 'b' }, { id: 'c', reasoning: 'y' }]

describe('thinking', () => {
  it('picks the last message with reasoning', () => {
    expect(latestReasoningId(msgs as never)).toBe('c')
  })
  it('uses overrides when present', () => {
    expect(resolveThinkingOpen('a', 'c', { a: true })).toBe(true)
    expect(resolveThinkingOpen('a', 'c', { a: false })).toBe(false)
  })
  it('falls back to auto (latest open)', () => {
    expect(resolveThinkingOpen('c', 'c', {})).toBe(true)
    expect(resolveThinkingOpen('a', 'c', {})).toBe(false)
  })
  it('live streaming block is latest', () => {
    expect(latestReasoningId(msgs as never, true)).toBe('__live__')
  })
})
```

**Step 2–4:** run to fail (`cd web && npx vitest run src/utils/thinking.test.ts`), implement, pass.

```ts
export const LIVE_THINKING_ID = '__live__'
export function latestReasoningId(messages: { id?: string; reasoning?: string }[], liveStreaming: boolean): string | null {
  if (liveStreaming) return LIVE_THINKING_ID
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].reasoning) return messages[i].id ?? String(i)
  }
  return null
}
export function resolveThinkingOpen(id: string, latestId: string | null, overrides: Record<string, boolean>): boolean {
  return overrides[id] ?? id === latestId
}
```

**Step 5: Commit:** `feat(web): add thinking auto-collapse helper`

---

### Task 3: Wire auto-collapse into the transcript

**Files:**
- Modify: `web/src/components/AgentPanel.tsx`
- Modify: `web/src/components/AgentChatTranscript.tsx`
- Test: `web/src/components/AgentChatTranscript.test.tsx`

**Step 1: Failing RTL test** — render a transcript with two reasoning messages, `isAtBottom: true`: assert only the second thinking body is visible. Toggle the first open; assert both visible and that the toggle callback fired. Re-render with `isAtBottom: false → true` transition: assert the first is collapsed again.

**Step 2: Implement**

- `AgentPanel`: add `const [isAtBottom, setIsAtBottom] = useState(true)`; in the existing scroll handler, set it only when `nearBottom` changes. Add `const [thinkingOverrides, setThinkingOverrides] = useState<Record<string, boolean>>({})` and an effect:

```ts
useEffect(() => {
  if (isAtBottom) {
    setThinkingOverrides({})
    setThinkingOpen(true)
  }
}, [isAtBottom])
```

- Compute `latestId = latestReasoningId(messages, isStreaming && !!currentStreamingReasoning)` and pass `thinking: { latestId, overrides, onToggle }` to the transcript. `onToggle = useCallback((id) => setThinkingOverrides(o => ({ ...o, [id]: !resolveThinkingOpen(id, latestId, o) })), [latestId])`.
- `AgentChatTranscript`: committed message view uses `resolveThinkingOpen(msg.id, latestId, overrides)` instead of local `thoughtOpen`; live `<details>` uses `resolveThinkingOpen(LIVE_THINKING_ID, ...)` with `onToggle` and no local `thinkingOpen` state.
- Verify the auto-scroll layout effect still re-scrolls when collapsing while at bottom (it is keyed on `messages`/streaming; collapse only shrinks content, and the shrink branch realigns when `wasAtBottomRef` is true).

**Step 3:** `npx vitest run src/components/AgentChatTranscript.test.tsx` + `npx tsc --noEmit`.

**Step 4: Commit:** `feat(web): auto-collapse thinking blocks to the latest one`

---

### Task 4: Subagent view lifecycle fixes

**Files:**
- Modify: `web/src/components/AgentPanel.tsx`
- Test: `web/src/components/AgentPanel.subagent.test.tsx` (create; mock `fetch` and `WebSocket`)

**Step 1: Failing tests**

1. With `localStorage['aether:subagentView'] = 'task-1'`, mount `AgentPanel` → `fetch('/api/v1/agents/subagent/task-1/messages')` is called once.
2. Simulate a `subagent_message` event over the mocked WS for the restored view → the message list grows (currently dropped because the ref is null).
3. Simulate `reconnect_sync` while a view is open → subagent messages are re-fetched.

**Step 2: Implement**

- Initialize `subagentViewRef` from `subagentView` and keep them in sync:

```ts
useEffect(() => {
  subagentViewRef.current = subagentView
  if (subagentView) fetchSubagentMessages(subagentView, setSubagentMessages, setSubagentLoading)
  else setSubagentMessages([])
}, [subagentView])
```

- Remove the imperative fetch from the click handlers (just `setSubagentView(id)`; the effect fetches). Keep `mainScrollRef` capture in the click handler.
- In the `reconnect_sync` handler, after applying the snapshot: `if (subagentViewRef.current) fetchSubagentMessages(subagentViewRef.current, ...)`.
- Back button: `setSubagentView(null)` (effect clears the ref/messages).

**Step 3:** run tests, commit: `fix(web): keep subagent view live across remounts and resyncs`

---

### Task 5: Persist running subagent rows (Go)

**Files:**
- Modify: `internal/agent/subagent.go` (`RunQueuedTasks`)
- Test: `internal/agent/subagent_test.go` (create/extend)

**Step 1: Failing test** — call `RunQueuedTasks` with a stub LLM that blocks briefly; while running, query `agent_messages` for the parent session and assert a `role='subagent'` row exists with `tool_calls[0]->>'task_id' = tid` and `status='running'`; after completion assert the same single row is updated to `completed` (not duplicated).

**Step 2: Implement**

When marking a task running (existing `UPDATE subagent_tasks SET status='running'`), upsert the parent message:

```go
tcJSON, _ := json.Marshal([]map[string]any{{"name": g, "arguments": map[string]any{"task_id": tid, "status": "running", "goal": g}}})
e.pool.Exec(ctx, `
    INSERT INTO agent_messages (session_id, role, content, tool_calls, created_at)
    SELECT $1, 'subagent', $2, $3, NOW()
    WHERE NOT EXISTS (
        SELECT 1 FROM agent_messages WHERE session_id = $1 AND role = 'subagent' AND content = $2
    )`, parentSessionID, tid, tcJSON)
```

Replace the completion-only `INSERT` with the same upsert carrying final `status/result/error`, so restarts/reconnects never duplicate.

**Step 3:** ensure `mapServerMessagesToChat` (`web/src/utils/agentTranscript.ts`) maps this row to the same placeholder shape as the live `subagent_status` handler (goal/status/result from `tool_calls[0].arguments`); add a Vitest case.

**Step 4: Commit:** `fix(agent): persist running subagent rows in the parent transcript`

---

### Task 6: Subagent token/reasoning streaming

**Files:**
- Modify: `internal/agent/subagent.go` (`runSubagentLoop`, ~286-436)
- Modify: `web/src/types/agent.ts` (add `subagent_token`, `subagent_reasoning`)
- Modify: `web/src/components/AgentPanel.tsx` (buffer + render)
- Modify: `web/src/utils/agentTranscript.ts` (pure reducer for subagent deltas)
- Test: Go subagent test, `web/src/utils/agentTranscript.test.ts`

**Step 1: Go failing test** — subscribe to the session stream, run a subagent with a stub LLM returning `"hello world"`, assert `subagent_token` events tagged with the task id arrive, and their concatenation equals the response text.

**Step 2: Implement (Go)** — after `resp, err := subagentLLM.Chat(...)` and saving the message, mirror the main engine's chunked emission. Use a coarse chunk to bound event volume (the stream keeps a 500-event rolling buffer):

```go
if resp != nil && len(resp.Choices) > 0 {
    msg := resp.Choices[0].Message
    chunkStr(msg.Content, 16, func(s string) {
        e.PublishSessionEvent(parentSessionID, map[string]any{"type": "subagent_token", "task_id": taskID, "delta": s})
        time.Sleep(8 * time.Millisecond)
    })
    chunkStr(msg.ReasoningContent, 16, func(s string) {
        e.PublishSessionEvent(parentSessionID, map[string]any{"type": "subagent_reasoning", "task_id": taskID, "delta": s})
        time.Sleep(8 * time.Millisecond)
    })
}
```

(`chunk` already exists in `engine.go`; reuse or expose it.)

**Step 3: Implement (TS)** — types union gains the two events. `agentTranscript.ts` gains:

```ts
export function applySubagentDelta(
  buffers: Record<string, { text: string; reasoning: string }>,
  taskId: string, kind: 'text' | 'reasoning', delta: string
) { /* append; return new object */ }

export function commitSubagentDelta(buffers, taskId) { /* drop the buffer */ }
```

`AgentPanel` keeps `subagentStreaming` state, appends on events when `subagentViewRef.current === task_id`, renders a synthetic trailing `ChatMessage` from the buffer, and clears the buffer on `subagent_message`. Use the same thinking rules for the live reasoning block.

**Step 4:** tests pass; commit: `feat(agent): stream subagent tokens and reasoning`

---

### Task 7: Stick-to-bottom + new-message pill (subagent detail)

**Files:**
- Create: `web/src/hooks/useStickToBottom.ts`
- Modify: `web/src/components/AgentPanel.tsx` (subagent list and main list)
- Test: `web/src/hooks/useStickToBottom.test.ts` (happy-dom), component test for the pill

**Step 1:** Extract the main list's logic (`wasAtBottomRef`, scroll guard, `useLayoutEffect`) into the hook with a `forceScroll` command, and use it for the subagent scroller, replacing the unconditional `scrollTop = scrollHeight` effect.

**Step 2:** When not at bottom and new content arrives, show a `New messages` pill over the subagent scroller; clicking scrolls to bottom and clears the count. Keep the main list behavior unchanged.

**Step 3:** tests + commit: `feat(web): stick-to-bottom and new-message pill for subagent detail`

---

### Task 8: Fix subagent notebook scope + watchdog

**Files:**
- Modify: `internal/agent/subagent.go` (`ToolContext.NotebookID`)
- Modify: `web/src/utils/agentTranscript.ts` (`oldestPendingToolAgeMs`)
- Test: `internal/agent/subagent_test.go`, `web/src/utils/agentTranscript.test.ts`

**Step 1:** Go test: subagent tool execution receives the parent notebook id (not the task id). Change `NotebookID: taskID` to the `notebookID` parameter already passed through `RunQueuedTasks` → `runSubagentLoop`.

**Step 2:** `oldestPendingToolAgeMs` skips tools that legitimately run long:

```ts
const LONG_RUNNING_TOOLS = new Set(['spawn_subagents'])
```

and ignores pending tool entries whose name is in the set. Add a Vitest case: a 10-minute-old pending `spawn_subagents` yields `null`; a 10-minute-old pending `run_cell` still returns the age.

**Step 3:** commit: `fix: subagent notebook scope and stale-tool watchdog false positives`

---

## Verification

- `cd web && npx tsc --noEmit && npm run build`
- `cd web && npx vitest run`
- `task check` (Go tests)
- Manual with docker dev stack: long subagent run while viewing detail (live tokens, pill, reconnect mid-run), thinking collapse while scrolling history.
