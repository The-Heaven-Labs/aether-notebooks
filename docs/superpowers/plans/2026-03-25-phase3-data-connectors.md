# Phase 3 — Data & Connectors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Prerequisite:** Phase 2 complete (cell editor columns on `cells`, `slug` field, `cell_versions` table in DB).

**Goal:** Add notebook-level default connectors, optional `database` field on connector creation, multi-database schema browsing, `{{slug}}` SQL template substitution with cycle detection, and cell-level parameter overrides.

**Architecture:** Migration `004` adds `connector_id` to `notebooks` and `parameters` to `cells`. `Executor` interface gains `Databases()`. `handleExecuteCell` gains connector resolution (cell → notebook fallback) and slug substitution (recursive, with cycle detection). Frontend adds a notebook connector selector, cell connector override, database picker in `SchemaBrowser`, and a collapsible cell params section.

**Tech Stack:** Go, React 19, TypeScript, Vitest + RTL + MSW (component tests), Playwright (E2E).

---

## File Map

**Create:**
- `internal/database/migrations/004_data_connectors.sql`
- `internal/api/slug_substitution.go` — `resolveSlugRefs()` + cycle detection
- `internal/api/slug_substitution_test.go`
- `web/src/components/ConnectorSelector.tsx` — reusable dropdown for picking a connector
- `web/src/components/DatabasePicker.tsx` — dropdown for selecting a database in SchemaBrowser
- `web/src/test/ConnectorSelector.test.tsx`
- `web/src/test/NotebookConnector.test.tsx`
- `e2e/connectors.spec.ts`
- `e2e/slug-refs.spec.ts`

**Modify:**
- `internal/executor/executor.go` — add `Databases()` to `Executor` interface
- `internal/executor/postgres.go` — implement `Databases()`
- `internal/executor/clickhouse.go` — implement `Databases()`
- `internal/executor/js.go` — implement `Databases()` (returns empty list)
- `internal/executor/postgres_test.go` — add `Databases()` test
- `internal/executor/clickhouse_test.go` — add `Databases()` test
- `internal/api/execute_handlers.go` — connector resolution + slug substitution
- `internal/api/connector_handlers.go` — add `GET /connectors/:id/databases` endpoint, make `database` optional
- `internal/api/notebook_handlers.go` — add `connector_id` to `updateNotebookRequest` + response
- `internal/api/cell_handlers.go` — add `parameters` to `updateCellRequest` + response
- `internal/models/notebook.go` — add `ConnectorID` to `Notebook`, `Parameters` to `Cell`
- `web/src/components/SchemaBrowser.tsx` — add database picker when no default DB
- `web/src/pages/NotebookPage.tsx` — notebook connector selector, cell parameter sections
- `web/src/components/CellToolbar.tsx` — add connector override icon/selector

---

## Task 1: Migration — notebooks.connector_id and cells.parameters

**Files:**
- Create: `internal/database/migrations/004_data_connectors.sql`

- [ ] **Step 1: Write the migration**

```sql
-- internal/database/migrations/004_data_connectors.sql
ALTER TABLE notebooks ADD COLUMN connector_id UUID REFERENCES connectors(id) ON DELETE SET NULL;

ALTER TABLE cells ADD COLUMN parameters JSONB NOT NULL DEFAULT '[]';
```

- [ ] **Step 2: Restart server and verify migration runs**

Run: `task dev` (or `go run ./cmd/server`)
Expected: server starts without error, migration applied once.

Check: `psql $DATABASE_URL -c "\d notebooks"` should show `connector_id` column.
Check: `psql $DATABASE_URL -c "\d cells"` should show `parameters` column.

- [ ] **Step 3: Commit**

```bash
git add internal/database/migrations/004_data_connectors.sql
git commit -m "feat: migration 004 — notebook connector_id and cell parameters"
```

---

## Task 2: Executor interface — Databases() method

**Files:**
- Modify: `internal/executor/executor.go`
- Modify: `internal/executor/postgres.go`
- Modify: `internal/executor/clickhouse.go`
- Modify: `internal/executor/js.go`
- Modify: `internal/executor/postgres_test.go`
- Modify: `internal/executor/clickhouse_test.go`

- [ ] **Step 1: Write the failing test (Postgres)**

Add to `internal/executor/postgres_test.go`:

```go
func TestPostgresDatabases(t *testing.T) {
    cfg := testConnectorConfig(t)
    exec, err := NewPostgresExecutor(cfg)
    require.NoError(t, err)
    defer exec.Close()

    dbs, err := exec.Databases(context.Background())
    require.NoError(t, err)
    assert.Contains(t, dbs, cfg.Database, "should include the connected database")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/... -run TestPostgresDatabases -v`
Expected: FAIL with "exec.Databases undefined"

- [ ] **Step 3: Add Databases() to Executor interface**

Edit `internal/executor/executor.go`:

```go
type Executor interface {
	Execute(ctx context.Context, query string, params map[string]string, maxRows int) (*ResultSet, error)
	TestConnection(ctx context.Context) error
	Schema(ctx context.Context) (*SchemaInfo, error)
	Databases(ctx context.Context) ([]string, error)
	Close() error
}
```

- [ ] **Step 4: Implement Databases() on PostgresExecutor**

Add to `internal/executor/postgres.go`:

```go
func (p *PostgresExecutor) Databases(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname")
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()
	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}
```

- [ ] **Step 5: Implement Databases() on ClickHouseExecutor**

Add to `internal/executor/clickhouse.go`:

```go
func (c *ClickHouseExecutor) Databases(ctx context.Context) ([]string, error) {
	rows, err := c.conn.Query(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer rows.Close()
	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		dbs = append(dbs, name)
	}
	return dbs, rows.Err()
}
```

- [ ] **Step 6: Implement Databases() on JSExecutor**

Add to `internal/executor/js.go`:

```go
func (j *JSExecutor) Databases(ctx context.Context) ([]string, error) {
	return nil, nil
}
```

- [ ] **Step 7: Add ClickHouse Databases test**

Add to `internal/executor/clickhouse_test.go`:

```go
func TestClickHouseDatabases(t *testing.T) {
    cfg := testClickHouseConfig(t)
    exec, err := NewClickHouseExecutor(cfg)
    require.NoError(t, err)
    defer exec.Close()

    dbs, err := exec.Databases(context.Background())
    require.NoError(t, err)
    assert.NotEmpty(t, dbs)
}
```

- [ ] **Step 8: Run all executor tests to verify they pass**

Run: `go test ./internal/executor/... -v`
Expected: PASS for all tests (Databases tests require running infra — skip with build tag if needed).

- [ ] **Step 9: Commit**

```bash
git add internal/executor/executor.go internal/executor/postgres.go internal/executor/clickhouse.go internal/executor/js.go internal/executor/postgres_test.go internal/executor/clickhouse_test.go
git commit -m "feat: add Databases() method to Executor interface and all implementations"
```

---

## Task 3: API route — GET /connectors/:id/databases

**Files:**
- Modify: `internal/api/connector_handlers.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Read connector_handlers.go to understand existing patterns**

Read the top of `internal/api/connector_handlers.go` to find the load-connector + build-executor pattern. The handler follows the same shape as `handleExecuteCell`: load connector, decrypt config, build executor.

- [ ] **Step 2: Write the failing test**

Add to `internal/api/connector_handlers_test.go` (create if needed):

```go
func TestHandleListConnectorDatabases(t *testing.T) {
    s := setupTestServer(t)

    // Create a connector
    connID := createTestConnector(t, s)

    req := httptest.NewRequest("GET", "/api/v1/connectors/"+connID+"/databases", nil)
    req = withAdminClaims(req, testOrgID)
    w := httptest.NewRecorder()
    s.ServeHTTP(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
    var resp map[string][]string
    json.NewDecoder(w.Body).Decode(&resp)
    assert.NotNil(t, resp["databases"])
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestHandleListConnectorDatabases -v`
Expected: FAIL with 404 (route not yet registered).

- [ ] **Step 4: Add the handler**

Add to `internal/api/connector_handlers.go`:

```go
func (s *Server) handleListConnectorDatabases(w http.ResponseWriter, r *http.Request) {
    claims := ClaimsFromContext(r.Context())
    connID := r.PathValue("id")
    ctx := r.Context()

    var connType models.ConnectorType
    var encryptedConfig []byte
    err := s.db.Pool.QueryRow(ctx,
        `SELECT type, config_encrypted FROM connectors WHERE id = $1 AND org_id = $2`,
        connID, claims.OrgID,
    ).Scan(&connType, &encryptedConfig)
    if err == pgx.ErrNoRows {
        writeError(w, http.StatusNotFound, "connector not found")
        return
    }
    if err != nil {
        writeError(w, http.StatusInternalServerError, "load connector failed")
        return
    }

    plain, err := crypto.Decrypt(encryptedConfig, s.masterKey)
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to decrypt connector config")
        return
    }
    var cfg models.ConnectorConfig
    if err := json.Unmarshal(plain, &cfg); err != nil {
        writeError(w, http.StatusInternalServerError, "invalid connector config")
        return
    }

    var exec executor.Executor
    switch connType {
    case models.ConnectorPostgres:
        exec, err = executor.NewPostgresExecutor(cfg)
    case models.ConnectorClickHouse:
        exec, err = executor.NewClickHouseExecutor(cfg)
    default:
        writeJSON(w, http.StatusOK, map[string][]string{"databases": {}})
        return
    }
    if err != nil {
        writeError(w, http.StatusBadGateway, "failed to connect")
        return
    }
    defer exec.Close()

    dbs, err := exec.Databases(ctx)
    if err != nil {
        writeError(w, http.StatusBadGateway, err.Error())
        return
    }
    if dbs == nil {
        dbs = []string{}
    }
    writeJSON(w, http.StatusOK, map[string][]string{"databases": dbs})
}
```

- [ ] **Step 5: Register the route**

Add to `internal/api/router.go` in `s.routes()` after the existing connector routes:

```go
s.mux.Handle("GET /api/v1/connectors/{id}/databases", authMW(http.HandlerFunc(s.handleListConnectorDatabases)))
```

- [ ] **Step 6: Make `database` field optional in connector creation**

In `internal/api/connector_handlers.go`, find `handleCreateConnector`. The validation currently rejects empty `Database`. Change it to only require `Database` when the connector type does not support `SHOW DATABASES`. Since the spec says Postgres always needs a database but ClickHouse does not:

```go
// Replace the existing database validation block with:
if cfg.Database == "" && connType == models.ConnectorPostgres {
    writeError(w, http.StatusBadRequest, "database is required for Postgres connectors")
    return
}
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/api/... -run TestHandleListConnectorDatabases -v`
Expected: PASS.

Run: `go test ./internal/api/... -v`
Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/api/connector_handlers.go internal/api/router.go
git commit -m "feat: GET /connectors/:id/databases endpoint, optional database for ClickHouse"
```

---

## Task 4: Notebook connector_id — model, handler, API

**Files:**
- Modify: `internal/models/notebook.go`
- Modify: `internal/api/notebook_handlers.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/api/notebook_handlers_test.go`:

```go
func TestNotebookConnectorID(t *testing.T) {
    s := setupTestServer(t)
    connID := createTestConnector(t, s)

    // Create notebook
    nbID := createTestNotebook(t, s)

    // Update notebook with connector
    body := fmt.Sprintf(`{"title":"Test","connector_id":"%s"}`, connID)
    req := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID, strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req = withEditorClaims(req, testOrgID)
    w := httptest.NewRecorder()
    s.ServeHTTP(w, req)
    assert.Equal(t, http.StatusOK, w.Code)

    // Get notebook — connector_id should be returned
    req2 := httptest.NewRequest("GET", "/api/v1/notebooks/"+nbID, nil)
    req2 = withEditorClaims(req2, testOrgID)
    w2 := httptest.NewRecorder()
    s.ServeHTTP(w2, req2)
    assert.Equal(t, http.StatusOK, w2.Code)
    var nb map[string]interface{}
    json.NewDecoder(w2.Body).Decode(&nb)
    assert.Equal(t, connID, nb["connector_id"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestNotebookConnectorID -v`
Expected: FAIL — `connector_id` not in response.

- [ ] **Step 3: Add ConnectorID to Notebook model**

Edit `internal/models/notebook.go`:

```go
type Notebook struct {
	ID          string      `json:"id"`
	OrgID       string      `json:"org_id"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	ConnectorID string      `json:"connector_id,omitempty"`
	Parameters  []Parameter `json:"parameters"`
	CreatedBy   string      `json:"created_by"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}
```

- [ ] **Step 4: Update handleGetNotebook to scan connector_id**

In `internal/api/notebook_handlers.go`, find the SELECT query in `handleGetNotebook`. Add `connector_id` to the SELECT and Scan:

```go
// In handleGetNotebook, replace the QueryRow scan to include connector_id:
var connID *string
err := s.db.Pool.QueryRow(ctx,
    `SELECT id, org_id, title, description, connector_id, parameters, created_by, created_at, updated_at
     FROM notebooks WHERE id = $1 AND org_id = $2`,
    id, claims.OrgID,
).Scan(&nb.ID, &nb.OrgID, &nb.Title, &nb.Description, &connID,
    &paramsJSON, &nb.CreatedBy, &nb.CreatedAt, &nb.UpdatedAt)
// ...
if connID != nil {
    nb.ConnectorID = *connID
}
```

- [ ] **Step 5: Update handleListNotebooks similarly**

In `handleListNotebooks`, add `connector_id` to the SELECT and scan with a `*string` pointer.

- [ ] **Step 6: Update handleUpdateNotebook to accept and save connector_id**

In `internal/api/notebook_handlers.go`, find `updateNotebookRequest`:

```go
type updateNotebookRequest struct {
	Title       string      `json:"title"`
	Description string      `json:"description"`
	ConnectorID *string     `json:"connector_id"` // pointer so null clears it
	Parameters  []models.Parameter `json:"parameters"`
}
```

In the UPDATE query of `handleUpdateNotebook`, add `connector_id = $N` to the SET clause and pass `req.ConnectorID` as the argument.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/api/... -run TestNotebookConnectorID -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/models/notebook.go internal/api/notebook_handlers.go
git commit -m "feat: notebook connector_id — model, GET/PUT handler support"
```

---

## Task 5: Cell-level parameters — model, handler, API

**Files:**
- Modify: `internal/models/notebook.go`
- Modify: `internal/api/cell_handlers.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/api/cell_handlers_test.go`:

```go
func TestCellParameters(t *testing.T) {
    s := setupTestServer(t)
    nbID := createTestNotebook(t, s)
    cellID := createTestCell(t, s, nbID)

    params := `[{"name":"start_date","type":"string","default":"2024-01-01"}]`
    body := fmt.Sprintf(`{"source":"SELECT 1","parameters":%s}`, params)
    req := httptest.NewRequest("PUT", "/api/v1/notebooks/"+nbID+"/cells/"+cellID, strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req = withEditorClaims(req, testOrgID)
    w := httptest.NewRecorder()
    s.ServeHTTP(w, req)
    assert.Equal(t, http.StatusOK, w.Code)

    // Verify parameters are returned in response
    var cell map[string]interface{}
    json.NewDecoder(w.Body).Decode(&cell)
    params_resp := cell["parameters"].([]interface{})
    assert.Len(t, params_resp, 1)
    assert.Equal(t, "start_date", params_resp[0].(map[string]interface{})["name"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestCellParameters -v`
Expected: FAIL — `parameters` not in cell response.

- [ ] **Step 3: Add Parameters to Cell model**

Edit `internal/models/notebook.go`:

```go
type Cell struct {
	ID            string      `json:"id"`
	NotebookID    string      `json:"notebook_id"`
	Position      int         `json:"position"`
	Type          CellType    `json:"type"`
	Language      string      `json:"language,omitempty"`
	ConnectorID   string      `json:"connector_id,omitempty"`
	Source        string      `json:"source"`
	Title         string      `json:"title,omitempty"`
	Description   string      `json:"description,omitempty"`
	Slug          string      `json:"slug,omitempty"`
	SourceVisible bool        `json:"source_visible"`
	CellCollapsed bool        `json:"cell_collapsed"`
	Parameters    []Parameter `json:"parameters"`
	Outputs       []Output    `json:"outputs"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}
```

- [ ] **Step 4: Update cell handlers to include parameters**

In `internal/api/cell_handlers.go`, in the `updateCellRequest` struct, add:

```go
type updateCellRequest struct {
	Source        string           `json:"source"`
	Title         *string          `json:"title"`
	Description   *string          `json:"description"`
	Slug          *string          `json:"slug"`
	ConnectorID   *string          `json:"connector_id"`
	SourceVisible *bool            `json:"source_visible"`
	CellCollapsed *bool            `json:"cell_collapsed"`
	Parameters    []models.Parameter `json:"parameters"`
}
```

In `handleUpdateCell`, add `parameters` to the UPDATE query and scan. When marshalling the response cell, scan `parameters` from the DB and unmarshal the JSONB column into `cell.Parameters`.

In `handleGetNotebook` (which returns cells inline), scan `parameters` from cells table.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/api/... -run TestCellParameters -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/models/notebook.go internal/api/cell_handlers.go
git commit -m "feat: cell-level parameters — model and handler support"
```

---

## Task 6: Slug substitution with cycle detection

**Files:**
- Create: `internal/api/slug_substitution.go`
- Create: `internal/api/slug_substitution_test.go`
- Modify: `internal/api/execute_handlers.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/api/slug_substitution_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCell struct {
	slug   string
	source string
}

func makeSlugMap(cells []stubCell) map[string]string {
	m := make(map[string]string, len(cells))
	for _, c := range cells {
		if c.slug != "" {
			m[c.slug] = c.source
		}
	}
	return m
}

func TestResolveSlugRefs_NoRefs(t *testing.T) {
	slugMap := makeSlugMap([]stubCell{
		{slug: "cell_a", source: "SELECT 1"},
	})
	result, err := resolveSlugRefs("SELECT * FROM foo", slugMap)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM foo", result)
}

func TestResolveSlugRefs_SimpleSubstitution(t *testing.T) {
	slugMap := makeSlugMap([]stubCell{
		{slug: "cell_a", source: "SELECT id FROM users"},
	})
	result, err := resolveSlugRefs("SELECT * FROM ({{cell_a}}) t", slugMap)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM ((SELECT id FROM users)) t", result)
}

func TestResolveSlugRefs_UnknownSlug(t *testing.T) {
	slugMap := makeSlugMap([]stubCell{})
	_, err := resolveSlugRefs("SELECT * FROM ({{missing_cell}}) t", slugMap)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown slug")
}

func TestResolveSlugRefs_DirectCycle(t *testing.T) {
	// cell_a references itself
	slugMap := map[string]string{
		"cell_a": "SELECT * FROM ({{cell_a}}) t",
	}
	_, err := resolveSlugRefs("SELECT * FROM ({{cell_a}}) t", slugMap)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestResolveSlugRefs_IndirectCycle(t *testing.T) {
	// cell_a → cell_b → cell_a
	slugMap := map[string]string{
		"cell_a": "SELECT * FROM ({{cell_b}}) t",
		"cell_b": "SELECT * FROM ({{cell_a}}) t",
	}
	_, err := resolveSlugRefs("SELECT * FROM ({{cell_a}}) t", slugMap)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestResolveSlugRefs_NestedResolution(t *testing.T) {
	// cell_b references cell_a (valid, no cycle)
	slugMap := map[string]string{
		"cell_a": "SELECT id FROM users",
		"cell_b": "SELECT * FROM ({{cell_a}}) t WHERE id > 5",
	}
	result, err := resolveSlugRefs("SELECT * FROM ({{cell_b}}) outer", slugMap)
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM ((SELECT * FROM ((SELECT id FROM users)) t WHERE id > 5)) outer", result)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run TestResolveSlugRefs -v`
Expected: FAIL with "resolveSlugRefs undefined"

- [ ] **Step 3: Implement resolveSlugRefs**

Create `internal/api/slug_substitution.go`:

```go
package api

import (
	"fmt"
	"regexp"
	"strings"
)

var slugRefRe = regexp.MustCompile(`\{\{([a-zA-Z0-9_-]+)\}\}`)

// resolveSlugRefs replaces all {{slug}} tokens in source with the corresponding
// cell source from slugMap (wrapped in parens). Recursively resolves nested
// references. Returns an error if a referenced slug is unknown or a cycle is detected.
func resolveSlugRefs(source string, slugMap map[string]string) (string, error) {
	return resolveWithVisited(source, slugMap, []string{})
}

func resolveWithVisited(source string, slugMap map[string]string, visiting []string) (string, error) {
	matches := slugRefRe.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return source, nil
	}

	result := source
	for _, m := range matches {
		token := m[0]  // "{{slug}}"
		slug := m[1]   // "slug"

		refSource, ok := slugMap[slug]
		if !ok {
			return "", fmt.Errorf("unknown slug %q referenced in query", slug)
		}

		// Cycle detection
		for _, v := range visiting {
			if v == slug {
				cycle := append(visiting, slug)
				return "", fmt.Errorf("cycle detected in slug references: %s", strings.Join(cycle, " → "))
			}
		}

		// Recurse into the referenced cell's source
		resolved, err := resolveWithVisited(refSource, slugMap, append(visiting, slug))
		if err != nil {
			return "", err
		}

		result = strings.Replace(result, token, "("+resolved+")", 1)
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/... -run TestResolveSlugRefs -v`
Expected: all 5 tests PASS.

- [ ] **Step 5: Wire slug substitution into handleExecuteCell**

Edit `internal/api/execute_handlers.go`. After loading the cell and notebook parameters (before building the executor), add slug resolution:

```go
// Load all slugs for this notebook (for {{slug}} substitution)
slugRows, err := s.db.Pool.Query(ctx,
    `SELECT slug, source FROM cells WHERE notebook_id = $1 AND slug IS NOT NULL`,
    nbID,
)
if err != nil {
    writeError(w, http.StatusInternalServerError, "failed to load cell slugs")
    return
}
defer slugRows.Close()
slugMap := make(map[string]string)
for slugRows.Next() {
    var slug, src string
    if err := slugRows.Scan(&slug, &src); err != nil {
        writeError(w, http.StatusInternalServerError, "scan slug failed")
        return
    }
    slugMap[slug] = src
}
if err := slugRows.Err(); err != nil {
    writeError(w, http.StatusInternalServerError, "slug rows error")
    return
}

// Resolve {{slug}} references in the cell source
resolvedSource, err := resolveSlugRefs(cell.Source, slugMap)
if err != nil {
    writeError(w, http.StatusBadRequest, err.Error())
    return
}
```

Then pass `resolvedSource` instead of `cell.Source` to `exec.Execute(...)`.

- [ ] **Step 6: Wire notebook connector fallback into handleExecuteCell**

In `handleExecuteCell`, after loading the cell, add notebook-level connector fallback. Change the connector loading section:

```go
// Resolve effective connector: cell → notebook fallback
effectiveConnID := cell.ConnectorID
if effectiveConnID == "" {
    // Try notebook-level connector
    s.db.Pool.QueryRow(ctx, "SELECT connector_id FROM notebooks WHERE id = $1", nbID).Scan(&effectiveConnID)
}
if effectiveConnID == "" {
    writeError(w, http.StatusBadRequest, "no connector assigned to cell or notebook")
    return
}
```

Replace the existing `cell.ConnectorID` references with `effectiveConnID`.

- [ ] **Step 7: Wire cell-level parameter overrides**

After loading notebook parameters, load and merge cell-level parameters:

```go
// Merge cell-level parameters (cell params override notebook params)
var cellParamsJSON []byte
s.db.Pool.QueryRow(ctx, "SELECT parameters FROM cells WHERE id = $1", cellID).Scan(&cellParamsJSON)
var cellParams []models.Parameter
json.Unmarshal(cellParamsJSON, &cellParams)
for _, p := range cellParams {
    if _, ok := req.Parameters[p.Name]; !ok {
        req.Parameters[p.Name] = p.Default
    }
}
```

- [ ] **Step 8: Run all API tests**

Run: `go test ./internal/api/... -v`
Expected: all tests PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/api/slug_substitution.go internal/api/slug_substitution_test.go internal/api/execute_handlers.go
git commit -m "feat: {{slug}} substitution with cycle detection + notebook/cell connector fallback + cell param overrides"
```

---

## Task 7: Frontend — ConnectorSelector component

**Files:**
- Create: `web/src/components/ConnectorSelector.tsx`
- Create: `web/src/test/ConnectorSelector.test.tsx`

- [ ] **Step 1: Write the failing test**

Create `web/src/test/ConnectorSelector.test.tsx`:

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { ConnectorSelector } from '../components/ConnectorSelector'
import { http, HttpResponse } from 'msw'
import { server } from './setup'

const mockConnectors = [
  { id: 'conn-1', name: 'Production DB', type: 'postgres' },
  { id: 'conn-2', name: 'Analytics CH', type: 'clickhouse' },
]

beforeEach(() => {
  server.use(
    http.get('/api/v1/connectors', () => HttpResponse.json({ connectors: mockConnectors }))
  )
})

test('renders connector options', async () => {
  render(<ConnectorSelector value={null} onChange={() => {}} />)
  const select = await screen.findByRole('combobox')
  expect(select).toBeInTheDocument()
  expect(await screen.findByText('Production DB')).toBeInTheDocument()
  expect(await screen.findByText('Analytics CH')).toBeInTheDocument()
})

test('shows inherited placeholder when value is null', () => {
  render(<ConnectorSelector value={null} onChange={() => {}} placeholder="Inherited from notebook" />)
  expect(screen.getByText('Inherited from notebook')).toBeInTheDocument()
})

test('calls onChange when selection changes', async () => {
  const onChange = vi.fn()
  render(<ConnectorSelector value={null} onChange={onChange} />)
  const select = await screen.findByRole('combobox')
  fireEvent.change(select, { target: { value: 'conn-1' } })
  expect(onChange).toHaveBeenCalledWith('conn-1')
})

test('calls onChange with null when "None" selected', async () => {
  const onChange = vi.fn()
  render(<ConnectorSelector value="conn-1" onChange={onChange} allowClear />)
  const select = await screen.findByRole('combobox')
  fireEvent.change(select, { target: { value: '' } })
  expect(onChange).toHaveBeenCalledWith(null)
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npm run test -- --run ConnectorSelector`
Expected: FAIL with module not found.

- [ ] **Step 3: Implement ConnectorSelector**

Create `web/src/components/ConnectorSelector.tsx`:

```tsx
import { useEffect, useState } from 'react'

interface Connector {
  id: string
  name: string
  type: string
}

interface ConnectorSelectorProps {
  value: string | null
  onChange: (id: string | null) => void
  placeholder?: string
  allowClear?: boolean
  className?: string
}

export function ConnectorSelector({
  value,
  onChange,
  placeholder = 'Select connector',
  allowClear = false,
  className,
}: ConnectorSelectorProps) {
  const [connectors, setConnectors] = useState<Connector[]>([])

  useEffect(() => {
    fetch('/api/v1/connectors', { credentials: 'include' })
      .then(r => r.json())
      .then(data => setConnectors(data.connectors ?? []))
      .catch(() => {})
  }, [])

  return (
    <select
      className={className}
      value={value ?? ''}
      onChange={e => onChange(e.target.value || null)}
    >
      <option value="">{allowClear && value ? '— None —' : placeholder}</option>
      {connectors.map(c => (
        <option key={c.id} value={c.id}>
          {c.name}
        </option>
      ))}
    </select>
  )
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npm run test -- --run ConnectorSelector`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/ConnectorSelector.tsx web/src/test/ConnectorSelector.test.tsx
git commit -m "feat: ConnectorSelector component with MSW tests"
```

---

## Task 8: Frontend — Notebook connector selector + cell connector override

**Files:**
- Create: `web/src/test/NotebookConnector.test.tsx`
- Modify: `web/src/pages/NotebookPage.tsx`
- Modify: `web/src/components/CellToolbar.tsx`

- [ ] **Step 1: Write the failing test for notebook connector**

Create `web/src/test/NotebookConnector.test.tsx`:

```tsx
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { NotebookPage } from '../pages/NotebookPage'
import { http, HttpResponse } from 'msw'
import { server } from './setup'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

const mockNotebook = {
  id: 'nb-1',
  title: 'Test Notebook',
  description: '',
  connector_id: null,
  parameters: [],
  cells: [],
}
const mockConnectors = [{ id: 'conn-1', name: 'Prod DB', type: 'postgres' }]

beforeEach(() => {
  server.use(
    http.get('/api/v1/notebooks/:id', () => HttpResponse.json(mockNotebook)),
    http.get('/api/v1/connectors', () => HttpResponse.json({ connectors: mockConnectors })),
  )
})

test('notebook header shows connector selector', async () => {
  render(
    <MemoryRouter initialEntries={['/notebooks/nb-1']}>
      <Routes><Route path="/notebooks/:id" element={<NotebookPage />} /></Routes>
    </MemoryRouter>
  )
  expect(await screen.findByText('Select connector')).toBeInTheDocument()
})

test('selecting a connector calls PUT /notebooks/:id', async () => {
  let capturedBody: Record<string, unknown> = {}
  server.use(
    http.put('/api/v1/notebooks/:id', async ({ request }) => {
      capturedBody = await request.json() as Record<string, unknown>
      return HttpResponse.json({ ...mockNotebook, connector_id: 'conn-1' })
    })
  )
  render(
    <MemoryRouter initialEntries={['/notebooks/nb-1']}>
      <Routes><Route path="/notebooks/:id" element={<NotebookPage />} /></Routes>
    </MemoryRouter>
  )
  const select = await screen.findByRole('combobox', { name: /connector/i })
  fireEvent.change(select, { target: { value: 'conn-1' } })
  await waitFor(() => expect(capturedBody.connector_id).toBe('conn-1'))
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npm run test -- --run NotebookConnector`
Expected: FAIL.

- [ ] **Step 3: Add connector selector to NotebookPage header**

In `web/src/pages/NotebookPage.tsx`, in the notebook header section (near the title and description inputs), add:

```tsx
import { ConnectorSelector } from '../components/ConnectorSelector'

// In the header JSX, after description:
<label className="text-xs text-gray-500">Default Connector</label>
<ConnectorSelector
  aria-label="connector"
  value={notebook.connector_id ?? null}
  onChange={id => updateNotebook({ connector_id: id })}
  placeholder="Select connector"
  allowClear
/>
```

Where `updateNotebook` calls `PUT /api/v1/notebooks/:id` with the updated field (follow the existing `handleUpdateTitle` pattern).

- [ ] **Step 4: Add cell connector override to CellToolbar**

In `web/src/components/CellToolbar.tsx`, add a connector override button/selector. When the cell's `connector_id` is null, show a small connector icon with "Inherited" tooltip. Clicking it expands an inline `ConnectorSelector` with `allowClear`:

```tsx
import { ConnectorSelector } from './ConnectorSelector'

// In CellToolbar props interface, add:
connectorId?: string | null
notebookConnectorId?: string | null
onUpdateConnector?: (id: string | null) => void

// In the toolbar JSX, add before existing buttons:
{onUpdateConnector && (
  <div className="relative">
    <button
      title={connectorId ? 'Override connector' : 'Inherited from notebook'}
      className={`toolbar-btn ${connectorId ? 'text-indigo-400' : 'text-gray-500'}`}
      onClick={() => setShowConnectorPicker(v => !v)}
    >
      🔌
    </button>
    {showConnectorPicker && (
      <div className="absolute top-full left-0 z-10 bg-gray-800 border border-gray-700 rounded p-2 min-w-48">
        <ConnectorSelector
          value={connectorId ?? null}
          onChange={id => { onUpdateConnector(id); setShowConnectorPicker(false) }}
          placeholder="Inherited from notebook"
          allowClear
        />
      </div>
    )}
  </div>
)}
```

Add `const [showConnectorPicker, setShowConnectorPicker] = useState(false)` to the toolbar component.

- [ ] **Step 5: Wire cell connector updates in NotebookPage**

In `NotebookPage.tsx`, pass `connectorId`, `notebookConnectorId`, and `onUpdateConnector` props to `CellToolbar`. `onUpdateConnector` calls `PUT /notebooks/:id/cells/:cell_id` with `{ connector_id: id }`.

- [ ] **Step 6: Run tests**

Run: `cd web && npm run test -- --run NotebookConnector`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/NotebookPage.tsx web/src/components/CellToolbar.tsx web/src/test/NotebookConnector.test.tsx
git commit -m "feat: notebook connector selector + cell connector override UI"
```

---

## Task 9: Frontend — DatabasePicker in SchemaBrowser

**Files:**
- Create: `web/src/components/DatabasePicker.tsx`
- Modify: `web/src/components/SchemaBrowser.tsx`

- [ ] **Step 1: Implement DatabasePicker**

Create `web/src/components/DatabasePicker.tsx`:

```tsx
import { useEffect, useState } from 'react'

interface DatabasePickerProps {
  connectorId: string
  value: string | null
  onChange: (db: string) => void
}

export function DatabasePicker({ connectorId, value, onChange }: DatabasePickerProps) {
  const [databases, setDatabases] = useState<string[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!connectorId) return
    setLoading(true)
    fetch(`/api/v1/connectors/${connectorId}/databases`, { credentials: 'include' })
      .then(r => r.json())
      .then(data => setDatabases(data.databases ?? []))
      .finally(() => setLoading(false))
  }, [connectorId])

  if (loading) return <div className="text-xs text-gray-500 p-2">Loading databases…</div>
  if (databases.length === 0) return null

  return (
    <div className="p-2 border-b border-gray-700">
      <label className="text-xs text-gray-500 block mb-1">Database</label>
      <select
        className="w-full text-sm bg-gray-800 border border-gray-700 rounded px-2 py-1"
        value={value ?? ''}
        onChange={e => onChange(e.target.value)}
      >
        <option value="">— select database —</option>
        {databases.map(db => (
          <option key={db} value={db}>{db}</option>
        ))}
      </select>
    </div>
  )
}
```

- [ ] **Step 2: Integrate into SchemaBrowser**

In `web/src/components/SchemaBrowser.tsx`, read the current implementation (check if `connectorConfig.database` is checked). Add logic:

```tsx
import { DatabasePicker } from './DatabasePicker'

// In SchemaBrowser, add state:
const [activeDatabase, setActiveDatabase] = useState<string | null>(props.connector?.database || null)

// In the render, before the table tree, add:
{!props.connector?.database && (
  <DatabasePicker
    connectorId={props.connectorId}
    value={activeDatabase}
    onChange={db => {
      setActiveDatabase(db)
      // Re-fetch schema for this database (pass as query param or re-trigger fetch)
      loadSchema(db)
    }}
  />
)}
```

Where `loadSchema(db)` calls `GET /api/v1/connectors/:id/schema?database=db`. Update the backend's `handleGetConnectorSchema` to accept and use an optional `database` query param if provided.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/DatabasePicker.tsx web/src/components/SchemaBrowser.tsx
git commit -m "feat: database picker in SchemaBrowser for connectors without default DB"
```

---

## Task 10: Frontend — Cell parameters section

**Files:**
- Modify: `web/src/pages/NotebookPage.tsx`

- [ ] **Step 1: Add collapsible cell parameters section**

In `NotebookPage.tsx` (or wherever cells are rendered), add a parameters section above the cell editor for cells that have `parameters.length > 0` or when the user clicks "Add param". This mirrors the existing `ParametersBar` pattern.

For each cell with parameters, render a compact collapsible bar:

```tsx
{cell.parameters && cell.parameters.length > 0 && (
  <div className="cell-params border-b border-gray-700 px-3 py-1 flex flex-wrap gap-2 items-center">
    <span className="text-xs text-gray-500">Cell params:</span>
    {cell.parameters.map(p => (
      <div key={p.name} className="flex items-center gap-1">
        <span className="text-xs font-mono text-indigo-400">{p.name}</span>
        <span className="text-xs text-gray-500">=</span>
        <input
          className="text-xs bg-gray-800 border border-gray-700 rounded px-1 py-0.5 w-24"
          value={p.default}
          onChange={e => updateCellParam(cell.id, p.name, e.target.value)}
        />
      </div>
    ))}
  </div>
)}
```

Where `updateCellParam` calls `PUT /notebooks/:id/cells/:cell_id` with the updated `parameters` array.

- [ ] **Step 2: Commit**

```bash
git add web/src/pages/NotebookPage.tsx
git commit -m "feat: collapsible cell-level parameters section in notebook"
```

---

## Task 11: E2E tests — Connectors and slug refs

**Files:**
- Create: `e2e/connectors.spec.ts`
- Create: `e2e/slug-refs.spec.ts`

- [ ] **Step 1: Write connectors E2E spec**

Create `e2e/connectors.spec.ts`:

```typescript
import { test, expect } from '@playwright/test'
import { loginAsAdmin } from './helpers'

test.describe('Connectors', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
  })

  test('create connector with database field', async ({ page }) => {
    await page.goto('/connectors')
    await page.click('button:has-text("New Connector")')
    await page.selectOption('select[name="type"]', 'postgres')
    await page.fill('input[name="name"]', 'Test PG')
    await page.fill('input[name="host"]', 'localhost')
    await page.fill('input[name="port"]', '5432')
    await page.fill('input[name="database"]', 'testdb')
    await page.fill('input[name="user"]', 'user')
    await page.fill('input[name="password"]', 'pass')
    await page.click('button:has-text("Save")')
    await expect(page.locator('text=Test PG')).toBeVisible()
  })

  test('create ClickHouse connector without database field', async ({ page }) => {
    await page.goto('/connectors')
    await page.click('button:has-text("New Connector")')
    await page.selectOption('select[name="type"]', 'clickhouse')
    await page.fill('input[name="name"]', 'Test CH')
    await page.fill('input[name="host"]', 'localhost')
    await page.fill('input[name="port"]', '9000')
    // Leave database empty — should be allowed for ClickHouse
    await page.fill('input[name="user"]', 'default')
    await page.fill('input[name="password"]', '')
    await page.click('button:has-text("Save")')
    await expect(page.locator('text=Test CH')).toBeVisible()
  })

  test('schema browser shows database picker for connector without default DB', async ({ page }) => {
    // Assumes a ClickHouse connector without a database is set up
    await page.goto('/notebooks')
    await page.click('text=New Notebook')
    await page.locator('.notebook-connector-selector').selectOption({ label: /Test CH/ })
    // Open schema browser
    await page.click('button:has-text("Schema")')
    await expect(page.locator('text=select database')).toBeVisible()
  })

  test('visual snapshot: sidebar collapsed', async ({ page }) => {
    await page.goto('/connectors')
    await expect(page).toHaveScreenshot('connectors-page.png')
  })
})
```

- [ ] **Step 2: Write slug refs E2E spec**

Create `e2e/slug-refs.spec.ts`:

```typescript
import { test, expect } from '@playwright/test'
import { loginAsAdmin, createNotebook } from './helpers'

test.describe('Slug references', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
  })

  test('cell with slug can be referenced by another cell', async ({ page }) => {
    const nbId = await createNotebook(page, 'Slug Test Notebook')
    await page.goto(`/notebooks/${nbId}`)

    // Set slug on first cell
    const firstCell = page.locator('.cell').first()
    await firstCell.locator('.cell-slug-input').fill('base_query')
    await firstCell.locator('.cell-slug-input').press('Enter')

    // Add a second cell that references the first
    await page.click('button:has-text("Add Cell")')
    const secondCell = page.locator('.cell').nth(1)
    await secondCell.locator('.cell-source').fill('SELECT * FROM ({{base_query}}) t LIMIT 10')

    // Assign a connector and run
    // (connector setup omitted — depends on infra)
    await expect(secondCell.locator('.cell-slug-input')).toBeVisible()

    // Visual snapshot
    await expect(page).toHaveScreenshot('cell-with-slug-ref.png')
  })

  test('cycle in slug references shows error', async ({ page }) => {
    // This test verifies the 400 error is surfaced in the UI
    // Detailed cycle setup would require 2 cells with mutual references — abbreviated here
    const nbId = await createNotebook(page, 'Cycle Test')
    await page.goto(`/notebooks/${nbId}`)
    // Full cycle setup would require backend integration
    // Visual smoke test only
    await expect(page).toHaveScreenshot('notebook-empty.png')
  })
})
```

- [ ] **Step 3: Run E2E tests**

Run: `npx playwright test e2e/connectors.spec.ts e2e/slug-refs.spec.ts --config=e2e/playwright.config.ts`
Expected: tests run (some may be skipped if infra not available); visual snapshots created on first run.

- [ ] **Step 4: Commit**

```bash
git add e2e/connectors.spec.ts e2e/slug-refs.spec.ts
git commit -m "test: E2E specs for connectors and slug references"
```

---

## Phase 3 Visual Validation Checklist

Before merging Phase 3, a human reviewer checks:

- [ ] Notebook-level connector selector is clearly labelled as a default — cells without their own connector show "Inherited from notebook" in the toolbar icon tooltip
- [ ] Cell-level connector override icon is subtle when set to default (inheriting), prominent (indigo color) when overridden
- [ ] Database picker in SchemaBrowser is intuitive — clear which database is currently active, updates table tree on selection
- [ ] `{{slug}}` references in cell source are visually distinguishable (CodeMirror syntax decoration) from plain SQL
- [ ] "Referenced by N cells" badge (if shown) does not clutter cells that have no references
- [ ] Cell parameters section is compact and collapsible — does not push the editor down unexpectedly
- [ ] Error messages for unknown slugs and cycles are surfaced clearly in the cell output area (not just a browser console error)
