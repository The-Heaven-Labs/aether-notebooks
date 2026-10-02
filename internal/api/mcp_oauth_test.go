package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/api"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/executor"
	"github.com/the-heaven-labs/aether/internal/oauth"
)

// Pinned resource/host: the tests derive the canonical resource URI from the
// request Host, so every request in this file pins req.Host = "example.com"
// and uses resource = "http://example.com/api/v1/mcp".
const (
	oauthHost     = "example.com"
	oauthResource = "http://example.com/api/v1/mcp"
	oauthRedirect = "http://localhost:33211/callback"
	// RFC 7636 requires 43–128 characters.
	oauthVerifier = "test-verifier-0123456789abcdef-0123456789abcdef"
)

func oauthChallenge(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(oauthVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func doJSON(t *testing.T, srv http.Handler, method, path, jwtToken, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(method, path, strings.NewReader(body))
	req.Host = oauthHost
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+jwtToken)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func oauthRegister(t *testing.T, srv http.Handler) string {
	t.Helper()
	rec := doJSON(t, srv, "POST", "/oauth/register", "",
		`{"client_name":"opencode","redirect_uris":["`+oauthRedirect+`"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out["client_id"].(string)
}

// oauthConsentApprove drives the SPA-mediated consent decision API directly
// (the browser step is exercised manually; see docs/mcp-oauth.md).
func oauthConsentApprove(t *testing.T, srv http.Handler, jwtToken, clientID string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": oauthResource,
		"state": "st-123", "code_challenge": oauthChallenge(t),
		"code_challenge_method": "S256", "approve": true,
	})
	rec := doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwtToken, string(payload))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	loc, _ := out["redirect"].(string)
	u, err := url.Parse(loc)
	require.NoError(t, err)
	require.Equal(t, "st-123", u.Query().Get("state"))
	require.Equal(t, oauthRedirect, u.Scheme+"://"+u.Host+u.Path)
	return u.Query().Get("code")
}

func oauthToken(t *testing.T, srv http.Handler, clientID, code string) map[string]any {
	t.Helper()
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {oauthRedirect}, "client_id": {clientID},
		"code_verifier": {oauthVerifier}, "resource": {oauthResource},
	}
	req, _ := http.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Host = oauthHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func oauthRefresh(t *testing.T, srv http.Handler, clientID, refreshToken string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {clientID}}
	req, _ := http.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Host = oauthHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// setupOAuthServer registers a user + connector and returns (srv, jwt, clientID, connectorID).
// Requires the `api` import: `github.com/the-heaven-labs/aether/internal/api` (same as testhelpers_test.go).
func setupOAuthServer(t *testing.T) (*api.Server, string, string, string) {
	t.Helper()
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	// OAuth endpoints share one Redis counter per IP+path across tests; raise
	// the thresholds so assertions exercise behavior, not rate limiting.
	t.Setenv("AETHER_RATE_LIMIT_OAUTH_REGISTER", "500")
	t.Setenv("AETHER_RATE_LIMIT_OAUTH_TOKEN", "500")
	// Authorize/consent share the login tier. Tests intentionally override
	// AETHER_RATE_LIMIT_LOGIN to pin throttling, so only default it when the
	// caller left it unset.
	if os.Getenv("AETHER_RATE_LIMIT_LOGIN") == "" {
		t.Setenv("AETHER_RATE_LIMIT_LOGIN", "500")
	}
	srv := setupTestServer(t)
	srv.SetMCPOAuthEnabled(true)
	jwt := registerAndGetToken(t, srv, fmt.Sprintf("mcp-oauth-%d@example.com", time.Now().UnixNano()), "OAuth Org")
	connID := createConnector(t, srv, jwt)
	clientID := oauthRegister(t, srv)
	return srv, jwt, clientID, connID
}

func TestOAuthDiscoveryEndpoints(t *testing.T) {
	srv, _, _, _ := setupOAuthServer(t)

	rec := doJSON(t, srv, "GET", "/.well-known/oauth-protected-resource", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var prm map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prm))
	require.Equal(t, oauthResource, prm["resource"])
	require.Contains(t, prm["authorization_servers"], "http://"+oauthHost)

	rec = doJSON(t, srv, "GET", "/.well-known/oauth-authorization-server", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meta))
	require.Contains(t, meta["code_challenge_methods_supported"], "S256")
	require.Contains(t, meta["grant_types_supported"], "refresh_token")
}

func TestOAuthDisabledFlag404(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t) // flag off by default
	rec := doJSON(t, srv, "GET", "/.well-known/oauth-protected-resource", "", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = doJSON(t, srv, "POST", "/oauth/register", "", `{"redirect_uris":["http://localhost:1/cb"]}`)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOAuthDCRValidation(t *testing.T) {
	srv, _, _, _ := setupOAuthServer(t)

	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"loopback http ok", `{"client_name":"x","redirect_uris":["http://localhost:1/cb"]}`, http.StatusCreated},
		{"https ok", `{"client_name":"x","redirect_uris":["https://c.example.com/cb"]}`, http.StatusCreated},
		{"remote http rejected", `{"client_name":"x","redirect_uris":["http://evil.example.com/cb"]}`, http.StatusBadRequest},
		{"secret auth rejected", `{"client_name":"x","redirect_uris":["http://localhost:1/cb"],"token_endpoint_auth_method":"client_secret_basic"}`, http.StatusBadRequest},
		{"no redirect", `{"client_name":"x"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, srv, "POST", "/oauth/register", "", tc.body)
			require.Equal(t, tc.status, rec.Code)
		})
	}
}

func TestOAuthFullFlowExecuteSQL(t *testing.T) {
	srv, jwt, clientID, connID := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)

	access := tok["access_token"].(string)
	require.NotEmpty(t, tok["refresh_token"])
	require.Equal(t, "Bearer", tok["token_type"])
	require.InDelta(t, 900.0, tok["expires_in"].(float64), 60)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute_sql","arguments":{"connector_id":"` + connID + `","query":"SELECT 1 AS x"}}}`
	req, _ := http.NewRequest("POST", "/api/v1/mcp", strings.NewReader(body))
	req.Host = oauthHost
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var envelope struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Result.Content)
	require.Contains(t, envelope.Result.Content[0].Text, `"x"`, "expected result column x")
}

func TestOAuthWrongAudienceRejected(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)

	// Same token, different Host → audience no longer matches → 401.
	req, _ := http.NewRequest("POST", "/api/v1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Host = "other.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok["access_token"].(string))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	challenge := rec.Header().Get("WWW-Authenticate")
	require.Contains(t, challenge, `Bearer realm="aether"`)
	require.Contains(t, challenge, `resource_metadata="http://other.example.com/.well-known/oauth-protected-resource"`)
	require.Contains(t, rec.Body.String(), "token audience does not match this resource")
}

func TestOAuthRefreshRotationAndReuseRevokesFamily(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)
	refresh := tok["refresh_token"].(string)

	rec := oauthRefresh(t, srv, clientID, refresh)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rotated map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rotated))
	newRefresh := rotated["refresh_token"].(string)
	require.NotEqual(t, refresh, newRefresh)

	// Replaying the OLD token revokes the family...
	rec = oauthRefresh(t, srv, clientID, refresh)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// ...so the NEW token is dead too.
	rec = oauthRefresh(t, srv, clientID, newRefresh)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOAuthTokenRejectedOutsideMCP(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)

	rec := doJSON(t, srv, "GET", "/api/v1/notebooks", tok["access_token"].(string), "")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestOAuthTokenRejectedOnInternalEndpoints pins the relay's internal
// endpoints to session/internals JWTs: an OAuth access token (ClientID set) is
// minted for the MCP resource and must never validate there.
func TestOAuthTokenRejectedOnInternalEndpoints(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	access := oauthToken(t, srv, clientID, code)["access_token"].(string)

	rec := doJSON(t, srv, "GET", "/internal/auth/validate", access, "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doJSON(t, srv, "GET", "/internal/yjs/00000000-0000-0000-0000-000000000000", access, "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doJSON(t, srv, "PUT", "/internal/yjs/00000000-0000-0000-0000-000000000000", access, "state")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestOAuthTokenRejectedOnMCPServersAPI pins the exact-path MCP check: the
// /api/v1/mcp-servers prefix must not admit OAuth access tokens.
func TestOAuthTokenRejectedOnMCPServersAPI(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	access := oauthToken(t, srv, clientID, code)["access_token"].(string)

	rec := doJSON(t, srv, "GET", "/api/v1/mcp-servers", access, "")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// TestOAuthTokenRejectedViaQueryParam pins the WebSocket-style ?token= fallback:
// it is subject to the same MCP-only restriction as the Authorization header.
func TestOAuthTokenRejectedViaQueryParam(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	access := oauthToken(t, srv, clientID, code)["access_token"].(string)

	rec := doJSON(t, srv, "GET", "/api/v1/notebooks?token="+url.QueryEscape(access), "", "")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestOAuthConsentInfoAndDeny(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)

	rec := doJSON(t, srv, "GET",
		"/api/v1/oauth/consent/info?client_id="+clientID+"&scope=mcp:query&resource="+oauthResource,
		jwt, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var info map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &info))
	require.Equal(t, "opencode", info["client_name"])
	require.NotEmpty(t, info["org_name"])
	require.Contains(t, info["scopes"], "mcp:query")

	// Deny → redirect with error=access_denied and no code.
	payload, _ := json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": false,
	})
	rec = doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusOK, rec.Code)
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Contains(t, out["redirect"], "error=access_denied")
}

func TestOAuthConsentRejectsUnknownClientOrRedirect(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)

	payload, _ := json.Marshal(map[string]any{
		"client_id": "mcp_missing", "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": true,
	})
	rec := doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Known client, unregistered redirect URI.
	payload, _ = json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": "https://evil.example.com/cb",
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": true,
	})
	rec = doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Known client, registered URI with a suffix — exact match required.
	payload, _ = json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect + "/x",
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": true,
	})
	rec = doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Known client, registered URI, resource pinned to a different server.
	payload, _ = json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": "https://evil.example.com/api/v1/mcp", "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": true,
	})
	rec = doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOAuthAuthorizeInvalidParamsRejected(t *testing.T) {
	srv, _, clientID, _ := setupOAuthServer(t)

	// Unknown client → plain 400 (never a redirect).
	rec := doJSON(t, srv, "GET", "/oauth/authorize?client_id=mcp_missing&redirect_uri="+oauthRedirect+"&code_challenge=abc&code_challenge_method=S256", "", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// plain challenge method rejected.
	u := "/oauth/authorize?client_id=" + clientID + "&redirect_uri=" + oauthRedirect +
		"&code_challenge=abc&code_challenge_method=plain&response_type=code"
	rec = doJSON(t, srv, "GET", u, "", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// response_type other than code rejected before any lookup.
	u = "/oauth/authorize?client_id=" + clientID + "&redirect_uri=" + oauthRedirect +
		"&code_challenge=abc&code_challenge_method=S256&response_type=token"
	rec = doJSON(t, srv, "GET", u, "", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "unsupported response_type")
}

// Unauthenticated OAuth endpoints cap request bodies at 64 KiB.
func TestOAuthOversizedBodiesRejected(t *testing.T) {
	srv, _, _, _ := setupOAuthServer(t)
	big := strings.Repeat("a", 65<<10)

	rec := doJSON(t, srv, "POST", "/oauth/register", "",
		`{"client_name":"`+big+`","redirect_uris":["http://localhost:1/cb"]}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var out map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, "invalid_client_metadata", out["error"])

	form := url.Values{"grant_type": {"authorization_code"}, "code_verifier": {big}}
	rec = postTokenForm(t, srv, form)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, "invalid_request", out["error"])
}

func TestOAuthScopeFiltering(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)
	access := tok["access_token"].(string)

	// tools/list only shows query tools.
	rec := doJSON(t, srv, "POST", "/api/v1/mcp", access,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "execute_sql")
	require.NotContains(t, rec.Body.String(), "create_notebook")

	// tools/call with a write tool is rejected even if guessed by name.
	rec = doJSON(t, srv, "POST", "/api/v1/mcp", access,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_notebook","arguments":{"title":"nope"}}}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "not available")

	// PATs keep the full allowlist (scope filtering must not leak into them).
	patCode, patResp := doCreateToken(t, srv, jwt, "full-pat", "")
	require.Equal(t, http.StatusCreated, patCode, patResp)
	rec = doJSON(t, srv, "POST", "/api/v1/mcp", patResp["token"].(string),
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "create_notebook")
}

func TestMCPToolsForTokenFailClosed(t *testing.T) {
	require.Nil(t, api.MCPToolsForTokenForTest(nil))
	require.Nil(t, api.MCPToolsForTokenForTest(&auth.Claims{}))

	empty := api.MCPToolsForTokenForTest(&auth.Claims{ClientID: "mcp_x", Scope: ""})
	require.NotNil(t, empty, "OAuth tokens must never fall back to the full allowlist")
	require.Empty(t, empty)

	bogus := api.MCPToolsForTokenForTest(&auth.Claims{ClientID: "mcp_x", Scope: "bogus:scope"})
	require.Empty(t, bogus)

	all := api.MCPToolsForTokenForTest(&auth.Claims{ClientID: "mcp_x", Scope: "mcp:query mcp:read mcp:write"})
	require.Contains(t, all, "execute_sql")
	require.Contains(t, all, "create_notebook")
}

func TestMCPUnauthenticatedChallengeAdvertisesResourceMetadata(t *testing.T) {
	srv, _, _, _ := setupOAuthServer(t)

	rec := doJSON(t, srv, "POST", "/api/v1/mcp", "", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	challenge := rec.Header().Get("WWW-Authenticate")
	require.Contains(t, challenge, `Bearer realm="aether"`)
	require.Contains(t, challenge, `resource_metadata="http://example.com/.well-known/oauth-protected-resource"`)
	require.Contains(t, rec.Body.String(), "missing or invalid authorization")
}

func TestMCPInvalidTokenChallengeAdvertisesResourceMetadata(t *testing.T) {
	srv, _, _, _ := setupOAuthServer(t)

	rec := doJSON(t, srv, "POST", "/api/v1/mcp", "bogus", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	challenge := rec.Header().Get("WWW-Authenticate")
	require.Contains(t, challenge, `Bearer realm="aether"`)
	require.Contains(t, challenge, `resource_metadata="http://example.com/.well-known/oauth-protected-resource"`)
	require.Contains(t, rec.Body.String(), "invalid token")
}

// With the OAuth authorization server disabled its well-known endpoints return
// 404, so the MCP challenge must not advertise resource_metadata: clients that
// follow it would hit a dead end.
func TestMCPUnauthenticatedChallengePlainWhenOAuthDisabled(t *testing.T) {
	srv := setupTestServer(t) // flag off by default

	rec := doJSON(t, srv, "POST", "/api/v1/mcp", "", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	challenge := rec.Header().Get("WWW-Authenticate")
	require.Contains(t, challenge, `Bearer realm="aether"`)
	require.NotContains(t, challenge, "resource_metadata")
}

func TestNonMCPUnauthorizedChallengeOmitsResourceMetadata(t *testing.T) {
	srv := setupTestServer(t)

	rec := doJSON(t, srv, "GET", "/api/v1/notebooks", "", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	challenge := rec.Header().Get("WWW-Authenticate")
	require.Contains(t, challenge, `Bearer realm="aether"`)
	require.NotContains(t, challenge, "resource_metadata")
}

// Every allowlisted tool must be reachable through some grantable scope and no
// scope may unlock a tool outside the allowlist.
func TestOAuthScopesCoverAllowlistExactly(t *testing.T) {
	srv := setupTestServer(t)
	allowlist := srv.MCPToolAllowlistForTest()
	reachable := oauth.ToolsForScopes(oauth.AllScopes)
	for _, name := range allowlist {
		_, ok := reachable[name]
		require.Truef(t, ok, "allowlist tool %q is not reachable through any scope", name)
	}
	for name := range reachable {
		require.Contains(t, allowlist, name, "scope unlocks tool %q outside the MCP allowlist", name)
	}
}

// A resource mismatch at consent must say what the server expected; without it
// the rejection is not actionable (e.g. an http/https scheme mismatch).
func TestOAuthConsentResourceMismatchEchoesExpected(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)

	payload, _ := json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": "https://other.example.com/api/v1/mcp",
		"state": "st-mismatch", "code_challenge": oauthChallenge(t),
		"code_challenge_method": "S256", "approve": true,
	})
	rec := doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "resource does not match this MCP server")
	require.Contains(t, rec.Body.String(), oauthResource, "the expected canonical resource must be echoed")
}

// A rejected PAT at the MCP endpoint must advertise the RFC 9728 challenge
// just like session JWTs, or harnesses cannot discover OAuth after a stale PAT.
func TestOAuthPATInvalidTokenGetsMCPChallenge(t *testing.T) {
	srv, _, _, _ := setupOAuthServer(t)

	rec := doJSON(t, srv, "POST", "/api/v1/mcp", "aether_tok_bogus",
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")
	require.Contains(t, rec.Body.String(), "invalid or expired API token")
}

// OAuth access tokens are header-only: the ?token= fallback exists for
// WebSocket handshakes and must not authenticate them at the MCP endpoint.
func TestOAuthTokenViaQueryParamRejected(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	access := oauthToken(t, srv, clientID, code)["access_token"].(string)

	rec := doJSON(t, srv, "POST", "/api/v1/mcp?token="+url.QueryEscape(access), "",
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "Authorization header")
	require.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata=")
}

// Authorize shares the login rate-limit tier (design decision #7).
func TestOAuthAuthorizeRateLimited(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_LOGIN", "1")
	srv, _, clientID, _ := setupOAuthServer(t)

	// Unique IP per run: the counter lives in shared Redis keyed by IP+path.
	ip := fmt.Sprintf("203.0.%d.%d", rand.IntN(256), rand.IntN(256))
	authorizeURL := "/oauth/authorize?client_id=" + clientID +
		"&redirect_uri=" + url.QueryEscape(oauthRedirect) +
		"&code_challenge=" + oauthChallenge(t) +
		"&code_challenge_method=S256&response_type=code"

	call := func() int {
		req := httptest.NewRequest("GET", authorizeURL, nil)
		req.Host = oauthHost
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}
	require.NotEqual(t, http.StatusTooManyRequests, call(), "first request must not be throttled")
	require.Equal(t, http.StatusTooManyRequests, call(), "second request must be throttled")
}

// The consent APIs share the login rate-limit tier with authorize (design
// decision #7).
func TestOAuthConsentRateLimited(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_LOGIN", "1")
	srv, jwt, clientID, _ := setupOAuthServer(t)

	// Unique IP per run: the counter lives in shared Redis keyed by IP+path.
	ip := fmt.Sprintf("203.0.%d.%d", rand.IntN(256), rand.IntN(256))
	infoURL := "/api/v1/oauth/consent/info?client_id=" + clientID +
		"&scope=mcp:query&resource=" + oauthResource

	call := func() int {
		req := httptest.NewRequest("GET", infoURL, nil)
		req.Host = oauthHost
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("X-Forwarded-For", ip)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}
	require.Equal(t, http.StatusOK, call(), "first request must not be throttled")
	require.Equal(t, http.StatusTooManyRequests, call(), "second request must be throttled")
}

func postTokenForm(t *testing.T, srv http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Host = oauthHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestOAuthWrongCodeVerifierRejected(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)

	// In-range length so the hash comparison, not the RFC 7636 length check,
	// is what rejects it.
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {oauthRedirect}, "client_id": {clientID},
		"code_verifier": {strings.Repeat("b", 43)}, "resource": {oauthResource},
	}
	rec := postTokenForm(t, srv, form)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	var out map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, "invalid_grant", out["error"])
}

func TestOAuthCodeClientMismatchRejected(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)

	otherClient := oauthRegister(t, srv)
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {oauthRedirect}, "client_id": {otherClient},
		"code_verifier": {oauthVerifier}, "resource": {oauthResource},
	}
	rec := postTokenForm(t, srv, form)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	var out map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t, "invalid_grant", out["error"])
}

// A consent decision is bound to the caller's org. With a subdomain host the
// subdomain org must equal the JWT org (enforced by AuthMiddleware); without a
// subdomain the JWT org wins. Either way no code may be bound to another org.
func TestOAuthConsentOrgMismatchRejected(t *testing.T) {
	srv, jwtA, clientID, _ := setupOAuthServer(t)
	claimsA, err := testJWT.Validate(jwtA)
	require.NoError(t, err)

	jwtB := registerAndGetToken(t, srv,
		fmt.Sprintf("mcp-oauth-orgb-%d@example.com", time.Now().UnixNano()), "OAuth Org B")
	claimsB, err := testJWT.Validate(jwtB)
	require.NoError(t, err)
	require.NotEqual(t, claimsA.OrgID, claimsB.OrgID)

	var slugA string
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(),
		`SELECT slug FROM orgs WHERE id = $1`, claimsA.OrgID).Scan(&slugA))

	payload, _ := json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": true,
	})

	// User B's token against org A's subdomain is rejected before any code is
	// minted: the subdomain org and the JWT org disagree.
	req := httptest.NewRequest("POST", "/api/v1/oauth/consent/decision",
		strings.NewReader(string(payload)))
	req.Host = slugA + ".example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwtB)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "code")

	// Without a subdomain the JWT org wins: consent succeeds, but the access
	// token must carry org B, never org A.
	rec = doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwtB, string(payload))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	u, err := url.Parse(out["redirect"].(string))
	require.NoError(t, err)
	code := u.Query().Get("code")
	require.NotEmpty(t, code)

	access := oauthToken(t, srv, clientID, code)["access_token"].(string)
	accessClaims, err := testJWT.Validate(access)
	require.NoError(t, err)
	require.Equal(t, claimsB.OrgID, accessClaims.OrgID)
	require.NotEqual(t, claimsA.OrgID, accessClaims.OrgID)
}

// Admin mode (the org-admin ACL bypass) is a first-party session feature.
// OAuth access tokens must not gain it even when the header is present.
func TestOAuthAdminModeSuppressedForOAuthTokens(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)

	// The probe replaces execute_sql so it is reachable with mcp:query and
	// reports the admin-mode flag its tool context carries.
	probe := &agent.ToolDef{Timeout: time.Second}
	probe.Function.Name = "execute_sql"
	probe.Function.Parameters = `{"type":"object","properties":{}}`
	probe.Handler = func(_ json.RawMessage, tc *agent.ToolContext) (any, error) {
		return map[string]any{"admin_mode": executor.AdminModeFromContext(tc.Context)}, nil
	}
	srv.RegisterToolForTest(probe)

	call := func(token string) map[string]any {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute_sql","arguments":{}}}`
		req, _ := http.NewRequest("POST", "/api/v1/mcp", strings.NewReader(body))
		req.Host = oauthHost
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-AETHER-Admin-Mode", "true")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var envelope struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
		require.NotEmpty(t, envelope.Result.Content, rec.Body.String())
		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(envelope.Result.Content[0].Text), &result))
		return result
	}

	// Session JWT: the header enables admin mode.
	require.Equal(t, true, call(jwt)["admin_mode"])

	// OAuth token: the same header is ignored.
	code := oauthConsentApprove(t, srv, jwt, clientID)
	access := oauthToken(t, srv, clientID, code)["access_token"].(string)
	require.Equal(t, false, call(access)["admin_mode"])
}
