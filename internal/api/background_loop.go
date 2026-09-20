package api

import (
	"context"
	"sync"
)

// backgroundLoop owns the lifecycle of one long-running background goroutine.
// start refuses a second loop while one is running and refuses to start after
// stop; stop cancels the loop, waits for it to exit, and refuses future starts.
// Both methods are idempotent and safe to call concurrently. The zero value is
// ready to use.
type backgroundLoop struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	closed bool
}

// start runs fn on a context derived from ctx. It is a no-op when a loop is
// already running or after stop.
func (l *backgroundLoop) start(ctx context.Context, fn func(context.Context)) {
	l.mu.Lock()
	if l.closed || l.cancel != nil {
		l.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	l.cancel = cancel
	l.done = done
	l.mu.Unlock()

	go func() {
		defer close(done)
		fn(loopCtx)
	}()
}

// stop cancels the loop and waits for it to exit. It is idempotent; a stop
// before any start still refuses future starts.
func (l *backgroundLoop) stop() {
	l.mu.Lock()
	l.closed = true
	cancel := l.cancel
	done := l.done
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
