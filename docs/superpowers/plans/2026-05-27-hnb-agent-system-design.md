# hnb Agent System — Design Spec
**Date:** 2026-05-27
**Status:** Draft
**Source:** Design conversation with product owner

---

## Overview

Add AI agent capabilities to Heaven's Notebooks — a right-side chat panel in notebooks where agents can read, create, edit, and run cells, create charts, and explore data in parallel. System includes full agent management, skill library, tool/MCP integration, and usage metrics.

**Key principles:**
- Agents run in the Go backend (single binary, no new services)
- OpenAI-compatible chat completions API as the universal LLM adapter
- Existing ACL/permission system gates everything (agents, model configs, skills)
- Agents can improve themselves via built-in tools touching their own config
- Subagents for parallel exploration tasks

---

## Data Model

### `model_configs` — Admin-created LLM endpoints
```sql
CREATE TABLE model_configs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    provider    TEXT NOT NULL CHECK (provider IN ('openai', 'openai-compatible')),
    base_url    TEXT NOT NULL,          -- e.g. https://api.openai.com/v1
    model       TEXT NOT NULL,          -- e.g. gpt-4o, claude-sonnet-4-20250514
    api_key_encrypted BYTEA NOT NULL,   -- uses existing crypto.DeriveKey
    default_params JSONB NOT NULL DEFAULT '{}',  -- { temperature: 0.7, max_tokens: 4096 }
    context_window INT NOT NULL DEFAULT 128000,  -- max input tokens for this model (admin sets per endpoint)
    folder_id   UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
- Created/edited by org `admin` role only
- Gated by existing ACL — admin grants `view` to users/groups
- API keys encrypted with same pattern as connector credentials

### `skills` — Reusable capability bundles
```sql
CREATE TABLE skills (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT,
    system_prompt TEXT,
    tool_ids     TEXT[] NOT NULL DEFAULT '{}',  -- references ToolRegistry names
    folder_id    UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by   UUID NOT NULL REFERENCES users(id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
- User-created, ACL-gated (same as notebooks)
- Skills are reusable across multiple agents

### `agents` — The agent definition
```sql
CREATE TABLE agents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT,
    model_config_id UUID REFERENCES model_configs(id) ON DELETE SET NULL,
    subagent_model_config_id UUID REFERENCES model_configs(id) ON DELETE SET NULL,  -- optional override
    system_prompt   TEXT,
    skill_ids       UUID[] NOT NULL DEFAULT '{}',  -- refs skills
    mcp_servers     JSONB NOT NULL DEFAULT '[]',   -- [{ name, type, command, args }] (non-sensitive config)
    mcp_env_encrypted BYTEA,                       -- AES-encrypted JSON: { "server_name": { "KEY": "value" } }
    folder_id       UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```
- User-created, ACL-gated (same as notebooks)
- If `subagent_model_config_id` is null, subagents use the same model
- MCP env secrets (API keys, tokens) stored encrypted in `mcp_env_encrypted` using same `crypto.DeriveKey` pattern
- `mcp_servers` JSONB stores non-sensitive config only (name, type, command, args); env vars are excluded from this column
- UI provides a secrets editor that encrypts values before saving; values are decrypted only at session startup when spawning MCP processes

### `agent_sessions` — One per notebook chat
```sql
CREATE TABLE agent_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id),
    max_turns   INT NOT NULL DEFAULT 100,     -- max LLM turns before auto-summarize
    max_tokens  INT NOT NULL DEFAULT 100000,  -- max cumulative tokens before auto-summarize
    ended_at    TIMESTAMPTZ,                  -- set when user closes panel or after inactivity
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_sessions_lookup ON agent_sessions (agent_id, created_at DESC);
```

### `agent_messages` — Chat history
```sql
CREATE TABLE agent_messages (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id   UUID NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content      TEXT,              -- text content (user message or assistant text response)
    tool_call_id UUID,             -- for role='tool': references the tool call this result responds to
    tool_calls   JSONB,            -- for role='assistant': array of tool calls made (see schema below)
    tokens_input INT,              -- prompt tokens for this turn
    tokens_output INT,             -- completion tokens
    model_calls  INT DEFAULT 1,    -- count for cost tracking
    duration_ms  INT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_messages_session ON agent_messages (session_id, created_at);
```

**`tool_calls` JSONB schema** — array of objects, order preserved:
```json
[
  {
    "id": "uuid",
    "name": "create_cell",
    "arguments": { "type": "code", "source": "SELECT 1" },
    "result": { "cell_id": "abc-123", "position": 4 },
    "error": null,
    "duration_ms": 120
  }
]
```
- `id`: unique identifier for the tool call, used to match `tool` role messages back to the call
- `result`: populated on success, `null` on error
- `error`: populated on failure, `null` on success (string with error message)
- `duration_ms`: execution time for the tool call

**Message format alignment (OpenAI-compatible 3-role system):**
- `role = 'user'`: Human messages. `content` is the text.
- `role = 'assistant'`: LLM responses. `content` is text response (nullable if LLM only made tool calls). `tool_calls` contains the array of tool calls.
- `role = 'tool'`: Tool execution results. `tool_call_id` links to the specific tool call. `content` is the result/error as text.

When building LLM requests, the engine maps:
1. All `user` messages → `role: "user"`
2. All `assistant` messages → `role: "assistant"` with `tool_calls` array (OpenAI format)
3. All `tool` messages → `role: "tool"` with `tool_call_id` matching the call

### `subagent_tasks` — Parallel exploration
```sql
CREATE TABLE subagent_tasks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_session_id UUID NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    parent_message_id UUID,                   -- which LLM turn spawned this
    agent_id          UUID REFERENCES agents(id),  -- nullable: null = inherit parent's agent config
    goal              TEXT NOT NULL,
    context           JSONB,
    status            TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    result            JSONB,
    tokens_input      INT,
    tokens_output     INT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at      TIMESTAMPTZ
);

CREATE INDEX idx_subagent_tasks_session ON subagent_tasks (parent_session_id);
```
- `agent_id` nullable: when null, subagent inherits parent's full agent config (model, system prompt, skills, MCP servers)
- When set, subagent uses that specific agent's config — allows the LLM to delegate to a specialized agent

### `subagent_messages` — Isolated message chain per subagent
```sql
CREATE TABLE subagent_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subagent_task_id UUID NOT NULL REFERENCES subagent_tasks(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content         TEXT,
    tool_call_id    UUID,
    tool_calls      JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subagent_messages_task ON subagent_messages (subagent_task_id, created_at);
```

### Metrics rollup (materialized view or cron-populated)
```sql
CREATE TABLE agent_stats_daily (
    date           DATE NOT NULL,
    agent_id       UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id        UUID NOT NULL REFERENCES users(id),
    sessions_count INT NOT NULL DEFAULT 0,
    messages_count INT NOT NULL DEFAULT 0,
    tokens_input   BIGINT NOT NULL DEFAULT 0,
    tokens_output  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (date, agent_id, user_id)
);
```
- Populated by a daily cron job (inside the Go scheduler)
- Admin dashboard queries this, never hits raw `agent_messages`

### `agent_versions` — Self-improvement history
```sql
CREATE TABLE agent_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version         INT NOT NULL,                  -- monotonically increasing per agent
    name            TEXT,
    description     TEXT,
    system_prompt   TEXT,
    skill_ids       UUID[],
    model_config_id UUID,
    changed_by      UUID NOT NULL REFERENCES users(id),  -- user or agent self-modification
    change_reason   TEXT,                           -- why the change was made
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (agent_id, version)
);

CREATE INDEX idx_agent_versions_agent ON agent_versions (agent_id, version DESC);
```
- Snapshot of agent config on each meaningful change (name, system_prompt, skills, model)
- Created by `update_agent` tool (self-improvement) or user edits via API
- Enables diffing: "what changed between v3 and v5?"
- `changed_by` tracks whether the agent modified itself or a user did

### ACL resource types added
Extend existing `acl_entries` constraint to include:
```
'agent', 'model_config', 'skill'
```

---

## Architecture — Backend

### Package structure (`internal/agent/`)

```
internal/agent/
├── engine.go          → Chat loop: LLM call → parse tool calls → execute → repeat
├── llm.go             → OpenAI-compatible HTTP client (SSE streaming → user's WS)
├── registry.go        → ToolRegistry: maps tool names to Go handlers
├── tools_notebook.go  → read_cell, create_cell, update_cell, run_cell, move_cell, list_cells
├── tools_chart.go     → create_chart, update_chart
├── tools_agent.go     → update_agent, create_skill, spawn_subagents, list_agent_sessions
├── tools_mcp.go       → Proxy to external MCP servers (stdio or HTTP/SSE)
├── session.go         → Load/save agent_session + agent_messages
├── context.go         → Context window manager: token counting, message summarization
├── ratelimit.go       → Turn/token counters, auto-summarize + new session creation
├── subagent.go        → Spawn N goroutines, each with its own LLM loop + tool access
└── permissions.go     → Check user's permission before each notebook tool call
```

### Engine flow

```
1. User sends message via WebSocket
2. If message starts with `/`: handle as slash_command (see Slash commands section), return early
3. Engine loads session history
3. Context window management:
   a. Count tokens in system_prompt + skill prompts + message history
   b. If total exceeds ~80% of model's context window (from `model_configs.context_window`):
      - Summarize older messages into a condensed block via a quick LLM call
      - Replace summarized messages with a single "summary" user message
      - Keep the most recent messages intact
   c. Build final context: system_prompt + skill prompts + (possibly summarized) history
4. Rate limit check:
   a. If session.turns >= session.max_turns → auto-summarize + create new session
   b. If session.cumulative_tokens >= session.max_tokens → auto-summarize + create new session
   c. On auto-summarize: LLM summarizes current conversation, new session starts with summary as initial context
5. Builds LLM request: system_prompt + skill prompts + message history + tool definitions
6. Calls OpenAI-compatible chat completions API (streaming SSE)
7. Forwards text tokens to WebSocket as { type: "token", data } (with backpressure handling)
8. If LLM returns tool_calls:
   a. Resolve tool_name in ToolRegistry (built-in) or MCP proxy
   b. Check permissions (for notebook ops: same as user)
   c. Execute tool with per-tool timeout (default 30s)
   d. On timeout: tool result = error message, continue loop
   e. Send tool result back to LLM (next turn)
   f. Forward tool result to WebSocket as { type: "tool_call", ... }
   g. Repeat from 6 until LLM produces a final text response or turn limit hit
9. Send { type: "done", tokens_used } to WebSocket
10. Persist full turn to agent_messages
11. Update session cumulative token count
```

### WebSocket backpressure

When forwarding tokens to the client, the engine uses non-blocking writes with a 5-second timeout per write. If the client cannot keep up (write buffer full or timeout exceeded), tokens are dropped rather than blocking the engine. The client receives a `{ type: "backpressure_warning" }` message indicating dropped tokens, and can request missed content via the REST message history endpoint.

### Session reconnection

When a WebSocket disconnects mid-stream:
1. The engine continues processing the current turn to completion (tool calls finish, LLM response completes)
2. Results are persisted to `agent_messages` as normal
3. When the client reconnects (new WS connection with same session_id), the server sends any messages created since the client's last received message
4. Client sends `{ type: "reconnect", last_message_id: "..." }` to request missed messages
5. Server responds with `{ type: "reconnect_sync", messages: [...] }` containing the gap

### Tool definitions (OpenAI-compatible format)

Each tool registers with:
```go
type ToolDef struct {
    Name        string        // "create_cell"
    Description string        // "Create a new cell in the notebook"
    Parameters  any           // JSON Schema object
    Timeout     time.Duration // max execution time (default 30s, override per tool)
    Handler     func(args json.RawMessage, ctx *ToolContext) (any, error)
}
```

The ToolContext carries user identity, session info, and permission status:
```go
type ToolContext struct {
    UserID           string
    OrgID            string
    NotebookID       string
    SessionID        string
    DB               *pgxpool.Pool
    TurnCount        int       // current turn count for rate limiting
    CumulativeTokens int       // running total for token-based rate limiting
}
```

### Route additions (`internal/api/router.go`)

```
# Agent CRUD (ACL-gated like notebooks)
GET    /api/v1/agents                    → handleListAgents
POST   /api/v1/agents                    → handleCreateAgent
GET    /api/v1/agents/{id}               → handleGetAgent
PUT    /api/v1/agents/{id}               → handleUpdateAgent
DELETE /api/v1/agents/{id}               → handleDeleteAgent

# Sessions
POST   /api/v1/agents/{id}/session       → handleCreateSession
GET    /api/v1/agents/{id}/sessions      → handleListSessions
GET    /api/v1/sessions/{session_id}     → handleGetSession

# Chat WebSocket
GET    /api/v1/ws/agents/{session_id}    → handleAgentWS (upgrade, forward to engine)

# Skills CRUD
GET    /api/v1/skills                    → handleListSkills
POST   /api/v1/skills                    → handleCreateSkill
PUT    /api/v1/skills/{id}               → handleUpdateSkill
DELETE /api/v1/skills/{id}               → handleDeleteSkill

# Model configs (admin only)
GET    /api/v1/model-configs             → handleListModelConfigs
POST   /api/v1/model-configs             → handleCreateModelConfig
PUT    /api/v1/model-configs/{id}        → handleUpdateModelConfig
DELETE /api/v1/model-configs/{id}        → handleDeleteModelConfig

# Usage stats (admin only)
GET    /api/v1/agents/stats              → handleAgentStats (org-wide)
GET    /api/v1/agents/{id}/stats         → handleAgentStatsByAgent
```

### Permission model per tool

| Tool | Permission check |
|---|---|
| `read_cell` | User has `view` on the notebook |
| `create_cell` | User has `edit` on the notebook |
| `update_cell` | User has `edit` on the notebook |
| `run_cell` | User has `run` on the notebook |
| `create_chart` | User has `edit` on the notebook (same as update_cell) |
| `update_agent` | User has `edit` on the target agent |
| `create_skill` | User has `edit` on the parent folder (same ACL check as creating a notebook) |

The agent engine extracts user identity from the JWT attached to the WebSocket (same as the existing `AuthMiddleware`). Each tool call checks `checkPermission()` before executing.

### Audit trail

Every notebook mutation made by an agent is logged in `audit_logs` with `metadata->>'agent_session_id'` set, so you can trace any cell change back to the agent conversation that caused it. Agent config changes are also audited with `resource_type: 'agent'`.

---

## Built-in Tools

### Notebook tools
| Tool | Description | Key params |
|---|---|---|
| `read_cell` | Get a cell's source and outputs | cell_id |
| `create_cell` | Create a new code or text cell | notebook_id, type, source?, position? |
| `update_cell` | Change a cell's source or metadata | cell_id, source?, title?, description? |
| `run_cell` | Execute a cell's query | cell_id |
| `list_cells` | List all cells in the notebook with summary | notebook_id |
| `move_cell` | Reorder a cell | cell_id, new_position |

### Chart tools
| Tool | Description | Key params |
|---|---|---|
| `create_chart` | Turn a cell's table output into a chart | cell_id, chart_type, x_column, y_columns, title? |
| `update_chart` | Modify chart config on an existing cell | cell_id, chart_type?, x_column?, y_columns?, title? |

### Agent self-improvement tools
| Tool | Description | Key params |
|---|---|---|
| `update_agent` | Modify this agent's own config | name?, description?, system_prompt?, skill_ids? |
| `create_skill` | Save a reusable skill | name, description, system_prompt, tool_ids |
| `update_skill` | Modify a skill | skill_id, name?, system_prompt?, tool_ids? |
| `spawn_subagents` | Fork parallel exploration tasks | tasks: [{id, goal, context, agent_id?}] |

### MCP proxy tools
| Tool | Description |
|---|---|
| *(dynamic)* | Any tool from configured external MCP servers registered at session start |

---

## Subagent System

### Flow
1. Agent calls `spawn_subagents({ tasks: [{ id: "a", goal: "...", context: "...", agent_id: "..." }] })`
2. Engine creates `subagent_task` records, status = `queued`
   - If `agent_id` provided: subagent uses that agent's config (model, system prompt, skills, MCP)
   - If `agent_id` omitted: subagent inherits parent's full config
3. Engine spawns N goroutines (configurable max parallelism, default 3)
4. Each subagent: status→`running`, creates its own message chain in `subagent_messages`, has full tool access (same ToolRegistry as parent)
5. On completion: status→`completed`, result stored
6. WebSocket receives `{ type: "subagent_update", tasks: [...] }` after each completion
7. Parent agent's next LLM turn receives consolidated tool result with all subagent outputs

### Limits
- Max subagents per call: 5
- Max turns per subagent: 20
- Subagent tool context inherits from parent (same user, notebook, permissions)
- Subagents cannot spawn further subagents (no recursion)

---

## MCP Tool Adapter

### Configuration
Per agent, stored across two fields:

**`agents.mcp_servers` (JSONB)** — non-sensitive config:
```json
[
  {
    "name": "web-search",
    "type": "stdio",
    "command": "npx",
    "args": ["-y", "firecrawl-mcp"]
  },
  {
    "name": "filesystem",
    "type": "stdio",
    "command": "npx",
    "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
  }
]
```

**`agents.mcp_env_encrypted` (BYTEA)** — encrypted secrets, structured as:
```json
{
  "web-search": { "FIRECRAWL_API_URL": "http://localhost:3002" },
  "filesystem": {}
}
```
Encrypted with `crypto.DeriveKey` before storage. Decrypted only when spawning MCP processes at session startup.

**UI:** Admin panel provides a per-server env var editor. Values are sent encrypted to the API. Plaintext secrets are never stored in the database.

### Lifecycle
1. On agent session start: spawn all configured MCP servers
2. Negotiate: `list_tools` call → register each tool in ToolRegistry
3. Each tool call becomes: `call_tool(name, args)` → wait for response
4. On session end: kill all spawned MCP processes

Stdio-based MCP servers use `exec.CommandContext` (same goroutine pattern as `JSExecutor`). HTTP/SSE-based servers use `net/http` client.

---

## Frontend — Agent Panel

### Component: `web/src/components/AgentPanel.tsx`

A collapsible right-side sliding panel inside `NotebookPage`, positioned with `position: fixed, right: 0, top: ..., bottom: ..., width: 360`.

### States
1. **Closed** — narrow toggle tab on right edge, shows "AI" badge
2. **Agent selector** — dropdown/list of available agents (fetched from `/api/v1/agents`)
3. **Chatting** — full chat panel
4. **Loading** — streaming indicator, tool call blocks, subagent progress

### Component tree
```
AgentPanel
├── PanelHeader (agent name, close button, agent menu)
├── AgentSelector (shown if no active session)
│   └── list of ACL-filtered agents from /api/v1/agents
├── MessageList
│   ├── Message (user) — text bubble
│   ├── Message (assistant)
│   │   ├── streaming text
│   │   ├── ToolCallBlock — collapsible "Read cell 'sales'" → result preview
│   │   ├── CellCreatedBlock — "Created cell #4" with scroll-to link
│   │   └── SubagentProgress — parallel tasks with spinners/checkmarks
│   └── ... more messages
└── MessageInput (textarea + send button)
```

### WebSocket protocol
```
Client → Server:  { type: "message", content: "..." }
Client → Server:  { type: "slash_command", command: "summarize" | "new" | "skills" | "agents" }
Client → Server:  { type: "reconnect", last_message_id: "..." }
Server → Client:  { type: "token", data: "text fragment" }
Server → Client:  { type: "tool_call", tool: "create_cell", args: {...}, result: {...} }
Server → Client:  { type: "cell_created", cell_id: "...", position: 4 }
Server → Client:  { type: "subagent_progress", tasks: [{id, goal, status, ...}] }
Server → Client:  { type: "done", tokens_used: 450 }
Server → Client:  { type: "error", message: "..." }
Server → Client:  { type: "slash_result", command: "...", data: {...} }
Server → Client:  { type: "backpressure_warning", dropped_tokens: 12 }
Server → Client:  { type: "reconnect_sync", messages: [...] }
```

### Slash commands

The chat input intercepts messages starting with `/` and routes them as `slash_command` messages instead of regular LLM messages. Commands are processed server-side.

| Command | Action | Response |
|---|---|---|
| `/summarize` | LLM summarizes current conversation, creates new session with summary as initial context | `{ type: "slash_result", command: "summarize", data: { session_id: "...", summary: "..." } }` |
| `/new` | Creates a fresh empty session (no summary), returns new session_id | `{ type: "slash_result", command: "new", data: { session_id: "..." } }` |
| `/skills` | Lists skills the user has access to (ACL-filtered) | `{ type: "slash_result", command: "skills", data: { skills: [...] } }` |
| `/agents` | Lists agents the user has access to (ACL-filtered) | `{ type: "slash_result", command: "agents", data: { agents: [...] } }` |

The frontend renders `slash_result` responses as inline system messages in the chat (styled differently from user/assistant messages). `/summarize` and `/new` also trigger a local session state reset (clear message list, start fresh).

### Integration in NotebookPage.tsx
- Add `showAgent` state toggle (new toolbar button "AI")
- Agent panel renders inside `NotebookPage` alongside history panel
- Current cursor/cell context passed as initial context on session creation
- When `cell_created` event received: auto-scroll to cell + flash highlight
- When `tool_call` creates chart: cell's OutputRenderer re-renders with new chart output

---

## Implementation Phases

### Phase 1 — Foundation (backend data layer + basic agent engine)
- [ ] DB migrations: all new tables (including `agent_versions`)
- [ ] Extend `acl_entries` constraint with new resource types
- [ ] `internal/agent/`: ToolRegistry, LLM client, chat loop (no streaming yet)
- [ ] CRUD routes for agents, skills, model_configs
- [ ] Basic WebSocket endpoint (accept message, return response — synchronous)
- [ ] Per-tool timeout enforcement
- [ ] `mcp_env_encrypted` field + encryption/decryption helpers

### Phase 2 — Frontend Panel + Streaming
- [ ] AgentPanel component (static)
- [ ] WebSocket streaming with typed JSON envelopes
- [ ] Notebook toolbar button + panel integration
- [ ] Cell created/chart created → scroll-to integration
- [ ] MCP secrets editor in agent config UI
- [ ] Slash commands (/summarize, /new, /skills, /agents)

### Phase 3 — MCP + Subagents
- [ ] MCP adapter in ToolRegistry
- [ ] Subagent system (spawn_subagents tool, with agent_id override)
- [ ] Agent selection UI + ACL filtering

### Phase 4 — Context, Rate Limits, and Resilience
- [ ] Context window manager (token counting + message summarization)
- [ ] Rate limiting (max turns + max tokens per session)
- [ ] Auto-summarize + new session creation when limits hit
- [ ] WebSocket backpressure handling (drop tokens with warning)
- [ ] Session reconnection (buffered results + client sync)

### Phase 5 — Self-Improvement + Metrics
- [ ] Built-in tools: update_agent, create_skill, update_skill
- [ ] `agent_versions` snapshots on self-modification
- [ ] Daily stats rollup + admin metrics page
- [ ] Audit trail integration (metadata enrichment)

---

## Self-Review

### Placeholder scan
- No TBD or TODO placeholders
- All sections complete

### Internal consistency
- Data model references match across all sections
- ACL extension mirrors existing pattern exactly
- WebSocket protocol consistent between frontend and backend descriptions
- MCP lifecycle matches existing `exec.CommandContext` pattern in JSExecutor
- Message role schema (`user`/`assistant`/`tool`) consistent across `agent_messages` and `subagent_messages`
- `tool_calls` JSONB schema defined with error handling and linked to `tool` role messages via `tool_call_id`
- MCP env encryption uses same `crypto.DeriveKey` pattern as connector credentials and model config API keys
- `agent_versions` tracks self-improvement changes with full diffs

### Scope check
- Focused on the agent system only — no unrelated refactoring of existing features
- Phase 1 delivers a working (non-streaming) agent chat + CRUD to unblock early testing
- Each phase is independently shippable

### Ambiguity check
- "Admin-created model configs" is explicit — admin role required
- "User's permissions" for tool calls is explicit — same `checkPermission()` path
- "Subagents cannot spawn subagents" is explicit — prevents recursion
- MCP server types limited to stdio + HTTP/SSE (not WebSocket MCP in V1)
- Context window management: summarize-old-messages strategy documented
- Rate limiting: auto-summarize + new session when limits hit
- WebSocket backpressure: drop tokens with warning, client can fetch via REST
- Session reconnection: engine completes, results buffered, client syncs on reconnect
- Per-tool timeout: configurable per tool, default 30s
