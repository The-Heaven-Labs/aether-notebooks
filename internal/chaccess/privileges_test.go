package chaccess

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBundleByMode(t *testing.T) {
	require.Equal(t,
		[]Privilege{AccessSelect, AccessDictGet, AccessShowDictionaries},
		PrivilegeModeFull.Bundle())
	require.Equal(t, []Privilege{AccessSelect}, PrivilegeModeLegacy.Bundle())

	// Bundle returns an independent copy: callers must not be able to mutate
	// the shared canonical order.
	b := PrivilegeModeFull.Bundle()
	b[0] = "MUTATED"
	require.Equal(t, AccessSelect, PrivilegeModeFull.Bundle()[0])
}

func TestModeFromProbe(t *testing.T) {
	require.Equal(t, PrivilegeModeLegacy, modeFromProbe(false, 0))
	require.Equal(t, PrivilegeModeLegacy, modeFromProbe(true, 0))
	require.Equal(t, PrivilegeModeLegacy, modeFromProbe(true, 1))
	require.Equal(t, PrivilegeModeFull, modeFromProbe(true, 2))
}

func TestGrantPrivilegeKey(t *testing.T) {
	a := GrantPrivilege{Grant: Grant{Database: "db", Table: "d1"}, Access: AccessDictGet}
	b := GrantPrivilege{Grant: Grant{Database: "db", Table: "d1"}, Access: AccessDictGet}
	c := GrantPrivilege{Grant: Grant{Database: "db", Table: "d1"}, Access: AccessSelect}
	require.Equal(t, a, b)
	require.NotEqual(t, a, c)
}
