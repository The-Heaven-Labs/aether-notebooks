# Aether — Claude Code Context

**Aether** is a collaborative SQL/data notebook platform (think Jupyter for analytics). It has a Go API server, a React frontend, and a Hocuspocus relay for real-time collaborative editing via Yjs.

## Agent API Access

When an agent needs to interact with the Aether API (create notebooks, manage connectors, list orgs, etc.), **always try the `aether` CLI first** before falling back to `curl` or direct HTTP calls. The CLI handles authentication, token management, and API URL resolution automatically. Use `curl`/HTTP directly only when the CLI lacks the needed operation (e.g., Swagger/OpenAPI docs or unsupported flags and subcommands).

## Architecture

```
cmd/aether-server      → Go API server (port 8088)
cmd/aether             → CLI client
internal/
  api/              → HTTP handlers + router (net/http ServeMux, no framework)
  auth/             → JWT issuer + OIDC providers
  config/           → Env-based config (Load())
  crypto/           → AES key derivation for connector credentials
  database/         → pgx connection pool + migrations
  executor/         → Executor interface + postgres/clickhouse/js implementations
  models/           → Shared model structs
  audit/            → ClickHouse audit logger
  scheduler/        → cron-based notebook scheduler
web/                → React + Vite + TypeScript frontend (port 5173 in dev)
relay/              → Hocuspocus WebSocket relay (port 3001) — TypeScript/Node
migrations/         → SQL migration files (applied at server startup)
```

## Dev Stack (Docker)

**Always use `docker-compose.dev.yml` for development.** This starts all services:

```bash
docker compose -f docker-compose.dev.yml up -d    # Start everything
docker compose -f docker-compose.dev.yml ps       # Check status
docker compose -f docker-compose.dev.yml logs -f web  # Follow web logs
```

Services: API (Go), Relay (TypeScript), Web (Vite), Postgres, Redis, ClickHouse, OpenSearch, Keycloak

### Subdomain Testing

For local multi-tenancy testing with subdomains (`org1.aether.test` → Org 1):

1. Add to `/etc/hosts`:
   ```
   127.0.0.1  aether.test
   127.0.0.1  org1.aether.test org2.aether.test
   ```

2. Create orgs with slugs matching subdomains:
   ```bash
   # Via API
   curl -s -X POST http://localhost:8088/api/v1/auth/org/create \
     -H "Authorization: Bearer $TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"name": "Org 1", "slug": "org1"}' | jq .
   ```

3. Visit `http://org1.aether.test:5173` — the app resolves the org from the subdomain.

## Commands

**Task runner: `task` (Taskfile.yml) — the only task runner in this repo.**

```bash
# Infrastructure
task infra:up          # Start Postgres, Redis, ClickHouse (skips already-running)
task infra:down        # Stop all
task infra:reset       # Destroy + recreate (data loss!)

# Development with Docker (preferred)
docker compose -f docker-compose.dev.yml up -d    # Start full dev stack (API, relay, web, Postgres, Redis, ClickHouse, OpenSearch)
docker compose -f docker-compose.dev.yml restart web  # Restart web container (clears Vite cache)

# Development (run concurrently in separate terminals)
task dev               # Go API server with infra:up dep
task dev:web           # Vite dev server (proxies /api → :8088)
task dev:relay         # Hocuspocus relay

# Build
task build             # Both Go binaries → ./bin/
task build:web         # React → web/dist/
task build:relay       # TypeScript relay → relay/dist/
task build:all         # Everything

# Testing
task test              # All Go tests (starts infra first; resets the disposable test DB)
task test:v            # Verbose
task test:api          # Only internal/api/... tests
task test:race         # With race detector
task test:smoke        # Smoke test against live server
task test:e2e          # Playwright E2E tests (requires dev stack on :5173)

The Go test targets run through `scripts/go-test.sh`, which resets a **disposable
per-worktree database** (`aether_test_<worktree>`, Redis DB 1) before the run, so
tests keep hitting a real Postgres with real migrations while the shared dev
database (and its accumulated rows) stays untouched and out of the way. Parallel
worktrees get separate databases. `task test:watch` reuses the database without
resetting; `AETHER_TEST_DB`/`AETHER_TEST_DB_RESET` override the defaults.

All Go test commands pass an explicit `-timeout 3m` (Taskfile and CI). Do not
raise the timeout — narrow the run with `-run` or split/speed up the test.
Agent-run test commands must include `-timeout 3m` too.

# Code quality
task fmt               # gofmt
task vet               # go vet
task tidy              # go mod tidy + verify
task check             # fmt + vet + tidy + test

# Database
task db:psql           # psql shell into dev DB
task db:reset          # Drop + recreate dev DB (data loss!)
```

## Environment Variables

| Variable | Required | Default | Notes |
|---|---|---|---|---|
| `AETHER_MASTER_KEY` | **yes** | — | AES key for encrypting connector credentials; also keys the HMAC lookup hash for personal access tokens. Rotating it invalidates PATs that already carry a lookup hash (legacy bcrypt-only tokens keep working and are transparently migrated on first use). |
| `AETHER_JWT_SECRET` | **yes** | — | JWT signing secret |
| `AETHER_DATABASE_URL` | no | `postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable` | |
| `AETHER_REDIS_URL` | no | `redis://localhost:6379` | |
| `AETHER_PORT` | no | `8088` | |
| `AETHER_OIDC_HOST_REWRITE` | no | — | `from=to` pair for rewriting the OIDC discovery host (e.g. `localhost:5557=host.docker.internal:5557`). Used in Docker dev where the API container must reach Keycloak via a different hostname than what's in the discovery URL. |
| `AETHER_PLATFORM_ADMIN_EMAIL` | no | — | Email of the user to auto-promote to platform admin on startup |
| `AETHER_PUBLIC_URL` | no | `http://localhost:8088` | Public-facing URL for link generation |
| `AETHER_FRONTEND_URL` | no | `http://localhost:5173` | Frontend URL for CORS and OIDC redirect |
| `AETHER_ATTACHMENT_DIR` | no | `./attachments` | Directory for local file attachments |
| `AETHER_STORAGE_BACKEND` | no | `local` | Storage backend type (`local` or `s3`) |
| `AETHER_S3_ENDPOINT` | no | — | S3-compatible storage endpoint |
| `AETHER_S3_BUCKET` | no | — | S3 bucket name |
| `AETHER_S3_REGION` | no | `us-east-1` | S3 region |
| `AETHER_S3_ACCESS_KEY` | no | — | S3 access key |
| `AETHER_S3_SECRET_KEY` | no | — | S3 secret key |
| `AETHER_MAX_ATTACHMENT_BYTES` | no | `10485760` | Maximum attachment file size in bytes |
| `AETHER_OUTPUT_LIMITS_MAX_BYTES` | no | `67108864` (64MB) | Platform ceiling applied to every org's `cell_output_max_bytes` / `notebook_inline_outputs_max_bytes` caps. `0` = no ceiling. A per-org value of `0` means unlimited. |
| `AETHER_AGENT_STATS_ROLLUP_INTERVAL` | no | `1h` | Agent usage hourly-rollup cadence (Go duration, floor `5m`) |
| `AETHER_AGENT_TOOL_TIMEOUT_DEFAULT` | no | `120s` | Global fallback timeout for agent tools that declare no explicit budget (Go duration, floor 1s). |
| `AETHER_TOOL_ALLOWED_DOMAINS` | no | — | Comma-separated list of allowed domains for webhook tools |
| `AETHER_DISABLE_REGISTRATION` | no | `false` | If set to `true`, disables new user registration |
| `AETHER_CH_TABLE_PERMISSIONS` | no | `false` | Enables per-user ClickHouse warehouse table permissions. When unset/false, every connector executes with its stored credential and reconcile is a no-op. |
| `AETHER_CH_RECONCILE_INTERVAL` | no | `10m` | Warehouse reconcile catch-up cadence (Go duration, floor `1m`): jittered startup enqueue-all plus interval re-enqueues, on top of mutation triggers. |
| `AETHER_MCP_OAUTH_ENABLED` | no | `false` | Serves the MCP OAuth 2.1 authorization-server endpoints (`/.well-known/oauth-protected-resource`, `/.well-known/oauth-authorization-server`, `/oauth/register`, `/oauth/authorize`, `/oauth/token`). When off all of them return `404`; the MCP endpoint itself keeps working with PATs/session JWTs. See `docs/mcp-oauth.md`. |
| `AETHER_MCP_SQL_TIMEOUT_MS` | no | `600000` (10m) | Ceiling for `execute_sql` calls that arrive over MCP (integer ms; values below `1000` are rejected at startup). In-app agents keep the 30s default; a per-call `timeout_ms` arg is clamped to this ceiling. |

`Taskfile.yml` sets dev values for `AETHER_DATABASE_URL`, `AETHER_MASTER_KEY`, `AETHER_JWT_SECRET`, and `AETHER_PLATFORM_ADMIN_EMAIL` automatically when using `task`. Other vars rely on defaults or are set in `docker-compose.dev.yml`.

## Test Users

When using the dev stack (`docker-compose.dev.yml`), use these test users:

| Email | Password | Notes |
|---|---|---|
| `nova@heaven-labs.com` | `nova123` | Primary test user |
| `sol@heaven-labs.com` | `sol123` | Secondary test user |

**Note**: Home folders are named using the user's email address (e.g., `nova@heaven-labs.com`). This ensures uniqueness and avoids confusion with similar names.

## SSO / OIDC (Dev Stack)

The dev stack includes **Keycloak** as the OIDC identity provider (Dex was removed — Keycloak handles all OIDC testing including group provisioning).

On first startup, the server auto-seeds a **platform-level** Keycloak SSO provider. This appears in the Admin > SSO settings page. You can also create org-level providers via Org Settings > SSO.

### Provider Details

| Field | Value |
|---|---|
| Name | `Keycloak (Dev)` (auto-seeded, platform-level) |
| Issuer URL | `http://localhost:5557/realms/aether-dev` |
| Client ID | `aether-dev` |
| Client Secret | `aether-dev-keycloak-secret` |
| Allowed Domains | `aether-dev.test` |
| Scopes | `openid`, `profile`, `email` |

### Keycloak Test Users (from `dev/keycloak-realm.json`)

| Email | Password | Groups |
|---|---|---|
| `alice@aether-dev.test` | `alice123` | aether-analysts, all-employees |
| `bob@aether-dev.test` | `bob123` | aether-engineering |
| `charlie@aether-dev.test` | `charlie123` | all-employees |
| `dave@aether-dev.test` | `dave123` | aether-engineering, all-employees |
| `eve@aether-dev.test` | `eve123` | aether-analysts |

**Admin console**: `http://localhost:5557` (admin / admin123)

### Docker Networking

The API server runs inside Docker and needs to reach Keycloak. The discovery URL uses `localhost:5557` (the host-facing port) so the browser redirects work correctly. Inside the container, `AETHER_OIDC_HOST_REWRITE=localhost:5557=host.docker.internal:5557` rewrites the connection target to `host.docker.internal` while preserving the `Host` header. This is configured in `docker-compose.dev.yml` — no action needed.

## API Documentation (Swagger/OpenAPI)

The API documentation is auto-generated using [swag](https://github.com/swaggo/swag). To regenerate after adding/modifying endpoints:

```bash
swag init -g cmd/aether-server/main.go -o internal/api/docs
```

This generates `docs/swagger.json` and `docs/swagger.yaml`. The docs are served at:
- `http://localhost:8088/docs` (direct API access)
- `http://localhost:5173/docs` (via Vite proxy)

### Adding annotations to new endpoints:

```go
// @Summary Get notebook by ID
// @Description Returns a notebook with all its cells
// @Tags notebooks
// @Accept json
// @Produce json
// @Param id path string true "Notebook ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id} [get]
func (s *Server) handleGetNotebook(w http.ResponseWriter, r *http.Request) {
    // ...
}
```

## Key Patterns

**Tests hit a real database** — no mocks. `task test` starts infra automatically. Tests use `setupTestServer(t)` from `testhelpers_test.go` which wires a real DB, JWT issuer, and audit logger.

**Static frontend assets** are embedded in the Go binary (`cmd/aether-server/embed.go`) and prepared once at startup: brotli (q5, preferred) and gzip variants plus content-hash ETags. `/assets/*` (Vite content-hashed) is served `Cache-Control: public, max-age=31536000, immutable`; other static files and the config-injected `index.html` are `no-cache` and revalidate via ETag (304). Ranges are served from the identity representation, and already-compressed extensions (`.woff2`, images, …) skip the compression pass.

**Frontend code splitting**: every page is route-lazy; heavy interaction-gated modules load on demand — ECharts via `OutputRenderer`'s lazy `ChartView`, the agent panel / shortcuts / MOTD markdown in `AppShell`, the collaboration stack (`web/src/components/collabRuntime.ts`: Yjs + Hocuspocus + y-codemirror) from `Cell`, `sql-formatter` and `@codemirror/lang-javascript` from their triggers, the image viewer from `MarkdownCell`/`AgentMessageImages`, and the notebook side panels (schema, schedules, history, parameters, permissions, share, session viewer). Fonts are self-hosted woff2 subsets under `web/public/fonts/` (no third-party font requests).

## Roles & Admin

Aether has a **two-level admin system**: org-level and instance-level. All non-admin permissioning is handled exclusively through ACL entries (user, group, or "Everyone" subjects) — org roles no longer grant implicit permissions.

### Org Roles

Every member of an organization has a role in `org_members`. The only meaningful role is:

| Role | Effect |
|---|---|
| `admin` | **Bypasses all ACLs** within their org — can see/edit/delete every resource, manage members, connectors, groups, audit logs, SSO, MCP servers, and MOTD messages |
| `editor` / `viewer` | Stored for legacy tracking but **grant no implicit permissions**. Access is determined entirely by ACLs. |

**How org admin is assigned**: The **person who creates the org automatically becomes its admin**. Additional admins can be assigned by existing admins via:
- `PUT /api/v1/members/{user_id}` (change a member's role)
- `POST /api/v1/members/invite-link` (invite with role `admin`)

### Platform Admin (instance-level super-admin)

A **platform admin** is a special flag (`users.is_platform_admin`) that grants access to **instance-wide management** across *all* organizations. This is distinct from an org admin, which only governs a single org.

**Why it exists**: Platform admin is designed for the **host/SaaS operator** of an Aether instance. Use cases include:
- Viewing all orgs and their user counts (`GET /api/v1/admin/orgs`)
- Managing all users across orgs (`GET /api/v1/admin/users`)
- Promoting/demoting other platform admins (`PUT /api/v1/admin/users/{id}`)
- Configuring **platform-level SSO providers** that org admins can then enable for their org
- Managing instance-wide Message of the Day (MOTD)

**How platform admin is assigned**:
1. **Auto-promotion at startup**: Set `AETHER_PLATFORM_ADMIN_EMAIL=envar` — the server promotes that user on startup and on registration.
2. **By another platform admin**: `PUT /api/v1/admin/users/{id}` with `{"is_platform_admin": true}`.
3. **Direct SQL**: `UPDATE users SET is_platform_admin=true WHERE email='...'`.

In dev, `Taskfile.yml` sets `AETHER_PLATFORM_ADMIN_EMAIL: admin@heaven-labs.com` — so that user is auto-promoted.

### How They Differ

| Aspect | Org Admin | Platform Admin |
|---|---|---|
| **Scope** | Single organization | Entire instance (all orgs) |
| **Storage** | `org_members.role = 'admin'` | `users.is_platform_admin = true` |
| **ACL bypass** | Yes — all resources in their org | N/A (operates at instance level) |
| **Middleware** | `RequireRole("admin")` | `RequirePlatformAdmin` |
| **Frontend** | "Settings" link in profile menu | "Admin" link in top bar |
| **Key features** | Invite members, manage connectors/groups/audit/SSO | List/manage orgs, users, platform SSO, MOTD |

**Filesystem**: Folders live in `folders` table with self-referential `parent_id` (adjacency list). All resource types (notebooks, connectors, dashboards) have a nullable `folder_id`. Each user gets a personal home folder created on registration/org-join, seeded with a full-access ACL entry. Home folders use the user's email as the name (e.g., `user@example.com`).

**Permissions**: `acl_entries` table stores per-resource ACL. Resolution walks the ancestor folder chain via recursive CTE, ordered by specificity (resource entry beats parent folder beats grandparent; within same depth: user beats group beats org_role). Deny by default if no ACL matches. Use `s.checkPermission(ctx, userID, orgID, orgRole, resourceType, resourceID, action)` (method on `*Server`) from `internal/api/permissions.go`. Route middleware: `requirePermission(resourceType, idParam, action)`.

**Agent session sharing**: `agent_session` is an ACL resource (V124) deliberately absent from `resourceTable` and the folder walk (sessions have no `org_id`/`folder_id`); `resourceOrgID` resolves it via `agent_sessions → agents.org_id`. `checkSessionPermission` (`internal/api/permissions.go`) is the single access primitive for every session route (get/messages/usage/PATCH/rename/subagent messages/attachments/WS/ACL GET), replacing agent-level checks: owner fallback (`session.user_id == caller` passes every action and survives an ACL replace) → `agent_session` ACL with the admin-mode bypass (org admins need admin mode for session ACL reads/writes, unlike the legacy unconditional bypass on other types) → live notebook-viewer inheritance for `view` only. Non-owner subjects are read-only: `PUT /acl/agent_session/{id}` accepts only same-org user/group/everyone entries whose actions are exactly `["view"]` (anything else is a 400), replaces non-owner entries, and upserts the owner's full-access entry last so a replace-style PUT can never lock the owner out.

**Session notebook linkage**: sessions carry a nullable `notebook_id`; opt-in `agent_sessions.share_with_notebook_viewers` grants live read-only access to anyone who can view that notebook (view only, evaluated live — revocation is immediate for HTTP reads; live WebSockets enforce it at connect and on each periodic re-validation). `PATCH /sessions/{session_id}` updates the title (session `edit`) and the flag (session `share`, notebook required); `PATCH /sessions/{session_id}/title` remains for compatibility. Any session — standalone or notebook-attached — is directly addressable at `/chats/{id}`; the link is ACL-gated (the URL itself grants nothing), rendering an interactive full-page chat for editors and the read-only live viewer for recipients.

**Session automation and listing**: `POST /agents/{id}/session` accepts `shares: [{subject_type, subject_id, actions}]` plus `share_with_notebook_viewers` and writes the session, owner ACL, shares, and flag in one transaction; CLI `aether agents sessions create` exposes `--share-user`/`--share-group`/`--share-everyone`/`--share-notebook-viewers`. `GET /notebooks/{id}/sessions` (requires notebook `view`) and `GET /sessions/shared` list visible sessions; list responses include `owner_email`, `shared`, `can_edit`, and `share_with_notebook_viewers`.

**Session WebSocket and streams**: WS connect requires session `view`; `canEdit` (session `edit`) is computed once, and connect-time admin mode is applied only to editors. `view` is re-validated after connect — on every inbound `reconnect` frame and periodically every ~60s (`agentWSViewRevalidateInterval`, a test seam) — so a viewer whose share/group/notebook access is revoked is disconnected within that interval rather than keeping the stream; `edit` is never re-checked (owner or admin mode, stable within a connection). Viewers receive all live events but every mutating frame is rejected with a `read-only session` error (only `reconnect` is allowed). Session events fan out via Redis pub/sub (`aether:agent:sess:{id}`; `:seq` non-expiring INCR counter; `:buf` 500-event list with 2h TTL) with an in-memory fallback when Redis is absent; `cancel`/`tool_confirm`/`question_answer`/steering remain pod-local, so an owner reconnecting to another replica mid-turn cannot confirm/cancel until timeout while viewers are unaffected. Stream keys are reclaimed only by `SessionStore.DeleteSession` (today: `/new` on an empty session); the creation-time empty-session sweep and cascade deletes (agent/user/notebook hard delete, trash purge) delete the session's `agent_session` ACL rows but do not remove the Redis keys — the non-expiring `:seq` keys accumulate by design as a small, bounded trade-off. Hard-deleting sessions, agents, users, or notebooks (including trash purge) deletes the session's `agent_session` ACL rows.

**Groups**: Custom groups (`groups` + `group_members` tables) are first-class permission subjects. Group management (create/rename/delete/members) requires `admin` role; viewing groups is open to all members. Groups carry a `source` column (`manual`/`sso`/`system`): SSO create/adopt sets `sso` (one-way, audited `group.sso.adopt`), Everyone is `system`, and admin-created groups are `manual`. `DELETE /api/v1/groups/{id}` refuses `sso` unless `?force=true` (audited `group.delete.forced`) and never deletes `system` groups.

**Pre-provisioned group members**: Org admins can stage group memberships by email before the person has an account (`pending_group_members`, V105; endpoints `POST`/`GET /api/v1/groups/{id}/pending-members` and `DELETE /api/v1/groups/{id}/pending-members/{email}`). `ApplyPendingGroups` (called inside the join transaction on password registration, SSO provisioning/auto-join, and invite redemption) materializes matching rows into `group_members` and consumes the pending rows; failures are audited (`group.pending_materialize.error`) and never block first login. Staged memberships are admin-curated and therefore survive IdP `auto_sync_groups` removals. Posting an email that already belongs to an org member adds them directly instead of staging.

**Pending-user grants**: Pre-account users can be granted access anywhere a user/group subject exists. Staged rows live in `pending_acl_entries` and `pending_warehouse_table_grants` (both V127), keyed by lowercased email; `GET /acl/{type}/{id}` and `GET /warehouses/{id}/grants` surface them as `subject_type: "pending_user"` (subject_id = email, `pending: true` on ACL rows, id = the pending row UUID), and PUT/create accept `pending_user` with a validated (single-`@`, lowercased) email. Session shares accept pending users with exactly `["view"]`. Materialization runs inside every join transaction via `s.applyPendingAccess` immediately after `s.applyPendingGroups` (registration auto-join, invite redemption, SSO/subdomain auto-join): staged ACL actions are unioned into any existing direct entry (`ON CONFLICT ... DO UPDATE`), warehouse grants dedupe with `DO NOTHING`, staged rows are consumed, and failures are audited (`acl.pending_materialize.error` / `warehouse.grant.pending_materialize.error`) without blocking first login. A residual race is a deliberate trade-off: a staging write that commits after a join's materializer snapshot stays visible as `pending_user` and self-heals on the next admin action for that email — a PUT converts the now-member email into a real user entry and the warehouse-grant create path re-runs the materializer before inserting. Treating a staged grant as executable is a bug: validation, the resolver, and the ClickHouse sync worker read only the canonical tables. Trash purge (`internal/scheduler/scheduler.go` `purgeTrash`) deletes pending ACL rows for purged notebooks/connectors/dashboards/folders and for the agent_session rows of purged notebooks; the empty-session sweep in `createSessionWithSharing` deletes a swept session's pending shares with its ACL rows. Maintainer invariants: writers take the pending lock before the canonical one (`agent_sessions → pending → real` for sessions, matching materialization) so concurrent writers cannot deadlock. Because `resource_id` carries no foreign key, staged rows are cleaned explicitly where the corresponding ACL rows are cleaned — the trash purge, the empty-session sweep, and the `agent_session` hard-delete paths; other hard-delete paths leave inert staged rows exactly as they leave `acl_entries` rows (follow-up). UI: `looksLikeEmail`/`normalizeEmail` live in `web/src/utils/email.ts`; PermissionsPanel offers "add by email" when the picker query matches nobody, and the warehouse table-grants/new-tables surfaces stage pending grants by email.

**Internal routes** (`/internal/*`) are unauthenticated by standard JWT middleware — they're called only by the Hocuspocus relay and validated via `handleInternalAuthValidate`. Do not add auth middleware to these.

**Migrations run automatically** on server startup (not a separate migration tool).

**Vite proxy**: In dev, Vite forwards `/api`, `/internal`, `/docs`, and `/swagger.json` to `localhost:8088`. The `API_URL` env var overrides the target to point the dev proxy at a non-default API (e.g., a worktree-local server).

**Connector credentials** are AES-encrypted using `crypto.DeriveKey(masterKey)` before storing in Postgres.

**ClickHouse Cloud idle state**: ClickHouse connectors carry an optional manual inference threshold `idle_timeout_minutes` (frontend-only, default 15 when absent; distinct from the service-reported `idle_timeout_minutes` the endpoint returns when credentials are configured) and optional ClickHouse Cloud API credentials (`cloud_org_id`, `cloud_service_id`, `cloud_key_id`, `cloud_key_secret` — masked as `***` in responses and merge-preserved when an edit leaves the secret blank) inside their encrypted config. `GET /api/v1/connectors/{id}/cloud-state` (connector `view`) reads the control-plane service state (including `running`/`idle`/`awaking`/`stopped`/`degraded`/`failed`) from the fixed host `https://api.clickhouse.cloud` with basic auth and a 5 s timeout; control-plane reads never wake an idle service. Successful results are cached in memory ~45 s with in-flight dedupe (single-flight) per connector; failures are not cached and retry on the next poll. Non-ClickHouse or incomplete-credential connectors return `200 {"configured": false}`, and upstream failures return `200 {"configured": true, "error": ...}` so the page degrades gracefully instead of surfacing a request error. Without credentials, the Connectors page infers for `*.clickhouse.cloud` hosts: "Likely idle" when `now − last_success_at > idle_timeout_minutes`, "Active recently" otherwise, and "Idle state unknown" when the connector has never been used; the cloud-state query runs only for ClickHouse connectors with credentials, with `staleTime` 30 s and `refetchInterval` 60 s while the page is open.

**Connector health**: `connectors.last_success_at` / `last_failure_at` / `last_error` (V128) persist one outcome timeline per connector; status is derived client-side by `ConnectorsPage` (failure newer than success → Failed; success exists → Connected; else Never used). Only connection-level outcomes are recorded: the explicit `POST /connectors/{id}/test` action, connect/dial failures while opening a run in every execution path (HTTP cells, dashboards including public, agent/MCP via `ToolContext.RecordConnectorActivity`), successful schema/databases introspection, and completed runs. Success writes are debounced ~30 s per connector (`internal/api/connector_health.go`), except when the last failure is newer than the last success (a Failed connector), where the next success always lands so a recovery inside the window flips status back immediately; failure writes are immediate and truncate the message to 500 runes. Recording is synchronous on the request path, detached from cancellation (`context.WithoutCancel`) and bounded by a 2 s timeout: a cancelled context cannot skip the write and a write failure never fails the request. SQL/semantic errors never flip status; OpenSearch constructs lazily, so an unreachable OpenSearch host during a run does not flip status (only the explicit Test does). The Connectors page performs zero data-plane probes on load — the row action "Test connection" is the only persisted trigger (the create/edit modal's unsaved-config Test records nothing), and its outcome is persisted and refetched.

**ClickHouse table permissions (warehouses)**: A connector linked to a `warehouses` row is **managed**: Aether provisions one ClickHouse user per member (`aether_<wh8>_u_<hash(warehouse,org,user)>`), one role per group (`aether_<wh8>_g_<hash>`), plus `aether_<wh8>_everyone`, and enforces table access with explicit per-table rows in `warehouse_table_grants` (subjects: user/group/Everyone; no wildcards). Service (`use`) access stays in `acl_entries`; `warehouse_service_preferences` stores the user's preferred service, used only as a default for new notebooks and dashboard selections — never as a routing override. An **unmanaged** connector (no `warehouse_id`) is never touched by the sync worker and executes with its stored credential exactly as before. `schema_snapshots` (V114) caches each connector's raw catalog for the new-tables inbox. Everything runs only when `AETHER_CH_TABLE_PERMISSIONS=true`; reconcile is single-flight per warehouse (Postgres advisory lock) and re-runs on mutation triggers, a jittered startup enqueue-all, and the `AETHER_CH_RECONCILE_INTERVAL` catch-up (default 10m). A warehouse's provisioner connector is non-executable for users, agents, and MCP by default (fail-closed even when the kill switch is off); org admins can opt a warehouse into managed execution through it with `warehouses.allow_provisioner_execution`, and keep admin-only access to its `/test|/schema|/databases` introspection. The reconcile worker's raw connection is unaffected.

**Execution routing**: `internal/api/execution_target.go` resolves the target for HTTP/agent/MCP runs; agent tools go through `openAgentExecutor` (`internal/agent/execution_target.go`). Scheduler execution is **not wired yet** — `cmd/aether-server/main.go` installs a no-op scheduler callback — and when it is, it must resolve through `resolveExecutionTarget` with an explicit identity so warehouse runs never fall back to stored credentials. Managed connectors must be `sync_status='ready'` or execution fails closed (HTTP 503) — the stored credential is **never** a fallback. Resolution fails closed when the selected connector lacks `use` (HTTP 403 `service_access_denied` carrying the permitted services), and on missing/soft-deleted/non-ClickHouse/cross-org connectors. The selected connector always serves the run when permitted; the stored preference and the legacy `pinned` flag (accepted, ignored) never re-route execution. Reconcile invalidates pooled `(endpoint, user)` connections before applying DDL and whenever it fails closed on wildcards/unexpected grants, so a resident session cannot keep old access. Invalidation is local-first, then broadcast on the Redis channel `aether:warehouse-identity-invalidation` (JSON, chunked at ≤1000 identity names per message) so every replica drops its own pooled sessions; the subscriber starts in `StartBackgroundJobs` and stops in `Server.Close`, and a nil Redis client skips it. The broadcast is best-effort: when Redis is unavailable the publisher logs and returns (bounded by a 1s publish deadline) while local invalidation still applies, so a Redis outage window leaves other replicas' resident sessions serving pre-reconcile access until their connections are reopened — restart the affected replicas to close that window. `cache.New` enables `ContextTimeoutEnabled` so callers' context deadlines (including that publish deadline) actually bound Redis operations.

**Warehouse runbook**: *Restore*: after restoring Postgres, enable `AETHER_CH_TABLE_PERMISSIONS`; the startup enqueue reconciles every warehouse, re-creating missing users/roles/grants and re-keying passwords if the master key changed (`warehouses.applied_master_fp`). *Provisioner rotation*: update the provisioner connector's credential; the next reconcile uses it. Rotating `AETHER_MASTER_KEY` re-keys every identity on the next reconcile (no per-user secrets are stored), but it also breaks decryption of every stored connector credential: re-enter the provisioner credential before reconcile can run. *Drift*: reconcile and the drift check emit `warehouse.drift` audit events; a wildcard or unexpected grant sets `sync_status='error'` and blocks managed execution until manually revoked (no auto-heal). *Deleting with the kill switch off* leaves ClickHouse identities behind: a `warehouse.identities.cleanup` audit with `deferred: true` is written; drop them manually (`DROP USER`/`DROP ROLE` matching `aether_<wh8>_%`).

**Dashboard query widgets**: widgets may own SQL + a connector (`widgets.connector_id/query/language`) instead of referencing a notebook cell. Dashboard variables live in `dashboards.settings.variables` and are interpolated server-side with type-aware escaping (`internal/dashboard`), never client-side. `POST /dashboards/{id}/execute` runs one widget as the viewer via the shared `openQuery` helper and caches successful results in Redis for `settings.query_cache_seconds` (default 30, 0 disables). The cache key is `sha256(org | accessFingerprint | cacheScope | connector | SQL | maxRows)`: the fingerprint is the viewer's effective warehouse table-grant union (user + groups + Everyone, length-prefixed canonical hash) for managed ClickHouse connectors and the constant `"unmanaged"` for connectors that execute with a shared stored credential. Fingerprint or permission errors fall back to a per-user key (`user:<id>`) — caching continues, entries are never shared on uncertainty — and a connector `use` pre-check runs before the cache read so a shared entry can never bypass authorization. `bypass_cache` (manual refresh/Run all/auto-refresh) skips the read but still writes the fresh result back to the shared entry, freshening it for everyone within the TTL. Concurrent identical misses are deduped by an in-process single flight (`Server.dashboardCacheSF`) per cache key; the shared computation is detached from caller cancellation (`context.WithoutCancel`) and bounded by the connector `timeout_seconds` with a 5-minute default (`dashboardQueryDefaultTimeout`; HTTP cell execution treats 0 as unlimited — deliberate divergence). Public dashboards run embedded queries only when `settings.public_live` is true, as the dashboard creator, rate-limited per token+IP (they keep the `token:<token>` scope and the `"public"` fingerprint).

**Live dashboards (Yjs)**: dashboards are live co-editing documents like notebooks. The relay document name is `dashboard:{uuid}`; the relay routes it to `GET/PUT /internal/dashboard-yjs/{id}` on the Go backend and calls `POST /internal/collab/authorize` at connect (dashboard `edit` ACL → read-write; `view`/`view_with_data` → read-only via `connectionConfig.readOnly`; **every non-200 rejects the connection — 403 is never mapped to read-only**, since a 403 means no access at all). A ~60s per-connection revalidation downgrades demoted editors to read-only, closes on 401/403/404, and retries transient failures so an API blip cannot evict live viewers. Store (`PUT`) merges the incoming update (`Y.applyUpdate`, idempotent) onto the stored state and materializes the derived `dashboards`/`widgets` rows in one transaction; guards refuse warning-tainted or empty projections over populated dashboards so a corrupt document cannot wipe one, and widgets with dangling connector/notebook/cell references are skipped per-widget. Backend-originated writes (REST widget add/delete, convert-to-query, settings/variables, agent tools) go through `internal/dashboarddoc` (seed → transaction → store → materialize) and publish `aether:dashboard-doc:{id}` for relay replicas to apply to loaded docs, with `aether:dashboard-doc-invalidate:{id}` on trash/purge/agent hard-delete (relays disconnect viewers and unload). Write-path split: layout drag/resize, SQL text, chart config, and title are direct browser→relay Yjs writes; widget add/delete, convert-to-query, and settings/variables stay REST (validated, audited) but are stored through the document. Viewers auto re-run affected query widgets on run-signature changes (per-widget ~2s debounce, through the shared cache above) and untouched viewers adopt changed variable defaults; public dashboards remain snapshot-based (no relay). Org admins without an explicit `edit` ACL entry cannot connect (internal relay routes carry no admin mode; REST editing still works through admin mode). The relay sends Bearer auth on load/store for notebook *and* dashboard docs (fixing a pre-existing notebook-Yjs 401 bug), and `github.com/reearth/ygo` is pinned at v1.51.5 (the prelim API is required for nested documents).

**Hocuspocus relay** fetches/stores Yjs document state via `/internal/yjs/{notebook_id}` on the Go backend (binary `application/octet-stream`). JWT auth is passed inside the Hocuspocus auth message, not as a URL param.

**Yjs as single source of truth** for cell content: Agent `update_cell` writes to Yjs first (via `ygo/crdt` Go library), then updates `cells.source` as a derived cache. The `agent_updated_at` column on `cells` suppresses frontend auto-save after agent updates. See `docs/designs/yjs-source-of-truth.md` for full architecture.

**SQL executor LIMIT behavior**: When a cell has a `limit` value > 0 and the query doesn't already contain `LIMIT`, the executor trims any trailing semicolon before appending ` LIMIT N`. This prevents `SELECT 1; LIMIT 1000` (broken) vs `SELECT 1 LIMIT 1000` (correct).

**Bounded outputs**: Every `Executor.Execute` takes an `executor.OutputLimits{MaxBytes, MaxRows}` (`<=0` = unlimited). Drivers accumulate a cheap per-row byte estimate and truncate at row boundaries when `MaxBytes` is exceeded, setting `truncated` / `rows_included` / `rows_total` (-1 when unknown) / `bytes` on the `ResultSet`. The per-cell cap is `orgs.cell_output_max_bytes` (default 10MB), clamped by the platform ceiling `AETHER_OUTPUT_LIMITS_MAX_BYTES` (default 64MB, `0` = no ceiling); org admins edit it under Settings → Cell Output Limits. Notebook GET embeds cell outputs as raw JSON (`models.Cell.Outputs` is `json.RawMessage`) and applies `orgs.notebook_inline_outputs_max_bytes` (default 32MB): cells that no longer fit get a `{"truncated": true, "bytes": N}` stub. Full payloads stream from `GET /api/v1/cells/{id}/outputs/download` (view permission on the owning notebook required), which the frontend links via a real navigation with `?token=` (Bearer auth can't ride an anchor).

**CodeMirror caret/cursor in dark theme**: The caret color is set globally via CSS at the `.cm-editor` and `.cm-editor .cm-content` level using `caret-color: var(--text-primary) !important` in `theme.css`. The CodeMirror `EditorView.theme()` extension should NOT set `caretColor` inline (inline values get `!important` injected by CodeMirror and override stylesheet rules). Use only `borderLeftColor` in the theme extension; use the stylesheet for `caret-color`.

**Agent tool timeouts**: Every builtin declares `ToolDef.Timeout`; `ask_question`, `spawn_subagents`, and the builtin `execute_sql` declare `NoTimeout`. Precedence for the rest: per-call arg (e.g. `run_cell`'s `timeout_ms`) → `tools.config.timeout_ms` DB override → registry default → `AETHER_AGENT_TOOL_TIMEOUT_DEFAULT`. All four dispatch paths use `ToolDef.Execute`; timeouts surface as `tool "<name>" timed out after <duration>`. `execute_sql` enforces its own budget in-handler (`sqlTimeoutBudget`, `internal/agent/tools_sql.go`), so the `tools.config.timeout_ms` DB override does not apply to it: agent paths get the historical 30s default, MCP callers get `AETHER_MCP_SQL_TIMEOUT_MS` as the ceiling, and a per-call `timeout_ms` arg is clamped to that ceiling. The dynamic `sql_query` tool keeps its 30s default (still overridable via `tools.config.timeout_ms`). Use `run_cell`/`create_cell(run=true)` for long queries (connector `timeout_seconds` applies; 5m when 0/unset; arg cap 10m).

**MCP authentication**: `POST /api/v1/mcp` accepts session JWTs, PATs (`aether_tok_…`), and OAuth access tokens (JWTs carrying `client_id`). PATs and session JWTs keep the full 45-tool allowlist; OAuth tokens are valid only at that exact path with `aud` matching the request's canonical resource URI (RFC 8707), are rejected on `/api/v1/mcp-servers`, the `/internal/*` relay endpoints, admin mode, and `?token=`, and their tool surface is narrowed by the granted `mcp:query`/`mcp:read`/`mcp:write` scopes (fail-closed; the allowlist remains the outer boundary). The authorization-server endpoints are flag-gated by `AETHER_MCP_OAUTH_ENABLED` (default off) and documented in `docs/mcp-oauth.md`; `execute_sql`'s MCP timeout ceiling is `AETHER_MCP_SQL_TIMEOUT_MS`.

## OIDC / SSO

OIDC providers are loaded dynamically from the database. SSO routes are disabled when no providers are configured (e.g., in tests). OAuth2 state is a random token; callback validates it from a short-lived cookie.

## Frontend

- React + React Query (`@tanstack/react-query`) for data fetching
- Cell sources auto-save with 1.5s debounce after keystroke (suppressed for 5s after agent updates via `agent_updated_at` check)
- Markdown cells persist on blur via `PUT /cells/:id`
- Real-time collaboration: `HocuspocusProvider` in `Cell` connects to relay on `:3001`
- Dev config injection: the Vite dev server does not inject `window.__AETHER_CONFIG__` (so `relayUrl` is missing), meaning browser collab in a bare `npm run dev` session requires injecting the config yourself or using the API-served build; the e2e specs inject it via `context.addInitScript`.
- Yjs document key convention: `cell:{cellID}` for each cell's text content
- **Resource catalog pages follow one pattern** (Connectors is the reference): create/edit opens the shared `FormModal` (`web/src/components/FormModal.tsx`) instead of an inline `FormCard`; row actions are icon buttons from `web/src/components/RowActions.tsx` (Test + Edit above Permissions + Delete, danger hover on delete); delete goes through `ConfirmDialog`; permissions through `PermissionsPanel`. Keep Models, Tools, Skills, MCP Servers, Agents, Warehouses, and Dashboards aligned with it.

**Real-browser validation is mandatory for every UI change.** After the dev stack is up, validate the changed flows with agent-browser (`agent-browser open`, `snapshot -i`, interact, `screenshot`, `errors`) — component tests alone are not sufficient.

### Debugging with agent-browser

When the screen is blank or components aren't rendering, check for console errors:

```bash
agent-browser errors          # View page errors
agent-browser console         # View console logs (includes React errors)
agent-browser console --clear # Clear console before testing
```

**Common issues:**
- Blank screen after editing: Usually a missing import or variable scope error. Check console for React component errors.
- Vite cache issues: Restart the web container with `docker compose -f docker-compose.dev.yml restart web`
- TypeScript errors: Run `cd web && npx tsc --noEmit` to check for type errors

### Frontend Development (AI-Assisted)

See `FRONTEND.md` for comprehensive visual documentation including:
- Design system (colors, typography, spacing)
- Component descriptions (visual appearance, states, interactions)
- Common UI patterns (cards, buttons, forms)
- Accessibility guidelines
- Visual regression testing workflow

**When implementing UI changes:**
1. **Describe specifically**: Use exact values (px, colors from theme, border-radius)
2. **Reference similar components**: "Similar to CodeCell but with X difference"
3. **Use visual tests**: Add/update Playwright snapshot tests for visual changes
4. **Follow conventions**: Use CSS variables (`var(--accent)`), not hardcoded values

**Visual regression tests:**
```bash
npx playwright test --config=e2e/playwright.config.ts   # Run all E2E tests
npx playwright test --update-snapshots     # Update snapshots
```

## Agent Token Tracking

Token consumption is tracked across the session and displayed in the agent panel's info bar (clickable for detailed breakdown). The backend sends actual `prompt_tokens`/`completion_tokens` from the API response. `reasoning_tokens` from `completion_tokens_details` is tracked when the provider returns it. `cached_tokens` from `prompt_tokens_details` is tracked as `cache_read`.

A per-component estimate (system prompt, history, user message, tool definitions, tool calls, tool results) is shown separately under "Estimated (tiktoken)" using `github.com/pkoukk/tiktoken-go`. The model-to-encoding mapping is in `internal/agent/tokens.go`.

**Price units**: `model_configs.price_per_input_token` / `price_per_output_token` / `price_per_cache_read_token` are dollars per **1M tokens** (the unit the model-config UI labels and the session panel divides by). `RollupHourlyStats` must divide token counts by `1e6` when computing `agent_stats_hourly.est_cost_usd`; `V106__fix_agent_stats_hourly_cost.sql` repaired pre-fix rows that skipped the division. Cost surfaces use `formatCost` (tiered: 4 decimals sub-$1, 2 above, compact `k`/`M` for large totals) and integer counts are formatted with a pinned `'en-US'` locale so they stay consistent beside the compact token formatters.

Chat messages now include `created_at` timestamps displayed as muted text at the top of each message bubble.

## Reasoning Effort

Reasoning effort is configurable per-chat via a dropdown in the agent info bar. The `default_params` JSONB on `model_configs` stores:
- `reasoning_effort_options`: array of effort levels (e.g., `["low", "medium", "high"]`)
- `reasoning_effort`: the default effort level pre-selected in chat

The selected effort is sent to the backend via a `set_reasoning_effort` WS message, stored per-session in a `sync.Map` on the Engine, and merged into the LLM API request body via `ChatRequest.Extra` + custom `MarshalJSON`.

## Tool Call Permissions

The `ToolDef` struct has a `ConfirmRequired bool` field. When set, the backend sends a `tool_confirm_required` WS event and waits for user approval on a channel. The frontend shows a confirmation dialog with a character-level diff for `update_cell`. An "Auto-Approve" checkbox in the agent info bar bypasses the dialog.

The confirm flow: backend → `tool_confirm_required` event → frontend shows dialog → user approves/denies → frontend sends `tool_confirm` → backend executes or skips the tool.

### Headless sessions (`auto_approve_tools` / `auto_answer_questions`)

Both are columns on `agent_sessions`, set only at session creation via `POST /agents/{id}/session`, and default to `false`. They exist because a session with no interactive client otherwise blocks until the context deadline — the backend emits its event and waits on a channel nobody is listening to.

| Flag | Effect when `true` |
|---|---|
| `auto_approve_tools` | Engine resolves `tool_confirm_required` itself, approving the call |
| `auto_answer_questions` | Engine resolves `ask_question` itself, answering that no interactive user is reachable — it does **not** pick one of the offered options |

They are independent: approving a tool the agent already chose is a far smaller step than proceeding with no human input at all, so setting one never implies the other. Forked sessions (`summarizeAndNewSession`) inherit both from the parent.

**These are unrelated to the frontend's "Auto-Approve" checkbox**, which is ephemeral React state (`autoConfirmTool`, defaults to `true`) that replies to the WS event from the browser. The browser never sends these flags, so UI-created sessions persist `false` for both. Do not wire the checkbox to these columns — it would turn a resettable, default-on UI preference into a durable database bit.

## DB Migration

Agent updates (`agent_updated_at`) now also update the local cell cache via WebSocket broadcast when `user_email` is `agent@aether`. This ensures cell content changes made by the agent appear without requiring a page refresh.

## Git & Engineering Workflow

### Branch Strategy

- **`main`** — stable, always deployable. All changes land here via PRs only.
- **NEVER commit directly to `main`** — always use a feature branch and PR, even for small fixes.
- **Feature branches** — branch from `main`, named `feat/<short-description>` or `fix/<short-description>`.
- Squash-merge PRs into `main` with a descriptive commit message.

### PR Workflow

1. Create feature branch from `main`
2. Implement with frequent commits
3. **Before opening a PR, run all CI checks locally** to catch errors early:
   - `task check` (Go: fmt + vet + tidy + test)
   - `cd web && npx tsc --noEmit` (frontend TypeScript)
   - `cd web && npm run build` (frontend build — uses `tsc -b` which is stricter than `--noEmit`)
   - `cd relay && npm run build` (relay build)
   - `task test:e2e` (smoke tests against live server)
4. Ensure CI passes (Go tests, frontend build, relay build, smoke tests)
5. Squash-merge when approved

### Release Process

Releases are fully automated via **goreleaser** — no manual steps.

```bash
# 1. Ensure main has all desired changes merged
# 2. Tag and push (triggers .github/workflows/release.yml)
git tag v<major>.<minor>.<patch>
git push origin v<major>.<minor>.<patch>
```

The release workflow:
- Builds the frontend (`npm run build`)
- Cross-compiles `aether-server` and `aether` CLI for linux/darwin × amd64/arm64
- Builds & pushes Docker image to `ghcr.io/the-heaven-labs/aether-server` (`:<tag>` and `:latest`)
- Creates a GitHub Release with all archives and checksums

The `v0.1.1` release is already published — tag `v0.2.0` for the next one.

### Changelog

The changelog is auto-generated by [git-cliff](https://git-cliff.org) from conventional commit messages during the release workflow. It's configured in `cliff.toml` at the project root.

To generate locally for testing:
```bash
git-cliff -o CHANGELOG.md --latest --strip header
```

### Local Dev Commands

```bash
task infra:up          # Start Postgres + Redis + ClickHouse (for tests)
task dev               # Run Go API server
task dev:web           # Vite frontend
task dev:relay         # Hocuspocus relay
task check             # fmt + vet + tidy + test
```

### History Cleanup

If a file was accidentally committed that should be ignored (e.g., `CLAUDE.md`, `IMPROVEMENTS.md`):

1. Add to `.gitignore`
2. `git filter-repo --path <filename> --invert-paths --force`
3. `git remote add origin <url> && git push origin main --force`

This rewrites history — coordinate with the team before doing it.

## Code Style

### Go
- Format with `gofmt` (no exceptions). Run `task fmt` before committing.
- Standard library `net/http ServeMux` — no frameworks (gin, chi, etc.).
- Error handling: use `errors.Is` / `errors.As` for sentinel checking, not `==`.
- Structs grouped by purpose; exported types get doc comments.
- Tests: `testing` package + `testify/require` or `testify/assert`. No mocking — tests hit a real database.

Example (`internal/api/permissions.go:23-30`):
```go
var resourceTable = map[string]string{
    "notebook":     "notebooks",
    "connector":    "connectors",
    "dashboard":    "dashboards",
    "agent":        "agents",
    "model_config": "model_configs",
    "skill":        "skills",
    "mcp_server":   "mcp_servers",
}
```

### TypeScript / React
- Functional components with hooks — no class components.
- Explicit typing everywhere — avoid `any` unless unavoidable (use `unknown` + type guard).
- Imports: `react` first, then third-party, then local modules (no blank-line rule enforced).
- CSS via CSS variables (`var(--accent)`, `var(--text-primary)`) — no hardcoded colors.
- Test files co-located with components (e.g., `Cell.test.tsx` beside `Cell.tsx`) using Vitest.

Example (`web/src/components/Cell.tsx:1-4`):
```tsx
import { lazy, Suspense, useState, useEffect, useRef, useMemo, useCallback, memo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Play, Loader2, ChevronUp, ChevronDown, Eye, EyeOff } from 'lucide-react'
```

### Database
- Migrations in `migrations/` with Flyway-compatible naming (`V<number>__<description>.sql`).
- Applied automatically on server startup — no manual migration tool.
- All schema changes get a new migration file; do not edit existing migrations.

## Boundaries

| Tier | What |
|---|---|
| **Always** | Run `task check` before pushing. Write tests for new handlers and executors. Use the `aether` CLI for API interactions. Follow existing patterns (router, middleware, error handling). |
| **Ask first** | Introducing new major dependencies, changing the DB schema in a non-backward-compatible way, adding new services to the Docker Compose stack, modifying the CI pipeline or release process. |
| **Never** | Commit secrets, API keys, or tokens. Force-push to `main`. Bypass ACL checks or introduce auth bypasses. Edit a migration that has already been applied in a release. Use `any` in TypeScript without a documented reason. |
