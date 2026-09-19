package chaccess

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSyncDebouncesAndRunsOnce(t *testing.T) {
	var runs int
	s := NewSyncService(SyncConfig{
		Debounce:  10 * time.Millisecond,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error { runs++; return nil },
		Logger:    slog.Default(),
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
	s := NewSyncService(SyncConfig{
		Debounce:   time.Millisecond,
		MaxRetries: 3,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			attempts++
			return errors.New("boom")
		},
		Logger: slog.Default(),
	})
	s.Enqueue(uuid.New())
	s.Close()
	require.Equal(t, 3, attempts)
}

func TestSyncRecoversFromPanicAndRetries(t *testing.T) {
	var attempts int
	s := NewSyncService(SyncConfig{
		Debounce:   time.Millisecond,
		MaxRetries: 3,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			attempts++
			panic("invariant violated")
		},
		Logger: slog.Default(),
	})
	s.Enqueue(uuid.New())
	s.Close()
	require.Equal(t, 3, attempts)
}

func TestSyncPanicSurfacedAsError(t *testing.T) {
	s := NewSyncService(SyncConfig{
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			panic("invariant violated")
		},
		Logger: slog.Default(),
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
		Logger:    slog.Default(),
	})
	s.Close()
	s.Enqueue(uuid.New())
	s.Close()
	require.Equal(t, 0, runs)
}
