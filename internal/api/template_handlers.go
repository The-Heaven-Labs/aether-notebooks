package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type createTemplateRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        string          `json:"type"` // "notebook" or "cell"
	Content     json.RawMessage `json:"content"`
}

// @Summary Create a template
// @Description Create a new notebook template
// @Tags templates
// @Accept json
// @Produce json
// @Param request body object true "Template details"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /templates [post]
func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req createTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || (req.Type != "notebook" && req.Type != "cell") {
		writeError(w, http.StatusBadRequest, "name and type ('notebook' or 'cell') required")
		return
	}
	if len(req.Content) == 0 {
		req.Content = json.RawMessage(`{}`)
	}

	ctx := r.Context()
	var id string
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO templates (org_id, name, description, type, content, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		claims.OrgID, req.Name, req.Description, req.Type, req.Content, claims.UserID,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create template")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// @Summary List templates
// @Description List all notebook templates
// @Tags templates
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /templates [get]
func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	typeFilter := r.URL.Query().Get("type")
	if typeFilter != "" && typeFilter != "notebook" && typeFilter != "cell" {
		writeError(w, http.StatusBadRequest, "type must be 'notebook' or 'cell'")
		return
	}

	type tmpl struct {
		ID          string          `json:"id"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Type        string          `json:"type"`
		Content     json.RawMessage `json:"content"`
		IsBuiltin   bool            `json:"is_builtin"`
		CreatedAt   time.Time       `json:"created_at"`
	}

	var args []interface{}
	query := `SELECT id, name, description, type, content, is_builtin, created_at
	          FROM templates WHERE org_id = $1`
	args = append(args, claims.OrgID)
	if typeFilter != "" {
		query += ` AND type = $2`
		args = append(args, typeFilter)
	}
	query += ` ORDER BY name`

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var templates []tmpl
	for rows.Next() {
		var t tmpl
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.Type, &t.Content, &t.IsBuiltin, &t.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query iteration failed")
		return
	}
	if templates == nil {
		templates = []tmpl{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": templates})
}

// @Summary Delete a template
// @Description Delete a notebook template
// @Tags templates
// @Param id path string true "Template ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /templates/{id} [delete]
func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	ctx := r.Context()

	var deletedID string
	err := s.db.Pool.QueryRow(ctx,
		`DELETE FROM templates WHERE id = $1 AND org_id = $2 AND is_builtin = false RETURNING id`,
		id, claims.OrgID,
	).Scan(&deletedID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "template not found or is built-in")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
