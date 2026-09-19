package chaccess

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserIdentStableAndScoped(t *testing.T) {
	org, user := uuid.New(), uuid.New()
	a := UserIdent(org, user)
	b := UserIdent(org, user)
	require.Equal(t, a, b)
	require.True(t, strings.HasPrefix(a, "aether_u_"))
	require.NotEqual(t, a, UserIdent(org, uuid.New()))
	require.NotEqual(t, a, UserIdent(uuid.New(), user))
}

func TestRoleIdentStableAndScoped(t *testing.T) {
	org, group := uuid.New(), uuid.New()
	require.Equal(t, RoleIdent(org, group), RoleIdent(org, group))
	require.True(t, strings.HasPrefix(RoleIdent(org, group), "aether_g_"))
	require.NotEqual(t, RoleIdent(org, group), RoleIdent(org, uuid.New()))
}

func TestDerivePasswordDeterministicAndComplex(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	wh, user := uuid.New(), uuid.New()
	p1 := DerivePassword(master, wh, user)
	p2 := DerivePassword(master, wh, user)
	require.Equal(t, p1, p2)
	require.GreaterOrEqual(t, len(p1), 12)
	require.Contains(t, p1, "A") // guaranteed uppercase prefix
	require.Contains(t, p1, "1") // guaranteed digit prefix
	require.NotEqual(t, p1, DerivePassword(master, wh, uuid.New()))
	require.NotEqual(t, p1, DerivePassword(master, uuid.New(), user))
	require.NotEqual(t, p1, DerivePassword([]byte("other"), wh, user))
}

func TestQuoteIdentRejectsInjection(t *testing.T) {
	q, err := QuoteIdent("fact_sales")
	require.NoError(t, err)
	require.Equal(t, "`fact_sales`", q)

	for _, bad := range []string{"", "a`b", "a; DROP USER x", "a b", "x\ny", strings.Repeat("a", 65)} {
		_, err := QuoteIdent(bad)
		require.Error(t, err, "must reject %q", bad)
	}
}

func TestQuoteObjectIdentAllowsCatalogNames(t *testing.T) {
	for _, good := range []string{"fact_sales", "MyTable", "db-1", "a.b", "events_2024$"} {
		q, err := QuoteObjectIdent(good)
		require.NoError(t, err, "must accept %q", good)
		require.Contains(t, q, good)
	}
	for _, bad := range []string{"", "a`b", "a\\b", "a b", "x\ny", "a;--", strings.Repeat("a", 128)} {
		_, err := QuoteObjectIdent(bad)
		require.Error(t, err, "must reject %q", bad)
	}
}
