package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

type createConnectorRequest struct {
	Name           string                 `json:"name"`
	Type           models.ConnectorType   `json:"type"`
	Config         models.ConnectorConfig `json:"config"`
	IsDefault      bool                   `json:"is_default"`
	FolderID       *string                `json:"folder_id,omitempty"`
	TableAllowlist []string               `json:"table_allowlist,omitempty"`
	TableDenylist  []string               `json:"table_denylist,omitempty"`
}

// @Summary Create a connector
// @Description Create a new database connector
// @Tags connectors
// @Accept json
// @Produce json
// @Param request body object true "Connector details"
// @Success 201 {object} models.Connector
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /connectors [post]
func (s *Server) handleCreateConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req createConnectorRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.Type == "" {
		writeError(w, http.StatusBadRequest, "name and type are required")
		return
	}
	if _, ok := executor.GetDriver(req.Type); !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported connector type: %s", req.Type))
		return
	}

	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid config")
		return
	}

	encrypted, err := crypto.Encrypt(configJSON, s.masterKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt config")
		return
	}

	ctx := r.Context()
	var id, orgID, name string
	var connType models.ConnectorType
	var maxRows, timeout int
	var isDefault bool
	var folderID *string

	if req.IsDefault {
		tx, err := s.db.Pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer tx.Rollback(ctx)

		if _, err := tx.Exec(ctx,
			`UPDATE connectors SET is_default=false WHERE org_id=$1`, claims.OrgID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		err = tx.QueryRow(ctx,
			`INSERT INTO connectors (org_id, name, type, config_encrypted, is_default, folder_id, created_by, table_allowlist, table_denylist)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 RETURNING id, org_id, name, type, max_rows, timeout_seconds, is_default, folder_id`,
			claims.OrgID, req.Name, req.Type, encrypted, true, req.FolderID, claims.UserID, req.TableAllowlist, req.TableDenylist,
		).Scan(&id, &orgID, &name, &connType, &maxRows, &timeout, &isDefault, &folderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create connector")
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	} else {
		err = s.db.Pool.QueryRow(ctx,
			`INSERT INTO connectors (org_id, name, type, config_encrypted, folder_id, created_by, table_allowlist, table_denylist)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 RETURNING id, org_id, name, type, max_rows, timeout_seconds, is_default, folder_id`,
			claims.OrgID, req.Name, req.Type, encrypted, req.FolderID, claims.UserID, req.TableAllowlist, req.TableDenylist,
		).Scan(&id, &orgID, &name, &connType, &maxRows, &timeout, &isDefault, &folderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create connector")
			return
		}
	}

	// Mask password in response
	req.Config.Password = "***"
	conn := models.Connector{
		ID: id, OrgID: orgID, Name: name, Type: connType,
		Config: req.Config, MaxRows: maxRows, TimeoutSeconds: timeout, IsDefault: isDefault,
		FolderID: folderID,
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "connector.create", ResourceType: "connector", ResourceID: id,
	})

	writeJSON(w, http.StatusCreated, conn)
}

// @Summary Get connector
// @Description Get a single connector by ID
// @Tags connectors
// @Produce json
// @Param id path string true "Connector ID"
// @Success 200 {object} models.Connector
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id} [get]
func (s *Server) handleGetConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", id, "view")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	var c models.Connector
	var encryptedConfig []byte
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, is_default, created_at, updated_at, folder_id, table_allowlist, table_denylist
		 FROM connectors WHERE id=$1 AND org_id=$2`,
		id, claims.OrgID,
	).Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
		&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.TableAllowlist, &c.TableDenylist)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	if plain, err := crypto.Decrypt(encryptedConfig, s.masterKey); err == nil {
		json.Unmarshal(plain, &c.Config)
		c.Config.Password = "***"
	}

	writeJSON(w, http.StatusOK, c)
}

// @Summary List connectors
// @Description List all connectors for the current organization
// @Tags connectors
// @Produce json
// @Success 200 {array} models.Connector
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /connectors [get]
func (s *Server) handleListConnectors(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, is_default, created_at, updated_at, folder_id, table_allowlist, table_denylist
		 FROM connectors WHERE org_id = $1 AND deleted_at IS NULL ORDER BY name ASC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type connectorWithPerms struct {
		models.Connector
		CanUse bool `json:"can_use"`
	}
	var result []connectorWithPerms
	for rows.Next() {
		var c models.Connector
		var encryptedConfig []byte
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
			&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.TableAllowlist, &c.TableDenylist); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		// Filter by permission: only return connectors user can view
		allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "view")
		if !allowed {
			continue
		}
		canUse, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "use")
		// Decrypt and mask password
		if plain, err := crypto.Decrypt(encryptedConfig, s.masterKey); err == nil {
			json.Unmarshal(plain, &c.Config)
			c.Config.Password = "***"
		}
		result = append(result, connectorWithPerms{Connector: c, CanUse: canUse})
	}

	if result == nil {
		result = []connectorWithPerms{}
	}

	writeJSON(w, http.StatusOK, result)
}

type updateConnectorRequest struct {
	Name           *string                 `json:"name,omitempty"`
	Config         *models.ConnectorConfig `json:"config,omitempty"`
	IsDefault      *bool                   `json:"is_default,omitempty"`
	TableAllowlist []string                `json:"table_allowlist,omitempty"`
	TableDenylist  []string                `json:"table_denylist,omitempty"`
}

// @Summary Update a connector
// @Description Update a database connector's configuration
// @Tags connectors
// @Accept json
// @Produce json
// @Param id path string true "Connector ID"
// @Param request body object true "Connector updates"
// @Success 200 {object} models.Connector
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id} [put]
func (s *Server) handleUpdateConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	ctx := r.Context()

	var req updateConnectorRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var orgID string
	err := s.db.Pool.QueryRow(ctx, `SELECT org_id FROM connectors WHERE id=$1`, id).Scan(&orgID)
	if err != nil || orgID != claims.OrgID {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	if req.Name != nil {
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE connectors SET name=$1, updated_at=NOW() WHERE id=$2 AND org_id=$3`,
			*req.Name, id, claims.OrgID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	if req.Config != nil {
		configJSON, err := json.Marshal(req.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid config")
			return
		}
		encrypted, err := crypto.Encrypt(configJSON, s.masterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt config")
			return
		}
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE connectors SET config_encrypted=$1, updated_at=NOW() WHERE id=$2 AND org_id=$3`,
			encrypted, id, claims.OrgID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	if req.IsDefault != nil && *req.IsDefault {
		tx, err := s.db.Pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `UPDATE connectors SET is_default=false WHERE org_id=$1`, claims.OrgID); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		if _, err := tx.Exec(ctx, `UPDATE connectors SET is_default=true WHERE id=$1`, id); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	if req.TableAllowlist != nil || req.TableDenylist != nil {
		allowlist := req.TableAllowlist
		denylist := req.TableDenylist
		if allowlist == nil {
			// Keep existing allowlist if not provided
			var existingAllowlist []string
			s.db.Pool.QueryRow(ctx, `SELECT table_allowlist FROM connectors WHERE id=$1`, id).Scan(&existingAllowlist)
			allowlist = existingAllowlist
		}
		if denylist == nil {
			// Keep existing denylist if not provided
			var existingDenylist []string
			s.db.Pool.QueryRow(ctx, `SELECT table_denylist FROM connectors WHERE id=$1`, id).Scan(&existingDenylist)
			denylist = existingDenylist
		}
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE connectors SET table_allowlist=$1, table_denylist=$2, updated_at=NOW() WHERE id=$3 AND org_id=$4`,
			allowlist, denylist, id, claims.OrgID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	var c models.Connector
	var encryptedConfig []byte
	err = s.db.Pool.QueryRow(ctx,
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, is_default, created_at, updated_at, folder_id, table_allowlist, table_denylist
		 FROM connectors WHERE id=$1`,
		id,
	).Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
		&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.TableAllowlist, &c.TableDenylist)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if plain, err := crypto.Decrypt(encryptedConfig, s.masterKey); err == nil {
		json.Unmarshal(plain, &c.Config)
		c.Config.Password = "***"
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "connector.update", ResourceType: "connector", ResourceID: id,
	})

	writeJSON(w, http.StatusOK, c)
}

// @Summary Set default connector
// @Description Set a connector as the default for the organization
// @Tags connectors
// @Param id path string true "Connector ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id}/default [put]
func (s *Server) handleSetDefaultConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")

	tx, err := s.db.Pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(r.Context())

	// Clear existing default for this org
	if _, err := tx.Exec(r.Context(),
		`UPDATE connectors SET is_default=false WHERE org_id=$1`, claims.OrgID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	// Set this connector as default (also verifies org ownership)
	var connID string
	err = tx.QueryRow(r.Context(),
		`UPDATE connectors SET is_default=true WHERE id=$1 AND org_id=$2 RETURNING id`,
		id, claims.OrgID,
	).Scan(&connID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Delete a connector
// @Description Delete a database connector
// @Tags connectors
// @Param id path string true "Connector ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id} [delete]
func (s *Server) handleDeleteConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	result, err := s.db.Pool.Exec(ctx,
		`UPDATE connectors SET deleted_at = NOW() WHERE id = $1 AND org_id = $2`,
		connID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "connector.delete", ResourceType: "connector", ResourceID: connID,
	})

	w.WriteHeader(http.StatusNoContent)
}

// loadConnectorRow fetches the connector type and encrypted config, scoped to orgID.
// Returns pgx.ErrNoRows if not found, so callers can distinguish 404 from 500.
func (s *Server) loadConnectorRow(ctx context.Context, connID, orgID string) (models.ConnectorType, []byte, error) {
	var configEnc []byte
	var connType models.ConnectorType
	err := s.db.Pool.QueryRow(ctx,
		`SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
		connID, orgID,
	).Scan(&connType, &configEnc)
	return connType, configEnc, err
}

// loadConnectorWithFilters fetches the connector type, encrypted config, and table filters.
func (s *Server) loadConnectorWithFilters(ctx context.Context, connID, orgID string) (models.ConnectorType, []byte, []string, []string, error) {
	var configEnc []byte
	var connType models.ConnectorType
	var allowlist, denylist []string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT type, config_encrypted, table_allowlist, table_denylist FROM connectors WHERE id = $1 AND org_id = $2`,
		connID, orgID,
	).Scan(&connType, &configEnc, &allowlist, &denylist)
	return connType, configEnc, allowlist, denylist, err
}

// buildExecutor decrypts connector config and constructs the appropriate executor.
func (s *Server) buildExecutor(connType models.ConnectorType, configEnc []byte) (executor.Executor, error) {
	plain, err := crypto.Decrypt(configEnc, s.masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	driver, ok := executor.GetDriver(connType)
	if !ok {
		return nil, fmt.Errorf("unsupported connector type: %s", connType)
	}
	return driver.NewExecutor(plain)
}

// @Summary Test a connector configuration
// @Description Test a database connection using raw config before saving
// @Tags connectors
// @Accept json
// @Produce json
// @Param request body object true "Connector details to test"
// @Success 200 {object} map[string]any
// @Security BearerAuth
// @Router /connectors/test [post]
func (s *Server) handleTestConnectorConfig(w http.ResponseWriter, r *http.Request) {
	var req createConnectorRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "invalid request body"})
		return
	}

	driver, ok := executor.GetDriver(req.Type)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "unsupported connector type"})
		return
	}
	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "invalid config"})
		return
	}
	if err := driver.TestConfig(r.Context(), configJSON); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// @Summary List connector databases
// @Description List all databases available through a connector
// @Tags connectors
// @Produce json
// @Param id path string true "Connector ID"
// @Success 200 {array} string
// @Failure 403 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id}/databases [get]
func (s *Server) handleListConnectorDatabases(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", connID, "use")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	connType, configEnc, err := s.loadConnectorRow(ctx, connID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to connect")
		return
	}
	defer exec.Close()

	dbs, err := exec.Databases(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to list databases")
		return
	}
	if dbs == nil {
		dbs = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"databases": dbs})
}

// @Summary Test a connector
// @Description Test connection to a database connector
// @Tags connectors
// @Produce json
// @Param id path string true "Connector ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id}/test [post]
func (s *Server) handleTestConnector(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", connID, "use")
	if err != nil || !allowed {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "insufficient permissions"})
		return
	}

	connType, configEnc, err := s.loadConnectorRow(ctx, connID, claims.OrgID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "connector not found"})
		return
	}
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "failed to connect"})
		return
	}
	defer exec.Close()

	if err := exec.TestConnection(ctx); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "connection failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// @Summary Get connector schema
// @Description Get the database schema for a connector
// @Tags connectors
// @Produce json
// @Param id path string true "Connector ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id}/schema [get]
func (s *Server) handleConnectorSchema(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	connID := r.PathValue("id")
	ctx := r.Context()

	allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", connID, "use")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	connType, configEnc, allowlist, denylist, err := s.loadConnectorWithFilters(ctx, connID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build connector")
		return
	}
	defer exec.Close()

	schema, err := exec.Schema(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "schema fetch failed")
		return
	}

	// Filter by database if specified
	if db := r.URL.Query().Get("database"); db != "" {
		var filtered []executor.TableInfo
		for _, t := range schema.Tables {
			if t.Schema == db || t.Name == db {
				filtered = append(filtered, t)
			}
		}
		schema.Tables = filtered
	}

	// Apply table allowlist/denylist filters
	if len(allowlist) > 0 || len(denylist) > 0 {
		var filtered []executor.TableInfo
		for _, t := range schema.Tables {
			tableName := t.Name
			if t.Schema != "" {
				tableName = t.Schema + "." + t.Name
			}

			// Check denylist first (deny takes precedence)
			denied := false
			for _, pattern := range denylist {
				if matched, _ := regexp.MatchString(pattern, tableName); matched {
					denied = true
					break
				}
			}
			if denied {
				continue
			}

			// Check allowlist (if specified, table must match at least one pattern)
			if len(allowlist) > 0 {
				allowed := false
				for _, pattern := range allowlist {
					if matched, _ := regexp.MatchString(pattern, tableName); matched {
						allowed = true
						break
					}
				}
				if !allowed {
					continue
				}
			}

			filtered = append(filtered, t)
		}
		schema.Tables = filtered
	}

	writeJSON(w, http.StatusOK, schema)
}
