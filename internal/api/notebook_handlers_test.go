package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotebookCRUD(t *testing.T) {
	srv := setupTestServer(t)

	email := fmt.Sprintf("nb-test-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "NB Org")

	// Create notebook
	nbBody, _ := json.Marshal(map[string]interface{}{
		"title":      "Test Notebook",
		"parameters": []map[string]string{{"name": "env", "type": "string", "default": "prod"}},
	})
	req := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(nbBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var nbResp map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&nbResp)
	nbID := nbResp["id"].(string)

	// List notebooks
	req = httptest.NewRequest("GET", "/api/v1/notebooks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec.Code)
	}

	// Get notebook
	req = httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete notebook
	req = httptest.NewRequest("DELETE", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec.Code)
	}
}
