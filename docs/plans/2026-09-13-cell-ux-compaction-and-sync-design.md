# Cell UX, Agent Tools, Compaction & Sync — Design

**Date:** 2026-09-13
**Status:** Approved (single-PR scope)
**Branch:** `feat/cell-ux-compaction-and-sync`
**Source:** `IMPROVEMENTS.md` items 1–7

## Context

Seven independent problems reported after the chart/legend and MCP work. All were investigated on current `main` (code reading plus live browser reproduction where practical); root causes are confirmed with file:line evidence below. The user chose to ship all seven in **one PR**.

| # | Problem | Root cause (short) | Area |
|---|---|---|---|
| P1 | `create_cell` limit not configurable | Hardcoded `1000` in INSERT + broadcast | Go agent tools |
| P2 | Agent-created cells don't get focus | Hub WS path only flashes when following; `focusedCellId` never set | Frontend |
| P3 | Cell `description` unused | Required by `create_cell`, stored, rendered nowhere | Full stack |
| P4 | Detail-view Ctrl+C hijacks selection | Global keydown `preventDefault()` + copies full value | Frontend |
| P5 | Auto-compaction invisible | Summary skipped on history rebuild; divider shows `before → before` | Engine + frontend |
| P6 | Token usage stale after compaction | Headline is per-turn cumulative; `context_current` dropped on `done`; double-add | Engine + frontend |
| P7 | `update_cell` stale until F5 | `attachCollab` sync-clobber race; dead-source broadcast; no re-fetch on WS reconnect | Frontend + Go |

### Key decisions (user-approved)

1. **Compaction becomes durable** — the summary is the context boundary for all future turns, and the UI tells the truth in live, reload and history paths.
2. **Token totals are session-scoped and compaction-aware** — headline = current context / window; hover = live breakdown + session totals (including compaction and subagent calls); new session/`/summarize` fork resets.
3. **P7 hardens the WS path only** — the relay↔API Yjs pipeline repair (401s) is a documented follow-up, not in this PR.
4. **`cells.description` is removed fully** — including a drop-column migration (destructive, explicitly approved).
5. **Focus means scroll + highlight + `focusedCellId`** — never steals keyboard focus from the chat input; applies regardless of the follow-agent toggle.
6. Everything ships in **one PR** on `feat/cell-ux-compaction-and-sync`.

### Migrations

- `V100__drop_cells_description.sql` — `ALTER TABLE cells DROP COLUMN IF EXISTS description;`
- `V101__agent_usage.sql` — `agent_messages.tokens_after INT` (nullable) plus `agent_sessions` columns: `context_tokens INT`, `context_window INT`, `total_input BIGINT`, `total_output BIGINT`, `total_reasoning BIGINT`, `total_cache_read BIGINT`, `total_model_calls INT`, `total_subagent_input BIGINT`, `total_subagent_output BIGINT` (all `NOT NULL DEFAULT 0`).
- `V102__agent_message_kept_count.sql` — `agent_messages.kept_count INT` (nullable): how many tail messages a compaction kept out of the summary, so the durable rebuild retains them alongside the injected summary.

Migration directory is `internal/database/migrations/` (embedded; AGENTS.md's `migrations/` path is stale). V102 is the current head.

---

## P1 — `create_cell` limit parameter

**Root cause:** `internal/agent/tools_notebook.go:536` hardcodes `1000` in the INSERT; `:565` broadcasts `"limit": 1000`. The `<select>` in `web/src/components/Cell.tsx:566-584` already supports 1000/100/10/Unlimited (NULL = unlimited), and the executor appends `LIMIT N` only when the column is non-null and > 0.

**Design**

- Add optional `limit` integer to the `create_cell` tool schema (prose + parameters). Semantics: omitted → `1000`; `0` → `NULL` (Unlimited); negative → validation error. No upper cap (the UI offers Unlimited).
- `req.Limit *int`; INSERT binds the effective value (`nil` for unlimited); the `cell_created` broadcast and the tool result carry the effective limit.
- Scope is `create_cell` only; `update_cell` can gain `limit` later if needed.

**Tests:** default (1000), explicit (e.g. 100), `0` → NULL, negative rejected. Assert the broadcast payload limit too.

---

## P2 — Focus agent-created cells

**Root cause:** `web/src/pages/NotebookPage.tsx:309-340` only calls `flashCell` when `shouldScroll(userEmail)` (manual follow toggle), never sets `focusedCellId` (`:220`); `AgentPanel.tsx:791-795` scrolls only while the panel is mounted, so a minimized/closed panel means no movement at all. Run completion has the same gap.

**Design**

- In the hub WS callbacks, treat `user_email === 'agent@aether'` as an implicit follow target:
  - `cell_created` → append (existing), scroll + highlight, and `setFocusedCellId(id)`.
  - `cell_output` → scroll + highlight on completion.
  - Human-originated events keep today's follow-toggle semantics; `cell_executing` behavior is unchanged.
- Debounce with a small queue (~250 ms, last cell wins) so a batch of creates doesn't thrash the viewport. Flush on unmount.
- No DOM focus changes: the chat input keeps the keyboard. `flashCell` already does `scrollIntoView` + highlight.
- Extract the queue/policy as a small pure helper (e.g. `web/src/utils/agentFocus.ts`) for unit tests.
- `AgentPanel` keeps its invalidation/cache behavior; duplicate scrolls to the same cell are harmless.

**Tests:** Vitest with fake timers for the debounce helper (last-wins, flush, cleanup); assertion that agent events bypass the follow toggle.

---

## P3 — Remove cell description

**Root cause:** `create_cell` requires it (`tools_notebook.go:480-482`), the column is written and returned by REST/snapshots/`read_cell`, but nothing in `web/` renders it (no reference in `Cell.tsx`, `MarkdownCell.tsx`, `OutputRenderer.tsx`, export or print). The user wants SQL comments instead.

**Design (full removal)**

- Migration `V100` drops `cells.description`.
- Remove from `models.Cell` and `models.SnapshotCell`.
- Remove every cell-path SELECT/INSERT/UPDATE/RETURNING/scan: `internal/api/cell_handlers.go` (create/update/duplicate), `internal/api/cell_history.go:220-225`, `internal/api/notebook_handlers.go` (create-with-cells, get notebook, clone), `internal/agent/snapshot.go` (capture + restore).
- Agent tools: delete `description` from `create_cell` (schema, required list, validation, INSERT, broadcast), `update_cell` (schema, SQL), and `read_cell` (struct, SELECT, output map).
- CLI `internal/cli/types.go:41`; frontend `useNotebookWs.ts:64` whitelist entry.
- Tests: delete/rewrite `TestAgentCreateCellRequiresTitleAndDescription`, `TestAgentCreateCellPersistsTitleDescription`, and the description rows of the validation table; remove description args from fixtures (`tools_notebook_test.go`, `mcp_pat_test.go:44`).
- Legacy snapshot JSON containing `description` is ignored on unmarshal; restore simply stops writing it. No backfill (accepted data loss).
- No swagger regen needed (cell endpoints use `body object`).

**Risks:** MCP `read_cell` no longer returns `description`; API response shape change (no in-repo consumers).

---

## P4 — Detail-view Ctrl+C

**Root cause (reproduced live):** `web/src/components/OutputRenderer.tsx:537-546` registers a `window` keydown handler that, on Ctrl/Cmd+C, `preventDefault()`s and calls `copyDetail()` (whole `detail.value`, `:356-362`) whenever `document.activeElement` isn't an input/CodeMirror. A selection in the detail `<pre>`, notebook title, or agent panel therefore gets replaced with the full cell value. Also `activeDetailCellId` (`:11-22`) is never reset on unmount, so `isAnyDetailActive()` (`useNotebookKeyboardShortcuts.ts:50`) can stay stuck.

**Design**

- Remove the Ctrl+C branch from the keydown handler (keep Escape/arrow navigation).
- Register a `copy` listener while the detail is open:
  - if the event target is an editable (INPUT/TEXTAREA/`.cm-editor`) → return;
  - if `window.getSelection()` is non-collapsed with text → return (native copy);
  - otherwise `preventDefault()` + `e.clipboardData.setData('text/plain', detail.value)`.
  This preserves "Ctrl+C copies the whole value when nothing is selected", fixes all selection cases, and removes the silent no-op when the Clipboard API is unavailable.
- Reset `activeDetailCellId` on unmount when it still points at this cell.

**Tests:** component tests for selection vs no-selection copy; unmount resets the global.

---

## P5 — Durable, visible compaction

**Root cause:** the live event and divider exist (`engine.go:884-909`, `agent_ws.go:439-444`, `AgentPanel.tsx:811-819`, divider `:105-121`) but:
1. `engine.go:590-593` skips every `role='compaction'` row when rebuilding history, so the summary is never re-injected and the next turn re-sends the full pre-compaction history — compaction only saves context *within* one turn.
2. The event sends `{Input: before, ContextCurrent: before}` (`engine.go:904`) → the divider renders `X → X`.
3. Reconnect/history never have after-counts (`agentTranscript.ts:107-110`), and the messages endpoint does not return `tokens_direct`/`tokens_after`.
4. The summarization LLM call's usage is discarded (`engine.go:278-280`).

**Design**

- **Boundary semantics:** on rebuild, find the latest non-empty compaction row deterministically (`created_at, id`; add the id tiebreaker to `GetMessages`), inject its summary as a system message immediately after the main system prompt, and rewind to the start of the tail it kept (`kept_count`, V102; `8` for legacy rows) so the prompt is summary, then the unsummarized tail, then post-boundary messages. Older compaction rows inside the window are superseded and skipped; messages before the window are summarized away. Existing `sanitizeChatMessages` (`engine.go:309-368`, already called at `:645`) heals tool-call pairing at the cut.
- **Truthful counts:** `compactChatHistory` returns the summarization usage and computes `after := tokenCounter.CountMessages(compacted)`. The compaction row stores `tokens_direct = before` (existing) and `tokens_after = after` (new column). The live event sends `{Input: before, ContextCurrent: after}`.
- **Plumbing:** messages endpoint and `reconnect_sync` return `tokens_direct` and `tokens_after`; `agentTranscript.ts` maps both; the divider renders `before → after (~)`; `SessionHistory` renders compaction as the same summary block instead of a generic tool bubble.
- `/summarize` stays a session fork.
- Summarization usage is added to session totals (P6).

**Tests:** update `TestCompactionEmitsEventAndPersists` expectations (durability + after-counts, exactly one injected summary, summarized prefix absent, kept tail retained); new unit tests that a second `ProcessMessage` sends the summary immediately after the system prompt followed by the retained tail and post-boundary messages, and that back-to-back compactions inject only the latest summary; divider rendering with after-counts from live and mapped rows.

---

## P6 — Token meter and session totals

**Root cause:** headline math is `(totalTokens.input + totalTokens.output)/contextWindow` (`AgentPanel.tsx:1504-1507`) where `Input` is the per-turn cumulative `apiInputTotal` (`engine.go:853,867`); `context_current` is emitted per call but dropped by the `done` handler (`AgentPanel.tsx:829`) and never persisted; `done` also *adds* the turn cumulative on top of the last `token_update` (double count). Subagent usage is only accumulated client-side and is lost on reconnect. No session aggregate exists server-side (`agent_sessions.max_tokens` is dead schema).

**Design**

- Persisted session usage (V101 columns listed above), maintained by the engine:
  - after every LLM call: add that call's prompt/completion/reasoning/cached tokens, `model_calls += 1`, set `context_tokens = resp.Usage.PromptTokens`;
  - after the summarization call: same (compaction is part of session cost);
  - on subagent completion: add `subagent_tasks.tokens_input/output` to the parent session's `total_subagent_*`.
  Writes go through a small `SessionStore.AddUsage(ctx, sessionID, delta)` helper; the engine mirrors the counters in memory for events.
- Events: `token_update` and `done` gain an additive `session_usage` object (`input/output/reasoning/cache_read/model_calls/subagent_input/subagent_output/context_tokens/context_window`); `done` also carries `context_current`. The frontend replaces state from events instead of doing arithmetic.
- Reconnect/resume: `reconnect_sync` (and the session info response) include `context_tokens`, `context_window` and `session_usage`, so a refresh restores the meter; `hasCompacted` is persisted in `AgentChatState`.
- UI:
  - headline = `context_current / context_window` (fallback to persisted `context_tokens`);
  - hover = live per-component estimate breakdown + "This session" section (input/output/reasoning/cache, model calls, subagent tokens);
  - the old cumulative sum and the client-side subagent accumulation are removed.
- Wire changes are additive JSON fields; old clients ignore them.

**Tests:** engine test asserting persisted deltas after calls/compaction/subagent completion; frontend reducer tests for set-not-add semantics and headline math; reconnect restore test.

---

## P7 — Stale `update_cell` until F5

**Root causes (race reproduced live):**
1. `Cell.tsx:319-337` — when the Yjs provider syncs late, `attachCollab` overwrites Yjs with the *stale editor buffer* if `yjsContent !== editorContent`. The source effect (`:358-372`) had already advanced `lastSourceRef`, so the new DB content is never re-pushed and the editor stays old until F5.
2. `tools_notebook.go:652-658` — `cell_updated` always broadcasts `source`, even for title-only updates; the frontend applies `source: ""` for agent updates, wiping the editor.
3. `useNotebookWs.ts:64` — the key whitelist omits `agent_updated_at`, so the 5s autosave suppression never sees fresh agent timestamps (confirmed in DB: autosave fired 1.5 s after an agent write).
4. Notebook WS reconnects (`useNotebookWs.ts:86-94`) never re-fetch cells after missed messages.
5. The relay↔API Yjs pipeline is dead: relay calls `/internal/yjs/*` without auth (`relay/src/index.ts:20,29`) while the endpoints require Bearer JWT (`internal_handlers.go:20-31`) → 401. **Out of scope** (follow-up), but informs the client strategy: live Yjs docs are currently session-local.

**Design (WS-path hardening)**

- `Cell.tsx`: on sync, seed ytext from the editor **only when ytext is empty**; never overwrite non-empty Yjs with the editor buffer. Otherwise reconfiguring `yCollab` applies Yjs to the editor, which fixes the race (source effect already wrote ytext). The source effect uses an existing collab instance via a new non-creating accessor (no provider churn / refcount leak); when no provider exists the DB cache drives the editor and `attachCollab` seeds on mount.
- Broadcast hygiene: `update_cell` takes `source *string` (missing vs empty is meaningful); include `source` in the broadcast only when provided and non-empty; always include `agent_updated_at` and `updated_at`. Same timestamp fields for REST cell updates. Frontend whitelist learns both.
- Notebook WS: add an `onReconnect` callback; `NotebookPage` invalidates the notebook query on re-open after a drop, so missed messages self-heal.

**Tests:** Cell sync-race unit test (mock unsynced provider, external source change, then sync → editor shows new content); broadcast payload test (title-only update omits `source`); whitelist + reconnect invalidation tests.

### Documented follow-up (out of scope): repair relay↔API Yjs

Evidence: relay fetches/PUTs `/internal/yjs/{id}` with no `Authorization` (401), and `UpdateCellInYjs` writes `yjs_documents` directly, disconnected from live relay docs (`docs/designs/yjs-source-of-truth.md` open question #1). Fix requires relay auth to the internal endpoints plus a live-apply path (internal apply endpoint or Redis pub/sub) and stale-doc reconciliation.

---

## Cross-cutting test & verification strategy

- Go: `AETHER_RATE_LIMIT_REGISTER=500 go test ./... -count=1 -timeout 30m` (real Postgres; `task check` times out under load).
- Frontend: `cd web && npx tsc -p tsconfig.app.json --noEmit && npm run test:run && npm run build` (`npx tsc --noEmit` is a no-op).
- Migrations run at server startup; verify V100/V101 apply cleanly on a populated dev DB.
- Browser (agent-browser + image analyzer, dev stack at `:8088`, `nova@heaven-labs.com`/`nova123`):
  - P2: trigger an agent create (run=true) → cell scrolls/highlights on create and on completion; chat input keeps focus.
  - P4: select part of a value in the detail view → Ctrl+C copies the selection; no selection → whole value; leaving the view restores normal shortcuts.
  - P7: with the relay blocked, agent `update_cell` → content appears once synced; title-only update does not wipe source; refresh consistency.
  - P5/P6: scripted long session (small `context_window` + threshold on a dev model config) → divider shows real before → after, meter shows current context, hover totals update without a new user message; reconnect restores.
- P1/P3: Go tool tests + `read_cell`/REST responses; quick browser check that cells still render/limit select works after the migration.

## Deployment notes

- **V100 is destructive and not backward compatible.** It drops `cells.description` irreversibly, and the *old* binary still selects/returns that column. Do **not** run `--migrate-only` or a standalone migration job ahead of a rolling deploy — applying it first would break every not-yet-upgraded instance's cell queries. All API instances must be upgraded together; a single-instance restart is safe (the `DROP COLUMN` takes a brief `ACCESS EXCLUSIVE` lock on `cells`).
- **Breaking surfaces to repeat in the PR body / release notes:**
  - `cells.description` column dropped — existing cell descriptions are permanently lost (approved breaking change).
  - REST cell JSON no longer contains `description`; a still-sent `description` argument is silently ignored.
  - CLI `Cell` JSON no longer contains `description`.
  - MCP / agent `read_cell` output no longer returns `description`.

## Risks & mitigations

- **Durable compaction changes prompt content** (model sees summary instead of full history). Mitigation: boundary sanitization reuses `sanitizeChatMessages`; tests assert exact message sets; `/summarize` untouched.
- **Dropping `cells.description` is destructive** (approved). Legacy snapshot JSON is tolerated; no backfill.
- **Yjs non-empty means Yjs-wins** on attach; a stale relay doc could override a newer DB row. Today's live docs are session-local (pipeline broken), so this is strictly better than the clobber race; the follow-up repair must define freshness.
- **Session usage writes add one UPDATE per LLM call**; acceptable cadence and authoritative for reconnect.
- **Event schema is additive**; `TokenBreakdown` gains a nested `session_usage` object and `done` gains `context_current` — old clients ignore unknown fields.

## Implementation order (input for the plan)

1. Migrations V100/V101 + models.
2. P3 removal sweep (Go + web) with tests.
3. P1 limit parameter with tests.
4. P4 copy handler + global reset with tests.
5. P2 focus queue + hub wiring with tests.
6. P7 client sync fix + broadcast/whitelist/reconnect with tests.
7. P5 durable compaction + divider plumbing with tests.
8. P6 session usage persistence + events + panel UI with tests.
9. Full `task check` equivalent, builds, browser sweep, PR.
