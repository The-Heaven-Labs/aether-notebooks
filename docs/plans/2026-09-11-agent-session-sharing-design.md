# Agent Session Sharing & Notebook Linkage — Design

**Date:** 2026-09-11
**Revised:** 2026-09-30 — reviewed against the V123-era codebase. Changes: opt-in notebook
inheritance replaces "never inherit" (D2), create-with-share automation (D7), owner
fallback + ACL-write safety (D8), Redis-backed stream fan-out for cross-pod live viewers
(D9), migration renumbered from V097 to **V124**, and the `AgentChatTranscript`
extraction (previously an external dependency) folded into this plan.
**Status:** Approved (design decisions D1–D9 settled)

## Problem

Agent sessions are already linked to notebooks (`agent_sessions.notebook_id`, nullable since
V061), but there is no sharing model and the current access control is too loose:

- Every session route (`list`, `get`, `messages`, `usage`, WS, subagent messages,
  attachments) authorizes against the **agent** (`view`), never the session or its owner.
  Any org member who can see an agent can read every user's sessions for that agent.
- Resuming someone else's session over WS executes tools, ACL checks, and audit entries as
  `session.UserID` (`engine.go` builds `ToolContext{UserID: session.UserID}`), so a second
  user acts as the owner.
- Attachment **fetch** has no ACL check at all beyond an org-scope join
  (`agent_attachment_handlers.go`), and the WS reader loop accepts `cancel`, `set_*` (including
  the client-supplied `set_admin_mode`), `tool_confirm`, `question_answer`, and `message`
  frames from any connection (`agent_ws.go`).
- There is no way to see which sessions belong to a notebook, no read-only access, and no way
  to create a session with its sharing in one request for API/CLI automation.
- Live session events (`internal/agent/stream.go`) are in-process only: the seq counter,
  500-event replay buffer, and subscriber list are per-pod, so a viewer routed to a different
  replica than the running turn receives `reconnect_sync` but no live tokens/events.

## Goals

- Sessions are linked to notebooks for discovery: a notebook shows the chats that happened in it.
- A session owner can explicitly share a session read-only with users/groups/Everyone.
- A session owner can opt a session into **notebook-viewer inheritance**, so anyone who can
  view the attached notebook can read it live (view-only; revocation is immediate).
- A shared user can read the transcript (including subagent detail and attachments) and watch
  it live, but can never send, cancel, confirm tools, answer questions, or change settings.
- **Automation:** `POST /agents/{id}/session` accepts the share list and inherit flag, applied
  in the same transaction as session creation — one request, no follow-up calls.
- **Multi-viewer live chat works across replicas** via a Redis-backed session stream.
- Fix the authorization holes above as part of the same change.

## Non-goals

- Public/unauthenticated session links (`public_tokens` stays notebook/dashboard only).
- Cross-org sharing (all ACLs are org-scoped).
- Co-authoring: non-owner subjects can only ever hold `view`; `edit` grants are rejected.
- Materialized inheritance ACLs (no background sync of notebook grants to session rows).
- Moving cancel/confirm/question/steering resolution to a cross-pod control plane (see Known
  Limitations); only event fan-out becomes cross-pod.
- Scheduler-created sessions (the scheduler still installs a no-op callback).

## Decisions

| # | Decision |
|---|---|
| D1 | Sessions become an ACL resource type: `agent_session`. |
| D2 | Explicit per-session sharing by default; an owner-controlled `share_with_notebook_viewers` flag grants live read-only access to notebook viewers. Sharing a notebook alone never exposes chats unless the flag is set. |
| D3 | Discovery ships in the UI (notebook Chats drawer + “Shared with me” + history badges). |
| D4 | Live read-only from day one: viewers connect over WS and receive stream events, cross-pod. |
| D5 | Access tightening approved: sessions are owner-only unless shared (directly or via the inherit flag). This is a behavior change. |
| D6 | Same-org only; no public links for sessions. |
| D7 | Create-with-share: `POST /agents/{id}/session` takes `shares` + `share_with_notebook_viewers`, written in one transaction. |
| D8 | The session owner always passes via `session.user_id == caller` (owner fallback), and `PUT /acl` for a session preserves the owner entry, so neither a missed backfill nor a replace-style ACL write can lock the owner out. |
| D9 | Session streams move to Redis pub/sub + shared seq + Redis replay buffer, with the existing in-memory path retained when Redis is not configured (tests, single-node). |

## Access model

`agent_session` is added to the `acl_entries.resource_type` CHECK constraint and to the
permission system. Owner ACL entries are created at session creation and backfilled by
migration, so `checkPermission` is the single access primitive — with an explicit owner
fallback for safety.

**Owner entry (create + backfill):**

```
subject_type='user', subject_id=<owner user id>, actions={view,edit,share,delete,admin}
```

**Owner fallback:** `checkSessionPermission` returns true for every action when
`session.user_id == caller`. This protects pre-backfill rows and makes the owner immune to an
ACL replace that omits their entry.

**Read-only sharing enforcement:** for `resource_type='agent_session'`, the ACL PUT handler
accepts only entries whose subject is the owner or whose actions are exactly `{view}`. We never
grant `edit` to another user through the share UI or API; the only `edit` paths are the owner
fallback, the owner entry, and platform/org admins in admin mode.

**Opt-in notebook inheritance (D2):** `agent_sessions.share_with_notebook_viewers` is a
boolean, settable at creation or later via `PATCH /sessions/{id}` (owner only). When it is
true and the session has a `notebook_id`, `checkSessionPermission(..., "view")` also passes if
the caller has notebook `view`. Evaluation is **live** (no materialized rows): unsharing the
notebook, removing a group, or flipping the flag off revokes access immediately. The flag can
only grant `view`.

**Org resolution / admin bypass:** `checkPermission` resolves the resource org for
`agent_session` through `agent_sessions → agents.org_id`. Sessions have no `folder_id`, so
they are not in the folder-inheritance walk (which still applies to notebook access checked
for the inherit flag). Org admins bypass only with admin mode enabled, as elsewhere; unlike
the existing `handleGetACL`/`handlePutACL` special-cases, the session paths require admin mode
for org admins.

### Access matrix

| Caller | List in notebook | Read transcript / subagents / attachments | Watch live | Rename | Send / cancel / confirm / settings |
|---|---|---|---|---|---|
| Owner | yes | yes | yes | yes | yes |
| Direct share (user/group/Everyone) | if they can view the notebook | yes | yes | no | no |
| Notebook viewer, inherit flag on | yes | yes | yes | no | no |
| Notebook viewer, inherit flag off | no | no | no | no | no |
| Org member without share | no | no | no | no | no |
| Org admin, no admin mode | per ACL / inherit only | per ACL / inherit only | per ACL / inherit only | no | no |
| Org admin + admin mode | yes | yes | yes | yes | yes |

Notes:

- A directly shared user does not need access to the agent, only the session. If they lack
  notebook view, the session is still readable but the notebook is marked unavailable in the UI.
- Tool call results, SQL, and reasoning are part of the transcript and are therefore visible
  to shared users. Sharing is explicit (or an owner-enabled notebook inheritance), so this is
  intended.
- Viewers receive every published stream event; a viewer’s mutating frames are rejected with a
  `read-only session` error and never touch the engine.

## Database changes (V124)

Next free migration number after V123 (`oauth_tables_hardening`).

1. Extend `acl_entries.resource_type` CHECK to include `agent_session` (keep all current types):

```sql
ALTER TABLE acl_entries DROP CONSTRAINT IF EXISTS acl_entries_resource_type_check;
ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_resource_type_check
    CHECK (resource_type IN ('folder','notebook','connector','dashboard','agent',
                             'model_config','skill','mcp_server','tool','agent_session'));
```

2. Add the inheritance flag:

```sql
ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS share_with_notebook_viewers BOOLEAN NOT NULL DEFAULT FALSE;
```

3. Backfill owner ACL entries for existing sessions:

```sql
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT a.org_id, 'agent_session', s.id, 'user', s.user_id::text,
       ARRAY['view','edit','share','delete','admin']
FROM agent_sessions s
JOIN agents a ON a.id = s.agent_id
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;
```

4. Add the notebook listing index:

```sql
CREATE INDEX IF NOT EXISTS idx_agent_sessions_notebook
    ON agent_sessions (notebook_id, created_at DESC);
```

No schema change is needed to link sessions to notebooks; `notebook_id` already exists.

## Cross-pod live viewing (Redis-backed session streams)

Today `StreamManager` (stream.go) owns a per-process map of sessions, each with subscriber
channels, a 500-event buffer, and a local seq counter. Same-pod multi-viewer already works
(Publish fans out to N channels); cross-pod does not. The notebook `Hub`
(`internal/api/ws.go`) already uses the intended pattern (`ws:notebook:{id}` pub/sub), and the
2026-07-09 horizontal-scaling audit prescribes it for agent streams.

**Keys and channel (per session, TTL 2h refreshed on publish):**

- `aether:agent:sess:{sessionID}:seq` — INCR counter; globally monotonic seq and `LastSeq`.
- `aether:agent:sess:{sessionID}:buf` — LIST, MAX 500, entries `{"seq":N,"msg":{...}}`.
- `aether:agent:sess:{sessionID}` — pub/sub channel.

**Publish** (one Lua script, atomic): `INCR` seq, `RPUSH` entry built from the JSON message,
`LTRIM -500 -1`, refresh TTLs, `PUBLISH` the entry. Local delivery happens **only through the
pod’s Redis subscription** — including on the publishing pod — so an event is never delivered
twice and every pod sees the same seq. Publishing no longer no-ops when the local stream map
has no entry (the subscriber may be on another replica).

**Subscribe:** the first local subscriber for a session starts the pod’s pump: SUBSCRIBE the
channel *first*, then `LRANGE` the buffer, delivering buffered events (unless `skipBuffer`)
and then live events, filtering duplicates by seq and holding live messages that arrive during
the buffer read. A gap (live seq > last delivered + 1) emits the existing `resync` marker so
the client reconciles via `reconnect`. Slow-subscriber eviction and its `resync` marker are
unchanged (the marker carries the dropped event’s seq). The last unsubscribe keeps the
existing 5-second grace before UNSUBSCRIBE + cleanup.

**`LastSeq`** reads the seq key (0 when absent); `reconnect_sync` is unchanged.

**Fallback:** when the engine has no Redis client (unit tests, single-node installs without
Redis), the current in-memory implementation is used unchanged. If Redis is configured but a
publish fails, the event falls back to local fan-out and is logged; seq continuity during such
an outage is best-effort and clients recover through `resync`/`reconnect_sync`.

The wire protocol, `seq` field, and frontend event handling do not change.

## Backend changes

### Permission helper (`internal/api/permissions.go`)

```go
// checkSessionPermission resolves the owner fallback, the agent_session ACL (with the
// admin-mode bypass), and — for view only — live notebook-viewer inheritance.
func (s *Server) checkSessionPermission(ctx context.Context, userID, orgID, orgRole, sessionID, action string) (bool, error)
```

- Owner fallback first (single `SELECT user_id, notebook_id, share_with_notebook_viewers ...`).
- Otherwise `checkPermission(..., "agent_session", sessionID, action)`.
- For `view` only: if the flag is set and `notebook_id` is present, pass when the caller has
  notebook `view`.
- Add a `resourceOrgID` helper used by the admin-mode bypass branch for `folder`,
  `resourceTable` types, and `agent_session` (`agent_sessions → agents.org_id`). Do **not** add
  `agent_session` to `resourceTable` (it has no `org_id`/`folder_id` columns).

### Session lifecycle (`internal/api/agent_handlers.go`)

- **Create** (`handleCreateSession`) request gains:
  `shares: [{subject_type, subject_id, actions}]` and `share_with_notebook_viewers: bool`.
  - If `notebook_id` is provided, require notebook `view` (today it is not checked at all);
    `share_with_notebook_viewers` without a notebook is a 400.
  - Validate shares: `subject_type` in `user|group|org_role`; `org_role` must be `everyone`;
    users must be org members; groups must belong to the org; `actions` empty defaults to
    `["view"]` and any other non-owner action is a 400.
  - Insert session, owner ACL entry, share entries, and the flag in **one transaction**.
  - Scope the “delete empty sessions” cleanup to the same `notebook_id`
    (`notebook_id IS NOT DISTINCT FROM $3`); today it deletes empty sessions for the
    user+agent across all notebooks.
- **List for agent** (`handleListSessions`): return only sessions the caller can view
  (owner, ACL `view`, or inherited), not all users’ sessions. Response gains `owner_email`,
  `shared`, `can_edit`, and `share_with_notebook_viewers`. Keep the existing “has messages”
  filter, ordering, and LIMIT 50; return `[]` instead of `null` for empty results.
- **List for notebook** (new `GET /api/v1/notebooks/{id}/sessions`): requires notebook `view`;
  returns sessions attached to that notebook visible to the caller (uses the new index). Same
  response shape.
- **Shared with me** (new `GET /api/v1/sessions/shared`): sessions the caller can view but
  does not own — direct ACL shares plus inherit-enabled sessions in notebooks they can view.
  Needed when the notebook itself was not shared.
- **Update session** (new `PATCH /api/v1/sessions/{session_id}`): `{title?, share_with_notebook_viewers?}`.
  Title requires session `edit`; the flag requires session `share` and an attached notebook.
  `PATCH /sessions/{session_id}/title` stays for compatibility.
- **Get session / messages / usage** (`handleGetSession`, `handleGetSessionMessages`,
  `handleGetSessionUsage`): require session `view`.
- **Subagent messages** (`handleGetSubagentMessages`): resolve `subagent_tasks.parent_session_id`
  and require the parent session `view` (today: same-org only).
- **Attachments** (`handleUploadAgentAttachment`, `handleGetAgentAttachment`): upload requires
  session `edit` (today agent `edit`); fetch requires session `view` (today: org scope only).

### WebSocket (`internal/api/agent_ws.go`)

- Connection requires session `view` (owner, shared user, inheritor, or admin mode), not
  agent `view`. Compute `canEdit := checkSessionPermission(..., "edit")` once.
- Inbound frames are split into read-only and mutating:
  - Allowed for viewers: `reconnect` only.
  - Require session `edit` and are rejected with a `read-only session` error for viewers:
    `message` (including steering), `cancel`, `slash_command`, `tool_confirm`,
    `question_answer`, `set_reasoning_effort`, `set_page_context`, `set_model_config`,
    `set_admin_mode`.
- Viewers receive every published stream event (tokens, reasoning, tool calls/results,
  subagent events, `done`, `reconnect_sync`) by subscribing to the session stream.
- `set_admin_mode` stays owner/admin-mode-only; a viewer must never flip the transient admin flag.

### ACL API (`internal/api/acl_handlers.go`)

- `handlePutACL` for `agent_session`: only the owner (or admin mode) may write; org admins
  without admin mode are treated as ordinary members (unlike the legacy unconditional bypass
  for other types). Reject any entry for a non-owner subject whose actions are not exactly
  `["view"]`. The owner entry is always preserved: the handler replaces non-owner entries and
  upserts the owner entry, so a replace-style PUT cannot drop the owner.
- `handleGetACL` for `agent_session`: session `view` or admin mode (again: no unconditional
  org-admin bypass).
- Validate subjects for `agent_session` writes same as create (org membership).

### Audit

Session ACL changes flow through existing `acl.granted` / `acl.revoked` / `acl.updated`
entries. Session creation already audits as `resource_type = "agent_session"`. Inheritance
flag changes audit as `agent_session.update_sharing` (or fold into the existing update title
audit if a single update entry is preferred).

## Frontend

### Shared transcript rendering

Extract `MemoizedChatMessage`, the transcript list, and `chatMarkdownComponents` from
`AgentPanel.tsx` into `AgentChatTranscript.tsx` (the previously external chat-UX dependency,
now included here). `AgentPanel` and the read-only viewer both use it.

### Read-only session viewer (new `SessionViewer.tsx`)

- Opens from notebook Chats, “Shared with me”, and session-history rows.
- Header: session title, owner email, “Shared by … · Read-only” banner; Share button only for
  the owner; no composer, no send, no cancel, no settings.
- Connects to the session WS in read-only mode: receives `reconnect_sync` and live events;
  ignores `tool_confirm_required` and `question` events.
- Subagent placeholders are clickable and load subagent detail read-only, live.

### Discovery

- **Notebook Chats**: a drawer on the notebook page listing sessions in that notebook the
  caller can view (`GET /notebooks/{id}/sessions`), with owner, title, first message preview,
  message count, timestamp, and a “Shared” badge.
- **Shared with me**: a section in the agent session history (`GET /sessions/shared`).
- **Session history**: split “My sessions” / “Shared with me”, show owner and shared state;
  clicking a shared session opens the viewer, clicking your own session resumes as today.
- **Sharing**: reuse `PermissionsPanel` with the new `agent_session` type (only `view` is
  selectable; backend enforces it) plus an owner-only “Anyone who can view this notebook”
  checkbox wired to `PATCH /sessions/{id}`.

## CLI / automation

`POST /agents/{id}/session` is the single automation entry point:

- CLI `agents sessions create` gains `--share-user` (repeatable; email or UUID, resolved via
  members), `--share-group` (repeatable; name or UUID, resolved via groups), `--share-everyone`,
  and `--share-notebook-viewers` (requires `--notebook`). All are sent in the create body.
- Fix `internal/cli/agents.go` `CreateSession`: it currently decodes the create response into
  `AgentSession`; the server returns `{session_id, context_window, auto_approve_tools,
  auto_answer_questions}`. Introduce a `CreateSessionResult` type and print the session id plus
  a share summary.
- Later changes: `aether acl set agent_session <id> --entries '[...]'` works once the migration
  allows the type (generic ACL command already exists).

## Testing

- **Redis streams:** two `StreamManager`s sharing one Redis — publish on A reaches subscriber
  on B with monotonic seq; late subscriber replays the buffer; seq is monotonic across
  publishers; nil-Redis keeps today’s behavior; overflow still emits `resync`.
- **Migration:** backfill creates owner entries for pre-existing sessions; `agent_session` is
  accepted by the constraint; flag column defaults false.
- **Permissions (real DB):** owner fallback; direct user/group/Everyone share; inherit flag
  union with notebook `view` and immediate revocation; admin-mode bypass; non-admin-mode org
  admin denied outside shares.
- **Create-with-share:** shares visible immediately after the 201; invalid subject/action 400;
  inherit without notebook 400; unviewable notebook 403; cleanup scoped to the notebook.
- **Read paths:** matrix tests for get/messages/usage/rename/subagent/attachments; `PUT /acl`
  rejections and owner-entry preservation.
- **WS:** viewer connects across managers and receives a published event; viewer mutation
  frames rejected; owner unaffected; admin-mode viewer can mutate; viewer `set_admin_mode` ignored.
- **Frontend:** Vitest for discovery helpers; component tests for the viewer (read-only, no
  composer, ignores confirm/question); `npx tsc --noEmit`.
- **E2E/manual:** two users on the dev stack; live viewer follows a running turn; security
  spot-check with a third user.

## Known limitations

- **Control frames stay pod-local.** `cancel`, `tool_confirm`, `question_answer`, and steering
  resolve through per-pod channels; an owner whose WS lands on a different replica than the
  in-flight turn cannot confirm/cancel until timeout. Viewers are unaffected (they never send
  these frames). A follow-up can move resolution to a Redis control channel.
- **Redis outage:** fan-out falls back to local; remote viewers miss events until Redis
  returns, then reconcile via `resync`/`reconnect_sync`.
- **Inheritance cannot grant edit** and is view-only by construction.
- **No public session links** (D6).

## Sequencing

1. Redis-backed stream fan-out (independent; enables live cross-pod viewing).
2. Migration V124 + permission primitive + owner fallback.
3. Create-with-share (one transaction) and read-path tightening (security fix; existing
   sessions become owner-only unless shared/inherited).
4. Listing endpoints + `PATCH /sessions/{id}`.
5. WS view/edit split.
6. ACL write rules and owner-entry preservation.
7. Transcript extraction + `SessionViewer` + sharing UI.
8. Discovery UI (notebook Chats drawer, shared-with-me, history badges).
9. CLI create flags + decode fix; Swagger + AGENTS.md.
10. Tests throughout; `task check`, frontend build, relay build, e2e before PR.
