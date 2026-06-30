package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mcpServerHandlers struct {
	server *Server
}

// @Summary List MCP servers
// @Description List all MCP servers for the organization
// @Tags mcp-servers
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /mcp-servers [get]
func (h *mcpServerHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	rows, err := h.server.db.Pool.Query(r.Context(), `
		SELECT id, org_id, name, type, command, args, created_by, created_at, updated_at
		FROM mcp_servers WHERE org_id = $1 ORDER BY created_at DESC
	`, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	servers := []models.MCPServerOrg{}
	for rows.Next() {
		var s models.MCPServerOrg
		if err := rows.Scan(&s.ID, &s.OrgID, &s.Name, &s.Type, &s.Command, &s.Args, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		allowed, _ := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "mcp_server", s.ID, "view")
		if !allowed {
			continue
		}
		servers = append(servers, s)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, servers)
}

// @Summary Create an MCP server
// @Description Create a new MCP server configuration
// @Tags mcp-servers
// @Accept json
// @Produce json
// @Param request body object true "MCP server details"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /mcp-servers [post]
func (h *mcpServerHandlers) handleCreate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	var req struct {
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Name == "" || req.Command == "" {
		writeError(w, http.StatusBadRequest, "name and command are required")
		return
	}
	if req.Type != "stdio" && req.Type != "http" {
		writeError(w, http.StatusBadRequest, "type must be 'stdio' or 'http'")
		return
	}
	if req.Args == nil {
		req.Args = []string{}
	}

	id := uuid.New().String()

	_, err := h.server.db.Pool.Exec(r.Context(), `
		INSERT INTO mcp_servers (id, org_id, name, type, command, args, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
	`, id, claims.OrgID, req.Name, req.Type, req.Command, req.Args, claims.UserID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an MCP server with this name already exists in your organization")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "mcp_server.create", ResourceType: "mcp_server", ResourceID: id,
	})

	// Grant creator full access
	h.server.db.Pool.Exec(r.Context(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'mcp_server', $2, 'user', $3, ARRAY['view','edit','delete'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, id, claims.UserID)

	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// @Summary Get an MCP server
// @Description Get an MCP server by ID
// @Tags mcp-servers
// @Produce json
// @Param id path string true "MCP Server ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /mcp-servers/{id} [get]
func (h *mcpServerHandlers) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	allowed, err := h.server.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, "mcp_server", id, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var s models.MCPServerOrg
	err = h.server.db.Pool.QueryRow(r.Context(), `
		SELECT id, org_id, name, type, command, args, created_by, created_at, updated_at
		FROM mcp_servers WHERE id = $1 AND org_id = $2
	`, id, claims.OrgID).Scan(&s.ID, &s.OrgID, &s.Name, &s.Type, &s.Command, &s.Args, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mcp server not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, s)
}

// @Summary Update an MCP server
// @Description Update an MCP server configuration
// @Tags mcp-servers
// @Accept json
// @Produce json
// @Param id path string true "MCP Server ID"
// @Param request body object true "MCP server updates"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /mcp-servers/{id} [put]
func (h *mcpServerHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var req struct {
		Name    *string  `json:"name"`
		Type    *string  `json:"type"`
		Command *string  `json:"command"`
		Args    []string `json:"args"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if req.Type != nil && *req.Type != "stdio" && *req.Type != "http" {
		writeError(w, http.StatusBadRequest, "type must be 'stdio' or 'http'")
		return
	}

	result, err := h.server.db.Pool.Exec(r.Context(), `
		UPDATE mcp_servers SET
			name = COALESCE($2, name),
			type = COALESCE($3, type),
			command = COALESCE($4, command),
			args = COALESCE($5, args),
			updated_at = NOW()
		WHERE id = $1 AND org_id = $6
	`, id, req.Name, req.Type, req.Command, req.Args, claims.OrgID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "an MCP server with this name already exists in your organization")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "mcp server not found")
		return
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "mcp_server.update", ResourceType: "mcp_server", ResourceID: id,
	})

	writeJSON(w, http.StatusOK, map[string]string{"id": id})
}

// @Summary Delete an MCP server
// @Description Delete an MCP server configuration
// @Tags mcp-servers
// @Param id path string true "MCP Server ID"
// @Success 200
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /mcp-servers/{id} [delete]
func (h *mcpServerHandlers) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	result, err := h.server.db.Pool.Exec(r.Context(), `DELETE FROM mcp_servers WHERE id = $1 AND org_id = $2`, id, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "mcp server not found")
		return
	}

	h.server.audit.Log(r.Context(), audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "mcp_server.delete", ResourceType: "mcp_server", ResourceID: id,
	})

	writeJSON(w, http.StatusNoContent, nil)
}

// @Summary Test an MCP server
// @Description Test connectivity to an MCP server
// @Tags mcp-servers
// @Produce json
// @Param id path string true "MCP Server ID"
// @Success 200 {object} map[string]any
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /mcp-servers/{id}/test [post]
func (h *mcpServerHandlers) handleTestMCPServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())

	var serverType, command string
	err := h.server.db.Pool.QueryRow(r.Context(),
		`SELECT type, command FROM mcp_servers WHERE id = $1 AND org_id = $2`,
		id, claims.OrgID,
	).Scan(&serverType, &command)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "MCP server not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if serverType == "http" {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(command)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"error":   err.Error(),
			})
			return
		}
		defer resp.Body.Close()
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     resp.StatusCode < 400,
			"status_code": resp.StatusCode,
		})
	} else {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   "testing not supported for stdio MCP servers",
		})
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
