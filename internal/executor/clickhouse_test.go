package executor_test

import (
	"context"
	"testing"

	"github.com/heavenlabs/hnb/internal/executor"
	"github.com/heavenlabs/hnb/internal/models"
)

func testClickHouseConfig(t *testing.T) models.ConnectorConfig {
	t.Helper()
	return models.ConnectorConfig{
		Host: "localhost", Port: 9000,
		User: "default", Password: "", Database: "default",
	}
}

func testDevClickHouseConfig(t *testing.T) models.ConnectorConfig {
	t.Helper()
	return models.ConnectorConfig{
		Host: "localhost", Port: 9000,
		User: "dev", Password: "dev", Database: "analytics",
	}
}

func TestClickHouseExecutorInterface(t *testing.T) {
	// Verify ClickHouseExecutor implements Executor
	var _ executor.Executor = (*executor.ClickHouseExecutor)(nil)
}

func TestClickHouseDecimalScan(t *testing.T) {
	cfg := testDevClickHouseConfig(t)
	exec, err := executor.NewClickHouseExecutor(cfg)
	if err != nil {
		t.Skipf("dev ClickHouse not reachable: %v", err)
	}
	defer exec.Close()

	result, err := exec.Execute(context.Background(), "SELECT * FROM events LIMIT 5", nil, 10)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(result.Rows) == 0 {
		t.Fatal("expected rows")
	}
	// Find revenue column index
	revenueIdx := -1
	for i, col := range result.Columns {
		if col.Name == "revenue" {
			revenueIdx = i
			break
		}
	}
	if revenueIdx == -1 {
		t.Fatal("revenue column not found")
	}
	// Revenue must scan as a numeric type, not a string
	for _, row := range result.Rows {
		switch row[revenueIdx].(type) {
		case float64, int64, uint64, nil:
			// ok
		default:
			t.Errorf("revenue scanned as %T, want float64", row[revenueIdx])
		}
	}
}

func TestClickHouseDatabases(t *testing.T) {
	cfg := testClickHouseConfig(t)
	exec, err := executor.NewClickHouseExecutor(cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer exec.Close()

	dbs, err := exec.Databases(context.Background())
	if err != nil {
		t.Fatalf("databases: %v", err)
	}
	if len(dbs) == 0 {
		t.Error("expected at least one database")
	}
}
