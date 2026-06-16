package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPServerCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-crud-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "MCP CRUD Org")

	t.Run("list empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/mcp-servers", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list mcp servers: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var servers []map[string]any
		json.NewDecoder(rec.Body).Decode(&servers)
		if len(servers) != 0 {
			t.Fatalf("expected 0 servers, got %d", len(servers))
		}
	})

	var mcpID string
	t.Run("create", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":    "Test MCP",
			"type":    "stdio",
			"command": "/usr/bin/test",
			"args":    []string{"--flag", "value"},
		})
		req := httptest.NewRequest("POST", "/api/v1/mcp-servers", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create mcp server: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		mcpID = resp["id"].(string)
	})

	t.Run("get", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/mcp-servers/"+mcpID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get mcp server: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var s map[string]any
		json.NewDecoder(rec.Body).Decode(&s)
		if s["name"] != "Test MCP" {
			t.Fatalf("expected name 'Test MCP', got %v", s["name"])
		}
		args, ok := s["args"].([]interface{})
		if !ok || len(args) != 2 {
			t.Fatalf("expected 2 args, got %v (type %T)", s["args"], s["args"])
		}
	})

	t.Run("list after create", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/mcp-servers", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list mcp servers: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var servers []map[string]any
		json.NewDecoder(rec.Body).Decode(&servers)
		if len(servers) != 1 {
			t.Fatalf("expected 1 server, got %d", len(servers))
		}
		if servers[0]["name"] != "Test MCP" {
			t.Fatalf("expected name 'Test MCP', got %v", servers[0]["name"])
		}
	})

	t.Run("update", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":    "Updated MCP",
			"command": "/usr/bin/updated",
			"args":    []string{"--new-flag"},
		})
		req := httptest.NewRequest("PUT", "/api/v1/mcp-servers/"+mcpID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update mcp server: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest("GET", "/api/v1/mcp-servers/"+mcpID, nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)
		var s map[string]any
		json.NewDecoder(rec2.Body).Decode(&s)
		if s["name"] != "Updated MCP" {
			t.Fatalf("expected name 'Updated MCP', got %v", s["name"])
		}
		args, ok := s["args"].([]interface{})
		if !ok || len(args) != 1 {
			t.Fatalf("expected 1 arg after update, got %v", s["args"])
		}
	})

	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/mcp-servers/"+mcpID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete mcp server: expected 204, got %d: %s", rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest("GET", "/api/v1/mcp-servers", nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)
		var servers []map[string]any
		json.NewDecoder(rec2.Body).Decode(&servers)
		if len(servers) != 0 {
			t.Fatalf("expected 0 servers after delete, got %d", len(servers))
		}
	})
}
