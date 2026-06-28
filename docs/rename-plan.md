# Rename Plan: hnb → Aether

**Brand**: "Aether" (short) / "Aether Notebooks" (full)
**Binaries**: `aether-server`, `aether` (CLI)
**Module**: `github.com/the-heaven-labs/aether`
**Env prefix**: `AETHER_`
**Database**: user `aether`, pass `aether_dev`, db `aether`

---

## Phase 0: Preparation

- [ ] Create a new git branch: `git checkout -b rename-aether`
- [ ] Rename directories: `cmd/hnb/` → `cmd/aether/`, `cmd/hnb-server/` → `cmd/aether-server/`
- [ ] Copy logo files to new names if desired
- [ ] Update `.gitignore` paths (`/hnb` → `/aether`, `/hnb-server` → `/aether-server`)

---

## Phase 1: Go Module & Imports (core, then cascade)

**Dependency**: Phase 0 (directory renames)

### go.mod
- `module github.com/heavenlabs/hnb` → `module github.com/the-heaven-labs/aether`

### All `.go` files (~60 files, ~250 import lines)
- `github.com/heavenlabs/hnb/...` → `github.com/the-heaven-labs/aether/...`

**Strategy**: Use `find` + `sed` for the bulk replace on imports, then handle edge cases manually:
```bash
find . -name '*.go' -exec sed -i 's|github\.com/heavenlabs/hnb|github.com/the-heaven-labs/aether|g' {} +
```

### Package declarations (cmd/)
- `cmd/aether/main.go`: `package main` (no change needed, just directory rename)
- `cmd/aether-server/main.go`: `package main` (same)

---

## Phase 2: CLI & Binary References

**Dependency**: Phase 1 (go.mod must be valid)

| File | Change |
|---|---|
| `cmd/aether/main.go` | `Use: "hnb"` → `Use: "aether"`, `Short: "Heaven's Notebooks CLI"` → `Short: "Aether Notebooks CLI"` |
| `cmd/aether-server/main.go` | `hnb-server listening on` → `aether-server listening on` |
| `Taskfile.yml` | `SERVER_BIN: "{{.BIN_DIR}}/hnb-server"` → `SERVER_BIN: "{{.BIN_DIR}}/aether-server"`, `CLI_BIN: "{{.BIN_DIR}}/hnb"` → `CLI_BIN: "{{.BIN_DIR}}/aether"`, build commands, docker tag `hnb:latest` → `aether:latest` |
| `Makefile` | Binary names, build targets |
| `.air.toml` | `./cmd/hnb-server` → `./cmd/aether-server` |
| `Dockerfile` | `go build -o /hnb-server` → `go build -o /aether-server`, `COPY ... /usr/local/bin/`, `CMD ["hnb-server"]` |
| `internal/cli/client.go` | `~/.hnb/` → `~/.aether/`, error messages |

---

## Phase 3: Environment Variables & Config

**Dependency**: Phase 1

### `internal/config/config.go`
- All `HNB_*` env var names → `AETHER_*`
- Config struct field tags
- Default `postgres://hnb:hnb_dev@localhost:5432/hnb` → `postgres://aether:aether_dev@localhost:5432/aether`

### All test files referencing `HNB_*` env vars
- `internal/api/testhelpers_test.go`
- `internal/audit/audit_test.go`
- `internal/sso/sso_test.go`
- `internal/database/database_test.go`
- `internal/agent/tools_notebook_test.go`

### All shell scripts
- `scripts/smoke-test.sh`: `HNB_URL` → `AETHER_URL`
- `scripts/sso-e2e-test.sh`: `HNB_URL` → `AETHER_URL`
- `scripts/permission-review/test-api.sh`: `HNB_API_URL` → `AETHER_API_URL`
- `e2e/smoke_test.sh`: `HNB_API_URL` → `AETHER_API_URL`

---

## Phase 4: Docker & Infrastructure

**Dependency**: Phase 1-2

| File | Change |
|---|---|
| `docker-compose.dev.yml` | `name: hnb-dev` → `name: aether-dev`; service names `hnb-postgres` → `aether-postgres`, etc.; env vars `HNB_*` → `AETHER_*`; default DB URL; depends_on refs |
| `docker-compose.yml` | Same pattern — service names, env vars, DB URL |
| `Taskfile.yml` psql commands | `hnb-claude-postgres-1` → `aether-postgres-1`, `-U hnb -d hnb` → `-U aether -d aether` |

---

## Phase 5: Frontend — Brand Names & Page Titles

**Dependency**: None (independent of Phases 1-4)

### `web/index.html`
- `<title>Heaven's Notebooks</title>` → `<title>Aether Notebooks</title>`
- `hnb_theme` localStorage ref → `aether_theme`

### Page titles (~18 files)
All `document.title = "... — Heaven's Notebooks"` → `"... — Aether Notebooks"`
All `document.title = "... — hnb"` → `"... — Aether"`

### Brand marks in UI
- `LoginPage.tsx`: `<h1>Heaven's<br />Notebooks</h1>` → `<h1>Aether</h1>` (or `<h1>Aether<br />Notebooks</h1>`)
- `OrgOnboardingPage.tsx`: `"Welcome to Heaven's Notebooks"` → `"Welcome to Aether Notebooks"`
- `PublicNotebookPage.tsx`: `<span>hnb</span>` → `<span>Aether</span>`, `"Powered by hnb"` → `"Powered by Aether"`
- `PublicDashboardPage.tsx`: Same
- `TopBar.tsx`: `<span>HNB</span>` → `<span>Aether</span>`

---

## Phase 6: Frontend — localStorage Keys

**Dependency**: None (but coordinate with Phase 5 to avoid merge conflicts)

All `hnb_*` and `hnb:*` localStorage keys → `aether_*` / `aether:*`

**Strategy**: grep for all occurrences and bulk-replace. Files affected include:
- `web/src/api/client.ts`
- `web/src/hooks/useAuth.ts`
- `web/src/components/App.tsx`
- `web/src/components/TopBar.tsx`
- `web/src/components/Sidebar.tsx`
- `web/src/components/Cell.tsx`
- `web/src/components/AgentPanel.tsx`
- `web/src/components/AppShell.tsx`
- `web/src/pages/LoginPage.tsx`
- `web/src/pages/AdminPage.tsx`
- etc. (~15 files)

**Important**: The key values are quoted strings — safe to bulk replace.

---

## Phase 7: Frontend — CSS & Events

| File | Change |
|---|---|
| `web/src/index.css` | `.aether-hamburger` → `.aether-hamburger` |
| `web/src/components/TopBar.tsx` | `className="hnb-hamburger"` |
| `web/src/components/Cell.tsx` | `'aether-collab'` event |
| `web/src/components/Sidebar.tsx` | `'aether-sidebar-mobile-open'` event |
| `web/.storybook/preview.tsx` | `'aether-light'`, `'aether-dark'` → `'aether-light'`, `'aether-dark'` |

---

## Phase 8: Frontend — Agent Email & Headers

| File | Change |
|---|---|
| `web/src/components/CollaboratorAvatars.tsx` | `'agent@aether'` → `'agent@aether'` |
| `web/src/api/client.ts` | `'X-AETHER-Admin-Mode'` → `'X-AETHER-Admin-Mode'` |

---

## Phase 9: Backend — Agent Email, Tokens, Sentinels

| File | Change |
|---|---|
| `internal/agent/tools_notebook.go` | `"user_email": "agent@aether"` (11 occurrences) → `"user_email": "agent@aether"` |
| `internal/agent/tools_chart.go` | Same (2 occurrences) |
| `internal/api/token_handlers.go` | `"aether_tok_"` prefix → `"aether_tok_"` |
| `internal/api/middleware.go` | `HasPrefix(token, "aether_tok_")` |
| `internal/api/tool_handlers.go` | `"aether-tool-probe"` → `"aether-tool-probe"` |
| `internal/api/tool_handlers.go` | `"__AETHER_REDACTED__"` → `"__AETHER_REDACTED__"` |

---

## Phase 10: Vite Config

| File | Change |
|---|---|
| `web/vite.config.ts` | `allowedHosts: ['.hnb.test']` → `allowedHosts: ['.aether.test']` |

---

## Phase 11: SSO / Keycloak Realm

**Dependency**: None (dev config only, but wide-reaching)

### `dev/keycloak-realm.json`
- realm name: `hnb-dev` → `aether-dev`
- clientId: `hnb-dev` → `aether-dev`
- client secret: `hnb-dev-keycloak-secret` → `aether-dev-keycloak-secret`
- groups: `hnb-analysts` → `aether-analysts`, `hnb-engineering` → `aether-engineering`
- user emails: `@hnb-dev.test` → `@aether-dev.test`
- group groupPrefixPaths: `/hnb-...` → `/aether-...`

### Test files
- `internal/api/oidc_handlers_test.go`: `"hnb-admins"`, `"hnb-team"`, `GroupPrefix: "hnb-"` etc.
- `internal/api/sso_group_sync_test.go`: Same
- `internal/sso/sso_test.go`: `GroupPrefix: "hnb-"` → `GroupPrefix: "aether-"`

### Shell scripts
- `scripts/sso-e2e-test.sh`: `KC_REALM="hnb-dev"`, client_id, secrets, group_prefix, domain refs

### Docs
- `AGENTS.md`: SSO section updates
- `docs/SSO_GROUP_PROVISIONING.md`: Update all examples

### UI placeholders
- `AdminPage.tsx`: `placeholder="hnb-"` → `placeholder="aether-"`
- `OrgSettingsPage.tsx`: Same

---

## Phase 12: Subdomain Testing Config

| File | Change |
|---|---|
| `web/vite.config.ts` | `allowedHosts: ['.hnb.test']` → `allowedHosts: ['.aether.test']` |
| `internal/api/middleware_test.go` | `slug + ".hnb.test"` → `slug + ".aether.test"` |
| `AGENTS.md` | Subdomain section: `hnb.test` → `aether.test`, `org1.hnb.test` → `org1.aether.test` |

---

## Phase 13: Documentation

**Dependency**: All phases (docs reference everything)

Systematic update of:
- `AGENTS.md` — the big one (~40 occurrences of "hnb", env vars, commands, subdomains)
- `FRONTEND.md` — localStorage key references
- `IMPROVEMENTS.md` — "hnb" mentions
- `logo-philosophy.md` — "Heavens Notebooks" → "Aether Notebooks" (ideally)
- `relay/README.md` — `# hnb Relay` → `# Aether Relay`
- `web/README.md` — Same
- `dev/README.md` — Service names, commands
- `docs/designs/tools-skills-split.md` — "hnb" refs
- `docs/plans/*.md` — All references

---

## Phase 14: Swagger / API Docs (regenerate)

**Dependency**: Phase 1

### `cmd/aether-server/main.go` (swagger annotations)
- `// @title hnb API` → `// @title Aether API`
- `// @description Heaven's Notebooks API...` → `// @description Aether Notebooks API...`

### Regenerate
```bash
swag init -g cmd/aether-server/main.go -o internal/api/docs
```

---

## Phase 15: Verify

- [ ] `go build ./...` — all Go compiles
- [ ] `go vet ./...` — no vet issues
- [ ] `task test` — all tests pass
- [ ] `cd web && npx tsc --noEmit` — TypeScript clean
- [ ] `cd web && npm run build` — frontend builds
- [ ] `cd relay && npm run build` — relay builds
- [ ] `docker compose -f docker-compose.dev.yml up -d` — stack starts

---

## Summary Table

| Phase | Scope | Approx. Files | Risk |
|---|---|---|---|
| 0 | Directory renames | ~5 (git mv) | Low |
| 1 | Go module + imports | ~60 .go files | **High** (must compile) |
| 2 | CLI + binary refs | ~10 files | Low |
| 3 | Env vars + config | ~15 files | Low |
| 4 | Docker infra | ~4 files | Medium |
| 5 | Frontend brand names | ~22 files | Low |
| 6 | Frontend localStorage | ~15 files | Low |
| 7 | CSS + events | ~4 files | Low |
| 8 | Agent email + headers | ~2 files | Low |
| 9 | Backend tokens/sentinels | ~4 files | Low |
| 10 | Vite config | 1 file | Low |
| 11 | SSO / Keycloak | ~10 files | Medium |
| 12 | Subdomain config | ~4 files | Low |
| 13 | Documentation | ~25 files | Low |
| 14 | Swagger regenerate | ~4 files | Low |
| 15 | Verify | — | Critical |

**Estimated total**: 15 phases, ~100+ files, ~800-1000 individual changes.
