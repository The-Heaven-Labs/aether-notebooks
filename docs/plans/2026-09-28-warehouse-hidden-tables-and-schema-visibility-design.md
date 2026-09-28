# Warehouse Hidden-Table Patterns & Per-User Schema Visibility — Design

**Date:** 2026-09-28
**Status:** 📝 Proposed

## Problem

Two related sources of noise and dishonesty in the warehouse experience:

1. **Garbage tables in the grant picker.** `WarehouseTableGrants` lists every table returned by
   the live schema endpoint. ClickHouse deployments accumulate helper/temp/internal objects
   (`_tmp_*`, migration bookkeeping, `.inner` artifacts) that will never be granted. The same
   garbage flows into the **new-tables inbox**, whose snapshot is written from the unfiltered
   reconcile catalog (`loadWarehouseCatalog` → `recordSchemaSnapshot`,
   `internal/api/warehouse_sync.go:129`). The only existing filters are per-connector regex
   `table_allowlist`/`table_denylist` (`connector_handlers.go:855-893`) with no UI, no
   validation, and no warehouse scope.

2. **The schema browser lies.** `/connectors/{id}/schema` always reads with the connector's
   stored credential, so every user sees every table — including tables they cannot `SELECT`
   once execution routes through their per-user ClickHouse identity. Users discover their real
   access only by running a query and getting denied. The agent's `explore_schema` tool already
   runs as the user's warehouse identity (`internal/agent/tools_notebook.go:1339-1347`), so the
   browser is the odd one out.

## Goals

- A warehouse-level list of exclusion patterns hides matching tables from the grant picker, the
  schema browser, and the new-tables inbox.
- Never hide existing access: granted tables stay visible so admins can always see and revoke.
- Patterns are validated at write time (no silent no-ops) and are curation only — they change no
  ClickHouse state and no grant rows.
- Non-admin users browsing a managed connector's schema see only tables their effective grants
  (direct + groups + Everyone) cover — the same union execution enforces.
- Org admins keep the full view (minus pattern-hidden ungranted tables), so administration is
  never obstructed.

## Non-goals

- Rejecting grant creation for pattern-matched tables via API. Patterns are visibility, not
  authorization; the UI never offers hidden tables. (Deliberate: avoids coupling a cosmetic
  setting to the grant API and the reconcile desired state.)
- Applying patterns to the agent's `explore_schema` tool. It already executes as the user's
  ClickHouse identity, and ClickHouse metadata is its source of truth.
- Row-level policies, column masking, or per-service pattern scopes.
- Changing connector-level `table_allowlist`/`table_denylist` behavior or giving them a UI.
- Auto-revoking grants for newly matched tables (explicitly rejected as silent access removal).
- Per-user filtering when the kill switch is off (execution uses the stored credential, so
  filtering the browser would misrepresent real access).

## Decisions

| Decision | Choice | Alternatives rejected |
|---|---|---|
| Pattern scope | Per warehouse, `TEXT[]` column | Per connector reuse of `table_denylist` (N configs, unfiltered snapshots, already affects notebook browser independently); org-level settings JSONB (no validation, wrong grain) |
| Matching | Unanchored Go RE2 against `database.table` | Anchored full match (surprising); Postgres `~` at read time (two regex dialects); globs (less expressive, new convention) |
| Validation | Compile on write, `400` naming the bad pattern; ≤100 patterns, ≤200 chars each | Silent skip (existing connector behavior — hides typos); accept-only-in-UI validation |
| Matched tables with grants | Stay visible (protected set) | Hide all + block new grants (invisible access); hide + auto-revoke (silent access removal) |
| Per-user filter placement | Inside `handleConnectorSchema`, gated by kill switch + warehouse link + non-admin | Opt-in query param (two paths, easy bypass); client-side (leaks full catalog, N+1) |
| Effective-grant resolution | Extract the existing everyone ∪ direct ∪ groups SQL from `handleWarehouseEffectiveAccess` | Duplicate SQL (drift between "what the page says" and "what you can query") |
| Snapshot scope | Pattern-filtered, never per-user filtered | Per-user snapshots (a viewer's subset would corrupt a warehouse-wide cache) |
| Agent `explore_schema` | Unchanged | Applying patterns there (diverges from ClickHouse truth) |

## Semantics

**Matching.** Each pattern is a Go RE2 regex matched unanchored against `"<database>.<table>"`,
identical to the connector filter semantics. `_tmp` matches any object containing `_tmp`;
`^db\.` anchors to a database when needed. Invalid patterns are impossible after write
validation; if a stored pattern somehow fails to compile during a read, log and skip it
(fail open — patterns are curation, not security).

**Visibility rule.** A table matched by any pattern is hidden **unless it already has a grant**
in this warehouse:

- Admin viewers: the protected set is *all* grant keys of the warehouse; matched-but-granted
  tables remain visible for revoke workflows.
- Non-admin viewers: they only ever see their effective grants (below), so every table they see
  is protected by construction — patterns cannot hide access a user has.
- A pattern removal immediately reveals matching ungranted tables again. No reconcile is
  enqueued and no ClickHouse DDL is affected; `warehouse_table_grants` rows are independent of
  patterns. Patterns are never part of the reconcile desired state.

**Per-user visibility.** When `AETHER_CH_TABLE_PERMISSIONS` is on and the connector is linked to
a warehouse, non-admin callers see only `(database, table)` pairs in their effective set:
`everyone` grants ∪ direct `user` grants ∪ grants of groups they belong to (org-scoped, exactly
`handleWarehouseEffectiveAccess:637-650`). Zero effective grants → `200` with `tables: []`.
Org admins bypass this filter. The provisioner path is already admin-only and unaffected.

When the kill switch is off, per-user filtering is skipped (execution falls back to the stored
credential and can query everything; the browser must not understate access). Hidden patterns
still apply — they are UI curation and independent of enforcement mode.

## Data model

New migration `V120__warehouse_hidden_table_patterns.sql`:

```sql
ALTER TABLE warehouses ADD COLUMN hidden_table_patterns TEXT[] NOT NULL DEFAULT '{}';
```

No other schema changes. Existing rows get an empty list.

## Backend

### Warehouse API

- `warehouseJSON` (`internal/api/warehouse_handlers.go:21`), `warehouseSelectColumns` (:43), and
  `scanWarehouseRow` (:85) gain `HiddenTablePatterns []string` (`json:"hidden_table_patterns"`)
  — included in list/get/create/update responses.
- `updateWarehouseRequest` (:418) adds `HiddenTablePatterns json.RawMessage`: absent = unchanged,
  `null`/`[]` = clear, non-empty array = validated (each pattern compiled, limits enforced).
  Mirrors the existing `name` RawMessage pattern.
- Audit reuses the existing `warehouse.update` event (:585) with old/new patterns in metadata.
- Pattern edits do **not** enqueue a reconcile.

### `handleConnectorSchema` (`internal/api/connector_handlers.go:800`)

`loadConnectorWithFilters` (:600) additionally selects `warehouse_id`. Filter order:

1. Live schema from executor.
2. `?database=` filter (existing).
3. Connector allow/deny filters (existing).
4. Hidden patterns: load compiled patterns for the connector's warehouse; drop matched tables
   not in the protected set (Section: Semantics).
5. Per-user effective grants, when enabled (kill switch on, warehouse linked, role ≠ admin):
   keep only granted `(database, table)` pairs.

Snapshot write (`touchSchemaSnapshot`, :900) receives the catalog after steps 1–4 only — never
after step 5, since snapshots are warehouse-wide, not per-viewer.

### Reconcile snapshot (`internal/api/warehouse_sync.go:129`)

Apply the warehouse's patterns to `LoadCatalogTables` output before `recordSchemaSnapshot`, so
new garbage never enters `schema_snapshots`. Pattern edits do not trigger reconcile, so
pre-existing rows are handled by the inbox (below).

### New-tables inbox (`internal/api/warehouse_handlers.go:1252`)

After the existing query (which already excludes granted tables), load the warehouse's patterns
and drop matching rows in Go before responding. This covers legacy snapshot rows regardless of
when they were written. The 200-row cap is applied after filtering; returning slightly fewer
than 200 is acceptable.

### Shared helpers (new `internal/api/schema_visibility.go`)

- `compileHiddenPatterns([]string) ([]*regexp.Regexp, error)` / `matchesHidden(patterns, db, table)`.
- `loadWarehouseGrantKeys(ctx, warehouseID, orgID)` — all grant keys (admin protected set).
- `loadEffectiveWarehouseGrantKeys(ctx, warehouseID, orgID, userID)` — extracted from
  `handleWarehouseEffectiveAccess`, which then reuses it (pure refactor; existing tests guard).
- `filterSchemaTables(tables []executor.TableInfo, patterns []*regexp.Regexp,
  allowed, protected map[[2]string]struct{})` — one implementation used by the schema endpoint;
  the inbox reuses the pattern matcher.

## Frontend

- New `WarehouseHiddenTables.tsx` rendered inside the expanded `WarehouseCard`
  (`WarehouseSettingsPage.tsx:609`, beside `WarehouseTableGrants`): chip-style pattern list with
  add/remove, saving immediately via `updateWarehouse`, inline server validation errors. Helper
  text: "Go regex matched against `database.table`; matching tables are hidden unless already
  granted." A live "hides N tables in this schema source" count uses the already-cached connector
  schema (no extra fetch) and is skipped when the schema is not cached.
- On pattern save, invalidate `connector-schema` queries for the linked connectors and
  `warehouse-new-tables` so the picker and inbox refresh immediately.
- `WarehouseTableGrants`: show a small "N tables hidden by patterns" hint when patterns exist,
  so admins are not confused by missing tables.
- `NewTablesInbox` and `SchemaBrowser`: no UI change (server-side filtering; existing empty
  states apply).

## Error handling

| Failure | Behavior |
|---|---|
| Invalid pattern on update | `400` naming the pattern and regex error; no partial save |
| Too many / too long patterns | `400` with the limit |
| Stored pattern fails to compile during read | Log, skip pattern (fail open) |
| Effective-grant resolution fails for non-admin | `500` "failed to resolve table access" (fail closed; never full catalog) |
| Snapshot write failure | Non-fatal, as today (log and continue) |
| Warehouse load failure on schema read | Fail open with warning (patterns are curation) |

## Testing

**Go integration tests** (real DB via `setupTestServer`, per repo convention):

- Migration: default `{}` on existing/new warehouses.
- `handleUpdateWarehouse`: absent unchanged; `null`/`[]` clears; valid patterns persist; invalid
  regex `400`; pattern-count and length limits; audit metadata.
- `handleConnectorSchema` as admin: pattern-hidden ungranted tables dropped; matched-but-granted
  tables kept; connector allow/deny order unchanged; response snapshot excludes hidden tables.
- `handleConnectorSchema` as non-admin: only direct ∪ group ∪ Everyone effective tables; zero
  grants → empty; kill switch off → full schema minus patterns; admin bypass.
- Cross-org isolation: another org's warehouse patterns/grants never apply.
- `handleWarehouseEffectiveAccess` unchanged after the extract-refactor.
- Inbox: pre-existing snapshot garbage row filtered at read; pattern removal re-reveals.
- Reconcile snapshot write excludes pattern-matched tables.

**Frontend component tests** (Vitest, co-located): pattern editor add/remove/validation display;
hidden-count hint.

**Real-browser validation (mandatory, all flows)** via agent-browser on the dev stack:

- Admin: create warehouse, add/remove patterns, confirm picker and inbox update, confirm
  invalid pattern shows inline error, confirm granted-but-hidden table remains listed, confirm
  hidden-count hint.
- Non-admin: schema browser shows only granted tables (direct, group, and Everyone variants);
  zero grants shows empty state; kill switch off restores full view; patterns still hide garbage.
- `swag init` regeneration for changed endpoint annotations.

## References

- `docs/plans/2026-09-19-clickhouse-warehouse-permissions-design.md` — warehouse model, grant
  semantics, kill switch.
- `docs/plans/2026-09-26-provisioner-execution-block-design.md` — provisioner introspection
  restrictions reused by the per-user gate.
- `internal/api/connector_handlers.go:800` — schema endpoint and existing filter pipeline.
- `internal/api/warehouse_grant_handlers.go:586` — effective-access resolution SQL.
- `internal/api/warehouse_handlers.go:1163` — snapshot writers.
- `internal/api/warehouse_sync.go:129` — reconcile catalog snapshot.
