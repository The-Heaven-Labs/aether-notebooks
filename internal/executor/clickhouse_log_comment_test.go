package executor

import (
	"context"
	"fmt"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"
)

// fakeRows is an empty driver.Rows: the executor's Query path needs a non-nil
// result to Close and inspect, but these tests only care about the context the
// query was issued with.
type fakeRows struct{}

func (fakeRows) Next() bool                       { return false }
func (fakeRows) Scan(...any) error                { return nil }
func (fakeRows) ScanStruct(any) error             { return nil }
func (fakeRows) ColumnTypes() []driver.ColumnType { return nil }
func (fakeRows) Totals(...any) error              { return nil }
func (fakeRows) Columns() []string                { return nil }
func (fakeRows) Close() error                     { return nil }
func (fakeRows) Err() error                       { return nil }
func (fakeRows) HasData() bool                    { return false }

// chQueryDump renders ctx's Go-syntax representation. clickhouse-go stores
// per-query settings on the context under an unexported key with no accessor,
// so dumping the context is how a fake conn observes the log_comment setting
// without a live server.
func chQueryDump(ctx context.Context) string {
	return fmt.Sprintf("%#v", ctx)
}

func TestExecutionIDContextRoundTrip(t *testing.T) {
	require.Empty(t, ExecutionIDFromContext(context.Background()))
	ctx := WithExecutionID(context.Background(), "exec-1")
	require.Equal(t, "exec-1", ExecutionIDFromContext(ctx))
}

func TestExecutePassesLogComment(t *testing.T) {
	const executionID = "6f9c2f1e-4b7a-4c1e-9d2b-8a3f5e7c1d40"
	want := `"log_comment":"aether:` + executionID + `"`

	t.Run("query path", func(t *testing.T) {
		fake := &fakeConn{}
		e := NewPooledClickHouseExecutor(fake, nil)
		_, err := e.Execute(WithExecutionID(context.Background(), executionID), "SELECT 1", nil, OutputLimits{})
		require.NoError(t, err)
		require.Contains(t, chQueryDump(fake.lastCtx), want)
	})

	t.Run("exec path", func(t *testing.T) {
		fake := &fakeConn{}
		e := NewPooledClickHouseExecutor(fake, nil)
		_, err := e.Execute(WithExecutionID(context.Background(), executionID), "CREATE TEMPORARY TABLE t (x UInt8)", nil, OutputLimits{})
		require.NoError(t, err)
		require.Contains(t, chQueryDump(fake.lastCtx), want)
	})

	t.Run("no execution id", func(t *testing.T) {
		fake := &fakeConn{}
		e := NewPooledClickHouseExecutor(fake, nil)
		_, err := e.Execute(context.Background(), "SELECT 1", nil, OutputLimits{})
		require.NoError(t, err)
		require.NotContains(t, chQueryDump(fake.lastCtx), "log_comment")
	})
}

// The aether_user comment is prepended before the statement is sent, but
// classification must run on the user's SQL first: otherwise every statement
// (DDL included) starts with "/*" and is sent through Query, which fails for
// commands that return no result set.
func TestExecuteClassifiesStatementsBeforeUserComment(t *testing.T) {
	const email = "trace@example.com"
	ctx := context.WithValue(context.Background(), CtxUserEmail{}, email)

	t.Run("ddl uses exec", func(t *testing.T) {
		conn := &recordingConn{fakeConn: &fakeConn{}}
		e := NewPooledClickHouseExecutor(conn, nil)
		_, err := e.Execute(ctx, "CREATE TEMPORARY TABLE t (x UInt8)", nil, OutputLimits{})
		require.NoError(t, err)
		require.Equal(t, int64(1), conn.execCount.Load())
		require.Equal(t, int64(0), conn.queryCount.Load())
		require.Contains(t, conn.lastQuery.Load().(string),
			"/* aether_user:"+email+" */ CREATE TEMPORARY TABLE")
	})

	t.Run("select uses query", func(t *testing.T) {
		conn := &recordingConn{fakeConn: &fakeConn{}}
		e := NewPooledClickHouseExecutor(conn, nil)
		_, err := e.Execute(ctx, "SELECT 1", nil, OutputLimits{})
		require.NoError(t, err)
		require.Equal(t, int64(0), conn.execCount.Load())
		require.Equal(t, int64(1), conn.queryCount.Load())
		require.Contains(t, conn.lastQuery.Load().(string),
			"/* aether_user:"+email+" */ SELECT 1")
	})
}

// ApplyLimit runs before Execute prepends the user comment; the final
// statement must keep both the limit and the tag.
func TestExecuteKeepsApplyLimitWithUserComment(t *testing.T) {
	limited := ApplyLimit("SELECT 1;", 10)
	require.Equal(t, "SELECT 1 LIMIT 10", limited)

	conn := &recordingConn{fakeConn: &fakeConn{}}
	e := NewPooledClickHouseExecutor(conn, nil)
	ctx := context.WithValue(context.Background(), CtxUserEmail{}, "trace@example.com")
	_, err := e.Execute(ctx, limited, nil, OutputLimits{})
	require.NoError(t, err)
	require.Equal(t, int64(1), conn.queryCount.Load())
	require.Contains(t, conn.lastQuery.Load().(string),
		"/* aether_user:trace@example.com */ SELECT 1 LIMIT 10")
}
