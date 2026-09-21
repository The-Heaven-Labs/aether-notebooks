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
	s.connPoolLoop.start(ctx, func(loopCtx context.Context) {
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
	})
}
