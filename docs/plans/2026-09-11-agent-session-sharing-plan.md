# Agent Session Sharing Implementation Plan

> **For the executor:** follow tasks in order; each task is TDD (failing test → implement →
> green → commit). Pre-flight: `task infra:up` (Postgres + Redis needed). Migration head is
> **V123**, so the new migration is **V124** — do not reuse the V097 number from earlier
> drafts of this plan (it was consumed by `api_tokens_lookup_hash`).

**Goal:** Link agent sessions to notebooks for discovery and add explicit, read-only,
live-viewable session sharing (including optional live notebook-viewer inheritance) while
sealing the current authorization holes and making cross-replica live viewing work.

**Architecture:** Make `agent_session` a first-class ACL resource with an owner fallback.
`checkSessionPermission` is the single access primitive (owner → ACL with admin-mode bypass →
live notebook `view` union when `share_with_notebook_viewers` is set). Session creation accepts
a share list and the inherit flag in one transaction. WS permissions split into `view`
(connect/read/live) and `edit` (send/cancel/confirm/settings). Session streams move to Redis
pub/sub with a shared seq counter and replay buffer, falling back to the in-memory manager when
Redis is absent. Add notebook-scoped and shared-with-me listing, a read-only live viewer, a
notebook Chats drawer, and CLI create flags.

**Tech Stack:** Go `net/http` ServeMux + pgx + go-redis, Postgres migration V124, React +
TypeScript, existing WS stream protocol.

**Design doc:** `docs/plans/2026-09-11-agent-session-sharing-design.md`

---

### Task 1: Redis-backed session stream fan-out

**Files:**
- Modify: `internal/agent/stream.go`
- Modify: `internal/agent/engine.go` (`NewEngine`, the `streams: NewStreamManager()` call)
- Modify: `internal/agent/stream_test.go` (constructor now takes a client)
- Create: `internal/agent/stream_redis_test.go`

**Step 1: Failing tests**

Existing tests keep `NewStreamManager(nil)` and must pass unchanged (nil = in-memory path).
New Redis tests (skip with `t.Skip` if `AETHER_REDIS_URL` is unset):

- Two managers sharing one Redis: publish 3 events on manager A, a subscriber on manager B
  receives all 3 with monotonically increasing `seq`.
- Seq is monotonic across managers (publish on A then B, seqs differ by 1).
- A late subscriber with `skipBuffer=false` replays the Redis buffer with original seqs.
- `LastSeq` on manager B matches the last publish on A.
- Publishing with no local subscriber still increments seq and stores the buffer.

Run `go test ./internal/agent/ -run 'Stream' -timeout 3m`; expected FAIL.

**Step 2: Implement**

- `StreamManager` gains `rdb *redis.Client`; keep the in-memory fields for the fallback.
- Per-session Redis keys (TTL 2h, refreshed on publish): `aether:agent:sess:{id}:seq` (INCR),
  `aether:agent:sess:{id}:buf` (LIST, `LTRIM -500 -1`, entries `{"seq":N,"msg":{...}}`), and
  channel `aether:agent:sess:{id}`.
- `Publish` with Redis: one Lua script — `INCR` seq, `RPUSH` entry, `LTRIM`, `EXPIRE` both
  keys, `PUBLISH` entry. Local delivery happens only via the pod’s subscription (including the
  publishing pod), so events are never double-delivered. On Redis error, fall back to the
  in-memory fan-out and log.
- `Subscribe`: first local subscriber starts a per-session pump that SUBSCRIBEs first, reads
  the buffer (`LRANGE`), delivers buffered events (unless `skipBuffer`), then live events with
  seq dedup; keep a local cache of received seqs for dedup and detect gaps (emit the existing
  `resync` marker). Last unsubscribe keeps the existing 5s grace, then UNSUBSCRIBEs.
- `LastSeq` reads the seq key (`0` when absent); in-memory path unchanged.
- `NewStreamManager(rdb *redis.Client)` and update `NewEngine`.

**Step 3:** pass; commit: `feat(agent): redis-backed session stream fan-out for cross-pod viewers`

---

### Task 2: Migration V124

**Files:**
- Create: `internal/database/migrations/V124__agent_session_acl.sql`
- Test: `internal/database/database_test.go` (add to the existing migration tests)

**Step 1: Failing test** — in a test that applies migrations, create an agent + session row,
then assert an owner `acl_entries` row exists for `resource_type='agent_session'` with actions
`{view,edit,share,delete,admin}`, that `agent_sessions.share_with_notebook_viewers` exists and
defaults to false, and that inserting an `acl_entries` row with `resource_type='agent_session'`
is accepted. Run `go test ./internal/database/ -timeout 3m`; expected FAIL.

**Step 2: Write the migration** (exact SQL in the design doc, “Database changes”):

1. Redefine `acl_entries_resource_type_check` with all current types plus `agent_session`
   (`folder, notebook, connector, dashboard, agent, model_config, skill, mcp_server, tool`).
2. `ALTER TABLE agent_sessions ADD COLUMN IF NOT EXISTS share_with_notebook_viewers BOOLEAN NOT NULL DEFAULT FALSE;`
3. Backfill owner ACL entries from `agent_sessions JOIN agents` with
   `ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING`.
4. `CREATE INDEX IF NOT EXISTS idx_agent_sessions_notebook ON agent_sessions (notebook_id, created_at DESC);`

**Step 3:** pass; commit: `feat(db): agent_session ACL type, inherit flag, and owner backfill`

---

### Task 3: Permission primitive and org resolution

**Files:**
- Modify: `internal/api/permissions.go` (admin bypass ~67–79; new helpers)
- Test: `internal/api/permissions_test.go` (or a new `agent_session_permissions_test.go`)

**Step 1: Failing tests** (real DB via `setupTestServer`):

- Owner passes `checkSessionPermission` for `view`/`edit` even with no ACL row (fallback).
- Org admin in admin mode can view a foreign session; without admin mode, cannot.
- Inherit flag on + notebook view → view passes; after removing the notebook grant it fails
  immediately; the flag never grants `edit`.
- Direct ACL `view` for a user passes; `edit` granted to a non-owner is ignored for the
  permission check unless admin mode.

**Step 2: Implement**

```go
// Owner fallback → agent_session ACL (admin-mode bypass) → live notebook inheritance (view only).
func (s *Server) checkSessionPermission(ctx context.Context, userID, orgID, orgRole, sessionID, action string) (bool, error)

// resourceOrgID resolves admin-bypass org for folder, resourceTable types, and agent_session.
func (s *Server) resourceOrgID(ctx context.Context, resourceType, resourceID string) (string, error)
```

Use `resourceOrgID` in the admin-mode branch of `checkPermission`; for `agent_session` resolve
via `agent_sessions → agents.org_id`. Do **not** add `agent_session` to `resourceTable` (no
`org_id`/`folder_id` columns) — direct ACL matching in `checkPermission` works unchanged, and
the folder walk simply stays nil for this type.

**Step 3:** pass; commit: `feat(api): session-level permission primitive with notebook inheritance`

---

### Task 4: Create session with sharing in one transaction

**Files:**
- Modify: `internal/api/agent_handlers.go` (`handleCreateSession` ~602)
- Test: `internal/api/agent_session_sharing_test.go` (create)

**Step 1: Failing tests**

- Creating with a `notebook_id` the caller cannot view → 403; without notebook → 201.
- Request `shares: [{user Bob, actions:["view"]}]` → Bob can immediately read the session
  (no second request).
- `actions:["edit"]` for a non-owner subject → 400; unknown user/group → 400; `org_role`
  subject other than `everyone` → 400.
- `share_with_notebook_viewers: true` without `notebook_id` → 400; with a notebook → any
  notebook viewer can read immediately.
- Owner ACL entry is inserted in the same transaction.
- Empty-session cleanup deletes only empty sessions for the same user+agent+notebook.

**Step 2: Implement**

- Extend the request struct with `shares []aclEntryInput` and
  `share_with_notebook_viewers bool` (existing fields unchanged: `notebook_id`, `max_turns`,
  `title`, `auto_approve_tools`, `auto_answer_questions`).
- After the agent `view` check: notebook `view` check when `notebook_id != ""`.
- Validate shares as in the design doc; empty actions default to `["view"]`.
- One transaction: scoped empty-session cleanup, `INSERT agent_sessions` (with the flag),
  owner ACL `{view,edit,share,delete,admin}`, share entries, commit; then audit
  (`agent_session.create` plus `acl.granted` per share).
- Response stays `{session_id, context_window, auto_approve_tools, auto_answer_questions}`.
- **ACL cleanup on session deletion (review finding):** sessions are hard-deleted but their
  `acl_entries` rows are not, leaking a row per deleted session (millions already in dev).
  Delete the session's `agent_session` ACL rows wherever sessions are deleted:
  `SessionStore.DeleteSession` (`internal/agent/session.go`), the empty-session sweep here,
  the agent delete path (`internal/api/agent_handlers.go` — grep `DELETE FROM agent_sessions`),
  and any notebook-delete cascade. Same transaction as the session delete; add tests.

**Step 3:** pass; commit: `feat(api): create sessions with shares and notebook inheritance in one request`

---

### Task 5: Tighten read paths

**Files:**
- Modify: `internal/api/agent_handlers.go` (`handleGetSession` ~771, `handleGetSessionMessages` ~813,
  `handleGetSessionUsage` ~911, `handleUpdateSessionTitle` ~950, `handleGetSubagentMessages` ~1182)
- Modify: `internal/api/agent_attachment_handlers.go` (upload ~26, fetch ~108)
- Test: `internal/api/agent_session_sharing_test.go`

**Step 1: Failing tests** — unrelated org member gets 403/404 on get/messages/usage/attachments/
subagent messages; owner and shared users work (write ACL rows in the test DB for now); rename
by a shared user fails, by owner succeeds without agent `edit`; attachment upload requires
session `edit`, fetch requires session `view`.

**Step 2: Implement**

- Replace every agent-level check with `checkSessionPermission(..., sessionID, "view"|"edit")`.
- `handleGetSubagentMessages`: resolve `subagent_tasks.parent_session_id`, load that session,
  require `view`.
- `handleGetAgentAttachment`: load the attachment’s `session_id`, require session `view`.
- `handleUploadAgentAttachment`: resolve session from the path, require `edit`.

**Step 3:** pass; commit: `fix(api): authorize agent session reads at the session level`

---

### Task 6: Listing endpoints and session update

**Files:**
- Modify: `internal/api/router.go` (routes)
- Modify: `internal/api/agent_handlers.go` (`handleListSessions`)
- Create: `internal/api/session_list_handlers.go` (`handleListNotebookSessions`,
  `handleListSharedSessions`, `handleUpdateSession`)
- Test: `internal/api/agent_session_sharing_test.go`

**Step 1: Failing tests** — agent list excludes foreign unshared sessions and includes shared
and inherited ones; notebook list returns visible sessions for that notebook and 403s without
notebook `view`; shared list contains only non-owned visible sessions; responses carry
`owner_email`, `shared`, `can_edit`, `share_with_notebook_viewers`, and empty lists serialize
as `[]`; `PATCH /sessions/{id}` updates title (owner) and the inherit flag (owner, notebook
required).

**Step 2: Implement**

- `handleListSessions`: keep the existing query (has-messages filter, `ORDER BY created_at
  DESC LIMIT 50`), add `JOIN users` for `owner_email`, then filter in Go with
  `checkSessionPermission(..., "view")`.
- `GET /api/v1/notebooks/{id}/sessions` → require notebook `view`; query by `notebook_id`
  (new index) with the same visibility filter.
- `GET /api/v1/sessions/shared` → non-owned viewable sessions: direct ACL matches (user, the
  caller’s groups, `everyone`) plus inherit-enabled sessions in notebooks the caller can view;
  dedupe and bound the scan.
- `PATCH /api/v1/sessions/{session_id}`: `{title?, share_with_notebook_viewers?}`; title needs
  `edit`, the flag needs `share` + a notebook; audit the sharing change. Leave
  `PATCH /sessions/{session_id}/title` in place.

**Step 3:** pass; commit: `feat(api): notebook and shared session listing`

---

### Task 7: WebSocket view/edit split

**Files:**
- Modify: `internal/api/agent_ws.go` (connect ~71–98, reader loop ~214–499)
- Test: `internal/api/agent_ws_test.go`

**Step 1: Failing tests** — shared viewer connects (session `view`) and receives a session
event published by the engine; viewer `message`, `cancel`, `slash_command`, `tool_confirm`,
`question_answer`, `set_reasoning_effort`, `set_model_config`, `set_page_context`, and
`set_admin_mode` frames are rejected with `read-only session`; `reconnect` is allowed; owner
can still send; admin-mode org admin can mutate.

**Step 2: Implement**

- On connect: load the session, `checkSessionPermission(..., "view")` (owner fallback),
  compute `canEdit := checkSessionPermission(..., "edit")` once, keep the transient admin-mode
  flag.
- Reader loop: allow `reconnect` for everyone; for all other frames, reject with a
  `read-only session` error when `!canEdit`. Steering is covered by the `message` branch.

**Step 3:** pass; commit: `feat(api): live read-only session viewing over WS`

---

### Task 8: ACL write rules for sessions

**Files:**
- Modify: `internal/api/acl_handlers.go` (`handleGetACL` ~41, `handlePutACL` ~96)
- Test: `internal/api/agent_session_sharing_test.go`

**Step 1: Failing tests**

- Owner PUT `[{user Bob, ["view"]}]` → 200; Bob can read.
- Non-owner PUT → 403; org admin without admin mode → 403; admin mode → 200.
- `actions:["edit"]` for a non-owner → 400.
- A PUT that omits the owner entry leaves the owner entry intact (no lockout) and the owner
  can still read/rename.
- Unknown user/group subject → 400.

**Step 2: Implement**

- For `resource_type == "agent_session"`, authorize with
  `checkSessionPermission(..., "share")` (the generic `share` check stays for other types; the
  unconditional org-admin bypass must not apply here). `handleGetACL` requires session `view`
  unless admin mode.
- Replace non-owner entries only: validate every entry subject (user/group/everyone, same org)
  and require non-owner actions to be exactly `["view"]`; then upsert the owner entry last, so
  a replace-style PUT cannot drop it. Keep the existing audit diff.

**Step 3:** pass; commit: `feat(api): read-only ACLs for shared agent sessions`

---

### Task 9: Frontend transcript extraction

**Files:**
- Create: `web/src/components/AgentChatTranscript.tsx`
- Modify: `web/src/components/AgentPanel.tsx` (move `MemoizedChatMessage` ~368,
  `chatMarkdownComponents` ~24, transcript list rendering)
- Modify: `web/src/components/SessionHistory.tsx` (update imports)
- Test: existing `AgentPanel`/`SessionHistory` component tests must pass.

**Step 1:** extract the message component, markdown components, and transcript list into the
new module with the same props; `AgentPanel` imports them (no behavior change).
**Step 2:** `npm test -- --run` and `npx tsc --noEmit`; commit:
`refactor(web): extract AgentChatTranscript for reuse`

---

### Task 10: Read-only live session viewer

**Files:**
- Create: `web/src/components/SessionViewer.tsx`
- Modify: `web/src/types/agent.ts` (`AgentSession`: add `owner_email`, `shared`, `can_edit`,
  `share_with_notebook_viewers`)
- Modify: `web/src/pages/NotebookPage.tsx` (render the viewer)
- Test: `web/src/components/SessionViewer.test.tsx`

**Step 1: Failing tests** — viewer fetches session + messages and renders the transcript; a
`token` WS event appends live; `tool_confirm_required`/`question` do not open dialogs; no
composer is rendered; the header shows owner and the read-only banner.

**Step 2: Implement** — standalone WS connect (token + `admin_mode` query params, same as
`AgentPanel`), send only `reconnect` on open, reuse `web/src/utils/agentTranscript.ts`
reducers, ignore confirm/question events, render `AgentChatTranscript`. Share button only when
`can_edit` (owner).

**Step 3:** pass; commit: `feat(web): read-only live session viewer`

---

### Task 11: Sharing UI

**Files:**
- Modify: `web/src/components/PermissionsPanel.tsx` (`ResourceType` union + action matrix +
  save path)
- Modify: `web/src/components/SessionViewer.tsx` and/or `web/src/components/AgentPanel.tsx`
  (Share button, owner only)
- Test: `web/src/components/PermissionsPanel.test.tsx` (if present) or new cases.

**Step 1:** add `agent_session` with only `view` selectable; the panel never offers other
actions for this type. Add an owner-only “Anyone who can view this notebook” checkbox when the
session has a notebook, wired to `PATCH /api/v1/sessions/{id}`.
**Step 2:** tests + `npx tsc --noEmit`; commit: `feat(web): session sharing panel and inherit toggle`

---

### Task 12: Discovery UI

**Files:**
- Create: `web/src/components/NotebookChats.tsx` (drawer; `GET /notebooks/{id}/sessions`)
- Modify: `web/src/components/SessionHistory.tsx` (“My sessions” / “Shared with me” sections,
  owner + shared badges; shared rows open the viewer, own rows resume)
- Modify: `web/src/pages/NotebookPage.tsx` (Chats entry in the View/Share toolbar area)
- Test: `web/src/components/NotebookChats.test.tsx`

**Step 1:** drawer lists owner/title/first-message/message count/date + Shared badge; every
row opens the read-only `SessionViewer` (own rows included — their `can_edit` suppresses the
“Shared · Read-only” banner). This is deliberate: the drawer has no cross-component resume
plumbing at this stage. Resuming one's own session stays in the agent panel’s session
history (“My sessions”), and `NotebookChats.onResumeSession` remains reserved for a future
wiring (the component already prefers it over `onOpenSession` when provided for an owned row).
**Step 2:** history sections + badges; `npx tsc --noEmit`; commit:
`feat(web): notebook chats drawer and shared-with-me discovery`

---

### Task 13: CLI create flags

**Files:**
- Modify: `internal/cli/types.go` (add `CreateSessionResult`)
- Modify: `internal/cli/agents.go` (`CreateSession` ~64, session create command ~215)
- Test: `internal/cli/agents_test.go` (if present) or covered by smoke.

**Step 1:** fix `CreateSession` to post and decode `CreateSessionResult`
(`session_id`, `context_window`, `auto_approve_tools`, `auto_answer_questions`), and add
`--share-user` (repeatable; email or UUID resolved via `ListMembers`), `--share-group`
(repeatable; name or UUID resolved via `ListGroups`), `--share-everyone`,
`--share-notebook-viewers` (requires `--notebook`). Print the session id and share summary.
**Step 2:** `go build ./... && go vet ./internal/cli/...`; commit:
`feat(cli): create sessions with sharing in one request`

---

### Task 14: Swagger, docs, and full verification

**Files:**
- Modify: `internal/api/docs/*` (regenerate)
- Modify: `AGENTS.md` (session sharing model, `agent_session` ACL behavior, Redis stream
  fan-out + pod-local control-frame limitation)

**Step 1:** regenerate Swagger:

```bash
swag init -g cmd/aether-server/main.go -o internal/api/docs
```

Annotate the create-session body, the new list routes, and `PATCH /sessions/{id}`.

**Step 2:** run the full gate:

```bash
task check
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
task test:e2e
```

**Step 3:** manual smoke on the dev stack: A creates a session in a notebook with
`share_with_notebook_viewers` on; B (notebook viewer) opens the Chats drawer, watches A’s turn
live, cannot send; C (no notebook access, no share) sees nothing; owner PUT via
`aether acl set agent_session <id>` works. If two API replicas are available, verify the viewer
still receives live events when connected to the other replica.

**Step 4:** commit: `docs: agent session sharing model`

---

## Notes for the executor

- **Behavior change:** unshared, non-inherited sessions become owner-only (plus admins in admin
  mode). Call this out in the PR description.
- Keep the owner fallback (`session.user_id == caller`) in every helper so pre-backfill
  sessions never lock out their owner.
- Subagent messages and attachments are readable whenever the parent session is readable.
- Sessions are listed only when they have at least one message (existing `first_message`
  filter); do not change that.
- The Redis stream task keeps the `rdb == nil` in-memory path — engine tests construct with
  nil and must stay green.
- Control frames (`cancel`, `tool_confirm`, `question_answer`, steering) remain pod-local by
  design; do not attempt to route them through Redis in this plan.
