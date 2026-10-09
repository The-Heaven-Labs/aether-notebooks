# Design: Pending-User Grants, Connector Health & ClickHouse Cloud Idle State

- **Date:** 2026-10-08
- **Status:** Approved (design validated section-by-section with the requester)
- **Branch:** `feat/pending-grants-connector-health` (worktree `.worktrees/pending-grants-connector-health`)
- **Deliverable:** this design + three implementation plans; each feature is independently shippable.

## 1. Problem

Three gaps, one theme — staged access and honest connector state:

1. **Pre-account users cannot be granted access outside groups.** Group memberships can be staged by email before the person has an account (`pending_group_members`, V105), but every other user/group picker — resource permissions (ACLs), warehouse table grants, the new-tables inbox — only offers people who already belong to the org. Admins work around it by creating a group per person, which is noisy and hides intent.
2. **Visiting the Connectors page wakes idle databases.** `ConnectorsPage.tsx` fires `POST /connectors/{id}/test` for every connector on load (3 workers, 150 ms apart; `ConnectorsPage.tsx:267-292`). For suspend-on-idle services (ClickHouse Cloud, Databricks SQL warehouses) this defeats the idle state and costs money. The test result is ephemeral client state; nothing about connector health is persisted.
3. **ClickHouse Cloud idle state is invisible.** Aether cannot tell whether a `*.clickhouse.cloud` service is `running`, `idle`, or `stopped`; users discover it by waiting for a wake-up on their first query.

## 2. Current behavior

- `pending_group_members` (V105) + `ApplyPendingGroups` materialize staged memberships inside the join transaction; call sites: `internal/api/auth_handlers.go:125`, `internal/api/org_handlers.go:302`, `internal/api/oidc_handlers.go:325,403,450,510`. `enqueueWarehouseSyncForUser` follows each of them (`auth_handlers.go:129`, `org_handlers.go:310`, `oidc_handlers.go:333,411,456,516`), so a materialized membership reconciles the affected warehouses.
- Groups UX reference: `web/src/pages/GroupsPage.tsx` offers "Add `<email>` as pending member" when the query looks like an email matching no user, shows a "Pending — awaiting first login" badge, and supports removal before materialization.
- ACL storage: `acl_entries` (V010, constraints extended in V047/V062/V065/V124) keeps `subject_type IN ('user','group','org_role')` and a `UNIQUE (resource_type, resource_id, subject_type, subject_id)` constraint. `PUT /acl/{type}/{id}` (`internal/api/acl_handlers.go:197`) is replace-style; session ACLs have their own write path (`handlePutSessionACL`, `acl_handlers.go:324`).
- Warehouse table grants: `warehouse_table_grants` (V108) with `subject_type IN ('user','group','everyone')`; `validateGrantSubject` (`internal/api/warehouse_grant_handlers.go:124`) requires user subjects to be org members.
- Connector health: `connectors` has no health columns. `POST /connectors/{id}/test` (`internal/api/connector_handlers.go:739`) returns `{ok, error}` without persisting. Execution choke points: `openQuery` (`internal/api/query_runner.go:45`, called by `handleExecuteCell` and `runDashboardQuery`) and the agent path (`internal/agent/execution_target.go:73`, used by `execute_sql`/`run_cell`, MCP included).
- ClickHouse Cloud: the control-plane API `GET https://api.clickhouse.cloud/v1/organizations/{org}/services/{service}` returns the service `state` (`starting`, `running`, `idle`, `awaking`, `stopped`, `degraded`, `failed`, …) plus `idleScaling` / `idleTimeoutMinutes`, authenticated with an API key pair — and per ClickHouse's own docs it does **not** wake an idle service.

## 3. Goals / Non-goals

**Goals**

- Any user/group picker accepts a never-signed-in user by email; the staged grant materializes automatically when that person first joins the org.
- The Connectors page performs **zero** data-plane probes on load. Tests run only on explicit action.
- Connector health is persisted: last successful use, last connection-level failure (+ message), shown without probing.
- ClickHouse Cloud service state is visible: exact via optional Cloud API credentials, inferred from activity otherwise (using a configurable idle timeout).

**Non-goals**

- Cross-org user search or instance-wide user directories (privacy); staged grants are org-scoped, like pending group members.
- Invitation emails or auto-created accounts.
- Control actions (wake/start/stop) from the UI — visibility only in this change.
- Changing existing `acl_entries` / `warehouse_table_grants` semantics or the permission resolver.
- Scheduler-driven connector probes (nothing periodic touches connectors).

## 4. Decisions

| # | Decision |
|---|---|
| D1 | **Staged grants live in side tables** (`pending_acl_entries`, `pending_warehouse_table_grants`) mirroring `pending_group_members`. The canonical tables keep their "real subjects only" invariant; the resolver, folder walk, and `chaccess` sync worker never see unregistered users. |
| D2 | **Materialization is join-time and transactional**, next to `applyPendingGroups` at every call site; failures are audited and never block first login. Existing `enqueueWarehouseSyncForUser` calls pick up materialized table grants. |
| D3 | **Staged entries surface in the normal GET responses** as `subject_type: "pending_user"` (subject_id = lowercased email), so all three UIs render them with the existing data flow. PUT/create paths accept `pending_user` and route it to the pending table. |
| D4 | **Union on materialization conflicts.** If the user already holds a direct entry for the same resource, staged actions are unioned into it (never silently dropped); warehouse grant rows dedupe with `ON CONFLICT DO NOTHING`. |
| D5 | **Session (chat) shares support pending users** with exactly `["view"]`, matching their read-only share rule. |
| D6 | **Connector health = one outcome timeline**: `last_success_at`, `last_failure_at`, `last_error`. Status is derived on read (failure newer than success → Failed; success exists → Connected; else Never used). |
| D7 | **Only connection-level failures are recorded** (explicit test failure; connect/dial failure while opening a run, all execution paths). SQL/semantic errors never flip status. Successes: completed runs (cell, dashboard incl. public, agent/MCP), successful tests, successful schema/databases introspection. |
| D8 | **Success writes are debounced** (~30 s per connector) to keep hot rows cool; failure writes are immediate. |
| D9 | **No automatic tests on page load.** The row action "Test connection" is the only data-plane trigger from the Connectors page; its outcome persists. |
| D10 | **Cloud API is optional per connector** (org ID, service ID, key ID/secret, encrypted like all connector config). When present the control-plane state is fetched server-side on page load (cached ~45 s, single-flight) — control plane never wakes the service. |
| D11 | **Inference uses a manual idle timeout** (`idle_timeout_minutes`, default 15): for `*.clickhouse.cloud` hosts without Cloud API credentials, "likely idle" when `now − last_success_at > idle_timeout_minutes`, with wording that stays probabilistic. |
| D12 | **No rollout flags.** All three features ship unconditionally; the Cloud API fields are opt-in by configuration. |

## 5. Detailed design

### 5.1 Pending users in every user/group picker

#### 5.1.1 Data model (migration V127)

```sql
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
CREATE UNIQUE INDEX uq_pending_wh_grants
    ON pending_warehouse_table_grants (warehouse_id, lower(email), database_name, table_name);
CREATE INDEX idx_pending_wh_grants_org_email ON pending_warehouse_table_grants (org_id, lower(email));
```

Emails are stored lowercased; all lookups match on `lower(email)` like `pending_group_members`.

#### 5.1.2 API

- **`GET /acl/{resource_type}/{resource_id}`** additionally returns staged rows synthesized as `models.ACLEntry` with `subject_type: "pending_user"`, `subject_id: <email>`, `pending: true`; the row `id` is the pending row's UUID so the UI can key and remove it. Ordering: real entries then pending entries.
- **`PUT /acl/{resource_type}/{resource_id}`** partitions submitted entries: `user|group|org_role` → `acl_entries` (unchanged), `pending_user` → `pending_acl_entries` (email validated: single `@`, non-empty local/domain, lowercased). Replace semantics apply to both sources for the resource. Action union on duplicate submission within one request.
- **Session ACL** (`PUT /acl/agent_session/{id}`, `normalizeSessionShareEntries`): accepts `pending_user` entries whose actions are exactly `["view"]`; stores them in `pending_acl_entries` (`resource_type='agent_session'`, org = session's org). The replace path deletes non-owner rows from both tables. The owner upsert is untouched.
- **Warehouse grants**:
  - `POST /warehouses/{id}/grants` accepts `subject_type: "pending_user"` with `subject_id` = email; `validateGrantSubject` validates and lowercases the email, then inserts into `pending_warehouse_table_grants`. The service-access warning is suppressed for pending subjects (they cannot connect yet by construction).
  - List includes pending rows (subject_type `pending_user`, `subject_email` set; id = pending row id) so the matrix and new-tables inbox render them; delete removes the row from whichever table holds the id.
  - Validation endpoints (`warehouse_validation.go`) ignore pending rows: warnings describe executable access, and a staged grant has none yet.
- **Audit**: reuse existing event names with a `"pending": true` metadata marker (`acl.granted/updated/revoked`, `warehouse.grant.create/delete`); materialization emits `acl.pending_materialize` / `warehouse.grant.pending_materialize` with counts, and `.error` variants on failure.

#### 5.1.3 Materialization

New `ApplyPendingAccess(ctx, tx, orgID, userID, email)` in `internal/api/pending_user_grants.go`, called via `s.applyPendingAccess(...)` immediately after each `s.applyPendingGroups(...)` call (6 sites listed in §2), inside the same join transaction:

1. ACL: `INSERT ... SELECT` from `pending_acl_entries` for `(org_id, lower(email))` as `'user'`, `ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO UPDATE SET actions = (SELECT ARRAY(SELECT DISTINCT unnest(acl_entries.actions || EXCLUDED.actions)))` — union, never a downgrade.
2. Warehouse grants: `INSERT ... SELECT` as `'user'` with `ON CONFLICT ... DO NOTHING`.
3. Delete the consumed pending rows.
4. Session rows are constrained to `['view']` at staging and defensively re-constrained at materialization.

A materialization error is audited (`acl.pending_materialize.error`, `warehouse.grant.pending_materialize.error`) and swallowed — it must never block first login. Because the existing `enqueueWarehouseSyncForUser` calls run after commit in every join flow, materialized ClickHouse grants are reconciled by the existing sync worker.

#### 5.1.4 Cleanup

- Trash purge (`internal/scheduler/scheduler.go:144`) also deletes `pending_acl_entries` rows for purged notebooks/connectors/dashboards/folders (same data-modifying-CTE pattern as the existing session-ACL cleanup), and for `agent_session` rows of purged notebooks. Warehouse pending grants cascade with `warehouse_id`.
- Existing `acl_entries` purge behavior is untouched (out of scope).
- Deleting a user (`admin_handlers.go:404`) is unchanged: pending rows are email-keyed, not user-keyed.

#### 5.1.5 UI

- Shared helper `looksLikeEmail` extracted from `GroupsPage.tsx` to `web/src/utils/`, reused by the three pickers.
- **PermissionsPanel**: the subject picker offers "Add `<email>` — pending (awaiting first login)" when the query looks like an email matching no person; adding creates a draft `pending_user` entry with the same capability chips as a user. Pending rows render with a muted avatar + "Pending — awaiting first login" secondary line and are removable like any entry. Save submits them; GET returns them after reload.
- **WarehouseTableGrants / NewTablesInbox**: an inline email affordance next to the subject picker stages a pending grant for the selected database/table (and the inbox row); staged rows show the pending badge and are deletable.
- Pickers keep showing only org members — pending is an explicit "add by email" action, never a global user search.

#### 5.1.6 Edge cases

- Email already belongs to a registered user **who is not in this org**: still staged; it materializes only if and when they join this org (mirrors `pending_group_members`).
- Email belongs to an existing org member: the UI adds them directly (they appear in the picker); if an email is staged that later becomes a member through another path without materialization running (e.g. row staged and the person joins through an old call site), the join-time call sites cover every join path (registration, org join, SSO provisioning/auto-join, invite redemption).
- Duplicate staged submissions merge (union of actions / DO NOTHING for grants).
- Staged rows are visible to anyone who can read the resource ACL / grants; visibility rules are unchanged.
- Replace-style PUT from an older client that omits pending rows removes staged grants for that resource — same semantics as omitting any entry today; accepted.

### 5.2 Connector health (migration V128)

```sql
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_success_at TIMESTAMPTZ;
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_failure_at TIMESTAMPTZ;
ALTER TABLE connectors ADD COLUMN IF NOT EXISTS last_error TEXT;
```

- `models.Connector` gains `last_success_at`, `last_failure_at` (`*time.Time`) and `last_error` (`string,omitempty`); list/get SELECTs include them.
- New `internal/api/connector_health.go`: `recordConnectorSuccess(ctx, orgID, connectorID)` — debounced `UPDATE ... WHERE last_success_at IS NULL OR last_success_at < NOW() - INTERVAL '30 seconds'`; `recordConnectorFailure(ctx, orgID, connectorID, err)` — immediate `UPDATE ... SET last_failure_at=NOW(), last_error=$3` (error text truncated to 500 chars). Both run with `context.WithoutCancel` + a short internal timeout so a cancelled execution context cannot skip the write.
- Recording sites:
  - `openQuery` (`query_runner.go`): the three `errQueryConnectFailed` returns (pooled-Get failure, managed/unmanaged `driver.NewExecutor` failures) → failure.
  - `handleExecuteCell` and `runDashboardQuery`: after `Execute` returns nil → success. Execution errors → no record (D7).
  - `handleTestConnector`: `TestConnection` ok/err → success/failure. The unsaved-config endpoint (`/connectors/test`) records nothing.
  - `handleConnectorSchema` / `handleConnectorDatabases`: buildExecutor/connect failure → failure; successful response → success.
  - Agent path: new `ToolContext.RecordConnectorActivity func(connectorID string, ok bool, errMsg string)` (propagated through `Engine` like `ResolveTarget`; wired in `mcp.go` and the engine construction). `execution_target.go` calls it with `ok=false` at its two connect sites (ConnPool.Get, driver.NewExecutor); `tools_sql.go`/`tools_notebook.go` call it with `ok=true` after a successful `Execute`. MCP inherits this through the shared context.
- Status derivation lives in the frontend: `failed = last_failure_at && (!last_success_at || last_failure_at > last_success_at)`; `connected = last_success_at && !failed`; else never used.
- UI (`ConnectorsPage.tsx`): delete the auto-test effect and its refs; Status column renders the derived status with a relative time (`Connected · used 3m ago`, `Failed · 12m ago` with `last_error` in the badge title, `Never used — click Test`). The row action Test keeps local "Testing…" feedback and invalidates the connectors query so the persisted status refreshes. A small relative-time helper is added under `web/src/utils/`.

### 5.3 ClickHouse Cloud idle state

**Config** (flat keys inside the encrypted connector config; masked where secret):

| Key | Meaning | Masked |
|---|---|---|
| `idle_timeout_minutes` | Manual idle threshold for inference (default 15) | no |
| `cloud_org_id` | ClickHouse Cloud organization ID | no |
| `cloud_service_id` | Service ID | no |
| `cloud_key_id` | API key ID | no |
| `cloud_key_secret` | API key secret | **yes** |

`secretFieldSet` (`connector_handlers.go:983`) adds `cloud_key_secret` so it is masked in responses and merge-preserved when the edit form leaves it blank.

**Endpoint** `GET /api/v1/connectors/{id}/cloud-state` (connector `view` required):

- Non-ClickHouse connector or incomplete/missing credentials → `200 {"configured": false}`.
- Configured → server-side `GET https://api.clickhouse.cloud/v1/organizations/{org}/services/{service}` with basic auth (5 s timeout), parsing `result.state`, `result.idleScaling`, `result.idleTimeoutMinutes`. Response: `{"configured": true, "state": "...", "idle_scaling": ..., "idle_timeout_minutes": ..., "checked_at": "..."}` or `{"configured": true, "error": "..."}` with HTTP 200 so the page degrades gracefully.
- Cache: in-memory TTL (~45 s) + in-flight dedupe per connector; the base URL is a package variable so tests can point at `httptest`.
- Security: fixed API host (no user-controlled URL → no SSRF), credentials encrypted at rest and never returned, secrets never logged.

**UI**:

- Connector form (ClickHouse only): "Idle timeout (minutes)" always; an optional "ClickHouse Cloud API" group (org ID, service ID, key ID, key secret) with help text explaining what it unlocks.
- Connectors list: for ClickHouse connectors, a second status chip from a `cloud-state` query per connector (`staleTime` ~30 s, `refetchInterval` 60 s while the page is open — control-plane reads never wake the service). States map to labels: `running`, `idle` ("Idle — wakes on next query"), `awaking`, `stopped` ("Stopped"), `degraded`/`failed` (error styling).
- Without Cloud API credentials, for hosts matching `*.clickhouse.cloud`: inference line "Likely idle — last activity 32m ago (idle timeout 15m)" when `now − last_success_at > idle_timeout_minutes`; never used → "Idle state unknown". Inference is intentionally probabilistic and replaces the old always-test behavior.

## 6. Testing

- **Materialization unit tests** (`internal/api/pending_user_grants_test.go`, real DB): creates+consumes rows; org isolation; case-insensitivity; idempotency; action union with an existing direct entry; warehouse rows dedupe; session rows stay view-only; failure audit never blocks the caller.
- **Handler tests**: ACL GET/PUT round-trips pending entries (incl. replace semantics); session ACL accepts pending view + rejects other actions; warehouse grant create/list/delete pending; validation ignores pending; connector health recorded on test success/failure, on `openQuery` connect failure, and on successful cell/dashboard/agent execution; `/cloud-state` with `httptest` mock covering configured state, API error, missing credentials, caching, and secret masking.
- **Agent tests**: `ToolContext.RecordConnectorActivity` stub assertions at connect-failure and success sites.
- **Frontend tests (Vitest)**: PermissionsPanel pending add/save/remove; ConnectorsPage asserts **no** `/test` request on mount and renders derived statuses; warehouse pickers stage pending grants; cloud-state chip + inference copy; relative-time helper.
- **Standards**: `task check` (fmt+vet+tidy+test), `cd web && npx tsc --noEmit && npm run build`, `cd relay && npm run build`; real-browser validation with agent-browser for the three UI surfaces (per AGENTS.md).

## 7. Compatibility & rollout

- All API changes are additive; older clients render `pending_user` rows as an email subject and round-trip them (replace semantics).
- No feature flags. ClickHouse Cloud support degrades to inference when there is no egress to `api.clickhouse.cloud` or no credentials.
- Migration order: V127 (pending tables) and V128 (connector health) are independent; both are backward-compatible (`CREATE TABLE`, nullable columns).
- Docs to update: `AGENTS.md` (pending-user grants, connector health, cloud-state endpoint).
