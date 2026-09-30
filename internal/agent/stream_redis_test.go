package agent_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
)

// redisStreamURL returns the Redis URL for stream tests, skipping when the
// environment does not configure one (unit-only runs, no Redis available).
func redisStreamURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("AETHER_REDIS_URL")
	if url == "" {
		t.Skip("AETHER_REDIS_URL not set")
	}
	return url
}

// newStreamRedisManager returns a StreamManager backed by its own Redis client
// (one client per manager so the tests model separate replicas) plus the
// client for direct key assertions.
func newStreamRedisManager(t *testing.T) (*agent.StreamManager, *redis.Client) {
	t.Helper()
	opt, err := redis.ParseURL(redisStreamURL(t))
	require.NoError(t, err)
	rdb := redis.NewClient(opt)
	require.NoError(t, rdb.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = rdb.Close() })
	return agent.NewStreamManager(rdb), rdb
}

// newStreamRedisSession returns a unique session id and removes its stream
// keys from the shared Redis when the test finishes.
func newStreamRedisSession(t *testing.T, rdb *redis.Client) string {
	t.Helper()
	sessionID := "test-" + uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = rdb.Del(ctx, sessionStreamSeqKey(sessionID), sessionStreamBufKey(sessionID)).Err()
	})
	return sessionID
}

func sessionStreamSeqKey(sessionID string) string { return "aether:agent:sess:" + sessionID + ":seq" }
func sessionStreamBufKey(sessionID string) string { return "aether:agent:sess:" + sessionID + ":buf" }

func nextStreamEvent(t *testing.T, ch <-chan agent.SequencedEvent) agent.SequencedEvent {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a stream event")
		return agent.SequencedEvent{}
	}
}

// An event published on one manager is delivered to a subscriber on another
// manager through the shared Redis channel, with monotonically increasing seq.
func TestStreamRedisCrossManagerFanout(t *testing.T) {
	a, _ := newStreamRedisManager(t)
	b, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	sub, unsub := b.Subscribe(sessionID, 16, false)
	defer unsub()

	for i := 0; i < 3; i++ {
		a.Publish(sessionID, map[string]any{"type": "token", "n": i})
	}

	var seqs []uint64
	for i := 0; i < 3; i++ {
		evt := nextStreamEvent(t, sub)
		msg, ok := evt.Msg.(map[string]any)
		require.True(t, ok, "event message decoded as %T", evt.Msg)
		require.Equal(t, "token", msg["type"])
		seqs = append(seqs, evt.Seq)
	}
	require.Equal(t, []uint64{1, 2, 3}, seqs)
}

// The seq counter is shared: publishing on A then B advances the same counter.
func TestStreamRedisSeqMonotonicAcrossManagers(t *testing.T) {
	a, _ := newStreamRedisManager(t)
	b, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	a.Publish(sessionID, map[string]any{"type": "token"})
	first := a.LastSeq(sessionID)
	b.Publish(sessionID, map[string]any{"type": "token"})
	second := b.LastSeq(sessionID)

	require.Equal(t, first+1, second)
}

// A subscriber that joins after events were published replays the Redis buffer
// with the original sequence numbers.
func TestStreamRedisLateSubscriberReplaysBuffer(t *testing.T) {
	a, _ := newStreamRedisManager(t)
	b, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	for i := 0; i < 3; i++ {
		a.Publish(sessionID, map[string]any{"type": "token", "n": i})
	}

	sub, unsub := b.Subscribe(sessionID, 16, false)
	defer unsub()

	var seqs []uint64
	for i := 0; i < 3; i++ {
		seqs = append(seqs, nextStreamEvent(t, sub).Seq)
	}
	require.Equal(t, []uint64{1, 2, 3}, seqs)
}

// LastSeq is global: manager B reports the seq of an event published on A.
func TestStreamRedisLastSeqMatchesRemotePublisher(t *testing.T) {
	a, _ := newStreamRedisManager(t)
	b, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	a.Publish(sessionID, map[string]any{"type": "token"})

	require.Equal(t, uint64(1), a.LastSeq(sessionID))
	require.Equal(t, a.LastSeq(sessionID), b.LastSeq(sessionID))
}

// Publishing with no local subscriber still increments the shared seq and
// stores the event in the Redis replay buffer.
func TestStreamRedisPublishWithoutSubscriberStoresBuffer(t *testing.T) {
	a, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	a.Publish(sessionID, map[string]any{"type": "token", "n": 1})
	a.Publish(sessionID, map[string]any{"type": "token", "n": 2})

	require.Equal(t, uint64(2), a.LastSeq(sessionID))

	entries, err := rdb.LRange(context.Background(), sessionStreamBufKey(sessionID), 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Contains(t, entries[0], `"seq":1`)
	require.Contains(t, entries[0], `"type":"token"`)
	require.Contains(t, entries[1], `"seq":2`)
}

// A live event that skips sequence numbers (an event lost between replicas)
// emits the resync marker instead of silently diverging.
func TestStreamRedisGapEmitsResyncMarker(t *testing.T) {
	a, rdb := newStreamRedisManager(t)
	b, _ := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	sub, unsub := b.Subscribe(sessionID, 16, false)
	defer unsub()

	a.Publish(sessionID, map[string]any{"type": "token"})
	first := nextStreamEvent(t, sub)
	require.Equal(t, uint64(1), first.Seq)

	// Simulate a lost event: advance the shared counter without publishing.
	require.NoError(t, rdb.Incr(context.Background(), sessionStreamSeqKey(sessionID)).Err())
	a.Publish(sessionID, map[string]any{"type": "token"})

	evt := nextStreamEvent(t, sub)
	msg, ok := evt.Msg.(map[string]any)
	require.True(t, ok, "event message decoded as %T", evt.Msg)
	require.Equal(t, "resync", msg["type"])
	require.Equal(t, uint64(3), evt.Seq)
}

// Overflow on a slow subscriber evicts queued events and marks the drop with a
// monotonic resync event, like the in-memory path.
func TestStreamRedisSlowSubscriberEmitsResyncMarker(t *testing.T) {
	a, rdb := newStreamRedisManager(t)
	b, _ := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	sub, unsub := b.Subscribe(sessionID, 1, false)
	defer unsub()

	for i := 0; i < 5; i++ {
		a.Publish(sessionID, map[string]any{"type": "token", "n": i})
	}
	require.Eventually(t, func() bool {
		return b.DropCount(sessionID) > 0
	}, 3*time.Second, 10*time.Millisecond)

	evt := nextStreamEvent(t, sub)
	msg, ok := evt.Msg.(map[string]any)
	require.True(t, ok, "event message decoded as %T", evt.Msg)
	require.Equal(t, "resync", msg["type"])
}
