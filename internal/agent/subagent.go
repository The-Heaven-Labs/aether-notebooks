package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/models"
)

const MaxSubagentParallelism = 3
const MaxSubagentTurns = 20

type SubagentResult struct {
	TaskID    string
	Status    string
	Result    any
	Error     string
	TokensIn  int
	TokensOut int
}

type SubagentTaskConfig struct {
	ID      string
	Goal    string
	Context map[string]any
	AgentID *string
}

func (e *Engine) SpawnSubagents(ctx context.Context, parentSessionID string, tasks []SubagentTaskConfig, masterKey []byte, onUpdate func([]SubagentResult)) error {
	var parentUserID, parentAgentID string
	err := e.pool.QueryRow(ctx, `SELECT user_id, agent_id FROM agent_sessions WHERE id = $1`, parentSessionID).Scan(&parentUserID, &parentAgentID)
	if err != nil {
		return fmt.Errorf("get parent session: %w", err)
	}
	var orgID string
	err = e.pool.QueryRow(ctx, `SELECT org_id FROM agents WHERE id = $1`, parentAgentID).Scan(&orgID)
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

func (e *Engine) runSubagent(ctx context.Context, parentSessionID string, task SubagentTaskConfig, parentUserID, parentOrgID string, masterKey []byte) SubagentResult {
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

		for _, tc := range choice.Message.ToolCalls {
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

	return SubagentResult{TaskID: taskID, Status: "failed", Error: "max turns reached"}
}

// RunQueuedTasks runs subagent tasks that were already inserted into the DB
// (status 'queued'). It updates each task's status as it progresses and
// broadcasts events via broadcastFn so the frontend can track progress.
// Returns the results of all subagent tasks.
func (e *Engine) RunQueuedTasks(ctx context.Context, parentSessionID string, taskIDs []string, masterKey []byte, broadcastFn func(notebookID string, msg any), notebookID string, llmClient *LLMClient) []SubagentResult {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("RunQueuedTasks: panic", "recover", r)
		}
	}()

	var parentUserID, parentAgentID string
	if err := e.pool.QueryRow(ctx, `SELECT user_id, agent_id FROM agent_sessions WHERE id = $1`, parentSessionID).Scan(&parentUserID, &parentAgentID); err != nil {
		slog.Error("RunQueuedTasks: get parent session", "error", err)
		return nil
	}
	var orgID string
	var maxSubagentTurns int
	if err := e.pool.QueryRow(ctx, `SELECT org_id FROM agents WHERE id = $1`, parentAgentID).Scan(&orgID); err != nil {
		slog.Error("RunQueuedTasks: get agent org", "error", err)
		return nil
	}
	// Look up the user's actual org role so admin users bypass ACL checks
	var orgRole string
	if err := e.pool.QueryRow(ctx, `SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, parentUserID).Scan(&orgRole); err != nil {
		orgRole = "editor"
	}
	// Read agent's max_subagent_turns setting
	e.pool.QueryRow(ctx, `SELECT COALESCE(max_subagent_turns, 20) FROM agents WHERE id = $1`, parentAgentID).Scan(&maxSubagentTurns)
	if maxSubagentTurns <= 0 {
		maxSubagentTurns = 20
	}

	sem := make(chan struct{}, MaxSubagentParallelism)
	var wg sync.WaitGroup
	var resultsMu sync.Mutex
	allResults := make([]SubagentResult, 0, len(taskIDs))

	for _, taskID := range taskIDs {
		// Fetch task details from DB
		var goal string
		var contextJSON []byte
		err := e.pool.QueryRow(ctx, `SELECT goal, context FROM subagent_tasks WHERE id = $1`, taskID).Scan(&goal, &contextJSON)
		if err != nil {
			slog.Error("RunQueuedTasks: fetch task", "task_id", taskID, "error", err)
			continue
		}

		var taskCtx map[string]any
		if contextJSON != nil {
			json.Unmarshal(contextJSON, &taskCtx)
		}

		sem <- struct{}{}
		wg.Add(1)

		go func(tid string, g string, tc map[string]any) {
			defer wg.Done()
			defer func() { <-sem }()
			subagentStart := time.Now()

			// Update status to running
			_, _ = e.pool.Exec(ctx, `UPDATE subagent_tasks SET status = 'running' WHERE id = $1`, tid)
			statusEvent := map[string]any{"type": "subagent_status", "task_id": tid, "status": "running", "goal": g, "duration_ms": 0}
			if broadcastFn != nil {
				broadcastFn(notebookID, statusEvent)
			}
			e.PublishSessionEvent(parentSessionID, statusEvent)

			// Run the subagent LLM loop with the agent's tools
			// Filter out spawn_subagents to prevent recursive subagent spawning
			allTools := e.registry.List()
			agentTools := make([]*ToolDef, 0, len(allTools))
			for _, t := range allTools {
				if t.Function.Name != "spawn_subagents" {
					agentTools = append(agentTools, t)
				}
			}
			result := e.runSubagentLoop(ctx, parentSessionID, tid, g, parentUserID, orgID, orgRole, masterKey, llmClient, agentTools, maxSubagentTurns)
			slog.Debug("subagent completed", "task_id", tid, "status", result.Status, "error", result.Error, "result_type", fmt.Sprintf("%T", result.Result))

			// Update final status
			status := "completed"
			if result.Error != "" {
				status = "failed"
			}
			storedResult := result.Result
			if storedResult == nil {
				storedResult = map[string]any{}
			}
			if rMap, ok := storedResult.(map[string]any); ok && result.Error != "" {
				rMap["error"] = result.Error
			}
			_, _ = e.pool.Exec(ctx, `
				UPDATE subagent_tasks SET status = $1, result = $2, tokens_input = $3, tokens_output = $4, completed_at = NOW()
				WHERE id = $5
			`, status, storedResult, result.TokensIn, result.TokensOut, tid)

			subagentDuration := int(time.Since(subagentStart).Milliseconds())
			completionEvent := map[string]any{
				"type":        "subagent_status",
				"task_id":     tid,
				"status":      status,
				"goal":        g,
				"result":      result.Result,
				"error":       result.Error,
				"duration_ms": subagentDuration,
			}
			if broadcastFn != nil {
				broadcastFn(notebookID, completionEvent)
			}
			e.PublishSessionEvent(parentSessionID, completionEvent)

			// Persist subagent status as an agent_message so it survives page refresh
			// and appears in reconnect_sync / history.
			tcJSON, _ := json.Marshal([]map[string]any{{"name": g, "arguments": map[string]any{"task_id": tid, "status": status, "result": result.Result, "error": result.Error}}})
			e.pool.Exec(ctx, `INSERT INTO agent_messages (session_id, role, content, tool_calls, created_at) VALUES ($1, 'subagent', $2, $3, NOW())`,
				parentSessionID, tid, tcJSON)

			resultsMu.Lock()
			allResults = append(allResults, result)
			resultsMu.Unlock()
		}(taskID, goal, taskCtx)
	}

	wg.Wait()
	return allResults
}

// runSubagentLoop runs the LLM loop for a subagent task that already exists in the DB.
// Unlike runSubagent, it does NOT insert the task record.
func (e *Engine) runSubagentLoop(ctx context.Context, parentSessionID string, taskID string, goal string, parentUserID, parentOrgID, parentOrgRole string, masterKey []byte, subagentLLM *LLMClient, agentTools []*ToolDef, maxTurns int) (result SubagentResult) {
	// saveMsg saves a message to the subagent conversation immediately.
	// For tool messages, content is saved as a JSON object with the tool
	// name and result, which the frontend parses for display.
	saveMsg := func(role, content, toolCallID string, toolCalls []ToolCall, reasoning string, durationMs int, toolResult ...string) {
		storeContent := content
		if role == "tool" && len(toolResult) > 0 {
			// Store tool name in content and result in a JSON envelope
			b, _ := json.Marshal(map[string]string{"name": content, "result": toolResult[0]})
			storeContent = string(b)
		}
		tcJSON, _ := json.Marshal(toolCalls)
		var tcID *string
		if toolCallID != "" {
			tcID = &toolCallID
		}
		dur := durationMs
		if dur == 0 {
			dur = 0
		}
		e.pool.Exec(ctx, `INSERT INTO subagent_messages (subagent_task_id, role, content, tool_call_id, tool_calls, reasoning_content, duration_ms, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())`,
			taskID, role, storeContent, tcID, tcJSON, reasoning, dur)
		event := map[string]any{
			"type":              "subagent_message",
			"task_id":           taskID,
			"role":              role,
			"content":           storeContent,
			"duration_ms":       dur,
			"tool_call_id":      toolCallID,
			"reasoning_content": reasoning,
		}
		var tcList []map[string]any
		if len(toolCalls) > 0 {
			b, _ := json.Marshal(toolCalls)
			json.Unmarshal(b, &tcList)
		}
		event["tool_calls"] = tcList
		e.PublishSessionEvent(parentSessionID, event)
	}

	messages := []ChatMessage{
		{Role: "user", Content: goal},
	}
	saveMsg("user", goal, "", nil, "", 0)

	for turn := 0; turn < maxTurns; turn++ {
		if subagentLLM == nil {
			saveMsg("assistant", "Error: no LLM client configured for subagent", "", nil, "", 0)
			return SubagentResult{TaskID: taskID, Status: "failed", Error: "no LLM client configured for subagent"}
		}

		// Build tool definitions for the subagent LLM
		subagentTools := make([]OpenAITool, 0, len(agentTools))
		for _, t := range agentTools {
			if oat, err := t.ToOpenAITool(); err == nil {
				subagentTools = append(subagentTools, oat)
			}
		}
		chatStart := time.Now()
		resp, err := subagentLLM.Chat(ctx, messages, subagentTools, masterKey)
		chatDuration := int(time.Since(chatStart).Milliseconds())
		if err != nil {
			errMsg := fmt.Sprintf("Error: %s", err.Error())
			messages = append(messages, ChatMessage{Role: "assistant", Content: errMsg})
			saveMsg("assistant", errMsg, "", nil, "", chatDuration)
			return SubagentResult{TaskID: taskID, Status: "failed", Error: err.Error()}
		}

		if len(resp.Choices) == 0 {
			messages = append(messages, ChatMessage{Role: "assistant", Content: "Error: LLM returned an empty response"})
			saveMsg("assistant", "Error: LLM returned an empty response", "", nil, "", chatDuration)
			return SubagentResult{TaskID: taskID, Status: "failed", Error: "no choices in response"}
		}

		choice := resp.Choices[0]

		// Append assistant message to the conversation
		// Note: tool_calls come from Message.ToolCalls (OpenAI API) not Choice.ToolCalls
		assistantMsg := ChatMessage{
			Role:             "assistant",
			Content:          choice.Message.Content,
			ToolCalls:        choice.Message.ToolCalls,
			ReasoningContent: choice.Message.ReasoningContent,
		}
		messages = append(messages, assistantMsg)
		saveMsg("assistant", assistantMsg.Content, "", assistantMsg.ToolCalls, assistantMsg.ReasoningContent, chatDuration)

		if choice.Message.Content != "" {
			return SubagentResult{
				TaskID:    taskID,
				Status:    "completed",
				Result:    choice.Message.Content,
				TokensIn:  resp.Usage.PromptTokens,
				TokensOut: resp.Usage.CompletionTokens,
			}
		}

		for _, tc := range choice.Message.ToolCalls {
			toolDef, ok := e.registry.Get(tc.Function.Name)
			if !ok {
				messages = append(messages, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: fmt.Sprintf("unknown tool: %s", tc.Function.Name)})
				saveMsg("tool", tc.Function.Name, tc.ID, nil, "", 0, fmt.Sprintf("unknown tool: %s", tc.Function.Name))
				continue
			}

			toolStart := time.Now()
			result, err := toolDef.Handler(json.RawMessage(tc.Function.Arguments), &ToolContext{
				Context:    ctx,
				UserID:     parentUserID,
				OrgID:      parentOrgID,
				OrgRole:    parentOrgRole,
				NotebookID: taskID,
				SessionID:  parentSessionID,
				DB:         e.pool,
				MasterKey:  masterKey,
			})
			toolDuration := int(time.Since(toolStart).Milliseconds())

			// Build tool call metadata for the tool_calls JSONB column
			toolCallMeta := []ToolCall{tc}

			if err != nil {
				messages = append(messages, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: err.Error()})
				saveMsg("tool", tc.Function.Name, "", toolCallMeta, "", toolDuration, err.Error())
			} else {
				resultJSON, _ := json.Marshal(result)
				resultStr := string(resultJSON)
				messages = append(messages, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: resultStr})
				saveMsg("tool", tc.Function.Name, "", toolCallMeta, "", toolDuration, resultStr)
			}
		}
	}

	saveMsg("assistant", "Subagent reached the maximum number of turns and did not complete its task.", "", nil, "", 0)
	return SubagentResult{TaskID: taskID, Status: "failed", Error: "max turns reached"}
}

func (e *Engine) GetSubagentTasks(ctx context.Context, parentSessionID string) ([]models.SubagentTask, error) {
	rows, err := e.pool.Query(ctx, `
		SELECT id, parent_session_id, parent_message_id, agent_id, goal, context, status, result, tokens_input, tokens_output, created_at, completed_at
		FROM subagent_tasks WHERE parent_session_id = $1 ORDER BY created_at
	`, parentSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []models.SubagentTask
	for rows.Next() {
		var t models.SubagentTask
		var goal, status string
		var contextJSON, resultJSON []byte
		rows.Scan(&t.ID, &t.ParentSessionID, &t.ParentMessageID, &t.AgentID, &goal, &contextJSON, &status, &resultJSON, &t.TokensInput, &t.TokensOutput, &t.CreatedAt, &t.CompletedAt)
		t.Goal = goal
		t.Status = status
		if contextJSON != nil {
			json.Unmarshal(contextJSON, &t.Context)
		}
		if resultJSON != nil {
			json.Unmarshal(resultJSON, &t.Result)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
