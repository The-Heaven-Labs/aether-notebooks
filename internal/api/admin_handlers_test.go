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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

func TestAdminListOrgs_RequiresPlatformAdmin(t *testing.T) {
	s := setupTestServer(t)

	// Regular admin cannot access platform admin routes
	req := httptest.NewRequest("GET", "/api/v1/admin/orgs", nil)
	req = withAdminClaims(req, testOrgID)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminListOrgs_PlatformAdminCanAccess(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/admin/orgs", nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAdminListUsers(t *testing.T) {
	s := setupTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestSeedPlatformAdmin(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	email := fmt.Sprintf("admin-%d@example.com", time.Now().UnixNano())

	// User doesn't exist yet: seed is a no-op, no error
	promoted, err := api.SeedPlatformAdmin(ctx, s.DB().Pool, email)
	assert.NoError(t, err)
	assert.False(t, promoted)

	// Create the user
	var userID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
         VALUES ($1, 'x', 'Test', false) RETURNING id`, email).Scan(&userID)
	require.NoError(t, err)

	// Seed: user now exists, should be promoted
	promoted, err = api.SeedPlatformAdmin(ctx, s.DB().Pool, email)
	require.NoError(t, err)
	assert.True(t, promoted)

	// Verify in DB
	var isPlatformAdmin bool
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT is_platform_admin FROM users WHERE id=$1`, userID).Scan(&isPlatformAdmin)
	require.NoError(t, err)
	assert.True(t, isPlatformAdmin)
}

func TestAdminUpdateUser_Promote(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	// Create a regular user to promote
	var targetID string
	promoteEmail := fmt.Sprintf("promote-target-%d@example.com", time.Now().UnixNano())
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
         VALUES ($1, 'x', 'Target', false) RETURNING id`, promoteEmail,
	).Scan(&targetID)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]bool{"is_platform_admin": true})
	req := httptest.NewRequest("PUT", "/api/v1/admin/users/"+targetID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var promoted bool
	err = s.DB().Pool.QueryRow(ctx, `SELECT is_platform_admin FROM users WHERE id=$1`, targetID).Scan(&promoted)
	require.NoError(t, err)
	assert.True(t, promoted)
}

func TestAdminUpdateUser_Demote(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	var targetID string
	demoteEmail := fmt.Sprintf("demote-target-%d@example.com", time.Now().UnixNano())
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified, is_platform_admin)
         VALUES ($1, 'x', 'Demote', false, true) RETURNING id`, demoteEmail,
	).Scan(&targetID)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]bool{"is_platform_admin": false})
	req := httptest.NewRequest("PUT", "/api/v1/admin/users/"+targetID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var demoted bool
	err = s.DB().Pool.QueryRow(ctx, `SELECT is_platform_admin FROM users WHERE id=$1`, targetID).Scan(&demoted)
	require.NoError(t, err)
	assert.False(t, demoted)
}

func TestAdminUpdateUser_SelfDemotionBlocked(t *testing.T) {
	s := setupTestServer(t)

	// withPlatformAdminClaims uses testUserID — try to demote self
	body, _ := json.Marshal(map[string]bool{"is_platform_admin": false})
	req := httptest.NewRequest("PUT", "/api/v1/admin/users/"+testUserID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminUpdateUser_NotFound(t *testing.T) {
	s := setupTestServer(t)

	body, _ := json.Marshal(map[string]bool{"is_platform_admin": true})
	req := httptest.NewRequest("PUT", "/api/v1/admin/users/00000000-0000-0000-0000-000000000000", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminUpdateUser_RequiresPlatformAdmin(t *testing.T) {
	s := setupTestServer(t)

	body, _ := json.Marshal(map[string]bool{"is_platform_admin": true})
	req := httptest.NewRequest("PUT", "/api/v1/admin/users/some-user-id", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminClaims(req, testOrgID)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminDeleteUser(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	// Ensure the test org referenced by memberships exists.
	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO orgs (id, name, slug) VALUES ($1, 'Test Org', 'test-org') ON CONFLICT (id) DO NOTHING`, testOrgID)
	require.NoError(t, err)

	// Ensure the platform admin (testUserID) exists as a DB row so reassigned
	// resources can reference them.
	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, email_verified)
		 VALUES ($1, 'platform-admin@example.com', 'x', 'Platform Admin', false)
		 ON CONFLICT (id) DO NOTHING`, testUserID)
	require.NoError(t, err)

	// Create a user with org membership, home folder, notebook, connector, API token.
	email := fmt.Sprintf("delete-target-%d@example.com", time.Now().UnixNano())
	var targetID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
		 VALUES ($1, 'x', 'Delete Me', false) RETURNING id`, email,
	).Scan(&targetID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'editor')`, testOrgID, targetID)
	require.NoError(t, err)

	var folderID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO folders (org_id, name, is_home, owner_id, created_by)
		 VALUES ($1, $2, true, $3, $3) RETURNING id`, testOrgID, email, targetID,
	).Scan(&folderID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2, 'user', $3, ARRAY['view','create','edit','manage','delete'])`,
		testOrgID, folderID, targetID)
	require.NoError(t, err)

	var nbID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO notebooks (org_id, title, created_by) VALUES ($1, 'Owned Notebook', $2) RETURNING id`,
		testOrgID, targetID,
	).Scan(&nbID)
	require.NoError(t, err)

	var connID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO connectors (org_id, name, type, config_encrypted) VALUES ($1, 'Owned Connector', 'postgres', '\x00') RETURNING id`,
		testOrgID,
	).Scan(&connID)
	require.NoError(t, err)
	_, err = s.DB().Pool.Exec(ctx,
		`UPDATE connectors SET created_by = $1 WHERE id = $2`, targetID, connID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO api_tokens (user_id, org_id, name, token_hash) VALUES ($1, $2, 'test', 'hash')`,
		targetID, testOrgID)
	require.NoError(t, err)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/users/"+targetID, nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// User deleted.
	var count int
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE id = $1`, targetID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Membership + home folder + ACL + API token cleaned up.
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_members WHERE user_id = $1`, targetID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM folders WHERE owner_id = $1`, targetID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM acl_entries WHERE subject_type = 'user' AND subject_id = $1`, targetID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_tokens WHERE user_id = $1`, targetID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Notebook reassigned to the platform admin (testUserID); connector's created_by nulled.
	var creator string
	err = s.DB().Pool.QueryRow(ctx, `SELECT created_by FROM notebooks WHERE id = $1`, nbID).Scan(&creator)
	require.NoError(t, err)
	assert.Equal(t, testUserID, creator)
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COALESCE(created_by::text, '') FROM connectors WHERE id = $1`, connID).Scan(&creator)
	require.NoError(t, err)
	assert.Equal(t, "", creator)
}

func TestAdminDeleteUser_SelfDeletionBlocked(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/users/"+testUserID, nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminDeleteUser_NotFound(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/users/00000000-0000-0000-0000-000000000000", nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminDeleteUser_RequiresPlatformAdmin(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/users/some-user-id", nil)
	req = withAdminClaims(req, testOrgID)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminDeleteOrg(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	// Ensure the platform admin (testUserID) exists as a DB row so memberships can reference it.
	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, email_verified)
		 VALUES ($1, 'platform-admin@example.com', 'x', 'Platform Admin', false)
		 ON CONFLICT (id) DO NOTHING`, testUserID)
	require.NoError(t, err)

	slug := fmt.Sprintf("del-org-%d", time.Now().UnixNano())
	var orgID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`, "Delete Org", slug,
	).Scan(&orgID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`, orgID, testUserID)
	require.NoError(t, err)

	var nbID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO notebooks (org_id, title, created_by) VALUES ($1, 'Org Notebook', $2) RETURNING id`,
		orgID, testUserID,
	).Scan(&nbID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO folders (org_id, name, is_home, owner_id, created_by)
		 VALUES ($1, 'Home', true, $2, $2)`, orgID, testUserID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO api_tokens (user_id, org_id, name, token_hash) VALUES ($1, $2, 'test', 'hash')`,
		testUserID, orgID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO motd_messages (org_id, content) VALUES ($1, 'hello')`, orgID)
	require.NoError(t, err)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/orgs/"+orgID, nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	var count int
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM orgs WHERE id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM notebooks WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM folders WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM api_tokens WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	err = s.DB().Pool.QueryRow(ctx, `SELECT COUNT(*) FROM motd_messages WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestAdminDeleteOrg_NotFound(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/orgs/00000000-0000-0000-0000-000000000000", nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminDeleteOrg_RequiresPlatformAdmin(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/orgs/some-org-id", nil)
	req = withAdminClaims(req, testOrgID)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
