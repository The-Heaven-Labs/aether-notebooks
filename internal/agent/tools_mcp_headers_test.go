package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
)

// mockMCPServer is a minimal Streamable HTTP MCP server used by the unit
// tests. It requires initialize before tools/list or tools/call, sets a
// session id, and records the headers/body/path of each request.
type mockMCPServer struct {
	mu           sync.Mutex
	initialized  bool
	sessionID    string
	headerSeen   map[string]http.Header // method -> headers
	lastBody     []byte
	lastInitBody []byte
	pathSeen     map[string]int // path -> count
	tools        []map[string]any
	callResult   any
}

func newMockMCPServer(t *testing.T) (*httptest.Server, *mockMCPServer) {
	m := &mockMCPServer{
		sessionID:  "sess-" + uuid.New().String(),
		headerSeen: map[string]http.Header{},
		pathSeen:   map[string]int{},
		tools:      []map[string]any{{"name": "check", "description": "check an IP"}},
		callResult: map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.headerSeen[req.Method] = r.Header.Clone()
		m.lastBody = body
		if req.Method == "initialize" {
			m.lastInitBody = body
		}
		m.pathSeen[r.URL.Path]++
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if req.Method == "initialize" {
			m.mu.Lock()
			m.initialized = true
			m.mu.Unlock()
			w.Header().Set("Mcp-Session-Id", m.sessionID)
			writeJSONRPC(t, w, req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "mock", "version": "t"},
			}, nil)
			return
		}
		if r.Header.Get("Mcp-Session-Id") != m.sessionID {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "tools/list":
			writeSSEJSONRPC(t, w, req.ID, map[string]any{"tools": m.tools}, nil)
		case "tools/call":
			writeJSONRPC(t, w, req.ID, m.callResult, nil)
		default:
			writeJSONRPC(t, w, req.ID, nil, &mcpJSONRPCError{Code: -32601, Message: "method not found"})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, m
}

func writeJSONRPC(t *testing.T, w http.ResponseWriter, id any, result any, mcpErr *mcpJSONRPCError) {
	t.Helper()
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	if mcpErr != nil {
		resp["error"] = mcpErr
	} else {
		resp["result"] = result
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("write jsonrpc: %v", err)
	}
}

func writeSSEJSONRPC(t *testing.T, w http.ResponseWriter, id any, result any, mcpErr *mcpJSONRPCError) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if mcpErr != nil {
		msg["error"] = mcpErr
	} else {
		msg["result"] = result
	}
	raw, _ := json.Marshal(msg)
	fmt.Fprintf(w, "data: %s\n\n", raw)
	if fl != nil {
		fl.Flush()
	}
}

type mcpJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// TestMCPClientInitializeAndListTools pins the Streamable HTTP protocol: the
// client performs the initialize handshake (carrying auth headers) before
// tools/list, round-trips the session id, posts to the single base URL (never
// re-appending /tools/list), and sends the JSON-RPC 2.0 envelope with the
// required Content-Type.
func TestMCPClientInitializeAndListTools(t *testing.T) {
	srv, m := newMockMCPServer(t)
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
	if got := m.headerSeen["initialize"].Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("initialize missing Authorization, got %q", got)
	}
	if got := m.headerSeen["tools/list"].Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("tools/list missing Authorization, got %q", got)
	}
	if got := m.headerSeen["tools/list"].Get("Mcp-Session-Id"); got != m.sessionID {
		t.Fatalf("tools/list session id mismatch, got %q", got)
	}
	if got := m.headerSeen["initialize"].Get("Content-Type"); got != "application/json" {
		t.Fatalf("initialize Content-Type should be application/json, got %q", got)
	}
	if got := m.headerSeen["initialize"].Get("Accept"); got != mcpAccept {
		t.Fatalf("initialize Accept should be %q, got %q", mcpAccept, got)
	}
	// JSON-RPC 2.0 envelope.
	var initReq struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}
	if err := json.Unmarshal(m.lastInitBody, &initReq); err != nil {
		t.Fatalf("unmarshal init body: %v", err)
	}
	if initReq.JSONRPC != "2.0" || initReq.Method != "initialize" {
		t.Fatalf("expected jsonrpc=2.0 method=initialize, got %+v", initReq)
	}
	// POST goes to the bare base URL — no /tools/list appended.
	if len(m.pathSeen) != 1 {
		t.Fatalf("expected all requests to the base path, got paths %v", m.pathSeen)
	}
	for path := range m.pathSeen {
		if path != "/" {
			t.Fatalf("expected POST to '/', got %q", path)
		}
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMCPClientCallTool asserts tools/call params are the MCP {name, arguments}
// shape, not the legacy REST {tool, arguments}.
func TestMCPClientCallTool(t *testing.T) {
	srv, m := newMockMCPServer(t)
	c := newMCPHTTPClient(srv.URL, map[string]string{"Authorization": "Bearer secret"})

	args, _ := json.Marshal(map[string]any{"tool": "check", "arguments": map[string]any{"ipAddress": "1.2.3.4"}})
	res, err := c.handleCallTool()(args, &ToolContext{Context: context.Background()})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var got struct {
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(m.lastBody, &got); err != nil {
		t.Fatalf("unmarshal call body: %v (body=%s)", err, m.lastBody)
	}
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

// TestMCPClientSSEListTools asserts an SSE-framed tools/list result is decoded
// while a leading notification (no id) is ignored.
func TestMCPClientSSEListTools(t *testing.T) {
	m := &mockMCPServer{
		sessionID:  "sess-sse",
		headerSeen: map[string]http.Header{},
		pathSeen:   map[string]int{},
		tools:      []map[string]any{{"name": "check"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		m.mu.Lock()
		m.headerSeen[req.Method] = r.Header.Clone()
		m.mu.Unlock()
		if req.Method == "initialize" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Mcp-Session-Id", m.sessionID)
			fl, _ := w.(http.Flusher)
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2025-06-18\",\"capabilities\":{}}}\n\n", req.ID)
			if fl != nil {
				fl.Flush()
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		// A notification (no id): must be skipped.
		fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		// The matching result.
		fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"check\"}]}}\n\n", req.ID)
		if fl != nil {
			fl.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	c := newMCPHTTPClient(srv.URL, nil)
	res, err := c.handleListTools()(nil, &ToolContext{Context: context.Background()})
	if err != nil {
		t.Fatalf("list tools sse: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestMCPClientHTMLError asserts a 4xx HTML page yields a clear error
// mentioning the HTTP status, NOT the raw json "invalid character '<'" leak.
func TestMCPClientHTMLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "<!DOCTYPE html><html><body>Not Found</body></html>")
	}))
	defer srv.Close()

	c := newMCPHTTPClient(srv.URL, nil)
	_, err := c.handleListTools()(nil, &ToolContext{Context: context.Background()})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid character '<'") {
		t.Fatalf("error leaked raw json failure: %q", msg)
	}
	if !strings.Contains(msg, "HTTP 404") {
		t.Fatalf("error should mention HTTP 404, got %q", msg)
	}
}

// TestProcessMessageSendsMCPCustomHeaders exercises the full path: stored
// (encrypted) mcp_servers headers -> engine query -> decrypt -> HTTP request
// made by the MCP call tool.
func TestProcessMessageSendsMCPCustomHeaders(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	masterKey := testMasterKey()

	var mu sync.Mutex
	var sawAuth string
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sawAuth = r.Header.Get("Authorization")
		mu.Unlock()
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "sess-e2e")
			writeJSONRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "mock"}}, nil)
			return
		}
		writeJSONRPC(t, w, req.ID, map[string]any{"content": []any{}}, nil)
	}))
	defer mcpSrv.Close()

	secret, err := crypto.EncryptString("Bearer mcp-secret", masterKey)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	headersJSON, _ := json.Marshal(map[string]any{"Authorization": secret})

	mcpID := uuid.New().String()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO mcp_servers (id, org_id, name, type, command, args, headers, header_secrets, created_by, created_at, updated_at)
		VALUES ($1,$2,'weather','http',$3,'{}',$4,ARRAY['Authorization'],$5,NOW(),NOW())
	`, mcpID, orgID, mcpSrv.URL, headersJSON, userID); err != nil {
		t.Fatalf("create mcp server: %v", err)
	}

	agentID := createTestAgentRow(t, db, orgID, userID, []string{})
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO agent_mcp_servers (agent_id, mcp_server_id) VALUES ($1,$2)
	`, agentID, mcpID); err != nil {
		t.Fatalf("link mcp server: %v", err)
	}
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	callID := uuid.New().String()
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{
					ToolCalls: []ToolCall{{ID: callID, Type: "function", Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "weather_call_tool", Arguments: `{"tool":"forecast","arguments":{}}`}}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		},
		{
			Choices: []Choice{{
				Message:      ChatMessage{Content: "done"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		},
	}
	var captured []map[string]any
	llmSrv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer llmSrv.Close()
	encKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(llmSrv.URL, "gpt-4", encKey, map[string]any{}, nil, nil))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool, nil)

	if _, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hello", nil, nil, masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if sawAuth != "Bearer mcp-secret" {
		t.Fatalf("expected decrypted Authorization header to reach the MCP server, got %q", sawAuth)
	}
}

// TestProcessMessageSkipsMCPOnDecryptFailure pins the fail-safe behavior: when
// a server's secret cannot be decrypted, the engine skips that server (no HTTP
// request with placeholder/garbled values) and still completes the turn.
func TestProcessMessageSkipsMCPOnDecryptFailure(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	masterKey := testMasterKey()

	var mu sync.Mutex
	requests := 0
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Mcp-Session-Id", "sess-skip")
		if req.ID != nil {
			writeJSONRPC(t, w, req.ID, map[string]any{"ok": true}, nil)
		}
	}))
	defer mcpSrv.Close()

	wrongKey := testMasterKey()
	wrongKey[0] ^= 0xff
	secret, err := crypto.EncryptString("Bearer mcp-secret", wrongKey)
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	headersJSON, _ := json.Marshal(map[string]any{"Authorization": secret})

	mcpID := uuid.New().String()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO mcp_servers (id, org_id, name, type, command, args, headers, header_secrets, created_by, created_at, updated_at)
		VALUES ($1,$2,'weather','http',$3,'{}',$4,ARRAY['Authorization'],$5,NOW(),NOW())
	`, mcpID, orgID, mcpSrv.URL, headersJSON, userID); err != nil {
		t.Fatalf("create mcp server: %v", err)
	}

	agentID := createTestAgentRow(t, db, orgID, userID, []string{})
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO agent_mcp_servers (agent_id, mcp_server_id) VALUES ($1,$2)
	`, agentID, mcpID); err != nil {
		t.Fatalf("link mcp server: %v", err)
	}
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	callID := uuid.New().String()
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{
					ToolCalls: []ToolCall{{ID: callID, Type: "function", Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "weather_call_tool", Arguments: `{"tool":"forecast","arguments":{}}`}}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		},
		{
			Choices: []Choice{{
				Message:      ChatMessage{Content: "done"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		},
	}
	var captured []map[string]any
	llmSrv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer llmSrv.Close()
	encKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(llmSrv.URL, "gpt-4", encKey, map[string]any{}, nil, nil))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool, nil)

	content, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hello", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessMessage failed on decrypt error: %v", err)
	}
	if content != "done" {
		t.Fatalf("expected final assistant content %q, got %q", "done", content)
	}

	mu.Lock()
	defer mu.Unlock()
	if requests != 0 {
		t.Fatalf("expected zero MCP requests when headers cannot be decrypted, got %d", requests)
	}

	var toolContent *string
	if err := db.Pool.QueryRow(context.Background(), `
		SELECT content FROM agent_messages WHERE session_id = $1 AND role = 'tool' ORDER BY created_at DESC LIMIT 1
	`, sid).Scan(&toolContent); err != nil {
		t.Fatalf("read tool message: %v", err)
	}
	if toolContent == nil || !contains(*toolContent, "tool not available: weather_call_tool") {
		t.Fatalf("expected the MCP tool to be unavailable after the skip, got %v", toolContent)
	}
}
