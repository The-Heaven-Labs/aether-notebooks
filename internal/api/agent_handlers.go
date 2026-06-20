package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/models"
)

type agentHandlers struct {
	server *Server
}

func (h *agentHandlers) handleListAgents(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT id, org_id, name, description, model_config_id, subagent_model_config_id,
			   system_prompt, skill_ids, tool_ids, folder_id, max_turns, created_by, created_at, updated_at
		FROM agents WHERE org_id = $1 ORDER BY created_at DESC
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
		if err := rows.Scan(&a.ID, &a.OrgID, &a.Name, &desc, &a.ModelConfigID, &a.SubagentModelConfigID,
			&sysPrompt, &a.SkillIDs, &a.ToolIDs, &a.FolderID, &a.MaxTurns, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
			continue
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
		MCPServerIDs          []string `json:"mcp_server_ids"`
		FolderID              *string  `json:"folder_id"`
		MaxTurns              *int     `json:"max_turns"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
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

	agentID := uuid.New().String()

	skillIDs := req.SkillIDs
	if skillIDs == nil {
		skillIDs = []string{}
	}

	_, err := h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO agents (id, org_id, name, description, model_config_id, subagent_model_config_id,
			system_prompt, skill_ids, tool_ids, folder_id, max_turns, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
	`, agentID, claims.OrgID, req.Name, req.Description, req.ModelConfigID, req.SubagentModelConfigID,
		req.SystemPrompt, skillIDs, req.ToolIDs, req.FolderID, req.MaxTurns, claims.UserID)
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
			   system_prompt, skill_ids, tool_ids, folder_id, max_turns, created_by, created_at, updated_at
		FROM agents WHERE id = $1 AND org_id = $2
	`, agentID, claims.OrgID).Scan(&a.ID, &a.OrgID, &a.Name, &desc, &a.ModelConfigID, &a.SubagentModelConfigID,
		&sysPrompt, &a.SkillIDs, &a.ToolIDs, &a.FolderID, &a.MaxTurns, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
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
		ModelConfigID         *string  `json:"model_config_id"`
		SubagentModelConfigID *string  `json:"subagent_model_config_id"`
		MCPServerIDs          []string `json:"mcp_server_ids"`
		MaxTurns              *int     `json:"max_turns"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
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
			updated_at = NOW()
		WHERE id = $1 AND org_id = $10
	`, agentID, req.Name, req.Description, req.SystemPrompt, req.SkillIDs, req.ToolIDs, req.ModelConfigID, req.SubagentModelConfigID, req.MaxTurns, claims.OrgID)
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

func (h *agentHandlers) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "agent", agentID, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var req struct {
		NotebookID string  `json:"notebook_id"`
		MaxTurns   int     `json:"max_turns"`
		MaxTokens  int     `json:"max_tokens"`
		Title      *string `json:"title"`
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
	if req.MaxTokens == 0 {
		req.MaxTokens = 100000
	}

	sessionID := uuid.New().String()
	var notebookID *string
	if req.NotebookID != "" {
		notebookID = &req.NotebookID
	}
	_, err = h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, max_tokens, title, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`, sessionID, agentID, notebookID, claims.UserID, req.MaxTurns, req.MaxTokens, req.Title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "agent_session.create", ResourceType: "agent_session", ResourceID: sessionID,
	})

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
		SELECT s.id, s.agent_id, s.notebook_id, s.user_id, s.max_turns, s.max_tokens, s.ended_at, s.title, s.created_at,
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
		if err := rows.Scan(&s.ID, &s.AgentID, &s.NotebookID, &s.UserID, &s.MaxTurns, &s.MaxTokens, &endedAt, &title, &s.CreatedAt, &firstMsg, &msgCount); err != nil {
			continue
		}
		sessions = append(sessions, map[string]any{
			"id":            s.ID,
			"agent_id":      s.AgentID,
			"notebook_id":   s.NotebookID,
			"user_id":       s.UserID,
			"max_turns":     s.MaxTurns,
			"max_tokens":    s.MaxTokens,
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

func (h *agentHandlers) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())

	var s models.AgentSession
	var endedAt *time.Time
	var title *string
	var notebookID *string
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, agent_id, notebook_id, user_id, max_turns, max_tokens, ended_at, title, created_at
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&s.ID, &s.AgentID, &notebookID, &s.UserID, &s.MaxTurns, &s.MaxTokens, &endedAt, &title, &s.CreatedAt)
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
		SELECT id, role, content, tool_calls, tool_call_id, reasoning_content, created_at
		FROM agent_messages WHERE session_id = $1 ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
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
		var createdAt time.Time
		rows.Scan(&id, &role, &content, &toolCalls, &toolCallID, &reasoning, &createdAt)
		msg := map[string]any{
			"id":         id,
			"role":       role,
			"created_at": createdAt,
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
		if len(toolCalls) > 0 {
			msg["tool_calls"] = json.RawMessage(toolCalls)
		}
		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, messages)
}

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

func (h *agentHandlers) handleAgentStats(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT s.date, s.agent_id, s.user_id, s.sessions_count, s.messages_count, s.tokens_input, s.tokens_output
		FROM agent_stats_daily s
		JOIN agents a ON a.id = s.agent_id
		WHERE a.org_id = $1 AND s.date >= NOW() - INTERVAL '30 days'
		ORDER BY s.date DESC
	`, claims.OrgID)
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
	claims := ClaimsFromContext(r.Context())

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT s.date, s.agent_id, s.user_id, s.sessions_count, s.messages_count, s.tokens_input, s.tokens_output
		FROM agent_stats_daily s
		JOIN agents a ON a.id = s.agent_id
		WHERE a.id = $1 AND a.org_id = $2 AND s.date >= NOW() - INTERVAL '30 days'
		ORDER BY s.date DESC
	`, agentID, claims.OrgID)
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
