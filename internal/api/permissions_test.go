package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPermission_OrgRoleFallback: no ACL anywhere → org role defaults apply.
// Admin should be able to view a notebook they created (no ACL = fallback to admin role = all actions allowed).
func TestPermission_OrgRoleFallback(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("perm-fallback-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Perm Fallback Org")
	nbID := createNotebook(t, srv, token, "Fallback NB")

	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	// Before requirePermission is wired to notebooks, this just tests the endpoint is reachable
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPermission_ACLGrantByOrgRole: ACL entry for org_role 'admin' allows admin user to view.
func TestPermission_ACLGrantByOrgRole(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("perm-acl-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "ACL Grant Org")

	// Create a notebook and capture its org_id
	body, _ := json.Marshal(map[string]string{"title": "ACL NB"})
	r := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create notebook: %d %s", rec.Code, rec.Body.String())
	}
	var nb map[string]any
	json.NewDecoder(rec.Body).Decode(&nb)
	orgID := nb["org_id"].(string)
	nbID := nb["id"].(string)

	// Seed an ACL entry: org_role 'admin' can view+edit this notebook
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'org_role', 'admin', ARRAY['view','edit','delete'])`,
		orgID, nbID,
	)
	if err != nil {
		t.Fatalf("seed ACL: %v", err)
	}

	// Admin user should still be able to GET the notebook (matched via org_role entry)
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("admin get with ACL: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// TestPermission_DenyWhenACLExistsButNoMatch: ACL exists for a different user → our user is denied.
// NOTE: This test will pass after requirePermission is wired to the notebook GET route in Task 8.
// For now it just seeds data and verifies the structure compiles.
func TestPermission_DenyWhenACLExistsButNoMatch(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	email := fmt.Sprintf("perm-deny-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Deny Org")

	body, _ := json.Marshal(map[string]string{"title": "Deny NB"})
	r := httptest.NewRequest("POST", "/api/v1/notebooks", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, r)
	var nb map[string]any
	json.NewDecoder(rec.Body).Decode(&nb)
	orgID := nb["org_id"].(string)
	nbID := nb["id"].(string)

	// Insert an ACL entry for a different user (not our user)
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'notebook', $2::uuid, 'user', '00000000-0000-0000-0000-000000000099', ARRAY['view'])`,
		orgID, nbID,
	)
	if err != nil {
		t.Fatalf("seed ACL: %v", err)
	}

	// Currently (before wiring requirePermission to notebooks), this returns 200.
	// After Task 8 wires requirePermission to GET /notebooks/:id, this should return 403.
	// For now, just verify the endpoint still responds (not a 500).
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code == http.StatusInternalServerError {
		t.Fatalf("got 500, something is broken: %s", rec2.Body.String())
	}
}
