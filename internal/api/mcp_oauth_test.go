package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// Pinned resource/host: the tests derive the canonical resource URI from the
// request Host, so every request in this file pins req.Host = "example.com"
// and uses resource = "http://example.com/api/v1/mcp".
const (
	oauthHost     = "example.com"
	oauthResource = "http://example.com/api/v1/mcp"
	oauthRedirect = "http://localhost:33211/callback"
	oauthVerifier = "test-verifier-0123456789abcdef"
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
	require.Contains(t, rec.Body.String(), `"x"`, "expected result column x")
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
	srv, jwt, _, _ := setupOAuthServer(t)

	payload, _ := json.Marshal(map[string]any{
		"client_id": "mcp_missing", "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": true,
	})
	rec := doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	payload, _ = json.Marshal(map[string]any{
		"client_id": "mcp_missing", "redirect_uri": "https://evil.example.com/cb",
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
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
}
