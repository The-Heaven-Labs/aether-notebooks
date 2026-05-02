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
)

// createSSOProviderForOrg creates an org-scoped SSO provider with the given allowed_domains
// and returns the provider ID.
func createSSOProviderForOrg(t *testing.T, srv http.Handler, token string, allowedDomains []string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name":            fmt.Sprintf("Probe Provider %d", time.Now().UnixNano()),
		"client_id":       "probe-client-id",
		"client_secret":   "probe-secret",
		"discovery_url":   "https://probe.example.com/.well-known/openid-configuration",
		"allowed_domains": allowedDomains,
		"enabled":         true,
	})
	req := httptest.NewRequest("POST", "/api/v1/sso/providers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create provider: %s", rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	return resp["id"].(string)
}

func TestSSOProbe_DomainMatch(t *testing.T) {
	s := setupTestServer(t)
	_, token := registerOrgAndGetAdminToken(t, s)
	createSSOProviderForOrg(t, s, token, []string{"example.com", "other.com"})

	req := httptest.NewRequest("GET", "/api/v1/auth/sso-providers?email=user@example.com", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var providers []map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&providers))
	require.NotEmpty(t, providers, "expected at least one provider for example.com")

	// Verify required fields are present and sensitive fields are absent.
	for _, p := range providers {
		assert.NotEmpty(t, p["id"], "provider must have id")
		assert.NotEmpty(t, p["name"], "provider must have name")
		assert.NotEmpty(t, p["provider_type"], "provider must have provider_type")
		assert.Nil(t, p["client_secret"], "client_secret must not be returned")
		assert.Nil(t, p["discovery_url"], "discovery_url must not be returned")
		assert.Nil(t, p["org_id"], "org_id must not be returned")
	}
}

func TestSSOProbe_UnknownDomain(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/auth/sso-providers?email=user@unknown-domain-xyz-123.tld", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var providers []any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&providers))
	assert.Empty(t, providers, "expected empty array for unknown domain")
}

func TestSSOProbe_NoEmailParam(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/auth/sso-providers", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var providers []any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&providers))
	assert.Empty(t, providers, "expected empty array when no email param")
}

func TestSSOProbe_RateLimit(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	// Use a distinct IP via X-Forwarded-For to isolate this test.
	testIP := fmt.Sprintf("10.0.%d.%d", time.Now().UnixNano()%255, (time.Now().UnixNano()/255)%255)
	rateLimitKey := fmt.Sprintf("ratelimit:sso-probe:%s", testIP)

	// Clean up the key before and after the test.
	s.Cache.Client().Del(ctx, rateLimitKey)
	t.Cleanup(func() { s.Cache.Client().Del(ctx, rateLimitKey) })

	makeProbeRequest := func() int {
		req := httptest.NewRequest("GET", "/api/v1/auth/sso-providers?email=user@example.com", nil)
		req.Header.Set("X-Forwarded-For", testIP)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}

	// First 20 requests should succeed.
	for i := 0; i < 20; i++ {
		code := makeProbeRequest()
		assert.Equal(t, http.StatusOK, code, "request %d should succeed", i+1)
	}

	// 21st request should be rate limited.
	code := makeProbeRequest()
	assert.Equal(t, http.StatusTooManyRequests, code, "21st request should be rate limited")
}
