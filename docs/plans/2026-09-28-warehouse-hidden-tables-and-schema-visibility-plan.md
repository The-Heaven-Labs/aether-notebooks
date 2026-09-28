# Warehouse Hidden-Table Patterns & Per-User Schema Visibility — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let org admins hide "garbage" tables in a warehouse with validated regex patterns, and make the schema browser show non-admin users only the tables their warehouse grants allow.

**Architecture:** One new `TEXT[]` column on `warehouses` plus a visibility pipeline in `handleConnectorSchema`: connector allow/deny → hidden patterns (granted tables protected) → per-user effective grants (managed mode only). Patterns are curated at write time and applied to the schema response, schema snapshots, and the new-tables inbox. No ClickHouse DDL or grant rows are affected by patterns.

**Tech Stack:** Go 1.25 (stdlib `net/http`, pgx v5, Go RE2), React + TypeScript + React Query + Vitest/MSW, Flyway-style SQL migrations.

**Design doc:** `docs/plans/2026-09-28-warehouse-hidden-tables-and-schema-visibility-design.md`

**Working branch:** `feat/warehouse-hidden-tables-schema-visibility` (already exists with the design doc committed).

---

## Conventions for every Go test run

- Make sure infra is up first: `task infra:up`
- Every Go test invocation must carry `-timeout 3m` (AGENTS.md requirement). Example:
  `go test ./internal/api/ -run TestWarehouseHiddenPatternsUpdate -v -timeout 3m`
- Tests hit the real dev Postgres (and ClickHouse for schema/fixture tests). A missing ClickHouse skips; a missing Postgres fails.

---

### Task 1: Migration + warehouse JSON plumbing

**Files:**
- Create: `internal/database/migrations/V120__warehouse_hidden_table_patterns.sql`
- Modify: `internal/api/warehouse_handlers.go:19-43` (`warehouseJSON`, `warehouseSelectColumns`), `:85-91` (`scanWarehouseRow`)
- Test: `internal/api/warehouse_handlers_test.go` (add a test near `TestWarehouseCRUD`)

**Step 1: Write the failing test**

Append to `internal/api/warehouse_handlers_test.go`:

```go
func TestWarehouseHiddenPatternsDefault(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)

	wh := createWarehouseViaAPI(t, s, admin, "Hidden Patterns Default WH")

	rec := warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses/"+wh.String(), admin, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.HiddenTablePatterns)

	// The list endpoint carries the field too.
	rec = warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses", admin, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list []warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.NotEmpty(t, list)
	for _, w := range list {
		if w.ID == wh.String() {
			require.Empty(t, w.HiddenTablePatterns)
		}
	}
}
```

**Step 2: Run it to verify it fails**

Run: `go test ./internal/api/ -run TestWarehouseHiddenPatternsDefault -v -timeout 3m`
Expected: FAIL — `got.HiddenTablePatterns` is undefined (compile error).

**Step 3: Implement**

Create `internal/database/migrations/V120__warehouse_hidden_table_patterns.sql`:

```sql
-- Warehouse-level regex patterns that hide matching tables from the grant
-- picker, the schema browser, and the new-tables inbox. Curation only: no
-- ClickHouse DDL or warehouse_table_grants rows are affected.
ALTER TABLE warehouses ADD COLUMN hidden_table_patterns TEXT[] NOT NULL DEFAULT '{}';
```

In `internal/api/warehouse_handlers.go`, add the field to `warehouseJSON`:

```go
type warehouseJSON struct {
	ID                        string                   `json:"id"`
	OrgID                     string                   `json:"org_id"`
	Name                      string                   `json:"name"`
	ProvisionerConnectorID    *string                  `json:"provisioner_connector_id"`
	AllowProvisionerExecution bool                     `json:"allow_provisioner_execution"`
	HiddenTablePatterns       []string                 `json:"hidden_table_patterns"`
	SyncStatus                string                   `json:"sync_status"`
	SyncError                 *string                  `json:"sync_error"`
	LastSyncedAt              *time.Time               `json:"last_synced_at"`
	CreatedAt                 time.Time                `json:"created_at"`
	UpdatedAt                 time.Time                `json:"updated_at"`
	Connectors                []warehouseConnectorJSON `json:"connectors,omitempty"`
}
```

Update the select columns (order must match `scanWarehouseRow`):

```go
const warehouseSelectColumns = `id, org_id, name, provisioner_connector_id, allow_provisioner_execution, hidden_table_patterns, sync_status, sync_error, last_synced_at, created_at, updated_at`
```

Update `scanWarehouseRow`:

```go
func scanWarehouseRow(row pgx.Row) (warehouseJSON, error) {
	var wh warehouseJSON
	err := row.Scan(&wh.ID, &wh.OrgID, &wh.Name, &wh.ProvisionerConnectorID,
		&wh.AllowProvisionerExecution, &wh.HiddenTablePatterns, &wh.SyncStatus, &wh.SyncError,
		&wh.LastSyncedAt, &wh.CreatedAt, &wh.UpdatedAt)
	return wh, err
}
```

All queries use `warehouseSelectColumns`, so create/list/get/provisioner responses are covered automatically. `handleCreateWarehouse`'s `INSERT ... RETURNING` also uses it, and the column has a default.

**Step 4: Run it to verify it passes**

Run: `go test ./internal/api/ -run 'TestWarehouseHiddenPatternsDefault|TestWarehouseCRUD' -v -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/database/migrations/V120__warehouse_hidden_table_patterns.sql internal/api/warehouse_handlers.go internal/api/warehouse_handlers_test.go
git commit -m "feat: add hidden_table_patterns column to warehouses"
```

---

### Task 2: Validate and persist patterns in PUT /warehouses/{id}

**Files:**
- Modify: `internal/api/warehouse_handlers.go` (`updateWarehouseRequest` :413-422, `handleUpdateWarehouse` :437-601)
- Test: `internal/api/warehouse_handlers_test.go`

**Step 1: Write the failing tests**

Append to `internal/api/warehouse_handlers_test.go`:

```go
func TestWarehouseHiddenPatternsUpdate(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Hidden Patterns Update WH")
	path := "/api/v1/warehouses/" + wh.String()

	// Set patterns.
	rec := updateWarehouseViaAPI(t, s, admin, wh, map[string]any{
		"hidden_table_patterns": []string{`^analytics\._tmp`, `_scratch$`},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{`^analytics\._tmp`, `_scratch$`}, got.HiddenTablePatterns)

	// Absent field leaves them unchanged.
	rec = updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"name": "Hidden Patterns Update WH 2"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, []string{`^analytics\._tmp`, `_scratch$`}, got.HiddenTablePatterns)

	// An empty array clears them.
	rec = updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"hidden_table_patterns": []string{}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.HiddenTablePatterns)

	// Explicit null clears them too.
	rec = updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"hidden_table_patterns": nil})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.HiddenTablePatterns)
}

func TestWarehouseHiddenPatternsValidation(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Hidden Patterns Validation WH")

	cases := []struct {
		name    string
		body    map[string]any
		message string
	}{
		{"invalid regex", map[string]any{"hidden_table_patterns": []string{`(`}}, "invalid pattern"},
		{"empty pattern", map[string]any{"hidden_table_patterns": []string{""}}, "empty"},
		{"whitespace pattern", map[string]any{"hidden_table_patterns": []string{"   "}}, "empty"},
		{"non-string", map[string]any{"hidden_table_patterns": []any{1}}, "must be an array"},
		{
			"too long",
			map[string]any{"hidden_table_patterns": []string{strings.Repeat("a", maxWarehouseHiddenPatternLength+1)}},
			"characters or fewer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := updateWarehouseViaAPI(t, s, admin, wh, tc.body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tc.message)
		})
	}

	tooMany := make([]string, maxWarehouseHiddenPatterns+1)
	for i := range tooMany {
		tooMany[i] = "p" + strconv.Itoa(i)
	}
	rec := updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"hidden_table_patterns": tooMany})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), strconv.Itoa(maxWarehouseHiddenPatterns))

	// The warehouse is untouched after every rejected write.
	rec = warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses/"+wh.String(), admin, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.HiddenTablePatterns)
}
```

Add `strconv` and `strings` to the test file's imports if missing.

**Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestWarehouseHiddenPatternsUpdate|TestWarehouseHiddenPatternsValidation' -v -timeout 3m`
Expected: FAIL — patterns are ignored (`got.HiddenTablePatterns` empty after set; no 400s).

**Step 3: Implement**

In `internal/api/warehouse_handlers.go`, extend the request struct and constants:

```go
const (
	maxWarehouseHiddenPatterns      = 100
	maxWarehouseHiddenPatternLength = 200
)

// parseHiddenTablePatterns validates the raw hidden_table_patterns field. An
// absent field is not passed here; explicit null/[] clears the list. Empty
// patterns are rejected because an empty regex matches every table.
func parseHiddenTablePatterns(raw json.RawMessage) ([]string, error) {
	var patterns []string
	if err := json.Unmarshal(raw, &patterns); err != nil {
		return nil, errors.New("hidden_table_patterns must be an array of strings")
	}
	if patterns == nil {
		patterns = []string{}
	}
	if len(patterns) > maxWarehouseHiddenPatterns {
		return nil, fmt.Errorf("hidden_table_patterns must contain %d patterns or fewer", maxWarehouseHiddenPatterns)
	}
	for i, pattern := range patterns {
		if strings.TrimSpace(pattern) == "" {
			return nil, fmt.Errorf("hidden_table_patterns[%d] must not be empty", i)
		}
		if utf8.RuneCountInString(pattern) > maxWarehouseHiddenPatternLength {
			return nil, fmt.Errorf("hidden_table_patterns[%d] must be %d characters or fewer", i, maxWarehouseHiddenPatternLength)
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %v", pattern, err)
		}
	}
	return patterns, nil
}
```

Add the field to `updateWarehouseRequest`:

```go
type updateWarehouseRequest struct {
	Name                      json.RawMessage `json:"name"`
	ProvisionerConnectorID    json.RawMessage `json:"provisioner_connector_id"`
	AllowProvisionerExecution *bool           `json:"allow_provisioner_execution"`
	HiddenTablePatterns       json.RawMessage `json:"hidden_table_patterns"`
}
```

In `handleUpdateWarehouse`, update the empty-body guard:

```go
	if req.Name == nil && req.ProvisionerConnectorID == nil && req.AllowProvisionerExecution == nil && req.HiddenTablePatterns == nil {
		writeError(w, http.StatusBadRequest, "at least one field must be provided")
		return
	}
```

After the name parsing block (around line 470), add:

```go
	var hiddenPatterns []string
	patternsSet := req.HiddenTablePatterns != nil
	if patternsSet {
		parsed, err := parseHiddenTablePatterns(req.HiddenTablePatterns)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		hiddenPatterns = parsed
	}
```

Extend the `FOR UPDATE` select (line ~510) and change detection:

```go
	var oldName string
	var oldProvisioner *uuid.UUID
	var oldAllow bool
	var oldPatterns []string
	err = tx.QueryRow(ctx, `
		SELECT name, provisioner_connector_id, allow_provisioner_execution, hidden_table_patterns FROM warehouses
		WHERE id = $1 AND org_id = $2 FOR UPDATE`,
		warehouseUUID.String(), claims.OrgID).Scan(&oldName, &oldProvisioner, &oldAllow, &oldPatterns)
	// ... unchanged error handling ...

	changedName := name != nil && *name != oldName
	changedProvisioner := provisionerSet && !uuidPointersEqual(oldProvisioner, provisionerID)
	changedAllow := req.AllowProvisionerExecution != nil && *req.AllowProvisionerExecution != oldAllow
	changedPatterns := patternsSet && !slices.Equal(hiddenPatterns, oldPatterns)
```

Add the update right after the `changedAllow` block:

```go
	if changedPatterns {
		if _, err := tx.Exec(ctx,
			`UPDATE warehouses SET hidden_table_patterns = $1, updated_at = now() WHERE id = $2 AND org_id = $3`,
			hiddenPatterns, warehouseUUID.String(), claims.OrgID); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
```

Extend the audit condition and metadata:

```go
	if changedName || changedProvisioner || changedAllow || changedPatterns {
		// ...existing metadata...
		if changedPatterns {
			meta["hidden_table_patterns"] = hiddenPatterns
			meta["previous_hidden_table_patterns"] = oldPatterns
		}
		// ...audit.Log unchanged...
	}
```

Add `slices` to imports. Pattern edits do **not** call `enqueueWarehouseSync`.

**Step 4: Run to verify pass**

Run: `go test ./internal/api/ -run 'TestWarehouseHiddenPatterns' -v -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/warehouse_handlers.go internal/api/warehouse_handlers_test.go
git commit -m "feat: validate and persist warehouse hidden-table patterns"
```

---

### Task 3: Shared visibility helpers + effective-grant refactor

**Files:**
- Create: `internal/api/schema_visibility.go`
- Create: `internal/api/schema_visibility_test.go`
- Modify: `internal/api/warehouse_grant_handlers.go:586-725` (`handleWarehouseEffectiveAccess`)

**Step 1: Write the failing tests**

Create `internal/api/schema_visibility_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/executor"
)

func TestCompileAndMatchHiddenPatterns(t *testing.T) {
	patterns := compileHiddenPatterns([]string{`^analytics\._tmp`, `_scratch$`, `(`})
	require.Len(t, patterns, 2, "an invalid stored pattern is skipped")

	require.True(t, matchesHiddenPattern(patterns, "analytics", "_tmp_123"))
	require.True(t, matchesHiddenPattern(patterns, "raw", "my_scratch"))
	require.False(t, matchesHiddenPattern(patterns, "analytics", "events"))
	require.False(t, matchesHiddenPattern(patterns, "", "scratchy"))
}

func TestFilterVisibleSchemaTables(t *testing.T) {
	tables := []executor.TableInfo{
		{Schema: "analytics", Name: "events"},
		{Schema: "analytics", Name: "_tmp_scratch"},
		{Schema: "raw", Name: "clicks"},
	}
	patterns := compileHiddenPatterns([]string{`_tmp`})

	// Admin view: patterns drop matched ungranted tables.
	got := filterVisibleSchemaTables(tables, patterns, nil, map[tableKey]struct{}{})
	require.Equal(t, []executor.TableInfo{{Schema: "analytics", Name: "events"}, {Schema: "raw", Name: "clicks"}}, got)

	// Granted tables survive a pattern match.
	protected := map[tableKey]struct{}{{Database: "analytics", Table: "_tmp_scratch"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, nil, protected)
	require.Len(t, got, 3)

	// An effective-grant allowlist wins over everything else.
	allowed := map[tableKey]struct{}{{Database: "raw", Table: "clicks"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, allowed, allowed)
	require.Equal(t, []executor.TableInfo{{Schema: "raw", Name: "clicks"}}, got)
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestCompileAndMatchHiddenPatterns|TestFilterVisibleSchemaTables' -v -timeout 3m`
Expected: FAIL — undefined symbols.

**Step 3: Implement helpers**

Create `internal/api/schema_visibility.go`:

```go
package api

import (
	"context"
	"log/slog"
	"regexp"
	"sort"

	"github.com/google/uuid"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// tableKey identifies one table independent of the type carrying it.
type tableKey struct {
	Database string
	Table    string
}

// compileHiddenPatterns compiles stored patterns. Invalid patterns are
// impossible after write validation; a stored one that no longer compiles is
// logged and skipped because patterns are curation, not security.
func compileHiddenPatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			slog.Warn("skipping invalid hidden-table pattern", "pattern", pattern, "error", err)
			continue
		}
		compiled = append(compiled, re)
	}
	return compiled
}

// matchesHiddenPattern reports whether database.table matches any pattern,
// with the same unanchored "database.table" semantics as connector filters.
func matchesHiddenPattern(patterns []*regexp.Regexp, database, table string) bool {
	name := table
	if database != "" {
		name = database + "." + table
	}
	for _, re := range patterns {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// loadWarehouseHiddenPatterns loads and compiles one warehouse's patterns.
// Failures fail open (log and return none): patterns are curation.
func (s *Server) loadWarehouseHiddenPatterns(ctx context.Context, warehouseID uuid.UUID) []*regexp.Regexp {
	var patterns []string
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT hidden_table_patterns FROM warehouses WHERE id = $1`,
		warehouseID.String()).Scan(&patterns); err != nil {
		slog.Warn("failed to load warehouse hidden-table patterns",
			"warehouse_id", warehouseID, "error", err)
		return nil
	}
	return compileHiddenPatterns(patterns)
}

// loadWarehouseGrantKeys returns every granted (database, table) of a warehouse.
func (s *Server) loadWarehouseGrantKeys(ctx context.Context, warehouseID uuid.UUID, orgID string) (map[tableKey]struct{}, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT database_name, table_name FROM warehouse_table_grants
		WHERE warehouse_id = $1 AND org_id = $2`,
		warehouseID.String(), orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := map[tableKey]struct{}{}
	for rows.Next() {
		var key tableKey
		if err := rows.Scan(&key.Database, &key.Table); err != nil {
			return nil, err
		}
		keys[key] = struct{}{}
	}
	return keys, rows.Err()
}

// loadEffectiveWarehouseGrants resolves one user's union of everyone + direct
// + group grants, the same resolution execution relies on. It also returns the
// sorted ClickHouse role idents implied by group/everyone grants so the
// effective-access endpoint keeps reporting them. Group membership is joined
// through org groups so a cross-org membership row can never import another
// org's grant.
func (s *Server) loadEffectiveWarehouseGrants(ctx context.Context, warehouseID uuid.UUID, orgID, userID string) ([]tableKey, []string, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT wtg.subject_type, wtg.subject_id, wtg.database_name, wtg.table_name
		FROM warehouse_table_grants wtg
		WHERE wtg.warehouse_id = $1 AND wtg.org_id = $2
		  AND (
		    wtg.subject_type = 'everyone'
		    OR (wtg.subject_type = 'user' AND wtg.subject_id = $3)
		    OR (wtg.subject_type = 'group' AND EXISTS (
		          SELECT 1 FROM group_members gm
		          JOIN groups g ON g.id = gm.group_id AND g.org_id = $2
		          WHERE gm.user_id = $4 AND gm.group_id::text = wtg.subject_id))
		  )
		ORDER BY wtg.database_name ASC, wtg.table_name ASC`,
		warehouseID.String(), orgID, userID, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var keys []tableKey
	seen := map[tableKey]struct{}{}
	roleSet := map[string]struct{}{}
	orgUUID, orgErr := uuid.Parse(orgID)
	for rows.Next() {
		var subjectType, subjectID, database, table string
		if err := rows.Scan(&subjectType, &subjectID, &database, &table); err != nil {
			return nil, nil, err
		}
		key := tableKey{Database: database, Table: table}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
		if orgErr != nil {
			continue
		}
		switch subjectType {
		case "group":
			if groupUUID, err := uuid.Parse(subjectID); err == nil {
				roleSet[chaccess.RoleIdent(warehouseID, orgUUID, groupUUID)] = struct{}{}
			}
		case "everyone":
			roleSet[chaccess.EveryoneRole(warehouseID)] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	roles := make([]string, 0, len(roleSet))
	for role := range roleSet {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return keys, roles, nil
}

// keysToSet converts a sorted key list into a lookup set.
func keysToSet(keys []tableKey) map[tableKey]struct{} {
	set := make(map[tableKey]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

// filterVisibleSchemaTables applies warehouse visibility. A nil allowed set
// means "no per-user restriction"; a non-nil one keeps only its keys. Patterns
// drop matched tables unless the key is in patternProtected.
func filterVisibleSchemaTables(
	tables []executor.TableInfo,
	patterns []*regexp.Regexp,
	allowed, patternProtected map[tableKey]struct{},
) []executor.TableInfo {
	filtered := make([]executor.TableInfo, 0, len(tables))
	for _, table := range tables {
		key := tableKey{Database: table.Schema, Table: table.Name}
		if allowed != nil {
			if _, ok := allowed[key]; !ok {
				continue
			}
		}
		if matchesHiddenPattern(patterns, table.Schema, table.Name) {
			if patternProtected == nil {
				continue
			}
			if _, ok := patternProtected[key]; !ok {
				continue
			}
		}
		filtered = append(filtered, table)
	}
	return filtered
}

// filterHiddenCatalogTables drops pattern-matched tables from a raw catalog
// snapshot (reconcile path), where there is no granted-table protection: the
// inbox already excludes granted tables.
func filterHiddenCatalogTables(patterns []*regexp.Regexp, tables []chaccess.CatalogTable) []chaccess.CatalogTable {
	if len(patterns) == 0 {
		return tables
	}
	filtered := make([]chaccess.CatalogTable, 0, len(tables))
	for _, table := range tables {
		if matchesHiddenPattern(patterns, table.Database, table.Table) {
			continue
		}
		filtered = append(filtered, table)
	}
	return filtered
}
```

**Step 4: Refactor `handleWarehouseEffectiveAccess` to use the helper**

Replace the block at `internal/api/warehouse_grant_handlers.go:634-683` (the union query + dedupe + roleSet loop) with:

```go
	keyList, roles, err := s.loadEffectiveWarehouseGrants(ctx, warehouseUUID, claims.OrgID, targetUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	tables := make([]warehouseEffectiveTableJSON, 0, len(keyList))
	for _, key := range keyList {
		tables = append(tables, warehouseEffectiveTableJSON{Database: key.Database, Table: key.Table})
	}
```

Then delete the old `roleSet` → `roles` conversion block at :704-708 (the helper returns sorted roles). Keep the `resp` construction and the `len(tables) > 0` CHUser/Roles logic unchanged. Remove now-unused imports (`sort`, possibly others) from the file if the compiler complains.

**Step 5: Run the tests**

Run: `go test ./internal/api/ -run 'TestCompileAndMatchHiddenPatterns|TestFilterVisibleSchemaTables|TestGrantCRUDAndEffectiveAccess|TestWarehouseGrantEffectiveAccessZeroGrantShape|TestWarehouseGrantEffectiveAccessIgnoresStalePreference' -v -timeout 3m`
Expected: PASS — helper tests pass and the effective-access behavior is unchanged.

**Step 6: Commit**

```bash
git add internal/api/schema_visibility.go internal/api/schema_visibility_test.go internal/api/warehouse_grant_handlers.go
git commit -m "feat: add warehouse schema visibility helpers"
```

---

### Task 4: Apply hidden patterns in the schema endpoint (admin) + snapshots

**Files:**
- Modify: `internal/api/connector_handlers.go:599-609` (`loadConnectorWithFilters`), `:826` (call site), `:855-911` (filters + snapshot)
- Test: `internal/api/schema_visibility_test.go` (integration test using the ClickHouse fixture)

**Step 1: Write the failing test**

Append to `internal/api/schema_visibility_test.go`. This uses `setupWarehouseFixture` (real ClickHouse; skips when unavailable), which already seeds an admin, a provisioner connector, a warehouse with a direct `analytics.events` grant and a group `analytics.daily_revenue` grant.

```go
func TestConnectorSchemaHiddenPatterns(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()

	database := "aether_vis_" + uuid.NewString()[:8]
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quotedDB))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = fx.conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB)
	})
	for _, table := range []string{"events", "_tmp_scratch", "daily_revenue"} {
		quoted, err := chaccess.QuoteObjectIdent(table)
		require.NoError(t, err)
		require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quoted+
			" (id UInt64) ENGINE = MergeTree ORDER BY id"))
	}

	// Hidden pattern for the tmp table; the fixture grants cover events and
	// daily_revenue in the analytics database, so add a grant matching the
	// pattern to prove granted tables survive.
	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`_tmp`}, fx.warehouseID.String())
	require.NoError(t, err)

	token, err := fx.s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)
	rec := warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+fx.connectorID.String()+"/schema", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var schema executor.SchemaInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schema))
	got := map[string]bool{}
	for _, table := range schema.Tables {
		got[table.Schema+"."+table.Name] = true
	}
	require.True(t, got[database+".events"], "unmatched tables survive")
	require.True(t, got[database+".daily_revenue"])
	require.False(t, got[database+"._tmp_scratch"], "pattern-matched ungranted tables are hidden")

	// The schema-read snapshot is pattern-filtered too.
	var hiddenRows int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2 AND table_name = '_tmp_scratch'`,
		fx.connectorID.String(), database).Scan(&hiddenRows))
	require.Zero(t, hiddenRows)

	// A granted table that also matches stays visible.
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'everyone', 'everyone', $3, '_tmp_scratch')`,
		fx.orgID.String(), fx.warehouseID.String(), database)
	require.NoError(t, err)

	rec = warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+fx.connectorID.String()+"/schema", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schema))
	got = map[string]bool{}
	for _, table := range schema.Tables {
		got[table.Schema+"."+table.Name] = true
	}
	require.True(t, got[database+"._tmp_scratch"], "granted tables stay visible even when matched")
}
```

Add `encoding/json`, `time`, `github.com/google/uuid`, `github.com/the-heaven-labs/aether/internal/chaccess`, `github.com/the-heaven-labs/aether/internal/executor` to the test imports.

**Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run TestConnectorSchemaHiddenPatterns -v -timeout 3m`
Expected: FAIL — `_tmp_scratch` is present (patterns not applied). The test skips if ClickHouse is unavailable; make sure `task infra:up` ran.

**Step 3: Implement**

Update `loadConnectorWithFilters` to also return `warehouse_id`:

```go
// loadConnectorWithFilters fetches the connector type, encrypted config,
// table filters, and warehouse link.
func (s *Server) loadConnectorWithFilters(ctx context.Context, connID, orgID string) (models.ConnectorType, []byte, []string, []string, *uuid.UUID, error) {
	var configEnc []byte
	var connType models.ConnectorType
	var allowlist, denylist []string
	var warehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx,
		`SELECT type, config_encrypted, table_allowlist, table_denylist, warehouse_id FROM connectors WHERE id = $1 AND org_id = $2`,
		connID, orgID,
	).Scan(&connType, &configEnc, &allowlist, &denylist, &warehouseID)
	return connType, configEnc, allowlist, denylist, warehouseID, err
}
```

Update the single call site (`connector_handlers.go:826`):

```go
	connType, configEnc, allowlist, denylist, warehouseID, err := s.loadConnectorWithFilters(ctx, connID, claims.OrgID)
```

After the connector allow/deny block (ends at line ~893), insert the pattern filter and move the snapshot write to use it:

```go
	// Hidden-table patterns hide ungranted tables from every viewer. Existing
	// grants stay visible (protected) so revoke workflows keep working, and the
	// per-user effective-grant filter is applied after the snapshot below.
	var patterns []*regexp.Regexp
	var patternProtected map[tableKey]struct{}
	var effective map[tableKey]struct{}
	if warehouseID != nil {
		patterns = s.loadWarehouseHiddenPatterns(ctx, *warehouseID)
		if claims.Role == "admin" && len(patterns) > 0 {
			grants, gErr := s.loadWarehouseGrantKeys(ctx, *warehouseID, claims.OrgID)
			if gErr != nil {
				slog.Warn("failed to load warehouse grants for pattern protection; serving unfiltered tables",
					"warehouse_id", *warehouseID, "error", gErr)
				patterns = nil
			} else {
				patternProtected = grants
			}
		}
		if s.warehouseManagementEnabled() && claims.Role != "admin" {
			keys, _, gErr := s.loadEffectiveWarehouseGrants(ctx, *warehouseID, claims.OrgID, claims.UserID)
			if gErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to resolve table access")
				return
			}
			effective = keysToSet(keys)
			patternProtected = effective
		}
	}
	if len(patterns) > 0 {
		schema.Tables = filterVisibleSchemaTables(schema.Tables, patterns, nil, patternProtected)
	}
```

The existing snapshot block (line ~895-911) now receives the pattern-filtered `schema.Tables` — leave it where it is. Immediately after it, apply the per-user filter:

```go
	// The per-user filter runs after the snapshot so schema_snapshots stays a
	// warehouse-wide catalog, never one viewer's subset.
	if effective != nil {
		schema.Tables = filterVisibleSchemaTables(schema.Tables, nil, effective, nil)
	}
```

Ensure `regexp` is imported in `connector_handlers.go` (already used by the allow/deny filter).

**Step 4: Run to verify pass**

Run: `go test ./internal/api/ -run 'TestConnectorSchemaHiddenPatterns|TestConnectorSchemaRecordsSnapshots' -v -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/connector_handlers.go internal/api/schema_visibility_test.go
git commit -m "feat: apply warehouse hidden patterns to connector schema and snapshots"
```

---

### Task 5: Per-user effective-grant filtering in the schema endpoint

**Files:**
- Modify: `internal/api/connector_handlers.go` (already wired in Task 4; this task verifies and hardens it)
- Test: `internal/api/schema_visibility_test.go`

**Step 1: Write the failing tests**

Append to `internal/api/schema_visibility_test.go`:

```go
// visibilityFixture builds a warehouse with a non-provisioner ClickHouse
// service linked, the given grants, and returns the service connector ID.
func visibilityFixture(t *testing.T, fx *warehouseSyncFixture, database string) (serviceID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	serviceID = insertClickHouseService(t, fx.s, fx.orgID, fx.connectorID, "Visibility Service", &fx.warehouseID)
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quotedDB))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = fx.conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB)
	})
	for _, table := range []string{"events", "daily_revenue", "clicks", "secret"} {
		quoted, err := chaccess.QuoteObjectIdent(table)
		require.NoError(t, err)
		require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quoted+
			" (id UInt64) ENGINE = MergeTree ORDER BY id"))
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = fx.s.db.Pool.Exec(cleanupCtx, `DELETE FROM connectors WHERE id = $1`, serviceID.String())
	})
	return serviceID
}

func TestConnectorSchemaPerUserGrantFilter(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()
	database := "aether_vis_" + uuid.NewString()[:8]
	serviceID := visibilityFixture(t, fx, database)

	memberID, memberToken := seedGrantOrgMember(t, fx.s, fx.orgID, "editor")
	grantConnectorUse(t, fx.s, fx.orgID, memberID, serviceID)
	_, err := fx.s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		fx.groupID.String(), memberID.String())
	require.NoError(t, err)

	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES
			($1, $2, 'user', $3, $4, 'events'),
			($1, $2, 'everyone', 'everyone', $4, 'clicks')`,
		fx.orgID.String(), fx.warehouseID.String(), memberID.String(), database)
	require.NoError(t, err)
	// The fixture's group grant (analytics.daily_revenue) is in another
	// database; add one in the visibility database to prove group inheritance.
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'group', $3, $4, 'daily_revenue')`,
		fx.orgID.String(), fx.warehouseID.String(), fx.groupID.String(), database)
	require.NoError(t, err)

	schemaFor := func(t *testing.T, token string) map[string]bool {
		t.Helper()
		rec := warehouseAPIRequest(t, fx.s, http.MethodGet,
			"/api/v1/connectors/"+serviceID.String()+"/schema", token, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var schema executor.SchemaInfo
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schema))
		got := map[string]bool{}
		for _, table := range schema.Tables {
			got[table.Schema+"."+table.Name] = true
		}
		return got
	}

	// Non-admin sees direct + group + everyone grants only.
	got := schemaFor(t, memberToken)
	require.True(t, got[database+".events"], "direct grant")
	require.True(t, got[database+".daily_revenue"], "group grant")
	require.True(t, got[database+".clicks"], "everyone grant")
	require.False(t, got[database+".secret"], "ungranted tables are hidden")

	// A member with service access but zero grants sees nothing.
	noGrantID, noGrantToken := seedGrantOrgMember(t, fx.s, fx.orgID, "editor")
	grantConnectorUse(t, fx.s, fx.orgID, noGrantID, serviceID)
	got = schemaFor(t, noGrantToken)
	require.False(t, got[database+".events"])
	require.False(t, got[database+".secret"])

	// Admin bypasses the per-user filter.
	adminToken, err := fx.s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)
	got = schemaFor(t, adminToken)
	require.True(t, got[database+".events"])
	require.True(t, got[database+".secret"])

	// With the kill switch off, execution uses the stored credential, so the
	// per-user filter must not understate access. (Dedicated server: safe.)
	fx.s.SetCHTablePermissions(false)
	got = schemaFor(t, memberToken)
	require.True(t, got[database+".secret"], "kill switch off disables per-user filtering")
}
```

Note: `seedGrantOrgMember` returns `(uuid.UUID, string)`. Check its exact signature at `internal/api/warehouse_grant_handlers_test.go:22` and use it accordingly.

**Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run TestConnectorSchemaPerUserGrantFilter -v -timeout 3m`
Expected: FAIL if Task 4's wiring was incomplete; if Task 4 included the effective filter, this may pass immediately — that is acceptable, but run it and record the result before moving on.

**Step 3: Implement (only if the test fails)**

The implementation is already in Task 4 (the `effective` branch). Fix any gaps it exposes: the gate must be exactly `s.warehouseManagementEnabled() && claims.Role != "admin"` and must run **after** the snapshot write.

**Step 4: Run all visibility tests**

Run: `go test ./internal/api/ -run 'TestConnectorSchema|TestFilterVisible|TestCompileAndMatch' -v -timeout 3m`
Expected: PASS (ClickHouse-dependent tests may skip if infra is down).

**Step 5: Commit**

```bash
git add internal/api/schema_visibility_test.go internal/api/connector_handlers.go
git commit -m "test: cover per-user schema grant filtering"
```

---

### Task 6: Pattern filtering in reconcile snapshots and the new-tables inbox

**Files:**
- Modify: `internal/api/warehouse_sync.go:129-138`
- Modify: `internal/api/warehouse_handlers.go:1252-1342` (`handleWarehouseNewTables`)
- Test: `internal/api/warehouse_new_tables_test.go`

**Step 1: Write the failing tests**

Append to `internal/api/warehouse_new_tables_test.go`:

```go
func TestWarehouseNewTablesHiddenPatterns(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Hidden Patterns Inbox WH")
	conn := seedWarehouseConnector(t, s, orgID, "Hidden Patterns Inbox Service")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)

	require.Equal(t, http.StatusOK, updateWarehouseViaAPI(t, s, admin, wh, map[string]any{
		"hidden_table_patterns": []string{`^analytics\._tmp`},
	}).Code)

	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(context.Background(),
		`UPDATE warehouses SET created_at = $1 WHERE id = $2`, now.Add(-4*time.Hour), wh.String())
	require.NoError(t, err)
	insertSchemaSnapshot(t, s, conn, "analytics", "events", now.Add(-3*time.Hour))
	insertSchemaSnapshot(t, s, conn, "analytics", "_tmp_scratch", now.Add(-3*time.Hour))

	resp := listNewTablesViaAPI(t, s, admin, wh, "")
	require.Len(t, resp.Tables, 1, "pre-existing snapshot rows matching patterns are filtered")
	require.Equal(t, "events", resp.Tables[0].Table)
}
```

Append a reconcile-path test to `internal/api/warehouse_sync_test.go` (mirrors `TestReconcileWarehouseProvisionsUsersAndRoles`):

```go
func TestReconcileWarehouseHiddenPatternsSnapshot(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()

	database := "aether_snap_" + uuid.NewString()[:8]
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quotedDB))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = fx.conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB)
	})
	for _, table := range []string{"events", "_tmp_scratch"} {
		quoted, err := chaccess.QuoteObjectIdent(table)
		require.NoError(t, err)
		require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quoted+
			" (id UInt64) ENGINE = MergeTree ORDER BY id"))
	}

	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`_tmp`}, fx.warehouseID.String())
	require.NoError(t, err)

	require.NoError(t, fx.s.reconcileWarehouse(ctx, fx.warehouseID))

	var hidden, visible int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2 AND table_name = '_tmp_scratch'`,
		fx.connectorID.String(), database).Scan(&hidden))
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2 AND table_name = 'events'`,
		fx.connectorID.String(), database).Scan(&visible))
	require.Zero(t, hidden, "pattern-matched tables never enter snapshots")
	require.Equal(t, 1, visible)
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestWarehouseNewTablesHiddenPatterns|TestReconcileWarehouseHiddenPatternsSnapshot' -v -timeout 3m`
Expected: FAIL — hidden rows/snapshots are present.

**Step 3: Implement**

In `internal/api/warehouse_sync.go`, change the catalog snapshot block:

```go
	if tables, catErr := chaccess.LoadCatalogTables(ctx, conn); catErr != nil {
		slog.Warn("warehouse catalog snapshot failed",
			"warehouse_id", warehouseID, "error", catErr)
	} else {
		patterns := s.loadWarehouseHiddenPatterns(ctx, warehouseID)
		if snapErr := s.recordSchemaSnapshot(ctx, *hdr.provisionerID, filterHiddenCatalogTables(patterns, tables)); snapErr != nil {
			slog.Warn("warehouse catalog snapshot write failed",
				"warehouse_id", warehouseID, "error", snapErr)
		}
	}
```

In `internal/api/warehouse_handlers.go`, inside `handleWarehouseNewTables`, compile patterns from the already-loaded warehouse row and skip matches in the row loop:

```go
	patterns := compileHiddenPatterns(wh.HiddenTablePatterns)
	// ...
	for rows.Next() {
		if len(tables) == maxWarehouseNewTables {
			// One row past the cap is enough to know the list is incomplete.
			truncated = true
			break
		}
		var t warehouseNewTableJSON
		if err := rows.Scan(&t.Database, &t.Table, &t.FirstSeenAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if matchesHiddenPattern(patterns, t.Database, t.Table) {
			continue
		}
		tables = append(tables, t)
	}
```

**Step 4: Run to verify pass**

Run: `go test ./internal/api/ -run 'TestWarehouseNewTables|TestReconcileWarehouse' -v -timeout 3m`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/warehouse_sync.go internal/api/warehouse_handlers.go internal/api/warehouse_new_tables_test.go internal/api/warehouse_sync_test.go
git commit -m "feat: apply hidden patterns to reconcile snapshots and the new-tables inbox"
```

---

### Task 7: Frontend API types

**Files:**
- Modify: `web/src/api/warehouses.ts:14-27, 134-143`

**Step 1: Implement**

Add to the `Warehouse` interface:

```ts
  hidden_table_patterns: string[]
```

Extend `updateWarehouse`'s data type:

```ts
export function updateWarehouse(
  id: string,
  data: {
    name?: string
    provisioner_connector_id?: string | null
    allow_provisioner_execution?: boolean
    hidden_table_patterns?: string[] | null
  },
): Promise<Warehouse> {
  return api.put<Warehouse>(`/api/v1/warehouses/${id}`, data)
}
```

**Step 2: Typecheck**

Run: `cd web && npx tsc --noEmit`
Expected: PASS (there may be other pre-existing errors — only new ones matter; if the warehouse fixtures in tests fail, add `hidden_table_patterns: []` to them).

**Step 3: Commit**

```bash
git add web/src/api/warehouses.ts
git commit -m "feat(web): add hidden_table_patterns to warehouse API types"
```

---

### Task 8: `WarehouseHiddenTables` component

**Files:**
- Create: `web/src/components/WarehouseHiddenTables.tsx`
- Create: `web/src/components/WarehouseHiddenTables.test.tsx`

**Step 1: Write the failing test**

Create `web/src/components/WarehouseHiddenTables.test.tsx`:

```tsx
import { describe, test, expect, beforeEach } from 'vitest'
import { screen, fireEvent, waitFor } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { server } from '../test/server'
import { renderWithProviders } from '../test/utils'
import { WarehouseHiddenTables } from './WarehouseHiddenTables'
import type { Warehouse, WarehouseConnector, WarehouseGrant } from '../api/warehouses'

const CONNECTORS: WarehouseConnector[] = [
  { id: 'c-1', name: 'CH RW', type: 'clickhouse', is_provisioner: true },
]

const WAREHOUSE: Warehouse = {
  id: 'wh-1', org_id: 'org-1', name: 'WH', provisioner_connector_id: 'c-1',
  allow_provisioner_execution: false, hidden_table_patterns: ['_tmp'],
  sync_status: 'ready', sync_error: null, last_synced_at: null,
  created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
}

const GRANTS: WarehouseGrant[] = [
  {
    id: 'gr-1', org_id: 'org-1', warehouse_id: 'wh-1', subject_type: 'everyone', subject_id: 'everyone',
    database: 'analytics', table: '_tmp_kept', created_by: null, created_at: '2026-01-01T00:00:00Z',
  },
]

const SCHEMA = {
  tables: [
    { schema: 'analytics', name: 'events', columns: [] },
    { schema: 'analytics', name: '_tmp_scratch', columns: [] },
    { schema: 'analytics', name: '_tmp_kept', columns: [] },
  ],
}

beforeEach(() => {
  server.use(
    http.get('/api/v1/connectors/c-1/schema', () => HttpResponse.json(SCHEMA)),
    http.get('/api/v1/warehouses/wh-1/grants', () => HttpResponse.json(GRANTS)),
  )
})

describe('WarehouseHiddenTables', () => {
  test('renders patterns and counts matches excluding already-granted tables', async () => {
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    expect(await screen.findByText('_tmp')).toBeInTheDocument()
    // Two tables match `_tmp`, but one is already granted and stays visible.
    expect(await screen.findByText(/hides 1 table/)).toBeInTheDocument()
  })

  test('adds a pattern through PUT /warehouses/{id}', async () => {
    let putBody: Record<string, unknown> | null = null
    server.use(
      http.put('/api/v1/warehouses/wh-1', async ({ request }) => {
        putBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ ...WAREHOUSE, hidden_table_patterns: ['_tmp', '^raw\\.old'] })
      }),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={WAREHOUSE.hidden_table_patterns} connectors={CONNECTORS} />,
    )
    await screen.findByText('_tmp')
    fireEvent.change(screen.getByLabelText('Pattern'), { target: { value: '^raw\\.old' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(putBody).toEqual({ hidden_table_patterns: ['_tmp', '^raw\\.old'] }))
  })

  test('shows the server validation error for an invalid pattern', async () => {
    server.use(
      http.put('/api/v1/warehouses/wh-1', () =>
        HttpResponse.json({ error: 'invalid pattern "(": error parsing regexp' }, { status: 400 }),
      ),
    )
    renderWithProviders(
      <WarehouseHiddenTables warehouseId="wh-1" patterns={[]} connectors={CONNECTORS} />,
    )
    fireEvent.change(await screen.findByLabelText('Pattern'), { target: { value: '(' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findByText(/invalid pattern/)).toBeInTheDocument()
  })
})
```

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/WarehouseHiddenTables.test.tsx`
Expected: FAIL — module not found.

**Step 3: Implement the component**

Create `web/src/components/WarehouseHiddenTables.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { connectorSchemaQueryKey, getConnectorSchema } from '../api/schema'
import { listGrants, updateWarehouse, type WarehouseConnector } from '../api/warehouses'
import { ErrorBanner } from './ErrorBanner'

interface Props {
  warehouseId: string
  patterns: string[]
  connectors?: WarehouseConnector[]
}

export function WarehouseHiddenTables({ warehouseId, patterns, connectors = [] }: Props) {
  const qc = useQueryClient()
  const [input, setInput] = useState('')
  const [error, setError] = useState<string | null>(null)

  const sourceConnectorId = useMemo(() => {
    const provisioner = connectors.find((c) => c.is_provisioner)
    return provisioner?.id ?? connectors[0]?.id ?? ''
  }, [connectors])

  const { data: schema } = useQuery({
    queryKey: connectorSchemaQueryKey(sourceConnectorId),
    queryFn: () => getConnectorSchema(sourceConnectorId),
    enabled: !!sourceConnectorId,
  })

  const { data: grants = [] } = useQuery({
    queryKey: ['warehouse-grants', warehouseId],
    queryFn: () => listGrants(warehouseId),
  })

  const hiddenCount = useMemo(() => {
    const compiled: RegExp[] = []
    for (const pattern of patterns) {
      try {
        compiled.push(new RegExp(pattern))
      } catch {
        // Invalid patterns are rejected by the server; ignore stale data.
      }
    }
    if (compiled.length === 0) return 0
    const granted = new Set(grants.map((g) => `${g.database}.${g.table}`))
    let count = 0
    for (const table of schema?.tables ?? []) {
      const name = table.schema ? `${table.schema}.${table.name}` : table.name
      if (granted.has(name)) continue
      if (compiled.some((re) => re.test(name))) count++
    }
    return count
  }, [patterns, grants, schema])

  const save = useMutation({
    mutationFn: (next: string[]) => updateWarehouse(warehouseId, { hidden_table_patterns: next }),
    onSuccess: () => {
      setError(null)
      setInput('')
      qc.invalidateQueries({ queryKey: ['warehouse', warehouseId] })
      qc.invalidateQueries({ queryKey: ['warehouses'] })
      qc.invalidateQueries({ queryKey: ['warehouse-new-tables', warehouseId] })
      for (const connector of connectors) {
        qc.invalidateQueries({ queryKey: connectorSchemaQueryKey(connector.id) })
      }
    },
    onError: (err: Error) => setError(err.message),
  })

  const addPattern = () => {
    const trimmed = input.trim()
    if (!trimmed) return
    if (patterns.includes(trimmed)) {
      setInput('')
      return
    }
    save.mutate([...patterns, trimmed])
  }

  const removePattern = (pattern: string) => {
    save.mutate(patterns.filter((p) => p !== pattern))
  }

  return (
    <section style={styles.section} aria-label="Hidden tables">
      <div style={styles.header}>
        <h3 style={styles.title}>Hidden tables</h3>
        <span style={styles.hint}>
          Go regex matched against <code>database.table</code>. Matching tables are hidden from the
          picker and inbox unless already granted.
        </span>
      </div>

      {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}

      {patterns.length > 0 ? (
        <div style={styles.chips}>
          {patterns.map((pattern) => (
            <span key={pattern} style={styles.chip}>
              <code style={styles.chipLabel}>{pattern}</code>
              <button
                type="button"
                style={styles.chipRemove}
                title="Remove pattern"
                aria-label={`Remove pattern ${pattern}`}
                disabled={save.isPending}
                onClick={() => removePattern(pattern)}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      ) : (
        <div style={styles.empty}>No hidden patterns.</div>
      )}

      <div style={styles.addRow}>
        <input
          aria-label="Pattern"
          style={styles.input}
          placeholder="e.g. ^analytics\._tmp"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') addPattern()
          }}
        />
        <button
          type="button"
          style={{ ...styles.addBtn, opacity: input.trim() && !save.isPending ? 1 : 0.5 }}
          disabled={!input.trim() || save.isPending}
          onClick={addPattern}
        >
          {save.isPending ? 'Saving…' : 'Add'}
        </button>
      </div>

      {patterns.length > 0 && schema && (
        <p style={styles.hint}>
          {hiddenCount === 1 ? 'hides 1 table' : `hides ${hiddenCount} tables`} in the current schema
          source.
        </p>
      )}
    </section>
  )
}

const styles: Record<string, React.CSSProperties> = {
  section: {
    borderTop: '1px solid var(--border)',
    paddingTop: 16,
    display: 'flex',
    flexDirection: 'column',
    gap: 10,
  },
  header: { display: 'flex', alignItems: 'baseline', gap: 10, flexWrap: 'wrap' },
  title: { margin: 0, fontSize: 14, fontWeight: 700, color: 'var(--text-primary)' },
  hint: { fontSize: 12, color: 'var(--text-muted)', margin: 0 },
  empty: { fontSize: 13, color: 'var(--text-secondary)', fontStyle: 'italic', padding: '4px 0' },
  chips: { display: 'flex', flexWrap: 'wrap', gap: 6 },
  chip: {
    display: 'inline-flex', alignItems: 'center', gap: 4, background: 'var(--bg-secondary)',
    border: '1px solid var(--border)', borderRadius: 10, padding: '1px 4px 1px 8px',
  },
  chipLabel: {
    fontSize: 12, fontFamily: 'var(--font-mono)', color: 'var(--text-primary)',
    overflowWrap: 'anywhere' as const,
  },
  chipRemove: {
    background: 'none', border: 'none', cursor: 'pointer', color: 'var(--text-muted)',
    fontSize: 14, lineHeight: 1, padding: '0 3px',
  },
  addRow: { display: 'flex', gap: 8, alignItems: 'center', maxWidth: 460 },
  input: {
    flex: 1, padding: '6px 10px', border: '1px solid var(--border)', borderRadius: 4,
    fontSize: 13, background: 'var(--bg-input)', color: 'var(--text-primary)', minWidth: 0,
  },
  addBtn: {
    padding: '7px 16px', background: 'var(--accent)', color: '#fff', border: 'none',
    borderRadius: 4, fontSize: 13, fontWeight: 600,
  },
}
```

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/components/WarehouseHiddenTables.test.tsx`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/components/WarehouseHiddenTables.tsx web/src/components/WarehouseHiddenTables.test.tsx
git commit -m "feat(web): add warehouse hidden-table pattern editor"
```

---

### Task 9: Wire into Warehouse settings + picker hint

**Files:**
- Modify: `web/src/pages/WarehouseSettingsPage.tsx:498-611` (expanded card body)
- Modify: `web/src/components/WarehouseTableGrants.tsx` (props + hint), test file `web/src/components/WarehouseTableGrants.test.tsx`

**Step 1: Write the failing test**

Append to `web/src/components/WarehouseTableGrants.test.tsx`:

```tsx
  test('hints how many tables match hidden patterns', async () => {
    renderWithProviders(
      <WarehouseTableGrants warehouseId="wh-1" connectors={CONNECTORS} hiddenPatterns={['_tmp']} />,
    )
    await screen.findAllByText('Data Team')
    // The SCHEMA mock has no _tmp tables, add one via a schema override.
    expect(screen.queryByText(/match hidden patterns/)).toBeNull()
  })

  test('shows the match hint when patterns match schema tables', async () => {
    server.use(
      http.get('/api/v1/connectors/c-1/schema', () =>
        HttpResponse.json({
          tables: [
            ...SCHEMA.tables,
            { schema: 'analytics', name: '_tmp_scratch', columns: [] },
          ],
        }),
      ),
    )
    renderWithProviders(
      <WarehouseTableGrants warehouseId="wh-1" connectors={CONNECTORS} hiddenPatterns={['_tmp']} />,
    )
    await screen.findAllByText('Data Team')
    expect(await screen.findByText(/1 table matches hidden patterns/)).toBeInTheDocument()
  })
```

**Step 2: Run to verify failure**

Run: `cd web && npx vitest run src/components/WarehouseTableGrants.test.tsx`
Expected: FAIL — `hiddenPatterns` prop not accepted / hint missing.

**Step 3: Implement**

In `WarehouseTableGrants.tsx`:

```tsx
interface Props {
  warehouseId: string
  connectors?: WarehouseConnector[]
  hiddenPatterns?: string[]
}
```

Destructure `hiddenPatterns = []`. Compute:

```tsx
  const hiddenMatchCount = useMemo(() => {
    const compiled: RegExp[] = []
    for (const pattern of hiddenPatterns) {
      try {
        compiled.push(new RegExp(pattern))
      } catch {
        // Server-validated; ignore stale data.
      }
    }
    if (compiled.length === 0) return 0
    let count = 0
    for (const table of schema?.tables ?? []) {
      const name = table.schema ? `${table.schema}.${table.name}` : table.name
      if (compiled.some((re) => re.test(name))) count++
    }
    return count
  }, [hiddenPatterns, schema])
```

Render inside the header, after the `hint` span:

```tsx
        {hiddenMatchCount > 0 && (
          <span style={styles.hint}>
            {hiddenMatchCount === 1
              ? '1 table matches hidden patterns'
              : `${hiddenMatchCount} tables match hidden patterns`}
          </span>
        )}
```

In `WarehouseSettingsPage.tsx`, import the new component and render it in the expanded card body immediately before `<WarehouseTableGrants ... />` (line ~609):

```tsx
          <WarehouseHiddenTables
            warehouseId={warehouse.id}
            patterns={detail?.hidden_table_patterns ?? warehouse.hidden_table_patterns ?? []}
            connectors={linked}
          />
          <WarehouseTableGrants
            warehouseId={warehouse.id}
            connectors={linked}
            hiddenPatterns={detail?.hidden_table_patterns ?? warehouse.hidden_table_patterns ?? []}
          />
```

Also add `hidden_table_patterns: []` to any `Warehouse` object literals in frontend tests/mocks that now fail typecheck (search: `rg -n "allow_provisioner_execution" web/src --glob '*.test.tsx' --glob '*.ts'`).

**Step 4: Run to verify pass**

Run: `cd web && npx vitest run src/components/WarehouseTableGrants.test.tsx src/components/WarehouseHiddenTables.test.tsx && npx tsc --noEmit`
Expected: PASS.

**Step 5: Commit**

```bash
git add web/src/pages/WarehouseSettingsPage.tsx web/src/components/WarehouseTableGrants.tsx web/src/components/WarehouseTableGrants.test.tsx
git commit -m "feat(web): surface hidden-table patterns in warehouse settings"
```

---

### Task 10: Regenerate API docs and run the full local CI

**Files:**
- Modify: `internal/api/docs/docs.go`, `internal/api/docs/swagger.json`, `internal/api/docs/swagger.yaml` (generated)

**Step 1: Regenerate Swagger**

Run: `swag init -g cmd/aether-server/main.go -o internal/api/docs`
Expected: docs regenerate without errors. If the binary is missing, `go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/aether-server/main.go -o internal/api/docs`.

**Step 2: Run all CI checks locally**

```bash
task check
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
task test:e2e   # requires the dev stack on :5173
```

Expected: all pass. Fix any failures before continuing.

**Step 3: Commit**

```bash
git add internal/api/docs
git commit -m "docs: regenerate Swagger for hidden-table pattern fields"
```

---

### Task 11: Real-browser validation (all flows)

The design mandates real-browser validation for every user-visible flow. Use the `agent-browser` skill for exact command syntax (`open`, `snapshot -i`, click, type, `screenshot`, `errors`).

**Setup**

```bash
docker compose -f docker-compose.dev.yml up -d
docker compose -f docker-compose.dev.yml ps
```

Admin credentials: `admin@heaven-labs.com` / `admin123` (org creator + platform admin). Non-admin: `nova@heaven-labs.com` / `nova123` (org editor).

**Flow 1 — Admin patterns on the warehouse page**

1. Log in as `admin@heaven-labs.com`, go to `/warehouses`, create a warehouse, link the existing `ClickHouse (Dev)` connector.
2. In "Hidden tables", add pattern `^default\.aether_garbage_`. Expect a chip and a "hides N tables" count.
3. Try adding `(`. Expect the inline error `invalid pattern`.
4. Expand "Table grants", pick a database: pattern-matched tables must be absent; an already-granted matched table must still be listed.
5. Screenshot the card and the picker; check `agent-browser errors` is empty.

To seed garbage tables:

```bash
docker compose -f docker-compose.dev.yml exec -T clickhouse clickhouse-client \
  --user dev --password dev -q \
  "CREATE DATABASE IF NOT EXISTS default; CREATE TABLE IF NOT EXISTS default.aether_garbage_1 (id UInt64) ENGINE=MergeTree ORDER BY id; CREATE TABLE IF NOT EXISTS default.aether_garbage_2 (id UInt64) ENGINE=MergeTree ORDER BY id;"
```

**Flow 2 — Non-admin schema browser**

1. Get the IDs via `task db:psql` or the UI, then grant Nova connector `use` and table grants:

```sql
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT om.org_id, 'connector', '<connector-id>'::uuid, 'user', u.id, ARRAY['view','use']
FROM users u JOIN org_members om ON om.user_id = u.id
WHERE u.email = 'nova@heaven-labs.com';

INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
SELECT om.org_id, '<warehouse-id>'::uuid, 'user', u.id, 'default', 'aether_garbage_2'
FROM users u JOIN org_members om ON om.user_id = u.id
WHERE u.email = 'nova@heaven-labs.com';
```

2. Log in as Nova, open a notebook, use the schema browser on the managed connector: only `aether_garbage_2` (the granted table) appears from the visibility database; ungranted tables are gone.
3. Add a group grant (`INSERT ... subject_type='group'`) and an `everyone` grant to verify both surface after reload.
4. Screenshot + `agent-browser errors` empty.

**Flow 3 — New-tables inbox**

1. As admin, create tables `default.aether_new_1` / `default.aether_new_2`, open the warehouse card: only non-matching tables appear in the inbox.

**Flow 4 — Kill switch off**

1. Temporarily set `AETHER_CH_TABLE_PERMISSIONS: "false"` for the `api` service in `docker-compose.dev.yml`, then `docker compose -f docker-compose.dev.yml up -d api`.
2. As Nova, reload the schema browser: all tables appear again (per-user filter off), but pattern-hidden tables stay hidden.
3. Revert: `git checkout -- docker-compose.dev.yml && docker compose -f docker-compose.dev.yml up -d api`.

**Commit:** no code changes expected; if screenshots/notes were requested, write them to `docs/plans/` only if the user asks. Do not commit env changes.

---

### Task 12: Final verification and handoff

1. Re-run the complete local CI (Task 10 Step 2) on the final tree.
2. `git status` must be clean; `git log --oneline` shows one commit per task.
3. Summarize: files changed, test evidence (exact commands + observed results), browser-validation screenshots, and any skipped checks.
4. **Ask the user before opening a PR.** Never push directly to `main`.

Suggested PR command (after approval):

```bash
gh pr create --title "feat: warehouse hidden-table patterns and per-user schema visibility" --body "..."
```

---

## Risk notes for the executor

- `setupWarehouseFixture` and `setupWarehouseFixtureWithServer` build their own `Server` (kill switch on). Do not flip the kill switch on the shared server used by `warehouseHandlersServer`.
- The schema endpoint's snapshot write must stay between the pattern filter and the per-user filter. Moving it after the per-user filter would poison `schema_snapshots` with one viewer's subset.
- `filterVisibleSchemaTables` mutates nothing; it returns a fresh slice.
- Invalid stored patterns fail open (log + skip). Per-user resolution failures fail closed (500). Do not swap these.
- Frontend: invalid regex literals in JS (`new RegExp`) can differ from Go RE2 for exotic patterns; never block saving client-side — the server is the only validator.
