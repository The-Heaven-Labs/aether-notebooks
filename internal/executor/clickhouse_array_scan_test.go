package executor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// testObjectArrayExecutor returns an executor against the dev ClickHouse, or
// skips the test when it is not reachable (matching the other driver tests).
func testObjectArrayExecutor(t *testing.T) *executor.ClickHouseExecutor {
	t.Helper()
	e, err := executor.NewClickHouseExecutor(testDevClickHouseConfig(t))
	if err != nil {
		t.Skipf("dev ClickHouse not reachable: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func executeFirstCellJSON(t *testing.T, e *executor.ClickHouseExecutor, query string) string {
	t.Helper()
	res, err := e.Execute(context.Background(), query, nil, executor.OutputLimits{})
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	require.Len(t, res.Rows[0], 1)
	b, err := json.Marshal(res.Rows[0][0])
	require.NoError(t, err)
	return string(b)
}

// Object-array columns (ClickHouse Nested -> Array(Tuple) on the wire) must
// scan like ClickHouse's own JSON serialization: named tuples as objects,
// unnamed tuples as positional arrays, with nesting preserved at every depth.
// Every case here used to 500 with "reflect.Set: value of type
// []map[string]interface {} is not assignable to type []interface {}".
func TestClickHouseObjectArrayExecute(t *testing.T) {
	e := testObjectArrayExecutor(t)

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{
			"unnamed tuple array scans as positional arrays",
			"SELECT [(1, 'x'), (2, 'y')]",
			`[[1,"x"],[2,"y"]]`,
		},
		{
			"named tuple array scans as objects",
			"SELECT CAST([(1, 'x')] AS Array(Tuple(a Int64, b String)))",
			`[{"a":1,"b":"x"}]`,
		},
		{
			"unnamed nested object array preserves nesting",
			"SELECT [[(1, 'x')], [(2, 'y'), (3, 'z')]]",
			`[[[1,"x"]],[[2,"y"],[3,"z"]]]`,
		},
		{
			"named nested object array preserves nesting",
			"SELECT CAST([[(1, 'x')], [(2, 'y')]] AS Array(Array(Tuple(a Int64, b String))))",
			`[[{"a":1,"b":"x"}],[{"a":2,"b":"y"}]]`,
		},
		{
			"named tuple with nested object array element",
			"SELECT CAST([(1, [(2, 'x')])] AS Array(Tuple(a Int64, b Array(Tuple(c Int64, d String)))))",
			`[{"a":1,"b":[{"c":2,"d":"x"}]}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, executeFirstCellJSON(t, e, tc.query))
		})
	}
}

// TestClickHouseObjectArrayDriverBehavior pins the clickhouse-go v2.47
// behavior the allocator relies on: an object array panics when scanned into
// *[]any (the production bug) and scans correctly into the typed destinations
// chAllocDest now chooses. If a future driver changes either direction, this
// fails loudly.
func TestClickHouseObjectArrayDriverBehavior(t *testing.T) {
	cfg := testDevClickHouseConfig(t)
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr:     []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)},
		Auth:     clickhouse.Auth{Username: cfg.User, Password: cfg.Password, Database: cfg.Database},
		Protocol: clickhouse.Native,
	})
	if err != nil {
		t.Skipf("dev ClickHouse not reachable: %v", err)
	}
	defer conn.Close()
	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		t.Skipf("dev ClickHouse not reachable: %v", err)
	}

	t.Run("object array into []any panics", func(t *testing.T) {
		rows, err := conn.Query(context.Background(), "SELECT [(1, 'x'), (2, 'y')]")
		require.NoError(t, err)
		defer rows.Close()

		defer func() {
			r := recover()
			require.NotNil(t, r, "expected clickhouse-go to panic scanning Array(Tuple) into *[]any")
			require.Contains(t, fmt.Sprint(r), "reflect.Set")
		}()

		require.True(t, rows.Next())
		var dest []any
		_ = rows.Scan(&dest)
	})

	t.Run("unnamed object array into [][]any", func(t *testing.T) {
		var dest [][]any
		scanFirstRow(t, conn, "SELECT [(1, 'x'), (2, 'y')]", &dest)
		require.Equal(t, [][]any{{uint8(1), "x"}, {uint8(2), "y"}}, dest)
	})

	t.Run("named object array into []map[string]any", func(t *testing.T) {
		var dest []map[string]any
		scanFirstRow(t, conn, "SELECT CAST([(1, 'x')] AS Array(Tuple(a Int64, b String)))", &dest)
		require.Equal(t, []map[string]any{{"a": int64(1), "b": "x"}}, dest)
	})
}

func scanFirstRow(t *testing.T, conn clickhouse.Conn, query string, dest any) {
	t.Helper()
	rows, err := conn.Query(context.Background(), query)
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(dest))
}
