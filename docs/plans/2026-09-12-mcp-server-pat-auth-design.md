# MCP Server for External Harnesses (PAT Auth) — Design

**Date:** 2026-09-12
**Status:** Approved

## Problem

Aether's agent panel is the only consumer of the built-in tool catalog. Users want to drive
Aether from external MCP harnesses (opencode, Claude Code, Cursor, etc.) using their own
account and permissions.

A JSON-RPC MCP endpoint already exists at `POST /api/v1/mcp`
(`initialize`/`tools/list`/`tools/call`) behind JWT/PAT auth, but it is not usable as a
supported integration surface:

- `tools/list` advertises the entire registry, including session-bound and interactive tools
  that cannot work without an agent session.
- Protocol gaps: no handling for `notifications/initialized` (returns `-32601`, which breaks
  most clients), no version negotiation, no `MCP-Protocol-Version` validation, no explicit
  `405` for `GET`/`DELETE`, no `WWW-Authenticate` on `401`.
- No setup documentation for harnesses.
- Only thin tests (`mcp_tools_test.go`).

## Goals

- External MCP clients connect over Streamable HTTP (POST JSON-RPC, JSON responses) using a
  personal access token.
- Both authentication methods are supported operationally: password and SSO users log in to
  the SPA, create a PAT, and configure the harness with a bearer header.
- Only a curated allowlist of tools is exposed. New catalog tools are never exposed
  automatically.
- Protocol behavior is close enough to the MCP spec for opencode, Claude Code, and generic
  clients to onboard and call tools.
- Zero new dependencies; stdlib `net/http`.

## Non-goals (deferred to a future OAuth design)

- OAuth 2.1 Authorization Server (`/oauth/authorize`, `/oauth/token`), authorization server
  metadata.
- RFC 9728 Protected Resource Metadata and the `resource_metadata` 401 challenge.
- Client registration: CIMD, DCR, pre-registration.
- Refresh tokens, scopes, audience/resource binding (RFC 8707).
- SSE/notifications, server-initiated messages, sessions/resumability.
- MCP prompts/resources primitives; stdio transport.
- Fixing Aether-as-MCP-client (outbound) — separate concern.

## Current state

| Piece | Location | Notes |
|---|---|---|
| MCP JSON-RPC handler | `internal/api/mcp.go` | `initialize` (advertises `2025-06-18`), `tools/list` (full registry), `tools/call` (registry lookup, `ToolContext` from claims, `ToolDef.Execute`, `isError` text), unknown method → `-32601` |
| Route | `internal/api/router.go` | `POST /api/v1/mcp` behind `authMW` |
| Auth | `internal/api/middleware.go` | JWT or `aether_tok_` PAT (bcrypt at rest in `api_tokens`), Bearer header or `?token=` |
| Registry / tool defs | `internal/agent/types.go`, `internal/agent/tools_*.go` | `ToolDef`, `ToolRegistry`, builtin catalog |
| Tests | `internal/api/mcp_tools_test.go` | `run_cell`, cancel, timeout |

## Decisions

- **D1 — PAT auth only for phase 1.** `api_tokens` is unchanged: org-scoped, optional expiry,
  bcrypt-hashed, revocable by deletion. A PAT acts as its user, so ACL checks in tool handlers
  are the authorization boundary. OAuth is designed separately later; the endpoint surface
  below is chosen to stay OAuth-ready.
- **D2 — Central allowlist, default deny.** A single `mcpToolAllowlist` map in
  `internal/api/mcp.go` enumerates exposed tool names. Filtering applies to both `tools/list`
  and `tools/call` (calling a non-allowlisted tool by name is rejected, not just hidden). Two
  drift tests: every allowlisted name must resolve in the registry (renames fail CI), and
  `tools/list` output must equal the allowlist exactly (new tools never auto-expose).
- **D3 — POST-only, JSON responses.** Keep the request/response body shape; respond
  `application/json` (never SSE). `GET`/`DELETE` on the endpoint return `405` with
  `Allow: POST`, which is how MCP clients learn there is no SSE stream and no session
  termination.
- **D4 — Version negotiation.** Support `2025-06-18`, `2025-11-25`, `2026-07-28`. On
  `initialize`, echo the requested `protocolVersion` when supported, otherwise return the
  latest supported. Subsequent requests carrying `MCP-Protocol-Version` are validated:
  unsupported → `400`; absent → accepted.
- **D5 — Curated catalog (45 tools).** Excluded tools are session/agent-bound or interactive:
  `ask_question`, `spawn_subagents`, `get_subagent_results`, `create_tasks`, `update_task`,
  `get_tasks`, `update_agent`. Dynamic `webhook`/`sql_query` tools are per-agent rows in
  `tools` and are not in the registry, so they remain internal-only.
- **D6 — Confirmations are the harness's concern.** `ToolDef.ConfirmRequired` is ignored over
  MCP; harnesses own tool-approval UX. Documented in `docs/mcp.md`.
- **D7 — No sessions.** Each POST is self-contained; no `Mcp-Session-Id` is issued.
- **D8 — Org binding.** The PAT's `org_id` is authoritative and the existing
  subdomain/token-org mismatch check continues to apply. Selecting an org during an OAuth
  consent flow is an OAuth-phase concern.

## Allowlist (45 tools)

- **Notebook/SQL (19):** `create_notebook`, `delete_notebook`, `update_notebook`,
  `read_cell`, `create_cell`, `update_cell`, `run_cell`, `list_cells`, `move_cell`,
  `swap_cells`, `execute_sql`, `explore_schema`, `delete_cell`, `get_notebook_context`,
  `create_snapshot`, `list_snapshots`, `restore_snapshot`, `list_notebook_parameters`,
  `set_notebook_parameters`
- **Dashboards/schedules/permissions/import-export (15):** `create_dashboard`,
  `list_dashboards`, `get_dashboard`, `update_dashboard`, `delete_dashboard`,
  `create_dashboard_widget`, `update_dashboard_widget`, `delete_dashboard_widget`,
  `create_schedule`, `delete_schedule`, `share_dashboard`, `read_permissions`,
  `update_permissions`, `export_notebook`, `import_notebook`
- **Skills/agents (5):** `list_skills`, `load_skill`, `create_skill`, `update_skill`,
  `list_agents`
- **Platform reads (4):** `list_notebooks`, `list_connectors`, `list_folders`,
  `get_folder_tree`
- **Charts (2):** `create_chart`, `update_chart`

## Protocol surface

| Request | Behavior |
|---|---|
| `initialize` | Version negotiation per D4; `capabilities.tools.listChanged=false`; `serverInfo` name `aether`, version from build |
| `notifications/initialized` (and any `notifications/*`) | HTTP `202`, empty body |
| `ping` | `{}` result |
| `tools/list` | Allowlist-filtered definitions with existing schema normalization |
| `tools/call` | Allowlisted tools only; `ToolContext` from claims; `ToolDef.Execute` (timeouts honored); success → text content with JSON result |
| Unknown method | JSON-RPC `-32601` |
| Malformed JSON | JSON-RPC `-32700` |
| Non-allowlisted / unknown tool | JSON-RPC `-32602` |
| Tool execution error | `result.isError = true` with text (existing behavior; lets the harness LLM recover) |
| Unauthenticated / bad token | HTTP `401` + `WWW-Authenticate: Bearer realm="aether"` |
| `GET` / `DELETE` | HTTP `405`, `Allow: POST` |
| Handler panic | Recovered and returned as JSON-RPC `-32603` (no dropped connection) |

Note: since no Protected Resource Metadata exists yet, OAuth-discovery-only clients cannot
onboard automatically; clients configured with a static Authorization header work and never
see the 401. This trade-off is documented.

## Auth UX and docs

PATs require no backend changes. New `docs/mcp.md` covers:

- Endpoint URL: `{AETHER_PUBLIC_URL}/api/v1/mcp` (or an org subdomain to pin the org).
- Creating a PAT from the UI or `aether tokens` after password or SSO login.
- opencode config snippet (`type: "remote"`, URL, `headers.Authorization`).
- `claude mcp add --transport http aether <url> --header "Authorization: Bearer aether_tok_…"`.
- A `curl` JSON-RPC smoke example.
- The allowlist, and that calls run as the token's user with ACLs enforced per resource.
- Security guidance: set an expiry, revoke by deleting, don't share tokens.

## Error handling

- Uniform JSON-RPC error codes as tabled above.
- Timeouts continue to surface as `tool "<name>" timed out after <duration>` through
  `ToolDef.Execute`.
- Nil-hook safety: allowlisted tools must not require `QuestionFunc`, session state, or event
  hooks; tests exercise allowlisted mutating tools over MCP to catch panics.

## Known limitations

- Per-tool `tools.config.timeout_ms` DB overrides are applied by the engine's
  `resolveToolDef`; the MCP path uses registry defs directly, so admin overrides do not apply
  to MCP calls. Documented; revisit if it matters.
- PAT-derived claims do not set `is_platform_admin` (pre-existing gap); no allowlisted tool
  depends on it.
- No progress notifications/SSE; long tools run silently until they return.

## Testing

Go tests in `internal/api/`:

- Protocol: version negotiation (supported and unsupported), `initialized` → `202`, `ping`,
  `MCP-Protocol-Version` validation, malformed JSON, unknown method, `GET`/`DELETE` → `405`,
  401 with `WWW-Authenticate`.
- Catalog: `tools/list` equals the allowlist; each allowlist entry resolves in the registry;
  a registered-but-unlisted probe tool is neither listed nor callable.
- Auth: PAT happy path, expired PAT, wrong-org PAT on a subdomain, JWT still works.
- End-to-end: notebook → cell → run SQL → read results over MCP; ACL-denied call returns an
  error.
- Manual verification against the dev stack with opencode and Claude Code; steps captured in
  `docs/mcp.md`. No new dependencies.

## Future work (OAuth phase)

A separate design will add a minimal OAuth 2.1 Authorization Server (new `internal/oauth`
package): authorization server metadata, an SPA-hosted consent page reusing the existing
password/SSO login (the SPA already holds the JWT, so no cookie infrastructure is needed), an
RFC 9728 Protected Resource Metadata document plus `resource_metadata` in the 401 challenge,
PKCE, refresh-token rotation, client registration via CIMD (SHOULD) with DCR backwards
compatibility (MAY), an org picker at consent, and optional scopes. None of it is built now.
