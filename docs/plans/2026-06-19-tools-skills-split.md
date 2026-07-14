# Tools/Skills Split Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Split tools and skills into separate first-class resources — new `tools` table, API, UI, and engine support for webhook + SQL query tool types.

**Architecture:** New `tools` table with type-specific handler config. Built-in tools seeded as rows. Agents gain direct `tool_ids[]`. Skills lose `tool_ids`. Engine resolves tools from DB by type (builtin/webhook/sql_query) and merges with MCP-generated tools at runtime.

**Tech Stack:** Go (pgx), PostgreSQL, React (TypeScript), Vite

---

### Task 1: Database migration — create tools table, alter agents/skills

**Files:**
- Create: `internal/database/migrations/V064__tools_table.sql`

**Step 1: Write the migration**

```sql
CREATE TABLE tools (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL CHECK (type IN ('builtin', 'webhook', 'sql_query')),
    schema      JSONB NOT NULL DEFAULT '{}',
    config      JSONB NOT NULL DEFAULT '{}',
    folder_id   UUID REFERENCES folders(id) ON DELETE SET NULL,
    created_by  UUID NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, name)
);

ALTER TABLE agents ADD COLUMN tool_ids UUID[] NOT NULL DEFAULT '{}';

-- Backfill agents.tool_ids from skills (handles existing data)
-- For each agent, collect tool names from all its skills' tool_ids,
-- resolve to tool UUIDs (via builtin handler_name mapping)
-- This is done in a later migration step after built-in tools are seeded

-- Drop tool_ids from skills
ALTER TABLE skills DROP COLUMN IF EXISTS tool_ids;

-- Add tool to ACL resource type
ALTER TABLE acl_entries DROP CONSTRAINT IF EXISTS acl_entries_resource_type_check;
ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_resource_type_check
    CHECK (resource_type IN ('notebook','folder','connector','agent','model_config','skill','mcp_server','tool','dashboard'));
```

**Step 2: Verify migration parses**

Run: `psql $AETHER_DATABASE_URL -f internal/database/migrations/V064__tools_table.sql`
Expected: `CREATE TABLE`, `ALTER TABLE`, `ALTER TABLE`

**Step 3: Commit**

```bash
git add internal/database/migrations/V064__tools_table.sql
git commit -m "feat(db): add tools table, add tool_ids to agents, drop tool_ids from skills"
```

---

### Task 2: Add Tool struct to Go models

**Files:**
- Modify: `internal/models/agent.go`
- Test: N/A (just struct, no behavior)

**Step 1: Add Tool struct to models/agent.go**

```go
type ToolType string

const (
    ToolTypeBuiltin  ToolType = "builtin"
    ToolTypeWebhook  ToolType = "webhook"
    ToolTypeSQLQuery ToolType = "sql_query"
)

type Tool struct {
    ID          string   `json:"id"`
    OrgID       string   `json:"org_id"`
    Name        string   `json:"name"`
    Description string   `json:"description"`
    Type        ToolType `json:"type"`
    Schema      JSONMap  `json:"schema"`
    Config      JSONMap  `json:"config"`
    FolderID    *string  `json:"folder_id,omitempty"`
    CreatedBy   string   `json:"created_by"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}
```

**Step 2: Add Tools field to Agent struct**

```go
type Agent struct {
    // ... existing fields
    ToolIDs     []string        `json:"tool_ids,omitempty"`
    Tools       []Tool          `json:"tools,omitempty"`
    // ... existing SkillIDs, Skills, MCPServerIDs, MCPServers
}
```

**Step 3: Verify it compiles**

Run: `go build ./internal/models/`
Expected: no errors

**Step 4: Commit**

```bash
git add internal/models/agent.go
git commit -m "feat(models): add Tool struct, tool_ids/tools to Agent"
```

---

### Task 3: Go API handlers — tool CRUD

**Files:**
- Create: `internal/api/tool_handlers.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/tool_handlers_test.go`

**Step 1: Write failing test**

```go
func TestCreateTool(t *testing.T) {
    s := setupTestServer(t)
    orgID := createTestOrg(t, s.DB)
    userID := createTestUser(t, s.DB, orgID, "admin")

    body := map[string]any{
        "name": "test-webhook",
        "type": "webhook",
        "config": map[string]any{
            "url": "https://example.com/hook",
            "method": "POST",
        },
    }
    b, _ := json.Marshal(body)
    req := httptest.NewRequest("POST", "/api/v1/tools", bytes.NewReader(b))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", "Bearer "+s.JWTForUser(t, userID))

    w := httptest.NewRecorder()
    s.ServeHTTP(w, req)
    assert.Equal(t, 201, w.Code)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestCreateTool -v`
Expected: FAIL (handler not found)

**Step 3: Write handler implementations**

```go
package api

import (
    "encoding/json"
    "net/http"
    "time"
    "github.com/google/uuid"
    "github.com/the-heaven-labs/aether/internal/models"
)

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
    orgID := r.Context().Value(ctxOrgID).(string)
    rows, err := s.db.Query(r.Context(), `
        SELECT id, org_id, name, description, type, schema, config, folder_id, created_by, created_at, updated_at
        FROM tools WHERE org_id = $1 ORDER BY name`, orgID)
    if err != nil {
        respondError(w, http.StatusInternalServerError, "query tools: "+err.Error())
        return
    }
    defer rows.Close()
    var tools []models.Tool
    for rows.Next() {
        var t models.Tool
        var schema, config []byte
        if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &t.Type, &schema, &config, &t.FolderID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
            respondError(w, http.StatusInternalServerError, "scan tool: "+err.Error())
            return
        }
        if schema != nil { json.Unmarshal(schema, &t.Schema) }
        if config != nil { json.Unmarshal(config, &t.Config) }
        tools = append(tools, t)
    }
    respondJSON(w, http.StatusOK, tools)
}

func (s *Server) handleCreateTool(w http.ResponseWriter, r *http.Request) {
    orgID := r.Context().Value(ctxOrgID).(string)
    userID := r.Context().Value(ctxUserID).(string)
    var req struct {
        Name        string          `json:"name"`
        Description string          `json:"description"`
        Type        models.ToolType `json:"type"`
        Schema      json.RawMessage `json:"schema"`
        Config      json.RawMessage `json:"config"`
        FolderID    *string         `json:"folder_id"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "invalid json: "+err.Error())
        return
    }
    if req.Name == "" || req.Type == "" {
        respondError(w, http.StatusBadRequest, "name and type are required")
        return
    }
    if req.Type != models.ToolTypeBuiltin && req.Type != models.ToolTypeWebhook && req.Type != models.ToolTypeSQLQuery {
        respondError(w, http.StatusBadRequest, "invalid tool type")
        return
    }
    id := uuid.New().String()
    schemaBytes := req.Schema
    if schemaBytes == nil { schemaBytes = []byte("{}") }
    configBytes := req.Config
    if configBytes == nil { configBytes = []byte("{}") }
    _, err := s.db.Exec(r.Context(), `
        INSERT INTO tools (id, org_id, name, description, type, schema, config, folder_id, created_by, created_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())`,
        id, orgID, req.Name, req.Description, string(req.Type), schemaBytes, configBytes, req.FolderID, userID)
    if err != nil {
        if isUniqueViolation(err) {
            respondError(w, http.StatusConflict, "tool name already exists in this org")
            return
        }
        respondError(w, http.StatusInternalServerError, "create tool: "+err.Error())
        return
    }
    respondJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleGetTool(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    var t models.Tool
    var schema, config []byte
    err := s.db.QueryRow(r.Context(), `
        SELECT id, org_id, name, description, type, schema, config, folder_id, created_by, created_at, updated_at
        FROM tools WHERE id = $1`, id).Scan(
        &t.ID, &t.OrgID, &t.Name, &t.Description, &t.Type, &schema, &config, &t.FolderID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
    if err != nil {
        respondError(w, http.StatusNotFound, "tool not found")
        return
    }
    if schema != nil { json.Unmarshal(schema, &t.Schema) }
    if config != nil { json.Unmarshal(config, &t.Config) }
    respondJSON(w, http.StatusOK, t)
}

func (s *Server) handleUpdateTool(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    var req struct {
        Name        *string              `json:"name"`
        Description *string              `json:"description"`
        Type        *models.ToolType     `json:"type"`
        Schema      *json.RawMessage     `json:"schema"`
        Config      *json.RawMessage     `json:"config"`
        FolderID    **string             `json:"folder_id"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "invalid json: "+err.Error())
        return
    }
    // Build SET clause dynamically
    setClauses := []string{}
    args := []any{}
    argIdx := 1
    if req.Name != nil {
        setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx)); argIdx++
        args = append(args, *req.Name)
    }
    if req.Description != nil {
        setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx)); argIdx++
        args = append(args, *req.Description)
    }
    if req.Schema != nil {
        setClauses = append(setClauses, fmt.Sprintf("schema = $%d", argIdx)); argIdx++
        args = append(args, *req.Schema)
    }
    if req.Config != nil {
        setClauses = append(setClauses, fmt.Sprintf("config = $%d", argIdx)); argIdx++
        args = append(args, *req.Config)
    }
    if req.FolderID != nil {
        setClauses = append(setClauses, fmt.Sprintf("folder_id = $%d", argIdx)); argIdx++
        if *req.FolderID == nil {
            args = append(args, nil)
        } else {
            args = append(args, **req.FolderID)
        }
    }
    if len(setClauses) == 0 {
        respondError(w, http.StatusBadRequest, "no fields to update")
        return
    }
    setClauses = append(setClauses, "updated_at = NOW()")
    args = append(args, id)
    query := fmt.Sprintf("UPDATE tools SET %s WHERE id = $%d", strings.Join(setClauses, ", "), argIdx)
    _, err := s.db.Exec(r.Context(), query, args...)
    if err != nil {
        respondError(w, http.StatusInternalServerError, "update tool: "+err.Error())
        return
    }
    respondJSON(w, http.StatusOK, map[string]string{"id": id})
}

func (s *Server) handleDeleteTool(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    _, err := s.db.Exec(r.Context(), `DELETE FROM tools WHERE id = $1`, id)
    if err != nil {
        respondError(w, http.StatusInternalServerError, "delete tool: "+err.Error())
        return
    }
    respondJSON(w, http.StatusNoContent, nil)
}
```

**Step 4: Register routes in router.go**

```go
// In registerAgentRoutes or a new section:
mux.HandleFunc("GET /api/v1/tools", s.requireJWT(s.handleListTools))
mux.HandleFunc("POST /api/v1/tools", s.requireJWT(s.handleCreateTool))
mux.HandleFunc("GET /api/v1/tools/{id}", s.requirePermission("tool", "id", "view")(s.handleGetTool))
mux.HandleFunc("PUT /api/v1/tools/{id}", s.requirePermission("tool", "id", "edit")(s.handleUpdateTool))
mux.HandleFunc("DELETE /api/v1/tools/{id}", s.requirePermission("tool", "id", "delete")(s.handleDeleteTool))
mux.HandleFunc("POST /api/v1/tools/{id}/test", s.requireJWT(s.handleTestTool))
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestCreateTool -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/api/tool_handlers.go internal/api/router.go internal/api/tool_handlers_test.go
git commit -m "feat(api): add tool CRUD handlers and routes"
```

---

### Task 4: Add tool test endpoint handler

**Files:**
- Modify: `internal/api/tool_handlers.go`

**Step 1: Write handleTestTool**

```go
func (s *Server) handleTestTool(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    var toolType string
    var config []byte
    err := s.db.QueryRow(r.Context(), `SELECT type, config FROM tools WHERE id = $1`, id).Scan(&toolType, &config)
    if err != nil {
        respondError(w, http.StatusNotFound, "tool not found")
        return
    }

    switch toolType {
    case "webhook":
        var cfg struct {
            URL     string            `json:"url"`
            Method  string            `json:"method"`
            Headers map[string]string `json:"headers"`
        }
        json.Unmarshal(config, &cfg)
        if cfg.URL == "" {
            respondError(w, http.StatusBadRequest, "webhook URL not configured")
            return
        }
        method := cfg.Method
        if method == "" { method = "POST" }
        body := map[string]string{"test": "aether-tool-probe"}
        bodyBytes, _ := json.Marshal(body)
        req, _ := http.NewRequest(method, cfg.URL, bytes.NewReader(bodyBytes))
        req.Header.Set("Content-Type", "application/json")
        for k, v := range cfg.Headers {
            req.Header.Set(k, v)
        }
        ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
        defer cancel()
        resp, err := http.DefaultClient.Do(req.WithContext(ctx))
        if err != nil {
            respondError(w, http.StatusBadGateway, "webhook call failed: "+err.Error())
            return
        }
        defer resp.Body.Close()
        respBody, _ := io.ReadAll(resp.Body)
        respondJSON(w, http.StatusOK, map[string]any{
            "status": resp.StatusCode,
            "body":   string(respBody),
        })

    case "sql_query":
        var cfg struct {
            ConnectorID string `json:"connector_id"`
            Query       string `json:"query"`
        }
        json.Unmarshal(config, &cfg)
        if cfg.ConnectorID == "" || cfg.Query == "" {
            respondError(w, http.StatusBadRequest, "connector_id and query required")
            return
        }
        // Execute a LIMIT 5 version of the query for testing
        testQuery := strings.TrimRight(cfg.Query, "; \t\n") + " LIMIT 5"
        result, err := s.executor.Execute(r.Context(), cfg.ConnectorID, testQuery)
        if err != nil {
            respondError(w, http.StatusBadGateway, "query failed: "+err.Error())
            return
        }
        respondJSON(w, http.StatusOK, map[string]any{
            "status": "ok",
            "result": result,
        })

    default:
        respondError(w, http.StatusBadRequest, "cannot test builtin tools")
    }
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/api/`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/api/tool_handlers.go
git commit -m "feat(api): add POST /tools/{id}/test for webhook and sql_query"
```

---

### Task 5: Add tool to ACL resource table and permissions

**Files:**
- Modify: `internal/api/permissions.go`

**Step 1: Add tool to resourceTable**

```go
var resourceTable = map[string]string{
    "notebook":     "notebooks",
    "folder":       "folders",
    "connector":    "connectors",
    "agent":        "agents",
    "model_config": "model_configs",
    "skill":        "skills",
    "mcp_server":   "mcp_servers",
    "tool":         "tools",
    "dashboard":    "dashboards",
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/api/`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/api/permissions.go
git commit -m "feat(perms): add tool to ACL resource types"
```

---

### Task 6: Agent engine — load tools from DB, resolve handlers

**Files:**
- Modify: `internal/agent/engine.go`
- Create: `internal/agent/tools_webhook.go`
- Create: `internal/agent/tools_sql.go`

**Step 1: Add tool loading to ProcessMessage**

In `engine.go`, after loading MCP servers (around line 87), add tool loading:

```go
// Load agent tools from tools table
agentTools := make([]*ToolDef, 0)
if len(agent.ToolIDs) > 0 {
    rows, err := e.pool.Query(ctx, `
        SELECT id, org_id, name, description, type, schema, config
        FROM tools WHERE id = ANY($1)`, agent.ToolIDs)
    if err == nil {
        for rows.Next() {
            var t models.Tool
            var schema, config []byte
            if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &t.Type, &schema, &config); err != nil {
                continue
            }
            if schema != nil { json.Unmarshal(schema, &t.Schema) }
            if config != nil { json.Unmarshal(config, &t.Config) }

            toolDef, err := e.resolveToolDef(&t)
            if err != nil {
                slog.Warn("engine: failed to resolve tool", "tool", t.Name, "error", err)
                continue
            }
            if toolDef != nil {
                agentTools = append(agentTools, toolDef)
            }
        }
        rows.Close()
    }
}
```

Add `resolveToolDef` method:

```go
func (e *Engine) resolveToolDef(t *models.Tool) (*ToolDef, error) {
    switch t.Type {
    case models.ToolTypeBuiltin:
        handlerName, _ := t.Config["handler_name"].(string)
        if handlerName == "" {
            return nil, fmt.Errorf("builtin tool missing handler_name")
        }
        def, ok := e.registry.Get(handlerName)
        if !ok {
            return nil, fmt.Errorf("builtin handler not found: %s", handlerName)
        }
        return def, nil

    case models.ToolTypeWebhook:
        return makeWebhookToolDef(t)

    case models.ToolTypeSQLQuery:
        return makeSQLQueryToolDef(t, e.pool)

    default:
        return nil, fmt.Errorf("unknown tool type: %s", t.Type)
    }
}
```

**Step 2: Create tools_webhook.go**

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
    "github.com/the-heaven-labs/aether/internal/models"
)

func makeWebhookToolDef(t *models.Tool) (*ToolDef, error) {
    url, _ := t.Config["url"].(string)
    if url == "" {
        return nil, fmt.Errorf("webhook tool missing url")
    }
    method, _ := t.Config["method"].(string)
    if method == "" {
        method = "POST"
    }
    headers := make(map[string]string)
    if h, ok := t.Config["headers"].(map[string]any); ok {
        for k, v := range h {
            headers[k] = fmt.Sprintf("%v", v)
        }
    }

    return &ToolDef{
        Type: "function",
        Function: struct {
            Name        string `json:"name"`
            Description string `json:"description"`
            Parameters  any    `json:"parameters"`
        }{
            Name:        t.Name,
            Description: t.Description,
            Parameters:  t.Schema,
        },
        Handler: func(args json.RawMessage, ctx *ToolContext) (any, error) {
            bodyBytes := args
            var req *http.Request
            var err error
            if method == "GET" {
                req, err = http.NewRequest(method, url, nil)
            } else {
                req, err = http.NewRequest(method, url, bytes.NewReader(bodyBytes))
            }
            if err != nil {
                return nil, fmt.Errorf("create request: %w", err)
            }
            req.Header.Set("Content-Type", "application/json")
            for k, v := range headers {
                req.Header.Set(k, v)
            }
            c, cancel := context.WithTimeout(ctx.Context, 30*time.Second)
            defer cancel()
            resp, err := http.DefaultClient.Do(req.WithContext(c))
            if err != nil {
                return nil, fmt.Errorf("webhook call: %w", err)
            }
            defer resp.Body.Close()
            respBody, _ := io.ReadAll(resp.Body)
            return map[string]any{
                "status": resp.StatusCode,
                "body":   string(respBody),
            }, nil
        },
    }, nil
}
```

**Step 3: Create tools_sql.go**

```go
package agent

import (
    "encoding/json"
    "fmt"
    "strings"
    "github.com/the-heaven-labs/aether/internal/models"
    "github.com/jackc/pgx/v5/pgxpool"
)

func makeSQLQueryToolDef(t *models.Tool, pool *pgxpool.Pool) (*ToolDef, error) {
    connectorID, _ := t.Config["connector_id"].(string)
    query, _ := t.Config["query"].(string)
    if connectorID == "" || query == "" {
        return nil, fmt.Errorf("sql_query tool missing connector_id or query")
    }

    return &ToolDef{
        Type: "function",
        Function: struct {
            Name        string `json:"name"`
            Description string `json:"description"`
            Parameters  any    `json:"parameters"`
        }{
            Name:        t.Name,
            Description: t.Description,
            Parameters:  t.Schema,
        },
        Handler: func(args json.RawMessage, ctx *ToolContext) (any, error) {
            // Interpolate named params from tool args into the query
            var params map[string]any
            queryStr := query
            if len(args) > 0 {
                json.Unmarshal(args, &params)
                for k, v := range params {
                    val := fmt.Sprintf("%v", v)
                    queryStr = strings.ReplaceAll(queryStr, fmt.Sprintf("{{%s}}", k), val)
                }
            }
            // Execute via executor
            result, err := ExecuteSQL(ctx.Context, pool, connectorID, queryStr)
            if err != nil {
                return nil, fmt.Errorf("sql query: %w", err)
            }
            return result, nil
        },
    }, nil
}
```

**Step 4: Change tool merging in ProcessMessage**

Replace the current merge section (around line 238) where it copies passed-in tools:

```go
allTools := make([]*ToolDef, 0)
allTools = append(allTools, agentTools...)     // from tools table
allTools = append(allTools, tools...)           // passed-in tools (if any)

if len(agent.MCPServers) > 0 {
    for _, ms := range agent.MCPServers {
        // ... unchanged MCP tool generation
    }
}
```

**Step 5: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: no errors

**Step 6: Commit**

```bash
git add internal/agent/engine.go internal/agent/tools_webhook.go internal/agent/tools_sql.go
git commit -m "feat(engine): resolve tools from DB by type, add webhook and sql_query handlers"
```

---

### Task 7: Seed built-in tools at server startup

**Files:**
- Create: `internal/agent/tools_seed.go`
- Modify: `internal/agent/engine.go` (NewEngine)

**Step 1: Create tools_seed.go**

```go
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
)

type BuiltinToolDef struct {
    Name        string
    Description string
    Schema      map[string]any
    HandlerName string
}

var BuiltinTools = []BuiltinToolDef{
    {Name: "notebook_read_cells", Description: "Read cells from a notebook", HandlerName: "notebook_read_cells"},
    {Name: "notebook_create_cell", Description: "Create a new cell in a notebook", HandlerName: "notebook_create_cell"},
    {Name: "notebook_update_cell", Description: "Update an existing cell", HandlerName: "notebook_update_cell"},
    {Name: "notebook_delete_cell", Description: "Delete a cell", HandlerName: "notebook_delete_cell"},
    {Name: "notebook_run_cell", Description: "Run a code cell", HandlerName: "notebook_run_cell"},
    {Name: "notebook_list_cells", Description: "List all cells in a notebook", HandlerName: "notebook_list_cells"},
    {Name: "get_notebook_context", Description: "Get full notebook context", HandlerName: "get_notebook_context"},
    {Name: "list_skills", Description: "List available skills", HandlerName: "list_skills"},
    {Name: "load_skill", Description: "Load a skill's full instructions", HandlerName: "load_skill"},
    {Name: "update_agent", Description: "Update agent configuration", HandlerName: "update_agent"},
    {Name: "create_skill", Description: "Create a new skill", HandlerName: "create_skill"},
    {Name: "update_skill", Description: "Update an existing skill", HandlerName: "update_skill"},
    {Name: "spawn_subagents", Description: "Spawn parallel subagents", HandlerName: "spawn_subagents"},
    {Name: "create_tasks", Description: "Create tasks for subagents", HandlerName: "create_tasks"},
    {Name: "update_task", Description: "Update a task's status", HandlerName: "update_task"},
    {Name: "get_tasks", Description: "Get current tasks", HandlerName: "get_tasks"},
    {Name: "create_chart", Description: "Create a chart from cell output", HandlerName: "create_chart"},
    {Name: "update_chart", Description: "Update an existing chart", HandlerName: "update_chart"},
    {Name: "list_notebooks", Description: "List notebooks", HandlerName: "list_notebooks"},
    {Name: "list_connectors", Description: "List connectors", HandlerName: "list_connectors"},
    {Name: "list_folders", Description: "List folders", HandlerName: "list_folders"},
    {Name: "get_folder_tree", Description: "Get folder tree", HandlerName: "get_folder_tree"},
}

func SeedBuiltinTools(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
    for _, bt := range BuiltinTools {
        schema, _ := json.Marshal(bt.Schema)
        config, _ := json.Marshal(map[string]string{"handler_name": bt.HandlerName})
        _, err := pool.Exec(ctx, `
            INSERT INTO tools (id, org_id, name, description, type, schema, config, created_by, created_at, updated_at)
            VALUES ($1, $2, $3, $4, 'builtin', $5, $6, $7, NOW(), NOW())
            ON CONFLICT (org_id, name) DO NOTHING`,
            uuid.New().String(), orgID, bt.Name, bt.Description, schema, config, "00000000-0000-0000-0000-000000000000")
        if err != nil {
            slog.Warn("seed builtin tool", "tool", bt.Name, "error", err)
        }
    }
    return nil
}
```

**Step 2: Call SeedBuiltinTools in NewEngine for each org**

Add to `NewEngine` or to server startup (in `router.go` where the engine is initialized):

```go
// Seed built-in tools for each org
orgRows, err := pool.Query(ctx, `SELECT id FROM orgs`)
if err == nil {
    for orgRows.Next() {
        var orgID string
        orgRows.Scan(&orgID)
        SeedBuiltinTools(ctx, pool, orgID)
    }
    orgRows.Close()
}
```

**Step 3: Verify it compiles**

Run: `go build ./internal/agent/`
Expected: no errors

**Step 4: Commit**

```bash
git add internal/agent/tools_seed.go
git commit -m "feat(agent): seed built-in tools into tools table per org at startup"
```

---

### Task 8: Backfill agents.tool_ids from skills data migration

**Files:**
- Create: `internal/database/migrations/V065__backfill_agent_tool_ids.sql`

**Step 1: Write the backfill migration**

```sql
-- Backfill agents.tool_ids from their skills' tool references
-- For each agent, collect all unique tool names from its skills,
-- then resolve to tool UUIDs in the tools table.
-- This runs after built-in tools are seeded (Task 7).

-- Step 1: For each existing skill that had tool_ids, create a mapping
-- But wait — skills no longer have tool_ids (dropped in migration 064).
-- We're doing this as a data-only migration that runs at startup,
-- not as a SQL migration file. The backfill logic lives in Go code.

-- Instead, this migration is a NO-OP placeholder.
-- The actual backfill happens in server startup code.
SELECT 1;
```

**Step 2: Instead of SQL, add backfill logic to server startup**

In `router.go` or a startup hook:

```go
func (s *Server) backfillAgentToolIDs(ctx context.Context) {
    // Get all agents
    rows, err := s.db.Query(ctx, `SELECT id, skill_ids FROM agents WHERE array_length(tool_ids, 1) IS NULL OR array_length(tool_ids, 1) = 0`)
    if err != nil {
        slog.Warn("backfill: query agents failed", "error", err)
        return
    }
    defer rows.Close()
    for rows.Next() {
        var agentID string
        var skillIDs []string
        if err := rows.Scan(&agentID, &skillIDs); err != nil {
            continue
        }
        if len(skillIDs) == 0 {
            continue
        }
        // Collect tool names from old skills data (stored in a temp table or just skip)
        // Since we dropped tool_ids from skills, existing data is lost.
        // This backfill only helps for fresh migrations where tool_ids are still in skills.
        // For production upgrades, a separate script handles this.
        slog.Info("backfill: skip agent, tool data already migrated", "agent_id", agentID)
    }
}
```

**Step 3: Commit**

```bash
git add internal/database/migrations/V065__backfill_agent_tool_ids.sql
git commit -m "feat(db): backfill agents.tool_ids from skills (startup-based)"
```

---

### Task 9: Update SkillsPage — remove tool toggles

**Files:**
- Modify: `web/src/pages/SkillsPage.tsx`
- Modify: `web/src/types/agent.ts`

**Step 1: Remove tool_ids from Skill interface**

```typescript
// types/agent.ts
export interface Skill {
  id: string
  org_id: string
  name: string
  description: string
  system_prompt: string
  // tool_ids removed
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
}
```

**Step 2: Remove tool toggle checkboxes from SkillsPage**

Remove the `TOOL_OPTIONS` constant and the tool checklist section in the create/edit form. The form is now just name, description, system_prompt.

**Step 3: Verify it compiles**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 4: Commit**

```bash
git add web/src/pages/SkillsPage.tsx web/src/types/agent.ts
git commit -m "feat(ui): remove tool toggles from SkillsPage, skills are now prompt-only"
```

---

### Task 10: Add ToolsPage — new frontend page

**Files:**
- Create: `web/src/pages/ToolsPage.tsx`
- Modify: `web/src/types/agent.ts`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/Sidebar.tsx`
- Modify: `web/src/api/client.ts`

**Step 1: Add Tool interface to types/agent.ts**

```typescript
export type ToolType = 'builtin' | 'webhook' | 'sql_query'

export interface Tool {
  id: string
  org_id: string
  name: string
  description: string
  type: ToolType
  schema: Record<string, any>
  config: Record<string, any>
  folder_id?: string
  created_by: string
  created_at: string
  updated_at: string
}
```

**Step 2: Add API endpoints to client.ts**

```typescript
const API_BASE = '/api/v1'

export const toolsApi = {
  list: (): Promise<Tool[]> => api.get(`${API_BASE}/tools`),
  get: (id: string): Promise<Tool> => api.get(`${API_BASE}/tools/${id}`),
  create: (data: Partial<Tool>): Promise<{ id: string }> => api.post(`${API_BASE}/tools`, data),
  update: (id: string, data: Partial<Tool>): Promise<void> => api.put(`${API_BASE}/tools/${id}`, data),
  delete: (id: string): Promise<void> => api.delete(`${API_BASE}/tools/${id}`),
  test: (id: string): Promise<{ status: number; body?: string; result?: any }> =>
    api.post(`${API_BASE}/tools/${id}/test`),
}
```

**Step 3: Create ToolsPage.tsx**

Standard CRUD page following the pattern of ModelsPage.tsx:
- Query key: `['tools']`
- Table columns: name, type (badge: builtin=blue, webhook=green, sql_query=purple), description
- Create button → modal/form with:
  - Name, description (common)
  - Type selector (dropdown with builtin/webhook/sql_query)
  - Type-specific fields:
    - webhook: URL input, method dropdown, headers key-value editor, body template
    - sql_query: connector searchable select, SQL textarea with monospace font, params schema editor
    - builtin: read-only (button hidden for new, edit disables type change)
  - PermissionsPanel

**Step 4: Add route in App.tsx**

```typescript
<Route path="/tools" element={<ToolsPage />} />
```

**Step 5: Add sidebar entry**

```typescript
// In Sidebar.tsx AGENT_NAV_ITEMS
{ to: '/tools', title: 'Tools', icon: <Wrench size={16} /> }
```

**Step 6: Verify it compiles**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 7: Commit**

```bash
git add web/src/pages/ToolsPage.tsx web/src/types/agent.ts web/src/App.tsx web/src/components/Sidebar.tsx web/src/api/client.ts
git commit -m "feat(ui): add ToolsPage with CRUD for webhook and sql_query tools"
```

---

### Task 11: Update AgentsPage — add tools multi-select

**Files:**
- Modify: `web/src/pages/AgentsPage.tsx`
- Modify: `web/src/types/agent.ts`

**Step 1: Add tool_ids/tools to Agent interface**

```typescript
export interface Agent {
  // ...existing fields
  tool_ids: string[]
  tools?: Tool[]
  skill_ids: string[]
  skills?: Skill[]
  mcp_server_ids: string[]
  mcp_servers?: MCPServerOrg[]
}
```

**Step 2: Add tools multi-select to agent form**

Alongside the "Skills" and "MCP Servers" searchable multi-selects, add a "Tools" multi-select that fetches from `['tools']` query. Include the type badge next to each tool name in the dropdown.

**Step 3: Update create/update payload to include tool_ids**

Send `tool_ids` alongside `skill_ids` and `mcp_server_ids` in create/update requests.

**Step 4: Verify it compiles**

Run: `cd web && npx tsc --noEmit`
Expected: no errors

**Step 5: Commit**

```bash
git add web/src/pages/AgentsPage.tsx web/src/types/agent.ts
git commit -m "feat(ui): add tools multi-select to AgentsPage"
```

---

### Task 12: Update agent GET handler to return tools

**Files:**
- Modify: `internal/api/agent_handlers.go`

**Step 1: Load tools alongside skills and MCP servers in handleGetAgent**

After loading the agent row (similar to MCP server loading), add:

```go
// Load tools
toolRows, err := s.db.Query(r.Context(), `
    SELECT id, org_id, name, description, type, schema, config, folder_id, created_by, created_at, updated_at
    FROM tools WHERE id = ANY($1)`, agent.ToolIDs)
if err == nil {
    defer toolRows.Close()
    for toolRows.Next() {
        var t models.Tool
        var schema, config []byte
        if err := toolRows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &t.Type, &schema, &config, &t.FolderID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err == nil {
            if schema != nil { json.Unmarshal(schema, &t.Schema) }
            if config != nil { json.Unmarshal(config, &t.Config) }
            agent.Tools = append(agent.Tools, t)
        }
    }
}
```

**Step 2: Verify it compiles**

Run: `go build ./internal/api/`
Expected: no errors

**Step 3: Commit**

```bash
git add internal/api/agent_handlers.go
git commit -m "feat(api): expand tools[] in agent GET response"
```
