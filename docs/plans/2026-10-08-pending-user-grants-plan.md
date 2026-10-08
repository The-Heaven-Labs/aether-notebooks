# Pending-User Grants Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let org admins grant resource ACLs, session (chat) shares, and warehouse table grants to people who have not signed in yet, by staging them per email and materializing them transactionally at first join — surfaced as `pending_user` subjects in all three existing UIs.

**Architecture:** Two side tables (`pending_acl_entries`, `pending_warehouse_table_grants`, migration V127) mirror `pending_group_members` (V105): canonical tables keep their "real subjects only" invariant, and `ApplyPendingAccess` materializes staged rows inside every join transaction right after `ApplyPendingGroups` (registration, org join, SSO provisioning/auto-join, invite redemption). GET paths synthesize staged rows as `subject_type: "pending_user"` so the existing data flow renders them; PUT/create paths accept `pending_user` and route writes to the pending tables.

**Tech Stack:** Go net/http ServeMux + pgx; React 18 + TypeScript + Vitest; Postgres migrations (`internal/database/migrations`).

---

## Conventions used by every Go task

- Working directory: repository root of this worktree (`/home/jesus/Projects/aether-notebooks/.worktrees/pending-grants-connector-health`).
- Every Go test command carries the env prefix and `-timeout 3m`:

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run '<regex>' -count=1
```

- Tests hit the real dev database; helpers live in `internal/api/testhelpers_test.go` (`setupTestServer`, `registerAndGetToken`, `createNotebook`), `internal/api/pending_group_membership_test.go` (`pendingTestOrg`, `pendingTestUser`), `internal/api/warehouse_handlers_test.go` (`warehouseHandlersServer`, `seedWarehouseOrgAdmin`, `createWarehouseViaAPI`, `warehouseAPIRequest`, `grantBody`, `createGrantViaAPI`, `listGrantsViaAPI`, `deleteGrantViaAPI`, `warehouseGrantAuditMeta`), and `internal/api/agent_session_sharing_test.go` (`setupSessionSharingFixture`, `postCreateSession`).
- Follow the staging pattern of `internal/api/pending_group_membership.go` and `internal/api/group_pending_handlers.go`: org-isolated matching on `org_id + lower(email)`, lowercase storage, audit events, errors swallowed at join time.

**Design note (from `docs/plans/2026-10-08-pending-grants-and-connector-health-design.md` §5.1, decisions D1–D5):** staged grants live in side tables; materialization is join-time and transactional; GET responses surface `subject_type: "pending_user"`; conflicts union actions (ACL) or `DO NOTHING` (warehouse grants); session shares allow pending users with exactly `["view"]`.

---

### Task 1: Migration V127 — pending staging tables

**Files:**
- Create: `internal/database/migrations/V127__pending_user_grants.sql`
- Test: `internal/api/pending_user_grants_test.go`

**Step 1: Write the failing test** — create `internal/api/pending_user_grants_test.go`:

```go
package api_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPendingGrantTablesExist pins the V127 schema: both staging tables exist
// so materialization and the handlers can rely on them.
func TestPendingGrantTablesExist(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	for _, table := range []string{"pending_acl_entries", "pending_warehouse_table_grants"} {
		var exists bool
		require.NoError(t, db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table).Scan(&exists))
		require.True(t, exists, "%s must exist", table)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestPendingGrantTablesExist' -count=1
```

Expected: FAIL — `pending_acl_entries must exist` (the migration file does not exist yet, so `db.Migrate` creates nothing and the `information_schema` query returns false).

**Step 3: Write minimal implementation** — create `internal/database/migrations/V127__pending_user_grants.sql` with exactly the design's DDL (§5.1.1):

```sql
-- Pending-user grants: stage ACL entries and warehouse table grants by email
-- before the person has an account, mirroring pending_group_members (V105).
-- Rows are materialized into acl_entries / warehouse_table_grants and consumed
-- when the email first joins the org (registration, org join, SSO
-- provisioning/auto-join, invite redemption).
--
-- Staged rows carry no FK to users (the row exists precisely while the email
-- has no account) and are keyed by lower(email) so lookups are
-- case-insensitive. resource_id is polymorphic, so it carries no FK either;
-- trash purge cleans it up explicitly (internal/scheduler/scheduler.go).
CREATE TABLE pending_acl_entries (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('folder','notebook','connector','dashboard',
                       'agent','model_config','skill','mcp_server','tool','agent_session')),
    resource_id   UUID NOT NULL,
    email         TEXT NOT NULL,
    actions       TEXT[] NOT NULL,
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Expression unique constraints are not valid in a table constraint, so this
-- is a unique index instead. Also makes
-- ON CONFLICT (resource_type, resource_id, lower(email)) inference work.
CREATE UNIQUE INDEX uq_pending_acl_resource_email
    ON pending_acl_entries (resource_type, resource_id, lower(email));
CREATE INDEX idx_pending_acl_org_email ON pending_acl_entries (org_id, lower(email));

CREATE TABLE pending_warehouse_table_grants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    warehouse_id  UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    database_name TEXT NOT NULL,
    table_name    TEXT NOT NULL,
    created_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Also makes ON CONFLICT (warehouse_id, lower(email), database_name,
-- table_name) inference work.
CREATE UNIQUE INDEX uq_pending_wh_grants
    ON pending_warehouse_table_grants (warehouse_id, lower(email), database_name, table_name);
CREATE INDEX idx_pending_wh_grants_org_email ON pending_warehouse_table_grants (org_id, lower(email));
```

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestPendingGrantTablesExist' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/database/migrations/V127__pending_user_grants.sql internal/api/pending_user_grants_test.go
git commit -m "feat(db): add V127 pending-user grant staging tables"
```

---

### Task 2: `normalizePendingEmail` validation helper

**Files:**
- Create: `internal/api/pending_user_grants.go`
- Test: `internal/api/pending_user_grants_internal_test.go`

**Step 1: Write the failing test** — create `internal/api/pending_user_grants_internal_test.go` (package `api`, so the unexported helper is reachable):

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizePendingEmail(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"lowercases", "Alice@Example.com", "alice@example.com", true},
		{"trims surrounding whitespace", "  alice@example.com  ", "alice@example.com", true},
		{"rejects missing domain", "alice@", "", false},
		{"rejects missing local part", "@example.com", "", false},
		{"rejects double at", "alice@@example.com", "", false},
		{"rejects inner whitespace", "ali ce@example.com", "", false},
		{"rejects empty", "", "", false},
		{"rejects plain name", "alice", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizePendingEmail(tc.in)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestNormalizePendingEmail' -count=1
```

Expected: FAIL to build — `undefined: normalizePendingEmail` (the canonical red for a new function).

**Step 3: Write minimal implementation** — create `internal/api/pending_user_grants.go`:

```go
package api

import (
	"strings"
)

// normalizePendingEmail canonicalizes an email staged for a not-yet-registered
// user: trimmed, lowercased, required to have exactly one "@" with a
// non-empty local part and domain, and no whitespace. The lowercased form is
// what the pending tables' lower(email) indexes and all lookups use.
func normalizePendingEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if strings.ContainsAny(email, " \t\n\r") {
		return "", false
	}
	local, domain, found := strings.Cut(email, "@")
	if !found || strings.Contains(domain, "@") {
		return "", false
	}
	if local == "" || domain == "" {
		return "", false
	}
	return email, true
}
```

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestNormalizePendingEmail' -count=1
```

Expected: PASS (all 8 subtests).

**Step 5: Commit**

```bash
git add internal/api/pending_user_grants.go internal/api/pending_user_grants_internal_test.go
git commit -m "feat(api): add pending-email normalizer"
```

---

### Task 3: `ApplyPendingAccess` materialization (ACL + warehouse grants)

**Files:**
- Modify: `internal/api/pending_user_grants.go` (append phase helpers + exported `ApplyPendingAccess`)
- Test: `internal/api/pending_user_grants_test.go` (append helpers + materialization tests)

**Step 1: Write the failing tests** — append to `internal/api/pending_user_grants_test.go` (replace the file's import block with the one shown, which adds `fmt`, `time`, `uuid`, and `api`):

```go
package api_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// TestPendingGrantTablesExist pins the V127 schema: both staging tables exist
// so materialization and the handlers can rely on them.
func TestPendingGrantTablesExist(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	for _, table := range []string{"pending_acl_entries", "pending_warehouse_table_grants"} {
		var exists bool
		require.NoError(t, db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)`,
			table).Scan(&exists))
		require.True(t, exists, "%s must exist", table)
	}
}

// ─── Fixture helpers ─────────────────────────────────────────────────────────

func stagePendingACL(t *testing.T, s *api.Server, orgID, resourceType, resourceID, email string, actions []string) {
	t.Helper()
	_, err := s.DB().Pool.Exec(context.Background(), `
		INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions)
		VALUES ($1, $2, $3, $4, $5)`, orgID, resourceType, resourceID, email, actions)
	require.NoError(t, err)
}

func stagePendingWarehouseGrant(t *testing.T, s *api.Server, orgID, warehouseID, email, database, table string) {
	t.Helper()
	_, err := s.DB().Pool.Exec(context.Background(), `
		INSERT INTO pending_warehouse_table_grants (org_id, warehouse_id, email, database_name, table_name)
		VALUES ($1, $2, $3, $4, $5)`, orgID, warehouseID, email, database, table)
	require.NoError(t, err)
}

func applyPendingAccessInTx(t *testing.T, s *api.Server, orgID, userID, email string) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.DB().Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, api.ApplyPendingAccess(ctx, tx, orgID, userID, email))
	require.NoError(t, tx.Commit(ctx))
}

func aclActionsFor(t *testing.T, s *api.Server, resourceType, resourceID, subjectID string) []string {
	t.Helper()
	var actions []string
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(), `
		SELECT actions FROM acl_entries
		WHERE resource_type = $1 AND resource_id = $2::uuid
		  AND subject_type = 'user' AND subject_id = $3`,
		resourceType, resourceID, subjectID).Scan(&actions))
	return actions
}

func countPendingACLRows(t *testing.T, s *api.Server, orgID, email string) int {
	t.Helper()
	var n int
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pending_acl_entries WHERE org_id = $1 AND lower(email) = lower($2)`,
		orgID, email).Scan(&n))
	return n
}

// ─── ApplyPendingAccess ──────────────────────────────────────────────────────

func TestApplyPendingAccessMaterializesAndConsumes(t *testing.T) {
	s := setupTestServer(t)
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-user-%d@example.com", time.Now().UnixNano()))
	nbID := uuid.NewString()

	// Staged with mixed case; materialized with a different case.
	stagePendingACL(t, s, orgID, "notebook", nbID, "Alice@Example.com", []string{"view", "edit"})

	applyPendingAccessInTx(t, s, orgID, userID, "alice@example.com")

	require.ElementsMatch(t, []string{"view", "edit"}, aclActionsFor(t, s, "notebook", nbID, userID))
	require.Zero(t, countPendingACLRows(t, s, orgID, "alice@example.com"), "staged rows must be consumed")
}

func TestApplyPendingAccessOrgIsolation(t *testing.T) {
	s := setupTestServer(t)
	orgA := pendingTestOrg(t, s)
	orgB := pendingTestOrg(t, s)
	userA := pendingTestUser(t, s, fmt.Sprintf("pua-iso-%d@example.com", time.Now().UnixNano()))
	nbB := uuid.NewString()

	stagePendingACL(t, s, orgB, "notebook", nbB, "shared@example.com", []string{"view"})

	applyPendingAccessInTx(t, s, orgA, userA, "shared@example.com")

	var count int
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM acl_entries
		WHERE resource_id = $1::uuid AND subject_type = 'user' AND subject_id = $2`,
		nbB, userA).Scan(&count))
	require.Zero(t, count, "org A's join must not materialize org B's staged row")
	require.Equal(t, 1, countPendingACLRows(t, s, orgB, "shared@example.com"), "org B's staged row must remain")
}

func TestApplyPendingAccessIsIdempotent(t *testing.T) {
	s := setupTestServer(t)
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-idem-%d@example.com", time.Now().UnixNano()))
	nbID := uuid.NewString()

	stagePendingACL(t, s, orgID, "notebook", nbID, "idem@example.com", []string{"view"})

	applyPendingAccessInTx(t, s, orgID, userID, "idem@example.com")
	applyPendingAccessInTx(t, s, orgID, userID, "idem@example.com") // nothing staged the second time

	var count int
	require.NoError(t, s.DB().Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM acl_entries
		WHERE resource_id = $1::uuid AND subject_id = $2`, nbID, userID).Scan(&count))
	require.Equal(t, 1, count, "a second materialization must not duplicate rows")
	require.Zero(t, countPendingACLRows(t, s, orgID, "idem@example.com"))
}

func TestApplyPendingAccessUnionsExistingDirectEntry(t *testing.T) {
	s := setupTestServer(t)
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-union-%d@example.com", time.Now().UnixNano()))
	nbID := uuid.NewString()

	_, err := s.DB().Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'notebook', $2, 'user', $3, ARRAY['view','delete'])`, orgID, nbID, userID)
	require.NoError(t, err)
	stagePendingACL(t, s, orgID, "notebook", nbID, "union@example.com", []string{"view", "edit"})

	applyPendingAccessInTx(t, s, orgID, userID, "union@example.com")

	require.ElementsMatch(t, []string{"view", "delete", "edit"}, aclActionsFor(t, s, "notebook", nbID, userID),
		"staged actions must be unioned into the existing direct entry, never dropped")
}

func TestApplyPendingAccessWarehouseGrantsDedupe(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-wh-%d@example.com", time.Now().UnixNano()))
	whID := uuid.NewString()

	_, err := s.DB().Pool.Exec(ctx,
		`INSERT INTO warehouses (id, org_id, name) VALUES ($1, $2, 'Pending WH')`, whID, orgID)
	require.NoError(t, err)

	// Existing real grant for the same user + table, plus staged duplicates.
	_, err = s.DB().Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, 'analytics', 'events')`, orgID, whID, userID)
	require.NoError(t, err)
	stagePendingWarehouseGrant(t, s, orgID, whID, "Dedupe@Example.com", "analytics", "events")
	stagePendingWarehouseGrant(t, s, orgID, whID, "dedupe@example.com", "analytics", "users")

	applyPendingAccessInTx(t, s, orgID, userID, "dedupe@example.com")

	var grants int
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM warehouse_table_grants WHERE warehouse_id = $1 AND subject_id = $2`,
		whID, userID).Scan(&grants))
	require.Equal(t, 2, grants, "the duplicate grant dedupes and the new table materializes")

	var pending int
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, whID).Scan(&pending))
	require.Zero(t, pending, "staged grant rows must be consumed")
}

func TestApplyPendingAccessConstrainsSessionRowsToView(t *testing.T) {
	s := setupTestServer(t)
	orgID := pendingTestOrg(t, s)
	userID := pendingTestUser(t, s, fmt.Sprintf("pua-session-%d@example.com", time.Now().UnixNano()))
	sessionID := uuid.NewString()

	// A staged session row that was somehow written with extra actions must not
	// materialize them: non-owner session subjects are read-only.
	stagePendingACL(t, s, orgID, "agent_session", sessionID, "viewer@example.com", []string{"view", "edit", "delete"})

	applyPendingAccessInTx(t, s, orgID, userID, "viewer@example.com")

	require.Equal(t, []string{"view"}, aclActionsFor(t, s, "agent_session", sessionID, userID))
}
```

**Step 2: Run tests to verify they fail**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestApplyPendingAccess' -count=1
```

Expected: FAIL to build — `undefined: api.ApplyPendingAccess` (the exported function does not exist yet).

**Step 3: Write minimal implementation** — append to `internal/api/pending_user_grants.go` (replace the import block with the one shown):

```go
import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// applyPendingACL materializes pending ACL entries staged for email as real
// user entries for userID and consumes them. Staged actions are unioned into
// any existing direct entry (never a downgrade); agent_session rows are
// defensively re-constrained to view-only even if a staged row was written
// with extra actions, matching the read-only session-share rule. It returns
// the number of real ACL rows inserted or updated.
func applyPendingACL(ctx context.Context, tx pgx.Tx, orgID, userID, email string) (int64, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		SELECT pae.org_id, pae.resource_type, pae.resource_id, 'user', $2,
		       CASE WHEN pae.resource_type = 'agent_session'
		            THEN ARRAY(SELECT DISTINCT a FROM unnest(pae.actions) AS a WHERE a = 'view')
		            ELSE pae.actions END
		FROM pending_acl_entries pae
		WHERE pae.org_id = $1 AND lower(pae.email) = lower($3)
		ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		DO UPDATE SET actions = (
			SELECT ARRAY(SELECT DISTINCT unnest(acl_entries.actions || EXCLUDED.actions))
		)`, orgID, userID, email)
	if err != nil {
		return 0, fmt.Errorf("materialize pending ACL entries: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_acl_entries WHERE org_id = $1 AND lower(email) = lower($2)`,
		orgID, email); err != nil {
		return 0, fmt.Errorf("consume pending ACL entries: %w", err)
	}
	return tag.RowsAffected(), nil
}

// applyPendingWarehouseGrants materializes pending warehouse table grants
// staged for email as real user grants and consumes them. An existing grant
// for the same (warehouse, user, database, table) dedupes with DO NOTHING. It
// returns the number of real grant rows inserted.
func applyPendingWarehouseGrants(ctx context.Context, tx pgx.Tx, orgID, userID, email string) (int64, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name, created_by)
		SELECT pgt.org_id, pgt.warehouse_id, 'user', $2, pgt.database_name, pgt.table_name, pgt.created_by
		FROM pending_warehouse_table_grants pgt
		WHERE pgt.org_id = $1 AND lower(pgt.email) = lower($3)
		ON CONFLICT (warehouse_id, subject_type, subject_id, database_name, table_name) DO NOTHING`,
		orgID, userID, email)
	if err != nil {
		return 0, fmt.Errorf("materialize pending warehouse grants: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_warehouse_table_grants WHERE org_id = $1 AND lower(email) = lower($2)`,
		orgID, email); err != nil {
		return 0, fmt.Errorf("consume pending warehouse grants: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ApplyPendingAccess materializes both pending ACL entries and pending
// warehouse table grants staged for email into real rows for a user that just
// appeared in orgID, then consumes the staged rows. It mirrors
// ApplyPendingGroups: org-isolated (org_id + lower(email)), case-insensitive,
// idempotent, and it must run inside the same transaction that creates the org
// membership and home folder.
func ApplyPendingAccess(ctx context.Context, tx pgx.Tx, orgID, userID, email string) error {
	if _, err := applyPendingACL(ctx, tx, orgID, userID, email); err != nil {
		return err
	}
	_, err := applyPendingWarehouseGrants(ctx, tx, orgID, userID, email)
	return err
}
```

**Step 4: Run tests to verify they pass**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestApplyPendingAccess' -count=1
```

Expected: PASS (6 tests; `-run` also matches nothing else).

**Step 5: Commit**

```bash
git add internal/api/pending_user_grants.go internal/api/pending_user_grants_test.go
git commit -m "feat(api): materialize pending ACLs and warehouse grants at join time"
```

---

### Task 4: Server-side wrapper, audits, and join call sites

**Files:**
- Modify: `internal/api/pending_user_grants.go` (append `(s *Server) applyPendingAccess`)
- Modify: `internal/api/auth_handlers.go` (after `s.applyPendingGroups` at the registration site)
- Modify: `internal/api/org_handlers.go` (after `s.applyPendingGroups` at the org-join site)
- Modify: `internal/api/oidc_handlers.go` (after all four `s.applyPendingGroups` calls)
- Test: `internal/api/pending_user_grants_internal_test.go` (append wrapper audit test)
- Test: `internal/api/pending_user_grants_test.go` (append HTTP join integration test + `registerOnly` helper)

**Design note:** the wrapper calls the two phase helpers (not the exported composite) so each phase's audit event carries its own count or error. `ApplyPendingAccess` remains the single-call API for callers/tests, exactly like the `ApplyPendingGroups` + `applyPendingGroups` pair.

**Step 1: Write the failing tests**

Append to `internal/api/pending_user_grants_internal_test.go`:

```go
func TestApplyPendingAccessAuditsErrorsWithoutBlocking(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "pending-audit")
	userID := insertSessionPermUser(t, s, "pending-audit-user")
	addSessionPermMember(t, s, orgID, userID, "editor")

	tx, err := s.db.Pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx)) // closed transaction: every Exec fails

	// The wrapper must swallow the failure (first login is never blocked) and
	// audit both error events.
	require.NotPanics(t, func() {
		s.applyPendingAccess(ctx, tx, orgID.String(), userID.String(), "future@example.com")
	})

	var n int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'acl.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "ACL materialization failure must be audited")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'warehouse.grant.pending_materialize.error'`,
		orgID.String()).Scan(&n))
	require.Equal(t, 1, n, "warehouse materialization failure must be audited")
}
```

Append to `internal/api/pending_user_grants_test.go` (add `bytes`, `encoding/json`, `net/http`, `net/http/httptest` to the imports):

```go
// registerOnly registers a user and returns the onboarding token without
// creating an org, mirroring the first half of testhelpers.registerAndGetToken.
func registerOnly(t *testing.T, srv *api.Server, email string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": "pass123", "name": "Future Member"})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	token, _ := resp["onboarding_token"].(string)
	require.NotEmpty(t, token)
	return token
}

// TestOrgJoinMaterializesPendingAccess drives the real invite-redemption join
// path end to end: the staged ACL must become a real entry inside the join
// transaction, the staged row must be consumed, and the success audit must be
// written.
func TestOrgJoinMaterializesPendingAccess(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	ctx := context.Background()

	adminEmail := fmt.Sprintf("pending-join-admin-%d@example.com", time.Now().UnixNano())
	memberEmail := fmt.Sprintf("pending-join-member-%d@example.com", time.Now().UnixNano())
	adminToken := registerAndGetToken(t, srv, adminEmail, "Pending Join Org")
	memberOnboarding := registerOnly(t, srv, memberEmail)

	// Resolve the admin's user + org.
	var adminID, orgID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, adminEmail).Scan(&adminID))
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `SELECT org_id FROM org_members WHERE user_id = $1`, adminID).Scan(&orgID))

	// Admin invites the future member by email (the invite endpoint only
	// accepts role "admin").
	inviteBody, _ := json.Marshal(map[string]string{"email": memberEmail, "role": "admin"})
	inviteReq := httptest.NewRequest("POST", "/api/v1/members/invite", bytes.NewReader(inviteBody))
	inviteReq.Header.Set("Content-Type", "application/json")
	inviteReq.Header.Set("Authorization", "Bearer "+adminToken)
	inviteRec := httptest.NewRecorder()
	srv.ServeHTTP(inviteRec, inviteReq)
	require.Equal(t, http.StatusCreated, inviteRec.Code, inviteRec.Body.String())
	var inviteResp map[string]string
	require.NoError(t, json.NewDecoder(inviteRec.Body).Decode(&inviteResp))
	inviteToken := inviteResp["token"]
	require.NotEmpty(t, inviteToken)

	// Stage a pending ACL for the future member's email.
	nbID := createNotebook(t, srv, adminToken, "Pending Join NB")
	stagePendingACL(t, srv, orgID, "notebook", nbID, "Member@Example.com", []string{"view", "edit"})

	// The member redeems the invite with their onboarding token.
	joinBody, _ := json.Marshal(map[string]string{"invite_token": inviteToken})
	joinReq := httptest.NewRequest("POST", "/api/v1/auth/org/join", bytes.NewReader(joinBody))
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+memberOnboarding)
	joinRec := httptest.NewRecorder()
	srv.ServeHTTP(joinRec, joinReq)
	require.Equal(t, http.StatusOK, joinRec.Code, joinRec.Body.String())

	// Materialized as a real user entry; the staged row is consumed.
	var memberID string
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT id FROM users WHERE lower(email) = lower($1)`, memberEmail).Scan(&memberID))
	require.ElementsMatch(t, []string{"view", "edit"}, aclActionsFor(t, srv, "notebook", nbID, memberID))
	require.Zero(t, countPendingACLRows(t, srv, orgID, memberEmail))

	// Success audit carries the materialization.
	var audits int
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit_logs WHERE org_id = $1 AND action = 'acl.pending_materialize'`,
		orgID).Scan(&audits))
	require.Equal(t, 1, audits)
}
```

**Step 2: Run tests to verify they fail**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestApplyPendingAccessAuditsErrorsWithoutBlocking|TestOrgJoinMaterializesPendingAccess' -count=1
```

Expected: FAIL to build — `s.applyPendingAccess` is undefined (internal test), and after fixing that the join test fails because the join path never calls the materializer (`require.ElementsMatch` gets no `acl_entries` row). Run the internal test first if you prefer a non-build failure.

**Step 3: Write minimal implementation**

Append to `internal/api/pending_user_grants.go` (add `"github.com/the-heaven-labs/aether/internal/audit"` to imports):

```go
// applyPendingAccess runs the pending ACL and warehouse-grant materializers and
// audits the outcome without failing — a materialization error must never
// block first login, so call sites keep committing their transaction
// regardless. Success emits acl.pending_materialize /
// warehouse.grant.pending_materialize with the materialized row count; each
// phase's failure emits its own .error event.
func (s *Server) applyPendingAccess(ctx context.Context, tx pgx.Tx, orgID, userID, email string) {
	aclCount, err := applyPendingACL(ctx, tx, orgID, userID, email)
	if err != nil {
		s.audit.Log(ctx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "acl.pending_materialize.error", ResourceType: "acl",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"error":   err.Error(),
			},
		})
	} else if aclCount > 0 {
		s.audit.Log(ctx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "acl.pending_materialize", ResourceType: "acl",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"count":   aclCount,
			},
		})
	}

	grantCount, err := applyPendingWarehouseGrants(ctx, tx, orgID, userID, email)
	if err != nil {
		s.audit.Log(ctx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "warehouse.grant.pending_materialize.error", ResourceType: "warehouse",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"error":   err.Error(),
			},
		})
	} else if grantCount > 0 {
		s.audit.Log(ctx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "warehouse.grant.pending_materialize", ResourceType: "warehouse",
			Metadata: map[string]any{
				"email":   email,
				"user_id": userID,
				"count":   grantCount,
			},
		})
	}
}
```

Then add the call immediately after each existing `s.applyPendingGroups(...)` call, so the call sites become:

`internal/api/auth_handlers.go` (registration into a target org):

```go
				s.applyPendingGroups(ctx, tx, targetOrgID, userID, req.Email)
				s.applyPendingAccess(ctx, tx, targetOrgID, userID, req.Email)
				tx.Commit(ctx)
```

`internal/api/org_handlers.go` (invite/link join):

```go
	s.applyPendingGroups(ctx, joinTx, orgID, claims.UserID, joinUserEmail)
	s.applyPendingAccess(ctx, joinTx, orgID, claims.UserID, joinUserEmail)
```

`internal/api/oidc_handlers.go` (SSO `auto_join` provisioning and OIDC `create_org` provider path — the block below appears **twice**, around line 325 and line 403; apply the added line at both):

```go
			// Materialize memberships pre-provisioned for this email.
			s.applyPendingGroups(ctx, tx, orgID, userID, claims.Email)
			s.applyPendingAccess(ctx, tx, orgID, userID, claims.Email)
```

`internal/api/oidc_handlers.go` (existing user auto-joining the provider org, ~line 450):

```go
					if txErr == nil {
						s.applyPendingGroups(ctx, tx, targetOrgID, userID, claims.Email)
						s.applyPendingAccess(ctx, tx, targetOrgID, userID, claims.Email)
					}
```

`internal/api/oidc_handlers.go` (subdomain auto-join, ~line 510):

```go
					if txErr == nil {
						s.applyPendingGroups(ctx, tx, subdomainOrgID, userID, userEmail)
						s.applyPendingAccess(ctx, tx, subdomainOrgID, userID, userEmail)
					}
```

**Step 4: Run tests to verify they pass**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestApplyPendingAccessAuditsErrorsWithoutBlocking|TestOrgJoinMaterializesPendingAccess|TestNormalizePendingEmail' -count=1
```

Expected: PASS. Also run `go build ./...` (or `task vet`) to prove all six call sites compile.

**Step 5: Commit**

```bash
git add internal/api/pending_user_grants.go internal/api/pending_user_grants_internal_test.go internal/api/pending_user_grants_test.go internal/api/auth_handlers.go internal/api/org_handlers.go internal/api/oidc_handlers.go
git commit -m "feat(api): materialize pending access at every join call site"
```

---

### Task 5: ACL GET surfaces staged rows as `pending_user`

**Files:**
- Modify: `internal/models/acl.go` (`ACLEntry`)
- Modify: `internal/api/acl_handlers.go` (add `pendingQueryer`, `pendingACLSelect`, `queryPendingACLEntries`; `handleGetACL`)
- Test: `internal/api/pending_user_acl_test.go` (create)

**Step 1: Write the failing test** — create `internal/api/pending_user_acl_test.go`:

```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

// aclEntryJSON is the test-side shape of models.ACLEntry plus the synthesized
// pending marker.
type aclEntryJSON struct {
	ID          string   `json:"id"`
	SubjectType string   `json:"subject_type"`
	SubjectID   string   `json:"subject_id"`
	Actions     []string `json:"actions"`
	Pending     bool     `json:"pending"`
}

func getACLViaAPI(t *testing.T, srv *api.Server, token, resourceType, resourceID string) []aclEntryJSON {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/acl/"+resourceType+"/"+resourceID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var entries []aclEntryJSON
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	return entries
}

func putACLViaAPI(t *testing.T, srv *api.Server, token, resourceType, resourceID string, entries []map[string]any) (int, []aclEntryJSON) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"entries": entries})
	req := httptest.NewRequest("PUT", "/api/v1/acl/"+resourceType+"/"+resourceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var out []aclEntryJSON
	if rec.Body.Len() > 0 {
		_ = json.NewDecoder(rec.Body).Decode(&out)
	}
	return rec.Code, out
}

func TestACLGetIncludesPendingRows(t *testing.T) {
	srv := setupTestServer(t)
	email := fmt.Sprintf("acl-get-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending GET Org")
	nbID := createNotebook(t, srv, token, "Pending GET NB")
	userID := userIDFromToken(t, srv, token)
	orgID := orgIDFromUser(t, srv, userID)

	_, err := srv.DB().Pool.Exec(context.Background(), `
		INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions)
		VALUES ($1, 'notebook', $2, 'future@example.com', ARRAY['view','edit'])`, orgID, nbID)
	require.NoError(t, err)

	entries := getACLViaAPI(t, srv, token, "notebook", nbID)
	require.Equal(t, "user", entries[0].SubjectType, "real entries come first")

	var pending *aclEntryJSON
	for i := range entries {
		if entries[i].SubjectType == "pending_user" {
			pending = &entries[i]
			break
		}
	}
	require.NotNil(t, pending, "the staged row must be returned")
	require.Equal(t, "future@example.com", pending.SubjectID)
	require.True(t, pending.Pending)
	require.NotEmpty(t, pending.ID, "the request gets the pending row's UUID so it can key/remove it")
	require.ElementsMatch(t, []string{"view", "edit"}, pending.Actions)
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestACLGetIncludesPendingRows' -count=1
```

Expected: FAIL — no entry has `subject_type == "pending_user"` (the GET handler only reads `acl_entries`).

**Step 3: Write minimal implementation**

`internal/models/acl.go` — add the `Pending` field:

```go
type ACLEntry struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	SubjectType  string    `json:"subject_type"`
	SubjectID    string    `json:"subject_id"`
	Actions      []string  `json:"actions"`
	CreatedAt    time.Time `json:"created_at"`
	// Pending is true on synthesized rows returned for staged pending_user
	// subjects; it is never stored on acl_entries.
	Pending bool `json:"pending,omitempty"`
}
```

`internal/api/acl_handlers.go` — add `"context"` to the import block and append these helpers (place them next to `scanACLEntries`):

```go
// pendingQueryer is the query surface staged-row loading needs; both
// *pgxpool.Pool and pgx.Tx satisfy it.
type pendingQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// pendingACLSelect loads staged rows for a resource. Emails are stored
// lowercased by every writer and are synthesized as the subject_id.
const pendingACLSelect = `
	SELECT id, org_id, resource_type, resource_id::text, email, actions, created_at
	FROM pending_acl_entries
	WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3
	ORDER BY lower(email)`

// queryPendingACLEntries returns the staged rows for a resource through q as
// models.ACLEntry rows with subject_type "pending_user" and pending: true. The
// row ID is the pending row's UUID so clients can key and remove it.
func queryPendingACLEntries(ctx context.Context, q pendingQueryer, orgID, resourceType, resourceID string) ([]models.ACLEntry, error) {
	rows, err := q.Query(ctx, pendingACLSelect, resourceType, resourceID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []models.ACLEntry
	for rows.Next() {
		var e models.ACLEntry
		if err := rows.Scan(&e.ID, &e.OrgID, &e.ResourceType, &e.ResourceID,
			&e.SubjectID, &e.Actions, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.SubjectType = "pending_user"
		e.Pending = true
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
```

In `handleGetACL`, change the tail of the function from:

```go
	entries, err := scanACLEntries(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}
	if entries == nil {
		entries = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
```

to:

```go
	entries, err := scanACLEntries(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}

	// Staged rows follow the real entries: same visibility rules, additional
	// pending_user subject type.
	pendingEntries, err := queryPendingACLEntries(ctx, s.db.Pool, scanOrgID, resourceType, resourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	entries = append(entries, pendingEntries...)

	if entries == nil {
		entries = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
```

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestACLGetIncludesPendingRows' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/models/acl.go internal/api/acl_handlers.go internal/api/pending_user_acl_test.go
git commit -m "feat(api): surface staged pending ACL rows in GET /acl"
```

---

### Task 6: ACL PUT stages, validates, and replaces pending rows

**Files:**
- Modify: `internal/api/acl_handlers.go` (`handlePutACL`, `aclAuditDiff`; add `unionActions` and `aclAuditMetadata`)
- Test: `internal/api/pending_user_acl_test.go` (append)

**Design note:** the existing PUT loop silently skips invalid entries (empty subject/actions). This plan applies the same rule to invalid pending emails rather than returning 400, keeping one invalid-entry policy for the endpoint; session shares (Task 8) remain strict 400 because `normalizeSessionShareEntries` rejects invalid input by design.

**Step 1: Write the failing test** — append to `internal/api/pending_user_acl_test.go`:

```go
func TestACLPutStagesAndReplacesPendingEntries(t *testing.T) {
	srv := setupTestServer(t)
	ctx := context.Background()
	email := fmt.Sprintf("acl-put-%d@example.com", time.Now().UnixNano())
	token := registerAndGetToken(t, srv, email, "Pending PUT Org")
	nbID := createNotebook(t, srv, token, "Pending PUT NB")
	userID := userIDFromToken(t, srv, token)

	putCode, inserted := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{
		{"subject_type": "user", "subject_id": userID, "actions": []string{"view", "edit"}},
		{"subject_type": "pending_user", "subject_id": "Future.User@Example.com", "actions": []string{"view", "edit"}},
		{"subject_type": "pending_user", "subject_id": "future.user@example.com", "actions": []string{"run"}},
		{"subject_type": "pending_user", "subject_id": "not-an-email", "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, putCode)
	require.Len(t, inserted, 2, "invalid pending emails are skipped")

	var pending *aclEntryJSON
	for i := range inserted {
		if inserted[i].SubjectType == "pending_user" {
			pending = &inserted[i]
		}
	}
	require.NotNil(t, pending)
	require.Equal(t, "future.user@example.com", pending.SubjectID, "emails are lowercased")
	require.True(t, pending.Pending)
	require.ElementsMatch(t, []string{"view", "edit", "run"}, pending.Actions, "duplicate submissions union actions")

	// GET round-trips the staged row with the same pending row UUID.
	got := getACLViaAPI(t, srv, token, "notebook", nbID)
	found := false
	for _, e := range got {
		if e.SubjectType == "pending_user" {
			found = true
			require.Equal(t, pending.ID, e.ID)
		}
	}
	require.True(t, found)

	// The grant audit reuses acl.granted with the pending marker.
	var metaRaw []byte
	require.NoError(t, srv.DB().Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE action = 'acl.granted' AND metadata->>'subject_type' = 'pending_user'
		ORDER BY id DESC LIMIT 1`).Scan(&metaRaw))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaRaw, &meta))
	require.Equal(t, true, meta["pending"])

	// Replace semantics: a PUT that omits staged rows removes them.
	clearCode, cleared := putACLViaAPI(t, srv, token, "notebook", nbID, []map[string]any{})
	require.Equal(t, http.StatusOK, clearCode)
	require.Empty(t, cleared)
	require.Empty(t, getACLViaAPI(t, srv, token, "notebook", nbID))
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestACLPutStagesAndReplacesPendingEntries' -count=1
```

Expected: FAIL — the current handler inserts `subject_type = 'pending_user'` into `acl_entries`, which violates the `acl_entries_subject_type_check` constraint, so the PUT returns 500 where 200 is expected.

**Step 3: Write minimal implementation**

Add `unionActions` and `aclAuditMetadata` to `internal/api/acl_handlers.go` (near `slicesEqual`):

```go
// unionActions merges two action lists preserving first-seen order.
func unionActions(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, action := range list {
			if !seen[action] {
				seen[action] = true
				out = append(out, action)
			}
		}
	}
	return out
}

// aclAuditMetadata labels one audit subject; staged pending subjects carry
// pending: true so consumers can tell them from real entries.
func aclAuditMetadata(subjectType, subjectID string) map[string]any {
	meta := map[string]any{
		"subject_type": subjectType,
		"subject_id":   subjectID,
	}
	if subjectType == "pending_user" {
		meta["pending"] = true
	}
	return meta
}
```

Replace `aclAuditDiff` entirely with:

```go
// aclAuditDiff compares previous and replacement ACL entries by subject and
// returns the acl.revoked / acl.updated / acl.granted events describing the
// change. Pending subjects are compared like any other subject so replacing a
// staged row audits a revoke/grant pair.
func aclAuditDiff(userID, orgID, resourceType, resourceID string, oldEntries, newEntries []models.ACLEntry) []audit.Entry {
	var auditEvents []audit.Entry
	for _, old := range oldEntries {
		found := false
		for _, newEntry := range newEntries {
			if old.SubjectType == newEntry.SubjectType && old.SubjectID == newEntry.SubjectID {
				found = true
				if !slicesEqual(old.Actions, newEntry.Actions) {
					meta := aclAuditMetadata(newEntry.SubjectType, newEntry.SubjectID)
					meta["old_actions"] = old.Actions
					meta["new_actions"] = newEntry.Actions
					auditEvents = append(auditEvents, audit.Entry{
						OrgID:        orgID,
						UserID:       userID,
						Action:       "acl.updated",
						ResourceType: resourceType,
						ResourceID:   resourceID,
						Metadata:     meta,
					})
				}
				break
			}
		}
		if !found {
			meta := aclAuditMetadata(old.SubjectType, old.SubjectID)
			meta["actions"] = old.Actions
			auditEvents = append(auditEvents, audit.Entry{
				OrgID:        orgID,
				UserID:       userID,
				Action:       "acl.revoked",
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Metadata:     meta,
			})
		}
	}
	for _, newEntry := range newEntries {
		found := false
		for _, old := range oldEntries {
			if old.SubjectType == newEntry.SubjectType && old.SubjectID == newEntry.SubjectID {
				found = true
				break
			}
		}
		if !found {
			meta := aclAuditMetadata(newEntry.SubjectType, newEntry.SubjectID)
			meta["actions"] = newEntry.Actions
			auditEvents = append(auditEvents, audit.Entry{
				OrgID:        orgID,
				UserID:       userID,
				Action:       "acl.granted",
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Metadata:     meta,
			})
		}
	}
	return auditEvents
}
```

Replace the body of `handlePutACL` (everything between the function signature and the closing brace) with:

```go
	resourceType := r.PathValue("resource_type")
	resourceID := r.PathValue("resource_id")

	if resourceType == "agent_session" {
		s.handlePutSessionACL(w, r, resourceID)
		return
	}

	claims := ClaimsFromContext(r.Context())

	// Org admins always have ACL management rights; others need "manage" (folders) or "share".
	if claims.Role != "admin" {
		requiredAction := "share"
		if resourceType == "folder" {
			requiredAction = "manage"
		}
		allowed, err := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, resourceType, resourceID, requiredAction)
		if err != nil || !allowed {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	var req struct {
		Entries []aclEntryInput `json:"entries"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(ctx)

	// Capture existing entries before deleting (for audit comparison). Staged
	// pending rows participate so a replace that drops them audits a revoke.
	existingRows, err := tx.Query(ctx,
		`SELECT subject_type, subject_id, actions FROM acl_entries
         WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query existing ACL")
		return
	}
	var oldEntries []models.ACLEntry
	for existingRows.Next() {
		var e models.ACLEntry
		if err := existingRows.Scan(&e.SubjectType, &e.SubjectID, &e.Actions); err != nil {
			existingRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		oldEntries = append(oldEntries, e)
	}
	if err := existingRows.Err(); err != nil {
		existingRows.Close()
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}
	existingRows.Close()

	pendingOld, err := queryPendingACLEntries(ctx, tx, claims.OrgID, resourceType, resourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query existing ACL")
		return
	}
	oldEntries = append(oldEntries, pendingOld...)

	// Replace semantics apply to both sources: a PUT that omits staged rows
	// removes them, exactly like omitting a real entry.
	if _, err := tx.Exec(ctx,
		`DELETE FROM acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, claims.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear ACL")
		return
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, claims.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear pending ACL")
		return
	}

	// Insert new entries, skipping invalid ones. pending_user entries are
	// validated, lowercased, deduped, and routed to pending_acl_entries.
	type pendingInsert struct {
		email   string
		actions []string
	}
	var inserted []models.ACLEntry
	var pendingInserts []pendingInsert
	pendingIndex := map[string]int{}
	for _, e := range req.Entries {
		if e.SubjectType == "" || e.SubjectID == "" || len(e.Actions) == 0 {
			continue
		}
		if e.SubjectType == "pending_user" {
			email, ok := normalizePendingEmail(e.SubjectID)
			if !ok {
				continue
			}
			if idx, seen := pendingIndex[email]; seen {
				pendingInserts[idx].actions = unionActions(pendingInserts[idx].actions, e.Actions)
				continue
			}
			pendingIndex[email] = len(pendingInserts)
			pendingInserts = append(pendingInserts, pendingInsert{email: email, actions: unionActions(nil, e.Actions)})
			continue
		}
		var entry models.ACLEntry
		err := tx.QueryRow(ctx,
			`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
             VALUES ($1, $2, $3::uuid, $4, $5, $6)
             RETURNING id, org_id, resource_type, resource_id::text, subject_type, subject_id, actions, created_at`,
			claims.OrgID, resourceType, resourceID, e.SubjectType, e.SubjectID, e.Actions,
		).Scan(&entry.ID, &entry.OrgID, &entry.ResourceType, &entry.ResourceID,
			&entry.SubjectType, &entry.SubjectID, &entry.Actions, &entry.CreatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to insert ACL entry")
			return
		}
		inserted = append(inserted, entry)
	}
	for _, p := range pendingInserts {
		var entry models.ACLEntry
		err := tx.QueryRow(ctx, `
			INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions, created_by)
			VALUES ($1, $2, $3::uuid, $4, $5, $6)
			ON CONFLICT (resource_type, resource_id, lower(email)) DO UPDATE
			SET actions = (SELECT ARRAY(SELECT DISTINCT unnest(pending_acl_entries.actions || EXCLUDED.actions)))
			RETURNING id, org_id, resource_type, resource_id::text, email, actions, created_at`,
			claims.OrgID, resourceType, resourceID, p.email, p.actions, claims.UserID,
		).Scan(&entry.ID, &entry.OrgID, &entry.ResourceType, &entry.ResourceID,
			&entry.SubjectID, &entry.Actions, &entry.CreatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to insert ACL entry")
			return
		}
		entry.SubjectType = "pending_user"
		entry.Pending = true
		inserted = append(inserted, entry)
	}

	// Compare old vs new to build audit events
	auditEvents := aclAuditDiff(claims.UserID, claims.OrgID, resourceType, resourceID, oldEntries, inserted)

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	// Log audit events after successful commit
	for _, e := range auditEvents {
		if err := s.audit.Log(ctx, e); err != nil {
			// Log error but don't fail the request since ACL was updated successfully
			continue
		}
	}

	if inserted == nil {
		inserted = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, inserted)
```

**Step 4: Run tests to verify they pass**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestACLGetIncludesPendingRows|TestACLPutStagesAndReplacesPendingEntries|TestACLGetAndPut' -count=1
```

Expected: PASS, including the pre-existing `TestACLGetAndPut` regression check.

**Step 5: Commit**

```bash
git add internal/api/acl_handlers.go internal/api/pending_user_acl_test.go
git commit -m "feat(api): stage pending_user entries through PUT /acl"
```

---

### Task 7: Session share normalization accepts pending users (view-only)

**Files:**
- Modify: `internal/api/session_sharing.go` (`normalizeSessionShareEntries`)
- Test: `internal/api/pending_user_session_internal_test.go` (create)

**Step 1: Write the failing test** — create `internal/api/pending_user_session_internal_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSessionShareEntriesAcceptsPendingViewOnly(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "pending-share")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")

	got, err := s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: "Future@Example.com"},
	})
	require.NoError(t, err)
	require.Equal(t, []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: "future@example.com", Actions: []string{"view"}},
	}, got, "pending emails are lowercased and default to view")

	_, err = s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: "future@example.com", Actions: []string{"view", "edit"}},
	})
	require.ErrorIs(t, err, errInvalidSessionShare, "pending shares stay read-only")

	_, err = s.normalizeSessionShareEntries(ctx, s.db.Pool, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "pending_user", SubjectID: "not-an-email", Actions: []string{"view"}},
	})
	require.ErrorIs(t, err, errInvalidSessionShare, "pending shares require a valid email")
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestNormalizeSessionShareEntriesAcceptsPendingViewOnly' -count=1
```

Expected: FAIL — the normalizer's `default` branch returns `invalid share subject_type` for `pending_user`.

**Step 3: Write minimal implementation** — in `internal/api/session_sharing.go`, extend the doc comment of `normalizeSessionShareEntries` and add the `pending_user` case to the switch (between `case "org_role"` and `default`):

```go
		case "pending_user":
			// Not a member yet: validate the email here and stage the share in
			// pending_acl_entries at insert time. Read-only applies unchanged.
			email, ok := normalizePendingEmail(e.SubjectID)
			if !ok {
				return nil, fmt.Errorf("%w: invalid share email", errInvalidSessionShare)
			}
			e.SubjectID = email
```

The full switch case block becomes:

```go
		case "pending_user":
			email, ok := normalizePendingEmail(e.SubjectID)
			if !ok {
				return nil, fmt.Errorf("%w: invalid share email", errInvalidSessionShare)
			}
			e.SubjectID = email
		case "org_role":
			if e.SubjectID != "everyone" {
				return nil, fmt.Errorf(`%w: org_role shares must use subject_id "everyone"`, errInvalidSessionShare)
			}
		default:
			return nil, fmt.Errorf("%w: invalid share subject_type", errInvalidSessionShare)
		}
```

**Step 4: Run test to verify it passes**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestNormalizeSessionShareEntries' -count=1
```

Expected: PASS (the three new assertions plus the pre-existing normalization tests).

**Step 5: Commit**

```bash
git add internal/api/session_sharing.go internal/api/pending_user_session_internal_test.go
git commit -m "feat(api): accept pending users in session share normalization"
```

---

### Task 8: Session shares store, replace, and return pending rows

**Files:**
- Modify: `internal/api/session_sharing.go` (`insertSessionACLEntries`)
- Modify: `internal/api/acl_handlers.go` (`handlePutSessionACL`)
- Modify: `internal/api/agent_handlers.go` (`createSessionWithSharing`: sweep + insert call + audit marker)
- Test: `internal/api/pending_user_session_test.go` (create)

**Design note (extension beyond §5.1.4):** the design names the trash purge as the pending-ACL cleanup site. The empty-session sweep in `createSessionWithSharing` is the other place that deletes `agent_session` ACL rows (created when an empty session is replaced); pending rows of the swept sessions must be deleted there too, or they would leak with no session to reference — same invariant the existing ACL sweep enforces. This task adds the pending counterpart and a test for it.

**Step 1: Write the failing test** — create `internal/api/pending_user_session_test.go`:

```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/api"
)

func putSessionACLViaAPI(t *testing.T, srv *api.Server, token, sessionID string, entries []map[string]any) (int, []aclEntryJSON) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"entries": entries})
	req := httptest.NewRequest("PUT", "/api/v1/acl/agent_session/"+sessionID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var out []aclEntryJSON
	if rec.Body.Len() > 0 {
		_ = json.NewDecoder(rec.Body).Decode(&out)
	}
	return rec.Code, out
}

func countPendingSessionShares(t *testing.T, srv *api.Server, sessionID string) int {
	t.Helper()
	var n int
	require.NoError(t, srv.DB().Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pending_acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = $1::uuid`, sessionID).Scan(&n))
	return n
}

func TestSessionPendingShareRoundTrip(t *testing.T) {
	fx := setupSessionSharingFixture(t)

	// Session creation accepts a pending view-only share and stages it.
	code, resp := postCreateSession(t, fx.srv, fx.aliceToken, fx.agentID, map[string]any{
		"shares": []map[string]any{
			{"subject_type": "pending_user", "subject_id": "Future.Viewer@Example.com", "actions": []string{"view"}},
		},
	})
	require.Equal(t, http.StatusCreated, code, fmt.Sprint(resp))
	sessionID := resp["session_id"].(string)
	require.Equal(t, 1, countPendingSessionShares(t, fx.srv, sessionID))

	// GET surfaces the staged row as pending_user with the pending marker.
	req := httptest.NewRequest("GET", "/api/v1/acl/agent_session/"+sessionID, nil)
	req.Header.Set("Authorization", "Bearer "+fx.aliceToken)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	fx.srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var entries []aclEntryJSON
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	var pending *aclEntryJSON
	for i := range entries {
		if entries[i].SubjectType == "pending_user" {
			pending = &entries[i]
		}
	}
	require.NotNil(t, pending)
	require.Equal(t, "future.viewer@example.com", pending.SubjectID)
	require.True(t, pending.Pending)
	require.Equal(t, []string{"view"}, pending.Actions)

	// Non-view pending shares are rejected as invalid input.
	badCode, _ := putSessionACLViaAPI(t, fx.srv, fx.aliceToken, sessionID, []map[string]any{
		{"subject_type": "pending_user", "subject_id": "future.viewer@example.com", "actions": []string{"view", "edit"}},
	})
	require.Equal(t, http.StatusBadRequest, badCode)

	// PUT replaces non-owner entries: a real member share plus a pending share,
	// then dropping the pending one on the next PUT.
	putCode, putEntries := putSessionACLViaAPI(t, fx.srv, fx.aliceToken, sessionID, []map[string]any{
		{"subject_type": "user", "subject_id": fx.bobID, "actions": []string{"view"}},
		{"subject_type": "pending_user", "subject_id": "other@example.com", "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, putCode)
	require.Equal(t, 1, countPendingSessionShares(t, fx.srv, sessionID))
	require.Len(t, putEntries, 3, "owner + member share + staged pending share")

	lastCode, _ := putSessionACLViaAPI(t, fx.srv, fx.aliceToken, sessionID, []map[string]any{
		{"subject_type": "user", "subject_id": fx.bobID, "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, lastCode)
	require.Zero(t, countPendingSessionShares(t, fx.srv, sessionID), "a PUT omitting the staged share removes it")
}

func TestEmptySessionSweepRemovesPendingShares(t *testing.T) {
	fx := setupSessionSharingFixture(t)

	code, resp := postCreateSession(t, fx.srv, fx.aliceToken, fx.agentID, map[string]any{})
	require.Equal(t, http.StatusCreated, code, fmt.Sprint(resp))
	first := resp["session_id"].(string)

	putCode, _ := putSessionACLViaAPI(t, fx.srv, fx.aliceToken, first, []map[string]any{
		{"subject_type": "pending_user", "subject_id": "future@example.com", "actions": []string{"view"}},
	})
	require.Equal(t, http.StatusOK, putCode)
	require.Equal(t, 1, countPendingSessionShares(t, fx.srv, first))

	// Creating another empty session for the same agent/user/notebook sweeps
	// the first session and must sweep its staged shares too.
	code2, _ := postCreateSession(t, fx.srv, fx.aliceToken, fx.agentID, map[string]any{})
	require.Equal(t, http.StatusCreated, code2)

	require.Zero(t, countPendingSessionShares(t, fx.srv, first))
	var sessions int
	require.NoError(t, fx.srv.DB().Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent_sessions WHERE id = $1`, first).Scan(&sessions))
	require.Zero(t, sessions)
}
```

**Step 2: Run tests to verify they fail**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestSessionPendingShareRoundTrip|TestEmptySessionSweepRemovesPendingShares' -count=1
```

Expected: FAIL — session creation with a pending share returns 400 because `insertSessionACLEntries` writes every share into `acl_entries` (or, before the handler changes, `handleCreateSession` returns 400 once normalization lands — this task wires the insert path). The sweep test fails because pending rows survive the sweep.

**Step 3: Write minimal implementation**

Replace `insertSessionACLEntries` in `internal/api/session_sharing.go` with a version that takes the staging actor and routes `pending_user` shares to `pending_acl_entries`:

```go
// insertSessionACLEntries inserts one agent_session ACL row per normalized
// share through tx. pending_user shares are staged in pending_acl_entries
// instead, keyed by lowercased email, and materialize as view-only rows when
// the person first joins. ON CONFLICT DO NOTHING preserves any pre-existing
// real row — in particular the owner's full-access entry, so a share naming
// the owner can never downgrade it.
func insertSessionACLEntries(ctx context.Context, tx pgx.Tx, orgID, sessionID, createdBy string, shares []aclEntryInput) error {
	for _, share := range shares {
		if share.SubjectType == "pending_user" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions, created_by)
				VALUES ($1, 'agent_session', $2::uuid, $3, $4, $5)
				ON CONFLICT (resource_type, resource_id, lower(email)) DO UPDATE
				SET actions = (SELECT ARRAY(SELECT DISTINCT unnest(pending_acl_entries.actions || EXCLUDED.actions)))
			`, orgID, sessionID, share.SubjectID, share.Actions, createdBy); err != nil {
				return fmt.Errorf("insert pending session share: %w", err)
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			VALUES ($1, 'agent_session', $2::uuid, $3, $4, $5)
			ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING
		`, orgID, sessionID, share.SubjectType, share.SubjectID, share.Actions); err != nil {
			return fmt.Errorf("insert session share: %w", err)
		}
	}
	return nil
}
```

In `internal/api/agent_handlers.go`, update `createSessionWithSharing`:

1. The sweep must also delete staged rows — replace the `if len(sweptIDs) > 0 { ... }` block with:

```go
	if len(sweptIDs) > 0 {
		if _, err := tx.Exec(ctx,
			`DELETE FROM acl_entries WHERE resource_type = 'agent_session' AND resource_id = ANY($1)`,
			sweptIDs); err != nil {
			return fmt.Errorf("delete swept session ACLs: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM pending_acl_entries WHERE resource_type = 'agent_session' AND resource_id = ANY($1)`,
			sweptIDs); err != nil {
			return fmt.Errorf("delete swept session pending ACLs: %w", err)
		}
	}
```

2. The insert call becomes:

```go
	if err := insertSessionACLEntries(ctx, tx, p.OrgID, sessionID, p.UserID, p.Shares); err != nil {
		return err
	}
```

3. The per-share audit loop at the end of `handleCreateSession` (currently `for _, share := range shares { h.server.audit.Log(...) }`) becomes:

```go
	for _, share := range shares {
		metadata := map[string]any{
			"subject_type": share.SubjectType,
			"subject_id":   share.SubjectID,
			"actions":      share.Actions,
		}
		if share.SubjectType == "pending_user" {
			metadata["pending"] = true
		}
		h.server.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "acl.granted", ResourceType: "agent_session", ResourceID: sessionID,
			Metadata: metadata,
		})
	}
```

In `internal/api/acl_handlers.go`, replace `handlePutSessionACL` with the version below (it adds pending capture, pending replace, the new insert signature, and pending rows in the response):

```go
func (s *Server) handlePutSessionACL(w http.ResponseWriter, r *http.Request, sessionID string) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(ctx)

	// Lock and resolve the session before touching acl_entries. Locking only
	// the session row keeps the transaction from touching agents rows and
	// holds the lock order the create/delete paths rely on.
	var ownerID, sessionOrgID string
	err = tx.QueryRow(ctx, `
		SELECT s.user_id, a.org_id
		FROM agent_sessions s
		JOIN agents a ON a.id = s.agent_id
		WHERE s.id = $1
		FOR UPDATE OF s
	`, sessionID).Scan(&ownerID, &sessionOrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load session")
		return
	}

	// Only the owner or an org admin in admin mode may write. This mirrors
	// checkSessionPermission's "share" rule against the row already locked and
	// resolved above, so no second pool connection is needed while the
	// transaction holds the lock.
	if claims.UserID != ownerID &&
		!(claims.Role == "admin" && adminModeFromContext(ctx) && claims.OrgID == sessionOrgID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Entries []aclEntryInput `json:"entries"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The normalizer drops entries naming the owner and rejects anything that
	// is not a same-org, view-only share (pending_user shares are view-only
	// emails); invalid input maps to 400 and database failures map to 500.
	shares, err := s.normalizeSessionShareEntries(ctx, tx, ownerID, sessionOrgID, req.Entries)
	if err != nil {
		writeSessionShareError(w, err)
		return
	}

	// Capture the previous non-owner entries for the audit diff. The preserved
	// owner row is excluded so it never surfaces as revoked.
	var oldEntries []models.ACLEntry
	oldRows, err := tx.Query(ctx, `
		SELECT subject_type, subject_id, actions FROM acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = $1::uuid AND org_id = $2
		  AND NOT (subject_type = 'user' AND subject_id = $3)
	`, sessionID, sessionOrgID, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query existing ACL")
		return
	}
	for oldRows.Next() {
		var e models.ACLEntry
		if err := oldRows.Scan(&e.SubjectType, &e.SubjectID, &e.Actions); err != nil {
			oldRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		oldEntries = append(oldEntries, e)
	}
	if err := oldRows.Err(); err != nil {
		oldRows.Close()
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}
	oldRows.Close()

	// Staged pending shares are part of the replace set too; they are always
	// non-owner rows.
	pendingOld, err := queryPendingACLEntries(ctx, tx, sessionOrgID, "agent_session", sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query existing ACL")
		return
	}
	oldEntries = append(oldEntries, pendingOld...)

	// Replace only the non-owner entries; the owner row is untouched here.
	if _, err := tx.Exec(ctx, `
		DELETE FROM acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = $1::uuid AND org_id = $2
		  AND NOT (subject_type = 'user' AND subject_id = $3)
	`, sessionID, sessionOrgID, ownerID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear ACL")
		return
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM pending_acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = $1::uuid AND org_id = $2
	`, sessionID, sessionOrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear pending ACL")
		return
	}

	if err := insertSessionACLEntries(ctx, tx, sessionOrgID, sessionID, claims.UserID, shares); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert ACL entry")
		return
	}

	// Upsert the owner entry last: recreate it for legacy sessions that lack
	// one and restore full access if it was ever downgraded.
	if _, err := tx.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'agent_session', $2::uuid, 'user', $3, $4)
		ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		DO UPDATE SET actions = EXCLUDED.actions
	`, sessionOrgID, sessionID, ownerID, sessionOwnerActions); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preserve owner ACL entry")
		return
	}

	newEntries := make([]models.ACLEntry, 0, len(shares))
	for _, share := range shares {
		entry := models.ACLEntry{
			SubjectType: share.SubjectType,
			SubjectID:   share.SubjectID,
			Actions:     share.Actions,
		}
		if share.SubjectType == "pending_user" {
			entry.Pending = true
		}
		newEntries = append(newEntries, entry)
	}
	// Attribute the audit to the session's org: that is where the ACL rows
	// live even when the owner writes with a token for another org.
	auditEvents := aclAuditDiff(claims.UserID, sessionOrgID, "agent_session", sessionID, oldEntries, newEntries)

	// Return the resource's resulting ACL state, owner entry included, with
	// staged pending rows appended.
	resultRows, err := tx.Query(ctx,
		`SELECT id, org_id, resource_type, resource_id::text, subject_type, subject_id, actions, created_at
         FROM acl_entries
         WHERE resource_type = 'agent_session' AND resource_id = $1::uuid AND org_id = $2
         ORDER BY subject_type, subject_id`,
		sessionID, sessionOrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ACL")
		return
	}
	entries, err := scanACLEntries(resultRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ACL")
		return
	}
	pendingEntries, err := queryPendingACLEntries(ctx, tx, sessionOrgID, "agent_session", sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ACL")
		return
	}
	entries = append(entries, pendingEntries...)

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	for _, e := range auditEvents {
		if err := s.audit.Log(ctx, e); err != nil {
			// Log error but don't fail the request since the ACL was updated.
			continue
		}
	}

	if entries == nil {
		entries = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}
```

**Step 4: Run tests to verify they pass**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestSessionPendingShareRoundTrip|TestEmptySessionSweepRemovesPendingShares|TestNormalizeSessionShareEntries' -count=1
```

Expected: PASS. Also run `go build ./...` so the two updated `insertSessionACLEntries` call sites are proven to compile.

**Step 5: Commit**

```bash
git add internal/api/session_sharing.go internal/api/acl_handlers.go internal/api/agent_handlers.go internal/api/pending_user_session_test.go
git commit -m "feat(api): stage pending session shares and sweep them with empty sessions"
```

---

### Task 9: Warehouse grant creation accepts `pending_user`

**Files:**
- Modify: `internal/api/warehouse_grant_handlers.go` (`validateGrantSubject`, `handleCreateWarehouseGrant`; add `pendingWarehouseGrantSelect`, `scanPendingWarehouseGrant`, `loadPendingWarehouseGrant`)
- Test: `internal/api/pending_user_warehouse_test.go` (create)

**Step 1: Write the failing test** — create `internal/api/pending_user_warehouse_test.go` (package `api`, so it can use the warehouse test helpers):

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPendingWarehouseGrantCreate(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Grant WH")
	rec.reset()

	email := "Future.User@Example.com"
	createRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", email, "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	require.Equal(t, "pending_user", created.SubjectType)
	require.Equal(t, "future.user@example.com", created.SubjectID)
	require.Equal(t, "future.user@example.com", created.SubjectEmail)
	require.Empty(t, created.Warning, "pending subjects are never warned about service access")

	ctx := context.Background()
	var realCount, pendingCount int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&realCount))
	require.Zero(t, realCount, "staging must not write a real grant")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&pendingCount))
	require.Equal(t, 1, pendingCount)
	require.False(t, rec.contains(wh), "staging must not enqueue a warehouse sync")

	// Idempotent replay returns the same staged row with 200.
	replayRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", "future.user@example.COM", "analytics", "events"))
	require.Equal(t, http.StatusOK, replayRec.Code, replayRec.Body.String())
	var replay warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(replayRec.Body.Bytes(), &replay))
	require.Equal(t, created.ID, replay.ID)

	// Audit carries the pending marker.
	meta := warehouseGrantAuditMeta(t, s, "warehouse.grant.create", created.ID)
	require.Equal(t, true, meta["pending"])
}

func TestPendingWarehouseGrantRejectsInvalidEmail(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Invalid WH")

	for _, email := range []string{"", "future@", "@example.com", "two@@example.com"} {
		rec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", email, "analytics", "events"))
		require.Equal(t, http.StatusBadRequest, rec.Code, "email %q must be rejected", email)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestPendingWarehouseGrant' -count=1
```

Expected: FAIL — `validateGrantSubject`'s default branch rejects `pending_user` with 400 where 201/400-by-email is expected.

**Step 3: Write minimal implementation**

In `internal/api/warehouse_grant_handlers.go`, add the `pending_user` case to `validateGrantSubject` (before `default`) and update the default message:

```go
	case "pending_user":
		email, ok := normalizePendingEmail(subjectID)
		if !ok {
			return "", invalidGrant("subject_id must be an email for subject_type pending_user")
		}
		return email, nil
	default:
		return "", invalidGrant("subject_type must be one of user, group, everyone, pending_user")
	}
```

Add the pending select/scan/load helpers next to `warehouseGrantSelect` / `loadWarehouseGrant`:

```go
// pendingWarehouseGrantSelect loads staged grants; the email is synthesized as
// the subject_id and subject_email and labeled as the subject name.
const pendingWarehouseGrantSelect = `
	SELECT id, org_id, warehouse_id, email, database_name, table_name, created_by, created_at
	FROM pending_warehouse_table_grants`

// scanPendingWarehouseGrant maps a staged row onto the API representation with
// subject_type "pending_user".
func scanPendingWarehouseGrant(row pgx.Row) (warehouseGrantJSON, error) {
	var (
		g         warehouseGrantJSON
		email     string
		createdBy *string
	)
	err := row.Scan(&g.ID, &g.OrgID, &g.WarehouseID, &email, &g.Database, &g.Table, &createdBy, &g.CreatedAt)
	if err != nil {
		return g, err
	}
	g.SubjectType = "pending_user"
	g.SubjectID = email
	g.SubjectName = email
	g.SubjectEmail = email
	g.CreatedBy = createdBy
	return g, nil
}

// loadPendingWarehouseGrant returns one staged grant scoped to its org.
func (s *Server) loadPendingWarehouseGrant(ctx context.Context, orgID, grantID string) (warehouseGrantJSON, error) {
	return scanPendingWarehouseGrant(s.db.Pool.QueryRow(ctx,
		pendingWarehouseGrantSelect+` WHERE id = $1 AND org_id = $2`, grantID, orgID))
}
```

In `handleCreateWarehouseGrant`, immediately after `subjectID, err := s.validateGrantSubject(...)` succeeds, branch to the pending path:

```go
	// Pending subjects are staged in their own table: they cannot connect yet
	// by construction, so the service-access warning is suppressed, and the
	// staging write changes no ClickHouse desired state, so no sync is
	// enqueued. The insert is idempotent like the real-grant path.
	if req.SubjectType == "pending_user" {
		inserted := false
		var grantID string
		err = s.db.Pool.QueryRow(ctx, `
			INSERT INTO pending_warehouse_table_grants
				(org_id, warehouse_id, email, database_name, table_name, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (warehouse_id, lower(email), database_name, table_name) DO NOTHING
			RETURNING id`,
			claims.OrgID, warehouseUUID.String(), subjectID,
			req.Database, req.Table, claims.UserID).Scan(&grantID)
		switch {
		case err == nil:
			inserted = true
		case errors.Is(err, pgx.ErrNoRows):
			// Idempotent replay: return the staged grant that already exists.
			err = s.db.Pool.QueryRow(ctx, `
				SELECT id FROM pending_warehouse_table_grants
				WHERE warehouse_id = $1 AND org_id = $2 AND lower(email) = $3
				  AND database_name = $4 AND table_name = $5`,
				warehouseUUID.String(), claims.OrgID, subjectID,
				req.Database, req.Table).Scan(&grantID)
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "warehouse not found")
				return
			}
		case isForeignKeyViolation(err):
			writeError(w, http.StatusNotFound, "warehouse not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create grant")
			return
		}

		grant, err := s.loadPendingWarehouseGrant(ctx, claims.OrgID, grantID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load grant")
			return
		}

		status := http.StatusOK
		if inserted {
			status = http.StatusCreated
			s.audit.Log(ctx, audit.Entry{
				OrgID: claims.OrgID, UserID: claims.UserID,
				Action: "warehouse.grant.create", ResourceType: "warehouse", ResourceID: warehouseUUID.String(),
				Metadata: map[string]any{
					"grant_id":     grantID,
					"subject_type": "pending_user",
					"subject_id":   subjectID,
					"database":     req.Database,
					"table":        req.Table,
					"pending":      true,
				},
			})
		}
		writeJSON(w, status, warehouseGrantCreateJSON{warehouseGrantJSON: grant})
		return
	}
```

The existing real-subject code below stays unchanged.

**Step 4: Run tests to verify they pass**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestPendingWarehouseGrant' -count=1
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/warehouse_grant_handlers.go internal/api/pending_user_warehouse_test.go
git commit -m "feat(api): stage pending_user warehouse table grants"
```

---

### Task 10: Warehouse grant list/delete handle staged rows; validation ignores them

**Files:**
- Modify: `internal/api/warehouse_grant_handlers.go` (`handleListWarehouseGrants`, `handleDeleteWarehouseGrant`)
- Test: `internal/api/pending_user_warehouse_test.go` (append)

**Design note:** `internal/api/warehouse_validation.go` needs **no change** — it reads only `warehouse_table_grants`, so staged rows are invisible to warnings by construction. The second test below pins that invariant so a future refactor cannot accidentally union the pending table in.

**Step 1: Write the failing tests** — append to `internal/api/pending_user_warehouse_test.go`:

```go
func TestPendingWarehouseGrantListAndDelete(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending List WH")

	// A real grant (Everyone) plus a staged one; the list unions both, staged last.
	realRec := createGrantViaAPI(t, s, admin, wh, grantBody("everyone", "everyone", "raw", "clicks"))
	require.Equal(t, http.StatusCreated, realRec.Code, realRec.Body.String())
	createRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", "future@example.com", "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	rec.reset()

	grants := listGrantsViaAPI(t, s, admin, wh)
	require.Len(t, grants, 2)
	require.Equal(t, "everyone", grants[0].SubjectType)
	require.Equal(t, "pending_user", grants[1].SubjectType)
	require.Equal(t, created.ID, grants[1].ID)
	require.Equal(t, "future@example.com", grants[1].SubjectEmail)

	// Delete removes the staged row from its table and 404s on replay.
	require.Equal(t, http.StatusNoContent,
		deleteGrantViaAPI(t, s, admin, wh, mustParseUUID(t, created.ID)).Code)
	require.Equal(t, http.StatusNotFound,
		deleteGrantViaAPI(t, s, admin, wh, mustParseUUID(t, created.ID)).Code)

	grants = listGrantsViaAPI(t, s, admin, wh)
	require.Len(t, grants, 1)

	var pending int
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&pending))
	require.Zero(t, pending)
	require.False(t, rec.contains(wh), "deleting a staged grant must not enqueue a warehouse sync")

	// The delete audit carries the pending marker.
	meta := warehouseGrantAuditMeta(t, s, "warehouse.grant.delete", created.ID)
	require.Equal(t, true, meta["pending"])
}

func TestWarehouseValidationIgnoresPendingGrants(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Validation WH")

	createRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", "future@example.com", "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	valRec := warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+wh.String()+"/validation", admin, nil)
	require.Equal(t, http.StatusOK, valRec.Code, valRec.Body.String())
	var v warehouseValidationJSON
	require.NoError(t, json.Unmarshal(valRec.Body.Bytes(), &v))
	require.Empty(t, v.TablesWithoutService, "staged grants must not produce validation warnings")
	require.Empty(t, v.ServicesWithoutTables)
}

// mustParseUUID fails the test on an invalid grant ID returned by the API.
func mustParseUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(raw)
	require.NoError(t, err)
	return parsed
}
```

Add `"github.com/google/uuid"` to the test file's imports.

**Step 2: Run tests to verify they fail**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestPendingWarehouseGrantListAndDelete|TestWarehouseValidationIgnoresPendingGrants' -count=1
```

Expected: `TestPendingWarehouseGrantListAndDelete` FAILS — the list contains one real grant only (`require.Len(t, grants, 2)`), and the delete of the staged ID returns 404. `TestWarehouseValidationIgnoresPendingGrants` already passes (guard test).

**Step 3: Write minimal implementation**

In `handleListWarehouseGrants`, after the real-rows scan loop and before `writeJSON`, append:

```go
	// Staged pending grants follow the real entries: the admin matrix and the
	// new-tables inbox render them as pending_user subjects.
	pendingRows, err := s.db.Pool.Query(ctx, pendingWarehouseGrantSelect+`
		WHERE warehouse_id = $1 AND org_id = $2
		ORDER BY lower(email) ASC, database_name ASC, table_name ASC
		LIMIT $3`,
		warehouseUUID.String(), claims.OrgID, maxWarehouseGrantRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer pendingRows.Close()
	for pendingRows.Next() {
		g, err := scanPendingWarehouseGrant(pendingRows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		grants = append(grants, g)
	}
	if err := pendingRows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
```

In `handleDeleteWarehouseGrant`, replace the delete block (from `var subjectType, subjectID, database, table string` through the audit/write call) with:

```go
	var subjectType, subjectID, database, table string
	pending := false
	err := s.db.Pool.QueryRow(ctx, `
		DELETE FROM warehouse_table_grants
		WHERE id = $1 AND warehouse_id = $2 AND org_id = $3
		RETURNING subject_type, subject_id, database_name, table_name`,
		grantUUID.String(), warehouseUUID.String(), claims.OrgID).
		Scan(&subjectType, &subjectID, &database, &table)
	if errors.Is(err, pgx.ErrNoRows) {
		// The ID may name a staged pending grant instead.
		err = s.db.Pool.QueryRow(ctx, `
			DELETE FROM pending_warehouse_table_grants
			WHERE id = $1 AND warehouse_id = $2 AND org_id = $3
			RETURNING email, database_name, table_name`,
			grantUUID.String(), warehouseUUID.String(), claims.OrgID).
			Scan(&subjectID, &database, &table)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "grant not found")
			return
		}
		subjectType = "pending_user"
		pending = true
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete grant")
		return
	}

	// A staged grant never shaped ClickHouse desired state, so deleting it
	// does not schedule a reconcile.
	if !pending {
		s.enqueueWarehouseSync(warehouseUUID)
	}
	metadata := map[string]any{
		"grant_id":     grantUUID.String(),
		"subject_type": subjectType,
		"subject_id":   subjectID,
		"database":     database,
		"table":        table,
	}
	if pending {
		metadata["pending"] = true
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "warehouse.grant.delete", ResourceType: "warehouse", ResourceID: warehouseUUID.String(),
		Metadata: metadata,
	})

	w.WriteHeader(http.StatusNoContent)
```

**Step 4: Run tests to verify they pass**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestPendingWarehouseGrant|TestWarehouseValidationIgnoresPendingGrants|TestGrantCRUDAndEffectiveAccess' -count=1
```

Expected: PASS, including the pre-existing warehouse grant CRUD regression check.

**Step 5: Commit**

```bash
git add internal/api/warehouse_grant_handlers.go internal/api/pending_user_warehouse_test.go
git commit -m "feat(api): union staged grants into warehouse grant list and delete"
```

---

### Task 11: Trash purge cleans pending ACL rows

**Files:**
- Modify: `internal/scheduler/scheduler.go` (`purgeTrash`)
- Test: `internal/scheduler/purge_pending_test.go` (create)

**Step 1: Write the failing test** — create `internal/scheduler/purge_pending_test.go`:

```go
package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/database"
)

// seedPendingACLForResource inserts one staged row; the test only checks that
// purge deletes the right rows.
func seedPendingACLForResource(t *testing.T, db *database.DB, orgID, resourceType, resourceID string) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions)
		VALUES ($1, $2, $3, 'future@example.com', ARRAY['view'])`, orgID, resourceType, resourceID)
	require.NoError(t, err)
}

func countPendingFor(t *testing.T, db *database.DB, resourceType, resourceID string) int {
	t.Helper()
	var n int
	require.NoError(t, db.Pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pending_acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid`,
		resourceType, resourceID).Scan(&n))
	return n
}

func TestPurgeTrashRemovesPendingACLsOfPurgedResources(t *testing.T) {
	db := setupPurgeTestDB(t)
	ctx := context.Background()
	f := seedPurgeTestOrg(t, db)

	pastRetention := time.Now().Add(-8 * 24 * time.Hour)
	withinRetention := time.Now().Add(-24 * time.Hour)

	purgedNB, purgedSession := seedPurgeNotebook(t, db, f, &pastRetention)
	trashedNB, trashedSession := seedPurgeNotebook(t, db, f, &withinRetention)
	liveNB, liveSession := seedPurgeNotebook(t, db, f, nil)

	for _, r := range []struct{ resourceType, id string }{
		{"notebook", purgedNB}, {"notebook", trashedNB}, {"notebook", liveNB},
		{"agent_session", purgedSession}, {"agent_session", trashedSession}, {"agent_session", liveSession},
	} {
		seedPendingACLForResource(t, db, f.orgID, r.resourceType, r.id)
	}

	seedConnector := func(deletedAt *time.Time) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO connectors (id, org_id, name, type, config_encrypted, deleted_at)
			VALUES ($1, $2, 'Purge Conn', 'postgres', $3, $4)`, id, f.orgID, []byte("{}"), deletedAt)
		require.NoError(t, err)
		return id
	}
	seedDashboard := func(deletedAt *time.Time) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO dashboards (id, org_id, title, created_by, deleted_at)
			VALUES ($1, $2, 'Purge Dashboard', $3, $4)`, id, f.orgID, f.userID, deletedAt)
		require.NoError(t, err)
		return id
	}
	seedFolder := func(deletedAt *time.Time) string {
		id := uuid.NewString()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO folders (id, org_id, name, created_by, deleted_at)
			VALUES ($1, $2, 'Purge Folder', $3, $4)`, id, f.orgID, f.userID, deletedAt)
		require.NoError(t, err)
		return id
	}

	purgedConnector, liveConnector := seedConnector(&pastRetention), seedConnector(nil)
	purgedDashboard, liveDashboard := seedDashboard(&pastRetention), seedDashboard(nil)
	purgedFolder, liveFolder := seedFolder(&pastRetention), seedFolder(nil)
	for _, r := range []struct{ resourceType, id string }{
		{"connector", purgedConnector}, {"connector", liveConnector},
		{"dashboard", purgedDashboard}, {"dashboard", liveDashboard},
		{"folder", purgedFolder}, {"folder", liveFolder},
	} {
		seedPendingACLForResource(t, db, f.orgID, r.resourceType, r.id)
	}

	New(db, nil).purgeTrash(ctx)

	for _, r := range []struct {
		resourceType string
		id           string
		want         int
	}{
		{"notebook", purgedNB, 0}, {"notebook", trashedNB, 1}, {"notebook", liveNB, 1},
		{"agent_session", purgedSession, 0}, {"agent_session", trashedSession, 1}, {"agent_session", liveSession, 1},
		{"connector", purgedConnector, 0}, {"connector", liveConnector, 1},
		{"dashboard", purgedDashboard, 0}, {"dashboard", liveDashboard, 1},
		{"folder", purgedFolder, 0}, {"folder", liveFolder, 1},
	} {
		require.Equal(t, r.want, countPendingFor(t, db, r.resourceType, r.id),
			"pending rows for %s %s", r.resourceType, r.id)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/scheduler/ -run 'TestPurgeTrashRemovesPendingACLsOfPurgedResources' -count=1
```

Expected: FAIL — past-retention pending rows still count 1 where 0 is expected.

**Step 3: Write minimal implementation** — in `internal/scheduler/scheduler.go`, replace the notebook-purge statement in `purgeTrash` with:

```go
	// The notebook purge must also clear the agent_session ACL rows and pending
	// ACL rows of the sessions that the notebook FK cascade removes: neither
	// table has an FK to agent_sessions, so those rows would leak. Notebook
	// pending rows are cleaned in the same statement. Data-modifying CTEs keep
	// the notebook delete and both cleanups atomic.
	if _, err := s.db.Pool.Exec(ctx, `
		WITH purged AS (
			DELETE FROM notebooks
			WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - INTERVAL '7 days'
			RETURNING id
		), session_acl_cleanup AS (
			DELETE FROM acl_entries
			WHERE resource_type = 'agent_session'
			  AND resource_id IN (
				  SELECT s.id FROM agent_sessions s
				  WHERE s.notebook_id IN (SELECT id FROM purged)
			  )
		), session_pending_cleanup AS (
			DELETE FROM pending_acl_entries
			WHERE resource_type = 'agent_session'
			  AND resource_id IN (
				  SELECT s.id FROM agent_sessions s
				  WHERE s.notebook_id IN (SELECT id FROM purged)
			  )
		), notebook_pending_cleanup AS (
			DELETE FROM pending_acl_entries
			WHERE resource_type = 'notebook'
			  AND resource_id IN (SELECT id FROM purged)
		)
		SELECT COUNT(*) FROM purged
	`); err != nil {
		slog.Warn("scheduler: purge trash", "table", "notebooks", "error", err)
	}
```

Replace the connectors/dashboards/folders loop with:

```go
	// Each table purge also consumes the pending ACL rows staged for the
	// deleted resources; pending_acl_entries has no FK to clean up via cascade.
	pendingResourceType := map[string]string{
		"connectors": "connector",
		"dashboards": "dashboard",
		"folders":    "folder",
	}
	for _, table := range []string{"connectors", "dashboards", "folders"} {
		_, err := s.db.Pool.Exec(ctx, fmt.Sprintf(`
			WITH purged AS (
				DELETE FROM %s
				WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - INTERVAL '7 days'
				RETURNING id
			)
			DELETE FROM pending_acl_entries
			WHERE resource_type = '%s' AND resource_id IN (SELECT id FROM purged)`,
			table, pendingResourceType[table]))
		if err != nil {
			slog.Warn("scheduler: purge trash", "table", table, "error", err)
		}
	}
```

**Step 4: Run tests to verify they pass**

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/scheduler/ -run 'TestPurgeTrash' -count=1
```

Expected: PASS (the new test plus the existing session-ACL purge test).

**Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/purge_pending_test.go
git commit -m "fix(scheduler): purge pending ACLs for trashed resources"
```

---

### Task 12: Frontend setup (`npm ci`)

**Files:**
- No source changes — this task prepares the worktree for the frontend test tasks. `web/node_modules` does not exist in this worktree.

**Step 1: Confirm the lockfile exists**

```bash
cd web && ls package-lock.json
```

Expected: `package-lock.json`.

**Step 2: Install dependencies**

```bash
cd web && npm ci
```

Expected: install completes without errors.

**Step 3: Verify the test environment with an existing suite**

```bash
cd web && npm run test:run -- src/test/PermissionsPanel.test.tsx
```

Expected: PASS (all existing PermissionsPanel tests). If Vitest reports a missing browser/JSdom environment, re-run `npm ci` to repair a partial install.

**Step 4: Verify the type checker runs**

```bash
cd web && npx tsc --noEmit
```

Expected: no output (clean).

**Step 5: Commit**

No commit — this task only prepares the worktree environment.

---

### Task 13: Extract shared email helpers (`looksLikeEmail` + `normalizeEmail`)

**Files:**
- Create: `web/src/utils/email.ts`
- Create: `web/src/utils/email.test.ts`
- Modify: `web/src/pages/GroupsPage.tsx` (remove the local `looksLikeEmail`, import the shared one)

**Step 1: Write the failing test** — create `web/src/utils/email.test.ts`:

```ts
import { describe, test, expect } from 'vitest'
import { looksLikeEmail, normalizeEmail } from './email'

describe('looksLikeEmail', () => {
  test('accepts a plain address', () => expect(looksLikeEmail('future@example.com')).toBe(true))
  test('accepts surrounding whitespace', () => expect(looksLikeEmail('  future@example.com  ')).toBe(true))
  test('rejects a missing domain', () => expect(looksLikeEmail('future@')).toBe(false))
  test('rejects a missing local part', () => expect(looksLikeEmail('@example.com')).toBe(false))
  test('rejects inner whitespace', () => expect(looksLikeEmail('fu ture@example.com')).toBe(false))
  test('rejects a plain name', () => expect(looksLikeEmail('Alice')).toBe(false))
})

describe('normalizeEmail', () => {
  test('trims and lowercases', () =>
    expect(normalizeEmail('  Future.User@Example.COM ')).toBe('future.user@example.com'))
})
```

**Step 2: Run test to verify it fails**

```bash
cd web && npm run test:run -- src/utils/email.test.ts
```

Expected: FAIL — cannot resolve `./email` (the module does not exist).

**Step 3: Write minimal implementation** — create `web/src/utils/email.ts`:

```ts
/** True when the value looks like an email address typed for a not-yet-registered user. */
export function looksLikeEmail(value: string): boolean {
  const v = value.trim()
  const at = v.indexOf('@')
  return at > 0 && at < v.length - 1 && !/\s/.test(v)
}

/** Canonical email spelling used by the pending-subject APIs (the server lowercases too). */
export function normalizeEmail(value: string): string {
  return value.trim().toLowerCase()
}
```

In `web/src/pages/GroupsPage.tsx`:

1. Add after the existing `groupLabel` import:

```ts
import { looksLikeEmail } from '../utils/email'
```

2. Delete the local helper (currently between the `MemberDropdownProps` interface and `formatPendingNotice`):

```ts
function looksLikeEmail(value: string): boolean {
  const v = value.trim()
  const at = v.indexOf('@')
  return at > 0 && at < v.length - 1 && !/\s/.test(v)
}
```

The call site at `const canAddPending = !!onAddPending && filtered.length === 0 && looksLikeEmail(query)` is unchanged.

**Step 4: Run tests to verify they pass**

```bash
cd web && npm run test:run -- src/utils/email.test.ts src/test/GroupsPage.test.tsx
```

Expected: PASS (new helper tests plus the existing GroupsPage pending-member tests, which prove the extraction is behavior-preserving).

**Step 5: Commit**

```bash
git add web/src/utils/email.ts web/src/utils/email.test.ts web/src/pages/GroupsPage.tsx
git commit -m "refactor(web): extract shared email helpers"
```

---

### Task 14: PermissionsPanel offers and renders pending users

**Files:**
- Modify: `web/src/components/PermissionsPanel.tsx` (types, Avatar, SubjectPicker, known-emails, subject labels, add/save handlers, CSS)
- Modify: `web/src/types/index.ts` (`ACLEntry`)
- Test: `web/src/test/PermissionsPanel.test.tsx` (append)

**Step 1: Write the failing tests** — append to `web/src/test/PermissionsPanel.test.tsx`:

```tsx
// ── Pending users ───────────────────────────────────────────────────────────

describe('Pending users', () => {
  test('offers a pending option for an email that matches nobody and adds it as a draft', async () => {
    renderPanel()
    await waitForAclLoaded()
    await openComposer()

    const input = screen.getByRole('combobox', { name: /search people and groups/i })
    fireEvent.change(input, { target: { value: 'Future.User@Example.com' } })

    const option = await screen.findByRole('option', { name: /future\.user@example\.com/i })
    expect(within(option).getByText('Pending — awaiting first login')).toBeInTheDocument()
    fireEvent.mouseDown(option)

    // The composer shows the pending subject and defaults to view.
    expect(await screen.findByText('future.user@example.com')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /^Add$/i }))

    // The draft row renders with the pending secondary line and chips.
    const row = findEntryRow('future.user@example.com')
    expect(row).not.toBeNull()
    expect(within(row!).getByText('Pending — awaiting first login')).toBeInTheDocument()
    expect(within(row!).getByRole('button', { name: 'view' })).toHaveAttribute('aria-pressed', 'true')
  })

  test('saving a pending entry PUTs pending_user with the lowercased email', async () => {
    let putBody: { entries: Array<{ subject_type: string; subject_id: string; actions: string[] }> } | null = null
    server.use(
      http.put('/api/v1/acl/notebook/nb-1', async ({ request }) => {
        putBody = (await request.json()) as typeof putBody
        return HttpResponse.json(putBody!.entries.map((e, i) => ({ id: `e-${i}`, ...e })))
      }),
    )
    renderPanel()
    await waitForAclLoaded()
    await openComposer()

    fireEvent.change(screen.getByRole('combobox', { name: /search people and groups/i }), {
      target: { value: 'Future.User@Example.com' },
    })
    fireEvent.mouseDown(await screen.findByRole('option', { name: /future\.user@example\.com/i }))
    fireEvent.click(await screen.findByRole('button', { name: /^Add$/i }))
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    const pending = putBody!.entries.find((e) => e.subject_type === 'pending_user')
    expect(pending).toMatchObject({ subject_type: 'pending_user', subject_id: 'future.user@example.com' })
    expect(pending!.actions).toEqual(['view'])
  })

  test('renders staged pending rows returned by GET and removes them on save', async () => {
    server.use(
      http.get('/api/v1/acl/notebook/nb-1', () => HttpResponse.json([
        ...ACL_ENTRIES,
        {
          id: 'pending-1', org_id: 'org-1', resource_type: 'notebook', resource_id: 'nb-1',
          subject_type: 'pending_user', subject_id: 'future@example.com', pending: true,
          actions: ['view', 'edit'], created_at: '2026-01-01T00:00:00Z',
        },
      ])),
    )
    let putBody: unknown = null
    server.use(
      http.put('/api/v1/acl/notebook/nb-1', async ({ request }) => {
        putBody = await request.json()
        return HttpResponse.json([])
      }),
    )
    renderPanel()
    await waitForAclLoaded()

    const row = findEntryRow('future@example.com')
    expect(row).not.toBeNull()
    expect(within(row!).getByText('Pending — awaiting first login')).toBeInTheDocument()
    fireEvent.click(within(row!).getByTitle('Remove'))
    fireEvent.click(await screen.findByRole('button', { name: /^Save$/i }))

    await waitFor(() => expect(putBody).not.toBeNull())
    const entries = (putBody as { entries: Array<{ subject_type: string }> }).entries
    expect(entries.some((e) => e.subject_type === 'pending_user')).toBe(false)
  })

  test('does not offer pending when the typed email already belongs to a member', async () => {
    renderPanel()
    await waitForAclLoaded()
    await openComposer()

    fireEvent.change(screen.getByRole('combobox', { name: /search people and groups/i }), {
      target: { value: 'alice@test.com' },
    })
    expect(await screen.findByRole('option', { name: /alice admin/i })).toBeInTheDocument()
    expect(screen.queryByText(/pending — awaiting first login/i)).toBeNull()
  })
})
```

**Step 2: Run tests to verify they fail**

```bash
cd web && npm run test:run -- src/test/PermissionsPanel.test.tsx
```

Expected: the four new tests FAIL — no pending option renders (the picker only lists people/groups/everyone), and no row shows the pending secondary line.

**Step 3: Write minimal implementation** — edit `web/src/components/PermissionsPanel.tsx`:

(a) Add `Mail` to the lucide-react import and import the email helpers:

```ts
import {
  X,
  Users,
  UsersRound,
  UserPlus,
  Search,
  Check,
  Copy,
  ShieldCheck,
  Mail,
} from 'lucide-react'
import { api } from '../api/client'
import { groupLabel } from '../utils/groupLabel'
import { chatLinkUrl } from '../utils/chatLink'
import { looksLikeEmail, normalizeEmail } from '../utils/email'
import { ConfirmModal } from './ConfirmModal'
```

(b) Extend the types:

```ts
type SubjectType = 'user' | 'group' | 'org_role' | 'pending_user'
```

```ts
interface AclEntry {
  id: string
  subject_type: SubjectType
  subject_id: string
  actions: string[]
  pending?: boolean
}
```

(c) Update `Avatar` for the muted pending treatment:

```tsx
function Avatar({ name, type, size = 30 }: { name: string; type: SubjectType; size?: number }) {
  return (
    <span
      aria-hidden="true"
      className={`access-avatar${type === 'user' ? ' is-user' : type === 'pending_user' ? ' is-pending' : ''}`}
      style={{ width: size, height: size, fontSize: size <= 24 ? 10 : 11 }}
    >
      {type === 'user'
        ? initials(name)
        : type === 'pending_user'
          ? <Mail size={13} />
          : type === 'group'
            ? <Users size={13} />
            : <UsersRound size={13} />}
    </span>
  )
}
```

(d) Extend `PickerOption.section` and pass known emails into the picker:

```ts
interface PickerOption {
  key: string
  kind: SubjectType
  name: string
  secondary?: string
  section: 'People' | 'Groups' | 'Organization' | 'Pending'
}

interface SubjectPickerProps {
  options: PickerOption[]
  knownEmails: Set<string>
  onPick: (option: PickerOption) => void
  onCancel: () => void
}
```

Replace `SubjectPicker` entirely with:

```tsx
function SubjectPicker({ options, knownEmails, onPick, onCancel }: SubjectPickerProps) {
  const [query, setQuery] = useState('')
  const [focusedIdx, setFocusedIdx] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  const q = query.trim().toLowerCase()
  const filtered = q
    ? options.filter((o) =>
        o.name.toLowerCase().includes(q) ||
        (o.secondary ?? '').toLowerCase().includes(q))
    : options

  // Offer an explicit "add by email" action when the query looks like an email
  // that matches no person, group, or existing entry — never a global user
  // search. The email is lowercased to match the pending API's canonical form.
  const pendingEmail =
    q.length > 0 && looksLikeEmail(q) && !knownEmails.has(normalizeEmail(q))
      ? normalizeEmail(q)
      : null
  const pendingOption: PickerOption | null =
    pendingEmail && filtered.length === 0
      ? {
          key: subjectKey('pending_user', pendingEmail),
          kind: 'pending_user',
          name: pendingEmail,
          secondary: 'Pending — awaiting first login',
          section: 'Pending',
        }
      : null
  const displayOptions = pendingOption ? [...filtered, pendingOption] : filtered

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setFocusedIdx((i) => Math.min(i + 1, displayOptions.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setFocusedIdx((i) => Math.max(i - 1, -1))
    } else if (e.key === 'Enter' && focusedIdx >= 0 && displayOptions[focusedIdx]) {
      e.preventDefault()
      onPick(displayOptions[focusedIdx])
    } else if (e.key === 'Escape') {
      // Close the picker first; the dialog's window-level Escape handler
      // skips events already handled locally (defaultPrevented).
      e.preventDefault()
      e.stopPropagation()
      onCancel()
    }
  }

  return (
    <div className="access-picker">
      <div className="access-picker-search">
        <Search size={14} aria-hidden="true" className="access-picker-search-icon" />
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls="access-picker-listbox"
          aria-activedescendant={focusedIdx >= 0 ? `access-picker-option-${focusedIdx}` : undefined}
          aria-label="Search people and groups"
          autoComplete="off"
          placeholder="Search people and groups…"
          value={query}
          onChange={(e) => { setQuery(e.target.value); setFocusedIdx(-1) }}
          onKeyDown={handleKeyDown}
        />
        <button
          type="button"
          className="access-icon-btn"
          onClick={onCancel}
          title="Cancel"
          aria-label="Cancel adding access"
        >
          <X size={14} />
        </button>
      </div>
      <ul id="access-picker-listbox" role="listbox" aria-label="People and groups" className="access-picker-list">
        {displayOptions.length === 0 && (
          <li className="access-picker-empty">
            {q ? <>No matches for “{query}”.</> : 'No people or groups left to add.'}
          </li>
        )}
        {displayOptions.map((option, idx) => {
          const showSection = idx === 0 || displayOptions[idx - 1].section !== option.section
          return (
            <Fragment key={option.key}>
              {showSection && (
                <li className="access-label access-picker-section" role="presentation">{option.section}</li>
              )}
              <li
                id={`access-picker-option-${idx}`}
                role="option"
                aria-selected={idx === focusedIdx}
                className={`access-picker-option${idx === focusedIdx ? ' is-focused' : ''}`}
                onMouseEnter={() => setFocusedIdx(idx)}
                onMouseDown={(e) => { e.preventDefault(); onPick(option) }}
              >
                <Avatar name={option.name} type={option.kind} size={26} />
                <span className="access-picker-option-name">{option.name}</span>
                {option.secondary && <span className="access-picker-option-meta">{option.secondary}</span>}
              </li>
            </Fragment>
          )
        })}
      </ul>
    </div>
  )
}
```

(e) In `PermissionsPanel`, after the `visibleEntries` / `inheritedCount` derivations (before `pickerOptions`), add the known-emails set and pass it to the picker:

```tsx
  const visibleKeys = new Set(visibleEntries.map((e) => subjectKey(e.subject_type, e.subject_id)))

  // Emails that must not be offered as "pending": current members (even when
  // their entry is already in the draft) and any staged pending entry.
  const knownEmails = new Set<string>(members.map((m) => m.email.toLowerCase()))
  for (const e of visibleEntries) {
    if (e.subject_type === 'pending_user') knownEmails.add(e.subject_id.toLowerCase())
    if (e.subject_type === 'user') {
      const m = members.find((m) => m.user_id === e.subject_id)
      if (m) knownEmails.add(m.email.toLowerCase())
    }
  }
```

and change the picker usage to:

```tsx
                    {composerPhase === 'picking' && (
                      <SubjectPicker
                        options={pickerOptions}
                        knownEmails={knownEmails}
                        onPick={handlePickSubject}
                        onCancel={() => setComposerPhase('idle')}
                      />
                    )}
```

(f) Add the pending branches to `subjectName` / `subjectSecondary`:

```tsx
  function subjectName(entry: AclEntry): string {
    if (entry.subject_type === 'pending_user') return entry.subject_id
    if (entry.subject_type === 'user') {
      const m = members.find((m) => m.user_id === entry.subject_id)
      return m ? (m.name || m.email) : entry.subject_id
    } else if (entry.subject_type === 'org_role') {
      return entry.subject_id === 'everyone' ? 'Everyone' : entry.subject_id
    } else {
      const g = groups.find((g) => g.id === entry.subject_id)
      return g ? groupLabel(g) : entry.subject_id
    }
  }

  function subjectSecondary(entry: AclEntry): string {
    if (entry.subject_type === 'pending_user') return 'Pending — awaiting first login'
    if (entry.subject_type === 'user') {
      const m = members.find((m) => m.user_id === entry.subject_id)
      return m ? m.email : 'User'
    }
    if (entry.subject_type === 'group') return 'Group'
    return 'All organization members'
  }
```

(g) Fix subject-id extraction for keys containing punctuation (emails) in `handleAddEntry`:

```tsx
  function handleAddEntry() {
    if (!newSubject || newActions.length === 0 || aclLoading) return
    const current = draft ?? aclData ?? []
    const updated: AclEntry[] = [
      ...current,
      {
        id: '',
        subject_type: newSubject.kind,
        subject_id: newSubject.key.slice(newSubject.key.indexOf(':') + 1),
        actions: newActions,
      },
    ]
    setDraft(updated)
    setNewSubject(null)
    setNewActions([])
    setComposerPhase('idle')
  }
```

(h) Drop the synthesized `pending` flag from the PUT payload in `handleSave`:

```tsx
  function handleSave() {
    if (draft === null) return
    saveAcl.mutate(
      draft.map(({ id: _id, pending: _pending, ...rest }) => ({
        ...rest,
        actions: constrainActions(resourceType, rest.actions),
      }))
    )
  }
```

(i) Add the muted pending avatar CSS to the `css` template string, after `.access-avatar.is-user`:

```css
.access-avatar.is-pending {
  background: color-mix(in srgb, var(--text-primary) 4%, transparent);
  border-style: dashed;
  color: var(--text-muted);
}
```

(j) In `web/src/types/index.ts`, extend the shared ACL type:

```ts
export interface ACLEntry {
  id: string
  org_id: string
  resource_type: string
  resource_id: string
  subject_type: 'user' | 'group' | 'org_role' | 'pending_user'
  subject_id: string
  actions: string[]
  created_at: string
  pending?: boolean
}
```

**Step 4: Run tests to verify they pass**

```bash
cd web && npm run test:run -- src/test/PermissionsPanel.test.tsx src/test/GroupsPage.test.tsx && npx tsc --noEmit
```

Expected: PASS; `tsc` reports no errors.

**Step 5: Commit**

```bash
git add web/src/components/PermissionsPanel.tsx web/src/types/index.ts web/src/test/PermissionsPanel.test.tsx
git commit -m "feat(web): add pending users to the permissions panel"
```

---

### Task 15: Warehouse table grants stage pending grants by email

**Files:**
- Modify: `web/src/api/warehouses.ts` (`WarehouseSubjectType`)
- Modify: `web/src/components/WarehouseTableGrants.tsx` (state, subject resolution, rows, form, styles)
- Test: `web/src/components/WarehouseTableGrants.test.tsx` (append)

**Step 1: Write the failing tests** — append to `web/src/components/WarehouseTableGrants.test.tsx`:

```tsx
const PENDING_GRANT: WarehouseGrant = {
  id: 'gr-pending', org_id: 'org-1', warehouse_id: 'wh-1', subject_type: 'pending_user',
  subject_id: 'future@example.com', subject_name: 'future@example.com', subject_email: 'future@example.com',
  database: 'analytics', table: 'sessions', created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
}

describe('pending grants', () => {
  test('stages a pending grant from the email affordance', async () => {
    let posted: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/warehouses/wh-1/grants', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(
          { ...PENDING_GRANT, database: 'analytics', table: 'users' },
          { status: 201 },
        )
      }),
    )
    renderGrants()
    await screen.findAllByText('Data Team')

    fireEvent.change(screen.getByLabelText('Pending email'), { target: { value: 'Future@Example.COM' } })
    await screen.findByRole('option', { name: 'analytics' })
    fireEvent.change(screen.getByLabelText('Database'), { target: { value: 'analytics' } })
    await waitFor(() => expect(screen.getByLabelText('users')).toBeInTheDocument())
    fireEvent.click(screen.getByLabelText('users'))
    fireEvent.click(screen.getByText('Add grant'))

    await waitFor(() =>
      expect(posted).toEqual({
        subject_type: 'pending_user',
        subject_id: 'future@example.com',
        database: 'analytics',
        table: 'users',
      }),
    )
  })

  test('renders staged rows with the pending badge and lets them be removed', async () => {
    let deleted = ''
    server.use(
      http.get('/api/v1/warehouses/wh-1/grants', () => HttpResponse.json([...GRANTS, PENDING_GRANT])),
      http.delete('/api/v1/warehouses/wh-1/grants/:gid', ({ params }) => {
        deleted = String(params.gid)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderGrants()
    expect(await screen.findByText('future@example.com')).toBeInTheDocument()
    expect(screen.getAllByText('Pending — awaiting first login').length).toBeGreaterThan(0)
    fireEvent.click(screen.getByLabelText('Remove grant analytics.sessions'))
    await waitFor(() => expect(deleted).toBe('gr-pending'))
  })
})
```

**Step 2: Run tests to verify they fail**

```bash
cd web && npm run test:run -- src/components/WarehouseTableGrants.test.tsx
```

Expected: the two new tests FAIL — no `Pending email` input exists and the staged row renders without the pending label/badge.

**Step 3: Write minimal implementation**

In `web/src/api/warehouses.ts`:

```ts
export type WarehouseSubjectType = 'user' | 'group' | 'everyone' | 'pending_user'
```

In `web/src/components/WarehouseTableGrants.tsx`:

(a) Import the email helpers and extend the subject list / row shape:

```ts
import { groupLabel } from '../utils/groupLabel'
import { looksLikeEmail, normalizeEmail } from '../utils/email'
import type { Group, Member } from '../types'
```

```ts
interface SubjectRow {
  key: string
  subjectType: WarehouseSubjectType
  subjectId: string
  label: string
  detail?: string
  pending: boolean
  grants: WarehouseGrant[]
}

const SUBJECT_TYPES: WarehouseSubjectType[] = ['user', 'group', 'everyone', 'pending_user']
```

(b) Add the pending-email state and the resolved subject candidate:

```tsx
  const [subjectSelection, setSubjectSelection] = useState('')
  const [pendingEmail, setPendingEmail] = useState('')
```

after the other `useState` declarations, and after the member/group queries:

```tsx
  // A valid email in the "Pending email" field takes precedence over the
  // subject select: it stages a grant for someone who has no account yet.
  const pendingCandidate = useMemo(
    () =>
      looksLikeEmail(pendingEmail)
        ? { subjectType: 'pending_user' as const, subjectId: normalizeEmail(pendingEmail) }
        : null,
    [pendingEmail],
  )
```

(c) Make `grantedTables` use the resolved subject:

```tsx
  // Tables already granted to the currently selected subject in the selected
  // database — rendered as checked + disabled instead of offering a duplicate.
  const grantedTables = useMemo(() => {
    const parsed = pendingCandidate ?? parseSubjectKey(subjectSelection)
    const set = new Set<string>()
    if (!parsed) return set
    for (const grant of grants) {
      if (
        grant.subject_type === parsed.subjectType &&
        grant.subject_id === parsed.subjectId &&
        grant.database === database
      ) {
        set.add(grant.table)
      }
    }
    return set
  }, [grants, subjectSelection, pendingCandidate, database])
```

(d) Add the pending branch to `subjectLabel`:

```tsx
  function subjectLabel(
    subjectType: WarehouseSubjectType,
    subjectId: string,
    grant?: WarehouseGrant,
  ): string {
    if (subjectType === 'everyone') return 'Everyone'
    if (subjectType === 'user') {
      return memberNames.get(subjectId) ?? grant?.subject_name ?? grant?.subject_email ?? subjectId
    }
    if (subjectType === 'pending_user') {
      return grant?.subject_email ?? grant?.subject_name ?? subjectId
    }
    return groupNames.get(subjectId) ?? grant?.subject_name ?? subjectId
  }
```

(e) Update the `rows` memo so pending rows label by email and carry the pending flag:

```tsx
  const rows = useMemo<SubjectRow[]>(() => {
    const byKey = new Map<string, SubjectRow>()
    for (const grant of grants) {
      const key = subjectKey(grant.subject_type, grant.subject_id)
      let row = byKey.get(key)
      if (!row) {
        const label =
          grant.subject_type === 'everyone'
            ? 'Everyone'
            : grant.subject_type === 'user'
              ? memberNames.get(grant.subject_id) ??
                grant.subject_name ??
                grant.subject_email ??
                grant.subject_id
              : grant.subject_type === 'pending_user'
                ? grant.subject_email ?? grant.subject_name ?? grant.subject_id
                : groupNames.get(grant.subject_id) ?? grant.subject_name ?? grant.subject_id
        row = {
          key,
          subjectType: grant.subject_type,
          subjectId: grant.subject_id,
          label,
          detail: grant.subject_type === 'user' ? grant.subject_email : undefined,
          pending: grant.subject_type === 'pending_user',
          grants: [],
        }
        byKey.set(key, row)
      }
      row.grants.push(grant)
    }
    return Array.from(byKey.values())
  }, [grants, memberNames, groupNames])
```

(f) Use the resolved subject in the mutation and clear the email on success:

```tsx
  const addGrants = useMutation({
    mutationFn: async () => {
      const parsed = pendingCandidate ?? parseSubjectKey(subjectSelection)
      if (!parsed) throw new Error('Select a subject')
      if (!database) throw new Error('Select a database')
      const targets = Array.from(checkedTables).filter((t) => !grantedTables.has(t))
      if (targets.length === 0) throw new Error('Select at least one table')
      // The endpoint is idempotent per grant, so allSettled retries are safe
      // and one bad table name does not abort the rest.
      const settled = await Promise.allSettled(
        targets.map((table) =>
          createGrant(warehouseId, {
            subject_type: parsed.subjectType,
            subject_id: parsed.subjectId,
            database,
            table,
          }),
        ),
      )
      const fulfilled = settled
        .filter((r): r is PromiseFulfilledResult<WarehouseGrantCreateResult> => r.status === 'fulfilled')
        .map((r) => r.value)
      const failures = settled
        .filter((r): r is PromiseRejectedResult => r.status === 'rejected')
        .map((r) => (r.reason instanceof Error ? r.reason.message : String(r.reason)))
      return { parsed, fulfilled, failures, requested: targets.length }
    },
    onSuccess: ({ parsed, fulfilled, failures, requested }) => {
      const key = subjectKey(parsed.subjectType, parsed.subjectId)
      if (fulfilled.length > 0) {
        if (fulfilled.some((g) => g.warning)) {
          setWarnings((prev) => ({ ...prev, [key]: true }))
        } else {
          // A warning-free response proves the subject can use a service now.
          clearWarning(key)
        }
      }
      setCheckedTables(new Set())
      setPendingEmail('')
      invalidateAfterGrantChange()
      if (failures.length > 0) {
        setError(`${failures.length} of ${requested} grants failed: ${failures.join('; ')}`)
      } else {
        setError(null)
      }
    },
    onError: (err: Error) => setError(err.message),
  })
```

(g) Update `canSubmit`:

```tsx
  const canSubmit =
    !!(pendingCandidate ?? subjectSelection) && !!database && pendingTables.length > 0 && !addGrants.isPending
```

(h) Render the pending badge and the email input. In the grants table cell, change:

```tsx
                <td style={cellStyle}>
                  <span style={{ fontWeight: 600 }}>{row.label}</span>
                  {row.detail && <span style={styles.detail}>{row.detail}</span>}
```

to:

```tsx
                <td style={cellStyle}>
                  <span style={{ fontWeight: 600 }}>{row.label}</span>
                  {row.detail && <span style={styles.detail}>{row.detail}</span>}
                  {row.pending && <span style={styles.pendingBadge}>Pending — awaiting first login</span>}
```

and in the add form, after the Subject `<label>` block, insert:

```tsx
        <label style={styles.field}>
          <span style={styles.fieldLabel}>Pending email</span>
          <input
            aria-label="Pending email"
            style={styles.input}
            type="email"
            autoComplete="off"
            placeholder="email@example.com (no account yet)"
            value={pendingEmail}
            onChange={(e) => setPendingEmail(e.target.value)}
          />
        </label>
```

(i) Add the badge style next to `warningBadge` in the `styles` object:

```ts
  pendingBadge: {
    display: 'inline-block',
    marginLeft: 8,
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-muted)',
    background: 'color-mix(in srgb, var(--text-primary) 6%, transparent)',
    border: '1px dashed var(--border)',
    borderRadius: 10,
    padding: '1px 8px',
    verticalAlign: 'middle',
  },
```

**Step 4: Run tests to verify they pass**

```bash
cd web && npm run test:run -- src/components/WarehouseTableGrants.test.tsx && npx tsc --noEmit
```

Expected: PASS (new pending tests + existing grant tests).

**Step 5: Commit**

```bash
git add web/src/api/warehouses.ts web/src/components/WarehouseTableGrants.tsx web/src/components/WarehouseTableGrants.test.tsx
git commit -m "feat(web): stage pending warehouse grants by email"
```

---

### Task 16: New-tables inbox stages pending grants by email

**Files:**
- Modify: `web/src/components/NewTablesInbox.tsx` (state, granted sets, mutation, row rendering, styles)
- Test: `web/src/components/NewTablesInbox.test.tsx` (append)

**Design note:** `handleWarehouseNewTables`' server-side `NOT EXISTS` only checks real grants, so a table with only a staged grant still appears in the inbox. The client distinguishes `realGrantedTables` from `pendingGrantedTables`: a pending-only table shows a "pending — awaiting first login" badge while still allowing a real grant for someone else. The staged row itself is deletable from the Table grants matrix (Task 15).

**Step 1: Write the failing tests** — append to `web/src/components/NewTablesInbox.test.tsx`:

```tsx
describe('pending grants', () => {
  test('stages a pending grant from the inbox email input', async () => {
    let posted: Record<string, unknown> | null = null
    server.use(
      http.post('/api/v1/warehouses/wh-1/grants', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(
          {
            id: 'gr-pending', org_id: 'org-1', warehouse_id: 'wh-1',
            subject_type: 'pending_user', subject_id: 'future@example.com',
            subject_email: 'future@example.com', database: 'analytics', table: 'events',
            created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
          },
          { status: 201 },
        )
      }),
    )
    renderInbox()
    await screen.findByText('analytics.events')

    fireEvent.change(screen.getByLabelText('Pending email for analytics.events'), {
      target: { value: 'Future@Example.com' },
    })
    fireEvent.click(screen.getByLabelText('Add grant for analytics.events'))

    await waitFor(() =>
      expect(posted).toEqual({
        subject_type: 'pending_user',
        subject_id: 'future@example.com',
        database: 'analytics',
        table: 'events',
      }),
    )
  })

  test('marks a table with only a staged grant as pending, not granted', async () => {
    server.use(
      http.get('/api/v1/warehouses/wh-1/grants', () =>
        HttpResponse.json([
          {
            id: 'gr-pending', org_id: 'org-1', warehouse_id: 'wh-1',
            subject_type: 'pending_user', subject_id: 'future@example.com',
            subject_email: 'future@example.com', database: 'analytics', table: 'events',
            created_by: 'user-1', created_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
    )
    renderInbox()
    expect(await screen.findByText('analytics.events')).toBeInTheDocument()
    expect(screen.getAllByText(/pending — awaiting first login/i).length).toBeGreaterThan(0)
    expect(screen.queryByText('already granted')).toBeNull()
  })
})
```

**Step 2: Run tests to verify they fail**

```bash
cd web && npm run test:run -- src/components/NewTablesInbox.test.tsx
```

Expected: the two new tests FAIL — there is no per-row pending email input, and a pending-only grant currently marks the table "already granted".

**Step 3: Write minimal implementation** — edit `web/src/components/NewTablesInbox.tsx`:

(a) Import the email helpers:

```ts
import { groupLabel } from '../utils/groupLabel'
import { looksLikeEmail, normalizeEmail } from '../utils/email'
```

(b) Add per-row pending email state:

```tsx
  const [selections, setSelections] = useState<Record<string, string>>({})
  const [pendingEmails, setPendingEmails] = useState<Record<string, string>>({})
  const [error, setError] = useState<string | null>(null)
```

(c) Split the granted set:

```tsx
  const realGrantedTables = useMemo(() => {
    const set = new Set<string>()
    for (const grant of grants) {
      if (grant.subject_type !== 'pending_user') set.add(`${grant.database}.${grant.table}`)
    }
    return set
  }, [grants])

  const pendingGrantedTables = useMemo(() => {
    const set = new Set<string>()
    for (const grant of grants) {
      if (grant.subject_type === 'pending_user') set.add(`${grant.database}.${grant.table}`)
    }
    return set
  }, [grants])
```

(d) Resolve the pending email in the mutation and clear it on success:

```tsx
  const addGrant = useMutation({
    mutationFn: ({ table, selection }: { table: WarehouseNewTable; selection: string }) => {
      const key = tableKey(table)
      const email = pendingEmails[key] ?? ''
      // A valid email takes precedence over the subject select: it stages the
      // grant for someone who has no account yet.
      const parsed = looksLikeEmail(email)
        ? { subjectType: 'pending_user' as const, subjectId: normalizeEmail(email) }
        : parseSubjectKey(selection)
      if (!parsed) throw new Error('Select a subject or enter an email')
      return createGrant(warehouseId, {
        subject_type: parsed.subjectType,
        subject_id: parsed.subjectId,
        database: table.database,
        table: table.table,
      })
    },
    onSuccess: (_, { table }) => {
      const key = tableKey(table)
      setSelections((prev) => {
        if (!(key in prev)) return prev
        const next = { ...prev }
        delete next[key]
        return next
      })
      setPendingEmails((prev) => {
        if (!(key in prev)) return prev
        const next = { ...prev }
        delete next[key]
        return next
      })
      qc.invalidateQueries({ queryKey: ['warehouse-new-tables', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouse-grants', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouse-validation', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouses'] })
      qc.invalidateQueries({ queryKey: ['warehouse', warehouseId] })
      setError(null)
    },
    onError: (err: Error) => setError(err.message),
  })
```

(e) In the row render, replace the `granted` derivation and the select/action cells:

```tsx
                const key = tableKey(table)
                const selection = selections[key] ?? ''
                const pendingEmail = pendingEmails[key] ?? ''
                const pendingEmailValid = looksLikeEmail(pendingEmail)
                const pending = pendingKey === key
                const granted = realGrantedTables.has(key)
                const pendingStaged = pendingGrantedTables.has(key)
```

Replace the select cell with:

```tsx
                    <td style={styles.selectCell}>
                      {granted ? (
                        <span style={styles.grantedBadge}>already granted</span>
                      ) : (
                        <>
                          <select
                            aria-label={`Subject for ${key}`}
                            style={styles.input}
                            value={selection}
                            onChange={(e) =>
                              setSelections((prev) => ({ ...prev, [key]: e.target.value }))
                            }
                          >
                            <option value="">Select subject…</option>
                            <optgroup label="Users">
                              {members.map((m) => (
                                <option key={m.user_id} value={`user:${m.user_id}`}>
                                  {m.name || m.email}
                                </option>
                              ))}
                            </optgroup>
                            <optgroup label="Groups">
                              {groups
                                .filter((g) => !/^everyone$/i.test(g.name))
                                .map((g) => (
                                  <option key={g.id} value={`group:${g.id}`}>
                                    {groupLabel(g)}
                                  </option>
                                ))}
                            </optgroup>
                            <option value="everyone:everyone">Everyone</option>
                          </select>
                          <input
                            aria-label={`Pending email for ${key}`}
                            style={{ ...styles.input, marginTop: 4 }}
                            type="email"
                            autoComplete="off"
                            placeholder="email@example.com (pending)"
                            value={pendingEmail}
                            onChange={(e) =>
                              setPendingEmails((prev) => ({ ...prev, [key]: e.target.value }))
                            }
                          />
                          {pendingStaged && (
                            <span style={styles.pendingBadge}>pending — awaiting first login</span>
                          )}
                        </>
                      )}
                    </td>
```

and the action cell with:

```tsx
                    <td style={styles.actionCell}>
                      {!granted && (
                        <button
                          type="button"
                          aria-label={`Add grant for ${key}`}
                          style={{
                            ...styles.addBtn,
                            opacity: (selection || pendingEmailValid) && !pending ? 1 : 0.5,
                            cursor: (selection || pendingEmailValid) && !pending ? 'pointer' : 'not-allowed',
                          }}
                          disabled={(!selection && !pendingEmailValid) || pending}
                          onClick={() => addGrant.mutate({ table, selection })}
                        >
                          {pending ? 'Adding…' : 'Add grant'}
                        </button>
                      )}
                    </td>
```

(f) Add the badge style next to `grantedBadge` in the `styles` object:

```ts
  pendingBadge: {
    display: 'inline-block',
    marginTop: 4,
    fontSize: 11,
    fontWeight: 600,
    color: 'var(--text-muted)',
    background: 'color-mix(in srgb, var(--text-primary) 6%, transparent)',
    border: '1px dashed var(--border)',
    borderRadius: 10,
    padding: '2px 8px',
    whiteSpace: 'nowrap' as const,
  },
```

**Step 4: Run tests to verify they pass**

```bash
cd web && npm run test:run -- src/components/NewTablesInbox.test.tsx && npx tsc --noEmit
```

Expected: PASS (new pending tests + existing inbox tests).

**Step 5: Commit**

```bash
git add web/src/components/NewTablesInbox.tsx web/src/components/NewTablesInbox.test.tsx
git commit -m "feat(web): stage pending grants from the new-tables inbox"
```

---

### Task 17: Document the feature and run broader verification

**Files:**
- Modify: `AGENTS.md` (add a pending-user grants paragraph after the pre-provisioned group members paragraph)

**Step 1: Write the documentation** — locate the paragraph starting `**Pre-provisioned group members**:` in the "Key Patterns" section and insert this paragraph immediately after it:

```markdown
**Pending-user grants**: Pre-account users can be granted access anywhere a user/group subject exists. Staged rows live in `pending_acl_entries` and `pending_warehouse_table_grants` (both V127), keyed by lowercased email; `GET /acl/{type}/{id}` and `GET /warehouses/{id}/grants` surface them as `subject_type: "pending_user"` (subject_id = email, `pending: true` on ACL rows, id = the pending row UUID), and PUT/create accept `pending_user` with a validated (single-`@`, lowercased) email. Session shares accept pending users with exactly `["view"]`. Materialization runs inside every join transaction via `s.applyPendingAccess` immediately after `s.applyPendingGroups` (registration, org join, SSO provisioning/auto-join, invite redemption): staged ACL actions are unioned into any existing direct entry (`ON CONFLICT ... DO UPDATE`), warehouse grants dedupe with `DO NOTHING`, staged rows are consumed, and failures are audited (`acl.pending_materialize.error` / `warehouse.grant.pending_materialize.error`) without blocking first login. Treating a staged grant as executable is a bug: validation, the resolver, and the ClickHouse sync worker read only the canonical tables. Trash purge (`internal/scheduler/scheduler.go` `purgeTrash`) deletes pending ACL rows for purged notebooks/connectors/dashboards/folders and for the agent_session rows of purged notebooks; the empty-session sweep in `createSessionWithSharing` deletes a swept session's pending shares with its ACL rows. UI: `looksLikeEmail`/`normalizeEmail` live in `web/src/utils/email.ts`; PermissionsPanel offers "add by email" when the picker query matches nobody, and the warehouse table-grants/new-tables surfaces stage pending grants by email.
```

**Step 2: Run the targeted backend suites**

```bash
task fmt && task vet
```

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'Pending' -count=1
```

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/api/ -run 'TestACLGetAndPut|TestGrantCRUDAndEffectiveAccess|TestNormalizeSessionShareEntries' -count=1
```

```bash
AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable" AETHER_MASTER_KEY=dev-master-key-change-in-production AETHER_JWT_SECRET=dev-jwt-secret-change-in-production AETHER_REDIS_URL=redis://localhost:6379 go test -timeout 3m ./internal/scheduler/ -run 'TestPurgeTrash' -count=1
```

Expected: all PASS. (`-run 'Pending'` covers every test added by Tasks 1–11.)

Before opening the PR (repo rule: "Always run `task check` before pushing"), run the full Go gate once:

```bash
task check
```

This is the fmt + vet + tidy + full Go test pass; it is slower because the shared dev database accumulates data, which is why the per-task commands above stay targeted.

**Step 3: Run the frontend suite, type check, and build**

```bash
cd web && npm run test:run -- src/utils/email.test.ts src/test/PermissionsPanel.test.tsx src/components/WarehouseTableGrants.test.tsx src/components/NewTablesInbox.test.tsx
```

```bash
cd web && npx tsc --noEmit && npm run build
```

Expected: all tests PASS; `tsc -b` and the Vite build complete with no errors.

**Step 4: Real-browser validation (mandatory per AGENTS.md)**

Bring up the dev stack (`docker compose -f docker-compose.dev.yml up -d`) and validate all three surfaces with agent-browser (log in as `nova@heaven-labs.com` / `nova123`):

1. **PermissionsPanel** — open a notebook → Share/Permissions → "Add people or groups…" → type `future.person@example.com` → pick the pending option → grant `view` → Save → reload → the row still renders with "Pending — awaiting first login" → Remove → Save → reload → gone.
2. **Warehouse table grants** — Warehouse settings → Table grants → type a new email in "Pending email", select a database, check a table, Add grant → the row appears with the pending badge → remove the chip.
3. **New-tables inbox** — observe a table with no grants, type the email in the row's pending field, Add grant → the badge appears; also confirm no table with a staged grant shows "already granted".
4. Check `agent-browser errors` after each flow (blank screens / React errors mean a missing import or TS narrowing issue; restart the web container if Vite's cache is stale).

Note: the staged rows do not become ClickHouse access until the person actually joins; that is validated by the Go integration test in Task 4, not by the browser.

**Step 5: Commit**

```bash
git add AGENTS.md
git commit -m "docs: document pending-user grants"
```

---

## Done criteria

- Migration V127 applies cleanly and `TestPendingGrantTablesExist` passes.
- Staged rows materialize exactly once, union actions, and never block a join (`ApplyPendingAccess` + wrapper + all six call sites + the org-join integration test).
- ACL GET/PUT, session shares (create + PUT + sweep), warehouse grants (create/list/delete), and validation all handle `pending_user` per design §§5.1.2–5.1.4, with trash purge cleanup.
- Frontend: PermissionsPanel, WarehouseTableGrants, and NewTablesInbox stage/remove/render pending subjects; `looksLikeEmail`/`normalizeEmail` are shared; type check and build are clean.
- AGENTS.md documents the staged-grant invariant: resolver, validation, and the sync worker read only the canonical tables.



