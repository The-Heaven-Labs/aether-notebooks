package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/crypto"
)

func TestSessionUsageAccumulates(t *testing.T) {
	db := setupTestDB(t)
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	agentID := createSessionTestAgent(t, db.Pool, orgID, userID)

	store := agent.NewSessionStore(db.Pool)
	ctx := context.Background()
	session, err := store.CreateSession(ctx, agentID, nbID, userID, 10, nil)
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

func TestProcessMessagePersistsSessionUsage(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	agentID := createSessionTestAgent(t, db.Pool, orgID, userID)

	masterKey := make([]byte, 32)
	encKey, err := crypto.Encrypt([]byte("sk-test"), masterKey)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(agent.ChatResponse{
			Choices: []agent.Choice{{Message: agent.ChatMessage{Content: "hi"}, FinishReason: "stop"}},
			Usage:   agent.Usage{PromptTokens: 120, CompletionTokens: 10, TotalTokens: 130},
		})
	}))
	defer srv.Close()

	mcID := uuid.New().String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, created_by, created_at, updated_at)
		VALUES ($1,$2,'UsageModel','openai',$3,'gpt-4',$4,'{}',64000,$5,NOW(),NOW())
	`, mcID, orgID, srv.URL, encKey, userID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `UPDATE agents SET model_config_id = $1 WHERE id = $2`, mcID, agentID)
	require.NoError(t, err)

	store := agent.NewSessionStore(db.Pool)
	session, err := store.CreateSession(ctx, agentID, nbID, userID, 10, nil)
	require.NoError(t, err)

	engine := agent.NewEngine(ctx, db.Pool, nil)
	engine.SetLLMClient(agent.NewLLMClient(srv.URL, "gpt-4", encKey, nil))

	var events []agent.EngineEvent
	_, _, _, _, _, err = engine.ProcessMessage(ctx, session.ID, "hello", nil, nil, masterKey, nil, nil, nil, nil, nil, func(evt agent.EngineEvent) {
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

	u, err := store.GetUsage(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, int64(120), u.Input)
	require.Equal(t, int64(10), u.Output)
	require.Equal(t, 1, u.ModelCalls)
	require.Equal(t, int64(120), u.ContextTokens)
	require.Equal(t, 64000, u.ContextWindow)
}

func TestProcessMessagePersistsCompactionUsage(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	orgID, userID := createTestOrgAndUser(t, db.Pool)
	nbID := createTestNotebook(t, db.Pool, orgID, userID)
	agentID := createSessionTestAgent(t, db.Pool, orgID, userID)

	masterKey := make([]byte, 32)
	encKey, err := crypto.Encrypt([]byte("sk-test"), masterKey)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		isSummary := false
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "Summarize the following") {
				isSummary = true
				break
			}
		}
		resp := agent.ChatResponse{
			Choices: []agent.Choice{{Message: agent.ChatMessage{Content: "main answer"}, FinishReason: "stop"}},
			Usage:   agent.Usage{PromptTokens: 800, CompletionTokens: 10, TotalTokens: 810},
		}
		if isSummary {
			resp = agent.ChatResponse{
				Choices: []agent.Choice{{Message: agent.ChatMessage{Content: "summary"}, FinishReason: "stop"}},
				Usage:   agent.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	mcID := uuid.New().String()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted, default_params, context_window, created_by, created_at, updated_at)
		VALUES ($1,$2,'CompactionUsageModel','openai',$3,'gpt-4',$4,'{"compaction_threshold":50}',1000,$5,NOW(),NOW())
	`, mcID, orgID, srv.URL, encKey, userID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `UPDATE agents SET model_config_id = $1 WHERE id = $2`, mcID, agentID)
	require.NoError(t, err)

	store := agent.NewSessionStore(db.Pool)
	session, err := store.CreateSession(ctx, agentID, nbID, userID, 10, nil)
	require.NoError(t, err)

	for i := 0; i < 12; i++ {
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO agent_messages (id, session_id, role, content, created_at)
			VALUES ($1,$2,'user',$3,NOW())
		`, uuid.New().String(), session.ID, "history message")
		require.NoError(t, err)
	}

	engine := agent.NewEngine(ctx, db.Pool, nil)
	engine.SetLLMClient(agent.NewLLMClient(srv.URL, "gpt-4", encKey, nil))

	var events []agent.EngineEvent
	_, _, _, _, _, err = engine.ProcessMessage(ctx, session.ID, "trigger", nil, nil, masterKey, nil, nil, nil, nil, nil, func(evt agent.EngineEvent) {
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
	u, err := store.GetUsage(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, int64(820), u.Input)
	require.Equal(t, int64(20), u.Output)
	require.Equal(t, 2, u.ModelCalls)
	require.Equal(t, int64(800), u.ContextTokens)
}
