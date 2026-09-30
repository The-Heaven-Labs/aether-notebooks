package api_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/auth"
)

// The MCP tools/call path threads AETHER_MCP_SQL_TIMEOUT_MS into the agent
// ToolContext as the execute_sql ceiling; unset means zero (agent default).
func TestMCPToolContextQueryTimeoutCeiling(t *testing.T) {
	srv := setupTestServer(t)

	// Claims are built directly: a bare httptest request never runs the auth
	// middleware, so ClaimsFromContext would return nil.
	claims := &auth.Claims{UserID: testUserID, OrgID: testOrgID, Role: "editor"}
	req := httptest.NewRequest("POST", "/api/v1/mcp", nil)

	ctx := srv.MCPToolContextForTest(req, claims)
	require.Equal(t, time.Duration(0), ctx.QueryTimeoutCeiling, "unset config means agent default")

	srv.SetMCPSQLTimeout(10 * time.Minute)
	ctx = srv.MCPToolContextForTest(req, claims)
	require.Equal(t, 10*time.Minute, ctx.QueryTimeoutCeiling)
}
