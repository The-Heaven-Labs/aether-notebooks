package api

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/cache"
)

// newDashboardDocEventsTestServer wires a minimal Server to a real Redis
// cache. These publishers touch nothing else on the Server, so no database or
// JWT wiring is needed. It skips when Redis is unreachable.
func newDashboardDocEventsTestServer(t *testing.T) *Server {
	t.Helper()
	redisURL := os.Getenv("AETHER_REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}
	c, err := cache.New(redisURL)
	require.NoError(t, err)
	if err := c.Ping(context.Background()); err != nil {
		c.Close()
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return &Server{Cache: c}
}

// waitForDashboardDocEventsSubscriber blocks until the test's own
// subscription is registered with Redis. That is a real readiness handshake:
// a subsequent publish cannot race subscriber startup and be dropped.
func waitForDashboardDocEventsSubscriber(t *testing.T, s *Server, channel string) {
	t.Helper()
	require.Eventually(t, func() bool {
		counts, err := s.Cache.Client().PubSubNumSub(context.Background(), channel).Result()
		if err != nil {
			return false
		}
		return counts[channel] >= 1
	}, 5*time.Second, 10*time.Millisecond, "no subscriber attached to %s", channel)
}

// receiveDashboardDocEvent waits for one message on the test subscription.
func receiveDashboardDocEvent(t *testing.T, ch <-chan *redis.Message) *redis.Message {
	t.Helper()
	select {
	case msg := <-ch:
		require.NotNil(t, msg)
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a dashboard doc event")
		return nil
	}
}

// TestDashboardDocEventsUpdateRoundTrip publishes a non-trivial binary Yjs
// update (including zero bytes) and asserts it arrives byte-for-byte intact on
// the dashboard's own channel.
func TestDashboardDocEventsUpdateRoundTrip(t *testing.T) {
	s := newDashboardDocEventsTestServer(t)
	ctx := context.Background()

	// Unique dashboard ID per test: the shared dev Redis can carry a running
	// API's traffic, so channels must never collide across tests or processes.
	dashboardID := uuid.NewString()
	channel := dashboardDocChannel(dashboardID)

	sub := s.Cache.Client().Subscribe(ctx, channel)
	t.Cleanup(func() { sub.Close() })
	waitForDashboardDocEventsSubscriber(t, s, channel)

	// Raw Yjs update bytes: binary, with interior and trailing zero bytes.
	update := []byte{0x00, 0x01, 0x02, 0x00, 0xff, 0xfe, 0x7f, 0x80, 0x00}
	s.publishDashboardDocUpdate(ctx, dashboardID, update)

	msg := receiveDashboardDocEvent(t, sub.Channel())
	require.Equal(t, channel, msg.Channel)
	require.Equal(t, update, []byte(msg.Payload), "binary update bytes must survive the round trip")
}

// TestDashboardDocEventsInvalidateRoundTrip publishes invalidation notices and
// asserts the JSON body decodes with the caller's reason.
func TestDashboardDocEventsInvalidateRoundTrip(t *testing.T) {
	s := newDashboardDocEventsTestServer(t)
	ctx := context.Background()

	dashboardID := uuid.NewString()
	channel := dashboardDocInvalidateChannel(dashboardID)

	sub := s.Cache.Client().Subscribe(ctx, channel)
	t.Cleanup(func() { sub.Close() })
	waitForDashboardDocEventsSubscriber(t, s, channel)

	for _, reason := range []string{"trashed", "purged"} {
		s.publishDashboardDocInvalidate(ctx, dashboardID, reason)

		msg := receiveDashboardDocEvent(t, sub.Channel())
		require.Equal(t, channel, msg.Channel)
		require.JSONEq(t, `{"reason":"`+reason+`"}`, msg.Payload,
			"the wire shape must stay a reason object")

		var payload dashboardDocInvalidatePayload
		require.NoError(t, json.Unmarshal([]byte(msg.Payload), &payload))
		require.Equal(t, reason, payload.Reason)
	}
}

// TestDashboardDocEventsPublishSurvivesCancelledContext proves the publishers
// detach from the caller's context: a request that is already cancelled (e.g.
// the client disconnected mid-request) must not skip the fan-out.
func TestDashboardDocEventsPublishSurvivesCancelledContext(t *testing.T) {
	s := newDashboardDocEventsTestServer(t)

	dashboardID := uuid.NewString()
	updateChannel := dashboardDocChannel(dashboardID)
	invalidateChannel := dashboardDocInvalidateChannel(dashboardID)

	sub := s.Cache.Client().Subscribe(context.Background(), updateChannel, invalidateChannel)
	t.Cleanup(func() { sub.Close() })
	waitForDashboardDocEventsSubscriber(t, s, updateChannel)
	waitForDashboardDocEventsSubscriber(t, s, invalidateChannel)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the caller's context is dead before either publish

	s.publishDashboardDocUpdate(ctx, dashboardID, []byte{0x00, 0x2a})
	s.publishDashboardDocInvalidate(ctx, dashboardID, "trashed")

	seen := make(map[string]bool, 2)
	for len(seen) < 2 {
		msg := receiveDashboardDocEvent(t, sub.Channel())
		seen[msg.Channel] = true
	}
	require.True(t, seen[updateChannel], "the update must be published despite the cancelled caller context")
	require.True(t, seen[invalidateChannel], "the invalidate must be published despite the cancelled caller context")
}

// TestDashboardDocEventsNilCacheNoop proves a Server without Redis (tests,
// single-node setups without a cache) neither panics nor needs a subscriber:
// the publishers are simply no-ops.
func TestDashboardDocEventsNilCacheNoop(t *testing.T) {
	s := &Server{}
	require.NotPanics(t, func() {
		s.publishDashboardDocUpdate(context.Background(), uuid.NewString(), []byte{0x00, 0x01})
		s.publishDashboardDocInvalidate(context.Background(), uuid.NewString(), "purged")
	})
}
