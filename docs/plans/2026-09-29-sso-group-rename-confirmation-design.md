# Guarded Rename for SSO-Managed Groups

**Date:** 2026-09-29
**Status:** Design

## Problem

`groups.name` is the only key SSO sync matches on
(`FindOrCreateGroup`, `internal/api/sso_group_sync.go:173`), yet the console and
API let an org admin rename an SSO-managed group without any warning:

1. **The rename silently breaks sync.** On the next login,
   `FindOrCreateGroup` no longer matches the renamed row, so it creates a brand
   new `source='sso'` group for the IdP name (`sso_group_sync.go:194`).
   `FindStaleSSOGroups` then sees the renamed group's name missing from the
   resolved IdP list and removes its `group_members` and
   `sso_group_memberships` rows (`sso_group_sync.go:120-152`).
2. **Access disappears without a signal.** ACL entries reference the group ID,
   so every permission granted to the renamed group stops applying to its
   former SSO members, who now only live in the replacement group. Warehouse
   `warehouse_table_grants` behave the same way once
   `enqueueWarehouseSyncForGroup` reconciles the removed memberships
   (`oidc_handlers.go:561-563`).
3. **Existing guards don't cover rename.** Delete is already source-aware
   (`group_handlers.go:252-260`), but `handleUpdateGroup` only protects
   `Everyone` and never emits a rename-specific audit event. The UI renders the
   SSO badge but allows the same inline rename as manual groups
   (`GroupsPage.tsx:683-713`, `:731`).
4. **`display_name` is the safe escape hatch.** `V116` added
   `groups.display_name` as presentation-only, and sync never reads or writes
   it (`TestFindOrCreateGroup_PreservesDisplayName`), so labels can be edited
   freely.

The facility to customize the label exists; what is missing is a deliberate,
hard-to-accident confirmation before changing the sync identity — and an
explicit API contract so CLI and agent callers can't do it by mistake either.

## Goals

1. Editing only the display label of an SSO-managed group keeps working with no
   extra step.
2. Renaming an SSO-managed group (`name` changes) requires an explicit
   type-the-current-name confirmation in the UI.
3. The API fails closed on unacknowledged SSO renames: the caller must send
   `confirm_name` equal to the group's current name; otherwise **409 Conflict**
   with a message naming the value to confirm.
4. Manual groups and display-only updates are unaffected — no new required
   fields for existing clients.
5. An acknowledged SSO rename emits a distinct, discoverable audit event
   (`group.sso.rename`) with old/new names.
6. The CLI exposes `--confirm-name` so a deliberate rename is possible without
   hand-rolled HTTP.
7. Frontend changes are validated in a real browser (agent-browser) and covered
   by component and E2E tests.

## Non-Goals

- No schema change, no new columns, no `external_id` / provider-group mapping
- No change to SSO matching, adoption, or stale-membership semantics
- No tracking or repair of already-renamed orphaned groups (force-delete
  remains the cleanup path)
- No new delete guard; `system` groups remain uneditable and everyone's rename
  guard is untouched
- No change to display-name validation rules (`everyone` reserved, blank → NULL)

## Design

### 1. API Contract (`internal/api/group_handlers.go`)

`updateGroupRequest` gains an optional field:

```go
type updateGroupRequest struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
	ConfirmName *string `json:"confirm_name"`
}
```

`handleUpdateGroup` (`:151`) changes:

1. Load the target row up front:
   ```sql
   SELECT name, source FROM groups WHERE id=$1 AND org_id=$2
   ```
   `Everyone` is still derived by `strings.EqualFold(currentName, "everyone")`,
   so all existing `Everyone` errors keep the same behavior and status codes
   (the shared `isEveryoneGroup` helper stays for the member handlers that use
   it).
2. After the existing name/display validation, compute
   `nameChanged := req.Name != nil && *req.Name != currentName` (exact compare
   — a case-only rename counts).
3. If `currentSource == "sso" && nameChanged`:
   - `req.ConfirmName == nil || *req.ConfirmName != currentName` →
     `409 Conflict` with
     `group is managed by SSO; renaming it disconnects sync. Send confirm_name with the current name ("<current>") to proceed`.
   - Otherwise proceed with the update.
4. Audit:
   - Acknowledged SSO rename → action `group.sso.rename`, metadata
     `{"old_name": ..., "new_name": ..., "display_name": ...}`.
   - All other updates → existing `group.update`.
5. Unique-name collision: a `23505` from the UPDATE now returns
   `409 Conflict` `"a group with this name already exists"` via the existing
   `isUniqueViolation` helper (`mcp_server_handlers.go:303`), instead of the
   misleading 404 today. Unknown group stays 404.
6. Swagger annotation updated (`409` response, `confirm_name` documented);
   regenerate with `swag init -g cmd/aether-server/main.go -o internal/api/docs`.

### 2. Frontend (`web/src/pages/GroupsPage.tsx`)

- `updateGroup` mutation gains optional `confirm_name`, included in the body
  only when set.
- `handleRenameSubmit` (`:499`):
  - Empty/reserved-name checks unchanged.
  - If the target group has `source === 'sso'` and the trimmed name differs
    from `group.name`, stash `{ group, newName, newDisplay }` and open the
    confirmation dialog instead of mutating.
  - Otherwise (manual group, or SSO group with unchanged name) mutate exactly
    as today.
- New type-to-confirm dialog, copying the platform-admin delete-org pattern
  (`AdminPage.tsx:1097-1121`) with the shared `ConfirmDialog`
  (`confirmDisabled`, `defaultFocusRef`, `children` already supported):
  - Title `Rename SSO-managed group?`, destructive styling, confirm label
    `Rename group`.
  - Message explains the consequence: the next SSO login will create a new
    group for `"<name>"` and remove its members from this one, so its
    permissions and warehouse grants stop applying.
  - Instruction `Type <strong>{group.name}</strong> to confirm` + controlled
    input; confirm disabled until the typed value exactly equals the current
    name.
  - Confirm sends `confirm_name: group.name`; cancel closes the dialog and
    leaves the inline editor open unchanged.
- The SSO badge, `Everyone`/`system` action gating, and delete flow are
  untouched.

### 3. CLI (`internal/cli/groups.go`)

- `UpdateGroup(id, name, confirmName string)`; `confirm_name` is included only
  when non-empty.
- `aether groups update <id> --name X [--confirm-name Y]`; help text:
  `Current group name; required to rename an SSO-managed group`.

### 4. Testing

Backend (`internal/api/group_handlers_test.go`, real DB):

- SSO group rename without `confirm_name` → 409, name unchanged in DB.
- SSO group rename with a wrong `confirm_name` → 409.
- SSO group rename with the exact current name → 200, name changed, one
  `group.sso.rename` audit row carrying old/new names.
- SSO group display-name-only update → 200 without `confirm_name`, emits
  `group.update`.
- Manual group rename without `confirm_name` → 200 (no regression).
- Duplicate-name rename → 409 `"a group with this name already exists"`.

Frontend (`web/src/test/GroupsPage.test.tsx`, MSW):

- SSO group + changed name opens the dialog; confirm stays disabled until the
  exact current name is typed; typing and confirming sends `PUT` with
  `confirm_name`.
- Cancel sends nothing and keeps the old name.
- SSO group display-label-only edit saves directly, no dialog.
- Manual group rename shows no dialog.
- Update the MSW PUT handler fixture if it needs to accept `confirm_name`.

E2E (`e2e/group-source.spec.ts`, dev stack on :5173):

- Create a group, flip it to `source='sso'` with the existing psql helper,
  open Rename, change the name, assert the confirmation dialog and disabled
  confirm until the synced name is typed, then confirm and assert the rename.

Browser validation (mandatory per AGENTS.md): run the same flow manually with
agent-browser against the dev stack, screenshot the dialog, and check
`agent-browser errors` / `console` are clean.

### 5. Docs

- `docs/SSO_GROUP_PROVISIONING.md`: add the guarded-rename behavior, the
  `confirm_name` contract, and `group.sso.rename` to the audit event list.
- Swagger regenerated.

## Migration Plan

1. Backend request struct + source-aware guard + audit + 409 collision fix.
2. Backend tests.
3. Frontend mutation/state/dialog + component tests.
4. CLI flag.
5. E2E spec extension; agent-browser validation pass.
6. Swagger regen + docs.
