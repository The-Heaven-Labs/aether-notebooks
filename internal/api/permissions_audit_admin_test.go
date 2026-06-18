package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ——— LIST ORGS ———

func TestAdminRoute_ListOrgs(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"platAdmin — 200", "platAdmin", http.StatusOK},
		{"adminA — 403 (not platform admin)", "adminA", http.StatusForbidden},
		{"aliceA — 403", "aliceA", http.StatusForbidden},
		{"adminB — 403 (not platform admin)", "adminB", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/admin/orgs", nil)
			require.Equal(t, tt.want, status, "body: %s", body)
		})
	}
}

// ——— LIST USERS ———

func TestAdminRoute_ListUsers(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"platAdmin — 200", "platAdmin", http.StatusOK},
		{"adminA — 403 (not platform admin)", "adminA", http.StatusForbidden},
		{"aliceA — 403", "aliceA", http.StatusForbidden},
		{"adminB — 403 (not platform admin)", "adminB", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/admin/users", nil)
			require.Equal(t, tt.want, status, "body: %s", body)
		})
	}
}

// ——— UPDATE USER ———

func TestAdminRoute_UpdateUser(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("platAdmin promotes aliceA — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "platAdmin", "PUT",
			"/api/v1/admin/users/"+f.UserIDs["aliceA"],
			map[string]bool{"is_platform_admin": true})
		require.Equal(t, http.StatusOK, status, "body: %s", body)
	})

	t.Run("adminA tries to promote — 403 (not platform admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "PUT",
			"/api/v1/admin/users/"+f.UserIDs["aliceA"],
			map[string]bool{"is_platform_admin": true})
		require.Equal(t, http.StatusForbidden, status, "body: %s", body)
	})

	t.Run("aliceA tries to promote — 403", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT",
			"/api/v1/admin/users/"+f.UserIDs["bobA"],
			map[string]bool{"is_platform_admin": true})
		require.Equal(t, http.StatusForbidden, status, "body: %s", body)
	})

	t.Run("platAdmin self-demotion — 400 (prevented)", func(t *testing.T) {
		status, body := f.DoRequest(t, "platAdmin", "PUT",
			"/api/v1/admin/users/"+f.UserIDs["platAdmin"],
			map[string]bool{"is_platform_admin": false})
		require.Equal(t, http.StatusBadRequest, status, "body: %s", body)
	})

	t.Run("platAdmin on non-existent user — 404", func(t *testing.T) {
		status, body := f.DoRequest(t, "platAdmin", "PUT",
			"/api/v1/admin/users/00000000-0000-0000-0000-000000000000",
			map[string]bool{"is_platform_admin": true})
		require.Equal(t, http.StatusNotFound, status, "body: %s", body)
	})
}
