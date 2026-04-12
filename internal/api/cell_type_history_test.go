package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCellTypeChangeLogsVersion(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("cell-type-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "CellTypeOrg")

	nbID := createNotebook(t, srv, token, "History NB")
	cellID := createCell(t, srv, token, nbID, "sql", "SELECT 1", "")

	// Change type to text
	body, _ := json.Marshal(map[string]any{"type": "text"})
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/notebooks/%s/cells/%s", nbID, cellID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Check version history contains a type-change note
	req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/notebooks/%s/cells/%s/versions", nbID, cellID), nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on versions, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var versions []map[string]any
	if err := json.NewDecoder(rec2.Body).Decode(&versions); err != nil {
		t.Fatalf("decode versions: %v", err)
	}

	found := false
	for _, v := range versions {
		if src, _ := v["source"].(string); strings.Contains(src, "type changed") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a version entry for type change, got %d versions: %v", len(versions), versions)
	}
}
