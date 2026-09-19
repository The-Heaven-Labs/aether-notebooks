package chaccess

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserIdentStableAndScoped(t *testing.T) {
	wh, org, user := uuid.New(), uuid.New(), uuid.New()
	a := UserIdent(wh, org, user)
	b := UserIdent(wh, org, user)
	require.Equal(t, a, b)
	require.True(t, strings.HasPrefix(a, "aether_u_"))
	require.NotEqual(t, a, UserIdent(wh, org, uuid.New()))
	require.NotEqual(t, a, UserIdent(wh, uuid.New(), user))
	require.NotEqual(t, a, UserIdent(uuid.New(), org, user))
}

func TestRoleIdentStableAndScoped(t *testing.T) {
	wh, org, group := uuid.New(), uuid.New(), uuid.New()
	a := RoleIdent(wh, org, group)
	require.Equal(t, a, RoleIdent(wh, org, group))
	require.True(t, strings.HasPrefix(a, "aether_g_"))
	require.NotEqual(t, a, RoleIdent(wh, org, uuid.New()))
	require.NotEqual(t, a, RoleIdent(wh, uuid.New(), group))
	require.NotEqual(t, a, RoleIdent(uuid.New(), org, group))
}

func TestGoldenValues(t *testing.T) {
	wh := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	org := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	user := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	group := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	// Golden values — changing these means changing the identity scheme
	// (requires re-provisioning).
	require.Equal(t, "aether_u_2b4ea91627ef8e43", UserIdent(wh, org, user))
	require.Equal(t, "aether_g_7679a9b59a843fcb", RoleIdent(wh, org, group))

	pw := DerivePassword([]byte("0123456789abcdef0123456789abcdef"), wh, user)
	require.Equal(t, "Ae1_oGAUbvqm3-oINFPX4yhM28yEMXiNxMrg", pw)
	require.Len(t, pw, 36)
	require.Regexp(t, `^Ae1_[A-Za-z0-9_-]{32}$`, pw)
	require.NotContains(t, pw, "'")
	require.NotContains(t, pw, `\`)
}

func TestDerivePasswordDeterministicAndComplex(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	wh, user := uuid.New(), uuid.New()
	p1 := DerivePassword(master, wh, user)
	p2 := DerivePassword(master, wh, user)
	require.Equal(t, p1, p2)
	require.Len(t, p1, 36)
	require.Regexp(t, `[A-Z]`, p1)
	require.Regexp(t, `[0-9]`, p1)
	require.NotEqual(t, p1, DerivePassword(master, wh, uuid.New()))
	require.NotEqual(t, p1, DerivePassword(master, uuid.New(), user))
	require.NotEqual(t, p1, DerivePassword([]byte("other"), wh, user))
}

func TestQuoteIdentRejectsInjection(t *testing.T) {
	q, err := QuoteIdent("fact_sales")
	require.NoError(t, err)
	require.Equal(t, "`fact_sales`", q)

	max := strings.Repeat("a", 64)
	q, err = QuoteIdent(max)
	require.NoError(t, err)
	require.Equal(t, "`"+max+"`", q)

	for _, bad := range []string{"", "a`b", "a; DROP USER x", "a b", "x\ny", strings.Repeat("a", 65)} {
		_, err := QuoteIdent(bad)
		require.Error(t, err, "must reject %q", bad)
	}
}

func TestQuoteObjectIdentAllowsCatalogNames(t *testing.T) {
	for _, good := range []string{"fact_sales", "MyTable", "db-1", "a.b", "events_2024$", "1table", strings.Repeat("a", 127)} {
		q, err := QuoteObjectIdent(good)
		require.NoError(t, err, "must accept %q", good)
		require.Equal(t, "`"+good+"`", q)
	}
	for _, bad := range []string{"", "a`b", "a\\b", "a b", "x\ny", "a;--", "-leading", ".leading", strings.Repeat("a", 128)} {
		_, err := QuoteObjectIdent(bad)
		require.Error(t, err, "must reject %q", bad)
	}
}

// ClickHouse legally allows names like "my table" in backticks, but quoting
// here is whitelist validation rather than escape encoding, so names
// containing whitespace cannot be used as grant targets. Rejecting them keeps
// every interpolated identifier provably breakout-free.
func TestQuoteObjectIdentRejectsRealisticButUnsupportedNames(t *testing.T) {
	_, err := QuoteObjectIdent("my table")
	require.Error(t, err)
}
