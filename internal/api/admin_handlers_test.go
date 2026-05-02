package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/heavenlabs/hnb/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
