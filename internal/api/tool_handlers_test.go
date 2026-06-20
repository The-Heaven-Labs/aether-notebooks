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

func TestToolCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("tool-crud-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Tool CRUD Org")

	t.Run("list empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/tools", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list tools: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var tools []map[string]any
		json.NewDecoder(rec.Body).Decode(&tools)
		if len(tools) != 0 {
			t.Fatalf("expected 0 tools, got %d", len(tools))
		}
	})

	var toolID string
	t.Run("create", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":        "Test Tool",
			"description": "A test webhook tool",
			"type":        "webhook",
			"config": map[string]any{
				"url":    "https://example.com/webhook",
				"method": "POST",
			},
		})
		req := httptest.NewRequest("POST", "/api/v1/tools", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create tool: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		toolID = resp["id"].(string)
		if toolID == "" {
			t.Fatal("expected non-empty tool id")
		}
	})

	t.Run("list after create", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/tools", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list tools: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var tools []map[string]any
		json.NewDecoder(rec.Body).Decode(&tools)
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}
		if tools[0]["name"] != "Test Tool" {
			t.Fatalf("expected name 'Test Tool', got %v", tools[0]["name"])
		}
		if tools[0]["type"] != "webhook" {
			t.Fatalf("expected type 'webhook', got %v", tools[0]["type"])
		}
	})

	t.Run("get single", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/tools/"+toolID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get tool: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var tool map[string]any
		json.NewDecoder(rec.Body).Decode(&tool)
		if tool["name"] != "Test Tool" {
			t.Fatalf("expected name 'Test Tool', got %v", tool["name"])
		}
		if tool["type"] != "webhook" {
			t.Fatalf("expected type 'webhook', got %v", tool["type"])
		}
	})

	t.Run("update", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":        "Updated Tool",
			"description": "Updated description",
		})
		req := httptest.NewRequest("PUT", "/api/v1/tools/"+toolID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update tool: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest("GET", "/api/v1/tools/"+toolID, nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)
		var tool map[string]any
		json.NewDecoder(rec2.Body).Decode(&tool)
		if tool["name"] != "Updated Tool" {
			t.Fatalf("expected name 'Updated Tool', got %v", tool["name"])
		}
	})

	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/tools/"+toolID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete tool: expected 204, got %d: %s", rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest("GET", "/api/v1/tools/"+toolID, nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusNotFound {
			t.Fatalf("get after delete: expected 404, got %d: %s", rec2.Code, rec2.Body.String())
		}
	})
}
