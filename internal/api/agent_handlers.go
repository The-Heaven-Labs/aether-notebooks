package api

import (
	"encoding/json"
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
			   system_prompt, skill_ids, mcp_servers, folder_id, created_by, created_at, updated_at
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
		var mcpServers []byte
		var skillIDs []byte
		rows.Scan(&a.ID, &a.OrgID, &a.Name, &desc, &a.ModelConfigID, &a.SubagentModelConfigID,
			&sysPrompt, &skillIDs, &mcpServers, &a.FolderID, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
		if desc != nil {
			a.Description = *desc
		}
		if sysPrompt != nil {
			a.SystemPrompt = *sysPrompt
		}
		if skillIDs != nil {
			json.Unmarshal(skillIDs, &a.SkillIDs)
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
		Name                  string              `json:"name"`
		Description           string              `json:"description"`
		ModelConfigID         *string             `json:"model_config_id"`
		SubagentModelConfigID *string             `json:"subagent_model_config_id"`
		SystemPrompt          string              `json:"system_prompt"`
		SkillIDs              []string            `json:"skill_ids"`
		MCPServers            []models.MCPServer  `json:"mcp_servers"`
		FolderID              *string             `json:"folder_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.SkillIDs == nil {
		req.SkillIDs = []string{}
	}
	if req.MCPServers == nil {
		req.MCPServers = []models.MCPServer{}
	}

	agentID := uuid.New().String()
	mcpServersJSON, _ := json.Marshal(req.MCPServers)

	skillIDs := req.SkillIDs
	if skillIDs == nil {
		skillIDs = []string{}
	}

	_, err := h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO agents (id, org_id, name, description, model_config_id, subagent_model_config_id,
			system_prompt, skill_ids, mcp_servers, folder_id, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
	`, agentID, claims.OrgID, req.Name, req.Description, req.ModelConfigID, req.SubagentModelConfigID,
		req.SystemPrompt, skillIDs, mcpServersJSON, req.FolderID, claims.UserID)
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
	var skillIDs []byte
	err = h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, description, model_config_id, subagent_model_config_id,
			   system_prompt, skill_ids, mcp_servers, folder_id, created_by, created_at, updated_at
		FROM agents WHERE id = $1
	`, agentID).Scan(&a.ID, &a.OrgID, &a.Name, &desc, &a.ModelConfigID, &a.SubagentModelConfigID,
		&sysPrompt, &skillIDs, &mcpServers, &a.FolderID, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
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
	if skillIDs != nil {
		json.Unmarshal(skillIDs, &a.SkillIDs)
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
		Name                  *string  `json:"name"`
		Description           *string  `json:"description"`
		SystemPrompt          *string  `json:"system_prompt"`
		SkillIDs              []string `json:"skill_ids"`
		ModelConfigID         *string  `json:"model_config_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	_, err = h.server.db.Pool.Exec(r.Context(), `
		UPDATE agents SET
			name = COALESCE($2, name),
			description = COALESCE($3, description),
			system_prompt = COALESCE($4, system_prompt),
			skill_ids = COALESCE($5, skill_ids),
			model_config_id = COALESCE($6, model_config_id),
			updated_at = NOW()
		WHERE id = $1
	`, agentID, req.Name, req.Description, req.SystemPrompt, req.SkillIDs, req.ModelConfigID)
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
		SELECT s.id, s.agent_id, s.notebook_id, s.user_id, s.max_turns, s.max_tokens, s.ended_at, s.created_at,
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
		rows.Scan(&s.ID, &s.AgentID, &s.NotebookID, &s.UserID, &s.MaxTurns, &s.MaxTokens, &endedAt, &s.CreatedAt, &firstMsg, &msgCount)
		sessions = append(sessions, map[string]any{
			"id":            s.ID,
			"agent_id":      s.AgentID,
			"notebook_id":   s.NotebookID,
			"user_id":       s.UserID,
			"max_turns":     s.MaxTurns,
			"max_tokens":    s.MaxTokens,
			"ended_at":      endedAt,
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
	var endedAt *string
	err := h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, agent_id, notebook_id, user_id, max_turns, max_tokens, ended_at, created_at
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&s.ID, &s.AgentID, &s.NotebookID, &s.UserID, &s.MaxTurns, &s.MaxTokens, &endedAt, &s.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	_ = claims

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

func (h *agentHandlers) handleAgentStats(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	_ = claims

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
