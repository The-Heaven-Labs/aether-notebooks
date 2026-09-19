package chaccess

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Fingerprint returns a cheap non-reversible tag for comparing passwords.
func Fingerprint(pw string) string {
	sum := sha256.Sum256([]byte(pw))
	return hex.EncodeToString(sum[:8])
}

func grantSet(gs ...Grant) map[Grant]struct{} {
	m := make(map[Grant]struct{}, len(gs))
	for _, x := range gs {
		m[x] = struct{}{}
	}
	return m
}

func sortedGrants(m map[Grant]struct{}) []Grant {
	out := make([]Grant, 0, len(m))
	for x := range m {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Database != out[j].Database {
			return out[i].Database < out[j].Database
		}
		return out[i].Table < out[j].Table
	})
	return out
}

// Statements returns an ordered, idempotent DDL plan that moves ClickHouse
// from a to d. Generated identities are validated/quoted (a failure is an
// internal invariant violation and panics); catalog database/table names that
// cannot be quoted are reported in skipped and omitted from the plan. The
// caller runs statements sequentially through the provisioner connector.
func Statements(d DesiredState, a ActualState) (out []string, skipped []string) {
	// 1. Roles: create, grant additions, revoke extras, drop orphan roles.
	for _, ident := range sortedKeys(d.Roles) {
		rs := d.Roles[ident]
		out = append(out, fmt.Sprintf("CREATE ROLE IF NOT EXISTS %s", mustQuote(rs.Ident)))
		actual := a.Roles[ident]
		for _, gr := range sortedGrants(rs.Grants) {
			if _, ok := actual[gr]; !ok {
				if stmt, ok := grantDDL(gr, mustQuote(rs.Ident), &skipped); ok {
					out = append(out, stmt)
				}
			}
		}
		for _, gr := range sortedGrants(actual) {
			if _, ok := rs.Grants[gr]; !ok {
				if stmt, ok := revokeDDL(gr, mustQuote(rs.Ident), &skipped); ok {
					out = append(out, stmt)
				}
			}
		}
	}
	for _, ident := range sortedKeys(a.Roles) {
		if _, ok := d.Roles[ident]; !ok {
			out = append(out, fmt.Sprintf("DROP ROLE IF EXISTS %s", mustQuote(ident)))
		}
	}

	// 2. Users: create, password reset, role membership, default roles,
	//    direct grants, drop orphans.
	for _, ident := range sortedKeys(d.Users) {
		us := d.Users[ident]
		actual, exists := a.Users[ident]
		if !exists {
			out = append(out, fmt.Sprintf(
				"CREATE USER IF NOT EXISTS %s IDENTIFIED WITH sha256_password BY '%s' GRANTEES NONE",
				mustQuote(us.Ident), escapePassword(us.Password)))
		} else if a.ForcePasswordReset {
			out = append(out, fmt.Sprintf(
				"ALTER USER %s IDENTIFIED WITH sha256_password BY '%s'",
				mustQuote(us.Ident), escapePassword(us.Password)))
		}
		want := make(map[string]struct{}, len(us.Roles))
		for _, r := range us.Roles {
			want[r] = struct{}{}
		}
		for _, role := range sortedStrings(us.Roles) {
			if _, ok := actual.Roles[role]; !ok {
				out = append(out, fmt.Sprintf("GRANT %s TO %s", mustQuote(role), mustQuote(us.Ident)))
			}
		}
		for _, role := range sortedSet(actual.Roles) {
			if _, ok := want[role]; !ok {
				out = append(out, fmt.Sprintf("REVOKE %s FROM %s", mustQuote(role), mustQuote(us.Ident)))
			}
		}
		if len(us.Roles) > 0 || len(actual.Roles) > 0 {
			out = append(out, fmt.Sprintf("SET DEFAULT ROLE ALL TO %s", mustQuote(us.Ident)))
		}
		for _, gr := range sortedGrants(us.DirectGrants) {
			if _, ok := actual.DirectGrants[gr]; !ok {
				if stmt, ok := grantDDL(gr, mustQuote(us.Ident), &skipped); ok {
					out = append(out, stmt)
				}
			}
		}
		for _, gr := range sortedGrants(actual.DirectGrants) {
			if _, ok := us.DirectGrants[gr]; !ok {
				if stmt, ok := revokeDDL(gr, mustQuote(us.Ident), &skipped); ok {
					out = append(out, stmt)
				}
			}
		}
	}
	for _, ident := range sortedKeys(a.Users) {
		if _, ok := d.Users[ident]; !ok {
			out = append(out, fmt.Sprintf("DROP USER IF EXISTS %s", mustQuote(ident)))
		}
	}
	return out, skipped
}

func mustQuote(s string) string {
	q, err := QuoteIdent(s)
	if err != nil {
		panic(err) // generated identifiers are validated at construction
	}
	return q
}

// grantDDL/revokeDDL validate catalog object names and skip unquotable ones
// (appended to skipped) rather than panicking or aborting the whole sync.
func grantDDL(gr Grant, target string, skipped *[]string) (string, bool) {
	db, err1 := QuoteObjectIdent(gr.Database)
	tbl, err2 := QuoteObjectIdent(gr.Table)
	if err1 != nil || err2 != nil {
		*skipped = append(*skipped, gr.Database+"."+gr.Table)
		return "", false
	}
	return fmt.Sprintf("GRANT SELECT ON %s.%s TO %s", db, tbl, target), true
}

func revokeDDL(gr Grant, target string, skipped *[]string) (string, bool) {
	db, err1 := QuoteObjectIdent(gr.Database)
	tbl, err2 := QuoteObjectIdent(gr.Table)
	if err1 != nil || err2 != nil {
		*skipped = append(*skipped, gr.Database+"."+gr.Table)
		return "", false
	}
	return fmt.Sprintf("REVOKE SELECT ON %s.%s FROM %s", db, tbl, target), true
}

// escapePassword hardens the string literal. Derived passwords are already
// base64url + prefix, so only a defensive check is needed.
func escapePassword(pw string) string {
	for _, r := range pw {
		if r == '\'' || r == '\\' {
			panic("derived passwords must not contain quotes or backslashes")
		}
	}
	return pw
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStrings(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

func sortedSet(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
