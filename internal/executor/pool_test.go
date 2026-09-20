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

func mustGet(t *testing.T, p *ConnPool, endpoint, user string, cfg models.ConnectorConfig) (clickhouse.Conn, func()) {
	t.Helper()
	conn, release, err := p.Get(endpoint, user, cfg)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NotNil(t, release)
	return conn, release
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
	c1, rel1 := mustGet(t, p, "ep1:9000", "u1", cfg)
	c2, rel2 := mustGet(t, p, "ep1:9000", "u1", cfg)
	require.Equal(t, int64(1), opened.Load(), "same key must reuse")
	require.Same(t, c1, c2)
	rel1()
	rel2()
	require.False(t, c1.(*fakeConn).closed, "release must not close a pooled conn")
	require.Equal(t, 1, p.Len())

	cU2, relU2 := mustGet(t, p, "ep1:9000", "u2", cfg)
	cU3, relU3 := mustGet(t, p, "ep1:9000", "u3", cfg)

	require.Equal(t, int64(3), opened.Load())
	require.Equal(t, 2, p.Len())
	require.True(t, c1.(*fakeConn).closed, "LRU entry must be closed on eviction")
	require.False(t, cU2.(*fakeConn).closed, "recent entries must stay open")
	require.False(t, cU3.(*fakeConn).closed, "recent entries must stay open")
	relU2()
	relU3()
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
	u1, rel1 := mustGet(t, p, "ep", "u1", cfg)
	u2, rel2 := mustGet(t, p, "ep", "u2", cfg)
	_, rel1b := mustGet(t, p, "ep", "u1", cfg)
	rel1()
	rel1b()
	rel2()

	_, rel3 := mustGet(t, p, "ep", "u3", cfg)
	require.Equal(t, int64(3), opened.Load())
	require.Equal(t, 2, p.Len())
	require.False(t, u1.(*fakeConn).closed, "recently used entry must survive")
	require.True(t, u2.(*fakeConn).closed, "least recently used entry must be evicted")
	rel3()
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
	a, relA := mustGet(t, p, "ep", "u1", cfg)
	b, relB := mustGet(t, p, "ep", "u2", cfg)
	require.NotSame(t, a, b)
	require.Equal(t, int64(2), opened.Load())
	require.Equal(t, 2, p.Len())
	relA()
	relB()
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
	c1, rel1 := mustGet(t, p, "ep", "u1", cfg)
	_, rel2 := mustGet(t, p, "ep", "u1", cfg)
	rel1()
	rel2()

	cfg.Password = "new"
	c3, rel3 := mustGet(t, p, "ep", "u1", cfg)
	require.NotSame(t, c1, c3)
	require.Equal(t, int64(2), opened.Load())
	require.True(t, c1.(*fakeConn).closed, "stale-credential conn must be closed")
	require.False(t, c3.(*fakeConn).closed)
	require.Equal(t, 1, p.Len())
	rel3()
}

func TestPoolReopensOnDatabaseChange(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000, User: "u1", Password: "p", Database: "db1"}
	c1, rel1 := mustGet(t, p, "ep", "u1", cfg)
	rel1()

	cfg.Database = "db2"
	c2, rel2 := mustGet(t, p, "ep", "u1", cfg)
	require.NotSame(t, c1, c2)
	require.Equal(t, int64(2), opened.Load())
	require.True(t, c1.(*fakeConn).closed, "stale-database conn must be closed")
	require.Equal(t, 1, p.Len())
	rel2()
}

func TestPoolReopensOnHostChange(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h1", Port: 9000, User: "u1"}
	c1, rel1 := mustGet(t, p, "ep", "u1", cfg)
	rel1()

	cfg.Host = "h2"
	c2, rel2 := mustGet(t, p, "ep", "u1", cfg)
	require.NotSame(t, c1, c2)
	require.True(t, c1.(*fakeConn).closed)
	require.Equal(t, int64(2), opened.Load())
	rel2()
}

func TestCredentialFingerprintDistinguishesFieldBoundaries(t *testing.T) {
	a := credentialFingerprint(models.ConnectorConfig{Host: "a", Database: "bc"})
	b := credentialFingerprint(models.ConnectorConfig{Host: "ab", Database: "c"})
	require.NotEqual(t, a, b)
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
	c1, rel1 := mustGet(t, p, "ep", "u1", cfg)
	_, rel2 := mustGet(t, p, "ep", "u2", cfg)
	rel1()
	rel2()

	p.Invalidate("ep", "u1")
	require.True(t, c1.(*fakeConn).closed)
	require.Equal(t, 1, p.Len())

	p.Invalidate("ep", "u1")
	require.Equal(t, 1, p.Len())

	c2, rel3 := mustGet(t, p, "ep", "u1", cfg)
	require.NotSame(t, c1, c2)
	require.Equal(t, int64(3), opened.Load())
	require.False(t, c2.(*fakeConn).closed)
	rel3()
}

func TestPoolInvalidateWhileInUseClosesOnLastRelease(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	c1, rel1 := mustGet(t, p, "ep", "u1", cfg)
	_, rel2 := mustGet(t, p, "ep", "u1", cfg)

	p.Invalidate("ep", "u1")
	require.Equal(t, 0, p.Len(), "invalidated entry leaves the pool immediately")
	require.False(t, c1.(*fakeConn).closed, "in-use conn must not close mid-lease")

	c2, rel3 := mustGet(t, p, "ep", "u1", cfg)
	require.NotSame(t, c1, c2)
	require.Equal(t, int64(2), opened.Load())

	rel1()
	require.False(t, c1.(*fakeConn).closed, "another lease is still outstanding")
	rel2()
	require.True(t, c1.(*fakeConn).closed, "last release must close invalidated conn")
	require.False(t, c2.(*fakeConn).closed)
	rel3()
}

func TestPoolEvictionSkipsInUseAndResolvesOverage(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 1,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	u1, rel1 := mustGet(t, p, "ep", "u1", cfg)
	u2, rel2 := mustGet(t, p, "ep", "u2", cfg)
	require.Equal(t, int64(2), opened.Load())
	require.Equal(t, 2, p.Len(), "in-use entries may temporarily exceed MaxPools")
	require.False(t, u1.(*fakeConn).closed)
	require.False(t, u2.(*fakeConn).closed)

	rel1()
	require.Equal(t, 1, p.Len(), "overage resolves on release")
	require.True(t, u1.(*fakeConn).closed, "released LRU entry is evicted")
	require.False(t, u2.(*fakeConn).closed)
	rel2()
}

func TestPoolReleaseIsIdempotent(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	c1, rel := mustGet(t, p, "ep", "u1", cfg)
	rel()
	rel()

	c2, rel2 := mustGet(t, p, "ep", "u1", cfg)
	require.Same(t, c1, c2, "double release must not drop the entry")
	require.False(t, c1.(*fakeConn).closed)
	require.Equal(t, int64(1), opened.Load())
	rel2()
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
	expired, relExpired := mustGet(t, p, "ep", "expired", cfg)
	p.mu.Lock()
	p.entries[poolKey{endpoint: "ep", user: "expired"}].lastUsed = base.Add(-2 * time.Minute)
	p.mu.Unlock()

	fresh, relFresh := mustGet(t, p, "ep", "fresh", cfg)
	relExpired()
	relFresh()

	p.CloseIdle(base)
	require.Equal(t, 1, p.Len(), "only the expired entry should be evicted")
	require.True(t, expired.(*fakeConn).closed)
	require.False(t, fresh.(*fakeConn).closed)

	p.CloseIdle(base.Add(2 * time.Minute))
	require.Equal(t, 0, p.Len())
	require.True(t, fresh.(*fakeConn).closed)
	require.Equal(t, int64(2), opened.Load())
}

func TestPoolCloseIdleSkipsInUse(t *testing.T) {
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		IdleTTL:  time.Minute,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	c1, rel := mustGet(t, p, "ep", "u1", cfg)
	p.mu.Lock()
	p.entries[poolKey{endpoint: "ep", user: "u1"}].lastUsed = time.Now().Add(-2 * time.Minute)
	p.mu.Unlock()

	p.CloseIdle(time.Now())
	require.Equal(t, 1, p.Len(), "in-use entry must not be evicted")
	require.False(t, c1.(*fakeConn).closed)

	rel()
	p.CloseIdle(time.Now().Add(time.Minute))
	require.Equal(t, 0, p.Len(), "entry is evicted once idle")
	require.True(t, c1.(*fakeConn).closed)
}

func TestPoolCloseIdleDisabledWithZeroTTL(t *testing.T) {
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			return &fakeConn{}, nil
		},
	})
	conn, rel := mustGet(t, p, "ep", "u1", models.ConnectorConfig{Host: "h", Port: 9000})
	rel()

	p.CloseIdle(time.Now().Add(24 * time.Hour))
	require.Equal(t, 1, p.Len())
	require.False(t, conn.(*fakeConn).closed)
}

func TestPoolCloseAllClosesAndEmpties(t *testing.T) {
	var opened atomic.Int64
	p := NewConnPool(PoolConfig{
		MaxPools: 4,
		Open: func(models.ConnectorConfig) (clickhouse.Conn, error) {
			opened.Add(1)
			return &fakeConn{}, nil
		},
	})

	cfg := models.ConnectorConfig{Host: "h", Port: 9000}
	a, relA := mustGet(t, p, "ep", "u1", cfg)
	b, relB := mustGet(t, p, "ep", "u2", cfg)
	relA()
	relB()

	c, relC := mustGet(t, p, "ep", "u3", cfg)
	p.CloseAll()
	require.Equal(t, 0, p.Len())
	require.True(t, a.(*fakeConn).closed)
	require.True(t, b.(*fakeConn).closed)
	require.False(t, c.(*fakeConn).closed, "in-use conn must not close mid-query")

	relC()
	require.True(t, c.(*fakeConn).closed, "in-use conn closes on release after CloseAll")
	require.Equal(t, int64(3), opened.Load())
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
			conn, release, err := p.Get("ep", fmt.Sprintf("u%d", w%users), cfg)
			if err != nil {
				t.Errorf("get: %v", err)
				return
			}
			results[w%users] <- conn
			release()
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

	conn, release, err := p.Get("127.0.0.1:1", "u", models.ConnectorConfig{Host: "127.0.0.1", Port: 1})
	require.NoError(t, err, "default open must not dial")
	require.NotNil(t, conn)
	release()
	require.NoError(t, conn.Close())
}
