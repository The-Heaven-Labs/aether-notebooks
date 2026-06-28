package executor

import (
	"encoding/json"
	"testing"

	"github.com/the-heaven-labs/aether/internal/models"
)

func TestClickHouseDriver_Type(t *testing.T) {
	d := &ClickHouseDriver{}
	if d.Type() != models.ConnectorClickHouse {
		t.Fatalf("expected %q, got %q", models.ConnectorClickHouse, d.Type())
	}
}

func TestClickHouseDriver_ConfigSchema(t *testing.T) {
	d := &ClickHouseDriver{}
	schema := d.ConfigSchema()
	if len(schema.Fields) == 0 {
		t.Fatal("expected config schema to have fields")
	}
	fieldNames := map[string]bool{}
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}
	for _, required := range []string{"host", "port", "user", "password", "database"} {
		if !fieldNames[required] {
			t.Fatalf("expected field %q in config schema", required)
		}
	}
}

func TestClickHouseDriver_NewExecutor_InvalidConfig(t *testing.T) {
	d := &ClickHouseDriver{}
	_, err := d.NewExecutor(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
