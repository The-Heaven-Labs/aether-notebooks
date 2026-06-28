# SSO Group Provisioning via OIDC Claims

**Date:** 2026-06-28
**Status:** Design

## Problem

When users authenticate via SSO/OIDC, hnb only extracts `email` and `name` from the ID token. Many organizations use IDP groups (Azure AD groups, Keycloak groups, Okta groups, etc.) to manage access, and users expect those group memberships to carry over into hnb automatically.

## Goals

1. Extract group membership from the OIDC ID token and/or UserInfo endpoint
2. Auto-create hnb groups that match IDP groups (scoped to the user's org)
3. Add/remove users from groups in sync with the IDP on each login
4. Support a group name prefix filter to avoid syncing irrelevant cross-system groups
5. Preserve manually-added group memberships (only reconcile SSO-provisioned ones)
6. Be IDP-agnostic — works with any standard OIDC provider

## Non-Goals

- Complex claim mapping/transforms (regex filtering, prefix/suffix transforms on group names)
- Authentication gating via groups (allowing only certain groups to log in)
- Real-time group sync (reconciliation happens on login only)
- SCIM provisioning

## Design

### 1. Database Schema

New migration adds columns to `sso_providers` and a tracking table:

```sql
ALTER TABLE sso_providers
  ADD COLUMN scopes           text[] NOT NULL DEFAULT '{}',
  ADD COLUMN groups_claim     text   NOT NULL DEFAULT 'groups',
  ADD COLUMN group_prefix     text   NOT NULL DEFAULT '',
  ADD COLUMN auto_sync_groups bool   NOT NULL DEFAULT false,
  ADD COLUMN get_user_info    bool   NOT NULL DEFAULT false;

CREATE TABLE sso_group_memberships (
    provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
    group_id    UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_id, group_id, user_id)
);
```

#### Column Details

| Column | Purpose |
|---|---|
| `scopes` | Additional OIDC scopes beyond `openid profile email`. E.g., `["groups"]` to request group claims |
| `groups_claim` | The claim key in the ID token/UserInfo that contains the groups array. Default `"groups"` handles Azure AD, Keycloak, Okta. Can be overridden for non-standard claim names |
| `group_prefix` | Only sync groups whose names start with this prefix. Empty string = sync all. E.g., `"hnb-"` only syncs `hnb-analysts`, `hnb-viewers` but skips `all-employees` |
| `auto_sync_groups` | Master toggle to enable group provisioning for this provider |
| `get_user_info` | Whether to call the UserInfo endpoint for additional claims after token exchange |

#### `sso_group_memberships` Purpose

This table tracks which group memberships were provisioned via SSO. On each login, we diff the current IDP groups against SSO-provisioned memberships to determine which to remove. Manually added memberships (via the admin group UI) are never touched because they have no `sso_group_memberships` row.

### 2. Go Model Changes

**`internal/auth/oidc.go`:**

```go
type OIDCClaims struct {
    Subject string
    Email   string
    Name    string
    Groups  []string // new
}

type GenericOIDCProvider struct {
    name        string
    verifier    *oidc.IDTokenVerifier
    oauth       oauth2.Config
    groupsClaim string   // new — which claim to read
    getUserInfo bool     // new — whether to call UserInfo
}
```

`NewGenericOIDCProvider` signature gains `groupsClaim string` and `getUserInfo bool` parameters.

`Exchange` method changes:
1. Parse the groups claim from the ID token (using `groupsClaim` as the key)
2. If `getUserInfo` is true, call the UserInfo endpoint using the access token, and merge groups from the response

**`internal/sso/sso.go`:**

```go
type Provider struct {
    // existing fields...
    Scopes         []string
    GroupsClaim    string
    GroupPrefix    string
    AutoSyncGroups bool
    GetUserInfo    bool
}
```

The `Provider` struct, CRUD functions, caching, and serialization all need updates for new fields. The `scanProvider`, `CreateProvider`, `UpdateProvider` functions update their SQL queries accordingly.

### 3. Provider Configuration at Login

`handleOIDCLogin` and `handleOIDCCallback` in `internal/api/oidc_handlers.go` currently pass `nil` for scopes. With the new design:

```go
provider, err := auth.NewGenericOIDCProvider(ctx,
    dbProvider.Name, dbProvider.DiscoveryURL,
    dbProvider.ClientID, dbProvider.ClientSecret,
    s.callbackURL(r, providerID),
    dbProvider.Scopes,        // now passed explicitly
    dbProvider.GroupsClaim,   // new
    dbProvider.GetUserInfo,   // new
)
```

### 4. Group Reconciliation Logic

Added to `handleOIDCCallback` after user lookup/creation:

```
After user lookup/creation succeeds:
  if dbProvider.AutoSyncGroups AND len(claims.Groups) > 0:
    1. Filter groups by prefix:
       if dbProvider.GroupPrefix != "":
           filtered = [g for g in claims.Groups if g starts with dbProvider.GroupPrefix]
       else:
           filtered = claims.Groups

    2. For each group name in filtered:
       a. Find or create hnb group (within user's org)
          - Check case-insensitive match first (groups have UNIQUE(org_id, name))
          - INSERT group if not found
          - Audit log: group.sso.create
       b. INSERT INTO group_members ON CONFLICT DO NOTHING
       c. INSERT INTO sso_group_memberships ON CONFLICT DO NOTHING

    3. Remove stale memberships:
       SELECT sgm.group_id
       FROM sso_group_memberships sgm
       JOIN groups g ON g.id = sgm.group_id
       WHERE sgm.provider_id = $1 AND sgm.user_id = $2
       AND g.name NOT IN ($filtered_group_names)

       For each stale group_id:
         DELETE FROM group_members WHERE group_id=$1 AND user_id=$2
         DELETE FROM sso_group_memberships WHERE provider_id=$1 AND group_id=$2 AND user_id=$3
         Audit log: group.sso.remove_member
```

#### Key behaviors:

- **Auto-create**: Groups are created in the user's org with the exact name from the IDP (prefix included)
- **Case handling**: Group names are matched case-insensitively against the `UNIQUE(org_id, name)` constraint
- **Partial failure**: If group reconciliation fails, the login still succeeds — the error is logged and audited
- **No group deletion**: Groups themselves are never deleted, only memberships are removed. This prevents accidental data loss
- **Manual groups preserved**: Only memberships with a row in `sso_group_memberships` are removed during sync. Groups the admin manually added the user to are never touched
- **Multiple providers**: Each provider tracks its own SSO-managed memberships via the `provider_id` key. Provider A won't touch Provider B's memberships

### 5. Transaction Safety

The reconciliation runs within the existing user provisioning transaction (for new users) or a new transaction (for existing users). Group creation uses `SAVEPOINT` so individual group failures don't roll back the entire login.

### 6. API & Frontend Changes

**SSO Provider CRUD** — Update JSON serialization for new fields in admin/org handlers.

**Frontend types** (`web/src/types/index.ts`):

```typescript
export interface SSOProvider {
  id: string;
  name: string;
  provider_type: string;
  client_id: string;
  discovery_url: string;
  allowed_domains: string[];
  enabled: boolean;
  scopes: string[];
  groups_claim: string;
  group_prefix: string;
  auto_sync_groups: boolean;
  get_user_info: boolean;
  created_at: string;
  updated_at: string;
}
```

**Admin UI** (`AdminPage.tsx`, `OrgSettingsPage.tsx`):

New form fields in the SSO provider create/edit dialog:
- **Scopes** — comma-separated text input, e.g. `groups, profile`
- **Groups claim** — text input, default `groups`
- **Group prefix** — text input, optional
- **Auto-sync groups** — toggle
- **Call UserInfo** — toggle

### 7. Error Handling & Audit Events

New audit event types:

| Event | When |
|---|---|
| `group.sso.create` | Auto-creating a group from an IDP group claim |
| `group.sso.add_member` | Adding user to a group via SSO sync |
| `group.sso.remove_member` | Removing user from a group via SSO sync |
| `group.sso.error` | Group reconciliation failure (non-fatal) |

Group reconciliation failures are logged and audited but never block the login flow.

## Testing Strategy

- **Unit test**: `Exchange` with mock OIDC provider, verify groups are parsed from ID token + UserInfo
- **Unit test**: Group reconciliation in isolation with mock DB
- **Integration test**: Full SSO login flow with group provisioning against test DB
  - New user with IDP groups → groups created, user added
  - Returning user with new IDP groups → user added to new groups
  - Returning user with removed IDP groups → user removed from SSO-managed groups
  - Manual memberships preserved during sync
  - Prefix filter behavior
  - Empty groups claim gracefully handled

## Migration Plan

1. SQL migration: new columns + new table
2. Go model updates: `Provider` struct, `OIDCClaims`, `GenericOIDCProvider`
3. OIDC Exchange: optional UserInfo call, flexible groups claim parsing
4. Group reconciliation logic: new helper function in `internal/api/`
5. Callback handler: wire in reconciliation
6. SSO provider CRUD: update serialization for new fields
7. Frontend: new form fields in SSO provider admin UI
8. Audit events: add new event types
