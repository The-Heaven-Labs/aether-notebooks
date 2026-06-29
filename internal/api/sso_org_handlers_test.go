package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registerOrgAndGetAdminToken registers a new org + user and returns (orgID, token).
func registerOrgAndGetAdminToken(t *testing.T, srv http.Handler) (string, string) {
	t.Helper()
	email := fmt.Sprintf("orgadmin-%d@example.com", time.Now().UnixNano())
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "pass123",
		"name":     "Admin User",
	})
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	regReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, regReq)
	require.Equal(t, http.StatusCreated, rec.Code, "register: %s", rec.Body.String())
	var regResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&regResp))
	onboardingToken, ok := regResp["onboarding_token"].(string)
	require.True(t, ok, "register response should contain onboarding_token, got %v", regResp)

	orgName := fmt.Sprintf("OrgSSO %d", time.Now().UnixNano())
	orgBody, _ := json.Marshal(map[string]string{"org_name": orgName})
	orgReq := httptest.NewRequest("POST", "/api/v1/auth/org/create", bytes.NewReader(orgBody))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	orgRec := httptest.NewRecorder()
	srv.ServeHTTP(orgRec, orgReq)
	require.Equal(t, http.StatusCreated, orgRec.Code, "org create: %s", orgRec.Body.String())
	var orgResp map[string]any
	require.NoError(t, json.NewDecoder(orgRec.Body).Decode(&orgResp))
	token := orgResp["token"].(string)
	org := orgResp["org"].(map[string]any)
	orgID := org["id"].(string)
	return orgID, token
}

// TestOrgAdminSSOProviderCRUD tests create, list, update, delete of an org-scoped provider.
func TestOrgAdminSSOProviderCRUD(t *testing.T) {
	s := setupTestServer(t)
	_, token := registerOrgAndGetAdminToken(t, s)

	authHeader := func(r *http.Request) *http.Request {
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}

	createBody := map[string]any{
		"name":            "Corp OIDC",
		"client_id":       "cid-org-1",
		"client_secret":   "org-secret",
		"discovery_url":   "https://corp.example.com/.well-known/openid-configuration",
		"allowed_domains": []string{"corp.example.com"},
		"enabled":         true,
	}

	// 1. Create provider → 201
	body, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/api/v1/sso/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = authHeader(req)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create: %s", rec.Body.String())

	var created map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))
	providerID, ok := created["id"].(string)
	require.True(t, ok, "response should have id")
	assert.Equal(t, "org", created["scope"])
	assert.Equal(t, "Corp OIDC", created["name"])
	assert.Equal(t, "oidc", created["provider_type"])
	assert.Equal(t, "cid-org-1", created["client_id"])
	assert.Nil(t, created["client_secret"], "client_secret must not be returned")
	assert.True(t, created["enabled"].(bool))

	// 2. List providers → includes created one
	req = httptest.NewRequest("GET", "/api/v1/sso/providers", nil)
	req = authHeader(req)
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
			assert.Equal(t, "org", pm["scope"])
		}
	}
	assert.True(t, found, "created provider should appear in list")

	// 3. Update → 200
	updateBody := map[string]any{
		"name":            "Corp OIDC Updated",
		"client_id":       "cid-org-1",
		"client_secret":   "org-secret",
		"discovery_url":   "https://corp.example.com/.well-known/openid-configuration",
		"allowed_domains": []string{"corp.example.com"},
		"enabled":         false,
	}
	body, _ = json.Marshal(updateBody)
	req = httptest.NewRequest("PUT", "/api/v1/sso/providers/"+providerID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "update: %s", rec.Body.String())

	var updated map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&updated))
	assert.Equal(t, "Corp OIDC Updated", updated["name"])
	assert.False(t, updated["enabled"].(bool))
	assert.Nil(t, updated["client_secret"])

	// 4. Delete → 204
	req = httptest.NewRequest("DELETE", "/api/v1/sso/providers/"+providerID, nil)
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	// 5. List again → not present
	req = httptest.NewRequest("GET", "/api/v1/sso/providers", nil)
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var listResp2 map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&listResp2))
	providers2, _ := listResp2["providers"].([]any)
	for _, p := range providers2 {
		pm := p.(map[string]any)
		assert.NotEqual(t, providerID, pm["id"], "deleted provider should not appear")
	}
}

// TestOrgAdminCannotModifyOtherOrgProvider ensures org A cannot modify org B's provider.
func TestOrgAdminCannotModifyOtherOrgProvider(t *testing.T) {
	s := setupTestServer(t)

	// Org A creates a provider
	_, tokenA := registerOrgAndGetAdminToken(t, s)
	// Org B registers separately
	_, tokenB := registerOrgAndGetAdminToken(t, s)

	// Org A creates provider
	createBody := map[string]any{
		"name":          "Org A Provider",
		"client_id":     "cid-a",
		"client_secret": "secret-a",
		"discovery_url": "https://a.example.com/.well-known/openid-configuration",
		"enabled":       true,
	}
	body, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/api/v1/sso/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenA)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "org A create: %s", rec.Body.String())

	var created map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))
	providerID := created["id"].(string)

	updateBody := map[string]any{
		"name":          "Hijacked",
		"client_id":     "evil",
		"client_secret": "evil-secret",
		"discovery_url": "https://evil.example.com/.well-known/openid-configuration",
		"enabled":       true,
	}
	body, _ = json.Marshal(updateBody)

	// Org B tries to update → 403
	req = httptest.NewRequest("PUT", "/api/v1/sso/providers/"+providerID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenB)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "org B update should be 403")

	// Org B tries to delete → 403
	req = httptest.NewRequest("DELETE", "/api/v1/sso/providers/"+providerID, nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "org B delete should be 403")
}

// TestOrgAdminPlatformProviderEnableDisable tests enabling/disabling platform providers.
func TestOrgAdminPlatformProviderEnableDisable(t *testing.T) {
	s := setupTestServer(t)
	_, token := registerOrgAndGetAdminToken(t, s)

	authHeader := func(r *http.Request) *http.Request {
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}

	// Use the admin API to create a platform provider (requires platform admin)
	createBody := map[string]any{
		"name":          "Platform Google",
		"client_id":     "google-cid",
		"client_secret": "google-secret",
		"discovery_url": "https://accounts.google.com/.well-known/openid-configuration",
		"enabled":       true,
	}
	body, _ := json.Marshal(createBody)
	req := httptest.NewRequest("POST", "/api/v1/admin/sso/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPlatformAdminClaims(req)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create platform provider: %s", rec.Body.String())

	var platformProvider map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&platformProvider))
	platformProviderID := platformProvider["id"].(string)

	// Cleanup: delete platform provider via admin API after test
	t.Cleanup(func() {
		cleanupReq := httptest.NewRequest("DELETE", "/api/v1/admin/sso/providers/"+platformProviderID, nil)
		cleanupReq = withPlatformAdminClaims(cleanupReq)
		cleanupRec := httptest.NewRecorder()
		s.ServeHTTP(cleanupRec, cleanupReq)
	})

	// List platform providers → enabled_for_org should be false
	req = httptest.NewRequest("GET", "/api/v1/sso/platform-providers", nil)
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var listResp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&listResp))
	platformProviders := listResp["providers"].([]any)
	foundDisabled := false
	for _, p := range platformProviders {
		pm := p.(map[string]any)
		if pm["id"] == platformProviderID {
			foundDisabled = true
			assert.False(t, pm["enabled_for_org"].(bool), "should not be enabled yet")
		}
	}
	assert.True(t, foundDisabled, "platform provider should appear in list")

	// Enable the platform provider for this org
	req = httptest.NewRequest("POST", "/api/v1/sso/platform-providers/"+platformProviderID+"/enable", nil)
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, "enable: %s", rec.Body.String())

	// List platform providers → enabled_for_org should be true
	req = httptest.NewRequest("GET", "/api/v1/sso/platform-providers", nil)
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var listResp2 map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&listResp2))
	platformProviders2 := listResp2["providers"].([]any)
	foundEnabled := false
	for _, p := range platformProviders2 {
		pm := p.(map[string]any)
		if pm["id"] == platformProviderID {
			foundEnabled = true
			assert.True(t, pm["enabled_for_org"].(bool), "should be enabled now")
		}
	}
	assert.True(t, foundEnabled, "platform provider should appear after enable")

	// Disable the platform provider
	req = httptest.NewRequest("DELETE", "/api/v1/sso/platform-providers/"+platformProviderID+"/enable", nil)
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, "disable: %s", rec.Body.String())

	// List platform providers → enabled_for_org should be false again
	req = httptest.NewRequest("GET", "/api/v1/sso/platform-providers", nil)
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var listResp3 map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&listResp3))
	platformProviders3 := listResp3["providers"].([]any)
	for _, p := range platformProviders3 {
		pm := p.(map[string]any)
		if pm["id"] == platformProviderID {
			assert.False(t, pm["enabled_for_org"].(bool), "should be disabled again")
		}
	}
}

// TestOrgAdminSSOSettings tests GET/PUT of sso_password_login.
func TestOrgAdminSSOSettings(t *testing.T) {
	s := setupTestServer(t)
	_, token := registerOrgAndGetAdminToken(t, s)

	authHeader := func(r *http.Request) *http.Request {
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}

	// GET settings → default should be true
	req := httptest.NewRequest("GET", "/api/v1/sso/settings", nil)
	req = authHeader(req)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "get settings: %s", rec.Body.String())

	var settings map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&settings))
	assert.True(t, settings["sso_password_login"].(bool), "default should be true")

	// PUT settings → set to false
	body, _ := json.Marshal(map[string]any{"sso_password_login": false})
	req = httptest.NewRequest("PUT", "/api/v1/sso/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "put settings: %s", rec.Body.String())

	var updatedSettings map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&updatedSettings))
	assert.False(t, updatedSettings["sso_password_login"].(bool))

	// GET settings again → should be false
	req = httptest.NewRequest("GET", "/api/v1/sso/settings", nil)
	req = authHeader(req)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var settings2 map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&settings2))
	assert.False(t, settings2["sso_password_login"].(bool), "should still be false")
}
