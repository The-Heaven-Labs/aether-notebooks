package chaccess

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func g(db, tbl string) Grant { return Grant{Database: db, Table: tbl} }

func grantSet(gs ...Grant) map[Grant]struct{} {
	m := make(map[Grant]struct{}, len(gs))
	for _, x := range gs {
		m[x] = struct{}{}
	}
	return m
}

func stmtIdx(t *testing.T, stmts []string, want string) int {
	t.Helper()
	for i, s := range stmts {
		if s == want {
			return i
		}
	}
	t.Fatalf("statement %q not found in %#v", want, stmts)
	return -1
}

func TestStatementsCreateRoleUserAndGrants(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Grants: grantSet(g("db1", "t1"), g("db1", "t2"))},
		},
		Users: map[string]UserState{
			"aether_u_bbb": {
				Password: "Ae1_x",
				Roles:    []string{"aether_g_aaa"},
			},
		},
	}
	stmts, skipped := Statements(d, ActualState{})
	require.Empty(t, skipped)
	require.Equal(t, []string{
		"CREATE ROLE IF NOT EXISTS `aether_g_aaa`",
		"GRANT SELECT ON `db1`.`t1` TO `aether_g_aaa`",
		"GRANT SELECT ON `db1`.`t2` TO `aether_g_aaa`",
		"CREATE USER IF NOT EXISTS `aether_u_bbb` IDENTIFIED WITH sha256_password BY 'Ae1_x' GRANTEES NONE",
		"GRANT `aether_g_aaa` TO `aether_u_bbb`",
		"SET DEFAULT ROLE ALL TO `aether_u_bbb`",
	}, stmts)
}

func TestStatementsRevokeExtrasAndDropOrphans(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Grants: grantSet(g("db1", "t1"))},
		},
		Users: map[string]UserState{
			"aether_u_bbb": {Password: "Ae1_x", Roles: []string{"aether_g_aaa"}},
		},
	}
	a := ActualState{
		Roles: map[string]map[Grant]struct{}{
			"aether_g_aaa": grantSet(g("db1", "t1"), g("db1", "old")),
			"aether_g_zzz": grantSet(g("db1", "stale")),
		},
		Users: map[string]UserActual{
			"aether_u_bbb": {Roles: map[string]struct{}{"aether_g_aaa": {}, "aether_g_old": {}}},
			"aether_u_old": {Roles: map[string]struct{}{}},
		},
	}
	stmts, skipped := Statements(d, a)
	require.Empty(t, skipped)
	joined := strings.Join(stmts, "\n")
	require.Contains(t, joined, "REVOKE SELECT ON `db1`.`old` FROM `aether_g_aaa`")
	require.Contains(t, joined, "DROP ROLE IF EXISTS `aether_g_zzz`")
	require.Contains(t, joined, "REVOKE `aether_g_old` FROM `aether_u_bbb`")
	require.Contains(t, joined, "DROP USER IF EXISTS `aether_u_old`")
}

func TestStatementsOrdersDependencies(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_new": {Grants: grantSet(g("db1", "new"))},
		},
		Users: map[string]UserState{
			"aether_u_bbb": {Password: "Ae1_newpw", Roles: []string{"aether_g_new"}},
		},
	}
	a := ActualState{
		ForcePasswordReset: true,
		Roles: map[string]map[Grant]struct{}{
			"aether_g_old": grantSet(g("db1", "old")),
		},
		Users: map[string]UserActual{
			"aether_u_bbb": {Roles: map[string]struct{}{"aether_g_old": {}}},
		},
	}
	stmts, skipped := Statements(d, a)
	require.Empty(t, skipped)

	createRole := stmtIdx(t, stmts, "CREATE ROLE IF NOT EXISTS `aether_g_new`")
	alterUser := stmtIdx(t, stmts, "ALTER USER `aether_u_bbb` IDENTIFIED WITH sha256_password BY 'Ae1_newpw'")
	grantRole := stmtIdx(t, stmts, "GRANT `aether_g_new` TO `aether_u_bbb`")
	revokeRole := stmtIdx(t, stmts, "REVOKE `aether_g_old` FROM `aether_u_bbb`")
	dropRole := stmtIdx(t, stmts, "DROP ROLE IF EXISTS `aether_g_old`")

	require.Less(t, createRole, grantRole, "role must exist before it is granted")
	require.Less(t, alterUser, grantRole, "password reset must precede membership changes")
	require.Less(t, revokeRole, dropRole, "membership must be revoked before the role is dropped")
	for i, s := range stmts {
		if strings.HasPrefix(s, "GRANT ") {
			require.Less(t, i, dropRole, "grant %q must precede the orphan role drop", s)
		}
	}
	require.Equal(t, len(stmts)-1, dropRole, "orphan role drops must be last")
}

func TestStatementsResetsPasswordOnMasterKeyChange(t *testing.T) {
	d := DesiredState{Users: map[string]UserState{
		"aether_u_bbb": {Password: "Ae1_new"},
	}}
	a := ActualState{
		ForcePasswordReset: true,
		Users: map[string]UserActual{
			"aether_u_bbb": {},
		},
	}
	stmts, _ := Statements(d, a)
	require.Contains(t, strings.Join(stmts, "\n"),
		"ALTER USER `aether_u_bbb` IDENTIFIED WITH sha256_password BY 'Ae1_new'")
}

func TestStatementsForcePasswordResetDoesNotAlterNewUser(t *testing.T) {
	d := DesiredState{Users: map[string]UserState{
		"aether_u_bbb": {Password: "Ae1_new", Roles: []string{"aether_g_aaa"}},
	}}
	stmts, _ := Statements(d, ActualState{ForcePasswordReset: true})
	joined := strings.Join(stmts, "\n")
	require.Contains(t, joined, "CREATE USER IF NOT EXISTS `aether_u_bbb`")
	require.NotContains(t, joined, "ALTER USER")
}

func TestStatementsNoOpOnEmptyState(t *testing.T) {
	stmts, skipped := Statements(DesiredState{}, ActualState{})
	require.Empty(t, stmts)
	require.Empty(t, skipped)
}

func TestStatementsNoOpWhenDesiredMatchesActual(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Grants: grantSet(g("db1", "t1"))},
		},
		Users: map[string]UserState{
			"aether_u_bbb": {Password: "Ae1_x", Roles: []string{"aether_g_aaa"}},
		},
	}
	a := ActualState{
		Roles: map[string]map[Grant]struct{}{
			"aether_g_aaa": grantSet(g("db1", "t1")),
		},
		Users: map[string]UserActual{
			"aether_u_bbb": {
				Roles:           map[string]struct{}{"aether_g_aaa": {}},
				DefaultRolesAll: true,
			},
		},
	}
	stmts, skipped := Statements(d, a)
	require.Empty(t, stmts)
	require.Empty(t, skipped)
}

func TestStatementsSetsDefaultRoleWhenActualDefaultMissing(t *testing.T) {
	d := DesiredState{Users: map[string]UserState{
		"aether_u_bbb": {Password: "Ae1_x", Roles: []string{"aether_g_aaa"}},
	}}
	a := ActualState{Users: map[string]UserActual{
		"aether_u_bbb": {
			Roles:           map[string]struct{}{"aether_g_aaa": {}},
			DefaultRolesAll: false,
		},
	}}
	stmts, skipped := Statements(d, a)
	require.Empty(t, skipped)
	require.Contains(t, stmts, "SET DEFAULT ROLE ALL TO `aether_u_bbb`")
}

func TestStatementsSkipsDefaultRoleWhenActualDefaultSet(t *testing.T) {
	d := DesiredState{Users: map[string]UserState{
		"aether_u_bbb": {Password: "Ae1_x", Roles: []string{"aether_g_aaa"}},
	}}
	a := ActualState{Users: map[string]UserActual{
		"aether_u_bbb": {
			Roles:           map[string]struct{}{"aether_g_aaa": {}},
			DefaultRolesAll: true,
		},
	}}
	stmts, skipped := Statements(d, a)
	require.Empty(t, skipped)
	require.NotContains(t, stmts, "SET DEFAULT ROLE ALL TO `aether_u_bbb`")
}

func TestActualStateWildcards(t *testing.T) {
	var a ActualState
	require.False(t, a.HasWildcard())

	a.Wildcards = []WildcardGrant{{Subject: "aether_u_bbb", Scope: "analytics.*"}}
	require.True(t, a.HasWildcard())

	stmts, skipped := Statements(DesiredState{}, a)
	require.Empty(t, stmts)
	require.Empty(t, skipped)
}

func TestStatementsSkipsUnquotableCatalogNames(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Grants: grantSet(g("db1", "ok"), g("db1", "my table"))},
		},
	}
	stmts, skipped := Statements(d, ActualState{})
	require.Contains(t, stmts, "GRANT SELECT ON `db1`.`ok` TO `aether_g_aaa`")
	require.Equal(t, []string{"db1.my table"}, skipped)
}

func TestStatementsSkipsUnquotableRevokeTarget(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Grants: grantSet(g("db1", "ok"))},
		},
	}
	a := ActualState{
		Roles: map[string]map[Grant]struct{}{
			"aether_g_aaa": grantSet(g("db1", "ok"), g("db1", "my table")),
		},
	}
	stmts, skipped := Statements(d, a)
	require.NotContains(t, strings.Join(stmts, "\n"), "REVOKE")
	require.Equal(t, []string{"db1.my table"}, skipped)
}

func TestStatementsSkipsUnquotableDirectGrant(t *testing.T) {
	d := DesiredState{Users: map[string]UserState{
		"aether_u_bbb": {Password: "Ae1_x", DirectGrants: grantSet(g("db1", "my table"))},
	}}
	stmts, skipped := Statements(d, ActualState{})
	require.Contains(t, strings.Join(stmts, "\n"), "CREATE USER IF NOT EXISTS `aether_u_bbb`")
	require.Equal(t, []string{"db1.my table"}, skipped)
}

func TestStatementsSkipsUnquotableActualNames(t *testing.T) {
	a := ActualState{
		Roles: map[string]map[Grant]struct{}{"aether_g_BAD": {}},
		Users: map[string]UserActual{"aether_u_BAD": {}},
	}
	stmts, skipped := Statements(DesiredState{}, a)
	require.Empty(t, stmts)
	require.Equal(t, []string{"aether_g_BAD", "aether_u_BAD"}, skipped)
}

func TestStatementsSkipsUnquotableActualRoleRevoke(t *testing.T) {
	d := DesiredState{Users: map[string]UserState{
		"aether_u_bbb": {Password: "Ae1_x"},
	}}
	a := ActualState{Users: map[string]UserActual{
		"aether_u_bbb": {Roles: map[string]struct{}{"aether_g_BAD": {}}},
	}}
	stmts, skipped := Statements(d, a)
	require.Empty(t, stmts)
	require.Equal(t, []string{"aether_g_BAD"}, skipped)
}

func TestStatementsDedupesSkippedNames(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Grants: grantSet(g("db1", "my table"))},
		},
		Users: map[string]UserState{
			"aether_u_bbb": {Password: "Ae1_x", DirectGrants: grantSet(g("db1", "my table"))},
		},
	}
	_, skipped := Statements(d, ActualState{})
	require.Equal(t, []string{"db1.my table"}, skipped)
}

func TestFingerprintUsesFullSHA256(t *testing.T) {
	fp := Fingerprint("derived-master-key")
	require.Len(t, fp, 64)
	require.Equal(t, fp, Fingerprint("derived-master-key"))
	require.NotEqual(t, fp, Fingerprint("other-master-key"))
}
