package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// findMessageByContent locates a decoded message row by its content. It
// accepts both []any (websocket payloads) and []map[string]any (REST payloads).
func findMessageByContent[T any](t *testing.T, msgs []T, content string) map[string]any {
	t.Helper()
	for _, raw := range msgs {
		m, _ := any(raw).(map[string]any)
		if m["content"] == content {
			return m
		}
	}
	t.Fatalf("message %q not found in %v", content, msgs)
	return nil
}

// A persisted compaction row must carry its real before/after token counts
// through reconnect_sync and the REST messages endpoint, so the divider keeps
// its numbers after a reload. Legacy rows (NULL tokens_after and NULL
// duration_ms) and non-compaction rows must stay safe: NULL duration_ms is
// coalesced, and a missing tokens_after is never synthesized into a count.
func TestAgentWSReconnectCompactionTokens(t *testing.T) {
	srv := setupTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	email := fmt.Sprintf("ws-compaction-tokens-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "WS Compaction Tokens Org")
	nbID := createNotebook(t, srv, token, "WS Compaction Tokens NB")
	mcID := createModelConfig(t, srv, token)
	agentID := createAgent(t, srv, token, mcID)
	sessionID := createAgentSession(t, srv, token, agentID, nbID)

	// The legacy compaction row and the user row have NULL duration_ms — the
	// messages endpoint used to 500 on those scans.
	if _, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO agent_messages (session_id, role, content, tokens_direct, tokens_after, duration_ms, created_at) VALUES
			($1, 'compaction', 'legacy context summary', 900, NULL, NULL, NOW() - INTERVAL '2 minutes'),
			($1, 'user', 'hello', 0, NULL, NULL, NOW() - INTERVAL '1 minute'),
			($1, 'compaction', 'earlier context summary', 1200, 400, 0, NOW())
	`, sessionID); err != nil {
		t.Fatalf("insert messages: %v", err)
	}

	assertCurrentCounts := func(t *testing.T, msg map[string]any) {
		t.Helper()
		if got, _ := msg["tokens_direct"].(float64); got != 1200 {
			t.Fatalf("tokens_direct = %v, want 1200 (%v)", msg["tokens_direct"], msg)
		}
		if got, _ := msg["tokens_after"].(float64); got != 400 {
			t.Fatalf("tokens_after = %v, want 400 (%v)", msg["tokens_after"], msg)
		}
	}

	t.Run("reconnect_sync", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/agents/" + sessionID + "?token=" + token
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial agent ws: %v", err)
		}
		defer conn.Close()

		if err := conn.WriteJSON(map[string]string{"type": "reconnect", "last_message_id": ""}); err != nil {
			t.Fatalf("write reconnect: %v", err)
		}
		found := readWSUntil(t, conn, map[string]bool{"reconnect_sync": true})
		msgs, _ := found["reconnect_sync"]["messages"].([]any)
		if len(msgs) != 3 {
			t.Fatalf("messages = %v, want 3 rows", found["reconnect_sync"]["messages"])
		}
		assertCurrentCounts(t, findMessageByContent(t, msgs, "earlier context summary"))

		legacy := findMessageByContent(t, msgs, "legacy context summary")
		if got, _ := legacy["tokens_direct"].(float64); got != 900 {
			t.Fatalf("legacy tokens_direct = %v, want 900 (%v)", legacy["tokens_direct"], legacy)
		}
		if _, has := legacy["tokens_after"]; has {
			t.Fatalf("legacy row must omit tokens_after, got %v", legacy)
		}

		user := findMessageByContent(t, msgs, "hello")
		if _, has := user["tokens_after"]; has {
			t.Fatalf("non-compaction row must omit tokens_after, got %v", user)
		}
	})

	t.Run("messages endpoint", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID+"/messages", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("messages: %d %s", rec.Code, rec.Body.String())
		}
		var msgs []map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&msgs); err != nil {
			t.Fatalf("decode messages: %v", err)
		}
		if len(msgs) != 3 {
			t.Fatalf("messages = %v, want 3 rows", msgs)
		}
		assertCurrentCounts(t, findMessageByContent(t, msgs, "earlier context summary"))

		legacy := findMessageByContent(t, msgs, "legacy context summary")
		if got, _ := legacy["tokens_direct"].(float64); got != 900 {
			t.Fatalf("legacy tokens_direct = %v, want 900 (%v)", legacy["tokens_direct"], legacy)
		}
		// The wire carries 0 for the NULL tokens_after; the frontend mapper
		// treats 0 as absent so the divider falls back to no counts.
		if got, _ := legacy["tokens_after"].(float64); got != 0 {
			t.Fatalf("legacy tokens_after = %v, want 0 (%v)", legacy["tokens_after"], legacy)
		}
	})
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

func mockLLMWithCapture(t *testing.T, bodies []string, captured *[]map[string]any) *httptest.Server {
	t.Helper()
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)
		*captured = append(*captured, reqBody)
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

func builtinToolID(t *testing.T, srv *api.Server, email, handler string) string {
	t.Helper()
	var orgID string
	err := srv.DB().Pool.QueryRow(context.Background(),
		`SELECT org_id FROM org_members WHERE user_id = (SELECT id FROM users WHERE email=$1)`, email).Scan(&orgID)
	if err != nil {
		t.Fatalf("lookup org: %v", err)
	}
	var toolID string
	err = srv.DB().Pool.QueryRow(context.Background(),
		`SELECT id FROM tools WHERE org_id=$1 AND config->>'handler_name'=$2`, orgID, handler).Scan(&toolID)
	if err != nil {
		t.Fatalf("lookup tool %s: %v", handler, err)
	}
	return toolID
}

func createAgentWithTools(t *testing.T, srv *api.Server, token, modelConfigID string, toolIDs []string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":            "Steer Agent",
		"model_config_id": modelConfigID,
		"description":     "steering test agent",
		"tool_ids":        toolIDs,
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

// wsCollector reads every message on conn in the background so tests can wait
// for specific types without splitting the stream across readers.
type wsCollector struct {
	t    *testing.T
	conn *websocket.Conn
	mu   sync.Mutex
	msgs []map[string]any
}

func collectWS(t *testing.T, conn *websocket.Conn) *wsCollector {
	t.Helper()
	c := &wsCollector{t: t, conn: conn}
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				continue
			}
			c.mu.Lock()
			c.msgs = append(c.msgs, m)
			c.mu.Unlock()
		}
	}()
	return c
}

func (c *wsCollector) waitFor(typ string) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, m := range c.msgs {
			if m["type"] == typ {
				c.mu.Unlock()
				return m
			}
		}
		c.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	c.t.Fatalf("timed out waiting for %q", typ)
	return nil
}

func (c *wsCollector) count(typ string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.msgs {
		if m["type"] == typ {
			n++
		}
	}
	return n
}

// A message sent while a turn is blocked must be steered into the running
// turn (steering_accepted + steering event, folded into the next LLM call) —
// never silently dropped, and never run as a second concurrent turn.
func TestAgentWSSteeringAcceptedWhileBusy(t *testing.T) {
	srv := setupTestServer(t)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	questionBody := `{"id":"x","model":"gpt-4","choices":[{"message":{"tool_calls":[{"id":"call-q1","type":"function","function":{"name":"ask_question","arguments":"{\"question\":\"pick one\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	finalBody := `{"id":"x","model":"gpt-4","choices":[{"message":{"content":"steered done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":60,"completion_tokens":5,"total_tokens":65}}`
	var captured []map[string]any
	llm := mockLLMWithCapture(t, []string{questionBody, finalBody}, &captured)
	defer llm.Close()

	email := fmt.Sprintf("ws-steer-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "WS Steer Org")
	nbID := createNotebook(t, srv, token, "WS Steer NB")
	mcID := createModelConfigWithURL(t, srv, token, llm.URL)
	askID := builtinToolID(t, srv, email, "ask_question")
	agentID := createAgentWithTools(t, srv, token, mcID, []string{askID})
	sessionID := createAgentSession(t, srv, token, agentID, nbID)

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/v1/ws/agents/" + sessionID + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial agent ws: %v", err)
	}
	defer conn.Close()
	col := collectWS(t, conn)

	if err := conn.WriteJSON(map[string]string{"type": "message", "content": "start"}); err != nil {
		t.Fatalf("write message: %v", err)
	}
	// Wait until the turn blocks on the question dialog.
	if q := col.waitFor("question"); q["question"] == nil {
		t.Fatalf("expected question event, got %v", q)
	}

	// Steer while busy: must be accepted, not dropped.
	if err := conn.WriteJSON(map[string]string{"type": "message", "content": "actually pick blue"}); err != nil {
		t.Fatalf("write steer: %v", err)
	}
	col.waitFor("steering_accepted")

	// Answer the question so the turn completes.
	if err := conn.WriteJSON(map[string]string{"type": "question_answer", "answer": "blue"}); err != nil {
		t.Fatalf("write answer: %v", err)
	}
	doneMsg := col.waitFor("done")
	data, _ := doneMsg["data"].(map[string]any)
	if got, _ := data["content"].(string); got != "steered done" {
		t.Fatalf("unexpected done content: %v", doneMsg)
	}

	// Exactly 2 LLM calls = one turn: no second concurrent turn ran, and the
	// steered message reached the model's next call.
	if len(captured) != 2 {
		t.Fatalf("expected exactly 2 LLM calls (one turn), got %d", len(captured))
	}
	msgs, _ := captured[1]["messages"].([]any)
	var sawSteer bool
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "user" && mm["content"] == "actually pick blue" {
			sawSteer = true
		}
	}
	if !sawSteer {
		t.Fatalf("second LLM call missing steered message: %v", captured[1]["messages"])
	}

	// The steering event must reach the transcript stream.
	deadline := time.Now().Add(5 * time.Second)
	sawSteeringEvent := false
	for time.Now().Before(deadline) {
		col.mu.Lock()
		for _, m := range col.msgs {
			if m["type"] == "steering" && m["content"] == "actually pick blue" {
				sawSteeringEvent = true
				break
			}
		}
		col.mu.Unlock()
		if sawSteeringEvent {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !sawSteeringEvent {
		t.Fatal("expected steering stream event with the steered content")
	}
	if n := col.count("question"); n != 1 {
		t.Fatalf("expected exactly 1 question (single turn), got %d", n)
	}

	var count int
	err = srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_messages WHERE session_id=$1 AND role='user' AND content='actually pick blue'`,
		sessionID).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("steered message persisted != once: count=%d err=%v", count, err)
	}
}
