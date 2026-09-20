package executor

import "context"

// contextKey is unexported so only this package can produce values of the type.
type contextKey string

const adminModeKey contextKey = "aether_admin_mode"

// WithAdminMode returns a context carrying the admin-mode flag. Admin mode
// gates the org-admin ACL bypass during execution-target resolution, and the
// flag lives here (rather than in internal/api) so agent and MCP execution
// paths — which cannot import internal/api — can install the same signal before
// invoking a server-wired target resolver.
func WithAdminMode(ctx context.Context, enabled bool) context.Context {
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
