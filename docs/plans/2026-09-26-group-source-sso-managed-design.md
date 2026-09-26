# Group Provenance & SSO-Managed Delete Guard

**Date:** 2026-09-26
**Status:** Design

## Problem

The console cannot tell how a group came to exist, and nothing stops an admin
from deleting a group that SSO depends on:

1. **No provenance.** `groups` has no origin column. A group inserted by
   `FindOrCreateGroup` (`internal/api/sso_group_sync.go:167`) is row-identical
   to one created through `POST /groups` (`internal/api/group_handlers.go:100`).
2. **`sso_group_memberships` cannot stand in for provenance.** It tracks
   memberships, not group origin; rows are deleted as soon as the last tracked
   membership is removed (`sso_group_sync.go:117-150`), it cascades away with
   the group (`V073`), an existing manual group can be adopted by SSO via the
   case-insensitive lookup in `FindOrCreateGroup` (`:170`), and staged
   `pending_group_members` deliberately leave no tracking rows at all.
3. **Deletion is unguarded.** `handleDeleteGroup`
   (`internal/api/group_handlers.go:236`) only protects the name-reserved
   `Everyone` group. Deleting an SSO-created group silently drops its
   `group_members` and `sso_group_memberships` rows and orphans every ACL entry
   that referenced its ID. On the next login SSO recreates the group as a new
   row, so the resource ACLs attached to the old ID are gone for good.
4. **No UI signal.** `GroupsPage.tsx` renders a "System" badge only for
   `Everyone` (`:717-719`); admins have no way to see that a group is
   IdP-managed.

SSO sync itself already preserves group rows — it only removes memberships
(`docs/SSO_GROUP_PROVISIONING.md:83`) — so the missing half is protecting the
row from manual deletion and making its origin visible.

## Goals

1. Durable provenance on every group: `manual`, `sso`, or `system`.
2. A visible badge in the console for SSO-managed and system groups.
3. Delete guard: `system` groups can never be deleted; `sso` groups are
   blocked unless the admin explicitly forces deletion, which is audited.
4. SSO adoption: when sync matches an existing manual group by name, that group
   becomes `sso` (one-way).
5. Best-effort backfill of existing rows.
6. Frontend-affecting changes are validated in a real browser (agent-browser)
   and covered by an E2E spec.

## Non-Goals

- `external_id` / provider-group mapping tables and IdP rename tracking
- Automatic deletion or pruning of stale SSO groups (force delete is the
  admin's escape hatch)
- Reverting `sso` → `manual` when SSO sync is disabled or the provider is
  removed
- SCIM, user deactivation, or changes to membership sync semantics

## Design

### 1. Schema (migration V119)

```sql
ALTER TABLE groups ADD COLUMN source text NOT NULL DEFAULT 'manual';
ALTER TABLE groups ADD CONSTRAINT groups_source_check
  CHECK (source IN ('manual', 'sso', 'system'));

UPDATE groups SET source = 'system' WHERE LOWER(name) = 'everyone';
UPDATE groups SET source = 'sso'
 WHERE source = 'manual'
   AND id IN (SELECT DISTINCT group_id FROM sso_group_memberships);
```

- The default keeps existing INSERTs valid; every creation path that must not
  be `manual` is updated explicitly.
- Backfill is best-effort: groups whose SSO tracking rows were already removed
  are indistinguishable from manual and stay `manual`. They become `sso` if
  SSO sync adopts them again.
- The `Everyone` check remains name-based in handlers as defense-in-depth; the
  durable marker is `source='system'`.

### 2. Model & API (`internal/models/group.go`, `internal/api/group_handlers.go`)

- `models.Group` gains `Source string \`json:"source"\``.
- `handleListGroups` selects `g.source` in both queries (`:50`, `:59`).
- `handleCreateGroup` INSERT `RETURNING` includes `source` (default `manual`);
  no request field — clients cannot set it.
- `handleUpdateGroup` `RETURNING` includes `source`; rename/display-name edits
  never change provenance.
- Swagger regenerated: `swag init -g cmd/aether-server/main.go -o internal/api/docs`.

### 3. SSO Sync Adoption (`internal/api/sso_group_sync.go`)

`FindOrCreateGroup` becomes `(id string, created, adopted bool, err error)`:

| Lookup result | Action |
|---|---|
| no row | `INSERT ... source='sso'`; `created=true` |
| `source='manual'` | `UPDATE groups SET source='sso' WHERE id=$1 AND source='manual'`; if a row was affected `adopted=true` |
| `source='sso'` | no-op |
| `source='system'` | never flipped; membership sync proceeds unchanged |

- `SyncSSOGroups` audits `group.sso.adopt` (resource type `group`, resource ID)
  when `adopted` is true, alongside the existing `group.sso.create`.
- Adoption is one-way and survives provider removal or `auto_sync_groups`
  being disabled. An admin who wants the group gone force-deletes it.
- The `UPDATE ... AND source='manual'` predicate makes concurrent logins safe:
  exactly one wins the flip and emits the audit event.

### 4. Delete Guard (`internal/api/group_handlers.go`)

`handleDeleteGroup` loads `source` together with the existence check and
applies:

| `source` | no `force` | `?force=true` |
|---|---|---|
| `manual` | delete, audit `group.delete` | — |
| `sso` | 400 `"group is managed by SSO; pass force=true to delete"` | delete, audit `group.delete.forced` |
| `system` | 400 `"this group cannot be deleted"` | 400, same |

- `force` is `r.URL.Query().Get("force") == "true"`; the route keeps its
  existing org-admin middleware (`router.go:553`).
- The existing Everyone name check stays in front of the switch as a fallback,
  so a hypothetical row named `Everyone` that missed the backfill is still
  undeletable.
- Warehouse sync enqueue (the deleted group's grants) is unchanged and runs for
  forced deletes too.

### 5. Everyone Bootstraps

Every `INSERT INTO groups ... 'Everyone'` sets `source='system'`:

- `internal/api/org_handlers.go:105` (create org)
- `internal/api/org_handlers.go:282` (join org)
- `internal/api/admin_handlers.go:301` (platform-admin org create)
- `internal/api/auth_handlers.go:123` (registration joining an org)

`ON CONFLICT DO NOTHING` sites need no update path; the migration backfills
pre-existing rows.

### 6. Frontend (`web/src/pages/GroupsPage.tsx`, `web/src/types/index.ts`)

- `Group` type gains `source: 'manual' | 'sso' | 'system'`.
- **Badge**: when `source === 'sso'`, render an accent-tinted "SSO" badge next
  to the group name, reusing the existing `systemBadge` styling pattern with
  CSS variables (no hardcoded colors). `Everyone` keeps its "System" badge.
- **Actions**: the `...` menu stays hidden for `source === 'system'`
  (generalizing today's `isEveryone` hide). For `source === 'sso'` the menu
  keeps Delete.
- **Force-delete dialog**: SSO groups open a distinct confirmation —
  *"Managed by SSO — deleting it won't stop your identity provider from
  recreating it if it still sends a group named “X”. Delete anyway?"* — with a
  red **Delete anyway** button that calls `DELETE /groups/{id}?force=true`.
  Manual groups keep the current dialog and request.
- `deleteGroup` mutation accepts `{ id, force?: boolean }`.

### 7. CLI (`internal/cli/types.go`, `internal/cli/groups.go`)

- `Group` gains `Source`.
- `groups delete <id> --force` sets the query param.
- `groups list` marks SSO and system groups (e.g. `[SSO]`, `[system]`).

### 8. Testing

Backend (real DB, existing helpers):

- `group_handlers_test.go`: `source` present in list/create/update responses;
  manual delete 204; SSO delete 400; SSO + force 204 with a
  `group.delete.forced` audit row; system + force 400; unknown group 404.
- `sso_group_sync_test.go`: sync-created group has `source='sso'` and audits
  `group.sso.create`; syncing a name that matches a manual group flips it to
  `sso` and audits `group.sso.adopt`; a `system` group is never flipped.
- `everyone_group_test.go`: Everyone rows are `source='system'` and remain
  undeletable with `force=true`.

Frontend:

- `GroupsPage.test.tsx`: SSO badge renders only for SSO groups; SSO delete
  dialog shows the warning and sends `force=true`; system group has no actions
  menu; manual flow unchanged.

E2E (`e2e/group-source.spec.ts`, dev stack on :5173):

- Create a manual group via UI → no SSO badge; regular dialog deletes it.
- Create a group, flip it to `source='sso'` with
  `docker compose -f docker-compose.dev.yml exec -T aether-postgres psql -U aether -d aether -c ...`,
  reload `/groups` → SSO badge visible; API `DELETE` without force returns 400;
  Delete → warning dialog → **Delete anyway** removes it.
- Assert the Everyone row has no actions menu.

### 9. Browser Validation (mandatory for UI changes)

Every change that touches the frontend is validated in a real browser before
being called done, independently of the E2E spec. For this change:

1. `docker compose -f docker-compose.dev.yml up -d`, wait for API/web health.
2. `agent-browser --session aether open http://localhost:5173/login`, log in as
   an org admin.
3. Create a manual group → assert no SSO badge; screenshot.
4. Flip a group to `source='sso'` via SQL, reload → badge visible; screenshot.
5. Open Delete → warning dialog; screenshot before confirming; confirm
   **Delete anyway** → group disappears.
6. Confirm the Everyone row renders no actions menu.
7. API double-check with curl: `DELETE /api/v1/groups/{id}` on an SSO group
   without `force` returns 400.
8. `agent-browser errors` and `agent-browser console` are clean.

This rule is added to `AGENTS.md`'s Frontend section so it applies to all
future UI work.

### 10. Docs

- `docs/SSO_GROUP_PROVISIONING.md`: provenance column, adoption semantics,
  force-delete escape hatch.
- `AGENTS.md`: groups/SSO paragraph (source, guard) and the mandatory
  browser-validation rule.
- Swagger regenerated.

## Migration Plan

1. V119 migration + `models.Group.Source`.
2. `handleListGroups` / `handleCreateGroup` / `handleUpdateGroup` source
   selection and serialization.
3. `FindOrCreateGroup` adoption + `SyncSSOGroups` audit event.
4. `handleDeleteGroup` guard + force audit; update the four Everyone bootstrap
   INSERTs.
5. CLI `--force` and source marker.
6. Frontend type, badge, menu gating, force-delete dialog.
7. Backend, frontend, and E2E tests.
8. Swagger regen, docs, `AGENTS.md` rule, browser validation pass.
