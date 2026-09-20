package api

import (
	"context"
	"time"
)

// Warehouse connection pool defaults. MaxPools bounds the process-wide
// ClickHouse connections held for per-user executions; IdleTTL closes
// connections that have gone unused so identities do not pin sockets forever.
const (
	connPoolMaxPools = 100
	connPoolIdleTTL  = 10 * time.Minute
	// connPoolIdleInterval is how often the idle-eviction ticker fires.
	connPoolIdleInterval = time.Minute
)

// startConnPoolIdleLoop starts the CloseIdle ticker. The loop stops when ctx
// is done or Server.Close runs; Close waits for it before closing the pool.
// It is safe to call more than once: a second call is a no-op while a loop is
// running, and after Close no loop starts.
func (s *Server) startConnPoolIdleLoop(ctx context.Context) {
	s.connPoolLoopMu.Lock()
	if s.connPoolLoopClosed || s.connPoolLoopCancel != nil {
		s.connPoolLoopMu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.connPoolLoopCancel = cancel
	s.connPoolLoopDone = done
	s.connPoolLoopMu.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(connPoolIdleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case now := <-ticker.C:
				s.connPool.CloseIdle(now)
			}
		}
	}()
}

// stopConnPoolIdleLoop stops the idle-eviction loop and waits for it to exit.
func (s *Server) stopConnPoolIdleLoop() {
	s.connPoolLoopMu.Lock()
	s.connPoolLoopClosed = true
	cancel := s.connPoolLoopCancel
	done := s.connPoolLoopDone
	s.connPoolLoopMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
