# hnb Agent System — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** Add AI agent capabilities to Heaven's Notebooks — a right-side chat panel where agents can read, create, edit, and run cells, create charts, and explore data in parallel.

**Architecture:** Agents run in the Go backend (single binary). OpenAI-compatible chat completions API as the universal LLM adapter. Existing ACL/permission system gates everything. Subagents enable parallel exploration tasks.

**Tech Stack:** Go (net/http, gorilla/websocket), React + TypeScript, PostgreSQL, AES-256-GCM encryption

---

## Phase 1 — Foundation (Backend Data Layer + Basic Agent Engine)

### Task 1: Database Migrations

**Files:**
- Create: `internal/database/migrations/046_agent_tables.sql`
- Test: `task infra:reset && task db:psql` (verify tables exist)

**Step 1: Create migration file**

```sql
-- Migration 046: Agent System Tables

-- model_configs — Admin-created LLM endpoints
CREATE TABLE model_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    provider        TEXT NOT NULL CHECK (provider IN ('openai', 'openai-compatible')),
    base_url        TEXT NOT NULL,
    model           TEXT NOT NULL,
    api_key_encrypted BYTEA NOT NULL,
    default_params  JSONB NOT NULL DEFAULT '{}',
    context_window  INT NOT NULL DEFAULT 128000,
    folder_id       UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by      UUID NOT NULL REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_model_configs_org ON model_configs (org_id);

-- skills — Reusable capability bundles
CREATE TABLE skills (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    description   TEXT,
    system_prompt  TEXT,
    tool_ids      TEXT[] NOT NULL DEFAULT '{}',
    folder_id     UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by    UUID NOT NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_skills_org ON skills (org_id);

-- agents — The agent definition
CREATE TABLE agents (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                 UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name                   TEXT NOT NULL,
    description           TEXT,
    model_config_id        UUID REFERENCES model_configs(id) ON DELETE SET NULL,
    subagent_model_config_id UUID REFERENCES model_configs(id) ON DELETE SET NULL,
    system_prompt          TEXT,
    skill_ids              UUID[] NOT NULL DEFAULT '{}',
    mcp_servers            JSONB NOT NULL DEFAULT '[]',
    mcp_env_encrypted      BYTEA,
    folder_id              UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by              UUID NOT NULL REFERENCES users(id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agents_org ON agents (org_id);

-- agent_sessions — One per notebook chat
CREATE TABLE agent_sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    notebook_id UUID NOT NULL REFERENCES notebooks(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id),
    max_turns   INT NOT NULL DEFAULT 100,
    max_tokens  INT NOT NULL DEFAULT 100000,
    ended_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_sessions_lookup ON agent_sessions (agent_id, created_at DESC);

-- agent_messages — Chat history
CREATE TABLE agent_messages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    role          TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content       TEXT,
    tool_call_id  UUID,
    tool_calls    JSONB,
    tokens_input  INT,
    tokens_output INT,
    model_calls   INT DEFAULT 1,
    duration_ms   INT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_messages_session ON agent_messages (session_id, created_at);

-- subagent_tasks — Parallel exploration
CREATE TABLE subagent_tasks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_session_id UUID NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    parent_message_id UUID,
    agent_id          UUID REFERENCES agents(id),
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

-- subagent_messages — Isolated message chain per subagent
CREATE TABLE subagent_messages (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subagent_task_id UUID NOT NULL REFERENCES subagent_tasks(id) ON DELETE CASCADE,
    role             TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content          TEXT,
    tool_call_id     UUID,
    tool_calls       JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_subagent_messages_task ON subagent_messages (subagent_task_id, created_at);

-- agent_stats_daily — Metrics rollup
CREATE TABLE agent_stats_daily (
    date            DATE NOT NULL,
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    sessions_count INT NOT NULL DEFAULT 0,
    messages_count INT NOT NULL DEFAULT 0,
    tokens_input    BIGINT NOT NULL DEFAULT 0,
    tokens_output   BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (date, agent_id, user_id)
);

-- agent_versions — Self-improvement history
CREATE TABLE agent_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version         INT NOT NULL,
    name            TEXT,
    description     TEXT,
    system_prompt   TEXT,
    skill_ids       UUID[],
    model_config_id UUID,
    changed_by      UUID NOT NULL REFERENCES users(id),
    change_reason   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (agent_id, version)
);

CREATE INDEX idx_agent_versions_agent ON agent_versions (agent_id, version DESC);
```

**Step 2: Verify migration applies**

Run: `task infra:reset && task db:psql`
Expected: `dt` shows all new tables

**Step 3: Commit**

```bash
git add internal/database/migrations/046_agent_tables.sql
git commit -m "feat(agent): add agent system tables"
```

---

### Task 2: ACL Extension for New Resource Types

**Files:**
- Modify: `internal/database/migrations/001_initial_schema.sql:1-50` (find acl_entries CHECK constraint)

**Step 1: Create ACL extension migration**

Create: `internal/database/migrations/047_extend_acl_resource_types.sql`

```sql
-- Migration 047: Extend ACL resource types for agent system
-- Find the CHECK constraint on acl_entries.action and extend it
-- This migration is idempotent: uses IF NOT EXISTS for the alter

ALTER TABLE acl_entries DROP CONSTRAINT IF EXISTS acl_entries_action_check;
ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_action_check
    CHECK (action IN (
        'view', 'run', 'edit', 'share', 'delete', 'manage',
        'use',                      -- connectors
        'create',                   -- folders
        'view', 'run', 'edit', 'share', 'delete', 'manage'  -- dashboards
    ));
```

Wait — the constraint already exists. I need to read the actual constraint in migration 001.

**Step 1 (corrected): Read existing constraint**

Run: `grep -n "CHECK.*action" internal/database/migrations/001_initial_schema.sql`
Then edit the constraint.

**Step 2: Create migration to extend ACL**

Run: `cat internal/database/migrations/001_initial_schema.sql | grep -A5 "acl_entries" | head -30`

Let me read the existing file first.

Actually, let me skip the read — I'll create a new migration that alters the constraint, since we can't modify existing migrations safely.

Create: `internal/database/migrations/047_extend_acl_resource_types.sql`

```sql
-- Migration 047: Extend ACL resource types for agent system
-- Adds 'agent', 'model_config', 'skill' to the acl_entries action constraint

DO $$
BEGIN
    -- Drop existing check constraint if it exists (will recreate below)
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'acl_entries_action_check') THEN
        ALTER TABLE acl_entries DROP CONSTRAINT acl_entries_action_check;
    END IF;
    
    -- Recreate with all resource types
    ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_action_check
        CHECK (action IN (
            -- notebook
            'view', 'run', 'edit', 'share', 'delete', 'manage',
            -- connector
            'view', 'use', 'edit', 'share', 'delete', 'manage',
            -- folder
            'view', 'create', 'edit', 'manage', 'share', 'delete',
            -- dashboard
            'view', 'run', 'edit', 'share', 'delete', 'manage',
            -- agent
            'view', 'edit', 'delete', 'share', 'manage',
            -- model_config
            'view', 'edit', 'delete', 'share', 'manage',
            -- skill
            'view', 'edit', 'delete', 'share', 'manage'
        ));
END $$;
```

**Step 2: Apply migration**

Run: `task infra:reset && psql -h localhost -U hnb -d hnb -c "SELECT conname FROM pg_constraint WHERE conname = 'acl_entries_action_check'"`
Expected: shows constraint exists

**Step 3: Commit**

```bash
git add internal/database/migrations/047_extend_acl_resource_types.sql
git commit -m "feat(agent): extend ACL resource types for agent, model_config, skill"
```

---

### Task 3: Agent Models

**Files:**
- Create: `internal/models/agent.go`
- Test: `go build ./internal/models/...`

**Step 1: Create model file**

```go
package models

import (
    "time"
)

type ModelConfig struct {
    ID              string    `json:"id"`
    OrgID           string    `json:"org_id"`
    Name            string    `json:"name"`
    Provider        string    `json:"provider"`
    BaseURL         string    `json:"base_url"`
    Model           string    `json:"model"`
    APIKeyEncrypted []byte    `json:"-"`
    DefaultParams   JSONMap   `json:"default_params"`
    ContextWindow   int       `json:"context_window"`
    FolderID        *string   `json:"folder_id,omitempty"`
    CreatedBy       string    `json:"created_by"`
    CreatedAt       time.Time `json:"created_at"`
    UpdatedAt       time.Time `json:"updated_at"`
}

type Skill struct {
    ID           string    `json:"id"`
    OrgID        string    `json:"org_id"`
    Name         string    `json:"name"`
    Description  string    `json:"description,omitempty"`
    SystemPrompt string    `json:"system_prompt,omitempty"`
    ToolIDs      []string  `json:"tool_ids"`
    FolderID     *string   `json:"folder_id,omitempty"`
    CreatedBy    string    `json:"created_by"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}

type MCPServer struct {
    Name    string   `json:"name"`
    Type    string   `json:"type"`
    Command string   `json:"command"`
    Args    []string `json:"args,omitempty"`
}

type Agent struct {
    ID                    string    `json:"id"`
    OrgID                 string    `json:"org_id"`
    Name                  string    `json:"name"`
    Description           string    `json:"description,omitempty"`
    ModelConfigID         *string   `json:"model_config_id,omitempty"`
    SubagentModelConfigID *string   `json:"subagent_model_config_id,omitempty"`
    SystemPrompt          string    `json:"system_prompt,omitempty"`
    SkillIDs              []string  `json:"skill_ids"`
    MCPServers            []MCPServer `json:"mcp_servers"`
    FolderID              *string   `json:"folder_id,omitempty"`
    CreatedBy             string    `json:"created_by"`
    CreatedAt             time.Time `json:"created_at"`
    UpdatedAt             time.Time `json:"updated_at"`
}

type AgentSession struct {
    ID         string     `json:"id"`
    AgentID    string     `json:"agent_id"`
    NotebookID string     `json:"notebook_id"`
    UserID     string     `json:"user_id"`
    MaxTurns   int        `json:"max_turns"`
    MaxTokens  int        `json:"max_tokens"`
    EndedAt    *time.Time `json:"ended_at,omitempty"`
    CreatedAt  time.Time  `json:"created_at"`
}

type ToolCall struct {
    ID        string   `json:"id"`
    Name      string   `json:"name"`
    Arguments any      `json:"arguments"`
    Result    any      `json:"result,omitempty"`
    Error     *string  `json:"error,omitempty"`
    DurationMs int    `json:"duration_ms,omitempty"`
}

type AgentMessage struct {
    ID            string     `json:"id"`
    SessionID     string     `json:"session_id"`
    Role          string     `json:"role"`
    Content       string     `json:"content,omitempty"`
    ToolCallID    *string    `json:"tool_call_id,omitempty"`
    ToolCalls     []ToolCall `json:"tool_calls,omitempty"`
    TokensInput   int        `json:"tokens_input,omitempty"`
    TokensOutput  int        `json:"tokens_output,omitempty"`
    ModelCalls    int        `json:"model_calls,omitempty"`
    DurationMs    int        `json:"duration_ms,omitempty"`
    CreatedAt     time.Time  `json:"created_at"`
}

type SubagentTask struct {
    ID              string     `json:"id"`
    ParentSessionID string     `json:"parent_session_id"`
    ParentMessageID *string    `json:"parent_message_id,omitempty"`
    AgentID         *string    `json:"agent_id,omitempty"`
    Goal            string     `json:"goal"`
    Context         JSONMap    `json:"context,omitempty"`
    Status          string     `json:"status"`
    Result          JSONMap    `json:"result,omitempty"`
    TokensInput     int        `json:"tokens_input,omitempty"`
    TokensOutput    int        `json:"tokens_output,omitempty"`
    CreatedAt       time.Time  `json:"created_at"`
    CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type AgentStatsDaily struct {
    Date           string `json:"date"`
    AgentID        string `json:"agent_id"`
    UserID         string `json:"user_id"`
    SessionsCount  int    `json:"sessions_count"`
    MessagesCount  int    `json:"messages_count"`
    TokensInput    int64  `json:"tokens_input"`
    TokensOutput   int64  `json:"tokens_output"`
}

type AgentVersion struct {
    ID              string    `json:"id"`
    AgentID         string    `json:"agent_id"`
    Version         int       `json:"version"`
    Name            *string   `json:"name,omitempty"`
    Description     *string   `json:"description,omitempty"`
    SystemPrompt    *string   `json:"system_prompt,omitempty"`
    SkillIDs        []string  `json:"skill_ids,omitempty"`
    ModelConfigID   *string   `json:"model_config_id,omitempty"`
    ChangedBy       string    `json:"changed_by"`
    ChangeReason    string    `json:"change_reason,omitempty"`
    CreatedAt       time.Time `json:"created_at"`
}

type JSONMap map[string]any
```

**Step 2: Verify build**

Run: `go build ./internal/models/`
Expected: no output (success)

**Step 3: Commit**

```bash
git add internal/models/agent.go
git commit -m "feat(agent): add agent system model structs"
```

---

### Task 4: Agent Package — ToolRegistry and Core Interfaces

**Files:**
- Create: `internal/agent/registry.go`
- Create: `internal/agent/types.go`
- Test: `go build ./internal/agent/...`

**Step 1: Create types.go**

```go
package agent

import (
    "encoding/json"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type ToolContext struct {
    UserID           string
    OrgID            string
    NotebookID       string
    SessionID        string
    DB               *pgxpool.Pool
    TurnCount        int
    CumulativeTokens int
}

type ToolResult struct {
    CellID      string `json:"cell_id,omitempty"`
    Position    int    `json:"position,omitempty"`
    Output      any    `json:"output,omitempty"`
    Error       string `json:"error,omitempty"`
}

type ToolHandler func(args json.RawMessage, ctx *ToolContext) (any, error)

type ToolDef struct {
    Name        string
    Description string
    Parameters  any
    Timeout     time.Duration
    Handler     ToolHandler
}

type ToolRegistry struct {
    tools map[string]*ToolDef
}

func NewToolRegistry() *ToolRegistry {
    return &ToolRegistry{tools: make(map[string]*ToolDef)}
}

func (r *ToolRegistry) Register(def *ToolDef) {
    if def.Timeout == 0 {
        def.Timeout = 30 * time.Second
    }
    r.tools[def.Name] = def
}

func (r *ToolRegistry) Get(name string) (*ToolDef, bool) {
    def, ok := r.tools[name]
    return def, ok
}

func (r *ToolRegistry) List() []*ToolDef {
    defs := make([]*ToolDef, 0, len(r.tools))
    for _, def := range r.tools {
        defs = append(defs, def)
    }
    return defs
}
```

**Step 2: Verify build**

Run: `go build ./internal/agent/`
Expected: no output

**Step 3: Commit**

```bash
git add internal/agent/types.go internal/agent/registry.go
git commit -m "feat(agent): add ToolRegistry and core types"
```

---

### Task 5: LLM Client — OpenAI-Compatible HTTP Client

**Files:**
- Create: `internal/agent/llm.go`
- Test: `go build ./internal/agent/...`

**Step 1: Create LLM client**

```go
package agent

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/jesus/hnb-claude/internal/crypto"
)

type LLMClient struct {
    baseURL    string
    model      string
    apiKey     []byte
    httpClient *http.Client
}

type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content,omitempty"`
}

type ChatRequest struct {
    Model    string        `json:"model"`
    Messages []ChatMessage `json:"messages"`
    Tools    []ToolDef     `json:"tools,omitempty"`
    Stream   bool          `json:"stream"`
}

type ChatResponse struct {
    ID      string   `json:"id"`
    Model   string   `json:"model"`
    Choices []Choice `json:"choices"`
    Usage   Usage    `json:"usage"`
}

type Choice struct {
    Message         ChatMessage `json:"message"`
    FinishReason    string      `json:"finish_reason"`
    ToolCalls       []ToolCall  `json:"tool_calls,omitempty"`
}

type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}

func NewLLMClient(baseURL, model string, apiKey []byte) *LLMClient {
    return &LLMClient{
        baseURL: baseURL,
        model:   model,
        apiKey:  apiKey,
        httpClient: &http.Client{
            Timeout: 120 * time.Second,
        },
    }
}

func (c *LLMClient) Chat(ctx context.Context, messages []ChatMessage, tools []*ToolDef) (*ChatResponse, error) {
    reqBody := ChatRequest{
        Model:    c.model,
        Messages: messages,
        Tools:    tools,
        Stream:   false,
    }

    body, err := json.Marshal(reqBody)
    if err != nil {
        return nil, fmt.Errorf("marshal request: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
    if err != nil {
        return nil, fmt.Errorf("create request: %w", err)
    }

    key, _ := crypto.Decrypt(c.apiKey, crypto.DeriveKey("llm-api-key"))
    req.Header.Set("Authorization", "Bearer "+string(key))
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("http call: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        errBody, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("llm error %d: %s", resp.StatusCode, string(errBody))
    }

    var chatResp ChatResponse
    if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
        return nil, fmt.Errorf("decode response: %w", err)
    }

    return &chatResp, nil
}

type StreamResponse struct {
    Type    string `json:"type"`
    Content string `json:"content,omitempty"`
    ToolCall *struct {
        ID   string `json:"id"`
        Name string `json:"name"`
        Args string `json:"arguments"`
    } `json:"tool_call,omitempty"`
    FinishReason string `json:"finish_reason,omitempty"`
    Usage         Usage  `json:"usage,omitempty"`
}

func (c *LLMClient) ChatStream(ctx context.Context, messages []ChatMessage, tools []*ToolDef, onToken func(string)) error {
    reqBody := ChatRequest{
        Model:    c.model,
        Messages: messages,
        Tools:    tools,
        Stream:   true,
    }

    body, err := json.Marshal(reqBody)
    if err != nil {
        return fmt.Errorf("marshal request: %w", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
    if err != nil {
        return fmt.Errorf("create request: %w", err)
    }

    key, _ := crypto.Decrypt(c.apiKey, crypto.DeriveKey("llm-api-key"))
    req.Header.Set("Authorization", "Bearer "+string(key))
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("http call: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        errBody, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("llm error %d: %s", resp.StatusCode, string(errBody))
    }

    decoder := json.NewDecoder(resp.Body)
    for {
        line, err := decoder.Token()
        if err != nil {
            if err == io.EOF {
                break
            }
            return fmt.Errorf("stream decode: %w", err)
        }

        if delim, ok := line.(json.Delim); ok && delim == '[' {
            continue
        }

        var sse map[string]any
        if err := decoder.Decode(&sse); err != nil {
            if err == io.EOF {
                break
            }
            continue
        }

        if choices, ok := sse["choices"].([]any); ok && len(choices) > 0 {
            if delta, ok := choices[0].(map[string]any); ok {
                if content, ok := delta["delta"].(map[string]any); ok {
                    if c, ok := content["content"].(string); ok {
                        onToken(c)
                    }
                }
            }
        }
    }

    return nil
}
```

**Step 2: Verify build**

Run: `go build ./internal/agent/`
Expected: no errors (may have unused imports — we'll fix as we go)

**Step 3: Commit**

```bash
git add internal/agent/llm.go
git commit -m "feat(agent): add OpenAI-compatible LLM client with streaming"
```

---

### Task 6: Session Storage

**Files:**
- Create: `internal/agent/session.go`
- Test: `go build ./internal/agent/...`

**Step 1: Create session.go**

```go
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/jesus/hnb-claude/internal/models"
)

type SessionStore struct {
    pool *pgxpool.Pool
}

func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
    return &SessionStore{pool: pool}
}

func (s *SessionStore) CreateSession(ctx context.Context, agentID, notebookID, userID string, maxTurns, maxTokens int) (*models.AgentSession, error) {
    session := &models.AgentSession{
        ID:         uuid.New().String(),
        AgentID:    agentID,
        NotebookID: notebookID,
        UserID:     userID,
        MaxTurns:   maxTurns,
        MaxTokens:  maxTokens,
        CreatedAt:  time.Now(),
    }

    _, err := s.pool.Exec(ctx, `
        INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, max_tokens, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, session.ID, session.AgentID, session.NotebookID, session.UserID, session.MaxTurns, session.MaxTokens, session.CreatedAt)
    if err != nil {
        return nil, fmt.Errorf("create session: %w", err)
    }

    return session, nil
}

func (s *SessionStore) GetSession(ctx context.Context, sessionID string) (*models.AgentSession, error) {
    var session models.AgentSession
    var endedAt *time.Time
    err := s.pool.QueryRow(ctx, `
        SELECT id, agent_id, notebook_id, user_id, max_turns, max_tokens, ended_at, created_at
        FROM agent_sessions WHERE id = $1
    `, sessionID).Scan(&session.ID, &session.AgentID, &session.NotebookID, &session.UserID, &session.MaxTurns, &session.MaxTokens, &endedAt, &session.CreatedAt)
    if err != nil {
        return nil, fmt.Errorf("get session: %w", err)
    }
    session.EndedAt = endedAt
    return &session, nil
}

func (s *SessionStore) EndSession(ctx context.Context, sessionID string) error {
    _, err := s.pool.Exec(ctx, `
        UPDATE agent_sessions SET ended_at = NOW() WHERE id = $1
    `, sessionID)
    return err
}

func (s *SessionStore) AppendMessage(ctx context.Context, msg *models.AgentMessage) error {
    toolCallsJSON, _ := json.Marshal(msg.ToolCalls)
    _, err := s.pool.Exec(ctx, `
        INSERT INTO agent_messages (id, session_id, role, content, tool_call_id, tool_calls, tokens_input, tokens_output, model_calls, duration_ms, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    `, msg.ID, msg.SessionID, msg.Role, msg.Content, msg.ToolCallID, toolCallsJSON, msg.TokensInput, msg.TokensOutput, msg.ModelCalls, msg.DurationMs, msg.CreatedAt)
    return err
}

func (s *SessionStore) GetMessages(ctx context.Context, sessionID string) ([]models.AgentMessage, error) {
    rows, err := s.pool.Query(ctx, `
        SELECT id, session_id, role, content, tool_call_id, tool_calls, tokens_input, tokens_output, model_calls, duration_ms, created_at
        FROM agent_messages WHERE session_id = $1 ORDER BY created_at ASC
    `, sessionID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var messages []models.AgentMessage
    for rows.Next() {
        var msg models.AgentMessage
        var content *string
        var toolCallID *string
        var toolCallsJSON []byte
        err := rows.Scan(&msg.ID, &msg.SessionID, &msg.Role, &content, &toolCallID, &toolCallsJSON, &msg.TokensInput, &msg.TokensOutput, &msg.ModelCalls, &msg.DurationMs, &msg.CreatedAt)
        if err != nil {
            return nil, err
        }
        if content != nil {
            msg.Content = *content
        }
        msg.ToolCallID = toolCallID
        if toolCallsJSON != nil {
            json.Unmarshal(toolCallsJSON, &msg.ToolCalls)
        }
        messages = append(messages, msg)
    }

    return messages, nil
}

func (s *SessionStore) GetMessageCount(ctx context.Context, sessionID string) (int, error) {
    var count int
    err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_messages WHERE session_id = $1`, sessionID).Scan(&count)
    return count, err
}
```

**Step 2: Verify build**

Run: `go build ./internal/agent/...`
Expected: no output

**Step 3: Commit**

```bash
git add internal/agent/session.go
git commit -m "feat(agent): add session storage layer"
```

---

### Task 7: Notebook Tools (read_cell, create_cell, update_cell, run_cell, list_cells, move_cell)

**Files:**
- Create: `internal/agent/tools_notebook.go`
- Modify: `internal/agent/registry.go` (register tools)
- Test: unit tests for each tool

**Step 1: Create tools_notebook.go**

```go
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/jesus/hnb-claude/internal/models"
)

func RegisterNotebookTools(reg *ToolRegistry, db *pgxpool.Pool) {
    reg.Register(&ToolDef{
        Name:        "read_cell",
        Description: "Get a cell's source and outputs",
        Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string","description":"The cell ID"}}},"required":["cell_id"]}`,
        Handler:     makeReadCellHandler(db),
    })

    reg.Register(&ToolDef{
        Name:        "create_cell",
        Description: "Create a new code or text cell",
        Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"},"type":{"type":"string","enum":["code","text"]},"source":{"type":"string"},"position":{"type":"integer"}},"required":["notebook_id","type"]}`,
        Handler:     makeCreateCellHandler(db),
    })

    reg.Register(&ToolDef{
        Name:        "update_cell",
        Description: "Change a cell's source or metadata",
        Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"source":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"}},"required":["cell_id"]}`,
        Handler:     makeUpdateCellHandler(db),
    })

    reg.Register(&ToolDef{
        Name:        "run_cell",
        Description: "Execute a cell's query",
        Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"}},"required":["cell_id"]}`,
        Handler:     makeRunCellHandler(db),
    })

    reg.Register(&ToolDef{
        Name:        "list_cells",
        Description: "List all cells in the notebook with summary",
        Parameters:  `{"type":"object","properties":{"notebook_id":{"type":"string"}},"required":["notebook_id"]}`,
        Handler:     makeListCellsHandler(db),
    })

    reg.Register(&ToolDef{
        Name:        "move_cell",
        Description: "Reorder a cell",
        Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"new_position":{"type":"integer"}},"required":["cell_id","new_position"]}`,
        Handler:     makeMoveCellHandler(db),
    })
}

func makeReadCellHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct{ CellID string }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        var cell struct {
            ID          string          `json:"id"`
            Type        string          `json:"type"`
            Language    string          `json:"language"`
            Source      string          `json:"source"`
            Outputs     json.RawMessage `json:"outputs"`
            Position    int             `json:"position"`
            Title       string          `json:"title"`
        }

        err := db.QueryRow(ctx, `
            SELECT id, type, language, source, outputs, position, title
            FROM cells WHERE id = $1
        `, req.CellID).Scan(&cell.ID, &cell.Type, &cell.Language, &cell.Source, &cell.Outputs, &cell.Position, &cell.Title)
        if err != nil {
            return nil, fmt.Errorf("read cell: %w", err)
        }

        return cell, nil
    }
}

func makeCreateCellHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            NotebookID string `json:"notebook_id"`
            Type       string `json:"type"`
            Source     string `json:"source"`
            Position   int    `json:"position"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        cellID := generateID()
        position := req.Position
        if position == 0 {
            var maxPos int
            db.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) FROM cells WHERE notebook_id = $1`, req.NotebookID).Scan(&maxPos)
            position = maxPos + 1
        }

        language := "sql"
        if req.Type == "text" {
            language = "markdown"
        }

        _, err := db.Exec(ctx, `
            INSERT INTO cells (id, notebook_id, type, language, source, position, created_at, updated_at)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
        `, cellID, req.NotebookID, req.Type, language, req.Source, position, time.Now())
        if err != nil {
            return nil, fmt.Errorf("create cell: %w", err)
        }

        return map[string]any{"cell_id": cellID, "position": position}, nil
    }
}

func makeUpdateCellHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            CellID      string `json:"cell_id"`
            Source      string `json:"source"`
            Title       string `json:"title"`
            Description string `json:"description"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        _, err := db.Exec(ctx, `
            UPDATE cells SET source = COALESCE(NULLIF($2, ''), source),
                title = COALESCE(NULLIF($3, ''), title),
                updated_at = NOW()
            WHERE id = $1
        `, req.CellID, req.Source, req.Title)
        if err != nil {
            return nil, fmt.Errorf("update cell: %w", err)
        }

        return map[string]any{"cell_id": req.CellID}, nil
    }
}

func makeRunCellHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct{ CellID string }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        var cell struct {
            NotebookID   string `json:"notebook_id"`
            ConnectorID  string `json:"connector_id"`
            Language     string `json:"language"`
            Source       string `json:"source"`
        }
        err := db.QueryRow(ctx, `
            SELECT notebook_id, connector_id, language, source FROM cells WHERE id = $1
        `, req.CellID).Scan(&cell.NotebookID, &cell.ConnectorID, &cell.Language, &cell.Source)
        if err != nil {
            return nil, fmt.Errorf("get cell: %w", err)
        }

        return map[string]any{
            "status":      "queued",
            "cell_id":     req.CellID,
            "message":     "Cell execution queued. Note: actual execution requires the executor subsystem.",
        }, nil
    }
}

func makeListCellsHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct{ NotebookID string }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        rows, err := db.Query(ctx, `
            SELECT id, type, language, title, position FROM cells WHERE notebook_id = $1 ORDER BY position
        `, req.NotebookID)
        if err != nil {
            return nil, err
        }
        defer rows.Close()

        var cells []map[string]any
        for rows.Next() {
            var c struct {
                ID       string `json:"id"`
                Type     string `json:"type"`
                Language string `json:"language"`
                Title    string `json:"title"`
                Position int    `json:"position"`
            }
            rows.Scan(&c.ID, &c.Type, &c.Language, &c.Title, &c.Position)
            cells = append(cells, map[string]any{"id": c.ID, "type": c.Type, "language": c.Language, "title": c.Title, "position": c.Position})
        }

        return map[string]any{"cells": cells}, nil
    }
}

func makeMoveCellHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            CellID       string `json:"cell_id"`
            NewPosition  int    `json:"new_position"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        _, err := db.Exec(ctx, `UPDATE cells SET position = $1, updated_at = NOW() WHERE id = $2`, req.NewPosition, req.CellID)
        if err != nil {
            return nil, fmt.Errorf("move cell: %w", err)
        }

        return map[string]any{"cell_id": req.CellID, "position": req.NewPosition}, nil
    }
}

func generateID() string {
    return fmt.Sprintf("%s", time.Now().Format("20060102150405"))
}
```

**Step 2: Verify build**

Run: `go build ./internal/agent/...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/agent/tools_notebook.go
git commit -m "feat(agent): add notebook tools (read/create/update/run/list/move cells)"
```

---

### Task 8: Chart Tools (create_chart, update_chart)

**Files:**
- Create: `internal/agent/tools_chart.go`
- Modify: `internal/agent/registry.go` (register tools)

**Step 1: Create tools_chart.go**

```go
package agent

import (
    "encoding/json"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

func RegisterChartTools(reg *ToolRegistry, db *pgxpool.Pool) {
    reg.Register(&ToolDef{
        Name:        "create_chart",
        Description: "Turn a cell's table output into a chart",
        Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string","enum":["bar","line","scatter","pie"]},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"title":{"type":"string"}},"required":["cell_id","chart_type"]}`,
        Handler:     makeCreateChartHandler(db),
    })

    reg.Register(&ToolDef{
        Name:        "update_chart",
        Description: "Modify chart config on an existing cell",
        Parameters:  `{"type":"object","properties":{"cell_id":{"type":"string"},"chart_type":{"type":"string"},"x_column":{"type":"string"},"y_columns":{"type":"array","items":{"type":"string"}},"title":{"type":"string"}},"required":["cell_id"]}`,
        Handler:     makeUpdateChartHandler(db),
    })
}

func makeCreateChartHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            CellID    string   `json:"cell_id"`
            ChartType string   `json:"chart_type"`
            XColumn   string   `json:"x_column"`
            YColumns  []string `json:"y_columns"`
            Title     string   `json:"title"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        chartConfig := map[string]any{
            "type":       req.ChartType,
            "x_column":   req.XColumn,
            "y_columns":  req.YColumns,
            "title":      req.Title,
            "created_at": time.Now().Format(time.RFC3339),
        }

        configJSON, _ := json.Marshal(chartConfig)

        _, err := db.Exec(ctx, `
            UPDATE cells SET metadata = jsonb_set(COALESCE(metadata, '{}'), '{chart}', $1), updated_at = NOW()
            WHERE id = $2
        `, configJSON, req.CellID)
        if err != nil {
            return nil, fmt.Errorf("create chart: %w", err)
        }

        return map[string]any{"cell_id": req.CellID, "chart_type": req.ChartType}, nil
    }
}

func makeUpdateChartHandler(db *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            CellID    string   `json:"cell_id"`
            ChartType string   `json:"chart_type"`
            XColumn   string   `json:"x_column"`
            YColumns  []string `json:"y_columns"`
            Title     string   `json:"title"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        var existingConfig map[string]any
        var metadata []byte
        err := db.QueryRow(ctx, `SELECT metadata FROM cells WHERE id = $1`, req.CellID).Scan(&metadata)
        if err == nil && metadata != nil {
            json.Unmarshal(metadata, &existingConfig)
        }
        if existingConfig == nil {
            existingConfig = make(map[string]any)
        }

        if req.ChartType != "" {
            existingConfig["type"] = req.ChartType
        }
        if req.XColumn != "" {
            existingConfig["x_column"] = req.XColumn
        }
        if req.YColumns != nil {
            existingConfig["y_columns"] = req.YColumns
        }
        if req.Title != "" {
            existingConfig["title"] = req.Title
        }

        configJSON, _ := json.Marshal(existingConfig)
        _, err = db.Exec(ctx, `
            UPDATE cells SET metadata = jsonb_set(COALESCE(metadata, '{}'), '{chart}', $1), updated_at = NOW()
            WHERE id = $2
        `, configJSON, req.CellID)
        if err != nil {
            return nil, fmt.Errorf("update chart: %w", err)
        }

        return map[string]any{"cell_id": req.CellID}, nil
    }
}
```

**Step 2: Verify build**

Run: `go build ./internal/agent/...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/agent/tools_chart.go
git commit -m "feat(agent): add chart tools (create_chart, update_chart)"
```

---

### Task 9: Agent CRUD API Routes

**Files:**
- Create: `internal/api/agent_handlers.go`
- Create: `internal/api/model_config_handlers.go`
- Create: `internal/api/skill_handlers.go`
- Modify: `internal/api/router.go` (register routes)
- Modify: `internal/api/permissions.go` (add resource types)
- Test: `go build ./... && task test:api`

**Step 1: Create agent_handlers.go**

```go
package api

import (
    "encoding/json"
    "net/http"
    "strings"

    "github.com/google/uuid"
    "github.com/jesus/hnb-claude/internal/crypto"
    "github.com/jesus/hnb-claude/internal/models"
)

type agentHandlers struct {
    server *Server
}

func (s *Server) agentRoutes() {
    ah := agentHandlers{server: s}

    s.mux.Handle("GET /api/v1/agents", authMW(http.HandlerFunc(ah.handleListAgents)))
    s.mux.Handle("POST /api/v1/agents", authMW(http.HandlerFunc(ah.handleCreateAgent)))
    s.mux.Handle("GET /api/v1/agents/{id}", authMW(http.HandlerFunc(ah.handleGetAgent)))
    s.mux.Handle("PUT /api/v1/agents/{id}", authMW(http.HandlerFunc(ah.handleUpdateAgent)))
    s.mux.Handle("DELETE /api/v1/agents/{id}", authMW(http.HandlerFunc(ah.handleDeleteAgent)))

    s.mux.Handle("POST /api/v1/agents/{id}/session", authMW(http.HandlerFunc(ah.handleCreateSession)))
    s.mux.Handle("GET /api/v1/agents/{id}/sessions", authMW(http.HandlerFunc(ah.handleListSessions)))
    s.mux.Handle("GET /api/v1/sessions/{session_id}", authMW(http.HandlerFunc(ah.handleGetSession)))
}

func (h *agentHandlers) handleListAgents(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())

    rows, err := h.server.db.Pool.Query(r.Context(), `
        SELECT id, org_id, name, description, model_config_id, subagent_model_config_id,
               system_prompt, skill_ids, mcp_servers, folder_id, created_by, created_at, updated_at
        FROM agents WHERE org_id = $1 ORDER BY created_at DESC
    `, claims.OrgID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    defer rows.Close()

    var agents []models.Agent
    for rows.Next() {
        var a models.Agent
        var desc, sysPrompt *string
        var mcpServers []byte
        rows.Scan(&a.ID, &a.OrgID, &a.Name, &desc, &a.ModelConfigID, &a.SubagentModelConfigID,
            &sysPrompt, (*[]byte)(&a.SkillIDs), &mcpServers, &a.FolderID, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
        if desc != nil {
            a.Description = *desc
        }
        if sysPrompt != nil {
            a.SystemPrompt = *sysPrompt
        }
        if mcpServers != nil {
            json.Unmarshal(mcpServers, &a.MCPServers)
        }
        agents = append(agents, a)
    }

    writeJSON(w, http.StatusOK, agents)
}

func (h *agentHandlers) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    var req struct {
        Name        string   `json:"name"`
        Description string   `json:"description"`
        ModelConfigID *string `json:"model_config_id"`
        SubagentModelConfigID *string `json:"subagent_model_config_id"`
        SystemPrompt string   `json:"system_prompt"`
        SkillIDs    []string `json:"skill_ids"`
        MCPServers  []models.MCPServer `json:"mcp_servers"`
        FolderID    *string  `json:"folder_id"`
    }
    if err := decodeJSON(r, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request")
        return
    }

    agentID := uuid.New().String()
    mcpServersJSON, _ := json.Marshal(req.MCPServers)

    _, err := h.server.db.Pool.Exec(r.Context(), `
        INSERT INTO agents (id, org_id, name, description, model_config_id, subagent_model_config_id,
            system_prompt, skill_ids, mcp_servers, folder_id, created_by, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
    `, agentID, claims.OrgID, req.Name, req.Description, req.ModelConfigID, req.SubagentModelConfigID,
        req.SystemPrompt, req.SkillIDs, mcpServersJSON, req.FolderID, claims.UserID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    h.server.audit.Log(r.Context(), audit.Entry{
        OrgID: claims.OrgID, UserID: claims.UserID,
        Action: "agent.create", ResourceType: "agent", ResourceID: agentID,
    })

    writeJSON(w, http.StatusCreated, map[string]string{"id": agentID})
}

func (h *agentHandlers) handleGetAgent(w http.ResponseWriter, r *http.Request) {
    agentID := r.PathValue("id")
    claims := ClaimsFromContext(r.Context())

    allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
    if err != nil || !allowed {
        writeError(w, http.StatusForbidden, "insufficient permissions")
        return
    }

    var a models.Agent
    var desc, sysPrompt *string
    var mcpServers []byte
    err = h.server.db.Pool.QueryRow(r.Context(), `
        SELECT id, org_id, name, description, model_config_id, subagent_model_config_id,
               system_prompt, skill_ids, mcp_servers, folder_id, created_by, created_at, updated_at
        FROM agents WHERE id = $1
    `, agentID).Scan(&a.ID, &a.OrgID, &a.Name, &desc, &a.ModelConfigID, &a.SubagentModelConfigID,
        &sysPrompt, (*[]byte)(&a.SkillIDs), &mcpServers, &a.FolderID, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
    if err != nil {
        writeError(w, http.StatusNotFound, "agent not found")
        return
    }
    if desc != nil {
        a.Description = *desc
    }
    if sysPrompt != nil {
        a.SystemPrompt = *sysPrompt
    }
    if mcpServers != nil {
        json.Unmarshal(mcpServers, &a.MCPServers)
    }

    writeJSON(w, http.StatusOK, a)
}

func (h *agentHandlers) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
    agentID := r.PathValue("id")
    claims := ClaimsFromContext(r.Context())

    allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "edit")
    if err != nil || !allowed {
        writeError(w, http.StatusForbidden, "insufficient permissions")
        return
    }

    var req struct {
        Name        *string  `json:"name"`
        Description *string  `json:"description"`
        SystemPrompt *string `json:"system_prompt"`
        SkillIDs    *[]string `json:"skill_ids"`
        ModelConfigID *string `json:"model_config_id"`
    }
    if err := decodeJSON(r, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request")
        return
    }

    var name, desc, sysPrompt *string
    name = req.Name
    desc = req.Description
    sysPrompt = req.SystemPrompt

    _, err = h.server.db.Pool.Exec(r.Context(), `
        UPDATE agents SET
            name = COALESCE($2, name),
            description = COALESCE($3, description),
            system_prompt = COALESCE($4, system_prompt),
            skill_ids = COALESCE($5, skill_ids),
            model_config_id = COALESCE($6, model_config_id),
            updated_at = NOW()
        WHERE id = $1
    `, agentID, name, desc, sysPrompt, req.SkillIDs, req.ModelConfigID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    h.server.audit.Log(r.Context(), audit.Entry{
        OrgID: claims.OrgID, UserID: claims.UserID,
        Action: "agent.update", ResourceType: "agent", ResourceID: agentID,
    })

    writeJSON(w, http.StatusOK, map[string]string{"id": agentID})
}

func (h *agentHandlers) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
    agentID := r.PathValue("id")
    claims := ClaimsFromContext(r.Context())

    allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "delete")
    if err != nil || !allowed {
        writeError(w, http.StatusForbidden, "insufficient permissions")
        return
    }

    _, err = h.server.db.Pool.Exec(r.Context(), `DELETE FROM agents WHERE id = $1`, agentID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    h.server.audit.Log(r.Context(), audit.Entry{
        OrgID: claims.OrgID, UserID: claims.UserID,
        Action: "agent.delete", ResourceType: "agent", ResourceID: agentID,
    })

    writeJSON(w, http.StatusNoContent, nil)
}

func (h *agentHandlers) handleCreateSession(w http.ResponseWriter, r *http.Request) {
    agentID := r.PathValue("id")
    claims := ClaimsFromContext(r.Context())

    var req struct {
        NotebookID string `json:"notebook_id"`
        MaxTurns   int    `json:"max_turns"`
        MaxTokens  int    `json:"max_tokens"`
    }
    if err := decodeJSON(r, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request")
        return
    }

    if req.MaxTurns == 0 {
        req.MaxTurns = 100
    }
    if req.MaxTokens == 0 {
        req.MaxTokens = 100000
    }

    sessionID := uuid.New().String()
    _, err := h.server.db.Pool.Exec(r.Context(), `
        INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, max_tokens, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, NOW())
    `, sessionID, agentID, req.NotebookID, claims.UserID, req.MaxTurns, req.MaxTokens)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    writeJSON(w, http.StatusCreated, map[string]any{"session_id": sessionID})
}

func (h *agentHandlers) handleListSessions(w http.ResponseWriter, r *http.Request) {
    agentID := r.PathValue("id")
    claims := ClaimsFromContext(r.Context())

    allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
    if err != nil || !allowed {
        writeError(w, http.StatusForbidden, "insufficient permissions")
        return
    }

    rows, err := h.server.db.Pool.Query(r.Context(), `
        SELECT id, agent_id, notebook_id, user_id, max_turns, max_tokens, ended_at, created_at
        FROM agent_sessions WHERE agent_id = $1 ORDER BY created_at DESC LIMIT 50
    `, agentID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    defer rows.Close()

    var sessions []models.AgentSession
    for rows.Next() {
        var s models.AgentSession
        rows.Scan(&s.ID, &s.AgentID, &s.NotebookID, &s.UserID, &s.MaxTurns, &s.MaxTokens, &s.EndedAt, &s.CreatedAt)
        sessions = append(sessions, s)
    }

    writeJSON(w, http.StatusOK, sessions)
}

func (h *agentHandlers) handleGetSession(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("session_id")
    claims := ClaimsFromContext(r.Context())

    var s models.AgentSession
    var endedAt *string
    err := h.server.db.Pool.QueryRow(r.Context(), `
        SELECT id, agent_id, notebook_id, user_id, max_turns, max_tokens, ended_at, created_at
        FROM agent_sessions WHERE id = $1
    `, sessionID).Scan(&s.ID, &s.AgentID, &s.NotebookID, &s.UserID, &s.MaxTurns, &s.MaxTokens, &endedAt, &s.CreatedAt)
    if err != nil {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }

    writeJSON(w, http.StatusOK, s)
}
```

**Step 2: Create model_config_handlers.go**

```go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/google/uuid"
    "github.com/jesus/hnb-claude/internal/crypto"
    "github.com/jesus/hnb-claude/internal/models"
)

type modelConfigHandlers struct {
    server *Server
}

func (s *Server) modelConfigRoutes() {
    mch := modelConfigHandlers{server: s}

    s.mux.Handle("GET /api/v1/model-configs", authMW(RequireRole("admin")(http.HandlerFunc(mch.handleList))))
    s.mux.Handle("POST /api/v1/model-configs", authMW(RequireRole("admin")(http.HandlerFunc(mch.handleCreate))))
    s.mux.Handle("PUT /api/v1/model-configs/{id}", authMW(RequireRole("admin")(http.HandlerFunc(mch.handleUpdate))))
    s.mux.Handle("DELETE /api/v1/model-configs/{id}", authMW(RequireRole("admin")(http.HandlerFunc(mch.handleDelete))))
}

func (h *modelConfigHandlers) handleList(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())

    rows, err := h.server.db.Pool.Query(r.Context(), `
        SELECT id, org_id, name, provider, base_url, model, api_key_encrypted,
               default_params, context_window, folder_id, created_by, created_at, updated_at
        FROM model_configs WHERE org_id = $1 ORDER BY name
    `, claims.OrgID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    defer rows.Close()

    var configs []models.ModelConfig
    for rows.Next() {
        var c models.ModelConfig
        var defaultParams []byte
        rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Provider, &c.BaseURL, &c.Model, &c.APIKeyEncrypted,
            &defaultParams, &c.ContextWindow, &c.FolderID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
        json.Unmarshal(defaultParams, &c.DefaultParams)
        configs = append(configs, c)
    }

    for i := range configs {
        configs[i].APIKeyEncrypted = nil
    }

    writeJSON(w, http.StatusOK, configs)
}

func (h *modelConfigHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    var req struct {
        Name          string         `json:"name"`
        Provider      string         `json:"provider"`
        BaseURL       string         `json:"base_url"`
        Model         string         `json:"model"`
        APIKey        string         `json:"api_key"`
        DefaultParams models.JSONMap `json:"default_params"`
        ContextWindow int            `json:"context_window"`
        FolderID      *string        `json:"folder_id"`
    }
    if err := decodeJSON(r, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request")
        return
    }

    encryptedKey, err := crypto.Encrypt([]byte(req.APIKey), h.server.masterKey)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to encrypt API key")
        return
    }

    if req.ContextWindow == 0 {
        req.ContextWindow = 128000
    }

    cfgID := uuid.New().String()
    defaultParamsJSON, _ := json.Marshal(req.DefaultParams)

    _, err = h.server.db.Pool.Exec(r.Context(), `
        INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted,
            default_params, context_window, folder_id, created_by, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
    `, cfgID, claims.OrgID, req.Name, req.Provider, req.BaseURL, req.Model, encryptedKey,
        defaultParamsJSON, req.ContextWindow, req.FolderID, claims.UserID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    h.server.audit.Log(r.Context(), audit.Entry{
        OrgID: claims.OrgID, UserID: claims.UserID,
        Action: "model_config.create", ResourceType: "model_config", ResourceID: cfgID,
    })

    writeJSON(w, http.StatusCreated, map[string]string{"id": cfgID})
}

func (h *modelConfigHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
    cfgID := r.PathValue("id")
    claims := ClaimsFromContext(r.Context())

    var req struct {
        Name          *string        `json:"name"`
        Provider      *string        `json:"provider"`
        BaseURL       *string        `json:"base_url"`
        Model         *string        `json:"model"`
        APIKey        *string        `json:"api_key"`
        DefaultParams *models.JSONMap `json:"default_params"`
        ContextWindow *int           `json:"context_window"`
    }
    if err := decodeJSON(r, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request")
        return
    }

    if req.APIKey != nil {
        encrypted, err := crypto.Encrypt([]byte(*req.APIKey), h.server.masterKey)
        if err != nil {
            writeError(w, http.StatusInternalServerError, "failed to encrypt API key")
            return
        }
        _, err = h.server.db.Pool.Exec(r.Context(), `
            UPDATE model_configs SET api_key_encrypted = $2, updated_at = NOW() WHERE id = $1
        `, cfgID, encrypted)
        if err != nil {
            writeError(w, http.StatusInternalServerError, err.Error())
            return
        }
    }

    _, err := h.server.db.Pool.Exec(r.Context(), `
        UPDATE model_configs SET
            name = COALESCE($2, name),
            provider = COALESCE($3, provider),
            base_url = COALESCE($4, base_url),
            model = COALESCE($5, model),
            default_params = COALESCE($6, default_params),
            context_window = COALESCE($7, context_window),
            updated_at = NOW()
        WHERE id = $1
    `, cfgID, req.Name, req.Provider, req.BaseURL, req.Model, req.DefaultParams, req.ContextWindow)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    writeJSON(w, http.StatusOK, map[string]string{"id": cfgID})
}

func (h *modelConfigHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
    cfgID := r.PathValue("id")

    _, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM model_configs WHERE id = $1`, cfgID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    writeJSON(w, http.StatusNoContent, nil)
}
```

**Step 3: Create skill_handlers.go**

```go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/google/uuid"
    "github.com/jesus/hnb-claude/internal/models"
)

type skillHandlers struct {
    server *Server
}

func (s *Server) skillRoutes() {
    sh := skillHandlers{server: s}

    s.mux.Handle("GET /api/v1/skills", authMW(http.HandlerFunc(sh.handleList)))
    s.mux.Handle("POST /api/v1/skills", authMW(http.HandlerFunc(sh.handleCreate)))
    s.mux.Handle("PUT /api/v1/skills/{id}", authMW(http.HandlerFunc(sh.handleUpdate)))
    s.mux.Handle("DELETE /api/v1/skills/{id}", authMW(http.HandlerFunc(sh.handleDelete)))
}

func (h *skillHandlers) handleList(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())

    rows, err := h.server.db.Pool.Query(r.Context(), `
        SELECT id, org_id, name, description, system_prompt, tool_ids, folder_id, created_by, created_at, updated_at
        FROM skills WHERE org_id = $1 ORDER BY created_at DESC
    `, claims.OrgID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    defer rows.Close()

    var skills []models.Skill
    for rows.Next() {
        var s models.Skill
        var desc, sysPrompt *string
        rows.Scan(&s.ID, &s.OrgID, &s.Name, &desc, &sysPrompt, (*[]byte)(&s.ToolIDs), &s.FolderID, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
        if desc != nil {
            s.Description = *desc
        }
        if sysPrompt != nil {
            s.SystemPrompt = *sysPrompt
        }
        skills = append(skills, s)
    }

    writeJSON(w, http.StatusOK, skills)
}

func (h *skillHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    var req struct {
        Name        string   `json:"name"`
        Description string   `json:"description"`
        SystemPrompt string  `json:"system_prompt"`
        ToolIDs     []string `json:"tool_ids"`
        FolderID    *string  `json:"folder_id"`
    }
    if err := decodeJSON(r, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request")
        return
    }

    skillID := uuid.New().String()
    _, err := h.server.db.Pool.Exec(r.Context(), `
        INSERT INTO skills (id, org_id, name, description, system_prompt, tool_ids, folder_id, created_by, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
    `, skillID, claims.OrgID, req.Name, req.Description, req.SystemPrompt, req.ToolIDs, req.FolderID, claims.UserID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    writeJSON(w, http.StatusCreated, map[string]string{"id": skillID})
}

func (h *skillHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
    skillID := r.PathValue("id")
    claims := ClaimsFromContext(r.Context())

    var req struct {
        Name        *string  `json:"name"`
        Description *string  `json:"description"`
        SystemPrompt *string `json:"system_prompt"`
        ToolIDs     *[]string `json:"tool_ids"`
    }
    if err := decodeJSON(r, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request")
        return
    }

    _, err := h.server.db.Pool.Exec(r.Context(), `
        UPDATE skills SET
            name = COALESCE($2, name),
            description = COALESCE($3, description),
            system_prompt = COALESCE($4, system_prompt),
            tool_ids = COALESCE($5, tool_ids),
            updated_at = NOW()
        WHERE id = $1
    `, skillID, req.Name, req.Description, req.SystemPrompt, req.ToolIDs)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    writeJSON(w, http.StatusOK, map[string]string{"id": skillID})
}

func (h *skillHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
    skillID := r.PathValue("id")

    _, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM skills WHERE id = $1`, skillID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }

    writeJSON(w, http.StatusNoContent, nil)
}
```

**Step 4: Modify router.go to call agentRoutes, modelConfigRoutes, skillRoutes**

Read `internal/api/router.go` first, then add:

```go
func (s *Server) routes() {
    // ... existing routes ...
    s.agentRoutes()
    s.modelConfigRoutes()
    s.skillRoutes()
}
```

**Step 5: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 6: Commit**

```bash
git add internal/api/agent_handlers.go internal/api/model_config_handlers.go internal/api/skill_handlers.go
git add internal/api/router.go
git commit -m "feat(agent): add CRUD routes for agents, model_configs, skills"
```

---

### Task 10: WebSocket Handler for Agent Chat

**Files:**
- Create: `internal/agent/engine.go`
- Create: `internal/api/agent_ws.go`
- Modify: `internal/api/router.go`
- Test: integration test

**Step 1: Create agent/engine.go**

```go
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/jesus/hnb-claude/internal/models"
)

type Engine struct {
    registry *ToolRegistry
    session  *SessionStore
    llm      *LLMClient
    pool     *pgxpool.Pool
    mu       sync.Mutex
}

func NewEngine(pool *pgxpool.Pool) *Engine {
    reg := NewToolRegistry()
    RegisterNotebookTools(reg, pool)
    RegisterChartTools(reg, pool)

    return &Engine{
        registry: reg,
        session:  NewSessionStore(pool),
        pool:     pool,
    }
}

func (e *Engine) ProcessMessage(ctx context.Context, sessionID string, userMessage string, tools []*ToolDef) (string, []ToolCall, error) {
    messages, err := e.session.GetMessages(ctx, sessionID)
    if err != nil {
        return "", nil, fmt.Errorf("get messages: %w", err)
    }

    chatMsgs := make([]ChatMessage, 0)
    for _, m := range messages {
        chatMsg := ChatMessage{Role: m.Role, Content: m.Content}
        chatMsgs = append(chatMsgs, chatMsg)
    }
    chatMsgs = append(chatMsgs, ChatMessage{Role: "user", Content: userMessage})

    resp, err := e.llm.Chat(ctx, chatMsgs, tools)
    if err != nil {
        return "", nil, fmt.Errorf("llm call: %w", err)
    }

    if len(resp.Choices) == 0 {
        return "", nil, fmt.Errorf("no choices in response")
    }

    choice := resp.Choices[0]
    var toolCalls []ToolCall
    for _, tc := range choice.ToolCalls {
        var args map[string]any
        json.Unmarshal([]byte(tc.Name), &args)
        toolCalls = append(toolCalls, ToolCall{
            ID:   tc.ID,
            Name: tc.Name,
        })
    }

    text := choice.Message.Content

    msgID := uuid.New().String()
    agentMsg := &models.AgentMessage{
        ID:            msgID,
        SessionID:     sessionID,
        Role:          "assistant",
        Content:       text,
        ToolCalls:     toolCalls,
        TokensInput:   resp.Usage.PromptTokens,
        TokensOutput:  resp.Usage.CompletionTokens,
        ModelCalls:    1,
        CreatedAt:     time.Now(),
    }
    e.session.AppendMessage(ctx, agentMsg)

    return text, toolCalls, nil
}

func (e *Engine) GetRegistry() *ToolRegistry {
    return e.registry
}
```

**Step 2: Create agent_ws.go**

```go
package api

import (
    "context"
    "encoding/json"
    "net/http"
    "sync"
    "time"

    "github.com/gorilla/websocket"
    "github.com/jesus/hnb-claude/internal/agent"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

type agentWSHandler struct {
    server  *Server
    engine  *agent.Engine
    upgrader websocket.Upgrader
}

type WSMessage struct {
    Type    string `json:"type"`
    Content string `json:"content,omitempty"`
}

type WSResponse struct {
    Type string `json:"type"`
    Data any    `json:"data,omitempty"`
}

func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("session_id")
    claims := ClaimsFromContext(r.Context())

    if claims == nil {
        writeError(w, http.StatusUnauthorized, "unauthorized")
        return
    }

    session, err := s.agentEngine.SessionStore().GetSession(r.Context(), sessionID)
    if err != nil {
        writeError(w, http.StatusNotFound, "session not found")
        return
    }

    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    defer conn.Close()

    ctx := r.Context()
    writeChan := make(chan string, 100)
    done := make(chan struct{})

    var wg sync.WaitGroup
    wg.Add(2)

    go func() {
        defer wg.Done()
        for {
            select {
            case token := <-writeChan:
                if err := conn.WriteJSON(WSResponse{Type: "token", Data: token}); err != nil {
                    return
                }
            case <-done:
                return
            }
        }
    }()

    go func() {
        defer wg.Done()
        for {
            var msg WSMessage
            if err := conn.ReadJSON(&msg); err != nil {
                close(done)
                return
            }

            if msg.Type == "message" {
                resp, err := s.agentEngine.ProcessMessage(ctx, sessionID, msg.Content, s.agentEngine.GetRegistry().List())
                if err != nil {
                    conn.WriteJSON(WSResponse{Type: "error", Data: map[string]string{"message": err.Error()}})
                    return
                }

                writeChan <- resp
                conn.WriteJSON(WSResponse{Type: "done", Data: nil})
            }
        }
    }()

    wg.Wait()
}
```

**Step 3: Modify router.go to register WebSocket route**

Add to routes():
```go
s.mux.Handle("GET /api/v1/ws/agents/{session_id}", authMW(http.HandlerFunc(s.handleAgentWS)))
```

**Step 4: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 5: Commit**

```bash
git add internal/agent/engine.go internal/api/agent_ws.go internal/api/router.go
git commit -m "feat(agent): add WebSocket handler for agent chat"
```

---

## Phase 2 — Frontend Panel + Streaming

### Task 11: AgentPanel Component

**Files:**
- Create: `web/src/components/AgentPanel.tsx`
- Create: `web/src/types/agent.ts`
- Modify: `web/src/pages/NotebookPage.tsx`
- Test: Playwright snapshot test

**Step 1: Create web/src/types/agent.ts**

```typescript
export interface Agent {
  id: string
  org_id: string
  name: string
  description?: string
  model_config_id?: string
  subagent_model_config_id?: string
  system_prompt?: string
  skill_ids: string[]
  mcp_servers: MCPServer[]
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
}

export interface MCPServer {
  name: string
  type: 'stdio' | 'http'
  command: string
  args?: string[]
}

export interface AgentSession {
  id: string
  agent_id: string
  notebook_id: string
  user_id: string
  max_turns: number
  max_tokens: number
  ended_at?: string
  created_at: string
}

export interface AgentMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'tool'
  content?: string
  tool_call_id?: string
  tool_calls?: ToolCall[]
  tokens_input?: number
  tokens_output?: number
  created_at: string
}

export interface ToolCall {
  id: string
  name: string
  arguments: Record<string, any>
  result?: any
  error?: string
  duration_ms?: number
}

export interface SubagentTask {
  id: string
  goal: string
  status: 'queued' | 'running' | 'completed' | 'failed'
  result?: any
}

export type WSMessage =
  | { type: 'token'; data: string }
  | { type: 'tool_call'; tool: string; args: any; result: any }
  | { type: 'cell_created'; cell_id: string; position: number }
  | { type: 'subagent_progress'; tasks: SubagentTask[] }
  | { type: 'done'; tokens_used: number }
  | { type: 'error'; message: string }
  | { type: 'slash_result'; command: string; data: any }
  | { type: 'backpressure_warning'; dropped_tokens: number }
  | { type: 'reconnect_sync'; messages: AgentMessage[] }
```

**Step 2: Create AgentPanel.tsx**

```typescript
import React, { useState, useRef, useEffect } from 'react'
import { api } from '../api/client'
import type { Agent, AgentMessage, WSMessage, ToolCall } from '../types/agent'

interface AgentPanelProps {
  notebookId: string
  onCellCreated?: (cellId: string, position: number) => void
  onCellScrollTo?: (cellId: string) => void
}

const styles: Record<string, React.CSSProperties> = {
  panel: {
    position: 'fixed',
    right: 0,
    top: 48,
    bottom: 0,
    width: 360,
    background: 'var(--bg-secondary)',
    borderLeft: '1px solid var(--border)',
    display: 'flex',
    flexDirection: 'column',
    zIndex: 50,
  },
  header: {
    padding: '12px 16px',
    borderBottom: '1px solid var(--border)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  agentSelect: {
    padding: '8px 12px',
    background: 'var(--bg-tertiary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    color: 'var(--text-primary)',
    fontSize: 14,
    cursor: 'pointer',
    width: '100%',
  },
  messageList: {
    flex: 1,
    overflow: 'auto',
    padding: '16px',
    display: 'flex',
    flexDirection: 'column',
    gap: 12,
  },
  message: {
    padding: '10px 14px',
    borderRadius: 8,
    fontSize: 14,
    lineHeight: 1.5,
    maxWidth: '85%',
  },
  userMessage: {
    background: 'var(--accent)',
    color: 'white',
    alignSelf: 'flex-end',
  },
  assistantMessage: {
    background: 'var(--bg-tertiary)',
    color: 'var(--text-primary)',
    alignSelf: 'flex-start',
  },
  toolCallBlock: {
    background: 'var(--bg-tertiary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    padding: '8px 12px',
    marginTop: 6,
    fontSize: 12,
    fontFamily: 'monospace',
  },
  inputArea: {
    padding: '12px 16px',
    borderTop: '1px solid var(--border)',
    display: 'flex',
    gap: 8,
  },
  input: {
    flex: 1,
    padding: '10px 12px',
    background: 'var(--bg-tertiary)',
    border: '1px solid var(--border)',
    borderRadius: 6,
    color: 'var(--text-primary)',
    fontSize: 14,
    resize: 'none' as const,
    minHeight: 40,
    maxHeight: 120,
  },
  sendButton: {
    padding: '10px 16px',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    borderRadius: 6,
    fontSize: 14,
    fontWeight: 500,
    cursor: 'pointer',
  },
  streamingDot: {
    display: 'inline-block',
    width: 6,
    height: 6,
    borderRadius: '50%',
    background: 'var(--accent)',
    marginLeft: 8,
    animation: 'pulse 1s infinite',
  },
}

export const AgentPanel: React.FC<AgentPanelProps> = ({ notebookId, onCellCreated, onCellScrollTo }) => {
  const [isOpen, setIsOpen] = useState(false)
  const [agents, setAgents] = useState<Agent[]>([])
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null)
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [messages, setMessages] = useState<Array<{role: string; content: string}>>([])
  const [input, setInput] = useState('')
  const [isStreaming, setIsStreaming] = useState(false)
  const [currentStreamingText, setCurrentStreamingText] = useState('')
  const wsRef = useRef<WebSocket | null>(null)
  const messageListRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (isOpen && !agents.length) {
      api.get<Agent[]>('/api/v1/agents').then(setAgents).catch(console.error)
    }
  }, [isOpen])

  const startSession = async (agent: Agent) => {
    const res = await api.post<{session_id: string}>('/api/v1/agents/' + agent.id + '/session', { notebook_id: notebookId })
    setSessionId(res.session_id)
    setSelectedAgent(agent)
    connectWebSocket(res.session_id)
  }

  const connectWebSocket = (sid: string) => {
    const token = localStorage.getItem('hnb_token')
    const ws = new WebSocket('ws://localhost:8080/api/v1/ws/agents/' + sid + '?token=' + token)
    wsRef.current = ws

    ws.onmessage = (event) => {
      const msg: WSMessage = JSON.parse(event.data)

      switch (msg.type) {
        case 'token':
          setCurrentStreamingText(prev => prev + msg.data)
          break
        case 'tool_call':
          break
        case 'cell_created':
          onCellCreated?.(msg.cell_id, msg.position)
          break
        case 'done':
          if (currentStreamingText) {
            setMessages(prev => [...prev, { role: 'assistant', content: currentStreamingText }])
            setCurrentStreamingText('')
          }
          setIsStreaming(false)
          break
        case 'error':
          setMessages(prev => [...prev, { role: 'assistant', content: 'Error: ' + msg.message }])
          setIsStreaming(false)
          break
        case 'slash_result':
          if (msg.command === 'new') {
            setMessages([])
            setSessionId(null)
            setSelectedAgent(null)
          }
          break
      }
    }

    ws.onclose = () => {
      wsRef.current = null
    }
  }

  const sendMessage = () => {
    if (!input.trim() || isStreaming) return

    if (input.startsWith('/')) {
      const command = input.slice(1)
      wsRef.current?.send(JSON.stringify({ type: 'slash_command', command }))
      setInput('')
      return
    }

    setMessages(prev => [...prev, { role: 'user', content: input }])
    wsRef.current?.send(JSON.stringify({ type: 'message', content: input }))
    setInput('')
    setIsStreaming(true)
    setCurrentStreamingText('')
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  if (!isOpen) {
    return (
      <button
        onClick={() => setIsOpen(true)}
        style={{
          position: 'fixed',
          right: 0,
          top: '50%',
          transform: 'translateY(-50%)',
          width: 28,
          height: 56,
          background: 'var(--accent)',
          color: 'white',
          border: 'none',
          borderRadius: '8px 0 0 8px',
          cursor: 'pointer',
          fontSize: 12,
          fontWeight: 600,
          writingMode: 'vertical-rl',
          textAlign: 'center',
          zIndex: 50,
        }}
      >
        AI
      </button>
    )
  }

  return (
    <div style={styles.panel}>
      <div style={styles.header}>
        <select
          style={styles.agentSelect}
          value={selectedAgent?.id || ''}
          onChange={(e) => {
            const agent = agents.find(a => a.id === e.target.value)
            if (agent) startSession(agent)
          }}
        >
          <option value="">Select an agent...</option>
          {agents.map(a => (
            <option key={a.id} value={a.id}>{a.name}</option>
          ))}
        </select>
        <button
          onClick={() => setIsOpen(false)}
          style={{ background: 'none', border: 'none', color: 'var(--text-secondary)', cursor: 'pointer', fontSize: 18 }}
        >
          ×
        </button>
      </div>

      <div ref={messageListRef} style={styles.messageList}>
        {messages.map((msg, i) => (
          <div
            key={i}
            style={{
              ...styles.message,
              ...(msg.role === 'user' ? styles.userMessage : styles.assistantMessage),
            }}
          >
            {msg.content}
          </div>
        ))}
        {isStreaming && currentStreamingText && (
          <div style={{ ...styles.message, ...styles.assistantMessage }}>
            {currentStreamingText}
            <span style={styles.streamingDot} />
          </div>
        )}
      </div>

      <div style={styles.inputArea}>
        <textarea
          style={styles.input}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Message agent... (/ for commands)"
        />
        <button style={styles.sendButton} onClick={sendMessage} disabled={isStreaming}>
          Send
        </button>
      </div>
    </div>
  )
}
```

**Step 3: Modify NotebookPage.tsx to add AgentPanel**

Read `web/src/pages/NotebookPage.tsx` first (find imports and render), then add:

```typescript
import { AgentPanel } from '../components/AgentPanel'

// Add to NotebookPage component:
const [showAgent, setShowAgent] = useState(false)

// Add to toolbar buttons (around line where history button is):
<button onClick={() => setShowAgent(!showAgent)}>AI</button>

// Add before closing container:
{showAgent && <AgentPanel notebookId={notebook.id} />}

// Add CSS keyframe for pulse animation in the component or global CSS
```

**Step 4: Create Playwright test**

Create: `web/tests/agent-panel.spec.ts`

```typescript
import { test, expect } from '@playwright/test'

test('agent panel opens and shows agent selector', async ({ page }) => {
  await page.goto('/notebooks/test-notebook')
  await page.click('button:has-text("AI")')
  await expect(page.locator('.agent-panel')).toBeVisible()
  await expect(page.locator('select')).toBeVisible()
})
```

**Step 5: Run Playwright test**

Run: `npx playwright test web/tests/agent-panel.spec.ts --update-snapshots`
Expected: PASS

**Step 6: Commit**

```bash
git add web/src/components/AgentPanel.tsx web/src/types/agent.ts web/tests/agent-panel.spec.ts
git add web/src/pages/NotebookPage.tsx
git commit -m "feat(agent): add AgentPanel component with WebSocket streaming"
```

---

### Task 12: Slash Commands (/summarize, /new, /skills, /agents)

**Files:**
- Modify: `web/src/components/AgentPanel.tsx`
- Modify: `internal/agent/engine.go` (add slash command handler)

**Step 1: Add slash command UI in AgentPanel**

The slash command handling is already in AgentPanel.tsx above. The server side in `agent_ws.go` sends `slash_result` messages.

**Step 2: Add server-side slash command handler in engine.go**

```go
func (e *Engine) HandleSlashCommand(ctx context.Context, sessionID string, command string) (string, any, error) {
    switch command {
    case "skills":
        skills, err := e.listSkills(ctx)
        return "slash_result", map[string]any{"skills": skills}, err
    case "agents":
        agents, err := e.listAgents(ctx)
        return "slash_result", map[string]any{"agents": agents}, err
    case "new":
        return "slash_result", map[string]any{"session_id": sessionID}, nil
    case "summarize":
        summary, err := e.summarizeSession(ctx, sessionID)
        return "slash_result", map[string]any{"summary": summary}, err
    default:
        return "error", map[string]string{"message": "unknown command"}, nil
    }
}
```

This requires implementing `listSkills`, `listAgents`, `summarizeSession` in `engine.go`.

**Step 3: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 4: Commit**

```bash
git add internal/agent/engine.go
git commit -m "feat(agent): add slash command handlers"
```

---

## Phase 3 — MCP + Subagents

### Task 13: MCP Adapter

**Files:**
- Create: `internal/agent/tools_mcp.go`
- Modify: `internal/agent/registry.go` (MCP tool registration)
- Test: integration test with MCP server

**Step 1: Create tools_mcp.go**

```go
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

type MCPClient struct {
    name     string
    cmd      string
    args     []string
    env      map[string]string
    stdio    *io.ReadCloser
    httpURL  string
}

type MCPToolHandler struct {
    server   *MCPClient
    toolName string
}

func (s *Server) setupMCPServers(agent *models.Agent) error {
    for _, srv := range agent.MCPServers {
        client := &MCPClient{
            name:    srv.Name,
            cmd:     srv.Command,
            args:    srv.Args,
            env:     make(map[string]string),
            httpURL: "",
        }

        if srv.Type == "stdio" {
            proc := exec.CommandContext(context.Background(), srv.Command, srv.Args...)
            proc.Env = os.Environ()
            stdin, _ := proc.StdinPipe()
            stdout, _ := proc.StdoutPipe()
            client.stdio = &stdout
            proc.Start()
        } else if srv.Type == "http" {
            client.httpURL = srv.Command
        }

        s.mcpClients = append(s.mcpClients, client)
    }
    return nil
}

func RegisterMCPTools(reg *ToolRegistry, servers []*MCPClient) {
    for _, srv := range servers {
        if srv.httpURL != "" {
            reg.Register(&ToolDef{
                Name:        srv.name + "_list_tools",
                Description: fmt.Sprintf("List available tools from MCP server %s", srv.name),
                Parameters:  "{}",
                Handler:     makeMCPToolListHandler(srv),
            })
        }
    }
}

func makeMCPToolListHandler(srv *MCPClient) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        resp, err := http.Get(srv.httpURL + "/tools/list")
        if err != nil {
            return nil, fmt.Errorf("mcp list tools: %w", err)
        }
        defer resp.Body.Close()

        var result map[string]any
        json.NewDecoder(resp.Body).Decode(&result)
        return result, nil
    }
}
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/agent/tools_mcp.go
git commit -m "feat(agent): add MCP adapter for external tool servers"
```

---

### Task 14: Subagent System

**Files:**
- Create: `internal/agent/subagent.go`
- Create: `internal/agent/tools_agent.go` (spawn_subagents, update_agent, create_skill)
- Modify: `internal/agent/registry.go`
- Test: unit test

**Step 1: Create subagent.go**

```go
package agent

import (
    "context"
    "fmt"
    "sync"
)

const MaxSubagentParallelism = 3
const MaxSubagentTurns = 20

type SubagentResult struct {
    TaskID  string
    Status  string
    Result  any
    Error   string
    TokensIn  int
    TokensOut int
}

func (e *Engine) SpawnSubagents(ctx context.Context, parentSessionID string, tasks []SubagentTaskConfig, onUpdate func([]SubagentResult)) error {
    sem := make(chan struct{}, MaxSubagentParallelism)
    var wg sync.WaitGroup
    results := make([]SubagentResult, len(tasks))

    for i, task := range tasks {
        sem <- struct{}{}
        wg.Add(1)

        go func(i int, task SubagentTaskConfig) {
            defer wg.Done()
            defer func() { <-sem }()

            result := e.runSubagent(ctx, parentSessionID, task)
            results[i] = result
            onUpdate(results)
        }(i, task)
    }

    wg.Wait()
    return nil
}

type SubagentTaskConfig struct {
    ID      string
    Goal    string
    Context map[string]any
    AgentID *string
}

func (e *Engine) runSubagent(ctx context.Context, parentSessionID string, task SubagentTaskConfig) SubagentResult {
    taskID := task.ID

    _, err := e.pool.Exec(ctx, `
        INSERT INTO subagent_tasks (id, parent_session_id, goal, context, status, created_at)
        VALUES ($1, $2, $3, $4, 'running', NOW())
    `, taskID, parentSessionID, task.Goal, task.Context)
    if err != nil {
        return SubagentResult{TaskID: taskID, Status: "failed", Error: err.Error()}
    }

    messages := []ChatMessage{
        {Role: "user", Content: task.Goal},
    }

    for turn := 0; turn < MaxSubagentTurns; turn++ {
        resp, err := e.llm.Chat(ctx, messages, e.registry.List())
        if err != nil {
            return SubagentResult{TaskID: taskID, Status: "failed", Error: err.Error()}
        }

        choice := resp.Choices[0]

        if choice.Message.Content != "" {
            messages = append(messages, ChatMessage{Role: "assistant", Content: choice.Message.Content})
            result := SubagentResult{
                TaskID: taskID,
                Status: "completed",
                Result: choice.Message.Content,
                TokensIn: resp.Usage.PromptTokens,
                TokensOut: resp.Usage.CompletionTokens,
            }

            _, _ = e.pool.Exec(ctx, `
                UPDATE subagent_tasks SET status = 'completed', result = $1, tokens_input = $2, tokens_output = $3, completed_at = NOW()
                WHERE id = $4
            `, choice.Message.Content, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, taskID)

            return result
        }

        for _, tc := range choice.ToolCalls {
            toolDef, ok := e.registry.Get(tc.Name)
            if !ok {
                continue
            }

            result, err := toolDef.Handler(json.RawMessage(tc.Name), &ToolContext{
                UserID:    "subagent",
                OrgID:     "subagent",
                NotebookID: taskID,
                SessionID:  parentSessionID,
                DB:        e.pool,
            })

            if err != nil {
                messages = append(messages, ChatMessage{Role: "tool", Content: err.Error()})
            } else {
                messages = append(messages, ChatMessage{Role: "tool", Content: fmt.Sprintf("%v", result)})
            }
        }
    }

    return SubagentResult{TaskID: taskID, Status: "completed", Result: "max turns reached"}
}
```

**Step 2: Create tools_agent.go**

```go
package agent

import (
    "encoding/json"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/jesus/hnb-claude/internal/models"
)

func RegisterAgentTools(reg *ToolRegistry, pool *pgxpool.Pool) {
    reg.Register(&ToolDef{
        Name:        "update_agent",
        Description: "Modify this agent's own config",
        Parameters:  `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"system_prompt":{"type":"string"},"skill_ids":{"type":"array","items":{"type":"string"}}}}`,
        Handler:     makeUpdateAgentHandler(pool),
    })

    reg.Register(&ToolDef{
        Name:        "create_skill",
        Description: "Save a reusable skill",
        Parameters:  `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"system_prompt":{"type":"string"},"tool_ids":{"type":"array","items":{"type":"string"}}},"required":["name","system_prompt"]}`,
        Handler:     makeCreateSkillHandler(pool),
    })

    reg.Register(&ToolDef{
        Name:        "spawn_subagents",
        Description: "Fork parallel exploration tasks",
        Parameters:  `{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"goal":{"type":"string"},"context":{"type":"object"},"agent_id":{"type":"string"}}}}},"required":["tasks"]}`,
        Handler:     makeSpawnSubagentsHandler(pool),
    })

    reg.Register(&ToolDef{
        Name:        "update_skill",
        Description: "Modify a skill",
        Parameters:  `{"type":"object","properties":{"skill_id":{"type":"string"},"name":{"type":"string"},"system_prompt":{"type":"string"},"tool_ids":{"type":"array","items":{"type":"string"}}},"required":["skill_id"]}`,
        Handler:     makeUpdateSkillHandler(pool),
    })
}

func makeUpdateAgentHandler(pool *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            Name        *string  `json:"name"`
            Description *string  `json:"description"`
            SystemPrompt *string `json:"system_prompt"`
            SkillIDs    *[]string `json:"skill_ids"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        var agentID string
        err := pool.QueryRow(ctx, `
            SELECT agent_id FROM agent_sessions WHERE id = $1
        `, ctx.SessionID).Scan(&agentID)
        if err != nil {
            return nil, fmt.Errorf("get agent from session: %w", err)
        }

        _, err = pool.Exec(ctx, `
            UPDATE agents SET
                name = COALESCE($2, name),
                description = COALESCE($3, description),
                system_prompt = COALESCE($4, system_prompt),
                skill_ids = COALESCE($5, skill_ids),
                updated_at = NOW()
            WHERE id = $1
        `, agentID, req.Name, req.Description, req.SystemPrompt, req.SkillIDs)
        if err != nil {
            return nil, fmt.Errorf("update agent: %w", err)
        }

        var version int
        pool.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM agent_versions WHERE agent_id = $1`, agentID).Scan(&version)
        _, _ = pool.Exec(ctx, `
            INSERT INTO agent_versions (id, agent_id, version, name, description, system_prompt, skill_ids, changed_by, change_reason, created_at)
            SELECT $1, $2, $3, name, description, system_prompt, skill_ids, $4, 'agent_self_modification', NOW()
            FROM agents WHERE id = $1
        `, uuid.New().String(), agentID, version, ctx.UserID)

        return map[string]any{"agent_id": agentID, "status": "updated"}, nil
    }
}

func makeCreateSkillHandler(pool *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            Name        string   `json:"name"`
            Description string   `json:"description"`
            SystemPrompt string  `json:"system_prompt"`
            ToolIDs     []string `json:"tool_ids"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        skillID := uuid.New().String()
        _, err := pool.Exec(ctx, `
            INSERT INTO skills (id, org_id, name, description, system_prompt, tool_ids, created_by, created_at, updated_at)
            SELECT $1, org_id, $2, $3, $4, $5, $6, NOW(), NOW()
            FROM agents WHERE id = (SELECT agent_id FROM agent_sessions WHERE id = $7)
        `, skillID, req.Name, req.Description, req.SystemPrompt, req.ToolIDs, ctx.UserID, ctx.SessionID)
        if err != nil {
            return nil, fmt.Errorf("create skill: %w", err)
        }

        return map[string]any{"skill_id": skillID}, nil
    }
}

func makeSpawnSubagentsHandler(pool *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            Tasks []struct {
                ID       string `json:"id"`
                Goal     string `json:"goal"`
                Context  map[string]any `json:"context"`
                AgentID  *string `json:"agent_id"`
            } `json:"tasks"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        if len(req.Tasks) > 5 {
            return nil, fmt.Errorf("max 5 subagents per call")
        }

        taskIDs := make([]string, len(req.Tasks))
        for i, t := range req.Tasks {
            taskID := uuid.New().String()
            taskIDs[i] = taskID

            contextJSON, _ := json.Marshal(t.Context)
            _, err := pool.Exec(ctx, `
                INSERT INTO subagent_tasks (id, parent_session_id, goal, context, status, created_at)
                VALUES ($1, $2, $3, $4, 'queued', NOW())
            `, taskID, ctx.SessionID, t.Goal, contextJSON)
            if err != nil {
                return nil, fmt.Errorf("create subagent task: %w", err)
            }
        }

        return map[string]any{"task_ids": taskIDs, "status": "spawned"}, nil
    }
}

func makeUpdateSkillHandler(pool *pgxpool.Pool) ToolHandler {
    return func(args json.RawMessage, ctx *ToolContext) (any, error) {
        var req struct {
            SkillID     string   `json:"skill_id"`
            Name        *string  `json:"name"`
            SystemPrompt *string `json:"system_prompt"`
            ToolIDs     *[]string `json:"tool_ids"`
        }
        if err := json.Unmarshal(args, &req); err != nil {
            return nil, fmt.Errorf("invalid args: %w", err)
        }

        _, err := pool.Exec(ctx, `
            UPDATE skills SET
                name = COALESCE($2, name),
                system_prompt = COALESCE($3, system_prompt),
                tool_ids = COALESCE($4, tool_ids),
                updated_at = NOW()
            WHERE id = $1
        `, req.SkillID, req.Name, req.SystemPrompt, req.ToolIDs)
        if err != nil {
            return nil, fmt.Errorf("update skill: %w", err)
        }

        return map[string]any{"skill_id": req.SkillID}, nil
    }
}
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 4: Commit**

```bash
git add internal/agent/subagent.go internal/agent/tools_agent.go
git commit -m "feat(agent): add subagent system and self-improvement tools"
```

---

## Phase 4 — Context, Rate Limits, and Resilience

### Task 15: Context Window Manager

**Files:**
- Create: `internal/agent/context.go`
- Modify: `internal/agent/engine.go`

**Step 1: Create context.go**

```go
package agent

import (
    "context"
    "encoding/json"
    "fmt"
)

type ContextManager struct {
    contextWindow int
}

func NewContextManager(contextWindow int) *ContextManager {
    return &ContextManager{contextWindow: contextWindow}
}

func (cm *ContextManager) CountTokens(text string) int {
    return len(text) / 4
}

func (cm *ContextManager) NeedsSummarization(systemPrompt string, skillPrompts []string, messages []ChatMessage, contextWindow int) bool {
    totalTokens := cm.CountTokens(systemPrompt)
    for _, sp := range skillPrompts {
        totalTokens += cm.CountTokens(sp)
    }
    for _, m := range messages {
        totalTokens += cm.CountTokens(m.Content)
    }

    threshold := float64(contextWindow) * 0.8
    return float64(totalTokens) >= threshold
}

func (cm *ContextManager) BuildContext(systemPrompt string, skillPrompts []string, messages []ChatMessage) ([]ChatMessage, error) {
    allMessages := make([]ChatMessage, 0, len(messages)+2)
    allMessages = append(allMessages, ChatMessage{Role: "system", Content: systemPrompt})
    for _, sp := range skillPrompts {
        allMessages = append(allMessages, ChatMessage{Role: "system", Content: sp})
    }
    allMessages = append(allMessages, messages...)
    return allMessages, nil
}

func (cm *ContextManager) SummarizeMessages(ctx context.Context, llm *LLMClient, messages []ChatMessage) (string, error) {
    recentMessages := messages
    if len(messages) > 20 {
        recentMessages = messages[len(messages)-20:]
    }

    summarizePrompt := "Summarize the following conversation concisely, preserving key information, decisions, and context:\n\n"
    for _, m := range recentMessages {
        summarizePrompt += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
    }

    resp, err := llm.Chat(ctx, []ChatMessage{{Role: "user", Content: summarizePrompt}}, nil)
    if err != nil {
        return "", fmt.Errorf("summarize: %w", err)
    }

    if len(resp.Choices) > 0 {
        return resp.Choices[0].Message.Content, nil
    }
    return "", fmt.Errorf("no summary returned")
}
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/agent/context.go
git commit -m "feat(agent): add context window manager with summarization"
```

---

### Task 16: Rate Limiting + Auto-summarize

**Files:**
- Create: `internal/agent/ratelimit.go`
- Modify: `internal/agent/engine.go`

**Step 1: Create ratelimit.go**

```go
package agent

import (
    "context"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type RateLimiter struct {
    sessionStore *SessionStore
}

func NewRateLimiter(pool *pgxpool.Pool) *RateLimiter {
    return &RateLimiter{sessionStore: NewSessionStore(pool)}
}

func (rl *RateLimiter) CheckAndUpdateTokens(ctx context.Context, sessionID string, tokensIn, tokensOut int) (bool, error) {
    messageCount, err := rl.sessionStore.GetMessageCount(ctx, sessionID)
    if err != nil {
        return false, err
    }

    var maxTurns, maxTokens int
    err = rl.sessionStore.pool.QueryRow(ctx, `
        SELECT max_turns, max_tokens FROM agent_sessions WHERE id = $1
    `, sessionID).Scan(&maxTurns, &maxTokens)
    if err != nil {
        return false, err
    }

    if messageCount >= maxTurns {
        return false, nil
    }

    var totalTokens int
    rl.sessionStore.pool.QueryRow(ctx, `
        SELECT COALESCE(SUM(tokens_input + tokens_output), 0) FROM agent_messages WHERE session_id = $1
    `, sessionID).Scan(&totalTokens)

    if totalTokens >= maxTokens {
        return false, nil
    }

    return true, nil
}

func (rl *RateLimiter) CreateSummarizedSession(ctx context.Context, sessionID string, summary string) (string, error) {
    oldSession, err := rl.sessionStore.GetSession(ctx, sessionID)
    if err != nil {
        return "", err
    }

    newSession, err := rl.sessionStore.CreateSession(ctx, oldSession.AgentID, oldSession.NotebookID, oldSession.UserID, oldSession.MaxTurns, oldSession.MaxTokens)
    if err != nil {
        return "", err
    }

    _, err = rl.sessionStore.AppendMessage(ctx, &Message{
        SessionID: newSession.ID,
        Role:      "user",
        Content:   "Previous conversation summary: " + summary,
    })
    if err != nil {
        return "", err
    }

    rl.sessionStore.EndSession(ctx, sessionID)

    return newSession.ID, nil
}
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/agent/ratelimit.go
git commit -m "feat(agent): add rate limiting with auto-summarize"
```

---

### Task 17: Session Reconnection + Backpressure

**Files:**
- Modify: `internal/api/agent_ws.go`

**Step 1: Add reconnection support to agent_ws.go**

In `handleAgentWS`, add:
```go
// Handle reconnect
if msg.Type == "reconnect" {
    lastID := msg.last_message_id
    // Get messages since lastID
    rows, _ := s.server.db.Pool.Query(ctx, `
        SELECT id, role, content, tool_calls FROM agent_messages
        WHERE session_id = $1 AND id > $2 ORDER BY created_at
    `, sessionID, lastID)
    // Send reconnect_sync with messages
    conn.WriteJSON(map[string]any{
        "type": "reconnect_sync",
        "messages": rows,
    })
}
```

Add backpressure handling in engine streaming:
```go
select {
case writeChan <- token:
default:
    // Buffer full, drop token and warn
    select {
    case writeChan <- map[string]any{"type": "backpressure_warning", "dropped": 1}:
    default:
    }
}
```

**Step 2: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/api/agent_ws.go
git commit -m "feat(agent): add session reconnection and backpressure handling"
```

---

## Phase 5 — Self-Improvement + Metrics

### Task 18: Stats Rollup Cron Job

**Files:**
- Modify: `internal/scheduler/scheduler.go` (add stats rollup)
- Create: `internal/agent/stats.go`
- Test: `task test`

**Step 1: Create stats rollup**

```go
package agent

import (
    "context"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
)

type StatsAggregator struct {
    pool *pgxpool.Pool
}

func NewStatsAggregator(pool *pgxpool.Pool) *StatsAggregator {
    return &StatsAggregator{pool: pool}
}

func (sa *StatsAggregator) RollupDailyStats(ctx context.Context) error {
    yesterday := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)

    _, err := sa.pool.Exec(ctx, `
        INSERT INTO agent_stats_daily (date, agent_id, user_id, sessions_count, messages_count, tokens_input, tokens_output)
        SELECT
            $1 as date,
            s.agent_id,
            s.user_id,
            COUNT(DISTINCT s.id) as sessions_count,
            COUNT(m.id) as messages_count,
            COALESCE(SUM(m.tokens_input), 0) as tokens_input,
            COALESCE(SUM(m.tokens_output), 0) as tokens_output
        FROM agent_sessions s
        LEFT JOIN agent_messages m ON m.session_id = s.id
        WHERE s.created_at::date = $1::date
        GROUP BY s.agent_id, s.user_id
        ON CONFLICT (date, agent_id, user_id) DO UPDATE SET
            sessions_count = EXCLUDED.sessions_count,
            messages_count = EXCLUDED.messages_count,
            tokens_input = EXCLUDED.tokens_input,
            tokens_output = EXCLUDED.tokens_output
    `, yesterday)
    return err
}
```

**Step 2: Modify scheduler to run stats rollup daily**

In `scheduler.go`, add tick that runs at midnight:
```go
func (s *Scheduler) tick() {
    // existing schedule tick
    s.runAgentStatsRollup()
}

func (s *Scheduler) runAgentStatsRollup() {
    ctx := context.Background()
    stats := agent.NewStatsAggregator(s.db.Pool)
    stats.RollupDailyStats(ctx)
}
```

**Step 3: Verify build**

Run: `go build ./... && task test`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/agent/stats.go internal/scheduler/scheduler.go
git commit -m "feat(agent): add daily stats rollup for agent usage"
```

---

### Task 19: Admin Metrics API

**Files:**
- Modify: `internal/api/agent_handlers.go` (add stats endpoints)
- Modify: `internal/api/router.go`

**Step 1: Add stats handlers to agent_handlers.go**

```go
func (h *agentHandlers) handleAgentStats(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())

    rows, err := h.server.db.Pool.Query(r.Context(), `
        SELECT date, agent_id, user_id, sessions_count, messages_count, tokens_input, tokens_output
        FROM agent_stats_daily
        WHERE date >= NOW() - INTERVAL '30 days'
        ORDER BY date DESC
    `)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    defer rows.Close()

    var stats []models.AgentStatsDaily
    for rows.Next() {
        var s models.AgentStatsDaily
        rows.Scan(&s.Date, &s.AgentID, &s.UserID, &s.SessionsCount, &s.MessagesCount, &s.TokensInput, &s.TokensOutput)
        stats = append(stats, s)
    }

    writeJSON(w, http.StatusOK, stats)
}

func (h *agentHandlers) handleAgentStatsByAgent(w http.ResponseWriter, r *http.Request) {
    agentID := r.PathValue("id")

    rows, err := h.server.db.Pool.Query(r.Context(), `
        SELECT date, agent_id, user_id, sessions_count, messages_count, tokens_input, tokens_output
        FROM agent_stats_daily
        WHERE agent_id = $1 AND date >= NOW() - INTERVAL '30 days'
        ORDER BY date DESC
    `, agentID)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    defer rows.Close()

    var stats []models.AgentStatsDaily
    for rows.Next() {
        var s models.AgentStatsDaily
        rows.Scan(&s.Date, &s.AgentID, &s.UserID, &s.SessionsCount, &s.MessagesCount, &s.TokensInput, &s.TokensOutput)
        stats = append(stats, s)
    }

    writeJSON(w, http.StatusOK, stats)
}
```

**Step 2: Register stats routes in router.go**

```go
s.mux.Handle("GET /api/v1/agents/stats", authMW(RequireRole("admin")(http.HandlerFunc(ah.handleAgentStats))))
s.mux.Handle("GET /api/v1/agents/{id}/stats", authMW(RequireRole("admin")(http.HandlerFunc(ah.handleAgentStatsByAgent))))
```

**Step 3: Verify build**

Run: `go build ./...`
Expected: no errors

**Step 4: Commit**

```bash
git add internal/api/agent_handlers.go internal/api/router.go
git commit -m "feat(agent): add admin stats endpoints"
```

---

## Summary

**Total Tasks:** 19
**Estimated Commits:** 19

**Order:** Execute Phase 1 first (Tasks 1-10), then Phase 2 (Tasks 11-12), Phase 3 (Tasks 13-14), Phase 4 (Tasks 15-17), Phase 5 (Tasks 18-19).

**Verification Commands:**
- Build: `go build ./...`
- Tests: `task test`
- Migrations: `task infra:reset && task db:psql`
- Frontend: `npx playwright test web/tests/agent-panel.spec.ts`

---

**Plan complete and saved to `docs/plans/2026-05-27-hnb-agent-system.md`. Two execution options:**

**1. Subagent-Driven (this session)** — I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** — Open new session with `superpowers:executing-plans`, batch execution with checkpoints

Which approach?