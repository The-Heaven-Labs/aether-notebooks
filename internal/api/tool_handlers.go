package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5/pgconn"
)

type toolHandlers struct {
	server *Server
}

func (h *toolHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT id, org_id, name, description, type, schema, config, folder_id, created_by, created_at, updated_at
		FROM tools WHERE org_id = $1 ORDER BY name
	`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	tools := []models.Tool{}
	for rows.Next() {
		var t models.Tool
		var desc *string
		var schemaRaw, configRaw []byte
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &desc, &t.Type, &schemaRaw, &configRaw, &t.FolderID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		if desc != nil {
			t.Description = *desc
		}
		if len(schemaRaw) > 0 {
			json.Unmarshal(schemaRaw, &t.Schema)
		}
		if len(configRaw) > 0 {
			json.Unmarshal(configRaw, &t.Config)
		}
		if t.Schema == nil {
			t.Schema = models.JSONMap{}
		}
		if t.Config == nil {
			t.Config = models.JSONMap{}
		}
		allowed, _ := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "tool", t.ID, "view")
		if !allowed {
			continue
		}
		tools = append(tools, t)
	}

	writeJSON(w, http.StatusOK, tools)
}

func (h *toolHandlers) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "tool", id, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var t models.Tool
	var desc *string
	var schemaRaw, configRaw []byte
	err = h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, description, type, schema, config, folder_id, created_by, created_at, updated_at
		FROM tools WHERE id = $1 AND org_id = $2
	`, id, claims.OrgID).Scan(&t.ID, &t.OrgID, &t.Name, &desc, &t.Type, &schemaRaw, &configRaw, &t.FolderID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "tool not found")
		return
	}
	if desc != nil {
		t.Description = *desc
	}
	if len(schemaRaw) > 0 {
		json.Unmarshal(schemaRaw, &t.Schema)
	}
	if len(configRaw) > 0 {
		json.Unmarshal(configRaw, &t.Config)
	}
	if t.Schema == nil {
		t.Schema = models.JSONMap{}
	}
	if t.Config == nil {
		t.Config = models.JSONMap{}
	}

	writeJSON(w, http.StatusOK, t)
}

func (h *toolHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Type        string         `json:"type"`
		Schema      models.JSONMap `json:"schema"`
		Config      models.JSONMap `json:"config"`
		FolderID    *string        `json:"folder_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required")
		return
	}
	if req.Type != "builtin" && req.Type != "webhook" && req.Type != "sql_query" {
		writeError(w, http.StatusBadRequest, "type must be 'builtin', 'webhook', or 'sql_query'")
		return
	}

	if req.Schema == nil {
		req.Schema = models.JSONMap{}
	}
	if req.Config == nil {
		req.Config = models.JSONMap{}
	}

	toolID := uuid.New().String()

	_, err := h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO tools (id, org_id, name, description, type, schema, config, folder_id, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
	`, toolID, claims.OrgID, req.Name, req.Description, req.Type, req.Schema, req.Config, req.FolderID, claims.UserID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a tool with this name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.server.db.Pool.Exec(r.Context(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'tool', $2, 'user', $3, ARRAY['view','edit','delete'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, toolID, claims.UserID)

	writeJSON(w, http.StatusCreated, map[string]string{"id": toolID})
}

func (h *toolHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
	toolID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var req struct {
		Name        *string          `json:"name"`
		Description *string          `json:"description"`
		Type        *string          `json:"type"`
		Schema      *models.JSONMap  `json:"schema"`
		Config      *models.JSONMap  `json:"config"`
		FolderID    *string          `json:"folder_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	result, err := h.server.db.Pool.Exec(r.Context(), `
		UPDATE tools SET
			name = COALESCE($2, name),
			description = COALESCE($3, description),
			type = COALESCE($4, type),
			schema = COALESCE($5, schema),
			config = COALESCE($6, config),
			folder_id = COALESCE($7, folder_id),
			updated_at = NOW()
		WHERE id = $1 AND org_id = $8
	`, toolID, req.Name, req.Description, req.Type, req.Schema, req.Config, req.FolderID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "tool not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"id": toolID})
}

func (h *toolHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
	toolID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	result, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM tools WHERE id = $1 AND org_id = $2`, toolID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "tool not found")
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}
