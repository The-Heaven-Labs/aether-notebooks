package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
)

// TestProcessMessage_TokensDirect_PerTool verifies per-tool direct tokens are recorded
func TestProcessMessage_TokensDirect_PerTool(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	// Use a single tool that we know will be called
	toolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	agentID := createTestAgentRow(t, db, orgID, userID, toolIDs)
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	masterKey := make([]byte, 32)
	// Mock LLM: first turn calls list_notebook_parameters, second turn returns done
	argsJSON := `{"notebook_id":"` + nbID + `"}`
	argsTok := engine.tokenCounter.CountText(argsJSON, "gpt-4")
	callID := uuid.New().String()
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

	_, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "list params", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}

	// Query the tool message for tokens_direct
	var content *string
	var tokensDirect int
	err = db.Pool.QueryRow(context.Background(), `SELECT content, tokens_direct FROM agent_messages WHERE session_id=$1 AND role='tool' ORDER BY created_at LIMIT 1`, sid).Scan(&content, &tokensDirect)
	if err != nil {
		t.Fatalf("query tool message: %v", err)
	}
	if content == nil {
		t.Fatalf("tool content nil")
	}
	// Compute expected: argsTok + resultTok
	resultTok := engine.tokenCounter.CountText(*content, "gpt-4")
	expected := argsTok + resultTok
	if tokensDirect != expected {
		t.Fatalf("tokens_direct mismatch: got %d, expected %d (args %d + result %d, content %q)", tokensDirect, expected, argsTok, resultTok, *content)
	}
	t.Logf("per-tool tokens_direct correct: %d (args %d + result %d)", tokensDirect, argsTok, resultTok)
}

// TestProcessMessage_TokensDirect_GlobalTotals verifies global totals equal sum of per-call
func TestProcessMessage_TokensDirect_GlobalTotals(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	toolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters", "set_notebook_parameters")
	agentID := createTestAgentRow(t, db, orgID, userID, toolIDs)
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	masterKey := make([]byte, 32)
	// Mock LLM that calls two tools in one turn, then done
	args1 := `{"notebook_id":"` + nbID + `"}`
	args2 := `{"notebook_id":"` + nbID + `","parameters":[]}`
	call1 := uuid.New().String()
	call2 := uuid.New().String()
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{
					ToolCalls: []ToolCall{
						{ID: call1, Type: "function", Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "list_notebook_parameters", Arguments: args1}},
						{ID: call2, Type: "function", Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "set_notebook_parameters", Arguments: args2}},
					},
				},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 100, CompletionTokens: 20},
		},
		{
			Choices: []Choice{{
				Message:      ChatMessage{Content: "done"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 60, CompletionTokens: 10},
		},
	}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)

	_, _, _, _, tokBrk, err := engine.ProcessMessage(context.Background(), sid, "do both", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	// Query per-tool tokens_direct sum
	rows, err := db.Pool.Query(context.Background(), `SELECT tokens_direct FROM agent_messages WHERE session_id=$1 AND role='tool'`, sid)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	sum := 0
	count := 0
	for rows.Next() {
		var td int
		if err := rows.Scan(&td); err != nil {
			t.Fatalf("scan: %v", err)
		}
		sum += td
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 tool messages, got %d", count)
	}
	// Global totals should be >0 and at least sum should be <= global sums? Actually global ToolCalls is sum of args, ToolResults sum of results, sum of direct is sum of both
	// So sum should equal tokBrk.ToolCalls + tokBrk.ToolResults
	expectedSum := tokBrk.ToolCalls + tokBrk.ToolResults
	if sum != expectedSum {
		t.Fatalf("sum of tokens_direct %d != global sum %d (calls %d + results %d)", sum, expectedSum, tokBrk.ToolCalls, tokBrk.ToolResults)
	}
	t.Logf("global totals consistent: sum %d == calls %d + results %d", sum, tokBrk.ToolCalls, tokBrk.ToolResults)
}

// TestCompactionTriggerUsesCurrentNotCumulative verifies the trigger bug fix
func TestCompactionTriggerUsesCurrentNotCumulative(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	// Create a model config with small context window and threshold 70
	mcID := uuid.New().String()
	encKey, _ := crypto.Encrypt([]byte("sk-test"), make([]byte, 32))
	// Use a simple model config with context_window 1000
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, created_by, created_at, updated_at)
		VALUES ($1,$2,'TestModel','openai','https://api.example.com/v1','gpt-4',$3,'{"compaction_threshold":70}',1000,$4,NOW(),NOW())
	`, mcID, orgID, encKey, userID)
	if err != nil {
		t.Fatalf("create model_config: %v", err)
	}
	// Agent with that model
	agentID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO agents (id, org_id, name, description, system_prompt, skill_ids, tool_ids, folder_id, max_turns, model_config_id, created_by, created_at, updated_at)
		VALUES ($1,$2,'TestAgent','', 'you are helpful','{}','{}',NULL,10,$3,$4,NOW(),NOW())
	`, agentID, orgID, mcID, userID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	_, _ = db.Pool.Exec(context.Background(), `INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions) VALUES ($1,'agent',$2,'user',$3, ARRAY['view','edit','delete']) ON CONFLICT DO NOTHING`, orgID, agentID, userID)
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)
	// Insert 11 dummy user messages to make len(chatMsgs) >10
	for i := 0; i < 11; i++ {
		_, err := db.Pool.Exec(context.Background(), `INSERT INTO agent_messages (id, session_id, role, content, created_at) VALUES ($1,$2,'user',$3,NOW())`, uuid.New().String(), sid, "history message "+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("insert history: %v", err)
		}
	}

	masterKey := make([]byte, 32)
	// Mock LLM that returns PromptTokens 200 (below 70% of 1000=700) but cumulative would be 200*4=800 >700
	// We need to simulate multiple turns? The simplest is to have the first turn return 200, and we check that compaction does NOT happen
	// We can do this by having a single turn with PromptTokens 200, and we check that no compaction event was emitted
	// Then a second test with PromptTokens 800 should emit compaction

	// First case: 200 should NOT trigger
	responses200 := []ChatResponse{
		{
			Choices: []Choice{{Message: ChatMessage{Content: "hello 200"}, FinishReason: "stop"}},
			Usage:   Usage{PromptTokens: 200, CompletionTokens: 10},
		},
	}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses200, &captured)
	// Update model_config base_url to mock server so ProcessMessage uses it
	db.Pool.Exec(context.Background(), `UPDATE model_configs SET base_url=$1 WHERE id=$2`, srv.URL, mcID)
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{"compaction_threshold": 70}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)
	var events []EngineEvent
	_, _, _, _, _, err = engine.ProcessMessage(context.Background(), sid, "test 200", nil, nil, masterKey, nil, nil, nil, nil, nil, func(evt EngineEvent) { events = append(events, evt) })
	if err != nil {
		t.Fatalf("ProcessMessage 200: %v", err)
	}
	hasCompaction := false
	for _, e := range events {
		if e.Type == "context_compacted" {
			hasCompaction = true
		}
	}
	if hasCompaction {
		t.Fatalf("compaction should NOT fire for current 200 < 700 (even though cumulative would be >700)")
	}
	t.Logf("correctly did not compact for 200 tokens")

	// Second case: 800 should trigger (since len>10 and 800>700)
	// Create a new session with same history
	sid2 := createTestSession(t, db, agentID, nbID, userID)
	for i := 0; i < 11; i++ {
		db.Pool.Exec(context.Background(), `INSERT INTO agent_messages (id, session_id, role, content, created_at) VALUES ($1,$2,'user',$3,NOW())`, uuid.New().String(), sid2, "history "+string(rune('a'+i)))
	}
	responses800 := []ChatResponse{
		{
			Choices: []Choice{{Message: ChatMessage{Content: "hello 800"}, FinishReason: "stop"}},
			Usage:   Usage{PromptTokens: 800, CompletionTokens: 10},
		},
	}
	var captured2 []map[string]any
	srv2 := newMockLLMServerWithCapture(t, masterKey, responses800, &captured2)
	defer srv2.Close()
	srv.Close()
	db.Pool.Exec(context.Background(), `UPDATE model_configs SET base_url=$1 WHERE id=$2`, srv2.URL, mcID)
	enc2, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv2.URL, "gpt-4", enc2, map[string]any{"compaction_threshold": 70}))
	events = nil
	_, _, _, _, _, err = engine.ProcessMessage(context.Background(), sid2, "test 800", nil, nil, masterKey, nil, nil, nil, nil, nil, func(evt EngineEvent) { events = append(events, evt) })
	if err != nil {
		t.Fatalf("ProcessMessage 800: %v", err)
	}
	hasCompaction = false
	for _, e := range events {
		if e.Type == "context_compacted" {
			hasCompaction = true
			if e.Summary == "" {
				t.Fatalf("compaction event should have summary")
			}
			if e.Tokens == nil || e.Tokens.Input == 0 {
				t.Fatalf("compaction event should have tokens")
			}
		}
	}
	if !hasCompaction {
		t.Fatalf("compaction should fire for current 800 > 700")
	}
	t.Logf("correctly compacted for 800 tokens")

	// Also verify that the compaction row is persisted
	// The compaction row should be in DB with role='compaction'
	var count int
	db.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_messages WHERE session_id=$1 AND role='compaction'`, sid2).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 compaction row, got %d", count)
	}
}

// TestCompactionEmitsEventAndPersists verifies compaction persistence and filtering
func TestCompactionEmitsEventAndPersists(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	mcID := uuid.New().String()
	encKey, _ := crypto.Encrypt([]byte("sk-test"), make([]byte, 32))
	_, err := db.Pool.Exec(context.Background(), `INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, created_by, created_at, updated_at) VALUES ($1,$2,'TestModel2','openai','https://api.example.com/v1','gpt-4',$3,'{"compaction_threshold":10}',100,$4,NOW(),NOW())`, mcID, orgID, encKey, userID)
	if err != nil {
		t.Fatalf("create model_config2: %v", err)
	}
	agentID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `INSERT INTO agents (id, org_id, name, description, system_prompt, skill_ids, tool_ids, folder_id, max_turns, model_config_id, created_by, created_at, updated_at) VALUES ($1,$2,'TestAgent2','', 'you are helpful','{}','{}',NULL,10,$3,$4,NOW(),NOW())`, agentID, orgID, mcID, userID)
	if err != nil {
		t.Fatalf("create agent2: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions) VALUES ($1,'agent',$2,'user',$3, ARRAY['view','edit','delete']) ON CONFLICT DO NOTHING`, orgID, agentID, userID)
	if err != nil {
		t.Fatalf("acl: %v", err)
	}
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)
	for i := 0; i < 12; i++ {
		db.Pool.Exec(context.Background(), `INSERT INTO agent_messages (id, session_id, role, content, created_at) VALUES ($1,$2,'user',$3,NOW())`, uuid.New().String(), sid, "msg "+string(rune('a'+i)))
	}
	masterKey := make([]byte, 32)
	// Need a mock that will be used for compactChatHistory's internal LLM call as well as the main call
	// The engine will make a call for the main turn (PromptTokens 50) and then for compaction it will make another call for summarization
	// We can set up a server that returns different responses based on request content
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		callCount++
		var resp ChatResponse
		// If the request is for summarization (contains "Summarize the following"), return a summary
		isSummary := false
		for _, m := range req.Messages {
			if len(m.Content) > 20 && strings.Contains(m.Content, "Summarize the following") {
				isSummary = true
				break
			}
		}
		if isSummary {
			resp = ChatResponse{
				Choices: []Choice{{Message: ChatMessage{Content: "summary of old messages"}, FinishReason: "stop"}},
				Usage:   Usage{PromptTokens: 20, CompletionTokens: 10},
			}
		} else {
			// Main call with large PromptTokens to trigger compaction
			resp = ChatResponse{
				Choices: []Choice{{Message: ChatMessage{Content: "final answer"}, FinishReason: "stop"}},
				Usage:   Usage{PromptTokens: 80, CompletionTokens: 10},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	db.Pool.Exec(context.Background(), `UPDATE model_configs SET base_url=$1 WHERE id=$2`, srv.URL, mcID)
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{"compaction_threshold": 10}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)
	var events []EngineEvent
	_, _, _, _, _, err = engine.ProcessMessage(context.Background(), sid, "trigger", nil, nil, masterKey, nil, nil, nil, nil, nil, func(evt EngineEvent) { events = append(events, evt) })
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	// Check for compaction event
	found := false
	for _, e := range events {
		if e.Type == "context_compacted" {
			found = true
			if e.Summary != "summary of old messages" {
				t.Fatalf("unexpected summary: %q", e.Summary)
			}
		}
	}
	if !found {
		t.Fatalf("expected context_compacted event, got %v", events)
	}
	// Check persistence
	var cnt int
	db.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM agent_messages WHERE session_id=$1 AND role='compaction'`, sid).Scan(&cnt)
	if cnt != 1 {
		t.Fatalf("expected 1 compaction row, got %d", cnt)
	}
	var summaryContent string
	db.Pool.QueryRow(context.Background(), `SELECT content FROM agent_messages WHERE session_id=$1 AND role='compaction'`, sid).Scan(&summaryContent)
	if summaryContent != "summary of old messages" {
		t.Fatalf("compaction content mismatch: %q", summaryContent)
	}
	// The compaction row is a durable boundary: the next turn starts with the
	// injected summary instead of re-sending the pre-compaction transcript.
	responses2 := []ChatResponse{
		{Choices: []Choice{{Message: ChatMessage{Content: "ok"}, FinishReason: "stop"}}, Usage: Usage{PromptTokens: 10, CompletionTokens: 5}},
	}
	var captured2 []map[string]any
	srv2 := newMockLLMServerWithCapture(t, masterKey, responses2, &captured2)
	defer srv2.Close()
	db.Pool.Exec(context.Background(), `UPDATE model_configs SET base_url=$1 WHERE id=$2`, srv2.URL, mcID)
	enc2, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv2.URL, "gpt-4", enc2, map[string]any{"compaction_threshold": 10}))
	_, _, _, _, _, err = engine.ProcessMessage(context.Background(), sid, "next", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("second ProcessMessage should succeed but got %v", err)
	}
	if len(captured2) != 1 {
		t.Fatalf("expected 1 LLM call on the second turn, got %d", len(captured2))
	}
	raw2, _ := json.Marshal(captured2[0])
	rawStr2 := string(raw2)
	for _, gone := range []string{`"msg a"`, `"msg b"`, `"msg c"`, `"msg d"`, `"msg e"`} {
		if strings.Contains(rawStr2, gone) {
			t.Fatalf("summarized message %s re-sent on the second turn: %s", gone, rawStr2)
		}
	}
	for _, want := range []string{`"msg f"`, `"msg l"`, "trigger"} {
		if !strings.Contains(rawStr2, want) {
			t.Fatalf("retained tail message %s missing on the second turn: %s", want, rawStr2)
		}
	}
	msgs2, _ := captured2[0]["messages"].([]any)
	if len(msgs2) < 2 {
		t.Fatalf("expected at least 2 messages on the second turn, got %v", captured2[0]["messages"])
	}
	summaryCount := 0
	for _, raw := range msgs2 {
		summaryMsg, _ := raw.(map[string]any)
		summaryContent, _ := summaryMsg["content"].(string)
		if summaryMsg["role"] == "system" && strings.Contains(summaryContent, "summary of old messages") {
			summaryCount++
		}
	}
	if summaryCount != 1 {
		t.Fatalf("expected exactly one injected summary, got %d: %v", summaryCount, msgs2)
	}
}

// TestCompactionIsDurableAcrossTurns verifies the persisted compaction row acts
// as a durable history boundary: the next turn re-sends the unsummarized tail,
// injects the summary exactly once, drops the summarized prefix, and keeps the
// post-boundary exchange.
func TestCompactionIsDurableAcrossTurns(t *testing.T) {
	env := setupCompactionTestEnv(t, 100, 10)
	for i := 0; i < 5; i++ {
		env.addHistory(t, "user", "PRECOMPACT-USER-"+string(rune('a'+i)))
		env.addHistory(t, "assistant", "PRECOMPACT-ASSISTANT-"+string(rune('a'+i)))
	}
	callID := uuid.New().String()
	if _, err := env.db.Pool.Exec(context.Background(), `
		INSERT INTO agent_messages (id, session_id, role, content, tool_calls, created_at)
		VALUES ($1,$2,'assistant','',$3,NOW())
	`, uuid.New().String(), env.sid, `[{"id":"`+callID+`","name":"read_cell","arguments":{}}]`); err != nil {
		t.Fatalf("insert assistant tool call: %v", err)
	}
	if _, err := env.db.Pool.Exec(context.Background(), `
		INSERT INTO agent_messages (id, session_id, role, content, tool_call_id, created_at)
		VALUES ($1,$2,'tool','PRECOMPACT-TOOL-RESULT',$3,NOW())
	`, uuid.New().String(), env.sid, callID); err != nil {
		t.Fatalf("insert tool result: %v", err)
	}

	srv := newCompactionScriptedServer(t, 80, "durable summary")
	defer srv.Close()
	env.useServer(t, srv, 10)
	if _, _, _, _, _, err := env.engine.ProcessMessage(context.Background(), env.sid, "PRECOMPACT-TURN1-QUESTION", nil, nil, env.masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("first ProcessMessage: %v", err)
	}

	responses := []ChatResponse{
		{Choices: []Choice{{Message: ChatMessage{Content: "second answer"}, FinishReason: "stop"}}, Usage: Usage{PromptTokens: 10, CompletionTokens: 5}},
	}
	var captured []map[string]any
	srv2 := newMockLLMServerWithCapture(t, env.masterKey, responses, &captured)
	defer srv2.Close()
	env.useServer(t, srv2, 10)
	if _, _, _, _, _, err := env.engine.ProcessMessage(context.Background(), env.sid, "second turn", nil, nil, env.masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("second ProcessMessage: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("expected exactly 1 LLM call on the second turn, got %d", len(captured))
	}
	raw, _ := json.Marshal(captured[0])
	rawStr := string(raw)
	msgs, _ := captured[0]["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages on the second turn, got %v", captured[0]["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	firstContent, _ := first["content"].(string)
	if first["role"] != "system" || !strings.Contains(firstContent, "you are helpful") {
		t.Fatalf("first message should be the system prompt, got %v", first)
	}
	if n := strings.Count(rawStr, "durable summary"); n != 1 {
		t.Fatalf("expected exactly one injected summary, got %d: %s", n, rawStr)
	}
	for _, want := range []string{
		"PRECOMPACT-TURN1-QUESTION",
		"main answer",
		"PRECOMPACT-ASSISTANT-c",
		"PRECOMPACT-USER-d",
		"PRECOMPACT-ASSISTANT-d",
		"PRECOMPACT-USER-e",
		"PRECOMPACT-ASSISTANT-e",
		"PRECOMPACT-TOOL-RESULT",
	} {
		if !strings.Contains(rawStr, want) {
			t.Fatalf("retained tail message %q missing from second turn: %s", want, rawStr)
		}
	}
	for _, gone := range []string{
		"PRECOMPACT-USER-a",
		"PRECOMPACT-ASSISTANT-a",
		"PRECOMPACT-USER-b",
		"PRECOMPACT-ASSISTANT-b",
		"PRECOMPACT-USER-c",
	} {
		if strings.Contains(rawStr, gone) {
			t.Fatalf("summarized prefix message %q re-sent on second turn: %s", gone, rawStr)
		}
	}
}

// TestCompactionLatestBoundaryWins drives two compactions and verifies that only
// the latest summary is injected while the tail kept before the latest boundary
// is retained.
func TestCompactionLatestBoundaryWins(t *testing.T) {
	env := setupCompactionTestEnv(t, 100, 10)
	for i := 0; i < 12; i++ {
		env.addHistory(t, "user", "HIST-"+string(rune('a'+i)))
	}

	mainCalls := 0
	summaryCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		isSummary := false
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "Summarize the following") {
				isSummary = true
				break
			}
		}
		var resp ChatResponse
		if isSummary {
			summaryCalls++
			resp = ChatResponse{
				Choices: []Choice{{Message: ChatMessage{Content: "summary number " + strconv.Itoa(summaryCalls)}, FinishReason: "stop"}},
				Usage:   Usage{PromptTokens: 20, CompletionTokens: 10},
			}
		} else {
			mainCalls++
			if mainCalls == 1 {
				resp = ChatResponse{
					Choices: []Choice{{
						Message: ChatMessage{ToolCalls: []ToolCall{{
							ID:   uuid.New().String(),
							Type: "function",
							Function: struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							}{Name: "not_a_real_tool", Arguments: "{}"},
						}}},
						FinishReason: "tool_calls",
					}},
					Usage: Usage{PromptTokens: 80, CompletionTokens: 10},
				}
			} else {
				resp = ChatResponse{
					Choices: []Choice{{Message: ChatMessage{Content: "latest answer"}, FinishReason: "stop"}},
					Usage:   Usage{PromptTokens: 80, CompletionTokens: 10},
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	env.useServer(t, srv, 10)
	if _, _, _, _, _, err := env.engine.ProcessMessage(context.Background(), env.sid, "PRECOMPACT-QUESTION", nil, nil, env.masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("first ProcessMessage: %v", err)
	}
	if mainCalls != 2 || summaryCalls != 2 {
		t.Fatalf("expected 2 main + 2 summary calls, got %d + %d", mainCalls, summaryCalls)
	}

	responses := []ChatResponse{
		{Choices: []Choice{{Message: ChatMessage{Content: "next answer"}, FinishReason: "stop"}}, Usage: Usage{PromptTokens: 5, CompletionTokens: 5}},
	}
	var captured []map[string]any
	srv2 := newMockLLMServerWithCapture(t, env.masterKey, responses, &captured)
	defer srv2.Close()
	env.useServer(t, srv2, 10)
	if _, _, _, _, _, err := env.engine.ProcessMessage(context.Background(), env.sid, "third turn", nil, nil, env.masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("second ProcessMessage: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("expected exactly 1 LLM call on the third turn, got %d", len(captured))
	}
	raw, _ := json.Marshal(captured[0])
	rawStr := string(raw)
	if n := strings.Count(rawStr, "summary number 2"); n != 1 {
		t.Fatalf("expected exactly one latest summary, got %d: %s", n, rawStr)
	}
	if strings.Contains(rawStr, "summary number 1") {
		t.Fatalf("superseded summary re-injected: %s", rawStr)
	}
	for _, want := range []string{
		"HIST-h",
		"HIST-i",
		"HIST-j",
		"HIST-k",
		"HIST-l",
		"PRECOMPACT-QUESTION",
		"tool not available: not_a_real_tool",
		"latest answer",
	} {
		if !strings.Contains(rawStr, want) {
			t.Fatalf("retained tail message %q missing from third turn: %s", want, rawStr)
		}
	}
	for _, gone := range []string{"HIST-a", "HIST-f", "HIST-g"} {
		if strings.Contains(rawStr, gone) {
			t.Fatalf("summarized message %q re-sent on third turn: %s", gone, rawStr)
		}
	}
}

// TestCompactionEventAndRowCarryAfterTokens verifies the persisted compaction
// row and the emitted event carry the real before/after token counts.
func TestCompactionEventAndRowCarryAfterTokens(t *testing.T) {
	env := setupCompactionTestEnv(t, 100, 10)
	for i := 0; i < 12; i++ {
		env.addHistory(t, "user", "history "+string(rune('a'+i)))
	}

	srv := newCompactionScriptedServer(t, 80, "carried summary")
	defer srv.Close()
	env.useServer(t, srv, 10)

	var events []EngineEvent
	if _, _, _, _, _, err := env.engine.ProcessMessage(context.Background(), env.sid, "trigger", nil, nil, env.masterKey, nil, nil, nil, nil, nil, func(evt EngineEvent) { events = append(events, evt) }); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}

	var tokensDirect int
	var tokensAfter *int
	var keptCount *int
	err := env.db.Pool.QueryRow(context.Background(), `
		SELECT tokens_direct, tokens_after, kept_count FROM agent_messages
		WHERE session_id=$1 AND role='compaction'
	`, env.sid).Scan(&tokensDirect, &tokensAfter, &keptCount)
	if err != nil {
		t.Fatalf("query compaction row: %v", err)
	}
	if tokensDirect <= 0 {
		t.Fatalf("compaction row tokens_direct = %d, want > 0", tokensDirect)
	}
	if tokensAfter == nil || *tokensAfter <= 0 {
		t.Fatalf("compaction row tokens_after = %v, want non-nil and > 0", tokensAfter)
	}
	if keptCount == nil || *keptCount <= 0 {
		t.Fatalf("compaction row kept_count = %v, want non-nil and > 0", keptCount)
	}

	found := false
	for _, e := range events {
		if e.Type == "context_compacted" {
			found = true
			if e.Tokens == nil {
				t.Fatalf("context_compacted event missing tokens")
			}
			if e.Tokens.Input != tokensDirect {
				t.Fatalf("event Tokens.Input = %d, want %d", e.Tokens.Input, tokensDirect)
			}
			if e.Tokens.ContextCurrent != *tokensAfter {
				t.Fatalf("event Tokens.ContextCurrent = %d, want %d", e.Tokens.ContextCurrent, *tokensAfter)
			}
		}
	}
	if !found {
		t.Fatalf("expected context_compacted event, got %v", events)
	}
}

type compactionTestEnv struct {
	db        *database.DB
	engine    *Engine
	sid       string
	mcID      string
	masterKey []byte
}

func setupCompactionTestEnv(t *testing.T, contextWindow, threshold int) *compactionTestEnv {
	t.Helper()
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	mcID := uuid.New().String()
	encKey, _ := crypto.Encrypt([]byte("sk-test"), make([]byte, 32))
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, created_by, created_at, updated_at)
		VALUES ($1,$2,'CompactionModel','openai','https://api.example.com/v1','gpt-4',$3,$4,$5,$6,NOW(),NOW())
	`, mcID, orgID, encKey, `{"compaction_threshold":`+strconv.Itoa(threshold)+`}`, contextWindow, userID)
	if err != nil {
		t.Fatalf("create model_config: %v", err)
	}
	agentID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO agents (id, org_id, name, description, system_prompt, skill_ids, tool_ids, folder_id, max_turns, model_config_id, created_by, created_at, updated_at)
		VALUES ($1,$2,'CompactionAgent','','you are helpful','{}','{}',NULL,10,$3,$4,NOW(),NOW())
	`, agentID, orgID, mcID, userID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions) VALUES ($1,'agent',$2,'user',$3, ARRAY['view','edit','delete']) ON CONFLICT DO NOTHING`, orgID, agentID, userID)
	if err != nil {
		t.Fatalf("acl: %v", err)
	}
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)
	env := &compactionTestEnv{db: db, engine: engine, sid: sid, mcID: mcID, masterKey: make([]byte, 32)}
	env.engine.pool = db.Pool
	env.engine.session = NewSessionStore(db.Pool)
	return env
}

func (env *compactionTestEnv) addHistory(t *testing.T, role, content string) {
	t.Helper()
	_, err := env.db.Pool.Exec(context.Background(), `
		INSERT INTO agent_messages (id, session_id, role, content, created_at)
		VALUES ($1,$2,$3,$4,NOW())
	`, uuid.New().String(), env.sid, role, content)
	if err != nil {
		t.Fatalf("insert history message: %v", err)
	}
}

func (env *compactionTestEnv) useServer(t *testing.T, srv *httptest.Server, threshold int) {
	t.Helper()
	if _, err := env.db.Pool.Exec(context.Background(), `UPDATE model_configs SET base_url=$1 WHERE id=$2`, srv.URL, env.mcID); err != nil {
		t.Fatalf("update model_config base_url: %v", err)
	}
	enc, _ := crypto.Encrypt([]byte("sk-test"), env.masterKey)
	env.engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{"compaction_threshold": threshold}))
}

func newCompactionScriptedServer(t *testing.T, mainPromptTokens int, summary string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		isSummary := false
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "Summarize the following") {
				isSummary = true
				break
			}
		}
		resp := ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "main answer"}, FinishReason: "stop"}},
			Usage:   Usage{PromptTokens: mainPromptTokens, CompletionTokens: 10},
		}
		if isSummary {
			resp = ChatResponse{
				Choices: []Choice{{Message: ChatMessage{Content: summary}, FinishReason: "stop"}},
				Usage:   Usage{PromptTokens: 20, CompletionTokens: 10},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}
