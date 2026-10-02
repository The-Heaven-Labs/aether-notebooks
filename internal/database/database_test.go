package database_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/models"
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

func TestMigration108WarehouseTableGrants(t *testing.T) {
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
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='warehouse_table_grants')").Scan(&exists); err != nil {
		t.Fatalf("warehouse_table_grants existence query: %v", err)
	}
	if !exists {
		t.Fatal("warehouse_table_grants table should exist after migration")
	}

	// Column order is pinned on purpose: the leading warehouse_id keeps the
	// unique btree usable for warehouse-scoped grant lookups.
	rows, err := db.Pool.Query(ctx, `
		SELECT a.attname
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE t.relname = 'warehouse_table_grants' AND c.contype = 'u'
		ORDER BY k.ord`)
	if err != nil {
		t.Fatalf("unique constraint query: %v", err)
	}
	defer rows.Close()

	var uniqueCols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatalf("scan: %v", err)
		}
		uniqueCols = append(uniqueCols, col)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	wantUnique := []string{"warehouse_id", "subject_type", "subject_id", "database_name", "table_name"}
	if !slices.Equal(uniqueCols, wantUnique) {
		t.Fatalf("warehouse_table_grants unique constraint columns = %v, want %v", uniqueCols, wantUnique)
	}

	var checkDef string
	if err := db.Pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		WHERE t.relname = 'warehouse_table_grants' AND c.conname = 'warehouse_table_grants_subject_type_check'`).Scan(&checkDef); err != nil {
		t.Fatalf("subject_type check constraint lookup: %v", err)
	}
	for _, v := range []string{"'user'", "'group'", "'everyone'"} {
		if !strings.Contains(checkDef, v) {
			t.Fatalf("subject_type CHECK constraint %q missing %s", checkDef, v)
		}
	}

	for _, tc := range []struct {
		name       string
		refTable   string
		cols       string
		deleteRule string
	}{
		{"warehouse_table_grants_warehouse_org_fkey", "warehouses", "warehouse_id,org_id", "CASCADE"},
		{"warehouse_table_grants_org_id_fkey", "orgs", "org_id", "CASCADE"},
		{"warehouse_table_grants_created_by_fkey", "users", "created_by", "SET NULL"},
	} {
		var cols, rule string
		if err := db.Pool.QueryRow(ctx, `
			SELECT string_agg(a.attname, ',' ORDER BY k.ord), rc.delete_rule
			FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
			JOIN pg_class rt ON rt.oid = c.confrelid
			JOIN pg_namespace rn ON rn.oid = rt.relnamespace AND rn.nspname = 'public'
			JOIN unnest(c.conkey, c.confkey) WITH ORDINALITY AS k(attnum, refattnum, ord) ON true
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
			JOIN information_schema.referential_constraints rc
			  ON rc.constraint_name = c.conname AND rc.constraint_schema = 'public'
			WHERE t.relname = 'warehouse_table_grants' AND c.conname = $1 AND rt.relname = $2
			GROUP BY rc.delete_rule`, tc.name, tc.refTable).Scan(&cols, &rule); err != nil {
			t.Fatalf("%s lookup: %v", tc.name, err)
		}
		if cols != tc.cols || rule != tc.deleteRule {
			t.Fatalf("%s = (%s, %s), want (%s, %s)", tc.name, cols, rule, tc.cols, tc.deleteRule)
		}
	}
}

func TestMigration109ServicePreferences(t *testing.T) {
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
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='warehouse_service_preferences')").Scan(&exists); err != nil {
		t.Fatalf("warehouse_service_preferences existence query: %v", err)
	}
	if !exists {
		t.Fatal("warehouse_service_preferences table should exist after migration")
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'warehouse_service_preferences'
		ORDER BY ordinal_position`)
	if err != nil {
		t.Fatalf("columns query: %v", err)
	}
	defer rows.Close()

	type column struct {
		dataType string
		nullable string
		def      *string
	}
	got := map[string]column{}
	var order []string
	for rows.Next() {
		var name string
		var c column
		if err := rows.Scan(&name, &c.dataType, &c.nullable, &c.def); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = c
		order = append(order, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	wantOrder := []string{"user_id", "warehouse_id", "connector_id", "updated_at"}
	if !slices.Equal(order, wantOrder) {
		t.Fatalf("warehouse_service_preferences columns = %v, want %v", order, wantOrder)
	}
	for name, want := range map[string]struct {
		dataType string
		nullable string
	}{
		"user_id":      {"uuid", "NO"},
		"warehouse_id": {"uuid", "NO"},
		"connector_id": {"uuid", "NO"},
		"updated_at":   {"timestamp with time zone", "NO"},
	} {
		c, ok := got[name]
		if !ok {
			t.Fatalf("column %s missing", name)
		}
		if c.dataType != want.dataType || c.nullable != want.nullable {
			t.Fatalf("%s = (%s, nullable=%s), want (%s, %s)", name, c.dataType, c.nullable, want.dataType, want.nullable)
		}
	}
	if def := got["updated_at"].def; def == nil || *def != "now()" {
		t.Fatalf("updated_at default = %v, want now()", def)
	}

	// Primary key column order is pinned on purpose: it is the lookup key for
	// a user's routing preference within one warehouse.
	var pkCols string
	if err := db.Pool.QueryRow(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY k.ord)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE t.relname = 'warehouse_service_preferences' AND c.contype = 'p'`).Scan(&pkCols); err != nil {
		t.Fatalf("primary key query: %v", err)
	}
	if pkCols != "user_id,warehouse_id" {
		t.Fatalf("primary key covers %q, want \"user_id,warehouse_id\"", pkCols)
	}

	// cardinality(conkey) = 1 makes the lookup return no rows for a
	// multi-column FK, failing the scan instead of silently passing.
	for _, tc := range []struct {
		name       string
		col        string
		refTable   string
		deleteRule string
	}{
		{"warehouse_service_preferences_user_id_fkey", "user_id", "users", "CASCADE"},
		{"warehouse_service_preferences_warehouse_id_fkey", "warehouse_id", "warehouses", "CASCADE"},
		{"warehouse_service_preferences_connector_id_fkey", "connector_id", "connectors", "CASCADE"},
	} {
		var cols, rule string
		if err := db.Pool.QueryRow(ctx, `
			SELECT string_agg(a.attname, ',' ORDER BY k.ord), rc.delete_rule
			FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
			JOIN pg_class rt ON rt.oid = c.confrelid
			JOIN pg_namespace rn ON rn.oid = rt.relnamespace AND rn.nspname = 'public'
			JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
			JOIN information_schema.referential_constraints rc
			  ON rc.constraint_name = c.conname AND rc.constraint_schema = 'public'
			WHERE t.relname = 'warehouse_service_preferences'
			  AND c.conname = $1
			  AND c.contype = 'f'
			  AND rt.relname = $2
			  AND cardinality(c.conkey) = 1
			GROUP BY rc.delete_rule`, tc.name, tc.refTable).Scan(&cols, &rule); err != nil {
			t.Fatalf("%s lookup: %v", tc.name, err)
		}
		if cols != tc.col || rule != tc.deleteRule {
			t.Fatalf("%s = (%s, %s), want (%s, %s)", tc.name, cols, rule, tc.col, tc.deleteRule)
		}
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	var orgID, userID, warehouseID, otherWarehouseID, connectorID, otherConnectorID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ('V109 Preference Org', 'v109-preference-org') RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ('v109-preference@test.local', 'V109 Preference') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO warehouses (org_id, name) VALUES ($1, 'V109 Preference Warehouse') RETURNING id::text`, orgID).Scan(&warehouseID); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO warehouses (org_id, name) VALUES ($1, 'V109 Preference Warehouse 2') RETURNING id::text`, orgID).Scan(&otherWarehouseID); err != nil {
		t.Fatalf("seed second warehouse: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO connectors (org_id, name, type, config_encrypted) VALUES ($1, 'V109 Preference Connector', 'clickhouse', '\x'::bytea) RETURNING id::text`, orgID).Scan(&connectorID); err != nil {
		t.Fatalf("seed connector: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO connectors (org_id, name, type, config_encrypted) VALUES ($1, 'V109 Preference Connector 2', 'clickhouse', '\x'::bytea) RETURNING id::text`, orgID).Scan(&otherConnectorID); err != nil {
		t.Fatalf("seed second connector: %v", err)
	}

	const insertPref = `INSERT INTO warehouse_service_preferences (user_id, warehouse_id, connector_id) VALUES ($1, $2, $3)`

	if _, err := tx.Exec(ctx, insertPref, userID, warehouseID, connectorID); err != nil {
		t.Fatalf("valid preference insert: %v", err)
	}

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	_, err = sp.Exec(ctx, insertPref, userID, warehouseID, connectorID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate preference insert = %v, want SQLSTATE 23505", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback savepoint: %v", err)
	}

	// The connector is soft-deleted in normal operation, so this row must
	// survive until the soft-delete handler cleans it up explicitly.
	if _, err := tx.Exec(ctx, insertPref, userID, otherWarehouseID, connectorID); err != nil {
		t.Fatalf("second preference insert: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE connectors SET deleted_at = NOW() WHERE id=$1`, connectorID); err != nil {
		t.Fatalf("soft-delete connector: %v", err)
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM warehouse_service_preferences WHERE connector_id=$1`, connectorID).Scan(&remaining); err != nil {
		t.Fatalf("count by connector after soft delete: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("%d preference rows after connector soft delete, want 2", remaining)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM connectors WHERE id=$1`, connectorID); err != nil {
		t.Fatalf("hard-delete connector: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM warehouse_service_preferences WHERE connector_id=$1`, connectorID).Scan(&remaining); err != nil {
		t.Fatalf("count by connector after hard delete: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d preference rows survived connector hard delete, want 0", remaining)
	}

	if _, err := tx.Exec(ctx, insertPref, userID, warehouseID, otherConnectorID); err != nil {
		t.Fatalf("third preference insert: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM warehouses WHERE id=$1`, warehouseID); err != nil {
		t.Fatalf("hard-delete warehouse: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM warehouse_service_preferences WHERE warehouse_id=$1`, warehouseID).Scan(&remaining); err != nil {
		t.Fatalf("count by warehouse after hard delete: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d preference rows survived warehouse hard delete, want 0", remaining)
	}
}

func TestMigration110WarehouseGrantsIntegrity(t *testing.T) {
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

	var idOrgCols string
	if err := db.Pool.QueryRow(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY k.ord)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE t.relname = 'warehouses' AND c.conname = 'warehouses_id_org_key' AND c.contype = 'u'`).Scan(&idOrgCols); err != nil {
		t.Fatalf("warehouses_id_org_key lookup: %v", err)
	}
	if idOrgCols != "id,org_id" {
		t.Fatalf("warehouses_id_org_key covers %q, want \"id,org_id\"", idOrgCols)
	}

	var fkCols, fkRule string
	if err := db.Pool.QueryRow(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY k.ord), rc.delete_rule
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		JOIN pg_class rt ON rt.oid = c.confrelid
		JOIN pg_namespace rn ON rn.oid = rt.relnamespace AND rn.nspname = 'public'
		JOIN unnest(c.conkey, c.confkey) WITH ORDINALITY AS k(attnum, refattnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		JOIN information_schema.referential_constraints rc
		  ON rc.constraint_name = c.conname AND rc.constraint_schema = 'public'
		WHERE t.relname = 'warehouse_table_grants'
		  AND c.conname = 'warehouse_table_grants_warehouse_org_fkey'
		  AND rt.relname = 'warehouses'
		GROUP BY rc.delete_rule`).Scan(&fkCols, &fkRule); err != nil {
		t.Fatalf("warehouse_table_grants_warehouse_org_fkey lookup: %v", err)
	}
	if fkCols != "warehouse_id,org_id" || fkRule != "CASCADE" {
		t.Fatalf("warehouse_table_grants_warehouse_org_fkey = (%s, %s), want (warehouse_id,org_id, CASCADE)", fkCols, fkRule)
	}

	var idxCols string
	if err := db.Pool.QueryRow(ctx, `
		SELECT string_agg(a.attname, ',' ORDER BY k.ord)
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
		WHERE t.relname = 'idx_wh_grants_subject_lookup'`).Scan(&idxCols); err != nil {
		t.Fatalf("idx_wh_grants_subject_lookup lookup: %v", err)
	}
	if idxCols != "subject_type,subject_id" {
		t.Fatalf("idx_wh_grants_subject_lookup covers %q, want \"subject_type,subject_id\"", idxCols)
	}

	for _, old := range []string{"idx_wh_grants_subject", "idx_wh_grants_group"} {
		var n int
		if err := db.Pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM pg_class t
			JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
			WHERE t.relname = $1 AND t.relkind = 'i'`, old).Scan(&n); err != nil {
			t.Fatalf("%s existence query: %v", old, err)
		}
		if n != 0 {
			t.Fatalf("%s should have been dropped", old)
		}
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	var orgID, warehouseID, userID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ('V110 Integrity Org', 'v110-integrity-org') RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO warehouses (org_id, name) VALUES ($1, 'V110 Integrity Warehouse') RETURNING id::text`, orgID).Scan(&warehouseID); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ('v110-integrity@test.local', 'V110 Integrity') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	const insertGrant = `INSERT INTO warehouse_table_grants
		(org_id, warehouse_id, subject_type, subject_id, database_name, table_name, created_by)
		VALUES ($1, $2, $3, $4, 'analytics', 'events', $5)`
	if _, err := tx.Exec(ctx, insertGrant, orgID, warehouseID, "user", userID, userID); err != nil {
		t.Fatalf("valid grant insert: %v", err)
	}

	expectSQLState := func(wantCode, subjectType, subjectID string) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err = sp.Exec(ctx, insertGrant, orgID, warehouseID, subjectType, subjectID, userID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != wantCode {
			t.Fatalf("insert (%s,%s) = %v, want SQLSTATE %s", subjectType, subjectID, err, wantCode)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback savepoint: %v", err)
		}
	}

	expectSQLState("23505", "user", userID)
	expectSQLState("23514", "user", "ABCDEF01-2345-6789-ABCD-EF0123456789")
	expectSQLState("23514", "everyone", "not-everyone")
}

func TestMigration111WarehouseMasterFingerprint(t *testing.T) {
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

	var dataType, nullable string
	if err := db.Pool.QueryRow(ctx, `
		SELECT data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'warehouses' AND column_name = 'applied_master_fp'`).
		Scan(&dataType, &nullable); err != nil {
		t.Fatalf("warehouses.applied_master_fp missing: %v", err)
	}
	if dataType != "text" || nullable != "YES" {
		t.Fatalf("warehouses.applied_master_fp = (%s, nullable=%s), want (text, YES)", dataType, nullable)
	}

	var applied int
	if err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='111_warehouse_master_fingerprint'`).Scan(&applied); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if applied != 1 {
		t.Fatalf("V111 not recorded (count=%d)", applied)
	}
}

func TestMigration112MembershipUserIndexes(t *testing.T) {
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

	for _, want := range []struct {
		index string
		table string
		col   string
	}{
		{"idx_group_members_user", "group_members", "user_id"},
		{"idx_org_members_user", "org_members", "user_id"},
	} {
		var cols string
		if err := db.Pool.QueryRow(ctx, `
			SELECT COALESCE(string_agg(a.attname, ',' ORDER BY k.ord), '')
			FROM pg_class i
			JOIN pg_index ix ON ix.indexrelid = i.oid
			JOIN pg_class t ON t.oid = ix.indrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
			JOIN unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
			WHERE i.relname = $1 AND t.relname = $2`, want.index, want.table).Scan(&cols); err != nil {
			t.Fatalf("%s lookup: %v", want.index, err)
		}
		if cols != want.col {
			t.Fatalf("%s covers %q, want %q", want.index, cols, want.col)
		}
	}

	var applied int
	if err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='112_membership_user_indexes'`).Scan(&applied); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if applied != 1 {
		t.Fatalf("V112 not recorded (count=%d)", applied)
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

func TestMigration114SchemaSnapshots(t *testing.T) {
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
		"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='schema_snapshots')").Scan(&exists); err != nil {
		t.Fatalf("schema_snapshots existence query: %v", err)
	}
	if !exists {
		t.Fatal("schema_snapshots table should exist after migration")
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT a.attname
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE t.relname = 'schema_snapshots' AND c.contype = 'p'
		ORDER BY k.ord`)
	if err != nil {
		t.Fatalf("primary key query: %v", err)
	}
	defer rows.Close()

	var pkCols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatalf("scan: %v", err)
		}
		pkCols = append(pkCols, col)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	wantPK := []string{"connector_id", "database_name", "table_name"}
	if !slices.Equal(pkCols, wantPK) {
		t.Fatalf("schema_snapshots primary key columns = %v, want %v", pkCols, wantPK)
	}

	var deleteRule string
	if err := db.Pool.QueryRow(ctx, `
		SELECT rc.delete_rule
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		JOIN information_schema.referential_constraints rc
		  ON rc.constraint_name = c.conname AND rc.constraint_schema = 'public'
		WHERE t.relname = 'schema_snapshots'
		  AND c.conname = 'schema_snapshots_connector_id_fkey'`).Scan(&deleteRule); err != nil {
		t.Fatalf("connector FK lookup: %v", err)
	}
	if deleteRule != "CASCADE" {
		t.Fatalf("schema_snapshots connector FK delete rule = %s, want CASCADE", deleteRule)
	}

	// Deleting a connector must remove its snapshots, not orphan them.
	var orgID, connectorID string
	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO orgs (name, slug)
		VALUES ('Snapshot FK Org', 'snap-fk-' || substr(gen_random_uuid()::text, 1, 8))
		RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	defer func() { _, _ = db.Pool.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, orgID) }()

	if err := db.Pool.QueryRow(ctx, `
		INSERT INTO connectors (org_id, name, type, config_encrypted)
		VALUES ($1, 'Snapshot FK Connector', 'clickhouse', '{}')
		RETURNING id`, orgID).Scan(&connectorID); err != nil {
		t.Fatalf("seed connector: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO schema_snapshots (connector_id, database_name, table_name)
		VALUES ($1, 'analytics', 'events')`, connectorID); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `DELETE FROM connectors WHERE id = $1`, connectorID); err != nil {
		t.Fatalf("delete connector: %v", err)
	}
	var remaining int
	if err := db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1`, connectorID).Scan(&remaining); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("schema_snapshots rows after connector delete = %d, want 0", remaining)
	}
}

func TestMigration119GroupSource(t *testing.T) {
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

	var dataType, nullable string
	var def *string
	if err := db.Pool.QueryRow(ctx, `
		SELECT data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'groups' AND column_name = 'source'`).
		Scan(&dataType, &nullable, &def); err != nil {
		t.Fatalf("groups.source missing: %v", err)
	}
	if dataType != "text" || nullable != "NO" || def == nil || *def != "'manual'::text" {
		t.Fatalf("groups.source = (%s, nullable=%s, default=%v), want (text, NO, 'manual'::text)", dataType, nullable, def)
	}

	var checkCount int
	if err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.table_constraints
		WHERE table_schema = 'public' AND table_name = 'groups'
		  AND constraint_name = 'groups_source_check' AND constraint_type = 'CHECK'`).Scan(&checkCount); err != nil {
		t.Fatalf("groups_source_check lookup: %v", err)
	}
	if checkCount != 1 {
		t.Fatalf("groups_source_check constraint count = %d, want 1", checkCount)
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	var orgID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ('V119 Group Source Org', 'v119-group-source-org') RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}

	var source string
	if err := tx.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'V119 Default Group') RETURNING source`, orgID).Scan(&source); err != nil {
		t.Fatalf("default group insert: %v", err)
	}
	if source != "manual" {
		t.Fatalf("groups.source default = %q, want manual", source)
	}

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	_, err = sp.Exec(ctx,
		`INSERT INTO groups (org_id, name, source) VALUES ($1, 'V119 Bad Group', 'bogus')`, orgID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("invalid source insert = %v, want SQLSTATE 23514", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatalf("rollback savepoint: %v", err)
	}
}

func TestMigration121DashboardVariables(t *testing.T) {
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()

	// Schema: new columns exist.
	for _, col := range []string{"connector_id", "query", "language"} {
		var exists bool
		if err := db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='widgets' AND column_name=$1)`, col).Scan(&exists); err != nil {
			t.Fatalf("column %s query: %v", col, err)
		}
		if !exists {
			t.Fatalf("widgets.%s missing after V121", col)
		}
	}

	// Conversion: run the marked block against a simulated legacy dashboard.
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `ALTER TABLE widgets DROP CONSTRAINT IF EXISTS widgets_type_check`); err != nil {
		t.Fatalf("drop constraint: %v", err)
	}
	var orgID, userID, dashID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ('V121 Org', 'v121-' || md5(random()::text)) RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ('v121-' || md5(random()::text) || '@example.com', 'V121') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO dashboards (org_id, title, settings, created_by)
		 VALUES ($1, 'V121', '{"parameter_overrides":{"x":"y"}}', $2) RETURNING id::text`, orgID, userID).Scan(&dashID); err != nil {
		t.Fatalf("insert dashboard: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO widgets (dashboard_id, type, layout, config) VALUES
		 ($1, 'date_picker', '{}', '{"paramName":"start_date","label":"Start date"}'),
		 ($1, 'multi_select', '{}', '{"paramName":"region","label":"Region","options":["EMEA","AMER"]}')`, dashID); err != nil {
		t.Fatalf("insert widgets: %v", err)
	}

	content, err := os.ReadFile("migrations/V121__dashboard_query_widgets.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	startMarker, endMarker := "-- +conversion:start", "-- +conversion:end"
	start := strings.Index(string(content), startMarker)
	end := strings.Index(string(content), endMarker)
	if start < 0 || end < 0 {
		t.Fatal("conversion markers missing from V121")
	}
	if _, err := tx.Exec(ctx, string(content)[start+len(startMarker):end]); err != nil {
		t.Fatalf("run conversion: %v", err)
	}

	var settings []byte
	if err := tx.QueryRow(ctx, `SELECT settings FROM dashboards WHERE id=$1`, dashID).Scan(&settings); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	var got struct {
		Variables []models.DashboardVariable `json:"variables"`
	}
	if err := json.Unmarshal(settings, &got); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if len(got.Variables) != 2 {
		t.Fatalf("expected 2 variables, got %+v", got.Variables)
	}
	var region *models.DashboardVariable
	for i := range got.Variables {
		if got.Variables[i].Name == "region" {
			region = &got.Variables[i]
		}
	}
	if region == nil || region.Type != "multi_select" || region.Options == nil || len(region.Options.Values) != 2 {
		t.Fatalf("region variable wrong: %+v", region)
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM widgets WHERE dashboard_id=$1`, dashID).Scan(&remaining); err != nil {
		t.Fatalf("count widgets: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected input widgets deleted, %d remain", remaining)
	}
}

func TestMigration124AgentSessionACL(t *testing.T) {
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

	// The notebook-viewer inheritance flag must exist, be NOT NULL and default false.
	var dataType, nullable string
	var def *string
	if err := db.Pool.QueryRow(ctx, `
		SELECT data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'agent_sessions'
		  AND column_name = 'share_with_notebook_viewers'`).
		Scan(&dataType, &nullable, &def); err != nil {
		t.Fatalf("agent_sessions.share_with_notebook_viewers missing: %v", err)
	}
	if dataType != "boolean" || nullable != "NO" || def == nil || *def != "false" {
		t.Fatalf("share_with_notebook_viewers = (%s, nullable=%s, default=%v), want (boolean, NO, false)", dataType, nullable, def)
	}

	// The notebook listing index is pinned to exactly (notebook_id, created_at
	// DESC); normalize schema qualification so only the index shape is compared.
	var idxDef string
	if err := db.Pool.QueryRow(ctx, `
		SELECT pg_get_indexdef(i.indexrelid)
		FROM pg_index i
		JOIN pg_class t ON t.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace AND n.nspname = 'public'
		WHERE t.relname = 'idx_agent_sessions_notebook'`).Scan(&idxDef); err != nil {
		t.Fatalf("idx_agent_sessions_notebook missing: %v", err)
	}
	const wantIdxDef = "CREATE INDEX idx_agent_sessions_notebook ON public.agent_sessions USING btree (notebook_id, created_at DESC)"
	if got := strings.ReplaceAll(idxDef, "public.", ""); got != strings.ReplaceAll(wantIdxDef, "public.", "") {
		t.Fatalf("idx_agent_sessions_notebook = %q, want %q", idxDef, wantIdxDef)
	}

	// The redefined CHECK must still accept every legacy ACL resource type plus
	// agent_session. Quoted matching keeps "agent" from matching "agent_session".
	var conDef string
	if err := db.Pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conrelid = 'acl_entries'::regclass
		  AND conname = 'acl_entries_resource_type_check'`).Scan(&conDef); err != nil {
		t.Fatalf("acl_entries_resource_type_check missing: %v", err)
	}
	for _, resourceType := range []string{
		"folder", "notebook", "connector", "dashboard", "agent",
		"model_config", "skill", "mcp_server", "tool", "agent_session",
	} {
		if !strings.Contains(conDef, "'"+resourceType+"'") {
			t.Fatalf("acl_entries_resource_type_check = %q, missing resource type %q", conDef, resourceType)
		}
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	var orgID, userID, agentID, sessionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO orgs (name, slug)
		VALUES ('V124 Session Org', 'v124-session-' || substr(gen_random_uuid()::text, 1, 8))
		RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, name)
		VALUES ('v124-session-' || substr(gen_random_uuid()::text, 1, 8) || '@test.local', 'V124 Session')
		RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO agents (org_id, name, created_by)
		VALUES ($1, 'V124 Session Agent', $2)
		RETURNING id::text`, orgID, userID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	// A fresh session must pick up the false default at insert time.
	var flag bool
	if err := tx.QueryRow(ctx, `
		INSERT INTO agent_sessions (agent_id, user_id)
		VALUES ($1, $2)
		RETURNING id::text, share_with_notebook_viewers`, agentID, userID).Scan(&sessionID, &flag); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if flag {
		t.Fatal("share_with_notebook_viewers should default to false")
	}

	// The extended CHECK constraint must accept agent_session entries.
	if _, err := tx.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'agent_session', gen_random_uuid(), 'user', $2, ARRAY['view'])`, orgID, userID); err != nil {
		t.Fatalf("agent_session ACL insert rejected: %v", err)
	}

	// Execute the migration's real backfill statement against this session,
	// twice, to prove the owner entry contents and ON CONFLICT idempotency.
	content, err := os.ReadFile("migrations/V124__agent_session_acl.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	const startMarker, endMarker = "-- +backfill:start", "-- +backfill:end"
	start := strings.Index(string(content), startMarker)
	end := strings.Index(string(content), endMarker)
	if start < 0 || end < 0 || end < start {
		t.Fatal("backfill markers missing from V124")
	}
	backfill := string(content)[start+len(startMarker) : end]
	for i := 0; i < 2; i++ {
		if _, err := tx.Exec(ctx, backfill); err != nil {
			t.Fatalf("run backfill (attempt %d): %v", i+1, err)
		}
	}

	var aclOrgID, resourceID, subjectType, subjectID string
	var actions []string
	if err := tx.QueryRow(ctx, `
		SELECT org_id::text, resource_id::text, subject_type, subject_id, actions
		FROM acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = $1 AND subject_type = 'user'`, sessionID).
		Scan(&aclOrgID, &resourceID, &subjectType, &subjectID, &actions); err != nil {
		t.Fatalf("owner ACL entry missing after backfill: %v", err)
	}
	if aclOrgID != orgID || resourceID != sessionID || subjectType != "user" || subjectID != userID {
		t.Fatalf("owner ACL entry = (org %s, resource %s, %s/%s), want (org %s, resource %s, user/%s)",
			aclOrgID, resourceID, subjectType, subjectID, orgID, sessionID, userID)
	}
	// Actions are semantically a set; compare order-insensitively rather than
	// pinning the array order the migration happens to emit today.
	wantActions := []string{"view", "edit", "share", "delete", "admin"}
	if !slices.Equal(slices.Sorted(slices.Values(actions)), slices.Sorted(slices.Values(wantActions))) {
		t.Fatalf("owner ACL actions = %v, want %v", actions, wantActions)
	}
}

func TestMigration125DashboardCreatorViewWithData(t *testing.T) {
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

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	var orgID, ownerID, viewerID, dashID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO orgs (name, slug)
		VALUES ('V125 Dashboard Org', 'v125-dashboard-' || substr(gen_random_uuid()::text, 1, 8))
		RETURNING id::text`).Scan(&orgID); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, name)
		VALUES ('v125-owner-' || substr(gen_random_uuid()::text, 1, 8) || '@test.local', 'V125 Owner')
		RETURNING id::text`).Scan(&ownerID); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, name)
		VALUES ('v125-viewer-' || substr(gen_random_uuid()::text, 1, 8) || '@test.local', 'V125 Viewer')
		RETURNING id::text`).Scan(&viewerID); err != nil {
		t.Fatalf("seed viewer: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO dashboards (org_id, title, settings, created_by)
		VALUES ($1, 'V125 Legacy Dashboard', '{}', $2)
		RETURNING id::text`, orgID, ownerID).Scan(&dashID); err != nil {
		t.Fatalf("seed dashboard: %v", err)
	}

	// Pre-V121 owner entry and a read-only viewer entry on the same dashboard.
	if _, err := tx.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'dashboard', $2::uuid, 'user', $3, ARRAY['view','edit','delete','share']),
		       ($1, 'dashboard', $2::uuid, 'user', $4, ARRAY['view'])`, orgID, dashID, ownerID, viewerID); err != nil {
		t.Fatalf("seed ACL entries: %v", err)
	}

	// Execute the migration's real backfill statement twice to prove it is
	// idempotent for the creator and leaves other subjects untouched.
	content, err := os.ReadFile("migrations/V125__dashboard_creator_view_with_data.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	const startMarker, endMarker = "-- +backfill:start", "-- +backfill:end"
	start := strings.Index(string(content), startMarker)
	end := strings.Index(string(content), endMarker)
	if start < 0 || end < 0 || end < start {
		t.Fatal("backfill markers missing from V125")
	}
	backfill := string(content)[start+len(startMarker) : end]
	for i := 0; i < 2; i++ {
		if _, err := tx.Exec(ctx, backfill); err != nil {
			t.Fatalf("run backfill (attempt %d): %v", i+1, err)
		}
	}

	var ownerActions, viewerActions []string
	if err := tx.QueryRow(ctx, `
		SELECT actions FROM acl_entries
		WHERE resource_type = 'dashboard' AND resource_id = $1::uuid AND subject_id = $2`,
		dashID, ownerID).Scan(&ownerActions); err != nil {
		t.Fatalf("owner ACL lookup: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		SELECT actions FROM acl_entries
		WHERE resource_type = 'dashboard' AND resource_id = $1::uuid AND subject_id = $2`,
		dashID, viewerID).Scan(&viewerActions); err != nil {
		t.Fatalf("viewer ACL lookup: %v", err)
	}

	wantOwner := []string{"view", "edit", "delete", "share", "view_with_data"}
	if !slices.Equal(slices.Sorted(slices.Values(ownerActions)), slices.Sorted(slices.Values(wantOwner))) {
		t.Fatalf("owner ACL actions = %v, want %v", ownerActions, wantOwner)
	}
	if !slices.Equal(viewerActions, []string{"view"}) {
		t.Fatalf("viewer ACL actions = %v, want [view]", viewerActions)
	}

	var applied int
	if err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version='125_dashboard_creator_view_with_data'`).Scan(&applied); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if applied != 1 {
		t.Fatalf("V125 not recorded (count=%d)", applied)
	}
}
