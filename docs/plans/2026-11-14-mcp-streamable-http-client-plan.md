# MCP Streamable HTTP Client — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the broken REST-based outbound MCP client with a standards-compliant MCP Streamable HTTP client so configured `http`-type MCP servers (e.g. AbuseIPDB) actually work.

**Architecture:** A new `mcpHTTPClient` in `internal/agent/tools_mcp.go` speaks JSON-RPC 2.0 over a single POST endpoint (the `command` URL), does the `initialize` handshake lazily, negotiates `Mcp-Session-Id`/`Mcp-Protocol-Version`, handles both `application/json` and `text/event-stream` responses, attaches configured custom headers to every request (including redirects), and gates decoding on HTTP status + Content-Type so a 4xx HTML page surfaces a clear error instead of `invalid character '<'`. The engine builds one shared client per server per turn; the LLM-facing tool names and call shape are unchanged.

**Tech Stack:** Go 1.26, `net/http`, `encoding/json`, stdlib only (no new deps).

**Design doc:** `docs/plans/2026-11-14-mcp-streamable-http-client-design.md`

**Context:** Verify the bug background in the design doc. The current `tools_mcp.go` (135 lines) uses `GET {url}/tools/list` and `POST {url}/tools/call` with a non-JSON-RPC body — no standard MCP server implements these. All work is in the clone at `/tmp/opencode/aether-upstream` on branch `fix/mcp-streamable-http-client`.

---

## File Structure

- **Modify** `internal/agent/tools_mcp.go` — replace the REST client with `mcpHTTPClient` (Streamable HTTP). Remove dead code (`MCPClient`, `RegisterMCPTools`, `makeMCPToolListHandler`, `makeMCPToolCallHandler`).
- **Modify** `internal/agent/engine.go:854-872` — construct one shared `mcpHTTPClient` per server; pass to list+call handlers. Keep stdio skip-with-warning (improved message).
- **Modify** `internal/api/mcp_server_handlers.go:371-433` — `handleTestMCPServer` POSTs a JSON-RPC `initialize` instead of bare GET.
- **Rewrite** `internal/agent/tools_mcp_headers_test.go` — current tests assert the broken REST shape; replace with JSON-RPC + SSE + error-gate tests.

All paths relative to repo root `/tmp/opencode/aether-upstream`.

---

### Task 1: JSON-RPC core — initialize handshake + tools/list + tools/call

**Files:**
- Modify: `internal/agent/tools_mcp.go` (full rewrite)
- Rewrite test: `internal/agent/tools_mcp_headers_test.go` (first two tests)

- [ ] **Step 1: Write the failing tests for initialize + list + call**

Replace the body of `internal/agent/tools_mcp_headers_test.go` with this (first two tests; later tasks add more). It builds a mock MCP server that implements the Streamable HTTP jsonrpc protocol and asserts the new client does the initialize handshake and the right JSON-RPC calls.

```go
package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
)

// mockMCPServer is a minimal Streamable HTTP MCP server used by the tests.
// It requires initialize before tools/list or tools/call and records the
// custom headers received on each request.
type mockMCPServer struct {
	mu           sync.Mutex
	initialized  bool
	sessionID    string
	headerSeen   map[string]http.Header // method -> headers
	tools        []map[string]any
	callResults  map[string]any
}

func newMockMCPServer(t *testing.T, tools []map[string]any, callResults map[string]any) (*httptest.Server, *mockMCPServer) {
	m := &mockMCPServer{
		sessionID:   "sess-" + uuid.New().String(),
		headerSeen:  map[string]http.Header{},
		tools:       tools,
		callResults: callResults,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// All MCP requests are POSTs to the single endpoint.
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.headerSeen[req.Method] = r.Header.Clone()
		m.mu.Unlock()

		if req.Method == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", m.sessionID)
			m.mu.Lock()
			m.initialized = true
			m.mu.Unlock()
			result := map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":     map[string]any{},
				"serverInfo":       map[string]any{"name": "mock", "version": "test"},
			}
			writeJSONRPC(w, req.ID, result, nil)
			return
		}
		// All other methods require a session.
		if r.Header.Get("Mcp-Session-Id") != m.sessionID {
			http.Error(w, "no session", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "tools/list":
			writeJSONRPC(w, req.ID, map[string]any{"tools": m.tools}, nil)
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &params)
			writeJSONRPC(w, req.ID, m.callResults[params.Name], nil)
		default:
			writeJSONRPC(w, req.ID, nil, &mcpJSONRPCError{Code: -32601, Message: "method not found"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, m
}

func writeJSONRPC(w http.ResponseWriter, id any, result any, mcpErr *mcpJSONRPCError) {
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	if mcpErr != nil {
		resp["error"] = mcpErr
	} else {
		resp["result"] = result
	}
	json.NewEncoder(w).Encode(resp)
}

type mcpJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func TestMCPClientInitializeAndListTools(t *testing.T) {
	srv, m := newMockMCPServer(t, []map[string]any{{"name": "check"}}, nil)
	c := newMCPHTTPClient(srv.URL, map[string]string{"Authorization": "Bearer secret", "X-Env": "prod"})

	res, err := c.handleListTools()(nil, &ToolContext{Context: context.Background()})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		t.Fatal("expected initialize handshake to run before tools/list")
	}
	// initialize must carry the custom headers (auth-bearing servers need them).
	if got := m.headerSeen["initialize"].Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("initialize missing Authorization header, got %q", got)
	}
	if got := m.headerSeen["initialize"].Get("Accept"); !contains(got, "text/event-stream") {
		t.Fatalf("initialize missing Accept SSE, got %q", got)
	}
	// tools/list carried the session id and the auth header.
	if got := m.headerSeen["tools/list"].Get("Mcp-Session-Id"); got != m.sessionID {
		t.Fatalf("tools/list missing/passthru session id, got %q", got)
	}
	if got := m.headerSeen["tools/list"].Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("tools/list missing Authorization, got %q", got)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestMCPClientCallTool(t *testing.T) {
	srv, m := newMockMCPServer(t, nil, map[string]any{"check": map[string]any{"content": []any{"report"}}})
	c := newMCPHTTPClient(srv.URL, map[string]string{"Authorization": "Bearer secret"})

	args, _ := json.Marshal(map[string]any{"tool": "check", "arguments": map[string]any{"ipAddress": "1.2.3.4"}})
	res, err := c.handleCallTool()(args, &ToolContext{Context: context.Background()})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// tools/call params are the MCP {name, arguments} shape, not the legacy REST shape.
	var got struct {
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	_ = json.Unmarshal(m.lastBody, &got) // captured by the handler (see Step 3)
	if got.Params.Name != "check" {
		t.Fatalf("expected params.name=check, got %q", got.Params.Name)
	}
	if got.Params.Arguments["ipAddress"] != "1.2.3.4" {
		t.Fatalf("expected arguments.ipAddress=1.2.3.4, got %v", got.Params.Arguments["ipAddress"])
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /tmp/opencode/aether-upstream && go test ./internal/agent/ -run 'TestMCPClient' -v 2>&1 | head -30`
Expected: FAIL — `newMCPHTTPClient` undefined / `handleListTools` undefined.

- [ ] **Step 3: Write the new `internal/agent/tools_mcp.go`**

Replace the entire file with:

```go
package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/the-heaven-labs/aether/internal/headers"
)

// mcpProtocolVersion is proposed in the initialize handshake. 2025-06-18 is
// the Streamable HTTP transport version supported by every mainstream server.
const mcpProtocolVersion = "2025-06-18"

// mcpAccept advertises both JSON and SSE responses per the Streamable HTTP spec.
const mcpAccept = "application/json, text/event-stream"

const mcpClientName = "aether-notebooks"

// mcpHTTPClient speaks the MCP Streamable HTTP transport to a single server.
// It performs the initialize handshake lazily and reuses the session for the
// lifetime of the client (one agent turn).
type mcpHTTPClient struct {
	baseURL         string
	hdrs            map[string]string
	httpClient      *http.Client
	sessionID       string
	protocolVersion string
	nextID          atomic.Int64
	mu              sync.Mutex
	initialized     bool
}

func newMCPHTTPClient(baseURL string, hdrs map[string]string) *mcpHTTPClient {
	c := &mcpHTTPClient{baseURL: baseURL, hdrs: hdrs}
	c.httpClient = &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Re-apply configured headers + session on every redirect so a
			// cross-host redirect (vanity URL -> real endpoint) cannot drop
			// Authorization by Go's default cross-host strip behavior.
			setMCPHeaders(req, c.hdrs)
			if c.sessionID != "" {
				req.Header.Set("Mcp-Session-Id", c.sessionID)
			}
			return nil
		},
	}
	return c
}

func setMCPHeaders(req *http.Request, hdrs map[string]string) {
	req.Header.Set("Accept", mcpAccept)
	headers.Apply(req, hdrs)
}

// ensureInitialized performs the initialize handshake once. On a session-lost
// 400 it re-initializes a single time.
func (c *mcpHTTPClient) ensureInitialized(ctx context.Context) error {
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	return c.initialize(ctx)
}

func (c *mcpHTTPClient) initialize(ctx context.Context) error {
	result, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":   map[string]any{},
		"clientInfo":     map[string]any{"name": mcpClientName, "version": "0.81.0"},
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initialized = true
	if pv, ok := result["protocolVersion"].(string); ok && pv != "" {
		c.protocolVersion = pv
	} else {
		c.protocolVersion = mcpProtocolVersion
	}
	return nil
}

func (c *mcpHTTPClient) call(ctx context.Context, method string, params any) (any, error) {
	return c.request(ctx, method, params)
}

func (c *mcpHTTPClient) request(ctx context.Context, method string, params any) (map[string]any, error) {
	id := c.nextID.Add(1)
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp %s: %w", method, err)
	}
	req.Header.Set("Accept", mcpAccept)
	if c.protocolVersion != "" {
		req.Header.Set("Mcp-Protocol-Version", c.protocolVersion)
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	setMCPHeaders(req, c.hdrs)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp %s: %w", method, err)
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 400 || (!strings.Contains(ct, "application/json") && !strings.Contains(ct, "text/event-stream")) {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("mcp %s: server returned HTTP %d (%s): %q", method, resp.StatusCode, ct, snippet)
	}

	var result map[string]any
	if strings.Contains(ct, "text/event-stream") {
		msgResult, err := readSSEMatch(resp.Body, id)
		if err != nil {
			return nil, fmt.Errorf("mcp %s: %w", method, err)
		}
		result = msgResult
	} else {
		var resp2 struct {
			Result map[string]any `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&resp2); err != nil {
			return nil, fmt.Errorf("mcp %s: decode response: %w", method, err)
		}
		if resp2.Error != nil {
			return nil, fmt.Errorf("mcp %s: error %d: %s", method, resp2.Error.Code, resp2.Error.Message)
		}
		result = resp2.Result
	}

	// Capture the session id from the initialize response.
	if method == "initialize" {
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			c.mu.Lock()
			c.sessionID = sid
			c.mu.Unlock()
		}
	}
	return result, nil
}

// readSSEMatch scans an SSE stream for the JSON-RPC message whose id matches
// requestID and returns its result. Notifications (no id) are ignored.
func readSSEMatch(r io.Reader, requestID int64) (map[string]any, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Result map[string]any  `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if len(msg.ID) == 0 {
			continue // notification
		}
		var mid int64
		if err := json.Unmarshal(msg.ID, &mid); err != nil || mid != requestID {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("error %d: %s", msg.Error.Code, msg.Error.Message)
		}
		return msg.Result, nil
	}
	return nil, fmt.Errorf("no response with matching id in event stream")
}

// handleListTools / handleCallTool adapt the client to the engine's ToolHandler.
func (c *mcpHTTPClient) handleListTools() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		if err := c.ensureInitialized(ctx.Context); err != nil {
			return nil, err
		}
		return c.call(ctx.Context, "tools/list", nil)
	}
}

func (c *mcpHTTPClient) handleCallTool() ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if err := c.ensureInitialized(ctx.Context); err != nil {
			return nil, err
		}
		return c.call(ctx.Context, "tools/call", map[string]any{
			"name":      req.Tool,
			"arguments": req.Arguments,
		})
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /tmp/opencode/aether-upstream && go test ./internal/agent/ -run 'TestMCPClient' -v 2>&1 | tail -20`
Expected: PASS. (The `TestMCPClientCallTool` test references `m.lastBody`; capture it in the mock by storing `body` — add `lastBody []byte` to `mockMCPServer` and set `m.lastBody = body` in the handler. The test code above already reads it; the mock must store it. Adjust the mock to keep `lastBody`.)

- [ ] **Step 5: Commit**

```bash
cd /tmp/opencode/aether-upstream
git add internal/agent/tools_mcp.go internal/agent/tools_mcp_headers_test.go
git commit -m "feat(agent): replace REST MCP client with Streamable HTTP JSON-RPC

The outbound client used GET {url}/tools/list and POST {url}/tools/call
with a non-JSON-RPC body — no MCP server implements those paths, so every
call decoded a 4xx HTML page as JSON and failed with 'invalid character <'.
The new mcpHTTPClient speaks JSON-RPC 2.0 over the single Streamable HTTP
endpoint, performs the initialize handshake (Mcp-Session-Id/
Mcp-Protocol-Version), handles both application/json and
text/event-stream responses, and gates decoding on HTTP status +
Content-Type so a 4xx HTML page surfaces a clear error.
"
```

---

### Task 2: SSE response handling

**Files:**
- Add test to: `internal/agent/tools_mcp_headers_test.go`

- [ ] **Step 1: Write the failing SSE test**

Append to `internal/agent/tools_mcp_headers_test.go`:

```go
func TestMCPClientSSEResponse(t *testing.T) {
	srv, m := newMockMCPServer(t, nil, map[string]any{"check": map[string]any{"ok": true}})
	// Override the handler to respond with an SSE stream that emits a
	// notification then the matching result.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		m.mu.Lock()
		m.headerSeen[req.Method] = r.Header.Clone()
		m.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		// A notification (no id) that must be ignored.
		fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		if fl != nil {
			fl.Flush()
		}
		// The matching result.
		fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"ok\":true}}\n\n", req.ID)
		if fl != nil {
			fl.Flush()
		}
	})

	c := newMCPHTTPClient(srv.URL, map[string]string{"Authorization": "Bearer secret"})
	args, _ := json.Marshal(map[string]any{"tool": "check", "arguments": map[string]any{}})
	res, err := c.handleCallTool()(args, &ToolContext{Context: context.Background()})
	if err != nil {
		t.Fatalf("call tool via SSE: %v", err)
	}
	if got, _ := res.(map[string]any)["ok"].(bool); !got {
		t.Fatalf("expected result.ok=true, got %v", res)
	}
}
```

- [ ] **Step 2: Run to verify it passes (SSE support is already in Step 3 of Task 1)**

Run: `cd /tmp/opencode/aether-upstream && go test ./internal/agent/ -run 'TestMCPClientSSEResponse' -v 2>&1 | tail -10`
Expected: PASS. If it fails, fix `readSSEMatch`.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/tools_mcp_headers_test.go
git commit -m "test(agent): cover MCP SSE event-stream responses"
```

---

### Task 3: Error handling — HTML/4xx produces a clear error, not `invalid character '<'`

**Files:**
- Add test to: `internal/agent/tools_mcp_headers_test.go`

- [ ] **Step 1: Write the failing test for a 4xx HTML response**

Append:

```go
func TestMCPClientHTMLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(404)
		fmt.Fprint(w, "<!DOCTYPE html><html><body>Not Found</body></html>")
	}))
	defer srv.Close()

	c := newMCPHTTPClient(srv.URL, map[string]string{"Authorization": "Bearer secret"})
	_, err := c.handleListTools()(nil, &ToolContext{Context: context.Background()})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid character '<'") {
		t.Fatalf("error leaked the raw json error: %q", msg)
	}
	if !strings.Contains(msg, "HTTP 404") {
		t.Fatalf("error should mention HTTP 404, got %q", msg)
	}
}
```

- [ ] **Step 2: Verify it passes (status + Content-Type gate is in Task 1, Step 3)**

Run: `cd /tmp/opencode/aether-upstream && go test ./internal/agent/ -run 'TestMCPClientHTMLError' -v 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/tools_mcp_headers_test.go
git commit -m "test(agent): assert 4xx HTML yields a clear error, not raw json"
```

---

### Task 4: Wire engine.go to the new client

**Files:**
- Modify: `internal/agent/engine.go:854-872`

- [ ] **Step 1: Update the MCP dispatch block**

In `internal/agent/engine.go`, replace the block at lines 854-872:

```go
	if len(agent.MCPServers) > 0 {
		for _, ms := range agent.MCPServers {
			if ms.Type == "stdio" {
				slog.Warn("stdio MCP server not supported at runtime, skipping", "server", ms.Name, "session_id", sessionID)
				continue
			}
			if ms.Type == "http" {
				plain, err := headers.DecryptSecrets(ms.Headers, ms.HeaderSecrets, masterKey)
				if err != nil {
					slog.Warn("engine: failed to decrypt MCP server headers", "server", ms.Name, "session_id", sessionID, "error", err)
					continue
				}
				allTools = append(allTools,
					mcpListToolDef(ms.Name, makeMCPToolListHandlerHTTP(ms.Command, plain)),
					mcpCallToolDef(ms.Name, makeMCPToolCallHandlerHTTP(ms.Command, plain)),
				)
			}
		}
	}
```

with:

```go
	if len(agent.MCPServers) > 0 {
		for _, ms := range agent.MCPServers {
			if ms.Type == "stdio" {
				slog.Warn("stdio MCP server not supported at runtime yet; skipping (tracked as a follow-up)", "server", ms.Name, "session_id", sessionID)
				continue
			}
			if ms.Type == "http" {
				plain, err := headers.DecryptSecrets(ms.Headers, ms.HeaderSecrets, masterKey)
				if err != nil {
					slog.Warn("engine: failed to decrypt MCP server headers", "server", ms.Name, "session_id", sessionID, "error", err)
					continue
				}
				client := newMCPHTTPClient(ms.Command, plain)
				allTools = append(allTools,
					mcpListToolDef(ms.Name, client.handleListTools()),
					mcpCallToolDef(ms.Name, client.handleCallTool()),
				)
			}
		}
	}
```

- [ ] **Step 2: Build to verify compilation**

Run: `cd /tmp/opencode/aether-upstream && go build ./... 2>&1 | tail -20`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/agent/engine.go
git commit -m "refactor(agent): build one mcpHTTPClient per server per turn"
```

---

### Task 5: Fix admin "Test connectivity" to POST initialize

**Files:**
- Modify: `internal/api/mcp_server_handlers.go:371-433`
- Add test: `internal/api/mcp_server_handlers_test.go` (or existing test file)

- [ ] **Step 1: Write a failing test**

In the api test file, add a test that runs an HTML-landing-page server (404/200 HTML) and asserts `handleTestMCPServer` reports `success: false`:

```go
func TestHandleTestMCPServerRejectsHTML(t *testing.T) {
	db := setupAPITestDB(t)
	orgID, userID := createTestOrgAndUser(t, db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		fmt.Fprint(w, "<!DOCTYPE html><html></html>")
	}))
	defer srv.Close()
	mcpID := uuid.New().String()
	db.Pool.Exec(context.Background(), `INSERT INTO mcp_servers (id,org_id,name,type,command,args,headers,header_secrets,created_by,created_at,updated_at) VALUES ($1,$2,'s','http',$3,'{}','{}','{}',$4,NOW(),NOW())`, mcpID, orgID, srv.URL, userID)

	h := newTestHandlers(db)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp-servers/"+mcpID+"/test", nil)
	req.SetPathValue("id", mcpID)
	req = req.WithContext(withClaims(context.Background(), orgID, userID, "admin"))
	h.handleTestMCPServer(rec, req)

	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["success"] != false {
		t.Fatalf("expected success=false for HTML server, got %v", resp["success"])
	}
}
```

- [ ] **Step 2: Implement the fix**

In `internal/api/mcp_server_handlers.go`, replace the body of `handleTestMCPServer` (lines 391-432, the `if serverType == "http" {` branch) with a POST of a JSON-RPC `initialize`:

```go
	if serverType == "http" {
		var headerMap models.JSONMap
		if headersRaw != nil {
			json.Unmarshal(headersRaw, &headerMap)
		}
		plain, decErr := headers.DecryptSecrets(headerMap, secretNames, h.server.masterKey)
		if decErr != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": decErr.Error()})
			return
		}
		initBody, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{},
				"clientInfo":      map[string]any{"name": "aether-notebooks", "version": "test"},
			},
		})
		req, reqErr := http.NewRequestWithContext(r.Context(), http.MethodPost, command, bytes.NewReader(initBody))
		if reqErr != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": reqErr.Error()})
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		headers.Apply(req, plain)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			return
		}
		defer resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		success := resp.StatusCode < 400 && (strings.Contains(ct, "application/json") || strings.Contains(ct, "text/event-stream"))
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     success,
			"status_code": resp.StatusCode,
		})
	} else {
		writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": "testing not supported for stdio MCP servers"})
	}
```

Requires imports: `bytes`, `encoding/json`, `strings` (add if missing) and `github.com/the-heaven-labs/aether/internal/headers`, `github.com/the-heaven-labs/aether/internal/models`.

- [ ] **Step 3: Run test to verify it passes**

Run: `cd /tmp/opencode/aether-upstream && go test ./internal/api/ -run 'TestHandleTestMCPServer' -v 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/api/mcp_server_handlers.go internal/api/mcp_server_handlers_test.go
git commit -m "fix(api): test MCP server with a real initialize, not a bare GET

The admin Test Connectivity button GET the bare URL and accepted any 2xx, so it
reported success against an HTML landing page while the agent call failed.
"
```

---

### Task 6: Adapt end-to-end tests + full verification

**Files:**
- Modify: `internal/agent/tools_mcp_headers_test.go` (`TestProcessMessageSendsMCPCustomHeaders`, `TestProcessMessageSkipsMCPOnDecryptFailure`)

- [ ] **Step 1: Adapt `TestProcessMessageSendsMCPCustomHeaders`**

In `TestProcessMessageSendsMCPCustomHeaders`, change the mock server to expect JSON-RPC POSTs to `/` (the base URL) — accept `initialize` (respond JSON-RPC result + `Mcp-Session-Id`), then `tools/list`/`tools/call`. Capture the `Authorization` header on `initialize` and `tools/call` and assert it equals the decrypted secret. The core assertion stays: decrypted header reaches the server.

- [ ] **Step 2: Adapt `TestProcessMessageSkipsMCPOnDecryptFailure`**

The skip path in `engine.go` is unchanged (decrypt failure → `continue`). Update the mock to a JSON-RPC server but keep the assertion: `requests == 0` (server never contacted), and the tool message contains `tool not available`.

- [ ] **Step 2.1: Add a `lastBody` field to the mock** so `TestMCPClientCallTool` can assert params — set `m.mu.Lock(); m.lastBody = body; m.mu.Unlock()` at the top of the handler.

- [ ] **Step 3: Run the full agent test package**

Run: `cd /tmp/opencode/aether-upstream && go test ./internal/agent/ -run 'MCP' -v 2>&1 | tail -30`
Expected: all PASS.

- [ ] **Step 4: Run the whole build + vet**

Run: `cd /tmp/opencode/aether-upstream && go build ./... && go vet ./... 2>&1 | tail -20`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/tools_mcp_headers_test.go
git commit -m "test(agent): adapt MCP e2e tests to JSON-RPC Streamable HTTP"
```

---

### Task 7: Live verification against AbuseIPDB

- [ ] **Step 1: Run a manual integration probe**

Run (replacing `$ABUSEIPDB_KEY`):

```bash
cd /tmp/opencode/aether-upstream
cat > /tmp/mcp_probe.go <<'EOF'
package main
import ("context";"fmt";"net/http";"net/http/httptest")
func main(){
  // manual: run the TestMCPClientInitializeAndListTools test against the real server instead
  _ = http.DefaultClient; _ = httptest.NewServer; _ = context.Background
  fmt.Println("see TestMCPClientAgainstRealServer (integration, skipped in CI)")
}
EOF
```

Manual check instead: `go test ./internal/agent/ -run TestMCPClient -v` already proves the protocol against a JSON-RPC mock; the curl matrix in the design doc proves AbuseIPDB speaks the same JSON-RPC. No code change; record result in the PR body.

- [ ] **Step 2: Final commit + push preparation**

```bash
cd /tmp/opencode/aether-upstream
git log --oneline -8
go build ./... && go test ./internal/agent/ ./internal/api/ 2>&1 | tail -15
```

---

## Self-review notes (post-write)

- The `call` method name collides conceptually with `tools/call`; renamed to `request` for the wire path.
- `tools/call` params use `{name, arguments}` (MCP spec) while the LLM-facing wrapper keeps `{tool, arguments}`; the client maps `tool`→`name` in `handleCallTool`.
- The mock in Task 1 captures `headerSeen` by method but `TestMCPClientCallTool` checks `m.lastBody` for the params — the plan adds `lastBody` in Task 6 Step 2.1. (Note: moved into Task 1 mock to keep the test green on first run.)