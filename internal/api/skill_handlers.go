package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/models"
)

type skillHandlers struct {
	server *Server
}

func (h *skillHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT id, org_id, name, description, system_prompt, tool_ids, folder_id, created_by, created_at, updated_at
		FROM skills WHERE org_id = $1 ORDER BY created_at DESC
	`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	skills := []models.Skill{}
	for rows.Next() {
		var s models.Skill
		var desc, sysPrompt *string
		var toolIDs []byte
		rows.Scan(&s.ID, &s.OrgID, &s.Name, &desc, &sysPrompt, &toolIDs, &s.FolderID, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
		if desc != nil {
			s.Description = *desc
		}
		if sysPrompt != nil {
			s.SystemPrompt = *sysPrompt
		}
		if toolIDs != nil {
			json.Unmarshal(toolIDs, &s.ToolIDs)
		}
		skills = append(skills, s)
	}

	writeJSON(w, http.StatusOK, skills)
}

func (h *skillHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		SystemPrompt string  `json:"system_prompt"`
		ToolIDs     []string `json:"tool_ids"`
		FolderID    *string  `json:"folder_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.ToolIDs == nil {
		req.ToolIDs = []string{}
	}

	skillID := uuid.New().String()

	_, err := h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO skills (id, org_id, name, description, system_prompt, tool_ids, folder_id, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
	`, skillID, claims.OrgID, req.Name, req.Description, req.SystemPrompt, req.ToolIDs, req.FolderID, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": skillID})
}

func (h *skillHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var req struct {
		Name        *string  `json:"name"`
		Description *string  `json:"description"`
		SystemPrompt *string `json:"system_prompt"`
		ToolIDs     []string `json:"tool_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	_, err := h.server.db.Pool.Exec(r.Context(), `
		UPDATE skills SET
			name = COALESCE($2, name),
			description = COALESCE($3, description),
			system_prompt = COALESCE($4, system_prompt),
			tool_ids = COALESCE($5, tool_ids),
			updated_at = NOW()
		WHERE id = $1
	`, skillID, req.Name, req.Description, req.SystemPrompt, req.ToolIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = claims

	writeJSON(w, http.StatusOK, map[string]string{"id": skillID})
}

func (h *skillHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")

	_, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM skills WHERE id = $1`, skillID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}
