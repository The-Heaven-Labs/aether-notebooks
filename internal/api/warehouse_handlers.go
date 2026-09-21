package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/chaccess"
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

const maxWarehouseNameLength = 255

var (
	errProvisionerConnectorMissing = errors.New("provisioner connector not found in this organization")
	errProvisionerConnectorForeign = errors.New("provisioner connector must belong to the warehouse")
	errProvisionerConnectorType    = errors.New("provisioner connector must be a clickhouse connector")
	errWarehouseDeleteRefused      = errors.New("warehouse has linked connectors")
	errWarehouseIdentityCleanup    = errors.New("warehouse identity cleanup failed; warehouse not deleted")
)

// enqueueWarehouseSync schedules a reconcile for one warehouse. It is
// best-effort, matching the other warehouse sync triggers, and a no-op while
// the AETHER_CH_TABLE_PERMISSIONS kill switch is off.
func (s *Server) enqueueWarehouseSync(warehouseID uuid.UUID) {
	if s.warehouseSync == nil || !s.warehouseManagementEnabled() {
		return
	}
	s.warehouseSync.Enqueue(warehouseID)
}

// warehouseSyncForcer is implemented by the production sync worker; test
// recorders without it fall back to the debounced Enqueue.
type warehouseSyncForcer interface{ EnqueueNow(uuid.UUID) }

// enqueueWarehouseSyncNow schedules a reconcile that skips the debounce delay,
// used when a state change must converge promptly (e.g. a warehouse losing its
// provisioner). It falls back to a normal enqueue when the worker does not
// support immediacy, and is a no-op while the AETHER_CH_TABLE_PERMISSIONS kill
// switch is off.
func (s *Server) enqueueWarehouseSyncNow(warehouseID uuid.UUID) {
	if s.warehouseSync == nil || !s.warehouseManagementEnabled() {
		return
	}
	if forcer, ok := s.warehouseSync.(warehouseSyncForcer); ok {
		forcer.EnqueueNow(warehouseID)
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
func (s *Server) loadWarehouseForOrg(ctx context.Context, orgID string, warehouseID uuid.UUID) (warehouseJSON, error) {
	return scanWarehouseRow(s.db.Pool.QueryRow(ctx,
		`SELECT `+warehouseSelectColumns+` FROM warehouses WHERE id = $1 AND org_id = $2`,
		warehouseID.String(), orgID))
}

// parsePathUUID parses an {id} path value. A malformed ID is reported as not
// found rather than surfacing a Postgres cast error as a 500.
func parsePathUUID(id string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(id)
	return parsed, err == nil
}

// parseOptionalUUID decodes a RawMessage that is either a quoted UUID or JSON
// null. Missing (nil raw) is treated as null.
func parseOptionalUUID(raw json.RawMessage) (*uuid.UUID, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func uuidPointersEqual(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// parseWarehouseName trims and validates a warehouse name.
func parseWarehouseName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > maxWarehouseNameLength {
		return "", fmt.Errorf("name must be %d characters or fewer", maxWarehouseNameLength)
	}
	return name, nil
}

// countWarehouseConnectors counts the live connectors linked to a warehouse,
// scoped to the warehouse's org so a foreign warehouse's links can never leak
// into a delete decision.
func (s *Server) countWarehouseConnectors(ctx context.Context, orgID string, warehouseID uuid.UUID) (int, error) {
	var n int
	err := s.db.Pool.QueryRow(ctx, `
		SELECT count(*) FROM connectors
		WHERE warehouse_id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		warehouseID.String(), orgID).Scan(&n)
	return n, err
}

// validateWarehouseProvisioner checks that connectorID exists in orgID, is a
// ClickHouse connector, and is already linked to warehouseID. A provisioner is
// adopted by linking the connector first (or passing it at create time), so a
// connector from another warehouse can never adopt a credential namespace it
// does not own.
func (s *Server) validateWarehouseProvisioner(ctx context.Context, orgID string, warehouseID, connectorID uuid.UUID) error {
	var connectorType string
	var connectorWarehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `
		SELECT type, warehouse_id FROM connectors
		WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		connectorID.String(), orgID,
	).Scan(&connectorType, &connectorWarehouseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errProvisionerConnectorMissing
	}
	if err != nil {
		return fmt.Errorf("load provisioner connector: %w", err)
	}
	if connectorType != "clickhouse" {
		return errProvisionerConnectorType
	}
	if connectorWarehouseID == nil || *connectorWarehouseID != warehouseID {
		return errProvisionerConnectorForeign
	}
	return nil
}

// writeProvisionerValidationError maps a provisioner validation failure to a
// 400 for user errors and a 500 for lookup failures.
func writeProvisionerValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errProvisionerConnectorMissing),
		errors.Is(err, errProvisionerConnectorForeign),
		errors.Is(err, errProvisionerConnectorType):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "failed to validate provisioner connector")
	}
}

// writeWarehouseDeleteConflict reports that connectors are still linked and an
// explicit confirmation is required. force=true bypasses only this check; the
// managed ClickHouse identities are revoked either way.
func writeWarehouseDeleteConflict(w http.ResponseWriter, linked int) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": fmt.Sprintf(
			"warehouse has %d linked connector(s); deleting it unlinks them and returns them to shared-credential mode. Re-send with force=true to confirm (managed ClickHouse identities are revoked in either case)",
			linked),
		"connector_count": linked,
	})
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

// createWarehouseRequest: provisioner_connector_id may be omitted or null
// (the warehouse starts without a provisioner) or name a ClickHouse connector
// in the same org that is not yet linked, which is adopted as the warehouse's
// first service.
type createWarehouseRequest struct {
	Name                   string          `json:"name"`
	ProvisionerConnectorID json.RawMessage `json:"provisioner_connector_id"`
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
	name, err := parseWarehouseName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	provisionerID, err := parseOptionalUUID(req.ProvisionerConnectorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid provisioner_connector_id")
		return
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(ctx)

	wh, err := scanWarehouseRow(tx.QueryRow(ctx,
		`INSERT INTO warehouses (org_id, name) VALUES ($1, $2) RETURNING `+warehouseSelectColumns,
		claims.OrgID, name))
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
			WHERE id = $2 AND org_id = $3 AND type = 'clickhouse'
			  AND deleted_at IS NULL AND warehouse_id IS NULL`,
			wh.ID, provisionerID.String(), claims.OrgID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusBadRequest,
				"provisioner connector must be a clickhouse connector in this organization that is not linked to another warehouse")
			return
		}
		if _, err := tx.Exec(ctx, `UPDATE warehouses SET provisioner_connector_id = $1 WHERE id = $2`, provisionerID.String(), wh.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		// Echo the canonical parsed UUID, not the request spelling.
		canonical := provisionerID.String()
		wh.ProvisionerConnectorID = &canonical
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if provisionerID != nil {
		if warehouseUUID, parseErr := uuid.Parse(wh.ID); parseErr == nil {
			s.enqueueWarehouseSync(warehouseUUID)
		}
	}

	meta := map[string]any{"name": name}
	if provisionerID != nil {
		meta["provisioner_connector_id"] = provisionerID.String()
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "warehouse.create", ResourceType: "warehouse", ResourceID: wh.ID,
		Metadata: meta,
	})

	writeJSON(w, http.StatusCreated, wh)
}

// @Summary Get a warehouse
// @Description Get a warehouse and the ClickHouse connectors linked to it
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

	warehouseUUID, ok := parsePathUUID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}

	wh, err := s.loadWarehouseForOrg(ctx, claims.OrgID, warehouseUUID)
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
		WHERE warehouse_id = $1 AND org_id = $2 AND type = 'clickhouse' AND deleted_at IS NULL
		ORDER BY name ASC, id ASC`, warehouseUUID.String(), claims.OrgID)
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

// updateWarehouseRequest: an absent field leaves the value unchanged; an
// explicit null provisioner_connector_id clears the provisioner. name must be
// a string (it cannot be nulled) and an empty body is rejected.
type updateWarehouseRequest struct {
	Name                   json.RawMessage `json:"name"`
	ProvisionerConnectorID json.RawMessage `json:"provisioner_connector_id"`
}

// @Summary Update a warehouse
// @Description Update a warehouse's name and/or provisioner connector. An absent field leaves the value unchanged; an explicit null provisioner_connector_id clears the provisioner.
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

	warehouseUUID, ok := parsePathUUID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}

	var req updateWarehouseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == nil && req.ProvisionerConnectorID == nil {
		writeError(w, http.StatusBadRequest, "at least one field must be provided")
		return
	}

	var name *string
	if req.Name != nil {
		var raw string
		if err := json.Unmarshal(req.Name, &raw); err != nil {
			writeError(w, http.StatusBadRequest, "name must be a string")
			return
		}
		parsed, err := parseWarehouseName(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		name = &parsed
	}

	var provisionerID *uuid.UUID
	provisionerSet := req.ProvisionerConnectorID != nil
	if provisionerSet {
		parsed, err := parseOptionalUUID(req.ProvisionerConnectorID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid provisioner_connector_id")
			return
		}
		if parsed != nil {
			if err := s.validateWarehouseProvisioner(ctx, claims.OrgID, warehouseUUID, *parsed); err != nil {
				writeProvisionerValidationError(w, err)
				return
			}
		}
		provisionerID = parsed
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(ctx)

	// Re-check the connector under a share lock before locking the warehouse
	// row: the connector-move handler takes its locks in the same order, so a
	// move landing after the handler validation is caught here instead of
	// persisting a provisioner that no longer belongs to the warehouse.
	if provisionerSet && provisionerID != nil {
		if err := s.lockWarehouseProvisioner(ctx, tx, claims.OrgID, warehouseUUID, *provisionerID); err != nil {
			writeProvisionerValidationError(w, err)
			return
		}
	}

	var oldName string
	var oldProvisioner *uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT name, provisioner_connector_id FROM warehouses
		WHERE id = $1 AND org_id = $2 FOR UPDATE`,
		warehouseUUID.String(), claims.OrgID).Scan(&oldName, &oldProvisioner)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	changedName := name != nil && *name != oldName
	changedProvisioner := provisionerSet && !uuidPointersEqual(oldProvisioner, provisionerID)

	if changedName {
		if _, err := tx.Exec(ctx,
			`UPDATE warehouses SET name = $1, updated_at = now() WHERE id = $2 AND org_id = $3`,
			*name, warehouseUUID.String(), claims.OrgID); err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "a warehouse with this name already exists in your organization")
				return
			}
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
	if changedProvisioner {
		if err := s.writeWarehouseProvisioner(ctx, tx, claims.OrgID, warehouseUUID, provisionerID); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if changedProvisioner && provisionerID != nil {
		s.enqueueWarehouseSync(warehouseUUID)
	}

	if changedName || changedProvisioner {
		meta := map[string]any{}
		if changedName {
			meta["name"] = *name
			meta["previous_name"] = oldName
		}
		if changedProvisioner {
			if provisionerID != nil {
				meta["provisioner_connector_id"] = provisionerID.String()
			} else {
				meta["provisioner_connector_id"] = nil
			}
			if oldProvisioner != nil {
				meta["previous_provisioner_connector_id"] = oldProvisioner.String()
			}
		}
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "warehouse.update", ResourceType: "warehouse", ResourceID: warehouseUUID.String(),
			Metadata: meta,
		})
	}

	wh, err := s.loadWarehouseForOrg(ctx, claims.OrgID, warehouseUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, wh)
}

// writeWarehouseProvisioner applies a provisioner change on tx, resetting sync
// state so the next reconcile provisions through the new connector. A nil
// connectorID clears the provisioner.
func (s *Server) writeWarehouseProvisioner(ctx context.Context, tx pgx.Tx, orgID string, warehouseID uuid.UUID, connectorID *uuid.UUID) error {
	var value any
	if connectorID != nil {
		value = connectorID.String()
	}
	tag, err := tx.Exec(ctx, `
		UPDATE warehouses
		SET provisioner_connector_id = $1,
		    sync_status = 'pending',
		    sync_error = NULL,
		    updated_at = now()
		WHERE id = $2 AND org_id = $3`, value, warehouseID.String(), orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// lockWarehouseProvisioner re-checks a provisioner connector inside the update
// transaction with FOR SHARE. The connector-move and connector-delete handlers
// hold FOR UPDATE on the same connector row while rewriting the link, so a
// move that lands between handler validation and the provisioner write is
// caught here rather than persisted.
func (s *Server) lockWarehouseProvisioner(ctx context.Context, tx pgx.Tx, orgID string, warehouseID, connectorID uuid.UUID) error {
	var id string
	err := tx.QueryRow(ctx, `
		SELECT id FROM connectors
		WHERE id = $1 AND org_id = $2 AND type = 'clickhouse'
		  AND deleted_at IS NULL AND warehouse_id = $3
		FOR SHARE`,
		connectorID.String(), orgID, warehouseID.String()).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errProvisionerConnectorForeign
	}
	if err != nil {
		return fmt.Errorf("re-check provisioner connector: %w", err)
	}
	return nil
}

// setWarehouseProvisioner updates the provisioner under a row lock and reports
// whether the value actually changed.
func (s *Server) setWarehouseProvisioner(ctx context.Context, orgID string, warehouseID uuid.UUID, connectorID *uuid.UUID) (bool, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Lock order matches the connector-move handler: connector, then
	// warehouse.
	if connectorID != nil {
		if err := s.lockWarehouseProvisioner(ctx, tx, orgID, warehouseID, *connectorID); err != nil {
			return false, err
		}
	}

	var old *uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT provisioner_connector_id FROM warehouses
		WHERE id = $1 AND org_id = $2 FOR UPDATE`,
		warehouseID.String(), orgID).Scan(&old); err != nil {
		return false, err
	}
	changed := !uuidPointersEqual(old, connectorID)
	if changed {
		if err := s.writeWarehouseProvisioner(ctx, tx, orgID, warehouseID, connectorID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return changed, nil
}

// setWarehouseSyncPending marks a warehouse for reconciliation after its
// identities were revoked but the delete was refused.
func (s *Server) setWarehouseSyncPending(ctx context.Context, orgID string, warehouseID uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE warehouses SET sync_status = 'pending', sync_error = NULL, updated_at = now()
		WHERE id = $1 AND org_id = $2`, warehouseID.String(), orgID)
	return err
}

// setWarehouseProvisionerRequest: connector_id is required; an explicit null
// clears the provisioner, while a missing key is rejected.
type setWarehouseProvisionerRequest struct {
	ConnectorID json.RawMessage `json:"connector_id"`
}

// @Summary Set a warehouse provisioner
// @Description Designate the connector used to provision ClickHouse access for a warehouse. connector_id is required; an explicit null clears the provisioner.
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

	warehouseUUID, ok := parsePathUUID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}

	var req setWarehouseProvisionerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ConnectorID == nil {
		writeError(w, http.StatusBadRequest, "connector_id is required (use null to clear)")
		return
	}
	connectorID, err := parseOptionalUUID(req.ConnectorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid connector_id")
		return
	}
	if connectorID != nil {
		if err := s.validateWarehouseProvisioner(ctx, claims.OrgID, warehouseUUID, *connectorID); err != nil {
			writeProvisionerValidationError(w, err)
			return
		}
	}

	changed, err := s.setWarehouseProvisioner(ctx, claims.OrgID, warehouseUUID, connectorID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	if err != nil {
		writeProvisionerValidationError(w, err)
		return
	}
	if changed && connectorID != nil {
		s.enqueueWarehouseSync(warehouseUUID)
	}
	if changed {
		meta := map[string]any{}
		if connectorID != nil {
			meta["provisioner_connector_id"] = connectorID.String()
		} else {
			meta["provisioner_connector_id"] = nil
		}
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "warehouse.provisioner.set", ResourceType: "warehouse", ResourceID: warehouseUUID.String(),
			Metadata: meta,
		})
	}

	wh, err := s.loadWarehouseForOrg(ctx, claims.OrgID, warehouseUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, wh)
}

// @Summary Delete a warehouse
// @Description Delete a warehouse. Refuses with 409 while connectors are still linked unless force=true; managed ClickHouse identities are revoked before the row is removed.
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param force query bool false "Unlink linked connectors and delete"
// @Success 204
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id} [delete]
func (s *Server) handleDeleteWarehouse(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := parsePathUUID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	force := r.URL.Query().Get("force") == "true"

	if _, err := s.loadWarehouseForOrg(ctx, claims.OrgID, warehouseUUID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Refuse before revoking identities so a confirmation refusal never
	// leaves the warehouse without its managed identities.
	linked, err := s.countWarehouseConnectors(ctx, claims.OrgID, warehouseUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	if linked > 0 && !force {
		writeWarehouseDeleteConflict(w, linked)
		return
	}

	// Fail closed: never delete the row while live ClickHouse identities
	// remain, and never let force skip this step. The per-warehouse sync lock
	// is taken unconditionally — even with no provisioner configured — and the
	// header is re-read inside it, so a concurrent reconcile or
	// provisioner-set cannot create identities after the row is gone.
	//
	// With the AETHER_CH_TABLE_PERMISSIONS kill switch off the delete is
	// DB-only: the provisioner is not required and no ClickHouse connection is
	// opened, because rollback-mode operators may not have a reachable
	// provisioner. The deferred cleanup is audited and warned about below.
	lockCtx, cancel := context.WithTimeout(ctx, warehouseSyncTimeout)
	defer cancel()
	var (
		deleteErr error
		revoked   bool
	)
	deleteErr = s.withWarehouseSyncLock(lockCtx, warehouseUUID, func(lockCtx context.Context) error {
		if s.warehouseManagementEnabled() {
			var cleanupErr error
			revoked, cleanupErr = s.dropWarehouseIdentitiesLocked(lockCtx, warehouseUUID)
			if cleanupErr != nil {
				// Preserve the sentinel chain for the no-provisioner case;
				// otherwise redact defensively before the message reaches a client.
				if errors.Is(cleanupErr, errWarehouseProvisionerNeeded) {
					return fmt.Errorf("%w: %w", errWarehouseIdentityCleanup, cleanupErr)
				}
				return fmt.Errorf("%w: %s", errWarehouseIdentityCleanup, redactSecrets(cleanupErr.Error()))
			}
		}
		var rowErr error
		linked, rowErr = s.deleteWarehouseRow(lockCtx, claims.OrgID, warehouseUUID, force)
		return rowErr
	})

	switch {
	case errors.Is(deleteErr, errWarehouseProvisionerNeeded):
		// Nothing can revoke the identities; there is no state to converge.
		writeError(w, http.StatusServiceUnavailable, deleteErr.Error())
		return
	case errors.Is(deleteErr, errWarehouseSyncInProgress):
		writeError(w, http.StatusServiceUnavailable, deleteErr.Error()+"; retry")
		return
	case errors.Is(deleteErr, errWarehouseIdentityCleanup):
		// The drop may have partially applied; converge the warehouse so the
		// next reconcile restores whatever was removed.
		if pendingErr := s.setWarehouseSyncPending(ctx, claims.OrgID, warehouseUUID); pendingErr == nil {
			s.enqueueWarehouseSyncNow(warehouseUUID)
		}
		writeError(w, http.StatusServiceUnavailable, deleteErr.Error())
		return
	case errors.Is(deleteErr, errWarehouseDeleteRefused):
		// A connector was linked after the pre-check; the identities were
		// already revoked, so schedule a reconcile to restore them.
		if revoked {
			if pendingErr := s.setWarehouseSyncPending(ctx, claims.OrgID, warehouseUUID); pendingErr == nil {
				s.enqueueWarehouseSyncNow(warehouseUUID)
			}
		}
		writeWarehouseDeleteConflict(w, linked)
		return
	case errors.Is(deleteErr, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	case deleteErr != nil:
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "warehouse.delete", ResourceType: "warehouse", ResourceID: warehouseUUID.String(),
		Metadata: map[string]any{"force": force, "connector_count": linked},
	})
	if !s.warehouseManagementEnabled() {
		s.auditDeferredWarehouseIdentityCleanup(ctx, claims.OrgID, warehouseUUID)
	}

	w.WriteHeader(http.StatusNoContent)
}

// deleteWarehouseRow deletes a warehouse under a row lock, re-checking linked
// connectors so a link that lands after the pre-check cannot be silently
// unlinked by a concurrent delete. It returns errWarehouseDeleteRefused (with
// the connector count) when confirmation is still required.
func (s *Server) deleteWarehouseRow(ctx context.Context, orgID string, warehouseID uuid.UUID, force bool) (int, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Lock the warehouse's connectors before the warehouse row. Every other
	// path that touches both (move, soft-delete, set-provisioner) takes
	// connector locks first, and DELETE's ON DELETE SET NULL would otherwise
	// lock connectors after the warehouse and deadlock (40P01). The filter
	// deliberately includes soft-deleted rows so the FK action needs no new
	// locks.
	connRows, err := tx.Query(ctx, `
		SELECT id FROM connectors
		WHERE warehouse_id = $1 AND org_id = $2
		ORDER BY id FOR UPDATE`,
		warehouseID.String(), orgID)
	if err != nil {
		return 0, err
	}
	for connRows.Next() {
		var connectorID uuid.UUID
		if err := connRows.Scan(&connectorID); err != nil {
			connRows.Close()
			return 0, err
		}
	}
	connRows.Close()
	if err := connRows.Err(); err != nil {
		return 0, err
	}

	var lockedID string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM warehouses WHERE id = $1 AND org_id = $2 FOR UPDATE`,
		warehouseID.String(), orgID).Scan(&lockedID); err != nil {
		return 0, err
	}

	var linked int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM connectors
		WHERE warehouse_id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		warehouseID.String(), orgID).Scan(&linked); err != nil {
		return 0, err
	}
	if linked > 0 && !force {
		return linked, errWarehouseDeleteRefused
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM warehouses WHERE id = $1 AND org_id = $2`,
		warehouseID.String(), orgID); err != nil {
		return linked, err
	}
	if err := tx.Commit(ctx); err != nil {
		return linked, err
	}
	return linked, nil
}

// setConnectorWarehouseRequest: an absent warehouse_id leaves the link
// unchanged; an explicit null unlinks the connector.
type setConnectorWarehouseRequest struct {
	WarehouseID json.RawMessage `json:"warehouse_id"`
}

// @Summary Link a connector to a warehouse
// @Description Set or clear the warehouse a connector belongs to, changing its execution mode. An absent warehouse_id leaves the link unchanged; an explicit null unlinks the connector.
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

	connUUID, ok := parsePathUUID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}

	var req setConnectorWarehouseRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WarehouseID == nil {
		// Absent field = no change: echo the current link without mutating.
		var current *uuid.UUID
		err := s.db.Pool.QueryRow(ctx, `
			SELECT warehouse_id FROM connectors
			WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
			connUUID.String(), claims.OrgID).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "connector not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		var currentValue any
		if current != nil {
			currentValue = current.String()
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": connUUID.String(), "warehouse_id": currentValue})
		return
	}
	newWarehouseID, err := parseOptionalUUID(req.WarehouseID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid warehouse_id")
		return
	}
	if newWarehouseID != nil {
		var exists bool
		if err := s.db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM warehouses WHERE id = $1 AND org_id = $2)`,
			newWarehouseID.String(), claims.OrgID).Scan(&exists); err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		if !exists {
			writeError(w, http.StatusBadRequest, "warehouse not found in this organization")
			return
		}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	defer tx.Rollback(ctx)

	var oldWarehouseID *uuid.UUID
	var connType string
	err = tx.QueryRow(ctx, `
		SELECT warehouse_id, type FROM connectors
		WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL FOR UPDATE`,
		connUUID.String(), claims.OrgID).Scan(&oldWarehouseID, &connType)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "connector not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if newWarehouseID != nil && connType != "clickhouse" {
		writeError(w, http.StatusBadRequest, "only clickhouse connectors can be linked to a warehouse")
		return
	}

	var value any
	if newWarehouseID != nil {
		value = newWarehouseID.String()
	}
	if _, err := tx.Exec(ctx, `
		UPDATE connectors SET warehouse_id = $1, updated_at = now()
		WHERE id = $2 AND org_id = $3 AND deleted_at IS NULL`,
		value, connUUID.String(), claims.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// A preference is scoped to the connector's warehouse; stale rows for any
	// other warehouse can never be satisfied and must not survive the move.
	if _, err := tx.Exec(ctx, `
		DELETE FROM warehouse_service_preferences
		WHERE connector_id = $1 AND warehouse_id IS DISTINCT FROM $2`,
		connUUID.String(), value); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	// A warehouse that used this connector as its provisioner loses it: fail
	// closed with a pending status so an operator must pick a replacement.
	orphans, err := tx.Query(ctx, `
		UPDATE warehouses
		SET provisioner_connector_id = NULL, sync_status = 'pending',
		    sync_error = NULL, updated_at = now()
		WHERE provisioner_connector_id = $1 AND id IS DISTINCT FROM $2
		RETURNING id`,
		connUUID.String(), value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	var orphanedWarehouses []uuid.UUID
	for orphans.Next() {
		var id uuid.UUID
		if err := orphans.Scan(&id); err != nil {
			orphans.Close()
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
		orphanedWarehouses = append(orphanedWarehouses, id)
	}
	orphans.Close()
	if err := orphans.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	for _, orphanID := range orphanedWarehouses {
		s.enqueueWarehouseSyncNow(orphanID)
	}
	moved := newWarehouseID != nil && (oldWarehouseID == nil || *oldWarehouseID != *newWarehouseID)
	if moved {
		s.enqueueWarehouseSync(*newWarehouseID)
	}

	meta := map[string]any{"connector_id": connUUID.String()}
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
		Action: "connector.warehouse.set", ResourceType: "connector", ResourceID: connUUID.String(),
		Metadata: meta,
	})

	writeJSON(w, http.StatusOK, map[string]any{"id": connUUID.String(), "warehouse_id": value})
}

// maxWarehouseNewTables bounds the new-tables inbox response. A catalog with
// more never-granted tables than this needs a review workflow, not a longer
// list.
const maxWarehouseNewTables = 200

// schemaSnapshotTouchInterval throttles last_seen_at refreshes from the
// connector schema path. Schema reads are frequent (every schema browser open)
// and the inbox only needs recency ordering, so an existing row is left alone
// until this interval elapses. New tables are always inserted; the reconcile
// path refreshes unconditionally.
const schemaSnapshotTouchInterval = "15 minutes"

// recordSchemaSnapshot upserts raw catalog observations for one connector,
// refreshing last_seen_at on every call. The reconcile worker uses it; schema
// reads use touchSchemaSnapshot instead.
func (s *Server) recordSchemaSnapshot(ctx context.Context, connectorID uuid.UUID, tables []chaccess.CatalogTable) error {
	return s.writeSchemaSnapshot(ctx, connectorID, tables, false)
}

// touchSchemaSnapshot records catalog observations from a schema read without
// rewriting last_seen_at more than once per schemaSnapshotTouchInterval.
func (s *Server) touchSchemaSnapshot(ctx context.Context, connectorID uuid.UUID, tables []chaccess.CatalogTable) error {
	return s.writeSchemaSnapshot(ctx, connectorID, tables, true)
}

// writeSchemaSnapshot performs the shared upsert. Sanitizing first is
// load-bearing: a duplicate pair in the batch would otherwise abort the whole
// statement with "ON CONFLICT DO UPDATE command cannot affect row a second
// time", and names that cannot be grant object identifiers are dropped so the
// inbox can never suggest a name the grants API would reject.
func (s *Server) writeSchemaSnapshot(ctx context.Context, connectorID uuid.UUID, tables []chaccess.CatalogTable, throttled bool) error {
	tables = chaccess.SanitizeCatalogTables(tables)
	if len(tables) == 0 {
		return nil
	}
	databases := make([]string, 0, len(tables))
	names := make([]string, 0, len(tables))
	for _, t := range tables {
		databases = append(databases, t.Database)
		names = append(names, t.Table)
	}
	query := `
		INSERT INTO schema_snapshots (connector_id, database_name, table_name)
		SELECT $1::uuid, d, t FROM unnest($2::text[], $3::text[]) AS x(d, t)
		ON CONFLICT (connector_id, database_name, table_name)
		DO UPDATE SET last_seen_at = now()`
	if throttled {
		query += `
		WHERE schema_snapshots.last_seen_at < now() - interval '` + schemaSnapshotTouchInterval + `'`
	}
	if _, err := s.db.Pool.Exec(ctx, query, connectorID.String(), databases, names); err != nil {
		return fmt.Errorf("write schema snapshot: %w", err)
	}
	return nil
}

// warehouseNewTableJSON is one table in the new-tables inbox.
type warehouseNewTableJSON struct {
	Database    string    `json:"database"`
	Table       string    `json:"table"`
	FirstSeenAt time.Time `json:"first_seen_at"`
}

// warehouseNewTablesJSON is the GET /warehouses/{id}/new-tables response.
// Since echoes the cutoff actually applied: the caller's ?since= when
// supplied, otherwise the warehouse's most recent grant-creation time,
// falling back to the warehouse's creation time when it has no grants yet.
// Truncated reports that the response stopped at maxWarehouseNewTables.
type warehouseNewTablesJSON struct {
	WarehouseID string                  `json:"warehouse_id"`
	Since       time.Time               `json:"since"`
	Truncated   bool                    `json:"truncated"`
	Tables      []warehouseNewTableJSON `json:"tables"`
}

// warehouseLastGrantReview returns the cutoff used when ?since= is absent:
// the most recent grant creation time for the warehouse, or the warehouse's
// creation time when no grants exist.
func (s *Server) warehouseLastGrantReview(ctx context.Context, orgID, warehouseID string, createdAt time.Time) (time.Time, error) {
	since := createdAt
	var lastGrant *time.Time
	if err := s.db.Pool.QueryRow(ctx, `
		SELECT max(created_at) FROM warehouse_table_grants
		WHERE warehouse_id = $1 AND org_id = $2`,
		warehouseID, orgID).Scan(&lastGrant); err != nil {
		return time.Time{}, fmt.Errorf("load last grant review: %w", err)
	}
	if lastGrant != nil && lastGrant.After(since) {
		since = *lastGrant
	}
	return since, nil
}

// @Summary List warehouse tables observed since the last grant review
// @Description List catalog tables first observed on a live warehouse connector after the review cutoff and not yet granted to any subject. The cutoff is the `since` query parameter (RFC3339) when provided, otherwise the warehouse's most recent grant creation time (or its creation time when it has no grants).
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Param since query string false "RFC3339 cutoff timestamp (defaults to the last grant review)"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/new-tables [get]
func (s *Server) handleWarehouseNewTables(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := parsePathUUID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	wh, err := s.loadWarehouseForOrg(ctx, claims.OrgID, warehouseUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	since := wh.CreatedAt
	if raw := r.URL.Query().Get("since"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be an RFC3339 timestamp")
			return
		}
		since = parsed
	} else {
		since, err = s.warehouseLastGrantReview(ctx, claims.OrgID, warehouseUUID.String(), wh.CreatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
	}

	// The HAVING clause is deliberate: filtering per-snapshot first would let a
	// table seen long ago on one service and recently on another pass the
	// cutoff on the min over the surviving rows. Aggregating first, then
	// applying the cutoff to min(first_seen_at), uses the true first sighting.
	rows, err := s.db.Pool.Query(ctx, `
		SELECT s.database_name, s.table_name, min(s.first_seen_at) AS first_seen_at
		FROM schema_snapshots s
		JOIN connectors c ON c.id = s.connector_id
		WHERE c.org_id = $1
		  AND c.warehouse_id = $2
		  AND c.type = 'clickhouse'
		  AND c.deleted_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM warehouse_table_grants g
		      WHERE g.org_id = $1
		        AND g.warehouse_id = $2
		        AND g.database_name = s.database_name
		        AND g.table_name = s.table_name)
		GROUP BY s.database_name, s.table_name
		HAVING min(s.first_seen_at) > $3
		ORDER BY first_seen_at DESC, s.database_name ASC, s.table_name ASC
		LIMIT $4`,
		claims.OrgID, warehouseUUID.String(), since, maxWarehouseNewTables+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	tables := []warehouseNewTableJSON{}
	truncated := false
	for rows.Next() {
		if len(tables) == maxWarehouseNewTables {
			// One row past the cap is enough to know the list is incomplete.
			truncated = true
			break
		}
		var t warehouseNewTableJSON
		if err := rows.Scan(&t.Database, &t.Table, &t.FirstSeenAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	writeJSON(w, http.StatusOK, warehouseNewTablesJSON{
		WarehouseID: warehouseUUID.String(),
		Since:       since,
		Truncated:   truncated,
		Tables:      tables,
	})
}
