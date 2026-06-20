package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

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
		redactToolConfig(&t)
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

	redactToolConfig(&t)
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
		 VALUES ($1, 'tool', $2, 'user', $3, ARRAY['view','edit','delete','use'])
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

func (h *toolHandlers) handleTest(w http.ResponseWriter, r *http.Request) {
	toolID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var toolType string
	var configRaw []byte
	err := h.server.db.Pool.QueryRow(r.Context(),
		`SELECT type, config FROM tools WHERE id = $1 AND org_id = $2`,
		toolID, claims.OrgID).Scan(&toolType, &configRaw)
	if err != nil {
		writeError(w, http.StatusNotFound, "tool not found")
		return
	}

	var cfg models.JSONMap
	if len(configRaw) > 0 {
		json.Unmarshal(configRaw, &cfg)
	}

	switch toolType {
	case "webhook":
		url, _ := cfg["url"].(string)
		if url == "" {
			writeError(w, http.StatusBadRequest, "webhook URL not configured")
			return
		}
		method, _ := cfg["method"].(string)
		if method == "" {
			method = "POST"
		}
		headers := make(map[string]string)
		if h, ok := cfg["headers"].(map[string]any); ok {
			for k, v := range h {
				headers[k] = fmt.Sprintf("%v", v)
			}
		}
		body := map[string]string{"test": "hnb-tool-probe"}
		bodyBytes, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, url, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		resp, err := http.DefaultClient.Do(req.WithContext(ctx))
		if err != nil {
			writeError(w, http.StatusBadGateway, "webhook call failed: "+err.Error())
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": resp.StatusCode,
			"body":   string(respBody),
		})

	case "sql_query":
		connectorID, _ := cfg["connector_id"].(string)
		query, _ := cfg["query"].(string)
		if connectorID == "" {
			writeError(w, http.StatusBadRequest, "connector_id is required")
			return
		}
		if query == "" {
			writeError(w, http.StatusBadRequest, "query is required")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"note":   "connector and query configured; execution tested at runtime",
		})

	default:
		writeError(w, http.StatusBadRequest, "testing not supported for builtin tools")
	}
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

func redactToolConfig(t *models.Tool) {
	if t.Type == "webhook" {
		if _, ok := t.Config["headers"]; ok {
			delete(t.Config, "headers")
		}
	}
}
