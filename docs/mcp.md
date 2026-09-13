# Using Aether from External MCP Harnesses

Aether exposes its built-in agent tools over the Model Context Protocol (MCP) at:

```
{AETHER_PUBLIC_URL}/api/v1/mcp
```

`AETHER_PUBLIC_URL` is your instance's public base URL — substitute it wherever
the examples below show `https://aether.example.com` (for a local dev stack,
`http://localhost:8088`).

The endpoint speaks MCP JSON-RPC over Streamable HTTP: `POST` only, plain JSON
responses, no SSE stream, no sessions. On an org subdomain (e.g.
`https://org1.aether.example.com/api/v1/mcp`) the token's org must match the
subdomain, otherwise the request is rejected.

Supported protocol versions are `2025-06-18`, `2025-11-25`, and `2026-07-28`.
Clients that send the `MCP-Protocol-Version` header must use one of these;
unsupported values are rejected with HTTP `400` and JSON-RPC `-32600`.

## 1. Create a personal access token

1. Log in to Aether with your password **or SSO**.
2. Open your profile menu → **Profile settings** → **Personal Access Tokens**
   (or run `aether tokens create --name opencode`).
3. Give it a clear name and, ideally, an expiry date (the web UI offers preset
   expiries; the CLI creates non-expiring tokens).

The raw token starts with `aether_tok_` and is shown only once. It is scoped to
the org you created it in and grants the same permissions as your account —
tool calls are ACL-checked as you.

Related CLI commands: `aether tokens list` and `aether tokens delete <id>`.

## 2. Configure your harness

### opencode

The snippet goes in `opencode.json` at the project root or
`~/.config/opencode/opencode.json`; merge it into an existing `mcp` block if
present.

```jsonc
{
  "mcp": {
    "aether": {
      "type": "remote",
      "url": "https://aether.example.com/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer aether_tok_REPLACE_ME"
      }
    }
  }
}
```

### Claude Code

```bash
claude mcp add --transport http aether https://aether.example.com/api/v1/mcp \
  --header "Authorization: Bearer aether_tok_REPLACE_ME"
```

## 3. Verify with curl

```bash
export AETHER_TOKEN=aether_tok_REPLACE_ME

curl -s https://aether.example.com/api/v1/mcp \
  -H "Authorization: Bearer $AETHER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | jq '.result.tools[].name'
```

This prints the 45 exposed tool names. To verify from a harness instead, run
`claude mcp list`.

## Exposed tools

The endpoint exposes a fixed allowlist (new internal tools are not exposed
automatically). The list is defined by `mcpToolAllowlist` in
`internal/api/mcp.go`. The current catalog (45 tools):

**Notebooks, cells & SQL** (19)

- `create_notebook`, `delete_notebook`, `update_notebook`
- `read_cell`, `create_cell`, `update_cell`, `delete_cell`, `run_cell`, `list_cells`, `move_cell`, `swap_cells`
- `execute_sql`, `explore_schema`, `get_notebook_context`
- `create_snapshot`, `list_snapshots`, `restore_snapshot`
- `list_notebook_parameters`, `set_notebook_parameters`

**Dashboards, schedules, permissions & import/export** (15)

- `create_dashboard`, `list_dashboards`, `get_dashboard`, `update_dashboard`, `delete_dashboard`
- `create_dashboard_widget`, `update_dashboard_widget`, `delete_dashboard_widget`
- `create_schedule`, `delete_schedule`, `share_dashboard`
- `read_permissions`, `update_permissions`
- `export_notebook`, `import_notebook`

**Skills & agents (read/authoring only)** (5)

- `list_skills`, `load_skill`, `create_skill`, `update_skill`, `list_agents`

**Platform reads** (4)

- `list_notebooks`, `list_connectors`, `list_folders`, `get_folder_tree`

**Charts** (2)

- `create_chart`, `update_chart`

Interactive and agent-session tools (`ask_question`, `spawn_subagents`,
`get_subagent_results`, `create_tasks`, `update_task`, `get_tasks`,
`update_agent`) and dynamic `webhook`/`sql_query` tools are **not** exposed.

## Permissions and errors

- Calls run as the token's user; ACLs are enforced per tool call.
- Tools that normally require interactive confirmation inside Aether (e.g.
  `update_cell`, `delete_cell`) execute immediately over MCP — Aether does not
  prompt; approval is the harness's responsibility.
- Tool execution failures return `result.isError = true` with a text message.
- Unknown tools return JSON-RPC `-32602`.
- Missing/expired/revoked tokens return HTTP `401` with
  `WWW-Authenticate: Bearer realm="aether"`.
- `GET`/`DELETE` return `405` (no SSE, no sessions).

## Known limitations

- Per-tool `tools.config.timeout_ms` admin overrides do not apply to MCP calls;
  tools use their registry default timeout.
- Long-running tools produce no progress output until they return.

## Security

- Treat a PAT like your password: don't share it, set an expiry, and revoke it
  (delete the token) when a harness no longer needs it.
- Tokens are stored as an HMAC-SHA-256 lookup hash plus bcrypt; because the
  lookup key derives from `AETHER_MASTER_KEY`, rotating that key invalidates
  tokens that carry a lookup hash. Tokens created before the lookup migration
  keep working via bcrypt and are transparently migrated on first use.
- OAuth 2.1 onboarding (browser consent, no manual token) is planned but not in
  this phase; harnesses must be configured with the static header today.
