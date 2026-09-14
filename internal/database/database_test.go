package database_test

import (
	"context"
	"os"
	"testing"

	"github.com/the-heaven-labs/aether/internal/database"
)

func TestConnect(t *testing.T) {
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	var result int
	err = db.Pool.QueryRow(context.Background(), "SELECT 1").Scan(&result)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if result != 1 {
		t.Fatalf("expected 1, got %d", result)
	}
}

func TestMigrate(t *testing.T) {
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	err = db.Migrate(context.Background())
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Verify a table exists
	var exists bool
	err = db.Pool.QueryRow(context.Background(),
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name='notebooks')").Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("notebooks table should exist after migration")
	}
}

func TestMigrateAgentUsageColumns(t *testing.T) {
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	ctx := context.Background()

	for _, col := range []string{"tokens_after"} {
		var n int
		err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name='agent_messages' AND column_name=$1`, col).Scan(&n)
		if err != nil || n != 1 {
			t.Fatalf("agent_messages.%s missing (n=%d err=%v)", col, n, err)
		}
		var nullable string
		err = db.Pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_name='agent_messages' AND column_name=$1`, col).Scan(&nullable)
		if err != nil || nullable != "YES" {
			t.Fatalf("agent_messages.%s should be nullable (nullable=%q err=%v)", col, nullable, err)
		}
	}
	for _, col := range []string{"context_tokens", "context_window", "total_input", "total_output", "total_reasoning", "total_cache_read", "total_model_calls", "total_subagent_input", "total_subagent_output"} {
		var n int
		err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name='agent_sessions' AND column_name=$1`, col).Scan(&n)
		if err != nil || n != 1 {
			t.Fatalf("agent_sessions.%s missing (n=%d err=%v)", col, n, err)
		}
		var nullable string
		var def *string
		err = db.Pool.QueryRow(ctx, `SELECT is_nullable, column_default FROM information_schema.columns WHERE table_name='agent_sessions' AND column_name=$1`, col).Scan(&nullable, &def)
		if err != nil || nullable != "NO" || def == nil || *def != "0" {
			t.Fatalf("agent_sessions.%s should be NOT NULL DEFAULT 0 (nullable=%q default=%v err=%v)", col, nullable, def, err)
		}
	}
}

func TestMigrateDropsCellsDescription(t *testing.T) {
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}

	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	ctx := context.Background()

	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_name='cells' AND column_name='description'`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Fatalf("cells.description still exists after migrations")
	}

	var applied int
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='098_drop_cells_description'`).Scan(&applied); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if applied != 1 {
		t.Fatalf("V098 not recorded (count=%d)", applied)
	}
}
