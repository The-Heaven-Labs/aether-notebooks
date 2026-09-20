package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

// warehouseService is one connector row inside a warehouse, as loaded during
// routing. encrypted is the still-encrypted connector config.
type warehouseService struct {
	id          uuid.UUID
	name        string
	warehouseID *uuid.UUID
	encrypted   []byte
}

// resolveExecutionTarget implements the routing rules:
//  1. A pinned connector must be usable directly (use on that exact service).
//  2. Otherwise, resolve the warehouse from the requested connector.
//  3. Allowed services = connectors in the warehouse with `use` for the user.
//  4. Preference wins when allowed; a sole allowed service is used directly;
//     several allowed services without a preference return
//     executor.ErrServiceChoiceRequired.
//
// Managed warehouses must be ready before any routing happens. A requested
// connector without a warehouse returns executor.ErrUnmanagedConnector, which
// callers treat as the legacy shared-credential path.
func (s *Server) resolveExecutionTarget(ctx context.Context, userID uuid.UUID, requestedConnectorID uuid.UUID, pinned bool) (*executor.ExecutionTarget, error) {
	requested, err := s.loadServiceConnector(ctx, requestedConnectorID)
	if err != nil {
		return nil, err
	}
	if requested.warehouseID == nil {
		return nil, fmt.Errorf("connector %s: %w", requestedConnectorID, executor.ErrUnmanagedConnector)
	}
	warehouseID := *requested.warehouseID

	var orgID uuid.UUID
	var syncStatus string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT org_id, sync_status FROM warehouses WHERE id = $1`, warehouseID.String()).
		Scan(&orgID, &syncStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("warehouse %s not found for connector %s: %w", warehouseID, requestedConnectorID, err)
	}
	if err != nil {
		return nil, fmt.Errorf("load warehouse %s: %w", warehouseID, err)
	}
	if syncStatus != "ready" {
		return nil, fmt.Errorf("warehouse %s sync_status %q: %w", warehouseID, syncStatus, executor.ErrProvisioningNotReady)
	}

	var role string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`,
		orgID.String(), userID.String()).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user %s is not a member of org %s: %w", userID, orgID, executor.ErrServiceAccessDenied)
	}
	if err != nil {
		return nil, fmt.Errorf("load org role: %w", err)
	}

	if pinned {
		allowed, err := s.connectorUseAllowed(ctx, userID, orgID, role, requested.id)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return nil, fmt.Errorf("pinned connector %s: %w", requestedConnectorID, executor.ErrServiceAccessDenied)
		}
		return s.buildExecutionTarget(warehouseID, orgID, userID, requested)
	}

	services, err := s.listWarehouseServices(ctx, warehouseID, orgID)
	if err != nil {
		return nil, err
	}
	allowed := make([]warehouseService, 0, len(services))
	for _, svc := range services {
		ok, err := s.connectorUseAllowed(ctx, userID, orgID, role, svc.id)
		if err != nil {
			return nil, err
		}
		if ok {
			allowed = append(allowed, svc)
		}
	}

	var preferredID *uuid.UUID
	err = s.db.Pool.QueryRow(ctx,
		`SELECT connector_id FROM warehouse_service_preferences WHERE user_id = $1 AND warehouse_id = $2`,
		userID.String(), warehouseID.String()).Scan(&preferredID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load service preference: %w", err)
	}
	if preferredID != nil {
		for _, svc := range allowed {
			if svc.id == *preferredID {
				return s.buildExecutionTarget(warehouseID, orgID, userID, svc)
			}
		}
		// A stale preference (revoked access, moved or soft-deleted service)
		// is ignored; the normal fallback below still applies.
	}

	switch len(allowed) {
	case 0:
		return nil, fmt.Errorf("warehouse %s: %w", warehouseID, executor.ErrServiceAccessDenied)
	case 1:
		return s.buildExecutionTarget(warehouseID, orgID, userID, allowed[0])
	default:
		choices := make([]executor.ServiceChoice, 0, len(allowed))
		for _, svc := range allowed {
			choices = append(choices, executor.ServiceChoice{ConnectorID: svc.id, Name: svc.name})
		}
		return nil, &executor.ServiceChoiceError{WarehouseID: warehouseID, Allowed: choices}
	}
}

// loadServiceConnector loads a single non-deleted connector row for routing.
// A missing or soft-deleted connector is reported as pgx.ErrNoRows.
//
// A managed connector must live in the same org as the warehouse it points
// at: the join rejects a mismatched row even if some other write path skipped
// the CRUD validation, so it can never borrow another org's credential
// namespace or ACLs.
func (s *Server) loadServiceConnector(ctx context.Context, connectorID uuid.UUID) (warehouseService, error) {
	var svc warehouseService
	err := s.db.Pool.QueryRow(ctx, `
		SELECT c.id, c.name, c.warehouse_id, c.config_encrypted
		FROM connectors c
		LEFT JOIN warehouses w ON w.id = c.warehouse_id
		WHERE c.id = $1 AND c.deleted_at IS NULL
		  AND (c.warehouse_id IS NULL OR w.org_id = c.org_id)`, connectorID.String()).
		Scan(&svc.id, &svc.name, &svc.warehouseID, &svc.encrypted)
	if err != nil {
		return warehouseService{}, fmt.Errorf("connector %s: %w", connectorID, err)
	}
	return svc, nil
}

// listWarehouseServices lists the non-deleted connectors of a warehouse in a
// stable order (name, then ID) for deterministic choice prompts. orgID is the
// warehouse's org, so cross-org rows are filtered out here too.
func (s *Server) listWarehouseServices(ctx context.Context, warehouseID, orgID uuid.UUID) ([]warehouseService, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, name, warehouse_id, config_encrypted
		FROM connectors
		WHERE warehouse_id = $1 AND org_id = $2 AND deleted_at IS NULL
		ORDER BY name ASC, id ASC`, warehouseID.String(), orgID.String())
	if err != nil {
		return nil, fmt.Errorf("list warehouse %s services: %w", warehouseID, err)
	}
	defer rows.Close()

	var services []warehouseService
	for rows.Next() {
		var svc warehouseService
		if err := rows.Scan(&svc.id, &svc.name, &svc.warehouseID, &svc.encrypted); err != nil {
			return nil, fmt.Errorf("scan warehouse %s service: %w", warehouseID, err)
		}
		services = append(services, svc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate warehouse %s services: %w", warehouseID, err)
	}
	return services, nil
}

// connectorUseAllowed checks the `use` action for one connector. Access is
// resolved against ACLs; an org admin only bypasses them in admin mode, which
// checkPermission reads from the context.
func (s *Server) connectorUseAllowed(ctx context.Context, userID, orgID uuid.UUID, role string, connectorID uuid.UUID) (bool, error) {
	allowed, err := s.checkPermission(ctx, userID.String(), orgID.String(), role, "connector", connectorID.String(), "use")
	if err != nil {
		return false, fmt.Errorf("check connector %s use: %w", connectorID, err)
	}
	return allowed, nil
}

// buildExecutionTarget decrypts the chosen service's endpoint config and
// replaces its stored credential with the warehouse-scoped per-user
// ClickHouse identity. The stored credential is never returned to callers.
func (s *Server) buildExecutionTarget(warehouseID, orgID, userID uuid.UUID, svc warehouseService) (*executor.ExecutionTarget, error) {
	plain, err := crypto.Decrypt(svc.encrypted, s.masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt connector %s config: %w", svc.id, err)
	}
	var cfg models.ConnectorConfig
	if err := json.Unmarshal(plain, &cfg); err != nil {
		return nil, fmt.Errorf("parse connector %s config: %w", svc.id, err)
	}

	chUser := chaccess.UserIdent(warehouseID, orgID, userID)
	password := chaccess.DerivePassword(s.masterKey, warehouseID, userID)
	cfg.User = chUser
	cfg.Password = password

	return &executor.ExecutionTarget{
		WarehouseID:   warehouseID,
		ConnectorID:   svc.id,
		ConnectorName: svc.name,
		Endpoint:      fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Config:        cfg,
		CHUser:        chUser,
		Password:      password,
	}, nil
}
