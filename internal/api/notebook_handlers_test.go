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
	if rec.Code != http.StatusForbidden {
		t.Fatalf("not-found: expected 403, got %d", rec.Code)
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

func TestNotebookConnectorID(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("nb-conn-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "ConnNB Org")
	connID := createConnector(t, srv, token)
	nbID := createNotebook(t, srv, token, "ConnTest NB")

	// Update notebook with connector
	body := fmt.Sprintf(`{"title":"Test","connector_id":"%s"}`, connID)
	req := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT notebook: got %d, body: %s", w.Code, w.Body.String())
	}

	// GET notebook — connector_id should be returned
	req2 := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET notebook: got %d, body: %s", w2.Code, w2.Body.String())
	}
	var nb map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&nb)
	if nb["connector_id"] != connID {
		t.Fatalf("expected connector_id %q, got %v", connID, nb["connector_id"])
	}

	// LIST notebooks — connector_id should be present
	req3 := httptest.NewRequest("GET", "/api/v1/notebooks", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	w3 := httptest.NewRecorder()
	srv.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("LIST notebooks: got %d, body: %s", w3.Code, w3.Body.String())
	}
	var notebooks []map[string]interface{}
	json.NewDecoder(w3.Body).Decode(&notebooks)
	if len(notebooks) == 0 {
		t.Fatal("expected at least one notebook in list")
	}
	found := false
	for _, n := range notebooks {
		if n["id"] == nbID {
			found = true
			if n["connector_id"] != connID {
				t.Fatalf("LIST: expected connector_id %q, got %v", connID, n["connector_id"])
			}
		}
	}
	if !found {
		t.Fatalf("notebook %q not found in list", nbID)
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

func TestNotebook_FolderID(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("nb-folder-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "FolderNB Org")

	// Create a folder
	folderBody, _ := json.Marshal(map[string]string{"name": "My Folder"})
	req := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(folderBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder: %d %s", rec.Code, rec.Body.String())
	}
	var folder map[string]any
	json.NewDecoder(rec.Body).Decode(&folder)
	folderID := folder["id"].(string)

	// Create notebook inside the folder
	body, _ := json.Marshal(map[string]any{"title": "Folder NB", "folder_id": folderID})
	req2 := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create nb with folder: %d %s", rec2.Code, rec2.Body.String())
	}
	var nb map[string]any
	json.NewDecoder(rec2.Body).Decode(&nb)
	if nb["folder_id"] != folderID {
		t.Errorf("expected folder_id=%s in response, got %v", folderID, nb["folder_id"])
	}
	nbID := nb["id"].(string)

	// Move notebook to root (explicit null via update)
	moveBody, _ := json.Marshal(map[string]any{"folder_id": nil})
	req3 := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID, bytes.NewReader(moveBody))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("move to root: %d %s", rec3.Code, rec3.Body.String())
	}
	var updated map[string]any
	json.NewDecoder(rec3.Body).Decode(&updated)
	if updated["folder_id"] != nil {
		t.Errorf("expected folder_id=nil after move to root, got %v", updated["folder_id"])
	}
}
