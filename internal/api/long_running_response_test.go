package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type writeDeadlineRecorder struct {
	http.ResponseWriter
	deadline time.Time
	set      bool
}

func (w *writeDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	w.set = true
	return nil
}

// Long-running data-plane handlers must replace the server's 60s WriteTimeout
// so an idle ClickHouse Cloud wake or a long query can still return a response.
func TestAllowLongRunningResponseExtendsWriteDeadline(t *testing.T) {
	require.Greater(t, longRunningWriteDeadline, 60*time.Second,
		"the override must exceed the server WriteTimeout or it would shorten it")

	w := &writeDeadlineRecorder{ResponseWriter: httptest.NewRecorder()}
	before := time.Now()
	allowLongRunningResponse(w)

	require.True(t, w.set)
	require.WithinDuration(t, before.Add(longRunningWriteDeadline), w.deadline, time.Second)
}
