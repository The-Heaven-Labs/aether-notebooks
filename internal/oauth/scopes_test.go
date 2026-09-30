package oauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseScopes(t *testing.T) {
	require.Nil(t, ParseScopes(""))
	require.Equal(t, []string{"mcp:query"}, ParseScopes("mcp:query"))
	require.Equal(t, []string{"mcp:query", "mcp:read"}, ParseScopes("mcp:query mcp:read"))
	require.Equal(t, []string{"mcp:query"}, ParseScopes("mcp:query  mcp:query"))
}

func TestNormalizeScopes(t *testing.T) {
	require.Equal(t, []string{"mcp:query", "mcp:read"},
		NormalizeScopes([]string{"mcp:query", "bogus:scope", "mcp:read"}))
	require.Empty(t, NormalizeScopes([]string{"bogus:scope"}))
	require.Empty(t, NormalizeScopes(nil))
}

func TestToolsForScopes(t *testing.T) {
	tools := ToolsForScopes([]string{"mcp:query"})
	require.Contains(t, tools, "execute_sql")
	require.Contains(t, tools, "explore_schema")
	require.Contains(t, tools, "list_connectors")
	require.NotContains(t, tools, "create_notebook")

	all := ToolsForScopes([]string{"mcp:query", "mcp:read", "mcp:write"})
	require.Contains(t, all, "create_notebook")
	require.Contains(t, all, "run_cell")
	require.Contains(t, all, "execute_sql")

	require.Empty(t, ToolsForScopes(nil))
}

func TestValidateRedirectURI(t *testing.T) {
	valid := []string{
		"http://localhost:33211/callback",
		"http://127.0.0.1:8080/oauth/callback",
		"https://client.example.com/oauth/callback",
	}
	for _, uri := range valid {
		require.Truef(t, ValidateRedirectURI(uri), "expected valid: %s", uri)
	}
	invalid := []string{
		"",
		"not a uri",
		"http://evil.example.com/callback",
		"https://client.example.com/callback#fragment",
		"ftp://client.example.com/callback",
	}
	for _, uri := range invalid {
		require.Falsef(t, ValidateRedirectURI(uri), "expected invalid: %s", uri)
	}
}
