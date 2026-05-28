package agent

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func RegisterAgentTools(reg *ToolRegistry, pool *pgxpool.Pool) {
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
			Parameters:  `{"type":"object","properties":{"name":{"type":"string"},"description":{"type":"string"},"system_prompt":{"type":"string"},"tool_ids":{"type":"array","items":{"type":"string"}}},"required":["name","system_prompt"]}`,
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
			Description: "Fork parallel exploration tasks",
			Parameters:  `{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"goal":{"type":"string"},"context":{"type":"object"},"agent_id":{"type":"string"}}}}},"required":["tasks"]}`,
		},
		Handler: makeSpawnSubagentsHandler(pool),
	})

	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "update_skill",
			Description: "Modify a skill",
			Parameters:  `{"type":"object","properties":{"skill_id":{"type":"string"},"name":{"type":"string"},"system_prompt":{"type":"string"},"tool_ids":{"type":"array","items":{"type":"string"}}},"required":["skill_id"]}`,
		},
		Handler: makeUpdateSkillHandler(pool),
	})
}

func makeUpdateAgentHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Name        *string  `json:"name"`
			Description *string  `json:"description"`
			SystemPrompt *string `json:"system_prompt"`
			SkillIDs    []string `json:"skill_ids"`
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
			WHERE id = $1
		`, agentID, req.Name, req.Description, req.SystemPrompt, skillIDsJSON)
		if err != nil {
			return nil, fmt.Errorf("update agent: %w", err)
		}

		var version int
		pool.QueryRow(ctx.Context, `SELECT COALESCE(MAX(version), 0) + 1 FROM agent_versions WHERE agent_id = $1`, agentID).Scan(&version)
		_, _ = pool.Exec(ctx.Context, `
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
		toolIDsJSON, _ := json.Marshal(req.ToolIDs)

		_, err := pool.Exec(ctx.Context, `
			INSERT INTO skills (id, org_id, name, description, system_prompt, tool_ids, created_by, created_at, updated_at)
			SELECT $1, org_id, $2, $3, $4, $5, $6, NOW(), NOW()
			FROM agents WHERE id = (SELECT agent_id FROM agent_sessions WHERE id = $7)
		`, skillID, req.Name, req.Description, req.SystemPrompt, toolIDsJSON, ctx.UserID, ctx.SessionID)
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
				ID      string `json:"id"`
				Goal    string `json:"goal"`
				Context map[string]any `json:"context"`
				AgentID *string `json:"agent_id"`
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
			taskID := t.ID
			if taskID == "" {
				taskID = uuid.New().String()
			}
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

		return map[string]any{"task_ids": taskIDs, "status": "spawned"}, nil
	}
}

func makeUpdateSkillHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			SkillID     string   `json:"skill_id"`
			Name        *string  `json:"name"`
			SystemPrompt *string `json:"system_prompt"`
			ToolIDs     []string `json:"tool_ids"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}

		skillIDsJSON, _ := json.Marshal(req.ToolIDs)

		_, err := pool.Exec(ctx.Context, `
			UPDATE skills SET
				name = COALESCE($2, name),
				system_prompt = COALESCE($3, system_prompt),
				tool_ids = COALESCE($4, tool_ids),
				updated_at = NOW()
			WHERE id = $1
		`, req.SkillID, req.Name, req.SystemPrompt, skillIDsJSON)
		if err != nil {
			return nil, fmt.Errorf("update skill: %w", err)
		}

		return map[string]any{"skill_id": req.SkillID}, nil
	}
}
