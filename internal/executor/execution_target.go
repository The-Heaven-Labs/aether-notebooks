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

// Compile-time guards: callers rely on Error for messages and on Unwrap for
// errors.Is(err, ErrServiceChoiceRequired) routing.
var (
	_ error                       = (*ServiceChoiceError)(nil)
	_ interface{ Unwrap() error } = (*ServiceChoiceError)(nil)
)

// ExecutionTarget is a fully resolved per-user ClickHouse execution endpoint:
// the warehouse that governs access, the service connector to dial, and the
// derived per-user identity. It is self-contained so callers can open a pooled
// connection without re-querying or re-decrypting anything.
type ExecutionTarget struct {
	WarehouseID   uuid.UUID
	ConnectorID   uuid.UUID
	ConnectorName string
	// Endpoint is the "host:port" connection key (also what the pool keys on).
	Endpoint string
	// Config is the decrypted connector config carrying the selected
	// endpoint's host/port/TLS settings with User/Password replaced by the
	// per-user ClickHouse identity.
	Config   models.ConnectorConfig
	CHUser   string
	Password string
}
