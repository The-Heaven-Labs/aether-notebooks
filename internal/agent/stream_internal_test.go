package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// internalTestRedisClient returns a Redis client for tests that need the
// unexported stream internals, skipping when Redis is not configured.
func internalTestRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("AETHER_REDIS_URL")
	if url == "" {
		t.Skip("AETHER_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	require.NoError(t, err)
	rdb := redis.NewClient(opt)
	require.NoError(t, rdb.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// internalTestSession returns a unique session id and removes its stream keys
// from the shared Redis when the test finishes.
func internalTestSession(t *testing.T, rdb *redis.Client) string {
	t.Helper()
	sessionID := "itest-" + uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = rdb.Del(ctx, sessionStreamSeqKey(sessionID), sessionStreamBufferKey(sessionID)).Err()
	})
	return sessionID
}

// setCleanupGrace shrinks the manager's cleanup grace period for the test and
// restores it afterwards.
func setCleanupGrace(t *testing.T, sm *StreamManager, d time.Duration) {
	t.Helper()
	old := sm.cleanupGrace
	sm.cleanupGrace = d
	t.Cleanup(func() { sm.cleanupGrace = old })
}

// nextInternalStreamEvent waits for one event on a package-internal
// subscription channel.
func nextInternalStreamEvent(t *testing.T, ch <-chan SequencedEvent) SequencedEvent {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a stream event")
		return SequencedEvent{}
	}
}

// waitStreamSubscriberReady waits until the session's pump has subscribed to
// Redis and completed catch-up for its first subscriber, so tests can observe
// the subscriber baseline deterministically instead of sleeping.
func waitStreamSubscriberReady(t *testing.T, sm *StreamManager, rdb *redis.Client, sessionID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		n, err := rdb.PubSubNumSub(context.Background(), sessionStreamChannel(sessionID)).Result()
		if err != nil || n[sessionStreamChannel(sessionID)] != 1 {
			return false
		}
		sm.mu.RLock()
		stream := sm.streams[sessionID]
		sm.mu.RUnlock()
		if stream == nil {
			return false
		}
		stream.mu.RLock()
		defer stream.mu.RUnlock()
		return len(stream.subscribers) == 1 && stream.subscribers[0].ready
	}, 3*time.Second, 5*time.Millisecond, "pump never completed the subscriber's catch-up")
}

// Repeated unsubscribe -> immediate re-subscribe must never leave the new
// subscriber attached to a stream whose pump the grace timer already cancelled.
func TestUnsubscribeImmediateResubscribeKeepsStreamAlive(t *testing.T) {
	sm := NewStreamManager(nil)
	setCleanupGrace(t, sm, 2*time.Millisecond)
	sessionID := "resub-" + uuid.NewString()

	for i := 0; i < 50; i++ {
		_, unsub := sm.Subscribe(sessionID, 8, true)
		unsub()
		// Leave the grace timer free to fire around the next Subscribe.
		time.Sleep(time.Millisecond)
		sub, unsub2 := sm.Subscribe(sessionID, 8, true)
		sm.Publish(sessionID, map[string]any{"type": "token"})
		select {
		case evt := <-sub:
			require.NotZero(t, evt.Seq)
		case <-time.After(2 * time.Second):
			t.Fatalf("iteration %d: subscriber received no event (stream lost to cleanup)", i)
		}
		unsub2()
	}
}

// When a resuming subscriber's replay buffer was already trimmed past the
// events it missed, catch-up must emit a resync marker before replaying the
// surviving entries instead of presenting them as if nothing were missing.
func TestStreamCatchUpTrimmedBufferEmitsResync(t *testing.T) {
	rdb := internalTestRedisClient(t)
	sm := NewStreamManager(rdb)
	sessionID := internalTestSession(t, rdb)
	ctx := context.Background()

	for i := 0; i < 15; i++ {
		sm.Publish(sessionID, map[string]any{"type": "token", "n": i})
	}
	require.NoError(t, rdb.LTrim(ctx, sessionStreamBufferKey(sessionID), -5, -1).Err())

	stream := sm.newSessionStream()
	sub := &streamSubscriber{ch: make(chan SequencedEvent, 16), ready: true, lastSeq: 1}
	stream.subscribers = append(stream.subscribers, sub)

	sm.catchUpSessionStream(ctx, sessionID, stream)

	marker := <-sub.ch
	msg, ok := marker.Msg.(map[string]any)
	require.True(t, ok, "marker decoded as %T", marker.Msg)
	require.Equal(t, resyncMarkerType, msg["type"])
	require.Equal(t, uint64(2), marker.Seq)

	first := <-sub.ch
	require.Equal(t, uint64(11), first.Seq)
}

// Unsubscribing the last viewer must stop the pump: no leaked Redis
// subscription and no lingering stream once the grace period elapses.
func TestStreamUnsubscribeStopsPump(t *testing.T) {
	rdb := internalTestRedisClient(t)
	sm := NewStreamManager(rdb)
	setCleanupGrace(t, sm, 20*time.Millisecond)
	sessionID := internalTestSession(t, rdb)
	channel := sessionStreamChannel(sessionID)

	_, unsub := sm.Subscribe(sessionID, 8, true)
	require.Eventually(t, func() bool {
		n, err := rdb.PubSubNumSub(context.Background(), channel).Result()
		return err == nil && n[channel] == 1
	}, 3*time.Second, 10*time.Millisecond, "pump never subscribed")

	unsub()

	require.Eventually(t, func() bool {
		n, err := rdb.PubSubNumSub(context.Background(), channel).Result()
		return err == nil && n[channel] == 0
	}, 3*time.Second, 10*time.Millisecond, "pump still subscribed after unsubscribe")

	sm.mu.RLock()
	_, ok := sm.streams[sessionID]
	sm.mu.RUnlock()
	require.False(t, ok, "stream still registered after cleanup")
}

// A counter reset re-bases a subscriber's seqs through sub.offset. Catch-up
// must apply the same offset to replayed events; otherwise missed source seqs
// compare below the subscriber's lastSeq and are silently skipped.
func TestStreamCatchUpReplaysAgainstSubscriberOffset(t *testing.T) {
	rdb := internalTestRedisClient(t)
	a := NewStreamManager(rdb)
	b := NewStreamManager(rdb)
	sessionID := internalTestSession(t, rdb)
	ctx := context.Background()

	sub, unsub := b.Subscribe(sessionID, 16, false)
	defer unsub()

	for i := 0; i < 3; i++ {
		a.Publish(sessionID, map[string]any{"type": "token", "n": i})
	}
	for i := 0; i < 3; i++ {
		require.Equal(t, uint64(i+1), nextInternalStreamEvent(t, sub).Seq)
	}

	// Reset the shared clock and buffer under the connected subscriber. The
	// next event arrives at source seq 1, so the subscriber first gets a resync
	// marker (client seq 4) and the event re-based to seq 5 (offset 4).
	require.NoError(t, rdb.Del(ctx, sessionStreamSeqKey(sessionID), sessionStreamBufferKey(sessionID)).Err())
	a.Publish(sessionID, map[string]any{"type": "token", "n": 99})
	marker := nextInternalStreamEvent(t, sub)
	require.Equal(t, uint64(4), marker.Seq)
	require.Equal(t, resyncMarkerType, marker.Msg.(map[string]any)["type"])
	recovered := nextInternalStreamEvent(t, sub)
	require.Equal(t, uint64(5), recovered.Seq)
	require.Equal(t, float64(99), recovered.Msg.(map[string]any)["n"])

	// Stop the pump so the next two publishes are not delivered live, then
	// invoke catch-up directly (as a reconnect would): the missed events must
	// be delivered re-based, not skipped.
	b.mu.Lock()
	stream := b.streams[sessionID]
	b.mu.Unlock()
	require.NotNil(t, stream)
	stream.mu.Lock()
	cancel := stream.pumpCancel
	stream.mu.Unlock()
	require.NotNil(t, cancel)
	cancel()
	require.Eventually(t, func() bool {
		n, err := rdb.PubSubNumSub(context.Background(), sessionStreamChannel(sessionID)).Result()
		return err == nil && n[sessionStreamChannel(sessionID)] == 0
	}, 3*time.Second, 10*time.Millisecond, "pump never unsubscribed")

	a.Publish(sessionID, map[string]any{"type": "token", "n": 100})
	a.Publish(sessionID, map[string]any{"type": "token", "n": 101})

	b.catchUpSessionStream(ctx, sessionID, stream)

	first := nextInternalStreamEvent(t, sub)
	require.Equal(t, uint64(6), first.Seq)
	require.Equal(t, float64(100), first.Msg.(map[string]any)["n"])
	second := nextInternalStreamEvent(t, sub)
	require.Equal(t, uint64(7), second.Seq)
	require.Equal(t, float64(101), second.Msg.(map[string]any)["n"])
}

// skipBuffer subscribers only get live events: buffered events published before
// the subscription are not replayed. Readiness is observed through Redis
// (PUBSUB NUMSUB) and the subscriber's internal baseline, not a fixed sleep.
func TestStreamRedisSkipBufferLiveOnly(t *testing.T) {
	rdb := internalTestRedisClient(t)
	a := NewStreamManager(rdb)
	b := NewStreamManager(rdb)
	sessionID := internalTestSession(t, rdb)

	a.Publish(sessionID, map[string]any{"type": "token", "n": 0})

	sub, unsub := b.Subscribe(sessionID, 16, true)
	defer unsub()
	waitStreamSubscriberReady(t, b, rdb, sessionID)

	select {
	case evt := <-sub:
		t.Fatalf("skipBuffer subscriber got replayed seq %d", evt.Seq)
	default:
	}

	a.Publish(sessionID, map[string]any{"type": "token", "n": 1})
	evt := nextInternalStreamEvent(t, sub)
	require.Equal(t, uint64(2), evt.Seq)
}
