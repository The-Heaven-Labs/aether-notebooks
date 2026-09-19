package database_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
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
