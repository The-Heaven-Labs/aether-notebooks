package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEveryoneGroupExists(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("everyone-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Everyone Org")

	req := httptest.NewRequest("GET", "/api/v1/groups", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var groups []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&groups); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	found := false
	for _, g := range groups {
		if g["name"] == "Everyone" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'Everyone' group to exist after org creation")
	}
}
