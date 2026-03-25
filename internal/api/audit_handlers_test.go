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

func TestAuditLogs(t *testing.T) {
	srv := setupTestServer(t)

	ts := time.Now().UnixNano()

	// 1. Register admin user and get token
	adminEmail := fmt.Sprintf("audit-admin-%d@example.com", ts)
	adminToken := registerAndGetToken(t, srv, adminEmail, "Audit Test Org")

	// 2. Do a mutating action: create a notebook
	createNotebook(t, srv, adminToken, "Audit Test Notebook")

	// 3. GET /api/v1/audit → 200, at least one entry
	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list audit: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var entries []map[string]any
	json.NewDecoder(rec.Body).Decode(&entries)
	if len(entries) < 1 {
		t.Fatalf("list audit: expected at least 1 entry, got %d", len(entries))
	}

	// 4. GET /api/v1/audit?action=notebook.create → filters correctly
	req = httptest.NewRequest("GET", "/api/v1/audit?action=notebook.create", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list audit filtered: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var filtered []map[string]any
	json.NewDecoder(rec.Body).Decode(&filtered)
	if len(filtered) < 1 {
		t.Fatalf("list audit filtered: expected at least 1 entry, got %d", len(filtered))
	}
	for _, e := range filtered {
		if e["action"] != "notebook.create" {
			t.Fatalf("list audit filtered: expected action=notebook.create, got %s", e["action"])
		}
	}

	// Extract admin's org_id from the audit response for role-testing below.
	adminOrgID, _ := entries[0]["org_id"].(string)
	if adminOrgID == "" {
		t.Fatal("could not determine admin org_id from audit entries")
	}

	// 5. Non-admin (viewer) gets 403.
	// Register a viewer user, invite them into admin's org as "viewer",
	// then login with admin's org_id to get a viewer-scoped token.
	viewerEmail := fmt.Sprintf("audit-viewer-%d@example.com", ts)
	regBody, _ := json.Marshal(map[string]string{
		"email": viewerEmail, "password": "pass123", "name": "Viewer",
		"org_name": fmt.Sprintf("Viewer Own Org %d", ts),
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register viewer: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Invite viewer into admin's org as "viewer"
	inviteBody, _ := json.Marshal(map[string]string{"email": viewerEmail, "role": "viewer"})
	req = httptest.NewRequest("POST", "/api/v1/members", bytes.NewReader(inviteBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("invite viewer: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Login as viewer scoped to admin's org to get a viewer-role token
	loginBody, _ := json.Marshal(map[string]string{
		"email": viewerEmail, "password": "pass123", "org_id": adminOrgID,
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login viewer: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var loginResp map[string]any
	json.NewDecoder(rec.Body).Decode(&loginResp)
	viewerToken, _ := loginResp["token"].(string)
	if viewerToken == "" {
		t.Fatal("login viewer: expected token in response")
	}

	// Viewer should get 403 on GET /api/v1/audit
	req = httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer audit: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// Also test editor (non-admin) gets 403
	editorEmail := fmt.Sprintf("audit-editor-%d@example.com", ts)
	regBody, _ = json.Marshal(map[string]string{
		"email": editorEmail, "password": "pass123", "name": "Editor",
		"org_name": fmt.Sprintf("Editor Own Org %d", ts),
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register editor: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	inviteBody, _ = json.Marshal(map[string]string{"email": editorEmail, "role": "editor"})
	req = httptest.NewRequest("POST", "/api/v1/members", bytes.NewReader(inviteBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("invite editor: expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	loginBody, _ = json.Marshal(map[string]string{
		"email": editorEmail, "password": "pass123", "org_id": adminOrgID,
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login editor: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var editorLoginResp map[string]any
	json.NewDecoder(rec.Body).Decode(&editorLoginResp)
	editorToken, _ := editorLoginResp["token"].(string)
	if editorToken == "" {
		t.Fatal("login editor: expected token in response")
	}

	req = httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set("Authorization", "Bearer "+editorToken)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor audit: expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
