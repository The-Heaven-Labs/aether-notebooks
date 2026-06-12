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

Services: API (Go), Relay (TypeScript), Web (Vite), Postgres, Redis, ClickHouse, OpenSearch

## Commands

**Preferred task runner: `task` (Taskfile.yml). `make` is a thin alias.**

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

`Taskfile.yml` sets dev values for all of these automatically when using `task`.

## Test Users

When using the dev stack (`docker-compose.dev.yml`), use these test users:

| Email | Password | Notes |
|---|---|---|
| `demon@heaven-labs.com` | `demon123` | Primary test user |
| `angel@heaven-labs.com` | `angel123` | Secondary test user |

**Note**: Home folders are named using the user's email address (e.g., `demon@heaven-labs.com`). This ensures uniqueness and avoids confusion with similar names.

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

**Roles**: `viewer`, `editor`, `admin`. Routes use `RequireRole("editor")` middleware. First registered user becomes org admin.

**Filesystem**: Folders live in `folders` table with self-referential `parent_id` (adjacency list). All resource types (notebooks, connectors, dashboards) have a nullable `folder_id`. Each user gets a personal home folder created on registration/org-join, seeded with a full-access ACL entry. Home folders use the user's email as the name (e.g., `user@example.com`).

**Permissions**: `acl_entries` table stores per-resource ACL. Resolution walks the ancestor folder chain via recursive CTE, ordered by specificity (resource entry beats parent folder beats grandparent; within same depth: user beats group beats org_role). Falls back to org-role defaults only when no ACL entry exists anywhere in the chain. Use `checkPermission(ctx, pool, orgID, userID, resourceType, resourceID, action)` from `internal/api/permissions.go`. Route middleware: `requirePermission(resourceType, idParam, action)`.

**Groups**: Custom groups (`groups` + `group_members` tables) are first-class permission subjects. Group management (create/rename/delete/members) requires `admin` role; viewing groups is open to all members.

**Internal routes** (`/internal/*`) are unauthenticated by standard JWT middleware — they're called only by the Hocuspocus relay and validated via `handleInternalAuthValidate`. Do not add auth middleware to these.

**Migrations run automatically** on server startup (not a separate migration tool).

**Vite proxy**: In dev, Vite forwards `/api` and `/internal` to `localhost:8080`. The `API_URL` env var overrides the target (used inside Docker).

**Connector credentials** are AES-encrypted using `crypto.DeriveKey(masterKey)` before storing in Postgres.

**Hocuspocus relay** fetches/stores Yjs document state via `/internal/yjs/{notebook_id}` on the Go backend (binary `application/octet-stream`). JWT auth is passed inside the Hocuspocus auth message, not as a URL param.

**Yjs as single source of truth** for cell content: Agent `update_cell` writes to Yjs first (via `ygo` Go library), then updates `cells.source` as a derived cache. The `agent_updated_at` column on `cells` suppresses frontend auto-save after agent updates. See `docs/designs/yjs-source-of-truth.md` for full architecture.

**SQL executor LIMIT behavior**: When a cell has a `limit` value > 0 and the query doesn't already contain `LIMIT`, the executor trims any trailing semicolon before appending ` LIMIT N`. This prevents `SELECT 1; LIMIT 1000` (broken) vs `SELECT 1 LIMIT 1000` (correct).

**CodeMirror caret/cursor in dark theme**: The caret color is set globally via CSS at the `.cm-editor` and `.cm-editor .cm-content` level using `caret-color: var(--text-primary) !important` in `theme.css`. The CodeMirror `EditorView.theme()` extension should NOT set `caretColor` inline (inline values get `!important` injected by CodeMirror and override stylesheet rules). Use only `borderLeftColor` in the theme extension; use the stylesheet for `caret-color`.

## OIDC / SSO

OIDC providers are configured at startup and stored in `Server.oidcProviders`. The `nil` value (in tests) disables SSO routes gracefully. OAuth2 state is a random token; callback validates it from a short-lived cookie.

## Frontend

- React + React Query (`@tanstack/react-query`) for data fetching
- Cell sources auto-save with 1.5s debounce after keystroke (suppressed for 5s after agent updates via `agent_updated_at` check)
- Markdown cells persist on blur via `PUT /cells/:id`
- Real-time collaboration: `HocuspocusProvider` in `CodeCell` connects to relay on `:3001`
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
npx playwright test                        # Run all E2E tests
npx playwright test --update-snapshots     # Update snapshots
```

## Planned Improvements

See `IMPROVEMENTS.md` for the full backlog of UX and feature improvements tracked by the product owner.
