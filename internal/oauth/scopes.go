// Package oauth implements the server side of the MCP Authorization spec:
// OAuth 2.1 primitives (PKCE, public-client registration rules) and the
// scope model that narrows Aether's MCP tool surface.
package oauth

import (
	"net/url"
	"strings"
)

// Scopes granted to MCP clients. mcp:query is the data-plane scope; mcp:read
// and mcp:write mirror the platform read/write tool split.
const (
	ScopeQuery = "mcp:query"
	ScopeRead  = "mcp:read"
	ScopeWrite = "mcp:write"
)

// AllScopes is the full set of grantable scopes.
var AllScopes = []string{ScopeQuery, ScopeRead, ScopeWrite}

// scopeTools maps each scope to the MCP tool names it unlocks. The MCP
// allowlist (internal/api/mcp.go) remains the outer boundary; scopes can
// only narrow it.
var scopeTools = map[string][]string{
	ScopeQuery: {"execute_sql", "explore_schema", "list_connectors"},
	ScopeRead: {
		"list_notebooks", "list_cells", "get_notebook_context",
		"list_snapshots", "list_notebook_parameters", "list_dashboards",
		"get_dashboard", "list_skills", "load_skill", "list_agents",
		"list_connectors", "list_folders", "get_folder_tree",
		"read_permissions",
	},
	ScopeWrite: {
		"create_notebook", "delete_notebook", "update_notebook",
		"read_cell", "create_cell", "update_cell", "run_cell", "move_cell",
		"swap_cells", "delete_cell", "create_snapshot", "restore_snapshot",
		"set_notebook_parameters", "create_dashboard", "update_dashboard",
		"delete_dashboard", "create_dashboard_widget", "update_dashboard_widget",
		"delete_dashboard_widget", "create_schedule", "delete_schedule",
		"share_dashboard", "update_permissions", "export_notebook",
		"import_notebook", "create_skill", "update_skill", "create_chart",
		"update_chart",
	},
}

// ParseScopes splits a space-separated scope string (OAuth wire format),
// deduplicating while preserving first-seen order.
func ParseScopes(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Fields(raw)
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		if !seen[p] {
			out = append(out, p)
			seen[p] = true
		}
	}
	return out
}

// NormalizeScopes filters a requested scope list down to grantable scopes,
// preserving request order.
func NormalizeScopes(requested []string) []string {
	known := map[string]bool{}
	for _, s := range AllScopes {
		known[s] = true
	}
	out := make([]string, 0, len(requested))
	for _, s := range requested {
		if known[s] {
			out = append(out, s)
		}
	}
	return out
}

// ToolsForScopes returns the set of tool names unlocked by the scopes.
func ToolsForScopes(scopes []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, s := range scopes {
		for _, t := range scopeTools[s] {
			out[t] = struct{}{}
		}
	}
	return out
}

// ValidateRedirectURI enforces the MCP security requirement that redirect
// URIs are HTTPS or loopback HTTP (localhost / 127.0.0.1, any port) with no
// fragment. Exact-match comparison happens at authorize/token time.
func ValidateRedirectURI(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Fragment != "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}
