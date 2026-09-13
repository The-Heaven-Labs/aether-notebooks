# PAT Auth Lookup Hash — Design

**Date:** 2026-09-12
**Status:** Approved

## Problem

`validateAPIToken` (`internal/api/middleware.go`) loads **every** row in `api_tokens`
(optional org filter) and bcrypt-compares each candidate until one matches. Cost is
O(total tokens) × ~60ms. The dev database had 117 tokens, so each authenticated request
took ~7 seconds — surfaced during the MCP browser sweep, but it affects every API call,
the CLI, and the frontend.

bcrypt is the right tool for user passwords (low entropy; slow hashing is the defense).
API tokens are different: `createToken` generates `aether_tok_` + 28 `crypto/rand` bytes
(224 bits of entropy), so brute force is infeasible regardless of hash speed. Paying
bcrypt per candidate row buys no meaningful security and is the entire latency problem.

## Decision

Use a keyed lookup hash and an indexed column:

- `token_lookup_hash = HMAC-SHA-256(key, "aether-pat-lookup:v1:"+token)` where `key` is
  the existing derived `AETHER_MASTER_KEY` (`crypto.DeriveKey`). The label provides
  domain separation from connector-credential encryption.
- Fast path: index lookup by `token_lookup_hash` (+ `org_id` when on a subdomain).
  A hit authenticates without bcrypt (~1ms): matching a 224-bit secret's HMAC is
  sufficient proof of possession. This is the GitHub/Stripe model for API tokens.
- `bcrypt(token)` remains the at-rest/legacy hash and is still written on create.
- Legacy rows (created before this migration) have `token_lookup_hash IS NULL`. They take
  the existing bcrypt scan on first use, and the matched row is backfilled so subsequent
  requests are O(1). No raw tokens are needed for migration.
- HMAC (rather than plain SHA-256) means a leaked database alone cannot be used to test
  token guesses without the master key.

**Ops caveat:** `AETHER_MASTER_KEY` now also keys PAT lookup. Changing it invalidates
existing personal access tokens (legacy bcrypt rows survive until backfilled). Documented
in `AGENTS.md` and `docs/mcp.md`.

## Changes

| Area | Change |
|---|---|
| `internal/crypto/crypto.go` | `TokenLookupHash(key []byte, token string) string` |
| `internal/database/migrations/V097__api_tokens_lookup_hash.sql` | nullable `token_lookup_hash TEXT` + partial unique index |
| `internal/api/token_handlers.go` | store the lookup hash on create |
| `internal/api/middleware.go` | `AuthMiddleware` takes the master key; `validateAPIToken` gets a fast path plus legacy scan/backfill; shared `completeAPITokenAuth` |
| `internal/api/router.go`, `middleware_test.go` | signature updates |
| `docs/mcp.md`, `AGENTS.md` | master-key dependency note |

No token format change, no client change, no Redis cache.

## Testing

- Unit: `TokenLookupHash` deterministic, 64 hex chars, key- and token-sensitive.
- Create stores the expected HMAC for the submitted raw token.
- Legacy row (bcrypt only, NULL lookup) authenticates and is backfilled to the expected HMAC.
- Fast path: all existing PAT auth tests now exercise it.
- Expired, revoked, cross-org-subdomain behavior unchanged.
- Perf check: authenticated request latency before/after, plus a browser re-check of the
  MCP endpoint with a freshly created PAT.

## Out of scope

Dropping the bcrypt column, token prefix/ID format changes, Redis validation cache,
password hashing changes (bcrypt remains appropriate; Argon2id is a possible future
upgrade with rehash-on-login).
