package chaccess

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGrantRows(t *testing.T) {
	rows := []GrantRow{
		{UserName: "aether_u_x", Database: "db", Table: "t1"},
		{UserName: "aether_u_x", Database: "db", Table: "t2"},
	}
	users := map[string]UserActual{"aether_u_x": {}}
	a := buildActualFromGrantRows(rows, nil, users, nil)
	require.Contains(t, a.Users["aether_u_x"].DirectGrants, Grant{"db", "t1"})
	require.NotContains(t, a.Users["aether_u_x"].Roles, "aether_g_y")
}

func TestParseWildcardRowsFlagged(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", Database: "db", Table: ""},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil)
	require.True(t, a.HasWildcard())
	require.Equal(t, WildcardGrant{Subject: "aether_wh_g_x", Scope: "db.*"}, a.Wildcards[0])
}

func TestParseGlobalWildcardRow(t *testing.T) {
	rows := []GrantRow{
		{UserName: "aether_wh_u_x", Database: "", Table: ""},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil)
	require.Equal(t, []WildcardGrant{{Subject: "aether_wh_u_x", Scope: "*.*"}}, a.Wildcards)
}

func TestParsePartialRevokeAndTablePrefixWildcards(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", Database: "db", Table: "revoked", IsPartialRevoke: 1},
		{RoleName: "aether_wh_g_x", Database: "db", Table: "events*"},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil)
	require.Empty(t, a.Roles["aether_wh_g_x"], "partial revokes and prefix wildcards are not table grants")
	require.True(t, a.HasWildcard())
	require.Equal(t, "db.events*", a.Wildcards[0].Scope)
}

func TestParseIsWildcardRowWithoutStar(t *testing.T) {
	// Servers with is_wildcard may still report the table without the "*".
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", Database: "db", Table: "events", IsWildcard: 1},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil)
	require.Equal(t, []WildcardGrant{{Subject: "aether_wh_g_x", Scope: "db.events*"}}, a.Wildcards)
}

func TestParseZeroGrantRolesAreLoaded(t *testing.T) {
	roles := map[string]map[Grant]struct{}{"aether_wh_g_empty": {}}
	a := buildActualFromGrantRows(nil, roles, nil, nil)
	require.Contains(t, a.Roles, "aether_wh_g_empty")
	require.Empty(t, a.Roles["aether_wh_g_empty"])
}

func TestParseGrantOptionIsUnexpectedDrift(t *testing.T) {
	rows := []GrantRow{
		{UserName: "aether_wh_u_x", Database: "db", Table: "t1", GrantOption: 1},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil)
	require.Contains(t, a.Users["aether_wh_u_x"].DirectGrants, Grant{"db", "t1"}, "grant is still applied")
	require.Equal(t, []string{"grant option on db.t1 for aether_wh_u_x"}, a.Unexpected)
}

func TestParseWildcardOrderingDeterministic(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_z", Database: "db2", Table: ""},
		{RoleName: "aether_wh_g_a", Database: "db2", Table: ""},
		{RoleName: "aether_wh_g_a", Database: "db1", Table: ""},
		{RoleName: "aether_wh_g_a", Database: "db2", Table: "events*"},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil)
	require.Equal(t, []WildcardGrant{
		{Subject: "aether_wh_g_a", Scope: "db1.*"},
		{Subject: "aether_wh_g_a", Scope: "db2.*"},
		{Subject: "aether_wh_g_a", Scope: "db2.events*"},
		{Subject: "aether_wh_g_z", Scope: "db2.*"},
	}, a.Wildcards)
}

func TestParseUnexpectedSortedAndDeduped(t *testing.T) {
	a := buildActualFromGrantRows(nil, nil, nil, []string{"b", "a", "b"})
	require.Equal(t, []string{"a", "b"}, a.Unexpected)
}

func TestNonSelectDriftEntries(t *testing.T) {
	got := nonSelectDrift([]NonSelectGrant{
		{AccessType: "INSERT", RoleName: "aether_wh_g_x", Database: "db", Table: "t1"},
		{AccessType: "ALTER", UserName: "aether_wh_u_x", Database: "db", Table: ""},
		{AccessType: "DROP", UserName: "aether_wh_u_x"},
		{AccessType: "INSERT", RoleName: "aether_wh_g_x", Database: "db", Table: "t1"},
		{AccessType: "DELETE", Database: "db", Table: "t1"}, // no subject: ignored
	})
	require.Equal(t, []string{
		"ALTER on db.* for aether_wh_u_x",
		"DROP on *.* for aether_wh_u_x",
		"INSERT on db.t1 for aether_wh_g_x",
	}, got)
}

func TestClassifyRoleGrantRows(t *testing.T) {
	prefix := "aether_wh_"

	memberUser, drift := classifyRoleGrantRow("aether_wh_u_x", "", "aether_wh_g_a", prefix)
	require.Equal(t, "aether_wh_u_x", memberUser)
	require.Empty(t, drift)

	memberUser, drift = classifyRoleGrantRow("other_user", "", "aether_wh_g_a", prefix)
	require.Empty(t, memberUser)
	require.Equal(t, "role aether_wh_g_a granted to other_user", drift)

	memberUser, drift = classifyRoleGrantRow("", "aether_wh_g_b", "aether_wh_g_a", prefix)
	require.Empty(t, memberUser)
	require.Equal(t, "role aether_wh_g_a granted role aether_wh_g_b", drift)
}

func TestWildcardScopeFromGrantLine(t *testing.T) {
	cases := []struct {
		line  string
		scope string
		ok    bool
	}{
		{"GRANT SELECT ON db.events* TO aether_g", "db.events*", true},
		{"GRANT SELECT ON db.* TO aether_g", "db.*", true},
		{"GRANT SELECT ON *.* TO aether_g", "*.*", true},
		{"GRANT SELECT ON `db`.`events*` TO aether_g", "db.events*", true},
		{"GRANT SELECT ON db.events* TO aether_g WITH GRANT OPTION", "db.events*", true},
		{"GRANT SELECT ON db.t TO aether_g", "", false},
		{"GRANT SELECT(col) ON db.t TO aether_g", "", false},
		{"GRANT SELECT ON db.t TO aether_g WITH GRANT OPTION", "", false},
		{"GRANT aether_g1 TO aether_g2", "", false},
		{"REVOKE SELECT ON db.t FROM aether_g", "", false},
		{"GRANT INSERT ON db.* TO aether_g", "", false},
	}
	for _, tc := range cases {
		scope, ok := wildcardScopeFromGrantLine(tc.line)
		require.Equal(t, tc.ok, ok, tc.line)
		require.Equal(t, tc.scope, scope, tc.line)
	}
}
