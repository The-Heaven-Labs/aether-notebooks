package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformAdminSSOProviderCRUD(t *testing.T) {
	s := setupTestServer(t)

	createBody := map[string]any{
		"name":              "Acme Okta",
		"client_id":         "abc123",
		"client_secret":     "super-secret",
		"discovery_url":     "https://acme.okta.com/.well-known/openid-configuration",
		"allowed_domains":   []string{"acme.com", "acme.org"},
		"enabled":           true,
		"provisioning_mode": "join_provider_org",
		"default_role":      "viewer",
	}

	// 1. Create a platform provider → 201
	body, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/api/v1/admin/sso/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPlatformAdminClaims(req)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create body: %s", rec.Body.String())

	var created map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))
	providerID, ok := created["id"].(string)
	require.True(t, ok, "response should have id")
	assert.Equal(t, "platform", created["scope"])
	assert.Equal(t, "Acme Okta", created["name"])
	assert.Equal(t, "oidc", created["provider_type"])
	assert.Equal(t, "abc123", created["client_id"])
	assert.Equal(t, "https://acme.okta.com/.well-known/openid-configuration", created["discovery_url"])
	assert.Nil(t, created["client_secret"], "client_secret must not be returned")
	assert.True(t, created["enabled"].(bool))
	assert.Equal(t, "join_provider_org", created["provisioning_mode"])
	assert.Equal(t, "viewer", created["default_role"])
	_, hasCallback := created["callback_url"]
	assert.True(t, hasCallback, "callback_url should be present in the response")

	// 2. List providers → includes the created one
	req = httptest.NewRequest("GET", "/api/v1/admin/sso/providers", nil)
	req = withPlatformAdminClaims(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var listResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&listResp))
	providers := listResp["providers"].([]any)
	found := false
	for _, p := range providers {
		pm := p.(map[string]any)
		if pm["id"] == providerID {
			found = true
			assert.Nil(t, pm["client_secret"], "client_secret must not be in list response")
		}
	}
	assert.True(t, found, "created provider should appear in list")

	// 3. Update name → 200, verify name changed
	updateBody := map[string]any{
		"name":            "Acme Okta Updated",
		"client_id":       "abc123",
		"client_secret":   "super-secret",
		"discovery_url":   "https://acme.okta.com/.well-known/openid-configuration",
		"allowed_domains": []string{"acme.com"},
		"enabled":         true,
	}
	body, _ = json.Marshal(updateBody)
	req = httptest.NewRequest("PUT", "/api/v1/admin/sso/providers/"+providerID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPlatformAdminClaims(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "update body: %s", rec.Body.String())

	var updated map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&updated))
	assert.Equal(t, "Acme Okta Updated", updated["name"])
	assert.Nil(t, updated["client_secret"], "client_secret must not be returned on update")

	// 4. Delete → 204
	req = httptest.NewRequest("DELETE", "/api/v1/admin/sso/providers/"+providerID, nil)
	req = withPlatformAdminClaims(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	// 5. List again → not present
	req = httptest.NewRequest("GET", "/api/v1/admin/sso/providers", nil)
	req = withPlatformAdminClaims(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var listResp2 map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&listResp2))
	providers2 := listResp2["providers"].([]any)
	for _, p := range providers2 {
		pm := p.(map[string]any)
		assert.NotEqual(t, providerID, pm["id"], "deleted provider should not appear in list")
	}
}

func TestPlatformAdminSSOProviders_ForbiddenForNonPlatformAdmin(t *testing.T) {
	s := setupTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"name":          "Test",
		"client_id":     "id",
		"client_secret": "secret",
		"discovery_url": "https://example.com/.well-known/openid-configuration",
		"enabled":       true,
	})

	routes := []struct {
		method string
		path   string
		body   []byte
	}{
		{"GET", "/api/v1/admin/sso/providers", nil},
		{"POST", "/api/v1/admin/sso/providers", body},
		{"PUT", "/api/v1/admin/sso/providers/some-id", body},
		{"DELETE", "/api/v1/admin/sso/providers/some-id", nil},
	}

	for _, r := range routes {
		var bodyReader *bytes.Reader
		if r.body != nil {
			bodyReader = bytes.NewReader(r.body)
		} else {
			bodyReader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(r.method, r.path, bodyReader)
		req.Header.Set("Content-Type", "application/json")
		req = withAdminClaims(req, testOrgID) // regular org admin, not platform admin
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code, "expected 403 for %s %s", r.method, r.path)
	}
}

func TestPlatformAdminSSOProviders_UpdateNotFound(t *testing.T) {
	s := setupTestServer(t)

	body, _ := json.Marshal(map[string]any{
		"name":          "Test",
		"client_id":     "id",
		"client_secret": "secret",
		"discovery_url": "https://example.com/.well-known/openid-configuration",
		"enabled":       true,
	})
	req := httptest.NewRequest("PUT", "/api/v1/admin/sso/providers/00000000-0000-0000-0000-000000000999", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPlatformAdminClaims(req)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPlatformAdminSSOProviders_DeleteNotFound(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/v1/admin/sso/providers/00000000-0000-0000-0000-000000000999", nil)
	req = withPlatformAdminClaims(req)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	// DELETE of non-existent is idempotent — returns 204
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
