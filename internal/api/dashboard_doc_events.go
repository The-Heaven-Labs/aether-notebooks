package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// dashboardDocChannelPrefix is the Redis pub/sub channel prefix carrying
// backend-originated Yjs updates for dashboard documents. The Hocuspocus relay
// replicas psubscribe "aether:dashboard-doc:*" and apply a received update to
// the document when that replica has it loaded (idempotent; a replica without
// the doc skips it because Postgres is already current).
const dashboardDocChannelPrefix = "aether:dashboard-doc:"

// dashboardDocInvalidatePrefix is the Redis pub/sub channel prefix carrying
// dashboard-document invalidation notices (trash/purge). The relay replicas
// psubscribe "aether:dashboard-doc-invalidate:*" and disconnect viewers and
// unload the document so a stale in-memory copy cannot outlive the row.
const dashboardDocInvalidatePrefix = "aether:dashboard-doc-invalidate:"

// dashboardDocPublishTimeout bounds one publish attempt. Fan-out is additive
// on top of the durable document state, so a Redis outage must fail open
// quickly instead of blocking the request path.
const dashboardDocPublishTimeout = time.Second

// dashboardDocInvalidatePayload is the JSON body of one invalidation notice.
// The field (rather than a bare string) leaves room for growth without
// breaking a rolling deploy.
type dashboardDocInvalidatePayload struct {
	Reason string `json:"reason"`
}

// dashboardDocChannel returns the update channel for one dashboard.
func dashboardDocChannel(dashboardID string) string {
	return dashboardDocChannelPrefix + dashboardID
}

// dashboardDocInvalidateChannel returns the invalidation channel for one
// dashboard.
func dashboardDocInvalidateChannel(dashboardID string) string {
	return dashboardDocInvalidatePrefix + dashboardID
}

// publishDashboardDocUpdate broadcasts backend-originated Yjs state bytes to
// every relay replica. It is best-effort: the caller has already persisted and
// materialized the new state, so a Redis failure is logged and swallowed and
// never fails the request. The payload is raw Yjs update bytes and is
// published as-is. A nil cache (no Redis configured), empty dashboard ID, or
// zero-length update skips the broadcast entirely.
func (s *Server) publishDashboardDocUpdate(ctx context.Context, dashboardID string, update []byte) {
	// A zero-length update is not valid Yjs state (Y.applyUpdate throws on the
	// relay), so it is never fanned out.
	if s.Cache == nil || dashboardID == "" || len(update) == 0 {
		return
	}
	// Detach from the caller's cancellation so a client disconnect cannot skip
	// the fan-out, then bound the publish so Redis cannot stall the caller.
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dashboardDocPublishTimeout)
	defer cancel()
	if err := s.Cache.Client().Publish(pubCtx, dashboardDocChannel(dashboardID), update).Err(); err != nil {
		slog.Warn("dashboard doc update publish failed",
			"error", err, "dashboard_id", dashboardID)
	}
}

// publishDashboardDocInvalidate broadcasts an invalidation notice (e.g. reason
// "trashed" or "purged") to every relay replica. Like
// publishDashboardDocUpdate it is best-effort: a Redis failure is logged and
// swallowed and never fails the caller. A nil cache or empty dashboard ID
// skips the broadcast entirely.
func (s *Server) publishDashboardDocInvalidate(ctx context.Context, dashboardID, reason string) {
	if s.Cache == nil || dashboardID == "" {
		return
	}
	payload, err := json.Marshal(dashboardDocInvalidatePayload{Reason: reason})
	if err != nil {
		slog.Warn("dashboard doc invalidate marshal failed",
			"error", err, "dashboard_id", dashboardID, "reason", reason)
		return
	}
	// Detach from the caller's cancellation so a client disconnect cannot skip
	// the fan-out, then bound the publish so Redis cannot stall the caller.
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dashboardDocPublishTimeout)
	defer cancel()
	if err := s.Cache.Client().Publish(pubCtx, dashboardDocInvalidateChannel(dashboardID), payload).Err(); err != nil {
		slog.Warn("dashboard doc invalidate publish failed",
			"error", err, "dashboard_id", dashboardID, "reason", reason)
	}
}

// PublishDashboardDocInvalidate broadcasts a "purged" invalidation notice for a
// dashboard that was hard-deleted outside a request path — the scheduler's
// trash purge. Exported for the cmd/aether-server wiring (the scheduler is
// constructed before the Server, so it is set through
// Scheduler.SetTrashInvalidator). Best-effort like the unexported publisher.
func (s *Server) PublishDashboardDocInvalidate(ctx context.Context, dashboardID string) {
	s.publishDashboardDocInvalidate(ctx, dashboardID, "purged")
}
