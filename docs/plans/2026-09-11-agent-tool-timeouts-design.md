# Agent Tool Timeouts — Design

**Date:** 2026-09-11
**Status:** Approved (design decisions D9–D11 settled)

## Problem

Built-in tools have no execution budget in the registry. Only a few paths carry hardcoded
timeouts (`execute_sql`/`sql_query` 30s, webhook 30s, MCP 30/60s), a handful respect
`timeout_ms`, and `explore_schema` plus several DB paths can hang indefinitely. The only hard
bound is the 30-minute WS turn context, so a single stuck tool can consume an entire turn.
Some code paths escape even that (`NewPostgresExecutor` uses `context.Background()`;
auto-snapshots spawn with `context.Background()`).

## Goals

- Every built-in tool has an explicit default timeout, overridable administratively.
- Any tool timeout surfaces a clean, consistent error to the LLM and the UI.
- Interactive and intentionally long-running tools can opt out explicitly.
- SQL ad-hoc tools stay short (light exploration); `run_cell` remains the long-query path
  with connector-aware defaults.

## `ToolDef` API

```go
// DefaultToolTimeout applies when a tool declares Timeout == 0.
// Overridable via AETHER_AGENT_TOOL_TIMEOUT_DEFAULT (default 120s).
const DefaultToolTimeout = 120 * time.Second

// NoTimeout marks interactive tools that must not be wrapped.
const NoTimeout = -1 * time.Second

type ToolDef struct {
    // ... existing fields ...
    Timeout time.Duration `json:"-"`
    // TimeoutFromArgs optionally extracts a per-call timeout (e.g. timeout_ms).
    TimeoutFromArgs func(json.RawMessage) (time.Duration, bool) `json:"-"`
}

// Execute derives a deadline context, calls Handler, and maps wrapper-caused
// deadlines to "tool <name> timed out after <d>".
func (t *ToolDef) Execute(args json.RawMessage, tc *ToolContext) (any, error)
```

`Execute` shallow-copies the `ToolContext` and swaps in the derived context (handlers never
mutate `ToolContext` in place except through hooks). A timeout error is only synthesized when
the handler returned an error **and** the wrapper’s deadline fired (parent context still
alive), so handlers that map deadlines to structured results (e.g. `run_cell`’s `timed_out`)
keep their semantics.

### Precedence (highest wins)

1. Per-call argument via `TimeoutFromArgs` (e.g. `timeout_ms`, clamped by the handler cap).
2. Per-tool DB override `tools.config.timeout_ms` (admin config, applies to builtin and
   dynamic tools).
3. Registry default `ToolDef.Timeout`.
4. Global fallback `AETHER_AGENT_TOOL_TIMEOUT_DEFAULT` (default 120s).
5. `NoTimeout` (-1) skips 1–4 entirely.

`resolveToolDef` must **clone** `*ToolDef` before applying the DB override; registry entries
are shared across orgs/sessions and must not be mutated. A test asserts all registered
builtins declare a non-zero timeout (with `NoTimeout` for the two interactive tools), so new
tools cannot silently ship without a budget.

## Enforcement sites

All four dispatch paths use `ToolDef.Execute`:

| Site | Location |
|---|---|
| Main agent loop | `internal/agent/engine.go` tool dispatch |
| Legacy subagent loop | `internal/agent/subagent.go` (`runSubagent`) |
| Subagent loop | `internal/agent/subagent.go` (`runSubagentLoop`) |
| MCP JSON-RPC endpoint | `internal/api/mcp.go` |

## Defaults

| Category | Tools | Timeout |
|---|---|---|
| In-memory task tools | `create_tasks`, `update_task`, `get_tasks` | 5s |
| CRUD / listing | all `list_*`, `read_cell`, `get_dashboard`, `read_permissions`, notebook/cell/dashboard/widget/schedule/chart/skill/agent create-update-delete, `share_dashboard`, `get_subagent_results` | 15s |
| Heavier internal work | `update_permissions`, `get_folder_tree`, `list_agents`, `get_notebook_context`, `update_cell` (Yjs) | 30s |
| Snapshots / schema / export | `explore_schema`, `create_snapshot`, `restore_snapshot`, `export_notebook` | 60s |
| Bulk import | `import_notebook` | 120s |
| Ad-hoc SQL | `execute_sql`, `sql_query` | 30s fixed |
| Cell SQL | `run_cell`, `create_cell(run=true)` | connector `timeout_seconds` if set, else 5m; `timeout_ms` overrides (cap 10m) |
| Webhook tools | dynamic webhook | 30s |
| MCP tools | `list_tools` / `call_tool` | 30s / 60s |
| Subagents | `spawn_subagents` | **NoTimeout** (D9) |
| Interaction | `ask_question` | **NoTimeout** (user-dependent) |

Tool confirmations are not tool executions; they stay bounded by the turn context.

### SQL policy

- `execute_sql` and `sql_query` remain short (30s) regardless of connector timeout; they are
  for light log exploration and schema understanding. The `execute_sql` description already
  advertises the 30s budget and points at `create_cell(run=true)`; keep it aligned.
- `run_cell` / `create_cell(run=true)`: connector `timeout_seconds` becomes the default
  (it is currently ignored by agent SQL paths), falling back to 5m; the `timeout_ms` argument
  still overrides with a 10m cap. The registry `Timeout` for these tools is the 10m hard cap
  so the wrapper never fires before the handler’s own deadline.
- Fix adjacent bug: `execute_sql` parses `limit` but ignores it (hardcoded 1000 in
  `executeAgentSQL`); thread the value through and document the cap.

## Fixing context escapes

- `NewPostgresExecutor` / `Ping` (`internal/executor/postgres.go`): pass the caller context
  (or set `connect_timeout`) instead of `context.Background()`.
- Auto-snapshots spawned from `delete_cell` / `delete_notebook` (`tools_notebook.go`): keep
  background execution but attach their own ~5m timeout context.
- `explore_schema`: now covered by 60s (it had none). OpenSearch’s per-index `DESCRIBE` loop
  inherits the tool context.
- ClickHouse connect ping already has 10s; keep.

## Error surfacing

- Keyboard-stable message: `tool "<name>" timed out after <duration>`.
- Applies uniformly to: main loop tool messages, subagent tool content, MCP `isError`
  responses, and persisted `ToolCall.Error` in `agent_messages.tool_calls`.
- The frontend already renders tool errors from `tool_result`; no protocol change needed.

## Config

- `AETHER_AGENT_TOOL_TIMEOUT_DEFAULT` (Go duration, default `120s`): fallback for tools with
  no explicit budget. Documented in `AGENTS.md`.
- `tools.config.timeout_ms`: per-tool override editable through the existing tool config JSON
  (admins). No new UI required in this change.

## Tests

- `TestAllBuiltinToolsHaveTimeout`: every registry tool declares `Timeout != 0`, with
  `ask_question` and `spawn_subagents` on `NoTimeout`.
- Dispatch tests for all four sites using a hanging probe handler + short timeout; assert
  the clean timeout error reaches the tool result and DB record.
- Config override test: `tools.config.timeout_ms` beats the registry default without mutating
  the shared registry def.
- `run_cell` regression: `timeout_ms` still produces the structured `timed_out` result, and a
  connector-timeout default applies when the arg is absent.
- `execute_sql` limit plumbing test.

## Dependencies

- The subagent UX design relies on `NoTimeout` classification to exclude `spawn_subagents`
  from the 120s stale-tool watchdog.
- The ClickHouse guidance design adds `explore_schema` to the seeded builtins; it must be
  registered with its 60s timeout.
