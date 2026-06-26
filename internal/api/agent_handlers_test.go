package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
)

func TestAgentCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("agent-crud-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Agent CRUD Org")
	mcID := createModelConfig(t, srv, token)

	t.Run("list empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list agents: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var agents []interface{}
		if err := json.NewDecoder(rec.Body).Decode(&agents); err != nil {
			t.Fatalf("decode agents: %v", err)
		}
		if len(agents) != 0 {
			t.Fatalf("expected 0 agents, got %d", len(agents))
		}
	})

	var agentID string
	t.Run("create", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":            "Test Agent",
			"description":     "A test agent",
			"system_prompt":   "You are helpful",
			"model_config_id": mcID,
			"skill_ids":       []string{},
			"mcp_server_ids":  []string{},
		})
		req := httptest.NewRequest("POST", "/api/v1/agents", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create agent: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		id, ok := resp["id"].(string)
		if !ok {
			t.Fatalf("create agent: no id in response: %v", resp)
		}
		agentID = id
	})

	t.Run("list after create", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list agents: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var agents []map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&agents); err != nil {
			t.Fatalf("decode agents: %v", err)
		}
		if len(agents) != 1 {
			t.Fatalf("expected 1 agent, got %d", len(agents))
		}
		a := agents[0]
		if a["name"] != "Test Agent" {
			t.Fatalf("expected name 'Test Agent', got %v", a["name"])
		}
		if a["description"] != "A test agent" {
			t.Fatalf("expected description 'A test agent', got %v", a["description"])
		}
		if a["system_prompt"] != "You are helpful" {
			t.Fatalf("expected system_prompt 'You are helpful', got %v", a["system_prompt"])
		}
		sids, ok := a["skill_ids"].([]interface{})
		if !ok || len(sids) != 0 {
			t.Fatalf("expected empty skill_ids, got %v (type %T)", a["skill_ids"], a["skill_ids"])
		}
	})

	t.Run("get single", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/agents/"+agentID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get agent: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var a map[string]any
		json.NewDecoder(rec.Body).Decode(&a)
		if a["name"] != "Test Agent" {
			t.Fatalf("expected name 'Test Agent', got %v", a["name"])
		}
		if a["id"] != agentID {
			t.Fatalf("expected id %s, got %v", agentID, a["id"])
		}
	})

	t.Run("update", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":          "Updated Agent",
			"description":   "Updated desc",
			"system_prompt": "New prompt",
		})
		req := httptest.NewRequest("PUT", "/api/v1/agents/"+agentID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update agent: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest("GET", "/api/v1/agents/"+agentID, nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)
		var a map[string]any
		json.NewDecoder(rec2.Body).Decode(&a)
		if a["name"] != "Updated Agent" {
			t.Fatalf("expected name 'Updated Agent', got %v", a["name"])
		}
		if a["description"] != "Updated desc" {
			t.Fatalf("expected description 'Updated desc', got %v", a["description"])
		}
		if a["system_prompt"] != "New prompt" {
			t.Fatalf("expected system_prompt 'New prompt', got %v", a["system_prompt"])
		}
	})

	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/agents/"+agentID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete agent: expected 204, got %d: %s", rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest("GET", "/api/v1/agents", nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)
		var agents []map[string]any
		json.NewDecoder(rec2.Body).Decode(&agents)
		if len(agents) != 0 {
			t.Fatalf("expected 0 agents after delete, got %d", len(agents))
		}
	})
}

func TestAgentCreateWithSkills(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("agent-skills-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Agent Skills Org")
	mcID := createModelConfig(t, srv, token)

	skillID := createSkill(t, srv, token)

	body, _ := json.Marshal(map[string]any{
		"name":            "Agent With Skill",
		"model_config_id": mcID,
		"skill_ids":       []string{skillID},
		"mcp_server_ids":  []string{},
	})
	req := httptest.NewRequest("POST", "/api/v1/agents", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create agent with skill: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var createResp map[string]any
	json.NewDecoder(rec.Body).Decode(&createResp)
	agentID := createResp["id"].(string)

	req2 := httptest.NewRequest("GET", "/api/v1/agents/"+agentID, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("get agent: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var a map[string]any
	json.NewDecoder(rec2.Body).Decode(&a)
	sids, ok := a["skill_ids"].([]interface{})
	if !ok {
		t.Fatalf("expected skill_ids to be array, got %T: %v", a["skill_ids"], a["skill_ids"])
	}
	if len(sids) != 1 || sids[0] != skillID {
		t.Fatalf("expected skill_ids to contain %s, got %v", skillID, sids)
	}
}

func TestAgentCreateWithMCPServers(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("agent-mcp-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Agent MCP Org")
	mcID := createModelConfig(t, srv, token)

	mcpID := createMCPServer(t, srv, token)

	body, _ := json.Marshal(map[string]any{
		"name":            "Agent With MCP",
		"model_config_id": mcID,
		"skill_ids":       []string{},
		"mcp_server_ids":  []string{mcpID},
	})
	req := httptest.NewRequest("POST", "/api/v1/agents", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create agent with MCP: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var createResp map[string]any
	json.NewDecoder(rec.Body).Decode(&createResp)
	agentID := createResp["id"].(string)

	req2 := httptest.NewRequest("GET", "/api/v1/agents/"+agentID, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("get agent: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var a map[string]any
	json.NewDecoder(rec2.Body).Decode(&a)
	mcpIDs, ok := a["mcp_server_ids"].([]interface{})
	if !ok {
		t.Fatalf("expected mcp_server_ids to be array, got %T: %v", a["mcp_server_ids"], a["mcp_server_ids"])
	}
	if len(mcpIDs) != 1 || mcpIDs[0] != mcpID {
		t.Fatalf("expected mcp_server_ids to contain %s, got %v", mcpID, mcpIDs)
	}
	mcpServers, ok := a["mcp_servers"].([]interface{})
	if !ok || len(mcpServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %v", a["mcp_servers"])
	}
}

func TestAgentListIncludesMCPServers(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("agent-list-mcp-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Agent List MCP Org")
	mcID := createModelConfig(t, srv, token)

	mcpID := createMCPServer(t, srv, token)

	body, _ := json.Marshal(map[string]any{
		"name":            "Agent MCP List",
		"model_config_id": mcID,
		"skill_ids":       []string{},
		"mcp_server_ids":  []string{mcpID},
	})
	req := httptest.NewRequest("POST", "/api/v1/agents", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest("GET", "/api/v1/agents", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list agents: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var agents []map[string]any
	json.NewDecoder(rec2.Body).Decode(&agents)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	a := agents[0]
	mcpServers, ok := a["mcp_servers"].([]interface{})
	if !ok || len(mcpServers) != 1 {
		t.Fatalf("expected 1 MCP server in list, got %v", a["mcp_servers"])
	}
	mcpIDs, ok := a["mcp_server_ids"].([]interface{})
	if !ok || len(mcpIDs) != 1 {
		t.Fatalf("expected 1 MCP server ID in list, got %v", a["mcp_server_ids"])
	}
}

func TestAgentSessionCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("agent-session-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Agent Session Org")
	mcID := createModelConfig(t, srv, token)
	agentID := createAgent(t, srv, token, mcID)
	nbID := createNotebook(t, srv, token, "Session NB")

	sessionID := createAgentSession(t, srv, token, agentID, nbID)
	if sessionID == "" {
		t.Fatal("expected non-empty session ID")
	}

	t.Run("get session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/sessions/"+sessionID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get session: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var s map[string]any
		json.NewDecoder(rec.Body).Decode(&s)
		if s["id"] != sessionID {
			t.Fatalf("expected session id %s, got %v", sessionID, s["id"])
		}
		if s["agent_id"] != agentID {
			t.Fatalf("expected agent_id %s, got %v", agentID, s["agent_id"])
		}
	})

	t.Run("list sessions", func(t *testing.T) {
		// Insert a message so the session isn't empty (empty sessions are filtered out)
		_, err := srv.DB().Pool.Exec(context.Background(), `
			INSERT INTO agent_messages (session_id, role, content, created_at)
			VALUES ($1, 'user', 'hello', NOW())
		`, sessionID)
		if err != nil {
			t.Fatalf("insert message: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/v1/agents/"+agentID+"/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list sessions: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var sessions []map[string]any
		json.NewDecoder(rec.Body).Decode(&sessions)
		if len(sessions) != 1 {
			t.Fatalf("expected 1 session, got %d", len(sessions))
		}
	})
}

func createSkill(t *testing.T, srv *api.Server, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":        "Test Skill",
		"description": "A test skill",
	})
	req := httptest.NewRequest("POST", "/api/v1/skills", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createSkill failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}

func createMCPServer(t *testing.T, srv *api.Server, token string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":    fmt.Sprintf("Test MCP %d", time.Now().UnixNano()),
		"type":    "stdio",
		"command": "/usr/bin/test",
		"args":    []string{},
	})
	req := httptest.NewRequest("POST", "/api/v1/mcp-servers", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createMCPServer failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}
