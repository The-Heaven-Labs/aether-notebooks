package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

var (
	errQueryConnectorNotFound = errors.New("connector not found")
	errQueryConnectorDenied   = errors.New("connector access denied")
	errQueryProvisionerDenied = errors.New("provisioner not executable")
	errQueryServiceDenied     = errors.New("service access denied")
	errQueryNotReady          = errors.New("warehouse not ready")
	errQueryConnectFailed     = errors.New("connect failed")
	errQueryUnsupported       = errors.New("unsupported connector type")
	errQueryInternal          = errors.New("internal error")
)

// openedQuery is a connector resolved into a ready executor for one identity.
// The caller owns Exec and must Close it.
type openedQuery struct {
	Exec              executor.Executor
	DialedConnectorID string
	MaxRows           int
	TimeoutSecs       int
	WarehouseID       string
	WarehouseName     string
	ServiceName       string
	CHUser            string
}

// openQuery loads the connector, applies the `use` gate for unmanaged
// connectors, resolves the execution target for managed ClickHouse, and
// returns a ready executor. Shared by cell runs (handleExecuteCell) and
// dashboard query widgets.
func (s *Server) openQuery(ctx context.Context, orgID, userID, orgRole, connectorID string, pinned bool) (*openedQuery, error) {
	var connType models.ConnectorType
	var encryptedConfig []byte
	var maxRows, timeout int
	var connectorWarehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx,
		`SELECT type, config_encrypted, max_rows, timeout_seconds, warehouse_id
		 FROM connectors WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		connectorID, orgID,
	).Scan(&connType, &encryptedConfig, &maxRows, &timeout, &connectorWarehouseID)
	if err == pgx.ErrNoRows {
		return nil, errQueryConnectorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load connector: %w", err)
	}

	// Managed ClickHouse service access is enforced against the service that
	// actually serves the run (routing preference / sole-allowed-service).
	managedClickHouse := connType == models.ConnectorClickHouse && connectorWarehouseID != nil
	if !managedClickHouse || !s.warehouseManagementEnabled() {
		useOK, err := s.checkPermission(ctx, userID, orgID, orgRole, "connector", connectorID, "use")
		if err != nil {
			return nil, fmt.Errorf("permission check: %w", err)
		}
		if !useOK {
			return nil, errQueryConnectorDenied
		}
	}

	plain, err := crypto.Decrypt(encryptedConfig, s.masterKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt connector config: %w", err)
	}
	driver, ok := executor.GetDriver(connType)
	if !ok {
		return nil, errQueryUnsupported
	}

	out := &openedQuery{DialedConnectorID: connectorID, MaxRows: maxRows, TimeoutSecs: timeout}
	switch {
	case connType == models.ConnectorClickHouse:
		userUUID, err := uuid.Parse(userID)
		if err != nil {
			return nil, fmt.Errorf("invalid user id: %w", err)
		}
		connUUID, err := uuid.Parse(connectorID)
		if err != nil {
			return nil, errQueryConnectorNotFound
		}
		target, targetErr := s.resolveExecutionTarget(ctx, userUUID, connUUID, pinned)
		switch {
		case targetErr == nil:
			conn, release, getErr := s.connPool.Get(target.Endpoint, target.CHUser, target.Config)
			if getErr != nil {
				return nil, errQueryConnectFailed
			}
			out.Exec = executor.NewPooledClickHouseExecutor(conn, release)
			out.WarehouseID = target.WarehouseID.String()
			out.WarehouseName = target.WarehouseName
			out.ServiceName = target.ConnectorName
			out.CHUser = target.CHUser
			out.DialedConnectorID = target.ConnectorID.String()
			out.MaxRows = target.MaxRows
			out.TimeoutSecs = target.TimeoutSeconds
		case errors.Is(targetErr, executor.ErrProvisionerNotExecutable):
			return nil, errQueryProvisionerDenied
		case errors.Is(targetErr, executor.ErrUnmanagedConnector):
			out.Exec, err = driver.NewExecutor(plain)
			if err != nil {
				return nil, errQueryConnectFailed
			}
		case errors.Is(targetErr, executor.ErrConnectorNotFound):
			return nil, errQueryConnectorNotFound
		case errors.Is(targetErr, executor.ErrProvisioningNotReady):
			return nil, errQueryNotReady
		case errors.Is(targetErr, executor.ErrServiceAccessDenied):
			// Preserve the concrete *ServiceAccessDeniedError in the chain so
			// writeOpenQueryError can render the allowed-services list.
			return nil, fmt.Errorf("%w: %w", errQueryServiceDenied, targetErr)
		default:
			slog.Error("resolve execution target", "connector_id", connectorID, "error", targetErr)
			return nil, errQueryInternal
		}
	default:
		out.Exec, err = driver.NewExecutor(plain)
		if err != nil {
			return nil, errQueryConnectFailed
		}
	}
	return out, nil
}

// writeOpenQueryError maps openQuery failures onto the responses shared by
// cell execution and dashboard query execution.
func writeOpenQueryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errQueryConnectorNotFound):
		writeError(w, http.StatusNotFound, "connector not found")
	case errors.Is(err, errQueryConnectorDenied):
		writeError(w, http.StatusForbidden, "you don't have permission to use this connector")
	case errors.Is(err, errQueryProvisionerDenied):
		writeError(w, http.StatusForbidden,
			"this connector is the warehouse provisioner and cannot run queries; choose another service or ask an admin to enable queries through the provisioner")
	case errors.Is(err, errQueryServiceDenied):
		var denied *executor.ServiceAccessDeniedError
		if errors.As(err, &denied) {
			writeServiceAccessDenied(w, denied)
			return
		}
		writeError(w, http.StatusForbidden, "no permitted service in warehouse")
	case errors.Is(err, errQueryNotReady):
		writeError(w, http.StatusServiceUnavailable, "warehouse provisioning is not ready")
	case errors.Is(err, errQueryUnsupported):
		writeError(w, http.StatusBadRequest, "unsupported connector type")
	case errors.Is(err, errQueryConnectFailed):
		writeError(w, http.StatusBadGateway, "failed to connect to database")
	default:
		slog.Error("open query", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resolve execution target")
	}
}
