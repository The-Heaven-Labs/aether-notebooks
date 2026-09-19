# ClickHouse Warehouse Table Permissions — Design

**Date:** 2026-09-19
**Status:** Approved

## Problem

Aether connectors expose ClickHouse through a single shared credential. Connector ACLs only
gate `view`/`use` at the connector level, `table_allowlist`/`table_denylist` filter the schema
listing but are not enforced at execution (`connector_handlers.go:708-746`), and execution
loads only `type, config_encrypted, max_rows, timeout_seconds` (`execute_handlers.go:168-172`).
Any user with `use` on a connector can run arbitrary SQL as the shared user. This is not
acceptable for an adversarial threat model where users could craft subqueries, table functions,
or system-table reads to reach data they were not granted.

At the same time, ClickHouse Cloud deployments organize compute into **warehouses** — groups of
**services** that share one data catalog and one access-control namespace (users, passwords,
roles, grants). Users need table-level data access plus explicit service routing for
compute/QoS/cost attribution, while collaborating on the same notebooks.

## Goals

- Table-level access control enforced by ClickHouse itself, not by Aether SQL parsing.
- Per-person ClickHouse identity for audit, quotas, and credential blast-radius containment.
- Explicit service access per subject (no defaults, no implicit "all endpoints").
- Routing that preserves workload isolation and per-service cost attribution.
- Shared notebooks that work across collaborators with different service access.
- Group-first administration: one grant edit updates all members.

## Non-goals

- Row-level policies and column masking (table-level only for now).
- Direct ClickHouse access for users; Aether remains the only client.
- Non-ClickHouse executors (Postgres/OpenSearch) keep their current behavior.
- ClickHouse Cloud control-plane API integration.

## Decisions

| Decision | Choice | Alternatives rejected |
|---|---|---|
| Grant unit | Group roles + per-person CH users | Per-user materialized roles (O(members) DDL per policy change); curated views (view-level, sprawl) |
| Execution identity | Per-person CH user | Shared runner + `SET ROLE` (proxy credential blast radius, pooled role-state leaks) |
| Notebook target | Warehouse (logical), routing preference per user | Per-run selector (friction); silent remap (confusing); admin-only assignment (rigid) |
| Connector type | Keep `clickhouse`; warehouse is a first-class entity | New `clickhouse_cloud` type (duplicate driver paths, hostname not a reliable signal) |
| Wildcards | No CH wildcard grants; explicit per-table rows | `GRANT SELECT ON db.*` (silently covers future tables, messy partial revokes) |

## Model

Three distinct concepts:

- **Warehouse** — logical target. Owns table grants, ClickHouse identities/roles, the
  provisioner connector, and one ClickHouse access namespace. A standalone service is a
  warehouse of one.
- **Service (connector)** — physical endpoint. Owns host/TLS, service type (RO/RW), and
  *service access* (existing connector `use` ACL). Explicit per subject; no defaults.
- **Routing preference** — user-level, per warehouse: which of the user's explicitly granted
  services their queries run on. Set once, changeable. Optional per-notebook/cell **pin** for
  workloads that must run on a specific service; then only subjects with `use` on that service
  can run it.

Because all services in a warehouse expose identical data and grants, choosing among services
the user was explicitly granted does not change results — only compute placement and cost.

## Aether data model

- `warehouses (id, org_id, name, provisioner_connector_id)` — new table.
- `connectors.warehouse_id` — nullable FK; unset means implicit warehouse-of-one.
- `warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name,
  table_name)` — allow-only; subject types mirror `acl_entries` (user/group/Everyone). Rows,
  not arrays, so sync can diff.
- Service access: reuse `acl_entries` on connectors (`use` action). No new mechanism.
- `warehouse_service_preferences (user_id, warehouse_id, connector_id)` — resolution order:
  explicit user preference → if exactly one granted service, that one → otherwise fail with a
  "choose a service" prompt that stores the choice. Admin-set group defaults deferred.
- Pre-account users: groups remain the only staging path (`pending_group_members` V105 +
  `ApplyPendingGroups`). No per-email direct table grants in v1.
- New-table convenience: a "new tables since last review" inbox per warehouse, derived from the
  cached schema metadata. Optional auto-grant policy (v2) materializes explicit per-table
  grants, never wildcards.

## ClickHouse objects (per warehouse)

- One CH user per Aether user: `aether_<org>_<user>` derived from UUID prefixes, never email.
- Password = HKDF(masterKey, warehouse, user); `sha256_password`, `GRANTEES NONE`, settings
  profile attached; never `NOT IDENTIFIED`.
- One role per Aether group: `aether_<org>_<group>` with that group's table grants; an
  `Everyone` role; direct user grants attach to the user object.
- Default roles = all of the user's group roles (`SET DEFAULT ROLE ALL`). ClickHouse roles are
  additive, matching Aether's allow-only ACL union semantics (`acl_entries` has no deny column).
- Provisioning condition: at least one effective table grant. Service access gates execution
  only, so half-configured subjects are possible and surfaced in the UI.

## Provisioning & sync

- One designated RW, non-idling **provisioner connector** per warehouse; all DDL and
  reconciliation runs through it. Other connectors' credentials are not used for provisioning.
- Desired state is computed from `warehouse_table_grants` + group memberships; a single worker
  per warehouse processes it, debounced 1–3s, batching multi-name DDL, idempotent
  (`CREATE USER IF NOT EXISTS`, `OR REPLACE` roles).
- Reconciliation loop compares `system.users`, `system.roles`, `system.grants`,
  `system.role_grants` against desired state; alerts on drift and unknown entities; auto-heals
  extra grants with explicit `REVOKE`s.
- Triggers: account materialization (post-`ApplyPendingGroups`), group mapping edits, membership
  changes (from API, SSO sync, pending materialization), offboarding.
- Revocation blocks in Aether immediately; ClickHouse converges asynchronously. Connector
  marked stale → fail closed.
- Mis-grouped connectors (grouped but not sharing an access namespace) surface as reconcile
  mismatch errors rather than silent misbehavior.

## Execution & routing

1. Resolve target warehouse from the cell's connector (or the pinned connector).
2. Authorize service access: at least one `use` grant in the warehouse; pinned targets require
   `use` on that exact service.
3. Resolve the user's ClickHouse identity and preferred service; connect to that endpoint as
   that user.
4. Execute; record actual endpoint + warehouse + execution ID in the Aether audit row and
   `log_comment='aether:<execution_id>'` for `system.query_log` joins.

- Per-(endpoint, user) connection pools with a global per-connector cap and LRU eviction of
  idle pools. Identity cache invalidated on membership, role, and credential-rotation events.
- Every execution path resolves identity: HTTP, agent (`run_cell`, `execute_sql`,
  `sql_query`), MCP via PAT owner, scheduler. Fail closed when identity is missing — never fall
  back to connector or provisioner credentials.
- Public sharing remains read-only and never executes.

## Security controls

- Provisioner: dedicated admin user (not `default`), PrivateLink and/or IP allowlist, used only
  for DDL; alert if it runs `SELECT`; rotate on schedule.
- Secrets: HKDF-derived per-user passwords, nothing per-user at rest; TLS `verify-full`;
  optional KMS envelope encryption for the master key.
- Privileges: table-by-table grants only (no wildcards); table functions (`URL`, `REMOTE`,
  `S3`, `FILE`, …) and DDL/DML privileges remain ungranted; no `WITH GRANT OPTION`;
  `GRANTEES NONE`.
- Query limits: settings profile per role — `readonly`, constrained `max_execution_time`,
  `max_memory_usage`, `max_result_rows`, `max_rows_to_read`/`max_bytes_to_read`,
  `max_concurrent_queries_for_user`, `result_overflow_mode=throw`; DDL quotas per group for
  read bytes / execution time / queries per interval.
- Identifiers: whitelist `[a-z0-9_]{1,64}` derived from UUIDs, never org-provided names.
- Lifecycle: `DROP USER` (or credential reset) on offboarding, orphan sweep, optional
  `VALID UNTIL` expiry; restore runbook re-provisions the warehouse.
- Audit: every execution records user, warehouse, endpoint, duration, read bytes; alerts for
  failed authentications, sync-created grants, provisioner SELECTs, abnormal read volume.
- Residual risk, stated plainly: Aether runtime compromise equals all-access because it must
  derive credentials to execute; routing is app-layer, not ClickHouse-enforced.

## Performance constraints

- Replace today's connect+ping-per-execution (`clickhouse.go:44-53`) with pooled per-user
  connections keyed by `(endpoint, user)`.
- Global connection cap per connector; small per-user pools (1–2); LRU eviction; bounded
  queue when capped.
- Raw schema metadata cached per connector and filtered per user's grants in Aether; never
  cache per-user or per-user scan `system.columns`.
- Sync batched, debounced, single-flight per warehouse, off the request path.
- `max_concurrent_queries_for_user` + quotas so no single user monopolizes a service.

## Admin & user UX

- Warehouse page: table access matrix (subjects × tables), service access, provisioner
  designation, new-tables inbox.
- User routing preference per warehouse; execution results show "ran on <service>".
- Validation warnings: tables without service access (granted but unusable) and service access
  without tables (can connect, every query fails).
- Pending emails shown on group rows.
- Org admin (Aether ACL bypass) remains separate from ClickHouse data grants; admin group
  management is manual, made low-effort by the new-tables inbox.

## Rollout

1. Schema: `warehouses`, `connectors.warehouse_id`, `warehouse_table_grants`,
   `warehouse_service_preferences`.
2. Warehouse grouping + UI, grants CRUD, validation.
3. Provisioning/sync worker behind a feature flag; existing shared-credential execution
   unchanged until sync and reconciliation are verified.
4. Per-user execution + routing, then disable shared-credential execution path.
5. Reconcile every existing connector: implicit warehouse-of-one, backfill grants from current
   `table_allowlist` where applicable.

## Open items to verify

- Whether warehouse membership can be auto-detected (e.g. `system.clusters`,
  `all_groups.default`) or must stay admin-declared.
- Whether a service's RO/RW type is discoverable from SQL; otherwise admin-declared.
- `SET DEFAULT ROLE` behavior after the underlying role is revoked.
- `log_comment` under `readonly`, and driver-set `max_execution_time` vs `readonly=1`
  (`changeable_in_readonly`).
- Propagation delay of access entities across services in a warehouse (retry on
  `ACCESS_DENIED` after fresh grants).

## Out of scope

- Row policies, column masking, and row-level filters.
- Client-side OIDC/JWT gateway (e.g. Trino) for user-held tokens.
- ClickHouse Cloud control-plane automation (service discovery, IP allowlist management).
