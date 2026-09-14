package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
)

type agentHandlers struct {
	server *Server
}

// @Summary List agents
// @Description List all agents for the current organization
// @Tags agents
// @Produce json
// @Success 200 {array} models.Agent
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /agents [get]
func (h *agentHandlers) handleListAgents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT a.id, a.org_id, a.name, a.description, a.model_config_id, a.subagent_model_config_id,
			   a.system_prompt, a.skill_ids, a.tool_ids, a.all_builtin_tools, a.folder_id, a.max_turns, a.max_subagents, a.max_subagent_turns, a.created_by, a.created_at, a.updated_at,
			   mc.default_params
		FROM agents a
		LEFT JOIN model_configs mc ON mc.id = a.model_config_id
		WHERE a.org_id = $1 ORDER BY a.created_at DESC
	`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	agents := []models.Agent{}
	for rows.Next() {
		var a models.Agent
		var desc, sysPrompt *string
		var mcDefaultParams []byte
		if err := rows.Scan(&a.ID, &a.OrgID, &a.Name, &desc, &a.ModelConfigID, &a.SubagentModelConfigID,
			&sysPrompt, &a.SkillIDs, &a.ToolIDs, &a.AllBuiltinTools, &a.FolderID, &a.MaxTurns, &a.MaxSubAgents, &a.MaxSubagentTurns, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt,
			&mcDefaultParams); err != nil {
			continue
		}
		if mcDefaultParams != nil {
			json.Unmarshal(mcDefaultParams, &a.ModelConfigParams)
		}
		if desc != nil {
			a.Description = *desc
		}
		if sysPrompt != nil {
			a.SystemPrompt = *sysPrompt
		}
		allowed, _ := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", a.ID, "view")
		if !allowed {
			continue
		}
		agents = append(agents, a)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if len(agents) > 0 {
		agentIDs := make([]string, len(agents))
		for i := range agents {
			agentIDs[i] = agents[i].ID
		}

		mcpMap := h.batchLoadMCPHandlers(r.Context(), agentIDs)
		skillMap := h.batchLoadSkills(r.Context(), agents)
		toolMap := h.batchLoadTools(r.Context(), agents)

		for i := range agents {
			if mcp, ok := mcpMap[agents[i].ID]; ok {
				agents[i].MCPServerIDs = mcp.IDs
				agents[i].MCPServers = mcp.Servers
			}
			agents[i].Skills = skillMap[agents[i].ID]
			agents[i].Tools = toolMap[agents[i].ID]
		}
	}

	writeJSON(w, http.StatusOK, agents)
}

type mcpGroup struct {
	IDs     []string
	Servers []models.MCPServerOrg
}

func (h *agentHandlers) batchLoadMCPHandlers(ctx context.Context, agentIDs []string) map[string]*mcpGroup {
	rows, err := h.server.db.Pool.Query(ctx, `
		SELECT ams.agent_id, ms.id, ms.org_id, ms.name, ms.type, ms.command, ms.args, ms.created_by, ms.created_at, ms.updated_at
		FROM agent_mcp_servers ams
		JOIN mcp_servers ms ON ms.id = ams.mcp_server_id
		WHERE ams.agent_id = ANY($1)
		ORDER BY ms.name
	`, agentIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]*mcpGroup)
	for rows.Next() {
		var agentID string
		var s models.MCPServerOrg
		if err := rows.Scan(&agentID, &s.ID, &s.OrgID, &s.Name, &s.Type, &s.Command, &s.Args, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
			continue
		}
		if result[agentID] == nil {
			result[agentID] = &mcpGroup{}
		}
		result[agentID].IDs = append(result[agentID].IDs, s.ID)
		result[agentID].Servers = append(result[agentID].Servers, s)
	}
	return result
}

func (h *agentHandlers) batchLoadSkills(ctx context.Context, agents []models.Agent) map[string][]models.Skill {
	allIDs := []string{}
	seen := map[string]bool{}
	for _, a := range agents {
		for _, sid := range a.SkillIDs {
			if !seen[sid] {
				seen[sid] = true
				allIDs = append(allIDs, sid)
			}
		}
	}
	if len(allIDs) == 0 {
		return nil
	}

	rows, err := h.server.db.Pool.Query(ctx, `SELECT id, name FROM skills WHERE id = ANY($1)`, allIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()

	skillByName := map[string]models.Skill{}
	for rows.Next() {
		var s models.Skill
		rows.Scan(&s.ID, &s.Name)
		skillByName[s.ID] = s
	}

	result := map[string][]models.Skill{}
	for _, a := range agents {
		for _, sid := range a.SkillIDs {
			if s, ok := skillByName[sid]; ok {
				result[a.ID] = append(result[a.ID], s)
			}
		}
	}
	return result
}

func (h *agentHandlers) batchLoadTools(ctx context.Context, agents []models.Agent) map[string][]models.Tool {
	allIDs := []string{}
	seen := map[string]bool{}
	for _, a := range agents {
		for _, tid := range a.ToolIDs {
			if !seen[tid] {
				seen[tid] = true
				allIDs = append(allIDs, tid)
			}
		}
	}
	if len(allIDs) == 0 {
		return nil
	}

	rows, err := h.server.db.Pool.Query(ctx, `
		SELECT id, name, description, type FROM tools WHERE id = ANY($1)`, allIDs)
	if err != nil {
		return nil
	}
	defer rows.Close()

	toolByID := map[string]models.Tool{}
	for rows.Next() {
		var t models.Tool
		var desc *string
		rows.Scan(&t.ID, &t.Name, &desc, &t.Type)
		if desc != nil {
			t.Description = *desc
		}
		toolByID[t.ID] = t
	}

	result := map[string][]models.Tool{}
	for _, a := range agents {
		for _, tid := range a.ToolIDs {
			if t, ok := toolByID[tid]; ok {
				result[a.ID] = append(result[a.ID], t)
			}
		}
	}
	return result
}

func (h *agentHandlers) validateToolAccess(ctx context.Context, userID, orgID, role string, toolIDs []string) error {
	if role == "admin" {
		return nil
	}
	for _, tid := range toolIDs {
		allowed, err := h.server.checkPermission(ctx, userID, orgID, role, "tool", tid, "view")
		if err != nil {
			return fmt.Errorf("check tool %s: %w", tid, err)
		}
		if !allowed {
			return fmt.Errorf("you don't have access to one or more tools")
		}
	}
	return nil
}

// @Summary Create an agent
// @Description Create a new agent configuration
// @Tags agents
// @Accept json
// @Produce json
// @Param request body object true "Agent details"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /agents [post]
func (h *agentHandlers) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Name                  string   `json:"name"`
		Description           string   `json:"description"`
		ModelConfigID         *string  `json:"model_config_id"`
		SubagentModelConfigID *string  `json:"subagent_model_config_id"`
		SystemPrompt          string   `json:"system_prompt"`
		SkillIDs              []string `json:"skill_ids"`
		ToolIDs               []string `json:"tool_ids"`
		AllBuiltinTools       *bool    `json:"all_builtin_tools"`
		MCPServerIDs          []string `json:"mcp_server_ids"`
		FolderID              *string  `json:"folder_id"`
		MaxTurns              *int     `json:"max_turns"`
		MaxSubAgents          *int     `json:"max_subagents"`
		MaxSubagentTurns      *int     `json:"max_subagent_turns"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.AllBuiltinTools != nil {
		slog.Warn("all_builtin_tools is deprecated and ignored; use tool_ids", "agent_name", req.Name)
	}
	if req.SkillIDs == nil {
		req.SkillIDs = []string{}
	}
	if req.ToolIDs == nil {
		req.ToolIDs = []string{}
	}
	if req.MCPServerIDs == nil {
		req.MCPServerIDs = []string{}
	}
	maxSubAgents := 5
	if req.MaxSubAgents != nil && *req.MaxSubAgents > 0 {
		maxSubAgents = *req.MaxSubAgents
	}
	maxSubagentTurns := 20
	if req.MaxSubagentTurns != nil && *req.MaxSubagentTurns > 0 {
		maxSubagentTurns = *req.MaxSubagentTurns
	}

	if req.FolderID != nil && *req.FolderID == "" {
		req.FolderID = nil
	}

	agentID := uuid.New().String()

	skillIDs := req.SkillIDs
	if skillIDs == nil {
		skillIDs = []string{}
	}

	_, err := h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO agents (id, org_id, name, description, model_config_id, subagent_model_config_id,
			system_prompt, skill_ids, tool_ids, folder_id, max_turns, max_subagents, max_subagent_turns, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW(), NOW())
	`, agentID, claims.OrgID, req.Name, req.Description, req.ModelConfigID, req.SubagentModelConfigID,
		req.SystemPrompt, skillIDs, req.ToolIDs, req.FolderID, req.MaxTurns, maxSubAgents, maxSubagentTurns, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if len(req.ToolIDs) > 0 {
		if err := h.validateToolAccess(r.Context(), claims.UserID, claims.OrgID, claims.Role, req.ToolIDs); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if len(req.MCPServerIDs) > 0 {
		var count int
		err := h.server.db.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM mcp_servers WHERE id = ANY($1) AND org_id = $2`, req.MCPServerIDs, claims.OrgID).Scan(&count)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if count != len(req.MCPServerIDs) {
			writeError(w, http.StatusBadRequest, "one or more mcp_server_ids not found in your organization")
			return
		}
		for _, mcpID := range req.MCPServerIDs {
			_, err := h.server.db.Pool.Exec(r.Context(), `
				INSERT INTO agent_mcp_servers (agent_id, mcp_server_id) VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, agentID, mcpID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "agent.create", ResourceType: "agent", ResourceID: agentID,
	})

	// Grant creator full access
	h.server.db.Pool.Exec(r.Context(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'agent', $2, 'user', $3, ARRAY['view','edit','delete'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, agentID, claims.UserID)

	writeJSON(w, http.StatusCreated, map[string]string{"id": agentID})
}

// @Summary Get an agent
// @Description Get a single agent by ID
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {object} models.Agent
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /agents/{id} [get]
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
	err = h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, description, model_config_id, subagent_model_config_id,
			   system_prompt, skill_ids, tool_ids, all_builtin_tools, folder_id, max_turns, max_subagents, max_subagent_turns, created_by, created_at, updated_at
		FROM agents WHERE id = $1 AND org_id = $2
	`, agentID, claims.OrgID).Scan(&a.ID, &a.OrgID, &a.Name, &desc, &a.ModelConfigID, &a.SubagentModelConfigID,
		&sysPrompt, &a.SkillIDs, &a.ToolIDs, &a.AllBuiltinTools, &a.FolderID, &a.MaxTurns, &a.MaxSubAgents, &a.MaxSubagentTurns, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
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

	mcpMap := h.batchLoadMCPHandlers(r.Context(), []string{a.ID})
	if mcp, ok := mcpMap[a.ID]; ok {
		a.MCPServerIDs = mcp.IDs
		a.MCPServers = mcp.Servers
	}

	// Load tools
	if len(a.ToolIDs) > 0 {
		tRows, err := h.server.db.Pool.Query(r.Context(), `
			SELECT id, org_id, name, description, type, schema, config, folder_id, created_by, created_at, updated_at
			FROM tools WHERE id = ANY($1)`, a.ToolIDs)
		if err == nil {
			defer tRows.Close()
			for tRows.Next() {
				var t models.Tool
				var schema, config []byte
				if err := tRows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &t.Type, &schema, &config, &t.FolderID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err == nil {
					if len(schema) > 0 {
						json.Unmarshal(schema, &t.Schema)
					}
					if len(config) > 0 {
						json.Unmarshal(config, &t.Config)
					}
					a.Tools = append(a.Tools, t)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, a)
}

// @Summary Update an agent
// @Description Update an existing agent configuration
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body object true "Agent updates"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /agents/{id} [put]
func (h *agentHandlers) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "edit")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		Name                  *string  `json:"name"`
		Description           *string  `json:"description"`
		SystemPrompt          *string  `json:"system_prompt"`
		SkillIDs              []string `json:"skill_ids"`
		ToolIDs               []string `json:"tool_ids"`
		AllBuiltinTools       *bool    `json:"all_builtin_tools"`
		ModelConfigID         *string  `json:"model_config_id"`
		SubagentModelConfigID *string  `json:"subagent_model_config_id"`
		MCPServerIDs          []string `json:"mcp_server_ids"`
		MaxTurns              *int     `json:"max_turns"`
		MaxSubAgents          *int     `json:"max_subagents"`
		MaxSubagentTurns      *int     `json:"max_subagent_turns"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.AllBuiltinTools != nil {
		slog.Warn("all_builtin_tools is deprecated and ignored; use tool_ids", "agent_id", agentID)
	}

	// Custom COALESCE for max_subagents (nullable int with default)
	var updateMaxSubAgents any
	if req.MaxSubAgents != nil {
		updateMaxSubAgents = *req.MaxSubAgents
	}
	var updateMaxSubagentTurns any
	if req.MaxSubagentTurns != nil {
		updateMaxSubagentTurns = *req.MaxSubagentTurns
	}

	result, err := h.server.db.Pool.Exec(r.Context(), `
		UPDATE agents SET
			name = COALESCE($2, name),
			description = COALESCE($3, description),
			system_prompt = COALESCE($4, system_prompt),
			skill_ids = COALESCE($5, skill_ids),
			tool_ids = COALESCE($6, tool_ids),
			model_config_id = COALESCE($7, model_config_id),
			subagent_model_config_id = COALESCE($8, subagent_model_config_id),
			max_turns = COALESCE($9, max_turns),
			max_subagents = COALESCE($10, max_subagents),
			max_subagent_turns = COALESCE($11, max_subagent_turns),
			updated_at = NOW()
		WHERE id = $1 AND org_id = $12
	`, agentID, req.Name, req.Description, req.SystemPrompt, req.SkillIDs, req.ToolIDs, req.ModelConfigID, req.SubagentModelConfigID, req.MaxTurns, updateMaxSubAgents, updateMaxSubagentTurns, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	if req.ToolIDs != nil && len(req.ToolIDs) > 0 {
		if err := h.validateToolAccess(r.Context(), claims.UserID, claims.OrgID, claims.Role, req.ToolIDs); err != nil {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
	}

	if req.MCPServerIDs != nil {
		if len(req.MCPServerIDs) > 0 {
			var count int
			err := h.server.db.Pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM mcp_servers WHERE id = ANY($1) AND org_id = $2`, req.MCPServerIDs, claims.OrgID).Scan(&count)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if count != len(req.MCPServerIDs) {
				writeError(w, http.StatusBadRequest, "one or more mcp_server_ids not found in your organization")
				return
			}
		}
		tx, err := h.server.db.Pool.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer tx.Rollback(r.Context())

		_, err = tx.Exec(r.Context(), `DELETE FROM agent_mcp_servers WHERE agent_id = $1`, agentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, mcpID := range req.MCPServerIDs {
			_, err := tx.Exec(r.Context(), `
				INSERT INTO agent_mcp_servers (agent_id, mcp_server_id) VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, agentID, mcpID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "agent.update", ResourceType: "agent", ResourceID: agentID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"id": agentID})
}

// @Summary Delete an agent
// @Description Delete an agent configuration
// @Tags agents
// @Param id path string true "Agent ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /agents/{id} [delete]
func (h *agentHandlers) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "delete")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	result, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM agents WHERE id = $1 AND org_id = $2`, agentID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "agent.delete", ResourceType: "agent", ResourceID: agentID,
	})

	writeJSON(w, http.StatusNoContent, nil)
}

// @Summary Create an agent session
// @Description Create a new chat session with an agent
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body object true "Session details"
// @Success 201 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /agents/{id}/session [post]
func (h *agentHandlers) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		NotebookID          string  `json:"notebook_id"`
		MaxTurns            int     `json:"max_turns"`
		Title               *string `json:"title"`
		AutoApproveTools    bool    `json:"auto_approve_tools"`
		AutoAnswerQuestions bool    `json:"auto_answer_questions"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.Title != nil && len(*req.Title) > 50 {
		writeError(w, http.StatusBadRequest, "title must be 50 characters or less")
		return
	}

	if req.MaxTurns == 0 {
		req.MaxTurns = 100
	}

	// Clean up any empty sessions for this user+agent before creating a new one
	_, err = h.server.db.Pool.Exec(r.Context(), `
		DELETE FROM agent_sessions
		WHERE agent_id = $1 AND user_id = $2
			AND id NOT IN (SELECT DISTINCT session_id FROM agent_messages)
	`, agentID, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sessionID := uuid.New().String()
	var notebookID *string
	if req.NotebookID != "" {
		notebookID = &req.NotebookID
	}
	_, err = h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, title, created_at, auto_approve_tools, auto_answer_questions)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), $7, $8)
	`, sessionID, agentID, notebookID, claims.UserID, req.MaxTurns, req.Title, req.AutoApproveTools, req.AutoAnswerQuestions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.server.agentEngine != nil {
		h.server.agentEngine.SessionStore().SetAdminMode(sessionID, adminModeFromContext(r.Context()))
	}

	// Look up the model's context window for display purposes
	var contextWindow int
	h.server.db.Pool.QueryRow(r.Context(), `
		SELECT COALESCE(mc.context_window, 128000)
		FROM agents a
		JOIN model_configs mc ON mc.id = a.model_config_id
		WHERE a.id = $1
	`, agentID).Scan(&contextWindow)
	if contextWindow == 0 {
		contextWindow = 128000
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "agent_session.create", ResourceType: "agent_session", ResourceID: sessionID,
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"session_id":            sessionID,
		"context_window":        contextWindow,
		"auto_approve_tools":    req.AutoApproveTools,
		"auto_answer_questions": req.AutoAnswerQuestions,
	})
}

// @Summary List agent sessions
// @Description List all sessions for a given agent
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {array} object
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /agents/{id}/sessions [get]
func (h *agentHandlers) handleListSessions(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT s.id, s.agent_id, s.notebook_id, s.user_id, s.max_turns, s.ended_at, s.title, s.created_at,
			COALESCE(
				(SELECT content FROM agent_messages WHERE session_id = s.id AND role = 'user' ORDER BY created_at ASC LIMIT 1),
				''
			) as first_message,
			COALESCE(
				(SELECT COUNT(*) FROM agent_messages WHERE session_id = s.id),
				0
			) as message_count
		FROM agent_sessions s
		WHERE s.agent_id = $1
			AND s.id IN (SELECT DISTINCT session_id FROM agent_messages)
		ORDER BY s.created_at DESC LIMIT 50
	`, agentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var sessions []map[string]any
	for rows.Next() {
		var s models.AgentSession
		var firstMsg string
		var msgCount int
		var endedAt *time.Time
		var title *string
		var notebookID *string
		if err := rows.Scan(&s.ID, &s.AgentID, &notebookID, &s.UserID, &s.MaxTurns, &endedAt, &title, &s.CreatedAt, &firstMsg, &msgCount); err != nil {
			continue
		}
		if notebookID != nil {
			s.NotebookID = *notebookID
		}
		sessions = append(sessions, map[string]any{
			"id":            s.ID,
			"agent_id":      s.AgentID,
			"notebook_id":   s.NotebookID,
			"user_id":       s.UserID,
			"max_turns":     s.MaxTurns,
			"ended_at":      endedAt,
			"title":         title,
			"created_at":    s.CreatedAt,
			"first_message": firstMsg,
			"message_count": msgCount,
		})
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, sessions)
}

// @Summary Get a session
// @Description Get a single agent session by ID
// @Tags agents
// @Produce json
// @Param session_id path string true "Session ID"
// @Success 200 {object} models.AgentSession
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /sessions/{session_id} [get]
func (h *agentHandlers) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())

	var s models.AgentSession
	var endedAt *time.Time
	var title *string
	var notebookID *string
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, agent_id, notebook_id, user_id, max_turns, ended_at, title, created_at
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&s.ID, &s.AgentID, &notebookID, &s.UserID, &s.MaxTurns, &endedAt, &title, &s.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	s.EndedAt = endedAt
	s.NotebookID = ""
	if notebookID != nil {
		s.NotebookID = *notebookID
	}
	s.Title = title

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", s.AgentID, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	writeJSON(w, http.StatusOK, s)
}

// @Summary Get session messages
// @Description Get all messages for a given session
// @Tags agents
// @Produce json
// @Param session_id path string true "Session ID"
// @Success 200 {array} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /sessions/{session_id}/messages [get]
func (h *agentHandlers) handleGetSessionMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())

	var agentID string
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT agent_id FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT id, role, content, tool_calls, tool_call_id, reasoning_content, image_ids, tokens_input, tokens_output, COALESCE(tokens_direct,0), COALESCE(tokens_after,0), COALESCE(duration_ms,0), created_at
		FROM agent_messages WHERE session_id = $1 ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		slog.Error("get session messages query failed", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var messages []map[string]any
	for rows.Next() {
		var id, role string
		var content *string
		var toolCalls []byte
		var toolCallID *string
		var reasoning *string
		var imageIDs []string
		var tokensInput, tokensOutput *int
		var tokensDirect, tokensAfter int
		var durationMs int
		var createdAt time.Time
		if err := rows.Scan(&id, &role, &content, &toolCalls, &toolCallID, &reasoning, &imageIDs, &tokensInput, &tokensOutput, &tokensDirect, &tokensAfter, &durationMs, &createdAt); err != nil {
			slog.Error("get session messages scan failed", "session_id", sessionID, "error", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		msg := map[string]any{
			"id":            id,
			"role":          role,
			"tokens_direct": tokensDirect,
			"tokens_after":  tokensAfter,
			"created_at":    createdAt,
		}
		if content != nil {
			msg["content"] = *content
		}
		if toolCallID != nil {
			msg["tool_call_id"] = *toolCallID
		}
		if reasoning != nil {
			msg["reasoning_content"] = *reasoning
		}
		if tokensInput != nil {
			msg["tokens_input"] = *tokensInput
		}
		if tokensOutput != nil {
			msg["tokens_output"] = *tokensOutput
		}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = json.RawMessage(toolCalls)
		}
		if len(imageIDs) > 0 {
			msg["image_ids"] = imageIDs
		}
		if durationMs > 0 {
			msg["duration_ms"] = durationMs
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		slog.Error("get session messages rows iteration error", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, messages)
}

// @Summary Get session usage
// @Description Get the accumulated token usage for a given session
// @Tags agents
// @Produce json
// @Param id path string true "Session ID"
// @Success 200 {object} models.SessionUsage
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /agents/sessions/{id}/usage [get]
func (h *agentHandlers) handleGetSessionUsage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var agentID string
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT agent_id FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	usage, err := h.server.agentEngine.SessionStore().GetUsage(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// @Summary Update session title
// @Description Update the title of an agent session
// @Tags agents
// @Accept json
// @Produce json
// @Param session_id path string true "Session ID"
// @Param request body object true "Title update"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /sessions/{session_id}/title [patch]
func (h *agentHandlers) handleUpdateSessionTitle(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())

	var agentID string
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT agent_id FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&agentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "edit")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		Title *string `json:"title"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.Title != nil && len(*req.Title) > 50 {
		writeError(w, http.StatusBadRequest, "title must be 50 characters or less")
		return
	}

	if err := h.server.agentEngine.SessionStore().UpdateTitle(r.Context(), sessionID, req.Title); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "agent_session.update_title", ResourceType: "agent_session", ResourceID: sessionID,
	})

	writeJSON(w, http.StatusOK, map[string]any{"title": req.Title})
}

// AgentStatRow is one hourly (or daily-aggregated) usage bucket with
// resolved names so the UI can filter without extra requests.
type AgentStatRow struct {
	BucketStart     time.Time `json:"bucket_start"`
	AgentID         string    `json:"agent_id"`
	AgentName       string    `json:"agent_name"`
	UserID          string    `json:"user_id"`
	UserName        string    `json:"user_name"`
	UserEmail       string    `json:"user_email"`
	SessionsCount   int64     `json:"sessions_count"`
	MessagesCount   int64     `json:"messages_count"`
	TokensInput     int64     `json:"tokens_input"`
	TokensOutput    int64     `json:"tokens_output"`
	TokensDirect    int64     `json:"tokens_direct"`
	TokensSubagent  int64     `json:"tokens_subagent"`
	ModelCalls      int64     `json:"model_calls"`
	TotalDurationMs int64     `json:"total_duration_ms"`
	EstCostUSD      float64   `json:"est_cost_usd"`
}

type agentStatsParams struct {
	From        time.Time
	To          time.Time
	UserID      string
	Granularity string // "hour" or "day"
}

func parseAgentStatsParams(r *http.Request) (agentStatsParams, error) {
	q := r.URL.Query()
	now := time.Now()
	p := agentStatsParams{To: now, From: now.Add(-30 * 24 * time.Hour), Granularity: "day"}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return p, fmt.Errorf("invalid from (use RFC3339)")
		}
		p.From = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return p, fmt.Errorf("invalid to (use RFC3339)")
		}
		p.To = t
	}
	if p.To.Before(p.From) {
		return p, fmt.Errorf("to must not be before from")
	}
	p.UserID = q.Get("user_id")
	switch g := q.Get("granularity"); g {
	case "", "day":
		p.Granularity = "day"
	case "hour":
		p.Granularity = "hour"
	default:
		return p, fmt.Errorf("invalid granularity (want hour|day)")
	}
	return p, nil
}

func (h *agentHandlers) queryAgentStats(ctx context.Context, orgID, agentID string, p agentStatsParams) ([]AgentStatRow, error) {
	bucket := "h.bucket_start"
	if p.Granularity == "day" {
		bucket = "date_trunc('day', h.bucket_start)"
	}
	query := `
		SELECT ` + bucket + `, h.agent_id, a.name, h.user_id, u.name, u.email,
			SUM(h.sessions_count), SUM(h.messages_count),
			SUM(h.tokens_input), SUM(h.tokens_output), SUM(h.tokens_direct), SUM(h.tokens_subagent),
			SUM(h.model_calls), SUM(h.total_duration_ms), SUM(h.est_cost_usd)::float8
		FROM agent_stats_hourly h
		JOIN agents a ON a.id = h.agent_id
		JOIN users u ON u.id = h.user_id
		WHERE a.org_id = $1 AND h.bucket_start >= $2 AND h.bucket_start < $3`
	args := []any{orgID, p.From, p.To}
	if agentID != "" {
		args = append(args, agentID)
		query += fmt.Sprintf(" AND h.agent_id = $%d", len(args))
	}
	if p.UserID != "" {
		args = append(args, p.UserID)
		query += fmt.Sprintf(" AND h.user_id = $%d", len(args))
	}
	query += ` GROUP BY 1, 2, 3, 4, 5, 6 ORDER BY 1 DESC`

	rows, err := h.server.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := []AgentStatRow{}
	for rows.Next() {
		var s AgentStatRow
		if err := rows.Scan(&s.BucketStart, &s.AgentID, &s.AgentName, &s.UserID, &s.UserName, &s.UserEmail,
			&s.SessionsCount, &s.MessagesCount, &s.TokensInput, &s.TokensOutput, &s.TokensDirect, &s.TokensSubagent,
			&s.ModelCalls, &s.TotalDurationMs, &s.EstCostUSD); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// @Summary Get agent stats
// @Description Org-wide agent usage statistics from hourly rollups, with optional filters
// @Tags agents
// @Produce json
// @Param from query string false "Start (RFC3339, default now-30d)"
// @Param to query string false "End (RFC3339, default now)"
// @Param user_id query string false "Filter by user"
// @Param agent_id query string false "Filter by agent"
// @Param granularity query string false "hour or day (default day)"
// @Success 200 {array} object
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /agents/stats [get]
func (h *agentHandlers) handleAgentStats(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	params, err := parseAgentStatsParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stats, err := h.queryAgentStats(r.Context(), claims.OrgID, r.URL.Query().Get("agent_id"), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// @Summary Get agent stats by agent
// @Description Get usage statistics for a specific agent
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {array} object
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /agents/{id}/stats [get]
func (h *agentHandlers) handleAgentStatsByAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	params, err := parseAgentStatsParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stats, err := h.queryAgentStats(r.Context(), claims.OrgID, agentID, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// @Summary Roll up agent stats now
// @Description Synchronously roll up recent agent usage into hourly buckets (idempotent)
// @Tags agents
// @Produce json
// @Success 200 {object} object
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /agents/stats/rollup [post]
func (h *agentHandlers) handleAgentStatsRollup(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	res, err := agent.NewStatsAggregator(h.server.db.Pool).RollupHourlyStats(r.Context(), time.Now().Add(-25*time.Hour))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "agent_stats.rollup", ResourceType: "agent_stats",
	})

	writeJSON(w, http.StatusOK, map[string]any{"rolled_up": res})
}

func (h *agentHandlers) handleGetSubagentMessages(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("task_id")
	claims := ClaimsFromContext(r.Context())

	var orgID string
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT a.org_id FROM subagent_tasks st
		JOIN agent_sessions s ON s.id = st.parent_session_id
		JOIN agents a ON a.id = s.agent_id
		WHERE st.id = $1
	`, taskID).Scan(&orgID)
	if err != nil || orgID != claims.OrgID {
		writeError(w, http.StatusNotFound, "subagent task not found")
		return
	}

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT role, content, tool_call_id, tool_calls, reasoning_content, COALESCE(duration_ms, 0), created_at
		FROM subagent_messages WHERE subagent_task_id = $1 ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type msg struct {
		Role             string `json:"role"`
		Content          string `json:"content"`
		ToolCallID       string `json:"tool_call_id,omitempty"`
		ToolCalls        any    `json:"tool_calls,omitempty"`
		ReasoningContent string `json:"reasoning_content,omitempty"`
		DurationMs       int    `json:"duration_ms,omitempty"`
		CreatedAt        string `json:"created_at"`
	}
	var msgs []msg
	for rows.Next() {
		var m msg
		var toolCallsJSON []byte
		var createdAt time.Time
		var toolCallID *string
		if err := rows.Scan(&m.Role, &m.Content, &toolCallID, &toolCallsJSON, &m.ReasoningContent, &m.DurationMs, &createdAt); err != nil {
			continue
		}
		if toolCallID != nil {
			m.ToolCallID = *toolCallID
		}
		if len(toolCallsJSON) > 0 {
			json.Unmarshal(toolCallsJSON, &m.ToolCalls)
		}
		m.CreatedAt = createdAt.Format(time.RFC3339)
		msgs = append(msgs, m)
	}
	writeJSON(w, http.StatusOK, msgs)
}
