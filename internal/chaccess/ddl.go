package chaccess

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// Fingerprint returns a stable, non-reversible change-detection fingerprint
// for a derived master key. The reconcile compares it with
// warehouses.applied_master_fp to decide whether every provisioned user
// password must be re-issued; it is not a password comparison.
func Fingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
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
// internal invariant violation and panics); catalog database/table names and
// observed identity names that cannot be quoted are reported in skipped and
// omitted from the plan. The caller runs statements sequentially through the
// provisioner connector.
//
// Ordering matters: roles are created before they are granted, orphan role
// drops come last (ClickHouse rejects REVOKE ... FROM user for a role that no
// longer exists with UNKNOWN_ROLE), and user password resets happen before
// membership changes.
func Statements(d DesiredState, a ActualState) (out []string, skipped []string) {
	// 1. Roles: create, grant additions, revoke extras.
	for _, ident := range sortedKeys(d.Roles) {
		rs := d.Roles[ident]
		if _, exists := a.Roles[ident]; !exists {
			out = append(out, fmt.Sprintf("CREATE ROLE IF NOT EXISTS %s", mustQuote(ident)))
		}
		actual := a.Roles[ident]
		for _, gr := range sortedGrants(rs.Grants) {
			if _, ok := actual[gr]; !ok {
				if stmt, ok := grantDDL(gr, mustQuote(ident), &skipped); ok {
					out = append(out, stmt)
				}
			}
		}
		for _, gr := range sortedGrants(actual) {
			if _, ok := rs.Grants[gr]; !ok {
				if stmt, ok := revokeDDL(gr, mustQuote(ident), &skipped); ok {
					out = append(out, stmt)
				}
			}
		}
	}

	// 2. Users: create, password reset, role membership, default roles,
	//    direct grants.
	for _, ident := range sortedKeys(d.Users) {
		us := d.Users[ident]
		actual, exists := a.Users[ident]
		if !exists {
			out = append(out, fmt.Sprintf(
				"CREATE USER IF NOT EXISTS %s IDENTIFIED WITH sha256_password BY '%s' GRANTEES NONE",
				mustQuote(ident), escapePassword(us.Password)))
		} else if a.ForcePasswordReset {
			out = append(out, fmt.Sprintf(
				"ALTER USER %s IDENTIFIED WITH sha256_password BY '%s'",
				mustQuote(ident), escapePassword(us.Password)))
		}
		want := make(map[string]struct{}, len(us.Roles))
		for _, r := range us.Roles {
			want[r] = struct{}{}
		}
		rolesChanged := false
		for _, role := range sortedStrings(us.Roles) {
			if _, ok := actual.Roles[role]; !ok {
				out = append(out, fmt.Sprintf("GRANT %s TO %s", mustQuote(role), mustQuote(ident)))
				rolesChanged = true
			}
		}
		for _, role := range sortedSet(actual.Roles) {
			if _, ok := want[role]; !ok {
				q, ok := tryQuoteIdent(role)
				if !ok {
					skipped = append(skipped, role)
					continue
				}
				out = append(out, fmt.Sprintf("REVOKE %s FROM %s", q, mustQuote(ident)))
				rolesChanged = true
			}
		}
		if len(us.Roles) > 0 && (rolesChanged || !actual.DefaultRolesAll) {
			out = append(out, fmt.Sprintf("SET DEFAULT ROLE ALL TO %s", mustQuote(ident)))
		}
		for _, gr := range sortedGrants(us.DirectGrants) {
			if _, ok := actual.DirectGrants[gr]; !ok {
				if stmt, ok := grantDDL(gr, mustQuote(ident), &skipped); ok {
					out = append(out, stmt)
				}
			}
		}
		for _, gr := range sortedGrants(actual.DirectGrants) {
			if _, ok := us.DirectGrants[gr]; !ok {
				if stmt, ok := revokeDDL(gr, mustQuote(ident), &skipped); ok {
					out = append(out, stmt)
				}
			}
		}
	}

	// 3. Orphans: drop users, then delete roles after every membership change
	//    so no REVOKE references a role that has already been dropped.
	for _, ident := range sortedKeys(a.Users) {
		if _, ok := d.Users[ident]; !ok {
			if q, ok := tryQuoteIdent(ident); ok {
				out = append(out, fmt.Sprintf("DROP USER IF EXISTS %s", q))
			} else {
				skipped = append(skipped, ident)
			}
		}
	}
	for _, ident := range sortedKeys(a.Roles) {
		if _, ok := d.Roles[ident]; !ok {
			if q, ok := tryQuoteIdent(ident); ok {
				out = append(out, fmt.Sprintf("DROP ROLE IF EXISTS %s", q))
			} else {
				skipped = append(skipped, ident)
			}
		}
	}
	return out, dedupeSorted(skipped)
}

func mustQuote(s string) string {
	q, err := QuoteIdent(s)
	if err != nil {
		panic(err) // generated identifiers are validated at construction
	}
	return q
}

// tryQuoteIdent is the non-panicking variant of mustQuote for identifiers read
// from ClickHouse. A failure is reported in skipped instead of crashing the
// sync on catalog data that does not match Aether's generated-name charset.
func tryQuoteIdent(name string) (string, bool) {
	q, err := QuoteIdent(name)
	if err != nil {
		return "", false
	}
	return q, true
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
// base64url + prefix, so only a defensive check is needed. An empty password
// is rejected because ClickHouse would silently create a passwordless account.
func escapePassword(pw string) string {
	if pw == "" {
		panic("derived passwords must not be empty")
	}
	for _, r := range pw {
		if r == '\'' || r == '\\' {
			panic("derived passwords must not contain quotes or backslashes")
		}
	}
	return pw
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(in))
	for _, s := range in {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
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
