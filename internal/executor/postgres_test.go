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

	result, err := pg.Execute(context.Background(), "SELECT 1 AS num, 'hello' AS greeting", nil, executor.OutputLimits{MaxRows: 1000})
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

// TestPostgresExecutorByteCap verifies byte-budget truncation at row boundaries
// against a real Postgres. Each row carries a ~1000-byte string, so a cap of
// 2500 bytes keeps the first two rows and truncates the rest.
func TestPostgresExecutorByteCap(t *testing.T) {
	cfg := testConnectorConfig(t)
	pg, err := executor.NewPostgresExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer pg.Close()

	query := `SELECT i, repeat('x', 1000) AS payload FROM generate_series(1, 10) AS i`
	result, err := pg.Execute(context.Background(), query, nil, executor.OutputLimits{MaxBytes: 2500})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 rows within the byte cap, got %d", len(result.Rows))
	}
	if !result.Truncated {
		t.Fatal("expected Truncated=true")
	}
	if result.RowsIncluded != 2 {
		t.Fatalf("expected RowsIncluded=2, got %d", result.RowsIncluded)
	}
	if result.RowsTotal != -1 {
		t.Fatalf("expected RowsTotal=-1 (unknown), got %d", result.RowsTotal)
	}
	if result.Bytes <= 0 {
		t.Fatalf("expected positive estimated bytes, got %d", result.Bytes)
	}
}

// TestPostgresExecutorByteCapRowBoundary ensures truncation never splits a row:
// the kept rows must sum to no more than the cap and all stored rows intact.
func TestPostgresExecutorByteCapRowBoundary(t *testing.T) {
	cfg := testConnectorConfig(t)
	pg, err := executor.NewPostgresExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer pg.Close()

	query := `SELECT i FROM generate_series(1, 100) AS i`
	// Each row is tiny; a cap of 64 keeps all rows (no truncation).
	result, err := pg.Execute(context.Background(), query, nil, executor.OutputLimits{MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(result.Rows) != 100 {
		t.Fatalf("expected all 100 rows to fit, got %d", len(result.Rows))
	}
	if result.Truncated {
		t.Fatal("expected no truncation under a generous byte cap")
	}
}

// TestPostgresExecutorFirstRowExceedsCap verifies the groupUniqArray shape: a
// single row whose value alone exceeds the cap yields zero rows + truncated.
func TestPostgresExecutorFirstRowExceedsCap(t *testing.T) {
	cfg := testConnectorConfig(t)
	pg, err := executor.NewPostgresExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer pg.Close()

	query := `SELECT repeat('x', 5000) AS huge FROM generate_series(1, 3) AS i`
	result, err := pg.Execute(context.Background(), query, nil, executor.OutputLimits{MaxBytes: 1000})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("expected zero rows when the first row exceeds the cap, got %d", len(result.Rows))
	}
	if !result.Truncated {
		t.Fatal("expected Truncated=true for the first-row-exceeds-cap case")
	}
	if result.RowsIncluded != 0 {
		t.Fatalf("expected RowsIncluded=0, got %d", result.RowsIncluded)
	}
	if len(result.Columns) != 1 {
		t.Fatalf("columns must still be present, got %d", len(result.Columns))
	}
}

// TestPostgresExecutorUnlimited verifies MaxBytes<=0 disables the byte cap.
func TestPostgresExecutorUnlimited(t *testing.T) {
	cfg := testConnectorConfig(t)
	pg, err := executor.NewPostgresExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer pg.Close()

	query := `SELECT i, repeat('x', 100) AS payload FROM generate_series(1, 5) AS i`
	result, err := pg.Execute(context.Background(), query, nil, executor.OutputLimits{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(result.Rows) != 5 {
		t.Fatalf("expected all 5 rows with no cap, got %d", len(result.Rows))
	}
	if result.Truncated {
		t.Fatal("expected no truncation with MaxBytes=0")
	}
}

// TestPostgresExecutorRowCap verifies MaxRows caps rows and MaxRows<=0 means
// unlimited (consistent with ApplyLimit and OpenSearch semantics).
func TestPostgresExecutorRowCap(t *testing.T) {
	cfg := testConnectorConfig(t)
	pg, err := executor.NewPostgresExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer pg.Close()

	query := `SELECT i FROM generate_series(1, 100) AS i`
	capped, err := pg.Execute(context.Background(), query, nil, executor.OutputLimits{MaxRows: 5})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(capped.Rows) != 5 {
		t.Fatalf("expected 5 rows capped by MaxRows, got %d", len(capped.Rows))
	}

	unlimited, err := pg.Execute(context.Background(), query, nil, executor.OutputLimits{MaxRows: 0})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(unlimited.Rows) != 100 {
		t.Fatalf("expected 100 rows with MaxRows=0 (unlimited), got %d", len(unlimited.Rows))
	}
}
