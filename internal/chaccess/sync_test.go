package chaccess

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func discardSyncLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSyncRequiresReconcile(t *testing.T) {
	require.PanicsWithValue(t, "chaccess: SyncConfig.Reconcile must not be nil", func() {
		NewSyncService(SyncConfig{Logger: discardSyncLogger()})
	})
}

func TestSyncDebouncesAndRunsOnce(t *testing.T) {
	var runs int
	s := NewSyncService(SyncConfig{
		Debounce:  10 * time.Millisecond,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error { runs++; return nil },
		Logger:    discardSyncLogger(),
	})
	wh := uuid.New()
	for i := 0; i < 5; i++ {
		s.Enqueue(wh)
	}
	s.Close()
	require.Equal(t, 1, runs)
}

func TestSyncRetriesWithBackoff(t *testing.T) {
	var attempts int
	exhausted := make(chan struct{})
	s := NewSyncService(SyncConfig{
		Debounce:     time.Millisecond,
		RetryBackoff: time.Millisecond,
		MaxAttempts:  3,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			attempts++
			if attempts == 3 {
				close(exhausted)
			}
			return errors.New("boom")
		},
		Logger: discardSyncLogger(),
	})
	s.Enqueue(uuid.New())
	select {
	case <-exhausted:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile did not exhaust MaxAttempts")
	}
	s.Close()
	require.Equal(t, 3, attempts)
}

func TestSyncRetriesUntilSuccess(t *testing.T) {
	var logs bytes.Buffer
	var attempts int
	succeeded := make(chan struct{})
	s := NewSyncService(SyncConfig{
		Debounce:     time.Millisecond,
		RetryBackoff: time.Millisecond,
		MaxAttempts:  3,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			attempts++
			if attempts == 1 {
				return errors.New("transient")
			}
			close(succeeded)
			return nil
		},
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	s.Enqueue(uuid.New())
	select {
	case <-succeeded:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile did not retry after a transient failure")
	}
	s.Close()
	require.Equal(t, 2, attempts)
	require.Contains(t, logs.String(), "warehouse sync failed")
	require.NotContains(t, logs.String(), "giving up")
}

func TestSyncRecoversFromPanicAndRetries(t *testing.T) {
	var logs bytes.Buffer
	var attempts int
	gaveUp := make(chan struct{})
	s := NewSyncService(SyncConfig{
		Debounce:     time.Millisecond,
		RetryBackoff: time.Millisecond,
		MaxAttempts:  3,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			attempts++
			if attempts == 3 {
				close(gaveUp)
			}
			panic("invariant violated")
		},
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	s.Enqueue(uuid.New())
	select {
	case <-gaveUp:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile did not exhaust MaxAttempts after panics")
	}
	s.Close()
	require.Equal(t, 3, attempts)
	out := logs.String()
	require.Contains(t, out, "warehouse sync giving up")
	require.Contains(t, out, "invariant violated")
	require.Equal(t, 1, strings.Count(out, "stack="), "recovered stack should be logged once, at give-up")
}

func TestSyncPanicSurfacedAsError(t *testing.T) {
	s := NewSyncService(SyncConfig{
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			panic("invariant violated")
		},
		Logger: discardSyncLogger(),
	})
	err := s.reconcile(context.Background(), uuid.New())
	require.Error(t, err)
	require.Contains(t, err.Error(), "invariant violated")
}

func TestSyncEnqueueAfterCloseIgnored(t *testing.T) {
	var runs int
	s := NewSyncService(SyncConfig{
		Debounce:  time.Millisecond,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error { runs++; return nil },
		Logger:    discardSyncLogger(),
	})
	s.Close()
	s.Enqueue(uuid.New())
	s.Close()
	require.Equal(t, 0, runs)
}

func TestSyncRerunsWhenEnqueueLandsDuringActiveReconcile(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	var calls int
	s := NewSyncService(SyncConfig{
		Debounce:     time.Millisecond,
		RetryBackoff: time.Millisecond,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			mu.Lock()
			calls++
			first := calls == 1
			mu.Unlock()
			if first {
				close(started)
				<-release
			}
			return nil
		},
		Logger: discardSyncLogger(),
	})
	wh := uuid.New()
	s.Enqueue(wh)
	<-started
	s.Enqueue(wh)
	close(release)
	s.Close()
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, calls)
}

func TestSyncWarehousesRunIndependently(t *testing.T) {
	whA, whB := uuid.New(), uuid.New()
	startedA := make(chan struct{})
	blockA := make(chan struct{})
	bDone := make(chan struct{})
	var onceA, onceB sync.Once
	s := NewSyncService(SyncConfig{
		Debounce:     time.Millisecond,
		RetryBackoff: time.Millisecond,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			if warehouseID == whA {
				onceA.Do(func() { close(startedA) })
				<-blockA
				return nil
			}
			onceB.Do(func() { close(bDone) })
			return nil
		},
		Logger: discardSyncLogger(),
	})
	s.Enqueue(whA)
	<-startedA
	s.Enqueue(whB)
	select {
	case <-bDone:
	case <-time.After(2 * time.Second):
		t.Fatal("warehouse B reconcile blocked behind warehouse A")
	}
	close(blockA)
	s.Close()
}

func TestSyncCloseWaitsForActiveRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	s := NewSyncService(SyncConfig{
		Debounce:     time.Millisecond,
		RetryBackoff: time.Millisecond,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			close(started)
			<-release
			return nil
		},
		Logger: discardSyncLogger(),
	})
	s.Enqueue(uuid.New())
	<-started
	go func() {
		s.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before the active reconcile finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the active reconcile finished")
	}
}
