package chaccess

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func g(db, tbl string) Grant { return Grant{Database: db, Table: tbl} }

func TestStatementsCreateRoleUserAndGrants(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Ident: "aether_g_aaa", Grants: grantSet(g("db1", "t1"), g("db1", "t2"))},
		},
		Users: map[string]UserState{
			"aether_u_bbb": {
				Ident: "aether_u_bbb", Password: "Ae1_x",
				Roles: []string{"aether_g_aaa"},
			},
		},
	}
	stmts, _ := Statements(d, ActualState{})
	require.Contains(t, stmts, "CREATE ROLE IF NOT EXISTS `aether_g_aaa`")
	require.Contains(t, stmts, "GRANT SELECT ON `db1`.`t1` TO `aether_g_aaa`")
	require.Contains(t, stmts, "GRANT SELECT ON `db1`.`t2` TO `aether_g_aaa`")
	require.Contains(t, stmts, "CREATE USER IF NOT EXISTS `aether_u_bbb` IDENTIFIED WITH sha256_password BY 'Ae1_x' GRANTEES NONE")
	require.Contains(t, stmts, "GRANT `aether_g_aaa` TO `aether_u_bbb`")
	require.Contains(t, stmts, "SET DEFAULT ROLE ALL TO `aether_u_bbb`")
}

func TestStatementsRevokeExtrasAndDropOrphans(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Ident: "aether_g_aaa", Grants: grantSet(g("db1", "t1"))},
		},
		Users: map[string]UserState{
			"aether_u_bbb": {Ident: "aether_u_bbb", Password: "Ae1_x", Roles: []string{"aether_g_aaa"}},
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
	stmts, _ := Statements(d, a)
	joined := strings.Join(stmts, "\n")
	require.Contains(t, joined, "REVOKE SELECT ON `db1`.`old` FROM `aether_g_aaa`")
	require.Contains(t, joined, "DROP ROLE IF EXISTS `aether_g_zzz`")
	require.Contains(t, joined, "REVOKE `aether_g_old` FROM `aether_u_bbb`")
	require.Contains(t, joined, "DROP USER IF EXISTS `aether_u_old`")
}

func TestStatementsResetsPasswordOnMasterKeyChange(t *testing.T) {
	d := DesiredState{Users: map[string]UserState{
		"aether_u_bbb": {Ident: "aether_u_bbb", Password: "Ae1_new"},
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

func TestStatementsSkipsUnquotableCatalogNames(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Ident: "aether_g_aaa", Grants: grantSet(g("db1", "ok"), g("db1", "my table"))},
		},
	}
	stmts, skipped := Statements(d, ActualState{})
	require.Contains(t, stmts, "GRANT SELECT ON `db1`.`ok` TO `aether_g_aaa`")
	require.Equal(t, []string{"db1.my table"}, skipped)
}
