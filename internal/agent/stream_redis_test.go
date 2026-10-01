package agent_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
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

// The shared seq counter must not expire under connected clients: a reset makes
// the server appear to move backwards. The replay buffer keeps its TTL.
func TestStreamRedisSeqKeyHasNoTTL(t *testing.T) {
	a, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)
	ctx := context.Background()

	a.Publish(sessionID, map[string]any{"type": "token"})

	seqTTL, err := rdb.TTL(ctx, sessionStreamSeqKey(sessionID)).Result()
	require.NoError(t, err)
	require.Equal(t, time.Duration(-1), seqTTL, "seq key must not expire")

	bufTTL, err := rdb.TTL(ctx, sessionStreamBufKey(sessionID)).Result()
	require.NoError(t, err)
	require.Greater(t, bufTTL, time.Duration(0), "buffer key keeps its TTL")
}

// The replay buffer is capped: only the newest 500 entries survive.
func TestStreamRedisBufferTrimmedAt500(t *testing.T) {
	a, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	const cap = 500
	for i := 0; i < cap+20; i++ {
		a.Publish(sessionID, map[string]any{"type": "token", "n": i})
	}

	entries, err := rdb.LRange(context.Background(), sessionStreamBufKey(sessionID), 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, entries, cap)
	require.Contains(t, entries[0], `"seq":21`)
	require.Contains(t, entries[cap-1], `"seq":520`)
}

// A catastrophic counter reset (flush/failover) while a subscriber is connected
// must never move the delivered seq backwards: the subscriber first gets a
// resync marker, then the recovered event re-based onto the old clock.
func TestStreamRedisCounterResetResyncsSubscriber(t *testing.T) {
	a, rdb := newStreamRedisManager(t)
	b, _ := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	sub, unsub := b.Subscribe(sessionID, 16, false)
	defer unsub()

	for i := 0; i < 3; i++ {
		a.Publish(sessionID, map[string]any{"type": "token", "n": i})
	}
	var seqs []uint64
	for i := 0; i < 3; i++ {
		seqs = append(seqs, nextStreamEvent(t, sub).Seq)
	}
	require.Equal(t, []uint64{1, 2, 3}, seqs)

	require.NoError(t, rdb.Del(context.Background(),
		sessionStreamSeqKey(sessionID), sessionStreamBufKey(sessionID)).Err())

	a.Publish(sessionID, map[string]any{"type": "token", "n": 99})

	marker := nextStreamEvent(t, sub)
	msg, ok := marker.Msg.(map[string]any)
	require.True(t, ok, "marker decoded as %T", marker.Msg)
	require.Equal(t, "resync", msg["type"])
	require.Equal(t, uint64(4), marker.Seq, "marker must be the next client-visible seq")

	evt := nextStreamEvent(t, sub)
	require.Equal(t, uint64(5), evt.Seq, "recovered event must stay above the marker")
	msg, ok = evt.Msg.(map[string]any)
	require.True(t, ok, "event decoded as %T", evt.Msg)
	require.Equal(t, "token", msg["type"])
	require.Equal(t, float64(99), msg["n"])
}

// publishFailHook fails EVAL/EVALSHA commands while enabled, simulating a Redis
// outage for publishes without breaking the pump's pub/sub connection.
type publishFailHook struct{ fail atomic.Bool }

func (h *publishFailHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *publishFailHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.fail.Load() {
			switch cmd.Name() {
			case "eval", "evalsha":
				return errors.New("simulated redis publish failure")
			}
		}
		return next(ctx, cmd)
	}
}

func (h *publishFailHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// newStreamRedisManagerWithHook is newStreamRedisManager plus a pluggable hook.
func newStreamRedisManagerWithHook(t *testing.T, hook redis.Hook) (*agent.StreamManager, *redis.Client) {
	t.Helper()
	opt, err := redis.ParseURL(redisStreamURL(t))
	require.NoError(t, err)
	rdb := redis.NewClient(opt)
	rdb.AddHook(hook)
	require.NoError(t, rdb.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = rdb.Close() })
	return agent.NewStreamManager(rdb), rdb
}

// After a fallback publish (Redis down), the next successful publish reuses the
// fallback seq, so local subscribers would drop the recovery event as a
// duplicate and remote subscribers would never learn an event was lost. The
// recovery must force a resync marker for both, with the client-visible seqs
// strictly increasing and the marker above the fallback seq.
func TestStreamRedisFallbackRecoveryForcesResync(t *testing.T) {
	hook := &publishFailHook{}
	a, _ := newStreamRedisManagerWithHook(t, hook)
	b, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	subA, unsubA := a.Subscribe(sessionID, 32, false)
	defer unsubA()
	subB, unsubB := b.Subscribe(sessionID, 32, false)
	defer unsubB()

	a.Publish(sessionID, map[string]any{"type": "token", "n": 0})
	require.Equal(t, uint64(1), nextStreamEvent(t, subA).Seq)
	require.Equal(t, uint64(1), nextStreamEvent(t, subB).Seq)

	// Redis publish fails: local fan-out only, remote replicas see nothing.
	hook.fail.Store(true)
	a.Publish(sessionID, map[string]any{"type": "token", "n": 1})
	fallback := nextStreamEvent(t, subA)
	require.Equal(t, uint64(2), fallback.Seq)

	// Redis recovers: the next publish must resync every subscriber.
	hook.fail.Store(false)
	a.Publish(sessionID, map[string]any{"type": "token", "n": 2})

	// collectUntilResync drains events in delivery order and stops at the
	// first resync marker.
	collectUntilResync := func(t *testing.T, ch <-chan agent.SequencedEvent) []agent.SequencedEvent {
		t.Helper()
		var events []agent.SequencedEvent
		deadline := time.After(3 * time.Second)
		for {
			select {
			case evt := <-ch:
				events = append(events, evt)
				if msg, ok := evt.Msg.(map[string]any); ok && msg["type"] == "resync" {
					return events
				}
			case <-deadline:
				t.Fatalf("timed out waiting for a resync marker, got %d events", len(events))
				return nil
			}
		}
	}

	// clientSeqs applies the viewer's dedup rule (`seq <= lastSeq` dropped,
	// AgentPanel.tsx) and returns the strictly increasing client-visible seqs.
	// The local fallback path may re-deliver the fallback seq once (publishLocal
	// pushes without touching lastSeq), which the client drops as a duplicate.
	clientSeqs := func(t *testing.T, last uint64, events []agent.SequencedEvent) []uint64 {
		t.Helper()
		var seqs []uint64
		for _, evt := range events {
			require.GreaterOrEqual(t, evt.Seq, last, "delivered seq moved backwards: %v", events)
			if evt.Seq == last {
				continue
			}
			seqs = append(seqs, evt.Seq)
			last = evt.Seq
		}
		return seqs
	}

	// The local subscriber already saw the fallback event: recovery must never
	// move its seq backwards, must end in a marker above the fallback seq.
	local := collectUntilResync(t, subA)
	localClient := clientSeqs(t, fallback.Seq, local)
	require.NotEmpty(t, localClient)
	for i := 1; i < len(localClient); i++ {
		require.Greater(t, localClient[i], localClient[i-1], "local seqs not strictly increasing")
	}
	require.Equal(t, "resync", local[len(local)-1].Msg.(map[string]any)["type"], "marker must arrive last")
	require.Greater(t, local[len(local)-1].Seq, fallback.Seq, "marker seq must exceed the fallback seq")

	// The remote subscriber missed the fallback event entirely and must be
	// told to reconcile: it sees the recovery event and then the marker, never
	// the fallback event itself.
	remote := collectUntilResync(t, subB)
	for _, evt := range remote {
		msg, ok := evt.Msg.(map[string]any)
		require.True(t, ok, "event decoded as %T", evt.Msg)
		if msg["type"] == "resync" {
			continue
		}
		require.NotEqual(t, float64(1), msg["n"], "remote subscriber must not see the fallback event")
	}
	remoteClient := clientSeqs(t, 1, remote)
	require.NotEmpty(t, remoteClient)
	for i := 1; i < len(remoteClient); i++ {
		require.Greater(t, remoteClient[i], remoteClient[i-1], "remote seqs not strictly increasing")
	}
	require.Equal(t, "resync", remote[len(remote)-1].Msg.(map[string]any)["type"], "marker must arrive last")
	require.Greater(t, remote[len(remote)-1].Seq, fallback.Seq, "marker seq must exceed the fallback seq")
}

// A fallback must be advertised even when the failing pod publishes no further
// events: the background retrier republishes the marker through Redis while
// another replica advances the stream.
func TestStreamRedisFallbackRetryResyncsWithoutLocalPublish(t *testing.T) {
	hook := &publishFailHook{}
	a, _ := newStreamRedisManagerWithHook(t, hook)
	b, rdb := newStreamRedisManager(t)
	sessionID := newStreamRedisSession(t, rdb)

	subA, unsubA := a.Subscribe(sessionID, 32, false)
	defer unsubA()
	subB, unsubB := b.Subscribe(sessionID, 32, false)
	defer unsubB()

	a.Publish(sessionID, map[string]any{"type": "token", "n": 0})
	require.Equal(t, uint64(1), nextStreamEvent(t, subA).Seq)
	require.Equal(t, uint64(1), nextStreamEvent(t, subB).Seq)

	// A's publish fails and falls back locally; A never publishes again.
	hook.fail.Store(true)
	a.Publish(sessionID, map[string]any{"type": "token", "n": 1})
	fallback := nextStreamEvent(t, subA)
	require.Equal(t, uint64(2), fallback.Seq)
	hook.fail.Store(false)

	// Another replica advances the stream; the retrier on A must still publish
	// the recovery marker so B's viewers reconcile.
	b.Publish(sessionID, map[string]any{"type": "token", "n": 2})

	var seqs []uint64
	var sawResync bool
	deadline := time.After(3 * time.Second)
	for !sawResync {
		select {
		case evt := <-subB:
			seqs = append(seqs, evt.Seq)
			msg, ok := evt.Msg.(map[string]any)
			require.True(t, ok, "event decoded as %T", evt.Msg)
			if msg["type"] == "resync" {
				sawResync = true
			} else {
				require.NotEqual(t, float64(1), msg["n"], "B must never see A's fallback event")
			}
		case <-deadline:
			t.Fatalf("B never received the fallback recovery marker, got seqs %v", seqs)
		}
	}
	require.True(t, sawResync)
	for i := 1; i < len(seqs); i++ {
		require.Greater(t, seqs[i], seqs[i-1], "B seqs not strictly increasing: %v", seqs)
	}
	require.Greater(t, seqs[len(seqs)-1], fallback.Seq, "marker seq must exceed the fallback seq")
}
