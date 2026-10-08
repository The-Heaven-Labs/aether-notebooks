package executor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/models"
)

// Sentinel errors returned by warehouse execution-target resolution
// (api.resolveExecutionTarget). They live here, in a package importable by
// both api and agent, so every execution path classifies failures the same
// way. Each one tells the caller which path to take (or which failure to
// surface); none of them ever falls back to a connector's stored credential.
var (
	// ErrUnmanagedConnector reports a connector that is not linked to a
	// warehouse. Callers must take the legacy shared-credential path.
	ErrUnmanagedConnector = errors.New("connector is not managed by a warehouse")

	// ErrProvisioningNotReady reports a managed warehouse whose sync_status is
	// not 'ready'. Callers must fail closed instead of using the stored
	// connector credential.
	ErrProvisioningNotReady = errors.New("warehouse provisioning is not ready")

	// ErrServiceAccessDenied reports that the user has no `use` grant on the
	// requested (selected) connector. The enriched ServiceAccessDeniedError
	// carries the services the user may use in that warehouse instead.
	ErrServiceAccessDenied = errors.New("no permitted service in warehouse")

	// ErrConnectorNotFound reports that the requested connector cannot be
	// resolved: it is missing, soft-deleted, not a ClickHouse connector, or
	// linked across org boundaries. Where the underlying cause is a missing
	// row, the returned error also wraps pgx.ErrNoRows.
	ErrConnectorNotFound = errors.New("execution connector not found")

	// ErrProvisionerNotExecutable reports a connector that is its warehouse's
	// provisioner. The provisioner credential is reserved for the reconcile
	// worker; user-facing execution must fail closed instead of falling back
	// to the stored credential. Org admins can opt a warehouse into managed
	// execution through warehouses.allow_provisioner_execution.
	ErrProvisionerNotExecutable = errors.New("warehouse provisioner cannot execute user queries")
)

// ServiceChoice is one permitted service carried by a ServiceAccessDeniedError,
// so callers can render the alternatives without re-querying.
type ServiceChoice struct {
	ConnectorID uuid.UUID
	Name        string
}

// ServiceAccessDeniedError reports that the requested connector cannot serve
// the acting user: they hold no `use` grant on it. It carries the services
// the user MAY use in the same warehouse so callers can render an actionable
// message instead of a bare denial.
type ServiceAccessDeniedError struct {
	WarehouseID uuid.UUID
	Allowed     []ServiceChoice
}

func (e *ServiceAccessDeniedError) Error() string {
	names := make([]string, 0, len(e.Allowed))
	for _, svc := range e.Allowed {
		names = append(names, svc.Name)
	}
	return fmt.Sprintf("no access to the requested service; permitted services in warehouse %s: %s",
		e.WarehouseID, strings.Join(names, ", "))
}

// Unwrap makes errors.Is(err, ErrServiceAccessDenied) true for every caller
// that only needs the sentinel.
func (e *ServiceAccessDeniedError) Unwrap() error { return ErrServiceAccessDenied }

// ExecutionTarget is a fully resolved per-user ClickHouse execution endpoint:
// the warehouse that governs access, the service connector to dial, and the
// derived per-user identity. It is self-contained so callers can open a pooled
// connection without re-querying or re-decrypting anything.
//
// Config carries the per-user identity credentials in User/Password; there is
// no second password copy on the target. Never serialize or log a target
// wholesale — Config.Password is a live credential.
type ExecutionTarget struct {
	WarehouseID uuid.UUID
	// WarehouseName is the display name of the governing warehouse, carried so
	// execution results can report which warehouse served a run.
	WarehouseName string
	ConnectorID   uuid.UUID
	ConnectorName string
	// Endpoint is the "host:port" connection key (also what the pool keys on).
	Endpoint string
	// Config is the decrypted connector config carrying the selected
	// endpoint's host/port/TLS settings with User/Password replaced by the
	// per-user ClickHouse identity.
	Config models.ConnectorConfig
	// CHUser is the derived ClickHouse username, duplicated from Config.User
	// only for audit fields that should not reach into Config.
	CHUser string
	// MaxRows and TimeoutSeconds are the selected service's execution limits.
	// Selection wins, so they are the requested connector's values; callers
	// must apply them to the run.
	MaxRows        int
	TimeoutSeconds int
}

// String redacts the per-user credential: it is the only representation safe
// to embed in logs or audit context. Never serialize the target itself.
func (t *ExecutionTarget) String() string {
	if t == nil {
		return "<nil>"
	}
	return fmt.Sprintf("ExecutionTarget{warehouse=%s connector=%s endpoint=%s user=%s}",
		t.WarehouseID, t.ConnectorID, t.Endpoint, t.CHUser)
}

// Compile-time guards: callers rely on Error for messages, Unwrap for
// errors.Is routing (ErrServiceAccessDenied), and String for redacted
// rendering.
var (
	_ error                       = (*ServiceAccessDeniedError)(nil)
	_ interface{ Unwrap() error } = (*ServiceAccessDeniedError)(nil)
	_ fmt.Stringer                = (*ExecutionTarget)(nil)
)
