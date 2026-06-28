# hnb — Claude Code Context

**hnb** is a collaborative SQL/data notebook platform (think Jupyter for analytics). It has a Go API server, a React frontend, and a Hocuspocus relay for real-time collaborative editing via Yjs.

## Architecture

```
cmd/hnb-server      → Go API server (port 8080)
cmd/hnb             → CLI client
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

For local multi-tenancy testing with subdomains (`org1.hnb.test` → Org 1):

1. Add to `/etc/hosts`:
   ```
   127.0.0.1  hnb.test
   127.0.0.1  org1.hnb.test org2.hnb.test
   ```

2. Create orgs with slugs matching subdomains:
   ```bash
   # Via API
   curl -s -X POST http://localhost:8080/api/v1/auth/org/create \
     -H "Authorization: Bearer $TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"name": "Org 1", "slug": "org1"}' | jq .
   ```

3. Visit `http://org1.hnb.test:5173` — the app resolves the org from the subdomain.

## Commands

**Preferred task runner: `task` (Taskfile.yml). `make` provides equivalent commands independently.**

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
task dev:web           # Vite dev server (proxies /api → :8080)
task dev:relay         # Hocuspocus relay

# Build
task build             # Both Go binaries → ./bin/
task build:web         # React → web/dist/
task build:relay       # TypeScript relay → relay/dist/
task build:all         # Everything

# Testing
task test              # All Go tests (starts infra first)
task test:v            # Verbose
task test:api          # Only internal/api/... tests
task test:race         # With race detector
task test:e2e          # Smoke test against live server

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
|---|---|---|---|
| `HNB_MASTER_KEY` | **yes** | — | AES key for encrypting connector credentials |
| `HNB_JWT_SECRET` | **yes** | — | JWT signing secret |
| `HNB_DATABASE_URL` | no | `postgres://hnb:hnb_dev@localhost:5432/hnb?sslmode=disable` | |
| `HNB_REDIS_URL` | no | `redis://localhost:6379` | |
| `HNB_PORT` | no | `8080` | |
| `HNB_OIDC_HOST_REWRITE` | no | — | `from=to` pair for rewriting the OIDC discovery host (e.g. `localhost:5557=host.docker.internal:5557`). Used in Docker dev where the API container must reach Keycloak via a different hostname than what's in the discovery URL. |
| `HNB_PLATFORM_ADMIN_EMAIL` | no | — | Email of the user to auto-promote to platform admin on startup |
| `HNB_PUBLIC_URL` | no | `http://localhost:8080` | Public-facing URL for link generation |
| `HNB_FRONTEND_URL` | no | `http://localhost:5173` | Frontend URL for CORS and OIDC redirect |
| `HNB_ATTACHMENT_DIR` | no | `./attachments` | Directory for local file attachments |
| `HNB_STORAGE_BACKEND` | no | `local` | Storage backend type (`local` or `s3`) |
| `HNB_S3_ENDPOINT` | no | — | S3-compatible storage endpoint |
| `HNB_S3_BUCKET` | no | — | S3 bucket name |
| `HNB_S3_REGION` | no | `us-east-1` | S3 region |
| `HNB_S3_ACCESS_KEY` | no | — | S3 access key |
| `HNB_S3_SECRET_KEY` | no | — | S3 secret key |
| `HNB_MAX_ATTACHMENT_BYTES` | no | `10485760` | Maximum attachment file size in bytes |
| `HNB_TOOL_ALLOWED_DOMAINS` | no | — | Comma-separated list of allowed domains for webhook tools |
| `HNB_DISABLE_REGISTRATION` | no | `false` | If set to `true`, disables new user registration |

`Taskfile.yml` sets dev values for `HNB_DATABASE_URL`, `HNB_MASTER_KEY`, `HNB_JWT_SECRET`, and `HNB_PLATFORM_ADMIN_EMAIL` automatically when using `task`. Other vars rely on defaults or are set in `docker-compose.dev.yml`.

## Test Users

When using the dev stack (`docker-compose.dev.yml`), use these test users:

| Email | Password | Notes |
|---|---|---|
| `demon@heaven-labs.com` | `demon123` | Primary test user |
| `angel@heaven-labs.com` | `angel123` | Secondary test user |

**Note**: Home folders are named using the user's email address (e.g., `demon@heaven-labs.com`). This ensures uniqueness and avoids confusion with similar names.

## SSO / OIDC (Dev Stack)

The dev stack includes **Keycloak** as the OIDC identity provider (Dex was removed — Keycloak handles all OIDC testing including group provisioning).

On first startup, the server auto-seeds a **platform-level** Keycloak SSO provider. This appears in the Admin > SSO settings page. You can also create org-level providers via Org Settings > SSO.

### Provider Details

| Field | Value |
|---|---|
| Name | `Keycloak (Dev)` (auto-seeded, platform-level) |
| Issuer URL | `http://localhost:5557/realms/hnb-dev` |
| Client ID | `hnb-dev` |
| Client Secret | `hnb-dev-keycloak-secret` |
| Allowed Domains | `hnb-dev.test` |
| Scopes | `openid`, `profile`, `email` |

### Keycloak Test Users (from `dev/keycloak-realm.json`)

| Email | Password | Groups |
|---|---|---|
| `alice@hnb-dev.test` | `alice123` | hnb-analysts, all-employees |
| `bob@hnb-dev.test` | `bob123` | hnb-engineering |
| `charlie@hnb-dev.test` | `charlie123` | all-employees |
| `dave@hnb-dev.test` | `dave123` | hnb-engineering, all-employees |
| `eve@hnb-dev.test` | `eve123` | hnb-analysts |

**Admin console**: `http://localhost:5557` (admin / admin123)

### Docker Networking

The API server runs inside Docker and needs to reach Keycloak. The discovery URL uses `localhost:5557` (the host-facing port) so the browser redirects work correctly. Inside the container, `HNB_OIDC_HOST_REWRITE=localhost:5557=host.docker.internal:5557` rewrites the connection target to `host.docker.internal` while preserving the `Host` header. This is configured in `docker-compose.dev.yml` — no action needed.

## API Documentation (Swagger/OpenAPI)

The API documentation is auto-generated using [swag](https://github.com/swaggo/swag). To regenerate after adding/modifying endpoints:

```bash
swag init -g cmd/hnb-server/main.go -o internal/api/docs
```

This generates `docs/swagger.json` and `docs/swagger.yaml`. The docs are served at:
- `http://localhost:8080/docs` (direct API access)
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

## Roles & Admin

hnb has a **two-level admin system**: org-level and instance-level. All non-admin permissioning is handled exclusively through ACL entries (user, group, or "Everyone" subjects) — org roles no longer grant implicit permissions.

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

**Why it exists**: Platform admin is designed for the **host/SaaS operator** of an hnb instance. Use cases include:
- Viewing all orgs and their user counts (`GET /api/v1/admin/orgs`)
- Managing all users across orgs (`GET /api/v1/admin/users`)
- Promoting/demoting other platform admins (`PUT /api/v1/admin/users/{id}`)
- Configuring **platform-level SSO providers** that org admins can then enable for their org
- Managing instance-wide Message of the Day (MOTD)

**How platform admin is assigned**:
1. **Auto-promotion at startup**: Set `HNB_PLATFORM_ADMIN_EMAIL=envar` — the server promotes that user on startup and on registration.
2. **By another platform admin**: `PUT /api/v1/admin/users/{id}` with `{"is_platform_admin": true}`.
3. **Direct SQL**: `UPDATE users SET is_platform_admin=true WHERE email='...'`.

In dev, `Taskfile.yml` sets `HNB_PLATFORM_ADMIN_EMAIL: admin@heaven-labs.com` — so that user is auto-promoted.

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

**Groups**: Custom groups (`groups` + `group_members` tables) are first-class permission subjects. Group management (create/rename/delete/members) requires `admin` role; viewing groups is open to all members.

**Internal routes** (`/internal/*`) are unauthenticated by standard JWT middleware — they're called only by the Hocuspocus relay and validated via `handleInternalAuthValidate`. Do not add auth middleware to these.

**Migrations run automatically** on server startup (not a separate migration tool).

**Vite proxy**: In dev, Vite forwards `/api`, `/internal`, `/docs`, and `/swagger.json` to `localhost:8080`. The `API_URL` env var overrides the target (used inside Docker).

**Connector credentials** are AES-encrypted using `crypto.DeriveKey(masterKey)` before storing in Postgres.

**Hocuspocus relay** fetches/stores Yjs document state via `/internal/yjs/{notebook_id}` on the Go backend (binary `application/octet-stream`). JWT auth is passed inside the Hocuspocus auth message, not as a URL param.

**Yjs as single source of truth** for cell content: Agent `update_cell` writes to Yjs first (via `ygo/crdt` Go library), then updates `cells.source` as a derived cache. The `agent_updated_at` column on `cells` suppresses frontend auto-save after agent updates. See `docs/designs/yjs-source-of-truth.md` for full architecture.

**SQL executor LIMIT behavior**: When a cell has a `limit` value > 0 and the query doesn't already contain `LIMIT`, the executor trims any trailing semicolon before appending ` LIMIT N`. This prevents `SELECT 1; LIMIT 1000` (broken) vs `SELECT 1 LIMIT 1000` (correct).

**CodeMirror caret/cursor in dark theme**: The caret color is set globally via CSS at the `.cm-editor` and `.cm-editor .cm-content` level using `caret-color: var(--text-primary) !important` in `theme.css`. The CodeMirror `EditorView.theme()` extension should NOT set `caretColor` inline (inline values get `!important` injected by CodeMirror and override stylesheet rules). Use only `borderLeftColor` in the theme extension; use the stylesheet for `caret-color`.

## OIDC / SSO

OIDC providers are loaded dynamically from the database. SSO routes are disabled when no providers are configured (e.g., in tests). OAuth2 state is a random token; callback validates it from a short-lived cookie.

## Frontend

- React + React Query (`@tanstack/react-query`) for data fetching
- Cell sources auto-save with 1.5s debounce after keystroke (suppressed for 5s after agent updates via `agent_updated_at` check)
- Markdown cells persist on blur via `PUT /cells/:id`
- Real-time collaboration: `HocuspocusProvider` in `Cell` connects to relay on `:3001`
- Yjs document key convention: `cell:{cellID}` for each cell's text content

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

Chat messages now include `created_at` timestamps displayed as muted text at the top of each message bubble.

## Reasoning Effort

Reasoning effort is configurable per-chat via a dropdown in the agent info bar. The `default_params` JSONB on `model_configs` stores:
- `reasoning_effort_options`: array of effort levels (e.g., `["low", "medium", "high"]`)
- `reasoning_effort`: the default effort level pre-selected in chat

The selected effort is sent to the backend via a `set_reasoning_effort` WS message, stored per-session in a `sync.Map` on the Engine, and merged into the LLM API request body via `ChatRequest.Extra` + custom `MarshalJSON`.

## Tool Call Permissions

The `ToolDef` struct has a `ConfirmRequired bool` field. When set, the backend sends a `tool_confirm_required` WS event and waits for user approval on a channel. The frontend shows a confirmation dialog with a character-level diff for `update_cell`. An "Auto-Approve" checkbox in the agent info bar bypasses the dialog.

The confirm flow: backend → `tool_confirm_required` event → frontend shows dialog → user approves/denies → frontend sends `tool_confirm` → backend executes or skips the tool.

## DB Migration

Agent updates (`agent_updated_at`) now also update the local cell cache via WebSocket broadcast when `user_email` is `agent@hnb`. This ensures cell content changes made by the agent appear without requiring a page refresh.

## Planned Improvements

See `IMPROVEMENTS.md` for the full backlog of UX and feature improvements tracked by the product owner.
