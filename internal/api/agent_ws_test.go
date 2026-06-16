package api_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/heavenlabs/hnb/internal/api"
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
