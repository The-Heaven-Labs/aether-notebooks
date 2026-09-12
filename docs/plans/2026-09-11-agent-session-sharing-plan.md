# Agent Session Sharing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Link agent sessions to notebooks for discovery and add explicit, read-only, live-viewable session sharing between users while sealing the current authorization holes.

**Architecture:** Make `agent_session` a first-class ACL resource. Owner ACL entries are created with the session and backfilled; `checkPermission` becomes the single access primitive (with an org special-case for admin mode). Tighten every read path and split WS permissions into `view` (connect/read/live) and `edit` (send/cancel/confirm/settings). Add notebook-scoped and shared-with-me listing, a read-only live viewer, and a notebook Chats drawer in the frontend.

**Tech Stack:** Go `net/http` ServeMux + pgx, Postgres migration V097, React + TypeScript, existing WS stream protocol.

**Design doc:** `docs/plans/2026-09-11-agent-session-sharing-design.md`

**Dependency:** the chat UX plan's `AgentChatTranscript` extraction for the viewer.

---

### Task 1: Migration V097

**Files:**
- Create: `internal/database/migrations/V097__agent_session_acl.sql`
- Test: `internal/database/migrations_test.go` or the existing migration/backfill test location

**Step 1: Write the migration**

```sql
-- Make agent sessions shareable resources and backfill owner ACL entries.
ALTER TABLE acl_entries DROP CONSTRAINT IF EXISTS acl_entries_resource_type_check;
ALTER TABLE acl_entries ADD CONSTRAINT acl_entries_resource_type_check
    CHECK (resource_type IN ('folder','notebook','connector','dashboard','agent',
                             'model_config','skill','mcp_server','tool','agent_session'));

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT a.org_id, 'agent_session', s.id, 'user', s.user_id::text,
       ARRAY['view','edit','share','delete','admin']
FROM agent_sessions s
JOIN agents a ON a.id = s.agent_id
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_agent_sessions_notebook
    ON agent_sessions (notebook_id, created_at DESC);
```

**Step 2: Failing test** — in a test that applies migrations, create a session row for an existing agent/user, then assert an owner `acl_entries` row exists with the expected actions and that inserting `resource_type='agent_session'` is accepted. Run `task test:api` or `go test ./internal/database/...`; expected FAIL before the migration.

**Step 3:** apply and pass; commit: `feat(db): agent_session ACL resource type and owner backfill`

---

### Task 2: Permission special-case

**Files:**
- Modify: `internal/api/permissions.go` (admin bypass, ~67-79)
- Test: `internal/api/permissions_test.go`

**Step 1: Failing test** — org admin in admin mode can view a foreign session; without admin mode, cannot (ACL-only). Use `setupTestServer(t)`.

**Step 2: Implement** — add a helper used only by the bypass path:

```go
func (s *Server) resourceOrgID(ctx context.Context, resourceType, resourceID string) (string, error) {
    switch resourceType {
    case "agent_session":
        var orgID string
        err := s.db.Pool.QueryRow(ctx,
            `SELECT a.org_id FROM agent_sessions s JOIN agents a ON a.id = s.agent_id WHERE s.id = $1`,
            resourceID).Scan(&orgID)
        return orgID, err
    }
    if table, ok := resourceTable[resourceType]; ok {
        var orgID string
        err := s.db.Pool.QueryRow(ctx, fmt.Sprintf("SELECT org_id FROM %s WHERE id=$1", table), resourceID).Scan(&orgID)
        return orgID, err
    }
    return "", pgx.ErrNoRows
}
```

Use it in the admin-mode branch; do not add `agent_session` to `resourceTable` (sessions have no `org_id`/`folder_id` columns). Direct ACL matching (step 2 of `checkPermission`) works unchanged.

**Step 3:** pass + commit: `feat(api): agent_session org resolution for admin bypass`

---

### Task 3: Session create — notebook check, owner ACL, scoped cleanup

**Files:**
- Modify: `internal/api/agent_handlers.go` (`handleCreateSession`, ~602-680)
- Test: `internal/api/agent_session_sharing_test.go` (create)

**Step 1: Failing tests**

- Creating a session with a `notebook_id` the caller cannot view → 403.
- Creating a session inserts an owner `acl_entries` row (`subject_type='user'`, actions include `share`).
- The empty-session cleanup deletes only empty sessions for the same user+agent+**notebook**.

**Step 2: Implement**

- After the agent `view` check: if `req.NotebookID != ""`, require notebook `view` via `checkPermission(..., "notebook", req.NotebookID, "view")`.
- Wrap session insert + owner ACL insert in a transaction:

```go
tx, err := h.server.db.Pool.Begin(r.Context())
// INSERT agent_sessions ...
// INSERT acl_entries (org_id, 'agent_session', sessionID, 'user', claims.UserID, {view,edit,share,delete,admin})
tx.Commit(r.Context())
```

- Cleanup:

```sql
DELETE FROM agent_sessions
WHERE agent_id = $1 AND user_id = $2
  AND notebook_id IS NOT DISTINCT FROM $3
  AND id NOT IN (SELECT DISTINCT session_id FROM agent_messages)
```

**Step 3:** pass + commit: `feat(api): notebook-checked session creation with owner ACL`

---

### Task 4: Tighten read paths

**Files:**
- Modify: `internal/api/agent_handlers.go` (`handleGetSession` ~767, `handleGetSessionMessages` ~809, `handleUpdateSessionTitle` ~907, `handleGetSubagentMessages` ~1139)
- Modify: `internal/api/agent_attachment_handlers.go` (`handleGetAgentAttachment` ~108)
- Test: `internal/api/agent_session_sharing_test.go`

**Step 1: Failing tests** — unrelated org member gets 403/404 on get/messages/attachments/subagent messages; owner works; shared user works after Task 6's ACL write (write ACLs directly in the test DB for now). Rename by a shared user fails; rename by owner succeeds without agent `edit`.

**Step 2: Implement** — add a helper:

```go
func (s *Server) checkSessionPermission(ctx context.Context, claims *Claims, session models.AgentSession, action string) (bool, error) {
    if session.UserID == claims.UserID { return true, nil } // owner fallback
    return s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "agent_session", session.ID, action)
}
```

Use `view` in get/messages/subagent/attachment/WS; `edit` in rename. `handleGetSubagentMessages`: load the parent session, check `view`. `handleGetAgentAttachment`: load the attachment's session, check `view`.

**Step 3:** pass + commit: `fix(api): authorize agent session reads at the session level`

---

### Task 5: WebSocket view/edit split

**Files:**
- Modify: `internal/api/agent_ws.go` (~71-105 connect, ~218-345 reader loop)
- Test: `internal/api/agent_ws_test.go`

**Step 1: Failing tests** — shared viewer connects and receives a published session event; viewer `message`, `cancel`, `tool_confirm`, `question_answer`, `set_reasoning_effort`, `set_model_config`, `set_page_context`, `set_admin_mode` frames are ignored/rejected; owner can still send.

**Step 2: Implement**

- On connect, resolve the session and require `view` (owner fallback), not agent `view`; keep the transient admin-mode flag.
- Compute `canEdit, _ := s.checkSessionPermission(ctx, claims, *session, "edit")` once per connection.
- In the reader loop: allow `reconnect` for everyone; for every other current branch (`cancel`, `set_*`, `tool_confirm`, `question_answer`, `message`) `if !canEdit { safeSend(WSResponse{Type:"error", Message:"read-only session"}); continue }`.

**Step 3:** pass + commit: `feat(api): live read-only session viewing over WS`

---

### Task 6: ACL write rules for sessions

**Files:**
- Modify: `internal/api/acl_handlers.go` (`handlePutACL`, ~96)
- Test: `internal/api/agent_session_sharing_test.go`

**Step 1: Failing tests**

- Owner PUT `[{"subject_type":"user","subject_id":<bob>,"actions":["view"]}]` → 200; then Bob can `GET /sessions/{id}/messages`.
- Owner PUT `actions:["edit"]` for Bob → 400.
- Non-owner cannot PUT.

**Step 2: Implement** — after the existing `share` check (owner passes via owner ACL):

```go
if resourceType == "agent_session" {
    var ownerID string
    h.server.db.Pool.QueryRow(ctx, `SELECT user_id FROM agent_sessions WHERE id=$1`, resourceID).Scan(&ownerID)
    if claims.UserID != ownerID && !adminModeFromContext(ctx) {
        writeError(w, http.StatusForbidden, "forbidden"); return
    }
    for _, e := range req.Entries {
        if !(e.SubjectType == "user" && e.SubjectID == ownerID) {
            for _, a := range e.Actions {
                if a != "view" { writeError(w, http.StatusBadRequest, "agent sessions are read-only when shared"); return }
            }
        }
    }
}
```

**Step 3:** pass + commit: `feat(api): read-only ACLs for shared agent sessions`

---

### Task 7: List endpoints

**Files:**
- Modify: `internal/api/router.go` (routes)
- Modify: `internal/api/agent_handlers.go` (`handleListSessions`)
- Create: `internal/api/session_list_handlers.go` (`handleListNotebookSessions`, `handleListSharedSessions`)
- Test: `internal/api/agent_session_sharing_test.go`

**Step 1: Failing tests** — agent list excludes foreign unshared sessions and includes shared ones; notebook list returns sessions for that notebook visible to caller and 403s without notebook `view`; shared list contains only non-owned visible sessions.

**Step 2: Implement**

- `handleListSessions`: fetch candidate rows for the agent (existing query + `owner_email` via `JOIN users`), then filter in Go with `checkSessionPermission(..., "view")`; add `"owner_email"`, `"shared": s.UserID != claims.UserID`, `"can_edit"`.
- `GET /api/v1/notebooks/{id}/sessions` → `handleListNotebookSessions`: require notebook `view`; query by `notebook_id` (uses the new index) with the same visibility filter.
- `GET /api/v1/sessions/shared` → `handleListSharedSessions`: sessions where caller has an ACL `view` and is not the owner.
- Register routes before `/sessions/{session_id}` (Go 1.22 ServeMux prefers the literal path, but keep the literal route explicit).

**Step 3:** pass + commit: `feat(api): notebook and shared session listing`

---

### Task 8: Frontend types and viewer

**Files:**
- Modify: `web/src/types/agent.ts` (`AgentSession`: `owner_email`, `shared`, `can_edit`)
- Modify: `web/src/components/AgentPanel.tsx` (read-only mode)
- Create: `web/src/components/SessionViewer.tsx`
- Test: `web/src/components/SessionViewer.test.tsx`

**Step 1:** `SessionViewer` fetches session + messages, renders `AgentChatTranscript`, opens a WS in read-only mode (reuse the AgentPanel event handling where possible), ignores `tool_confirm_required`/`question`, and shows the header (title, owner, “Shared by … · Read-only”). It must never render the composer.

**Step 2:** Failing tests — viewer fetches and renders messages; a `token` WS event appends live; a `tool_confirm_required` event does not open a dialog. Then implement.

**Step 3:** commit: `feat(web): read-only live session viewer`

---

### Task 9: Discovery UI

**Files:**
- Create: `web/src/components/NotebookChats.tsx` (drawer; consumes `GET /notebooks/{id}/sessions`)
- Modify: `web/src/components/SessionHistory.tsx` (notebook grouping, owner badge, shared badge)
- Modify: `web/src/pages/NotebookPage.tsx` (Chats button/drawer)
- Modify: `web/src/components/PermissionsPanel.tsx` (add `agent_session` labels; only `view` selectable)
- Test: `web/src/components/NotebookChats.test.tsx`, existing component tests

**Step 1:** Notebook Chats drawer lists sessions with owner/title/preview/count/date; clicking a shared session opens `SessionViewer`; clicking own resumes.
**Step 2:** SessionHistory splits “My sessions” / “Shared with me”, grouped by notebook; shared rows use the viewer.
**Step 3:** Share button in the viewer/agent header (owner only) opens the share modal backed by `PermissionsPanel`.
**Step 4:** tests + `npx tsc --noEmit`; commit: `feat(web): notebook chats drawer and session sharing UI`

---

### Task 10: Docs + full verification

**Files:**
- Modify: `AGENTS.md` (session sharing model + `agent_session` ACL type; session access rules)

**Step 1:** run the full gate:

```bash
task check
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
task test:e2e
```

**Step 2:** manual smoke on the dev stack: A creates a session in a notebook, shares the notebook with B, shares the session with B; B sees the Chats drawer entry, opens the live viewer while A chats, cannot send; security spot-check with C (no access).

**Step 3:** commit: `docs: agent session sharing model`

---

## Notes for the executor

- Existing behavior change: sessions become owner-only until shared. Call this out in the PR description.
- Keep the owner fallback (`session.UserID == claims.UserID`) in every helper so pre-backfill sessions never lock out their owner.
- Subagent messages and attachments are readable whenever the parent session is readable.
