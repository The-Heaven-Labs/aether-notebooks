# Design: OAuth 2.1 for the Aether MCP Server

- **Date:** 2026-09-29
- **Status:** Validated design (brainstormed against upstream `The-Heaven-Labs/aether-notebooks` @ `5e003fd`)
- **Upstream target:** new `internal/oauth` package + changes to `internal/api` MCP auth/tool dispatch

## 1. Problem

The ClickHouse Cloud MCP authenticates end users via OAuth and executes queries with the
user's ClickHouse Cloud identity. Aether outsources authentication and authorization to
itself: end users never hold ClickHouse Cloud credentials, so the ClickHouse MCP is
unusable for them. The equivalent capability must live in Aether.

Aether already ships an MCP server (`internal/api/mcp.go`):

- Streamable HTTP transport, POST-only JSON-RPC (no SSE/sessions; GET/DELETE → 405).
- Protocol versions `2025-06-18`, `2025-11-25`, `2026-07-28`.
- Curated tool allowlist (`mcpToolAllowlist`, `internal/api/mcp.go:57`) including
  `execute_sql` (connector_id + query, read-only guard, connector ACL `use` check,
  returns ResultSet + execution_id), `explore_schema`, `list_connectors`, plus the wider
  notebook/dashboard/skill surface.
- Auth: Bearer session JWT **or** PAT (`api_tokens`, org-scoped, lookup-hash;
  `internal/api/middleware.go`). Works, but requires the user to hand-create a PAT and
  paste it into the harness config.

Harnesses (OpenCode, Claude, ChatGPT connectors) implement the MCP Authorization spec
(OAuth 2.1 + RFC 9728 PRM + RFC 8414 AS metadata + RFC 7591 DCR + RFC 8707 resource
indicators). Aether has no OAuth authorization-server endpoints — it is an OIDC *client*
(SSO login) only. **The gap is OAuth, not the query tool.**

## 2. Decisions (validated)

| # | Question | Decision |
|---|---|---|
| 1 | Who issues MCP access tokens? | **Aether is its own OAuth 2.1 authorization server** (same process), reusing its existing login (password + OIDC SSO) inside `/oauth/authorize`. Mirrors how the ClickHouse console-backed MCP behaves. |
| 2 | Tool surface governance | **Scoped tools.** Access tokens carry scopes; `tools/list` and `tools/call` are filtered by scope on top of the existing allowlist. |
| 3 | Org resolution | **Subdomain binding.** `https://{org-slug}.host/mcp`; subdomain middleware resolves the org; authorize requires membership; token is bound to that org. Same model as PATs today (`TestMCPPATWrongOrgSubdomainRejected`). |
| 4 | Client registration | **Open DCR, public clients only.** PKCE S256 mandatory, no client secrets, redirect URIs restricted to `https://` or `http://localhost[:port]`, rate-limited. Zero-config onboarding for harnesses. |
| 5 | `mcp:write` scope in v1? | **Yes, include it.** Harnesses are expected to be query-only, but external-harness consolidation scenarios (e.g. a harness also wired to a Databricks MCP) justify write access; the user picked it on the consent screen. |
| 6 | Refresh token persistence | **Stateful.** Opaque refresh tokens hashed at rest, family-based rotation with reuse detection and family revocation. |
| 7 | Rate limits | **Reuse the existing central login-tier limits** for authorize/consent; DCR and token endpoints get dedicated, stricter central limits. |

## 3. Architecture

Aether plays two roles: **OAuth 2.1 resource server** (the MCP endpoint) and
**authorization server** (new endpoints, same process). No token passthrough: Aether
executes queries with its own connector credentials, never forwarding the MCP token.

```
Harness (OpenCode/Claude)          Aether
─────────────────────────          ──────
  1. POST /api/v1/mcp (no token)
        ─────────────────────▶  401 + WWW-Authenticate: Bearer
                                  resource_metadata="https://{host}/.well-known/oauth-protected-resource"
  2. GET /.well-known/oauth-protected-resource
        ─────────────────────▶  PRM (RFC 9728): resource, authorization_servers=[self],
                                 scopes_supported: mcp:read, mcp:query, mcp:write
  3. GET /.well-known/oauth-authorization-server
        ─────────────────────▶  AS metadata (RFC 8414): authorize/token/register,
                                 code_challenge_methods: [S256], grants: auth_code, refresh_token
  4. POST /oauth/register       DCR (RFC 7591) → client_id (public client)
  5. Browser: GET /oauth/authorize?...code_challenge(S256)&resource
        ─────────────────────▶  valid session cookie? consent UI : login (password or
                                 OIDC SSO) → consent → org = subdomain org (membership check)
  6. POST /oauth/token (code + verifier) → access JWT (~15 min) + opaque refresh (~30 d)
  7. tools/call with Bearer access token → scope-filtered tool dispatch → ResultSet
```

Canonical resource URI: the MCP server URL without trailing slash
(e.g. `https://{org-slug}.host/api/v1/mcp`). The `aud` claim of issued access tokens is
bound to it; the well-known endpoints derive their documents from the request `Host`, so
per-org subdomains each advertise their own canonical resource.

## 4. Endpoints

| Route | Auth | Purpose |
|---|---|---|
| `GET /.well-known/oauth-protected-resource` | none | PRM doc (RFC 9728) |
| `GET /.well-known/oauth-authorization-server` | none | AS metadata (RFC 8414) |
| `POST /oauth/register` | rate-limited | DCR: public clients; exact-match redirect URIs (`https://` or `http://localhost[:port]` only); stores `client_id`, `client_name`, `redirect_uris`, `created_at` |
| `GET /oauth/authorize` | session (or login redirect) | Validates `client_id`, exact `redirect_uri`, `code_challenge_method=S256`, `resource`; renders consent |
| `POST /oauth/consent` | session | User picks scopes → issues single-use, 60 s auth code (hashed at rest) bound to client + challenge + resource + org |
| `POST /oauth/token` | none (client params) | `authorization_code` (PKCE verify) and `refresh_token` (rotation) grants |

## 5. Data model (new migrations)

```
oauth_clients    (id, org_id NULL[DCR is org-agnostic], client_id UNIQUE, client_name,
                  redirect_uris TEXT[], created_at, last_used_at)
oauth_auth_codes (id, code_hash, client_id, user_id, org_id, scopes TEXT[],
                  resource, code_challenge, method, expires_at, used_at NULL)
oauth_tokens     (id, family_id, refresh_hash, client_id, user_id, org_id,
                  scopes TEXT[], resource, expires_at, revoked_at NULL,
                  replaced_by NULL, created_at)
```

- Refresh tokens: opaque, hashed (same `crypto.TokenLookupHash` pattern as PATs), 30 days.
- Rotation: every refresh issues a new row in the same `family_id`; presenting a
  superseded token revokes the whole family (reuse detection).
- Access tokens: stateless Aether JWTs, new claims `scope`, `aud` (resource URI),
  `client_id`, plus existing `sub`/`org_id`/`role`; 15-minute expiry.

## 6. Scopes → tool mapping

Enforced in `handleMCPToolsCall` alongside `mcpToolAllowed` (allowlist remains the outer
boundary — scopes can only narrow, never widen):

| Scope | Tools |
|---|---|
| `mcp:query` | `execute_sql`, `explore_schema`, `list_connectors` |
| `mcp:read` | read-only platform tools (`list_notebooks`, `list_cells`, `get_notebook_context`, `read_permissions`, `list_dashboards`, `get_dashboard`, `list_skills`, `list_agents`, …) |
| `mcp:write` | mutating tools (`create_notebook`, `run_cell`, snapshots, import/export, …) |

`tools/list` is filtered by the token's scopes. Per-call behavior inside tools (connector
ACL, read-only SQL guard, org scoping, per-user schema visibility) is untouched — it
already runs against `claims.UserID`/`OrgID`.

PATs and session JWTs keep the full allowlist behavior unchanged.

## 7. `execute_sql` configurable timeout (single tool, no fork)

Current behavior: `ToolDef.Execute` (`internal/agent/types.go:275`) wraps every call with
`Timeout` (execute_sql declares 30 s), overridable by `TimeoutFromArgs(args)` — used by
`run_cell` (clamped to `maxToolTimeoutMs` = 10 min). The MCP path calls the same
`def.Execute` (`internal/api/mcp.go:311`), so harness callers inherit the 30 s cap.
`TimeoutFromArgs` receives only args, not `ToolContext`, so it cannot distinguish callers.

Design — context-aware ceiling, one tool:

1. Add `QueryTimeoutCeiling time.Duration` to `ToolContext` (agent paths leave it zero →
   resolves to 30 s; behavior unchanged).
2. `handleMCPToolsCall` sets it from new server config `AETHER_MCP_SQL_TIMEOUT_MS`
   (default `600000` = 10 min, matching `maxToolTimeoutMs`).
3. `execute_sql` moves enforcement into its handler: deadline = `timeout_ms` arg if
   supplied, else the ceiling default, clamped to the ceiling; declare the def
   `Timeout: NoTimeout` to avoid double wrapping.

Result: `execute_sql{connector_id, query, limit, timeout_ms}` for harnesses; in-Aether
agents keep their 30 s guardrail (they are pushed toward cells by design).

## 8. Security

- **Audience binding:** the OAuth-token path requires `aud` == canonical resource URI;
  session JWTs/PATs skip this check (they are not OAuth tokens).
- **No token passthrough** (MCP security best practice / confused-deputy).
- **PKCE S256 mandatory**, `plain` rejected; auth codes single-use, hashed, 60 s TTL.
- **Refresh rotation** with family revocation on reuse.
- **Redirect URIs:** exact match, `https://` or loopback only (no open redirector).
- **Subdomain binding preserved:** resource/host mismatch → 401; org mismatch between
  token and subdomain → 403 (parity with PATs).
- **Rate limits:** authorize/consent reuse the central login-tier limits; DCR + token
  endpoints get dedicated stricter central limits.
- Access 15 min / refresh 30 d — bounded blast radius vs. an unexpiring PAT in a config
  file; PATs remain supported for curl/CI users.

## 9. Rollout (each phase shippable)

1. **Timeout fix** — `QueryTimeoutCeiling` + handler clamp. Independently useful; small
   upstream PR.
2. **OAuth AS core** — `internal/oauth` package, discovery endpoints, DCR,
   authorize/consent/token, `oauth_*` migrations, middleware `aud`/scope validation.
   Feature-flagged `AETHER_MCP_OAUTH_ENABLED` (default **off**).
3. **Consent UI + scope-filtered `tools/list`** — web frontend, same flag.

All implementation lands upstream (feature branches → MRs, conventional commits).

## 10. Testing

- **Unit:** PKCE verification; code single-use/expiry; refresh rotation + reuse-family
  revocation; scope→tool filter; `aud` validation; timeout clamping (agent 30 s / MCP
  ceiling / `timeout_ms` arg); redirect-URI allowlist.
- **Integration** (httptest, `mcp_pat_test.go` style): full DCR → authorize → token →
  `tools/call execute_sql` happy path; wrong-`aud` 401; expired access + refresh; subdomain
  org mismatch 403; scope-filtered `tools/list`; consent denial.
- **Manual acceptance:** wire OpenCode to a `docker compose` dev instance — the harness
  completes the browser dance with zero manual client config.

## 11. Out of scope

- SSE/streamable sessions on the MCP endpoint (stays POST-only).
- Confidential clients / client credentials grant.
- Delegating token issuance to a corporate IdP (rejected: no DCR in most IdPs, per-org
  burden, breaks password-login orgs).
