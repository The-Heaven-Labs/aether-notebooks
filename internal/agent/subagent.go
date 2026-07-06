package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

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

	return SubagentResult{TaskID: taskID, Status: "completed", Result: "max turns reached"}
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
	if err := e.pool.QueryRow(ctx, `SELECT org_id FROM agents WHERE id = $1`, parentAgentID).Scan(&orgID); err != nil {
		slog.Error("RunQueuedTasks: get agent org", "error", err)
		return nil
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

			// Update status to running
			_, _ = e.pool.Exec(ctx, `UPDATE subagent_tasks SET status = 'running' WHERE id = $1`, tid)
			statusEvent := map[string]any{"type": "subagent_status", "task_id": tid, "status": "running", "goal": g}
			if broadcastFn != nil {
				broadcastFn(notebookID, statusEvent)
			}
			e.PublishSessionEvent(parentSessionID, statusEvent)

			// Run the subagent LLM loop
			result := e.runSubagentLoop(ctx, parentSessionID, tid, g, parentUserID, orgID, masterKey, llmClient)

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

			completionEvent := map[string]any{
				"type":    "subagent_status",
				"task_id": tid,
				"status":  status,
				"goal":    g,
				"result":  result.Result,
			}
			if broadcastFn != nil {
				broadcastFn(notebookID, completionEvent)
			}
			e.PublishSessionEvent(parentSessionID, completionEvent)

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
func (e *Engine) runSubagentLoop(ctx context.Context, parentSessionID string, taskID string, goal string, parentUserID, parentOrgID string, masterKey []byte, subagentLLM *LLMClient) (result SubagentResult) {
	// saveMsg saves a message to the subagent conversation immediately.
	// For tool messages, content is saved as a JSON object with the tool
	// name and result, which the frontend parses for display.
	saveMsg := func(role, content, toolCallID string, toolCalls []ToolCall, reasoning string, toolResult ...string) {
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
		e.pool.Exec(ctx, `INSERT INTO subagent_messages (subagent_task_id, role, content, tool_call_id, tool_calls, reasoning_content, created_at) VALUES ($1,$2,$3,$4,$5,$6,NOW())`,
			taskID, role, storeContent, tcID, tcJSON, reasoning)
		event := map[string]any{
			"type":              "subagent_message",
			"task_id":           taskID,
			"role":              role,
			"content":           storeContent,
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
	saveMsg("user", goal, "", nil, "")

	for turn := 0; turn < MaxSubagentTurns; turn++ {
		if subagentLLM == nil {
			return SubagentResult{TaskID: taskID, Status: "failed", Error: "no LLM client configured for subagent"}
		}

		resp, err := subagentLLM.Chat(ctx, messages, nil, masterKey)
		if err != nil {
			errMsg := fmt.Sprintf("Error: %s", err.Error())
			messages = append(messages, ChatMessage{Role: "assistant", Content: errMsg})
			saveMsg("assistant", errMsg, "", nil, "")
			return SubagentResult{TaskID: taskID, Status: "failed", Error: err.Error()}
		}

		if len(resp.Choices) == 0 {
			messages = append(messages, ChatMessage{Role: "assistant", Content: "Error: LLM returned an empty response"})
			saveMsg("assistant", "Error: LLM returned an empty response", "", nil, "")
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
		saveMsg("assistant", assistantMsg.Content, "", assistantMsg.ToolCalls, assistantMsg.ReasoningContent)

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
				saveMsg("tool", tc.Function.Name, tc.ID, nil, "", fmt.Sprintf("unknown tool: %s", tc.Function.Name))
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
				saveMsg("tool", tc.Function.Name, tc.ID, nil, "", err.Error())
			} else {
				resultJSON, _ := json.Marshal(result)
				resultStr := string(resultJSON)
				messages = append(messages, ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: resultStr})
				saveMsg("tool", tc.Function.Name, tc.ID, nil, "", resultStr)
			}
		}
	}

	return SubagentResult{TaskID: taskID, Status: "completed", Result: "max turns reached"}
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
