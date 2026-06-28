package executor

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/the-heaven-labs/aether/internal/models"
)

// ConnectorDriver defines what each connector type must implement.
type ConnectorDriver interface {
	// Type returns the connector type identifier (e.g. "opensearch").
	Type() models.ConnectorType

	// ConfigSchema returns a descriptor of what config fields this
	// connector needs (for validation + future frontend form rendering).
	ConfigSchema() ConfigSchema

	// NewExecutor parses the raw config JSON and creates an executor.
	NewExecutor(rawConfig json.RawMessage) (Executor, error)

	// TestConfig validates and tests a connection from raw config.
	TestConfig(ctx context.Context, rawConfig json.RawMessage) error
}

// ConfigSchema describes the configuration fields for a connector type.
type ConfigSchema struct {
	Fields []ConfigField
}

// ConfigField describes a single configuration field.
type ConfigField struct {
	Name        string
	Type        string // "string", "int", "bool"
	Required    bool
	Default     interface{}
	Description string
}

var (
	drivers   = map[models.ConnectorType]ConnectorDriver{}
	driversMu sync.RWMutex
)

// RegisterDriver registers a connector driver for its type.
func RegisterDriver(d ConnectorDriver) {
	driversMu.Lock()
	defer driversMu.Unlock()
	drivers[d.Type()] = d
}

// GetDriver returns the driver for the given connector type.
func GetDriver(typ models.ConnectorType) (ConnectorDriver, bool) {
	driversMu.RLock()
	defer driversMu.RUnlock()
	d, ok := drivers[typ]
	return d, ok
}
