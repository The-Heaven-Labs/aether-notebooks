package agent

import (
	"sync"
	"sync/atomic"
	"time"
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

type StreamManager struct {
	mu      sync.RWMutex
	streams map[string]*SessionStream
}

type SessionStream struct {
	mu          sync.RWMutex
	subscribers []chan SequencedEvent
	buffer      []SequencedEvent
	bufferMu    sync.RWMutex
	cleanup     *time.Timer
	nextSeq     atomic.Uint64
	dropped     atomic.Uint64
}

func NewStreamManager() *StreamManager {
	return &StreamManager{
		streams: make(map[string]*SessionStream),
	}
}

// Subscribe creates (or joins) a session's event stream. When skipBuffer is
// true the returned channel only receives live events published after the
// subscription; when false it first receives a copy of the recent buffer for
// catch-up followed by live events.
// The caller must call the returned unsubscribe func when done.
func (sm *StreamManager) Subscribe(sessionID string, bufSize int, skipBuffer bool) (chan SequencedEvent, func()) {
	sm.mu.Lock()
	stream, ok := sm.streams[sessionID]
	if !ok {
		stream = &SessionStream{
			subscribers: make([]chan SequencedEvent, 0, 4),
		}
		sm.streams[sessionID] = stream
	} else if stream.cleanup != nil {
		// A cleanup timer was scheduled (last subscriber left). Cancel it
		// because we're re-joining the stream.
		stream.cleanup.Stop()
		stream.cleanup = nil
	}
	sm.mu.Unlock()

	ch := make(chan SequencedEvent, bufSize)

	if !skipBuffer {
		// Catch-up: replay buffered events so the new subscriber doesn't miss
		// tokens that were streamed before it joined (e.g. while navigating).
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
	stream.subscribers = append(stream.subscribers, ch)
	stream.mu.Unlock()

	unsubscribe := func() {
		stream.mu.Lock()
		for i, sub := range stream.subscribers {
			if sub == ch {
				stream.subscribers = append(stream.subscribers[:i], stream.subscribers[i+1:]...)
				close(ch)
				break
			}
		}
		empty := len(stream.subscribers) == 0
		stream.mu.Unlock()

		if empty {
			// Don't delete immediately – keep the stream alive for a brief
			// window so a reconnecting WebSocket (page navigation) can pick
			// up the in-flight buffer.
			sm.mu.Lock()
			if stream.cleanup == nil {
				stream.cleanup = time.AfterFunc(5*time.Second, func() {
					sm.mu.Lock()
					delete(sm.streams, sessionID)
					sm.mu.Unlock()
				})
			}
			sm.mu.Unlock()
		}
	}

	return ch, unsubscribe
}

// Publish fans out an event to every subscriber of the session's stream.
// If no subscribers exist but the stream is in the grace period (between
// unsubscribe and cleanup), the event is buffered for future subscribers.
//
// Every event gets a per-session sequence number. If a subscriber's channel is
// full, the event is dropped for that subscriber — but instead of forgetting it
// silently, the oldest queued event is evicted and a `resync` marker is queued
// so the client knows to reconcile via `reconnect`.
func (sm *StreamManager) Publish(sessionID string, msg any) {
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
	if len(stream.buffer) > 500 {
		stream.buffer = stream.buffer[len(stream.buffer)-500:]
	}
	stream.bufferMu.Unlock()

	stream.mu.RLock()
	for _, sub := range stream.subscribers {
		select {
		case sub <- evt:
		default:
			// Subscriber too slow (e.g. WebSocket gone). Evict the oldest
			// queued event to make room for a resync marker so the client
			// reconciles instead of diverging silently.
			stream.dropped.Add(1)
			select {
			case <-sub:
			default:
			}
			markerSeq := stream.nextSeq.Add(1)
			select {
			case sub <- SequencedEvent{Seq: markerSeq, Msg: map[string]any{"type": resyncMarkerType}}:
			default:
			}
		}
	}
	stream.mu.RUnlock()
}

// LastSeq returns the highest sequence number published to the session's
// stream (0 when the session has no stream). Served inside reconnect_sync as
// server_seq so clients can ignore stale replayed events.
func (sm *StreamManager) LastSeq(sessionID string) uint64 {
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
