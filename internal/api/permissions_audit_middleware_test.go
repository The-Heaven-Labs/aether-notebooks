package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiddleware_RequirePermission(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name       string
		userKey    string
		resourceID string
		want       int
	}{
		{
			name:       "adminA bypass on no-ACL connector",
			userKey:    "adminA",
			resourceID: f.OrgA.Connectors.NoACL,
			want:       http.StatusOK,
		},
		{
			name:       "aliceA denied on no-ACL connector",
			userKey:    "aliceA",
			resourceID: f.OrgA.Connectors.NoACL,
			want:       http.StatusForbidden,
		},
		{
			name:       "aliceA allowed via user ACL",
			userKey:    "aliceA",
			resourceID: f.OrgA.Connectors.UserACL,
			want:       http.StatusOK,
		},
		{
			name:       "bobA allowed via group ACL",
			userKey:    "bobA",
			resourceID: f.OrgA.Connectors.GroupACL,
			want:       http.StatusOK,
		},
		{
			name:       "carolA denied on no-ACL connector",
			userKey:    "carolA",
			resourceID: f.OrgA.Connectors.NoACL,
			want:       http.StatusForbidden,
		},
		{
			name:       "adminB cross-org on Org A connector — 403 bypass denied",
			userKey:    "adminB",
			resourceID: f.OrgA.Connectors.NoACL,
			want:       http.StatusForbidden,
		},
		{
			name:       "eveB cross-org denied on Org A connector",
			userKey:    "eveB",
			resourceID: f.OrgA.Connectors.NoACL,
			want:       http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/v1/connectors/" + tt.resourceID
			status, _ := f.DoRequest(t, tt.userKey, "GET", path, nil)
			require.Equal(t, tt.want, status)
		})
	}
}

func TestMiddleware_RequireRoleAdmin(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{
			name:    "adminA allowed to create group",
			userKey: "adminA",
			want:    http.StatusCreated,
		},
		{
			name:    "aliceA denied — not admin",
			userKey: "aliceA",
			want:    http.StatusForbidden,
		},
		{
			name:    "adminB allowed — admin of own org",
			userKey: "adminB",
			want:    http.StatusCreated,
		},
		{
			name:    "carolA denied — not admin",
			userKey: "carolA",
			want:    http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _ := f.DoRequest(t, tt.userKey, "POST", "/api/v1/groups", map[string]string{"name": "test-group"})
			require.Equal(t, tt.want, status)
		})
	}
}

func TestMiddleware_RequirePlatformAdmin(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{
			name:    "platAdmin allowed",
			userKey: "platAdmin",
			want:    http.StatusOK,
		},
		{
			name:    "adminA denied — not platform admin",
			userKey: "adminA",
			want:    http.StatusForbidden,
		},
		{
			name:    "adminB denied — not platform admin",
			userKey: "adminB",
			want:    http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _ := f.DoRequest(t, tt.userKey, "GET", "/api/v1/admin/orgs", nil)
			require.Equal(t, tt.want, status)
		})
	}
}

func TestMiddleware_AdminModeOff(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA admin mode off on no-ACL connector — denied", func(t *testing.T) {
		path := "/api/v1/connectors/" + f.OrgA.Connectors.NoACL
		req := f.Request(t, "adminA", "GET", path, nil)
		req.Header.Set("X-AETHER-Admin-Mode", "false")
		rec := httptest.NewRecorder()
		f.srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("adminA admin mode off on everyone-ACL connector — allowed", func(t *testing.T) {
		path := "/api/v1/connectors/" + f.OrgA.Connectors.EveryoneACL
		req := f.Request(t, "adminA", "GET", path, nil)
		req.Header.Set("X-AETHER-Admin-Mode", "false")
		rec := httptest.NewRecorder()
		f.srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}
