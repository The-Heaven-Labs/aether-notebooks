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
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
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
	require.Equal(t, http.StatusOK, code, "create_notebook: %v", resp)
	nbText := mcpResultText(t, resp)
	var nb map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(nbText), &nb), "result text: %s", nbText)
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
	require.Equal(t, http.StatusOK, code, "create_cell: %v", resp)
	cellText := mcpResultText(t, resp)
	var cell map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(cellText), &cell), "result text: %s", cellText)
	cellID, _ := cell["cell_id"].(string)
	require.NotEmpty(t, cellID, "create_cell result: %v", cell)

	// run_cell
	code, resp, _ = doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "run_cell", "arguments": map[string]any{"cell_id": cellID},
	})
	require.Equal(t, http.StatusOK, code, "run_cell: %v", resp)
	text := mcpResultText(t, resp)
	require.Contains(t, text, `"status":"completed"`)
	require.Contains(t, text, `"column_names":["x"]`)

	// list_cells sees the created cell through the same PAT
	code, resp, _ = doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "list_cells", "arguments": map[string]any{"notebook_id": nbID},
	})
	require.Equal(t, http.StatusOK, code, "list_cells: %v", resp)
	require.Contains(t, mcpResultText(t, resp), cellID)
}

func TestMCPExpiredPATRejected(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
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
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	email := fmt.Sprintf("mcp-wrong-org-%d@example.com", time.Now().UnixNano())
	jwt := registerAndGetToken(t, srv, email, "MCP Org A")

	code, tokResp := doCreateToken(t, srv, jwt, "wrong-org", "")
	require.Equal(t, http.StatusCreated, code, "create PAT: %v", tokResp)
	pat := tokResp["token"].(string)

	// Positive control: the same PAT works when no subdomain org is involved.
	code, _, _ = doMCPRequest(t, srv, pat, "tools/list", nil)
	require.Equal(t, http.StatusOK, code, "PAT must work without a subdomain")

	slug := fmt.Sprintf("mcp-other-%d", time.Now().UnixNano())
	var otherOrgID string
	err := srv.DB().Pool.QueryRow(context.Background(),
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&otherOrgID)
	require.NoError(t, err)
	t.Cleanup(func() {
		srv.DB().Pool.Exec(context.Background(), `DELETE FROM orgs WHERE id = $1`, otherOrgID)
	})

	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	req := httptest.NewRequest("POST", "/api/v1/mcp", bytes.NewReader(body))
	req.Host = slug + ".aether.test"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+pat)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, "PAT must not cross orgs: %s", rec.Body.String())
}

// A non-admin PAT with no ACL grant must be denied by the tool's permission
// check instead of silently running through the MCP channel.
func TestMCPPermissionDeniedForNonAdmin(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	db := setupTestDB(t)
	ctx := context.Background()

	adminEmail := fmt.Sprintf("mcp-acl-admin-%d@example.com", time.Now().UnixNano())
	adminJWT := registerAndGetToken(t, srv, adminEmail, "MCP ACL Org")
	nbID := createNotebook(t, srv, adminJWT, "MCP ACL NB")

	var orgID string
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT org_id FROM notebooks WHERE id = $1`, nbID).Scan(&orgID))

	viewerEmail := fmt.Sprintf("mcp-acl-viewer-%d@example.com", time.Now().UnixNano())
	var viewerUserID string
	require.NoError(t, db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified)
		 VALUES ($1, 'x', 'Viewer', false) RETURNING id`,
		viewerEmail,
	).Scan(&viewerUserID))
	_, err := db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'non-admin')`,
		orgID, viewerUserID)
	require.NoError(t, err)

	viewerJWT, err := testJWT.Issue(viewerUserID, orgID, "non-admin")
	require.NoError(t, err)
	code, tokResp := doCreateToken(t, srv, viewerJWT, "acl-viewer", "")
	require.Equal(t, http.StatusCreated, code, "create viewer PAT: %v", tokResp)
	pat := tokResp["token"].(string)

	code, resp, _ := doMCPRequest(t, srv, pat, "tools/call", map[string]any{
		"name": "list_cells", "arguments": map[string]any{"notebook_id": nbID},
	})
	require.Equal(t, http.StatusOK, code, "list_cells: %v", resp)
	result, _ := resp["result"].(map[string]any)
	require.Equal(t, true, result["isError"], "non-admin without ACL must be denied: %v", resp)
	require.Contains(t, mcpResultText(t, resp), "permission denied")
}
