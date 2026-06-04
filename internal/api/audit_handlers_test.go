package api_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuditLogEnrichment(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("audit-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "AuditOrg")
	createNotebook(t, srv, token, "My Notebook")

	// Fetch audit logs
	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("audit: %d %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Entries []map[string]any `json:"entries"`
		Total   int              `json:"total"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Entries) == 0 {
		t.Fatal("expected audit entries")
	}
	if resp.Total == 0 {
		t.Fatalf("expected total > 0, got %d", resp.Total)
	}
	// The notebook.create entry should have resource_name = "My Notebook"
	for _, e := range resp.Entries {
		if e["action"] == "notebook.create" {
			if e["resource_name"] != "My Notebook" {
				t.Fatalf("expected resource_name 'My Notebook', got %v", e["resource_name"])
			}
			if e["user_email"] == "" || e["user_email"] == nil {
				t.Fatalf("expected user_email, got %v", e["user_email"])
			}
			return
		}
	}
	t.Fatal("notebook.create entry not found")
}

func TestAuditLogAuthorization(t *testing.T) {
	srv := setupTestServer(t)
	ts := time.Now().UnixNano()

	// Register a user and get an admin token
	adminToken := registerAndGetToken(t, srv, fmt.Sprintf("audit-admin-%d@example.com", ts), fmt.Sprintf("AuditAuthOrg-%d", ts))

	// Admin can access audit log
	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("admin should get 200, got %d", rec.Code)
	}

	// Unauthenticated request gets 401
	req2 := httptest.NewRequest("GET", "/api/v1/audit", nil)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != 401 {
		t.Fatalf("unauthenticated should get 401, got %d", rec2.Code)
	}
}
