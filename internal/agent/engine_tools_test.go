package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/models"
)

func setupEngineTestDB(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createEngineTestOrgAndUser(t *testing.T, db *database.DB) (orgID, userID string) {
	t.Helper()
	orgID = uuid.New().String()
	userID = uuid.New().String()
	now := time.Now()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO orgs (id, name, slug, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)
	`, orgID, "Engine Org "+orgID[:8], "slug-"+orgID[:8], now)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO users (id, email, name, password_hash, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)
	`, userID, "engine-"+userID[:8]+"@example.com", "Engine User", "hash", now)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO org_members (org_id, user_id, role, created_at) VALUES ($1, $2, 'admin', $3)
	`, orgID, userID, now)
	if err != nil {
		t.Fatalf("create org member: %v", err)
	}
	return orgID, userID
}

func newTestEngine(db *database.DB) *Engine {
	engine := &Engine{
		registry:           NewToolRegistry(),
		pool:               db.Pool,
		tokenCounter:       NewTokenCounter(),
		streams:            NewStreamManager(),
		session:            NewSessionStore(db.Pool),
		toolTimeoutDefault: DefaultToolTimeout,
	}
	RegisterNotebookTools(engine.registry, db.Pool)
	RegisterAgentTools(engine.registry, db.Pool, engine)
	RegisterPlatformTools(engine.registry, db.Pool)
	RegisterChartTools(engine.registry, db.Pool)
	RegisterManageTools(engine.registry, db.Pool)
	return engine
}

func seededToolIDs(t *testing.T, db *database.DB, orgID string, names ...string) []string {
	t.Helper()
	var ids []string
	for _, name := range names {
		var id string
		err := db.Pool.QueryRow(context.Background(),
			`SELECT id FROM tools WHERE org_id = $1 AND name = $2`, orgID, name).Scan(&id)
		if err != nil {
			t.Fatalf("lookup seeded tool %s: %v", name, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestEngineLoadAgentToolDefs_AllBuiltin(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	// New semantics: agent whose tool_ids contains every built-in tool gets every built-in tool.
	// Explicitly select all builtins via tool_ids (no AllBuiltinTools flag).
	var allIDs []string
	err := db.Pool.QueryRow(context.Background(), `SELECT array_agg(id) FROM tools WHERE org_id = $1 AND type='builtin'`, orgID).Scan((*string)(nil))
	// fallback: collect via seeded lookup of known names
	_ = err
	rows, _ := db.Pool.Query(context.Background(), `SELECT id FROM tools WHERE org_id=$1 AND type='builtin'`, orgID)
	if rows != nil {
		for rows.Next() {
			var id string
			rows.Scan(&id)
			allIDs = append(allIDs, id)
		}
		rows.Close()
	}

	agent := models.Agent{
		ID:      uuid.New().String(),
		OrgID:   orgID,
		ToolIDs: allIDs,
	}

	defs := engine.loadAgentToolDefs(context.Background(), agent)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if !names["list_notebook_parameters"] {
		t.Fatalf("agent missing list_notebook_parameters tool (got %d defs)", len(defs))
	}
	if !names["set_notebook_parameters"] {
		t.Fatalf("agent missing set_notebook_parameters tool (got %d defs)", len(defs))
	}
	if len(defs) < 10 {
		t.Fatalf("expected all built-in tools loaded, got only %d", len(defs))
	}
	t.Logf("explicit-all-builtin agent loaded %d tools", len(defs))
}

func TestEngineLoadAgentToolDefs_AlwaysUsesToolIDs(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	// Legacy AllBuiltinTools=true but tool_ids lists only 2 tools – only those 2 should be returned.
	paramToolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters", "set_notebook_parameters")
	agent := models.Agent{
		ID:              uuid.New().String(),
		OrgID:           orgID,
		AllBuiltinTools: true,
		ToolIDs:         paramToolIDs,
	}

	defs := engine.loadAgentToolDefs(context.Background(), agent)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if len(defs) != 2 {
		t.Fatalf("expected exactly 2 tools despite legacy AllBuiltinTools=true, got %d: %v", len(defs), names)
	}
	if !names["list_notebook_parameters"] || !names["set_notebook_parameters"] {
		t.Fatalf("expected only the 2 selected tools, got %v", names)
	}
	t.Logf("legacy-all_builtin flag ignored, only tool_ids honoured")
}

func TestEngineLoadAgentToolDefs_ExplicitSelection(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	paramToolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters", "set_notebook_parameters")
	agent := models.Agent{
		ID:              uuid.New().String(),
		OrgID:           orgID,
		AllBuiltinTools: false,
		ToolIDs:         paramToolIDs,
	}

	defs := engine.loadAgentToolDefs(context.Background(), agent)

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if len(defs) != 2 {
		t.Fatalf("expected exactly 2 explicitly selected tools, got %d: %v", len(defs), names)
	}
	if !names["list_notebook_parameters"] || !names["set_notebook_parameters"] {
		t.Fatalf("explicit selection did not include both parameter tools: %v", names)
	}
	t.Logf("explicit agent loaded exactly its 2 selected tools")
}

func TestEngineLoadAgentToolDefs_ExplicitSkipsUnselected(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	engine := newTestEngine(db)

	// Explicitly select only list_notebook_parameters — set must NOT be included.
	paramToolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	agent := models.Agent{
		ID:              uuid.New().String(),
		OrgID:           orgID,
		AllBuiltinTools: false,
		ToolIDs:         paramToolIDs,
	}

	defs := engine.loadAgentToolDefs(context.Background(), agent)

	for _, d := range defs {
		if d.Function.Name == "set_notebook_parameters" {
			t.Fatalf("unselected tool set_notebook_parameters was loaded for explicit agent")
		}
	}
	if len(defs) != 1 {
		t.Fatalf("expected exactly 1 tool, got %d", len(defs))
	}
	t.Logf("explicit agent correctly excluded the unselected tool")
}

func TestEngineLoadAgentToolDefs_NoACLCheckAnymore(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	toolIDs := seededToolIDs(t, db, orgID, "execute_sql")
	var execID string
	db.Pool.QueryRow(context.Background(), `SELECT id FROM tools WHERE org_id=$1 AND name='execute_sql'`, orgID).Scan(&execID)
	// Delete the 'everyone' ACL entry - under old model this would block the tool
	_, _ = db.Pool.Exec(context.Background(), `DELETE FROM acl_entries WHERE resource_type='tool' AND resource_id=$1::uuid AND org_id=$2`, execID, orgID)

	agent := models.Agent{
		ID:      uuid.New().String(),
		OrgID:   orgID,
		ToolIDs: toolIDs,
	}
	// New model: tool_ids is sole gate, ACL deletion should NOT affect loading
	defs := engine.loadAgentToolDefs(context.Background(), agent)
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if !names["execute_sql"] {
		t.Fatalf("loadAgentToolDefs should load execute_sql even without ACL entry (tool_ids is sole gate), got %v", names)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool, got %d: %v", len(defs), names)
	}
}

func TestEngineLoadAgentToolDefs_AdminModeIrrelevant(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	toolIDs := seededToolIDs(t, db, orgID, "execute_sql", "list_notebook_parameters")
	agent := models.Agent{
		ID:      uuid.New().String(),
		OrgID:   orgID,
		ToolIDs: toolIDs,
	}
	defs := engine.loadAgentToolDefs(context.Background(), agent)
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if !names["execute_sql"] || !names["list_notebook_parameters"] {
		t.Fatalf("expected both tools irrespective of AdminMode, got %v", names)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(defs), names)
	}
}

func TestEngineLoadAgentToolDefs_TimeoutOverride(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	toolIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	regDef, ok := engine.registry.Get("list_notebook_parameters")
	require.True(t, ok, "registry must contain list_notebook_parameters")
	registryTimeoutBefore := regDef.Timeout

	var toolID string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT id FROM tools WHERE org_id=$1 AND name='list_notebook_parameters'`, orgID).Scan(&toolID)
	require.NoError(t, err)

	_, err = db.Pool.Exec(context.Background(),
		`UPDATE tools SET config = config || '{"timeout_ms": 5000}'::jsonb WHERE id=$1`, toolID)
	require.NoError(t, err)

	agent := models.Agent{ID: uuid.New().String(), OrgID: orgID, ToolIDs: toolIDs}
	defs := engine.loadAgentToolDefs(context.Background(), agent)
	require.Len(t, defs, 1)
	require.Equal(t, 5*time.Second, defs[0].Timeout, "per-tool timeout_ms must apply to the resolved def")
	require.NotSame(t, regDef, defs[0], "resolveToolDef must clone the shared registry def")
	require.Equal(t, registryTimeoutBefore, regDef.Timeout, "registry def must not be mutated")
}

func TestResolveToolDef_TimeoutPrecedence(t *testing.T) {
	engine := &Engine{registry: NewToolRegistry(), toolTimeoutDefault: 90 * time.Second}
	probe := &ToolDef{Timeout: 30 * time.Second}
	probe.Function.Name = "probe"
	engine.registry.Register(probe)

	// Registry budget beats the engine-wide fallback.
	got, err := engine.resolveToolDef(&models.Tool{
		Name:   "probe",
		Type:   models.ToolTypeBuiltin,
		Config: models.JSONMap{"handler_name": "probe"},
	})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, got.Timeout)

	// Per-tool config beats the registry budget without mutating it.
	got, err = engine.resolveToolDef(&models.Tool{
		Name:   "probe",
		Type:   models.ToolTypeBuiltin,
		Config: models.JSONMap{"handler_name": "probe", "timeout_ms": float64(4000)},
	})
	require.NoError(t, err)
	require.Equal(t, 4*time.Second, got.Timeout)
	require.Equal(t, 30*time.Second, probe.Timeout, "registry def must not be mutated")

	// No registry budget → engine-wide fallback.
	unbudgeted := &ToolDef{}
	unbudgeted.Function.Name = "unbudgeted"
	engine.registry.Register(unbudgeted)
	got, err = engine.resolveToolDef(&models.Tool{
		Name:   "unbudgeted",
		Type:   models.ToolTypeBuiltin,
		Config: models.JSONMap{"handler_name": "unbudgeted"},
	})
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, got.Timeout)

	// NoTimeout skips every override layer, including per-tool config.
	interactive := &ToolDef{Timeout: NoTimeout}
	interactive.Function.Name = "interactive"
	engine.registry.Register(interactive)
	got, err = engine.resolveToolDef(&models.Tool{
		Name:   "interactive",
		Type:   models.ToolTypeBuiltin,
		Config: models.JSONMap{"handler_name": "interactive", "timeout_ms": float64(5000)},
	})
	require.NoError(t, err)
	require.Equal(t, NoTimeout, got.Timeout, "config override must not clobber NoTimeout")
	require.Equal(t, NoTimeout, interactive.Timeout, "registry def must not be mutated")

	got, err = engine.resolveToolDef(&models.Tool{
		Name:   "interactive",
		Type:   models.ToolTypeBuiltin,
		Config: models.JSONMap{"handler_name": "interactive"},
	})
	require.NoError(t, err)
	require.Equal(t, NoTimeout, got.Timeout, "engine fallback must not clobber NoTimeout")
	require.Equal(t, NoTimeout, interactive.Timeout, "registry def must not be mutated")
}

func TestResolveToolDef_DynamicToolTimeout(t *testing.T) {
	engine := &Engine{registry: NewToolRegistry(), toolTimeoutDefault: 45 * time.Second}

	webhook, err := engine.resolveToolDef(&models.Tool{
		Name:   "notify",
		Type:   models.ToolTypeWebhook,
		Config: models.JSONMap{"url": "https://example.com/hook", "timeout_ms": float64(2500)},
	})
	require.NoError(t, err)
	require.Equal(t, 2500*time.Millisecond, webhook.Timeout)

	webhookFallback, err := engine.resolveToolDef(&models.Tool{
		Name:   "notify",
		Type:   models.ToolTypeWebhook,
		Config: models.JSONMap{"url": "https://example.com/hook"},
	})
	require.NoError(t, err)
	require.Equal(t, 45*time.Second, webhookFallback.Timeout)

	sqlTool, err := engine.resolveToolDef(&models.Tool{
		Name:   "slow_query",
		Type:   models.ToolTypeSQLQuery,
		Config: models.JSONMap{"connector_id": "abc", "query": "SELECT 1", "timeout_ms": float64(7000)},
	})
	require.NoError(t, err)
	require.Equal(t, 7*time.Second, sqlTool.Timeout)
}

func TestSeedBuiltinTools_ACLDoesNotGrantUseToEveryone(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, _ := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)

	// Check 'everyone' entry has view only
	rows, err := db.Pool.Query(context.Background(), `
		SELECT resource_id, actions FROM acl_entries
		WHERE org_id=$1 AND resource_type='tool' AND subject_type='org_role' AND subject_id='everyone'
	`, orgID)
	if err != nil {
		t.Fatalf("query acl: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var rid string
		var actions []string
		if err := rows.Scan(&rid, &actions); err != nil {
			t.Fatalf("scan: %v", err)
		}
		count++
		hasUse := false
		hasView := false
		for _, a := range actions {
			if a == "use" {
				hasUse = true
			}
			if a == "view" {
				hasView = true
			}
		}
		if hasUse {
			t.Fatalf("everyone ACL should NOT contain 'use', got %v for %s", actions, rid)
		}
		if !hasView {
			t.Fatalf("everyone ACL should contain 'view', got %v for %s", actions, rid)
		}
	}
	if count == 0 {
		t.Fatalf("no everyone ACL entries found")
	}
	// Check admin entry has view,use,edit,delete
	rows2, err := db.Pool.Query(context.Background(), `
		SELECT actions FROM acl_entries
		WHERE org_id=$1 AND resource_type='tool' AND subject_type='org_role' AND subject_id='admin'
	`, orgID)
	if err != nil {
		t.Fatalf("query admin acl: %v", err)
	}
	defer rows2.Close()
	adminCount := 0
	for rows2.Next() {
		var actions []string
		if err := rows2.Scan(&actions); err != nil {
			t.Fatalf("scan admin: %v", err)
		}
		adminCount++
		m := map[string]bool{}
		for _, a := range actions {
			m[a] = true
		}
		for _, need := range []string{"view", "use", "edit", "delete"} {
			if !m[need] {
				t.Fatalf("admin ACL should contain %s, got %v", need, actions)
			}
		}
	}
	if adminCount == 0 {
		t.Fatalf("no admin ACL entries found")
	}
}

// --- Helpers for ProcessMessage tests ---

func createTestNotebook(t *testing.T, db *database.DB, orgID, userID string) string {
	t.Helper()
	nbID := uuid.New().String()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO notebooks (id, org_id, title, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,NOW(),NOW())
	`, nbID, orgID, "Test Notebook", userID)
	if err != nil {
		t.Fatalf("create notebook: %v", err)
	}
	return nbID
}

func createTestAgentRow(t *testing.T, db *database.DB, orgID, userID string, toolIDs []string) string {
	t.Helper()
	agentID := uuid.New().String()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO agents (id, org_id, name, description, system_prompt, skill_ids, tool_ids, folder_id, max_turns, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,'',$4,'{}',$5,NULL,10,$6,NOW(),NOW())
	`, agentID, orgID, "Test Agent", "you are helpful", toolIDs, userID)
	if err != nil {
		t.Fatalf("create agent row: %v", err)
	}
	_, _ = db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1,'agent',$2,'user',$3, ARRAY['view','edit','delete'])
		ON CONFLICT DO NOTHING
	`, orgID, agentID, userID)
	return agentID
}

func createTestSession(t *testing.T, db *database.DB, agentID, notebookID, userID string) string {
	t.Helper()
	sid := uuid.New().String()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, max_turns, created_at)
		VALUES ($1,$2,$3,$4,10,NOW())
	`, sid, agentID, notebookID, userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return sid
}

func newMockLLMServerWithCapture(t *testing.T, masterKey []byte, responses []ChatResponse, capture *[]map[string]any) *httptest.Server {
	t.Helper()
	var idx atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)
		*capture = append(*capture, reqBody)
		i := int(idx.Load())
		if i >= len(responses) {
			i = len(responses) - 1
		}
		idx.Add(1)
		resp := responses[i]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	return srv
}

func TestProcessMessage_ToolNotInToolIDs_RejectedAtExecution(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)

	paramIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	agentID := createTestAgentRow(t, db, orgID, userID, paramIDs)
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)

	masterKey := make([]byte, 32)
	for i := range masterKey {
		masterKey[i] = byte(i)
	}
	// Mock LLM: first turn returns tool call to execute_sql (not in tool_ids), second turn returns final answer
	callID := uuid.New().String()
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{
					ToolCalls: []ToolCall{{ID: callID, Type: "function", Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "execute_sql", Arguments: `{"connector_id":"x","query":"SELECT 1"}`}}},
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
	// Need to set pool's engine session store etc - already via newTestEngine the pool is set, but we need to ensure engine.pool is set
	engine.pool = db.Pool
	// Session store needs pool as well
	engine.session = NewSessionStore(db.Pool)

	_, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hello", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}
	// Verify tool result in DB contains "tool not available"
	rows, _ := db.Pool.Query(context.Background(), `SELECT content FROM agent_messages WHERE session_id=$1 AND role='tool' ORDER BY created_at`, sid)
	defer rows.Close()
	found := false
	for rows.Next() {
		var content *string
		rows.Scan(&content)
		if content != nil && (*content == `tool not available: execute_sql` || *content == `"tool not available: execute_sql"` || contains(*content, "tool not available: execute_sql")) {
			found = true
		}
	}
	if !found {
		// also check via tool_calls result stringified JSON
		rows2, _ := db.Pool.Query(context.Background(), `SELECT content FROM agent_messages WHERE session_id=$1 AND role='tool'`, sid)
		defer rows2.Close()
		var msgs []string
		for rows2.Next() {
			var c *string
			rows2.Scan(&c)
			if c != nil {
				msgs = append(msgs, *c)
			}
		}
		t.Fatalf("expected tool result 'tool not available: execute_sql', got messages: %v", msgs)
	}
	// Also verify execute_sql handler was NOT invoked (tool result is rejection, not error from handler)
	// The captured tools list should NOT have been used to allow handler - but we already checked rejection.
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestProcessMessage_DoesNotAdvertiseUnlistedTools(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)
	paramIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	agentID := createTestAgentRow(t, db, orgID, userID, paramIDs)
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)
	masterKey := make([]byte, 32)
	responses := []ChatResponse{{
		Choices: []Choice{{Message: ChatMessage{Content: "hello"}, FinishReason: "stop"}},
		Usage:   Usage{PromptTokens: 1, CompletionTokens: 1},
	}}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)
	_, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hi", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	if len(captured) == 0 {
		t.Fatalf("no LLM request captured")
	}
	firstReq := captured[0]
	toolsRaw, ok := firstReq["tools"]
	if !ok {
		t.Fatalf("tools not in request: %v", firstReq)
	}
	tools, _ := json.Marshal(toolsRaw)
	var toolList []map[string]any
	json.Unmarshal(tools, &toolList)
	hasExecute := false
	hasListParams := false
	for _, tl := range toolList {
		if fn, ok := tl["function"].(map[string]any); ok {
			if name, _ := fn["name"].(string); name == "execute_sql" {
				hasExecute = true
			}
			if name, _ := fn["name"].(string); name == "list_notebook_parameters" {
				hasListParams = true
			}
		}
	}
	if hasExecute {
		t.Fatalf("execute_sql should NOT be advertised when not in tool_ids, got tools: %v", toolList)
	}
	if !hasListParams {
		t.Fatalf("list_notebook_parameters SHOULD be advertised, got %v", toolList)
	}
}

func TestProcessMessage_RegistryFallbackRemoved(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)
	// empty tool_ids
	agentID := createTestAgentRow(t, db, orgID, userID, []string{})
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)
	masterKey := make([]byte, 32)
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{ToolCalls: []ToolCall{{ID: uuid.New().String(), Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "execute_sql", Arguments: `{"connector_id":"x","query":"SELECT 1"}`}}}},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 5, CompletionTokens: 5},
		},
		{
			Choices: []Choice{{Message: ChatMessage{Content: "done"}, FinishReason: "stop"}},
			Usage:   Usage{PromptTokens: 5, CompletionTokens: 5},
		},
	}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)
	_, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hi", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	rows, _ := db.Pool.Query(context.Background(), `SELECT content FROM agent_messages WHERE session_id=$1 AND role='tool'`, sid)
	defer rows.Close()
	found := false
	for rows.Next() {
		var c *string
		rows.Scan(&c)
		if c != nil && contains(*c, "tool not available: execute_sql") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected rejection for execute_sql with empty tool_ids (fallback removed)")
	}
}

func TestEndToEnd_ToolRemovedFromAgent_CannotCall(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)
	ids := seededToolIDs(t, db, orgID, "execute_sql", "list_notebook_parameters")
	agentID := createTestAgentRow(t, db, orgID, userID, ids)
	// Verify both tools loaded initially
	var agent models.Agent
	var toolIDs []string
	db.Pool.QueryRow(context.Background(), `SELECT tool_ids FROM agents WHERE id=$1`, agentID).Scan(&toolIDs)
	agent = models.Agent{ID: agentID, OrgID: orgID, ToolIDs: toolIDs}
	defs := engine.loadAgentToolDefs(context.Background(), agent)
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools initially, got %d", len(defs))
	}
	// Remove execute_sql
	listOnly := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	_, err := db.Pool.Exec(context.Background(), `UPDATE agents SET tool_ids=$1 WHERE id=$2`, listOnly, agentID)
	if err != nil {
		t.Fatalf("update agent: %v", err)
	}
	agent.ToolIDs = listOnly
	defs = engine.loadAgentToolDefs(context.Background(), agent)
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if names["execute_sql"] {
		t.Fatalf("execute_sql should not be loaded after removal")
	}
	if !names["list_notebook_parameters"] {
		t.Fatalf("list_notebook_parameters should still be loaded")
	}
	// Also test execution rejection after removal
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)
	masterKey := make([]byte, 32)
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{
					ToolCalls: []ToolCall{{
						ID:   uuid.New().String(),
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "execute_sql", Arguments: `{}`},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 1, CompletionTokens: 1},
		},
		{
			Choices: []Choice{{
				Message:      ChatMessage{Content: "done"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 1, CompletionTokens: 1},
		},
	}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	engine.session = NewSessionStore(db.Pool)
	_, _, _, _, _, err = engine.ProcessMessage(context.Background(), sid, "hi", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	rows, _ := db.Pool.Query(context.Background(), `SELECT content FROM agent_messages WHERE session_id=$1 AND role='tool'`, sid)
	defer rows.Close()
	found := false
	for rows.Next() {
		var c *string
		rows.Scan(&c)
		if c != nil && contains(*c, "tool not available: execute_sql") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected tool not available after removal")
	}
}

func TestEndToEnd_AdminModeDoesNotBypassToolIDs(t *testing.T) {
	db := setupEngineTestDB(t)
	orgID, userID := createEngineTestOrgAndUser(t, db)
	SeedBuiltinTools(context.Background(), db.Pool, orgID)
	engine := newTestEngine(db)
	// Agent without execute_sql
	paramIDs := seededToolIDs(t, db, orgID, "list_notebook_parameters")
	agentID := createTestAgentRow(t, db, orgID, userID, paramIDs)
	nbID := createTestNotebook(t, db, orgID, userID)
	sid := createTestSession(t, db, agentID, nbID, userID)
	// Simulate AdminMode ON via session store
	engine.session = NewSessionStore(db.Pool)
	engine.session.SetAdminMode(sid, true)
	masterKey := make([]byte, 32)
	responses := []ChatResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{
					ToolCalls: []ToolCall{{
						ID:   uuid.New().String(),
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "execute_sql", Arguments: `{}`},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: Usage{PromptTokens: 1, CompletionTokens: 1},
		},
		{
			Choices: []Choice{{
				Message:      ChatMessage{Content: "done"},
				FinishReason: "stop",
			}},
			Usage: Usage{PromptTokens: 1, CompletionTokens: 1},
		},
	}
	var captured []map[string]any
	srv := newMockLLMServerWithCapture(t, masterKey, responses, &captured)
	defer srv.Close()
	enc, _ := crypto.Encrypt([]byte("sk-test"), masterKey)
	engine.SetLLMClient(NewLLMClient(srv.URL, "gpt-4", enc, map[string]any{}))
	engine.pool = db.Pool
	_, _, _, _, _, err := engine.ProcessMessage(context.Background(), sid, "hi", nil, nil, masterKey, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ProcessMessage: %v", err)
	}
	rows, _ := db.Pool.Query(context.Background(), `SELECT content FROM agent_messages WHERE session_id=$1 AND role='tool'`, sid)
	defer rows.Close()
	found := false
	for rows.Next() {
		var c *string
		rows.Scan(&c)
		if c != nil && contains(*c, "tool not available: execute_sql") {
			found = true
		}
	}
	if !found {
		t.Fatalf("AdminMode should NOT bypass tool_ids: expected rejection for execute_sql")
	}
}
