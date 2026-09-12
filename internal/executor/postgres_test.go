package executor_test

import (
	"context"
	"testing"
	"time"

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

// TestPostgresExecutorConnectTimeout verifies that connecting to a blackholed
// host fails within the executor's connect budget instead of hanging for the
// pgx default (2 minutes). 192.0.2.0/24 (TEST-NET-1) is reserved and unroutable.
func TestPostgresExecutorConnectTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping connect timeout test in short mode")
	}

	cfg := models.ConnectorConfig{
		Host: "192.0.2.1", Port: 5432,
		User: "aether", Password: "aether_dev", Database: "aether",
	}

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		pg, err := executor.NewPostgresExecutor(cfg)
		if pg != nil {
			pg.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error connecting to blackholed host, got nil")
		}
		if elapsed := time.Since(start); elapsed > 12*time.Second {
			t.Fatalf("connect took %v, want <= 12s", elapsed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("NewPostgresExecutor blocked for 15s on a blackholed host (unbounded connect)")
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
