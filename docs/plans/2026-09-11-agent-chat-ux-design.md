# Agent Chat UX — Thinking Auto-Collapse & Subagent Live View — Design

**Date:** 2026-09-11
**Status:** Approved (design decisions D7–D8 settled)

## Part 1 — Thinking blocks auto-collapse

### Current behavior

- Every committed thinking block renders open forever (`MemoizedChatMessage`, local
  `thoughtOpen = useState(true)`), so a long session accumulates many open reasoning blocks.
- The live streaming thinking block (`<details>` controlled by `thinkingOpen`) is a second,
  separate surface.
- “Is the user at the bottom?” exists only as `wasAtBottomRef` (a ref), so nothing re-renders
  when the user leaves/returns to the live chat.

### Behavior spec

- **Auto rule:** only the latest reasoning block is open — the live streaming block while
  reasoning streams, otherwise the most recent committed message that has `reasoning`.
- **Manual override:** if the user toggles a block, that choice sticks while they are reading
  history (scrolled up).
- **Return to live:** when the user scrolls back to the bottom and resumes following the live
  chat, manual overrides are cleared and the auto rule takes over again (all but the latest
  collapse). The live block is opened again on return.
- Collapsing/expanding must not fight the existing auto-scroll machinery
  (`scrollGuard`, `forceScrollRef`, `prevScrollHeightRef`), and must not jump the viewport
  when the user is reading history.

### Implementation

- Lift thinking open-state into the transcript owner:
  - `manualThinkingOverrides: Record<string, boolean>` (message id → user-chosen open).
  - `latestReasoningId`: last message id with `reasoning`, or sentinel for the live block.
  - `effectiveThinkingOpen(msgId) = overrides[msgId] ?? (msgId === latestReasoningId)`.
- Add `isAtBottom` **state** alongside the existing ref. The scroll handler computes
  `nearBottom` (30px threshold, same as today) and calls `setIsAtBottom` only on change, so
  scroll events do not cause render churn.
- Effect on `isAtBottom: false → true`: clear `manualThinkingOverrides`, set live block open.
- `MemoizedChatMessage` becomes controlled for thinking: `thinkingOpen` + stable
  `onToggleThinking(id)` props; remove local `thoughtOpen`.
- The same rules apply wherever a transcript renders: main panel, subagent detail, read-only
  session viewer (all use the extracted `AgentChatTranscript`).

### Tests

- Pure helper `resolveThinkingOpen(latestReasoningId, overrides, id)` in
  `web/src/utils/` with Vitest unit tests.
- React Testing Library test for the transcript: two reasoning messages → only the last is
  open; toggle an older one while “not at bottom” persists; flipping `isAtBottom` true clears
  overrides and re-collapses it.

## Part 2 — Subagent live view

### Root causes (verified)

1. **Ref desync:** `subagentView` is restored from `localStorage`, but `subagentViewRef`
   starts `null` and is never synced. After a remount (refresh, minimize, navigation), live
   `subagent_message` events are dropped (`if (subagentViewRef.current === msg.task_id)`) and
   no REST fetch is issued on mount. Only Back → click again re-fetches.
2. **Reconnect gap:** `reconnect_sync` jumps `lastSeqRef` to `server_seq` and only carries
   `agent_messages`, so buffered `subagent_message` frames (seq ≤ server_seq) are dropped and
   cannot be recovered from the sync payload. The 120s stale-tool watchdog fires during every
   long `spawn_subagents` run, making this routine.
3. **Running tasks are not persisted:** the parent transcript gets a `role='subagent'` row
   only at completion, so a running subagent disappears from the transcript on any reconnect.
4. **No token stream:** `runSubagentLoop` uses non-streaming `LLMClient.Chat` and emits only
   whole messages, so the detail view looks frozen during long generations.
5. **Mis-scoped notebook:** the subagent `ToolContext.NotebookID` is set to the **task ID**
   (`subagent.go`), so notebook broadcasts from subagent tools target a bogus room.
6. **Naive scroll:** subagent detail always jumps to the bottom on any new message.
7. The 120s stale-tool watchdog treats the long-running `spawn_subagents` call as stuck and
   forces resyncs.

### Fixes

**View lifecycle**

- Sync `subagentViewRef` from `subagentView` in one effect; fetch subagent messages on mount
  when a view was restored from storage.
- On `reconnect_sync`, if a subagent view is open, re-fetch that task's messages (REST is the
  source of truth for `subagent_messages`).
- Live appends gate on the state-synced ref so owner and read-only viewers behave the same.

**Persistence**

- Insert the parent `role='subagent'` row when a task starts, carrying
  `{task_id, goal, status, result, error}` in `tool_calls`; UPDATE the same row
  (`session_id + role='subagent' + content=task_id`) on running/completed/failed transitions
  instead of inserting only at the end. No migration needed; no unique constraint required.
- `mapServerMessagesToChat` maps these rows to the same placeholder shape the live
  `subagent_status` event produces, so reconnect preserves running placeholders.

**Token/reasoning streaming (D7)**

- Mirror the main chat’s chunked emission (`chunk()` + small sleeps in `engine.go`) in
  `runSubagentLoop`: emit `subagent_token` and `subagent_reasoning` events tagged with
  `task_id` while an assistant response is being produced, before saving the message.
  `ChatStream` exists but is currently unused; this design deliberately matches the main
  chat’s simulated streaming instead of switching transports.
- Frontend: for the viewed task, append deltas to a live assistant bubble; commit and clear
  the buffer when the corresponding `subagent_message` (assistant/tool) arrives. Live
  reasoning uses the same auto-collapse behavior as the main transcript.

**UX (D8)**

- Extract the main chat’s stick-to-bottom logic into a reusable hook and use it for subagent
  detail. When the user is scrolled up and new subagent content arrives, show a
  “N new messages” pill; clicking it jumps to the bottom and resumes following.

**Correctness**

- Pass the parent notebook ID into the subagent `ToolContext` instead of the task ID.
- Exclude tools with no configured timeout (`spawn_subagents`; see the tool-timeouts design)
  from the 120s stale-tool watchdog, or otherwise suppress the false-positive resync during
  subagent runs.

### Tests

- Go: subagent start persists a running row; completion updates it; token/reasoning events
  carry the task id; `ToolContext.NotebookID == parent notebook`.
- Vitest: reducer handles `subagent_token`/`subagent_reasoning` buffering and commit on
  `subagent_message`; pill visibility logic.
- Component test: restore `subagentView` from storage → fetch issued, live event appended
  without re-entering the view.

## Sequencing

1. Transcript extraction (shared with the session-sharing design).
2. Thinking auto-collapse.
3. Subagent lifecycle fixes (ref sync, fetch-on-restore, reconnect re-fetch, persistence).
4. Subagent token/reasoning streaming.
5. Stick-to-bottom + new-message pill.
