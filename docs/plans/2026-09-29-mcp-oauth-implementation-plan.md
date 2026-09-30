# OAuth 2.1 for the Aether MCP Server — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let external harnesses (OpenCode, Claude, ChatGPT connectors) connect to Aether's MCP server via the MCP Authorization spec (OAuth 2.1) instead of hand-pasted PATs, and give them a configurable `execute_sql` timeout ceiling.

**Architecture:** Aether plays both OAuth roles — resource server (the existing `POST /api/v1/mcp` endpoint) and authorization server (new discovery/DCR/token endpoints in the same process, reusing the SPA session for the consent step). Access tokens are short-lived JWTs with `aud` (RFC 8707 resource binding) and `scope` claims; refresh tokens are stateful, hashed, and rotate with reuse detection. Tool access is narrowed by scopes on top of the existing MCP allowlist.

**Tech Stack:** Go stdlib `net/http` (Go 1.22 pattern routing), `github.com/golang-jwt/jwt/v5`, `github.com/jackc/pgx/v5`, `github.com/stretchr/testify`; React SPA for the consent page. Repo: upstream `The-Heaven-Labs/aether-notebooks` — all paths below are relative to its root; module path `github.com/the-heaven-labs/aether`.

**Design doc:** `2026-09-29-mcp-oauth-design.md` (same directory; decisions are restated inline here).

**Key upstream facts this plan relies on (verified at commit `5e003fd`):**

- MCP endpoint: `internal/api/mcp.go` — `handleMCP` (JSON-RPC over POST), `handleMCPToolsList`, `handleMCPToolsCall` (constructs `agent.ToolContext` and calls `def.Execute`, `internal/api/mcp.go:292-311`), allowlist `mcpToolAllowlist` (`internal/api/mcp.go:57`).
- Routes: `internal/api/router.go:649-662` (MCP server CRUD + `POST /api/v1/mcp`); rate limits read inline from env at `internal/api/router.go:355-366` (`AETHER_RATE_LIMIT_LOGIN`/`AETHER_RATE_LIMIT_REGISTER`); limiter `s.rateLimit(rateLimitConfig{keyFunc: clientIP, limit, window})` in `internal/api/ratelimit.go`.
- Auth: `internal/api/middleware.go` `AuthMiddleware` — PATs by `aether_tok_` prefix, otherwise `issuer.Validate`; subdomain org match at `middleware.go:68`; claims via `ClaimsFromContext` / `OrgIDFromContext`.
- JWT: `internal/auth/jwt.go` — `Claims{uid, oid, role, is_platform_admin}` (HS256), `JWTIssuer.Validate`, `jwt.ClaimStrings.Audience.Contains`.
- Tool timeouts: `internal/agent/types.go:266-291` — `ToolDef.Execute` wraps with `Timeout`, overridable by `TimeoutFromArgs(args)`; `NoTimeout` (-1) exempts a tool from wrapping. `execute_sql` declares `Timeout: 30 * time.Second` at `internal/agent/tools_notebook.go:181`.
- `execute_sql` handler: `internal/agent/tools_sql.go:150-182` (`makeExecuteSQLHandler`), execution via `executeAgentSQL(tc, pool, connectorID, query, params, limit)` (`tools_sql.go:97`) which uses `tc.Context`.
- Config: `internal/config/config.go` — `Config` struct + `load()` + per-var parse helpers (pattern: `parseAgentToolTimeoutDefault`).
- Migrations: `internal/database/migrations/` — highest is `V120__warehouse_hidden_table_patterns.sql`; new files start at `V121`.
- Server wiring: setter pattern (`internal/api/router.go:144-188`, e.g. `SetOutputLimitsMaxBytes`); `NewServer(db, jwt, auditLogger, masterKey, redisCache)`; embedded SPA served via `s.frontendHandler`.
- Tests: `internal/api/testhelpers_test.go` — `setupTestServer(t)`, `registerAndGetToken`, `createConnector`, `doMCPRequest`, `mcpResultText`; agent tests use `setupEngineTestDB(t)` / `newTestEngine(db)`.
- Users/roles: `org_members(org_id, user_id, role)` with roles `admin|editor|viewer`; the SPA keeps the JWT in `localStorage` (`getToken()` at `web/src/api/client.ts:4`) and there is no session cookie — so the authorize/consent step is SPA-mediated (Task 10).

**Branches:** one branch per part, conventional commits, MRs into `main`.

- Part A → `feat/mcp-sql-timeout-ceiling`
- Part B → `feat/mcp-oauth-server`
- Part C → `feat/mcp-oauth-consent-ui`

---

## Part A — `execute_sql` configurable timeout (PR 1)

### Task 1: `sqlTimeoutBudget` helper with unit tests

**Files:**
- Modify: `internal/agent/tools_sql.go`
- Test: `internal/agent/tools_sql_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/agent/tools_sql_test.go` (keep the package/imports already present; add `"time"` and `"github.com/stretchr/testify/require"` if missing):

```go
func TestSQLTimeoutBudget(t *testing.T) {
	cases := []struct {
		name    string
		ceiling time.Duration
		argMs   int
		want    time.Duration
	}{
		{"zero ceiling falls back to 30s", 0, 0, 30 * time.Second},
		{"zero arg uses ceiling", 10 * time.Minute, 0, 10 * time.Minute},
		{"negative arg uses ceiling", 10 * time.Minute, -5, 10 * time.Minute},
		{"arg below ceiling honored", 10 * time.Minute, 45000, 45 * time.Second},
		{"arg above ceiling clamped", 10 * time.Minute, 900000, 10 * time.Minute},
		{"arg clamped to fallback ceiling", 0, 60000, 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sqlTimeoutBudget(tc.argMs, tc.ceiling))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestSQLTimeoutBudget -v`
Expected: FAIL — `undefined: sqlTimeoutBudget`

- [ ] **Step 3: Write minimal implementation**

Add near the top of `internal/agent/tools_sql.go` (ensure `context` and `time` imports):

```go
// defaultSQLToolTimeout is the historical execute_sql tool budget. It applies
// when the caller provides no ceiling (in-Aether agent paths), preserving the
// guardrail that pushes agents toward notebook cells for long queries.
const defaultSQLToolTimeout = 30 * time.Second

// sqlTimeoutBudget resolves the effective execution budget for an execute_sql
// call: the caller's timeout_ms when positive, clamped to the caller's
// ceiling. A zero ceiling falls back to defaultSQLToolTimeout. The MCP handler
// sets a larger ceiling (AETHER_MCP_SQL_TIMEOUT_MS) so harness callers can run
// longer queries; agent callers never do.
func sqlTimeoutBudget(timeoutMs int, ceiling time.Duration) time.Duration {
	if ceiling <= 0 {
		ceiling = defaultSQLToolTimeout
	}
	if timeoutMs <= 0 {
		return ceiling
	}
	d := time.Duration(timeoutMs) * time.Millisecond
	if d > ceiling {
		return ceiling
	}
	return d
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestSQLTimeoutBudget -v`
Expected: PASS (6 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/agent/tools_sql.go internal/agent/tools_sql_test.go
git commit -m "feat(agent): sqlTimeoutBudget helper for execute_sql caller-scoped timeout"
```

### Task 2: Enforce the budget inside the `execute_sql` handler

**Files:**
- Modify: `internal/agent/types.go` (add `QueryTimeoutCeiling` to `ToolContext`)
- Modify: `internal/agent/tools_sql.go` (handler enforcement)
- Modify: `internal/agent/tools_notebook.go:169-182` (def declares `NoTimeout`, description/params update)
- Modify: `internal/agent/tools_seed.go:29` (description update)
- Test: `internal/agent/tools_timeout_catalog_test.go`

- [ ] **Step 1: Update the catalog test (it will fail)**

In `internal/agent/tools_timeout_catalog_test.go`, add `execute_sql` to the `NoTimeout` exemption switch (lines 18-21):

```go
	for _, def := range engine.registry.List() {
		switch def.Function.Name {
		case "ask_question", "spawn_subagents", "execute_sql":
			require.Equal(t, NoTimeout, def.Timeout, def.Function.Name)
			seen[def.Function.Name] = true
		default:
			require.Positive(t, def.Timeout, "tool %s must declare a positive timeout", def.Function.Name)
		}
	}
```

In `TestDynamicToolDefsDeclareTimeout` (line 41), the dynamic `sql_query` tool keeps its fixed 30 s default — add a clarifying comment above the existing assertion:

```go
	// The builtin execute_sql declares NoTimeout and enforces its budget
	// in-handler (sqlTimeoutBudget) so the MCP path can raise the ceiling.
	// The dynamic sql_query tool keeps the fixed 30s default.
	require.Equal(t, 30*time.Second, sqlDef.Timeout)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run 'TestAllBuiltinToolsHaveTimeout|TestDynamicToolDefsDeclareTimeout' -v`
Expected: FAIL — `execute_sql` declares `30s`, not `NoTimeout`

- [ ] **Step 3: Add `QueryTimeoutCeiling` to `ToolContext`**

In `internal/agent/types.go`, inside `type ToolContext struct` (near the other limit fields):

```go
	// QueryTimeoutCeiling bounds execute_sql execution. Zero means the agent
	// default (30s). The MCP handler sets it from AETHER_MCP_SQL_TIMEOUT_MS
	// so harness callers can run longer queries; agent paths leave it zero.
	QueryTimeoutCeiling time.Duration
```

- [ ] **Step 4: Enforce the budget in the handler**

In `internal/agent/tools_sql.go`, replace the body of `makeExecuteSQLHandler` (lines 150-182) — same behavior plus the caller-scoped budget:

```go
func makeExecuteSQLHandler(pool *pgxpool.Pool) ToolHandler {
	return func(args json.RawMessage, ctx *ToolContext) (any, error) {
		var req struct {
			ConnectorID string `json:"connector_id"`
			Query       string `json:"query"`
			Limit       int    `json:"limit"`
			TimeoutMs   int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(args, &req); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if req.ConnectorID == "" {
			return nil, fmt.Errorf("connector_id is required")
		}
		if req.Query == "" {
			return nil, fmt.Errorf("query is required")
		}

		if err := ctx.CheckPermission("connector", req.ConnectorID, "use"); err != nil {
			return nil, err
		}

		if !isReadOnlyQuery(req.Query) {
			return nil, fmt.Errorf("only read-only queries (SELECT, SHOW, DESCRIBE, EXPLAIN) are allowed")
		}

		// The def declares NoTimeout; this handler enforces the caller-scoped
		// budget itself so the MCP path can raise the ceiling above the agent
		// default while agent paths keep the 30s guardrail.
		runCtx, cancel := context.WithTimeout(ctx.Context, sqlTimeoutBudget(req.TimeoutMs, ctx.QueryTimeoutCeiling))
		defer cancel()
		tc := *ctx
		tc.Context = runCtx

		result, err := executeAgentSQL(&tc, pool, req.ConnectorID, req.Query, nil, req.Limit)
		if err != nil {
			return nil, err
		}

		return result, nil
	}
}
```

(`context` must be added to the imports of `tools_sql.go`.)

- [ ] **Step 5: Update the `execute_sql` tool def**

In `internal/agent/tools_notebook.go`, replace the registration at lines 169-182:

```go
	reg.Register(&ToolDef{
		Function: struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Parameters  any    `json:"parameters"`
		}{
			Name:        "execute_sql",
			Description: "Run an ad-hoc SQL query on a database connector. Default timeout 30s; pass timeout_ms (milliseconds) for a longer budget, capped by the server's configured ceiling. For long-running queries, prefer create_cell with run=true. Returns up to 10000 rows (default 1000, override with limit). For SELECT, SHOW, DESCRIBE queries.",
			Parameters:  `{"type":"object","properties":{"connector_id":{"type":"string","description":"ID of the connector to query"},"query":{"type":"string","description":"The SQL query to execute"},"limit":{"type":"integer","description":"Max rows to return (default 1000, max 10000)"},"timeout_ms":{"type":"integer","description":"Execution budget in milliseconds; clamped to the server ceiling"}},"required":["connector_id","query"]}`,
		},
		Handler:         makeExecuteSQLHandler(db),
		ConfirmRequired: true,
		Timeout:         NoTimeout,
	})
```

In `internal/agent/tools_seed.go:29`, update the description:

```go
	{Name: "execute_sql", Description: "Run ad-hoc SQL queries (default 30s timeout; timeout_ms raises it up to the server ceiling — use create_cell with run=true for long queries)", HandlerName: "execute_sql"},
```

- [ ] **Step 6: Run the agent test suite**

Run: `go test ./internal/agent/`
Expected: PASS (updated catalog tests + existing `tools_sql_test.go`)

- [ ] **Step 7: Commit**

```bash
git add internal/agent/types.go internal/agent/tools_sql.go internal/agent/tools_notebook.go internal/agent/tools_seed.go internal/agent/tools_timeout_catalog_test.go
git commit -m "feat(agent): caller-scoped timeout budget for execute_sql

execute_sql now enforces its budget in-handler via QueryTimeoutCeiling:
agent paths keep the 30s default, while the MCP handler can raise the
ceiling (AETHER_MCP_SQL_TIMEOUT_MS) and callers can pass timeout_ms."
```

### Task 3: Config + MCP handler wiring

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/api/router.go` (field + setter)
- Modify: `internal/api/mcp.go` (extract `mcpToolContext`, set ceiling)
- Create: `internal/api/export_test.go` (if absent)
- Modify: `cmd/aether/main.go` (wire config)
- Test: `internal/api/mcp_timeout_test.go`, `internal/config/config_test.go` (if absent)

- [ ] **Step 1: Write the failing test**

Create `internal/api/mcp_timeout_test.go`:

```go
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The MCP tools/call path threads AETHER_MCP_SQL_TIMEOUT_MS into the agent
// ToolContext as the execute_sql ceiling; unset means zero (agent default).
func TestMCPToolContextQueryTimeoutCeiling(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)

	jwt := registerAndGetToken(t, srv, "mcp-ceiling@example.com", "MCP Ceiling Org")

	req := httptest.NewRequest("POST", "/api/v1/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)

	ctx := srv.MCPToolContextForTest(req)
	require.Equal(t, time.Duration(0), ctx.QueryTimeoutCeiling, "unset config means agent default")

	srv.SetMCPSQLTimeout(10 * time.Minute)
	ctx = srv.MCPToolContextForTest(req)
	require.Equal(t, 10*time.Minute, ctx.QueryTimeoutCeiling)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestMCPToolContextQueryTimeoutCeiling -v`
Expected: FAIL — `srv.MCPToolContextForTest undefined` / `srv.SetMCPSQLTimeout undefined`

- [ ] **Step 3: Implement**

1. `internal/config/config.go` — add to the `Config` struct (near `AgentToolTimeoutDefault`):

```go
	MCPSQLTimeoutMs            int           // execute_sql ceiling for MCP callers (AETHER_MCP_SQL_TIMEOUT_MS, default 600000ms)
```

In `load()` (next to the `agentToolTimeoutDefault` parsing):

```go
	mcpSQLTimeoutMs, err := parseMCPSQLTimeoutMs(os.Getenv("AETHER_MCP_SQL_TIMEOUT_MS"))
	if err != nil {
		return nil, err
	}
```

and in the `cfg := &Config{...}` literal:

```go
		MCPSQLTimeoutMs:            mcpSQLTimeoutMs,
```

Add the parse helper next to `parseAgentToolTimeoutDefault`:

```go
// parseMCPSQLTimeoutMs parses AETHER_MCP_SQL_TIMEOUT_MS. Empty means the
// 10-minute default (matching run_cell's maxToolTimeoutMs); values below
// 1000ms are rejected so a misconfigured var cannot make execute_sql useless.
func parseMCPSQLTimeoutMs(raw string) (int, error) {
	if raw == "" {
		return 600000, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid AETHER_MCP_SQL_TIMEOUT_MS %q: %w", raw, err)
	}
	if n < 1000 {
		return 0, fmt.Errorf("invalid AETHER_MCP_SQL_TIMEOUT_MS %q: must be at least 1000ms", raw)
	}
	return n, nil
}
```

Add a unit test in `internal/config/config_test.go` (create if absent; package `config`):

```go
func TestParseMCPSQLTimeoutMs(t *testing.T) {
	d, err := parseMCPSQLTimeoutMs("")
	require.NoError(t, err)
	require.Equal(t, 600000, d)

	d, err = parseMCPSQLTimeoutMs("30000")
	require.NoError(t, err)
	require.Equal(t, 30000, d)

	_, err = parseMCPSQLTimeoutMs("500")
	require.Error(t, err)

	_, err = parseMCPSQLTimeoutMs("abc")
	require.Error(t, err)
}
```

2. `internal/api/router.go` — add fields to `Server` (near `outputLimitsMaxBytes`):

```go
	mcpSQLTimeout   time.Duration  // execute_sql ceiling for MCP callers (0 = agent default)
	mcpOAuthEnabled bool           // serves the OAuth 2.1 authorization-server endpoints (Task 8)
	oauth           *oauth.Service // OAuth AS storage/logic (Task 7)
```

Add setters next to `SetOutputLimitsMaxBytes` (ensure `"time"` import):

```go
// SetMCPSQLTimeout sets the execute_sql ceiling applied to MCP callers.
func (s *Server) SetMCPSQLTimeout(d time.Duration) {
	s.mcpSQLTimeout = d
}

// SetMCPOAuthEnabled enables the OAuth 2.1 authorization-server endpoints
// for the MCP resource server. Must be called before the first request.
func (s *Server) SetMCPOAuthEnabled(enabled bool) {
	s.mcpOAuthEnabled = enabled
	if enabled && s.oauth == nil {
		s.oauth = oauth.NewService(s.db.Pool)
	}
}
```

(`oauth` import: `github.com/the-heaven-labs/aether/internal/oauth` — added in Task 7; to keep Part A compiling standalone, introduce the field as `oauth *oauth.Service` only in Task 7, and in this task add only `mcpSQLTimeout` + `SetMCPSQLTimeout`. `SetMCPOAuthEnabled` moves to Task 7.)

3. `internal/api/mcp.go` — extract context construction from `handleMCPToolsCall` (lines 292-309) into a method and set the ceiling:

```go
// mcpToolContext builds the agent ToolContext for an MCP tools/call. The
// execute_sql ceiling comes from AETHER_MCP_SQL_TIMEOUT_MS (via
// SetMCPSQLTimeout); zero keeps the agent-side 30s default.
func (s *Server) mcpToolContext(claims *auth.Claims, r *http.Request) *agent.ToolContext {
	return &agent.ToolContext{
		Context:   r.Context(),
		UserID:    claims.UserID,
		OrgID:     claims.OrgID,
		OrgRole:   claims.Role,
		DB:        s.db.Pool,
		MasterKey: s.masterKey,
		BroadcastFunc: func(notebookID string, msg interface{}) {
			s.hub.Broadcast(notebookID, msg)
		},
		SetRunningFunc:      s.hub.SetRunning,
		UnsetRunningFunc:    s.hub.UnsetRunning,
		SetCancelFunc:       s.hub.SetCancelFunc,
		DeleteCancelFunc:    s.hub.DeleteCancelFunc,
		ResolveTarget:       s.resolveExecutionTarget,
		ConnPool:            s.connPool,
		CheckPermissionFunc: s.checkPermission,
		QueryTimeoutCeiling: s.mcpSQLTimeout,
	}
}
```

`handleMCPToolsCall` now uses:

```go
	ctx := s.mcpToolContext(claims, r)
```

4. `internal/api/export_test.go` (create if absent; otherwise append):

```go
package api

import (
	"net/http"

	"github.com/the-heaven-labs/aether/internal/agent"
)

// MCPToolContextForTest exposes mcpToolContext to the api_test package.
func (s *Server) MCPToolContextForTest(r *http.Request) *agent.ToolContext {
	return s.mcpToolContext(ClaimsFromContext(r.Context()), r)
}
```

Note: methods cannot be declared on `*Server` in a `_test.go` file from a different package in the same directory — Go allows `export_test.go` in package `api` (same package, test-only file) to define such helpers. Confirm the existing `export_test.go` pattern in the repo (`internal/api/export_test.go` was listed in the file search) and append there instead of creating a duplicate.

5. Wire the config in `cmd/aether/main.go` where the other `Set*` calls happen (find the `SetOutputLimitsMaxBytes` call; ensure `"time"` import):

```go
	apiServer.SetMCPSQLTimeout(time.Duration(cfg.MCPSQLTimeoutMs) * time.Millisecond)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestMCPToolContextQueryTimeoutCeiling -v && go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Run the full backend suite**

Run: `task test`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/api/router.go internal/api/mcp.go internal/api/export_test.go cmd/aether/main.go internal/api/mcp_timeout_test.go
git commit -m "feat(api): configurable execute_sql ceiling for MCP callers

AETHER_MCP_SQL_TIMEOUT_MS (default 600000ms, floor 1000ms) is threaded
from config through SetMCPSQLTimeout into the MCP ToolContext as
QueryTimeoutCeiling; agent paths are unchanged (30s default)."
```

---

## Part B — OAuth 2.1 authorization server (PR 2)

### Task 4: Migration `V121__oauth_tables.sql`

**Files:**
- Create: `internal/database/migrations/V121__oauth_tables.sql`

- [ ] **Step 1: Write the migration**

```sql
-- Migration 121: OAuth 2.1 authorization server tables for the MCP endpoint
-- (RFC 7591 dynamic client registration, single-use auth codes, rotating
-- refresh tokens with reuse detection).

CREATE TABLE oauth_clients (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id     TEXT NOT NULL UNIQUE,
    client_name   TEXT NOT NULL DEFAULT '',
    redirect_uris TEXT[] NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ
);

CREATE TABLE oauth_auth_codes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash        TEXT NOT NULL UNIQUE,
    client_id        TEXT NOT NULL,
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id           UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    scopes           TEXT[] NOT NULL DEFAULT '{}',
    resource         TEXT NOT NULL DEFAULT '',
    redirect_uri     TEXT NOT NULL DEFAULT '',
    code_challenge   TEXT NOT NULL,
    challenge_method TEXT NOT NULL DEFAULT 'S256',
    expires_at       TIMESTAMPTZ NOT NULL,
    used_at          TIMESTAMPTZ
);

CREATE TABLE oauth_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id    TEXT NOT NULL,
    refresh_hash TEXT NOT NULL UNIQUE,
    client_id    TEXT NOT NULL,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id       UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    resource     TEXT NOT NULL DEFAULT '',
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ,
    replaced_by  UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_oauth_tokens_family ON oauth_tokens (family_id);
CREATE INDEX idx_oauth_tokens_user ON oauth_tokens (user_id);
```

- [ ] **Step 2: Verify migrations apply**

Run: `go test ./internal/database/... -v` (the repo's migration tests apply all embedded migrations on a fresh database)
Expected: PASS — migration applies cleanly.

- [ ] **Step 3: Commit**

```bash
git add internal/database/migrations/V121__oauth_tables.sql
git commit -m "feat(db): oauth clients, auth codes and rotating refresh token tables"
```

### Task 5: OAuth claims + `IssueMCPAccessToken`

**Files:**
- Modify: `internal/auth/jwt.go`
- Test: `internal/auth/jwt_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/auth/jwt_test.go`:

```go
func TestIssueMCPAccessTokenRoundTrip(t *testing.T) {
	issuer := NewJWTIssuer("test-secret", time.Hour)

	tok, err := issuer.IssueMCPAccessToken(
		"user-1", "org-1", "editor", "mcp:query mcp:read", "mcp_abc",
		"https://a.example.com/api/v1/mcp", 15*time.Minute)
	require.NoError(t, err)

	claims, err := issuer.Validate(tok)
	require.NoError(t, err)
	require.Equal(t, "user-1", claims.UserID)
	require.Equal(t, "org-1", claims.OrgID)
	require.Equal(t, "editor", claims.Role)
	require.Equal(t, "mcp:query mcp:read", claims.Scope)
	require.Equal(t, "mcp_abc", claims.ClientID)
	require.True(t, claims.Audience.Contains("https://a.example.com/api/v1/mcp"))
	require.False(t, claims.Audience.Contains("https://other.example.com/api/v1/mcp"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -run TestIssueMCPAccessTokenRoundTrip -v`
Expected: FAIL — `issuer.IssueMCPAccessToken undefined`

- [ ] **Step 3: Implement**

In `internal/auth/jwt.go`:

1. Extend `Claims`:

```go
// Claims represents the JWT claims used by Aether for authentication.
type Claims struct {
	UserID          string `json:"uid"`
	OrgID           string `json:"oid"`
	Role            string `json:"role"`
	IsPlatformAdmin bool   `json:"is_platform_admin,omitempty"`
	// OAuth access tokens (MCP authorization server) only:
	Scope    string `json:"scope,omitempty"`     // space-separated granted scopes
	ClientID string `json:"client_id,omitempty"` // DCR client the token was issued to
	jwt.RegisteredClaims
}
```

2. Add the issuer method:

```go
// IssueMCPAccessToken issues a short-lived access token for an OAuth MCP
// client. The audience binds the token to the canonical MCP resource URI
// (RFC 8707) so it cannot be replayed against another service, and ClientID
// marks it as an OAuth token — the API middleware rejects it everywhere
// except the MCP endpoint.
func (j *JWTIssuer) IssueMCPAccessToken(userID, orgID, role, scopes, clientID, audience string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:   userID,
		OrgID:    orgID,
		Role:     role,
		Scope:    scopes,
		ClientID: clientID,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.secret)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/auth/jwt.go internal/auth/jwt_test.go
git commit -m "feat(auth): audience-bound OAuth access tokens for MCP clients"
```

### Task 6: `internal/oauth` pure logic — scopes, PKCE, redirect URIs

**Files:**
- Create: `internal/oauth/scopes.go`
- Create: `internal/oauth/pkce.go`
- Test: `internal/oauth/scopes_test.go`
- Test: `internal/oauth/pkce_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/oauth/scopes_test.go`:

```go
package oauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseScopes(t *testing.T) {
	require.Nil(t, ParseScopes(""))
	require.Equal(t, []string{"mcp:query"}, ParseScopes("mcp:query"))
	require.Equal(t, []string{"mcp:query", "mcp:read"}, ParseScopes("mcp:query mcp:read"))
	require.Equal(t, []string{"mcp:query"}, ParseScopes("mcp:query  mcp:query"))
}

func TestNormalizeScopes(t *testing.T) {
	require.Equal(t, []string{"mcp:query", "mcp:read"},
		NormalizeScopes([]string{"mcp:query", "bogus:scope", "mcp:read"}))
	require.Empty(t, NormalizeScopes([]string{"bogus:scope"}))
	require.Empty(t, NormalizeScopes(nil))
}

func TestToolsForScopes(t *testing.T) {
	tools := ToolsForScopes([]string{"mcp:query"})
	require.Contains(t, tools, "execute_sql")
	require.Contains(t, tools, "explore_schema")
	require.Contains(t, tools, "list_connectors")
	require.NotContains(t, tools, "create_notebook")

	all := ToolsForScopes([]string{"mcp:query", "mcp:read", "mcp:write"})
	require.Contains(t, all, "create_notebook")
	require.Contains(t, all, "run_cell")
	require.Contains(t, all, "execute_sql")

	require.Empty(t, ToolsForScopes(nil))
}

func TestValidateRedirectURI(t *testing.T) {
	valid := []string{
		"http://localhost:33211/callback",
		"http://127.0.0.1:8080/oauth/callback",
		"https://client.example.com/oauth/callback",
	}
	for _, uri := range valid {
		require.Truef(t, ValidateRedirectURI(uri), "expected valid: %s", uri)
	}
	invalid := []string{
		"",
		"not a uri",
		"http://evil.example.com/callback",
		"https://client.example.com/callback#fragment",
		"ftp://client.example.com/callback",
	}
	for _, uri := range invalid {
		require.Falsef(t, ValidateRedirectURI(uri), "expected invalid: %s", uri)
	}
}
```

`internal/oauth/pkce_test.go`:

```go
package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerifyPKCE(t *testing.T) {
	verifier := "correct-horse-battery-staple-0123456789abcdef"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	require.True(t, VerifyPKCE(challenge, "S256", verifier))
	require.False(t, VerifyPKCE(challenge, "S256", "wrong-verifier"))
	require.False(t, VerifyPKCE(challenge, "plain", verifier), "plain must be rejected")
	require.False(t, VerifyPKCE("", "S256", verifier))
	require.False(t, VerifyPKCE(challenge, "S256", ""))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/oauth/ -v`
Expected: FAIL — `undefined: ParseScopes` / `undefined: VerifyPKCE` (create the directory first)

- [ ] **Step 3: Implement**

`internal/oauth/scopes.go`:

```go
// Package oauth implements the server side of the MCP Authorization spec:
// OAuth 2.1 primitives (PKCE, public-client registration rules) and the
// scope model that narrows Aether's MCP tool surface.
package oauth

import (
	"net/url"
	"strings"
)

// Scopes granted to MCP clients. mcp:query is the data-plane scope; mcp:read
// and mcp:write mirror the platform read/write tool split.
const (
	ScopeQuery = "mcp:query"
	ScopeRead  = "mcp:read"
	ScopeWrite = "mcp:write"
)

// AllScopes is the full set of grantable scopes.
var AllScopes = []string{ScopeQuery, ScopeRead, ScopeWrite}

// scopeTools maps each scope to the MCP tool names it unlocks. The MCP
// allowlist (internal/api/mcp.go) remains the outer boundary; scopes can
// only narrow it.
var scopeTools = map[string][]string{
	ScopeQuery: {"execute_sql", "explore_schema", "list_connectors"},
	ScopeRead: {
		"list_notebooks", "list_cells", "get_notebook_context",
		"list_snapshots", "list_notebook_parameters", "list_dashboards",
		"get_dashboard", "list_skills", "load_skill", "list_agents",
		"list_connectors", "list_folders", "get_folder_tree",
		"read_permissions",
	},
	ScopeWrite: {
		"create_notebook", "delete_notebook", "update_notebook",
		"read_cell", "create_cell", "update_cell", "run_cell", "move_cell",
		"swap_cells", "delete_cell", "create_snapshot", "restore_snapshot",
		"set_notebook_parameters", "create_dashboard", "update_dashboard",
		"delete_dashboard", "create_dashboard_widget", "update_dashboard_widget",
		"delete_dashboard_widget", "create_schedule", "delete_schedule",
		"share_dashboard", "update_permissions", "export_notebook",
		"import_notebook", "create_skill", "update_skill", "create_chart",
		"update_chart",
	},
}

// ParseScopes splits a space-separated scope string (OAuth wire format),
// deduplicating while preserving first-seen order.
func ParseScopes(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Fields(raw)
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		if !seen[p] {
			out = append(out, p)
			seen[p] = true
		}
	}
	return out
}

// NormalizeScopes filters a requested scope list down to grantable scopes,
// preserving request order.
func NormalizeScopes(requested []string) []string {
	known := map[string]bool{}
	for _, s := range AllScopes {
		known[s] = true
	}
	out := make([]string, 0, len(requested))
	for _, s := range requested {
		if known[s] {
			out = append(out, s)
		}
	}
	return out
}

// ToolsForScopes returns the set of tool names unlocked by the scopes.
func ToolsForScopes(scopes []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, s := range scopes {
		for _, t := range scopeTools[s] {
			out[t] = struct{}{}
		}
	}
	return out
}

// ValidateRedirectURI enforces the MCP security requirement that redirect
// URIs are HTTPS or loopback HTTP (localhost / 127.0.0.1, any port) with no
// fragment. Exact-match comparison happens at authorize/token time.
func ValidateRedirectURI(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Fragment != "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}
```

`internal/oauth/pkce.go`:

```go
package oauth

import (
	"crypto/sha256"
	"encoding/base64"
)

// VerifyPKCE validates a code_verifier against a stored code_challenge.
// Only S256 is supported; plain is rejected per OAuth 2.1.
func VerifyPKCE(challenge, method, verifier string) bool {
	if challenge == "" || verifier == "" || method != "S256" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return computed == challenge
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/oauth/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/oauth/
git commit -m "feat(oauth): scopes, PKCE S256 and redirect URI validation"
```

### Task 7: `internal/oauth` service — clients, codes, rotating refresh tokens

**Files:**
- Create: `internal/oauth/service.go`
- Modify: `internal/api/router.go` (`oauth` field + `SetMCPOAuthEnabled` from Task 3)

- [ ] **Step 1: Implement the service**

`internal/oauth/service.go`:

```go
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/auth"
)

// Sentinel errors mapped to OAuth error codes by the HTTP layer.
var (
	ErrNotFound = errors.New("oauth: not found")
	ErrReused   = errors.New("oauth: refresh token reuse detected")
)

const (
	// AccessTTL keeps access tokens short-lived; harnesses refresh silently.
	AccessTTL = 15 * time.Minute
	// RefreshTTL bounds a consent to 30 days of continued use.
	RefreshTTL = 30 * 24 * time.Hour
	// CodeTTL bounds the authorization code lifetime (spec max: 10 minutes).
	CodeTTL = 60 * time.Second
)

// Client is a dynamically registered public OAuth client (PKCE-only).
type Client struct {
	ID           string
	ClientID     string
	ClientName   string
	RedirectURIs []string
	CreatedAt    time.Time
}

// AuthCode is the stored record behind a single-use authorization code.
type AuthCode struct {
	ClientID        string
	UserID          string
	OrgID           string
	Scopes          []string
	Resource        string
	RedirectURI     string
	CodeChallenge   string
	ChallengeMethod string
}

// TokenRecord is a refresh token row.
type TokenRecord struct {
	ID        string
	FamilyID  string
	ClientID  string
	UserID    string
	OrgID     string
	Scopes    []string
	Resource  string
	ExpiresAt time.Time
	Revoked   bool
	Replaced  bool
}

// Service persists OAuth clients, codes and tokens.
type Service struct {
	pool *pgxpool.Pool
}

// NewService builds a Service over the application pool.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

func randomToken(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateClient registers a new public client (RFC 7591).
func (s *Service) CreateClient(ctx context.Context, name string, redirectURIs []string) (*Client, error) {
	clientID := "mcp_" + randomToken(16)
	c := &Client{ClientID: clientID, ClientName: name, RedirectURIs: redirectURIs}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO oauth_clients (client_id, client_name, redirect_uris)
		 VALUES ($1, $2, $3) RETURNING id, created_at`,
		clientID, name, redirectURIs).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetClient looks up a registered client by client_id.
func (s *Service) GetClient(ctx context.Context, clientID string) (*Client, error) {
	var c Client
	var uris []string
	err := s.pool.QueryRow(ctx,
		`SELECT id, client_id, client_name, redirect_uris, created_at
		 FROM oauth_clients WHERE client_id = $1`, clientID).
		Scan(&c.ID, &c.ClientID, &c.ClientName, &uris, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.RedirectURIs = uris
	return &c, nil
}

// IssueAuthCode stores a single-use code and returns its plaintext (shown
// once, in the consent redirect).
func (s *Service) IssueAuthCode(ctx context.Context, rec AuthCode) (string, error) {
	code := randomToken(32)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_auth_codes
		     (code_hash, client_id, user_id, org_id, scopes, resource, redirect_uri, code_challenge, challenge_method, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW() + make_interval(secs => $10))`,
		hashToken(code), rec.ClientID, rec.UserID, rec.OrgID, rec.Scopes, rec.Resource,
		rec.RedirectURI, rec.CodeChallenge, rec.ChallengeMethod, CodeTTL.Seconds())
	if err != nil {
		return "", err
	}
	return code, nil
}

// ConsumeAuthCode redeems a code exactly once (atomic UPDATE guards reuse).
func (s *Service) ConsumeAuthCode(ctx context.Context, code string) (*AuthCode, error) {
	var rec AuthCode
	var scopes []string
	err := s.pool.QueryRow(ctx,
		`UPDATE oauth_auth_codes SET used_at = NOW()
		 WHERE code_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		 RETURNING client_id, user_id, org_id, scopes, resource, redirect_uri, code_challenge, challenge_method`,
		hashToken(code)).
		Scan(&rec.ClientID, &rec.UserID, &rec.OrgID, &scopes, &rec.Resource,
			&rec.RedirectURI, &rec.CodeChallenge, &rec.ChallengeMethod)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rec.Scopes = scopes
	return &rec, nil
}

// IssueTokens starts a rotation family: returns the access token and a fresh
// refresh token bound to the identity.
func (s *Service) IssueTokens(ctx context.Context, issuer *auth.JWTIssuer, clientID, userID, orgID, role string, scopes []string, resource string) (access, refresh string, err error) {
	refresh = randomToken(32)
	_, err = s.pool.Exec(ctx,
		`INSERT INTO oauth_tokens
		     (family_id, refresh_hash, client_id, user_id, org_id, scopes, resource, expires_at)
		 VALUES (gen_random_uuid()::text, $1, $2, $3, $4, $5, $6, NOW() + make_interval(days => 30))`,
		hashToken(refresh), clientID, userID, orgID, scopes, resource)
	if err != nil {
		return "", "", err
	}
	access, err = issuer.IssueMCPAccessToken(userID, orgID, role, joinScopes(scopes), clientID, resource, AccessTTL)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// RotateRefresh exchanges a refresh token for a new one in the same family.
// Presenting a superseded or revoked token revokes the whole family (reuse
// detection) and returns ErrReused. An unknown or expired token returns
// ErrNotFound. Membership is re-checked so removing a user from the org kills
// their refresh chains.
func (s *Service) RotateRefresh(ctx context.Context, issuer *auth.JWTIssuer, refreshToken string) (access, newRefresh string, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rec TokenRecord
	var scopes []string
	err = tx.QueryRow(ctx,
		`SELECT id, family_id, client_id, user_id, org_id, scopes, resource, expires_at,
		        revoked_at IS NOT NULL, replaced_by IS NOT NULL
		 FROM oauth_tokens WHERE refresh_hash = $1 FOR UPDATE`,
		hashToken(refreshToken)).
		Scan(&rec.ID, &rec.FamilyID, &rec.ClientID, &rec.UserID, &rec.OrgID, &scopes, &rec.Resource,
			&rec.ExpiresAt, &rec.Revoked, &rec.Replaced)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}

	if rec.Revoked || rec.Replaced {
		// Reuse of a rotated/revoked token: kill the whole family.
		_, _ = tx.Exec(ctx,
			`UPDATE oauth_tokens SET revoked_at = NOW()
			 WHERE family_id = $1 AND revoked_at IS NULL`, rec.FamilyID)
		_ = tx.Commit(ctx)
		return "", "", ErrReused
	}
	if !time.Now().Before(rec.ExpiresAt) {
		return "", "", ErrNotFound // simply expired
	}

	var role string
	err = tx.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`,
		rec.OrgID, rec.UserID).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrNotFound // no longer a member
		}
		return "", "", err
	}

	newRefresh = randomToken(32)
	var newID string
	err = tx.QueryRow(ctx,
		`INSERT INTO oauth_tokens
		     (family_id, refresh_hash, client_id, user_id, org_id, scopes, resource, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW() + make_interval(days => 30))
		 RETURNING id`,
		rec.FamilyID, hashToken(newRefresh), rec.ClientID, rec.UserID, rec.OrgID, scopes, rec.Resource).Scan(&newID)
	if err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE oauth_tokens SET replaced_by = $1 WHERE id = $2`, newID, rec.ID); err != nil {
		return "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", err
	}

	access, err = issuer.IssueMCPAccessToken(rec.UserID, rec.OrgID, role, joinScopes(scopes), rec.ClientID, rec.Resource, AccessTTL)
	if err != nil {
		return "", "", err
	}
	return access, newRefresh, nil
}

func joinScopes(scopes []string) string { return strings.Join(scopes, " ") }
```

- [ ] **Step 2: Compile + vet**

Run: `go build ./... && go vet ./internal/oauth/`
Expected: clean

- [ ] **Step 3: Wire into the Server**

In `internal/api/router.go` add the fields to `Server` (near `mcpSQLTimeout` from Task 3) with the import `github.com/the-heaven-labs/aether/internal/oauth`:

```go
	mcpOAuthEnabled bool           // serves the OAuth 2.1 authorization-server endpoints
	oauth           *oauth.Service // OAuth AS storage/logic
```

Add the setter next to `SetMCPSQLTimeout`:

```go
// SetMCPOAuthEnabled enables the OAuth 2.1 authorization-server endpoints
// for the MCP resource server. Must be called before the first request.
func (s *Server) SetMCPOAuthEnabled(enabled bool) {
	s.mcpOAuthEnabled = enabled
	if enabled && s.oauth == nil {
		s.oauth = oauth.NewService(s.db.Pool)
	}
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/oauth/service.go internal/api/router.go
git commit -m "feat(oauth): client/code storage and rotating refresh tokens with reuse detection"
```

### Task 8: Discovery, DCR and token endpoints (API layer)

**Files:**
- Create: `internal/api/oauth_handlers.go`
- Modify: `internal/api/router.go` (routes + rate limits, behind the flag)
- Test: `internal/api/mcp_oauth_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/api/mcp_oauth_test.go`. Shared helpers first:

```go
package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Pinned resource/host: the tests derive the canonical resource URI from the
// request Host, so every request in this file pins req.Host = "example.com"
// and uses resource = "http://example.com/api/v1/mcp".
const (
	oauthHost      = "example.com"
	oauthResource  = "http://example.com/api/v1/mcp"
	oauthRedirect  = "http://localhost:33211/callback"
	oauthVerifier  = "test-verifier-0123456789abcdef"
)

func oauthChallenge(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(oauthVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func doJSON(t *testing.T, srv http.Handler, method, path, jwtToken, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(method, path, strings.NewReader(body))
	req.Host = oauthHost
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+jwtToken)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func oauthRegister(t *testing.T, srv http.Handler) string {
	t.Helper()
	rec := doJSON(t, srv, "POST", "/oauth/register", "",
		`{"client_name":"opencode","redirect_uris":["`+oauthRedirect+`"],"token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out["client_id"].(string)
}

// oauthConsentApprove drives the SPA-mediated consent decision API directly
// (the browser step is exercised manually; see docs/mcp-oauth.md).
func oauthConsentApprove(t *testing.T, srv http.Handler, jwtToken, clientID string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": oauthResource,
		"state": "st-123", "code_challenge": oauthChallenge(t),
		"code_challenge_method": "S256", "approve": true,
	})
	rec := doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwtToken, string(payload))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	loc, _ := out["redirect"].(string)
	u, err := url.Parse(loc)
	require.NoError(t, err)
	require.Equal(t, "st-123", u.Query().Get("state"))
	require.Equal(t, oauthRedirect, u.Scheme+"://"+u.Host+u.Path)
	return u.Query().Get("code")
}

func oauthToken(t *testing.T, srv http.Handler, clientID, code string) map[string]any {
	t.Helper()
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {oauthRedirect}, "client_id": {clientID},
		"code_verifier": {oauthVerifier}, "resource": {oauthResource},
	}
	req, _ := http.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Host = oauthHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func oauthRefresh(t *testing.T, srv http.Handler, clientID, refreshToken string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}, "client_id": {clientID}}
	req, _ := http.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Host = oauthHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// setupOAuthServer registers a user + connector and returns (srv, jwt, clientID, connectorID).
// Requires the `api` import: `github.com/the-heaven-labs/aether/internal/api` (same as testhelpers_test.go).
func setupOAuthServer(t *testing.T) (*api.Server, string, string, string) {
	t.Helper()
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	srv.SetMCPOAuthEnabled(true)
	jwt := registerAndGetToken(t, srv, fmt.Sprintf("mcp-oauth-%d@example.com", time.Now().UnixNano()), "OAuth Org")
	connID := createConnector(t, srv, jwt)
	clientID := oauthRegister(t, srv)
	return srv, jwt, clientID, connID
}
```

(The helper returns the concrete `*api.Server` so tests can call both `ServeHTTP` and server methods like `doCreateToken`.)

Endpoint tests:

```go
func TestOAuthDiscoveryEndpoints(t *testing.T) {
	srv, _, _, _ := setupOAuthServer(t)

	rec := doJSON(t, srv, "GET", "/.well-known/oauth-protected-resource", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var prm map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prm))
	require.Equal(t, oauthResource, prm["resource"])
	require.Contains(t, prm["authorization_servers"], "http://"+oauthHost)

	rec = doJSON(t, srv, "GET", "/.well-known/oauth-authorization-server", "", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meta))
	require.Contains(t, meta["code_challenge_methods_supported"], "S256")
	require.Contains(t, meta["grant_types_supported"], "refresh_token")
}

func TestOAuthDisabledFlag404(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t) // flag off by default
	rec := doJSON(t, srv, "GET", "/.well-known/oauth-protected-resource", "", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	rec = doJSON(t, srv, "POST", "/oauth/register", "", `{"redirect_uris":["http://localhost:1/cb"]}`)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOAuthDCRValidation(t *testing.T) {
	srv, _, _, _ := setupOAuthServer(t)

	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"loopback http ok", `{"client_name":"x","redirect_uris":["http://localhost:1/cb"]}`, http.StatusCreated},
		{"https ok", `{"client_name":"x","redirect_uris":["https://c.example.com/cb"]}`, http.StatusCreated},
		{"remote http rejected", `{"client_name":"x","redirect_uris":["http://evil.example.com/cb"]}`, http.StatusBadRequest},
		{"secret auth rejected", `{"client_name":"x","redirect_uris":["http://localhost:1/cb"],"token_endpoint_auth_method":"client_secret_basic"}`, http.StatusBadRequest},
		{"no redirect", `{"client_name":"x"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, srv, "POST", "/oauth/register", "", tc.body)
			require.Equal(t, tc.status, rec.Code)
		})
	}
}

func TestOAuthFullFlowExecuteSQL(t *testing.T) {
	srv, jwt, clientID, connID := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)

	access := tok["access_token"].(string)
	require.NotEmpty(t, tok["refresh_token"])
	require.Equal(t, "Bearer", tok["token_type"])
	require.InDelta(t, 900.0, tok["expires_in"].(float64), 60)

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute_sql","arguments":{"connector_id":"` + connID + `","query":"SELECT 1 AS x"}}}`
	req, _ := http.NewRequest("POST", "/api/v1/mcp", strings.NewReader(body))
	req.Host = oauthHost
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"x"`, "expected result column x")
}

func TestOAuthWrongAudienceRejected(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)

	// Same token, different Host → audience no longer matches → 401.
	req, _ := http.NewRequest("POST", "/api/v1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Host = "other.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok["access_token"].(string))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestOAuthRefreshRotationAndReuseRevokesFamily(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)
	refresh := tok["refresh_token"].(string)

	rec := oauthRefresh(t, srv, clientID, refresh)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rotated map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rotated))
	newRefresh := rotated["refresh_token"].(string)
	require.NotEqual(t, refresh, newRefresh)

	// Replaying the OLD token revokes the family...
	rec = oauthRefresh(t, srv, clientID, refresh)
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// ...so the NEW token is dead too.
	rec = oauthRefresh(t, srv, clientID, newRefresh)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOAuthTokenRejectedOutsideMCP(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)

	rec := doJSON(t, srv, "GET", "/api/v1/notebooks", tok["access_token"].(string), "")
	require.Equal(t, http.StatusForbidden, rec.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestOAuth' -v`
Expected: FAIL — `srv.SetMCPOAuthEnabled undefined` (until Task 7 lands) / 404s

- [ ] **Step 3: Implement the handlers**

Create `internal/api/oauth_handlers.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/the-heaven-labs/aether/internal/oauth"
)

// canonicalResourceURI returns the RFC 8707 canonical URI of the MCP server
// for the request: scheme://host/api/v1/mcp (no trailing slash). Subdomain
// deployments each advertise and validate their own resource.
func canonicalResourceURI(r *http.Request) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/api/v1/mcp"
}

// oauthBaseURL is the AS issuer/discovery base for the request host.
func oauthBaseURL(r *http.Request) string {
	return strings.TrimSuffix(canonicalResourceURI(r), "/api/v1/mcp")
}

// handleOAuthProtectedResource serves RFC 9728 Protected Resource Metadata.
func (s *Server) handleOAuthProtectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 canonicalResourceURI(r),
		"authorization_servers":    []string{oauthBaseURL(r)},
		"scopes_supported":         oauth.AllScopes,
		"bearer_methods_supported": []string{"header"},
	})
}

// handleOAuthASMetadata serves RFC 8414 Authorization Server Metadata.
func (s *Server) handleOAuthASMetadata(w http.ResponseWriter, r *http.Request) {
	base := oauthBaseURL(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      oauth.AllScopes,
	})
}

// handleOAuthRegister implements RFC 7591 dynamic client registration for
// public clients only.
func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.RedirectURIs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	if req.TokenEndpointAuthMethod != "" && req.TokenEndpointAuthMethod != "none" {
		writeError(w, http.StatusBadRequest, "only public clients (token_endpoint_auth_method=none) are supported")
		return
	}
	for _, gt := range append(req.GrantTypes, req.ResponseTypes...) {
		switch gt {
		case "", "authorization_code", "refresh_token", "code":
		default:
			writeError(w, http.StatusBadRequest, "unsupported grant or response type: "+gt)
			return
		}
	}
	for _, uri := range req.RedirectURIs {
		if !oauth.ValidateRedirectURI(uri) {
			writeError(w, http.StatusBadRequest, "redirect URIs must be https or loopback http")
			return
		}
	}
	client, err := s.oauth.CreateClient(r.Context(), req.ClientName, req.RedirectURIs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "registration failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ClientID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.ClientName,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

func oauthErrorResponse(w http.ResponseWriter, code, description string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"error": code, "error_description": description,
	})
}

// handleOAuthToken implements the authorization_code and refresh_token grants.
func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthErrorResponse(w, "invalid_request", "malformed form body")
		return
	}
	client, err := s.oauth.GetClient(r.Context(), r.PostFormValue("client_id"))
	if err != nil {
		oauthErrorResponse(w, "invalid_client", "unknown client")
		return
	}

	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		s.handleOAuthTokenAuthCode(w, r, client)
	case "refresh_token":
		s.handleOAuthTokenRefresh(w, r)
	default:
		oauthErrorResponse(w, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (s *Server) handleOAuthTokenAuthCode(w http.ResponseWriter, r *http.Request, client *oauth.Client) {
	redirectURI := r.PostFormValue("redirect_uri")
	resource := r.PostFormValue("resource")
	codeRec, err := s.oauth.ConsumeAuthCode(r.Context(), r.PostFormValue("code"))
	if err != nil {
		oauthErrorResponse(w, "invalid_grant", "code is invalid, expired or already used")
		return
	}
	if codeRec.ClientID != client.ClientID {
		oauthErrorResponse(w, "invalid_grant", "code was issued to another client")
		return
	}
	if redirectURI == "" || redirectURI != codeRec.RedirectURI {
		oauthErrorResponse(w, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if !oauth.VerifyPKCE(codeRec.CodeChallenge, codeRec.ChallengeMethod, r.PostFormValue("code_verifier")) {
		oauthErrorResponse(w, "invalid_grant", "PKCE verification failed")
		return
	}
	if resource != "" && resource != codeRec.Resource {
		oauthErrorResponse(w, "invalid_grant", "resource mismatch")
		return
	}
	// The role recorded at consent time is re-derived at rotation; for the
	// initial exchange fetch it from org_members.
	role, err := s.memberRole(r.Context(), codeRec.OrgID, codeRec.UserID)
	if err != nil {
		oauthErrorResponse(w, "invalid_grant", "user is not a member of the organization")
		return
	}
	access, refresh, err := s.oauth.IssueTokens(r.Context(), s.jwt, client.ClientID,
		codeRec.UserID, codeRec.OrgID, role, codeRec.Scopes, codeRec.Resource)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token issuance failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(oauth.AccessTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         strings.Join(codeRec.Scopes, " "),
	})
}

func (s *Server) handleOAuthTokenRefresh(w http.ResponseWriter, r *http.Request) {
	access, newRefresh, err := s.oauth.RotateRefresh(r.Context(), s.jwt, r.PostFormValue("refresh_token"))
	switch {
	case errors.Is(err, oauth.ErrReused):
		oauthErrorResponse(w, "invalid_grant", "refresh token reuse detected; all tokens in the family were revoked")
	case errors.Is(err, oauth.ErrNotFound):
		oauthErrorResponse(w, "invalid_grant", "refresh token is invalid or expired")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "token rotation failed")
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  access,
			"token_type":    "Bearer",
			"expires_in":    int(oauth.AccessTTL.Seconds()),
			"refresh_token": newRefresh,
		})
	}
}

// handleOAuthAuthorize validates the request, then hands the browser to the
// SPA consent page (the SPA owns the session token in localStorage). The
// consent page re-reads the same query parameters.
func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	client, err := s.oauth.GetClient(r.Context(), q.Get("client_id"))
	if err != nil {
		http.Error(w, "invalid authorization request: unknown client", http.StatusBadRequest)
		return
	}
	redirectURI := q.Get("redirect_uri")
	allowed := false
	for _, uri := range client.RedirectURIs {
		if uri == redirectURI {
			allowed = true
			break
		}
	}
	if !allowed || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "invalid authorization request", http.StatusBadRequest)
		return
	}
	if s.frontendHandler != nil {
		s.frontendHandler.ServeHTTP(w, r)
		return
	}
	http.Error(w, "consent UI is not available", http.StatusNotImplemented)
}
```

Add `memberRole` (same file or `oauth_consent_handlers.go` created in Task 9):

```go
// memberRole returns the caller's role in the org, for stamping access tokens.
func (s *Server) memberRole(ctx context.Context, orgID, userID string) (string, error) {
	var role string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role)
	return role, err
}
```

- [ ] **Step 4: Register the routes (behind the flag)**

In `internal/api/router.go`, after the existing MCP routes (lines 649-662), mirror the inline rate-limit env pattern (like `AETHER_RATE_LIMIT_REGISTER` at lines 361-366):

```go
	if s.mcpOAuthEnabled {
		s.mux.Handle("GET /.well-known/oauth-protected-resource", http.HandlerFunc(s.handleOAuthProtectedResource))
		s.mux.Handle("GET /.well-known/oauth-authorization-server", http.HandlerFunc(s.handleOAuthASMetadata))

		oauthRegisterLimit := 10
		if v := os.Getenv("AETHER_RATE_LIMIT_OAUTH_REGISTER"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				oauthRegisterLimit = n
			}
		}
		oauthTokenLimit := 30
		if v := os.Getenv("AETHER_RATE_LIMIT_OAUTH_TOKEN"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				oauthTokenLimit = n
			}
		}
		s.mux.Handle("POST /oauth/register", s.rateLimit(rateLimitConfig{
			keyFunc: clientIP, limit: oauthRegisterLimit, window: time.Minute,
		})(http.HandlerFunc(s.handleOAuthRegister)))
		s.mux.Handle("POST /oauth/token", s.rateLimit(rateLimitConfig{
			keyFunc: clientIP, limit: oauthTokenLimit, window: time.Minute,
		})(http.HandlerFunc(s.handleOAuthToken)))
		s.mux.Handle("GET /oauth/authorize", http.HandlerFunc(s.handleOAuthAuthorize))
	}
```

(The consent APIs from Task 9 register in the same block.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestOAuth' -v`
Expected: discovery/DCR/flag tests PASS. `TestOAuthFullFlowExecuteSQL`, `TestOAuthWrongAudienceRejected`, `TestOAuthRefreshRotationAndReuseRevokesFamily` and `TestOAuthTokenRejectedOutsideMCP` still FAIL (consent APIs + middleware land in Tasks 9-10).

- [ ] **Step 6: Commit**

```bash
git add internal/api/oauth_handlers.go internal/api/router.go internal/api/mcp_oauth_test.go
git commit -m "feat(api): OAuth 2.1 discovery, dynamic client registration and token endpoints"
```

### Task 9: Consent APIs + SPA-mediated authorize

**Files:**
- Create: `internal/api/oauth_consent_handlers.go`
- Modify: `internal/api/router.go` (two authed routes inside the flag block)
- Test: `internal/api/mcp_oauth_test.go` (additions)

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/mcp_oauth_test.go`:

```go
func TestOAuthConsentInfoAndDeny(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)

	rec := doJSON(t, srv, "GET",
		"/api/v1/oauth/consent/info?client_id="+clientID+"&scope=mcp:query&resource="+oauthResource,
		jwt, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var info map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &info))
	require.Equal(t, "opencode", info["client_name"])
	require.NotEmpty(t, info["org_name"])
	require.Contains(t, info["scopes"], "mcp:query")

	// Deny → redirect with error=access_denied and no code.
	payload, _ := json.Marshal(map[string]any{
		"client_id": clientID, "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": false,
	})
	rec = doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusOK, rec.Code)
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Contains(t, out["redirect"], "error=access_denied")
}

func TestOAuthConsentRejectsUnknownClientOrRedirect(t *testing.T) {
	srv, jwt, _, _ := setupOAuthServer(t)

	payload, _ := json.Marshal(map[string]any{
		"client_id": "mcp_missing", "redirect_uri": oauthRedirect,
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": true,
	})
	rec := doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	payload, _ = json.Marshal(map[string]any{
		"client_id": "mcp_missing", "redirect_uri": "https://evil.example.com/cb",
		"scope": "mcp:query", "resource": oauthResource, "state": "s",
		"code_challenge": oauthChallenge(t), "code_challenge_method": "S256",
		"approve": true,
	})
	rec = doJSON(t, srv, "POST", "/api/v1/oauth/consent/decision", jwt, string(payload))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOAuthAuthorizeInvalidParamsRejected(t *testing.T) {
	srv, _, clientID, _ := setupOAuthServer(t)

	// Unknown client → plain 400 (never a redirect).
	rec := doJSON(t, srv, "GET", "/oauth/authorize?client_id=mcp_missing&redirect_uri="+oauthRedirect+"&code_challenge=abc&code_challenge_method=S256", "", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// plain challenge method rejected.
	u := "/oauth/authorize?client_id=" + clientID + "&redirect_uri=" + oauthRedirect +
		"&code_challenge=abc&code_challenge_method=plain&response_type=code"
	rec = doJSON(t, srv, "GET", u, "", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestOAuthConsent|TestOAuthAuthorize' -v`
Expected: FAIL — handlers not registered (404)

- [ ] **Step 3: Implement**

Create `internal/api/oauth_consent_handlers.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/the-heaven-labs/aether/internal/oauth"
)

var consentScopeDescriptions = map[string]string{
	oauth.ScopeQuery: "Run read-only SQL queries against your connectors",
	oauth.ScopeRead:  "View notebooks, dashboards and other resources",
	oauth.ScopeWrite: "Create and modify notebooks and dashboards",
}

// handleOAuthConsentInfo backs the SPA consent page: describes the client,
// the org and the requested scopes. Requires membership of the subdomain org
// (authMW enforces token org == subdomain org before this runs).
func (s *Server) handleOAuthConsentInfo(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	subdomainOrg := OrgIDFromContext(r.Context())
	if claims == nil || subdomainOrg == "" || subdomainOrg != claims.OrgID {
		writeError(w, http.StatusForbidden, "consent requires an organization subdomain context")
		return
	}
	client, err := s.oauth.GetClient(r.Context(), r.URL.Query().Get("client_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unknown client")
		return
	}
	scopes := oauth.NormalizeScopes(strings.Fields(r.URL.Query().Get("scope")))
	if len(scopes) == 0 {
		writeError(w, http.StatusBadRequest, "no valid scopes requested")
		return
	}
	var orgName string
	if err := s.db.Pool.QueryRow(r.Context(),
		`SELECT name FROM orgs WHERE id = $1`, subdomainOrg).Scan(&orgName); err != nil {
		writeError(w, http.StatusInternalServerError, "org lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"client_name": client.ClientName,
		"org_id":      subdomainOrg,
		"org_name":    orgName,
		"scopes":      scopes,
		"scope_descriptions": consentScopeDescriptions,
	})
}

type oauthConsentDecisionRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	Resource            string `json:"resource"`
	State               string `json:"state"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Approve             bool   `json:"approve"`
}

// handleOAuthConsentDecision validates everything server-side and returns the
// redirect the SPA should follow (code or error appended). The auth code is
// single-use and bound to client + challenge + resource + org.
func (s *Server) handleOAuthConsentDecision(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	subdomainOrg := OrgIDFromContext(r.Context())
	if claims == nil || subdomainOrg == "" || subdomainOrg != claims.OrgID {
		writeError(w, http.StatusForbidden, "consent requires an organization subdomain context")
		return
	}
	var req oauthConsentDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	client, err := s.oauth.GetClient(r.Context(), req.ClientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unknown client")
		return
	}
	allowed := false
	for _, uri := range client.RedirectURIs {
		if uri == req.RedirectURI {
			allowed = true
			break
		}
	}
	if !allowed {
		writeError(w, http.StatusBadRequest, "redirect_uri is not registered for this client")
		return
	}
	if req.CodeChallenge == "" || req.CodeChallengeMethod != "S256" {
		writeError(w, http.StatusBadRequest, "code_challenge with S256 is required")
		return
	}
	resource := req.Resource
	if resource == "" {
		resource = canonicalResourceURI(r)
	}
	redirect := req.RedirectURI + "?state=" + url.QueryEscape(req.State)
	if !req.Approve {
		writeJSON(w, http.StatusOK, map[string]any{
			"redirect": redirect + "&error=access_denied",
		})
		return
	}
	scopes := oauth.NormalizeScopes(strings.Fields(req.Scope))
	if len(scopes) == 0 {
		writeError(w, http.StatusBadRequest, "no valid scopes requested")
		return
	}
	role, err := s.memberRole(r.Context(), subdomainOrg, claims.UserID)
	if err != nil {
		writeError(w, http.StatusForbidden, "you are not a member of this organization")
		return
	}
	code, err := s.oauth.IssueAuthCode(r.Context(), oauth.AuthCode{
		ClientID:        client.ClientID,
		UserID:          claims.UserID,
		OrgID:           subdomainOrg,
		Scopes:          scopes,
		Resource:        resource,
		RedirectURI:     req.RedirectURI,
		CodeChallenge:   req.CodeChallenge,
		ChallengeMethod: req.CodeChallengeMethod,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue authorization code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"redirect": redirect + "&code=" + url.QueryEscape(code),
	})
}
```

(`"net/url"` must be added to the imports. The real PKCE verification happens at `/oauth/token` (Task 8); consent time only validates the challenge shape.)

- [ ] **Step 4: Register the routes**

Inside the `if s.mcpOAuthEnabled` block from Task 8:

```go
		s.mux.Handle("GET /api/v1/oauth/consent/info", s.authMW(http.HandlerFunc(s.handleOAuthConsentInfo)))
		s.mux.Handle("POST /api/v1/oauth/consent/decision", s.authMW(http.HandlerFunc(s.handleOAuthConsentDecision)))
```

Check how the existing routes build `authMW` (router.go line 650 uses a local `authMW` value) and reuse exactly that construction.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestOAuthConsent|TestOAuthAuthorize|TestOAuthFullFlow|TestOAuthWrongAudience|TestOAuthRefresh|TestOAuthTokenRejected' -v`
Expected: consent tests PASS. Middleware-dependent tests (`TestOAuthFullFlowExecuteSQL`, `TestOAuthWrongAudienceRejected`, `TestOAuthTokenRejectedOutsideMCP`) still FAIL until Task 10.

- [ ] **Step 6: Commit**

```bash
git add internal/api/oauth_consent_handlers.go internal/api/router.go internal/api/mcp_oauth_test.go
git commit -m "feat(api): SPA-mediated OAuth consent endpoints for MCP authorizations"
```

### Task 10: Middleware enforcement — audience binding + MCP-only tokens

**Files:**
- Modify: `internal/api/middleware.go` (`AuthMiddleware`)
- Test: `internal/api/mcp_oauth_test.go` (Task 8 tests now pass)

- [ ] **Step 1: Implement**

In `internal/api/middleware.go`, inside `AuthMiddleware` after `issuer.Validate` succeeds and before the subdomain check (line ~63), add:

```go
			// OAuth access tokens (ClientID set) are bound to the MCP
			// resource: audience must match the canonical resource URI of
			// this request (RFC 8707), and they are only valid at the MCP
			// endpoint — never on the platform REST APIs.
			if claims.ClientID != "" {
				if !strings.HasPrefix(r.URL.Path, "/api/v1/mcp") {
					writeError(w, http.StatusForbidden, "oauth access tokens are only valid at the MCP endpoint")
					return
				}
				if !claims.Audience.Contains(canonicalResourceURI(r)) {
					writeError(w, http.StatusUnauthorized, "token audience does not match this resource")
					return
				}
			}
```

`canonicalResourceURI` lives in `internal/api/oauth_handlers.go` (Task 8); `strings` is already imported in middleware.go.

Also mark the client as used on successful MCP calls (optional bookkeeping, same file or `mcp.go`): in `handleMCPToolsCall`, after scope validation, fire-and-forget:

```go
	_, _ = s.db.Pool.Exec(r.Context(),
		`UPDATE oauth_clients SET last_used_at = NOW() WHERE client_id = $1`, claims.ClientID)
```

(Only executed when `claims.ClientID != ""` — guard it.)

- [ ] **Step 2: Run the full OAuth test set**

Run: `go test ./internal/api/ -run 'TestOAuth' -v`
Expected: ALL PASS — including `TestOAuthFullFlowExecuteSQL` (happy path through middleware), `TestOAuthWrongAudienceRejected` (401), `TestOAuthTokenRejectedOutsideMCP` (403).

- [ ] **Step 3: Run the whole backend suite (PAT/session regressions)**

Run: `task test`
Expected: PASS — PATs and session JWTs are unaffected (the new branch only fires for tokens carrying `client_id`).

- [ ] **Step 4: Commit**

```bash
git add internal/api/middleware.go internal/api/mcp.go
git commit -m "feat(api): enforce RFC 8707 audience binding and MCP-only scope for OAuth tokens"
```

### Task 11: Scope-filtered `tools/list` and `tools/call`

**Files:**
- Modify: `internal/api/mcp.go`
- Test: `internal/api/mcp_oauth_test.go` (additions)

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/mcp_oauth_test.go`:

```go
func TestOAuthScopeFiltering(t *testing.T) {
	srv, jwt, clientID, _ := setupOAuthServer(t)
	code := oauthConsentApprove(t, srv, jwt, clientID)
	tok := oauthToken(t, srv, clientID, code)
	access := tok["access_token"].(string)

	// tools/list only shows query tools.
	rec := doJSON(t, srv, "POST", "/api/v1/mcp", access,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "execute_sql")
	require.NotContains(t, rec.Body.String(), "create_notebook")

	// tools/call with a write tool is rejected even if guessed by name.
	rec = doJSON(t, srv, "POST", "/api/v1/mcp", access,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_notebook","arguments":{"title":"nope"}}}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "not available")

	// PATs keep the full allowlist (scope filtering must not leak into them).
	patCode, patResp := doCreateToken(t, srv, jwt, "full-pat", "")
	require.Equal(t, http.StatusCreated, patCode, patResp)
	rec = doJSON(t, srv, "POST", "/api/v1/mcp", patResp["token"].(string),
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "create_notebook")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestOAuthScopeFiltering -v`
Expected: FAIL — `create_notebook` still listed/callable for scoped tokens

- [ ] **Step 3: Implement**

In `internal/api/mcp.go`, add:

```go
// mcpToolsForToken returns the tool set the caller may use. OAuth tokens
// (identified by ClientID) are limited to the union of their granted scopes;
// PATs and session JWTs keep the full allowlist. A nil map means "all".
func mcpToolsForToken(claims *auth.Claims) map[string]struct{} {
	if claims == nil || claims.ClientID == "" {
		return nil
	}
	return oauth.ToolsForScopes(oauth.ParseScopes(claims.Scope))
}
```

In `handleMCPToolsList` (after `mcpToolAllowed` check):

```go
	allowed := mcpToolsForToken(claims)
	for _, d := range defs {
		if d.Function.Name == "" || !mcpToolAllowed(d.Function.Name) {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[d.Function.Name]; !ok {
				continue
			}
		}
		...
	}
```

In `handleMCPToolsCall` (next to the `mcpToolAllowed` rejection, line ~274):

```go
	if allowed := mcpToolsForToken(claims); allowed != nil {
		if _, ok := allowed[params.Name]; !ok {
			writeJSON(w, http.StatusOK, mcpJSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Error: &mcpError{Code: -32602, Message: "Tool not available for the granted scopes: " + params.Name},
			})
			return
		}
	}
```

(`oauth` import: `github.com/the-heaven-labs/aether/internal/oauth`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestOAuth|MCP' -v && task test`
Expected: ALL PASS — including the pre-existing `TestMCPToolsListMatchesAllowlist` (the allowlist itself is unchanged; OAuth filtering only narrows).

- [ ] **Step 5: Commit**

```bash
git add internal/api/mcp.go internal/api/mcp_oauth_test.go
git commit -m "feat(api): scope-filtered MCP tool surface for OAuth clients"
```

---

## Part C — Consent UI, flag wiring and docs (PR 3)

### Task 12: Feature-flag config + `OAuthConsentPage`

**Files:**
- Modify: `internal/config/config.go` + `internal/config/config_test.go`
- Modify: `cmd/aether/main.go`
- Create: `web/src/pages/OAuthConsentPage.tsx`
- Modify: `web/src/App.tsx` (route)

- [ ] **Step 1: Config flag**

In `internal/config/config.go`, add to the `Config` struct:

```go
	MCPOAuthEnabled            bool          // serve the MCP OAuth 2.1 authorization-server endpoints (AETHER_MCP_OAUTH_ENABLED, default false)
```

In `load()`'s `cfg := &Config{...}` literal:

```go
		MCPOAuthEnabled:            envOrDefault("AETHER_MCP_OAUTH_ENABLED", "false") == "true",
```

In `internal/config/config_test.go`:

```go
func TestMCPOAuthEnabledDefault(t *testing.T) {
	t.Setenv("AETHER_MCP_OAUTH_ENABLED", "")
	cfg, err := LoadMigrateOnly() // no secrets required
	require.NoError(t, err)
	require.False(t, cfg.MCPOAuthEnabled)

	t.Setenv("AETHER_MCP_OAUTH_ENABLED", "true")
	cfg, err = LoadMigrateOnly()
	require.NoError(t, err)
	require.True(t, cfg.MCPOAuthEnabled)
}
```

(If `LoadMigrateOnly` requires more env in this repo's tests, mirror how existing config tests construct a minimal `Config`.)

In `cmd/aether/main.go`, next to the other `Set*` calls (Task 3 placed `SetMCPSQLTimeout` there):

```go
	apiServer.SetMCPOAuthEnabled(cfg.MCPOAuthEnabled)
```

- [ ] **Step 2: Run config tests**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 3: Consent page**

Create `web/src/pages/OAuthConsentPage.tsx` (match the styling conventions of existing pages — this version is deliberately plain):

```tsx
import { useEffect, useState } from 'react'
import { getToken } from '../api/client'

interface ConsentInfo {
  client_name: string
  org_name: string
  scopes: string[]
  scope_descriptions: Record<string, string>
}

export default function OAuthConsentPage() {
  const params = new URLSearchParams(window.location.search)
  const [info, setInfo] = useState<ConsentInfo | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const clientId = params.get('client_id') || ''
  const redirectUri = params.get('redirect_uri') || ''
  const scope = params.get('scope') || ''
  const resource = params.get('resource') || ''
  const state = params.get('state') || ''
  const challenge = params.get('code_challenge') || ''
  const challengeMethod = params.get('code_challenge_method') || ''

  useEffect(() => {
    if (!clientId || !redirectUri || !challenge || challengeMethod !== 'S256') {
      setError('Invalid authorization request.')
      return
    }
    if (!getToken()) {
      setError('Sign in to Aether first, then reconnect your MCP client.')
      return
    }
    fetch(
      `/api/v1/oauth/consent/info?client_id=${encodeURIComponent(clientId)}&scope=${encodeURIComponent(scope)}&resource=${encodeURIComponent(resource)}`,
      { headers: { Authorization: `Bearer ${getToken()}` } },
    )
      .then(async (r) => (r.ok ? r.json() : Promise.reject(new Error('Failed to load authorization request'))))
      .then(setInfo)
      .catch((e: Error) => setError(e.message))
  }, [clientId, redirectUri, challenge, challengeMethod, scope, resource])

  const decide = async (approve: boolean) => {
    setBusy(true)
    setError('')
    try {
      const r = await fetch('/api/v1/oauth/consent/decision', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${getToken()}` },
        body: JSON.stringify({
          client_id: clientId, redirect_uri: redirectUri, scope, resource, state,
          code_challenge: challenge, code_challenge_method: challengeMethod, approve,
        }),
      })
      if (!r.ok) throw new Error('Authorization failed')
      const { redirect } = await r.json()
      window.location.href = redirect
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  if (error) {
    return <div className="p-8 max-w-md mx-auto"><p className="text-red-600">{error}</p></div>
  }
  if (!info) {
    return <div className="p-8 max-w-md mx-auto"><p>Loading…</p></div>
  }
  return (
    <div className="p-8 max-w-md mx-auto">
      <h1 className="text-xl font-semibold mb-2">Authorize {info.client_name}</h1>
      <p className="mb-4 text-sm text-gray-600">
        {info.client_name} wants to access your organization <strong>{info.org_name}</strong> via the Aether MCP server.
      </p>
      <ul className="mb-6 space-y-2">
        {info.scopes.map((s) => (
          <li key={s} className="text-sm">
            <code>{s}</code> — {info.scope_descriptions[s] ?? s}
          </li>
        ))}
      </ul>
      <div className="flex gap-2">
        <button
          className="px-4 py-2 rounded bg-blue-600 text-white disabled:opacity-50"
          disabled={busy}
          onClick={() => decide(true)}
        >
          Allow
        </button>
        <button
          className="px-4 py-2 rounded border disabled:opacity-50"
          disabled={busy}
          onClick={() => decide(false)}
        >
          Deny
        </button>
      </div>
    </div>
  )
}
```

In `web/src/App.tsx`, add the route next to `/login` (public route — the page itself handles the not-signed-in case):

```tsx
      <Route path="/oauth/authorize" element={<OAuthConsentPage />} />
```

with the import at the top:

```tsx
import OAuthConsentPage from './pages/OAuthConsentPage'
```

- [ ] **Step 4: Build + web tests**

Run: `cd web && npm run build && npm run test:run` (or from repo root: `task test:web`)
Expected: build compiles; tests pass. If the repo has a component-test setup (`@testing-library/react`), add `web/src/pages/OAuthConsentPage.test.tsx` covering: error on missing params; consent renders scopes and Allow triggers the decision fetch — otherwise rely on the manual walkthrough in Task 14 and note it in the MR description.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go cmd/aether/main.go web/src/pages/OAuthConsentPage.tsx web/src/App.tsx
git commit -m "feat(web): OAuth consent page for MCP clients + AETHER_MCP_OAUTH_ENABLED flag"
```

### Task 13: Documentation

**Files:**
- Create: `docs/mcp-oauth.md`

- [ ] **Step 1: Write the doc**

Create `docs/mcp-oauth.md` covering:

1. **Overview** — Aether serves an MCP server at `https://{org-subdomain}.<host>/api/v1/mcp` (Streamable HTTP, POST-only) implementing the MCP Authorization spec (OAuth 2.1, RFC 9728/8414/7591/8707).
2. **Configuration** — `AETHER_MCP_OAUTH_ENABLED=true` to enable; `AETHER_MCP_SQL_TIMEOUT_MS` (default `600000`) for the `execute_sql` ceiling; `AETHER_RATE_LIMIT_OAUTH_REGISTER` (default 10/min) and `AETHER_RATE_LIMIT_OAUTH_TOKEN` (default 30/min).
3. **Scopes** — table of `mcp:query` / `mcp:read` / `mcp:write` and the tools each unlocks; users select scopes on the consent page.
4. **Connecting harnesses** — zero-config examples:

   OpenCode (`opencode.json`):

   ```json
   {
     "mcp": {
       "aether": {
         "type": "remote",
         "url": "https://{org}.aether.example.com/api/v1/mcp",
         "enabled": true
       }
     }
   }
   ```

   Claude Code: `claude mcp add --transport http aether https://{org}.aether.example.com/api/v1/mcp`

   Both start the browser OAuth dance on first use; the harness registers itself via DCR and refreshes tokens automatically.

5. **PAT alternative** — PATs keep working (`Authorization: Bearer aether_tok_…`) for curl/CI; OAuth tokens are only valid at the MCP endpoint.
6. **Security notes** — audience binding, refresh rotation + reuse revocation, 15 min access / 30 day refresh TTLs, subdomain org binding.

- [ ] **Step 2: Commit**

```bash
git add docs/mcp-oauth.md
git commit -m "docs: MCP OAuth 2.1 server configuration and harness setup"
```

### Task 14: Manual acceptance walkthrough

- [ ] **Step 1: Local stack**

```bash
docker compose up -d --build
# enable the flag for the api process, e.g. in docker-compose.dev.yml:
#   AETHER_MCP_OAUTH_ENABLED: "true"
```

- [ ] **Step 2: Zero-config harness connect**

1. Configure OpenCode with the remote URL (Task 13 example) pointed at `http://localhost:8088/api/v1/mcp` — note: loopback harness redirects make localhost flows work without TLS.
2. Trigger an MCP call (ask the agent to list tools). Expect: OpenCode opens the browser → Aether authorize page → sign in → consent page listing `mcp:query` → Allow → redirect to `http://localhost:<port>/callback` → OpenCode completes the exchange.
3. Ask the agent to run `execute_sql` against a dev connector — expect rows returned.
4. Reconnect after killing the token files — expect the harness to refresh silently (no browser).

- [ ] **Step 3: Negative checks**

- Deny on the consent screen → harness reports `access_denied`, no token stored.
- Only `mcp:query` granted → the harness's tool list contains no notebook-mutation tools.

---

## Coverage matrix (design → tasks)

| Design decision | Tasks |
|---|---|
| Aether as its own OAuth 2.1 AS, in-process | 8, 9 |
| Reuses existing login (password + OIDC SSO) via SPA session | 9, 12 |
| Scoped tools (`mcp:query`/`mcp:read`/`mcp:write`), allowlist stays outer boundary | 6, 11 |
| `mcp:write` included in v1 | 6 |
| Subdomain org binding (token + consent both org-pinned) | 9, 10 |
| Open DCR, public clients, PKCE S256, loopback/https redirects | 6, 8 |
| RFC 8707 audience binding, MCP-only OAuth tokens, no passthrough | 5, 10 |
| Stateful rotating refresh tokens, reuse → family revocation | 7, 8 |
| Access 15 min / refresh 30 d / code 60 s single-use | 7 |
| Dedicated rate limits for register/token; central login-tier for authorize/consent | 8 |
| Configurable `execute_sql` ceiling, single tool, agents keep 30 s | 1, 2, 3 |
| Feature-flagged rollout (`AETHER_MCP_OAUTH_ENABLED`, default off) | 7, 8, 12 |
| Zero-config harness onboarding (acceptance) | 13, 14 |

## Execution notes

- Each part is an independent MR: **Part A** is valuable standalone; **Part B** depends on nothing from A (its tests do not exercise the ceiling) but the plan orders it after so the timeout work is already reviewed; **Part C** depends on B.
- Run `task test` (backend) and `task test:web` (frontend) before every MR; conventional commits throughout.
- The handler code in Tasks 8/9 follows the repo's existing `writeJSON`/`writeError`/`s.rateLimit` conventions — if signatures drifted since `5e003fd`, adapt call sites, not behavior.
