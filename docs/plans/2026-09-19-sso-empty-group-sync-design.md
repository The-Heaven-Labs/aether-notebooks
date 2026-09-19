# SSO Group Sync: Authoritative Empty Claims + Prefix Stripping

**Date:** 2026-09-19
**Status:** Design

## Problem

Two gaps make Aether group sync unusable for IdP-driven access management:

1. **Zero groups is a no-op.** `SyncSSOGroups` returns early when the filtered group list is empty (`internal/api/sso_group_sync.go:21-23`), and the callback only calls it when `len(claims.Groups) > 0` (`internal/api/oidc_handlers.go:522-525`). A user removed from every group in the IdP keeps their Aether memberships forever. Keycloak's group membership mapper *omits* the claim entirely for users with zero groups ([keycloak#22340](https://github.com/keycloak/keycloak/issues/22340)), so there is no explicit empty array to detect.
2. **No display/identity separation.** The `groups` table stores only `(id, org_id, name, created_at)`. Sync matches and creates by name, and `group_prefix` is a filter only — the prefix is kept in the stored name (`sso_group_sync.go:17-18,127-149`). IdP groups namespaced as `Aether Notebooks: Area` show up verbatim in the Aether UI.

Because an absent claim is indistinguishable from a removed/misconfigured mapper, both behaviors are **opt-in** so existing providers keep current semantics.

## Goals

1. Allow a user to legitimately end up with zero SSO-managed groups, reconciling stale memberships away on login.
2. Keep the group name namespace/filter prefix out of the user-visible name.
3. Preserve all existing behavior for providers that do not opt in.
4. Avoid wiping memberships on transient IdP/UserInfo failures.

## Non-Goals

- `external_id` / `display_name` columns or IdP rename tracking (names remain the identity)
- SCIM, real-time sync, user deactivation
- Group deletion (sync still only removes memberships)
- Claim transforms beyond literal prefix strip

## Design

### 1. Provider Configuration (migration V111)

```sql
ALTER TABLE sso_providers
  ADD COLUMN sync_empty_groups  boolean NOT NULL DEFAULT false,
  ADD COLUMN strip_group_prefix boolean NOT NULL DEFAULT false;
```

| Column | Purpose |
|---|---|
| `sync_empty_groups` | When `auto_sync_groups` is on: an absent/empty groups claim is authoritative — remove all memberships tracked in `sso_group_memberships` for this provider+user. When false (default), empty remains a no-op. |
| `strip_group_prefix` | When `group_prefix` is non-empty: store the IdP name with the prefix removed, trimmed. Filtering and stale comparison use the stripped name. When false (default), the full IdP name is stored. |

Added to `sso.Provider`, `selectProviderCols`, `CreateProvider`, `UpdateProvider`, `providerResponse`, `providerToResponse`, `ssoProviderRequest`, the frontend `SSOProvider` type, and both provider forms (`AdminPage.tsx`, `OrgSettingsPage.tsx`).

### 2. Sync Semantics (`internal/api/sso_group_sync.go`)

Name resolution:

```
for each claim group g:
    if group_prefix != "" && !strings.HasPrefix(g, group_prefix): skip
    name := g
    if strip_group_prefix: name = strings.TrimSpace(strings.TrimPrefix(g, group_prefix))
    if name == "": skip
    append name
```

- `FindOrCreateGroup` and stale comparison operate on resolved names.
- If no names remain:
  - `sync_empty_groups=false` → return (unchanged).
  - `sync_empty_groups=true` → skip the add loop and run stale removal with an empty current set, deleting every tracked `sso_group_memberships` row and its `group_members` counterpart. Manual and pending memberships (no tracking row) are untouched; groups are never deleted.
- Callback gate becomes `if dbProvider.AutoSyncGroups && !claims.GroupsUnavailable`.

Prefix behavior examples:

| IdP group | Prefix | Strip | Stored name |
|---|---|---|---|
| `aether-Area` | `aether-` | on | `Area` |
| `Aether Notebooks: Area Name` | `Aether Notebooks:` | on | `Area Name` |
| `aether-Analysts` | `aether-` | off | `aether-Analysts` |

The prefix match is case-sensitive; internal spaces are preserved.

### 3. Safety Guard (`internal/auth/oidc.go`)

Add `GroupsUnavailable bool` to `OIDCClaims`. It is set when `get_user_info=true` and the UserInfo request or claims decode fails *and* the ID token carried no groups claim. The callback then skips sync entirely, so a transient UserInfo outage cannot wipe memberships. If the ID token did carry groups, those are used as before.

A removed mapper still looks identical to "zero groups" (claim absent). That ambiguity is inherent to OIDC and is why `sync_empty_groups` is opt-in; admins enabling it accept the IdP as authoritative.

### 4. Adjacent Fixes

- **Case-insensitive stale comparison.** `FindStaleSSOGroups` currently compares names case-sensitively (`sso_group_sync.go:157`) while find-or-create is case-insensitive, causing add/remove churn when IdP casing varies. Lowercase the current names in Go and compare `LOWER(g.name) != ALL($3)`.
- **Audit events.** Emit the already-documented `group.sso.create`, `group.sso.add_member`, and `group.sso.remove_member` events. This makes authoritative-empty removals visible in the audit log. `FindOrCreateGroup` returns a `created bool` to support the create event. A skipped sync due to `GroupsUnavailable` is audited as `group.sso.error` with explanatory metadata.

### 5. API & Frontend

- `internal/api/sso_admin_handlers.go`, `sso_org_handlers.go`: serialize/accept the two new booleans.
- `web/src/types/index.ts`: extend `SSOProvider`.
- `AdminPage.tsx`, `OrgSettingsPage.tsx`: two checkboxes in the provider dialog — "Sync empty groups" (helper: removes SSO-managed memberships when the IdP reports no groups) and "Strip group prefix" (helper: store names without the prefix). Include both in the boolean field handler and create/update payloads.
- Regenerate Swagger: `swag init -g cmd/aether-server/main.go -o internal/api/docs`.

### 6. Testing

- Rework `TestSyncSSOGroups_EmptyGroups`: flag off → no-op; flag on → tracked memberships removed, manual memberships preserved, group rows retained.
- New: multi-word prefix strip (`Aether Notebooks: Area Name` → `Area Name`), whitespace trim, empty-after-strip skip, case-insensitive stale comparison.
- Callback tests: auto-sync with empty claim + `sync_empty_groups` on removes; `GroupsUnavailable` skips.
- Provider CRUD round-trip covers the new columns.
- Default-off paths keep existing tests green.

## Migration Plan

1. Migration V111 + `sso.Provider`/CRUD/serialization updates.
2. `OIDCClaims.GroupsUnavailable` and Exchange guard.
3. `SyncSSOGroups` filter/strip/empty handling + case-insensitive stale + audit events.
4. Callback wiring.
5. API responses/requests + frontend types/forms + Swagger.
6. Tests and `docs/SSO_GROUP_PROVISIONING.md` updates.
