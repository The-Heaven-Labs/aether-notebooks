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
// an event for a slow subscriber. Clients react by sending a `reconnect` message
// to reconcile against the authoritative DB state. A dropped event is never
// silently forgotten.
const resyncMarkerType = "resync"

const (
	// sessionStreamTTL is how long a session's Redis seq counter and replay
	// buffer survive after the last publish.
	sessionStreamTTL = 2 * time.Hour
	// sessionStreamBufferMax caps the Redis replay buffer.
	sessionStreamBufferMax = 500
	// sessionStreamRedisTimeout bounds every individual Redis call so a dead
	// Redis cannot block Publish, Subscribe, or LastSeq indefinitely.
	sessionStreamRedisTimeout = time.Second
	// sessionStreamPumpReconnectDelay is the pause before a session pump
	// resubscribes after its pub/sub channel closed.
	sessionStreamPumpReconnectDelay = time.Second
)

// sessionStreamPublishScript atomically assigns the next seq, appends the entry
// to the replay buffer, trims the buffer, refreshes both keys' TTLs, and
// publishes the entry to every replica. KEYS: seq, buffer, channel. ARGV: JSON
// message, TTL seconds. The entry embeds its seq so subscribers can dedup and
// detect gaps.
var sessionStreamPublishScript = redis.NewScript(fmt.Sprintf(`
local seq = redis.call('INCR', KEYS[1])
local entry = '{"seq":' .. seq .. ',"msg":' .. ARGV[1] .. '}'
redis.call('RPUSH', KEYS[2], entry)
redis.call('LTRIM', KEYS[2], -%d, -1)
redis.call('EXPIRE', KEYS[1], ARGV[2])
redis.call('EXPIRE', KEYS[2], ARGV[2])
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
}

type streamSubscriber struct {
	ch         chan SequencedEvent
	skipBuffer bool
	ready      bool
	lastSeq    uint64
}

// SessionStream is one session's local subscriber set plus, without Redis, its
// replay buffer and process-local seq counter.
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
}

// NewStreamManager returns a stream manager. With a nil Redis client it uses
// the in-process fan-out (unit tests, single-node installs); with a client it
// fans out through Redis pub/sub so viewers on every replica see the session.
func NewStreamManager(rdb *redis.Client) *StreamManager {
	return &StreamManager{
		rdb:     rdb,
		streams: make(map[string]*SessionStream),
	}
}

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

// Subscribe creates (or joins) a session's event stream. When skipBuffer is
// true the returned channel only receives live events published after the
// subscription; when false it first receives the recent events for catch-up
// followed by live events.
// The caller must call the returned unsubscribe func when done.
func (sm *StreamManager) Subscribe(sessionID string, bufSize int, skipBuffer bool) (chan SequencedEvent, func()) {
	sm.mu.Lock()
	stream, ok := sm.streams[sessionID]
	if !ok {
		stream = sm.newSessionStream()
		sm.streams[sessionID] = stream
	} else if stream.cleanup != nil {
		// A cleanup timer was scheduled (last subscriber left). Cancel it
		// because we're re-joining the stream.
		stream.cleanup.Stop()
		stream.cleanup = nil
	}
	sm.mu.Unlock()

	ch := make(chan SequencedEvent, bufSize)
	sub := &streamSubscriber{ch: ch, skipBuffer: skipBuffer}

	if sm.rdb == nil {
		// In-memory fan-out: replay the local buffer before registering the
		// subscriber so catch-up events cannot be interleaved with live ones.
		if !skipBuffer {
			stream.bufferMu.RLock()
			for _, evt := range stream.buffer {
				select {
				case ch <- evt:
				default:
				}
			}
			stream.bufferMu.RUnlock()
		}

		stream.mu.Lock()
		stream.subscribers = append(stream.subscribers, sub)
		stream.mu.Unlock()

		return ch, sm.unsubscribeFunc(sessionID, stream, sub)
	}

	var pumpCtx context.Context
	stream.mu.Lock()
	stream.subscribers = append(stream.subscribers, sub)
	if stream.pumpCancel == nil {
		var cancel context.CancelFunc
		pumpCtx, cancel = context.WithCancel(context.Background())
		stream.pumpCancel = cancel
	}
	stream.mu.Unlock()
	if pumpCtx != nil {
		go sm.runSessionPump(pumpCtx, sessionID, stream)
	}

	// The pump performs the subscriber's buffer catch-up: wake it in case it
	// is parked in the live select.
	select {
	case stream.wake <- struct{}{}:
	default:
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
		empty := len(stream.subscribers) == 0
		stream.mu.Unlock()

		if !empty {
			return
		}
		// Don't delete immediately – keep the stream alive for a brief
		// window so a reconnecting WebSocket (page navigation) can pick
		// up the in-flight buffer.
		sm.mu.Lock()
		if stream.cleanup == nil {
			stream.cleanup = time.AfterFunc(5*time.Second, func() {
				sm.mu.Lock()
				delete(sm.streams, sessionID)
				sm.mu.Unlock()

				stream.mu.Lock()
				cancel := stream.pumpCancel
				stream.pumpCancel = nil
				stream.mu.Unlock()
				if cancel != nil {
					cancel()
				}
			})
		}
		sm.mu.Unlock()
	}
}

// Publish fans out an event to the session's subscribers on every replica.
// With Redis it atomically assigns the shared seq, stores the event in the
// replay buffer, and publishes it so each pod's pump delivers it exactly once
// (including the publishing pod). If Redis is unavailable the event falls back
// to the local in-memory fan-out, and seq continuity is best-effort until the
// client reconciles through resync/reconnect_sync.
func (sm *StreamManager) Publish(sessionID string, msg any) {
	if sm.rdb != nil {
		err := sm.publishRedis(sessionID, msg)
		if err == nil {
			return
		}
		slog.Warn("agent stream: redis publish failed, falling back to local fan-out",
			"session_id", sessionID, "error", err)
	}
	sm.publishLocal(sessionID, msg)
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
		string(payload), int(sessionStreamTTL.Seconds()),
	).Uint64()
	if err != nil {
		return err
	}
	sm.seedLocalSeq(sessionID, seq)
	return nil
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
// when a Redis publish fails.
func (sm *StreamManager) publishLocal(sessionID string, msg any) {
	sm.mu.RLock()
	stream, ok := sm.streams[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return
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
		select {
		case sub.ch <- evt:
		default:
			// Subscriber too slow (e.g. WebSocket gone). Evict the oldest
			// queued event to make room for a resync marker so the client
			// reconciles instead of diverging silently.
			stream.dropped.Add(1)
			select {
			case <-sub.ch:
			default:
			}
			markerSeq := stream.nextSeq.Add(1)
			select {
			case sub.ch <- SequencedEvent{Seq: markerSeq, Msg: map[string]any{"type": resyncMarkerType}}:
			default:
			}
		}
	}
	stream.mu.RUnlock()
}

// runSessionPump maintains the pod's subscription to one session's Redis
// channel: it catches local subscribers up from the shared buffer, delivers
// live events, and resubscribes with a short backoff if the channel closes.
func (sm *StreamManager) runSessionPump(ctx context.Context, sessionID string, stream *SessionStream) {
	channel := sessionStreamChannel(sessionID)
	for {
		pubsub := sm.rdb.Subscribe(ctx, channel)
		// Confirm Redis processed the SUBSCRIBE before reading the buffer.
		// Without the ack, a publish landing between the buffer read and the
		// subscription taking effect would be delivered by neither path.
		if _, err := pubsub.ReceiveTimeout(ctx, sessionStreamRedisTimeout); err != nil {
			slog.Warn("agent stream: redis subscribe failed", "session_id", sessionID, "error", err)
			pubsub.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(sessionStreamPumpReconnectDelay):
			}
			continue
		}
		liveCh := pubsub.Channel()
		sm.catchUpSessionStream(ctx, sessionID, stream)
		if !sm.pumpSessionLive(ctx, sessionID, stream, liveCh) {
			pubsub.Close()
			return
		}
		pubsub.Close()
		select {
		case <-ctx.Done():
			return
		case <-time.After(sessionStreamPumpReconnectDelay):
		}
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
				return true
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
// has not seen. skipBuffer subscribers are not replayed; they are moved to
// ready with the current buffer head as their baseline so their first live
// event does not look like a gap.
func (sm *StreamManager) catchUpSessionStream(ctx context.Context, sessionID string, stream *SessionStream) {
	entries, maxSeq, err := sm.readSessionBuffer(ctx, sessionID)
	if err != nil {
		slog.Warn("agent stream: redis buffer read failed", "session_id", sessionID, "error", err)
		// Fail open: let live events flow for pending subscribers; their seq
		// gap will trigger a resync marker if buffered catch-up was missed.
		stream.mu.RLock()
		for _, sub := range stream.subscribers {
			sub.ready = true
		}
		stream.mu.RUnlock()
		return
	}

	stream.mu.RLock()
	defer stream.mu.RUnlock()
	for _, sub := range stream.subscribers {
		if sub.skipBuffer {
			if !sub.ready {
				sub.ready = true
				sub.lastSeq = maxSeq
			}
			continue
		}
		for _, evt := range entries {
			if evt.Seq <= sub.lastSeq {
				continue
			}
			stream.push(sub, evt)
			sub.lastSeq = evt.Seq
		}
		sub.ready = true
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

// deliver fans an event out to every ready local subscriber, deduplicating by
// seq and replacing a detected gap with the resync marker.
func (s *SessionStream) deliver(evt SequencedEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subscribers {
		if !sub.ready || evt.Seq <= sub.lastSeq {
			continue
		}
		if evt.Seq > sub.lastSeq+1 {
			s.push(sub, SequencedEvent{Seq: evt.Seq, Msg: map[string]any{"type": resyncMarkerType}})
		} else {
			s.push(sub, evt)
		}
		sub.lastSeq = evt.Seq
	}
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
	case sub.ch <- SequencedEvent{Seq: evt.Seq, Msg: map[string]any{"type": resyncMarkerType}}:
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
