package executor

import (
	"errors"
	"fmt"

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

	// ErrServiceAccessDenied reports that the user has no `use` grant on any
	// eligible service in the warehouse, or on the explicitly pinned connector.
	ErrServiceAccessDenied = errors.New("no permitted service in warehouse")

	// ErrServiceChoiceRequired reports ambiguous routing: several services are
	// allowed and no preference picked one. A ServiceChoiceError carries the
	// allowed list for the "choose a service" prompt.
	ErrServiceChoiceRequired = errors.New("multiple warehouse services available")

	// ErrConnectorNotFound reports that the requested connector cannot be
	// resolved: it is missing, soft-deleted, not a ClickHouse connector, or
	// linked across org boundaries. Where the underlying cause is a missing
	// row, the returned error also wraps pgx.ErrNoRows.
	ErrConnectorNotFound = errors.New("execution connector not found")
)

// ServiceChoice is one selectable service in a ServiceChoiceError, carrying
// enough to render a picker without re-querying.
type ServiceChoice struct {
	ConnectorID uuid.UUID
	Name        string
}

// ServiceChoiceError is returned when a user may use more than one service in
// a warehouse and has not recorded a routing preference.
type ServiceChoiceError struct {
	WarehouseID uuid.UUID
	Allowed     []ServiceChoice
}

func (e *ServiceChoiceError) Error() string {
	return fmt.Sprintf("warehouse %s has %d permitted services and no routing preference", e.WarehouseID, len(e.Allowed))
}

// Unwrap makes errors.Is(err, ErrServiceChoiceRequired) succeed.
func (e *ServiceChoiceError) Unwrap() error { return ErrServiceChoiceRequired }

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
	// MaxRows and TimeoutSeconds are the routed service's execution limits.
	// They can differ from the requested connector's values when a preference
	// or sole-service fallback picks another service, and callers must apply
	// the routed service's limits, not the requested connector's.
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
// errors.Is(err, ErrServiceChoiceRequired) routing, and String for redacted
// rendering.
var (
	_ error                       = (*ServiceChoiceError)(nil)
	_ interface{ Unwrap() error } = (*ServiceChoiceError)(nil)
	_ fmt.Stringer                = (*ExecutionTarget)(nil)
)
