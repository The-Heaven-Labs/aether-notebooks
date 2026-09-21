package executor_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/models"
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

	result, err := exec.Execute(context.Background(), "SELECT * FROM events LIMIT 5", nil, executor.OutputLimits{MaxRows: 10})
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

func TestClickHouseIntegerWidths(t *testing.T) {
	cfg := testDevClickHouseConfig(t)
	exec, err := executor.NewClickHouseExecutor(cfg)
	if err != nil {
		t.Skipf("dev ClickHouse not reachable: %v", err)
	}
	defer exec.Close()

	// Each toUIntN / toIntN cast returns that exact ClickHouse type.
	result, err := exec.Execute(context.Background(),
		`SELECT toUInt8(1) AS u8, toUInt16(2) AS u16, toUInt32(3) AS u32, toUInt64(4) AS u64,
		        toInt8(-1) AS i8, toInt16(-2) AS i16, toInt32(-3) AS i32, toInt64(-4) AS i64`,
		nil, executor.OutputLimits{MaxRows: 1})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}
	row := result.Rows[0]
	for i, col := range result.Columns {
		switch row[i].(type) {
		case int64, uint64:
			// ok — narrow types are widened
		default:
			t.Errorf("col %s scanned as %T, want int64 or uint64", col.Name, row[i])
		}
	}
}

func TestIsClickHouseAccessDenied(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"access denied code", &clickhouse.Exception{Code: 497}, true},
		{"other clickhouse code", &clickhouse.Exception{Code: 62}, false},
		{"wrapped access denied", fmt.Errorf("query: %w", &clickhouse.Exception{Code: 497}), true},
		{"message fallback access_denied", errors.New("Code: 497. DB::Exception: ... (ACCESS_DENIED)"), true},
		{"message fallback not enough privileges", errors.New("not enough privileges"), true},
		{"unrelated error", errors.New("syntax error near FROM"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := executor.IsClickHouseAccessDenied(tc.err); got != tc.want {
				t.Fatalf("IsClickHouseAccessDenied(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestClickHouseDatabases(t *testing.T) {
	cfg := testClickHouseConfig(t)
	exec, err := executor.NewClickHouseExecutor(cfg)
	if err != nil {
		t.Skipf("default ClickHouse not reachable: %v", err)
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
