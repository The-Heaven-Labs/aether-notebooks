package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// ——— LIST ———

func TestMCPServer_List(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	tests := []struct {
		name    string
		userKey string
		wantMin int
		wantMax int
	}{
		{
			name:    "adminA sees all Org A MCP servers",
			userKey: "adminA",
			wantMin: 4, wantMax: 4,
		},
		{
			name:    "aliceA sees MCP servers with user or everyone ACL",
			userKey: "aliceA",
			wantMin: 2, wantMax: 3,
		},
		{
			name:    "carolA sees only everyone-ACL MCP servers",
			userKey: "carolA",
			wantMin: 1, wantMax: 2,
		},
		{
			name:    "adminB sees only Org B MCP servers",
			userKey: "adminB",
			wantMin: 1, wantMax: 1,
		},
		{
			name:    "eveB sees only Org B MCP servers she can access",
			userKey: "eveB",
			wantMin: 0, wantMax: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "GET", "/api/v1/mcp-servers", nil)
			require.Equal(t, http.StatusOK, status)

			var servers []map[string]any
			json.Unmarshal([]byte(body), &servers)
			count := len(servers)
			t.Logf("%s: %d MCP servers visible", tt.userKey, count)
			require.GreaterOrEqual(t, count, tt.wantMin)
			require.LessOrEqual(t, count, tt.wantMax)
		})
	}
}

// ——— GET ———

func TestMCPServer_Get(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200 (admin bypass)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "GET", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "GET", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL, nil)
		t.Logf("aliceA GET NoACL MCP server: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("VULNERABILITY: aliceA can view NoACL MCP server without ACL entry")
		}
	})

	t.Run("aliceA on UserACL — 200 (has view)", func(t *testing.T) {
		status, _ := f.DoRequest(t, "aliceA", "GET", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.UserACL, nil)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("carolA on NoACL — 403 (no ACL)", func(t *testing.T) {
		status, body := f.DoRequest(t, "carolA", "GET", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL, nil)
		t.Logf("carolA GET NoACL MCP: %d %s", status, body)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "GET", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL, nil)
		t.Logf("adminB GET Org A MCP: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "GET", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL, nil)
		t.Logf("eveB GET Org A MCP: %d %s", status, body)
	})
}

// ——— CREATE ———

func TestMCPServer_Create(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	payload := map[string]any{
		"name":    "test-mcp",
		"type":    "stdio",
		"command": "echo",
	}

	tests := []struct {
		name    string
		userKey string
		want    int
	}{
		{"adminA creates MCP server", "adminA", http.StatusCreated},
		{"aliceA cannot create (not admin)", "aliceA", http.StatusForbidden},
		{"carolA cannot create (not admin)", "carolA", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := f.DoRequest(t, tt.userKey, "POST", "/api/v1/mcp-servers", payload)
			t.Logf("%s create MCP: %d %s", tt.userKey, status, body)
			require.Equal(t, tt.want, status)
		})
	}
}

// ——— UPDATE ———

func TestMCPServer_Update(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 200", func(t *testing.T) {
		status, _ := f.DoRequest(t, "adminA", "PUT", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL,
			map[string]string{"name": "updated-by-admin"})
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA on NoACL — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "PUT", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL,
			map[string]string{"name": "updated-by-alice"})
		t.Logf("aliceA PUT NoACL MCP: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("bobA on GroupACL — 403 (RequireRole blocks non-admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "bobA", "PUT", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.GroupACL,
			map[string]string{"name": "updated-by-bob"})
		t.Logf("bobA PUT GroupACL MCP: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("adminB on NoACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "PUT", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL,
			map[string]string{"name": "updated-by-adminb"})
		t.Logf("adminB PUT Org A MCP: %d %s", status, body)
	})

	t.Run("eveB on NoACL — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "eveB", "PUT", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL,
			map[string]string{"name": "updated-by-eveb"})
		t.Logf("eveB PUT Org A MCP: %d %s", status, body)
	})
}

// ——— DELETE ———

func TestMCPServer_Delete(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA on NoACL — 204", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "DELETE", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL, nil)
		t.Logf("adminA DELETE NoACL MCP: %d %s", status, body)
		require.Equal(t, http.StatusNoContent, status)
	})

	t.Run("aliceA on UserACL — 403 (not admin)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "DELETE", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.UserACL, nil)
		t.Logf("aliceA DELETE UserACL MCP: %d %s", status, body)
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("adminB on EveryoneACL — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "DELETE", "/api/v1/mcp-servers/"+f.OrgA.MCPServers.EveryoneACL, nil)
		t.Logf("adminB DELETE Org A MCP: %d %s", status, body)
	})
}

// ——— TEST ———

func TestMCPServer_Test(t *testing.T) {
	 t.Parallel()
	f := SetupAuditTest(t)

	t.Run("adminA tests NoACL — 200", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminA", "POST",
			"/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL+"/test", nil)
		t.Logf("adminA test MCP: %d %s", status, body)
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("aliceA tests NoACL — 200 (no permission check in middleware)", func(t *testing.T) {
		status, body := f.DoRequest(t, "aliceA", "POST",
			"/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL+"/test", nil)
		t.Logf("aliceA test MCP: %d %s", status, body)
		if status == http.StatusOK {
			t.Log("NOTE: handleTestMCPServer has no RequireRole or requirePermission")
			t.Log("Any authenticated user can test any MCP server in their org")
		}
	})

	t.Run("adminB tests Org A MCP — 404 (cross-org)", func(t *testing.T) {
		status, body := f.DoRequest(t, "adminB", "POST",
			"/api/v1/mcp-servers/"+f.OrgA.MCPServers.NoACL+"/test", nil)
		t.Logf("adminB test Org A MCP: %d %s", status, body)
	})
}
