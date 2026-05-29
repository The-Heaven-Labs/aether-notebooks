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

func TestSkillCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("skill-crud-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Skill CRUD Org")

	t.Run("list empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/skills", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list skills: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var skills []map[string]any
		json.NewDecoder(rec.Body).Decode(&skills)
		if len(skills) != 0 {
			t.Fatalf("expected 0 skills, got %d", len(skills))
		}
	})

	var skillID string
	t.Run("create", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":        "Test Skill",
			"description": "A test skill",
			"tool_ids":    []string{},
		})
		req := httptest.NewRequest("POST", "/api/v1/skills", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create skill: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(rec.Body).Decode(&resp)
		skillID = resp["id"].(string)
	})

	t.Run("list after create", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/skills", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list skills: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var skills []map[string]any
		json.NewDecoder(rec.Body).Decode(&skills)
		if len(skills) != 1 {
			t.Fatalf("expected 1 skill, got %d", len(skills))
		}
		if skills[0]["name"] != "Test Skill" {
			t.Fatalf("expected name 'Test Skill', got %v", skills[0]["name"])
		}
		toolIDs, ok := skills[0]["tool_ids"].([]interface{})
		if !ok {
			t.Fatalf("expected tool_ids to be array, got %T", skills[0]["tool_ids"])
		}
		if len(toolIDs) != 0 {
			t.Fatalf("expected empty tool_ids, got %v", toolIDs)
		}
	})

	t.Run("update", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":     "Updated Skill",
			"tool_ids": []string{"tool1", "tool2"},
		})
		req := httptest.NewRequest("PUT", "/api/v1/skills/"+skillID, strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update skill: expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest("GET", "/api/v1/skills", nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		rec2 := httptest.NewRecorder()
		srv.ServeHTTP(rec2, req2)
		var skills []map[string]any
		json.NewDecoder(rec2.Body).Decode(&skills)
		toolIDs, ok := skills[0]["tool_ids"].([]interface{})
		if !ok || len(toolIDs) != 2 {
			t.Fatalf("expected 2 tool_ids, got %v", skills[0]["tool_ids"])
		}
	})

	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/skills/"+skillID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("delete skill: expected 204, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}