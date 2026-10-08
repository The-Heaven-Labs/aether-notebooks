package api

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// connectorHealthWriteTimeout bounds the detached health write. Recording is
// best-effort: a slow or unavailable database must never block or fail the
// request that triggered it.
const connectorHealthWriteTimeout = 2 * time.Second

// maxConnectorErrorChars bounds the persisted last_error text.
const maxConnectorErrorChars = 500

// recordConnectorSuccess stamps last_success_at, debounced to at most one
// write per 30 seconds per connector (D8), with one exception: a success that
// is newer than the most recent failure always lands, so a recovery (success →
// failure → success within the debounce window) is never masked and the
// derived status flips back to connected immediately. The write is detached
// from ctx so a cancelled or timed-out execution context cannot skip it.
func (s *Server) recordConnectorSuccess(ctx context.Context, orgID, connectorID string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectorHealthWriteTimeout)
	defer cancel()
	if _, err := s.db.Pool.Exec(writeCtx,
		`UPDATE connectors SET last_success_at = NOW()
		 WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
		   AND (last_success_at IS NULL
		        OR last_success_at < NOW() - INTERVAL '30 seconds'
		        OR (last_failure_at IS NOT NULL AND (last_success_at IS NULL OR last_failure_at > last_success_at)))`,
		connectorID, orgID,
	); err != nil {
		slog.Warn("connector health: record success", "connector_id", connectorID, "error", err)
	}
}

// recordConnectorFailure stamps last_failure_at and last_error immediately
// (D8). last_success_at is left alone: status derivation compares the two
// timestamps on read (D6).
func (s *Server) recordConnectorFailure(ctx context.Context, orgID, connectorID string, failure error) {
	if failure == nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectorHealthWriteTimeout)
	defer cancel()
	if _, err := s.db.Pool.Exec(writeCtx,
		`UPDATE connectors SET last_failure_at = NOW(), last_error = $3
		 WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL`,
		connectorID, orgID, truncateConnectorError(failure.Error()),
	); err != nil {
		slog.Warn("connector health: record failure", "connector_id", connectorID, "error", err)
	}
}

// recordConnectorActivity is the agent/MCP-facing adapter: one callback, two
// outcomes. ok=true debounces a success write; ok=false is an immediate
// failure write. Wired onto agent.Engine and fulfilled for MCP by mcp.go.
func (s *Server) recordConnectorActivity(ctx context.Context, orgID, connectorID string, ok bool, errMsg string) {
	if ok {
		s.recordConnectorSuccess(ctx, orgID, connectorID)
		return
	}
	if errMsg == "" {
		errMsg = "connection failed"
	}
	s.recordConnectorFailure(ctx, orgID, connectorID, errors.New(errMsg))
}

// truncateConnectorError caps persisted error text without splitting a
// multibyte rune.
func truncateConnectorError(msg string) string {
	runes := []rune(msg)
	if len(runes) <= maxConnectorErrorChars {
		return msg
	}
	return string(runes[:maxConnectorErrorChars])
}
