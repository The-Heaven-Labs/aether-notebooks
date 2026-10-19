package api

import (
	"net/http"
	"time"
)

// longRunningWriteDeadline is the response write deadline applied to handlers
// that can legitimately block on a slow data-plane operation: an idle
// ClickHouse Cloud service waking (the native handshake may take minutes) or a
// long-running query. The HTTP server's 60s WriteTimeout otherwise cuts the
// response off before the run completes, even when the connector's
// timeout_seconds is 0 (unlimited).
//
// This is a transport guard, not a query budget: connector timeout_seconds
// still governs the query itself.
const longRunningWriteDeadline = 30 * time.Minute

// allowLongRunningResponse replaces the server's WriteTimeout for the current
// request. A ResponseWriter without deadline support keeps the server
// baseline; the error is intentionally ignored because the request can still
// proceed.
func allowLongRunningResponse(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(longRunningWriteDeadline))
}
