package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetRecent(t *testing.T) {
	srv := setupTestServer(t)

	email := fmt.Sprintf("recent-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Recent Org")

	// Create a notebook so there's something to appear in recent
	_ = createNotebook(t, srv, token, "Recent NB")

	req := httptest.NewRequest("GET", "/api/v1/recent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var items []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one recent item")
	}

	first := items[0]
	if _, ok := first["type"]; !ok {
		t.Error("missing type field")
	}
	if _, ok := first["id"]; !ok {
		t.Error("missing id field")
	}
	if _, ok := first["name"]; !ok {
		t.Error("missing name field")
	}
	if _, ok := first["updated_at"]; !ok {
		t.Error("missing updated_at field")
	}
}
