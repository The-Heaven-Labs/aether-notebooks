package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
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
