# Provisioner Connector Execution Block — Design

**Date:** 2026-09-26
**Status:** 📝 Proposed

## Problem

A warehouse's **provisioner connector** holds a privileged ClickHouse credential that the
reconcile worker uses to run user/role/DDL statements (`openWarehouseProvisionerConn`,
`internal/api/warehouse_sync.go`). That credential must reach the worker and nothing else.

Today the provisioner is just another connector linked to the warehouse (`connectors.warehouse_id`),
so it is a valid target on every user-facing path:

- **Managed routing** (`resolveExecutionTarget`, `internal/api/execution_target.go:51`) lists all
  live ClickHouse connectors of the warehouse as selectable **services**; a user with `use` on the
  provisioner can route notebook queries to it.
- **Kill switch off** (`AETHER_CH_TABLE_PERMISSIONS=false`): `resolveExecutionTarget` returns
  `ErrUnmanagedConnector` *before* warehouse logic, and every execution falls back to the stored
  credential — including the provisioner's privileged credential.
- **Introspection endpoints** `/connectors/{id}/test|schema|databases`
  (`internal/api/connector_handlers.go`) always use the stored credential, gated only by `use`,
  which non-admins can hold.

Managed routing replaces the credential with the per-user identity, so the direct leak is the
kill-switch-off fallback and the introspection endpoints; the routing hole is semantic (users
should never execute *through* the provisioner) but also a real path when the kill switch is off.

## Goals

- No user-facing path (HTTP cell execution, agent tools, MCP, connector introspection endpoints)
  ever executes against a provisioner connector's stored credential.
- The reconcile/drift/drop worker keeps working unchanged; it is the only consumer of the
  provisioner credential.
- A warehouse-level admin override can explicitly allow the provisioner to be used as a normal
  **managed** service (per-user identity), for operators who want it selectable.
- UIs do not offer a blocked provisioner as an execution target.

## Non-goals

- A generic per-connector "queries disabled" flag for arbitrary connectors.
- Auto-clearing `is_default` when a connector becomes a provisioner; execution fails with a clear
  403 instead.
- Changing how `/connectors/{id}/schema` etc. resolve managed identities for non-provisioner
  connectors (pre-existing behavior).
- Scheduler changes (scheduler execution is not wired yet; when it is, it resolves through
  `resolveExecutionTarget` and inherits the block).

## Decisions

| Decision | Choice | Alternatives rejected |
|---|---|---|
| What makes a connector a provisioner | `warehouses.provisioner_connector_id` (existing, derived) | New `connectors.is_provisioner` mirror column (drifts from the warehouse link) |
| Admin override | `warehouses.allow_provisioner_execution BOOLEAN NOT NULL DEFAULT false` | Connector-level generic flag (unrelated state, drifts); global config (can't scope per warehouse) |
| Override scope | Allows the provisioner as a normal managed service (per-user CH identity) only | Also unblocking stored-credential/introspection paths (leaks the privileged credential) |
| Kill switch off | Still blocked; override does not apply | Falling through to `ErrUnmanagedConnector` (uses the provisioner credential) |
| Enforcement point | Fail-closed sentinel `executor.ErrProvisionerNotExecutable` raised in `resolveExecutionTarget` + explicit guards in introspection handlers | SQL-level filtering only (agent/MCP paths bypass it) |

## Semantics

A connector is a provisioner **iff** it is the `provisioner_connector_id` of its warehouse. The
warehouse row is the single source of truth; no connector state is duplicated.

| Mode | Provisioner behavior |
|---|---|
| Warehouse management enabled (`AETHER_CH_TABLE_PERMISSIONS=true`), override **off** | Blocked on every user-facing path; excluded from service lists, `can_use`, and preferences. |
| Management enabled, override **on** | Usable as a normal managed service: routed per-user identity through the connection pool, table grants enforced. Introspection endpoints (`/test`, `/schema`, `/databases`) stay blocked because they use the stored credential. |
| Management disabled (kill switch off) | Blocked regardless of the override — managed execution does not exist, and the stored-credential fallback must never run with the provisioner credential. |
| Worker (`reconcileWarehouse`, `detectWarehouseDrift`, `dropWarehouseIdentitiesLocked`) | Unaffected: raw clickhouse-go connection from the decrypted provisioner config, never the executor/HTTP path. |

## Enforcement

- **Sentinel**: `executor.ErrProvisionerNotExecutable` in `internal/executor/execution_target.go`.
- **Load**: `loadServiceConnector` (`internal/api/execution_target.go:184`) adds a second
  `LEFT JOIN warehouses wprov ON wprov.provisioner_connector_id = c.id`, returning
  `isProvisioner` and `allowProvisionerExecution`. No extra hot-path query.
- **Resolve**: `resolveExecutionTarget` checks immediately after load, *before* the
  `!warehouseManagementEnabled()` early return. Block when provisioner and any of: override off,
  kill switch off, or the connector's warehouse link is missing (integrity violation, fail
  closed). Otherwise routing proceeds normally.
- **HTTP cells**: `handleExecuteCell` (`internal/api/execute_handlers.go:249`) maps the sentinel
  to `403` with a clear message.
- **Agent/MCP**: `resolveClickHouseTarget` (`internal/agent/execution_target.go:22`) maps the
  sentinel to a tool-facing message. All agent tools (`execute_sql`, `sql_query`, `run_cell`,
  `create_cell(run=true)`, `explore_schema`) and the MCP endpoint share these handlers.
- **Introspection**: `/connectors/{id}/test`, `/schema`, `/databases`
  (`internal/api/connector_handlers.go`) reject provisioner connectors (403 / `{ok:false}`),
  independent of the override; blocked attempts log a warning.
- **Listing/routing**: `listWarehouseServices` (`internal/api/execution_target.go:245`) excludes
  the provisioner unless the override is on. This fixes routing, `effective-access`,
  stale-preference handling, and grant warnings in one place.
- **Preference**: `handleSetWarehousePreference` rejects preferring a blocked provisioner.
- **Inventory**: `handleListConnectors` returns `is_provisioner` and forces `can_use=false`
  while blocked.

## Data model & API

- Migration `V118__warehouse_allow_provisioner_execution.sql`:
  `ALTER TABLE warehouses ADD COLUMN allow_provisioner_execution BOOLEAN NOT NULL DEFAULT false;`
- `warehouseJSON` gains `allow_provisioner_execution`; `handleUpdateWarehouse` accepts an optional
  boolean (absent = unchanged) and audits it in the `warehouse.update` metadata. No reconcile is
  needed: no ClickHouse users, roles, or grants change.
- Creating a warehouse keeps the default `false`; admins flip it after creation.

## Frontend

- `web/src/api/warehouses.ts`: `allow_provisioner_execution` on `Warehouse`; `updateWarehouse`
  accepts it; `Connector` in `web/src/types/index.ts` gains `is_provisioner`.
- `WarehouseSettingsPage.tsx` `WarehouseCard`: toggle directly after the provisioner selector,
  with helper text (only background provisioning uses the credential).
- `Cell.tsx` and `ConnectorSelector.tsx`: show "(provisioner)" and disable the option when
  `can_use === false`.
- `ConnectorsPage.tsx`: "Provisioner" badge next to "Default".

## Testing

- Routing matrix: kill switch on/off × override on/off; assert
  `ErrProvisionerNotExecutable` vs managed target, and that kill-switch-off provisioner never
  reaches `ErrUnmanagedConnector`.
- `handleExecuteCell` 403; agent error mapping; introspection endpoints blocked with override
  both off and on.
- Service list / `effective-access` / preference rejection; `handleListConnectors` fields;
  warehouse update persistence + audit.
- Existing suite must stay green (`task check`), plus frontend `tsc --noEmit`, web and relay
  builds, swagger regeneration.

## Edge cases

- Unlink/soft-delete a provisioner: the link clears (existing handlers) and the connector becomes
  a normal connector again automatically.
- Override on + warehouse not `ready`: normal `ErrProvisioningNotReady` (503) path.
- A user's stored preference pointing at a provisioner that becomes blocked is ignored by routing
  (existing stale-preference logic) and cannot be re-set while blocked.
- The provisioner may be `is_default`; runs block with the explicit 403 rather than silently
  remapping to another connector.
