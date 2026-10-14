package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/crypto"
)

func TestProcessMessageSendsModelConfigCustomHeaders(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	masterKey := testMasterKey()

	var gotHeaders http.Header
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "done"}, FinishReason: "stop"}},
			Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
		})
	}))
	defer llmSrv.Close()

	encKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	secret, _ := crypto.EncryptString("topsecret", masterKey)
	customHeaders, _ := json.Marshal(map[string]any{"X-Title": "Aether", "X-Api-Key": secret})

	mcID := uuid.New().String()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted,
			default_params, context_window, custom_headers, custom_header_secrets, created_by, created_at, updated_at)
		VALUES ($1,$2,'HeaderModel','openai',$3,'gpt-4',$4,'{}',128000,$5,ARRAY['X-Api-Key'],$6,NOW(),NOW())
	`, mcID, orgID, llmSrv.URL, encKey, customHeaders, userID); err != nil {
		t.Fatalf("create model config: %v", err)
	}

	agentID := uuid.New().String()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO agents (id, org_id, name, description, system_prompt, skill_ids, tool_ids, folder_id, max_turns, model_config_id, created_by, created_at, updated_at)
		VALUES ($1,$2,'HeaderAgent','','you are helpful','{}','{}',NULL,10,$3,$4,NOW(),NOW())
	`, agentID, orgID, mcID, userID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	_, _ = db.Pool.Exec(context.Background(), `INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions) VALUES ($1,'agent',$2,'user',$3, ARRAY['view','edit','delete']) ON CONFLICT DO NOTHING`, orgID, agentID, userID)

	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool, nil)

	if _, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hello", nil, nil, masterKey, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	if gotHeaders.Get("X-Title") != "Aether" {
		t.Fatalf("expected X-Title on LLM request, got %q", gotHeaders.Get("X-Title"))
	}
	if gotHeaders.Get("X-Api-Key") != "topsecret" {
		t.Fatalf("expected decrypted X-Api-Key on LLM request, got %q", gotHeaders.Get("X-Api-Key"))
	}
}

func TestDefaultSubagentLLMSendsModelConfigCustomHeaders(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	engine := newTestEngine(db)
	masterKey := testMasterKey()

	var gotHeaders http.Header
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "done"}, FinishReason: "stop"}},
			Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
		})
	}))
	defer llmSrv.Close()

	encKey, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	secret, _ := crypto.EncryptString("topsecret", masterKey)
	customHeaders, _ := json.Marshal(map[string]any{"X-Title": "Aether", "X-Api-Key": secret})

	mcID := uuid.New().String()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO model_configs (id, org_id, name, provider, base_url, model, api_key_encrypted,
			default_params, context_window, custom_headers, custom_header_secrets, created_by, created_at, updated_at)
		VALUES ($1,$2,'HeaderModel','openai',$3,'gpt-4',$4,'{}',128000,$5,ARRAY['X-Api-Key'],$6,NOW(),NOW())
	`, mcID, orgID, llmSrv.URL, encKey, customHeaders, userID); err != nil {
		t.Fatalf("create model config: %v", err)
	}

	agentID := uuid.New().String()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO agents (id, org_id, name, description, system_prompt, skill_ids, tool_ids, folder_id, max_turns, model_config_id, created_by, created_at, updated_at)
		VALUES ($1,$2,'HeaderAgent','','you are helpful','{}','{}',NULL,10,$3,$4,NOW(),NOW())
	`, agentID, orgID, mcID, userID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// defaultSubagentLLM silently falls back to e.llm (nil here) when its
	// SELECT scan drifts, so a nil client fails this test loudly.
	client := engine.defaultSubagentLLM(context.Background(), db.Pool, agentID, masterKey)
	if client == nil {
		t.Fatal("expected subagent LLM client from model config")
	}
	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}}, nil, masterKey); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if gotHeaders.Get("X-Title") != "Aether" {
		t.Fatalf("expected X-Title on LLM request, got %q", gotHeaders.Get("X-Title"))
	}
	if gotHeaders.Get("X-Api-Key") != "topsecret" {
		t.Fatalf("expected decrypted X-Api-Key on LLM request, got %q", gotHeaders.Get("X-Api-Key"))
	}
}
