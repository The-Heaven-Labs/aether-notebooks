package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
)

// warehouseJSON is the API representation of a warehouse row. It is defined
// here rather than in models because no other package consumes it.
type warehouseJSON struct {
	ID                     string                   `json:"id"`
	OrgID                  string                   `json:"org_id"`
	Name                   string                   `json:"name"`
	ProvisionerConnectorID *string                  `json:"provisioner_connector_id"`
	SyncStatus             string                   `json:"sync_status"`
	SyncError              *string                  `json:"sync_error"`
	LastSyncedAt           *time.Time               `json:"last_synced_at"`
	CreatedAt              time.Time                `json:"created_at"`
	UpdatedAt              time.Time                `json:"updated_at"`
	Connectors             []warehouseConnectorJSON `json:"connectors,omitempty"`
}

// warehouseConnectorJSON describes one connector linked to a warehouse.
type warehouseConnectorJSON struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	IsProvisioner bool   `json:"is_provisioner"`
}

const warehouseSelectColumns = `id, org_id, name, provisioner_connector_id, sync_status, sync_error, last_synced_at, created_at, updated_at`

var (
	errProvisionerConnectorMissing = errors.New("provisioner connector not found in this organization")
	errProvisionerConnectorForeign = errors.New("provisioner connector must belong to the warehouse")
)

// enqueueWarehouseSync schedules a reconcile for one warehouse. It is
// best-effort, matching the other warehouse sync triggers.
func (s *Server) enqueueWarehouseSync(warehouseID uuid.UUID) {
	if s.warehouseSync == nil {
		return
	}
	s.warehouseSync.Enqueue(warehouseID)
}

func scanWarehouseRow(row pgx.Row) (warehouseJSON, error) {
	var wh warehouseJSON
	err := row.Scan(&wh.ID, &wh.OrgID, &wh.Name, &wh.ProvisionerConnectorID,
		&wh.SyncStatus, &wh.SyncError, &wh.LastSyncedAt, &wh.CreatedAt, &wh.UpdatedAt)
	return wh, err
}

// loadWarehouseForOrg returns a warehouse scoped to its org. pgx.ErrNoRows is
// returned unwrapped so callers can map it to 404.
func (s *Server) loadWarehouseForOrg(ctx context.Context, orgID, warehouseID string) (warehouseJSON, error) {
	return scanWarehouseRow(s.db.Pool.QueryRow(ctx,
		`SELECT `+warehouseSelectColumns+` FROM warehouses WHERE id = $1 AND org_id = $2`,
		warehouseID, orgID))
}

// validateWarehouseProvisioner checks that connectorID exists in orgID and is
// already linked to warehouseID. A provisioner is adopted by linking the
// connector first (or passing it at create time), so a connector from another
// warehouse can never adopt a credential namespace it does not own.
func (s *Server) validateWarehouseProvisioner(ctx context.Context, orgID, warehouseID string, connectorID uuid.UUID) error {
	var connectorWarehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `
		SELECT warehouse_id FROM connectors
		WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		connectorID.String(), orgID,
	).Scan(&connectorWarehouseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errProvisionerConnectorMissing
	}
	if err != nil {
		return fmt.Errorf("load provisioner connector: %w", err)
	}
	if connectorWarehouseID == nil || connectorWarehouseID.String() != warehouseID {
		return errProvisionerConnectorForeign
	}
	return nil
}

// writeProvisionerValidationError maps a provisioner validation failure to a
// 400 for user errors and a 500 for lookup failures.
func writeProvisionerValidationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errProvisionerConnectorMissing) || errors.Is(err, errProvisionerConnectorForeign) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "failed to validate provisioner connector")
}

// @Summary List warehouses
// @Description List all warehouses for the current organization
// @Tags warehouses
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses [get]
func (s *Server) handleListWarehouses(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	rows, err := s.db.Pool.Query(r.Context(),
		`SELECT `+warehouseSelectColumns+` FROM warehouses WHERE org_id = $1 ORDER BY name ASC`,
		claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	warehouses := []warehouseJSON{}
	for rows.Next() {
		var wh warehouseJSON
		if err := rows.Scan(&wh.ID, &wh.OrgID, &wh.Name, &wh.ProvisionerConnectorID,
			&wh.SyncStatus, &wh.SyncError, &wh.LastSyncedAt, &wh.CreatedAt, &wh.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		warehouses = append(warehouses, wh)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, warehouses)
}

type createWarehouseRequest struct {
	Name                   string  `json:"name"`
	ProvisionerConnectorID *string `json:"provisioner_connector_id"`
}

// @Summary Create a warehouse
// @Description Create a warehouse grouping ClickHouse connectors that share one access namespace
// @Tags warehouses
// @Accept json
// @Produce json
// @Param request body object true "Warehouse details"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses [post]
func (s *Server) handleCreateWarehouse(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var req createWarehouseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	var provisionerID *uuid.UUID
	if req.ProvisionerConnectorID != nil && strings.TrimSpace(*req.ProvisionerConnectorID) != "" {
		id, err := uuid.Parse(*req.ProvisionerConnectorID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid provisioner_connector_id")
			return
		}
		provisionerID = &id
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(ctx)

	wh, err := scanWarehouseRow(tx.QueryRow(ctx,
		`INSERT INTO warehouses (org_id, name) VALUES ($1, $2) RETURNING `+warehouseSelectColumns,
		claims.OrgID, req.Name))
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a warehouse with this name already exists in your organization")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create warehouse")
		return
	}

	// A provisioner can only be passed at create time by adopting the
	// connector into the new warehouse: the validation invariant is that a
	// provisioner always belongs to its warehouse, and a fresh warehouse has
	// no connectors yet.
	if provisionerID != nil {
		tag, err := tx.Exec(ctx, `
			UPDATE connectors SET warehouse_id = $1, updated_at = now()
			WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL AND warehouse_id IS NULL`,
			wh.ID, provisionerID.String(), claims.OrgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusBadRequest,
				"provisioner connector not found, soft-deleted, or already linked to another warehouse")
			return
		}
		if _, err := tx.Exec(ctx, `UPDATE warehouses SET provisioner_connector_id = $1 WHERE id = $2`, provisionerID.String(), wh.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		wh.ProvisionerConnectorID = req.ProvisionerConnectorID
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if provisionerID != nil {
		if warehouseUUID, err := uuid.Parse(wh.ID); err == nil {
			s.enqueueWarehouseSync(warehouseUUID)
		}
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "warehouse.create", ResourceType: "warehouse", ResourceID: wh.ID,
	})

	writeJSON(w, http.StatusCreated, wh)
}

// @Summary Get a warehouse
// @Description Get a warehouse and the connectors linked to it
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id} [get]
func (s *Server) handleGetWarehouse(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	id := r.PathValue("id")

	wh, err := s.loadWarehouseForOrg(ctx, claims.OrgID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, name, type FROM connectors
		WHERE warehouse_id = $1 AND org_id = $2 AND deleted_at IS NULL
		ORDER BY name ASC, id ASC`, id, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var c warehouseConnectorJSON
		if err := rows.Scan(&c.ID, &c.Name, &c.Type); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		c.IsProvisioner = wh.ProvisionerConnectorID != nil && *wh.ProvisionerConnectorID == c.ID
		wh.Connectors = append(wh.Connectors, c)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, wh)
}

type updateWarehouseRequest struct {
	Name                   *string `json:"name"`
	ProvisionerConnectorID *string `json:"provisioner_connector_id"`
}

// @Summary Update a warehouse
// @Description Update a warehouse's name and/or provisioner connector
// @Tags warehouses
// @Accept json
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param request body object true "Warehouse updates"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id} [put]
func (s *Server) handleUpdateWarehouse(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	id := r.PathValue("id")

	var req updateWarehouseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if _, err := s.loadWarehouseForOrg(ctx, claims.OrgID, id); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if _, err := s.db.Pool.Exec(ctx,
			`UPDATE warehouses SET name = $1, updated_at = now() WHERE id = $2 AND org_id = $3`,
			name, id, claims.OrgID); err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "a warehouse with this name already exists in your organization")
				return
			}
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	var provisionerID *uuid.UUID
	if req.ProvisionerConnectorID != nil && strings.TrimSpace(*req.ProvisionerConnectorID) != "" {
		parsed, err := uuid.Parse(*req.ProvisionerConnectorID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid provisioner_connector_id")
			return
		}
		if err := s.validateWarehouseProvisioner(ctx, claims.OrgID, id, parsed); err != nil {
			writeProvisionerValidationError(w, err)
			return
		}
		provisionerID = &parsed
	}
	if req.ProvisionerConnectorID != nil {
		if err := s.writeWarehouseProvisioner(ctx, claims.OrgID, id, provisionerID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "warehouse not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		if provisionerID != nil {
			if warehouseUUID, parseErr := uuid.Parse(id); parseErr == nil {
				s.enqueueWarehouseSync(warehouseUUID)
			}
		}
	}

	wh, err := s.loadWarehouseForOrg(ctx, claims.OrgID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "warehouse.update", ResourceType: "warehouse", ResourceID: id,
	})

	writeJSON(w, http.StatusOK, wh)
}

// writeWarehouseProvisioner applies a provisioner change, resetting sync state
// so the next reconcile provisions through the new connector. A nil
// connectorID clears the provisioner.
func (s *Server) writeWarehouseProvisioner(ctx context.Context, orgID, warehouseID string, connectorID *uuid.UUID) error {
	var value any
	if connectorID != nil {
		value = connectorID.String()
	}
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE warehouses
		SET provisioner_connector_id = $1,
		    sync_status = 'pending',
		    sync_error = NULL,
		    updated_at = now()
		WHERE id = $2 AND org_id = $3`, value, warehouseID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// @Summary Set a warehouse provisioner
// @Description Designate the connector used to provision ClickHouse access for a warehouse
// @Tags warehouses
// @Accept json
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param request body object true "Provisioner connector (null clears)"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/provisioner [put]
func (s *Server) handleSetWarehouseProvisioner(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	id := r.PathValue("id")

	var req setWarehouseProvisionerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if _, err := s.loadWarehouseForOrg(ctx, claims.OrgID, id); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	var connectorID *uuid.UUID
	if req.ConnectorID != nil && strings.TrimSpace(*req.ConnectorID) != "" {
		parsed, err := uuid.Parse(*req.ConnectorID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid connector_id")
			return
		}
		if err := s.validateWarehouseProvisioner(ctx, claims.OrgID, id, parsed); err != nil {
			writeProvisionerValidationError(w, err)
			return
		}
		connectorID = &parsed
	}

	if err := s.writeWarehouseProvisioner(ctx, claims.OrgID, id, connectorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "warehouse not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if connectorID != nil {
		if warehouseUUID, parseErr := uuid.Parse(id); parseErr == nil {
			s.enqueueWarehouseSync(warehouseUUID)
		}
	}

	meta := map[string]any{}
	if connectorID != nil {
		meta["provisioner_connector_id"] = connectorID.String()
	} else {
		meta["provisioner_connector_id"] = nil
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "warehouse.provisioner.set", ResourceType: "warehouse", ResourceID: id,
		Metadata: meta,
	})

	wh, err := s.loadWarehouseForOrg(ctx, claims.OrgID, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, wh)
}

type setWarehouseProvisionerRequest struct {
	ConnectorID *string `json:"connector_id"`
}

// @Summary Delete a warehouse
// @Description Delete a warehouse. Refuses with 409 while connectors are still linked unless force=true.
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param force query bool false "Unlink linked connectors and delete"
// @Success 204
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id} [delete]
func (s *Server) handleDeleteWarehouse(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	id := r.PathValue("id")

	// connectors.warehouse_id is ON DELETE SET NULL, so an unconfirmed delete
	// would silently downgrade linked connectors to shared-credential mode.
	var linked int
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM connectors WHERE warehouse_id = $1`, id).Scan(&linked); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if linked > 0 && r.URL.Query().Get("force") != "true" {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": fmt.Sprintf(
				"warehouse has %d linked connector(s); deleting it unlinks them and returns them to shared-credential mode. Re-send with force=true to confirm",
				linked),
			"connector_count": linked,
		})
		return
	}

	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM warehouses WHERE id = $1 AND org_id = $2`, id, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "warehouse.delete", ResourceType: "warehouse", ResourceID: id,
		Metadata: map[string]any{"connector_count": linked},
	})

	w.WriteHeader(http.StatusNoContent)
}

type setConnectorWarehouseRequest struct {
	WarehouseID *string `json:"warehouse_id"`
}

// @Summary Link a connector to a warehouse
// @Description Set or clear the warehouse a connector belongs to, changing its execution mode
// @Tags connectors
// @Accept json
// @Produce json
// @Param id path string true "Connector ID"
// @Param request body object true "Warehouse link (null unlinks)"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /connectors/{id}/warehouse [put]
func (s *Server) handleSetConnectorWarehouse(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	connID := r.PathValue("id")

	var req setConnectorWarehouseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var newWarehouseID *uuid.UUID
	if req.WarehouseID != nil && strings.TrimSpace(*req.WarehouseID) != "" {
		parsed, err := uuid.Parse(*req.WarehouseID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid warehouse_id")
			return
		}
		var exists bool
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM warehouses WHERE id = $1 AND org_id = $2)`,
			parsed.String(), claims.OrgID).Scan(&exists); err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		if !exists {
			writeError(w, http.StatusBadRequest, "warehouse not found in this organization")
			return
		}
		newWarehouseID = &parsed
	}

	var oldWarehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `
		SELECT warehouse_id FROM connectors
		WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		connID, claims.OrgID).Scan(&oldWarehouseID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	var value any
	if newWarehouseID != nil {
		value = newWarehouseID.String()
	}
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE connectors SET warehouse_id = $1, updated_at = now()
		WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL`,
		value, connID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	// Routing (and, for a provisioner, provisioning) changes on both sides of
	// a move, so converge both warehouses.
	if oldWarehouseID != nil && (newWarehouseID == nil || oldWarehouseID.String() != newWarehouseID.String()) {
		s.enqueueWarehouseSync(*oldWarehouseID)
	}
	if newWarehouseID != nil && (oldWarehouseID == nil || oldWarehouseID.String() != newWarehouseID.String()) {
		s.enqueueWarehouseSync(*newWarehouseID)
	}

	meta := map[string]any{"connector_id": connID}
	if oldWarehouseID != nil {
		meta["previous_warehouse_id"] = oldWarehouseID.String()
	}
	if newWarehouseID != nil {
		meta["warehouse_id"] = newWarehouseID.String()
	} else {
		meta["warehouse_id"] = nil
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "connector.warehouse.set", ResourceType: "connector", ResourceID: connID,
		Metadata: meta,
	})

	writeJSON(w, http.StatusOK, map[string]any{"id": connID, "warehouse_id": value})
}
