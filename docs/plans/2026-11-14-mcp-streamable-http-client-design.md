# Design: Replace the MCP outbound client with a Streamable HTTP implementation

**Date:** 2026-11-14
**Status:** Proposed
**Scope:** `internal/agent/tools_mcp.go`, `internal/agent/engine.go`, `internal/api/mcp_server_handlers.go`, `internal/agent/tools_mcp_headers_test.go`

## Problem

The outbound MCP client (`internal/agent/tools_mcp.go`) cannot talk to any
standards-compliant MCP server. It does not implement the Model Context
Protocol at all. The client issues a custom REST dialect:

```
GET  {command}/tools/list
POST {command}/tools/call     body: {"tool":..., "arguments":...}
```

No `jsonrpc` envelope, no `initialize` handshake, no `Mcp-Session-Id`, no
`Accept: text/event-stream`. No standard MCP server implements those paths, so
they return 4xx as HTML. The client decodes the body without checking
`Status` or `Content-Type` (`tools_mcp.go:85`, `:126`), producing the cryptic:

```
decode mcp response: invalid character '<' looking for beginning of value
```

Reproduced against the official AbuseIPDB MCP server (`https://mcp.abuseipdb.com/`):
`GET /tools/list` → `404 text/html` (`<!DOCTYPE html>`), identical with and
without auth. The reported "MCP server is down" diagnosis was wrong; the client
never spoke MCP.

Three compounding defects:
1. **Wrong protocol** — REST `/tools/list` + `/tools/call`, no JSON-RPC, no handshake.
2. **Blind decode** — no status/Content-Type gate, so a 4xx HTML page surfaces as
   a raw `encoding/json` error and the real cause is hidden.
3. **Test/runtime divergence** — `handleTestMCPServer` does a bare `GET {command}`
   and accepts any 2xx, so the admin "Test connectivity" button reports success
   against an HTML landing page while the agent call fails.

## Non-goals

- **stdio transport.** Current code skips stdio servers at runtime with a warning
  (`engine.go:856`). Implementing a stdio MCP client (process spawn, newline-delimited
  JSON-RPC over stdin/stdout, env injection) is a larger change and out of scope.
  This PR keeps the skip-with-warning and documents it as a follow-up. The reported
  bug is exclusively HTTP.
- **The latent `header_secrets` passthrough** in `headers.go:DecryptSecrets`
  (ciphertext sent verbatim when a name is not listed in `header_secrets`). Real
  but not the cause of this failure; noted as a separate follow-up to keep the
  diff focused and reviewable.
- No DB migration, no frontend change, no agent-config change. The LLM-facing tool
  names (`<server>_list_tools`, `<server>_call_tool`) and the `call_tool` argument
  shape (`{"tool":..., "arguments":...}`) are unchanged — only the wire transport.

## Approach considered

1. **Adopt the official MCP Go SDK** (`github.com/modelcontextprotocol/go-sdk`).
   Pro: spec-correct, maintained. Con: pulls many transitive deps into a repo that
  is deliberately conservative about dependencies (~25 direct, no MCP client SDK
   today); larger surface for maintainers to vet. **Rejected** for this PR.
2. **Hand-roll a minimal Streamable HTTP client.** Pro: ~250 lines, matches the
   existing style (the file is already a hand-written client), zero new deps,
   keeps the diff reviewable, gives full control over error messages (part of the
   fix). **Chosen.** A future PR can swap to the SDK once the protocol is correct.

## Architecture

### `mcpHTTPClient`

A new struct in `internal/agent/tools_mcp.go` that owns the per-server transport:

```go
type mcpHTTPClient struct {
    baseURL          string
    headers          map[string]string          // decrypted plaintext
    httpClient       *http.Client                // custom CheckRedirect to re-apply headers
    sessionID        string                      // from Mcp-Session-Id response header
    protocolVersion  string                      // negotiated in initialize
    nextID           atomic.Int64
    initMu           sync.Mutex
    initialized      bool
}
```

Methods:

- `Initialize(ctx) error` — POST `initialize` JSON-RPC; store `Mcp-Session-Id`
  and the negotiated `protocolVersion`.
- `ListTools(ctx) (any, error)` — ensure initialized; POST `tools/list`; return result.
- `CallTool(ctx, name, args) (any, error)` — ensure initialized; POST `tools/call`.
- `listToolsHandler() ToolHandler` / `callToolHandler() ToolHandler` — adapt to the
  engine's `ToolHandler` contract; shared client instance means `initialize` runs
  once per server per turn.

`newMCPHTTPClient(baseURL, headers)` constructs with a 60s-timeout `http.Client`
and a `CheckRedirect` that re-applies `headers` (and the session/auth) to each
redirected request, so cross-host redirects (vanity URL → real endpoint) cannot
strip `Authorization` — a real failure mode for API-gateway-fronted servers.

### Wire format (MCP Streamable HTTP)

All requests are JSON-RPC 2.0 POSTed to the **single** base URL (the `command`
column):

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"aether-notebooks","version":"0.81.0"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"check","arguments":{"ipAddress":"1.2.3.4"}}}
```

Every request sets:

- `Content-Type: application/json`
- `Accept: application/json, text/event-stream`
- `Mcp-Protocol-Version: <negotiated>` (after initialize)
- `Mcp-Session-Id: <id>` (after initialize)
- all configured custom headers via `headers.Apply` (including `initialize`, so
  auth-bearing servers can be reached on the first call)

### Response handling

After the POST, branch on response `Content-Type` **before** decoding:

- `application/json`: decode one `jsonrpcResponse{result,error}`. If `error` is set,
  surface it (code + message). Else return `result`.
- `text/event-stream` (SSE): scan line-by-line; parse each `data:` payload as JSON-RPC.
  Collect the message whose `id` matches the request id (notifications have no `id`
  and are ignored). Return its `result`. Close the body once the match is found.
- anything else, or HTTP status >= 400: read up to 512 bytes of the body and return
  `fmt.Errorf("mcp server %s returned HTTP %d (%s): %q", method, status, contentType, snippet)`.
  This is the fix for the cryptic `<` error.

### Initialization lifecycle

The client lazy-initializes on the first `ListTools`/`CallTool`. Because engine
builds handlers fresh per `ProcessMessage` turn (`engine.go:854`), `initialize`
runs once per turn per server — one extra round-trip, acceptable for a first fix.
If a server returns `400` mid-turn (session expired), the client clears
`sessionID`/`initialized` and retries initialize-once.

### Engine wiring

`engine.go:854-872` changes from building two independent handler closures to
constructing one shared client per server:

```go
client := newMCPHTTPClient(ms.Command, plain)
allTools = append(allTools,
    mcpListToolDef(ms.Name, client.listToolsHandler()),
    mcpCallToolDef(ms.Name, client.callToolHandler()),
)
```

### Admin "Test connectivity" fix

`handleTestMCPServer` (`mcp_server_handlers.go:371-433`) currently GETs the bare
URL and accepts `< 400`. Change it to POST a JSON-RPC `initialize` and accept
success only on a JSON-RPC response with no error. This makes the test reflect
what the runtime actually does, closing the trap where Test=green and runtime=red.

### Dead-code removal

Remove `MCPClient` struct, `RegisterMCPTools`, `makeMCPToolListHandler`,
`makeMCPToolCallHandler` (tools_mcp.go:15-27, :29-60, :62-64, :133-135) — the
subagent grep confirmed zero callers. Keeps the file honest.

### Stdio behavior

Keep the skip-with-warning; improve the message to state stdio is not yet
supported and link the follow-up.

## Error handling contract

- HTTP ≥ 400 or non-JSON/SSE content type → clear error with status, content type,
  and a body snippet. Never attempt `json.Unmarshal` on an HTML body.
- JSON-RPC `error` object → surface `code` + `message`.
- `initialize` failure → propagate (the server is unreachable or non-MCP).
- Network/timeout → existing `mcp ... : %w` wrapping.

## Test plan

Rewrite `tools_mcp_headers_test.go` (current tests encode the broken REST shape)
to assert the real protocol:

1. `TestMCPClientInitializeAndListTools` — mock server requires `initialize` then
   `tools/list`; assert JSON-RPC posted to base URL, session id round-tripped,
   headers on both initialize and list, result returned.
2. `TestMCPClientCallTool` — assert `tools/call` JSON-RPC with `{name, arguments}`
   (note: the LLM-facing wrapper still passes `{tool, arguments}`; the client maps
   `tool`→`name` for the MCP params).
3. `TestMCPClientSSEResponse` — server responds `text/event-stream`; client extracts
   the matching-id `data` frame, ignores notifications.
4. `TestMCPClientHTMLError` — server returns `404 text/html`; assert a clear error
   mentioning HTTP 404 and a snippet — NOT `invalid character '<'`.
5. `TestProcessMessageSendsMCPCustomHeaders` — adapt end-to-end test to JSON-RPC,
   assert decrypted `Authorization` on the `initialize` + `tools/call` requests.
6. `TestProcessMessageSkipsMCPOnDecryptFailure` — unchanged behavior (skip path
   untouched); adapt server expectations.
7. Admin `TestHandleTestMCPServer` — assert it POSTs initialize and rejects an
   HTML-landing-page server.

## Risks

- **Per-turn `initialize` overhead**: one extra round-trip per server per turn.
  Mitigated by being a single JSON POST; acceptable until a session cache is added.
- **SSE scan complexity**: a minimal scanner that reads lines and parses `data:`
  payloads; bounded by closing the body on match. No streaming backpressure concerns
  for request/response calls.
- **Protocol version negotiation**: propose `2025-06-18` (widely supported); accept
  the server's negotiated version from the initialize result. Mirrors the inbound
  server's supported-version list (`internal/api/mcp.go`).

## Follow-ups (not in this PR)

- stdio MCP transport (process + JSON-RPC over stdio + env).
- `headers.DecryptSecrets` ciphertext-passthrough latent bug.
- Optional: cache `mcpHTTPClient` + session across turns with TTL.