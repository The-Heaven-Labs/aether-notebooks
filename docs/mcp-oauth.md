# MCP OAuth 2.1

Aether can act as its own OAuth 2.1 authorization server for MCP clients.
Harnesses that implement the [MCP Authorization
spec](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization)
(OpenCode and Claude Code are the tested paths) discover Aether from the MCP
endpoint, register themselves via Dynamic Client Registration, and complete a
browser consent flow on first use — no manual token creation or config editing.
The feature is **off by default**; see [Configuration](#2-configuration).

PAT-based (static header) MCP setup remains fully supported and is documented
separately in [`docs/mcp.md`](mcp.md).

## 1. Overview

The MCP endpoint is:

```
https://{org-subdomain}.{host}/api/v1/mcp
```

It speaks MCP JSON-RPC over Streamable HTTP: `POST` only, plain JSON
responses, no SSE stream, no sessions (`GET`/`DELETE` return `405`). As with
PATs, the organization is resolved from the subdomain.

Aether implements the MCP Authorization spec with these endpoints and
primitives:

| Piece | Surface |
|---|---|
| Protected Resource Metadata (RFC 9728) | `GET /.well-known/oauth-protected-resource` |
| Authorization Server Metadata (RFC 8414) | `GET /.well-known/oauth-authorization-server` |
| Dynamic Client Registration (RFC 7591) | `POST /oauth/register` |
| Authorization endpoint | `GET /oauth/authorize` |
| Token endpoint | `POST /oauth/token` (`authorization_code` + `refresh_token`) |
| Resource indicators (RFC 8707) | `aud` claim on access tokens == canonical resource URI |

The `401` challenge from the MCP endpoint is
`WWW-Authenticate: Bearer realm="aether",
resource_metadata="http://{host}/.well-known/oauth-protected-resource"`, so
harnesses can discover the metadata document directly from the challenge (they
may also probe the standard well-known paths at the host root). Other API
endpoints emit `WWW-Authenticate: Bearer realm="aether"` only.

The browser flow, all on the org's host:

1. The harness calls `/api/v1/mcp` without a token and gets `401`.
2. It fetches the `/.well-known/...` documents and registers itself via DCR
   (public client, no client secret).
3. It opens the browser at `/oauth/authorize` with its `client_id`, PKCE
   `code_challenge` (S256), `redirect_uri`, requested `scope`, and `resource`.
4. If you are not signed in, the page asks you to sign in (password or OIDC
   SSO), then shows the consent screen for the requesting client and the org
   the request resolves to (the subdomain's org, or your current org on the
   default host).
5. On **Allow**, Aether redirects back to the harness with a single-use
   authorization code. The harness exchanges the code plus its PKCE verifier
   at `/oauth/token` for a 15-minute access token and a 30-day refresh token,
   and refreshes silently thereafter.

Discovery documents are derived from the request `Host`, so each org subdomain
advertises its own canonical resource. Behind a reverse proxy, forward `Host`
and `X-Forwarded-Proto` so the advertised resource matches the URL clients use.

## 2. Configuration

| Variable | Default | Purpose |
|---|---|---|
| `AETHER_MCP_OAUTH_ENABLED` | `false` | Serve the OAuth authorization-server endpoints (`/.well-known/oauth-*`, `/oauth/*`). When off, all of them return `404`; PAT/session-JWT access to `/api/v1/mcp` is unaffected. |
| `AETHER_MCP_SQL_TIMEOUT_MS` | `600000` (10 min) | Ceiling for `execute_sql` calls that arrive over MCP. Values below `1000` are rejected at startup. In-app agents keep their 30s default; a per-call `timeout_ms` argument is clamped to this ceiling. |
| `AETHER_RATE_LIMIT_OAUTH_REGISTER` | `10`/min | Per-IP rate limit on Dynamic Client Registration. |
| `AETHER_RATE_LIMIT_OAUTH_TOKEN` | `30`/min | Per-IP rate limit on the token endpoint. |

### Dev & testing

The Docker dev stack enables the flag by default (`docker-compose.dev.yml`),
so no configuration is needed there. Production defaults to off
(`AETHER_MCP_OAUTH_ENABLED=false`); set it to `true` to expose the endpoints.

For a non-Docker foreground run, `AETHER_MCP_OAUTH_ENABLED=true task dev` works
too — but run `task build:web` first: `task dev` embeds `web/dist` into the
server, and the consent page is served from that embedded frontend.

Point the harness at the **API origin**, not the Vite dev server — Vite only
proxies `/api`, `/internal`, `/docs`, and `/swagger.json`; the OAuth discovery
and token endpoints are served by the Go server itself:

- `http://localhost:8088/api/v1/mcp` for the default host, or
- `http://{org}.aether.test:8088/api/v1/mcp` for a subdomain org (see the
  subdomain testing notes in `AGENTS.md`).

Plain-HTTP loopback is fine in development: the canonical resource uses the
request scheme/host, and DCR accepts `http://localhost` / `127.0.0.1` redirect
URIs, so a harness on the same machine can complete the browser flow.

### Troubleshooting

- **Discovery returns `404`.** Check `AETHER_MCP_OAUTH_ENABLED=true` is actually
  in the running API process (recreate the container after editing the compose
  file). Clients fall back to `GET /.well-known/oauth-protected-resource` at
  the **resource origin** — the scheme/host of the MCP URL — so a proxy or Vite
  dev server that does not forward `/.well-known` will 404 too.

## 3. Scopes

Aether's MCP endpoint exposes a fixed 45-tool allowlist. OAuth access tokens
carry scopes (requested by the client at authorization time, shown on the
consent page, and stored in the token) that **narrow** that allowlist — scopes
can never unlock a tool outside it. `tools/list` only returns tools the token's
scopes unlock, and `tools/call` rejects out-of-scope tools even if the name is
guessed. The consent page lists each requested scope with a description and
lets you allow or deny the request.

| Scope | Consent description | Unlocks |
|---|---|---|
| `mcp:query` | Run read-only SQL queries against your connectors | The SQL data plane: `execute_sql`, `explore_schema`, `list_connectors`. |
| `mcp:read` | View notebooks, dashboards and other resources | Read-only platform tools: `list_notebooks`, `list_cells`, `get_notebook_context`, `list_snapshots`, `list_notebook_parameters`, `list_dashboards`, `get_dashboard`, `list_skills` / `load_skill`, `list_agents`, `list_folders` / `get_folder_tree`, `read_permissions`, `list_connectors`. |
| `mcp:write` | Create and modify notebooks and dashboards | Mutating platform tools: notebook/cell authoring and execution (`create_notebook`, `update_cell`, `run_cell`, `read_cell`, snapshots, parameters), dashboards and widgets, schedules, `share_dashboard` / `update_permissions`, `export_notebook` / `import_notebook`, skills, and charts. |

Notes:

- `mcp:query` is independent of the platform scopes: a query-only token can run
  SQL without being able to see the notebook UI surface.
- Unknown scopes are dropped; a token with no valid scopes is fail-closed and
  can list and call nothing.
- PATs and session JWTs are **not** scope-filtered — they keep the full
  allowlist.
- The exhaustive scope→tool mapping lives in `internal/oauth/scopes.go`.

## 4. Connecting harnesses

Zero-config setup: both harnesses below support the MCP Authorization spec, so
they register themselves via DCR and start the browser consent flow on first
use, then refresh tokens automatically. Substitute your host and org subdomain
(the dev equivalents are in [Configuration](#2-configuration)).

### OpenCode (`opencode.json`)

At the project root or `~/.config/opencode/opencode.json`; merge into an
existing `mcp` block if present:

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

### Claude Code

```bash
claude mcp add --transport http aether https://{org}.aether.example.com/api/v1/mcp
```

Then use the harness (`/mcp` inside Claude Code, or simply the first tool
call) to trigger the browser flow. Sign in to Aether and click **Allow** on
the consent page; the harness stores the refresh token and reconnects silently
on later runs.

## 5. PAT alternative

OAuth is additive. Personal access tokens keep working at the MCP endpoint as
`Authorization: Bearer aether_tok_…` and remain the right choice for curl, CI,
and other headless callers that cannot run a browser flow. PATs are org-scoped
and carry your full ACL permissions without scope filtering. See
[`docs/mcp.md`](mcp.md) for creating a PAT and configuring harnesses with it.

OAuth access tokens are narrower than PATs by construction:

- They are accepted **only** at `POST /api/v1/mcp`. Every other API path
  rejects them even though the JWT itself is valid — `403` on the REST APIs
  (including `/api/v1/mcp-servers`), `401` on the `/internal/*` relay
  endpoints.
- They are bound to one org (the subdomain's org when one is in use, otherwise
  your current org at consent time) and one resource URI (`aud`).
- They cannot enable admin mode; ACL checks still run per tool call.

## 6. Security notes

- **Audience binding (RFC 8707).** Access tokens carry
  `aud = https://{host}/api/v1/mcp` for the host that issued them. Presenting a
  token to a different host fails the audience check with `401`.
- **MCP-only tokens.** An OAuth access token presented anywhere except the MCP
  endpoint is rejected with `403` — including via the WebSocket-style
  `?token=` query parameter.
- **Short-lived access, bounded refresh.** Access tokens expire after 15
  minutes; refresh tokens after 30 days. Authorization codes are single-use,
  expire after 60 seconds, and are stored hashed at rest; replaying a used or
  expired code fails.
- **Refresh rotation with reuse revocation.** Every refresh issues a new
  refresh token in the same family and marks the old one replaced. Presenting a
  superseded or revoked token revokes the whole family (all descendants die).
  Refresh tokens are also bound to the client they were issued to; another
  client gets `invalid_grant`.
- **PKCE is mandatory** (S256 only; `plain` is rejected). Dynamic Client
  Registration accepts public clients only (`token_endpoint_auth_method=none`;
  secrets are refused), redirect URIs must be exact matches, and are limited to
  `https://` or loopback HTTP (`localhost`, `127.0.0.1`, `::1`) with no
  fragment — no open redirector.
- **Subdomain org binding.** On an org subdomain, consent is granted to that
  org and the user must be a member; a token whose org does not match the
  request subdomain is rejected with `403` (on the default host the org is the
  session's current org). Membership is re-checked on every refresh, so
  removing a user from the org kills their refresh chains — the same model as
  PATs.
- **No token passthrough.** Aether executes queries with its own connector
  credentials; the OAuth token is never forwarded to the data source
  (confused-deputy protection).
- **Rate limits.** DCR and token endpoints are rate-limited per client IP
  (defaults `10`/min and `30`/min; see [Configuration](#2-configuration)).
