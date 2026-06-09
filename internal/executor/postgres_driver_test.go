package executor

import (
	"encoding/json"
	"testing"

	"github.com/heavenlabs/hnb/internal/models"
)

func TestPostgresDriver_Type(t *testing.T) {
	d := &PostgresDriver{}
	if d.Type() != models.ConnectorPostgres {
		t.Fatalf("expected %q, got %q", models.ConnectorPostgres, d.Type())
	}
}

func TestPostgresDriver_ConfigSchema(t *testing.T) {
	d := &PostgresDriver{}
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

func TestPostgresDriver_NewExecutor_InvalidConfig(t *testing.T) {
	d := &PostgresDriver{}
	_, err := d.NewExecutor(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for empty config")
	}
}
