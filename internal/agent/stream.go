package agent

import (
	"sync"
	"time"
)

type StreamManager struct {
	mu      sync.RWMutex
	streams map[string]*SessionStream
}

type SessionStream struct {
	mu          sync.RWMutex
	subscribers []chan any
	buffer      []any
	bufferMu    sync.RWMutex
	cleanup     *time.Timer
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
func (sm *StreamManager) Subscribe(sessionID string, bufSize int, skipBuffer bool) (chan any, func()) {
	sm.mu.Lock()
	stream, ok := sm.streams[sessionID]
	if !ok {
		stream = &SessionStream{
			subscribers: make([]chan any, 0, 4),
		}
		sm.streams[sessionID] = stream
	} else if stream.cleanup != nil {
		// A cleanup timer was scheduled (last subscriber left). Cancel it
		// because we're re-joining the stream.
		stream.cleanup.Stop()
		stream.cleanup = nil
	}
	sm.mu.Unlock()

	ch := make(chan any, bufSize)

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
func (sm *StreamManager) Publish(sessionID string, msg any) {
	sm.mu.RLock()
	stream, ok := sm.streams[sessionID]
	sm.mu.RUnlock()
	if !ok {
		return
	}

	// Keep a rolling buffer for late-joining subscribers
	stream.bufferMu.Lock()
	stream.buffer = append(stream.buffer, msg)
	if len(stream.buffer) > 500 {
		stream.buffer = stream.buffer[len(stream.buffer)-500:]
	}
	stream.bufferMu.Unlock()

	stream.mu.RLock()
	for _, sub := range stream.subscribers {
		select {
		case sub <- msg:
		default:
			// Subscriber too slow (e.g. WebSocket gone) – drop
		}
	}
	stream.mu.RUnlock()
}
