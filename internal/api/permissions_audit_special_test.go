package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// ——— PUBLIC ENDPOINTS (no auth required) ———

func TestPublicEndpoints(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)

	t.Run("GET /health — 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/health", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("GET /swagger.json — 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/swagger.json", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("POST /api/v1/auth/login (empty body) — 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("GET /api/v1/auth/sso-providers — 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/sso-providers", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("GET /api/v1/public/motd — 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/public/motd", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("GET /docs — 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/docs", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("GET /api/v1/_diagnose/master-key — 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/_diagnose/master-key", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}

// ——— AUTH-REQUIRED ENDPOINTS (unauthenticated → 401) ———

func TestAuthenticatedEndpoints_RejectUnauthenticated(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)

	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"GET /api/v1/users/me", "GET", "/api/v1/users/me"},
		{"PUT /api/v1/users/me", "PUT", "/api/v1/users/me"},
		{"GET /api/v1/home", "GET", "/api/v1/home"},
		{"GET /api/v1/recent", "GET", "/api/v1/recent"},
		{"GET /api/v1/members", "GET", "/api/v1/members"},
		{"GET /api/v1/notebooks", "GET", "/api/v1/notebooks"},
		{"POST /api/v1/notebooks", "POST", "/api/v1/notebooks"},
		{"GET /api/v1/groups", "GET", "/api/v1/groups"},
		{"GET /api/v1/connectors", "GET", "/api/v1/connectors"},
		{"GET /api/v1/folders", "GET", "/api/v1/folders"},
		{"GET /api/v1/agents", "GET", "/api/v1/agents"},
		{"GET /api/v1/mcp-servers", "GET", "/api/v1/mcp-servers"},
		{"GET /api/v1/templates", "GET", "/api/v1/templates"},
		{"GET /api/v1/dashboards", "GET", "/api/v1/dashboards"},
		{"GET /api/v1/tokens", "GET", "/api/v1/tokens"},
		{"GET /api/v1/admin/orgs", "GET", "/api/v1/admin/orgs"},
		{"GET /api/v1/audit", "GET", "/api/v1/audit"},
		{"GET /api/v1/motd", "GET", "/api/v1/motd"},
		{"GET /api/v1/model-configs", "GET", "/api/v1/model-configs"},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code,
				"expected 401 for %s %s, got %d: %s", ep.method, ep.path, rec.Code, rec.Body.String())
		})
	}
}

// ——— ATTACHMENT ENDPOINTS UNAUTHENTICATED ———

func TestAttachmentEndpoints_RejectUnauthenticated(t *testing.T) {
	t.Parallel()
	srv := setupTestServer(t)

	t.Run("POST upload attachment — 401", func(t *testing.T) {
		req := httptest.NewRequest("POST",
			"/api/v1/notebooks/00000000-0000-0000-0000-000000000000/attachments", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("GET list attachments — 401", func(t *testing.T) {
		req := httptest.NewRequest("GET",
			"/api/v1/notebooks/00000000-0000-0000-0000-000000000000/attachments", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("GET attachment — 401", func(t *testing.T) {
		req := httptest.NewRequest("GET",
			"/api/v1/attachments/00000000-0000-0000-0000-000000000000", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("DELETE attachment — 401", func(t *testing.T) {
		req := httptest.NewRequest("DELETE",
			"/api/v1/attachments/00000000-0000-0000-0000-000000000000", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

// ——— MOTD ENDPOINTS ———

func TestMOTD_Endpoints(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA creates MOTD — 201", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "POST", "/api/v1/admin/motd", map[string]any{
			"title":    "Test MOTD",
			"content":  "System maintenance tonight",
			"priority": 1,
		})
		require.Equal(t, http.StatusCreated, status, "body: %s", body)
	})

	t.Run("aliceA GET /api/v1/motd — 200 (any member)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/motd", nil)
		require.Equal(t, http.StatusOK, status, "body: %s", body)
	})

	t.Run("carolA GET /api/v1/motd — 200 (any member)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/motd", nil)
		require.Equal(t, http.StatusOK, status, "body: %s", body)
	})

	t.Run("adminA GET /api/v1/admin/motd — 200 (admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET", "/api/v1/admin/motd", nil)
		require.Equal(t, http.StatusOK, status, "body: %s", body)
	})

	t.Run("aliceA GET /api/v1/admin/motd — 403", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/admin/motd", nil)
		t.Logf("aliceA GET /api/v1/admin/motd: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA accessed admin MOTD without admin role")
		}
	})

	t.Run("aliceA POST /api/v1/admin/motd — 403", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST", "/api/v1/admin/motd", map[string]any{
			"title":   "Should not work",
			"content": "test",
		})
		t.Logf("aliceA POST /api/v1/admin/motd: %d %s", status, body)
		if status == http.StatusCreated {
			t.Log("VULNERABILITY: aliceA created MOTD without admin role")
		}
	})

	t.Run("carolA POST /api/v1/admin/motd — 403", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "POST", "/api/v1/admin/motd", map[string]any{
			"title":   "Should not work",
			"content": "test",
		})
		t.Logf("carolA POST /api/v1/admin/motd: %d %s", status, body)
		if status == http.StatusCreated {
			t.Log("VULNERABILITY: carolA created MOTD without admin role")
		}
	})

	t.Run("adminB (cross-org) GET /api/v1/motd — 200 (own org, empty)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/motd", nil)
		require.Equal(t, http.StatusOK, status, "body: %s", body)
	})
}

// ——— CROSS-ORG MEMBER LISTING ———

func TestMembers_CrossOrgIsolation(t *testing.T) {
	t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA lists Org A members — contains expected users", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "GET", "/api/v1/members", nil)
		require.Equal(t, http.StatusOK, status)
		var members []map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &members))
		t.Logf("adminA sees %d members", len(members))
		for _, m := range members {
			t.Logf("  member: %s (%s)", m["email"], m["role"])
		}
		// adminA's org has: adminA (admin), aliceA (editor), bobA (editor), carolA (editor), platAdmin (admin)
		require.GreaterOrEqual(t, len(members), 4)
		// Should NOT include Org B members
		for _, m := range members {
			email, _ := m["email"].(string)
			if email != "" && (email == "adminB" || email == "eveB") {
				t.Logf("VULNERABILITY: Org A member list contains cross-org user: %s", email)
			}
		}
	})

	t.Run("adminB lists Org B members — does not contain Org A users", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/members", nil)
		require.Equal(t, http.StatusOK, status)
		var members []map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &members))
		t.Logf("adminB sees %d members", len(members))
		for _, m := range members {
			t.Logf("  member: %s (%s)", m["email"], m["role"])
		}
		// adminB's org has: adminB (admin), eveB (editor)
		require.GreaterOrEqual(t, len(members), 1)
		require.LessOrEqual(t, len(members), 2)
	})
}
