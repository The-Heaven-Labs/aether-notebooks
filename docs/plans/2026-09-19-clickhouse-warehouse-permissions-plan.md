# ClickHouse Warehouse Table Permissions Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace shared-credential ClickHouse execution with per-person ClickHouse identities, group-level table grants enforced by ClickHouse RBAC, and explicit per-service routing for QoS/cost attribution.

**Architecture:** Aether is the policy source of truth. New `warehouses` group connectors that share a ClickHouse Cloud access namespace; table grants attach to warehouse + subject (user/group/everyone); a sync worker reconciles desired state into ClickHouse users/roles/grants through a designated provisioner connector; execution resolves the requesting user to a per-person ClickHouse identity and a routing-preferred service endpoint, connecting through a pooled per-(endpoint,user) connection.

**Tech Stack:** Go 1.x (`net/http` ServeMux, pgx, clickhouse-go/v2), PostgreSQL migrations, React + Vite + React Query + Vitest.

**Design doc:** `docs/plans/2026-09-19-clickhouse-warehouse-permissions-design.md`

**Conventions:** Tests hit a real database. Run `task infra:up` once, then targeted `go test` commands. Migrations are embedded and applied at startup (`internal/database/migrate.go`). Next migration numbers: `V107`, `V108`, `V109`, …

**Revision notes (post-review, apply to all tasks below):**
- Identities are warehouse-scoped **and warehouse-discriminated**:
  `UserIdent(warehouseID, orgID, userID)` → `aether_<wh8>_u_<hash>`,
  `RoleIdent(warehouseID, orgID, groupID)` → `aether_<wh8>_g_<hash>`, and
  `EveryoneRole(warehouseID)` → `aether_<wh8>_everyone`. `IdentifierPrefix(warehouseID)`
  returns `aether_<wh8>_` and scopes actual-state loading so two warehouses sharing one
  ClickHouse service cannot drop each other's entities. Golden values are pinned in
  `internal/chaccess/ident_test.go`; changing them requires re-provisioning every warehouse.
- `chaccess.Statements(d, a)` returns `(stmts []string, skipped []string)`. Catalog
  database/table names that fail `QuoteObjectIdent` are reported in `skipped` and audited as
  drift — never panic and never abort the whole sync for one odd table name. Actual-side
  identity names that fail quoting are likewise skipped (`tryQuoteIdent`).
- Password rotation is driven by `warehouses.applied_master_fp` (migration
  `V111__warehouse_master_fingerprint.sql`, added in Task 9), not by comparing ClickHouse
  password hashes (which are salted and unreadable). On fingerprint mismatch the reconcile
  emits `ALTER USER ... IDENTIFIED` for every provisioned user, then stores the new fingerprint.
- `ActualState.ForcePasswordReset` replaces the old `PasswordFingerprint` comparison. Actual
  state also carries `Wildcards` (subject + scope) and `UserActual.DefaultRolesAll`; a
  warehouse with wildcards fails closed in Task 9.
- **Task 5 as implemented (`885a2c02`)** differs from its snippet below in these ways (the code
  is the source of truth): `RoleState`/`UserState` have no `Ident` field (map keys are
  identity); `CREATE ROLE IF NOT EXISTS` is suppressed when the role exists in actual state;
  `SET DEFAULT ROLE ALL` is emitted only when desired roles are non-empty and either membership
  changed or `!actual.DefaultRolesAll`; `skipped` is sorted and deduplicated; empty passwords
  panic; `Fingerprint` is the full sha256 hex; `grantSet` lives in `ddl_test.go`.
- Settings profiles and quotas (readonly, max_execution_time, per-user concurrency) are
  **deferred to a follow-up change**; this implementation's boundary is ClickHouse grants.
  Documented in the design doc's out-of-scope section.
- Task 8's worker must wrap each `Reconcile` call in a `defer/recover`, converting any panic
  (e.g. a future desired-side invariant failure) into a sync error instead of crashing the API
  server process.
- Task 4's snippet below predates the warehouse-prefix refactor; `internal/chaccess/ident.go`
  and its tests are the source of truth for identity names.
- ClickHouse compatibility: the dev stack runs ClickHouse 24.x, whose `system.grants` has no
  `is_wildcard` column. Wildcard detection is version-agnostic (empty `table`, plus quote
  validation for table-prefix wildcards) and partial revokes (`is_partial_revoke = 1`) are
  excluded from actual state.
- **Task 7 as implemented (`2f52a710`)** detects prefix wildcards that older servers strip from
  `system.grants` (`db.events*` → `table='events'`): it probes `system.columns` for
  `is_wildcard`; when present it is used, when absent each namespace identity is checked with
  `SHOW GRANTS FOR` (any SELECT line containing `*` is a wildcard; false positives fail
  closed). Column-level grants are excluded (`coalesce(\`column\`, '') = ''`). `ActualState`
  also carries `Unexpected []string` for nested/foreign role grants and `WITH GRANT OPTION`
  rows.

---

## Phase 1 — Schema

### Task 1: Warehouse tables migration

**Files:**
- Create: `internal/database/migrations/V107__warehouses.sql`
- Modify: `internal/database/database_test.go` (add migration assertions)

**Step 1: Write the failing test**

Append to `internal/database/database_test.go`:

```go
func TestMigration107Warehouses(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	var exists bool
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='warehouses')`).Scan(&exists))
	require.True(t, exists, "warehouses table must exist")

	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='connectors' AND column_name='warehouse_id')`).Scan(&exists))
	require.True(t, exists, "connectors.warehouse_id must exist")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestMigration107Warehouses -v`
Expected: FAIL — `warehouses table must exist`.

**Step 3: Write the migration**

Create `internal/database/migrations/V107__warehouses.sql`:

```sql
-- Warehouses group connectors that share one ClickHouse access namespace
-- (ClickHouse Cloud: services sharing a warehouse share users, passwords,
-- roles, and grants). A connector with NULL warehouse_id behaves as a
-- warehouse of one. A standalone connector row is promoted by inserting a
-- warehouse and pointing the connector at it.
CREATE TABLE warehouses (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                   UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name                     TEXT NOT NULL,
    -- The connector whose stored credential is used for provisioning DDL.
    -- Must be a read-write, non-idling service. SET NULL if deleted so the
    -- warehouse becomes invalid until an admin picks a new provisioner.
    provisioner_connector_id UUID REFERENCES connectors(id) ON DELETE SET NULL,
    sync_status              TEXT NOT NULL DEFAULT 'pending'
                             CHECK (sync_status IN ('pending','syncing','ready','error')),
    sync_error               TEXT,
    last_synced_at           TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

ALTER TABLE connectors ADD COLUMN warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL;
CREATE INDEX idx_connectors_warehouse ON connectors (warehouse_id);
CREATE INDEX idx_warehouses_org ON warehouses (org_id);
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/database/ -run TestMigration107Warehouses -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/database/migrations/V107__warehouses.sql internal/database/database_test.go
git commit -m "feat(db): add warehouses and connector warehouse link"
```

---

### Task 2: Table grants migration

**Files:**
- Create: `internal/database/migrations/V108__warehouse_table_grants.sql`
- Modify: `internal/database/database_test.go`

**Step 1: Write the failing test**

```go
func TestMigration108WarehouseTableGrants(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	var exists bool
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='warehouse_table_grants')`).Scan(&exists))
	require.True(t, exists)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestMigration108WarehouseTableGrants -v`
Expected: FAIL.

**Step 3: Write the migration**

Create `internal/database/migrations/V108__warehouse_table_grants.sql`:

```sql
-- Allow-only table grants, mirroring acl_entries semantics (no deny entries).
-- subject_type 'everyone' pairs with subject_id 'everyone'; 'user'/'group'
-- pair with the entity UUID as text. Rows (not arrays) so the sync worker can
-- diff desired vs actual with simple set operations.
CREATE TABLE warehouse_table_grants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    warehouse_id  UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    subject_type  TEXT NOT NULL CHECK (subject_type IN ('user','group','everyone')),
    subject_id    TEXT NOT NULL,
    database_name TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (warehouse_id, subject_type, subject_id, database_name, table_name)
);

CREATE INDEX idx_wh_grants_subject ON warehouse_table_grants (warehouse_id, subject_type, subject_id);
CREATE INDEX idx_wh_grants_group ON warehouse_table_grants (subject_id) WHERE subject_type = 'group';
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/database/ -run TestMigration108WarehouseTableGrants -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/database/migrations/V108__warehouse_table_grants.sql internal/database/database_test.go
git commit -m "feat(db): add warehouse_table_grants"
```

---

### Task 3: Routing preference migration

**Files:**
- Create: `internal/database/migrations/V109__warehouse_service_preferences.sql`
- Modify: `internal/database/database_test.go`

**Step 1: Write the failing test**

```go
func TestMigration109ServicePreferences(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	var exists bool
	require.NoError(t, db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='warehouse_service_preferences')`).Scan(&exists))
	require.True(t, exists)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestMigration109ServicePreferences -v`
Expected: FAIL.

**Step 3: Write the migration**

Create `internal/database/migrations/V109__warehouse_service_preferences.sql`:

```sql
-- User-level routing preference: which explicitly granted service in a
-- warehouse this user's queries run on. Deleted with the connector so a
-- preference can never point at a removed endpoint.
CREATE TABLE warehouse_service_preferences (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    connector_id UUID NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, warehouse_id)
);
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/database/ -run TestMigration109ServicePreferences -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/database/migrations/V109__warehouse_service_preferences.sql internal/database/database_test.go
git commit -m "feat(db): add warehouse_service_preferences"
```

---

## Phase 2 — ClickHouse access engine (`internal/chaccess`)

New package with no DB/HTTP dependencies so it is fully unit-testable.

### Task 4: Identifier and password derivation

**Files:**
- Create: `internal/chaccess/ident.go`
- Test: `internal/chaccess/ident_test.go`

**Step 1: Write the failing test**

```go
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
	require.Equal(t, RoleIdent(wh, org, group), RoleIdent(wh, org, group))
	require.True(t, strings.HasPrefix(RoleIdent(wh, org, group), "aether_g_"))
	require.NotEqual(t, RoleIdent(wh, org, group), RoleIdent(wh, org, uuid.New()))
	require.NotEqual(t, RoleIdent(wh, org, group), RoleIdent(uuid.New(), org, group))
}

func TestDerivePasswordDeterministicAndComplex(t *testing.T) {
	master := []byte("0123456789abcdef0123456789abcdef")
	wh, user := uuid.New(), uuid.New()
	p1 := DerivePassword(master, wh, user)
	p2 := DerivePassword(master, wh, user)
	require.Equal(t, p1, p2)
	require.GreaterOrEqual(t, len(p1), 12)
	require.Contains(t, p1, "A") // guaranteed uppercase
	require.Contains(t, p1, "1") // guaranteed digit
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/chaccess/ -v`
Expected: FAIL — package does not exist.

**Step 3: Write the implementation**

Create `internal/chaccess/ident.go`:

```go
// Package chaccess derives ClickHouse access identities and computes the
// desired ClickHouse user/role/grant state for an Aether warehouse.
package chaccess

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

// EveryoneRole is granted to every provisioned user in a warehouse.
const EveryoneRole = "aether_everyone"

const (
	userPrefix = "aether_u_"
	rolePrefix = "aether_g_"
)

var (
	identRe       = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	objectIdentRe = regexp.MustCompile(`^[A-Za-z0-9_$][A-Za-z0-9_$.-]{0,126}$`)
)

func hexID(ids ...uuid.UUID) string {
	h := sha256.New()
	for _, id := range ids {
		h.Write(id[:])
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// UserIdent returns the ClickHouse username for an Aether user in a warehouse.
func UserIdent(warehouseID, orgID, userID uuid.UUID) string {
	return userPrefix + hexID(warehouseID, orgID, userID)
}

// RoleIdent returns the ClickHouse role name for an Aether group.
func RoleIdent(warehouseID, orgID, groupID uuid.UUID) string {
	return rolePrefix + hexID(warehouseID, orgID, groupID)
}

// DerivePassword deterministically derives a ClickHouse password from the
// master key so no per-user secret is stored at rest. The fixed prefix
// guarantees Cloud password complexity (uppercase + digit).
func DerivePassword(masterKey []byte, warehouseID, userID uuid.UUID) string {
	info := append([]byte("aether-ch-pw/v1:"+warehouseID.String()+":"), userID[:]...)
	key, err := hkdf.Key(sha256.New, masterKey, nil, string(info), 24)
	if err != nil {
		panic(err) // only fails on invalid hash/key parameters
	}
	return "Ae1_" + base64.RawURLEncoding.EncodeToString(key)
}

// QuoteIdent validates and backtick-quotes a generated ClickHouse identity
// name (user/role). These are always Aether-generated, so the charset is
// strict.
func QuoteIdent(name string) (string, error) {
	if !identRe.MatchString(name) {
		return "", fmt.Errorf("invalid clickhouse identifier %q", name)
	}
	return "`" + name + "`", nil
}

// QuoteObjectIdent validates and backtick-quotes a ClickHouse database/table
// name. Names come from the live catalog and may contain uppercase, dots,
// hyphens, or dollar signs; anything that could break out of backtick quoting
// (backtick, backslash, whitespace, control chars) is rejected.
func QuoteObjectIdent(name string) (string, error) {
	if !objectIdentRe.MatchString(name) {
		return "", fmt.Errorf("invalid clickhouse object name %q", name)
	}
	return "`" + name + "`", nil
}
```

Note: `crypto/hkdf` requires Go 1.24+. If the toolchain is older, use
`golang.org/x/crypto/hkdf` with `hkdf.New`. Check `go.mod` and pick the
import that compiles; keep `DerivePassword` behavior identical.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/chaccess/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chaccess/
git commit -m "feat(chaccess): add identity derivation and identifier quoting"
```

---

### Task 5: Desired state and DDL statement generation

**Files:**
- Create: `internal/chaccess/desired.go`
- Create: `internal/chaccess/ddl.go`
- Test: `internal/chaccess/ddl_test.go`

**Step 1: Write the failing test**

```go
package chaccess

import (
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
```

(Add `"strings"` import.)

**Step 2: Run test to verify it fails**

Run: `go test ./internal/chaccess/ -run TestStatements -v`
Expected: FAIL — undefined `DesiredState`.

**Step 3: Write the implementation**

Create `internal/chaccess/desired.go`:

```go
package chaccess

// Grant is a single table-level SELECT grant.
type Grant struct {
	Database string
	Table    string
}

// RoleState is the desired grant set for one group role.
type RoleState struct {
	Grants map[Grant]struct{}
}

// UserState is the desired state for one Aether user in a warehouse.
type UserState struct {
	Password     string
	Roles        []string
	DirectGrants map[Grant]struct{}
}

// DesiredState is the full desired ClickHouse access state for a warehouse.
type DesiredState struct {
	Roles map[string]RoleState
	Users map[string]UserState
}

// UserActual is the observed state of one ClickHouse user.
type UserActual struct {
	Roles           map[string]struct{}
	DirectGrants    map[Grant]struct{}
	DefaultRolesAll bool
}

// WildcardGrant is an unexpected ClickHouse wildcard grant. Aether never
// creates these; they defeat table-level least privilege and make a warehouse
// fail closed (Task 9).
type WildcardGrant struct {
	Subject string // user or role name
	Scope   string // "db.*" or "*.*"
}

// ActualState is the observed ClickHouse access state. ForcePasswordReset is
// set by the caller when warehouses.applied_master_fp differs from the current
// master-key fingerprint (ClickHouse password hashes are salted and cannot be
// compared).
type ActualState struct {
	Roles              map[string]map[Grant]struct{}
	Users              map[string]UserActual
	Wildcards          []WildcardGrant
	ForcePasswordReset bool
}

// HasWildcard reports whether any unexpected wildcard grant exists.
func (a ActualState) HasWildcard() bool { return len(a.Wildcards) > 0 }
```

Create `internal/chaccess/ddl.go`:

```go
package chaccess

import (
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
```

Add `crypto/sha256`, `encoding/hex` imports to `ddl.go`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/chaccess/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chaccess/
git commit -m "feat(chaccess): add desired state and DDL diff generation"
```

---

### Task 6: Desired state computation from Aether data

**Files:**
- Create: `internal/chaccess/compute.go`
- Test: `internal/chaccess/compute_test.go`

**Step 1: Write the failing test**

```go
package chaccess

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestComputeUnionsGroupsEveryoneAndDirect(t *testing.T) {
	org, wh, u1, g1, g2 := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()

	grants := []SubjectGrant{
		{SubjectType: "user", SubjectID: u1.String(), Database: "db", Table: "u_only"},
		{SubjectType: "group", SubjectID: g1.String(), Database: "db", Table: "g1_a"},
		{SubjectType: "group", SubjectID: g2.String(), Database: "db", Table: "g2_a"},
		{SubjectType: "everyone", SubjectID: "everyone", Database: "db", Table: "public"},
	}
	memberships := map[uuid.UUID][]uuid.UUID{u1: {g1}}
	users := []UserSpec{{ID: u1, Email: "u1@x.com"}}
	master := []byte("0123456789abcdef0123456789abcdef")

	d := Compute(org, wh, master, grants, memberships, users)

	require.Contains(t, d.Roles, RoleIdent(wh, org, g1))
	require.Contains(t, d.Roles, RoleIdent(wh, org, g2))
	require.Contains(t, d.Roles, EveryoneRole(wh))
	require.NotContains(t, d.Roles, RoleIdent(wh, org, uuid.New()))

	ust := d.Users[UserIdent(wh, org, u1)]
	require.Equal(t, []string{EveryoneRole(wh), RoleIdent(wh, org, g1)}, ust.Roles)
	require.Contains(t, ust.DirectGrants, Grant{Database: "db", Table: "u_only"})
	require.Equal(t, DerivePassword(master, wh, u1), ust.Password)
}

func TestComputeSkipsUserWithNoEffectiveGrants(t *testing.T) {
	org, wh, u1 := uuid.New(), uuid.New(), uuid.New()
	d := Compute(org, wh, []byte("k"), nil, map[uuid.UUID][]uuid.UUID{u1: {}}, []UserSpec{{ID: u1}})
	require.NotContains(t, d.Users, UserIdent(wh, org, u1))
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/chaccess/ -run TestCompute -v`
Expected: FAIL — undefined `Compute`.

**Step 3: Write the implementation**

Create `internal/chaccess/compute.go`:

```go
package chaccess

import "github.com/google/uuid"

// SubjectGrant is one row of warehouse_table_grants.
type SubjectGrant struct {
	SubjectType string // user | group | everyone
	SubjectID   string
	Database    string
	Table       string
}

// UserSpec is a user who may be provisioned in a warehouse.
type UserSpec struct {
	ID    uuid.UUID
	Email string
}

// Compute builds the desired ClickHouse state for a warehouse.
//
// - One role per group with at least one grant.
// - Everyone grants go to the warehouse's EveryoneRole(warehouseID) role.
// - A user is provisioned only when their union (direct + groups + everyone)
//   is non-empty.
// - A user's default roles are the roles of their granting groups plus
//   EveryoneRole(warehouseID) when applicable.
func Compute(
	orgID, warehouseID uuid.UUID,
	masterKey []byte,
	grants []SubjectGrant,
	memberships map[uuid.UUID][]uuid.UUID,
	users []UserSpec,
) DesiredState {
	roleGrants := map[string]map[Grant]struct{}{}
	userDirect := map[string]map[Grant]struct{}{}
	everyone := map[Grant]struct{}{}

	for _, gr := range grants {
		key := Grant{Database: gr.Database, Table: gr.Table}
		switch gr.SubjectType {
		case "group":
			gid, err := uuid.Parse(gr.SubjectID)
			if err != nil {
				continue
			}
			ident := RoleIdent(warehouseID, orgID, gid)
			if roleGrants[ident] == nil {
				roleGrants[ident] = map[Grant]struct{}{}
			}
			roleGrants[ident][key] = struct{}{}
		case "user":
			if userDirect[gr.SubjectID] == nil {
				userDirect[gr.SubjectID] = map[Grant]struct{}{}
			}
			userDirect[gr.SubjectID][key] = struct{}{}
		case "everyone":
			everyone[key] = struct{}{}
		}
	}

	roles := map[string]RoleState{}
	for ident, gs := range roleGrants {
		roles[ident] = RoleState{Grants: gs}
	}
	if len(everyone) > 0 {
		roles[EveryoneRole(warehouseID)] = RoleState{Grants: everyone}
	}

	usersOut := map[string]UserState{}
	for _, u := range users {
		var userRoles []string
		if len(everyone) > 0 {
			userRoles = append(userRoles, EveryoneRole(warehouseID))
		}
		for _, gid := range memberships[u.ID] {
			ident := RoleIdent(warehouseID, orgID, gid)
			if _, ok := roles[ident]; ok {
				userRoles = append(userRoles, ident)
			}
		}
		direct := userDirect[u.ID.String()]
		if len(userRoles) == 0 && len(direct) == 0 {
			continue // no effective grants: do not provision
		}
		pw := DerivePassword(masterKey, warehouseID, u.ID)
		usersOut[UserIdent(warehouseID, orgID, u.ID)] = UserState{
			Password:     pw,
			Roles:        sortedStrings(userRoles),
			DirectGrants: direct,
		}
	}

	return DesiredState{Roles: roles, Users: usersOut}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/chaccess/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chaccess/
git commit -m "feat(chaccess): compute desired state from grants and memberships"
```

---

### Task 7: Introspect actual ClickHouse state

**Files:**
- Create: `internal/chaccess/introspect.go`
- Test: `internal/chaccess/introspect_test.go` (pure parsing helpers only)

**Step 1: Write the failing test**

```go
func TestParseGrantRows(t *testing.T) {
	rows := []GrantRow{
		{UserName: "aether_u_x", AccessType: "SELECT", Database: "db", Table: "t1"},
		{UserName: "aether_u_x", AccessType: "SELECT", Database: "db", Table: "t2"},
	}
	users := map[string]UserActual{"aether_u_x": {}}
	a := buildActualFromGrantRows(rows, nil, users)
	require.Contains(t, a.Users["aether_u_x"].DirectGrants, Grant{"db", "t1"})
	require.NotContains(t, a.Users["aether_u_x"].Roles, "aether_g_y")
}

func TestParseWildcardRowsFlagged(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", AccessType: "SELECT", Database: "db", Table: ""},
	}
	a := buildActualFromGrantRows(rows, nil, nil)
	require.True(t, a.HasWildcard())
	require.Equal(t, WildcardGrant{Subject: "aether_wh_g_x", Scope: "db.*"}, a.Wildcards[0])
}

func TestParsePartialRevokeAndTablePrefixWildcards(t *testing.T) {
	rows := []GrantRow{
		{RoleName: "aether_wh_g_x", AccessType: "SELECT", Database: "db", Table: "revoked", IsPartialRevoke: 1},
		{RoleName: "aether_wh_g_x", AccessType: "SELECT", Database: "db", Table: "events*"},
	}
	a := buildActualFromGrantRows(rows, nil, nil)
	require.Empty(t, a.Roles["aether_wh_g_x"], "partial revokes and prefix wildcards are not table grants")
	require.True(t, a.HasWildcard())
	require.Equal(t, "db.events*", a.Wildcards[0].Scope)
}

func TestParseZeroGrantRolesAreLoaded(t *testing.T) {
	roles := map[string]map[Grant]struct{}{"aether_wh_g_empty": {}}
	a := buildActualFromGrantRows(nil, roles, nil)
	require.Contains(t, a.Roles, "aether_wh_g_empty")
	require.Empty(t, a.Roles["aether_wh_g_empty"])
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/chaccess/ -run TestParse -v`
Expected: FAIL.

**Step 3: Write the implementation**

Create `internal/chaccess/introspect.go`:

```go
package chaccess

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// GrantRow is one row of system.grants. is_wildcard does not exist before
// ClickHouse 26.x, so wildcard detection uses the empty-table signal plus
// quote validation (see buildActualFromGrantRows).
type GrantRow struct {
	UserName        string
	RoleName        string
	AccessType      string
	Database        string
	Table           string
	IsPartialRevoke uint8
}

// LoadActual reads users, roles, role grants, and direct grants for one
// warehouse's namespace. The namespace is selected with startsWith on
// IdentifierPrefix(warehouseID) rather than LIKE, so underscores in the
// prefix are never treated as wildcards.
//
// Roles are enumerated from system.roles (not only from grant rows) so a
// zero-grant orphan role is visible and can be dropped.
//
// It does NOT read password hashes: ClickHouse stores salted hashes that
// cannot be compared. Password rotation is driven by
// warehouses.applied_master_fp (see Task 9) via ActualState.ForcePasswordReset.
func LoadActual(ctx context.Context, conn clickhouse.Conn, warehouseID uuid.UUID) (ActualState, error) {
	prefix := IdentifierPrefix(warehouseID)

	var grants []GrantRow
	rows, err := conn.Query(ctx, `
		SELECT coalesce(user_name,''), coalesce(role_name,''), access_type, coalesce(database,''), coalesce(table,''), is_partial_revoke
		FROM system.grants
		WHERE (startsWith(user_name, ?) OR startsWith(role_name, ?))
		  AND access_type = 'SELECT' AND is_partial_revoke = 0`,
		prefix, prefix)
	if err != nil {
		return ActualState{}, fmt.Errorf("system.grants: %w", err)
	}
	for rows.Next() {
		var r GrantRow
		if err := rows.Scan(&r.UserName, &r.RoleName, &r.AccessType, &r.Database, &r.Table, &r.IsPartialRevoke); err != nil {
			return ActualState{}, fmt.Errorf("scan grant: %w", err)
		}
		grants = append(grants, r)
	}
	if err := rows.Err(); err != nil {
		return ActualState{}, err
	}

	roles := map[string]map[Grant]struct{}{}
	rrows, err := conn.Query(ctx, `SELECT name FROM system.roles WHERE startsWith(name, ?)`, prefix)
	if err != nil {
		return ActualState{}, fmt.Errorf("system.roles: %w", err)
	}
	for rrows.Next() {
		var name string
		if err := rrows.Scan(&name); err != nil {
			return ActualState{}, err
		}
		roles[name] = map[Grant]struct{}{}
	}
	if err := rrows.Err(); err != nil {
		return ActualState{}, err
	}

	memberships := map[string]map[string]struct{}{}
	mrows, err := conn.Query(ctx, `
		SELECT user_name, granted_role_name FROM system.role_grants
		WHERE startsWith(user_name, ?)`, prefix)
	if err != nil {
		return ActualState{}, fmt.Errorf("system.role_grants: %w", err)
	}
	for mrows.Next() {
		var user, role string
		if err := mrows.Scan(&user, &role); err != nil {
			return ActualState{}, err
		}
		if memberships[user] == nil {
			memberships[user] = map[string]struct{}{}
		}
		memberships[user][role] = struct{}{}
	}
	if err := mrows.Err(); err != nil {
		return ActualState{}, err
	}

	users := map[string]UserActual{}
	urows, err := conn.Query(ctx,
		`SELECT name, default_roles_all FROM system.users WHERE startsWith(name, ?)`, prefix)
	if err != nil {
		return ActualState{}, fmt.Errorf("system.users: %w", err)
	}
	for urows.Next() {
		var name string
		var defaultRolesAll uint8
		if err := urows.Scan(&name, &defaultRolesAll); err != nil {
			return ActualState{}, err
		}
		users[name] = UserActual{Roles: memberships[name], DefaultRolesAll: defaultRolesAll == 1}
	}
	if err := urows.Err(); err != nil {
		return ActualState{}, err
	}

	return buildActualFromGrantRows(grants, roles, users), nil
}

func buildActualFromGrantRows(grants []GrantRow, roles map[string]map[Grant]struct{}, users map[string]UserActual) ActualState {
	a := ActualState{Roles: map[string]map[Grant]struct{}{}, Users: map[string]UserActual{}}
	for name, gs := range roles {
		if gs == nil {
			gs = map[Grant]struct{}{}
		}
		a.Roles[name] = gs
	}
	for k, v := range users {
		if v.Roles == nil {
			v.Roles = map[string]struct{}{}
		}
		if v.DirectGrants == nil {
			v.DirectGrants = map[Grant]struct{}{}
		}
		a.Users[k] = v
	}
	for _, r := range grants {
		if r.IsPartialRevoke == 1 {
			continue
		}
		subject := r.RoleName
		if subject == "" {
			subject = r.UserName
		}
		// Version-agnostic wildcard detection (no is_wildcard column before
		// ClickHouse 26.x): an empty table means a database/global wildcard.
		// Table-prefix wildcards (e.g. db.events*) arrive as a non-empty
		// table that cannot be quoted; treat those as wildcards too so
		// reconcile fails closed instead of silently ignoring them.
		if r.Table == "" {
			scope := "*.*"
			if r.Database != "" {
				scope = r.Database + ".*"
			}
			a.Wildcards = append(a.Wildcards, WildcardGrant{Subject: subject, Scope: scope})
			continue
		}
		if _, err := QuoteObjectIdent(r.Database); err != nil {
			a.Wildcards = append(a.Wildcards, WildcardGrant{Subject: subject, Scope: r.Database + "." + r.Table})
			continue
		}
		if _, err := QuoteObjectIdent(r.Table); err != nil {
			a.Wildcards = append(a.Wildcards, WildcardGrant{Subject: subject, Scope: r.Database + "." + r.Table})
			continue
		}
		gr := Grant{Database: r.Database, Table: r.Table}
		if r.RoleName != "" {
			if a.Roles[r.RoleName] == nil {
				a.Roles[r.RoleName] = map[Grant]struct{}{}
			}
			a.Roles[r.RoleName][gr] = struct{}{}
		} else if r.UserName != "" {
			u := a.Users[r.UserName]
			if u.DirectGrants == nil {
				u.DirectGrants = map[Grant]struct{}{}
			}
			u.DirectGrants[gr] = struct{}{}
			a.Users[r.UserName] = u
		}
	}
	return a
}
```

`WildcardGrant`, `ActualState.Wildcards`, `ActualState.HasWildcard()` and
`UserActual.DefaultRolesAll` are defined in `desired.go` (Task 5 revision).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/chaccess/ -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chaccess/
git commit -m "feat(chaccess): introspect actual clickhouse access state"
```

---

## Phase 3 — Sync worker and triggers

### Task 8: Warehouse sync service

**Files:**
- Create: `internal/chaccess/sync.go`
- Test: `internal/chaccess/sync_test.go`

**Step 1: Write the failing test**

```go
func TestSyncDebouncesAndRunsOnce(t *testing.T) {
	var runs int
	s := NewSyncService(SyncConfig{
		Debounce:  10 * time.Millisecond,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error { runs++; return nil },
		Logger:    slog.Default(),
	})
	wh := uuid.New()
	for i := 0; i < 5; i++ {
		s.Enqueue(wh)
	}
	s.Close()
	require.Equal(t, 1, runs)
}

func TestSyncRetriesWithBackoff(t *testing.T) {
	var attempts int
	s := NewSyncService(SyncConfig{
		Debounce:  time.Millisecond,
		MaxRetries: 3,
		Reconcile: func(ctx context.Context, warehouseID uuid.UUID) error {
			attempts++
			return errors.New("boom")
		},
		Logger: slog.Default(),
	})
	s.Enqueue(uuid.New())
	s.Close()
	require.Equal(t, 3, attempts)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/chaccess/ -run TestSync -v`
Expected: FAIL.

**Step 3: Write the implementation**

Create `internal/chaccess/sync.go`:

```go
package chaccess

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SyncConfig wires the worker to the environment.
type SyncConfig struct {
	Debounce   time.Duration
	MaxRetries int
	Reconcile  func(ctx context.Context, warehouseID uuid.UUID) error
	Logger     *slog.Logger
}

// SyncService coalesces sync requests per warehouse and runs a single
// reconciliation at a time per warehouse. Close waits for in-flight work.
type SyncService struct {
	cfg    SyncConfig
	mu     sync.Mutex
	queued map[uuid.UUID]bool
	active map[uuid.UUID]bool
	wg     sync.WaitGroup
	closed bool
}

func NewSyncService(cfg SyncConfig) *SyncService {
	if cfg.Debounce <= 0 {
		cfg.Debounce = 2 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	return &SyncService{cfg: cfg, queued: map[uuid.UUID]bool{}, active: map[uuid.UUID]bool{}}
}

// Enqueue schedules a warehouse sync, coalescing bursts.
func (s *SyncService) Enqueue(warehouseID uuid.UUID) {
	s.mu.Lock()
	if s.closed || s.queued[warehouseID] || s.active[warehouseID] {
		s.mu.Unlock()
		return
	}
	s.queued[warehouseID] = true
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		time.Sleep(s.cfg.Debounce)

		s.mu.Lock()
		delete(s.queued, warehouseID)
		if s.closed { // Close() drains queued work by running it
			s.mu.Unlock()
			return
		}
		s.active[warehouseID] = true
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			delete(s.active, warehouseID)
			s.mu.Unlock()
		}()

		var err error
		for attempt := 0; attempt < s.cfg.MaxRetries; attempt++ {
			err = s.cfg.Reconcile(context.Background(), warehouseID)
			if err == nil {
				return
			}
			s.cfg.Logger.Warn("warehouse sync failed", "warehouse_id", warehouseID, "attempt", attempt+1, "error", err)
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		}
		s.cfg.Logger.Error("warehouse sync giving up", "warehouse_id", warehouseID, "error", err)
	}()
}

// Close waits for queued and active work to finish.
func (s *SyncService) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.wg.Wait()
}
```

Note: the test calls `s.Close()` immediately after `Enqueue`; the goroutine sleeps
`Debounce` then runs. `Close` waits on `wg`, which includes the debounce sleep, so
both tests are deterministic with small debounce values.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/chaccess/ -run TestSync -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/chaccess/
git commit -m "feat(chaccess): add per-warehouse sync service"
```

---

### Task 9: Reconcile function (DB + provisioner execution + audit)

**Files:**
- Create: `internal/api/warehouse_sync.go`
- Create: `internal/database/migrations/V111__warehouse_master_fingerprint.sql`
  (`ALTER TABLE warehouses ADD COLUMN applied_master_fp TEXT;`)
- Test: `internal/api/warehouse_sync_test.go`

**Step 1: Write the failing test**

Tests hit a real DB and a real ClickHouse (dev stack includes one). Create a
warehouse with one connector pointing at the dev ClickHouse, grants for one
user and one group, run `reconcileWarehouse`, then assert:

```go
func TestReconcileWarehouseProvisionsUsersAndRoles(t *testing.T) {
	s, orgID, userID, whID := setupWarehouseFixture(t)
	// fixture: warehouse + clickhouse connector + group + membership + grants
	require.NoError(t, s.reconcileWarehouse(context.Background(), whID))

	var n int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM warehouses WHERE id=$1 AND sync_status='ready'`, whID).Scan(&n))
	require.Equal(t, 1, n)

	// verify via provisioner connection explicitly (not the pool manager)
	conn := dialProvisioner(t, s, whID)
	requireUserExists(t, conn, chaccess.UserIdent(warehouseID, orgID, userID))
}
```

Implement helpers in the test file:
- `setupWarehouseFixture` creates org/user/group/membership/connector/warehouse/grants.
- `dialProvisioner` decrypts connector config and opens a ClickHouse connection.
- `requireUserExists` queries `system.users`.

**Step 2: Run test to verify it fails**

Run: `task infra:up && go test ./internal/api/ -run TestReconcileWarehouse -v`
Expected: FAIL — `s.reconcileWarehouse undefined`.

**Step 3: Write the implementation**

Create `internal/api/warehouse_sync.go` with:

```go
// reconcileWarehouse computes desired ClickHouse access state for a warehouse
// and applies it through the provisioner connector. Idempotent.
func (s *Server) reconcileWarehouse(ctx context.Context, warehouseID uuid.UUID) error {
	// 1. Load warehouse + org + provisioner connector row (filter connectors.deleted_at IS NULL).
	// 2. Decrypt provisioner config, open a ClickHouse connection (short-lived,
	//    NOT through the user pool manager).
	// 3. Load warehouse_table_grants.
	// 4. Load group memberships for all org members.
	// 5. Load org users that have any effective grant (users in granting groups,
	//    direct-grant users, everyone covers all org members).
	// 6. desired := chaccess.Compute(...)
	// 7. actual, err := chaccess.LoadActual(ctx, conn, warehouseID).
	//    If actual.HasWildcard() || len(actual.Unexpected) > 0 -> set
	//    sync_status='error' with sync_error listing wildcards (subject+scope)
	//    and unexpected entries, then return (fail closed; never mark ready
	//    while wildcards or unexpected role wiring / grant options exist).
	//    Manual remediation only; not auto-healed in v1.
	//    actual.ForcePasswordReset = (warehouse.applied_master_fp != chaccess.Fingerprint(derivedMasterKey))
	// 8. stmts, skipped := chaccess.Statements(desired, actual); audit skipped as warehouse.drift.
	//    for _, stmt := range stmts { exec; on error -> mark error + return }
	// 9. mark sync_status='ready', last_synced_at=now(), applied_master_fp=current fp.
	// 10. audit "warehouse.sync" with counts.
	// Wrap all DB mutations in setWarehouseSyncStatus(status, errorText).
}
```

Requirements:
- Org scoping is enforced at load time: users come from
  `org_members JOIN users` for the warehouse's org, and memberships only for
  those users. A subject ID that does not belong to the org must never be
  provisioned (defense in depth against any write-path gap).
- Statements execute sequentially; the first error aborts and sets
  `sync_status='error'` with `sync_error`.
- Skipped (unquotable) catalog names are audited as `warehouse.drift` and do
  not abort the sync.
- Master-key rotation self-heals: when `applied_master_fp` differs from the
  current fingerprint, `ForcePasswordReset` makes the plan emit
  `ALTER USER ... IDENTIFIED` for every existing provisioned user; the new
  fingerprint is stored only after all statements succeed.
- Auditing uses the existing audit logger (`s.audit`), action `warehouse.sync`,
  with `warehouse_id`, `statements`, `users`, `roles`.
- The provisioner must be RW; if the connector has no warehouse or no
  provisioner is designated, return a descriptive error.
- Soft-deleted connectors are never used as provisioners (`deleted_at IS NULL`).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestReconcileWarehouse -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/warehouse_sync.go internal/api/warehouse_sync_test.go
git commit -m "feat(api): reconcile warehouse clickhouse access state"
```

---

### Task 10: Wire sync triggers

**Files:**
- Modify: `internal/api/pending_group_membership.go` (after successful apply, enqueue affected warehouses)
- Modify: `internal/api/group_handlers.go` (membership add/remove)
- Modify: `internal/api/router.go` (construct `SyncService` on `Server`)
- Modify: `internal/api/server.go` (add field, start/stop worker)

**Step 1: Write the failing test**

```go
func TestMembershipChangeEnqueuesWarehouseSync(t *testing.T) {
	s, orgID := setupTestServer(t), uuid.New()
	whID, groupID, userID := seedWarehouseGroupUser(t, s, orgID)
	// stub the sync service with a recording enqueue
	rec := &recordingSync{}
	s.warehouseSync = rec

	// call the membership handler through the router (POST /groups/{id}/members)
	... assert rec.enqueued contains whID
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestMembershipChangeEnqueues -v`
Expected: FAIL.

**Step 3: Implement**

- Define an interface on `Server`:

```go
type warehouseSyncer interface{ Enqueue(uuid.UUID) }
```

- `Server.warehouseSync` is set in `NewServer` to `chaccess.NewSyncService(...)`
  whose `Reconcile` calls `s.reconcileWarehouse`.
- Add helper `func (s *Server) enqueueWarehouseSyncForOrg(ctx, orgID)` that
  enqueues every warehouse in the org (cheap; debounced).
- Add a finer helper `enqueueWarehouseSyncForGroup(ctx, groupID)` and
  `enqueueWarehouseSyncForUser(ctx, userID)` resolving affected warehouses via
  `warehouse_table_grants` and `connectors.warehouse_id`.
- Call them after: group membership add/remove, pending group materialization,
  group delete, user org join/leave, user delete.

**Step 4: Run tests**

Run: `go test ./internal/api/ -run 'TestMembershipChangeEnqueues|TestApplyPendingGroups' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat(api): trigger warehouse sync on membership changes"
```

---

### Task 11: Reconciliation loop and drift alerts

**Files:**
- Modify: `internal/api/warehouse_sync.go`
- Modify: `cmd/aether-server/main.go` (start loop)
- Modify: `internal/config/config.go` (`AETHER_CH_RECONCILE_INTERVAL`, default `10m`)

**Step 1: Write the failing test**

```go
func TestDriftDetectionAlertsOnUnexpectedGrant(t *testing.T) {
	s, whID := setupWarehouseReady(t)
	// manually add an unexpected grant in ClickHouse as provisioner
	execAsProvisioner(t, s, whID, "GRANT SELECT ON `db`.`secret` TO `aether_u_x`")
	// run drift check
	drift, err := s.detectWarehouseDrift(context.Background(), whID)
	require.NoError(t, err)
	require.Contains(t, drift.UnexpectedGrants, "db.secret")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestDriftDetection -v`
Expected: FAIL.

**Step 3: Implement**

- `detectWarehouseDrift(ctx, whID) (DriftReport, error)`: compares desired
  vs actual and reports unexpected grants/users/roles, missing grants,
  `Wildcards` (subject + scope), `Unexpected` entries, users whose
  `DefaultRolesAll` is false while roles exist, and users present in the
  warehouse's `IdentifierPrefix` namespace but absent from desired state.
- Audit `warehouse.drift` for non-empty reports; reconciliation loop enqueues
  sync for warehouses with drift.
- Loop: ticker at `AETHER_CH_RECONCILE_INTERVAL`, enqueue all warehouses,
  started from `main.go` and stopped on shutdown.

**Step 4: Run tests**

Run: `go test ./internal/api/ -run TestDriftDetection -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/ internal/config/ cmd/aether-server/main.go
git commit -m "feat(api): warehouse drift detection and reconcile loop"
```

---

## Phase 4 — Execution identity, routing, and pools

### Task 12: Per-user connection pool manager

**Files:**
- Create: `internal/executor/pool.go`
- Test: `internal/executor/pool_test.go`

**Step 1: Write the failing test**

```go
func TestPoolReusesConnPerUserAndEvictsLRU(t *testing.T) {
	var opened int
	p := NewConnPool(PoolConfig{
		MaxPools:  2,
		IdleTTL:   time.Minute,
		PerUserMaxOpen: 1,
		Open: func(cfg models.ConnectorConfig) (clickhouse.Conn, error) {
			opened++
			return &fakeConn{}, nil
		},
	})
	cfg := models.ConnectorConfig{Host: "h", Port: 9000, User: "u1"}
	c1, _ := p.Get("ep1:9000", "u1", cfg)
	c2, _ := p.Get("ep1:9000", "u1", cfg)
	require.Equal(t, 1, opened, "same key must reuse")
	require.Same(t, c1, c2)
	p.Get("ep1:9000", "u2", cfg)
	p.Get("ep1:9000", "u3", cfg) // exceeds MaxPools, evicts LRU
	require.Equal(t, 3, opened)
	require.Equal(t, 2, p.Len())
}

func TestPoolInvalidateClosesAndReopens(t *testing.T) {
	... call p.Invalidate("ep1:9000", "u1") then Get -> new conn
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run TestPool -v`
Expected: FAIL.

**Step 3: Write the implementation**

Create `internal/executor/pool.go`:

```go
package executor

import (
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/the-heaven-labs/aether/internal/models"
)

type PoolConfig struct {
	MaxPools       int           // global cap per connector/endpoint
	IdleTTL        time.Duration // close pools idle longer than this
	PerUserMaxOpen int
	Open           func(cfg models.ConnectorConfig) (clickhouse.Conn, error)
}

type poolEntry struct {
	conn     clickhouse.Conn
	lastUsed time.Time
	cfg      models.ConnectorConfig
	fp       string // credential fingerprint
}

type ConnPool struct {
	mu      sync.Mutex
	cfg     PoolConfig
	entries map[string]*poolEntry // key: endpoint|user
	keys    []string              // LRU order, most recent last
}

func NewConnPool(cfg PoolConfig) *ConnPool { ... }

// Get returns a pooled connection for (endpoint, user). The credentials are
// fingerprinted; a changed fingerprint transparently reopens the pool.
func (p *ConnPool) Get(endpoint, user string, cfg models.ConnectorConfig) (clickhouse.Conn, error) { ... }

// Invalidate closes and removes the pool for a user (offboarding/rotation).
func (p *ConnPool) Invalidate(endpoint, user string) { ... }

// CloseIdle evicts expired pools; call from a ticker.
func (p *ConnPool) CloseIdle(now time.Time) { ... }

func (p *ConnPool) Len() int { ... }
```

Implementation notes:
- The `Open` callback defaults to opening with `clickhouse.Open` + options
  derived from the connector config (share option-building with
  `NewClickHouseExecutor` by extracting `chOptions(cfg)`).
- `Get` takes `p.mu`, scans `keys` LRU-first, evicts entries beyond
  `MaxPools` (close them), opens a new entry if needed.
- Store `fp = fingerprint(cfg.Password)` and reopen when it differs.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/executor/ -run TestPool -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/executor/pool.go internal/executor/pool_test.go
git commit -m "feat(executor): per-user clickhouse connection pool"
```

---

### Task 13: Pooled executor with no-op Close

**Files:**
- Modify: `internal/executor/clickhouse.go`
- Modify: `internal/executor/clickhouse_driver.go`
- Test: `internal/executor/clickhouse_pooled_test.go`

**Step 1: Write the failing test**

```go
func TestPooledExecutorCloseDoesNotCloseConn(t *testing.T) {
	fake := &fakeConn{}
	e := NewPooledClickHouseExecutor(fake)
	require.NoError(t, e.Close())
	require.False(t, fake.closed)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run TestPooledExecutor -v`
Expected: FAIL.

**Step 3: Implement**

- Add `pooled bool` to `ClickHouseExecutor`.
- `NewPooledClickHouseExecutor(conn clickhouse.Conn) *ClickHouseExecutor`
  returns `&ClickHouseExecutor{conn: conn, pooled: true}`.
- `Close()` returns nil without closing when `pooled`.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/executor/ -run TestPooledExecutor -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/executor/
git commit -m "feat(executor): pooled clickhouse executor variant"
```

---

### Task 14: Execution target resolution

**Files:**
- Create: `internal/api/execution_target.go`
- Test: `internal/api/execution_target_test.go`

**Step 1: Write the failing test**

```go
func TestResolveExecutionTargetUsesPreference(t *testing.T) {
	s, orgID, _, connX, connP := setupRoutingFixture(t) // two connectors in one warehouse
	// user has explicit preference for P
	target, err := s.resolveExecutionTarget(ctx, userID, connX, false)
	require.NoError(t, err)
	require.Equal(t, connP, target.ConnectorID)
}

func TestResolveExecutionTargetFallsBackToSoleService(t *testing.T) {
	// one granted service, no preference -> that service
}

func TestResolveExecutionTargetRequiresServiceAccess(t *testing.T) {
	// no `use` on any warehouse connector -> 403
}

func TestResolveExecutionTargetHonorsPin(t *testing.T) {
	// pinned to X without use -> 403 even if P is granted
}

func TestResolveExecutionTargetAmbiguousWithoutPreference(t *testing.T) {
	// two granted services, no preference -> error prompting choice
}

func TestResolveExecutionTargetUnmanagedConnector(t *testing.T) {
	// connector with no warehouse -> ErrUnmanagedConnector (legacy path)
}

func TestResolveExecutionTargetNotReadyFailsClosed(t *testing.T) {
	// managed warehouse with sync_status != 'ready' -> ErrProvisioningNotReady
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestResolveExecutionTarget -v`
Expected: FAIL.

**Step 3: Implement**

Create `internal/api/execution_target.go`:

```go
type ExecutionTarget struct {
	WarehouseID uuid.UUID
	ConnectorID uuid.UUID
	Endpoint    string
	CHUser      string
	Password    string
}

// resolveExecutionTarget implements the routing rules:
//  1. A pinned connector must be usable directly.
//  2. Otherwise, resolve the warehouse from the requested connector.
//  3. Allowed services = connectors in the warehouse with `use` for the user.
//  4. Preference wins when allowed; a sole allowed service is used directly;
//     several allowed services without a preference return ErrServiceChoiceRequired.
func (s *Server) resolveExecutionTarget(ctx context.Context, userID uuid.UUID, requestedConnectorID uuid.UUID, pinned bool) (*ExecutionTarget, error)
```

Steps:
- Load connector (requested) + `warehouse_id`. When it is NULL, return
  `ErrUnmanagedConnector`; callers treat that as the legacy shared-credential
  path (connector stays exactly as today).
- Managed connectors require `warehouses.sync_status='ready'`; otherwise return
  `ErrProvisioningNotReady` (fail closed; never fall back to the stored
  credential).
- `checkPermission(ctx, userID, orgID, role, "connector", connectorID, "use")`.
- List connectors in the warehouse; filter by `use`.
- Apply preference lookup in `warehouse_service_preferences`.
- Build the CH identity via `chaccess.UserIdent(warehouseID, orgID, userID)` +
  `chaccess.DerivePassword(masterKey, warehouseID, userID)`.
- Return `ErrServiceChoiceRequired` when multiple services are allowed and no
  preference is set (HTTP 409 with the allowed list).

**Step 4: Run tests**

Run: `go test ./internal/api/ -run TestResolveExecutionTarget -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/execution_target.go internal/api/execution_target_test.go
git commit -m "feat(api): resolve execution target with service routing"
```

---

### Task 15: Wire HTTP execution path

**Files:**
- Modify: `internal/api/execute_handlers.go` (replace `buildExecutor` usage at `:206` with target resolution + pool)
- Modify: `internal/api/server.go` (hold `*executor.ConnPool`)
- Test: `internal/api/execute_warehouse_test.go`

**Step 1: Write the failing test**

```go
func TestExecuteCellUsesPerUserIdentity(t *testing.T) {
	s, ..., whID := setupWarehouseFixture(t)
	require.NoError(t, s.reconcileWarehouse(ctx, whID))
	// grant user only db1.allowed; execute SELECT from db1.forbidden as that user
	// expect CH access denied error surfaced as 403
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestExecuteCellUsesPerUserIdentity -v`
Expected: FAIL.

**Step 3: Implement**

- In `handleExecuteCell`, after notebook `run` and connector `use` checks:
  - `target, err := s.resolveExecutionTarget(ctx, userID, connectorID, false)`
  - `conn, err := s.connPool.Get(target.Endpoint, target.CHUser, cfgForTarget(target))`
  - `exec := executor.NewPooledClickHouseExecutor(conn)`
  - Set `executor.CtxUserEmail` as today; extend audit event with
    `warehouse_id`, `connector_id` (actual), `ch_user`.
  - On `ErrServiceChoiceRequired`, return HTTP 409 `{"error":"service_choice_required","services":[...]}`.
- `cfgForTarget` builds `models.ConnectorConfig` with host/port/TLS from the
  connector row and user/password from the target.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestExecuteCellUsesPerUserIdentity -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat(api): per-user clickhouse identity on HTTP execution"
```

---

### Task 16: Wire agent and MCP execution paths

**Files:**
- Modify: `internal/agent/tools_notebook.go` (`executeCell` at `:826`; `explore_schema` at `:1297`)
- Modify: `internal/agent/tools_sql.go` (`execute_sql`/`sql_query` at `:122`)
- Modify: `internal/agent/types.go` (`ToolContext` gains `ResolveTarget` callback)
- Test: `internal/agent/warehouse_identity_test.go`

**Step 1: Write the failing test**

```go
func TestAgentExecuteSQLUsesUserIdentity(t *testing.T) {
	// ToolContext with a stub ResolveTarget returning a target with a CH user
	// that lacks access; assert denial, and that the connector's stored
	// credential was never used.
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestAgentExecuteSQLUsesUserIdentity -v`
Expected: FAIL.

**Step 3: Implement**

- `ToolContext` gets `ResolveTarget func(ctx, connectorID) (*ExecutionTarget, error)`
  and `ConnPool *executor.ConnPool`, wired from the server when constructing
  agent tool contexts (PAT owner user ID for MCP sessions).
- Replace `driver.NewExecutor(plain)` with `ResolveTarget` + pool + pooled
  executor in all three locations.
- Fail closed when `ResolveTarget` is nil or errors — never fall back to the
  connector credential.
- Fix `ToolContext.CheckPermission` to delegate to a shared resolver rather
  than the duplicated simplified logic (`internal/agent/types.go:131-156`).

**Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestAgentExecuteSQLUsesUserIdentity -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/agent/
git commit -m "feat(agent): per-user clickhouse identity on agent and MCP paths"
```

---

### Task 17: Per-query `log_comment` and audit correlation

**Files:**
- Modify: `internal/executor/clickhouse.go` (`Execute`)
- Modify: `internal/api/execute_handlers.go`, `internal/agent/tools_notebook.go`, `internal/agent/tools_sql.go`
- Test: `internal/executor/clickhouse_log_comment_test.go`

**Step 1: Write the failing test**

```go
func TestExecutePassesLogComment(t *testing.T) {
	fake := &fakeConn{}
	e := NewPooledClickHouseExecutor(fake)
	_, _ = e.Execute(ctx, "SELECT 1", OutputLimits{})
	require.Contains(t, fake.lastQueryOptions, "log_comment")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run TestExecutePassesLogComment -v`
Expected: FAIL.

**Step 3: Implement**

- Add `executor.CtxExecutionID` context key.
- In `Execute`, pass `clickhouse.WithSettings(clickhouse.Settings{
  "log_comment": "aether:" + execID })` when the context carries an ID.
- Generate `execution_id` (UUID) in the HTTP handler and agent tools, set the
  context value, and include it in the audit event.
- Verify `log_comment` is accepted by a `readonly=1` user; if not, add
  `changeable_in_readonly` to the settings profile (`readonly` constraints) as
  described in the design doc.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/executor/ -run TestExecutePassesLogComment -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/executor/ internal/api/ internal/agent/
git commit -m "feat(executor): tag clickhouse queries with execution id"
```

---

## Phase 5 — Admin API

### Task 18: Warehouse CRUD handlers

**Files:**
- Create: `internal/api/warehouse_handlers.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/warehouse_handlers_test.go`

**Step 1: Write the failing test**

Cover: create requires org admin; list scoped to org; update name/provisioner;
delete cascades connector links; setting a connector's `warehouse_id` to a
warehouse in another org is rejected; provisioner must belong to the same
warehouse.

```go
func TestWarehouseCRUD(t *testing.T) {
	s, orgID, admin := setupTestServerWithOrgAdmin(t)
	wh := createWarehouseViaAPI(t, s, admin, "DWH Prod")
	list := listWarehousesViaAPI(t, s, admin)
	require.Len(t, list, 1)
	updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"name": "DWH Prod 2"})
	deleteWarehouseViaAPI(t, s, admin, wh)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestWarehouseCRUD -v`
Expected: FAIL.

**Step 3: Implement**

Routes (org-admin guarded, same middleware as connector create/update —
`internal/api/router.go:386-392`):

```
GET    /api/v1/warehouses
POST   /api/v1/warehouses
GET    /api/v1/warehouses/{id}
PUT    /api/v1/warehouses/{id}
DELETE /api/v1/warehouses/{id}
PUT    /api/v1/warehouses/{id}/provisioner      {"connector_id": "..."}
PUT    /api/v1/connectors/{id}/warehouse        {"warehouse_id": "..."|null}
```

Use existing `writeJSON`/`writeError` helpers and `@Router` swag annotations.
Validation: connector and warehouse same org; provisioner belongs to the
warehouse; `provisioner_connector_id` required before first sync.
Connector soft-delete (`internal/api/connector_handlers.go:485`) must also
`DELETE FROM warehouse_service_preferences WHERE connector_id=$1` — connectors
are soft-deleted, so the FK cascade never fires.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestWarehouseCRUD -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/warehouse_handlers.go internal/api/router.go internal/api/warehouse_handlers_test.go
git commit -m "feat(api): warehouse CRUD endpoints"
```

---

### Task 19: Table grant and preference handlers

**Files:**
- Create: `internal/api/warehouse_grant_handlers.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/warehouse_grant_handlers_test.go`

**Step 1: Write the failing test**

```go
func TestGrantCRUDAndEffectiveAccess(t *testing.T) {
	// POST grant {subject_type, subject_id, database, table}
	// GET grants -> returns rows
	// GET /warehouses/{id}/effective-access?user_id= -> union of groups + direct + everyone
	// DELETE grant
	// POST duplicate -> 200/409 idempotent
}

func TestPreferenceRequiresServiceAccess(t *testing.T) {
	// PUT preference for connector without `use` -> 403
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'TestGrantCRUD|TestPreferenceRequires' -v`
Expected: FAIL.

**Step 3: Implement**

Routes:

```
GET    /api/v1/warehouses/{id}/grants
POST   /api/v1/warehouses/{id}/grants                 (org admin)
DELETE /api/v1/warehouses/{id}/grants/{grant_id}      (org admin)
GET    /api/v1/warehouses/{id}/effective-access?user_id={id}   (org admin or self)
PUT    /api/v1/warehouses/{id}/preference             {"connector_id":"..."}  (self)
```

Rules:
- Every mutation enqueues warehouse sync.
- Granting tables to a subject with no service access returns a warning field
  `{"warning":"no_service_access"}` (not an error).
- Table/database names validated with the same whitelist used by
  `chaccess.QuoteObjectIdent`; reject invalid names with 400.
- Effective access returns the union set and the CH identity names.
- `PUT /warehouses/{id}/preference` validates the connector belongs to that
  warehouse (`connectors.warehouse_id = :id AND connectors.deleted_at IS NULL`);
  reject cross-warehouse or deleted connectors with 400 (negative test).
- Grant writes validate that `subject_id` belongs to the org (an org member for
  `user`, an org group for `group`); reject cross-org subjects with 400
  (negative test).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run 'TestGrantCRUD|TestPreferenceRequires' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/warehouse_grant_handlers.go internal/api/router.go internal/api/warehouse_grant_handlers_test.go
git commit -m "feat(api): warehouse table grants, effective access, preference"
```

---

### Task 20: Global kill switch and legacy path

**Files:**
- Modify: `internal/config/config.go` (`AETHER_CH_TABLE_PERMISSIONS`, default `false`)
- Modify: `internal/api/execute_handlers.go`, `internal/agent/*`
- Modify: `internal/api/execute_handlers_test.go`

**Step 1: Write the failing test**

```go
func TestWarehousePermissionsDisabledUsesLegacyPath(t *testing.T) {
	s := setupTestServer(t) // kill switch false by default
	// execute a cell on an unmanaged connector
	// assert success using the connector credential
}

func TestKillSwitchOffRoutesManagedConnectorLegacy(t *testing.T) {
	// managed warehouse configured, kill switch off
	// assert execution uses the connector credential
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'TestWarehousePermissionsDisabled|TestKillSwitchOff' -v`
Expected: FAIL if the new path is unconditional.

**Step 3: Implement**

- Add `config.CHTablePermissions bool` (kill switch).
- Off: every connector executes through `buildExecutor` + stored credential
  exactly as today. Log a one-time deprecation/info notice per server start.
- On: unmanaged connectors (no warehouse) still use the legacy path — that is
  their documented mode, not a fallback. Managed connectors use the per-user
  path exclusively; provisioning/permission failures fail closed and never
  fall back to the stored credential.

**Step 4: Run tests**

Run: `go test ./internal/api/ -run 'TestWarehousePermissions|TestKillSwitchOff|TestExecuteCell' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/config/ internal/api/ internal/agent/
git commit -m "feat(config): gate warehouse table permissions behind feature flag"
```

---

## Phase 6 — Frontend

### Task 21: Warehouse settings page

**Files:**
- Create: `web/src/api/warehouses.ts`
- Create: `web/src/pages/WarehouseSettingsPage.tsx`
- Create: `web/src/components/WarehouseTableGrants.tsx`
- Modify: `web/src/App.tsx` (route), `web/src/components/AppShell.tsx` (nav)
- Test: `web/src/components/WarehouseTableGrants.test.tsx`

**Step 1: Write the failing test**

Vitest + React Testing Library: renders grant rows, opens the subject picker
(groups + Everyone + users), posts a grant, shows the "no service access"
warning badge.

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/WarehouseTableGrants.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- `warehouses.ts` exports typed `listWarehouses`, `createWarehouse`,
  `updateWarehouse`, `deleteWarehouse`, `listGrants`, `createGrant`,
  `deleteGrant`, `effectiveAccess`, `setPreference` using the `request`
  helper pattern from `web/src/api/client.ts`.
- Page shows: name, provisioner connector selector (RW connectors only),
  sync status badge (`pending/syncing/ready/error` with error tooltip),
  members connector list with add/remove.
- Table grant matrix: rows = subjects, chips = `db.table`; add via a
  database/table picker sourced from the connector schema endpoint.
- Connectors page: access-mode badge per connector (`Managed — table grants
  enforced` vs `Shared credential — not table-scoped`); a confirmation dialog
  when linking a connector to a warehouse (execution mode changes and
  provisioning begins).
- Use CSS variables only (`var(--accent)`, `var(--text-primary)`).

**Step 4: Run tests and typecheck**

Run: `cd web && npx vitest run src/components/WarehouseTableGrants.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(web): warehouse settings and table grants UI"
```

---

### Task 22: Routing preference and run-result endpoint display

**Files:**
- Create: `web/src/components/RoutingPreference.tsx`
- Modify: `web/src/pages/ProfilePage.tsx` (preference section)
- Modify: `web/src/components/Cell.tsx` (show "ran on <service>" next to timing)
- Modify: `web/src/components/ConnectorSelector.tsx` (pin affordance)
- Test: `web/src/components/RoutingPreference.test.tsx`

**Step 1: Write the failing test**

Renders allowed services for a warehouse, saves a preference, and renders the
"choose a service" state when the API returns 409.

**Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/RoutingPreference.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- Profile page: per-warehouse service dropdown (only services with `use`).
- Cell result metadata: endpoint name + warehouse from the execute response.
- ConnectorSelector: optional "pin" toggle for a cell; pinned cells show a
  lock icon and fail with a clear message for users without access.
- On 409 `service_choice_required`, show a modal listing allowed services and
  store the choice.

**Step 4: Run tests and typecheck**

Run: `cd web && npx vitest run src/components/RoutingPreference.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/
git commit -m "feat(web): routing preference, pin, and endpoint display"
```

---

### Task 23: New-tables inbox and validation warnings

**Files:**
- Modify: `internal/api/warehouse_handlers.go` (new-tables endpoint)
- Modify: `internal/api/connector_handlers.go` (cache raw schema metadata)
- Create: `web/src/components/NewTablesInbox.tsx`
- Modify: `web/src/pages/WarehouseSettingsPage.tsx`
- Test: `internal/api/warehouse_new_tables_test.go`, `web/src/components/NewTablesInbox.test.tsx`

**Step 1: Write the failing tests**

Backend: schema metadata endpoint returns `new_since` list based on cached
`system.tables` snapshots per connector. Frontend: renders rows with an
"Add to group" action.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestWarehouseNewTables -v` and
`cd web && npx vitest run src/components/NewTablesInbox.test.tsx`
Expected: FAIL.

**Step 3: Implement**

- `GET /api/v1/warehouses/{id}/new-tables?since=` returns tables observed since
  the last grant review for that warehouse (raw metadata cache + timestamps).
- Cache: `schema_snapshots (connector_id, table_name, first_seen_at)` populated
  by the reconciliation loop's provisioner connection. Raw metadata only;
  per-user filtering happens in Aether from `warehouse_table_grants`.
- UI inbox lists new tables with subject and "Add grant" buttons.

**Step 4: Run tests**

Run: `go test ./internal/api/ -run TestWarehouseNewTables -v && cd web && npx vitest run src/components/NewTablesInbox.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/ web/src/
git commit -m "feat: new-tables inbox for warehouse grants"
```

---

## Phase 7 — Verification and rollout

### Task 24: E2E smoke coverage

**Files:**
- Modify: `scripts/smoke.sh` or the existing E2E entry (`task test:e2e`)
- Create: `internal/api/warehouse_rbac_e2e_test.go`

**Step 1: Write the failing test**

End-to-end against dev ClickHouse:
1. Create warehouse + connector + group + user.
2. Grant group tables `t1` only.
3. Sync; execute `SELECT * FROM t1` as the user → 200.
4. Execute `SELECT * FROM t2` as the user → CH access error.
5. Execute `remote('...')` → CH privilege error (table function ungranted).
6. Revoke grant; sync; query `t1` → denial.
7. Offboard user; sync; `system.users` no longer contains the identity.

**Step 2: Run test to verify it fails**

Run: `task infra:up && go test ./internal/api/ -run TestWarehouseRBACE2E -v`
Expected: FAIL.

**Step 3: Implement the fixture plumbing as needed**

Ensure the dev ClickHouse has two test tables and the provisioner credential
can `ACCESS MANAGEMENT`. Add tables to `Taskfile.yml` infra setup if missing.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestWarehouseRBACE2E -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/ Taskfile.yml
git commit -m "test(api): end-to-end warehouse rbac coverage"
```

---

### Task 25: Full CI check and rollout notes

**Files:**
- Modify: `docs/plans/2026-09-19-clickhouse-warehouse-permissions-design.md` (status → Implemented, add rollout notes)
- Modify: `AGENTS.md` (document `AETHER_CH_TABLE_PERMISSIONS`, warehouse concepts, sync trigger points)

**Step 1: Run the full local CI suite**

```bash
task check
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
task test:e2e
```

Expected: all pass.

**Step 2: Update docs**

- Mark the design doc as implemented with any deltas.
- Document the feature flag, env vars (`AETHER_CH_RECONCILE_INTERVAL`), the
  managed vs unmanaged connector modes, the global kill switch, and the
  operational runbook (restore → re-sync, provisioner rotation, drift alerts)
  in `AGENTS.md`.

**Step 3: Commit**

```bash
git add docs/plans/ AGENTS.md
git commit -m "docs: warehouse permissions rollout notes"
```

---

## Execution notes

- Run `task infra:up` before any test command that touches the DB.
- Commit after every task; each commit should keep `task check` green.
- Tasks 1–7 are pure and can be done in parallel with 12–13 if you prefer.
- Task 9 is the first test that requires a real ClickHouse; the dev stack
  provides one.
- Design deltas discovered during implementation belong in the design doc.
