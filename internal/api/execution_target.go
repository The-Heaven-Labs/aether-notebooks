package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

// warehouseService is one connector row inside a warehouse, as loaded during
// routing. encrypted is the still-encrypted connector config; maxRows and
// timeoutSeconds are the service's own execution limits, which the resolved
// target carries so callers apply the routed service's values.
type warehouseService struct {
	id             uuid.UUID
	name           string
	encrypted      []byte
	maxRows        int
	timeoutSeconds int
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
// callers treat as the legacy shared-credential path; a requested connector
// that is missing, soft-deleted, non-ClickHouse, or linked across orgs
// returns executor.ErrConnectorNotFound.
//
// When the AETHER_CH_TABLE_PERMISSIONS kill switch is off, the connector is
// still loaded and validated first (missing, soft-deleted, non-ClickHouse, and
// cross-org links are rejected exactly as in managed mode); only then is it
// reported as unmanaged so execution falls back to the stored credential. The
// validation must not be skipped: agent and MCP callers rely on resolution to
// reject a connector the cell-level HTTP load would have caught.
func (s *Server) resolveExecutionTarget(ctx context.Context, userID uuid.UUID, requestedConnectorID uuid.UUID, pinned bool) (*executor.ExecutionTarget, error) {
	requested, requestedWarehouseID, err := s.loadServiceConnector(ctx, requestedConnectorID)
	if err != nil {
		return nil, err
	}
	if !s.warehouseManagementEnabled() {
		return nil, fmt.Errorf("connector %s: per-user ClickHouse table permissions are disabled: %w",
			requestedConnectorID, executor.ErrUnmanagedConnector)
	}
	if requestedWarehouseID == nil {
		return nil, fmt.Errorf("connector %s: %w", requestedConnectorID, executor.ErrUnmanagedConnector)
	}
	warehouseID := *requestedWarehouseID

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

	services, preferredID, err := s.allowedWarehouseServices(ctx, userID, orgID, role, warehouseID)
	if err != nil {
		return nil, err
	}
	if preferredID != nil {
		for _, svc := range services {
			if svc.id == *preferredID {
				return s.buildExecutionTarget(warehouseID, orgID, userID, svc)
			}
		}
	}

	switch len(services) {
	case 0:
		return nil, fmt.Errorf("warehouse %s: %w", warehouseID, executor.ErrServiceAccessDenied)
	case 1:
		return s.buildExecutionTarget(warehouseID, orgID, userID, services[0])
	default:
		choices := make([]executor.ServiceChoice, 0, len(services))
		for _, svc := range services {
			choices = append(choices, executor.ServiceChoice{ConnectorID: svc.id, Name: svc.name})
		}
		return nil, &executor.ServiceChoiceError{WarehouseID: warehouseID, Allowed: choices}
	}
}

// allowedWarehouseServices returns the warehouse's live ClickHouse services
// the user may execute on, in stable order, plus the user's effective routing
// preference: the stored preference only while it still names one of those
// services, and nil otherwise (unset, revoked, moved, or soft-deleted).
// Resolving the preference here keeps execution routing and effective-access
// reporting from disagreeing about stale rows.
func (s *Server) allowedWarehouseServices(ctx context.Context, userID, orgID uuid.UUID, role string, warehouseID uuid.UUID) ([]warehouseService, *uuid.UUID, error) {
	services, err := s.listWarehouseServices(ctx, warehouseID, orgID)
	if err != nil {
		return nil, nil, err
	}
	allowed := make([]warehouseService, 0, len(services))
	for _, svc := range services {
		ok, err := s.connectorUseAllowed(ctx, userID, orgID, role, svc.id)
		if err != nil {
			return nil, nil, err
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
		return nil, nil, fmt.Errorf("load service preference: %w", err)
	}
	if preferredID != nil {
		inAllowed := false
		for _, svc := range allowed {
			if svc.id == *preferredID {
				inAllowed = true
				break
			}
		}
		if !inAllowed {
			// A stale preference (revoked access, moved or soft-deleted
			// service) is ignored by routing and must not be reported either.
			preferredID = nil
		}
	}
	return allowed, preferredID, nil
}

// loadServiceConnector loads a single connector row for routing along with its
// warehouse link (nil for an unmanaged connector).
//
// Only non-deleted ClickHouse connectors pass, and a managed connector must
// live in the same org as the warehouse it points at: both guards hold in SQL
// so a mismatched row can never borrow another org's credential namespace or
// ACLs even if some write path skipped the CRUD validation. A rejected
// connector is reported as executor.ErrConnectorNotFound wrapping
// pgx.ErrNoRows; cross-org links are additionally logged (see
// warnRejectedConnectorLink) instead of looking like plain missing rows.
func (s *Server) loadServiceConnector(ctx context.Context, connectorID uuid.UUID) (warehouseService, *uuid.UUID, error) {
	var svc warehouseService
	var warehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `
		SELECT c.id, c.name, c.config_encrypted, c.max_rows, c.timeout_seconds, c.warehouse_id
		FROM connectors c
		LEFT JOIN warehouses w ON w.id = c.warehouse_id
		WHERE c.id = $1
		  AND c.deleted_at IS NULL
		  AND c.type = 'clickhouse'
		  AND (c.warehouse_id IS NULL OR w.org_id = c.org_id)`, connectorID.String()).
		Scan(&svc.id, &svc.name, &svc.encrypted, &svc.maxRows, &svc.timeoutSeconds, &warehouseID)
	if errors.Is(err, pgx.ErrNoRows) {
		s.warnRejectedConnectorLink(ctx, connectorID)
		return warehouseService{}, nil, fmt.Errorf("connector %s: %w: %w",
			connectorID, executor.ErrConnectorNotFound, err)
	}
	if err != nil {
		return warehouseService{}, nil, fmt.Errorf("connector %s: %w", connectorID, err)
	}
	return svc, warehouseID, nil
}

// warnRejectedConnectorLink logs a cross-org connector↔warehouse link that the
// guarded load rejected, so an integrity violation is visible in logs instead
// of being indistinguishable from a missing connector. It is best-effort: any
// lookup failure is ignored and must never mask the not-found result.
func (s *Server) warnRejectedConnectorLink(ctx context.Context, connectorID uuid.UUID) {
	var (
		deletedAt   *time.Time
		connType    string
		connOrgID   uuid.UUID
		warehouseID *uuid.UUID
		whOrgID     *uuid.UUID
	)
	err := s.db.Pool.QueryRow(ctx, `
		SELECT c.deleted_at, c.type, c.org_id, c.warehouse_id, w.org_id
		FROM connectors c
		LEFT JOIN warehouses w ON w.id = c.warehouse_id
		WHERE c.id = $1`, connectorID.String()).
		Scan(&deletedAt, &connType, &connOrgID, &warehouseID, &whOrgID)
	if err != nil {
		return
	}
	if deletedAt != nil || connType != "clickhouse" || warehouseID == nil || whOrgID == nil {
		return
	}
	if *whOrgID == connOrgID {
		return
	}
	slog.Warn("execution target resolution rejected a cross-org connector link",
		"connector_id", connectorID.String(),
		"connector_org_id", connOrgID.String(),
		"warehouse_id", warehouseID.String(),
		"warehouse_org_id", whOrgID.String())
}

// listWarehouseServices lists the non-deleted ClickHouse connectors of a
// warehouse in a stable order (name, then ID) for deterministic choice
// prompts. orgID is the warehouse's org, so cross-org rows are filtered out
// here too.
func (s *Server) listWarehouseServices(ctx context.Context, warehouseID, orgID uuid.UUID) ([]warehouseService, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, name, config_encrypted, max_rows, timeout_seconds
		FROM connectors
		WHERE warehouse_id = $1 AND org_id = $2 AND type = 'clickhouse' AND deleted_at IS NULL
		ORDER BY name ASC, id ASC`, warehouseID.String(), orgID.String())
	if err != nil {
		return nil, fmt.Errorf("list warehouse %s services: %w", warehouseID, err)
	}
	defer rows.Close()

	var services []warehouseService
	for rows.Next() {
		var svc warehouseService
		if err := rows.Scan(&svc.id, &svc.name, &svc.encrypted, &svc.maxRows, &svc.timeoutSeconds); err != nil {
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
	cfg.User = chUser
	cfg.Password = chaccess.DerivePassword(s.masterKey, warehouseID, userID)

	port := cfg.Port
	if port == 0 {
		port = executor.DefaultClickHousePort
	}

	return &executor.ExecutionTarget{
		WarehouseID:    warehouseID,
		ConnectorID:    svc.id,
		ConnectorName:  svc.name,
		Endpoint:       fmt.Sprintf("%s:%d", cfg.Host, port),
		Config:         cfg,
		CHUser:         chUser,
		MaxRows:        svc.maxRows,
		TimeoutSeconds: svc.timeoutSeconds,
	}, nil
}
