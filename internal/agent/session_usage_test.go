package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/models"
)

func TestSessionUsageAccumulates(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	agentID := createSessionTestAgent(t, db.Pool, orgID, userID)

	store := agent.NewSessionStore(db.Pool)
	ctx := context.Background()
	session, err := store.CreateSession(ctx, agentID, nbID, userID, 10, nil, false, false, false)
	require.NoError(t, err)

	require.NoError(t, store.AddUsage(ctx, session.ID, agent.SessionUsageDelta{
		Input: 100, Output: 20, ModelCalls: 1, ContextTokens: 120, ContextWindow: 128000,
	}))
	require.NoError(t, store.AddUsage(ctx, session.ID, agent.SessionUsageDelta{
		Input: 50, Output: 5, ModelCalls: 1, ContextTokens: 90,
	}))

	u, err := store.GetUsage(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, int64(150), u.Input)
	require.Equal(t, int64(25), u.Output)
	require.Equal(t, 2, u.ModelCalls)
	require.Equal(t, int64(90), u.ContextTokens)
	require.Equal(t, 128000, u.ContextWindow)

	// GetSession must expose the same totals (the V099 columns were dead).
	sess, err := store.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, int64(150), sess.TotalInput)
	require.Equal(t, int64(25), sess.TotalOutput)
	require.Equal(t, 2, sess.TotalModelCalls)
	require.Equal(t, int64(90), sess.ContextTokens)
	require.Equal(t, 128000, sess.ContextWindow)
}

// usageTestEnv bundles the engine, store and session used by the
// ProcessMessage/RunQueuedTasks token-accounting tests.
type usageTestEnv struct {
	db        *database.DB
	engine    *agent.Engine
	store     *agent.SessionStore
	sessionID string
	masterKey []byte
	apiKey    []byte
}

func setupUsageTestEnv(t *testing.T, baseURL, defaultParams string, contextWindow int) *usageTestEnv {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	agentID := createSessionTestAgent(t, db.Pool, orgID, userID)

	masterKey := make([]byte, 32)
	encKey, err := crypto.Encrypt([]byte("sk-test"), masterKey)
	require.NoError(t, err)

	mcID := uuid.New().String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, created_by, created_at, updated_at)
		VALUES ($1,$2,'UsageModel','openai',$3,'gpt-4',$4,$5,$6,$7,NOW(),NOW())
	`, mcID, orgID, baseURL, encKey, defaultParams, contextWindow, userID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `UPDATE agents SET model_config_id = $1 WHERE id = $2`, mcID, agentID)
	require.NoError(t, err)

	store := agent.NewSessionStore(db.Pool)
	session, err := store.CreateSession(ctx, agentID, nbID, userID, 10, nil, false, false, false)
	require.NoError(t, err)

	engine := agent.NewEngine(ctx, db.Pool, nil)
	engine.SetLLMClient(agent.NewLLMClient(baseURL, "gpt-4", encKey, nil))

	return &usageTestEnv{db: db, engine: engine, store: store, sessionID: session.ID, masterKey: masterKey, apiKey: encKey}
}

// scriptedLLMServer returns canned ChatResponse bodies in call order.
func scriptedLLMServer(t *testing.T, responses []agent.ChatResponse) *httptest.Server {
	t.Helper()
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := i
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		i++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responses[idx])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProcessMessagePersistsSessionUsage(t *testing.T) {
	srv := scriptedLLMServer(t, []agent.ChatResponse{{
		Choices: []agent.Choice{{Message: agent.ChatMessage{Content: "hi"}, FinishReason: "stop"}},
		Usage:   agent.Usage{PromptTokens: 120, CompletionTokens: 10, TotalTokens: 130},
	}})
	env := setupUsageTestEnv(t, srv.URL, "{}", 64000)

	var events []agent.EngineEvent
	_, _, _, _, _, err := env.engine.ProcessMessage(context.Background(), env.sessionID, "hello", nil, nil, env.masterKey, nil, nil, nil, nil, nil, func(evt agent.EngineEvent) {
		events = append(events, evt)
	})
	require.NoError(t, err)

	var tokenEvent *agent.EngineEvent
	for i := range events {
		if events[i].Type == "token_update" {
			tokenEvent = &events[i]
		}
	}
	require.NotNil(t, tokenEvent, "expected a token_update event")
	require.NotNil(t, tokenEvent.SessionUsage, "token_update must carry session_usage")
	require.Equal(t, int64(120), tokenEvent.SessionUsage.Input)
	require.Equal(t, int64(10), tokenEvent.SessionUsage.Output)
	require.Equal(t, 1, tokenEvent.SessionUsage.ModelCalls)
	require.Equal(t, int64(120), tokenEvent.SessionUsage.ContextTokens)
	require.Equal(t, 64000, tokenEvent.SessionUsage.ContextWindow)

	u, err := env.store.GetUsage(context.Background(), env.sessionID)
	require.NoError(t, err)
	require.Equal(t, int64(120), u.Input)
	require.Equal(t, int64(10), u.Output)
	require.Equal(t, 1, u.ModelCalls)
	require.Equal(t, int64(120), u.ContextTokens)
	require.Equal(t, 64000, u.ContextWindow)
}

func TestProcessMessagePersistsCompactionUsage(t *testing.T) {
	srv := scriptedLLMServer(t, []agent.ChatResponse{
		{
			Choices: []agent.Choice{{Message: agent.ChatMessage{Content: "main answer"}, FinishReason: "stop"}},
			Usage:   agent.Usage{PromptTokens: 800, CompletionTokens: 10, TotalTokens: 810},
		},
		{
			Choices: []agent.Choice{{Message: agent.ChatMessage{Content: "summary"}, FinishReason: "stop"}},
			Usage:   agent.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		},
	})
	env := setupUsageTestEnv(t, srv.URL, `{"compaction_threshold":50}`, 1000)
	ctx := context.Background()

	for i := 0; i < 12; i++ {
		_, err := env.db.Pool.Exec(ctx, `
			INSERT INTO agent_messages (id, session_id, role, content, created_at)
			VALUES ($1,$2,'user',$3,NOW())
		`, uuid.New().String(), env.sessionID, "history message")
		require.NoError(t, err)
	}

	var events []agent.EngineEvent
	_, _, _, _, _, err := env.engine.ProcessMessage(ctx, env.sessionID, "trigger", nil, nil, env.masterKey, nil, nil, nil, nil, nil, func(evt agent.EngineEvent) {
		events = append(events, evt)
	})
	require.NoError(t, err)

	compacted := false
	for _, evt := range events {
		if evt.Type == "context_compacted" {
			compacted = true
		}
	}
	require.True(t, compacted, "expected a context_compacted event")

	// The main call (800/10) plus the summarization call (20/10).
	u, err := env.store.GetUsage(ctx, env.sessionID)
	require.NoError(t, err)
	require.Equal(t, int64(820), u.Input)
	require.Equal(t, int64(20), u.Output)
	require.Equal(t, 2, u.ModelCalls)

	// The compaction delta must persist the post-compaction context estimate,
	// not leave the pre-compaction prompt tokens in place.
	var tokensAfter int
	require.NoError(t, env.db.Pool.QueryRow(ctx, `
		SELECT tokens_after FROM agent_messages WHERE session_id = $1 AND role = 'compaction'
	`, env.sessionID).Scan(&tokensAfter))
	require.Greater(t, tokensAfter, 0)
	require.Equal(t, int64(tokensAfter), u.ContextTokens)
	require.Less(t, u.ContextTokens, int64(800))
	require.Equal(t, 1000, u.ContextWindow)
}

// A synchronous tool (e.g. spawn_subagents) can accrue usage to the session
// mid-turn. The running snapshot must be refreshed after tool execution so the
// next token_update/done does not regress below what was already emitted.
func TestProcessMessageRefreshesUsageAfterToolExecution(t *testing.T) {
	toolCall := agent.ToolCall{ID: "call-1", Type: "function"}
	toolCall.Function.Name = "accrue_like_subagent"
	toolCall.Function.Arguments = "{}"

	srv := scriptedLLMServer(t, []agent.ChatResponse{
		{
			Choices: []agent.Choice{{
				Message:      agent.ChatMessage{ToolCalls: []agent.ToolCall{toolCall}},
				FinishReason: "tool_calls",
			}},
			Usage: agent.Usage{PromptTokens: 50, CompletionTokens: 5, TotalTokens: 55},
		},
		{
			Choices: []agent.Choice{{Message: agent.ChatMessage{Content: "final"}, FinishReason: "stop"}},
			Usage:   agent.Usage{PromptTokens: 60, CompletionTokens: 10, TotalTokens: 70},
		},
	})
	env := setupUsageTestEnv(t, srv.URL, "{}", 64000)

	tool := &agent.ToolDef{Timeout: agent.NoTimeout}
	tool.Function.Name = "accrue_like_subagent"
	tool.Function.Description = "accrues subagent-like usage mid-turn"
	tool.Function.Parameters = map[string]any{"type": "object"}
	tool.Handler = func(args json.RawMessage, tc *agent.ToolContext) (any, error) {
		if err := env.store.AddUsage(tc.Context, env.sessionID, agent.SessionUsageDelta{
			SubagentInput: 40, SubagentOutput: 5, ModelCalls: 1,
		}); err != nil {
			return nil, err
		}
		return "accrued", nil
	}

	var tokenUpdates []agent.EngineEvent
	_, _, _, _, _, err := env.engine.ProcessMessage(context.Background(), env.sessionID, "go", nil, []*agent.ToolDef{tool}, env.masterKey, nil, nil, nil, nil, nil, func(evt agent.EngineEvent) {
		if evt.Type == "token_update" {
			tokenUpdates = append(tokenUpdates, evt)
		}
	})
	require.NoError(t, err)
	require.Len(t, tokenUpdates, 2)

	require.NotNil(t, tokenUpdates[0].SessionUsage)
	require.Equal(t, int64(0), tokenUpdates[0].SessionUsage.SubagentInput)
	require.Equal(t, 1, tokenUpdates[0].SessionUsage.ModelCalls)

	require.NotNil(t, tokenUpdates[1].SessionUsage)
	require.Equal(t, int64(40), tokenUpdates[1].SessionUsage.SubagentInput)
	require.Equal(t, int64(5), tokenUpdates[1].SessionUsage.SubagentOutput)
	require.Equal(t, 3, tokenUpdates[1].SessionUsage.ModelCalls)
}

// RunQueuedTasks must accrue completed subagent tokens to the parent session
// (DB totals) and emit the refreshed snapshot on subagent_status.
func TestRunQueuedTasksAccruesSubagentUsage(t *testing.T) {
	srv := scriptedLLMServer(t, []agent.ChatResponse{{
		Choices: []agent.Choice{{Message: agent.ChatMessage{Content: "subagent done"}, FinishReason: "stop"}},
		Usage:   agent.Usage{PromptTokens: 70, CompletionTokens: 15, TotalTokens: 85},
	}})
	env := setupUsageTestEnv(t, srv.URL, "{}", 64000)
	ctx := context.Background()

	taskID := uuid.New().String()
	_, err := env.db.Pool.Exec(ctx, `
		INSERT INTO subagent_tasks (id, parent_session_id, goal, context, status, created_at)
		VALUES ($1, $2, 'do the thing', '{}'::jsonb, 'queued', NOW())
	`, taskID, env.sessionID)
	require.NoError(t, err)

	var broadcast []map[string]any
	broadcastFn := func(notebookID string, msg any) {
		if m, ok := msg.(map[string]any); ok {
			broadcast = append(broadcast, m)
		}
	}

	llm := agent.NewLLMClient(srv.URL, "gpt-4", env.apiKey, nil)
	results := env.engine.RunQueuedTasks(ctx, env.sessionID, []string{taskID}, env.masterKey, broadcastFn, "", llm, nil)
	require.Len(t, results, 1)
	require.Equal(t, "completed", results[0].Status)

	u, err := env.store.GetUsage(ctx, env.sessionID)
	require.NoError(t, err)
	require.Equal(t, int64(70), u.SubagentInput)
	require.Equal(t, int64(15), u.SubagentOutput)
	require.Equal(t, 1, u.ModelCalls)

	var completion map[string]any
	for _, evt := range broadcast {
		if evt["type"] == "subagent_status" && evt["status"] == "completed" {
			completion = evt
		}
	}
	require.NotNil(t, completion, "expected a completed subagent_status event, got %v", broadcast)
	usage, ok := completion["session_usage"].(*models.SessionUsage)
	require.True(t, ok, "session_usage type = %T", completion["session_usage"])
	require.Equal(t, int64(70), usage.SubagentInput)
	require.Equal(t, int64(15), usage.SubagentOutput)
	require.Equal(t, 1, usage.ModelCalls)
}
