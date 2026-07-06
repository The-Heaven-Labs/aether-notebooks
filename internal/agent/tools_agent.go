package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterAgentTools(reg *ToolRegistry, pool *pgxpool.Pool, engine *Engine) {
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "list_skills",
			Description: "List all available skills in the organization. Returns skill names, descriptions, and capabilities.",
			Parameters:  `{"type":"object","properties":{},"required":[]}`,
		},
		Handler: makeListSkillsHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "load_skill",
			Description: "Load a skill's full instructions. Use this when a task matches a skill's description to get the detailed workflow.",
			Parameters:  `{"type":"object","properties":{"name":{"type":"string","description":"Name of the skill to load"}},"required":["name"]}`,
		},
		Handler: makeLoadSkillHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_agent",
			Description: "Modify this agent's own config",
			Parameters:  `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"system_prompt":{"type":"string"},"skill_ids":{"type":"array","items":{"type":"string"}}}}`,
		},
		Handler: makeUpdateAgentHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_skill",
			Description: "Save a reusable skill",
			Parameters:  `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"system_prompt":{"type":"string"}},"required":["name","system_prompt"]}`,
		},
		Handler: makeCreateSkillHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "spawn_subagents",
			Description: "Execute multiple independent tasks in parallel by launching separate AI sub-agents. Use this when a request has multiple distinct, independent subtasks that can be worked on simultaneously (e.g., exploring different database schemas, writing separate code modules, researching multiple topics). Each sub-agent gets its own goal and runs independently. Returns a task_id for each sub-agent so you can check on their progress. Unlike create_tasks (which only tracks to-do items), spawn_subagents actually runs work in parallel. Maximum 5 sub-agents per call.",
			Parameters:  `{"type":"object","properties":{"tasks":{"type":"array","description":"List of independent sub-tasks to execute in parallel","minItems":1,"maxItems":5,"items":{"type":"object","properties":{"id":{"type":"string","description":"Short unique identifier for this sub-task (e.g. 'explore_schema', 'build_query', 'research_api')"},"goal":{"type":"string","description":"Clear, specific instruction for the sub-agent. Include what data to query, what to build, or what question to answer."},"agent_id":{"type":"string","description":"Optional agent ID to use for this sub-task. Omit to use the current agent."}},"required":["id","goal"]}}},"required":["tasks"]}`,
		},
		Handler: makeSpawnSubagentsHandler(pool, engine),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_skill",
			Description: "Modify a skill",
			Parameters:  `{"type":"object","properties":{"skill_id":{"type":"string"},"name":{"type":"string"},"system_prompt":{"type":"string"}},"required":["skill_id"]}`,
		},
		Handler: makeUpdateSkillHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "create_tasks",
			Description: "Create a task list for the current session to track progress on complex work. Use this to break down complex requests into smaller, trackable tasks.",
			Parameters:  `{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string","description":"Short unique identifier for the task"},"description":{"type":"string","description":"What needs to be done"}},"required":["id","description"]}}},"required":["tasks"]}`,
		},
		Handler: makeCreateTasksHandler(),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_task",
			Description: "Update a task's status. Valid statuses: pending, in_progress, done.",
			Parameters:  `{"type":"object","properties":{"task_id":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","done"]}},"required":["task_id","status"]}`,
		},
		Handler: makeUpdateTaskHandler(),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "get_tasks",
			Description: "Get the current task list for the session. Use this to check what tasks are pending and what has been completed.",
			Parameters:  `{"type":"object","properties":{}}`,
		},
		Handler: makeGetTasksHandler(),
	})
}

func makeListSkillsHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		rows, err := pool.Query(ctx.Context, `SELECT id, name, description, system_prompt FROM skills WHERE org_id = $1`, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("list skills: %w", err)
		}
		defer rows.Close()
		var skills []map[string]string
		for rows.Next() {
			var id, name, desc, prompt string
			if err := rows.Scan(&id, &name, &desc, &prompt); err != nil {
				return nil, fmt.Errorf("scan skill: %w", err)
			}
			skills = append(skills, map[string]string{
				"id":           id,
				"name":         name,
				"description":  desc,
				"capabilities": prompt,
			})
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("list skills iter: %w", err)
		}
		if skills == nil {
			skills = []map[string]string{}
		}
		return map[string]any{"skills": skills}, nil
	}
}

func makeLoadSkillHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if req.Name == "" {
			return nil, fmt.Errorf("skill name is required")
		}

		// Look up skill by name (case-insensitive, handle spaces/hyphens)
		var systemPrompt string
		var description string
		err := pool.QueryRow(ctx.Context, `
			SELECT system_prompt, description FROM skills 
			WHERE org_id = $1 AND (
				LOWER(REPLACE(name, ' ', '-')) = LOWER(REPLACE($2, ' ', '-'))
				OR LOWER(name) = LOWER($2)
			)
			LIMIT 1
		`, ctx.OrgID, req.Name).Scan(&systemPrompt, &description)
		if err != nil {
			return nil, fmt.Errorf("skill '%s' not found", req.Name)
		}

		if systemPrompt == "" {
			return map[string]any{
				"name":        req.Name,
				"description": description,
				"content":     "(no instructions defined for this skill)",
			}, nil
		}

		return map[string]any{
			"name":        req.Name,
			"description": description,
			"content":     systemPrompt,
		}, nil
	}
}

func makeUpdateAgentHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Name         *string  `json:"name"`
			Description  *string  `json:"description"`
			SystemPrompt *string  `json:"system_prompt"`
			SkillIDs     []string `json:"skill_ids"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		var agentID string
		err := pool.QueryRow(ctx.Context, `
			SELECT agent_id FROM agent_sessions WHERE id = $1
		`, ctx.SessionID).Scan(&agentID)
		if err != nil {
			return nil, fmt.Errorf("get agent from session: %w", err)
		}

		skillIDsJSON, _ := json.Marshal(req.SkillIDs)

		_, err = pool.Exec(ctx.Context, `
			UPDATE agents SET
				name = COALESCE($2, name),
				description = COALESCE($3, description),
				system_prompt = COALESCE($4, system_prompt),
				skill_ids = COALESCE($5, skill_ids),
				updated_at = NOW()
			WHERE id = $1 AND org_id = $6
		`, agentID, req.Name, req.Description, req.SystemPrompt, skillIDsJSON, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("update agent: %w", err)
		}

		var version int
		if err := pool.QueryRow(ctx.Context, `SELECT COALESCE(MAX(version), 0) + 1 FROM agent_versions WHERE agent_id = $1`, agentID).Scan(&version); err != nil {
			slog.Warn("query agent version", "error", err)
		}
		if _, err := pool.Exec(ctx.Context, `
			INSERT INTO agent_versions (id, agent_id, version, name, description, system_prompt, skill_ids, changed_by, change_reason, created_at)
			SELECT $1, $2, $3, name, description, system_prompt, skill_ids, $4, 'agent_self_modification', NOW()
			FROM agents WHERE id = $2
		`, uuid.New().String(), agentID, version, ctx.UserID); err != nil {
			slog.Warn("record agent version", "error", err)
		}

		return map[string]any{"agent_id": agentID, "status": "updated"}, nil
	}
}

func makeCreateSkillHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			SystemPrompt string `json:"system_prompt"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		skillID := uuid.New().String()

		_, err := pool.Exec(ctx.Context, `
			INSERT INTO skills (id, org_id, name, description, system_prompt, created_by, created_at, updated_at)
			SELECT $1, org_id, $2, $3, $4, $5, NOW(), NOW()
			FROM agents WHERE id = (SELECT agent_id FROM agent_sessions WHERE id = $6)
		`, skillID, req.Name, req.Description, req.SystemPrompt, ctx.UserID, ctx.SessionID)
		if err != nil {
			return nil, fmt.Errorf("create skill: %w", err)
		}

		return map[string]any{"skill_id": skillID}, nil
	}
}

func makeSpawnSubagentsHandler(pool *pgxpool.Pool, engine *Engine) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Tasks []struct {
				ID      string         `json:"id"`
				Goal    string         `json:"goal"`
				Context map[string]any `json:"context"`
				AgentID *string        `json:"agent_id"`
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
			_, err := pool.Exec(ctx.Context, `
				INSERT INTO subagent_tasks (id, parent_session_id, goal, context, status, created_at)
				VALUES ($1, $2, $3, $4, 'queued', NOW())
			`, taskID, ctx.SessionID, t.Goal, contextJSON)
			if err != nil {
				return nil, fmt.Errorf("create subagent task: %w", err)
			}
		}

		// Launch subagent execution in background goroutine
		// Copy masterKey so the goroutine has its own slice
		mk := make([]byte, len(ctx.MasterKey))
		copy(mk, ctx.MasterKey)

		sessionID := ctx.SessionID
		notebookID := ctx.NotebookID
		broadcastFn := ctx.BroadcastFunc
		idsCopy := make([]string, len(taskIDs))
		copy(idsCopy, taskIDs)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("spawn_subagents: panic in background goroutine", "recover", r)
				}
			}()
			bgCtx := context.Background()
			engine.RunQueuedTasks(bgCtx, sessionID, idsCopy, mk, broadcastFn, notebookID)
		}()

		return map[string]any{"task_ids": taskIDs, "status": "spawned"}, nil
	}
}

func makeUpdateSkillHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			SkillID      string  `json:"skill_id"`
			Name         *string `json:"name"`
			SystemPrompt *string `json:"system_prompt"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if err := ctx.CheckPermission("skill", req.SkillID, "edit"); err != nil {
			return nil, err
		}

		_, err := pool.Exec(ctx.Context, `
			UPDATE skills SET
				name = COALESCE($2, name),
				system_prompt = COALESCE($3, system_prompt),
				updated_at = NOW()
			WHERE id = $1 AND org_id = $4
		`, req.SkillID, req.Name, req.SystemPrompt, ctx.OrgID)
		if err != nil {
			return nil, fmt.Errorf("update skill: %w", err)
		}

		return map[string]any{"skill_id": req.SkillID}, nil
	}
}

func makeCreateTasksHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Tasks []struct {
				ID          string `json:"id"`
				Description string `json:"description"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		tasks := make([]AgentTask, len(req.Tasks))
		for i, t := range req.Tasks {
			tasks[i] = AgentTask{ID: t.ID, Description: t.Description, Status: "pending"}
		}

		ctx.EmitTasksUpdated(tasks)

		return map[string]any{"tasks": tasks, "count": len(tasks)}, nil
	}
}

func makeUpdateTaskHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		if req.Status != "pending" && req.Status != "in_progress" && req.Status != "done" {
			return nil, fmt.Errorf("invalid status: %s. Must be pending, in_progress, or done", req.Status)
		}

		ctx.EmitTasksUpdated([]AgentTask{{ID: req.TaskID, Description: "", Status: req.Status}})

		return map[string]any{"task_id": req.TaskID, "status": req.Status}, nil
	}
}

func makeGetTasksHandler() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		return map[string]any{"tasks": []AgentTask{}, "message": "Task state is tracked via events sent to the UI"}, nil
	}
}
