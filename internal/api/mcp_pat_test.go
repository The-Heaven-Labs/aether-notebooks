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

	"github.com/stretchr/testify/require"
)

// A PAT created after login drives the full notebook workflow over MCP.
func TestMCPEndToEndWithPAT(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-e2e-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "MCP E2E Org")
	connID := createConnector(t, srv, jwt)

	code, tokResp := doCreateToken(t, srv, jwt, "opencode", "")
	require.Equal(t, http.StatusCreated, code, "create PAT: %v", tokResp)
	pat := tokResp["token"].(string)

	// create_notebook
	code, resp, _ := doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "create_notebook", "arguments": map[string]any{"title": "MCP E2E"},
	})
	require.Equal(t, http.StatusOK, code)
	var nb map[string]any
	require.NoError(t, json.Unmarshal([]byte(mcpResultText(t, resp)), &nb))
	nbID, _ := nb["notebook_id"].(string)
	require.NotEmpty(t, nbID, "create_notebook result: %v", nb)

	// create_cell
	code, resp, _ = doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "create_cell",
		"arguments": map[string]any{
			"notebook_id": nbID, "type": "code", "language": "sql",
			"title": "One", "description": "test cell",
			"source": "SELECT 1 AS x", "connector_id": connID,
		},
	})
	require.Equal(t, http.StatusOK, code)
	var cell map[string]any
	require.NoError(t, json.Unmarshal([]byte(mcpResultText(t, resp)), &cell))
	cellID, _ := cell["cell_id"].(string)
	require.NotEmpty(t, cellID, "create_cell result: %v", cell)

	// run_cell
	code, resp, _ = doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "run_cell", "arguments": map[string]any{"cell_id": cellID},
	})
	require.Equal(t, http.StatusOK, code)
	text := mcpResultText(t, resp)
	require.Contains(t, text, `"status":"completed"`)
	require.Contains(t, text, `"column_names":["x"]`)

	// list_cells sees the created cell through the same PAT
	code, resp, _ = doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "list_cells", "arguments": map[string]any{"notebook_id": nbID},
	})
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, mcpResultText(t, resp), cellID)
}

func TestMCPExpiredPATRejected(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-expired-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "MCP Expired Org")

	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	code, tokResp := doCreateToken(t, srv, jwt, "expired-mcp", past)
	require.Equal(t, http.StatusCreated, code, "create expired PAT: %v", tokResp)
	pat := tokResp["token"].(string)

	code, _, _ = doMCPRequest(t, srv, pat, "tools/list", nil)
	require.Equal(t, http.StatusUnauthorized, code, "expired PAT must be rejected")
}

// A PAT scoped to org A used against org B's subdomain is rejected by the
// existing subdomain/token check, before any tool logic runs.
func TestMCPPATWrongOrgSubdomainRejected(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-wrong-org-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "MCP Org A")

	code, tokResp := doCreateToken(t, srv, jwt, "wrong-org", "")
	require.Equal(t, http.StatusCreated, code, "create PAT: %v", tokResp)
	pat := tokResp["token"].(string)

	slug := fmt.Sprintf("mcp-other-%d", time.Now().UnixNano())
	_, err := srv.DB().Pool.Exec(context.Background(),
		`INSERT INTO orgs (name, slug) VALUES ($1, $2)`, slug, slug)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	req := httptest.NewRequest("POST", "/api/v1/mcp", bytes.NewReader(body))
	req.Host = slug + ".aether.test"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+pat)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "PAT must not cross orgs: %s", rec.Body.String())
}
