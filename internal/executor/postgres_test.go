package executor_test

import (
	"context"
	"testing"

	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
)

func testConnectorConfig(t *testing.T) models.ConnectorConfig {
	t.Helper()
	return models.ConnectorConfig{
		Host: "localhost", Port: 5432,
		User: "aether", Password: "aether_dev", Database: "aether",
	}
}

func TestPostgresExecutor(t *testing.T) {
	cfg := models.ConnectorConfig{
		Host: "localhost", Port: 5432,
		User: "aether", Password: "aether_dev", Database: "aether",
	}

	pg, err := executor.NewPostgresExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer pg.Close()

	result, err := pg.Execute(context.Background(), "SELECT 1 AS num, 'hello' AS greeting", nil, 1000)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(result.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(result.Columns))
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestPostgresDatabases(t *testing.T) {
	cfg := testConnectorConfig(t)
	exec, err := executor.NewPostgresExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer exec.Close()

	dbs, err := exec.Databases(context.Background())
	if err != nil {
		t.Fatalf("databases: %v", err)
	}

	found := false
	for _, db := range dbs {
		if db == cfg.Database {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected database %q in list %v", cfg.Database, dbs)
	}
}
