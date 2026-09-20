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
  provisioner connector, and one ClickHouse access namespace. A connector that is not
  linked to a warehouse is **unmanaged** (see Connector access modes).
- **Service (connector)** — physical endpoint. Owns host/TLS, service type (RO/RW), and
  *service access* (existing connector `use` ACL). Explicit per subject; no defaults.
- **Routing preference** — user-level, per warehouse: which of the user's explicitly granted
  services their queries run on. Set once, changeable. Optional per-notebook/cell **pin** for
  workloads that must run on a specific service; then only subjects with `use` on that service
  can run it.

## Connector access modes

Warehouse membership is the per-connector mode selector:

- **Managed (connector linked to a warehouse).** The sync worker provisions per-user
  ClickHouse identities and group roles; table grants are enforced by ClickHouse. The
  connector's stored credential is used only when it is the warehouse's provisioner (DDL and
  reconciliation), never for user queries.
- **Unmanaged (no warehouse).** The sync worker never touches the connector; no ClickHouse
  users, roles, or grants are created. Execution uses the connector's stored credential
  exactly as before this feature. The connector `use` ACL remains the only gate, so unmanaged
  connectors sit **outside** the ClickHouse-enforced table boundary.

An unmanaged connector may point at the same physical service as a managed warehouse; they are
independent Aether connectors. There is no "managed without provisioning" mode: managed implies
Aether owns identities for that warehouse. If unrestricted access is wanted, grant `everyone`
all tables explicitly.

**Global kill switch:** `AETHER_CH_TABLE_PERMISSIONS=false` makes every connector behave as
unmanaged regardless of warehouse configuration, for rollback without schema or config changes.

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

- One CH user per Aether user per warehouse: `aether_<wh8>_u_<hash(warehouse, org, user)>`
  derived from UUIDs, never email. The `wh8` prefix (first 8 hex of the warehouse UUID) scopes
  reconciliation so two warehouses sharing a service cannot drop each other's entities.
- Password = HKDF(masterKey, warehouse, user); `sha256_password`, `GRANTEES NONE`; never
  `NOT IDENTIFIED`.
- One role per group per warehouse: `aether_<wh8>_g_<hash(warehouse, org, group)>` with that
  group's table grants; an `aether_<wh8>_everyone` role; direct user grants attach to the user
  object.
- Default roles = all of the user's group roles (`SET DEFAULT ROLE ALL`). ClickHouse roles are
  additive, matching Aether's allow-only ACL union semantics (`acl_entries` has no deny column).
- Provisioning condition: at least one effective table grant. Service access gates execution
  only, so half-configured subjects are possible and surfaced in the UI.

## Provisioning & sync

- One designated RW, non-idling **provisioner connector** per warehouse; all DDL and
  reconciliation runs through it. Other connectors' credentials are not used for provisioning.
- Unmanaged connectors (no warehouse) are skipped entirely by the sync worker.
- `warehouses.applied_master_fp` records the master-key fingerprint last applied. When it
  differs, the next reconcile re-keys every provisioned user (`ALTER USER ... IDENTIFIED`) and
  only then stores the new fingerprint. ClickHouse stores salted password hashes, so hashes are
  never compared; rotation self-heals on the next sync.
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

1. If the kill switch is off, or the connector is unmanaged (no warehouse), execute through
   the legacy shared-credential path and stop.
2. Resolve the target warehouse from the cell's connector (or the pinned connector).
3. Authorize service access: at least one `use` grant in the warehouse; pinned targets require
   `use` on that exact service.
4. Resolve the user's ClickHouse identity and preferred service; connect to that endpoint as
   that user. Managed execution requires `sync_status='ready'`; otherwise fail closed with a
   provisioning error.
5. Execute; record actual endpoint + warehouse + execution ID in the Aether audit row and
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
- Wildcard drift fails closed: if reconciliation finds a wildcard grant (`system.grants`
  `is_wildcard=1`) in the warehouse namespace, `sync_status` is set to `error` with the
  offending subject/scope and managed execution stays blocked until an operator revokes it.
  Wildcards are not auto-healed in this iteration.
- Query limits via settings profiles and quotas are deferred to a follow-up change; until then,
  resource protection relies on Aether-side limits and ClickHouse defaults.
- Identifiers: generated user/role names are strict `[a-z0-9_]{1,64}` derived from UUIDs.
  Database/table names come from the ClickHouse catalog and use a conservative object-name
  charset (`[A-Za-z0-9_$][A-Za-z0-9_$.-]{0,126}`) with backtick quoting; backticks,
  backslashes, whitespace, and control characters are rejected.
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
5. Existing connectors stay unmanaged; admins opt into managed mode by creating a warehouse
   and linking connectors. No automatic backfill.

## Open items to verify

- Whether warehouse membership can be auto-detected (e.g. `system.clusters`,
  `all_groups.default`) or must stay admin-declared.
- Whether a service's RO/RW type is discoverable from SQL; otherwise admin-declared.
- `SET DEFAULT ROLE` behavior after the underlying role is revoked.
- `log_comment` under `readonly`, and driver-set `max_execution_time` vs `readonly=1`
  (`changeable_in_readonly`).
- Propagation delay of access entities across services in a warehouse (retry on
  `ACCESS_DENIED` after fresh grants).
- Org deletion currently leaves provisioned ClickHouse identities behind: `warehouses` cascade
  from `orgs`, so the sync worker can no longer resolve them; a follow-up must decide on an
  explicit cleanup path (e.g. pre-delete reconcile or an orphan sweep for deleted-org prefixes).

## Out of scope

- Row policies, column masking, and row-level filters.
- Client-side OIDC/JWT gateway (e.g. Trino) for user-held tokens.
- ClickHouse Cloud control-plane automation (service discovery, IP allowlist management).
- Settings profiles and quotas per role/user (readonly, execution-time, memory, concurrency).
  Follow-up change; see Security controls for the interim posture.
