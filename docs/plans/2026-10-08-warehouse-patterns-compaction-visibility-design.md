# Design: Warehouse Pattern Entry & Auto-Compaction Visibility

- **Date:** 2026-10-08
- **Status:** Approved (validated with the user; the pattern-entry UI brief was defined through Impeccable `/shape`)
- **Scope:** Frontend-only. Touches `web/src/components/WarehouseHiddenTables.tsx`, `web/src/components/AgentChatTranscript.tsx`, `web/src/components/AgentPanel.tsx`, and their tests. No backend, API, or schema changes.
- **Deliverable:** design + implementation plan; branch `feat/warehouse-patterns-compaction-visibility`, PR off `main`.

## 1. Problem

Two operator-facing papercuts, independent of each other.

### 1.1 Hidden-table patterns cannot be entered in bulk

Warehouse settings expose "Hidden tables": Go regex patterns matched against `database.table` that keep noisy tables out of the picker and inbox (`web/src/components/WarehouseHiddenTables.tsx`). The entry field is a single-line `<input>` with an Add button, so a copied list of twenty patterns arrives as one mangled line (browsers strip newlines in single-line inputs) and typing several requires an Add press per pattern. Operators copying patterns from a ticket or a query editor hit this every time. The connector form's allow/deny lists already solved the same problem with a one-per-line textarea (`web/src/pages/ConnectorsPage.tsx`); the warehouse pattern field did not.

### 1.2 Auto-compaction is invisible in the chat

The session token bar reports cumulative input tokens (e.g. 3,000,000) for an agent chat; the chat itself showed no auto-compaction event. The pipeline already persists a `compaction` row and renders a small dashed divider when it fires (`internal/agent/engine.go:1025-1066`, `web/src/components/AgentChatTranscript.tsx:121-137`), but nothing tells an operator whether compaction has ever run, when, how much context it recovered, or where the threshold sits. Cumulative input also reads as if it were context size, so the operator cannot distinguish "many calls under the limit" from "the window was compacted". In fact the UI cannot currently answer the question "did this session compact?" without scanning the transcript.

## 2. Goals / Non-goals

**Goals**

- Enter any number of hidden-table patterns in one action: type or paste one per line, press Enter once, all patterns become chips via a single request.
- Make pasted lines reviewable before committing (they stay as editable text).
- Teach the Enter / Shift+Enter contract in place, without a help modal.
- Make auto-compaction unmistakable in the transcript when it happens, and legible after the fact: count, timestamps, before → after recovery.
- Separate cumulative input from current context in the token panel and show the compaction trigger threshold.

**Non-goals**

- Hidden patterns in the create-warehouse modal (they remain a post-creation setting).
- Any change to compaction logic, thresholds, summary generation, or persistence.
- Backend/API/schema changes or new WebSocket events.
- Live toasts, session-history-list changes, or badge changes outside the agent chat token panel.
- The connector allow/deny lists (already one-per-line textareas).

## 3. Decisions

| # | Decision |
|---|---|
| D1 | **Textarea with Enter-to-add.** The single-line input becomes a textarea. Enter adds every non-empty line as a pattern through a single PUT; Shift+Enter inserts a newline. The Add button performs the same add, for mouse users. |
| D2 | **Paste stays editable.** Multi-line paste lands as text lines (browser default); nothing is committed until Enter/Add. One rule for typed and pasted content. |
| D3 | **Normalization.** Split on `/\r?\n/`, trim each line, drop empty lines, dedupe against existing patterns and within the batch (first occurrence wins), preserve order; PUT `[...existing, ...newOnes]` once. |
| D4 | **Preserved contracts.** All-duplicate input clears the field with no request (today's behavior); a server 400 surfaces in the `ErrorBanner` and keeps the text for fixing; success clears the field and keeps focus; empty input disables Add/no-ops; saving disables the control. Chips, hidden-count hint, cache-merge behavior unchanged. |
| D5 | **Inline affordance.** A muted 11px hint line under the field: "One pattern per line · `Enter` adds · `Shift+Enter` for a new line", keys as 3px-radius mono kbd chips (precedent: `ShortcutsModal`). The placeholder teaches the per-line model with two example patterns. The hint is linked via `aria-describedby`. No `?` tooltip. The existing regex hint above the field stays. |
| D6 | **Scope: the pattern field only.** No API change; no create-modal field; no connector-form change. |
| D7 | **Compaction becomes a transcript system event.** The divider upgrades to a full-width centered marker — a hairline rule with a pill reading `⚙ Context auto-compacted · 812k → ~180k` — expandable to the full summary, exact before count (actual) / after count (estimated), and timestamp. It is a keyboard-accessible toggle (`button` + `aria-expanded`/`aria-controls`). Rows without counts (legacy) render the label alone. |
| D8 | **Token panel tells the whole story.** Session input row relabels to `Input (cumulative)`; the `Current context` row gains `· compacts at 70%` (threshold from the selected model config's `default_params.compaction_threshold`, default 70; ≤0 renders `· auto-compact off`); when compaction rows exist the one-line "Compacted" note becomes `⚙ Compacted N× · last: X → ~Y` derived from the messages already passed to the meter. |
| D9 | **Data sources only.** Counts come from persisted `compaction` rows (`tokens_direct`, `tokens_after`, `created_at`); the threshold mirrors the engine's read of `model_configs.default_params` (`>0` enables, default 70, trigger at `contextWindow × threshold%`). No new events, no backend work. |
| D10 | **No live toast, no history-list changes.** The mid-turn marker is appended into the transcript and appears under the streaming output; auto-scroll is sufficient. |

## 4. Detailed design

### 4.1 Warehouse hidden-table patterns — textarea with Enter-to-add

UI brief (Operate surface, Observatory world — refinement, no new visual language):

- **Field.** Textarea, mono 12px, `minHeight: 60`, vertical resize, hairline `--border`, 6px radius, canvas/surface background; global focus ring. Visual precedent: the connector allow/deny textareas. Keep `maxWidth: 460` on the add row and let the textarea flex.
- **Add flow.** `onKeyDown`: Enter without Shift → `preventDefault()` + add; Shift+Enter → default newline. Add button → same handler. Handler normalizes per D3 and calls the existing save mutation once. Since the server validates the whole array, one invalid line fails the batch and the error explains the offending pattern.
- **Paste.** No interception; the textarea's native multi-line paste is the feature (D2).
- **Hint + placeholder.** Hint line below the field with kbd chips (`Enter`, `Shift+Enter`); placeholder:
  ```
  e.g. ^analytics\._tmp
       ^raw\.old$
  ```
- **States.** Empty → Add disabled, Enter no-op. Saving → controls disabled, button shows "Saving…". Success → text cleared, focus retained. Server error → banner, text retained. Duplicates-only → field cleared, no request.
- **A11y.** `aria-label="Pattern"` stays; hint linked via `aria-describedby`; kbd chips are presentational text.

### 4.2 Compaction marker in the transcript (`AgentChatTranscript.tsx`)

Replace the dashed bubble with a centered system-event marker:

```
────────────  ⚙ Context auto-compacted · 812k → ~180k  ────────────
```

- Structure: flex row, hairline rules (`--border-light`) filling both sides, pill centered. Pill: 10px radius (pill scale), amethyst-light wash, hairline border, 11px text; `⚙` glyph consistent with existing usage; counts in the existing compact formatter (`812k`), after-value prefixed `~` (the after count is a tiktoken estimate; the before count is the real prompt size).
- Toggle: click/Enter/Space expands. Collapsed by default; expanded region shows the summary (`whiteSpace: pre-wrap`), the exact counts `before: 812,345 tokens (actual) → after: ~180,200 tokens (estimated)` via `toLocaleString`, and the timestamp. `aria-expanded` + `aria-controls` on the control, chevron rotates (existing ▸/▾ convention).
- Legacy rows (`tokens_after` absent/0): label only, no counts, summary still expandable.
- Empty summary: marker still renders (persisted row is the fact).

### 4.3 Token panel (`AgentPanel.tsx` `TokenUsageMeter`)

- `Input` row → `Input (cumulative)`; `title` explains "Total prompt tokens across every model call in this session".
- `Current context` row → append `· compacts at N%`; compute `N` from `modelConfigs.find(id === modelConfigId)?.default_params?.compaction_threshold ?? 70`; if the resolved value is `<= 0` append `· auto-compact off`; omit the suffix when no window is known (row itself is hidden then).
- Compaction block: derive `compactions = messages.filter(m => m.role === 'compaction')`. `N > 0` → `⚙ Compacted N× · last: X → ~Y` (latest row's counts; omit counts when absent). `N === 0` but `hasCompacted` (restored state without rows) → keep today's one-liner fallback.
- No layout change to the popover structure; rows stay in the existing idiom.

## 5. Testing & verification

- **Vitest component tests.**
  - `WarehouseHiddenTables.test.tsx`: Enter adds all non-empty lines in one PUT; Shift+Enter keeps text and does not PUT; multi-line paste then Enter commits; dedupe across batch and existing; duplicates-only clears without PUT; server 400 keeps text and shows the banner; empty/disabled behavior.
  - `AgentPanel.compaction.test.tsx`: marker renders label + compact counts; expands to summary and exact counts; legacy row degrades; keyboard toggle.
  - `AgentPanel.tokens.test.tsx`: `Input (cumulative)` label; `compacts at 70%` suffix; `auto-compact off` for 0; `Compacted N× · last` derived from messages; fallback when only `hasCompacted`.
- **Type/build:** `cd web && npx tsc --noEmit && npm run build`.
- **Real-browser validation (mandatory per AGENTS.md):** against the dev stack — exercise the textarea (type, Shift+Enter, paste, Enter) on a real warehouse and confirm chips + PUT; seed a compaction row into a dev session's `agent_messages`, open `/chats/{id}`, and confirm the marker, expansion, and token-panel lines in the browser; check `agent-browser errors`.
- **Impeccable detector** on the changed UI files once implemented (`impeccable detect --json`).
- **PR checks:** `task check`, `cd relay && npm run build`, `task test:e2e` where the suite covers the surfaces.

## 6. Rollout

Frontend-only, no flags. Squash-merge the PR into `main`; no migration or deployment steps beyond the normal release.
