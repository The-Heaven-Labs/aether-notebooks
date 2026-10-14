package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
)

// TestMCPHTTPHandlersSendHeaders pins that the HTTP MCP handler factories send
// configured custom headers on both the list and call requests, without
// clobbering the JSON content type on the call request.
func TestMCPHTTPHandlersSendHeaders(t *testing.T) {
	var mu sync.Mutex
	var listHeaders, callHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		switch r.URL.Path {
		case "/tools/list":
			listHeaders = r.Header.Clone()
		case "/tools/call":
			callHeaders = r.Header.Clone()
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
	}))
	defer srv.Close()

	hdrs := map[string]string{"Authorization": "Bearer mcp-token", "X-Env": "prod"}
	list := makeMCPToolListHandlerHTTP(srv.URL, hdrs)
	call := makeMCPToolCallHandlerHTTP(srv.URL, hdrs)

	if _, err := list(nil, &ToolContext{Context: context.Background()}); err != nil {
		t.Fatalf("list handler: %v", err)
	}
	if _, err := call(json.RawMessage(`{"tool":"weather","arguments":{}}`), &ToolContext{Context: context.Background()}); err != nil {
		t.Fatalf("call handler: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if listHeaders == nil {
		t.Fatal("MCP server never received /tools/list")
	}
	if callHeaders == nil {
		t.Fatal("MCP server never received /tools/call")
	}
	for name, got := range map[string]http.Header{"list": listHeaders, "call": callHeaders} {
		if got.Get("Authorization") != "Bearer mcp-token" {
			t.Fatalf("%s: expected Authorization header, got %q", name, got.Get("Authorization"))
		}
		if got.Get("X-Env") != "prod" {
			t.Fatalf("%s: expected X-Env header, got %q", name, got.Get("X-Env"))
		}
	}
	if got := callHeaders.Get("Content-Type"); got != "application/json" {
		t.Fatalf("call: expected Content-Type application/json, got %q", got)
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
	var callHeaders http.Header
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tools/call" {
			mu.Lock()
			callHeaders = r.Header.Clone()
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
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
	if callHeaders == nil {
		t.Fatal("MCP server never received /tools/call")
	}
	if got := callHeaders.Get("Authorization"); got != "Bearer mcp-secret" {
		t.Fatalf("expected decrypted Authorization header on MCP call, got %q", got)
	}
}
