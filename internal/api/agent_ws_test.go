package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/the-heaven-labs/aether/internal/api"
)

func createModelConfig(t *testing.T, srv *api.Server, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":           "Test Model",
		"provider":       "openai",
		"base_url":       "https://api.example.com/v1",
		"model":          "gpt-4",
		"api_key":        "test-api-key",
		"context_window": 128000,
	})
	req := httptest.NewRequest("POST", "/api/v1/model-configs", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("createModelConfig failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	id, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("createModelConfig returned no id: %v", resp)
	}
	return id
}

func createAgent(t *testing.T, srv *api.Server, token, modelConfigID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":            "Test Agent",
		"model_config_id": modelConfigID,
		"description":     "A test agent",
	})
	req := httptest.NewRequest("POST", "/api/v1/agents", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("createAgent failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	id, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("createAgent returned no id: %v", resp)
	}
	return id
}

func createAgentSession(t *testing.T, srv *api.Server, token, agentID, notebookID string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"notebook_id": notebookID,
	})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/agents/%s/session", agentID), strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("createAgentSession failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	id, ok := resp["session_id"].(string)
	if !ok {
		t.Fatalf("createAgentSession returned no session_id: %v", resp)
	}
	return id
}

func TestAgentWSErrorReachesClient(t *testing.T) {
	srv := setupTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	email := fmt.Sprintf("ws-err-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "WS Error Org")
	nbID := createNotebook(t, srv, token, "WS Error NB")
	mcID := createModelConfig(t, srv, token)
	agentID := createAgent(t, srv, token, mcID)
	sessionID := createAgentSession(t, srv, token, agentID, nbID)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/agents/" + sessionID + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial agent ws: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	msg := map[string]string{"type": "message", "content": "hello"}
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write message: %v", err)
	}

	var lastMsg map[string]any
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var m map[string]any
		json.Unmarshal(data, &m)
		lastMsg = m
		if typ, ok := m["type"].(string); ok {
			if typ == "done" || typ == "error" {
				goto done2
			}
		}
	}
done2:

	if lastMsg == nil {
		t.Fatal("no message received from websocket - error was silently dropped (race condition bug)")
	}
	typ, _ := lastMsg["type"].(string)
	if typ == "error" {
		if _, ok := lastMsg["message"]; !ok {
			t.Fatalf("error message missing 'message' field: %v", lastMsg)
		}
	}
}

func TestAgentWSReconnect(t *testing.T) {
	srv := setupTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	email := fmt.Sprintf("ws-recon-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "WS Recon Org")
	nbID := createNotebook(t, srv, token, "WS Recon NB")
	mcID := createModelConfig(t, srv, token)
	agentID := createAgent(t, srv, token, mcID)
	sessionID := createAgentSession(t, srv, token, agentID, nbID)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/agents/" + sessionID + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial agent ws: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	reconMsg := map[string]string{"type": "reconnect", "last_message_id": ""}
	if err := conn.WriteJSON(reconMsg); err != nil {
		t.Fatalf("write reconnect: %v", err)
	}

	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read reconnect response: %v", err)
	}

	var m map[string]any
	json.Unmarshal(data, &m)
	if m["type"] != "reconnect_sync" {
		t.Fatalf("expected reconnect_sync, got: %v", m["type"])
	}
}

func createModelConfigWithURL(t *testing.T, srv *api.Server, token, baseURL string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":           "Test Model",
		"provider":       "openai",
		"base_url":       baseURL,
		"model":          "gpt-4",
		"api_key":        "test-api-key",
		"context_window": 128000,
	})
	req := httptest.NewRequest("POST", "/api/v1/model-configs", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("createModelConfig failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	id, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("createModelConfig returned no id: %v", resp)
	}
	return id
}

// mockLLMResponses serves canned chat completions in order (repeating last).
func mockLLMResponses(t *testing.T, bodies []string) *httptest.Server {
	t.Helper()
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n
		if i >= len(bodies) {
			i = len(bodies) - 1
		}
		n++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, bodies[i])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readWSUntil(t *testing.T, conn *websocket.Conn, wantTypes map[string]bool) map[string]map[string]any {
	t.Helper()
	found := map[string]map[string]any{}
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	for len(found) < len(wantTypes) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ws read: %v (found %v)", err, found)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if typ, _ := m["type"].(string); wantTypes[typ] {
			if _, ok := found[typ]; !ok {
				found[typ] = m
			}
		}
	}
	return found
}

// Tool events must carry tool_call_id and done must carry the final content,
// so reconnected clients that missed the token stream still converge.
func TestAgentWSToolCallIDAndDoneContent(t *testing.T) {
	srv := setupTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	toolCallBody := `{"id":"x","model":"gpt-4","choices":[{"message":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"whatever","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	finalBody := `{"id":"x","model":"gpt-4","choices":[{"message":{"content":"hello world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	llm := mockLLMResponses(t, []string{toolCallBody, finalBody})
	defer llm.Close()

	email := fmt.Sprintf("ws-tcid-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "WS TCID Org")
	nbID := createNotebook(t, srv, token, "WS TCID NB")
	mcID := createModelConfigWithURL(t, srv, token, llm.URL)
	agentID := createAgent(t, srv, token, mcID)
	sessionID := createAgentSession(t, srv, token, agentID, nbID)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/agents/" + sessionID + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial agent ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{"type": "message", "content": "hi"}); err != nil {
		t.Fatalf("write message: %v", err)
	}

	found := readWSUntil(t, conn, map[string]bool{"tool_call": true, "tool_result": true, "done": true})

	if got, _ := found["tool_call"]["tool_call_id"].(string); got != "call-1" {
		t.Fatalf("tool_call missing tool_call_id: %v", found["tool_call"])
	}
	if got, _ := found["tool_result"]["tool_call_id"].(string); got != "call-1" {
		t.Fatalf("tool_result missing tool_call_id: %v", found["tool_result"])
	}
	data, _ := found["done"]["data"].(map[string]any)
	if got, _ := data["content"].(string); got != "hello world" {
		t.Fatalf("done missing final content: %v", found["done"])
	}
	if _, ok := found["done"]["seq"]; !ok {
		t.Fatalf("stream events must carry seq: %v", found["done"])
	}
}

// Reconnect replays the stream buffer (with seq) and reconnect_sync reports
// server_seq for reconciliation.
func TestAgentWSReconnectReplaysBuffer(t *testing.T) {
	srv := setupTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	finalBody := `{"id":"x","model":"gpt-4","choices":[{"message":{"content":"replay me"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	llm := mockLLMResponses(t, []string{finalBody})
	defer llm.Close()

	email := fmt.Sprintf("ws-replay-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "WS Replay Org")
	nbID := createNotebook(t, srv, token, "WS Replay NB")
	mcID := createModelConfigWithURL(t, srv, token, llm.URL)
	agentID := createAgent(t, srv, token, mcID)
	sessionID := createAgentSession(t, srv, token, agentID, nbID)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/agents/" + sessionID + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial agent ws: %v", err)
	}
	if err := conn.WriteJSON(map[string]string{"type": "message", "content": "hi"}); err != nil {
		t.Fatalf("write message: %v", err)
	}
	readWSUntil(t, conn, map[string]bool{"done": true})
	conn.Close()

	// Reconnect inside the stream grace period: buffered events replay.
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial agent ws (reconnect): %v", err)
	}
	defer conn2.Close()

	conn2.SetReadDeadline(time.Now().Add(10 * time.Second))
	var replayed map[string]any
	for i := 0; i < 50; i++ {
		_, data, err := conn2.ReadMessage()
		if err != nil {
			t.Fatalf("read replay: %v", err)
		}
		var m map[string]any
		json.Unmarshal(data, &m)
		if _, ok := m["seq"]; ok {
			replayed = m
			break
		}
	}
	if replayed == nil {
		t.Fatal("expected replayed buffered events with seq on reconnect")
	}

	if err := conn2.WriteJSON(map[string]string{"type": "reconnect", "last_message_id": ""}); err != nil {
		t.Fatalf("write reconnect: %v", err)
	}
	found := readWSUntil(t, conn2, map[string]bool{"reconnect_sync": true})
	syncMsg := found["reconnect_sync"]
	seq, ok := syncMsg["server_seq"].(float64)
	if !ok || seq <= 0 {
		t.Fatalf("reconnect_sync must report server_seq > 0, got %v", syncMsg)
	}
	msgs, _ := syncMsg["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("reconnect_sync must return persisted history, got %v", syncMsg)
	}
}
