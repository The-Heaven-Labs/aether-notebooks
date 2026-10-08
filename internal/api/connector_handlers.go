package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

type createConnectorRequest struct {
	Name           string               `json:"name"`
	Type           models.ConnectorType `json:"type"`
	Config         json.RawMessage      `json:"config"`
	IsDefault      bool                 `json:"is_default"`
	TimeoutSeconds int                  `json:"timeout_seconds"`
	FolderID       *string              `json:"folder_id,omitempty"`
	TableAllowlist []string             `json:"table_allowlist,omitempty"`
	TableDenylist  []string             `json:"table_denylist,omitempty"`
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

	if req.FolderID != nil && *req.FolderID == "" {
		req.FolderID = nil
	}

	configJSON := req.Config
	if len(configJSON) == 0 || bytes.Equal(bytes.TrimSpace(configJSON), []byte("null")) {
		configJSON = json.RawMessage(`{}`)
	}
	if !isJSONObject(configJSON) {
		writeError(w, http.StatusBadRequest, "config must be a JSON object")
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
			`INSERT INTO connectors (org_id, name, type, config_encrypted, is_default, timeout_seconds, folder_id, created_by, table_allowlist, table_denylist)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 RETURNING id, org_id, name, type, max_rows, timeout_seconds, is_default, folder_id`,
			claims.OrgID, req.Name, req.Type, encrypted, true, req.TimeoutSeconds, req.FolderID, claims.UserID, req.TableAllowlist, req.TableDenylist,
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
			`INSERT INTO connectors (org_id, name, type, config_encrypted, timeout_seconds, folder_id, created_by, table_allowlist, table_denylist)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 RETURNING id, org_id, name, type, max_rows, timeout_seconds, is_default, folder_id`,
			claims.OrgID, req.Name, req.Type, encrypted, req.TimeoutSeconds, req.FolderID, claims.UserID, req.TableAllowlist, req.TableDenylist,
		).Scan(&id, &orgID, &name, &connType, &maxRows, &timeout, &isDefault, &folderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create connector")
			return
		}
	}

	// Seed ACL entry for the creator
	_, aclErr := s.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'connector', $2::uuid, 'user', $3, ARRAY['view','use'])
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`,
		claims.OrgID, id, claims.UserID,
	)
	if aclErr != nil {
		slog.Warn("connector ACL seeding failed", "id", id, "error", aclErr)
	}

	conn := models.Connector{
		ID: id, OrgID: orgID, Name: name, Type: connType,
		Config:  s.maskedConnectorConfig(connType, encrypted),
		MaxRows: maxRows, TimeoutSeconds: timeout, IsDefault: isDefault,
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
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, is_default, created_at, updated_at, folder_id, warehouse_id, table_allowlist, table_denylist,
		        last_success_at, last_failure_at, COALESCE(last_error, '')
		 FROM connectors WHERE id=$1 AND org_id=$2`,
		id, claims.OrgID,
	).Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
		&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.WarehouseID, &c.TableAllowlist, &c.TableDenylist,
		&c.LastSuccessAt, &c.LastFailureAt, &c.LastError)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	c.Config = s.maskedConnectorConfig(c.Type, encryptedConfig)

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
		`SELECT c.id, c.org_id, c.name, c.type, c.config_encrypted, c.max_rows, c.timeout_seconds, c.is_default, c.created_at, c.updated_at, c.folder_id, c.warehouse_id, c.table_allowlist, c.table_denylist,
		        c.last_success_at, c.last_failure_at, COALESCE(c.last_error, ''),
		        EXISTS (SELECT 1 FROM warehouses wp WHERE wp.provisioner_connector_id = c.id AND wp.org_id = c.org_id) AS is_provisioner,
		        CASE WHEN w.provisioner_connector_id = c.id THEN w.allow_provisioner_execution ELSE false END AS allow_provisioner_execution
		 FROM connectors c
		 LEFT JOIN warehouses w ON w.id = c.warehouse_id
		 WHERE c.org_id = $1 AND c.deleted_at IS NULL ORDER BY c.name ASC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type connectorWithPerms struct {
		models.Connector
		CanUse        bool `json:"can_use"`
		IsProvisioner bool `json:"is_provisioner"`
	}
	var result []connectorWithPerms
	for rows.Next() {
		var c models.Connector
		var encryptedConfig []byte
		var isProvisioner, allowProvisionerExecution bool
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
			&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.WarehouseID, &c.TableAllowlist, &c.TableDenylist,
			&c.LastSuccessAt, &c.LastFailureAt, &c.LastError,
			&isProvisioner, &allowProvisionerExecution); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		// Filter by permission: only return connectors user can view
		allowed, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "view")
		if !allowed {
			continue
		}
		canUse, _ := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", c.ID, "use")
		if isProvisioner && (!allowProvisionerExecution || !s.warehouseManagementEnabled()) {
			canUse = false
		}
		// Decrypt and mask secret fields
		c.Config = s.maskedConnectorConfig(c.Type, encryptedConfig)
		result = append(result, connectorWithPerms{Connector: c, CanUse: canUse, IsProvisioner: isProvisioner})
	}

	if result == nil {
		result = []connectorWithPerms{}
	}

	writeJSON(w, http.StatusOK, result)
}

type updateConnectorRequest struct {
	Name           *string         `json:"name,omitempty"`
	Config         json.RawMessage `json:"config,omitempty"`
	IsDefault      *bool           `json:"is_default,omitempty"`
	TimeoutSeconds *int            `json:"timeout_seconds,omitempty"`
	TableAllowlist []string        `json:"table_allowlist,omitempty"`
	TableDenylist  []string        `json:"table_denylist,omitempty"`
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
	var connType models.ConnectorType
	err := s.db.Pool.QueryRow(ctx, `SELECT org_id, type FROM connectors WHERE id=$1`, id).Scan(&orgID, &connType)
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

	if req.TimeoutSeconds != nil {
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE connectors SET timeout_seconds=$1, updated_at=NOW() WHERE id=$2 AND org_id=$3`,
			*req.TimeoutSeconds, id, claims.OrgID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	if len(req.Config) > 0 {
		if !isJSONObject(req.Config) {
			writeError(w, http.StatusBadRequest, "config must be a JSON object")
			return
		}
		var existingEnc []byte
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT config_encrypted FROM connectors WHERE id=$1 AND org_id=$2`,
			id, claims.OrgID,
		).Scan(&existingEnc); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load existing config")
			return
		}
		plain, err := crypto.Decrypt(existingEnc, s.masterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decrypt config")
			return
		}
		var existing map[string]any
		if err := json.Unmarshal(plain, &existing); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to parse config")
			return
		}
		var incoming map[string]any
		if err := json.Unmarshal(req.Config, &incoming); err != nil {
			writeError(w, http.StatusBadRequest, "config must be a JSON object")
			return
		}
		merged := mergeConnectorConfig(existing, incoming, secretFieldSet(connType))
		configJSON, err := json.Marshal(merged)
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
		`SELECT id, org_id, name, type, config_encrypted, max_rows, timeout_seconds, is_default, created_at, updated_at, folder_id, warehouse_id, table_allowlist, table_denylist,
		        last_success_at, last_failure_at, COALESCE(last_error, '')
		 FROM connectors WHERE id=$1`,
		id,
	).Scan(&c.ID, &c.OrgID, &c.Name, &c.Type, &encryptedConfig,
		&c.MaxRows, &c.TimeoutSeconds, &c.IsDefault, &c.CreatedAt, &c.UpdatedAt, &c.FolderID, &c.WarehouseID, &c.TableAllowlist, &c.TableDenylist,
		&c.LastSuccessAt, &c.LastFailureAt, &c.LastError)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	c.Config = s.maskedConnectorConfig(c.Type, encryptedConfig)

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

	connUUID, err := uuid.Parse(connID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx,
		`UPDATE connectors SET deleted_at = NOW() WHERE id = $1 AND org_id = $2`,
		connUUID.String(), claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	// Connectors are soft-deleted, so the preference FK cascade never fires.
	// Remove the rows explicitly or a preference could keep pointing at a
	// deleted service.
	if _, err := tx.Exec(ctx,
		`DELETE FROM warehouse_service_preferences WHERE connector_id = $1`, connUUID.String(),
	); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	// A warehouse that used this connector as its provisioner loses it: fail
	// closed with a pending status so an operator must pick a replacement
	// instead of pointing at a soft-deleted connector.
	rows, err := tx.Query(ctx, `
		UPDATE warehouses
		SET provisioner_connector_id = NULL, sync_status = 'pending',
		    sync_error = NULL, updated_at = now()
		WHERE provisioner_connector_id = $1
		RETURNING id`, connUUID.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	var orphanedWarehouses []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "delete failed")
			return
		}
		orphanedWarehouses = append(orphanedWarehouses, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	for _, orphanID := range orphanedWarehouses {
		s.enqueueWarehouseSyncNow(orphanID)
	}

	meta := map[string]any{}
	if len(orphanedWarehouses) > 0 {
		names := make([]string, 0, len(orphanedWarehouses))
		for _, id := range orphanedWarehouses {
			names = append(names, id.String())
		}
		meta["orphaned_provisioner_warehouses"] = names
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "connector.delete", ResourceType: "connector", ResourceID: connUUID.String(),
		Metadata: meta,
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

// loadConnectorWithFilters fetches the connector type, encrypted config,
// table filters, and warehouse link.
func (s *Server) loadConnectorWithFilters(ctx context.Context, connID, orgID string) (models.ConnectorType, []byte, []string, []string, *uuid.UUID, error) {
	var configEnc []byte
	var connType models.ConnectorType
	var allowlist, denylist []string
	var warehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx,
		`SELECT type, config_encrypted, table_allowlist, table_denylist, warehouse_id FROM connectors WHERE id = $1 AND org_id = $2`,
		connID, orgID,
	).Scan(&connType, &configEnc, &allowlist, &denylist, &warehouseID)
	return connType, configEnc, allowlist, denylist, warehouseID, err
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

// connectorIsProvisioner reports whether a connector in orgID is the
// provisioner of one of that org's warehouses. Provisioner connectors are
// reserved for the reconcile worker: stored-credential introspection rejects
// them for everyone except org admins, who need the warehouse grant-management
// table browser to keep working. An invalid connector id is not a provisioner
// (downstream ACL/load handling keeps its usual 403/404 semantics).
func (s *Server) connectorIsProvisioner(ctx context.Context, orgID, connectorID string) (bool, error) {
	if !isValidUUID(connectorID) {
		return false, nil
	}
	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM warehouses WHERE org_id = $2 AND provisioner_connector_id = $1)`,
		connectorID, orgID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check provisioner connector %s: %w", connectorID, err)
	}
	return exists, nil
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
	configJSON := req.Config
	if len(configJSON) == 0 {
		configJSON = json.RawMessage(`{}`)
	}
	if !isJSONObject(configJSON) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "config must be a JSON object"})
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

	isProvisioner, err := s.connectorIsProvisioner(ctx, claims.OrgID, connID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check connector")
		return
	}
	if isProvisioner {
		if claims.Role != "admin" {
			slog.Warn("blocked provisioner connector introspection",
				"connector_id", connID, "user_id", claims.UserID, "path", r.URL.Path)
			writeError(w, http.StatusForbidden,
				"the warehouse provisioner connector is reserved for background provisioning")
			return
		}
	} else {
		allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", connID, "use")
		if err != nil || !allowed {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	connType, configEnc, err := s.loadConnectorRow(ctx, connID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		s.recordConnectorFailure(ctx, claims.OrgID, connID, err)
		writeError(w, http.StatusBadGateway, "failed to connect")
		return
	}
	defer exec.Close()

	dbs, err := exec.Databases(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to list databases")
		return
	}
	s.recordConnectorSuccess(ctx, claims.OrgID, connID)
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

	isProvisioner, err := s.connectorIsProvisioner(ctx, claims.OrgID, connID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "failed to check connector"})
		return
	}
	if isProvisioner {
		if claims.Role != "admin" {
			slog.Warn("blocked provisioner connector introspection",
				"connector_id", connID, "user_id", claims.UserID, "path", r.URL.Path)
			writeJSON(w, http.StatusOK, map[string]any{"ok": false,
				"error": "the warehouse provisioner connector is reserved for background provisioning"})
			return
		}
	} else {
		allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", connID, "use")
		if err != nil || !allowed {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "insufficient permissions"})
			return
		}
	}

	connType, configEnc, err := s.loadConnectorRow(ctx, connID, claims.OrgID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "connector not found"})
		return
	}
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		s.recordConnectorFailure(ctx, claims.OrgID, connID, err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "failed to connect"})
		return
	}
	defer exec.Close()

	if err := exec.TestConnection(ctx); err != nil {
		s.recordConnectorFailure(ctx, claims.OrgID, connID, err)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "connection failed"})
		return
	}
	s.recordConnectorSuccess(ctx, claims.OrgID, connID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// @Summary Get connector schema
// @Description Get the database schema for a connector. For a warehouse-linked ClickHouse connector the response is filtered by the warehouse's hidden-table patterns and, for non-admins, by the caller's effective table grants; matched tables that are already granted stay visible. The response may include a hidden_tables count of pattern-matched ungranted tables that were dropped.
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

	isProvisioner, err := s.connectorIsProvisioner(ctx, claims.OrgID, connID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check connector")
		return
	}
	if isProvisioner {
		if claims.Role != "admin" {
			slog.Warn("blocked provisioner connector introspection",
				"connector_id", connID, "user_id", claims.UserID, "path", r.URL.Path)
			writeError(w, http.StatusForbidden,
				"the warehouse provisioner connector is reserved for background provisioning")
			return
		}
	} else {
		allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "connector", connID, "use")
		if err != nil || !allowed {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	connType, configEnc, allowlist, denylist, warehouseID, err := s.loadConnectorWithFilters(ctx, connID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	exec, err := s.buildExecutor(connType, configEnc)
	if err != nil {
		s.recordConnectorFailure(ctx, claims.OrgID, connID, err)
		writeError(w, http.StatusBadGateway, "failed to connect")
		return
	}
	defer exec.Close()

	schema, err := exec.Schema(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, "schema fetch failed")
		return
	}
	// A successful catalog read is a success signal (D7). Query errors above
	// stay unrecorded.
	s.recordConnectorSuccess(ctx, claims.OrgID, connID)

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

	// Hidden-table patterns hide ungranted tables from every viewer. Existing
	// grants stay visible (protected) so revoke workflows keep working, and the
	// per-user effective-grant filter is applied after the snapshot below.
	var patterns []*regexp.Regexp
	var patternProtected map[tableKey]struct{}
	var effective map[tableKey]struct{}
	if warehouseID != nil && string(connType) == "clickhouse" {
		patterns = s.loadWarehouseHiddenPatterns(ctx, *warehouseID, claims.OrgID)
		if len(patterns) > 0 {
			grants, gErr := s.loadWarehouseGrantKeys(ctx, *warehouseID, claims.OrgID)
			if gErr != nil {
				slog.Warn("failed to load warehouse grants for pattern protection; serving unfiltered tables",
					"warehouse_id", *warehouseID, "connector_id", connID, "user_id", claims.UserID, "error", gErr)
				patterns = nil
			} else {
				patternProtected = grants
			}
		}
		if s.warehouseManagementEnabled() && claims.Role != "admin" {
			keys, _, gErr := s.loadEffectiveWarehouseGrants(ctx, *warehouseID, claims.OrgID, claims.UserID)
			if gErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to resolve table access")
				return
			}
			effective = keysToSet(keys)
			// No longer set patternProtected = effective here: protection is the
			// full warehouse grant set, and the per-user filter runs after the
			// snapshot and bounds the response for non-admins.
		}
	}
	hiddenTables := 0
	if len(patterns) > 0 {
		for _, t := range schema.Tables {
			key := tableKey{Database: t.Schema, Table: t.Name}
			if !matchesHiddenPattern(patterns, t.Schema, t.Name) {
				continue
			}
			if _, protected := patternProtected[key]; protected {
				continue
			}
			hiddenTables++
		}
		schema.Tables = filterVisibleSchemaTables(schema.Tables, patterns, nil, patternProtected)
	}

	// Snapshot the observed catalog so the warehouse new-tables inbox can
	// notice tables without waiting for the next reconcile. Touching is
	// throttled: schema reads are frequent and only first_seen_at matters for
	// the inbox. Only ClickHouse objects can become warehouse grants, and a
	// failed cache write must never fail a schema read.
	if string(connType) == "clickhouse" {
		tables := make([]chaccess.CatalogTable, 0, len(schema.Tables))
		for _, t := range schema.Tables {
			tables = append(tables, chaccess.CatalogTable{Database: t.Schema, Table: t.Name})
		}
		if connectorUUID, parseErr := uuid.Parse(connID); parseErr == nil {
			if cacheErr := s.touchSchemaSnapshot(ctx, connectorUUID, tables); cacheErr != nil {
				slog.Warn("connector schema snapshot write failed",
					"connector_id", connID, "error", cacheErr)
			}
		}
	}

	// The per-user filter runs after the snapshot so schema_snapshots stays a
	// warehouse-wide catalog, never one viewer's subset.
	if effective != nil {
		schema.Tables = filterVisibleSchemaTables(schema.Tables, nil, effective, nil)
	}

	// HiddenTables lets schema-browsing UIs explain why tables are missing
	// without re-deriving the pattern/grant logic client-side. It is omitted
	// when zero so the response shape is unchanged for the common case.
	writeJSON(w, http.StatusOK, struct {
		executor.SchemaInfo
		HiddenTables int `json:"hidden_tables,omitempty"`
	}{SchemaInfo: *schema, HiddenTables: hiddenTables})
}

// isJSONObject reports whether raw is a JSON object (the only config shape the
// drivers accept; arrays, strings, and null are rejected at the API boundary).
func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var obj map[string]json.RawMessage
	return json.Unmarshal(trimmed, &obj) == nil
}

// secretFieldSet returns the lowercased config keys to mask: the union of the
// driver's ConfigSchema secrets and a conservative default key list. The union
// is always applied so a driver that declares one secret cannot leave another
// credential-looking key unmasked, and keys are lowercased because Go's JSON
// decoder matches struct fields case-insensitively (a "Password" key is consumed
// by the driver even though its tag is "password").
func secretFieldSet(connType models.ConnectorType) map[string]bool {
	keys := map[string]bool{"password": true, "token": true, "client_secret": true}
	if d, ok := executor.GetDriver(connType); ok {
		for _, f := range d.ConfigSchema().Fields {
			if f.Secret {
				keys[strings.ToLower(f.Name)] = true
			}
		}
	}
	return keys
}

// mergeConnectorConfig overlays incoming onto existing. Keys declared secret
// (case-insensitively) whose incoming value is null, empty, or the API mask
// "***" are skipped, so a secret can never be replaced by a placeholder; null
// values never clear a field.
func mergeConnectorConfig(existing, incoming map[string]any, secrets map[string]bool) map[string]any {
	merged := make(map[string]any, len(existing))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range incoming {
		if v == nil {
			continue
		}
		if secrets[strings.ToLower(k)] {
			if s, ok := v.(string); ok && (s == "" || s == "***") {
				continue
			}
		}
		// Drop any case-variant of the same key: JSON decoding is
		// case-insensitive, so coexisting keys would let a stale value win.
		for existingKey := range merged {
			if existingKey != k && strings.EqualFold(existingKey, k) {
				delete(merged, existingKey)
			}
		}
		merged[k] = v
	}
	return merged
}

// maskedConnectorConfig decrypts a stored config and masks every secret key
// (case-insensitively), returning a JSON object safe for API responses.
func (s *Server) maskedConnectorConfig(connType models.ConnectorType, encrypted []byte) json.RawMessage {
	plain, err := crypto.Decrypt(encrypted, s.masterKey)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	var cfg map[string]any
	if err := json.Unmarshal(plain, &cfg); err != nil {
		return json.RawMessage(`{}`)
	}
	secrets := secretFieldSet(connType)
	for k := range cfg {
		if secrets[strings.ToLower(k)] {
			cfg[k] = "***"
		}
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}
