package chaccess

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SyncConfig wires the worker to the environment.
type SyncConfig struct {
	// Debounce delays a queued reconcile so bursts of Enqueue calls
	// collapse into one run. Defaults to 2s.
	Debounce time.Duration
	// RetryBackoff is the pause between reconcile attempts. Defaults to
	// 500ms. There is no backoff after the final attempt.
	RetryBackoff time.Duration
	// MaxAttempts is the total number of reconcile attempts for one run,
	// including the first. Defaults to 3.
	MaxAttempts int
	// MaxConcurrent bounds how many reconciles run at the same time across
	// all warehouses, limiting ClickHouse and Postgres connection use when
	// startup enqueues every warehouse at once. Defaults to 4.
	MaxConcurrent int
	// Reconcile performs one reconciliation pass for a warehouse. Required.
	// Its ctx is cancelled by Close.
	Reconcile func(ctx context.Context, warehouseID uuid.UUID) error
	// Logger receives sync progress. Defaults to slog.Default().
	Logger *slog.Logger
}

// SyncService coalesces sync requests per warehouse and runs a single
// reconciliation per warehouse at a time, with a global concurrency cap. A
// change enqueued while a warehouse is reconciling sets a rerun flag, so one
// follow-up run is scheduled after the current one finishes; further enqueues
// coalesce into that follow-up. Close cancels retries and waits for queued and
// active work.
type SyncService struct {
	cfg    SyncConfig
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	queued map[uuid.UUID]struct{}
	active map[uuid.UUID]struct{}
	rerun  map[uuid.UUID]struct{}
	kicks  map[uuid.UUID]chan struct{}
	sem    chan struct{}
	wg     sync.WaitGroup
	closed bool
}

// NewSyncService returns a worker. Debounce defaults to 2s, RetryBackoff to
// 500ms, MaxAttempts to 3, MaxConcurrent to 4, and Logger to slog.Default. It
// panics if Reconcile is nil.
func NewSyncService(cfg SyncConfig) *SyncService {
	if cfg.Reconcile == nil {
		panic("chaccess: SyncConfig.Reconcile must not be nil")
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 2 * time.Second
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 500 * time.Millisecond
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &SyncService{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
		queued: map[uuid.UUID]struct{}{},
		active: map[uuid.UUID]struct{}{},
		rerun:  map[uuid.UUID]struct{}{},
		kicks:  map[uuid.UUID]chan struct{}{},
		sem:    make(chan struct{}, cfg.MaxConcurrent),
	}
}

// Enqueue schedules a warehouse sync, coalescing bursts. A request that lands
// while the warehouse is reconciling sets a rerun flag so the change is not
// lost; a request that lands while one is already queued collapses into it.
// After Close, Enqueue is a no-op.
func (s *SyncService) Enqueue(warehouseID uuid.UUID) {
	s.enqueue(warehouseID, false)
}

// EnqueueNow schedules a warehouse sync that skips the debounce delay. It is
// used for state changes that must converge promptly, such as a warehouse
// losing its provisioner mid-move. A run already in flight still completes
// first; the immediate request schedules a rerun that starts as soon as it
// settles. After Close, EnqueueNow is a no-op.
func (s *SyncService) EnqueueNow(warehouseID uuid.UUID) {
	s.enqueue(warehouseID, true)
}

func (s *SyncService) enqueue(warehouseID uuid.UUID, now bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	// Wake a debouncing worker before the queued/active checks: an active run
	// will honor the rerun flag below, and a queued run would otherwise sleep
	// out its debounce with the kick already buffered for the next loop.
	if now {
		if kick, ok := s.kicks[warehouseID]; ok {
			select {
			case kick <- struct{}{}:
			default:
			}
		}
	}
	if _, ok := s.active[warehouseID]; ok {
		s.rerun[warehouseID] = struct{}{}
		return
	}
	if _, ok := s.queued[warehouseID]; ok {
		return
	}
	s.queued[warehouseID] = struct{}{}
	kick := make(chan struct{}, 1)
	s.kicks[warehouseID] = kick
	if now {
		kick <- struct{}{}
	}
	s.wg.Add(1)
	go s.worker(warehouseID, kick)
}

// worker drains one warehouse's queued/rerun work. It is a single wg unit:
// Close waits for the whole drain, including follow-up runs. Each run holds a
// global concurrency slot, so at most MaxConcurrent reconciles are in flight
// across all warehouses.
func (s *SyncService) worker(warehouseID uuid.UUID, kick <-chan struct{}) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		if s.kicks[warehouseID] == kick {
			delete(s.kicks, warehouseID)
		}
		s.mu.Unlock()
	}()
	for {
		select {
		case <-time.After(s.cfg.Debounce):
		case <-kick:
		}

		s.mu.Lock()
		delete(s.queued, warehouseID)
		s.active[warehouseID] = struct{}{}
		s.mu.Unlock()

		s.sem <- struct{}{}
		s.run(s.ctx, warehouseID)
		<-s.sem

		s.mu.Lock()
		_, again := s.rerun[warehouseID]
		if !again {
			delete(s.active, warehouseID)
			s.mu.Unlock()
			return
		}
		delete(s.rerun, warehouseID)
		s.mu.Unlock()
	}
}

// run reconciles a warehouse with retries. A panic from Reconcile is
// recovered and converted to an error so one bad warehouse cannot take down
// the process; it is retried like any other failure. Cancellation stops the
// retry loop between attempts but never skips the first one.
func (s *SyncService) run(ctx context.Context, warehouseID uuid.UUID) {
	var err error
	for attempt := 1; attempt <= s.cfg.MaxAttempts; attempt++ {
		err = s.reconcile(ctx, warehouseID)
		if err == nil {
			return
		}
		s.cfg.Logger.Warn("warehouse sync failed", "warehouse_id", warehouseID, "attempt", attempt, "max_attempts", s.cfg.MaxAttempts, "error", err)
		if attempt == s.cfg.MaxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.cfg.RetryBackoff):
		}
	}
	attrs := []any{"warehouse_id", warehouseID, "error", err}
	var pe *panicError
	if errors.As(err, &pe) {
		attrs = append(attrs, "stack", string(pe.stack))
	}
	s.cfg.Logger.Error("warehouse sync giving up", attrs...)
}

// reconcile invokes the configured Reconcile, recovering panics into errors.
func (s *SyncService) reconcile(ctx context.Context, warehouseID uuid.UUID) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicError{warehouseID: warehouseID, value: r, stack: debug.Stack()}
		}
	}()
	return s.cfg.Reconcile(ctx, warehouseID)
}

// panicError carries the recovered stack so it is logged once, at give-up,
// instead of on every retry.
type panicError struct {
	warehouseID uuid.UUID
	value       any
	stack       []byte
}

func (e *panicError) Error() string {
	return fmt.Sprintf("warehouse %s reconcile panic: %v", e.warehouseID, e.value)
}

// Close rejects new Enqueue calls, cancels retry backoff, and waits for
// queued and active work to finish. Work already enqueued when Close is
// called (including a rerun requested before Close) still gets its first
// attempt, but the service context is cancelled, so retries stop and a
// context-aware Reconcile may abort before applying changes; restart
// catch-up (Task 11 startup enqueue) covers that.
func (s *SyncService) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
}
