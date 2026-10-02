package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A TLS-terminating proxy that forwards X-Forwarded-Host but not
// X-Forwarded-Proto must not downgrade an https deployment to http:// URLs:
// an http issuer/audience otherwise breaks discovery and token validation.
func TestOAuthHTTPSProxyWithoutForwardedProto(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	srv.SetPublicURL("https://aether.example.com")
	const resource = "https://aether.example.com/api/v1/mcp"

	proxyRequest := func(method, path, body, bearer string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(method, path, strings.NewReader(body))
		req.Host = "aether-api.aether.svc.cluster.local"
		req.Header.Set("X-Forwarded-Host", "aether.example.com")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	t.Run("authorization server metadata advertises https", func(t *testing.T) {
		rec := proxyRequest("GET", "/.well-known/oauth-authorization-server", "", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var meta map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meta))
		require.Equal(t, "https://aether.example.com", meta["issuer"])
		require.Equal(t, "https://aether.example.com/oauth/authorize", meta["authorization_endpoint"])
		require.Equal(t, "https://aether.example.com/oauth/token", meta["token_endpoint"])
	})

	t.Run("protected resource metadata advertises https", func(t *testing.T) {
		rec := proxyRequest("GET", "/.well-known/oauth-protected-resource", "", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var prm map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prm))
		require.Equal(t, resource, prm["resource"])
		require.Equal(t, []any{"https://aether.example.com"}, prm["authorization_servers"])
	})

	t.Run("MCP challenge advertises https", func(t *testing.T) {
		rec := proxyRequest("POST", "/api/v1/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "")
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Header().Get("WWW-Authenticate"),
			`resource_metadata="https://aether.example.com/.well-known/oauth-protected-resource"`)
	})

	// The break the user hits: the browser consent flow and the client's
	// https// resource must agree, and the token minted for it must pass the
	// MCP endpoint's audience check on the next request.
	t.Run("consent, token and audience agree on the https resource", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{
			"client_id": clientID, "redirect_uri": oauthRedirect,
			"scope": "mcp:query", "resource": resource,
			"state": "st-https", "code_challenge": oauthChallenge(t),
			"code_challenge_method": "S256", "approve": true,
		})
		rec := proxyRequest("POST", "/api/v1/oauth/consent/decision", string(payload), jwt)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var decision map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &decision))
		redirect, _ := decision["redirect"].(string)
		u, err := url.Parse(redirect)
		require.NoError(t, err)
		code := u.Query().Get("code")
		require.NotEmpty(t, code)

		form := url.Values{
			"grant_type": {"authorization_code"}, "code": {code},
			"redirect_uri": {oauthRedirect}, "client_id": {clientID},
			"code_verifier": {oauthVerifier}, "resource": {resource},
		}
		req, _ := http.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
		req.Host = "aether-api.aether.svc.cluster.local"
		req.Header.Set("X-Forwarded-Host", "aether.example.com")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var tok map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tok))

		rec = proxyRequest("POST", "/api/v1/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, tok["access_token"].(string))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}

// The v0.59.0 regression: the edge terminates TLS but the inner hop forwards
// plaintext, so X-Forwarded-Proto reads "http" while Host is preserved. The
// configured AETHER_PUBLIC_URL scheme is authoritative for its own domain, so
// discovery, the 401 challenge, consent, and the issued token audience must
// all agree on https.
func TestOAuthHTTPSProxyWithPlaintextInnerHop(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	srv.SetPublicURL("https://aether.example.com")
	const resource = "https://aether.example.com/api/v1/mcp"

	proxyRequest := func(method, path, contentType, body, bearer string) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(method, path, strings.NewReader(body))
		req.Host = "aether-api.aether.svc.cluster.local"
		req.Header.Set("X-Forwarded-Host", "aether.example.com")
		req.Header.Set("X-Forwarded-Proto", "http")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	t.Run("authorization server metadata advertises https", func(t *testing.T) {
		rec := proxyRequest("GET", "/.well-known/oauth-authorization-server", "", "", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var meta map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meta))
		require.Equal(t, "https://aether.example.com", meta["issuer"])
		require.Equal(t, "https://aether.example.com/oauth/authorize", meta["authorization_endpoint"])
		require.Equal(t, "https://aether.example.com/oauth/token", meta["token_endpoint"])
	})

	t.Run("protected resource metadata advertises https", func(t *testing.T) {
		rec := proxyRequest("GET", "/.well-known/oauth-protected-resource", "", "", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var prm map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prm))
		require.Equal(t, resource, prm["resource"])
		require.Equal(t, []any{"https://aether.example.com"}, prm["authorization_servers"])
	})

	t.Run("MCP challenge advertises https", func(t *testing.T) {
		rec := proxyRequest("POST", "/api/v1/mcp", "application/json",
			`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "")
		require.Equal(t, http.StatusUnauthorized, rec.Code)
		require.Contains(t, rec.Header().Get("WWW-Authenticate"),
			`resource_metadata="https://aether.example.com/.well-known/oauth-protected-resource"`)
	})

	t.Run("consent, token and audience agree on the https resource", func(t *testing.T) {
		payload, _ := json.Marshal(map[string]any{
			"client_id": clientID, "redirect_uri": oauthRedirect,
			"scope": "mcp:query", "resource": resource,
			"state": "st-plaintext-hop", "code_challenge": oauthChallenge(t),
			"code_challenge_method": "S256", "approve": true,
		})
		rec := proxyRequest("POST", "/api/v1/oauth/consent/decision", "application/json", string(payload), jwt)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var decision map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &decision))
		redirect, _ := decision["redirect"].(string)
		u, err := url.Parse(redirect)
		require.NoError(t, err)
		code := u.Query().Get("code")
		require.NotEmpty(t, code)

		form := url.Values{
			"grant_type": {"authorization_code"}, "code": {code},
			"redirect_uri": {oauthRedirect}, "client_id": {clientID},
			"code_verifier": {oauthVerifier}, "resource": {resource},
		}
		rec = proxyRequest("POST", "/oauth/token", "application/x-www-form-urlencoded", form.Encode(), "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var tok map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &tok))

		rec = proxyRequest("POST", "/api/v1/mcp", "application/json",
			`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, tok["access_token"].(string))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}
