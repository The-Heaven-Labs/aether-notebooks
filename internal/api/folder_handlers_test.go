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

func TestFolderCRUD(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("folder-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Folder Org")

	// Create folder at root
	body, _ := json.Marshal(map[string]string{"name": "Engineering"})
	req := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var folder map[string]any
	json.NewDecoder(rec.Body).Decode(&folder)
	folderID := folder["id"].(string)

	// Create sub-folder
	body2, _ := json.Marshal(map[string]any{"name": "Backend", "parent_id": folderID})
	req2 := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create sub-folder: expected 201, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var subFolder map[string]any
	json.NewDecoder(rec2.Body).Decode(&subFolder)
	subFolderID := subFolder["id"].(string)

	// Get root contents
	req3 := httptest.NewRequest("GET", "/api/v1/folders", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("list root: expected 200, got %d: %s", rec3.Code, rec3.Body.String())
	}

	// Get folder contents (has sub-folder)
	req4 := httptest.NewRequest("GET", "/api/v1/folders/"+folderID, nil)
	req4.Header.Set("Authorization", "Bearer "+token)
	rec4 := httptest.NewRecorder()
	srv.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("get folder: expected 200, got %d: %s", rec4.Code, rec4.Body.String())
	}
	var contents map[string]any
	json.NewDecoder(rec4.Body).Decode(&contents)
	folders := contents["folders"].([]any)
	if len(folders) != 1 {
		t.Errorf("expected 1 sub-folder, got %d", len(folders))
	}

	// Get ancestors breadcrumb
	req5 := httptest.NewRequest("GET", "/api/v1/folders/"+subFolderID+"/ancestors", nil)
	req5.Header.Set("Authorization", "Bearer "+token)
	rec5 := httptest.NewRecorder()
	srv.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("ancestors: expected 200, got %d: %s", rec5.Code, rec5.Body.String())
	}
	var ancestors []any
	json.NewDecoder(rec5.Body).Decode(&ancestors)
	if len(ancestors) != 2 { // Engineering + Backend
		t.Errorf("expected 2 ancestors, got %d", len(ancestors))
	}

	// Rename folder
	renameBody, _ := json.Marshal(map[string]string{"name": "Engineering Team"})
	req6 := httptest.NewRequest("PUT", "/api/v1/folders/"+folderID, bytes.NewReader(renameBody))
	req6.Header.Set("Content-Type", "application/json")
	req6.Header.Set("Authorization", "Bearer "+token)
	rec6 := httptest.NewRecorder()
	srv.ServeHTTP(rec6, req6)
	if rec6.Code != http.StatusOK {
		t.Fatalf("rename: expected 200, got %d: %s", rec6.Code, rec6.Body.String())
	}

	// Delete sub-folder (empty)
	req7 := httptest.NewRequest("DELETE", "/api/v1/folders/"+subFolderID, nil)
	req7.Header.Set("Authorization", "Bearer "+token)
	rec7 := httptest.NewRecorder()
	srv.ServeHTTP(rec7, req7)
	if rec7.Code != http.StatusNoContent {
		t.Fatalf("delete empty: expected 204, got %d: %s", rec7.Code, rec7.Body.String())
	}

	// Create a child so parent is non-empty
	body8, _ := json.Marshal(map[string]any{"name": "Child", "parent_id": folderID})
	req8 := httptest.NewRequest("POST", "/api/v1/folders", bytes.NewReader(body8))
	req8.Header.Set("Content-Type", "application/json")
	req8.Header.Set("Authorization", "Bearer "+token)
	rec8 := httptest.NewRecorder()
	srv.ServeHTTP(rec8, req8)
	if rec8.Code != http.StatusCreated {
		t.Fatalf("re-create child: expected 201, got %d: %s", rec8.Code, rec8.Body.String())
	}

	// Delete non-empty folder without force → 409
	req9 := httptest.NewRequest("DELETE", "/api/v1/folders/"+folderID, nil)
	req9.Header.Set("Authorization", "Bearer "+token)
	rec9 := httptest.NewRecorder()
	srv.ServeHTTP(rec9, req9)
	if rec9.Code != http.StatusConflict {
		t.Fatalf("delete non-empty: expected 409, got %d: %s", rec9.Code, rec9.Body.String())
	}

	// Force delete
	req10 := httptest.NewRequest("DELETE", "/api/v1/folders/"+folderID+"?force=true", nil)
	req10.Header.Set("Authorization", "Bearer "+token)
	rec10 := httptest.NewRecorder()
	srv.ServeHTTP(rec10, req10)
	if rec10.Code != http.StatusNoContent {
		t.Fatalf("force delete: expected 204, got %d: %s", rec10.Code, rec10.Body.String())
	}
}
