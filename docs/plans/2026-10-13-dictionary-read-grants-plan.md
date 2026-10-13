# Dictionary Read Grants Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Managed ClickHouse warehouse identities can read granted dictionaries — `SELECT *`, the whole `dictGet` function family (`dictGet`, `dictHas`, …), and `SHOW CREATE DICTIONARY` — by emitting a uniform read bundle (`SELECT, dictGet, SHOW DICTIONARIES`) for every granted object instead of `SELECT` alone.

**Architecture:** Grants stay name-scoped `(database, object)` rows; the ClickHouse DDL diff moves from single-privilege grants to `(object, privilege)` triples. Desired state expands every logical grant to the privilege bundle for the server's mode (full = three privileges, legacy = `SELECT` only for pre-granular servers, chosen by a one-shot capability probe). Introspection loads the managed privilege set instead of `SELECT` only, so the drift guard expects the bundle, fails closed on wildcards/grant options across all managed access types, and revokes extras. No migration, no API change, no UI change.

**Tech Stack:** Go 1.x, `net/http`, pgx, `github.com/ClickHouse/clickhouse-go/v2`, testify, real ClickHouse dev stack (`docker compose -f docker-compose.dev.yml`).

**Design doc:** `docs/plans/2026-10-13-dictionary-read-grants-design.md` (branch `feat/dictionary-read-grants`, already committed).

**Verified ClickHouse facts this plan relies on (24.10.2, re-verify with Task 5):**
- `SELECT` alone cannot read a dictionary; `dictGet()`/`dictHas()`/`SELECT * FROM dict` need the `dictGet` privilege; `SHOW CREATE DICTIONARY` needs `SHOW DICTIONARIES`.
- `dictGet`/`SHOW DICTIONARIES` are accepted and inert on plain tables and views.
- Multi-privilege `GRANT a, b, c ON obj TO x` / `REVOKE a, b, c ON obj FROM x` work.
- Dictionaries appear in `system.tables` (engine `Dictionary`) and `system.columns`, so catalog/snapshot/picker need no changes.
- `system.privileges` (database `system`) lists grantable privileges; contains `dictGet` on 24.x, absent on pre-granular servers.

**Test commands (agent rule: always pass `-timeout 3m`; the script already does):**
- Pure unit tests: `go test ./internal/chaccess/ -timeout 3m -count=1 -v -run 'TestName'`
- API/integration tests: `bash scripts/go-test.sh ./internal/api/... -count=1 -run 'TestName' -v` (script resets the per-worktree test DB; it prepends `-timeout 3m`).

---

## Task 1: Privilege model — types, bundle, mode probe

**Files:**
- Create: `internal/chaccess/privileges.go`
- Test: `internal/chaccess/privileges_test.go`

**Step 1: Write the failing test**

Create `internal/chaccess/privileges_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/chaccess/ -timeout 3m -count=1 -run 'TestBundleByMode|TestModeFromProbe|TestGrantPrivilegeKey'`
Expected: FAIL — `undefined: Privilege` / `undefined: PrivilegeModeFull` compile errors.

**Step 3: Write minimal implementation**

Create `internal/chaccess/privileges.go`:

```go
package chaccess

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Privilege is a ClickHouse access type Aether manages as a read grant.
// Tables and views ignore dictGet/SHOW DICTIONARIES; dictionaries require
// them (modern ClickHouse denies dictGet-family functions without dictGet).
type Privilege string

const (
	AccessSelect           Privilege = "SELECT"
	AccessDictGet          Privilege = "dictGet"
	AccessShowDictionaries Privilege = "SHOW DICTIONARIES"
)

// privilegeOrder is the deterministic order privileges are emitted in a
// GRANT/REVOKE statement. It is also the full (modern) read bundle.
var privilegeOrder = []Privilege{AccessSelect, AccessDictGet, AccessShowDictionaries}

// PrivilegeMode selects the read bundle a warehouse server supports. Granular
// dictionary privileges (dictGet) exist from ClickHouse 23.x onward;
// pre-granular servers reject them, so Aether falls back to SELECT-only, which
// was the dictionary read privilege there.
type PrivilegeMode string

const (
	// PrivilegeModeFull grants SELECT, dictGet, SHOW DICTIONARIES per object.
	PrivilegeModeFull PrivilegeMode = "full"
	// PrivilegeModeLegacy grants SELECT only (pre-granular servers).
	PrivilegeModeLegacy PrivilegeMode = "legacy"
)

// Bundle returns the privileges granted for every managed object under mode.
// The returned slice is an independent copy; callers must not mutate it.
func (m PrivilegeMode) Bundle() []Privilege {
	if m == PrivilegeModeLegacy {
		return []Privilege{AccessSelect}
	}
	return append([]Privilege(nil), privilegeOrder...)
}

// GrantPrivilege is one (object, privilege) pair: the unit of the ClickHouse
// DDL diff. Grant itself stays the object-level key — tables, views, and
// dictionaries share the name-based grant model.
type GrantPrivilege struct {
	Grant
	Access Privilege
}

// DetectPrivilegeMode probes the provisioner connection for granular
// dictionary privileges. A probe failure other than "table/rows missing"
// returns an error so a transient outage cannot silently flip modes and
// trigger grant/revoke churn.
func DetectPrivilegeMode(ctx context.Context, conn clickhouse.Conn) (PrivilegeMode, error) {
	var n uint64
	err := conn.QueryRow(ctx, `
		SELECT count() FROM system.columns
		WHERE database = 'system' AND table = 'privileges' AND name = 'privilege'`).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("probe system.privileges: %w", err)
	}
	if n == 0 {
		return modeFromProbe(false, 0), nil
	}
	err = conn.QueryRow(ctx, `
		SELECT count() FROM system.privileges
		WHERE toString(privilege) IN ('dictGet', 'SHOW DICTIONARIES')`).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("probe dictionary privileges: %w", err)
	}
	return modeFromProbe(true, n), nil
}

// modeFromProbe maps the privilege-catalog probe result to a mode. Both
// dictGet and SHOW DICTIONARIES must exist; anything else is the legacy
// SELECT-only world.
func modeFromProbe(hasPrivilegesTable bool, matching uint64) PrivilegeMode {
	if !hasPrivilegesTable || matching < 2 {
		return PrivilegeModeLegacy
	}
	return PrivilegeModeFull
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/chaccess/ -timeout 3m -count=1 -run 'TestBundleByMode|TestModeFromProbe|TestGrantPrivilegeKey'`
Expected: PASS (3 tests).

**Step 5: Commit**

```bash
git add internal/chaccess/privileges.go internal/chaccess/privileges_test.go
git commit -m "feat(chaccess): add managed privilege bundle and mode probe"
```

---

## Task 2: Privilege-aware Statements

**Files:**
- Modify: `internal/chaccess/desired.go` (ActualState gains privilege detail)
- Modify: `internal/chaccess/ddl.go` (Statements diffs `(object, privilege)`)
- Test: `internal/chaccess/ddl_test.go` (update helper + all call sites + new tests)

**Step 1: Write the failing tests**

In `internal/chaccess/ddl_test.go`:

1. Add a helper next to `grantSet`:

```go
// fullPrivSet expands object grants into the modern bundle's (object,
// privilege) pairs, the shape ActualState carries after introspection.
func fullPrivSet(gs ...Grant) map[GrantPrivilege]struct{} {
	m := make(map[GrantPrivilege]struct{}, len(gs)*3)
	for _, x := range gs {
		for _, p := range PrivilegeModeFull.Bundle() {
			m[GrantPrivilege{Grant: x, Access: p}] = struct{}{}
		}
	}
	return m
}
```

2. Update `TestStatementsCreateRoleUserAndGrants` — pass the mode and expect the bundle:

```go
	stmts, skipped := Statements(d, ActualState{}, PrivilegeModeFull)
	require.Empty(t, skipped)
	require.Equal(t, []string{
		"CREATE ROLE IF NOT EXISTS `aether_g_aaa`",
		"GRANT SELECT, dictGet, SHOW DICTIONARIES ON `db1`.`t1` TO `aether_g_aaa`",
		"GRANT SELECT, dictGet, SHOW DICTIONARIES ON `db1`.`t2` TO `aether_g_aaa`",
		"CREATE USER IF NOT EXISTS `aether_u_bbb` IDENTIFIED WITH sha256_password BY 'Ae1_x' GRANTEES NONE",
		"GRANT `aether_g_aaa` TO `aether_u_bbb`",
		"SET DEFAULT ROLE ALL TO `aether_u_bbb`",
	}, stmts)
```

3. Update `TestStatementsRevokeExtrasAndDropOrphans` — actual now carries privileges:

```go
	a := ActualState{
		Roles: map[string]map[Grant]struct{}{
			"aether_g_aaa": {},
			"aether_g_zzz": grantSet(g("db1", "stale")),
		},
		RolePrivileges: map[string]map[GrantPrivilege]struct{}{
			"aether_g_aaa": fullPrivSet(g("db1", "t1"), g("db1", "old")),
			"aether_g_zzz": fullPrivSet(g("db1", "stale")),
		},
		Users: map[string]UserActual{
			"aether_u_bbb": {Roles: map[string]struct{}{"aether_g_aaa": {}, "aether_g_old": {}}},
			"aether_u_old": {Roles: map[string]struct{}{}},
		},
	}
	stmts, skipped := Statements(d, a, PrivilegeModeFull)
	require.Empty(t, skipped)
	joined := strings.Join(stmts, "\n")
	require.Contains(t, joined, "REVOKE SELECT, dictGet, SHOW DICTIONARIES ON `db1`.`old` FROM `aether_g_aaa`")
	require.Contains(t, joined, "DROP ROLE IF EXISTS `aether_g_zzz`")
	require.Contains(t, joined, "REVOKE `aether_g_old` FROM `aether_u_bbb`")
	require.Contains(t, joined, "DROP USER IF EXISTS `aether_u_old`")
```

4. Add three new tests:

```go
func TestStatementsRepairsMissingBundlePrivileges(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Grants: grantSet(g("db1", "t1"))},
		},
	}
	a := ActualState{
		Roles: map[string]map[Grant]struct{}{"aether_g_aaa": {}},
		RolePrivileges: map[string]map[GrantPrivilege]struct{}{
			"aether_g_aaa": {{Grant: g("db1", "t1"), Access: AccessSelect}: {}},
		},
	}
	stmts, skipped := Statements(d, a, PrivilegeModeFull)
	require.Empty(t, skipped)
	require.Equal(t, []string{
		"GRANT dictGet, SHOW DICTIONARIES ON `db1`.`t1` TO `aether_g_aaa`",
	}, stmts, "only the missing privileges are granted, in canonical order")
}

func TestStatementsLegacyModeEmitsSelectOnly(t *testing.T) {
	d := DesiredState{
		Roles: map[string]RoleState{
			"aether_g_aaa": {Grants: grantSet(g("db1", "t1"))},
		},
	}
	stmts, skipped := Statements(d, ActualState{}, PrivilegeModeLegacy)
	require.Empty(t, skipped)
	require.Equal(t, []string{
		"CREATE ROLE IF NOT EXISTS `aether_g_aaa`",
		"GRANT SELECT ON `db1`.`t1` TO `aether_g_aaa`",
	}, stmts)
}

func TestStatementsPartialObjectRevokedAsObservedPrivileges(t *testing.T) {
	// A partial bundle on a dropped object revokes exactly the observed
	// privileges, never privileges the server never granted.
	d := DesiredState{}
	a := ActualState{
		Roles: map[string]map[Grant]struct{}{"aether_g_aaa": {}},
		RolePrivileges: map[string]map[GrantPrivilege]struct{}{
			"aether_g_aaa": {{Grant: g("db1", "gone"), Access: AccessDictGet}: {}},
		},
	}
	stmts, _ := Statements(d, a, PrivilegeModeFull)
	require.Contains(t, stmts, "REVOKE dictGet ON `db1`.`gone` FROM `aether_g_aaa`")
}
```

5. Mechanically update every remaining `Statements(` call in the file to pass a mode:
   - `TestStatementsOrdersDependencies` → `PrivilegeModeFull`; its actual only needs object maps (orphan role `aether_g_old` is dropped, not revoked).
   - `TestStatementsResetsPasswordOnMasterKeyChange`, `TestStatementsForcePasswordResetDoesNotAlterNewUser`, `TestStatementsNoOpOnEmptyState`, `TestStatementsSetsDefaultRoleWhenActualDefaultMissing`, `TestStatementsSkipsDefaultRoleWhenActualDefaultSet`, `TestActualStateWildcards`, `TestStatementsSkipsUnquotableActualNames`, `TestStatementsSkipsUnquotableActualRoleRevoke`, `TestStatementsDedupesSkippedNames` → `PrivilegeModeFull`.
   - `TestStatementsNoOpWhenDesiredMatchesActual` → `PrivilegeModeFull` **and** add to actual:
     ```go
     		RolePrivileges: map[string]map[GrantPrivilege]struct{}{
     			"aether_g_aaa": fullPrivSet(g("db1", "t1")),
     		},
     ```
   - `TestStatementsSkipsUnquotableCatalogNames` → expect
     `"GRANT SELECT, dictGet, SHOW DICTIONARIES ON `db1`.`ok` TO `aether_g_aaa`"`.
   - `TestStatementsSkipsUnquotableRevokeTarget` → actual gains
     ```go
     		RolePrivileges: map[string]map[GrantPrivilege]struct{}{
     			"aether_g_aaa": fullPrivSet(g("db1", "ok"), g("db1", "my table")),
     		},
     ```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/chaccess/ -timeout 3m -count=1 -run 'TestStatements'`
Expected: FAIL — `too many arguments in call to Statements` (signature not changed yet) and the new tests' functions undefined.

**Step 3: Write the implementation**

3a. `internal/chaccess/desired.go` — extend the actual-state structs (keep `Grant` unchanged):

```go
// UserActual is the observed state of one ClickHouse user.
type UserActual struct {
	Roles           map[string]struct{}
	DirectGrants    map[Grant]struct{}
	DefaultRolesAll bool
	// DirectPrivileges is the raw observed (object, privilege) set; every
	// entry is one of the managed read privileges.
	DirectPrivileges map[GrantPrivilege]struct{}
}
```

and

```go
type ActualState struct {
	Roles map[string]map[Grant]struct{}
	// RolePrivileges carries the raw observed (object, privilege) sets the
	// DDL diff consumes; Roles carries object-level presence (a full bundle)
	// for drift reporting.
	RolePrivileges     map[string]map[GrantPrivilege]struct{}
	Users              map[string]UserActual
	Wildcards          []WildcardGrant
	Unexpected         []string
	ForcePasswordReset bool
}
```

3b. `internal/chaccess/ddl.go` — change the signature and replace `grantDDL`/`revokeDDL` with grouped builders:

```go
// Statements returns an ordered, idempotent DDL plan that moves ClickHouse
// from a to d. Every granted object receives mode's privilege bundle
// (modern: SELECT, dictGet, SHOW DICTIONARIES; legacy: SELECT). Statements
// are grouped per object and list exactly the privileges being added or
// removed. Generated identities are validated/quoted (a failure is an
// internal invariant violation and panics); catalog names and observed
// identity names that cannot be quoted are reported in skipped and omitted
// from the plan.
//
// Ordering matters: roles are created before they are granted, orphan role
// drops come last (ClickHouse rejects REVOKE ... FROM user for a role that
// no longer exists with UNKNOWN_ROLE), and user password resets happen before
// membership changes.
func Statements(d DesiredState, a ActualState, mode PrivilegeMode) (out []string, skipped []string) {
	// 1. Roles: create, grant additions, revoke extras.
	for _, ident := range sortedKeys(d.Roles) {
		rs := d.Roles[ident]
		if _, exists := a.Roles[ident]; !exists {
			out = append(out, fmt.Sprintf("CREATE ROLE IF NOT EXISTS %s", mustQuote(ident)))
		}
		target := mustQuote(ident)
		actualPrivs := a.RolePrivileges[ident]
		out = append(out, grantStatements(rs.Grants, actualPrivs, mode, target, &skipped)...)
		out = append(out, revokeStatements(actualPrivs, rs.Grants, target, &skipped)...)
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
		target := mustQuote(ident)
		out = append(out, grantStatements(us.DirectGrants, actual.DirectPrivileges, mode, target, &skipped)...)
		out = append(out, revokeStatements(actual.DirectPrivileges, us.DirectGrants, target, &skipped)...)
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
```

Add (replace the old `grantDDL`/`revokeDDL`):

```go
// grantStatements emits one GRANT per desired object whose observed
// privileges do not cover mode's bundle. The statement lists exactly the
// missing privileges in canonical order, so a partial repair never touches
// privileges that are already correct. Unquotable names are skipped.
func grantStatements(desired map[Grant]struct{}, actual map[GrantPrivilege]struct{}, mode PrivilegeMode, target string, skipped *[]string) []string {
	var out []string
	for _, gr := range sortedGrants(desired) {
		qdb, err1 := QuoteObjectIdent(gr.Database)
		qtbl, err2 := QuoteObjectIdent(gr.Table)
		if err1 != nil || err2 != nil {
			*skipped = append(*skipped, gr.Database+"."+gr.Table)
			continue
		}
		var missing []string
		for _, p := range mode.Bundle() {
			if _, ok := actual[GrantPrivilege{Grant: gr, Access: p}]; !ok {
				missing = append(missing, string(p))
			}
		}
		if len(missing) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("GRANT %s ON %s.%s TO %s",
			strings.Join(missing, ", "), qdb, qtbl, target))
	}
	return out
}

// revokeStatements emits one REVOKE per observed object that is no longer
// desired, listing every observed privilege for it in canonical order.
// Objects that are still desired cannot hold out-of-bundle privileges (the
// managed set equals the bundle), so they never produce revokes here.
// Unquotable names are skipped.
func revokeStatements(actual map[GrantPrivilege]struct{}, desired map[Grant]struct{}, target string, skipped *[]string) []string {
	byObject := map[Grant]map[Privilege]struct{}{}
	for gp := range actual {
		if _, ok := desired[gp.Grant]; ok {
			continue
		}
		if byObject[gp.Grant] == nil {
			byObject[gp.Grant] = map[Privilege]struct{}{}
		}
		byObject[gp.Grant][gp.Access] = struct{}{}
	}
	var out []string
	for _, gr := range sortedGrantKeys(byObject) {
		qdb, err1 := QuoteObjectIdent(gr.Database)
		qtbl, err2 := QuoteObjectIdent(gr.Table)
		if err1 != nil || err2 != nil {
			*skipped = append(*skipped, gr.Database+"."+gr.Table)
			continue
		}
		var privs []string
		for _, p := range privilegeOrder {
			if _, ok := byObject[gr][p]; ok {
				privs = append(privs, string(p))
			}
		}
		out = append(out, fmt.Sprintf("REVOKE %s ON %s.%s FROM %s",
			strings.Join(privs, ", "), qdb, qtbl, target))
	}
	return out
}

// sortedGrantKeys returns the object keys of a map sorted deterministically.
func sortedGrantKeys[V any](m map[Grant]V) []Grant {
	out := make([]Grant, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Database != out[j].Database {
			return out[i].Database < out[j].Database
		}
		return out[i].Table < out[j].Table
	})
	return out
}
```

Add `"strings"` to the imports of `ddl.go`.

**Step 4: Run tests**

Run: `go test ./internal/chaccess/ -timeout 3m -count=1`
Expected: PASS — all `chaccess` tests, including the updated ones.

**Step 5: Commit**

```bash
git add internal/chaccess/desired.go internal/chaccess/ddl.go internal/chaccess/ddl_test.go
git commit -m "feat(chaccess): emit SELECT, dictGet, SHOW DICTIONARIES per granted object"
```

---

## Task 3: Introspection loads the managed privilege set

**Files:**
- Modify: `internal/chaccess/introspect.go`
- Test: `internal/chaccess/introspect_test.go`

**Step 1: Write the failing tests**

Rewrite/add in `internal/chaccess/introspect_test.go` (the builder gains a mode parameter; rows gain `AccessType`):

```go
func TestParseGrantRows(t *testing.T) {
	rows := []GrantRow{
		{UserName: "aether_u_x", Database: "db", Table: "t1", AccessType: "SELECT"},
		{UserName: "aether_u_x", Database: "db", Table: "t1", AccessType: "dictGet"},
		{UserName: "aether_u_x", Database: "db", Table: "t1", AccessType: "SHOW DICTIONARIES"},
		{UserName: "aether_u_x", Database: "db", Table: "t2", AccessType: "SELECT"},
	}
	users := map[string]UserActual{"aether_u_x": {}}
	a := buildActualFromGrantRows(rows, nil, users, nil, PrivilegeModeFull)
	require.Contains(t, a.Users["aether_u_x"].DirectGrants, Grant{"db", "t1"},
		"a full bundle makes the object object-present")
	require.NotContains(t, a.Users["aether_u_x"].DirectGrants, Grant{"db", "t2"},
		"a partial bundle leaves the object privilege-only so drift can report it")
	require.Contains(t, a.Users["aether_u_x"].DirectPrivileges,
		GrantPrivilege{Grant: Grant{"db", "t2"}, Access: AccessSelect})
	require.NotContains(t, a.Users["aether_u_x"].Roles, "aether_g_y")
}

func TestParseFullBundleRoleIsObjectPresent(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", Database: "db", Table: "d1", AccessType: "SELECT"},
		{RoleName: "aether_wh_g_x", Database: "db", Table: "d1", AccessType: "dictGet"},
		{RoleName: "aether_wh_g_x", Database: "db", Table: "d1", AccessType: "SHOW DICTIONARIES"},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil, PrivilegeModeFull)
	require.Contains(t, a.Roles["aether_wh_g_x"], Grant{"db", "d1"})
	require.Len(t, a.RolePrivileges["aether_wh_g_x"], 3)
}

func TestParseLegacyModeRequiresSelectOnly(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", Database: "db", Table: "t1", AccessType: "SELECT"},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil, PrivilegeModeLegacy)
	require.Contains(t, a.Roles["aether_wh_g_x"], Grant{"db", "t1"})
}

func TestParseNonManagedAccessTypesAreSkipped(t *testing.T) {
	rows := []GrantRow{
		{UserName: "aether_wh_u_x", Database: "db", Table: "t1", AccessType: "INSERT"},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil, PrivilegeModeFull)
	require.Empty(t, a.Users["aether_wh_u_x"].DirectPrivileges)
	require.Empty(t, a.Users["aether_wh_u_x"].DirectGrants)
	require.Empty(t, a.Unexpected)
}

func TestParseDictGetWildcardFlagged(t *testing.T) {
	rows := []GrantRow{
		{UserName: "aether_wh_u_x", Database: "db", Table: "", AccessType: "dictGet"},
	}
	a := buildActualFromGrantRows(rows, nil, nil, nil, PrivilegeModeFull)
	require.Equal(t, []WildcardGrant{{Subject: "aether_wh_u_x", Scope: "db.*"}}, a.Wildcards)
}
```

Update existing literals in the same file:
- `TestParseWildcardRowsFlagged`, `TestParseGlobalWildcardRow`, `TestParsePartialRevokeAndTablePrefixWildcards`, `TestParseIsWildcardRowWithoutStar`, `TestParseWildcardOrderingDeterministic`, `TestParseZeroGrantRolesAreLoaded` (no rows — only builder call), `TestParseUnexpectedSortedAndDeduped` (only builder call) → add the `PrivilegeModeFull` argument; add `AccessType: "SELECT"` to every row literal that represents a managed grant (wildcard rows too — one of them should use `"dictGet"` to prove privilege-agnostic wildcard handling).
- `TestParseGrantOptionIsUnexpectedDrift` → `AccessType: "SELECT"` + mode argument; behavior unchanged.

Extend `TestWildcardScopeFromGrantLine` cases with:

```go
		{"GRANT dictGet ON db.events* TO aether_g", "db.events*", true},
		{"GRANT dictGet ON db.* TO aether_g", "db.*", true},
		{"GRANT SHOW DICTIONARIES ON db.* TO aether_g", "db.*", true},
		{"GRANT dictGet ON db.t TO aether_g", "", false},
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/chaccess/ -timeout 3m -count=1 -run 'TestParse|TestWildcardScopeFromGrantLine'`
Expected: FAIL — builder/parser mismatch, undefined `AccessType` on `GrantRow`.

**Step 3: Write the implementation**

3a. `GrantRow` gains the access type:

```go
// GrantRow is one relevant row of system.grants: managed read privileges
// (SELECT, dictGet, SHOW DICTIONARIES) without a column list.
type GrantRow struct {
	UserName        string
	RoleName        string
	Database        string
	Table           string
	IsPartialRevoke uint8
	GrantOption     uint8
	IsWildcard      uint8
	AccessType      string
}
```

3b. `LoadActual` gains the mode and threads it through:

```go
func LoadActual(ctx context.Context, conn clickhouse.Conn, warehouseID uuid.UUID, mode PrivilegeMode) (ActualState, error) {
	prefix := IdentifierPrefix(warehouseID)

	hasIsWildcard, err := hasIsWildcardColumn(ctx, conn)
	if err != nil {
		return ActualState{}, err
	}

	grants, err := loadGrants(ctx, conn, prefix, hasIsWildcard, mode)
	if err != nil {
		return ActualState{}, err
	}

	roles, err := loadRoles(ctx, conn, prefix)
	if err != nil {
		return ActualState{}, err
	}

	memberships, unexpected, err := loadRoleGrants(ctx, conn, prefix)
	if err != nil {
		return ActualState{}, err
	}

	nonSelect, err := loadNonSelectGrants(ctx, conn, prefix, mode)
	if err != nil {
		return ActualState{}, err
	}
	unexpected = append(unexpected, nonSelectDrift(nonSelect)...)

	users, err := loadUsers(ctx, conn, prefix, memberships)
	if err != nil {
		return ActualState{}, err
	}

	a := buildActualFromGrantRows(grants, roles, users, unexpected, mode)
	if hasIsWildcard {
		return a, nil
	}
	wildcards, err := scanWildcardGrants(ctx, conn, prefix, roles, users)
	if err != nil {
		return ActualState{}, err
	}
	a.Wildcards = normalizeWildcards(append(a.Wildcards, wildcards...))
	return a, nil
}
```

Update its doc comment: replace "SELECT grants" wording with "the managed read privileges (SELECT, dictGet, SHOW DICTIONARIES; SELECT only in legacy mode)" and note the mode gates the query because filtering `system.grants.access_type` by a literal the server's enum does not know can itself error.

3c. `loadGrants` takes the mode:

```go
func loadGrants(ctx context.Context, conn clickhouse.Conn, prefix string, hasIsWildcard bool, mode PrivilegeMode) ([]GrantRow, error) {
	cols := "coalesce(user_name,''), coalesce(role_name,''), coalesce(database,''), coalesce(table,''), is_partial_revoke, grant_option"
	if hasIsWildcard {
		cols += ", is_wildcard"
	}
	cols += ", access_type"
	filter := "access_type = 'SELECT'"
	if mode != PrivilegeModeLegacy {
		filter = "access_type IN ('SELECT', 'dictGet', 'SHOW DICTIONARIES')"
	}
	query := "SELECT " + cols + " FROM system.grants" +
		" WHERE (startsWith(user_name, ?) OR startsWith(role_name, ?))" +
		" AND " + filter + " AND is_partial_revoke = 0" +
		" AND coalesce(`column`, '') = ''"

	rows, err := conn.Query(ctx, query, prefix, prefix)
	if err != nil {
		return nil, fmt.Errorf("system.grants: %w", err)
	}
	var grants []GrantRow
	for rows.Next() {
		var r GrantRow
		dest := []any{&r.UserName, &r.RoleName, &r.Database, &r.Table, &r.IsPartialRevoke, &r.GrantOption}
		if hasIsWildcard {
			dest = append(dest, &r.IsWildcard)
		}
		dest = append(dest, &r.AccessType)
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan system.grants: %w", err)
		}
		grants = append(grants, r)
	}
	// ... unchanged rows.Err()/Close() tail ...
}
```

3d. `loadNonSelectGrants` takes the mode:

```go
// loadNonSelectGrants reads namespace grants outside the managed read set
// (INSERT, ALTER, DROP, ...). The managed set is excluded in SQL so the
// enum literal is only referenced on servers whose mode supports it;
// partial revokes are not grants and are excluded, matching loadGrants.
func loadNonSelectGrants(ctx context.Context, conn clickhouse.Conn, prefix string, mode PrivilegeMode) ([]NonSelectGrant, error) {
	filter := "access_type != 'SELECT'"
	if mode != PrivilegeModeLegacy {
		filter = "access_type NOT IN ('SELECT', 'dictGet', 'SHOW DICTIONARIES')"
	}
	rows, err := conn.Query(ctx, `
		SELECT access_type, coalesce(user_name,''), coalesce(role_name,''), coalesce(database,''), coalesce(table,'')
		FROM system.grants
		WHERE (startsWith(user_name, ?) OR startsWith(role_name, ?))
		  AND `+filter+` AND is_partial_revoke = 0`,
		prefix, prefix)
	// ... unchanged tail ...
}
```

3e. `buildActualFromGrantRows` collects privileges and derives object presence from the full bundle:

```go
func buildActualFromGrantRows(grants []GrantRow, roles map[string]map[Grant]struct{}, users map[string]UserActual, unexpected []string, mode PrivilegeMode) ActualState {
	a := ActualState{
		Roles:          map[string]map[Grant]struct{}{},
		RolePrivileges: map[string]map[GrantPrivilege]struct{}{},
		Users:          map[string]UserActual{},
		Unexpected:     append([]string{}, unexpected...),
	}
	for name := range roles {
		a.Roles[name] = map[Grant]struct{}{}
	}
	for k, v := range users {
		if v.Roles == nil {
			v.Roles = map[string]struct{}{}
		}
		a.Users[k] = v
	}
	for _, r := range grants {
		if r.IsPartialRevoke == 1 {
			continue
		}
		if !managedAccessType(r.AccessType) {
			continue // loadGrants filters to the managed set; belt and braces
		}
		subject := r.RoleName
		if subject == "" {
			subject = r.UserName
		}
		if r.GrantOption == 1 {
			a.Unexpected = append(a.Unexpected, fmt.Sprintf("grant option on %s.%s for %s", r.Database, r.Table, subject))
		}
		// Version-agnostic wildcard detection. ... (keep the existing
		// IsWildcard / empty-table / unquotable branches verbatim)
		if r.IsWildcard == 1 { /* unchanged */ }
		if r.Table == "" { /* unchanged */ }
		if _, err := QuoteObjectIdent(r.Database); err != nil { /* unchanged */ }
		if _, err := QuoteObjectIdent(r.Table); err != nil { /* unchanged */ }

		gp := GrantPrivilege{
			Grant:  Grant{Database: r.Database, Table: r.Table},
			Access: Privilege(r.AccessType),
		}
		if r.RoleName != "" {
			if a.RolePrivileges[r.RoleName] == nil {
				a.RolePrivileges[r.RoleName] = map[GrantPrivilege]struct{}{}
			}
			a.RolePrivileges[r.RoleName][gp] = struct{}{}
		} else if r.UserName != "" {
			u := a.Users[r.UserName]
			if u.DirectPrivileges == nil {
				u.DirectPrivileges = map[GrantPrivilege]struct{}{}
			}
			u.DirectPrivileges[gp] = struct{}{}
			a.Users[r.UserName] = u
		}
	}
	// Object-level presence requires the full bundle: a manually revoked
	// privilege must resurface as a missing object grant in drift reports.
	for name, privs := range a.RolePrivileges {
		a.Roles[name] = fullBundleObjects(privs, mode)
	}
	for name, u := range a.Users {
		u.DirectGrants = fullBundleObjects(u.DirectPrivileges, mode)
		a.Users[name] = u
	}
	a.Wildcards = normalizeWildcards(a.Wildcards)
	a.Unexpected = dedupeSorted(a.Unexpected)
	return a
}

// fullBundleObjects returns the objects whose observed privileges cover every
// privilege in mode's bundle. Partially granted objects (for example a manual
// REVOKE of one bundle privilege) are deliberately absent: the drift report
// is object-level, and partial bundles must resurface as missing grants.
func fullBundleObjects(privs map[GrantPrivilege]struct{}, mode PrivilegeMode) map[Grant]struct{} {
	byObject := map[Grant]map[Privilege]struct{}{}
	for gp := range privs {
		if byObject[gp.Grant] == nil {
			byObject[gp.Grant] = map[Privilege]struct{}{}
		}
		byObject[gp.Grant][gp.Access] = struct{}{}
	}
	out := map[Grant]struct{}{}
	bundle := mode.Bundle()
	for gr, set := range byObject {
		full := true
		for _, p := range bundle {
			if _, ok := set[p]; !ok {
				full = false
				break
			}
		}
		if full {
			out[gr] = struct{}{}
		}
	}
	return out
}

// managedAccessType reports whether accessType is one of the read privileges
// Aether manages as part of the bundle.
func managedAccessType(accessType string) bool {
	switch Privilege(accessType) {
	case AccessSelect, AccessDictGet, AccessShowDictionaries:
		return true
	}
	return false
}
```

3f. The `SHOW GRANTS` fallback parser must recognize the new privilege lines (servers without `is_wildcard` — our 24.x fleet):

```go
// wildcardScopeFromGrantLine parses one line of SHOW GRANTS output. It is
// deliberately conservative: any line that mentions one of the managed read
// privileges and contains "*" is reported as a wildcard (false positives fail
// closed). ...
func wildcardScopeFromGrantLine(line string) (string, bool) {
	upper := strings.ToUpper(line)
	if !containsManagedPrivilege(upper) || !strings.Contains(line, "*") {
		return "", false
	}
	// ... rest unchanged ...
}

// containsManagedPrivilege reports whether an upper-cased SHOW GRANTS line
// references one of the managed read privileges.
func containsManagedPrivilege(upper string) bool {
	return strings.Contains(upper, "SELECT") ||
		strings.Contains(upper, "DICTGET") ||
		strings.Contains(upper, "SHOW DICTIONARIES")
}
```

**Step 4: Run tests**

Run: `go test ./internal/chaccess/ -timeout 3m -count=1`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chaccess/introspect.go internal/chaccess/introspect_test.go
git commit -m "feat(chaccess): load managed privilege bundle in actual state"
```

---

## Task 4: Wire the mode through reconcile, drift detection, and identity drop

**Files:**
- Modify: `internal/api/warehouse_sync.go`
- Verify: `internal/api/warehouse_sync_test.go` (compile + unit tests)

**Step 1: Adjust the three production call sites**

1a. `reconcileWarehouse` — after `defer conn.Close()` (provisioner connection) and before the catalog snapshot:

```go
	mode, err := chaccess.DetectPrivilegeMode(ctx, conn)
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID, fmt.Errorf("detect clickhouse privilege mode: %w", err))
	}
```

then change:

```go
	actual, err := chaccess.LoadActual(ctx, conn, warehouseID, mode)
```

and

```go
	stmts, skipped := chaccess.Statements(desired, actual, mode)
```

1b. `detectWarehouseDrift` — after its provisioner connection opens:

```go
	mode, err := chaccess.DetectPrivilegeMode(ctx, conn)
	if err != nil {
		return DriftReport{}, fmt.Errorf("detect warehouse %s drift: detect clickhouse privilege mode: %w", warehouseID, err)
	}
```

then `chaccess.LoadActual(ctx, conn, warehouseID, mode)`.

1c. `dropWarehouseIdentitiesLocked` — after its provisioner connection opens:

```go
	mode, err := chaccess.DetectPrivilegeMode(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("detect clickhouse privilege mode: %w", err)
	}
```

then both `chaccess.LoadActual(ctx, conn, warehouseID, mode)` calls and
`chaccess.Statements(chaccess.DesiredState{}, actual, mode)`.

**Step 2: Build and run the focused unit tests**

Run: `go build ./... && go test ./internal/chaccess/ -timeout 3m -count=1`
Expected: build OK; chaccess PASS.

Run: `bash scripts/go-test.sh ./internal/api/... -count=1 -run 'TestCompareWarehouseStateReportsAllDriftKinds|TestPooledWarehouseUsers|TestReconcileWarehouseStatementFailureMarksError|TestDriftDetection' -v`
Expected: `TestCompareWarehouseStateReportsAllDriftKinds` PASS (compare is object-level and untouched; hand-built `ActualState` literals still compile). `TestReconcileWarehouseStatementFailureMarksError` may FAIL: the limited provisioner only holds `SELECT ... WITH GRANT OPTION`, so the DDL now fails on the first role grant (`dictGet` not grantable) instead of at user creation. Fix the fixture (Step 3). The drift tests should PASS: a manually revoked bundle privilege leaves the object partial, which `fullBundleObjects` reports as missing.

**Step 3: Fix `TestReconcileWarehouseStatementFailureMarksError`**

The test's intent is "role created, user denied". The limited provisioner must be able to grant the full bundle. Replace its grant list entry:

```go
	for _, grant := range []string{
		"GRANT SELECT ON system.grants TO " + limitedUser,
		"GRANT SELECT ON system.users TO " + limitedUser,
		"GRANT SELECT ON system.roles TO " + limitedUser,
		"GRANT SELECT ON system.role_grants TO " + limitedUser,
		"GRANT CREATE ROLE ON *.* TO " + limitedUser,
		"GRANT SELECT, dictGet, SHOW DICTIONARIES ON analytics.* TO " + limitedUser + " WITH GRANT OPTION",
	} {
```

**Step 4: Re-run**

Run: `bash scripts/go-test.sh ./internal/api/... -count=1 -run 'TestReconcileWarehouseStatementFailureMarksError|TestReconcileWarehouseProvisionsUsersAndRoles|TestReconcileWarehouseAuditsSkippedCatalogNames' -v`
Expected: PASS. (`requireClickHouseGrantExists` checks `access_type = 'SELECT'` rows, which the bundle still creates; the second reconcile stays a clean no-op because the bundle is fully present.)

**Step 5: Commit**

```bash
git add internal/api/warehouse_sync.go internal/api/warehouse_sync_test.go
git commit -m "feat(api): probe privilege mode and thread it through warehouse sync"
```

---

## Task 5: Integration — dictionary lifecycle and guard behavior (real ClickHouse)

**Files:**
- Modify: `internal/api/warehouse_rbac_e2e_test.go`
- Maybe modify: `internal/api/warehouse_sync_test.go` (only if assertions below break)

**Step 1: Write the failing test**

Add a dictionary to the RBAC fixture and a new end-to-end test. In `setupWarehouseRBACE2EFixture`, after the two `CREATE TABLE` statements (the dictionary source must be readable by the ClickHouse user the test connects with):

```go
	require.NoError(t, conn.Exec(ctx, "CREATE DICTIONARY "+quotedDB+".`d1` (id UInt8, label String) PRIMARY KEY id "+
		"SOURCE(CLICKHOUSE(DB '"+database+"' TABLE 't1' USER '"+cfg.User+"' PASSWORD '"+cfg.Password+"')) "+
		"LIFETIME(MIN 0 MAX 0) LAYOUT(FLAT())"))
```

Add the test (uses the existing helpers `createGrantViaAPI`, `grantBody`, `deleteGrantViaAPI`, `requireClickHouseDenied`, `fx.runAs`, `decodeExecuteOutputs`, `fx.table`):

```go
// TestWarehouseDictionaryGrantsE2E proves the read bundle makes granted
// dictionaries usable: direct SELECT, dictGet-family functions, and
// SHOW CREATE DICTIONARY all work for the managed identity, and revocation
// removes all of it. It skips when ClickHouse is unreachable.
func TestWarehouseDictionaryGrantsE2E(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseRBACE2EFixture(t)

	if mode, err := chaccess.DetectPrivilegeMode(ctx, fx.conn); err != nil || mode != chaccess.PrivilegeModeFull {
		t.Skipf("server does not support granular dictionary privileges (mode=%q err=%v)", mode, err)
	}

	// Grant only the dictionary to the group; t1 stays ungranted.
	rec := createGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID,
		grantBody("group", fx.groupID.String(), fx.database, "d1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var createdGrant struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &createdGrant))
	grantID, err := uuid.Parse(createdGrant.ID)
	require.NoError(t, err)
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	groupRole := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	var n uint64
	require.NoError(t, fx.conn.QueryRow(ctx, `
		SELECT count() FROM system.grants
		WHERE access_type = 'dictGet' AND database = ? AND table = 'd1'
		  AND role_name = ?`, fx.database, groupRole).Scan(&n))
	require.Equal(t, uint64(1), n, "the reconcile must grant dictGet on the dictionary")

	// Direct read, dictGet-family call, and SHOW CREATE all work as the
	// member's own identity.
	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT label FROM "+fx.table(t, "d1")+" WHERE id = 1")
	out := decodeExecuteOutputs(t, rec)
	require.Equal(t, "one", out.Outputs[0].Data.Rows[0][0])

	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT dictGet('"+fx.database+".d1', 'label', toUInt8(1)) AS label")
	out = decodeExecuteOutputs(t, rec)
	require.Equal(t, "one", out.Outputs[0].Data.Rows[0][0])

	rec = fx.runAs(t, fx.memberID, http.StatusOK,
		"SELECT dictHas('"+fx.database+".d1', toUInt8(1)) AS has_key")
	decodeExecuteOutputs(t, rec)

	fx.runAs(t, fx.memberID, http.StatusOK, "SHOW CREATE DICTIONARY "+fx.table(t, "d1"))

	// The dictionary is a data grant of its own: the source table stays
	// denied.
	rec = fx.runAs(t, fx.memberID, http.StatusForbidden,
		"SELECT count() FROM "+fx.table(t, "t1"))
	requireClickHouseDenied(t, rec)

	// Revocation through the API removes every privilege again.
	rec = deleteGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID, grantID)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))
	rec = fx.runAs(t, fx.memberID, http.StatusForbidden,
		"SELECT label FROM "+fx.table(t, "d1")+" WHERE id = 1")
	requireClickHouseDenied(t, rec)
}
```

Check `dictGet`'s argument shape stays qualified: `dictGet('db.dict', 'attribute', key)` — the dictionary name is one string, never separate database/table arguments.

Also add a guard test for the drift universe (same file, reusing the fixture):

```go
// TestWarehouseDictionaryGuardE2E proves the drift guard now manages
// dictGet: an outside-of-Aether grant on an ungranted object is revoked, and
// a wildcard dictionary grant still fails the warehouse closed.
func TestWarehouseDictionaryGuardE2E(t *testing.T) {
	ctx := context.Background()
	fx := setupWarehouseRBACE2EFixture(t)
	if mode, err := chaccess.DetectPrivilegeMode(ctx, fx.conn); err != nil || mode != chaccess.PrivilegeModeFull {
		t.Skipf("server does not support granular dictionary privileges (mode=%q err=%v)", mode, err)
	}

	rec := createGrantViaAPI(t, fx.s, fx.adminToken, fx.warehouseID,
		grantBody("group", fx.groupID.String(), fx.database, "t1"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	groupRole := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	quotedRole, err := chaccess.QuoteIdent(groupRole)
	require.NoError(t, err)

	// An extra dictGet on the ungranted dictionary is managed drift: the
	// next reconcile revokes it (same treatment as an extra SELECT).
	require.NoError(t, fx.conn.Exec(ctx,
		"GRANT dictGet ON "+fx.table(t, "d1")+" TO "+quotedRole))
	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))
	var n uint64
	require.NoError(t, fx.conn.QueryRow(ctx, `
		SELECT count() FROM system.grants
		WHERE access_type = 'dictGet' AND database = ? AND table = 'd1'
		  AND role_name = ?`, fx.database, groupRole).Scan(&n))
	require.Equal(t, uint64(0), n, "extra dictGet on an ungranted object must be revoked")

	// A wildcard dictGet is outside the model and fails closed.
	require.NoError(t, fx.conn.Exec(ctx,
		"GRANT dictGet ON "+fx.database+".* TO "+quotedRole))
	err = fx.s.reconcileWarehouse(ctx, fx.warehouseID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manual remediation")
	var status string
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		"SELECT sync_status FROM warehouses WHERE id = $1", fx.warehouseID.String()).Scan(&status))
	require.Equal(t, "error", status)
}
```

Note on the wildcard test: on 24.x (no `system.grants.is_wildcard`) the `db.*` dictGet row is detected by the empty-table branch in `buildActualFromGrantRows` — no `SHOW GRANTS` parsing needed for the db-level wildcard.

**Step 2: Run the new tests**

Run: `bash scripts/go-test.sh ./internal/api/... -count=1 -run 'TestWarehouseDictionary|TestWarehouseRBACE2E' -v`
Expected: FAIL initially if any wiring is off; `TestWarehouseRBACE2E` itself must PASS unchanged (its drift assertion relies on object-level reporting, satisfied by `fullBundleObjects`).

**Step 3: Run the whole warehouse suite and fix expectation drift**

Run: `bash scripts/go-test.sh ./internal/api/... -count=1 -run 'Warehouse|Drift|Grant' -v`
Known failure classes to fix (update expectations, never weaken intent):
- Assertions counting raw `system.grants` rows per subject may see `SELECT` rows still present (helpers filter `access_type = 'SELECT'`, fine) — only fix ones that count all access types.
- Any test asserting an exact first-failure statement index in `TestReconcileWarehouseStatementFailureMarksError`-style flows.
- Snapshot tests (`TestReconcileWarehouseHiddenPatternsSnapshot`) are unaffected (catalog unchanged).

**Step 4: Full API package**

Run: `task test:api`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/warehouse_rbac_e2e_test.go internal/api/warehouse_sync_test.go
git commit -m "test(api): dictionary read grants end to end"
```

---

## Task 6: Docs and full verification

**Files:**
- Modify: `AGENTS.md`
- Modify: `docs/plans/2026-10-13-dictionary-read-grants-design.md` (status line)
- Modify: `docs/plans/2026-10-13-dictionary-read-grants-plan.md` (this file, optional: check off)

**Step 1: Update `AGENTS.md`**

In the "**ClickHouse table permissions (warehouses)**" paragraph, extend the sentence that describes the grant model with:

> Every granted object receives a uniform read bundle — `SELECT, dictGet, SHOW DICTIONARIES` (inert on tables/views, required for dictionaries: modern ClickHouse denies `dictGet()`-family calls and direct dictionary reads without `dictGet`; `SHOW CREATE DICTIONARY` needs `SHOW DICTIONARIES`). The drift guard treats those three access types as the managed diff universe: extras are revoked like extra SELECTs; wildcards and grant options on any of them still fail closed. Servers without granular privileges (probed via `system.privileges`; `dictGet` absent) fall back to a `SELECT`-only legacy mode. Provisioners must be able to grant the bundle (use `GRANT SELECT, dictGet, SHOW DICTIONARIES ON <db>.* TO <provisioner> WITH GRANT OPTION` for least-privilege setups).

In the runbook sentence about provisioner rotation, add a note that rotating to a least-privilege provisioner now requires the bundle's grant option.

Update the design doc's status line to:

```markdown
**Status:** ✅ Implemented
```

**Step 2: Format, vet, and full test run**

Run: `task fmt && task vet && task tidy`
Expected: no diffs, no vet errors.

Run: `task check`
Expected: PASS (fmt + vet + tidy + all Go tests, ~3 min budget per package).

**Step 3: Frontend/relay untouched — confirm nothing else needs rebuilding**

No frontend or relay files changed; skip `npm` builds.

**Step 4: Commit**

```bash
git add AGENTS.md docs/plans/2026-10-13-dictionary-read-grants-design.md docs/plans/2026-10-13-dictionary-read-grants-plan.md
git commit -m "docs: document the managed read bundle for dictionaries"
```

---

## Task 7: Finish the branch

Follow the repo's `finishing-a-development-branch` flow: ensure `task check` is green, push `feat/dictionary-read-grants`, open a PR to `main` with a `feat:` squash title (e.g. `feat: dictionary read grants for managed ClickHouse warehouses`), and run the local PR checklist from `AGENTS.md` (`task check`; frontend/relay builds are unchanged but cheap to confirm).

**Release note:** this is a `feat:` — after merge, tag the next minor per the release process.

---

## Out of scope (do not build)

- Ambient `SHOW DICTIONARIES`/`system.dictionaries` grants (rejected: all-or-nothing listing exposes every dictionary to every user).
- `object_type` columns, grant-model renames, picker/inbox badges.
- Table functions, named collections, `system.query_log`-style grants.
- Frontend/E2E Playwright changes (no UI surface changed).
