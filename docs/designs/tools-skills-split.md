# Design: Split Tools and Skills into First-Class Resources

**Status**: ✅ Implemented
**Author:** AI Assistant  
**Date:** 2026-06-19  
**Related Issue:** Agent resource architecture UX concerns

## Problem Statement

Aether's current resource architecture conflates two separate concerns — tools and skills — into a single `skills` entity:

1. **Skills** carry both `system_prompt` (behavioral instructions) and `tool_ids TEXT[]` (which tools the agent can use)
2. **Tools** are not a first-class resource — there's no `tools` table, no API, no UI. Tool names are opaque strings in a TEXT[] column
3. **No direct agent-to-tool assignment** — the only way to give an agent tools is through skills
4. **No custom tool creation** — users must write Go code to add new tools
5. **No per-agent tool permissions** — agents get all tools from their skills with no granularity

This makes the system harder to reason about: "I want this agent to have SQL query capability" requires either (a) creating a skill with the right tool_ids, or (b) writing Go code for a new tool type.

## Proposed Solution

Split tools and skills into separate first-class resources, inspired by opencode's clean separation:

- **Tools** (`tools` table) — first-class callable functions with name, description, JSON schema, and type-specific handler config
- **Skills** — pure prompt/behavioral bundles, lose `tool_ids`
- **Agents** — directly reference tools via `tool_ids[]`, alongside existing `skill_ids[]` and `agent_mcp_servers` join table

### Architecture Overview

```
Before:                          After:
Agent                            Agent
├── ModelConfig                  ├── ModelConfig
├── Skills[]                     ├── Skills[]
│   ├── system_prompt            │   └── system_prompt (pure prompt)
│   └── tool_ids[] ───────┐     ├── Tools[]      ← direct reference
├── MCPServers[]           │     ├── MCPServers[]
└──                        │     └──
                           └──>  Tools table (new)
                                   ├── builtin: Go handler (seeded)
                                   ├── webhook: URL + method + headers
                                   └── sql_query: connector_id + query
```

## Technical Design

### 1. Database Schema

#### New `tools` table:

```sql
CREATE TABLE tools (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL CHECK (type IN ('builtin', 'webhook', 'sql_query')),
    schema      JSONB NOT NULL DEFAULT '{}',
    config      JSONB NOT NULL DEFAULT '{}',
    -- webhook: { "url": "...", "method": "POST", "headers": {...}, "body_template": "..." }
    -- sql_query: { "connector_id": "...", "query": "SELECT ...", "params_schema": {...} }
    -- builtin: { "handler_name": "notebook_read_cells" }
    folder_id   UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, name)
);
```

#### Changes to `agents` — add `tool_ids`:

```sql
ALTER TABLE agents ADD COLUMN tool_ids UUID[] NOT NULL DEFAULT '{}';
```

#### Changes to `skills` — drop `tool_ids`:

```sql
ALTER TABLE skills DROP COLUMN tool_ids;
```

### 2. API Endpoints

#### New: `/api/v1/tools*`

| Method | Path | Handler | Auth |
|---|---|---|---|
| GET | /api/v1/tools | handleListTools | JWT |
| POST | /api/v1/tools | handleCreateTool | JWT |
| GET | /api/v1/tools/{id} | handleGetTool | requirePermission |
| PUT | /api/v1/tools/{id} | handleUpdateTool | requirePermission |
| DELETE | /api/v1/tools/{id} | handleDeleteTool | requirePermission |
| POST | /api/v1/tools/{id}/test | handleTestTool | JWT |

#### Modified: `/api/v1/agents*`

- `POST/PUT /api/v1/agents` — accept `tool_ids: string[]` alongside existing `skill_ids`, `mcp_server_ids`
- `GET /api/v1/agents/{id}` — return `tool_ids` and expanded `tools[]` array

#### Modified: `/api/v1/skills*`

- Remove `tool_ids` from create/update payload and response

### 3. Engine Changes (`internal/agent/`)

#### Current tool resolution in `ProcessMessage`:

```
skill_ids[] → resolve tool names from skill rows → ToolRegistry lookup
agent_mcp_servers → generate ToolDefs at runtime
merge both
```

#### New tool resolution:

```
tool_ids[] → fetch from tools table → resolve each by type:
  builtin:   look up handler in ToolRegistry (seeded handlers)
  webhook:   generate handler that POSTs to configured URL
  sql_query: generate handler that executes saved query against connector

skill_ids[] → fetch skills for catalog (no tool resolution)
agent_mcp_servers → generate ToolDefs (unchanged)

merge all three → ToolDefs
```

#### New files:

| File | Purpose |
|---|---|
| `internal/agent/tools_webhook.go` | Creates ToolHandler that sends HTTP request to webhook URL with tool args as JSON |
| `internal/agent/tools_sql.go` | Creates ToolHandler that executes saved query against a connector, passing tool args as params |

#### Seed built-in tools at startup:

On server startup, for each org, ensure built-in tool rows exist in `tools`:

```go
var builtinTools = []struct {
    Name        string
    Description string
    Schema      string // JSON Schema
    HandlerName string // registered in ToolRegistry
}{
    {"notebook_read_cells", "Read cells from a notebook", "{\"type\":\"object\",...}", "notebook_read_cells"},
    {"notebook_create_cell", "Create a new cell in a notebook", "...", "notebook_create_cell"},
    // ... all existing built-in tools
}
```

### 4. Migration Strategy

Single migration file:

1. Create `tools` table
2. Add `tool_ids UUID[]` to agents
3. Seed built-in tools per existing org (query `SELECT DISTINCT unnest(tool_ids) FROM skills` to determine which tools to seed)
4. Backfill `agents.tool_ids`: for each agent, collect tool names from all its skills' `tool_ids`, resolve to tool UUIDs, set `agents.tool_ids`
5. Drop `skills.tool_ids` column
6. Add `'tool'` to ACL resource type constraint

### 5. Frontend Changes

#### New page: `/tools` — `ToolsPage.tsx`

- CRUD table: name, type (badge), description
- Create/edit form with type-specific fields
  - **webhook**: URL, method dropdown, headers key-value editor, body template
  - **sql_query**: connector selector, SQL editor, params schema
  - **builtin**: read-only info card
- "Test" button → `POST /tools/{id}/test`
- PermissionsPanel

#### Sidebar

Add `/tools` to AI Agents nav:
```
Agents / Models / Skills / Tools / MCPs
```

#### SkillsPage

Remove tool toggle checkboxes. Skills are now just name + description + system_prompt.

#### AgentsPage

Add "Tools" searchable multi-select alongside "Skills" and "MCP Servers" multi-selects.

#### TypeScript types

```typescript
interface Tool {
  id: string
  org_id: string
  name: string
  description: string
  type: 'builtin' | 'webhook' | 'sql_query'
  schema: Record<string, any>
  config: Record<string, any>
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
}

interface Agent {
  // ...existing fields
  tool_ids: string[]
  tools?: Tool[]  // expanded
  skill_ids: string[]
  skills?: Skill[]
}
```

## Open Questions

1. Should built-in tools be seeded per-org at startup, or globally with a shared `org_id IS NULL` convention?
2. For SQL query tools: should params be passed as named arguments or positional?
3. Should the `POST /tools/{id}/test` endpoint for SQL tools actually execute the query against the connector (read-only), or just validate the config?

## Success Criteria

1. Users can create webhook tools and SQL query tools without writing Go code
2. Agent "Tools" multi-select works alongside existing "Skills" and "MCP Servers"
3. Skills are pure prompt bundles with no tool baggage
4. Built-in tools are visible as Tool rows in the UI
5. Existing skills/agents migrate cleanly with no data loss
6. Test endpoint works for both webhook and SQL tool types
