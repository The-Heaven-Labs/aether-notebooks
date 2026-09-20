package executor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/models"
)

type fakeConn struct {
	closed bool
}

var _ clickhouse.Conn = (*fakeConn)(nil)

func (f *fakeConn) Contributors() []string { return nil }

func (f *fakeConn) ServerVersion() (*driver.ServerVersion, error) { return nil, nil }

func (f *fakeConn) Select(context.Context, any, string, ...any) error { return nil }

func (f *fakeConn) Query(context.Context, string, ...any) (driver.Rows, error) { return nil, nil }

func (f *fakeConn) QueryRow(context.Context, string, ...any) driver.Row { return nil }

func (f *fakeConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	return nil, nil
}

func (f *fakeConn) Exec(context.Context, string, ...any) error { return nil }

func (f *fakeConn) AsyncInsert(context.Context, string, bool, ...any) error { return nil }

func (f *fakeConn) Ping(context.Context) error { return nil }

func (f *fakeConn) Stats() driver.Stats { return driver.Stats{} }

func (f *fakeConn) Close() error {
	f.closed = true
	return nil
}

func TestPoolReusesConnPerUserAndEvictsLRU(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 2,
		IdleTTL:  time.Minute,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000, User: "u1"}
	c1, err := p.Get("ep1:9000", "u1", cfg)
	require.NoError(t, err)
	c2, err := p.Get("ep1:9000", "u1", cfg)
	require.NoError(t, err)
	require.Equal(t, int64(1), opened.Load(), "same key must reuse")
	require.Same(t, c1, c2)

	cU2, err := p.Get("ep1:9000", "u2", cfg)
	require.NoError(t, err)
	cU3, err := p.Get("ep1:9000", "u3", cfg)
	require.NoError(t, err)

	require.Equal(t, int64(3), opened.Load())
	require.Equal(t, 2, p.Len())
	require.True(t, c1.(*fakeConn).closed, "LRU entry must be closed on eviction")
	require.False(t, cU2.(*fakeConn).closed, "recent entries must stay open")
	require.False(t, cU3.(*fakeConn).closed, "recent entries must stay open")
}

func TestPoolEvictsLeastRecentlyUsedNotOldestOpened(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 2,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	u1, err := p.Get("ep", "u1", cfg)
	require.NoError(t, err)
	u2, err := p.Get("ep", "u2", cfg)
	require.NoError(t, err)
	_, err = p.Get("ep", "u1", cfg)
	require.NoError(t, err)
	_, err = p.Get("ep", "u3", cfg)
	require.NoError(t, err)

	require.Equal(t, int64(3), opened.Load())
	require.Equal(t, 2, p.Len())
	require.False(t, u1.(*fakeConn).closed, "recently used entry must survive")
	require.True(t, u2.(*fakeConn).closed, "least recently used entry must be evicted")
}

func TestPoolSeparatesConnectionsPerUser(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	a, err := p.Get("ep", "u1", cfg)
	require.NoError(t, err)
	b, err := p.Get("ep", "u2", cfg)
	require.NoError(t, err)
	require.NotSame(t, a, b)
	require.Equal(t, int64(2), opened.Load())
	require.Equal(t, 2, p.Len())
}

func TestPoolReopensOnCredentialFingerprintChange(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000, User: "u1", Password: "old"}
	c1, err := p.Get("ep", "u1", cfg)
	require.NoError(t, err)
	c2, err := p.Get("ep", "u1", cfg)
	require.NoError(t, err)
	require.Same(t, c1, c2)

	cfg.Password = "new"
	c3, err := p.Get("ep", "u1", cfg)
	require.NoError(t, err)
	require.NotSame(t, c1, c3)
	require.Equal(t, int64(2), opened.Load())
	require.True(t, c1.(*fakeConn).closed, "stale-credential conn must be closed")
	require.False(t, c3.(*fakeConn).closed)
	require.Equal(t, 1, p.Len())
}

func TestPoolInvalidateClosesAndRemoves(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	c1, err := p.Get("ep", "u1", cfg)
	require.NoError(t, err)
	_, err = p.Get("ep", "u2", cfg)
	require.NoError(t, err)

	p.Invalidate("ep", "u1")
	require.True(t, c1.(*fakeConn).closed)
	require.Equal(t, 1, p.Len())

	p.Invalidate("ep", "u1")
	require.Equal(t, 1, p.Len())

	c2, err := p.Get("ep", "u1", cfg)
	require.NoError(t, err)
	require.NotSame(t, c1, c2)
	require.Equal(t, int64(3), opened.Load())
	require.False(t, c2.(*fakeConn).closed)
}

func TestPoolCloseIdleEvictsExpiredOnly(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		IdleTTL:  time.Minute,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	base := time.Now()
	expired, err := p.Get("ep", "expired", cfg)
	require.NoError(t, err)
	p.mu.Lock()
	p.entries[poolKey{endpoint: "ep", user: "expired"}].lastUsed = base.Add(-2 * time.Minute)
	p.mu.Unlock()

	fresh, err := p.Get("ep", "fresh", cfg)
	require.NoError(t, err)

	p.CloseIdle(base)
	require.Equal(t, 1, p.Len(), "only the expired entry should be evicted")
	require.True(t, expired.(*fakeConn).closed)
	require.False(t, fresh.(*fakeConn).closed)

	p.CloseIdle(base.Add(2 * time.Minute))
	require.Equal(t, 0, p.Len())
	require.True(t, fresh.(*fakeConn).closed)
	require.Equal(t, int64(2), opened.Load())
}

func TestPoolCloseIdleDisabledWithZeroTTL(t *testing.T) {
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			return &fakeConn{}, nil
		},
	})
	conn, err := p.Get("ep", "u1", models.ConnectorConfig{Host: "h", Port: 9000})
	require.NoError(t, err)

	p.CloseIdle(time.Now().Add(24 * time.Hour))
	require.Equal(t, 1, p.Len())
	require.False(t, conn.(*fakeConn).closed)
}

func TestPoolConcurrentGet(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		IdleTTL:  time.Minute,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	const users = 4
	const workers = 32
	results := make([]chan clickhouse.Conn, users)
	for i := range results {
		results[i] = make(chan clickhouse.Conn, workers)
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			conn, err := p.Get("ep", fmt.Sprintf("u%d", w%users), cfg)
			if err != nil {
				t.Errorf("get: %v", err)
				return
			}
			results[w%users] <- conn
		}(w)
	}
	wg.Wait()

	require.Equal(t, int64(users), opened.Load(), "one conn per user despite concurrent gets")
	require.Equal(t, users, p.Len())
	for i, ch := range results {
		first := <-ch
		for j := 1; j < workers/users; j++ {
			require.Same(t, first, <-ch, "user %d must share one conn", i)
		}
	}
}

func TestChOptionsSharedByExecutorAndPool(t *testing.T) {
	cfg := models.ConnectorConfig{
		Host: "ch.example", User: "u", Password: "p", Database: "db", SSLMode: "require",
	}
	opts := chOptions(cfg)
	require.Equal(t, []string{"ch.example:9000"}, opts.Addr)
	require.Equal(t, clickhouse.Native, opts.Protocol)
	require.Equal(t, "u", opts.Auth.Username)
	require.Equal(t, "p", opts.Auth.Password)
	require.Equal(t, "db", opts.Auth.Database)
	require.NotNil(t, opts.TLS)
	require.True(t, opts.TLS.InsecureSkipVerify)

	verify := chOptions(models.ConnectorConfig{Host: "h", Port: 9440, SSLMode: "verify-full"})
	require.Equal(t, []string{"h:9440"}, verify.Addr)
	require.NotNil(t, verify.TLS)
	require.False(t, verify.TLS.InsecureSkipVerify)

	plain := chOptions(models.ConnectorConfig{Host: "h"})
	require.Nil(t, plain.TLS)
	require.Empty(t, plain.Auth.Database)
}

func TestPoolDefaultOpenIsLazy(t *testing.T) {
	p := NewConnPool(PoolConfig{})
	require.NotNil(t, p.cfg.Open)

	conn, err := p.Get("127.0.0.1:1", "u", models.ConnectorConfig{Host: "127.0.0.1", Port: 1})
	require.NoError(t, err, "default open must not dial")
	require.NotNil(t, conn)
	require.NoError(t, conn.Close())
}
