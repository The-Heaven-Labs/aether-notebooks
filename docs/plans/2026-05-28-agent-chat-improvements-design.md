# Agent Chat Improvements — Design

Date: 2026-05-28

## Overview

Ten improvements to the agent chat system, grouped into five work streams. All items are currently in the "Not Yet Implemented" section of `IMPROVEMENTS.md`.

## Work Stream 1: Session History & Agent Memory

### 1. Agent chat history for past sessions

The backend already persists sessions and messages (`agent_sessions`, `agent_messages` tables) and exposes `GET /api/v1/agents/{id}/sessions` + `GET /api/v1/sessions/{session_id}`. The gap is entirely frontend.

- Add a "History" icon button in the `AgentPanel` header bar (next to "Change" agent button)
- Clicking it shows a session list view (replaces the message list): each row shows session start date, first user message preview, message count
- Clicking a session loads its messages via `GET /api/v1/sessions/{session_id}` and displays them read-only (no input bar)
- A "Back to chat" button returns to the active session
- Sessions sorted by `created_at` descending, paginated (20 per page)

### 2. AI button opens chat with last-used agent

- Store the last-used `agent_id` in `localStorage` under key `hnb:lastAgentId`
- When `AgentPanel` mounts and no agent is selected, check localStorage and auto-select that agent (skip the agent picker)
- When user selects/changes agent, update localStorage
- Falls back to showing the picker if the stored agent no longer exists

### 3. "/" command picker

- When input starts with `/`, show a floating dropdown above the input with matching commands
- Commands: `/new`, `/skills`, `/agents`, `/summarize` (from backend `HandleSlashCommand`)
- Filter as user types (fuzzy match on command name)
- Arrow keys to navigate, Enter to select (inserts command into input), Escape to close
- Show brief description next to each command
- If user types `/` followed by space or continues typing non-matching text, dismiss the picker

## Work Stream 2: Chat UX

### 4. Queue messages while LLM is working

- Instead of disabling the input during streaming, keep it enabled
- When user sends a message while streaming, add it to a `pendingMessages` queue (displayed below the current streaming message as dimmed "queued" bubbles)
- When the current turn completes (`done` message), automatically send the next queued message
- Show a small "Queued" badge on pending messages with a cancel (X) button

### 5. Keep focus on chat input after sending

- After sending a message, call `inputRef.current?.focus()` in the next render tick (`requestAnimationFrame` or `setTimeout(0)`)
- Also re-focus after streaming completes (in the `done` handler)

### 7. Auto-focus input when agent panel opens

- Add a `useEffect` in `AgentPanel` that focuses the input on mount
- Same mechanism as #5 — reuse a `focusInput()` helper

## Work Stream 3: Platform-Wide Tools

### 6. Native tool calls for listing notebooks, connectors, folders

New Go tools in `internal/agent/tools_platform.go`:

| Tool | Description | Params | DB Query |
|------|-------------|--------|----------|
| `list_notebooks` | List notebooks the user can access | `folder_id?`, `search?` | SELECT from notebooks + ACL check |
| `list_connectors` | List connectors the user can access | `search?` | SELECT from connectors + ACL check |
| `list_folders` | List folders (children of parent) | `parent_id?` | SELECT from folders + ACL check |
| `get_folder_tree` | Get full folder hierarchy | — | Recursive CTE on folders |

Each tool returns `[{id, name, description?, folder_id?, created_at}]`. Permission-filtered using the existing `checkPermission` pattern from `tools_notebook.go`.

### 8. Native tool call for data schema exploration

New tool in `internal/agent/tools_notebook.go`:

| Tool | Description | Params |
|------|-------------|--------|
| `explore_schema` | List tables, columns, types for a connector | `connector_id` |

Implementation:
- Fetches connector credentials (decrypted via `crypto.Decrypt`)
- For Postgres: queries `information_schema.columns` filtered by table schema
- For ClickHouse: queries `system.columns`
- Returns `[{table_name, columns: [{name, type, nullable?}]}]`
- Respects connector ACL (read permission required)

## Work Stream 4: Agent Task Tracking

### 9. Native tools for task tracking

Session-scoped (like Claude Code's todo list). The agent breaks complex requests into tasks and tracks progress.

New tools in `internal/agent/tools_agent.go`:

| Tool | Description | Params |
|------|-------------|--------|
| `create_tasks` | Create a task list for the current session | `tasks: [{id, description}]` |
| `update_task` | Mark a task as done/in-progress/pending | `task_id`, `status` |
| `get_tasks` | Get current task list | — |

Storage: In-memory on the `Engine` (keyed by session ID). Tasks are ephemeral — they live only for the session duration.

Frontend: New `TaskList` component rendered above messages, showing tasks with status icons (pending ○, in-progress ◐, done ✓). Tasks animate in when created. Updated via new WebSocket message type `tasks_updated`.

## Work Stream 5: Cell Highlighting

### 10. Highlight and scroll to new cells created by agent

Current issue: The cell might not be in the DOM after 300ms (React Query refetch is async). The flash is also brief (1.5s).

- Use a polling loop (`requestAnimationFrame` or `setInterval`) that waits for `#cell-{id}` to appear in the DOM (up to 5s timeout), then scrolls and highlights
- Make the highlight more prominent: brighter accent border, 3s duration, pulse animation
- Store a `Set<string>` of recently-created cell IDs in state; show a persistent "New" badge for 10s

## Files to Create/Modify

### New files
- `internal/agent/tools_platform.go` — list_notebooks, list_connectors, list_folders, get_folder_tree
- `web/src/components/SessionHistory.tsx` — Session list and message viewer
- `web/src/components/SlashCommandPicker.tsx` — "/" autocomplete dropdown
- `web/src/components/TaskList.tsx` — Agent task checklist

### Modified files (backend)
- `internal/agent/engine.go` — Register new platform + schema tools, add task storage
- `internal/agent/tools_agent.go` — Add create_tasks, update_task, get_tasks tools
- `internal/agent/tools_notebook.go` — Add explore_schema tool
- `internal/agent/types.go` — Add task-related types, new WS message type for tasks_updated
- `internal/api/agent_ws.go` — Handle tasks_updated emission

### Modified files (frontend)
- `web/src/components/AgentPanel.tsx` — History button, slash picker integration, message queueing, auto-focus, task list display
- `web/src/types/agent.ts` — New WS message types (tasks_updated)
- `web/src/pages/NotebookPage.tsx` — Improved cell_created scroll/highlight logic
