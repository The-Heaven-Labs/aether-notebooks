# ClickHouse Agent Guidance & Schema Enrichment — Design

**Date:** 2026-09-11
**Status:** Approved

## Problem

ClickHouse query performance is governed by each table’s `ORDER BY` sorting key. Agents
currently cannot see it:

- `ClickHouseExecutor.Schema()` reads `system.columns` + table comments only — no
  `sorting_key`, `primary_key`, or `engine`.
- `DESCRIBE TABLE` (which agents reach for) does not show the sorting key either.
- Agents therefore write queries that filter on a column inside a key expression
  (e.g. `WHERE timestamp >= …` when the key is `toStartOfHour(timestamp)`) and silently
  scan full tables.

The guidance must also capture the exact-match rule: an ORDER BY can hold expressions, and a
predicate only uses the index when it repeats that expression **exactly**.

## Guidance content

Injected only when the relevant connector is ClickHouse:

> **ClickHouse optimization notes**
> - Each table has an `ORDER BY` sorting key. Filters and joins are fast only when they use
>   the leftmost sorting-key expressions **exactly as defined**. If the key is an expression
>   such as `toStartOfHour(timestamp)`, the predicate must repeat it verbatim
>   (`WHERE toStartOfHour(timestamp) = …`); filtering on the bare column does **not** use the
>   index.
> - Before querying an unfamiliar table, inspect it with `SHOW CREATE TABLE <table>` —
>   `DESCRIBE` does not show the sorting key. Alternatively read `sorting_key` from
>   `explore_schema`.
> - Avoid wrapping key columns in functions unless the wrapper matches the key expression.

`SHOW CREATE TABLE` is already allowed by the read-only guard (`SHOW` prefix) used by
`execute_sql` / `sql_query`.

## Delivery layers

### 1. System prompt (primary)

`buildNotebookContext` (`internal/agent/engine.go`) already fetches the notebook connector’s
`name, type`. When `type == "clickhouse"`, append the guidance block to the context string.
This reaches every SQL-generating action of the session — `run_cell`, `create_cell(run=true)`,
`execute_sql`, charts — without mutating any agent configuration.

### 2. `explore_schema` enrichment (fallback)

Covers global sessions (no notebook connector) and ad-hoc connectors:

- Extend `executor.TableInfo` with `Engine`, `SortingKey`, `PrimaryKey` (optional, populated
  only where meaningful).
- In `ClickHouseExecutor.Schema()` fetch `engine`, `sorting_key`, `primary_key`, `comment`
  from `system.tables` in a **single** query, which also fixes the current N+1 per-table
  comment lookup.
- `makeExploreSchemaHandler` emits `engine` / `sorting_key` / `primary_key` when non-empty
  and adds a ClickHouse `note` (the guidance in one line) to the result.

### 3. `sql_query` custom tools

`makeSQLQueryToolDef` is built per tool with a fixed `connector_id`. Look up the connector
type at build time and append the one-line ClickHouse hint to the tool description, so
user-defined saved queries carry the rule with them.

### 4. Make `explore_schema` available to all (D- resolved)

`explore_schema` is registered in the Go registry but missing from `BuiltinTools`, so regular
agents (which run strictly from `agent.tool_ids`) cannot call it. Fix:

- Add `explore_schema` to `BuiltinTools` with its 60s timeout (tool-timeouts design).
- Add an `AutoAssign` flag to `BuiltinToolDef`; when seeding an auto-assigned tool, append
  its ID to every agent’s `tool_ids` in the org where missing (idempotent). The seeding loop
  runs for all orgs at engine startup and re-runs when `len(BuiltinTools)` grows, so existing
  installs are backfilled on upgrade. Scope the auto-assign to this tool (not all builtins).
- Grant `use` to `org_role: everyone` for this tool (alongside the existing `view`), so users
  can re-add it from the agent editor after removal. This is a deliberate, documented
  exception to the V094 “no everyone-use on builtins” policy: the tool is read-only
  introspection.
- The agent editor’s existing “select all builtins” helper and tool picker will then include
  it for new agents.

## Tests

- System prompt: ClickHouse connector adds the block; Postgres connector does not; session
  without a notebook does not attempt it.
- ClickHouse schema: `sorting_key`/`primary_key`/`engine` populated from `system.tables`
  (integration test against the dev ClickHouse), comments still returned, single query used.
- `explore_schema`: result contains the new fields and the ClickHouse note.
- Seeding: `BuiltinTools` includes `explore_schema`; `SeedBuiltinTools` backfills existing
  agents’ `tool_ids`; count-based tests
  (`tool_handlers_test.go`, `tools_seed_test.go`) updated.
- `sql_query` description contains the hint when bound to a ClickHouse connector.

## Notes / non-goals

- No SQL linting or runtime enforcement in this change; the guidance is contextual.
- Expression matching is described, not verified — we do not parse queries to check that
  predicates match the sorting key.
