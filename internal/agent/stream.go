package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// SequencedEvent wraps a session stream message with a monotonically increasing
// sequence number (per session). Clients use seq to drop replayed duplicates and
// to reconcile against reconnect_sync's server_seq after a reconnect.
type SequencedEvent struct {
	Seq uint64
	Msg any
}

// resyncMarkerType is the wire event type published when the stream had to drop
// an event for a slow subscriber, detected a gap in the shared counter, or
// detected that the shared counter moved backwards (Redis flush/failover/expiry).
// Clients react by sending a `reconnect` message to reconcile against the
// authoritative DB state. A dropped event is never silently forgotten.
const resyncMarkerType = "resync"

const (
	// sessionStreamBufferTTL is how long a session's Redis replay buffer
	// survives after the last publish.
	sessionStreamBufferTTL = 2 * time.Hour
	// sessionStreamBufferMax caps the Redis replay buffer.
	sessionStreamBufferMax = 500
	// sessionStreamRedisTimeout bounds every individual Redis call so a dead
	// Redis cannot block Publish, Subscribe, or LastSeq indefinitely.
	sessionStreamRedisTimeout = time.Second
	// sessionStreamPumpReconnectDelay is the initial pause before a session
	// pump retries after a failed subscribe or a closed pub/sub channel.
	sessionStreamPumpReconnectDelay = time.Second
	// sessionStreamPumpReconnectMax caps the pump's reconnect backoff so a
	// long Redis outage does not produce a per-session reconnect/log storm.
	sessionStreamPumpReconnectMax = 30 * time.Second
	// defaultSessionStreamCleanupGrace is how long a stream with no subscribers
	// is kept alive so a reconnecting WebSocket (page navigation) can pick up
	// the in-flight buffer before the pump is stopped. StreamManager.cleanupGrace
	// is the effective value.
	defaultSessionStreamCleanupGrace = 5 * time.Second
	// sessionStreamFallbackRetryInitial is the pause before the first retry of
	// a fallback recovery marker.
	sessionStreamFallbackRetryInitial = 250 * time.Millisecond
	// sessionStreamFallbackRetryMax caps the fallback retry backoff.
	sessionStreamFallbackRetryMax = 5 * time.Second
	// sessionStreamFallbackRetryLifetime bounds how long a fallback recovery
	// marker keeps being retried when Redis stays unavailable.
	sessionStreamFallbackRetryLifetime = 5 * time.Minute
)

// sessionStreamPublishScript atomically assigns the next seq, appends the entry
// to the replay buffer, trims the buffer, refreshes the buffer TTL, and
// publishes the entry to every replica. KEYS: seq, buffer, channel. ARGV: JSON
// message, buffer TTL seconds. The entry embeds its seq so subscribers can dedup
// and detect gaps.
//
// The seq key deliberately gets no TTL: it is the shared clock that every
// connected client's dedup depends on, and expiring it resets the counter
// backwards under clients that are still connected. Only the replay buffer is
// allowed to expire; a seq key lost any other way (flush, failover) is covered
// by the reset guard in deliverLocked. The trade-off is one small seq key per
// session for the Redis lifetime unless the session is deleted through
// SessionStore.DeleteSession, which removes both keys.
var sessionStreamPublishScript = redis.NewScript(fmt.Sprintf(`
local seq = redis.call('INCR', KEYS[1])
local entry = '{"seq":' .. seq .. ',"msg":' .. ARGV[1] .. '}'
redis.call('RPUSH', KEYS[2], entry)
redis.call('LTRIM', KEYS[2], -%d, -1)
redis.call('EXPIRE', KEYS[2], ARGV[2])
redis.call('PUBLISH', KEYS[3], entry)
return seq
`, sessionStreamBufferMax))

// sessionStreamResyncScript appends and publishes a resync marker whose seq is
// strictly greater than ARGV[2] (the highest seq a local fallback delivered), so
// clients that already saw fallback seqs still accept the marker. KEYS: seq,
// buffer, channel. ARGV: buffer TTL seconds, minimum seq.
var sessionStreamResyncScript = redis.NewScript(fmt.Sprintf(`
local floor = tonumber(ARGV[2])
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
if current < floor then
  redis.call('SET', KEYS[1], floor)
end
local seq = redis.call('INCR', KEYS[1])
local entry = '{"seq":' .. seq .. ',"msg":{"type":"resync"}}'
redis.call('RPUSH', KEYS[2], entry)
redis.call('LTRIM', KEYS[2], -%d, -1)
redis.call('EXPIRE', KEYS[2], ARGV[1])
redis.call('PUBLISH', KEYS[3], entry)
return seq
`, sessionStreamBufferMax))

// StreamManager fans session stream events out to local subscribers and, when
// backed by Redis, across replicas through a shared seq counter and replay
// buffer.
type StreamManager struct {
	rdb     *redis.Client
	mu      sync.RWMutex
	streams map[string]*SessionStream

	// cleanupGrace is how long a stream with no subscribers is kept alive so a
	// reconnecting WebSocket can pick up the in-flight buffer before the pump
	// stops. A field (rather than a package var) so tests can shrink it without
	// racing other tests; it is only read when arming a cleanup timer.
	cleanupGrace time.Duration

	// fallbackMu guards fellBack, which tracks sessions whose last Redis
	// publish failed and fell back to local fan-out. A background retrier
	// publishes a resync marker through Redis for each entry, independent of
	// whether this pod publishes again; the marker is deleted once it commits.
	fallbackMu sync.Mutex
	fellBack   map[string]*fallbackMarker
}

// fallbackMarker is the pending recovery state for one session: watermark is
// the highest seq the local fallback delivered (0 when no local subscriber
// received it), and retrying reports whether a background goroutine owns
// retrying the marker through Redis.
type fallbackMarker struct {
	watermark uint64
	retrying  bool
}

// streamSubscriber is one local viewer's delivery state. ready, lastSeq and
// offset are written only under the owning SessionStream's write lock, from the
// stream's single pump goroutine; the local fallback fan-out only pushes to ch
// without touching them.
type streamSubscriber struct {
	ch         chan SequencedEvent
	skipBuffer bool
	ready      bool
	lastSeq    uint64
	// offset re-bases source seqs onto the client-visible clock after the
	// shared Redis counter resets; deliveredSeq = sourceSeq + offset.
	offset uint64
}

// SessionStream is one session's local subscriber set plus, without Redis, its
// replay buffer and process-local seq counter. A stream has at most one live
// pump goroutine: pumpCancel is only set under mu when a pump starts, and only
// cleared (with dead set) by the grace-cleanup callback before it cancels the
// pump; Subscribe never attaches to a stream marked dead. Lock order is
// StreamManager.mu before SessionStream.mu.
type SessionStream struct {
	mu          sync.RWMutex
	subscribers []*streamSubscriber
	buffer      []SequencedEvent
	bufferMu    sync.RWMutex
	cleanup     *time.Timer
	nextSeq     atomic.Uint64
	dropped     atomic.Uint64
	wake        chan struct{}
	pumpCancel  context.CancelFunc
	// dead is set by the grace-cleanup callback under mu when the grace period
	// expires with no subscribers; Subscribe retries with a fresh stream.
	dead bool
}

// NewStreamManager returns a stream manager. With a nil Redis client it uses
// the in-process fan-out (unit tests, single-node installs); with a client it
// fans out through Redis pub/sub so viewers on every replica see the session.
func NewStreamManager(rdb *redis.Client) *StreamManager {
	return &StreamManager{
		rdb:          rdb,
		streams:      make(map[string]*SessionStream),
		fellBack:     make(map[string]*fallbackMarker),
		cleanupGrace: defaultSessionStreamCleanupGrace,
	}
}

// Only SessionStore.DeleteSession reclaims :seq and :buf (today: /new on an
// empty session); the creation-time sweep and cascade deletes (agent/user/
// notebook removal, trash purge) leave them behind, and :seq is non-expiring
// by design (see sessionStreamPublishScript).
func sessionStreamSeqKey(sessionID string) string {
	return "aether:agent:sess:" + sessionID + ":seq"
}

func sessionStreamBufferKey(sessionID string) string {
	return "aether:agent:sess:" + sessionID + ":buf"
}

func sessionStreamChannel(sessionID string) string {
	return "aether:agent:sess:" + sessionID
}

func (sm *StreamManager) newSessionStream() *SessionStream {
	stream := &SessionStream{subscribers: make([]*streamSubscriber, 0, 4)}
	if sm.rdb != nil {
		stream.wake = make(chan struct{}, 1)
	}
	return stream
}

// isDead reports whether the grace-cleanup callback already retired the stream.
// Callers must hold StreamManager.mu when calling it so a dead map entry cannot
// slip between the check and its replacement.
func (s *SessionStream) isDead() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dead
}

// Subscribe creates (or joins) a session's event stream. When skipBuffer is
// true the returned channel only receives live events published after the
// subscription; when false it first receives the recent events for catch-up
// followed by live events.
// The caller must call the returned unsubscribe func when done.
func (sm *StreamManager) Subscribe(sessionID string, bufSize int, skipBuffer bool) (chan SequencedEvent, func()) {
	ch := make(chan SequencedEvent, bufSize)
	sub := &streamSubscriber{ch: ch, skipBuffer: skipBuffer}

	var stream *SessionStream
	var pumpCtx context.Context
	for {
		sm.mu.Lock()
		s, ok := sm.streams[sessionID]
		if !ok || s.isDead() {
			s = sm.newSessionStream()
			sm.streams[sessionID] = s
		} else if s.cleanup != nil {
			// A cleanup timer was scheduled (last subscriber left). Cancel it
			// because we're re-joining the stream; a timer that already fired
			// is detected via s.dead below.
			s.cleanup.Stop()
			s.cleanup = nil
		}
		sm.mu.Unlock()

		s.mu.Lock()
		if s.dead {
			// The cleanup callback marked the stream dead between our map
			// read and this lock. Retry: the next iteration replaces the dead
			// map entry with a fresh stream.
			s.mu.Unlock()
			continue
		}
		if sm.rdb == nil && !skipBuffer {
			// In-memory fan-out: replay the local buffer before registering
			// the subscriber so catch-up events cannot be interleaved with
			// live ones.
			s.bufferMu.RLock()
			for _, evt := range s.buffer {
				select {
				case ch <- evt:
				default:
				}
			}
			s.bufferMu.RUnlock()
		}
		s.subscribers = append(s.subscribers, sub)
		if sm.rdb != nil && s.pumpCancel == nil {
			var cancel context.CancelFunc
			pumpCtx, cancel = context.WithCancel(context.Background())
			s.pumpCancel = cancel
		}
		s.mu.Unlock()
		stream = s
		break
	}

	if sm.rdb != nil {
		if pumpCtx != nil {
			go sm.runSessionPump(pumpCtx, sessionID, stream)
		}
		// The pump performs the subscriber's buffer catch-up: wake it in case
		// it is parked in the live select.
		select {
		case stream.wake <- struct{}{}:
		default:
		}
	}

	return ch, sm.unsubscribeFunc(sessionID, stream, sub)
}

// unsubscribeFunc removes the subscriber and, when it was the last one,
// schedules the stream's grace-period cleanup.
func (sm *StreamManager) unsubscribeFunc(sessionID string, stream *SessionStream, sub *streamSubscriber) func() {
	return func() {
		stream.mu.Lock()
		for i, s := range stream.subscribers {
			if s == sub {
				stream.subscribers = append(stream.subscribers[:i], stream.subscribers[i+1:]...)
				close(sub.ch)
				break
			}
		}
		empty := len(stream.subscribers) == 0 && !stream.dead
		stream.mu.Unlock()

		if !empty {
			return
		}
		sm.mu.Lock()
		if stream.cleanup == nil {
			stream.cleanup = time.AfterFunc(sm.cleanupGrace, func() {
				sm.cleanupSessionStream(sessionID, stream)
			})
		}
		sm.mu.Unlock()
	}
}

// cleanupSessionStream is the grace timer's callback. When the stream still has
// no subscribers it marks the stream dead under stream.mu before cancelling the
// pump, so a Subscribe racing the timer detects the dead stream and starts a
// fresh one instead of attaching to a pump-less stream (or spawning a second
// pump for the same subscribers). A subscriber that re-joined during the grace
// window keeps the stream, and the fired timer is cleared so a later
// unsubscribe arms a fresh one.
func (sm *StreamManager) cleanupSessionStream(sessionID string, stream *SessionStream) {
	sm.mu.Lock()
	stream.mu.Lock()
	if len(stream.subscribers) > 0 || stream.dead {
		stream.cleanup = nil
		stream.mu.Unlock()
		sm.mu.Unlock()
		return
	}
	stream.dead = true
	cancel := stream.pumpCancel
	stream.pumpCancel = nil
	stream.mu.Unlock()
	if sm.streams[sessionID] == stream {
		delete(sm.streams, sessionID)
	}
	sm.mu.Unlock()

	// The stream is retired: drop any pending fallback marker so its retrier
	// exits instead of leaking a map entry with no stream to protect.
	sm.forgetFallback(sessionID)

	if cancel != nil {
		cancel()
	}
}

// Publish fans out an event to the session's subscribers on every replica.
// With Redis it atomically assigns the shared seq, stores the event in the
// replay buffer, and publishes it so each pod's pump delivers it exactly once
// (including the publishing pod). If Redis is unavailable the event falls back
// to the local in-memory fan-out, and a resync marker is forced so every client
// reconciles instead of trusting a seq the fallback and the shared counter both
// handed out. The marker is published by the next successful publish or by a
// background retrier, whichever comes first.
func (sm *StreamManager) Publish(sessionID string, msg any) {
	if sm.rdb == nil {
		sm.publishLocal(sessionID, msg)
		return
	}
	if err := sm.publishRedis(sessionID, msg); err != nil {
		slog.Warn("agent stream: redis publish failed, falling back to local fan-out",
			"session_id", sessionID, "error", err)
		sm.markFellBack(sessionID, sm.publishLocal(sessionID, msg))
		return
	}
	if minSeq, ok := sm.takeFellBack(sessionID); ok {
		sm.publishRecoveryResync(sessionID, minSeq)
	}
}

// publishRecoveryResync publishes the forced resync marker after a Redis publish
// recovered from a fallback. If the marker itself cannot be published it is
// delivered to local subscribers and the session stays marked, so the retrier
// (or the next successful publish) tries again.
func (sm *StreamManager) publishRecoveryResync(sessionID string, minSeq uint64) {
	if err := sm.publishResyncRedis(sessionID, minSeq); err != nil {
		slog.Warn("agent stream: redis resync publish failed",
			"session_id", sessionID, "error", err)
		// The locally delivered marker may get a seq at or above minSeq; keep
		// the watermark above every seq local subscribers have seen.
		seq := sm.publishLocal(sessionID, map[string]any{"type": resyncMarkerType})
		sm.markFellBack(sessionID, max(minSeq, seq))
	}
}

func (sm *StreamManager) publishRedis(sessionID string, msg any) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionStreamRedisTimeout)
	defer cancel()
	seq, err := sessionStreamPublishScript.Run(ctx, sm.rdb,
		[]string{sessionStreamSeqKey(sessionID), sessionStreamBufferKey(sessionID), sessionStreamChannel(sessionID)},
		string(payload), int(sessionStreamBufferTTL.Seconds()),
	).Uint64()
	if err != nil {
		return err
	}
	sm.seedLocalSeq(sessionID, seq)
	return nil
}

// publishResyncRedis appends and publishes a resync marker whose seq is greater
// than minSeq (the highest seq a local fallback delivered) and refreshes the
// buffer TTL.
func (sm *StreamManager) publishResyncRedis(sessionID string, minSeq uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), sessionStreamRedisTimeout)
	defer cancel()
	seq, err := sessionStreamResyncScript.Run(ctx, sm.rdb,
		[]string{sessionStreamSeqKey(sessionID), sessionStreamBufferKey(sessionID), sessionStreamChannel(sessionID)},
		int(sessionStreamBufferTTL.Seconds()), minSeq,
	).Uint64()
	if err != nil {
		return err
	}
	sm.seedLocalSeq(sessionID, seq)
	return nil
}

// markFellBack records that a Redis publish failed on this pod and that local
// fallback seqs up to seq were delivered. A zero seq still marks the session:
// the event may have been lost for remote subscribers even when no local
// subscriber received it. The first call starts the session's single background
// retrier.
func (sm *StreamManager) markFellBack(sessionID string, seq uint64) {
	sm.fallbackMu.Lock()
	marker := sm.fellBack[sessionID]
	if marker == nil {
		marker = &fallbackMarker{}
		sm.fellBack[sessionID] = marker
	}
	if seq > marker.watermark {
		marker.watermark = seq
	}
	start := !marker.retrying
	if start {
		marker.retrying = true
	}
	sm.fallbackMu.Unlock()
	if start {
		go sm.retryFallbackResync(sessionID, marker)
	}
}

// takeFellBack consumes and returns the session's fallback watermark, if any.
// The retrier observes the marker is gone and exits.
func (sm *StreamManager) takeFellBack(sessionID string) (uint64, bool) {
	sm.fallbackMu.Lock()
	defer sm.fallbackMu.Unlock()
	marker, ok := sm.fellBack[sessionID]
	if !ok {
		return 0, false
	}
	delete(sm.fellBack, sessionID)
	return marker.watermark, true
}

// clearFallback drops the session's pending fallback marker when marker is
// still the current one, so a newer mark is left for its own retry.
func (sm *StreamManager) clearFallback(sessionID string, marker *fallbackMarker) {
	sm.fallbackMu.Lock()
	if sm.fellBack[sessionID] == marker {
		delete(sm.fellBack, sessionID)
	}
	sm.fallbackMu.Unlock()
}

// forgetFallback drops any pending fallback marker for the session, used when
// its stream is retired.
func (sm *StreamManager) forgetFallback(sessionID string) {
	sm.fallbackMu.Lock()
	delete(sm.fellBack, sessionID)
	sm.fallbackMu.Unlock()
}

// retryFallbackResync retries the session's recovery marker through Redis with
// capped backoff, independent of Publish, so a fallback is eventually
// advertised to every replica even when this pod publishes no further events.
// Redis may be down for a while, so the marker is retried until it commits, the
// stream is retired (forgetFallback removes the marker), or a bounded lifetime
// elapses.
func (sm *StreamManager) retryFallbackResync(sessionID string, marker *fallbackMarker) {
	deadline := time.Now().Add(sessionStreamFallbackRetryLifetime)
	backoff := sessionStreamFallbackRetryInitial
	for {
		sm.fallbackMu.Lock()
		current := sm.fellBack[sessionID]
		watermark := marker.watermark
		sm.fallbackMu.Unlock()
		if current != marker {
			return
		}
		if time.Now().After(deadline) {
			slog.Warn("agent stream: giving up fallback resync retry",
				"session_id", sessionID, "watermark", watermark)
			sm.clearFallback(sessionID, marker)
			return
		}
		if err := sm.publishResyncRedis(sessionID, watermark); err == nil {
			sm.fallbackMu.Lock()
			current := sm.fellBack[sessionID]
			if current == marker && marker.watermark == watermark {
				delete(sm.fellBack, sessionID)
				sm.fallbackMu.Unlock()
				return
			}
			newer := current == marker
			sm.fallbackMu.Unlock()
			if !newer {
				// The publish path consumed the marker first.
				return
			}
			// A newer fallback was recorded while the marker was in flight;
			// retry so the newer seqs are also covered.
		}
		time.Sleep(backoff)
		backoff = min(backoff*2, sessionStreamFallbackRetryMax)
	}
}

// seedLocalSeq keeps the in-memory fallback counter ahead of the shared Redis
// counter, so a publish during a Redis outage still gets a seq the client has
// not seen.
func (sm *StreamManager) seedLocalSeq(sessionID string, seq uint64) {
	sm.mu.RLock()
	stream := sm.streams[sessionID]
	sm.mu.RUnlock()
	if stream == nil {
		return
	}
	for {
		cur := stream.nextSeq.Load()
		if seq <= cur || stream.nextSeq.CompareAndSwap(cur, seq) {
			return
		}
	}
}

// publishLocal is the in-memory fan-out used without Redis and as the fallback
// when a Redis publish fails. It returns the seq it assigned, or 0 when the
// session has no local stream.
func (sm *StreamManager) publishLocal(sessionID string, msg any) uint64 {
	sm.mu.RLock()
	stream, ok := sm.streams[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return 0
	}

	seq := stream.nextSeq.Add(1)
	evt := SequencedEvent{Seq: seq, Msg: msg}

	// Keep a rolling buffer for late-joining subscribers
	stream.bufferMu.Lock()
	stream.buffer = append(stream.buffer, evt)
	if len(stream.buffer) > sessionStreamBufferMax {
		stream.buffer = stream.buffer[len(stream.buffer)-sessionStreamBufferMax:]
	}
	stream.bufferMu.Unlock()

	stream.mu.RLock()
	for _, sub := range stream.subscribers {
		stream.push(sub, evt)
	}
	stream.mu.RUnlock()
	return seq
}

// runSessionPump maintains the pod's subscription to one session's Redis
// channel: it catches local subscribers up from the shared buffer, delivers
// live events, and retries with exponential backoff if Redis is unreachable or
// the channel closes.
func (sm *StreamManager) runSessionPump(ctx context.Context, sessionID string, stream *SessionStream) {
	channel := sessionStreamChannel(sessionID)
	backoff := sessionStreamPumpReconnectDelay
	for {
		pubsub := sm.rdb.Subscribe(ctx, channel)
		// Confirm Redis processed the SUBSCRIBE before reading the buffer.
		// Without the ack, a publish landing between the buffer read and the
		// subscription taking effect would be delivered by neither path.
		if _, err := pubsub.ReceiveTimeout(ctx, sessionStreamRedisTimeout); err != nil {
			pubsub.Close()
			if ctx.Err() != nil {
				return
			}
			slog.Warn("agent stream: redis subscribe failed",
				"session_id", sessionID, "retry_in", backoff, "error", err)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = min(backoff*2, sessionStreamPumpReconnectMax)
			continue
		}
		backoff = sessionStreamPumpReconnectDelay
		liveCh := pubsub.Channel()
		sm.catchUpSessionStream(ctx, sessionID, stream)
		if !sm.pumpSessionLive(ctx, sessionID, stream, liveCh) {
			pubsub.Close()
			return
		}
		pubsub.Close()
		if !sleepCtx(ctx, sessionStreamPumpReconnectDelay) {
			return
		}
	}
}

// sleepCtx waits for d or until ctx is cancelled; it reports false on
// cancellation.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// pumpSessionLive delivers Redis channel messages until ctx is cancelled
// (returns false) or the channel closes and the pump should reconnect (returns
// true). A wake signal triggers a catch-up for subscribers that joined while
// the pump was parked.
func (sm *StreamManager) pumpSessionLive(ctx context.Context, sessionID string, stream *SessionStream, liveCh <-chan *redis.Message) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-stream.wake:
			sm.catchUpSessionStream(ctx, sessionID, stream)
		case msg, ok := <-liveCh:
			if !ok {
				return ctx.Err() == nil
			}
			evt, err := decodeSessionStreamEntry(msg.Payload)
			if err != nil {
				slog.Warn("agent stream: ignoring malformed redis event",
					"session_id", sessionID, "error", err)
				continue
			}
			stream.deliver(evt)
		}
	}
}

// catchUpSessionStream replays the shared buffer to local subscribers. Events
// are filtered by each subscriber's last delivered seq, so a subscriber that
// joined mid-stream (or one resuming after a reconnect) only receives what it
// has not seen. Replayed seqs are re-based through the subscriber's offset
// exactly like live delivery, so a counter reset can never make catch-up treat
// missed events as already seen. skipBuffer subscribers are not replayed; they
// are moved to ready with the current buffer head as their baseline so their
// first live event does not look like a gap. When a resuming subscriber's
// buffer has already been trimmed past the events it missed, a resync marker is
// emitted before the replay.
func (sm *StreamManager) catchUpSessionStream(ctx context.Context, sessionID string, stream *SessionStream) {
	entries, maxSeq, err := sm.readSessionBuffer(ctx, sessionID)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.Warn("agent stream: redis buffer read failed", "session_id", sessionID, "error", err)
		// Fail open: let live events flow for pending subscribers; their seq
		// gap will trigger a resync marker if buffered catch-up was missed.
		stream.mu.Lock()
		for _, sub := range stream.subscribers {
			sub.ready = true
		}
		stream.mu.Unlock()
		return
	}

	stream.mu.Lock()
	defer stream.mu.Unlock()
	for _, sub := range stream.subscribers {
		if sub.skipBuffer {
			if !sub.ready {
				sub.ready = true
				sub.lastSeq = maxSeq + sub.offset
			}
			continue
		}
		if !sub.ready {
			for _, evt := range entries {
				seq := evt.Seq + sub.offset
				if seq <= sub.lastSeq {
					continue
				}
				stream.push(sub, SequencedEvent{Seq: seq, Msg: evt.Msg})
				sub.lastSeq = seq
			}
			sub.ready = true
			continue
		}
		for _, evt := range entries {
			seq := evt.Seq + sub.offset
			if seq <= sub.lastSeq {
				continue
			}
			if seq > sub.lastSeq+1 {
				// The buffer no longer holds the events this subscriber
				// missed; replaying the next entry silently would look like
				// a gap. Mark the start of the missing range instead.
				stream.push(sub, resyncEvent(sub.lastSeq+1))
				sub.lastSeq++
			}
			stream.push(sub, SequencedEvent{Seq: seq, Msg: evt.Msg})
			sub.lastSeq = seq
		}
	}
}

// readSessionBuffer returns the buffered events in order plus the highest seq
// currently in the buffer.
func (sm *StreamManager) readSessionBuffer(ctx context.Context, sessionID string) ([]SequencedEvent, uint64, error) {
	readCtx, cancel := context.WithTimeout(ctx, sessionStreamRedisTimeout)
	defer cancel()
	raw, err := sm.rdb.LRange(readCtx, sessionStreamBufferKey(sessionID), 0, -1).Result()
	if err != nil {
		return nil, 0, err
	}
	entries := make([]SequencedEvent, 0, len(raw))
	var maxSeq uint64
	for _, entry := range raw {
		evt, err := decodeSessionStreamEntry(entry)
		if err != nil {
			slog.Warn("agent stream: skipping malformed buffer entry",
				"session_id", sessionID, "error", err)
			continue
		}
		entries = append(entries, evt)
		if evt.Seq > maxSeq {
			maxSeq = evt.Seq
		}
	}
	return entries, maxSeq, nil
}

// decodeSessionStreamEntry parses one buffered/published `{"seq":N,"msg":{...}}`
// entry.
func decodeSessionStreamEntry(raw string) (SequencedEvent, error) {
	var entry struct {
		Seq uint64          `json:"seq"`
		Msg json.RawMessage `json:"msg"`
	}
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return SequencedEvent{}, err
	}
	var msg any
	if err := json.Unmarshal(entry.Msg, &msg); err != nil {
		return SequencedEvent{}, err
	}
	return SequencedEvent{Seq: entry.Seq, Msg: msg}, nil
}

// resyncEvent builds a resync marker at seq.
func resyncEvent(seq uint64) SequencedEvent {
	return SequencedEvent{Seq: seq, Msg: map[string]any{"type": resyncMarkerType}}
}

// deliver fans a live event out to every ready local subscriber. Subscriber
// state is mutated under the write lock, which the single-pump invariant makes
// uncontended; the lock also excludes unsubscribe from closing a channel
// mid-push.
func (s *SessionStream) deliver(evt SequencedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subscribers {
		s.deliverLocked(sub, evt)
	}
}

// deliverLocked delivers one live source event to a subscriber while keeping
// the client-visible seq strictly increasing: duplicates are dropped, gaps
// become resync markers, and a source seq that moved backwards (the shared
// counter reset under a connected client) first emits a resync marker and then
// re-bases subsequent source seqs onto the client's clock via sub.offset.
// Callers must hold s.mu for writing.
func (s *SessionStream) deliverLocked(sub *streamSubscriber, evt SequencedEvent) {
	if !sub.ready {
		return
	}
	seq := evt.Seq + sub.offset
	if seq == sub.lastSeq {
		// A genuine duplicate: catch-up already replayed an event that was
		// still queued on the live channel. Only a strictly lower source seq
		// is a counter reset.
		return
	}
	if seq < sub.lastSeq {
		s.push(sub, resyncEvent(sub.lastSeq+1))
		sub.lastSeq++
		sub.offset = sub.lastSeq + 1 - evt.Seq
		seq = sub.lastSeq + 1
	}
	if seq > sub.lastSeq+1 {
		// A gap the client cannot detect on its own; the marker carries the
		// gap's target seq so the client drops stale replays and reconciles.
		s.push(sub, resyncEvent(seq))
		sub.lastSeq = seq
		return
	}
	s.push(sub, SequencedEvent{Seq: seq, Msg: evt.Msg})
	sub.lastSeq = seq
}

// push delivers an event without blocking. A full subscriber channel evicts
// its oldest queued event and queues a resync marker carrying the dropped
// event's seq, so the client can drop stale replays and reconcile.
func (s *SessionStream) push(sub *streamSubscriber, evt SequencedEvent) {
	select {
	case sub.ch <- evt:
		return
	default:
	}
	s.dropped.Add(1)
	select {
	case <-sub.ch:
	default:
	}
	select {
	case sub.ch <- resyncEvent(evt.Seq):
	default:
	}
}

// LastSeq returns the highest sequence number published to the session. With
// Redis this reads the shared counter (0 when absent); without Redis it is the
// process-local counter.
func (sm *StreamManager) LastSeq(sessionID string) uint64 {
	if sm.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), sessionStreamRedisTimeout)
		seq, err := sm.rdb.Get(ctx, sessionStreamSeqKey(sessionID)).Uint64()
		cancel()
		if err == nil {
			return seq
		}
		if !errors.Is(err, redis.Nil) {
			slog.Warn("agent stream: redis seq read failed", "session_id", sessionID, "error", err)
			return sm.localLastSeq(sessionID)
		}
		return 0
	}
	return sm.localLastSeq(sessionID)
}

func (sm *StreamManager) localLastSeq(sessionID string) uint64 {
	sm.mu.RLock()
	stream, ok := sm.streams[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return 0
	}
	return stream.nextSeq.Load()
}

// DropCount returns how many events were dropped for slow subscribers.
func (sm *StreamManager) DropCount(sessionID string) uint64 {
	sm.mu.RLock()
	stream, ok := sm.streams[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return 0
	}
	return stream.dropped.Load()
}
