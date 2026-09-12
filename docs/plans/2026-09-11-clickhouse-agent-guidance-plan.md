# ClickHouse Agent Guidance Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Teach agents ClickHouse sorting-key optimization and expose `sorting_key`/`primary_key`/`engine` through `explore_schema`, which becomes available to every agent.

**Architecture:** Inject a connector-aware guidance block into the per-session system prompt, enrich the ClickHouse schema path (single `system.tables` query), seed `explore_schema` for all orgs/agents, and annotate `sql_query` tool descriptions when bound to ClickHouse. Depends on the tool-timeouts plan for `explore_schema`'s 60s `ToolDef.Timeout`.

**Tech Stack:** Go, pgx, clickhouse-go via `internal/executor`, agent system-prompt builder, builtin tool seeding.

**Design doc:** `docs/plans/2026-09-11-clickhouse-agent-guidance-design.md`

---

### Task 1: Extend `TableInfo` and enrich ClickHouse `Schema()`

**Files:**
- Modify: `internal/executor/executor.go` (`TableInfo`, ~34)
- Modify: `internal/executor/clickhouse.go` (`Schema`, ~413)
- Test: `internal/executor/clickhouse_test.go` (extend; skipped when ClickHouse is absent, following existing patterns)

**Step 1: Failing test**

```go
func TestClickHouseSchemaIncludesSortingKey(t *testing.T) {
    exec := setupClickHouse(t) // existing helper pattern; skip if unavailable
    _, err := exec.Execute(ctx, `CREATE TABLE IF NOT EXISTS test_sk (ts DateTime, v UInt64)
        ENGINE = MergeTree ORDER BY toStartOfHour(ts)`, nil, 0)
    require.NoError(t, err)
    defer exec.Execute(ctx, `DROP TABLE IF EXISTS test_sk`, nil, 0)

    info, err := exec.Schema(ctx)
    require.NoError(t, err)
    tbl := findTable(t, info, "test_sk")
    require.Equal(t, "toStartOfHour(ts)", tbl.SortingKey)
    require.NotEmpty(t, tbl.Engine)
}
```

**Step 2: Run to fail**

Run: `go test ./internal/executor/ -run TestClickHouseSchemaIncludesSortingKey -v`
Expected: FAIL — unknown field `SortingKey`.

**Step 3: Implement**

```go
type TableInfo struct {
    Schema      string
    Name        string
    Columns     []ColumnInfo
    Description string
    Engine      string `json:",omitempty"`
    SortingKey  string `json:",omitempty"`
    PrimaryKey  string `json:",omitempty"`
}
```

In `ClickHouseExecutor.Schema()`: keep the `system.columns` query; replace the per-table comment loop with one query:

```go
rows2, err := c.conn.Query(ctx,
    `SELECT database, name, engine, sorting_key, primary_key, comment
     FROM system.tables
     WHERE database NOT IN ('system','information_schema','INFORMATION_SCHEMA')`)
// map into tableMap by db+"."+name
```

**Step 4: Pass + commit**

```bash
git add internal/executor/executor.go internal/executor/clickhouse.go internal/executor/clickhouse_test.go
git commit -m "feat(executor): expose ClickHouse sorting key and engine in schema"
```

---

### Task 2: Surface new fields + note in `explore_schema`

**Files:**
- Modify: `internal/agent/tools_notebook.go` (`makeExploreSchemaHandler`, ~1170-1246)
- Test: `internal/agent/tools_notebook_test.go`

**Step 1: Failing test** — using the existing ClickHouse test setup, call the handler for a ClickHouse connector and assert the table map contains `sorting_key` and that the result contains the guidance note.

**Step 2: Implement**

In the per-table map, after `column_count`:

```go
if t.Engine != "" { table["engine"] = t.Engine }
if t.SortingKey != "" { table["sorting_key"] = t.SortingKey }
if t.PrimaryKey != "" { table["primary_key"] = t.PrimaryKey }
```

Return:

```go
result := map[string]any{"tables": tables, "total_tables": len(tables)}
if connType == models.ConnectorTypeClickHouse {
    result["note"] = "Filters/joins use the index only when they repeat the leftmost ORDER BY (sorting_key) expressions exactly; if the key is an expression, repeat it verbatim."
}
return result, nil
```

**Step 3:** run test, commit: `feat(agent): expose ClickHouse sort keys in explore_schema`

---

### Task 3: System prompt injection

**Files:**
- Modify: `internal/agent/engine.go` (`buildNotebookContext`, ~1652-1730)
- Test: `internal/agent/engine_prompt_test.go` (create) or extend an existing prompt test

**Step 1: Failing test**

Create a notebook whose connector type is `clickhouse`, call `buildNotebookContext`, assert it contains `ClickHouse optimization notes` and `toStartOfHour(timestamp)`; repeat with `postgres` and assert absence.

**Step 2: Implement**

Where the connector type is scanned (`connName, connType`), add:

```go
if connType == string(models.ConnectorTypeClickHouse) {
    result += "\n\nClickHouse optimization notes:" +
        "\n- Each table has an ORDER BY sorting key. Filters and joins are fast only when they use the leftmost sorting-key expressions exactly as defined. If the key is an expression such as toStartOfHour(timestamp), the predicate must repeat it verbatim (WHERE toStartOfHour(timestamp) = ...); filtering on the bare column does not use the index." +
        "\n- Before querying an unfamiliar table, inspect it with SHOW CREATE TABLE <table> — DESCRIBE does not show the sorting key. Alternatively read sorting_key from explore_schema." +
        "\n- Avoid wrapping key columns in functions unless the wrapper matches the key expression."
}
```

Note: this is inside the `connectorID != nil` block; sessions with no notebook are covered by Task 2's fallback.

**Step 3:** pass + commit: `feat(agent): inject ClickHouse optimization guidance into system prompt`

---

### Task 4: Seed `explore_schema` for all agents

**Files:**
- Modify: `internal/agent/tools_seed.go`
- Modify: `internal/agent/tools_notebook.go` (ensure `explore_schema` has `Timeout: 60 * time.Second` from the tool-timeouts plan)
- Test: `internal/agent/tools_seed_test.go`, `internal/api/tool_handlers_test.go`

**Step 1: Failing test**

- `BuiltinTools` contains an entry with `HandlerName: "explore_schema"` and `AutoAssign: true`.
- After `SeedBuiltinTools(ctx, pool, orgID)` in a test org with an existing agent, the agent's `tool_ids` contains the seeded tool id.

**Step 2: Implement**

```go
type BuiltinToolDef struct {
    Name        string
    Description string
    Schema      map[string]any
    HandlerName string
    AutoAssign  bool
}
```

Add to `BuiltinTools`:

```go
{Name: "explore_schema", Description: "Explore database schema (tables, columns, indexes/sort keys). Use before writing queries.", HandlerName: "explore_schema", AutoAssign: true},
```

In `SeedBuiltinTools`, after each successful insert, capture the tool id (`INSERT ... RETURNING id` or select by `(org_id, name)`), and when `bt.AutoAssign`:

```go
pool.Exec(ctx, `UPDATE agents SET tool_ids = array_append(tool_ids, $1::uuid)
    WHERE org_id = $2 AND NOT ($1::uuid = ANY(tool_ids))`, toolID, orgID)
pool.Exec(ctx, `INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
    VALUES ($1, 'tool', $2::uuid, 'org_role', 'everyone', ARRAY['view','use'])
    ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
    DO UPDATE SET actions = ARRAY['view','use']`, orgID, toolID)
```

The startup seeding loop runs for every org and re-runs when `len(BuiltinTools)` grows, so existing installs backfill automatically.

**Step 3:** update count-based tests (`tool_handlers_test.go` uses `len(agent.BuiltinTools)`; add the seed-catalog assertions). Run `go test ./internal/agent/ ./internal/api/ -run 'Seed|Builtin' -v`.

**Step 4: Commit:** `feat(agent): seed explore_schema and auto-assign to all agents`

---

### Task 5: `sql_query` description hint

**Files:**
- Modify: `internal/agent/tools_sql.go` (`makeSQLQueryToolDef`, ~17)
- Test: `internal/agent/tools_sql_test.go`

**Step 1: Failing test** — build a `sql_query` tool bound to a ClickHouse connector and assert the description contains `ORDER BY sorting key`; bound to Postgres, assert absence.

**Step 2: Implement** — query the connector type:

```go
var connType string
if err := pool.QueryRow(context.Background(),
    `SELECT type FROM connectors WHERE id = $1`, connectorID).Scan(&connType); err == nil &&
    connType == string(models.ConnectorTypeClickHouse) {
    description += " ClickHouse: filter on the leftmost ORDER BY (sorting_key) expressions exactly as defined; check SHOW CREATE TABLE or explore_schema."
}
```

(Use the request/engine context rather than `context.Background()` where available; `makeSQLQueryToolDef` has no ctx today, so accept the documented background query or plumb a ctx parameter.)

**Step 3:** pass + commit: `feat(agent): add ClickHouse sorting-key hint to sql_query tools`

---

## Verification

- `task check`
- ClickHouse integration tests (dev stack provides ClickHouse; confirm the test helper targets it)
- Manual: open a notebook with a ClickHouse connector, ask the agent to inspect a table, confirm `SHOW CREATE TABLE` usage and `sorting_key` in `explore_schema` output.
