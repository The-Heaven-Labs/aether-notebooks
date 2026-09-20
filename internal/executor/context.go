package executor

import "context"

// contextKey is unexported so only this package can produce values of the type.
type contextKey string

const adminModeKey contextKey = "aether_admin_mode"

// executionIDKey carries the per-execution ID used to tag ClickHouse queries
// (log_comment) and to correlate them with Aether audit entries.
const executionIDKey contextKey = "aether_execution_id"

// WithExecutionID returns a context carrying the per-execution ID used for
// query tagging. A nil ctx is treated as a fresh background context.
func WithExecutionID(ctx context.Context, executionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, executionIDKey, executionID)
}

// ExecutionIDFromContext returns the execution ID carried by ctx, or "" when
// the context has none. A missing or non-string value defaults to empty, so an
// unstamped context never tags a query.
func ExecutionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	executionID, _ := ctx.Value(executionIDKey).(string)
	return executionID
}

// WithAdminMode returns a context carrying the admin-mode flag. Admin mode
// gates the org-admin ACL bypass during execution-target resolution, and the
// flag lives here (rather than in internal/api) so agent and MCP execution
// paths — which cannot import internal/api — can install the same signal before
// invoking a server-wired target resolver. A nil ctx is treated as a fresh
// background context.
func WithAdminMode(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, adminModeKey, enabled)
}

// AdminModeFromContext reports whether the context carries an enabled
// admin-mode flag. A missing or non-bool value defaults to false, so an
// unstamped context never gains the ACL bypass.
func AdminModeFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(adminModeKey).(bool)
	return enabled
}
