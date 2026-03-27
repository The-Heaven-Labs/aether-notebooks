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

func TestClickHouseExecutorInterface(t *testing.T) {
	// Verify ClickHouseExecutor implements Executor
	var _ executor.Executor = (*executor.ClickHouseExecutor)(nil)
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
