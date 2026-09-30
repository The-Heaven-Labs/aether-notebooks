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

// setCleanupGrace shrinks the cleanup grace period for the test and restores it
// afterwards.
func setCleanupGrace(t *testing.T, d time.Duration) {
	t.Helper()
	old := sessionStreamCleanupGrace
	sessionStreamCleanupGrace = d
	t.Cleanup(func() { sessionStreamCleanupGrace = old })
}

// Repeated unsubscribe -> immediate re-subscribe must never leave the new
// subscriber attached to a stream whose pump the grace timer already cancelled.
func TestUnsubscribeImmediateResubscribeKeepsStreamAlive(t *testing.T) {
	setCleanupGrace(t, 2*time.Millisecond)
	sm := NewStreamManager(nil)
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
	setCleanupGrace(t, 20*time.Millisecond)
	rdb := internalTestRedisClient(t)
	sm := NewStreamManager(rdb)
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
