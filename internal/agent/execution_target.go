package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

// resolveClickHouseTarget resolves the acting user's per-user identity
// for a ClickHouse connector through the server-wired ResolveTarget callback.
//
// A nil target with a nil error means the connector is not warehouse-managed
// and the caller must take the legacy stored-credential path. Every other
// failure is returned as an error and must fail the call closed: the stored
// credential is never a fallback for a managed connector, nor for one whose
// management cannot be determined because the callback is not wired.
func resolveClickHouseTarget(tc *ToolContext, connectorID string) (*executor.ExecutionTarget, error) {
	if tc.ResolveTarget == nil {
		return nil, fmt.Errorf("cannot resolve connector %s: per-user warehouse identity resolution is not configured", connectorID)
	}
	userID, err := uuid.Parse(tc.UserID)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve connector %s: invalid acting user id %q", connectorID, tc.UserID)
	}
	connID, err := uuid.Parse(connectorID)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve connector %s: invalid connector id", connectorID)
	}

	target, err := tc.ResolveTarget(tc.Context, userID, connID, false)
	if err == nil {
		return target, nil
	}

	switch {
	case errors.Is(err, executor.ErrUnmanagedConnector):
		return nil, nil
	case errors.Is(err, executor.ErrConnectorNotFound):
		return nil, fmt.Errorf("connector %s not found", connectorID)
	case errors.Is(err, executor.ErrProvisioningNotReady):
		return nil, fmt.Errorf("warehouse for connector %s is not ready; retry once provisioning completes", connectorID)
	case errors.Is(err, executor.ErrServiceAccessDenied):
		return nil, fmt.Errorf("you do not have permission to use any service in the warehouse for connector %s", connectorID)
	case errors.Is(err, executor.ErrServiceChoiceRequired):
		var choice *executor.ServiceChoiceError
		if errors.As(err, &choice) && len(choice.Allowed) > 0 {
			names := make([]string, 0, len(choice.Allowed))
			for _, svc := range choice.Allowed {
				names = append(names, svc.Name)
			}
			return nil, fmt.Errorf("connector %s: multiple warehouse services are available (%s); set a service preference in the warehouse settings", connectorID, strings.Join(names, ", "))
		}
		return nil, fmt.Errorf("connector %s: multiple warehouse services are available; set a service preference in the warehouse settings", connectorID)
	default:
		return nil, fmt.Errorf("resolve execution target for connector %s: %w", connectorID, err)
	}
}

// openAgentExecutor builds the executor for an agent-driven query. ClickHouse
// connectors linked to a ready warehouse execute as the acting user's
// provisioned identity on a pooled connection; unmanaged ClickHouse connectors
// and every other type keep the stored-credential driver path and require the
// acting user to hold `use` on the connector.
//
// The returned target is non-nil only on the managed route, so callers can
// apply the routed service's limits and audit the warehouse identity. The
// executor owns its pooled lease: Close releases it exactly once.
func openAgentExecutor(tc *ToolContext, connType models.ConnectorType, connectorID string, configEnc []byte) (executor.Executor, *executor.ExecutionTarget, error) {
	if connType == models.ConnectorClickHouse {
		target, err := resolveClickHouseTarget(tc, connectorID)
		if err != nil {
			return nil, nil, err
		}
		if target != nil {
			if tc.ConnPool == nil {
				return nil, nil, fmt.Errorf("cannot execute connector %s: ClickHouse connection pool is not configured", connectorID)
			}
			conn, release, err := tc.ConnPool.Get(target.Endpoint, target.CHUser, target.Config)
			if err != nil {
				return nil, nil, fmt.Errorf("connect to warehouse for connector %s: %w", connectorID, err)
			}
			return executor.NewPooledClickHouseExecutor(conn, release), target, nil
		}
	}

	// Legacy stored-credential path: unmanaged ClickHouse connectors, the
	// kill-switch-off route, and every non-ClickHouse connector. Execution
	// dials the connector with its stored credential, so the acting user must
	// hold `use` on it exactly as handleExecuteCell requires for the same
	// branch. Managed ClickHouse returned above is enforced by warehouse
	// service routing instead and must not be double-checked here.
	if err := tc.CheckPermission("connector", connectorID, "use"); err != nil {
		return nil, nil, err
	}

	if tc.MasterKey == nil {
		return nil, nil, fmt.Errorf("master key not available")
	}
	plain, err := crypto.Decrypt(configEnc, tc.MasterKey)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt credentials: %w", err)
	}
	driver, ok := executor.GetDriver(connType)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported connector type: %s", connType)
	}
	exec, err := driver.NewExecutor(plain)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	return exec, nil, nil
}
