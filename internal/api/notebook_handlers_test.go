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

func TestNotebookUpdate(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("nb-update-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Update Org")

	// Create a notebook to update
	nbBody, _ := json.Marshal(map[string]any{"title": "Original Title"})
	req := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(nbBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", rec.Code)
	}
	var nb map[string]any
	json.NewDecoder(rec.Body).Decode(&nb)
	nbID := nb["id"].(string)

	// Title-only update
	body, _ := json.Marshal(map[string]any{"title": "New Title"})
	req = httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update title: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated["title"] != "New Title" {
		t.Errorf("title not updated: got %v", updated["title"])
	}

	// Empty body → 400
	req = httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID, bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty update: expected 400, got %d", rec.Code)
	}

	// Not-found → 404
	body, _ = json.Marshal(map[string]any{"title": "X"})
	req = httptest.NewRequest("PUT", "/api/v1/notebooks/00000000-0000-0000-0000-000000000000", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not-found: expected 404, got %d", rec.Code)
	}
}

func TestNotebookDescription(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("desc-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "DescOrg")

	// Create notebook with description
	body, _ := json.Marshal(map[string]string{"title": "My NB", "description": "A test notebook"})
	req := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create notebook: %d %s", rec.Code, rec.Body.String())
	}

	var nb map[string]any
	json.NewDecoder(rec.Body).Decode(&nb)
	if nb["description"] != "A test notebook" {
		t.Fatalf("expected description 'A test notebook', got %v", nb["description"])
	}
}

func TestNotebookCRUD(t *testing.T) {
	srv := setupTestServer(t)

	email := fmt.Sprintf("nb-test-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "NB Org")

	// Create notebook
	nbBody, _ := json.Marshal(map[string]any{
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

	var nbResp map[string]any
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
