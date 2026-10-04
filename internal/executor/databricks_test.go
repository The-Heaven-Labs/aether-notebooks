package executor

import (
	"testing"
	"time"
)

func TestDatabricksIsCommand(t *testing.T) {
	cases := map[string]bool{
		"SELECT 1":                      false,
		"  select * from t":             false,
		"WITH x AS (SELECT 1) SELECT 1": false,
		"SHOW TABLES":                   false,
		"CREATE TABLE t (id INT)":       true,
		"INSERT INTO t VALUES (1)":      true,
		"DELETE FROM t":                 true,
		"MERGE INTO t USING s ON 1=1":   true,
		"DROP TABLE t":                  true,
	}
	for q, want := range cases {
		if got := databricksIsCommand(q); got != want {
			t.Errorf("databricksIsCommand(%q) = %v, want %v", q, got, want)
		}
	}
}

func TestNormalizeDatabricksValue(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := normalizeDatabricksValue(ts); got != "2026-01-02T03:04:05Z" {
		t.Fatalf("timestamp: got %v", got)
	}
	if got := normalizeDatabricksValue(time.Time{}); got != nil {
		t.Fatalf("zero time: want nil, got %v", got)
	}
	if got := normalizeDatabricksValue("plain"); got != "plain" {
		t.Fatalf("string passthrough: got %v", got)
	}
}

func TestValidDatabricksCatalogName(t *testing.T) {
	if !validDatabricksCatalogName("main") || !validDatabricksCatalogName("sales_2026") {
		t.Fatal("expected simple identifiers to be valid")
	}
	if validDatabricksCatalogName("bad`name") || validDatabricksCatalogName("") {
		t.Fatal("expected backticks and empty names to be rejected")
	}
}

func TestNormalizeDatabricksHost(t *testing.T) {
	cases := map[string]string{
		"https://dbc-abc.cloud.databricks.com/": "dbc-abc.cloud.databricks.com",
		"http://dbc-abc.cloud.databricks.com":   "dbc-abc.cloud.databricks.com",
		"  dbc-abc.cloud.databricks.com  ":      "dbc-abc.cloud.databricks.com",
		"dbc-abc.cloud.databricks.com":          "dbc-abc.cloud.databricks.com",
	}
	for in, want := range cases {
		if got := normalizeDatabricksHost(in); got != want {
			t.Errorf("normalizeDatabricksHost(%q) = %q, want %q", in, got, want)
		}
	}
}
