package chaccess

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SyncConfig wires the worker to the environment.
type SyncConfig struct {
	Debounce   time.Duration
	MaxRetries int
	Reconcile  func(ctx context.Context, warehouseID uuid.UUID) error
	Logger     *slog.Logger
}

// SyncService coalesces sync requests per warehouse and runs a single
// reconciliation at a time per warehouse. Close waits for in-flight work.
type SyncService struct {
	cfg    SyncConfig
	mu     sync.Mutex
	queued map[uuid.UUID]bool
	active map[uuid.UUID]bool
	wg     sync.WaitGroup
	closed bool
}

// NewSyncService returns a worker. Debounce defaults to 2s and MaxRetries to
// 3 when unset; a nil Logger falls back to slog.Default.
func NewSyncService(cfg SyncConfig) *SyncService {
	if cfg.Debounce <= 0 {
		cfg.Debounce = 2 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &SyncService{cfg: cfg, queued: map[uuid.UUID]bool{}, active: map[uuid.UUID]bool{}}
}

// Enqueue schedules a warehouse sync, coalescing bursts: while a sync for the
// warehouse is queued or running, further calls are dropped. After Close,
// Enqueue is a no-op.
func (s *SyncService) Enqueue(warehouseID uuid.UUID) {
	s.mu.Lock()
	if s.closed || s.queued[warehouseID] || s.active[warehouseID] {
		s.mu.Unlock()
		return
	}
	s.queued[warehouseID] = true
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		time.Sleep(s.cfg.Debounce)

		s.mu.Lock()
		delete(s.queued, warehouseID)
		s.active[warehouseID] = true
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			delete(s.active, warehouseID)
			s.mu.Unlock()
		}()

		s.run(context.Background(), warehouseID)
	}()
}

// run reconciles a warehouse with retries. A panic from Reconcile is
// recovered and converted to an error so one bad warehouse cannot take down
// the process; it is retried like any other failure.
func (s *SyncService) run(ctx context.Context, warehouseID uuid.UUID) {
	var err error
	for attempt := 0; attempt < s.cfg.MaxRetries; attempt++ {
		err = s.reconcile(ctx, warehouseID)
		if err == nil {
			return
		}
		s.cfg.Logger.Warn("warehouse sync failed", "warehouse_id", warehouseID, "attempt", attempt+1, "error", err)
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	s.cfg.Logger.Error("warehouse sync giving up", "warehouse_id", warehouseID, "error", err)
}

// reconcile invokes the configured Reconcile, recovering panics into errors.
func (s *SyncService) reconcile(ctx context.Context, warehouseID uuid.UUID) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.cfg.Logger.Error("warehouse sync panicked", "warehouse_id", warehouseID, "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("warehouse %s reconcile panic: %v", warehouseID, r)
		}
	}()
	return s.cfg.Reconcile(ctx, warehouseID)
}

// Close rejects new Enqueue calls and waits for queued and active work to
// finish. Work already enqueued when Close is called still runs to
// completion (queued work is drained, not cancelled).
func (s *SyncService) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.wg.Wait()
}
