package api

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackgroundLoopStartStop(t *testing.T) {
	var l backgroundLoop
	started := make(chan struct{})
	exited := make(chan struct{})
	l.start(context.Background(), func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(exited)
	})
	<-started

	// A second start while one loop is running must be refused.
	var extra atomic.Int32
	l.start(context.Background(), func(context.Context) { extra.Add(1) })
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, extra.Load(), "a second concurrent start must be refused")

	// stop must cancel the loop and wait for it to exit.
	l.stop()
	select {
	case <-exited:
	default:
		t.Fatal("stop must wait for the loop to exit")
	}

	// stop is idempotent and a start after stop must be refused.
	l.stop()
	l.start(context.Background(), func(context.Context) { extra.Add(1) })
	time.Sleep(20 * time.Millisecond)
	require.Zero(t, extra.Load(), "a start after stop must be refused")
}
