package api

import (
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
		rows.Scan(&s.ID, &s.OrgID, &s.Name, &desc, &sysPrompt, &s.ToolIDs, &s.FolderID, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
		if desc != nil {
			s.Description = *desc
		}
		if sysPrompt != nil {
			s.SystemPrompt = *sysPrompt
		}
		allowed, _ := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "skill", s.ID, "view")
		if !allowed {
			continue
		}
		skills = append(skills, s)
	}

	writeJSON(w, http.StatusOK, skills)
}

func (h *skillHandlers) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "skill", id, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var s models.Skill
	var desc, sysPrompt *string
	err = h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, description, system_prompt, tool_ids, folder_id, created_by, created_at, updated_at
		FROM skills WHERE id = $1 AND org_id = $2
	`, id, claims.OrgID).Scan(&s.ID, &s.OrgID, &s.Name, &desc, &sysPrompt, &s.ToolIDs, &s.FolderID, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}
	if desc != nil {
		s.Description = *desc
	}
	if sysPrompt != nil {
		s.SystemPrompt = *sysPrompt
	}

	writeJSON(w, http.StatusOK, s)
}

func (h *skillHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		SystemPrompt string   `json:"system_prompt"`
		ToolIDs      []string `json:"tool_ids"`
		FolderID     *string  `json:"folder_id"`
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

	// Grant creator full access
	h.server.db.Pool.Exec(r.Context(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'skill', $2, 'user', $3, ARRAY['view','edit','delete'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, skillID, claims.UserID)

	writeJSON(w, http.StatusCreated, map[string]string{"id": skillID})
}

func (h *skillHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var req struct {
		Name         *string  `json:"name"`
		Description  *string  `json:"description"`
		SystemPrompt *string  `json:"system_prompt"`
		ToolIDs      []string `json:"tool_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

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

	writeJSON(w, http.StatusOK, map[string]string{"id": skillID})
}

func (h *skillHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	result, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM skills WHERE id = $1 AND org_id = $2`, skillID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}
