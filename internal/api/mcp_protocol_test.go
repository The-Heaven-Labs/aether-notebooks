package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
)

// doMCPRequest posts a JSON-RPC request to the MCP endpoint and returns the
// status, decoded body, and response headers. token is the raw bearer value.
func doMCPRequest(t *testing.T, srv http.Handler, token, method string, params any) (int, map[string]any, http.Header) {
	t.Helper()
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		payload["params"] = params
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	return rec.Code, resp, rec.Header()
}

func TestMCPUnauthenticatedReturns401WithChallenge(t *testing.T) {
	srv := setupTestServer(t)

	code, _, header := doMCPRequest(t, srv, "", "tools/list", nil)
	require.Equal(t, http.StatusUnauthorized, code)
	require.Contains(t, header.Get("WWW-Authenticate"), `Bearer realm="aether"`)
}

func TestMCPGetAndDeleteReturn405(t *testing.T) {
	srv := setupTestServer(t)

	for _, method := range []string{"GET", "DELETE"} {
		req := httptest.NewRequest(method, "/api/v1/mcp", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code, "%s should be 405", method)
		require.Equal(t, "POST", rec.Header().Get("Allow"))
	}
}

func TestMCPInitializeProtocolVersion(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	tests := []struct{ requested, expected string }{
		{"2025-06-18", "2025-06-18"},
		{"2025-11-25", "2025-11-25"},
		{"2026-07-28", "2026-07-28"},
		{"1999-01-01", "2026-07-28"},
		{"", "2026-07-28"},
	}
	for _, tt := range tests {
		params := map[string]any{}
		if tt.requested != "" {
			params["protocolVersion"] = tt.requested
		}
		code, resp, _ := doMCPRequest(t, srv, token, "initialize", params)
		require.Equal(t, http.StatusOK, code, "requested %q", tt.requested)
		result, _ := resp["result"].(map[string]any)
		require.NotNil(t, result)
		require.Equal(t, tt.expected, result["protocolVersion"], "requested %q", tt.requested)
	}
}

func TestMCPInitializedNotificationReturns202(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	code, _, _ := doMCPRequest(t, srv, token, "notifications/initialized", map[string]any{})
	require.Equal(t, http.StatusAccepted, code)
}

func TestMCPPing(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	code, resp, _ := doMCPRequest(t, srv, token, "ping", nil)
	require.Equal(t, http.StatusOK, code)
	require.NotNil(t, resp["result"], "ping must return an empty result object")
}

func TestMCPProtocolVersionHeader(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	do := func(version string) int {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
		req := httptest.NewRequest("POST", "/api/v1/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if version != "" {
			req.Header.Set("MCP-Protocol-Version", version)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusBadRequest, do("1999-01-01"))
	require.Equal(t, http.StatusOK, do("2026-07-28"))
	require.Equal(t, http.StatusOK, do(""))
}

func TestMCPPanicIsRecovered(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	probe := &agent.ToolDef{Timeout: time.Second}
	probe.Function.Name = "probe_mcp_panic"
	probe.Function.Parameters = `{"type":"object","properties":{}}`
	probe.Handler = func(_ json.RawMessage, _ *agent.ToolContext) (any, error) {
		panic("boom")
	}
	srv.RegisterToolForTest(probe)
	srv.SetMCPToolAllowedForTest("probe_mcp_panic", true)
	t.Cleanup(func() { srv.SetMCPToolAllowedForTest("probe_mcp_panic", false) })

	code, resp, _ := doMCPRequest(t, srv, token, "tools/call", map[string]any{
		"name": "probe_mcp_panic", "arguments": map[string]any{},
	})
	require.Equal(t, http.StatusInternalServerError, code)
	errObj, _ := resp["error"].(map[string]any)
	require.NotNil(t, errObj, "expected JSON-RPC error body, got %v", resp)
	require.Equal(t, float64(-32603), errObj["code"])
}

func TestMCPToolsListMatchesAllowlist(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	code, resp, _ := doMCPRequest(t, srv, token, "tools/list", nil)
	require.Equal(t, http.StatusOK, code)
	result, _ := resp["result"].(map[string]any)
	require.NotNil(t, result)
	tools, _ := result["tools"].([]any)

	got := make([]string, 0, len(tools))
	for _, raw := range tools {
		m, _ := raw.(map[string]any)
		name, _ := m["name"].(string)
		got = append(got, name)
	}
	expected := srv.MCPToolAllowlistForTest()
	require.Len(t, expected, 45)
	sort.Strings(got)
	sort.Strings(expected)
	require.Equal(t, expected, got, "tools/list must match the allowlist exactly (renames or missing definitions will show as diffs)")

	for _, excluded := range []string{
		"ask_question", "spawn_subagents", "get_subagent_results",
		"create_tasks", "update_task", "get_tasks", "update_agent",
	} {
		require.NotContains(t, got, excluded)
	}
}

func TestMCPUnlistedToolIsNotExposedOrCallable(t *testing.T) {
	srv := setupTestServer(t)
	token, err := testJWT.Issue(testUserID, testOrgID, "admin")
	require.NoError(t, err)

	probe := &agent.ToolDef{Timeout: time.Second}
	probe.Function.Name = "probe_mcp_unlisted"
	probe.Function.Parameters = `{"type":"object","properties":{}}`
	probe.Handler = func(_ json.RawMessage, _ *agent.ToolContext) (any, error) {
		return map[string]any{"ran": true}, nil
	}
	srv.RegisterToolForTest(probe)

	_, resp, _ := doMCPRequest(t, srv, token, "tools/list", nil)
	result, _ := resp["result"].(map[string]any)
	for _, raw := range result["tools"].([]any) {
		m, _ := raw.(map[string]any)
		require.NotEqual(t, "probe_mcp_unlisted", m["name"])
	}

	_, resp, _ = doMCPRequest(t, srv, token, "tools/call", map[string]any{
		"name": "probe_mcp_unlisted", "arguments": map[string]any{},
	})
	errObj, _ := resp["error"].(map[string]any)
	require.NotNil(t, errObj, "unlisted tool must be rejected, got %v", resp)
	require.Equal(t, float64(-32602), errObj["code"])
}
