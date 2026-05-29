package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/models"
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
