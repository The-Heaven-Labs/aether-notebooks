# Agent Session Sharing & Notebook Linkage — Design

**Date:** 2026-09-11
**Status:** Approved (design decisions D1–D6 settled)

## Problem

Agent sessions are already linked to notebooks (`agent_sessions.notebook_id`, nullable since
V061), but there is no sharing model and the current access control is too loose:

- Every session route (`list`, `get`, `messages`, `WS`, subagent messages, attachments)
  authorizes against the **agent** (`view`), never the session or its owner. Any org member
  who can see an agent can read every user's sessions for that agent.
- Resuming someone else's session over WS executes tools, ACL checks, and audit entries as
  `session.UserID` (`engine.go` builds `ToolContext{UserID: session.UserID}`), so a second
  user acts as the owner.
- There is no way to see which sessions belong to a notebook, and no read-only access.

## Goals

- Sessions are linked to notebooks for discovery: a notebook shows the chats that happened in it.
- A session owner can explicitly share a session read-only with users/groups.
- A shared user can read the transcript (including subagent detail and attachments) and watch
  it live, but can never send, cancel, confirm tools, answer questions, or change settings.
- Fix the authorization holes above as part of the same change.

## Non-goals

- Public/unauthenticated session links (`public_tokens` stays notebook/dashboard only).
- Cross-org sharing (all ACLs are org-scoped).
- Auto-sharing sessions when a notebook is shared (sharing is always explicit per session).
- Editing/“co-authoring” a session by more than one user.

## Decisions

| # | Decision |
|---|---|
| D1 | Sessions become an ACL resource type: `agent_session`. |
| D2 | Sharing is explicit per session (no implicit notebook inheritance). |
| D3 | Discovery ships in the UI (notebook Chats view + “Shared with me” + history badges). |
| D4 | Live read-only from day one: viewers connect over WS and receive stream events. |
| D5 | Access tightening approved: unshared sessions are visible to their owner only. |
| D6 | Same-org only; no public links for sessions. |

## Access model

`agent_session` is added to the `acl_entries.resource_type` CHECK constraint and to the
permission system. Owner ACL entries are created at session creation and backfilled by
migration, so `checkPermission` is the single access primitive.

**Owner entry (create + backfill):**

```
subject_type='user', subject_id=<owner user id>, actions={view,edit,share,delete,admin}
```

**Read-only sharing enforcement:** for `resource_type='agent_session'`, the ACL PUT handler
rejects entries for any non-owner subject whose actions are not exactly `{view}`. We never
grant `edit` to another user through the share UI; the only `edit` path is the owner entry
(and platform/org admins in admin mode).

**Org resolution / admin bypass:** `checkPermission` resolves the resource org for
`agent_session` through `agent_sessions → agents.org_id`. Sessions have no `folder_id`, so
they are not in the folder-inheritance walk. Org admins bypass only with admin mode enabled,
as everywhere else.

### Access matrix

| Caller | List in notebook | Read transcript / subagents / attachments | Watch live | Rename | Send / cancel / confirm / settings |
|---|---|---|---|---|---|
| Owner | yes | yes | yes | yes | yes |
| Shared user or group | if they can view the notebook | yes | yes | no | no |
| Org member without share | no | no | no | no | no |
| Org admin, no admin mode | only shared | only shared | only shared | no | no |
| Org admin + admin mode | yes | yes | yes | yes | yes |

Notes:

- The owner always passes via the owner ACL entry; handlers may additionally fall back to
  `session.UserID == claims.UserID` for safety if a backfill was missed.
- A shared user does not need access to the agent, only the session. If they lack notebook
  view, the session is still readable but the notebook is marked unavailable in the UI.
- Tool call results, SQL, and reasoning are part of the transcript and are therefore visible
  to shared users. Sharing is explicit, so this is intended.

## Database changes (V097)

1. Extend `acl_entries.resource_type` CHECK to include `agent_session`.
2. Backfill owner ACL entries for existing sessions:

```sql
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT a.org_id, 'agent_session', s.id, 'user', s.user_id::text,
       ARRAY['view','edit','share','delete','admin']
FROM agent_sessions s
JOIN agents a ON a.id = s.agent_id
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;
```

3. Add `CREATE INDEX idx_agent_sessions_notebook ON agent_sessions (notebook_id, created_at DESC);`

No schema change is needed to link sessions to notebooks; `notebook_id` already exists.

## Backend changes

### Session lifecycle (`internal/api/agent_handlers.go`)

- **Create** (`handleCreateSession`):
  - If `notebook_id` is provided, require notebook `view` (today it is not checked at all).
  - Insert the owner ACL entry in the same transaction as the session.
  - Scope the “delete empty sessions” cleanup to the same `notebook_id`
    (`notebook_id IS NOT DISTINCT FROM $3`); today it deletes empty sessions for the
    user+agent across all notebooks.
- **List for agent** (`handleListSessions`): return only sessions the caller can view
  (owner or ACL `view`), not all users' sessions. Response gains `owner_email`, `shared`
  (bool), and `can_edit` (bool). Keep the existing “has messages” filter and LIMIT 50.
- **List for notebook** (new `GET /api/v1/notebooks/{id}/sessions`): requires notebook
  `view`; returns sessions attached to that notebook visible to the caller. Same response
  shape as above. Implementation can fetch by notebook then filter with
  `checkPermission` (bounded by LIMIT).
- **Shared with me** (new `GET /api/v1/sessions/shared`): sessions the caller can view but
  does not own. Needed when the notebook itself was not shared.
- **Get session / messages** (`handleGetSession`, `handleGetSessionMessages`): require
  session `view`.
- **Rename** (`handleUpdateSessionTitle`): require session `edit` (owner), not agent `edit`.
- **Subagent messages** (`handleGetSubagentMessages`): require the parent session `view`
  (today: same-org only).
- **Attachments** (`handleGetAgentAttachment`): require the owning session `view`
  (today: same-org only).

### WebSocket (`internal/api/agent_ws.go`)

- Connection requires session `view` (owners, shared users, admin mode), not agent `view`.
- Inbound frames are split into read-only and mutating:
  - Allowed for viewers: `reconnect` only.
  - Require session `edit` (owner or admin mode) and are ignored/rejected with an error for
    viewers: `message`, `cancel`, `tool_confirm`, `question_answer`,
    `set_reasoning_effort`, `set_page_context`, `set_model_config`, `set_admin_mode`.
- Viewers receive every published stream event (tokens, reasoning, tool calls/results,
  subagent events, `done`, `reconnect_sync`) simply by subscribing to the session stream.
- `set_admin_mode` stays owner-only; a viewer must never flip the transient admin flag.

### ACL API (`internal/api/acl_handlers.go`)

- `handlePutACL` for `agent_session`: only the owner (or admin mode) may write; reject any
  entry for a non-owner subject whose actions contain anything other than `view`.
- `handleGetACL` works through the existing `view` check.

### Permission helper (`internal/api/permissions.go`)

- Add `agent_session` org resolution for the admin-mode bypass path.
- Do **not** add `agent_session` to `resourceTable` (used for `org_id`/`folder_id` lookups);
  special-case the org query instead.

### Audit

Session ACL changes flow through existing `acl.granted` / `acl.revoked` / `acl.updated`
audit entries. Session creation already audits as `resource_type = "agent_session"`.

## Frontend

### Shared transcript rendering

Extract `MemoizedChatMessage` and the transcript list from `AgentPanel.tsx` into
`AgentChatTranscript.tsx` (also required by the chat UX design). Message state reduction for
streaming is extracted into a pure helper so the main panel and the read-only viewer share
event handling.

### Read-only session viewer (new)

- Opens from notebook Chats, “Shared with me”, and session history rows.
- Header: session title, owner email, “Shared by … · Read-only” banner; Share button only
  for the owner; no composer, no send, no cancel, no settings.
- Connects to the session WS in read-only mode: receives `reconnect_sync` and live events;
  ignores `tool_confirm_required` and `question` events.
- Subagent placeholders are clickable and load subagent detail read-only, live.

### Discovery

- **Notebook Chats**: a drawer/panel on the notebook page listing sessions in that notebook
  the caller can view, with owner, title, first message preview, message count, timestamp,
  and a “Shared” badge. This is the primary entry point named in the requirement.
- **Shared with me**: a section in the agent session history for sessions shared by others.
- **Session history**: group/filter by notebook, show owner and shared state; clicking a
  shared session opens the viewer, clicking your own session resumes as today.
- **Share modal**: reuse `PermissionsPanel` with the new `agent_session` type; only the
  `view` action is selectable and the backend enforces it.

## Testing

- API tests (real DB, no mocks):
  - Owner can list/get/messages/rename; unrelated org member gets 403/empty.
  - Explicit share grants read but not rename and not WS mutations.
  - `handlePutACL` rejects `edit` grants for non-owner subjects.
  - Notebook sessions endpoint filters correctly (owner + shared only, notebook-scoped).
  - Admin mode bypasses; non-admin-mode org admin does not.
  - WS: viewer connects and receives a stream event published by the engine; viewer
    `message`/`cancel`/`tool_confirm` frames are rejected.
  - Subagent messages and attachments honor session share.
- Migration test: backfill creates owner entries for pre-existing sessions.
- Frontend: Vitest for discovery filter helpers; Playwright optional for the viewer flow.

## Sequencing

1. Migration + permission special-case + owner ACL at create (no UI change).
2. Tighten all read paths and WS (security fix; existing sessions become owner-only).
3. List endpoints (agent list, notebook list, shared with me).
4. Transcript extraction + SessionViewer + share modal.
5. Discovery UI (notebook Chats drawer, shared-with-me, history badges).
6. Tests throughout; `task check`, frontend build, relay build, e2e before PR.
