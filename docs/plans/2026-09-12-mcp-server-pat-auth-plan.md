# MCP Server for External Harnesses (PAT Auth) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Aether's `POST /api/v1/mcp` endpoint a supported integration surface for external MCP harnesses (opencode, Claude Code, etc.), authenticated with personal access tokens and exposing a curated 45-tool allowlist.

**Architecture:** The existing JSON-RPC handler in `internal/api/mcp.go` stays the transport. We add protocol compliance (version negotiation, notifications, ping, panic recovery, `MCP-Protocol-Version` validation), explicit `405` for the unsupported SSE/session methods, an RFC 6750 `WWW-Authenticate` challenge on `401`, and a central allowlist in `mcp.go` that gates both `tools/list` and `tools/call`. No new dependencies, no schema changes, no tool-definition changes.

**Tech Stack:** Go 1.25 stdlib `net/http` ServeMux, existing JWT/PAT middleware, `testify/require` tests against the real dev database.

**Design doc:** `docs/plans/2026-09-12-mcp-server-pat-auth-design.md`

---

## Preconditions

- Work on branch `feat/mcp-server-pat-auth` (design doc already committed there).
- Start infrastructure once: `task infra:up`
- Run the targeted tests with: `go test ./internal/api/ -run 'TestMCP' -v` (tests hit the real Postgres from `task infra:up`).

---

### Task 1: 401 challenge and 405 for GET/DELETE

**Files:**
- Modify: `internal/api/helpers.go:14-16`
- Modify: `internal/api/router.go:482-483`
- Modify: `internal/api/mcp.go` (add `handleMCPNoStream` near the bottom)
- Test: `internal/api/mcp_protocol_test.go` (new file)

**Step 1: Write the failing tests**

Create `internal/api/mcp_protocol_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// doMCPRequest posts a JSON-RPC request to the MCP endpoint and returns the
// status, decoded body, and response headers. token is the raw bearer value.
func doMCPRequest(t *testing.T, srv http.Handler, token, method string, params any) (int, map[string]any, http.Header) {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		payload["params"] = params
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	return rec.Code, resp, rec.Header()
}

func TestMCPUnauthenticatedReturns401WithChallenge(t *testing.T) {
	srv := setupTestServer(t)

	code, _, header := doMCPRequest(t, srv, "", "tools/list", nil)
	require.Equal(t, http.StatusUnauthorized, code)
	require.Contains(t, header.Get("WWW-Authenticate"), `Bearer realm="aether"`)
}

func TestMCPGetAndDeleteReturn405(t *testing.T) {
	srv := setupTestServer(t)

	for _, method := range []string{"GET", "DELETE"} {
		req := httptest.NewRequest(method, "/api/v1/mcp", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code, "%s should be 405", method)
		require.Equal(t, "POST", rec.Header().Get("Allow"))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestMCPUnauthenticatedReturns401WithChallenge|TestMCPGetAndDeleteReturn405' -v`
Expected: FAIL — 401 has no `WWW-Authenticate`; `GET`/`DELETE` hit the SPA catch-all and return 404.

**Step 3: Implement**

`internal/api/helpers.go` — add the challenge header on every 401:

```go
func writeError(w http.ResponseWriter, status int, msg string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="aether"`)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}
```

`internal/api/mcp.go` — append:

```go
// handleMCPNoStream answers non-POST methods on the MCP endpoint. Aether does
// not offer the optional SSE stream or sessions from the Streamable HTTP
// transport, so GET/DELETE return 405 as permitted by the MCP spec.
func handleMCPNoStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST")
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
		"error": "method not allowed: Aether's MCP endpoint is POST-only (no SSE stream or sessions)",
	})
}
```

`internal/api/router.go` — replace the MCP route block (lines 482-483):

```go
	// MCP protocol endpoint (exposes built-in tools via Model Context Protocol)
	s.mux.Handle("POST /api/v1/mcp", authMW(http.HandlerFunc(s.handleMCP)))
	// Streamable HTTP clients probe GET for an SSE stream; Aether has none.
	// Register explicit 405s so the SPA catch-all never answers these.
	s.mux.Handle("GET /api/v1/mcp", http.HandlerFunc(handleMCPNoStream))
	s.mux.Handle("DELETE /api/v1/mcp", http.HandlerFunc(handleMCPNoStream))
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestMCPUnauthenticatedReturns401WithChallenge|TestMCPGetAndDeleteReturn405' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/helpers.go internal/api/mcp.go internal/api/router.go internal/api/mcp_protocol_test.go
git commit -m "feat(mcp): 401 bearer challenge and explicit 405 for non-POST methods"
```

---

### Task 2: Protocol negotiation, notifications, ping, panic recovery, version header

**Files:**
- Modify: `internal/api/mcp.go` (imports, `handleMCP`, `handleMCPInitialize`, new helpers)
- Test: `internal/api/mcp_protocol_test.go` (append)

**Step 1: Write the failing tests**

Append to `internal/api/mcp_protocol_test.go` (add imports `encoding/json` already present; add `log/slog` not needed in test; add `time` and `github.com/the-heaven-labs/aether/internal/agent`):

```go
func TestMCPInitializeProtocolVersion(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	tests := []struct{ requested, expected string }{
		{"2025-06-18", "2025-06-18"},
		{"2025-11-25", "2025-11-25"},
		{"2026-07-28", "2026-07-28"},
		{"1999-01-01", "2026-07-28"},
		{"", "2026-07-28"},
	}
	for _, tt := range tests {
		params := map[string]any{}
		if tt.requested != "" {
			params["protocolVersion"] = tt.requested
		}
		code, resp, _ := doMCPRequest(t, srv, token, "initialize", params)
		require.Equal(t, http.StatusOK, code, "requested %q", tt.requested)
		result, _ := resp["result"].(map[string]any)
		require.NotNil(t, result)
		require.Equal(t, tt.expected, result["protocolVersion"], "requested %q", tt.requested)
	}
}

func TestMCPInitializedNotificationReturns202(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	code, _, _ := doMCPRequest(t, srv, token, "notifications/initialized", map[string]any{})
	require.Equal(t, http.StatusAccepted, code)
}

func TestMCPPing(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	code, resp, _ := doMCPRequest(t, srv, token, "ping", nil)
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, resp["result"], "ping must return an empty result object")
}

func TestMCPProtocolVersionHeader(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	do := func(version string) int {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
		req := httptest.NewRequest("POST", "/api/v1/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if version != "" {
			req.Header.Set("MCP-Protocol-Version", version)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusBadRequest, do("1999-01-01"))
	require.Equal(t, http.StatusOK, do("2026-07-28"))
	require.Equal(t, http.StatusOK, do(""))
}

func TestMCPPanicIsRecovered(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	probe := &agent.ToolDef{Timeout: time.Second}
	probe.Function.Name = "probe_mcp_panic"
	probe.Function.Parameters = `{"type":"object","properties":{}}`
	probe.Handler = func(_ json.RawMessage, _ *agent.ToolContext) (any, error) {
		panic("boom")
	}
	srv.RegisterToolForTest(probe)

	code, resp, _ := doMCPRequest(t, srv, token, "tools/call", map[string]any{
		"name": "probe_mcp_panic", "arguments": map[string]any{},
	})
	require.Equal(t, http.StatusInternalServerError, code)
	errObj, _ := resp["error"].(map[string]any)
	require.NotNil(t, errObj, "expected JSON-RPC error body, got %v", resp)
	require.Equal(t, float64(-32603), errObj["code"])
}
```

Note: `TestMCPPanicIsRecovered` works without allowlist support because Task 3 is not merged yet. Task 3 updates it.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestMCPInitializeProtocolVersion|TestMCPInitializedNotificationReturns202|TestMCPPing|TestMCPProtocolVersionHeader|TestMCPPanicIsRecovered' -v`
Expected: FAIL — initialize always returns `2025-06-18`; `notifications/initialized` returns `-32601` with 200; ping returns `-32601`; unsupported version header is ignored; panic drops the connection (empty/500 without the `-32603` body).

**Step 3: Implement**

`internal/api/mcp.go` — update imports to:

```go
import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/auth"
)
```

Add the protocol-version helpers after the `mcpTool` struct:

```go
const mcpLatestProtocolVersion = "2026-07-28"

var mcpSupportedProtocolVersions = []string{"2025-06-18", "2025-11-25", "2026-07-28"}

func mcpSupportsProtocolVersion(v string) bool {
	for _, supported := range mcpSupportedProtocolVersions {
		if supported == v {
			return true
		}
	}
	return false
}
```

Replace `handleMCP` with:

```go
// handleMCP serves the MCP (Model Context Protocol) endpoint over HTTP.
// Authenticated via Bearer token (personal access token or JWT).
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("mcp handler panic", "panic", rec)
			writeJSON(w, http.StatusInternalServerError, mcpJSONRPCResponse{
				JSONRPC: "2.0", ID: nil,
				Error: &mcpError{Code: -32603, Message: "Internal error"},
			})
		}
	}()

	var req mcpJSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: nil,
			Error: &mcpError{Code: -32700, Message: "Parse error"},
		})
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSON(w, http.StatusBadRequest, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32600, Message: "Invalid Request: must use jsonrpc 2.0"},
		})
		return
	}

	if req.Method == "initialize" {
		s.handleMCPInitialize(w, req)
		return
	}

	// MCP clients send the negotiated version on every request after
	// initialize. Reject unknown versions; a missing header is accepted for
	// older clients.
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !mcpSupportsProtocolVersion(v) {
		writeJSON(w, http.StatusBadRequest, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32600, Message: "Unsupported MCP-Protocol-Version: " + v},
		})
		return
	}

	// JSON-RPC notifications carry no id and expect an empty 202 response.
	if strings.HasPrefix(req.Method, "notifications/") {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "ping":
		writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]interface{}{},
		})
	case "tools/list":
		s.handleMCPToolsList(w, req, claims)
	case "tools/call":
		s.handleMCPToolsCall(w, req, claims, r)
	default:
		writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32601, Message: "Method not found: " + req.Method},
		})
	}
}
```

Replace the response body of `handleMCPInitialize` with:

```go
	protocolVersion := mcpLatestProtocolVersion
	if mcpSupportsProtocolVersion(params.ProtocolVersion) {
		protocolVersion = params.ProtocolVersion
	}

	serverVersion := s.version
	if serverVersion == "" {
		serverVersion = "dev"
	}

	writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: map[string]interface{}{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    "aether",
				"version": serverVersion,
			},
		},
	})
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestMCPInitializeProtocolVersion|TestMCPInitializedNotificationReturns202|TestMCPPing|TestMCPProtocolVersionHeader|TestMCPPanicIsRecovered' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/mcp.go internal/api/mcp_protocol_test.go
git commit -m "feat(mcp): protocol version negotiation, notifications, ping, panic recovery"
```

---

### Task 3: Curated tool allowlist

**Files:**
- Modify: `internal/api/mcp.go` (add `mcpToolAllowlist`, `mcpToolAllowed`, filter in `handleMCPToolsList` and `handleMCPToolsCall`)
- Modify: `internal/api/export_test.go` (test accessors)
- Modify: `internal/api/mcp_tools_test.go` (allow the timeout probe)
- Modify: `internal/api/mcp_protocol_test.go` (allow the panic probe)
- Test: `internal/api/mcp_protocol_test.go` (append allowlist tests)

**Step 1: Write the failing tests**

Append to `internal/api/mcp_protocol_test.go` (add `sort` import):

```go
func TestMCPToolsListMatchesAllowlist(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	code, resp, _ := doMCPRequest(t, srv, token, "tools/list", nil)
	require.Equal(t, http.StatusOK, code)
	result, _ := resp["result"].(map[string]any)
	require.NotNil(t, result)
	tools, _ := result["tools"].([]any)

	got := make([]string, 0, len(tools))
	for _, raw := range tools {
		m, _ := raw.(map[string]any)
		name, _ := m["name"].(string)
		got = append(got, name)
	}
	expected := srv.MCPToolAllowlistForTest()
	sort.Strings(got)
	sort.Strings(expected)
	require.Equal(t, expected, got, "tools/list must match the allowlist exactly (renames or missing definitions will show as diffs)")
}

func TestMCPUnlistedToolIsNotExposedOrCallable(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	probe := &agent.ToolDef{Timeout: time.Second}
	probe.Function.Name = "probe_mcp_unlisted"
	probe.Function.Parameters = `{"type":"object","properties":{}}`
	probe.Handler = func(_ json.RawMessage, _ *agent.ToolContext) (any, error) {
		return map[string]any{"ran": true}, nil
	}
	srv.RegisterToolForTest(probe)

	_, resp, _ := doMCPRequest(t, srv, token, "tools/list", nil)
	result, _ := resp["result"].(map[string]any)
	for _, raw := range result["tools"].([]any) {
		m, _ := raw.(map[string]any)
		require.NotEqual(t, "probe_mcp_unlisted", m["name"])
	}

	_, resp, _ = doMCPRequest(t, srv, token, "tools/call", map[string]any{
		"name": "probe_mcp_unlisted", "arguments": map[string]any{},
	})
	errObj, _ := resp["error"].(map[string]any)
	require.NotNil(t, errObj, "unlisted tool must be rejected, got %v", resp)
	require.Equal(t, float64(-32602), errObj["code"])
}
```

Update the existing probe tests so they opt their probe into the allowlist. In `internal/api/mcp_tools_test.go`, inside `TestMCPToolsCallToolTimeout`, immediately after `srv.RegisterToolForTest(probe)` add:

```go
	srv.SetMCPToolAllowedForTest("probe_mcp_timeout", true)
	t.Cleanup(func() { srv.SetMCPToolAllowedForTest("probe_mcp_timeout", false) })
```

In `internal/api/mcp_protocol_test.go`, inside `TestMCPPanicIsRecovered`, immediately after `srv.RegisterToolForTest(probe)` add:

```go
	srv.SetMCPToolAllowedForTest("probe_mcp_panic", true)
	t.Cleanup(func() { srv.SetMCPToolAllowedForTest("probe_mcp_panic", false) })
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestMCPToolsListMatchesAllowlist|TestMCPUnlistedToolIsNotExposedOrCallable' -v`
Expected: FAIL to compile — `srv.MCPToolAllowlistForTest` and `srv.SetMCPToolAllowedForTest` are undefined.

**Step 3: Implement**

`internal/api/export_test.go` — append:

```go
// MCPToolAllowlistForTest exposes the curated MCP catalog so external tests can
// assert tools/list matches it exactly.
func (s *Server) MCPToolAllowlistForTest() []string {
	names := make([]string, 0, len(mcpToolAllowlist))
	for name := range mcpToolAllowlist {
		names = append(names, name)
	}
	return names
}

// SetMCPToolAllowedForTest temporarily adds or removes a tool from the MCP
// allowlist so dispatch-path tests can use probe tools.
func (s *Server) SetMCPToolAllowedForTest(name string, allowed bool) {
	if allowed {
		mcpToolAllowlist[name] = struct{}{}
	} else {
		delete(mcpToolAllowlist, name)
	}
}
```

`internal/api/mcp.go` — add after `mcpSupportsProtocolVersion`:

```go
// mcpToolAllowlist is the curated catalog exposed over MCP. It is deliberately
// an allowlist: adding a tool to the registry must never expose it externally
// until it is added here and covered by TestMCPToolsListMatchesAllowlist.
var mcpToolAllowlist = map[string]struct{}{
	// Notebooks, cells & SQL
	"create_notebook":          {},
	"delete_notebook":          {},
	"update_notebook":          {},
	"read_cell":                {},
	"create_cell":              {},
	"update_cell":              {},
	"run_cell":                 {},
	"list_cells":               {},
	"move_cell":                {},
	"swap_cells":               {},
	"execute_sql":              {},
	"explore_schema":           {},
	"delete_cell":              {},
	"get_notebook_context":     {},
	"create_snapshot":          {},
	"list_snapshots":           {},
	"restore_snapshot":         {},
	"list_notebook_parameters": {},
	"set_notebook_parameters":  {},
	// Dashboards, schedules, permissions & import/export
	"create_dashboard":        {},
	"list_dashboards":         {},
	"get_dashboard":           {},
	"update_dashboard":        {},
	"delete_dashboard":        {},
	"create_dashboard_widget": {},
	"update_dashboard_widget": {},
	"delete_dashboard_widget": {},
	"create_schedule":         {},
	"delete_schedule":         {},
	"share_dashboard":         {},
	"read_permissions":        {},
	"update_permissions":      {},
	"export_notebook":         {},
	"import_notebook":         {},
	// Skills & agents (read/authoring only)
	"list_skills":  {},
	"load_skill":   {},
	"create_skill": {},
	"update_skill": {},
	"list_agents":  {},
	// Platform reads
	"list_notebooks":  {},
	"list_connectors": {},
	"list_folders":    {},
	"get_folder_tree": {},
	// Charts
	"create_chart": {},
	"update_chart": {},
}

func mcpToolAllowed(name string) bool {
	_, ok := mcpToolAllowlist[name]
	return ok
}
```

In `handleMCPToolsList`, change the loop guard:

```go
	for _, d := range defs {
		if d.Function.Name == "" || !mcpToolAllowed(d.Function.Name) {
			continue
		}
```

In `handleMCPToolsCall`, insert before the registry lookup:

```go
	if !mcpToolAllowed(params.Name) {
		writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &mcpError{Code: -32602, Message: "Tool not available over MCP: " + params.Name},
		})
		return
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestMCP' -v`
Expected: PASS (including the updated timeout and panic probe tests). The allowlist-equality test also proves each name resolves in the registry: a renamed tool would be missing from `tools/list`.

**Step 5: Commit**

```bash
git add internal/api/mcp.go internal/api/export_test.go internal/api/mcp_tools_test.go internal/api/mcp_protocol_test.go
git commit -m "feat(mcp): expose a curated 45-tool allowlist"
```

---

### Task 4: End-to-end flow and PAT scoping tests

**Files:**
- Test: `internal/api/mcp_pat_test.go` (new file)

**Step 1: Write the tests**

Create `internal/api/mcp_pat_test.go`:

```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A PAT created after login drives the full notebook workflow over MCP.
func TestMCPEndToEndWithPAT(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-e2e-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "MCP E2E Org")
	connID := createConnector(t, srv, jwt)

	code, tokResp := doCreateToken(t, srv, jwt, "opencode", "")
	require.Equal(t, http.StatusCreated, code, "create PAT: %v", tokResp)
	pat := tokResp["token"].(string)

	// create_notebook
	code, resp, _ := doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "create_notebook", "arguments": map[string]any{"title": "MCP E2E"},
	})
	require.Equal(t, http.StatusOK, code)
	var nb map[string]any
	require.NoError(t, json.Unmarshal([]byte(mcpResultText(t, resp)), &nb))
	nbID, _ := nb["notebook_id"].(string)
	require.NotEmpty(t, nbID, "create_notebook result: %v", nb)

	// create_cell
	code, resp, _ = doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "create_cell",
		"arguments": map[string]any{
			"notebook_id": nbID, "type": "code", "language": "sql",
			"title": "One", "description": "test cell",
			"source": "SELECT 1 AS x", "connector_id": connID,
		},
	})
	require.Equal(t, http.StatusOK, code)
	var cell map[string]any
	require.NoError(t, json.Unmarshal([]byte(mcpResultText(t, resp)), &cell))
	cellID, _ := cell["cell_id"].(string)
	require.NotEmpty(t, cellID, "create_cell result: %v", cell)

	// run_cell
	code, resp, _ = doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "run_cell", "arguments": map[string]any{"cell_id": cellID},
	})
	require.Equal(t, http.StatusOK, code)
	text := mcpResultText(t, resp)
	require.Contains(t, text, `"status":"completed"`)
	require.Contains(t, text, `"column_names":["x"]`)

	// list_cells sees the created cell through the same PAT
	code, resp, _ = doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "list_cells", "arguments": map[string]any{"notebook_id": nbID},
	})
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, mcpResultText(t, resp), cellID)
}

func TestMCPExpiredPATRejected(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-expired-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "MCP Expired Org")

	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	code, tokResp := doCreateToken(t, srv, jwt, "expired-mcp", past)
	require.Equal(t, http.StatusCreated, code, "create expired PAT: %v", tokResp)
	pat := tokResp["token"].(string)

	code, _, _ = doMCPRequest(t, srv, pat, "tools/list", nil)
	require.Equal(t, http.StatusUnauthorized, code, "expired PAT must be rejected")
}

// A PAT scoped to org A used against org B's subdomain is rejected by the
// existing subdomain/token check, before any tool logic runs.
func TestMCPPATWrongOrgSubdomainRejected(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-wrong-org-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "MCP Org A")

	code, tokResp := doCreateToken(t, srv, jwt, "wrong-org", "")
	require.Equal(t, http.StatusCreated, code, "create PAT: %v", tokResp)
	pat := tokResp["token"].(string)

	slug := fmt.Sprintf("mcp-other-%d", time.Now().UnixNano())
	_, err := srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO orgs (name, slug) VALUES ($1, $2)`, slug, slug)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	req := httptest.NewRequest("POST", "/api/v1/mcp", bytes.NewReader(body))
	req.Host = slug + ".aether.test"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+pat)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "PAT must not cross orgs: %s", rec.Body.String())
}
```

**Step 2: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestMCPEndToEndWithPAT|TestMCPExpiredPATRejected|TestMCPPATWrongOrgSubdomainRejected' -v`
Expected: PASS. If `TestMCPEndToEndWithPAT` fails on a shape mismatch, print the actual result text and adjust the field names (`notebook_id`, `cell_id`).

**Step 3: Commit**

```bash
git add internal/api/mcp_pat_test.go
git commit -m "test(mcp): end-to-end PAT workflow and org scoping"
```

---

### Task 5: Harness documentation

**Files:**
- Create: `docs/mcp.md`

**Step 1: Write the doc**

Create `docs/mcp.md` with this structure (fill in the exact allowlist from `internal/api/mcp.go`):

````markdown
# Using Aether from External MCP Harnesses

Aether exposes its built-in agent tools over the Model Context Protocol (MCP) at:

```
{AETHER_PUBLIC_URL}/api/v1/mcp
```

The endpoint speaks MCP JSON-RPC over Streamable HTTP: `POST` only, plain JSON
responses, no SSE stream, no sessions. On an org subdomain (e.g.
`https://org1.aether.example.com/api/v1/mcp`) the token's org is additionally
checked against the subdomain.

## 1. Create a personal access token

1. Log in to Aether with your password **or SSO**.
2. Open your profile menu → **Tokens** (or run `aether tokens create --name opencode`).
3. Give it a clear name and, ideally, an expiry date.

The raw token starts with `aether_tok_` and is shown only once. It grants the
same permissions as your account — tool calls are ACL-checked as you.

## 2. Configure your harness

### opencode

```jsonc
{
  "mcp": {
    "aether": {
      "type": "remote",
      "url": "https://aether.example.com/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer aether_tok_REPLACE_ME"
      }
    }
  }
}
```

### Claude Code

```bash
claude mcp add --transport http aether https://aether.example.com/api/v1/mcp \
  --header "Authorization: Bearer aether_tok_REPLACE_ME"
```

## 3. Verify with curl

```bash
curl -s https://aether.example.com/api/v1/mcp \
  -H "Authorization: Bearer $AETHER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools[].name'
```

## Exposed tools

The endpoint exposes a fixed allowlist (new internal tools are not exposed
automatically). Include the full list generated from `mcpToolAllowlist`.

Interactive and agent-session tools (`ask_question`, `spawn_subagents`,
`get_subagent_results`, `create_tasks`, `update_task`, `get_tasks`,
`update_agent`) and dynamic `webhook`/`sql_query` tools are **not** exposed.

## Permissions and errors

- Calls run as the token's user; ACLs are enforced per tool call.
- Tool execution failures return `result.isError = true` with a text message.
- Unknown tools return JSON-RPC `-32602`.
- Missing/expired/revoked tokens return HTTP `401` with
  `WWW-Authenticate: Bearer realm="aether"`.
- `GET`/`DELETE` return `405` (no SSE, no sessions).

## Security

- Treat a PAT like your password: don't share it, set an expiry, and revoke it
  (delete the token) when a harness no longer needs it.
- OAuth 2.1 onboarding (browser consent, no manual token) is planned but not in
  this phase; harnesses must be configured with the static header today.
````

**Step 2: Commit**

```bash
git add docs/mcp.md
git commit -m "docs(mcp): harness setup guide for opencode, Claude Code, and curl"
```

---

### Task 6: Full verification

**Step 1: Run the full Go checks**

Run: `task check`
Expected: PASS (gofmt, go vet, go mod tidy, all Go tests).

**Step 2: Run the MCP suite once more explicitly**

Run: `go test ./internal/api/ -run 'TestMCP' -v`
Expected: PASS for every MCP test.

**Step 3: Manual smoke test (optional but recommended)**

Against the dev stack, create a PAT with the CLI, then:

```bash
AETHER_TOKEN=$(aether tokens create --name mcp-smoke | jq -r .token)
curl -s http://localhost:8088/api/v1/mcp \
  -H "Authorization: Bearer $AETHER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}' | jq .
curl -s http://localhost:8088/api/v1/mcp \
  -H "Authorization: Bearer $AETHER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | jq '.result.tools | length'
```

Expected: initialize returns `protocolVersion: "2026-07-28"`; tools/list returns `45`.

**Step 4: Commit any fixes**

```bash
git status --short
git add -A
git commit -m "chore(mcp): verification fixes"
```

Only commit if Step 1-3 produced changes.
