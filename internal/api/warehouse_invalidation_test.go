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

// newWarehouseInvalidationTestChannel returns a unique channel per test. The
// shared Redis can carry other processes' subscribers (a running dev API uses
// the default channel), so tests must never assert on global PUBSUB NUMSUB
// counts or publish on the default channel: a unique channel makes both the
// readiness and the detach checks local to the test.
func newWarehouseInvalidationTestChannel() string {
	return "test:warehouse-identity-invalidation:" + uuid.NewString()
}

// newWarehouseInvalidationTestServer builds a full Server (shared test
// database, its own connection pool) wired to the test Redis, so two instances
// can act as separate API replicas. Both replicas of a round-trip test must be
// created with the same channel. It skips when Redis is unreachable.
func newWarehouseInvalidationTestServer(t *testing.T, channel string) *Server {
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
	if channel != "" {
		s.warehouseInvalidationChannel = channel
	}
	t.Cleanup(s.Close)
	return s
}

// waitForWarehouseInvalidationSubscribers blocks until at least want
// subscribers are attached to the test's own channel. That is a real readiness
// handshake: the server's SUBSCRIBE has been processed by Redis, so a
// subsequent publish cannot race subscriber startup.
func waitForWarehouseInvalidationSubscribers(t *testing.T, rdb *redis.Client, channel string, want int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		counts, err := rdb.PubSubNumSub(context.Background(), channel).Result()
		if err != nil {
			return false
		}
		return counts[channel] >= want
	}, 5*time.Second, 10*time.Millisecond, "no subscriber attached to %s", channel)
}

// waitForWarehouseInvalidationSubscribersGone blocks until no subscriber
// remains on the test's own channel.
func waitForWarehouseInvalidationSubscribersGone(t *testing.T, rdb *redis.Client, channel string) {
	t.Helper()
	require.Eventually(t, func() bool {
		counts, err := rdb.PubSubNumSub(context.Background(), channel).Result()
		if err != nil {
			return false
		}
		return counts[channel] == 0
	}, 5*time.Second, 10*time.Millisecond, "subscriber did not detach from %s", channel)
}

// TestWarehouseInvalidationBroadcastRoundTrip is the cross-replica teeth: an
// invalidation on one replica must reach a second replica's pool and drop only
// the named identity's resident connection.
func TestWarehouseInvalidationBroadcastRoundTrip(t *testing.T) {
	requireClickHouseReachable(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := newWarehouseInvalidationTestChannel()
	publisher := newWarehouseInvalidationTestServer(t, channel)
	subscriber := newWarehouseInvalidationTestServer(t, channel)

	subscriber.startWarehouseInvalidationSubscriber(ctx)
	waitForWarehouseInvalidationSubscribers(t, subscriber.rdb, channel, 1)

	warehouseID, orgID, userID := uuid.New(), uuid.New(), uuid.New()
	decoyWarehouseID, decoyOrgID, decoyUserID := uuid.New(), uuid.New(), uuid.New()
	poolWarehouseIdentity(t, subscriber, warehouseID, orgID, userID)
	poolWarehouseIdentity(t, subscriber, decoyWarehouseID, decoyOrgID, decoyUserID)
	require.Equal(t, 2, subscriber.connPool.Len())

	// Re-publish on each tick: even with the NUMSUB readiness handshake, an
	// invalidation that lands before the subscriber is ready must not be able
	// to fail the test, and repeated invalidations are idempotent.
	require.Eventually(t, func() bool {
		publisher.invalidatePooledWarehouseIdentities(
			chaccess.DesiredState{Users: map[string]chaccess.UserState{
				chaccess.UserIdent(warehouseID, orgID, userID): {},
			}},
			chaccess.ActualState{},
		)
		return subscriber.connPool.Len() == 1
	}, 5*time.Second, 50*time.Millisecond,
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

	channel := newWarehouseInvalidationTestChannel()
	s := newWarehouseInvalidationTestServer(t, channel)
	s.startWarehouseInvalidationSubscriber(ctx)
	waitForWarehouseInvalidationSubscribers(t, s.rdb, channel, 1)

	require.NoError(t, s.rdb.Publish(ctx, channel, "not json at all").Err())
	require.NoError(t, s.rdb.Publish(ctx, channel, []byte(`{"users":[1,2]}`)).Err())

	warehouseID, orgID, userID := uuid.New(), uuid.New(), uuid.New()
	users := map[string]struct{}{chaccess.UserIdent(warehouseID, orgID, userID): {}}
	poolWarehouseIdentity(t, s, warehouseID, orgID, userID)
	require.Equal(t, 1, s.connPool.Len())

	require.Eventually(t, func() bool {
		s.publishWarehouseIdentityInvalidation(users)
		return s.connPool.Len() == 0
	}, 5*time.Second, 50*time.Millisecond,
		"the subscriber must survive malformed payloads and apply later messages")
}

// TestWarehouseInvalidationChunkingLargeSets publishes more identities than
// one message can carry and reads the raw channel: the set must arrive in
// multiple chunks with no loss and no duplication.
func TestWarehouseInvalidationChunkingLargeSets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := newWarehouseInvalidationTestChannel()
	s := newWarehouseInvalidationTestServer(t, channel)
	raw := s.rdb.Subscribe(ctx, channel)
	t.Cleanup(func() { raw.Close() })
	// Wait for the raw subscription to be registered before publishing so the
	// first chunk cannot be dropped.
	waitForWarehouseInvalidationSubscribers(t, s.rdb, channel, 1)
	ch := raw.Channel()

	total := warehouseIdentityInvalidationChunkSize + 1
	users := make(map[string]struct{}, total)
	for i := 0; i < total; i++ {
		users[fmt.Sprintf("aether_chunk_u_%04d", i)] = struct{}{}
	}

	s.publishWarehouseIdentityInvalidation(users)

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
	stopped := func(t *testing.T, s *Server, channel string) {
		t.Helper()
		waitForWarehouseInvalidationSubscribersGone(t, s.rdb, channel)

		warehouseID, orgID, userID := uuid.New(), uuid.New(), uuid.New()
		poolWarehouseIdentity(t, s, warehouseID, orgID, userID)
		s.publishWarehouseIdentityInvalidation(map[string]struct{}{
			chaccess.UserIdent(warehouseID, orgID, userID): {},
		})
		time.Sleep(100 * time.Millisecond)
		require.Equal(t, 1, s.connPool.Len(),
			"a stopped subscriber must not apply broadcasts")
	}

	t.Run("context cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		channel := newWarehouseInvalidationTestChannel()
		s := newWarehouseInvalidationTestServer(t, channel)
		s.startWarehouseInvalidationSubscriber(ctx)
		waitForWarehouseInvalidationSubscribers(t, s.rdb, channel, 1)

		cancel()
		select {
		case <-s.warehouseInvalidationLoop.done:
		case <-time.After(5 * time.Second):
			t.Fatal("the subscriber goroutine must exit when its context is cancelled")
		}
		stopped(t, s, channel)
	})

	t.Run("server close", func(t *testing.T) {
		channel := newWarehouseInvalidationTestChannel()
		s := newWarehouseInvalidationTestServer(t, channel)
		s.startWarehouseInvalidationSubscriber(context.Background())
		waitForWarehouseInvalidationSubscribers(t, s.rdb, channel, 1)

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
		stopped(t, s, channel)
	})
}

// TestWarehouseInvalidationPublishFailsOpenWithoutRedis proves a dead Redis
// neither blocks the caller nor skips the local invalidation: publishing is
// additive and best-effort.
func TestWarehouseInvalidationPublishFailsOpenWithoutRedis(t *testing.T) {
	s := newWarehouseInvalidationTestServer(t, newWarehouseInvalidationTestChannel())

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
