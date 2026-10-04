package executor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Live Databricks tests are skipped unless AETHER_TEST_DATABRICKS_HOST and
// AETHER_TEST_DATABRICKS_HTTP_PATH are set. Credentials are never committed.
// When AETHER_TEST_DATABRICKS_CLIENT_ID is set, OAuth M2M is exercised instead
// of PAT.
func liveDatabricksConfig(t *testing.T) databricksConfig {
	t.Helper()
	host := os.Getenv("AETHER_TEST_DATABRICKS_HOST")
	if host == "" {
		t.Skip("AETHER_TEST_DATABRICKS_HOST not set; skipping live Databricks tests")
	}
	httpPath := os.Getenv("AETHER_TEST_DATABRICKS_HTTP_PATH")
	if httpPath == "" {
		t.Fatal("AETHER_TEST_DATABRICKS_HOST is set but AETHER_TEST_DATABRICKS_HTTP_PATH is missing")
	}
	cfg := databricksConfig{
		Host:     host,
		HTTPPath: httpPath,
		AuthType: "pat",
		Token:    os.Getenv("AETHER_TEST_DATABRICKS_TOKEN"),
		Catalog:  os.Getenv("AETHER_TEST_DATABRICKS_CATALOG"),
		Schema:   os.Getenv("AETHER_TEST_DATABRICKS_SCHEMA"),
	}
	if id := os.Getenv("AETHER_TEST_DATABRICKS_CLIENT_ID"); id != "" {
		cfg.AuthType = "oauth_m2m"
		cfg.ClientID = id
		cfg.ClientSecret = os.Getenv("AETHER_TEST_DATABRICKS_CLIENT_SECRET")
	}
	return cfg
}

func openLiveDatabricks(t *testing.T) *DatabricksExecutor {
	t.Helper()
	cfg, err := validateDatabricksConfig(liveDatabricksConfig(t))
	if err != nil {
		t.Fatalf("invalid live config: %v", err)
	}
	exec, err := NewDatabricksExecutor(cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { exec.Close() })
	return exec
}

func TestDatabricksLiveTestConnection(t *testing.T) {
	exec := openLiveDatabricks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.TestConnection(ctx); err != nil {
		t.Fatalf("test connection: %v", err)
	}
}

func TestDatabricksLiveExecuteTypes(t *testing.T) {
	exec := openLiveDatabricks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rs, err := exec.Execute(ctx,
		"SELECT 1 AS one, CAST('2026-01-02 03:04:05' AS TIMESTAMP) AS ts, "+
			"CAST('12.34' AS DECIMAL(10,2)) AS dec, array(1, 2, 3) AS arr",
		nil, OutputLimits{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(rs.Rows) != 1 || len(rs.Columns) != 4 {
		t.Fatalf("unexpected shape: %d cols %d rows", len(rs.Columns), len(rs.Rows))
	}
	if fmt.Sprint(rs.Rows[0][0]) != "1" {
		t.Fatalf("SELECT 1 returned %v", rs.Rows[0][0])
	}
	ts, ok := rs.Rows[0][1].(string)
	if !ok || !strings.HasPrefix(ts, "2026-01-02T03:04:05") {
		t.Fatalf("timestamp must normalize to an RFC3339 string, got %#v", rs.Rows[0][1])
	}
	dec, ok := rs.Rows[0][2].(string)
	if !ok || dec != "12.34" {
		t.Fatalf("decimal must be the exact string 12.34, got %#v", rs.Rows[0][2])
	}
	if arr, ok := rs.Rows[0][3].(string); !ok || arr != "[1,2,3]" {
		t.Fatalf("array must arrive as the JSON string [1,2,3], got %#v", rs.Rows[0][3])
	}
	wantTypes := map[string]string{
		"one": "INT", "ts": "TIMESTAMP", "dec": "DECIMAL", "arr": "ARRAY",
	}
	for _, c := range rs.Columns {
		if want, ok := wantTypes[c.Name]; ok && c.Type != want {
			t.Fatalf("column %s type = %q, want %q", c.Name, c.Type, want)
		}
	}
	t.Logf("row: %#v", rs.Rows[0])
}

// Requires CREATE privileges in the configured catalog/schema.
func TestDatabricksLiveCommandPath(t *testing.T) {
	cfg, err := validateDatabricksConfig(liveDatabricksConfig(t))
	if err != nil {
		t.Fatalf("invalid live config: %v", err)
	}
	exec := openLiveDatabricks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	table := fmt.Sprintf("aether_live_test_%d", time.Now().UnixNano())
	if cfg.Catalog != "" && cfg.Schema != "" {
		table = cfg.Catalog + "." + cfg.Schema + "." + table
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		if _, err := exec.Execute(dropCtx, "DROP TABLE IF EXISTS "+table, nil, OutputLimits{}); err != nil {
			t.Logf("cleanup drop failed: %v", err)
		}
	})
	for _, stmt := range []string{
		fmt.Sprintf("CREATE TABLE %s (id INT, note STRING)", table),
		fmt.Sprintf("INSERT INTO %s VALUES (1, 'a'), (2, 'b')", table),
	} {
		if _, err := exec.Execute(ctx, stmt, nil, OutputLimits{}); err != nil {
			t.Fatalf("command %q: %v", stmt, err)
		}
	}

	rs, err := exec.Execute(ctx, "SELECT COUNT(*) FROM "+table, nil, OutputLimits{})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(rs.Rows) != 1 || fmt.Sprint(rs.Rows[0][0]) != "2" {
		t.Fatalf("expected 2 rows, got %#v", rs.Rows)
	}
}

// Exercises multi-batch/CloudFetch retrieval across the driver's 100k-row fetch
// boundary. Opt-in because it is slow.
func TestDatabricksLiveLargeResult(t *testing.T) {
	if os.Getenv("AETHER_TEST_DATABRICKS_BIG") == "" {
		t.Skip("set AETHER_TEST_DATABRICKS_BIG=1 to run the large-result test")
	}
	exec := openLiveDatabricks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rs, err := exec.Execute(ctx, "SELECT id FROM range(150000) ORDER BY id", nil, OutputLimits{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(rs.Rows) != 150000 {
		t.Fatalf("expected 150000 rows across fetch batches, got %d", len(rs.Rows))
	}
	first := fmt.Sprint(rs.Rows[0][0])
	last := fmt.Sprint(rs.Rows[len(rs.Rows)-1][0])
	if first != "0" || last != "149999" {
		t.Fatalf("unexpected page-boundary values: first=%s last=%s", first, last)
	}
}

func TestDatabricksLiveSchemaAndDatabases(t *testing.T) {
	exec := openLiveDatabricks(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dbs, err := exec.Databases(ctx)
	if err != nil {
		t.Fatalf("databases: %v", err)
	}
	if len(dbs) == 0 {
		t.Fatal("expected at least one catalog")
	}
	t.Logf("catalogs: %v", dbs)
	schema, err := exec.Schema(ctx)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	t.Logf("tables: %d", len(schema.Tables))
	if len(schema.Tables) > 0 {
		tbl := schema.Tables[0]
		if tbl.Schema == "" || tbl.Name == "" || len(tbl.Columns) == 0 {
			t.Fatalf("malformed table info: %+v", tbl)
		}
	}
}
