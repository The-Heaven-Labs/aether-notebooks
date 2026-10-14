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

	// An unknown mode is an internal invariant violation and must fail loudly
	// instead of silently picking a bundle.
	require.Panics(t, func() { PrivilegeMode("").Bundle() })
}

func TestModeFromProbe(t *testing.T) {
	require.Equal(t, PrivilegeModeLegacy, modeFromProbe(false, false, false))
	require.Equal(t, PrivilegeModeLegacy, modeFromProbe(true, false, false))
	require.Equal(t, PrivilegeModeLegacy, modeFromProbe(true, true, false))
	require.Equal(t, PrivilegeModeLegacy, modeFromProbe(true, false, true))
	require.Equal(t, PrivilegeModeFull, modeFromProbe(true, true, true))
}

func TestGrantPrivilegeKey(t *testing.T) {
	a := GrantPrivilege{Grant: Grant{Database: "db", Table: "d1"}, Access: AccessDictGet}
	b := GrantPrivilege{Grant: Grant{Database: "db", Table: "d1"}, Access: AccessDictGet}
	c := GrantPrivilege{Grant: Grant{Database: "db", Table: "d1"}, Access: AccessSelect}
	require.Equal(t, a, b)
	require.NotEqual(t, a, c)

	// GrantPrivilege is used as a map key: equal values must address the same
	// entry and differing privileges must not.
	m := map[GrantPrivilege]struct{}{a: {}}
	_, ok := m[b]
	require.True(t, ok)
	_, ok = m[c]
	require.False(t, ok)
}
