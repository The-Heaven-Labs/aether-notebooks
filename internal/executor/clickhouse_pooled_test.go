package executor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/models"
)

func TestPooledExecutorCloseReleasesOnceAndDoesNotCloseConn(t *testing.T) {
	fake := &fakeConn{}
	var releases atomic.Int64
	e := NewPooledClickHouseExecutor(fake, func() { releases.Add(1) })

	require.NoError(t, e.Close())
	require.Equal(t, int64(1), releases.Load())
	require.False(t, fake.closed, "pooled Close must not close the leased conn")

	require.NoError(t, e.Close())
	require.Equal(t, int64(1), releases.Load(), "double Close must release exactly once")
	require.False(t, fake.closed, "pooled Close must not close the leased conn")
}

func TestPooledExecutorCloseConcurrent(t *testing.T) {
	fake := &fakeConn{}
	var releases atomic.Int64
	e := NewPooledClickHouseExecutor(fake, func() { releases.Add(1) })

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.Close()
		}()
	}
	wg.Wait()

	require.Equal(t, int64(1), releases.Load(), "concurrent Close must release exactly once")
	require.False(t, fake.closed)
}

func TestPooledExecutorNilReleaseIsNoOp(t *testing.T) {
	fake := &fakeConn{}
	e := NewPooledClickHouseExecutor(fake, nil)

	require.NoError(t, e.Close())
	require.NoError(t, e.Close())
	require.False(t, fake.closed)
}

func TestNonPooledExecutorCloseClosesConn(t *testing.T) {
	fake := &fakeConn{}
	e := &ClickHouseExecutor{conn: fake}

	require.NoError(t, e.Close())
	require.True(t, fake.closed, "non-pooled executor must close its own conn")

	require.NoError(t, e.Close())
	require.True(t, fake.closed)
}

type recordingConn struct {
	*fakeConn
	execCount atomic.Int64
	lastQuery atomic.Value
}

func (r *recordingConn) Exec(ctx context.Context, query string, args ...any) error {
	r.execCount.Add(1)
	r.lastQuery.Store(query)
	return r.fakeConn.Exec(ctx, query, args...)
}

func TestPooledExecutorExecutesOnLeasedConn(t *testing.T) {
	p := NewConnPool(PoolConfig{
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			return &recordingConn{fakeConn: &fakeConn{}}, nil
		},
	})
	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	conn, release := mustGet(t, p, "ep", "u", cfg)
	e := NewPooledClickHouseExecutor(conn, release)

	res, err := e.Execute(context.Background(), "CREATE TEMPORARY TABLE t (x UInt8)", nil, OutputLimits{})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.Rows)

	rec := conn.(*recordingConn)
	require.Equal(t, int64(1), rec.execCount.Load())
	require.Contains(t, rec.lastQuery.Load().(string), "CREATE TEMPORARY TABLE")

	require.NoError(t, e.Close())
	require.False(t, rec.fakeConn.closed, "Close must return the lease, not close the conn")
	require.Equal(t, 1, p.Len(), "released conn stays in the pool")

	conn2, release2 := mustGet(t, p, "ep", "u", cfg)
	require.Same(t, conn, conn2, "released conn must be reused")
	release2()
}
