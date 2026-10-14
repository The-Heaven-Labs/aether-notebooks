# Custom Headers for Agents (LLM + MCP) & Encrypted Header Secrets — Design

**Date:** 2026-10-14
**Status:** ✅ Implemented
**Branch:** `feat/agent-custom-headers`

## Problem

Aether's agent stack talks to two kinds of third-party HTTP endpoints, and neither can be
configured with custom HTTP headers:

1. **LLM provider calls** (`internal/agent/llm.go`). Every request to a model endpoint sends
   exactly `Authorization: Bearer <key>` and `Content-Type: application/json`. Providers and
   gateways routinely need more: `X-Api-Key`-style auth (Azure OpenAI), `HTTP-Referer` /
   `X-Title` (OpenRouter attribution), tenant/routing headers on corporate gateways, tracing
   headers, etc. Today there is no way to add them.
2. **MCP server HTTP calls** (`internal/agent/tools_mcp.go`). `GET /tools/list` and
   `POST /tools/call` send no auth or custom headers at all, so any MCP server that requires a
   token is unusable over HTTP.

There is also a **security flaw in the existing webhook-tool header support**. Webhook tools
(`tools.config.headers` + `tools.config.secrets`) let admins mark header values `sensitive`,
and those values are redacted (`__AETHER_REDACTED__`) in API responses — but they are stored
**in plaintext** in the `tools.config` JSONB column. Sensitive header values are typically API
tokens or passwords; they must be encrypted at rest like every other credential in the system
(connector configs, model API keys, MCP env vars).

## Goals

- Per **model config**, admins can define custom headers applied to every LLM provider call
  (chat, streaming, summarization, title generation, subagent clients, and the Test button),
  on top of the existing `Authorization`/`Content-Type` headers.
- Per **MCP server** (HTTP type), admins can define custom headers applied to both
  `GET /tools/list` and `POST /tools/call` (and the Test action).
- Header values can be marked **sensitive**: encrypted at rest with the master key, masked in
  API responses, decrypted only where the request is actually made.
- Retrofit the existing webhook-tool sensitive headers to the same encryption at rest —
  **plaintext storage is the bug**, redaction alone is not enough.
- Same UX shape everywhere: key/value rows with a per-row `sensitive` toggle, consistent with
  the existing Tools page editor.

## Non-goals

- CLI flags for headers (`aether model-configs` stays minimal; follow-up).
- Per-agent header overrides — headers belong to the endpoint (model config / MCP server).
- Headers on stdio MCP servers (no HTTP involved).
- Storing non-sensitive header values encrypted — only marked-sensitive values are encrypted,
  so plain configuration remains readable in the DB and API.

## Existing patterns this design follows

- **Tool config secrets**: `config.headers` (map) + `config.secrets` (names); API redacts
  sensitive values to `__AETHER_REDACTED__`; update restores sentinel values from the old row.
  This design keeps that shape and adds encryption at rest.
- **Connector credentials**: AES-GCM via `crypto.Encrypt`/`Decrypt` with the key derived from
  `AETHER_MASTER_KEY`; masked in API responses. Header secrets use the same cipher and key.
- **Model config API key**: stored encrypted, decrypted per call inside `Chat`/`ChatStream`
  with the master key that is already threaded through every execution path.

## Design

### 1. Storage

Migration `V134__custom_headers.sql`:

```sql
ALTER TABLE model_configs
    ADD COLUMN custom_headers JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN custom_header_secrets TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE mcp_servers
    ADD COLUMN headers JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN header_secrets TEXT[] NOT NULL DEFAULT '{}';
```

- `model_configs.custom_headers` — map of header name → value. Sensitive values are stored as
  tagged ciphertext; everything else stays plaintext.
- `model_configs.custom_header_secrets` — names (as supplied) of sensitive headers.
- `mcp_servers.headers` / `mcp_servers.header_secrets` — same shape for MCP servers.
- Webhook tools keep `tools.config.headers` + `tools.config.secrets`; no backfill, values
  upgrade lazily on the next save (see crypto below).

At-rest format for a sensitive value: `enc:v1:<base64(nonce‖ciphertext)>` — the tag lets the
decrypt path distinguish ciphertext from legacy plaintext, which is what makes the lazy
migration possible.

### 2. Crypto & shared helpers

**`internal/crypto`** additions (thin wrappers over the existing AES-GCM primitives):

```go
func EncryptString(plaintext string, key []byte) (string, error) // "enc:v1:" + base64
func DecryptString(value string, key []byte) (string, error)     // untagged → returned as-is
func IsEncryptedString(value string) bool
```

**New package `internal/headers`** — shared by `internal/api` and `internal/agent`
(agent cannot import api; both can import this):

```go
const RedactedSentinel = "__AETHER_REDACTED__" // exact literal the frontend already checks

// EncryptSecrets returns a copy where every value named in secrets is stored as
// tagged ciphertext. Values already tagged are left as-is. Untagged values are
// encrypted (this is the lazy migration for legacy webhook rows).
func EncryptSecrets(headers map[string]any, secrets []string, key []byte) (map[string]any, error)

// DecryptSecrets resolves a runtime header table: tagged ciphertext is decrypted,
// untagged values pass through as legacy plaintext.
func DecryptSecrets(headers map[string]any, secrets []string, key []byte) (map[string]string, error)

// RedactSecrets mutates a map for API responses: sensitive values become RedactedSentinel.
func RedactSecrets(headers map[string]any, secrets []string)

// RestoreSecrets applies update semantics: sentinel values are restored from old
// when the name is still sensitive; deleted when it no longer is (existing tool
// behavior, preserved exactly).
func RestoreSecrets(oldHeaders, newHeaders map[string]any, secrets []string)
```

Matching of secret names is case-insensitive (HTTP header names are case-insensitive; the UI
preserves the admin's spelling, but `X-API-Key` and `x-api-key` must never diverge).

### 3. Validation (shared)

Applied on every write path (model configs, MCP servers, webhook tools):

- Header names must be RFC 7230 tokens (`^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$`), case-insensitively
  unique.
- Reserved names are rejected with `400`: `Host`, `Content-Length`, `Content-Type`.
  The transport manages the first two, and the request body is always JSON — overriding
  `Content-Type` can only break the call. `Authorization` may be overridden intentionally
  (gateways with custom auth schemes); custom headers are applied *after* the built-ins.
- Limits: ≤50 headers, name ≤128 chars, value ≤8192 chars.
- `secrets` must be a subset of the header names.
- Validation lives in `internal/headers` so REST and agent paths cannot drift.

### 4. Model configs (LLM calls)

- `models.ModelConfig` gains `CustomHeaders map[string]any` (`custom_headers`) and
  `CustomHeaderSecrets []string` (`custom_header_secrets`).
- **Create/update** (`internal/api/model_config_handlers.go`): validate → on update, restore
  sentinel values from the old row → `EncryptSecrets` → store. List/get responses run
  `RedactSecrets`; ciphertext is never returned to clients.
- **Test endpoint**: passes the raw stored map + secrets into `NewLLMClient`; decryption
  happens inside `Chat`, so "Test" verifies exactly what a real turn will send.
- **Engine SELECTs**: all four model-config reads (`ProcessMessage`, `defaultSubagentLLM`,
  `summarizeSession`, `generateSessionTitle`) add the two columns and hand them to the client.
- **`NewLLMClient`** gains `customHeaders map[string]any, headerSecrets []string`; `Chat` and
  `ChatStream` decrypt and apply after `Authorization`/`Content-Type`. Decryption uses the
  `masterKey` already passed to both methods — same lifecycle as the API key, no plaintext
  secrets retained between calls.

### 5. MCP servers (HTTP calls)

- `models.MCPServerOrg` gains `Headers map[string]any` (`headers`) and
  `HeaderSecrets []string` (`header_secrets`).
- CRUD + redact/restore/encrypt identical to model configs.
- Test endpoint (`POST /mcp-servers/{id}/test`) sends decrypted headers on its probe request.
- Runtime (`engine.go`): HTTP tool handlers are built per turn in `ProcessMessage` (master key
  in scope), which decrypts first:
  `makeMCPToolListHandlerHTTP(url, headers)` / `makeMCPToolCallHandlerHTTP(url, headers)`.
  `MCPClient` gains `Headers map[string]string` for the struct-based variants.

### 6. Webhook tools (security fix)

- **Write path** (`internal/api/tool_handlers.go`): after the existing sentinel restore,
  `EncryptSecrets(config.headers, config.secrets, masterKey)` before INSERT/UPDATE. New rows
  store ciphertext; existing plaintext rows upgrade on their next save.
- **Runtime** (`internal/agent`): `makeWebhookToolDef(t, allowedDomains, masterKey)` decrypts
  sensitive values; `loadAgentToolDefs(ctx, agent, masterKey)` and
  `resolveToolDef(t, masterKey)` thread the key through (both are called from
  `ProcessMessage`, where the master key is already a parameter).
- **Tool test endpoint**: decrypts sensitive values before the probe request.
- Redaction/restore logic moves to `internal/headers` (`RedactSecrets`/`RestoreSecrets`) so
  tools, model configs, and MCP servers share one implementation. Sentinel string and
  user-visible behavior are unchanged.

### 7. Frontend

- New shared component `HeadersEditor` (key / value / sensitive rows with add/remove), lifted
  from the ToolsPage pattern:
  - sensitive values render as password inputs; the sentinel is displayed as an empty field
    but round-trips in form state so an unchanged secret is preserved on save;
  - unchecking `sensitive` clears the value, matching today's Tools page behavior.
- `ModelsPage` gains a "Custom Headers" section in the create/edit modal; `MCPPage` gains the
  same section (shown only for `type: http`); `ToolsPage` adopts the shared component.
- `types/agent.ts` gains the new fields on `ModelConfig` and `MCPServerOrg`.

### 8. Error handling & operational notes

- Invalid or reserved header names → `400` with a message naming the offending header.
- Masked round-trip is server-authoritative: `__AETHER_REDACTED__` in a write means
  "keep the stored value"; removing the row removes the header.
- **Master-key rotation**: encrypted header values become undecryptable, exactly like
  connector credentials and model API keys. Existing runbook applies; no new failure mode.
- Audit events: none added — `model_config.update`, `mcp_server.update`, `tool.update` already
  capture these changes at resource level.
- Swagger: request/response shapes are annotated as `object`, so no `swag init` regeneration
  is required, but it will be run if handler annotations change.

## Testing

Go tests (real Postgres, per repo convention):

- `internal/crypto`: tagged string round-trip, legacy plaintext passthrough, wrong-key failure.
- `internal/headers`: encrypt/decrypt/redact/restore incl. case-insensitive names, sentinel
  semantics (keep vs delete), validation (reserved names, bad tokens, duplicates, limits).
- Model configs API: create stores ciphertext (`enc:v1:` prefix asserted in DB), list/get
  return sentinel, update with sentinel preserves the stored secret, update with a new value
  re-encrypts, reserved name → 400.
- MCP servers API: same matrix; test endpoint.
- `internal/agent`: `httptest` capture proving `Chat`, `ChatStream`, MCP list/call, and
  webhook calls send the configured headers (plain and decrypted secret).
- Webhook tools: create/update encryption; runtime decryption of legacy plaintext rows.

Frontend:

- ModelsPage / MCPPage form tests: masked render, payload sentinel round-trip.
- `task check`, `tsc --noEmit`, web/relay builds.
- Real-browser validation with `agent-browser` on the dev stack (mandatory for UI changes):
  create a model config with a plain + a sensitive header, verify masking on edit, verify
  persistence via Test; same for an HTTP MCP server.

## Rollout

- Migration is additive with defaults; old code reading new columns is unaffected, new code
  reading old rows sees empty maps.
- Webhook secret encryption is lazy: no backfill, no downtime, values become ciphertext on
  next edit. Rows never edited remain plaintext — acceptable because redaction still protects
  API responses, and flagging this as a known limitation rather than scripting a backfill.
