package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/the-heaven-labs/aether/internal/crypto"
)

// Failing LLM stub: every request gets HTTP 500.
func newFailingLLMServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"gateway overloaded"}`))
	}))
}

// Terminal LLM failure must be visible (llm_retry events with backoff) and
// persisted (assistant error message survives refresh via reconnect_sync).
func TestProcessMessage_LLMFailureRetriesAndPersists(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	agentID := createTestAgentRow(t, db, orgID, userID, []string{})
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	masterKey := make([]byte, 32)
	srv := newFailingLLMServer(t)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)

	var events []EngineEvent
	_, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hello", nil, nil, masterKey, nil, nil, nil, nil, nil,
		func(evt EngineEvent) { events = append(events, evt) })
	if err == nil || !strings.Contains(err.Error(), "llm call failed after 3 retries") {
		t.Fatalf("expected terminal llm error, got %v", err)
	}

	var retries []EngineEvent
	for _, e := range events {
		if e.Type == "llm_retry" {
			retries = append(retries, e)
		}
	}
	if len(retries) != 2 {
		t.Fatalf("expected 2 llm_retry events, got %d (%v)", len(retries), events)
	}
	if retries[0].Attempt != 1 || retries[1].Attempt != 2 || retries[0].MaxAttempts != 3 {
		t.Fatalf("bad retry numbering: %+v", retries)
	}

	// The failure must be persisted for reconnect_sync.
	var role, content string
	err = db.Pool.QueryRow(context.Background(),
		`SELECT role, content FROM agent_messages WHERE session_id=$1 AND role='assistant' ORDER BY created_at DESC LIMIT 1`,
		sid).Scan(&role, &content)
	if err != nil {
		t.Fatalf("query persisted error message: %v", err)
	}
	if !strings.Contains(content, "llm call failed after 3 retries") {
		t.Fatalf("persisted message missing failure text: %q", content)
	}
}

// Tool results must be recorded on the assistant message's tool_calls so
// reconnect_sync renders completed tools without joining tool rows.
func TestProcessMessage_PersistsToolCallResults(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	toolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	agentID := createTestAgentRow(t, db, orgID, userID, toolIDs)
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	masterKey := make([]byte, 32)
	argsJSON := `{"notebook_id":"` + nbID + `"}`
	callID := "call-persist-1"
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{
					ToolCalls: []ToolCall{{
						ID:   callID,
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "list_notebook_parameters", Arguments: argsJSON},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 50, CompletionTokens: 10},
		},
		{
			Choices: []Choice{{
				Message:      ChatMessage{Content: "done"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 50, CompletionTokens: 10},
		},
	}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)

	if _, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "list params", nil, nil, masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}

	var toolCallsJSON []byte
	err := db.Pool.QueryRow(context.Background(),
		`SELECT tool_calls FROM agent_messages WHERE session_id=$1 AND role='assistant' AND tool_calls IS NOT NULL ORDER BY created_at LIMIT 1`,
		sid).Scan(&toolCallsJSON)
	if err != nil {
		t.Fatalf("query assistant tool calls: %v", err)
	}
	var calls []struct {
		ID     string `json:"id"`
		Result any    `json:"result"`
	}
	if err := json.Unmarshal(toolCallsJSON, &calls); err != nil {
		t.Fatalf("decode tool calls: %v", err)
	}
	if len(calls) != 1 || calls[0].ID != callID {
		t.Fatalf("unexpected tool calls: %v", calls)
	}
	if calls[0].Result == nil {
		t.Fatal("tool call result was not persisted on the assistant message")
	}
}
