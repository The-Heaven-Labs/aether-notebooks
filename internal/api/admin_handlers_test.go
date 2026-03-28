package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
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
