# Dictionary Read Grants for Managed ClickHouse Warehouses — Design

**Date:** 2026-10-13
**Status:** 🟡 Design approved — implementation pending
**Related:** `2026-09-19-clickhouse-warehouse-permissions-design.md` (identity model, wildcard policy),
`2026-09-28-warehouse-hidden-tables-and-schema-visibility-design.md` (catalog/snapshots)

## Problem

Managed warehouses (connector linked to a warehouse, `AETHER_CH_TABLE_PERMISSIONS=true`) give every
provisioned identity exactly one privilege per granted object: `GRANT SELECT ON db.obj`.
Dictionaries break under that rule.

ClickHouse dictionaries are **table-like everywhere in Aether's pipeline** — they are rows of
`system.tables` (`engine = Dictionary`), their attributes are rows of `system.columns`, so the
catalog snapshot, new-objects inbox, grant picker, schema browser, and schema-visibility filters
already list them and admins can already grant them. Nothing about *discovery* or *storage* is
broken. What is broken is the **privilege**: on modern ClickHouse, `SELECT` alone does not make a
dictionary readable. Users granted a dictionary get:

```
Code: 497. DB::Exception: Not enough privileges.
To execute this query, it's necessary to have the grant dictGet ON db.dict
```

…for `SELECT * FROM db.dict`, `dictGet(...)`, `dictHas(...)`, and every other member of the
`dictGet` function family. `SHOW CREATE DICTIONARY` additionally requires the `SHOW DICTIONARIES`
privilege.

### Empirical findings (verified 2026-10-13 against `clickhouse-server 24.10.2`)

| Probe | Result |
|---|---|
| `GRANT SELECT ON db.dict` → `SELECT * FROM db.dict` | **ACCESS_DENIED**: `dictGet` required |
| `GRANT SELECT ON db.dict` → `dictGet()` / `dictHas()` | **ACCESS_DENIED**: `dictGet` required |
| `GRANT dictGet ON db.dict` → `SELECT *`, `dictGet()`, `dictHas()`, `dictGetOrDefault()` | all work |
| `SHOW CREATE DICTIONARY db.dict` | requires `SHOW DICTIONARIES ON db.dict` |
| `GRANT SELECT, dictGet, SHOW DICTIONARIES ON dbtbl TO x` (plain table) | accepted; extras inert |
| same bundle on a view | accepted; extras inert |
| per-dictionary / database-scoped `SHOW DICTIONARIES` grants | accepted, but listing (`SHOW DICTIONARIES`, `system.dictionaries` rows) filters only on the **global** `SHOW DICTIONARIES ON *.*` grant |
| dictionary grants vs. source tables | independent: `dictGet` works from a dictionary whose source table the user cannot read |
| dictionaries in `system.tables` / `system.columns` (provisioner) | present (`engine = Dictionary`, attribute columns) |

Version context: ClickHouse tightened dictionary ACLs over time (PRs #57362, #65359; strict
`dictGet` enforcement observed from ~23.9 through our 24.10.2), then PR #72051 ("Correct permissions
for dictionaries", backported to 24.3/24.8/24.9/24.10) relaxed **direct** reads to accept `SELECT`
**or** `dictGet` — while `dictGet`-family *function* calls require `dictGet` in every version. A
bundle containing both privileges is correct on old, current, and future servers.

## Goals

- Managed identities can fully read every granted dictionary: direct `SELECT`, every `dictGet`
  family function (`dictGet`, `dictHas`, `dictGetOrDefault`, …), and `SHOW CREATE DICTIONARY`.
- Fix it with **no migration, no API change, no UI change** — dictionaries already flow through
  every catalog path; only the emitted privilege set is wrong.
- Preserve the fail-closed security posture for anything outside the managed read set.

## Non-goals

- Ambient dictionary listing (`SHOW DICTIONARIES ON *.*` / `system.dictionaries` for everyone).
  Rejected: the filter is all-or-nothing in 24.10, so it exposes every dictionary name/schema to
  every managed user. Discovery stays access-filtered: `system.tables` synthesizes rows for exactly
  the dictionaries a user can read, and the Aether UI filters the catalog by grants.
- Table functions (`url`, `s3`, `file`, `remote`, …), named collections, `system.query_log`,
  `system.parts` and other system-activity tables. Table functions in particular bypass
  per-object grants entirely and stay deliberately ungranted.
- Picker/inbox badges marking entries as dictionaries (display-only polish, follow-up).
- Row/column-level policies; per-dictionary grant UI (grants stay object-name based).

## Decisions

| Decision | Choice | Alternatives rejected |
|---|---|---|
| Grant shape | Uniform read bundle `SELECT, dictGet, SHOW DICTIONARIES ON db.obj` for **every** granted object | Store `object_type` per grant + conditional bundle (migration + API/UI plumbing for no security gain — extras are inert on tables/views); classify at reconcile via `system.dictionaries` (live coupling, race-prone, same outcomes) |
| Diff universe | `(privilege, database, object)` triples derived from existing `(db, object)` grants | Separate dictionary-grant table (`warehouse_dictionary_grants`) — duplicates CRUD/validation/UI paths; object-name drift semantics unchanged either way |
| Extra managed privileges in namespace | Revoke (reconcile treats them as managed read privileges) | Fail closed like foreign privileges (blocks the warehouse for read drift Aether fully models) |
| Old servers without the granular privileges | Capability probe per reconcile → legacy mode emits `SELECT` only | Assume modern (would break `GRANT` on pre-granular servers); version-string parsing (fragile with forks/backports) |
| Ambient listing grants | None | Global `SHOW DICTIONARIES` + `SELECT ON system.dictionaries` (exposes all dictionary metadata to all users) |

## Design

### 1. Managed read bundle (`internal/chaccess/ddl.go`)

`Statements` diffs a per-subject set of `(access_type, database, object)` triples. Desired state
expands each logical grant to the bundle below (modern mode); actual state is read the same way
(Section 2). Statements are grouped per object with a fixed privilege order
(`SELECT`, `dictGet`, `SHOW DICTIONARIES`):

```sql
GRANT SELECT, dictGet, SHOW DICTIONARIES ON `db`.`obj` TO `aether_wh8_u_…`
GRANT dictGet, SHOW DICTIONARIES ON `db`.`obj` TO `aether_wh8_u_…`   -- partial repair
REVOKE SELECT, dictGet, SHOW DICTIONARIES ON `db`.`obj` FROM `…`     -- extra object
REVOKE SHOW DICTIONARIES ON `db`.`obj` FROM `…`                      -- extra privilege
```

Legacy mode (Section 3) emits `SELECT` only, exactly today's statements. Statement ordering,
quoting/skipping of unquotable names, password handling, and orphan drops are unchanged.

Why uniform rather than dictionary-conditional: `dictGet`/`SHOW DICTIONARIES` are accepted and
inert on plain tables and views (verified), so one code path covers tables, views,
dictionaries, and `ENGINE = Dictionary` tables with no kind plumbing anywhere in the stack. Grants
are name-scoped by ClickHouse itself; if an object is later recreated as a different kind, the
grant follows the name — identical to the outcome of a live-classification design, minus the
coupling.

### 2. Introspection and drift guard (`internal/chaccess/introspect.go`)

- `LoadActual` loads the managed privilege set as the diff universe instead of `SELECT` only.
  Rows are classified exactly like SELECT rows today:
  - grant option on any managed row → `Unexpected` → fail closed;
  - wildcard shapes (`*.*`, `db.*`, `is_wildcard`, unquotable names) → `Wildcards` → fail closed;
  - partial revokes and column-level rows stay excluded.
- The `SHOW GRANTS` fallback parser (used on servers without `system.grants.is_wildcard` — our
  24.x fleet) currently only recognizes lines containing `SELECT`. It must also recognize
  `dictGet` / `SHOW DICTIONARIES` lines containing `*`, or a manual `GRANT dictGet ON db.*` would
  slip through on exactly the servers in production today.
- `loadNonSelectGrants` continues to fail closed for every non-managed access type (INSERT, ALTER,
  DROP, …). The managed set is the only thing `Unexpected` no longer catches.
- Extras inside the managed set are revoked (consistent with today's extra-SELECT handling), not
  fail-closed: `dictGet`/`SHOW DICTIONARIES` are now Aether-managed read privileges. Manual
  wildcard forms still halt with `sync_status=error` and require manual remediation.

### 3. Version compatibility (probe + legacy mode)

One probe per reconcile, following the existing `hasIsWildcardColumn` pattern (check the column
exists via `system.columns`, then query rows):

1. Does `system.privileges` exist? (absent → legacy)
2. Does it contain both `dictGet` and `SHOW DICTIONARIES`? (missing → legacy)

Modern → bundle mode; legacy → `SELECT`-only mode (on pre-granular servers, `SELECT` was the
dictionary privilege, so dictionary reads keep working there). Probe *errors* (other than
"table/rows missing") abort the reconcile — a transient outage must never silently flip modes and
trigger grant/revoke churn; the mode is deterministic per server.

The mode parameterizes both desired-state computation and the actual-state query: filtering
`system.grants.access_type` by a literal the server's enum does not know can itself error, so
legacy mode must not reference the new types at all.

Affected call sites: the reconcile loop, `dropWarehouseIdentitiesLocked`, and their tests.

### 4. Security notes

- Per-object, deny-by-default grants are unchanged (user / group / Everyone / pending-user
  subjects).
- `dictGet` deliberately bypasses source-table grants — that is ClickHouse semantics: the
  dictionary is its own access unit. Granting a dictionary is an explicit admin action on that
  object; dictionary data derived from restricted sources is therefore only reachable when the
  admin grants the dictionary itself.
- `displaySecretsInShowAndSelect` is never granted; ClickHouse hides source credentials in
  `SHOW CREATE` output and `system.dictionaries.source` regardless.
- The extended managed set is read-only by construction: `SELECT`, `dictGet`, `SHOW DICTIONARIES`.
  Nothing new becomes grantable or executable beyond what a granted object already implies.

## Testing

- `internal/chaccess/ddl_test.go`: golden statements for modern bundle (fresh grant, partial
  repair, full revoke, partial revoke), legacy mode (`SELECT` only), ordering unchanged.
- `internal/chaccess/introspect_test.go`: rows across the three access types; wildcard
  `dictGet`/`SHOW DICTIONARIES` → `Wildcards`; grant option on them → `Unexpected`; non-managed
  types still drift; `SHOW GRANTS` line parser recognizes the new wildcard forms; legacy query
  shape does not reference the new access types.
- Probe tests: present/missing table, missing privilege rows, query error → error.
- Integration (real Postgres + ClickHouse, `warehouse_sync_test.go` /
  `warehouse_rbac_e2e_test.go`): create table + dictionary; grant the dictionary to a user, a
  group, and Everyone; reconcile; connect as the identity and run `SELECT * FROM dict`,
  `dictGet()`, `dictHas()`, `SHOW CREATE DICTIONARY`; revoke → all denied; manually injected extra
  `dictGet` on an un-granted dictionary → revoked by reconcile; manually injected wildcard
  `dictGet ON db.*` → `sync_status=error` fail closed.
- Existing golden DDL expectations update mechanically for the new bundle.

## Rollout

- No migration, no API change, no UI change. The next reconcile (startup, interval, trigger, or
  manual) adds `dictGet` + `SHOW DICTIONARIES` to every existing managed identity's grants. The
  extras are inert on tables/views and make dictionaries readable — the reported failure is fixed
  with zero admin action.
- Servers that predate granular privileges keep today's behavior via legacy mode.
- `AETHER_CH_TABLE_PERMISSIONS` kill switch semantics are unchanged.
- `AGENTS.md` ("ClickHouse table permissions" section) documents the grant bundle.

## Follow-ups

- Display-only dictionary badges in the grant picker / new-objects inbox (schema response gains
  `engine`, UI renders a chip).
- Revisit ambient dictionary listing if users want warehouse-wide dictionary discovery
  (metadata exposure trade-off documented above).
- New `dictGet`-family functions (`dictGetKeys`, `dictGetRoot`, …) ride the same `dictGet`
  privilege; no changes needed as ClickHouse adds them.
