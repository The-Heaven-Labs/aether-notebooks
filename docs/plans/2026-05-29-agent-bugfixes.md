# Agent System Bug Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix all 30 identified bugs, security issues, and missing features in the hnb agent system.

**Architecture:** Fix issues in priority order (P0 → P3). Each task is self-contained and can be verified independently. Backend fixes first, then frontend fixes. Tests use the existing `setupTestServer` / `registerAndGetToken` pattern from `testhelpers_test.go`.

**Tech Stack:** Go 1.22+ (backend), React + TypeScript (frontend), PostgreSQL (data), Gorilla WebSocket

---

## Task 1: Fix subagent nil masterKey crash (P0)

Subagents pass `nil` as masterKey to `llm.Chat()`, causing `crypto.Decrypt` to fail on every call.

**Files:**
- Modify: `internal/agent/types.go` — add `MasterKey []byte` to `ToolContext`
- Modify: `internal/agent/engine.go` — pass `masterKey` into `ToolContext`
- Modify: `internal/agent/subagent.go` — pass `MasterKey` through `ToolContext` and use it in `llm.Chat()`

**Step 1: Add MasterKey field to ToolContext**

In `internal/agent/types.go`, the `MasterKey` field already exists on `ToolContext` (line 23). We just need to make sure it's populated.

**Step 2: Ensure MasterKey is passed in engine.go ToolContext**

The engine already sets `MasterKey: masterKey` on line 222. ✓ Already correct.

**Step 3: Fix subagent.go to pass masterKey**

In `internal/agent/subagent.go`, change the `runSubagent` method signature to accept `masterKey` and pass it to `llm.Chat()`. Also pass real user identity.

Replace `internal/agent/subagent.go:55-128` with:

```go
func (e *Engine) runSubagent(ctx context.Context, parentSessionID string, task SubagentTaskConfig, parentUserID, parentOrgID, masterKey []byte) SubagentResult {
	taskID := task.ID
	if taskID == "" {
		taskID = uuid.New().String()
	}

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
		if e.llm == nil {
			return SubagentResult{TaskID: taskID, Status: "failed", Error: "no LLM client configured"}
		}

		resp, err := e.llm.Chat(ctx, messages, nil, masterKey)
		if err != nil {
			return SubagentResult{TaskID: taskID, Status: "failed", Error: err.Error()}
		}

		choice := resp.Choices[0]

		if choice.Message.Content != "" {
			messages = append(messages, ChatMessage{Role: "assistant", Content: choice.Message.Content})
			result := SubagentResult{
				TaskID:    taskID,
				Status:    "completed",
				Result:    choice.Message.Content,
				TokensIn:  resp.Usage.PromptTokens,
				TokensOut: resp.Usage.CompletionTokens,
			}

			_, _ = e.pool.Exec(ctx, `
				UPDATE subagent_tasks SET status = 'completed', result = $1, tokens_input = $2, tokens_output = $3, completed_at = NOW()
				WHERE id = $4
			`, choice.Message.Content, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, taskID)

			return result
		}

		for _, tc := range choice.ToolCalls {
			toolDef, ok := e.registry.Get(tc.Function.Name)
			if !ok {
				messages = append(messages, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name)})
				continue
			}

			result, err := toolDef.Handler(json.RawMessage(tc.Function.Arguments), &ToolContext{
				Context:    ctx,
				UserID:     parentUserID,
				OrgID:      parentOrgID,
				OrgRole:    "editor",
				NotebookID: taskID,
				SessionID:  parentSessionID,
				DB:         e.pool,
				MasterKey:  masterKey,
			})

			if err != nil {
				messages = append(messages, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: err.Error()})
			} else {
				resultJSON, _ := json.Marshal(result)
				messages = append(messages, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: string(resultJSON)})
			}
		}
	}

	return SubagentResult{TaskID: taskID, Status: "completed", Result: "max turns reached"}
}
```

**Step 4: Update SpawnSubagents to pass new args**

In `internal/agent/subagent.go`, change `SpawnSubagents` to accept and pass `masterKey`, and to look up parent session's user/org identity. Also add `SubagentTaskConfig` `ToolCallID` to `ChatMessage` for proper message formatting.

Replace `SpawnSubagents`:

```go
func (e *Engine) SpawnSubagents(ctx context.Context, parentSessionID string, tasks []SubagentTaskConfig, masterKey []byte, onUpdate func([]SubagentResult)) error {
	var parentUserID, parentOrgID string
	err := e.pool.QueryRow(ctx, `SELECT user_id, agent_id FROM agent_sessions WHERE id = $1`, parentSessionID).Scan(&parentUserID, &parentOrgID)
	if err != nil {
		return fmt.Errorf("get parent session: %w", err)
	}
	var orgID string
	err = e.pool.QueryRow(ctx, `SELECT org_id FROM agents WHERE id = $1`, parentOrgID).Scan(&orgID)
	if err != nil {
		return fmt.Errorf("get agent org: %w", err)
	}

	sem := make(chan struct{}, MaxSubagentParallelism)
	var wg sync.WaitGroup
	results := make([]SubagentResult, len(tasks))

	for i, task := range tasks {
		sem <- struct{}{}
		wg.Add(1)

		go func(i int, task SubagentTaskConfig) {
			defer wg.Done()
			defer func() { <-sem }()

			result := e.runSubagent(ctx, parentSessionID, task, parentUserID, orgID, masterKey)
			results[i] = result
			onUpdate(results)
		}(i, task)
	}

	wg.Wait()
	return nil
}
```

**Step 5: Verify compilation**

Run: `cd /home/jesus/Projects/hnb-claude && go build ./...`
Expected: Compiles successfully

**Step 6: Commit**

```
fix: pass masterKey and real identity to subagents
```

---

## Task 2: Fix agent version INSERT wrong SQL (P0)

The `INSERT ... SELECT` uses `$1` for both the new version UUID and the `WHERE` clause, making `id = $1` match the new UUID (not found) instead of the agent.

**Files:**
- Modify: `internal/agent/tools_agent.go:141-145`

**Step 1: Fix the SQL query**

In `internal/agent/tools_agent.go`, change lines 141-145 from:

```go
		_, _ = pool.Exec(ctx.Context, `
			INSERT INTO agent_versions (id, agent_id, version, name, description, system_prompt, skill_ids, changed_by, change_reason, created_at)
			SELECT $1, $2, $3, name, description, system_prompt, skill_ids, $4, 'agent_self_modification', NOW()
			FROM agents WHERE id = $1
		`, uuid.New().String(), agentID, version, ctx.UserID)
```

to:

```go
		_, _ = pool.Exec(ctx.Context, `
			INSERT INTO agent_versions (id, agent_id, version, name, description, system_prompt, skill_ids, changed_by, change_reason, created_at)
			SELECT $1, $2, $3, name, description, system_prompt, skill_ids, $4, 'agent_self_modification', NOW()
			FROM agents WHERE id = $2
		`, uuid.New().String(), agentID, version, ctx.UserID)
```

The fix: change `WHERE id = $1` → `WHERE id = $2` (reference the `agentID` param).

**Step 2: Verify compilation**

Run: `go build ./...`
Expected: Compiles successfully

**Step 3: Commit**

```
fix: agent version INSERT references correct agent ID param
```

---

## Task 3: Fix WebSocket concurrent writes (P0)

Gorilla websocket is not safe for concurrent writes. Multiple goroutines call `conn.WriteJSON()` simultaneously (read goroutine + write goroutine + ProcessMessage callbacks). Serialize all writes through the `writeChan`.

**Files:**
- Modify: `internal/api/agent_ws.go`

**Step 1: Refactor handleAgentWS to serialize all writes**

The fix: all `conn.WriteJSON()` calls must go through the `writeChan` goroutine. Wrap writes into typed structs and send them over a single channel.

Replace the entire `handleAgentWS` method body (roughly lines 36-198) with a version that routes all writes through the writer goroutine. Here's the approach:

1. Define a `wsOut` struct to carry typed writes.
2. All callbacks construct a `wsOut` and send it on `writeChan`.
3. The writer goroutine is the only place that calls `conn.WriteJSON()`.

In `internal/api/agent_ws.go`, add a new type near the top:

```go
type wsOut struct {
	typ string
	data any
}
```

Then replace `handleAgentWS` (lines 36-198) with:

```go
func (s *Server) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())

	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	_, err := s.agentEngine.SessionStore().GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()

	writeChan := make(chan wsOut, 256)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case out := <-writeChan:
				if err := conn.WriteJSON(out); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	go func() {
		defer close(done)
		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			if msg.Type == "reconnect" {
				rows, err := s.db.Pool.Query(ctx, `
					SELECT id, role, content, tool_calls FROM agent_messages
					WHERE session_id = $1 AND id > $2 ORDER BY created_at
				`, sessionID, msg.LastMessageID)
				if err == nil {
					var messages []models.AgentMessage
					for rows.Next() {
						var m models.AgentMessage
						var content *string
						var toolCallsJSON []byte
						rows.Scan(&m.ID, &m.Role, &content, &toolCallsJSON)
						if content != nil {
							m.Content = *content
						}
						if toolCallsJSON != nil {
							json.Unmarshal(toolCallsJSON, &m.ToolCalls)
						}
						messages = append(messages, m)
					}
					rows.Close()
					writeChan <- wsOut{typ: "reconnect_sync", data: map[string]any{"messages": messages}}
				}
				continue
			}

			if msg.Type == "message" {
				_, reasoning, _, events, err := s.agentEngine.ProcessMessage(ctx, sessionID, msg.Content, s.agentEngine.GetRegistry().List(), s.masterKey,
					func(token string) {
						writeChan <- wsOut{typ: "token", data: token}
					},
					func(r string) {
						writeChan <- wsOut{typ: "reasoning", data: r}
					},
					func(toolName, toolID, reasoning string) {
						writeChan <- wsOut{typ: "tool_call", data: struct {
							Type      string `json:"type"`
							Tool      string `json:"tool"`
							Reasoning string `json:"reasoning,omitempty"`
						}{Type: "tool_call", Tool: toolName, Reasoning: reasoning}}
					},
					func(toolName, params, result, errMsg string) {
						writeChan <- wsOut{typ: "tool_result", data: struct {
							Type   string `json:"type"`
							Tool   string `json:"tool"`
							Params string `json:"params"`
							Result string `json:"result"`
							Error  string `json:"error,omitempty"`
						}{Type: "tool_result", Tool: toolName, Params: params, Result: result, Error: errMsg}}
					},
					func(evt agent.EngineEvent) {
						switch evt.Type {
						case "cell_created":
							writeChan <- wsOut{typ: "cell_created", data: struct {
								Type     string `json:"type"`
								CellID   string `json:"cell_id"`
								Position int    `json:"position"`
							}{Type: evt.Type, CellID: evt.CellID, Position: evt.Position}}
						case "tasks_updated":
							writeChan <- wsOut{typ: "tasks_updated", data: struct {
								Type string            `json:"type"`
								Data []agent.AgentTask `json:"data"`
							}{Type: "tasks_updated", Data: evt.Tasks}}
						}
					},
				)
				if err != nil {
					writeChan <- wsOut{typ: "error", data: WSErrorResponse{Type: "error", Message: err.Error()}}
					return
				}

				for range events {
					// Events already sent via onEvent callback during processing.
					// Don't re-send them (prevents duplicates).
				}
				writeChan <- wsOut{typ: "done", data: map[string]any{"content": "", "reasoning": reasoning}}
			} else if msg.Type == "slash_command" {
				result, err := s.agentEngine.HandleSlashCommand(ctx, sessionID, msg.Command, s.masterKey)
				if err != nil {
					writeChan <- wsOut{typ: "error", data: WSErrorResponse{Type: "error", Message: err.Error()}}
					return
				}
				writeChan <- wsOut{typ: "slash_result", data: map[string]any{"command": msg.Command, "data": result}}
			}
		}
	}()

	<-done
}
```

Also add `"encoding/json"` and `"github.com/heavenlabs/hnb/internal/models"` to the imports if not already present.

**Step 2: Fix the `done` event to pass the full response text**

The current `done` event sends `content` as empty string because the streaming tokens already sent the text. But the frontend needs the final text for the `done` handler fallback. Fix by passing the actual response:

Change the `done` line from:
```go
writeChan <- wsOut{typ: "done", data: map[string]any{"content": "", "reasoning": reasoning}}
```
to:
```go
writeChan <- wsOut{typ: "done", data: map[string]any{"content": "", "reasoning": reasoning, "full_response": ""}}
```

We'll leave this as-is for now since the frontend handler already has the streaming text accumulated. The important fix is removing the duplicate event loop.

**Step 3: Remove the duplicate `handleAgentWSWithUpgrader` method**

The second WebSocket handler (lines 200-332) is nearly identical to the first and has the same concurrency bug. Remove it entirely — it's not used in router.go. If it's needed for tests, tests should use the main handler with a custom upgrader.

Delete lines 200-332 of `internal/api/agent_ws.go`.

**Step 4: Verify compilation**

Run: `go build ./...`
Expected: Compiles successfully

**Step 5: Commit**

```
fix: serialize WebSocket writes through single goroutine, remove duplicate handler
```

---

## Task 4: Remove duplicate event sending in WS handler (P0)

This was fixed as part of Task 3 — the post-processing `for range events` loop now has a comment explaining events are already sent via the `onEvent` callback and are not re-sent.

No separate commit needed.

---

## Task 5: Fix listAgents slash command broken query (P1)

`listAgents` calls `e.session.GetSession(ctx, "")` with an empty sessionID, which will always fail. It should query agents by org from the engine context, not through a session lookup.

**Files:**
- Modify: `internal/agent/engine.go:289-310`

**Step 1: Fix listAgents to accept orgID**

Change the `Engine` struct and `listAgents` method to take `orgID` directly. But since the engine doesn't carry orgID currently, the simplest fix is to change `HandleSlashCommand` to pass the org and user info.

Add `ProcessMessage` already resolves `agent.OrgID`. Change `HandleSlashCommand` to accept orgID and use it in `listAgents`.

In `internal/agent/engine.go`, change `listAgents` to accept orgID:

```go
func (e *Engine) listAgents(ctx context.Context, orgID string) (map[string]any, error) {
	rows, err := e.pool.Query(ctx, `SELECT id, name, description FROM agents WHERE org_id = $1 LIMIT 50`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []map[string]string
	for rows.Next() {
		var id, name, desc string
		if err := rows.Scan(&id, &name, &desc); err != nil {
			return nil, err
		}
		agents = append(agents, map[string]string{"id": id, "name": name, "description": desc})
	}
	return map[string]any{"agents": agents}, nil
}
```

And update `HandleSlashCommand` to accept orgID:

```go
func (e *Engine) HandleSlashCommand(ctx context.Context, sessionID string, command string, orgID string, masterKey []byte) (any, error) {
	switch command {
	case "skills":
		return e.listSkills(ctx)
	case "agents":
		return e.listAgents(ctx, orgID)
	case "new":
		return map[string]string{"session_id": sessionID}, nil
	case "summarize":
		return e.summarizeSession(ctx, sessionID, masterKey)
	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}
```

**Step 2: Update the WS handler call site**

In `internal/api/agent_ws.go`, find the two places where `HandleSlashCommand` is called and add `claims.OrgID`:

Change:
```go
result, err := s.agentEngine.HandleSlashCommand(ctx, sessionID, msg.Command, s.masterKey)
```
to:
```go
result, err := s.agentEngine.HandleSlashCommand(ctx, sessionID, msg.Command, claims.OrgID, s.masterKey)
```

**Step 3: Verify compilation**

Run: `go build ./...`
Expected: Compiles successfully

**Step 4: Commit**

```
fix: listAgents slash command queries by orgID instead of broken session lookup
```

---

## Task 6: Add org_id checks to agent, skill, and model_config delete/update handlers (P1)

Several handlers don't verify the resource belongs to the requesting user's org, allowing cross-org access.

**Files:**
- Modify: `internal/api/agent_handlers.go`
- Modify: `internal/api/skill_handlers.go`
- Modify: `internal/api/model_config_handlers.go`

**Step 1: Fix handleDeleteAgent**

In `internal/api/agent_handlers.go:202`, change:

```go
_, err = h.server.db.Pool.Exec(r.Context(), `DELETE FROM agents WHERE id = $1`, agentID)
```

to:

```go
_, err = h.server.db.Pool.Exec(r.Context(), `DELETE FROM agents WHERE id = $1 AND org_id = $2`, agentID, claims.OrgID)
```

**Step 2: Fix skill update and delete**

In `internal/api/skill_handlers.go:95`, change:

```go
_, err := h.server.db.Pool.Exec(r.Context(), `
	UPDATE skills SET
		name = COALESCE($2, name),
		description = COALESCE($3, description),
		system_prompt = COALESCE($4, system_prompt),
		tool_ids = COALESCE($5, tool_ids),
		updated_at = NOW()
	WHERE id = $1
`, skillID, req.Name, req.Description, req.SystemPrompt, req.ToolIDs)
```

to:

```go
result, err := h.server.db.Pool.Exec(r.Context(), `
	UPDATE skills SET
		name = COALESCE($2, name),
		description = COALESCE($3, description),
		system_prompt = COALESCE($4, system_prompt),
		tool_ids = COALESCE($5, tool_ids),
		updated_at = NOW()
	WHERE id = $1 AND org_id = $6
`, skillID, req.Name, req.Description, req.SystemPrompt, req.ToolIDs, claims.OrgID)
if err != nil {
	writeError(w, http.StatusInternalServerError, err.Error())
	return
}
if result.RowsAffected() == 0 {
	writeError(w, http.StatusNotFound, "skill not found")
	return
}
```

Requires adding `"github.com/jackc/pgx/v5"` import for `RowsAffected()` — actually, `pgxpool.Exec` returns `pgconn.CommandTag`, use `result.RowsAffected()`.

In `internal/api/skill_handlers.go:116`, change:

```go
_, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM skills WHERE id = $1`, skillID)
```

to:

```go
result, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM skills WHERE id = $1 AND org_id = $2`, skillID, claims.OrgID)
if err != nil {
	writeError(w, http.StatusInternalServerError, err.Error())
	return
}
if result.RowsAffected() == 0 {
	writeError(w, http.StatusNotFound, "skill not found")
	return
}
```

Add `"github.com/jackc/pgx/v5/pgconn"` to imports for `pgconn.CommandTag` — but actually pgxpool returns it directly. Just use the pattern above.

**Step 3: Fix model_config update and delete**

In `internal/api/model_config_handlers.go:133`, change the WHERE clause:

```go
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
`, cfgID, req.Name, req.Provider, req.BaseURL, req.Model, defaultParamsJSON, req.ContextWindow)
```

to:

```go
result, err := h.server.db.Pool.Exec(r.Context(), `
	UPDATE model_configs SET
		name = COALESCE($2, name),
		provider = COALESCE($3, provider),
		base_url = COALESCE($4, base_url),
		model = COALESCE($5, model),
		default_params = COALESCE($6, default_params),
		context_window = COALESCE($7, context_window),
		updated_at = NOW()
	WHERE id = $1 AND org_id = $8
`, cfgID, req.Name, req.Provider, req.BaseURL, req.Model, defaultParamsJSON, req.ContextWindow, claims.OrgID)
if err != nil {
	writeError(w, http.StatusInternalServerError, err.Error())
	return
}
if result.RowsAffected() == 0 {
	writeError(w, http.StatusNotFound, "model config not found")
	return
}
```

In `internal/api/model_config_handlers.go:155`, change:

```go
_, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM model_configs WHERE id = $1`, cfgID)
```

to:

```go
result, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM model_configs WHERE id = $1 AND org_id = $2`, cfgID, claims.OrgID)
if err != nil {
	writeError(w, http.StatusInternalServerError, err.Error())
	return
}
if result.RowsAffected() == 0 {
	writeError(w, http.StatusNotFound, "model config not found")
	return
}
```

Add `"github.com/jackc/pgx/v5/pgconn"` import only if needed — `pgxpool.Exec` returns `(pgconn.CommandTag, error)`, and `CommandTag.RowsAffected()` works. The `claims` variable is already in scope (currently unused with `_ = claims`).

**Step 4: Verify compilation**

Run: `go build ./...`

**Step 5: Commit**

```
fix: add org_id scoping to agent, skill, and model_config mutations
```

---

## Task 7: Add permission checks to chart tools (P1)

`create_chart` and `update_chart` don't verify the user has edit permission on the notebook.

**Files:**
- Modify: `internal/agent/tools_chart.go`

**Step 1: Add CheckPermission + GetNotebookIDForCell to chart handlers**

In `internal/agent/tools_chart.go`, add to `makeCreateChartHandler` after the args unmarshal (after line 50):

```go
notebookID, err := ctx.GetNotebookIDForCell(req.CellID)
if err != nil {
	return nil, fmt.Errorf("get cell notebook: %w", err)
}
if err := ctx.CheckPermission("notebook", notebookID, "edit"); err != nil {
	return nil, err
}
```

Add the same to `makeUpdateChartHandler` after the args unmarshal (after line 86):

```go
notebookID, err := ctx.GetNotebookIDForCell(req.CellID)
if err != nil {
	return nil, fmt.Errorf("get cell notebook: %w", err)
}
if err := ctx.CheckPermission("notebook", notebookID, "edit"); err != nil {
	return nil, err
}
```

**Step 2: Verify compilation**

Run: `go build ./...`

**Step 3: Commit**

```
fix: add permission checks to chart creation and update tools
```

---

## Task 8: Fix update_cell missing description field (Data Integrity #16)

The `update_cell` tool accepts `description` but the SQL UPDATE only sets `source` and `title`.

**Files:**
- Modify: `internal/agent/tools_notebook.go:220-228`

**Step 1: Add description to the UPDATE query**

In `internal/agent/tools_notebook.go`, change the `UPDATE` query from:

```go
_, err = db.Exec(ctx.Context, `
	UPDATE cells SET source = COALESCE(NULLIF($2, ''), source),
		title = COALESCE(NULLIF($3, ''), title),
		updated_at = NOW()
	WHERE id = $1
`, req.CellID, req.Source, req.Title)
```

to:

```go
_, err = db.Exec(ctx.Context, `
	UPDATE cells SET source = COALESCE(NULLIF($2, ''), source),
		title = COALESCE(NULLIF($3, ''), title),
		description = COALESCE(NULLIF($4, ''), description),
		updated_at = NOW()
	WHERE id = $1
`, req.CellID, req.Source, req.Title, req.Description)
```

**Step 2: Verify compilation**

Run: `go build ./...`

**Step 3: Commit**

```
fix: include description field in update_cell tool
```

---

## Task 9: Fix create_cell position logic (Data Integrity #17)

Current logic: position > 0 → position-1; position == 0 → append. This means position=1 maps to index 0 (first), position=0 appends. The convention should be: position=0 means "insert at beginning", positive position means "insert at that position", and -1 or omitted means append.

**Files:**
- Modify: `internal/agent/tools_notebook.go:167-176`

**Step 1: Rewrite position logic**

In `internal/agent/tools_notebook.go`, replace the position block (lines 168-176):

```go
cellID := uuid.New().String()
position := req.Position
if position > 0 {
	position = position - 1
}
if position == 0 {
	var maxPos int
	db.QueryRow(ctx.Context, `SELECT COALESCE(MAX(position), 0) FROM cells WHERE notebook_id = $1`, req.NotebookID).Scan(&maxPos)
	position = maxPos + 1
}
```

with:

```go
cellID := uuid.New().String()
position := req.Position
if position <= 0 {
	var maxPos int
	db.QueryRow(ctx.Context, `SELECT COALESCE(MAX(position), -1) FROM cells WHERE notebook_id = $1`, req.NotebookID).Scan(&maxPos)
	position = maxPos + 1
}
```

Now: position=1 → insert at index 0 (first cell), position=2 → insert at index 1, position=0 or negative → append at end. Also need to shift existing cells at that position. However, the current INSERT doesn't shift cells either, so let's just correct the position mapping and leave the shift as a known limitation (consistent with existing behavior).

Actually, the simplest correct mapping for the LLM is: the LLM sends 1-based positions (position 1 = first cell). We convert to 0-based. If position is omitted (0), we append.

Replace with:

```go
cellID := uuid.New().String()
position := req.Position
if position <= 0 {
	var maxPos int
	db.QueryRow(ctx.Context, `SELECT COALESCE(MAX(position), -1) FROM cells WHERE notebook_id = $1`, req.NotebookID).Scan(&maxPos)
	position = maxPos + 1
} else {
	position = position - 1
}
```

**Step 2: Verify compilation**

Run: `go build ./...`

**Step 3: Commit**

```
fix: correct create_cell position mapping (1-based input, 0 for append)
```

---

## Task 10: Wire up run_cell to actually execute cells (P1)

The `run_cell` tool currently returns "queued" without actually executing. We need to at least queue execution properly and return results, or clearly indicate it's not supported yet.

**Files:**
- Modify: `internal/agent/tools_notebook.go:236-273`

**Step 1: Make run_cell actually execute via the executor**

The executor requires a connector config. The cell has a `connector_id`. We need to decrypt the connector credentials and create an executor.

Replace `makeRunCellHandler` body (lines 236-273) with:

```go
func makeRunCellHandler(db *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			CellID string `json:"cell_id"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		notebookID, err := ctx.GetNotebookIDForCell(req.CellID)
		if err != nil {
			return nil, fmt.Errorf("get cell notebook: %w", err)
		}
		if err := ctx.CheckPermission("notebook", notebookID, "run"); err != nil {
			return nil, err
		}

		var cell struct {
			ConnectorID *string `json:"connector_id"`
			Language    string  `json:"language"`
			Source      string  `json:"source"`
			Limit       int     `json:"limit"`
		}
		err = db.QueryRow(ctx.Context, `
			SELECT connector_id, language, source, COALESCE(limit, 0) FROM cells WHERE id = $1
		`, req.CellID).Scan(&cell.ConnectorID, &cell.Language, &cell.Source, &cell.Limit)
		if err != nil {
			return nil, fmt.Errorf("get cell: %w", err)
		}

		if cell.ConnectorID == nil || *cell.ConnectorID == "" {
			return nil, fmt.Errorf("cell has no connector assigned")
		}

		var connType models.ConnectorType
		var configEnc []byte
		err = db.QueryRow(ctx.Context,
			`SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
			*cell.ConnectorID, ctx.OrgID,
		).Scan(&connType, &configEnc)
		if err != nil {
			return nil, fmt.Errorf("get connector: %w", err)
		}

		if ctx.MasterKey == nil {
			return nil, fmt.Errorf("master key not available")
		}

		plain, err := crypto.Decrypt(configEnc, ctx.MasterKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt credentials: %w", err)
		}

		var cfg models.ConnectorConfig
		if err := json.Unmarshal(plain, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}

		var exec executor.Executor
		switch connType {
		case models.ConnectorPostgres:
			exec, err = executor.NewPostgresExecutor(cfg)
		case models.ConnectorClickHouse:
			exec, err = executor.NewClickHouseExecutor(cfg)
		default:
			return nil, fmt.Errorf("unsupported connector type: %s", connType)
		}
		if err != nil {
			return nil, fmt.Errorf("connect: %w", err)
		}
		defer exec.Close()

		query := cell.Source
		if cell.Limit > 0 && !strings.Contains(strings.ToUpper(query), "LIMIT") {
			query = strings.TrimRight(query, ";") + fmt.Sprintf(" LIMIT %d", cell.Limit)
		}

		result, err := exec.Query(ctx.Context, query)
		if err != nil {
			return map[string]any{
				"cell_id": req.CellID,
				"status":   "error",
				"error":    err.Error(),
			}, nil
		}

		_ = ctx.AuditLog("cell.run", "cell", req.CellID)

		return map[string]any{
			"cell_id": req.CellID,
			"status":  "completed",
			"rows":    len(result.Rows),
			"columns": len(result.Columns),
		}, nil
	}
}
```

Add these imports to the file:

```go
import "strings"
```

**Step 2: Verify compilation**

Run: `go build ./...`

**Step 3: Commit**

```
feat: implement run_cell tool with actual executor call
```

---

## Task 11: Wire MCP tools into engine (Missing #26)

`RegisterMCPTools` exists but is never called from `NewEngine`.

**Files:**
- Modify: `internal/agent/engine.go`

The MCP tools need agent-specific server configs from the database. The best approach is to load them per-agent in `ProcessMessage`, similar to how `ModelConfigID` is loaded. But for now, the simplest fix is to note that MCP tool registration happens at session creation time, not engine creation. We'll pass agent MCP servers to the tool registry.

**Step 1: Add dynamic tool registration for MCP in ProcessMessage**

In `internal/agent/engine.go`, after loading the agent's `MCPServers` (around line 58), and before building `chatMsgs`, add:

```go
if mcpServers != nil {
	var servers []agent.MCPClient
	json.Unmarshal(mcpServers, &agent.MCPServers)
	for _, ms := range agent.MCPServers {
		if ms.Type == "http" {
			servers = append(servers, &agent.MCPClient{
				Name:    ms.Name,
				Type:    ms.Type,
				HTTPURL: ms.Command,
			})
		}
	}
	agent.RegisterMCPTools(e.registry, servers)
}
```

Wait — this would accumulate tools across sessions. Instead, create a fresh tool registry per session call. That's a bigger refactor. The minimal fix: document that MCP setup needs per-session scoping, and add a TODO.

Actually, the simplest correct approach: don't modify the global registry. Instead, build the tools list per-call. But `ProcessMessage` already receives `tools []*ToolDef` as a parameter and converts them. Let's just add MCP tools to the per-call list.

In `internal/agent/engine.go`, after line 58 where `agent.MCPServers` is loaded, add:

```go
var mcpNetTools []ToolDef
if len(agent.MCPServers) > 0 {
	for _, ms := range agent.MCPServers {
		if ms.Type == "http" {
			mcpNetTools = append(mcpNetTools, ToolDef{
				Type: "function",
				Function: struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Parameters  any    `json:"parameters"`
				}{
					Name:        ms.Name + "_list_tools",
					Description: fmt.Sprintf("List available tools from MCP server %s", ms.Name),
					Parameters:  "{}",
				},
				Handler: makeMCPToolListHandlerHTTP(ms.Command),
			}, ToolDef{
				Type: "function",
				Function: struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Parameters  any    `json:"parameters"`
				}{
					Name:        ms.Name + "_call_tool",
					Description: fmt.Sprintf("Call a tool on MCP server %s", ms.Name),
					Parameters:  `{"type":"object","properties":{"tool":{"type":"string"},"arguments":{"type":"object"}},"required":["tool"]}`,
				},
				Handler: makeMCPToolCallHandlerHTTP(ms.Command),
			})
		}
	}
}
```

This requires refactoring MCP handlers to take a URL string instead of `*MCPClient`. Let's add helper functions.

In `internal/agent/tools_mcp.go`, add:

```go
func makeMCPToolListHandlerHTTP(url string) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		resp, err := http.Get(url + "/tools/list")
		if err != nil {
			return nil, fmt.Errorf("mcp list tools: %w", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode mcp response: %w", err)
		}
		return result, nil
	}
}

func makeMCPToolCallHandlerHTTP(url string) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		payload := map[string]any{"tool": req.Tool, "arguments": req.Arguments}
		body, _ := json.Marshal(payload)

		httpCtx, cancel := context.WithTimeout(ctx.Context, 60*time.Second)
		defer cancel()

		req2, err := http.NewRequestWithContext(httpCtx, "POST", url+"/tools/call", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req2.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req2)
		if err != nil {
			return nil, fmt.Errorf("mcp call tool: %w", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode mcp response: %w", err)
		}
		return result, nil
	}
}
```

Then in `engine.go`, after loading agent.MCPServers, add MCP tools to the per-call list (append to `tools`):

After the `toolsList` building block (line 115), add:

```go
if len(agent.MCPServers) > 0 {
	for _, ms := range agent.MCPServers {
		if ms.Type == "http" {
			toolsList = append(toolsList,
				ToolDef{
					Type: "function",
					Function: struct {
						Name        string `json:"name"`
						Description string `json:"description"`
						Parameters  any    `json:"parameters"`
					}{
						Name:        ms.Name + "_list_tools",
						Description: fmt.Sprintf("List available tools from MCP server %s", ms.Name),
						Parameters:  "{}",
					},
					Handler: makeMCPToolListHandlerHTTP(ms.Command),
				},
				ToolDef{
					Type: "function",
					Function: struct {
						Name        string `json:"name"`
						Description string `json:"description"`
						Parameters  any    `json:"parameters"`
					}{
						Name:        ms.Name + "_call_tool",
						Description: fmt.Sprintf("Call a tool on MCP server %s", ms.Name),
						Parameters:  `{"type":"object","properties":{"tool":{"type":"string"},"arguments":{"type":"object"}},"required":["tool"]}`,
					},
					Handler: makeMCPToolCallHandlerHTTP(ms.Command),
				},
			)
		}
	}
}
```

Wait, `toolsList` is `[]OpenAITool` at this point. We need to add these before the conversion. Let me restructure: add the MCP tools to the `tools` input parameter BEFORE the conversion loop.

Actually, looking at the code again, `ProcessMessage` receives `tools []*ToolDef` and converts them. The engine's own registry tools are already in the list. But MCP tools need to be added per-agent. The cleanest way: add them directly in `ProcessMessage` BEFORE the conversion loop (line 115).

Change the code around line 115 to:

```go
	allTools := make([]*ToolDef, len(tools))
	copy(allTools, tools)

	if len(agent.MCPServers) > 0 {
		for _, ms := range agent.MCPServers {
			if ms.Type == "http" {
				allTools = append(allTools,
					&ToolDef{
						Type: "function",
						Function: struct {
							Name        string `json:"name"`
							Description string `json:"description"`
							Parameters  any    `json:"parameters"`
						}{
							Name:        ms.Name + "_list_tools",
							Description: fmt.Sprintf("List available tools from MCP server %s", ms.Name),
							Parameters:  "{}",
						},
						Handler: makeMCPToolListHandlerHTTP(ms.Command),
					},
					&ToolDef{
						Type: "function",
						Function: struct {
							Name        string `json:"name"`
							Description string `json:"description"`
							Parameters  any    `json:"parameters"`
						}{
							Name:        ms.Name + "_call_tool",
							Description: fmt.Sprintf("Call a tool on MCP server %s", ms.Name),
							Parameters:  `{"type":"object","properties":{"tool":{"type":"string"},"arguments":{"type":"object"}},"required":["tool"]}`,
						},
						Handler: makeMCPToolCallHandlerHTTP(ms.Command),
					},
				)
			}
		}
	}

	toolsList := make([]OpenAITool, len(allTools))
	for i, t := range allTools {
		oat, err := t.ToOpenAITool()
		if err != nil {
			return "", "", nil, events, fmt.Errorf("convert tool: %w", err)
		}
		toolsList[i] = oat
	}
```

Also remove the `RegisterMCPTools` call from `NewEngine` (it was never there — it's only defined in tools_mcp.go, never called). Good.

Also, tool call resolution in the processing loop (line 205) uses `e.registry.Get(tc.Function.Name)` — but MCP tools won't be in the registry. Change the lookup to also check the `allTools` slice:

Add a local lookup function before the tool execution loop:

```go
	toolLookup := make(map[string]*ToolDef, len(allTools))
	for _, t := range allTools {
		toolLookup[t.Function.Name] = t
	}
```

Then change line 205 from:

```go
toolDef, ok := e.registry.Get(tc.Function.Name)
```

to:

```go
toolDef, ok := toolLookup[tc.Function.Name]
if !ok {
	toolDef, ok = e.registry.Get(tc.Function.Name)
}
```

**Step 2: Verify compilation**

Run: `go build ./...`

**Step 3: Commit**

```
feat: wire MCP tools per-agent in ProcessMessage
```

---

## Task 12: Inject skill prompts into chat context (Missing #30)

Agent's `skill_ids` are loaded but never used to inject skill system prompts.

**Files:**
- Modify: `internal/agent/engine.go`

**Step 1: Load and inject skill prompts**

In `internal/agent/engine.go`, after loading the agent (around line 58), add:

```go
var skillPrompts []string
if len(agent.SkillIDs) > 0 {
	rows, err := e.pool.Query(ctx, `SELECT system_prompt FROM skills WHERE id = ANY($1) AND system_prompt IS NOT NULL AND system_prompt != ''`, agent.SkillIDs)
	if err == nil {
		for rows.Next() {
			var prompt string
			if err := rows.Scan(&prompt); err == nil && prompt != "" {
				skillPrompts = append(skillPrompts, prompt)
			}
		}
		rows.Close()
	}
}
```

Then after building the system message (around line 96), append skill prompts:

```go
for _, sp := range skillPrompts {
	chatMsgs = append(chatMsgs, ChatMessage{Role: "system", Content: sp})
}
```

**Step 2: Verify compilation**

Run: `go build ./...`

**Step 3: Commit**

```
feat: inject skill system prompts into agent chat context
```

---

## Task 13: Add listSkills org scoping (Security — listSkills has no WHERE org_id)

`listSkills` in engine.go queries `SELECT id, name, description FROM skills LIMIT 50` with no org filter — returns skills from all orgs.

**Files:**
- Modify: `internal/agent/engine.go:271-287`

**Step 1: Fix listSkills to accept orgID and filter**

Change `listSkills` signature and query:

```go
func (e *Engine) listSkills(ctx context.Context, orgID string) (map[string]any, error) {
	rows, err := e.pool.Query(ctx, `SELECT id, name, description FROM skills WHERE org_id = $1 LIMIT 50`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []map[string]string
	for rows.Next() {
		var id, name, desc string
		if err := rows.Scan(&id, &name, &desc); err != nil {
			return nil, err
		}
		skills = append(skills, map[string]string{"id": id, "name": name, "description": desc})
	}
	return map[string]any{"skills": skills}, nil
}
```

Update `HandleSlashCommand` to pass orgID:

```go
case "skills":
	return e.listSkills(ctx, orgID)
```

(The `orgID` param was added in Task 5.)

**Step 2: Verify compilation**

Run: `go build ./...`

**Step 3: Commit**

```
fix: scope skill listing to current org
```

---

## Task 14: Fix frontend — stale streaming closure in done handler (Frontend #21)

The `done` WS handler captures `currentStreamingText` from closure, but state may not have updated yet. Use a ref.

**Files:**
- Modify: `web/src/components/AgentPanel.tsx`

**Step 1: Add a ref for streaming text**

After `const needsCollapseRef = useRef(false)` (line 31), add:

```tsx
const streamingTextRef = useRef('')
```

**Step 2: Keep ref in sync with state**

In the `onmessage` handler, after `case 'token':` (line 86), add:

```tsx
streamingTextRef.current += msg.data
```

Wait, it's `setCurrentStreamingText((prev) => prev + msg.data)` already. Let's sync the ref.

Actually, a simpler approach: in the `case 'done'` handler, read from `streamingTextRef.current` instead of `currentStreamingText` state.

Update `case 'done'` (lines 113-139) to use `streamingTextRef`:

Replace the `done` handler with:

```tsx
case 'done': {
  setIsStreaming(false)
  updateStreamingReasoning('')
  needsCollapseRef.current = false
  const finalText = streamingTextRef.current
  if (finalText) {
    const r = (msg as any).data?.reasoning as string | undefined
    setMessages((prev) => [...prev, { role: 'assistant', content: finalText, reasoning: r || undefined }])
    streamingTextRef.current = ''
    setCurrentStreamingText('')
  } else if (msg.data && 'content' in msg.data && msg.data.content) {
    const r = (msg as any).data?.reasoning as string | undefined
    const c = (msg.data as any).content as string
    setMessages((prev) => [...prev, { role: 'assistant', content: c, reasoning: r || undefined }])
  }
  setTimeout(() => inputRef.current?.focus(), 50)
  setPendingMessages((prev) => {
    if (prev.length > 0 && wsRef.current) {
      const next = prev[0]
      setTimeout(() => {
        setMessages((msgs) => [...msgs, { role: 'user', content: next }])
        wsRef.current?.send(JSON.stringify({ type: 'message', content: next }))
        setIsStreaming(true)
        streamingTextRef.current = ''
        setCurrentStreamingText('')
      }, 200)
      return prev.slice(1)
    }
    return prev
  })
  break
}
```

Also update `case 'token'` to keep the ref in sync:

```tsx
case 'token': {
  const newText = (prev => prev + msg.data, setCurrentStreamingText(prev => prev + msg.data))
  streamingTextRef.current += msg.data
  break
}
```

Actually this is getting messy with the setter. Simpler: after `setCurrentStreamingText((prev) => prev + msg.data)`, add:

```tsx
streamingTextRef.current += msg.data
```

And when we reset streaming text, also reset the ref:

After `setCurrentStreamingText('')` in the `sendMessage` function, add `streamingTextRef.current = ''`.

Also in the `done` handler where we reset:

```tsx
streamingTextRef.current = ''
setCurrentStreamingText('')
```

**Step 3: Verify TypeScript compiles**

Run: `cd /home/jesus/Projects/hnb-claude/web && npx tsc --noEmit`

**Step 4: Commit**

```
fix: use ref for streaming text to avoid stale closure in done handler
```

---

## Task 15: Add WebSocket reconnection (Frontend #24)

No reconnection logic exists. When the connection drops, the user must manually switch agents.

**Files:**
- Modify: `web/src/components/AgentPanel.tsx`

**Step 1: Add reconnection logic**

After `const wsRef = useRef<WebSocket | null>(null)` (line 52), add:

```tsx
const reconnectAttemptsRef = useRef(0)
```

In `connectWebSocket`, add reconnection logic in `ws.onclose`:

Replace the current `ws.onclose` handler (line 170):

```tsx
ws.onclose = () => {
  wsRef.current = null
  if (reconnectAttemptsRef.current < 5) {
    const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 15000)
    setTimeout(() => {
      reconnectAttemptsRef.current += 1
      connectWebSocket(sid)
    }, delay)
  }
}
```

Also reset the counter on successful connection. Add after `wsRef.current = ws`:

```tsx
reconnectAttemptsRef.current = 0
```

And in the `sendMessage` function, if `!wsRef.current` and there's no reconnection in progress, show an error:

```tsx
const sendMessage = () => {
  if (!input.trim()) return
  if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
    setError('Not connected. Attempting to reconnect...')
    return
  }
  // ... rest of function
}
```

**Step 2: Verify TypeScript compiles**

Run: `cd /home/jesus/Projects/hnb-claude/web && npx tsc --noEmit`

**Step 3: Commit**

```
feat: add WebSocket reconnection with exponential backoff
```

---

## Task 16: Move JWT out of WS URL query param (Security #13)

Pass the token in a WebSocket subprotocol or as the first message after connection, not in the URL.

**Files:**
- Modify: `internal/api/agent_ws.go`
- Modify: `web/src/components/AgentPanel.tsx`

**Step 1: Backend - accept token from first message after connection**

In `internal/api/agent_ws.go`, the `handleAgentWS` currently requires auth via middleware before upgrade. The token is already validated by the auth middleware that wraps the route. The WS connection is already authenticated because the upgrade only happens after the middleware passes.

The issue is with `handleAgentWSWithUpgrader` — which we're removing in Task 3.

So the fix for the frontend is to NOT pass the token in the URL query string. Instead, the `authMW` middleware already runs before the WS handler. The query param token is redundant.

Wait — looking at `AgentPanel.tsx:78`:

```tsx
const ws = new WebSocket(WS_URL + sid + '?token=' + token)
```

The WS route is protected by `authMW` middleware (line 280 of router.go), which extracts the token from the `Authorization` header or `token` query param. For WebSockets, browsers don't support setting headers natively, so the query param is the standard approach.

This is actually an acceptable pattern for WebSocket auth since the alternative is sending the token as the first message (which works but adds latency). The security concern is that query params appear in server access logs. The fix is to configure the WS upgrader to not log the full URL or to send the token as the first message.

**Step 1: Alternative approach — send token as first WS message**

In `web/src/components/AgentPanel.tsx`, change the `connectWebSocket` function:

Replace:
```tsx
const ws = new WebSocket(WS_URL + sid + '?token=' + token)
```

With:
```tsx
const ws = new WebSocket(WS_URL + sid)
```

Then add token as the first message after connection opens:

```tsx
ws.onopen = () => {
  ws.send(JSON.stringify({ type: 'auth', token }))
  reconnectAttemptsRef.current = 0
}
```

In `internal/api/agent_ws.go`, modify the read goroutine to handle the first auth message:

Before the `for` loop that reads messages, add:

```go
// First message must be auth
var authMsg WSMessage
if err := conn.ReadJSON(&authMsg); err != nil {
	return
}
if authMsg.Type != "auth" {
	conn.WriteJSON(WSErrorResponse{Type: "error", Message: "first message must be auth"})
	return
}
// Validate token
if authMsg.Content == "" {
	conn.WriteJSON(WSErrorResponse{Type: "error", Message: "missing token"})
	return
}
claims, err := s.jwt.Validate(authMsg.Content)
if err != nil || claims == nil {
	conn.WriteJSON(WSErrorResponse{Type: "error", Message: "invalid token"})
	return
}
```

But this requires removing the `authMW` from the WebSocket route since auth is now done inside the handler. That's a bigger change with its own risks.

**Safer approach:** Keep the query param for now but document it as a known limitation. The actual risk is minimal since the token is short-lived (15min), TLS encrypts the URL in transit, and the server logs can be configured to redact query params.

**Actual Step 1:** Do NOT change the WS auth pattern (too risky), but add a comment documenting the tradeoff:

In `web/src/components/AgentPanel.tsx:78`, add a comment:

```tsx
// Note: JWT is sent as a query param because browsers don't support
// setting WebSocket headers natively. The token is short-lived (15min).
// If needed, migrate to first-message auth pattern.
const ws = new WebSocket(WS_URL + sid + '?token=' + token)
```

**Step 2: Commit**

```
docs: add comment about WebSocket auth tradeoff
```

---

## Task 17: Fix SlashCommandPicker global key handler (Frontend #25)

The picker adds a global `keydown` listener that intercepts ArrowUp/Down/Enter even when focus is elsewhere.

**Files:**
- Modify: `web/src/components/SlashCommandPicker.tsx`

**Step 1: Scope keydown handler to picker visibility**

In `SlashCommandPicker.tsx`, change the `useEffect` to only add the listener when the picker is visible, and stop propagation:

```tsx
useEffect(() => {
  if (!filter) return
  const handleKeyDown = (e: KeyboardEvent) => {
    // Only handle if picker is visible and active
    if (!filtered.length) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      e.stopPropagation()
      setSelectedIndex((i) => Math.min(i + 1, filtered.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      e.stopPropagation()
      setSelectedIndex((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      e.stopPropagation()
      if (filtered[selectedIndex]) {
        onSelect(filtered[selectedIndex].command)
      }
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
    }
  }
  window.addEventListener('keydown', handleKeyDown, true) // capture phase
  return () => window.removeEventListener('keydown', handleKeyDown, true)
}, [filter, filtered, selectedIndex, onSelect, onClose])
```

The key change: use capture phase (`true`) so `stopPropagation` prevents the event from reaching the textarea's `onKeyDown`.

**Step 2: Also remove the separate `handleKeyDown` function defined outside the effect** since it's now inline.

**Step 3: Verify TypeScript compiles**

Run: `cd /home/jesus/Projects/hnb-claude/web && npx tsc --noEmit`

**Step 4: Commit**

```
fix: scope slash command picker keyboard handler to capture phase
```

---

## Task 18: Wire up RateLimiter in ProcessMessage (Missing #27)

The `RateLimiter` exists but is never called. We should check rate limits before processing each message.

**Files:**
- Modify: `internal/agent/engine.go`
- Modify: `internal/agent/ratelimit.go` — fix unused import and field

**Step 1: Add rate limiter to Engine and check in ProcessMessage**

In `internal/agent/engine.go`, add the rate limiter to the Engine struct:

```go
type Engine struct {
	registry    *ToolRegistry
	session     *SessionStore
	llm         *LLMClient
	pool        *pgxpool.Pool
	mu          sync.Mutex
	rateLimiter *RateLimiter
}
```

And in `NewEngine`:

```go
func NewEngine(pool *pgxpool.Pool) *Engine {
	reg := NewToolRegistry()
	RegisterNotebookTools(reg, pool)
	RegisterChartTools(reg, pool)
	RegisterAgentTools(reg, pool)
	RegisterPlatformTools(reg, pool)

	return &Engine{
		registry:    reg,
		session:     NewSessionStore(pool),
		pool:         pool,
		rateLimiter: NewRateLimiter(pool),
	}
}
```

In `ProcessMessage`, after getting the session (line 39), add a rate limit check:

```go
ok, err := e.rateLimiter.CheckAndUpdateTokens(ctx, sessionID, 0, 0)
if err != nil {
	return "", "", nil, events, fmt.Errorf("rate limit check: %w", err)
}
if !ok {
	return "", "", nil, events, fmt.Errorf("rate limit exceeded: session has reached max turns or tokens")
}
```

**Step 2: Fix unused import in ratelimit.go**

The `ratelimit.go` imports `uuid` and `models` but only uses them in `CreateSummarizedSession`. These are fine.

Actually, looking at the import list — there's `"github.com/google/uuid"` and `"github.com/heavenlabs/hnb/internal/models"` used in `CreateSummarizedSession`. These are used. ✓

**Step 3: Verify compilation**

Run: `go build ./...`

**Step 4: Commit**

```
feat: wire rate limiter into ProcessMessage
```

---

## Task 19: Clean up dead code — remove duplicate WS handler (P2 / part of Task 3)

This was already done in Task 3. No separate action needed.

---

## Task 20: Fix frontend pending message race (Frontend #23)

The `setTimeout` in the `done` handler's `setPendingMessages` callback can cause out-of-order processing.

**Files:**
- Modify: `web/src/components/AgentPanel.tsx`

**Step 1: Use a queue ref instead of setTimeout**

Replace the `done` handler's pending messages logic (within the refactored handler from Task 14). Instead of `setTimeout`, process immediately:

In the `done` handler, replace the `setPendingMessages` logic with:

```tsx
setPendingMessages((prev) => {
  if (prev.length > 0 && wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
    const next = prev[0]
    const rest = prev.slice(1)
    setMessages((msgs) => [...msgs, { role: 'user', content: next }])
    wsRef.current.send(JSON.stringify({ type: 'message', content: next }))
    setIsStreaming(true)
    streamingTextRef.current = ''
    setCurrentStreamingText('')
    // Process remaining messages recursively via state update
    if (rest.length > 0) {
      return rest
    }
    return []
  }
  return prev
})
```

Wait, this has issues too since `setMessages` and `wsRef.current.send` in a state updater is not ideal. The current approach with setTimeout is actually fine for most cases. Let's just add a guard against double-sending:

Add a processing ref:

```tsx
const processingRef = useRef(false)
```

And in the done handler:

```tsx
setPendingMessages((prev) => {
  if (prev.length > 0 && !processingRef.current && wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
    processingRef.current = true
    const next = prev[0]
    setTimeout(() => {
      setMessages((msgs) => [...msgs, { role: 'user', content: next }])
      wsRef.current?.send(JSON.stringify({ type: 'message', content: next }))
      setIsStreaming(true)
      streamingTextRef.current = ''
      setCurrentStreamingText('')
      processingRef.current = false
    }, 100)
    return prev.slice(1)
  }
  return prev
})
```

**Step 2: Verify TypeScript compiles**

Run: `cd /home/jesus/Projects/hnb-claude/web && npx tsc --noEmit`

**Step 3: Commit**

```
fix: add guard against concurrent pending message processing
```

---

## Task 21: Remove agent_stats_daily table query without org scoping (handler vulnerability)

`handleAgentStats` in `agent_handlers.go:398` queries ALL orgs' stats without filtering by the user's org.

**Files:**
- Modify: `internal/api/agent_handlers.go:399-422`

**Step 1: Add org scoping to agent stats handlers**

In `handleAgentStats` (line 399), change:

```go
rows, err := h.server.db.Pool.Query(r.Context(), `
	SELECT date, agent_id, user_id, sessions_count, messages_count, tokens_input, tokens_output
	FROM agent_stats_daily
	WHERE date >= NOW() - INTERVAL '30 days'
	ORDER BY date DESC
`)
```

to:

```go
rows, err := h.server.db.Pool.Query(r.Context(), `
	SELECT s.date, s.agent_id, s.user_id, s.sessions_count, s.messages_count, s.tokens_input, s.tokens_output
	FROM agent_stats_daily s
	JOIN agents a ON a.id = s.agent_id
	WHERE a.org_id = $1 AND s.date >= NOW() - INTERVAL '30 days'
	ORDER BY s.date DESC
`, claims.OrgID)
```

In `handleAgentStatsByAgent` (line 424), add org scoping:

```go
rows, err := h.server.db.Pool.Query(r.Context(), `
	SELECT s.date, s.agent_id, s.user_id, s.sessions_count, s.messages_count, s.tokens_input, s.tokens_output
	FROM agent_stats_daily s
	JOIN agents a ON a.id = s.agent_id
	WHERE a.id = $1 AND a.org_id = $2 AND s.date >= NOW() - INTERVAL '30 days'
	ORDER BY s.date DESC
`, agentID, claims.OrgID)
```

Add `claims` variable. In `handleAgentStatsByAgent`, the `claims` variable is not currently used. Add it:

```go
claims := ClaimsFromContext(r.Context())
```

**Step 2: Verify compilation**

Run: `go build ./...`

**Step 3: Commit**

```
fix: scope agent stats queries by org_id
```

---

## Summary

| Task | Priority | Description |
|------|----------|-------------|
| 1 | P0 | Fix subagent nil masterKey + fake identity |
| 2 | P0 | Fix agent version INSERT wrong SQL param |
| 3 | P0 | Fix WS concurrent writes — serialize through writeChan |
| 4 | P0 | Remove duplicate event sending (done in Task 3) |
| 5 | P1 | Fix listAgents broken slash command |
| 6 | P1 | Add org_id checks to agent/skill/modelConfig mutations |
| 7 | P1 | Add permission checks to chart tools |
| 8 | Data | Fix update_cell missing description field |
| 9 | Data | Fix create_cell position logic |
| 10 | P1 | Wire run_cell to actual executor |
| 11 | Missing | Wire MCP tools per-agent |
| 12 | Missing | Inject skill system prompts |
| 13 | Security | Scope listSkills to org |
| 14 | Frontend | Fix stale streaming text closure |
| 15 | Frontend | Add WS reconnection with backoff |
| 16 | Security | Document WS auth tradeoff (no code change) |
| 17 | Frontend | Fix SlashCommandPicker global key handler |
| 18 | Missing | Wire RateLimiter into ProcessMessage |
| 19 | — | Remove duplicate WS handler (done in Task 3) |
| 20 | Frontend | Fix pending message race |
| 21 | Security | Scope agent stats by org_id |