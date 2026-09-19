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

	for _, col := range []string{"tokens_after", "kept_count"} {
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
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='100_drop_cells_description'`).Scan(&applied); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if applied != 1 {
		t.Fatalf("V100 not recorded (count=%d)", applied)
	}
}

func TestMigration107Warehouses(t *testing.T) {
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

	var exists bool
	if err := db.Pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name='warehouses')").Scan(&exists); err != nil {
		t.Fatalf("warehouses existence query: %v", err)
	}
	if !exists {
		t.Fatal("warehouses table should exist after migration")
	}

	var nullable string
	if err := db.Pool.QueryRow(ctx,
		`SELECT is_nullable FROM information_schema.columns WHERE table_name='connectors' AND column_name='warehouse_id'`).Scan(&nullable); err != nil {
		t.Fatalf("connectors.warehouse_id missing: %v", err)
	}
	if nullable != "YES" {
		t.Fatalf("connectors.warehouse_id should be nullable (nullable=%q)", nullable)
	}

	var fk int
	if err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON kcu.constraint_schema = tc.constraint_schema AND kcu.constraint_name = tc.constraint_name
		JOIN information_schema.constraint_column_usage ccu
		  ON ccu.constraint_schema = tc.constraint_schema AND ccu.constraint_name = tc.constraint_name
		JOIN information_schema.referential_constraints rc
		  ON rc.constraint_schema = tc.constraint_schema AND rc.constraint_name = tc.constraint_name
		WHERE tc.table_name = 'connectors'
		  AND tc.constraint_type = 'FOREIGN KEY'
		  AND kcu.table_name = 'connectors'
		  AND kcu.column_name = 'warehouse_id'
		  AND ccu.table_name = 'warehouses'
		  AND rc.delete_rule = 'SET NULL'`).Scan(&fk); err != nil {
		t.Fatalf("connectors.warehouse_id foreign key query: %v", err)
	}
	if fk != 1 {
		t.Fatalf("connectors.warehouse_id should reference warehouses with ON DELETE SET NULL (fk count=%d)", fk)
	}
}

// TestNoRowLevelSecurityWithoutPolicies is a regression guard for the
// V103 migration: RLS was enabled on six tables in V001 but no policies were
// ever created, making every non-owner role default-deny. The migration drops
// RLS from those tables. If any user table still has RLS enabled with zero
// policies, the guard fails so future migrations cannot reintroduce the trap.
func TestNoRowLevelSecurityWithoutPolicies(t *testing.T) {
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

	// Any table with relrowsecurity = true must have at least one policy.
	rows, err := db.Pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r' AND c.relrowsecurity
		  AND NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid)`)
	if err != nil {
		t.Fatalf("rls check query failed: %v", err)
	}
	defer rows.Close()

	var offenders []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		offenders = append(offenders, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("tables have RLS enabled with zero policies: %v", offenders)
	}

	// The six tables that had RLS in V001 must no longer have it enabled.
	for _, tbl := range []string{"orgs", "notebooks", "cells", "connectors", "dashboards", "audit_logs"} {
		var enabled bool
		err := db.Pool.QueryRow(ctx,
			`SELECT c.relrowsecurity
			 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname = current_schema() AND c.relname = $1`, tbl).Scan(&enabled)
		if err != nil {
			t.Fatalf("relrowsecurity lookup for %s: %v", tbl, err)
		}
		if enabled {
			t.Fatalf("%s still has RLS enabled after V103", tbl)
		}
	}
}
