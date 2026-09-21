package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/cache"
	"github.com/the-heaven-labs/aether/internal/chaccess"
)

// warehouseInvalidationTestRedisURL returns the Redis URL used by these tests.
func warehouseInvalidationTestRedisURL() string {
	if u := os.Getenv("AETHER_REDIS_URL"); u != "" {
		return u
	}
	return "redis://localhost:6379"
}

// newWarehouseInvalidationTestServer builds a full Server (shared test
// database, its own connection pool) wired to the test Redis, so two instances
// can act as separate API replicas. It skips when Redis is unreachable.
func newWarehouseInvalidationTestServer(t *testing.T) *Server {
	t.Helper()
	shared, key := sharedWarehouseTestServer(t)
	c, err := cache.New(warehouseInvalidationTestRedisURL())
	if err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		c.Close()
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	s := NewServer(shared.db, auth.NewJWTIssuer("test-secret", 15*time.Minute), audit.NewLogger(shared.db), key, c)
	s.SetCHTablePermissions(true)
	t.Cleanup(s.Close)
	return s
}

// waitForWarehouseInvalidationSubscribers blocks until at least want
// subscribers are attached to the invalidation channel, so a test publish
// cannot race the subscriber's SUBSCRIBE command.
func waitForWarehouseInvalidationSubscribers(t *testing.T, rdb *redis.Client, want int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		counts, err := rdb.PubSubNumSub(context.Background(), warehouseIdentityInvalidationChannel).Result()
		if err != nil {
			return false
		}
		return counts[warehouseIdentityInvalidationChannel] >= want
	}, 5*time.Second, 10*time.Millisecond, "no subscriber attached to %s", warehouseIdentityInvalidationChannel)
}

// waitForWarehouseInvalidationSubscribersGone blocks until no subscriber
// remains on the invalidation channel.
func waitForWarehouseInvalidationSubscribersGone(t *testing.T, rdb *redis.Client) {
	t.Helper()
	require.Eventually(t, func() bool {
		counts, err := rdb.PubSubNumSub(context.Background(), warehouseIdentityInvalidationChannel).Result()
		if err != nil {
			return false
		}
		return counts[warehouseIdentityInvalidationChannel] == 0
	}, 5*time.Second, 10*time.Millisecond, "subscriber did not detach from %s", warehouseIdentityInvalidationChannel)
}

// TestWarehouseInvalidationBroadcastRoundTrip is the cross-replica teeth: an
// invalidation on one replica must reach a second replica's pool and drop only
// the named identity's resident connection.
func TestWarehouseInvalidationBroadcastRoundTrip(t *testing.T) {
	requireClickHouseReachable(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	publisher := newWarehouseInvalidationTestServer(t)
	subscriber := newWarehouseInvalidationTestServer(t)

	subscriber.startWarehouseInvalidationSubscriber(ctx)
	waitForWarehouseInvalidationSubscribers(t, subscriber.rdb, 1)

	warehouseID, orgID, userID := uuid.New(), uuid.New(), uuid.New()
	decoyWarehouseID, decoyOrgID, decoyUserID := uuid.New(), uuid.New(), uuid.New()
	poolWarehouseIdentity(t, subscriber, warehouseID, orgID, userID)
	poolWarehouseIdentity(t, subscriber, decoyWarehouseID, decoyOrgID, decoyUserID)
	require.Equal(t, 2, subscriber.connPool.Len())

	publisher.invalidatePooledWarehouseIdentities(
		chaccess.DesiredState{Users: map[string]chaccess.UserState{
			chaccess.UserIdent(warehouseID, orgID, userID): {},
		}},
		chaccess.ActualState{},
	)

	require.Eventually(t, func() bool { return subscriber.connPool.Len() == 1 },
		5*time.Second, 20*time.Millisecond,
		"the other replica must drop the invalidated identity's pooled connection")
	require.Equal(t, 1, subscriber.connPool.Len(),
		"the untargeted identity must stay resident")
}

// TestWarehouseInvalidationMalformedPayloadIgnored proves an unparseable or
// wrong-shaped payload neither panics nor kills the subscriber: a valid
// invalidation afterwards must still be applied.
func TestWarehouseInvalidationMalformedPayloadIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newWarehouseInvalidationTestServer(t)
	s.startWarehouseInvalidationSubscriber(ctx)
	waitForWarehouseInvalidationSubscribers(t, s.rdb, 1)

	require.NoError(t, s.rdb.Publish(ctx, warehouseIdentityInvalidationChannel, "not json at all").Err())
	require.NoError(t, s.rdb.Publish(ctx, warehouseIdentityInvalidationChannel, []byte(`{"users":[1,2]}`)).Err())

	warehouseID, orgID, userID := uuid.New(), uuid.New(), uuid.New()
	poolWarehouseIdentity(t, s, warehouseID, orgID, userID)
	require.Equal(t, 1, s.connPool.Len())

	s.publishWarehouseIdentityInvalidation(map[string]struct{}{
		chaccess.UserIdent(warehouseID, orgID, userID): {},
	})

	require.Eventually(t, func() bool { return s.connPool.Len() == 0 },
		5*time.Second, 20*time.Millisecond,
		"the subscriber must survive malformed payloads and apply later messages")
}

// TestWarehouseInvalidationChunkingLargeSets publishes more identities than
// one message can carry and reads the raw channel: the set must arrive in
// multiple chunks with no loss and no duplication.
func TestWarehouseInvalidationChunkingLargeSets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newWarehouseInvalidationTestServer(t)
	raw := s.rdb.Subscribe(ctx, warehouseIdentityInvalidationChannel)
	t.Cleanup(func() { raw.Close() })
	waitForWarehouseInvalidationSubscribers(t, s.rdb, 1)

	total := warehouseIdentityInvalidationChunkSize + 1
	users := make(map[string]struct{}, total)
	for i := 0; i < total; i++ {
		users[fmt.Sprintf("aether_chunk_u_%04d", i)] = struct{}{}
	}

	s.publishWarehouseIdentityInvalidation(users)

	ch := raw.Channel()
	seen := make(map[string]int, total)
	messages := 0
	timeout := time.After(10 * time.Second)
	for len(seen) < total {
		select {
		case msg := <-ch:
			require.NotNil(t, msg)
			var payload warehouseIdentityInvalidationMessage
			require.NoError(t, json.Unmarshal([]byte(msg.Payload), &payload))
			require.LessOrEqual(t, len(payload.Users), warehouseIdentityInvalidationChunkSize)
			messages++
			for _, name := range payload.Users {
				seen[name]++
			}
		case <-timeout:
			t.Fatalf("timed out: received %d/%d identities across %d message(s)",
				len(seen), total, messages)
		}
	}
	require.GreaterOrEqual(t, messages, 2, "a set larger than the chunk size must be split")
	for name, count := range seen {
		require.Equal(t, 1, count, "identity %s must arrive exactly once", name)
	}
}

// TestWarehouseInvalidationSubscriberStops proves the subscriber goroutine
// exits and unsubscribes on context cancellation and on Server.Close, and that
// a stopped subscriber no longer applies broadcasts.
func TestWarehouseInvalidationSubscriberStops(t *testing.T) {
	t.Run("context cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s := newWarehouseInvalidationTestServer(t)
		s.startWarehouseInvalidationSubscriber(ctx)
		waitForWarehouseInvalidationSubscribers(t, s.rdb, 1)

		cancel()
		select {
		case <-s.warehouseInvalidationLoop.done:
		case <-time.After(5 * time.Second):
			t.Fatal("the subscriber goroutine must exit when its context is cancelled")
		}
		waitForWarehouseInvalidationSubscribersGone(t, s.rdb)

		warehouseID, orgID, userID := uuid.New(), uuid.New(), uuid.New()
		poolWarehouseIdentity(t, s, warehouseID, orgID, userID)
		s.publishWarehouseIdentityInvalidation(map[string]struct{}{
			chaccess.UserIdent(warehouseID, orgID, userID): {},
		})
		time.Sleep(100 * time.Millisecond)
		require.Equal(t, 1, s.connPool.Len(),
			"a stopped subscriber must not apply broadcasts")
	})

	t.Run("server close", func(t *testing.T) {
		s := newWarehouseInvalidationTestServer(t)
		s.startWarehouseInvalidationSubscriber(context.Background())
		waitForWarehouseInvalidationSubscribers(t, s.rdb, 1)

		closed := make(chan struct{})
		go func() {
			s.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Close must not block on the invalidation subscriber")
		}
		waitForWarehouseInvalidationSubscribersGone(t, s.rdb)
	})
}

// TestWarehouseInvalidationPublishFailsOpenWithoutRedis proves a dead Redis
// neither blocks the caller nor skips the local invalidation: publishing is
// additive and best-effort.
func TestWarehouseInvalidationPublishFailsOpenWithoutRedis(t *testing.T) {
	s := newWarehouseInvalidationTestServer(t)

	// Black-hole Redis: the listener accepts TCP so the client dials
	// successfully, then never answers, forcing the publish to wait for its
	// deadline instead of failing fast with connection refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var (
		connMu sync.Mutex
		conns  []net.Conn
	)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			connMu.Lock()
			conns = append(conns, c)
			connMu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		connMu.Lock()
		defer connMu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})

	blackhole, err := cache.New("redis://" + ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { blackhole.Close() })
	s.rdb = blackhole.Client()

	warehouseID, orgID, userID := uuid.New(), uuid.New(), uuid.New()
	poolWarehouseIdentity(t, s, warehouseID, orgID, userID)
	require.Equal(t, 1, s.connPool.Len())

	start := time.Now()
	s.invalidatePooledWarehouseIdentities(
		chaccess.DesiredState{Users: map[string]chaccess.UserState{
			chaccess.UserIdent(warehouseID, orgID, userID): {},
		}},
		chaccess.ActualState{},
	)
	elapsed := time.Since(start)

	require.Zero(t, s.connPool.Len(), "local invalidation must apply even when the broadcast fails")
	require.Less(t, elapsed, 2*time.Second, "publish must fail open instead of blocking the caller")
}
