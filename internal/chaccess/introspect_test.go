package chaccess

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGrantRows(t *testing.T) {
	rows := []GrantRow{
		{UserName: "aether_u_x", AccessType: "SELECT", Database: "db", Table: "t1"},
		{UserName: "aether_u_x", AccessType: "SELECT", Database: "db", Table: "t2"},
	}
	users := map[string]UserActual{"aether_u_x": {}}
	a := buildActualFromGrantRows(rows, nil, users)
	require.Contains(t, a.Users["aether_u_x"].DirectGrants, Grant{"db", "t1"})
	require.NotContains(t, a.Users["aether_u_x"].Roles, "aether_g_y")
}

func TestParseWildcardRowsFlagged(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", AccessType: "SELECT", Database: "db", Table: ""},
	}
	a := buildActualFromGrantRows(rows, nil, nil)
	require.True(t, a.HasWildcard())
	require.Equal(t, WildcardGrant{Subject: "aether_wh_g_x", Scope: "db.*"}, a.Wildcards[0])
}

func TestParsePartialRevokeAndTablePrefixWildcards(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", AccessType: "SELECT", Database: "db", Table: "revoked", IsPartialRevoke: 1},
		{RoleName: "aether_wh_g_x", AccessType: "SELECT", Database: "db", Table: "events*"},
	}
	a := buildActualFromGrantRows(rows, nil, nil)
	require.Empty(t, a.Roles["aether_wh_g_x"], "partial revokes and prefix wildcards are not table grants")
	require.True(t, a.HasWildcard())
	require.Equal(t, "db.events*", a.Wildcards[0].Scope)
}

func TestParseZeroGrantRolesAreLoaded(t *testing.T) {
	roles := map[string]map[Grant]struct{}{"aether_wh_g_empty": {}}
	a := buildActualFromGrantRows(nil, roles, nil)
	require.Contains(t, a.Roles, "aether_wh_g_empty")
	require.Empty(t, a.Roles["aether_wh_g_empty"])
}
