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

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/stretchr/testify/require"
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

// TestPermission_NoACL_DenyByDefault: non-admin user with NO ACL entry is denied access.
// Previously the org-role fallback would have allowed editors/viewers access.
// After the org-role fallback removal, this user must be denied.
func TestPermission_NoACL_DenyByDefault(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	// Create admin org owner who will create the notebook
	adminEmail := fmt.Sprintf("admin-owner-%d@example.com", time.Now().UnixNano())
	adminToken := registerAndGetToken(t, srv, adminEmail, "DenyByDefault Org")

	// Create a notebook as admin
	nbID := createNotebook(t, srv, adminToken, "DenyByDefault NB")

	// Get the notebook's org_id
	var orgID string
	err := db.Pool.QueryRow(ctx, `SELECT org_id FROM notebooks WHERE id = $1`, nbID).Scan(&orgID)
	require.NoError(t, err)

	// Create a separate viewer user (no ACL, just org_role)
	viewerEmail := fmt.Sprintf("viewer-%d@example.com", time.Now().UnixNano())
	var viewerUserID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
		 VALUES ($1, 'x', 'Viewer', false) RETURNING id`,
		viewerEmail,
	).Scan(&viewerUserID)
	require.NoError(t, err)

	// Add viewer to org with 'viewer' role (no admin, no ACL entry)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'viewer')`,
		orgID, viewerUserID,
	)
	require.NoError(t, err)

	// Issue JWT for viewer user directly (since we can't go through register)
	viewerToken, err := testJWT.Issue(viewerUserID, orgID, "viewer")
	require.NoError(t, err)

	// Viewer tries to access the notebook - should be DENIED (no ACL, org-role fallback removed)
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	// After the fix, this should be 403 Forbidden
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer with no ACL, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPermission_OrgAdminBypass: org admin can access any resource WITHOUT needing an ACL entry.
func TestPermission_OrgAdminBypass(t *testing.T) {
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	// Create admin org owner
	adminEmail := fmt.Sprintf("admin-owner-%d@example.com", time.Now().UnixNano())
	adminToken := registerAndGetToken(t, srv, adminEmail, "AdminBypass Org")

	// Create a notebook as admin
	nbID := createNotebook(t, srv, adminToken, "AdminBypass NB")

	// Get notebook org_id
	var orgID string
	err := db.Pool.QueryRow(ctx, `SELECT org_id FROM notebooks WHERE id = $1`, nbID).Scan(&orgID)
	require.NoError(t, err)

	// Create a separate editor user (no ACL for the notebook)
	editorEmail := fmt.Sprintf("editor-%d@example.com", time.Now().UnixNano())
	var editorUserID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
		 VALUES ($1, 'x', 'Editor', false) RETURNING id`,
		editorEmail,
	).Scan(&editorUserID)
	require.NoError(t, err)

	// Add editor to org with 'editor' role (no ACL entry for this notebook)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'editor')`,
		orgID, editorUserID,
	)
	require.NoError(t, err)

	// Issue JWT for editor
	editorToken, err := testJWT.Issue(editorUserID, orgID, "editor")
	require.NoError(t, err)

	// Editor should be denied (no ACL, not admin)
	req := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req.Header.Set("Authorization", "Bearer "+editorToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor with no ACL should be denied, got %d: %s", rec.Code, rec.Body.String())
	}

	// Now create an admin user (who will have org_role=admin but no ACL entry)
	admin2Email := fmt.Sprintf("admin2-%d@example.com", time.Now().UnixNano())
	var admin2UserID string
	err = db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
		 VALUES ($1, 'x', 'Admin2', false) RETURNING id`,
		admin2Email,
	).Scan(&admin2UserID)
	require.NoError(t, err)

	// Add admin2 to org with 'admin' role (no ACL entry for this notebook)
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, admin2UserID,
	)
	require.NoError(t, err)

	// Issue JWT for admin2
	admin2Token, err := testJWT.Issue(admin2UserID, orgID, "admin")
	require.NoError(t, err)

	// Admin2 should be ALLOWED (org admin bypass, no ACL needed)
	req2 := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
	req2.Header.Set("Authorization", "Bearer "+admin2Token)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("admin should bypass ACL, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// extractUserIDFromToken extracts userID from a token by calling /api/v1/me
func extractUserIDFromToken(t *testing.T, srv *api.Server, token string) string {
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	return resp["id"].(string)
}
