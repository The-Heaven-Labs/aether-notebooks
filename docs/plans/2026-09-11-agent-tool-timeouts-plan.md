# Agent Tool Timeouts Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Give every built-in agent tool an explicit, overridable execution timeout so no tool can hang a turn indefinitely.

**Architecture:** Add `Timeout` + `TimeoutFromArgs` to `ToolDef` and a central `Execute` wrapper that derives a deadline context and maps wrapper-caused deadlines to a consistent error. Resolve per-tool DB overrides by cloning defs in `resolveToolDef`. Route all four dispatch sites through `Execute`. Hardcode sensible defaults per tool, fix known context escapes, and expose a global fallback env.

**Tech Stack:** Go (net/http, pgx), existing agent engine/registry, `testify/require`, real Postgres tests via `task test`.

**Design doc:** `docs/plans/2026-09-11-agent-tool-timeouts-design.md`

---

### Task 1: `ToolDef.Execute` + timeout constants

**Files:**
- Modify: `internal/agent/types.go`
- Test: `internal/agent/types_timeout_test.go` (create)

**Step 1: Write the failing tests**

```go
package agent

import (
    "context"
    "encoding/json"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

func testTool(name string, timeout time.Duration, h ToolHandler) *ToolDef {
    d := &ToolDef{Timeout: timeout, Handler: h}
    d.Function.Name = name
    return d
}

func TestToolDefExecuteAppliesTimeout(t *testing.T) {
    d := testTool("hang", 50*time.Millisecond, func(_ json.RawMessage, tc *ToolContext) (any, error) {
        <-tc.Context.Done()
        return nil, tc.Context.Err()
    })
    _, err := d.Execute(nil, &ToolContext{Context: context.Background()})
    require.Error(t, err)
    require.Contains(t, err.Error(), `tool "hang" timed out after 50ms`)
}

func TestToolDefExecuteNoTimeout(t *testing.T) {
    d := testTool("interactive", NoTimeout, func(_ json.RawMessage, tc *ToolContext) (any, error) {
        return "ok", nil
    })
    out, err := d.Execute(nil, &ToolContext{Context: context.Background()})
    require.NoError(t, err)
    require.Equal(t, "ok", out)
}

func TestToolDefExecutePreservesHandlerDeadlineResult(t *testing.T) {
    d := testTool("graceful", 20*time.Millisecond, func(_ json.RawMessage, tc *ToolContext) (any, error) {
        <-tc.Context.Done()
        return map[string]any{"timed_out": true}, nil
    })
    out, err := d.Execute(nil, &ToolContext{Context: context.Background()})
    require.NoError(t, err)
    require.Equal(t, map[string]any{"timed_out": true}, out)
}

func TestToolDefExecuteTimeoutFromArgs(t *testing.T) {
    d := testTool("argtimeout", 5*time.Second, func(_ json.RawMessage, tc *ToolContext) (any, error) {
        <-tc.Context.Done()
        return nil, tc.Context.Err()
    })
    d.TimeoutFromArgs = func(args json.RawMessage) (time.Duration, bool) {
        var req struct{ TimeoutMs int `json:"timeout_ms"` }
        if json.Unmarshal(args, &req) != nil || req.TimeoutMs <= 0 {
            return 0, false
        }
        return time.Duration(req.TimeoutMs) * time.Millisecond, true
    }
    _, err := d.Execute(json.RawMessage(`{"timeout_ms":25}`), &ToolContext{Context: context.Background()})
    require.Error(t, err)
    require.Contains(t, err.Error(), "timed out after 25ms")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/agent/ -run 'TestToolDefExecute' -v`
Expected: FAIL — `Timeout`/`Execute` undefined.

**Step 3: Implement**

In `internal/agent/types.go`, add imports `context`, `time`, `errors`. Extend `ToolDef`:

```go
// DefaultToolTimeout applies when a tool declares Timeout == 0 (e.g. a
// custom test tool). Tools should normally declare an explicit budget.
const DefaultToolTimeout = 120 * time.Second

// NoTimeout marks interactive/long-running tools that must not be wrapped.
const NoTimeout = -1 * time.Second

type ToolDef struct {
    Type     string `json:"type"`
    Function struct {
        Name        string `json:"name"`
        Description string `json:"description"`
        Parameters  any    `json:"parameters"`
    } `json:"function"`
    Handler         ToolHandler `json:"-"`
    ConfirmRequired bool        `json:"-"`
    // Timeout is the default execution budget. 0 falls back to
    // DefaultToolTimeout; NoTimeout (-1) disables wrapping.
    Timeout time.Duration `json:"-"`
    // TimeoutFromArgs overrides Timeout when the caller supplied one
    // (e.g. run_cell's timeout_ms).
    TimeoutFromArgs func(json.RawMessage) (time.Duration, bool) `json:"-"`
}

// Execute runs the handler under the effective timeout and normalizes
// wrapper-caused deadline errors.
func (t *ToolDef) Execute(args json.RawMessage, tc *ToolContext) (any, error) {
    timeout := t.Timeout
    if t.TimeoutFromArgs != nil {
        if d, ok := t.TimeoutFromArgs(args); ok {
            timeout = d
        }
    }
    if timeout == 0 {
        timeout = DefaultToolTimeout
    }
    if timeout < 0 {
        return t.Handler(args, tc)
    }
    runCtx, cancel := context.WithTimeout(tc.Context, timeout)
    defer cancel()
    copy := *tc
    copy.Context = runCtx
    result, err := t.Handler(args, &copy)
    if err != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) && tc.Context.Err() == nil {
        return result, fmt.Errorf("tool %q timed out after %s", t.Function.Name, timeout)
    }
    return result, err
}
```

Replace the old “NOTE: there is deliberately no timeout field” comment.

**Step 4: Run tests**

Run: `go test ./internal/agent/ -run 'TestToolDefExecute' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/types.go internal/agent/types_timeout_test.go
git commit -m "feat(agent): add ToolDef execution timeouts and Execute wrapper"
```

---

### Task 2: Env fallback + per-tool DB config override

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/agent/engine.go` (`NewEngine`, `resolveToolDef`)
- Modify: callers of `agent.NewEngine` (find with `rg "NewEngine\\(" cmd internal`)
- Test: `internal/agent/engine_tools_test.go` (extend)

**Step 1: Failing test**

Add a test that resolves a builtin DB row whose `config` includes `{"handler_name":"...","timeout_ms":5000}` and asserts the returned `ToolDef.Timeout == 5*time.Second` while the registry def is unchanged. Follow the existing `TestEngineLoadAgentToolDefs_*` setup in `engine_tools_test.go`.

**Step 2: Run to fail**

Run: `go test ./internal/agent/ -run TestEngineLoadAgentToolDefs_TimeoutOverride -v`
Expected: FAIL

**Step 3: Implement**

- `config.go`: add `AgentToolTimeoutDefault time.Duration` loaded from `AETHER_AGENT_TOOL_TIMEOUT_DEFAULT` (parse Go duration, default `120s`, floor 1s).
- `Engine`: add field `toolTimeoutDefault time.Duration`, initialize to `agent.DefaultToolTimeout` in `NewEngine`; add `func (e *Engine) SetToolTimeoutDefault(d time.Duration)`.
- `cmd/aether-server/main.go` (where the engine is constructed): call `SetToolTimeoutDefault(cfg.AgentToolTimeoutDefault)`.
- `resolveToolDef` builtin branch: copy the def before mutating:

```go
def0, ok := e.registry.Get(handlerName)
if !ok { ... }
def := *def0
if ms, ok := t.Config["timeout_ms"].(float64); ok && ms > 0 {
    def.Timeout = time.Duration(ms) * time.Millisecond
} else if def.Timeout == 0 {
    def.Timeout = e.toolTimeoutDefault
}
return &def, nil
```

- `resolveToolDef` webhook/sql_query branches: after `make*ToolDef`, apply the same `timeout_ms` override (helper `applyToolTimeout(def, t.Config, e.toolTimeoutDefault)`).

**Step 4: Pass + commit**

Run: `go test ./internal/agent/ -run TestEngineLoadAgentToolDefs -v`

```bash
git add internal/config/config.go internal/agent/engine.go cmd/aether-server/main.go internal/agent/engine_tools_test.go
git commit -m "feat(agent): tool timeout env fallback and per-tool config override"
```

---

### Task 3: Declare defaults on all builtin registrations

**Files:**
- Modify: `internal/agent/tools_notebook.go`
- Modify: `internal/agent/tools_agent.go`
- Modify: `internal/agent/tools_platform.go`
- Modify: `internal/agent/tools_chart.go`
- Modify: `internal/agent/tools_manage.go`
- Modify: `internal/agent/tools_webhook.go`
- Modify: `internal/agent/tools_sql.go`
- Modify: `internal/agent/engine.go` (MCP dynamic defs at ~703-740)
- Test: `internal/agent/tools_timeout_catalog_test.go` (create)

Apply the table from the design doc:

- 5s: `create_tasks`, `update_task`, `get_tasks`
- 15s: all `list_*`, `read_cell`, `get_dashboard`, `read_permissions`, create/update/delete notebook/cell/dashboard/widget/schedule/chart/skill/agent, `share_dashboard`, `get_subagent_results`, `create_notebook`, `update_notebook`, `delete_notebook`, `move_cell`, `swap_cells`
- 30s: `update_permissions`, `get_folder_tree`, `list_agents`, `get_notebook_context`, `update_cell`
- 60s: `explore_schema`, `create_snapshot`, `restore_snapshot`, `export_notebook`
- 120s: `import_notebook`
- 30s: `execute_sql`, `sql_query` (webhook defs read their own `timeout_ms`, else 30s)
- `run_cell`, `create_cell`: `Timeout: 10 * time.Minute` with `TimeoutFromArgs` parsing `timeout_ms` (clamp 0..600000; return false when absent/0). Keep connector-timeout default inside the handler.
- MCP defs in `engine.go`: `makeMCPToolListHandlerHTTP` defs `30s`, call defs `60s`.
- `NoTimeout`: `ask_question`, `spawn_subagents`.

**Step 1: Write the catalog test**

```go
func TestAllBuiltinToolsHaveTimeout(t *testing.T) {
    e := newTestEngine(t) // reuse existing helper in engine_tools_test.go
    noTimeout := map[string]bool{"ask_question": true, "spawn_subagents": true}
    for _, def := range e.registry.List() {
        if noTimeout[def.Function.Name] {
            require.Equal(t, NoTimeout, def.Timeout, def.Function.Name)
            continue
        }
        require.NotZero(t, def.Timeout, "tool %s must declare a timeout", def.Function.Name)
    }
}
```

**Step 2–4:** run to fail, add fields, run to pass.

**Step 5: Commit**

```bash
git add internal/agent/
git commit -m "feat(agent): explicit default timeouts for all builtin tools"
```

---

### Task 4: Route all dispatch sites through `Execute`

**Files:**
- Modify: `internal/agent/engine.go` (tool dispatch, ~1162)
- Modify: `internal/agent/subagent.go` (~123 legacy, ~407 `runSubagentLoop`)
- Modify: `internal/api/mcp.go` (~167)
- Test: extend `internal/agent/engine_tools_test.go`, `internal/agent/engine_retry_test.go`, `internal/api/mcp_tools_test.go`

**Step 1: Failing test** — register a probe tool whose handler blocks on `ctx.Context.Done()` and returns the ctx error; run it through `ProcessMessage` with a short `Timeout`. Assert the persisted `agent_messages` tool result (and `tool_calls[].error`) contains `timed out after`. Mirror `TestProcessMessage_PersistsToolCallResults` for setup.

**Step 2: Fail, then replace each `def.Handler(args, toolCtx)` with `def.Execute(args, toolCtx)`.** MCP has its own error envelope — keep `isError: true` and let the timeout string flow through as the text content.

**Step 3:** run `go test ./internal/agent/ ./internal/api/ -run 'Timeout|PersistsToolCallResults|MCP' -v`.

**Step 4: Commit**

```bash
git add internal/agent/engine.go internal/agent/subagent.go internal/api/mcp.go internal/agent/*_test.go internal/api/mcp_tools_test.go
git commit -m "feat(agent): enforce tool timeouts at all dispatch sites"
```

---

### Task 5: SQL policy — 30s ad-hoc, connector-aware cells, limit fix

**Files:**
- Modify: `internal/agent/tools_sql.go`
- Modify: `internal/agent/tools_notebook.go` (`executeAgentSQL` usage, `executeCell` deadline mapping, `execute_sql` handler)
- Test: `internal/agent/tools_notebook_test.go` (extend `TestAgentRunCellTimeout`), `internal/agent/tools_sql_test.go` (create)

**Step 1: Failing tests**

- `execute_sql` with `limit: 7` calls the executor with limit 7 (assert via a fake driver or by checking the SQL executor param — use the existing test driver pattern if present; otherwise assert the handler passes it by extracting a tiny `clampLimit` helper and unit-testing that).
- `run_cell` with no `timeout_ms` against a connector with `timeout_seconds = 1` sets an error output with `timed_out: true` (integration, following `TestAgentRunCellTimeout`).

**Step 2: Implement**

- `executeAgentSQL`: accept a `limit int` parameter (default/clamp 1000) and pass it to `exec.Execute`.
- `makeExecuteSQLHandler`: pass `req.Limit`.
- `run_cell`/`create_cell` handler: when no `timeout_ms`, read the connector's `timeout_seconds` (already loaded for execution) and use it, else 5m; keep the 10m clamp.
- `executeCell`: set the `timed_out` flag whenever the tool context deadline fired, independent of which layer set it:

```go
if errors.Is(c.Context.Err(), context.DeadlineExceeded) {
    // mark structured timeout output
}
```

- `execute_sql` description: change "SHOW, DESCRIBE queries" wording only if touched; no timeout text change needed.

**Step 3: Pass + commit**

```bash
git add internal/agent/tools_sql.go internal/agent/tools_notebook.go internal/agent/tools_notebook_test.go internal/agent/tools_sql_test.go
git commit -m "fix(agent): honor connector timeout, thread limit, mark wrapper deadlines"
```

---

### Task 6: Seal context escapes

**Files:**
- Modify: `internal/executor/postgres.go` (~36-43)
- Modify: `internal/agent/tools_notebook.go` (auto-snapshots in `delete_cell`, `delete_notebook`)
- Test: `internal/executor/postgres_test.go` (connect context), `internal/agent/tools_notebook_test.go`

**Steps:** `NewPostgresExecutor` should accept/use the context passed to `Execute`/`Schema`/ping (preferred: store DSN and use `pgxpool.New` with a 10s timeout context derived from the caller where available; minimal fix: `context.WithTimeout(context.Background(), 10*time.Second)` for `Ping` and set `connect_timeout=10` in the pool config). Auto-snapshot goroutines: `ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)`.

Test: assert `NewPostgresExecutor` against a blackholed address returns within ~11s (guarded by `testing.Short()`).

**Commit:** `fix(executor): bound Postgres connect and auto-snapshot background work`

---

### Task 7: Docs

**Files:**
- Modify: `AGENTS.md` (env table: `AETHER_AGENT_TOOL_TIMEOUT_DEFAULT`)

**Step:** add the row + a Key Patterns note: "Agent tools declare explicit `ToolDef.Timeout`; per-tool override via `tools.config.timeout_ms`; interactive tools use `NoTimeout`."

**Commit:** `docs: document agent tool timeout controls`

---

## Verification

- `task check` (fmt, vet, tidy, all Go tests with real Postgres/ClickHouse)
- `cd web && npx tsc --noEmit`
- No frontend changes required; no relay changes.
- Manual: add `timeout_ms` to a builtin tool row in the DB, start a session, confirm the resolved budget (log line or test).
