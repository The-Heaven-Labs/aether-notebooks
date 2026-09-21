# SSO Group Provisioning

Auto-provision users into Aether groups based on group membership claims from an OIDC identity provider. Groups are created on demand, memberships are synced bidirectionally on each SSO login, and a prefix filter prevents syncing unrelated groups.

## How It Works

```
User clicks "Login with {provider}" 
  → Aether redirects to IDP
  → User authenticates
  → IDP redirects back with auth code
  → Aether exchanges code for ID token (+ optionally calls UserInfo)
  → Aether creates/finds user by email
  → Aether syncs group memberships:
       1. Filter groups by prefix (if configured), optionally stripping it from stored names
       2. For each matching group: find-or-create in Aether, add user
       3. Remove user from groups the IDP no longer lists them in
       4. Empty result: skip by default, or treat as authoritative when sync_empty_groups is on
  → Login complete, user redirected to frontend with token
```

## Provider Configuration

New fields on the SSO provider create/edit form:

| Field | Type | Default | Description |
|---|---|---|---|
| `scopes` | `string[]` | `[]` (defaults to `openid profile email`) | Additional OIDC scopes to request. E.g., `["groups"]` if the IDP requires an explicit scope. |
| `groups_claim` | `string` | `"groups"` | Which claim in the ID token / UserInfo response contains the group list. Use `"cognito:groups"` for AWS Cognito, `"memberOf"` for Azure AD, etc. |
| `group_prefix` | `string` | `""` | Only sync groups whose names start with this prefix. Empty = sync all. E.g., `"aether-"` syncs `aether-analysts` but skips `all-employees`. |
| `auto_sync_groups` | `boolean` | `false` | Master toggle to enable group provisioning for this provider. |
| `get_user_info` | `boolean` | `false` | Whether to call the UserInfo endpoint for additional claims after token exchange. Some IDPs include groups only in UserInfo, not in the ID token (or hit token size limits). |
| `sync_empty_groups` | `boolean` | `false` | When enabled (and `auto_sync_groups` is on), an absent/empty groups claim is authoritative: all memberships tracked under this provider for that user are removed. Warning: Keycloak omits the claim entirely when a user has zero groups, so a removed or misconfigured group mapper is indistinguishable from "no groups". |
| `strip_group_prefix` | `boolean` | `false` | When enabled (and `auto_sync_groups` is on) with `group_prefix` set, the prefix is removed from stored/displayed group names (`Aether Notebooks: Area` → `Area`). Filtering still uses the prefix. |

## Database Schema

### `sso_providers` — new columns

```sql
scopes             text[]   NOT NULL DEFAULT '{}'
groups_claim       text     NOT NULL DEFAULT 'groups'
group_prefix       text     NOT NULL DEFAULT ''
auto_sync_groups   bool     NOT NULL DEFAULT false
get_user_info      bool     NOT NULL DEFAULT false
sync_empty_groups  bool     NOT NULL DEFAULT false
strip_group_prefix bool     NOT NULL DEFAULT false
```

### `sso_group_memberships` — new table

Tracks which group memberships were provisioned via SSO. Enables clean reconciliation: only SSO-provisioned memberships are removed during sync; manually-added memberships are preserved.

```sql
provider_id UUID NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
group_id    UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
PRIMARY KEY (provider_id, group_id, user_id)
```

### `groups` — new column

```sql
display_name text
```

An admin-owned label rendered in the UI instead of `name`. It never participates in matching or permission resolution — see [Display Names](#display-names).

## Reconciliation Logic

On each SSO login (both new and returning users):

1. **Resolve**: Apply `group_prefix`. If set, groups whose names don't start with it are dropped. When `strip_group_prefix` is also on, the prefix is removed from the name that is looked up and stored (`Aether Notebooks: Area` → `Area`); filtering always uses the full prefixed name, and names that become empty are dropped.
2. **Find or create**: For each resolved group name, do a case-insensitive lookup in the user's org. If not found, create the group. New groups are created with no display name — see [Display Names](#display-names).
3. **Add**: Insert into `group_members` (`ON CONFLICT DO NOTHING`).
4. **Track**: Insert into `sso_group_memberships` (`ON CONFLICT DO NOTHING`).
5. **Remove stale**: Query `sso_group_memberships` for memberships tracked under this provider but whose group names aren't in the current resolved list. Delete those memberships.
6. **Empty result**: If the resolved list is empty and `sync_empty_groups` is `true`, the empty list is authoritative and step 5 removes every membership tracked under this provider for that user. With the default `false`, an empty list skips reconciliation entirely (steps 2–5 do not run).

**Key behaviors:**
- Groups are never deleted — only memberships are removed
- Manual memberships (no corresponding `sso_group_memberships` row) are never touched
- With `sync_empty_groups`, an IdP reporting zero groups removes all SSO-managed memberships for that user — manual memberships and group rows are preserved
- Errors are non-fatal — the login succeeds even if sync fails, errors are audit-logged
- Each SSO provider tracks its own memberships independently via `provider_id`

## Empty Claims and Failed Group Sources

"Zero groups" and "couldn't read the groups" are different states, and Aether keeps them apart:

- **Keycloak omission.** Keycloak omits the groups claim entirely when a user has no groups, so a deleted or misconfigured group mapper looks exactly like a user who genuinely belongs to zero groups. OIDC exposes no way to distinguish them, which is why `sync_empty_groups` is opt-in: enabling it declares the IdP authoritative.
- **Failed or malformed source.** When `get_user_info=true`, a failed UserInfo request — or a non-array groups claim (for example a string instead of an array) — marks the source unavailable. An array that contains no strings is not malformed: it is treated as an empty list. If the ID token carried no groups either, the claims get `GroupsUnavailable` and the callback **skips group sync entirely** instead of treating the empty list as zero groups. The skip is recorded as a `group.sso.error` audit event (with `provider_id`, `provider_name`, and `user_id`) and a `slog` warning; no memberships are removed.
- **ID token fallback.** If the ID token did carry groups, those are used even when UserInfo fails, so sync proceeds normally.
- **Successful empty is not unavailable.** A UserInfo response that succeeds and genuinely contains no groups is *not* marked unavailable, but it is only authoritative when the ID token carried no groups either: `Exchange` replaces the ID-token groups with UserInfo's only when UserInfo yields at least one group, so a populated ID token wins over an empty UserInfo. With `sync_empty_groups`, an empty list therefore removes SSO-managed memberships only when neither source produced groups (or the provider isn't using `get_user_info` and the ID token had none).

## Admin Override

If an admin manually removes a user from a group that was provisioned via SSO, the **next login re-adds them** (because the IDP still lists them and the `sso_group_memberships` row still exists). SSO is authoritative for its provisioned memberships.

If an admin **adds** a user to a group that isn't in the IDP's list, the sync never removes them (because there's no `sso_group_memberships` row for that membership).

## Group Renames

If the IDP renames a group, the old Aether group persists with stale memberships and a new group is created. There is no rename tracking — the old group must be cleaned up manually. This is a known limitation.

## Display Names

Group sync creates groups with no label — `display_name` is `NULL` — and never writes it, so repeated logins cannot overwrite an admin edit. Any group except `Everyone` can carry a label; `Everyone` is always rendered as "Everyone" and rejects one with a 400.

The label is presentation-only. The frontend renders `display_name?.trim() || name` everywhere through the shared `groupLabel` helper, while `name` remains the sync identity: matching, stale comparison, the `UNIQUE (org_id, name)` constraint, and audit resource names all use `name`. Permission resolution uses group IDs, with the `Everyone` special case matching on `name`, so `display_name` never participates in an access decision.

Because identity is still `name`, labels survive re-sync: an existing group row is reused and its label is left untouched. An IdP rename still creates a new, unlabeled group, leaving the old group (and its label) behind until cleaned up manually. Example: `aether-notebooks-data-analysts-infra` with `strip_group_prefix` stores `data-analysts-infra`, which an admin can label `Data Analysts Infra`.

`POST /groups` accepts an optional `display_name`. `PUT /groups/{id}` accepts `name` and/or `display_name`: an omitted field keeps its current value, and a blank (`""` or whitespace) label clears it back to `NULL`. Values are trimmed.

## Development: Testing with Keycloak

The dev stack includes a pre-configured Keycloak instance (port 5557):

```bash
docker compose -f docker-compose.dev.yml up -d    # Keycloak starts automatically
```

### SSO provider configuration (Admin UI):

| Field | Value |
|---|---|
| Name | `Keycloak Dev` |
| Client ID | `aether-dev` |
| Client Secret | `aether-dev-keycloak-secret` |
| Discovery URL | `http://localhost:5557/realms/aether-dev` |
| Scopes | *(leave empty)* |
| Groups Claim | `groups` |
| Group Prefix | `aether-` |
| Auto-sync Groups | ✅ |
| Call UserInfo | ✅ |

### Test users:

| User | Password | Groups |
|---|---|---|
| alice@aether-dev.test | alice123 | aether-analysts, all-employees |
| bob@aether-dev.test | bob123 | aether-engineering |
| charlie@aether-dev.test | charlie123 | all-employees |
| dave@aether-dev.test | dave123 | aether-engineering, all-employees |
| eve@aether-dev.test | eve123 | aether-analysts |

Login as `alice` — you'll be auto-added to `aether-analysts` and `aether-engineering`. The `all-employees` group is filtered by the `aether-` prefix.

### E2E test script:

```bash
bash scripts/sso-e2e-test.sh
```

Runs the full flow (no browser needed): creates provider, logs in at Keycloak via curl, completes callback, verifies user creation.

## Docker Networking

In the dev Docker environment, the API server (inside a container) connects to Keycloak via `host.docker.internal:5557` (using the `extra_hosts` mapping in `docker-compose.dev.yml`), while the browser reaches Keycloak at `localhost:5557`. The `oidcHTTPClient` function rewrites the connection target for `localhost:5557` → `host.docker.internal:5557` while preserving the original `Host` header, so Keycloak issues tokens with the correct issuer (`localhost:5557`).

For production OIDC providers using a real URL, the custom transport is not applied — `http.DefaultClient` is used instead.

## Test Coverage

| Test file | Tests | What it covers |
|---|---|---|
| `internal/api/oidc_handlers_test.go` | 20 | OIDC exchange with groups, UserInfo fallback and unavailable-source guard, full callback + group sync (empty-authoritative, skip-on-unavailable), edge cases (empty, case-insensitive, stale) |
| `internal/api/sso_group_sync_test.go` | 7 | Group creation, prefix filter/stripping, empty-claim removal, display-name preservation, manual membership preservation, audit events |
| `internal/sso/sso_test.go` | 10 | Provider CRUD round-trip with new fields |

All tests hit a real PostgreSQL database (no mocks).

## Audit Events

Emitted during SSO group provisioning:

| Event | When |
|---|---|
| `group.sso.create` | Auto-creating a group from an IDP group claim |
| `group.sso.add_member` | Adding a user to a group via SSO sync (only when the membership is newly inserted) |
| `group.sso.remove_member` | Removing a user from a group via SSO sync (only when a tracked membership is actually deleted) |
| `group.sso.error` | Group reconciliation failure (non-fatal), including a skipped sync when the groups source is unavailable |

## Migration

Migration `073_sso_group_provisioning.sql` adds the initial columns and table, `V115__sso_group_sync_options.sql` adds `sync_empty_groups` and `strip_group_prefix`, and `V116__group_display_names.sql` adds `groups.display_name`. Migrations run automatically on server startup.

## Cleaning Up

To remove all SSO-provisioned data:

```sql
DELETE FROM sso_group_memberships;
-- Groups and memberships remain intact; only SSO tracking is removed.
-- Next SSO login will re-provision all memberships fresh.
```
