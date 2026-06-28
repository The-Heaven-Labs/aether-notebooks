package executor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/the-heaven-labs/aether/internal/models"
)

func TestRegisterAndLookupDriver(t *testing.T) {
	// Clear registry for test isolation
	origDrivers := drivers
	driversMu.Lock()
	drivers = map[models.ConnectorType]ConnectorDriver{}
	driversMu.Unlock()
	defer func() {
		driversMu.Lock()
		drivers = origDrivers
		driversMu.Unlock()
	}()

	mock := &mockDriver{typ: models.ConnectorType("mock")}
	RegisterDriver(mock)

	got, ok := GetDriver(models.ConnectorType("mock"))
	if !ok {
		t.Fatal("expected driver to be registered")
	}
	if got.Type() != models.ConnectorType("mock") {
		t.Fatalf("expected type 'mock', got %q", got.Type())
	}
}

func TestGetDriver_NotFound(t *testing.T) {
	origDrivers := drivers
	driversMu.Lock()
	drivers = map[models.ConnectorType]ConnectorDriver{}
	driversMu.Unlock()
	defer func() {
		driversMu.Lock()
		drivers = origDrivers
		driversMu.Unlock()
	}()

	_, ok := GetDriver(models.ConnectorType("nonexistent"))
	if ok {
		t.Fatal("expected driver not to be found")
	}
}

// mockDriver is a minimal implementation for testing the registry
type mockDriver struct {
	typ models.ConnectorType
}

func (m *mockDriver) Type() models.ConnectorType                                      { return m.typ }
func (m *mockDriver) ConfigSchema() ConfigSchema                                      { return ConfigSchema{} }
func (m *mockDriver) NewExecutor(rawConfig json.RawMessage) (Executor, error)         { return nil, nil }
func (m *mockDriver) TestConfig(ctx context.Context, rawConfig json.RawMessage) error { return nil }
