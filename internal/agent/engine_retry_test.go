package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// The engine must propagate running-state hooks into every ToolContext, the
// same way BroadcastFunc is propagated.
func TestProcessMessage_PropagatesRunningHooks(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	engine.SetRunningFunc = func(cellID, notebookID string, startedAt time.Time) {}
	engine.UnsetRunningFunc = func(cellID string) {}
	engine.SetCancelFunc = func(cellID string, cancel context.CancelFunc) {}
	engine.DeleteCancelFunc = func(cellID string) {}
	engine.BroadcastFunc = func(notebookID string, msg any) {}

	agentID := createTestAgentRow(t, db, orgID, userID, []string{})
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	var sawBroadcast, sawSetRunning, sawUnsetRunning, sawSetCancel, sawDeleteCancel bool
	probe := &ToolDef{
		Type: "function",
		Handler: func(args json.RawMessage, ctx *ToolContext) (any, error) {
			sawBroadcast = ctx.BroadcastFunc != nil
			sawSetRunning = ctx.SetRunningFunc != nil
			sawUnsetRunning = ctx.UnsetRunningFunc != nil
			sawSetCancel = ctx.SetCancelFunc != nil
			sawDeleteCancel = ctx.DeleteCancelFunc != nil
			return map[string]any{"ok": true}, nil
		},
	}
	probe.Function.Name = "probe_hooks"
	probe.Function.Parameters = `{"type":"object","properties":{}}`

	masterKey := make([]byte, 32)
	callID := "call-hooks-1"
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
						}{Name: "probe_hooks", Arguments: `{}`},
					}},
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
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)

	if _, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "probe", nil, []*ToolDef{probe}, masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	if !sawBroadcast || !sawSetRunning || !sawUnsetRunning || !sawSetCancel || !sawDeleteCancel {
		t.Fatalf("hooks not propagated: broadcast=%v setRunning=%v unsetRunning=%v setCancel=%v deleteCancel=%v",
			sawBroadcast, sawSetRunning, sawUnsetRunning, sawSetCancel, sawDeleteCancel)
	}
}

// Steering mailbox: FIFO order, bounded, drain clears.
func TestSteeringMailboxFIFOAndBounded(t *testing.T) {
	db := setupEngineTestDB(t)
	engine := newTestEngine(db)
	sid := "steer-box-1"

	if got := engine.DrainSteering(sid); len(got) != 0 {
		t.Fatalf("fresh mailbox must drain empty, got %v", got)
	}
	ch := engine.SteeringChan(sid)
	if ch != engine.SteeringChan(sid) {
		t.Fatal("SteeringChan must return the same mailbox")
	}
	for _, m := range []string{"first", "second", "third"} {
		ch <- m
	}
	got := engine.DrainSteering(sid)
	if len(got) != 3 || got[0] != "first" || got[1] != "second" || got[2] != "third" {
		t.Fatalf("FIFO violated: %v", got)
	}
	if again := engine.DrainSteering(sid); len(again) != 0 {
		t.Fatalf("drain must clear, got %v", again)
	}
	// Bounded: fills to capacity without blocking, then reports full.
	for i := 0; i < 16; i++ {
		select {
		case ch <- "x":
		default:
			t.Fatalf("mailbox blocked at %d/16", i)
		}
	}
	select {
	case ch <- "overflow":
		t.Fatal("mailbox must be bounded at 16")
	default:
	}
	engine.DrainSteering(sid)
}

// A follow-up pushed while a tool executes lands in the next LLM call:
// persisted, published, and ordered after the tool result.
func TestProcessMessage_SteeringFoldedAtTurnTop(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	agentID := createTestAgentRow(t, db, orgID, userID, []string{})
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	started := make(chan struct{})
	release := make(chan struct{})
	probe := &ToolDef{Type: "function"}
	probe.Function.Name = "probe_block"
	probe.Function.Parameters = `{"type":"object","properties":{}}`
	probe.Handler = func(args json.RawMessage, ctx *ToolContext) (any, error) {
		close(started)
		select {
		case <-release:
		case <-time.After(15 * time.Second):
			return nil, context.DeadlineExceeded
		}
		return map[string]any{"ok": true}, nil
	}

	masterKey := make([]byte, 32)
	callID := "call-steer-1"
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
						}{Name: "probe_block", Arguments: `{}`},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 10, CompletionTokens: 5},
		},
		{
			Choices: []Choice{{
				Message:      ChatMessage{Content: "steered done"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 60, CompletionTokens: 5},
		},
	}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)

	var events []EngineEvent
	done := make(chan error, 1)
	go func() {
		_, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "start", nil, []*ToolDef{probe}, masterKey, nil, nil, nil, nil, nil,
			func(evt EngineEvent) { events = append(events, evt) })
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("probe tool never started")
	}
	// Steer while the tool executes.
	engine.SteeringChan(sid) <- "actually use the other table"
	close(release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessMessage: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("ProcessMessage did not finish")
	}

	if len(captured) < 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(captured))
	}
	msgs, _ := captured[1]["messages"].([]any)
	var roles []string
	var sawSteer bool
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		role, _ := mm["role"].(string)
		roles = append(roles, role)
		if role == "user" && mm["content"] == "actually use the other table" {
			sawSteer = true
		}
	}
	if !sawSteer {
		t.Fatalf("second LLM call missing steered message (roles %v)", roles)
	}
	// Steer must come after the tool result (provider ordering).
	steerIdx, toolIdx := -1, -1
	for i, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" {
			toolIdx = i
		}
		if mm["role"] == "user" && mm["content"] == "actually use the other table" {
			steerIdx = i
		}
	}
	if steerIdx < 0 || toolIdx < 0 || steerIdx < toolIdx {
		t.Fatalf("steer must follow tool results: steer=%d tool=%d", steerIdx, toolIdx)
	}

	var sawSteeringEvent bool
	for _, e := range events {
		if e.Type == "steering" && e.Content == "actually use the other table" {
			sawSteeringEvent = true
		}
	}
	if !sawSteeringEvent {
		t.Fatal("expected a steering event for the transcript")
	}

	var count int
	err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_messages WHERE session_id=$1 AND role='user' AND content='actually use the other table'`,
		sid).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("steered message must be persisted once, count=%d err=%v", count, err)
	}
}
