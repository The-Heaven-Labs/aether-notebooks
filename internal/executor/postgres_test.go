package executor_test

import (
	"context"
	"testing"

	"github.com/heavenlabs/hnb/internal/executor"
	"github.com/heavenlabs/hnb/internal/models"
)

func TestPostgresExecutor(t *testing.T) {
	cfg := models.ConnectorConfig{
		Host: "localhost", Port: 5432,
		User: "hnb", Password: "hnb_dev", Database: "hnb",
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
