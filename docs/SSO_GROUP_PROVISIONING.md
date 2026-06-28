# SSO Group Provisioning

Auto-provision users into hnb groups based on group membership claims from an OIDC identity provider. Groups are created on demand, memberships are synced bidirectionally on each SSO login, and a prefix filter prevents syncing unrelated groups.

## How It Works

```
User clicks "Login with {provider}" 
  → hnb redirects to IDP
  → User authenticates
  → IDP redirects back with auth code
  → hnb exchanges code for ID token (+ optionally calls UserInfo)
  → hnb creates/finds user by email
  → hnb syncs group memberships:
      1. Filter groups by prefix (if configured)
      2. For each matching group: find-or-create in hnb, add user
      3. Remove user from groups the IDP no longer lists them in
  → Login complete, user redirected to frontend with token
```

## Provider Configuration

New fields on the SSO provider create/edit form:

| Field | Type | Default | Description |
|---|---|---|---|
| `scopes` | `string[]` | `[]` (defaults to `openid profile email`) | Additional OIDC scopes to request. E.g., `["groups"]` if the IDP requires an explicit scope. |
| `groups_claim` | `string` | `"groups"` | Which claim in the ID token / UserInfo response contains the group list. Use `"cognito:groups"` for AWS Cognito, `"memberOf"` for Azure AD, etc. |
| `group_prefix` | `string` | `""` | Only sync groups whose names start with this prefix. Empty = sync all. E.g., `"hnb-"` syncs `hnb-analysts` but skips `all-employees`. |
| `auto_sync_groups` | `boolean` | `false` | Master toggle to enable group provisioning for this provider. |
| `get_user_info` | `boolean` | `false` | Whether to call the UserInfo endpoint for additional claims after token exchange. Some IDPs include groups only in UserInfo, not in the ID token (or hit token size limits). |

## Database Schema

### `sso_providers` — new columns

```sql
scopes           text[]   NOT NULL DEFAULT '{}'
groups_claim     text     NOT NULL DEFAULT 'groups'
group_prefix     text     NOT NULL DEFAULT ''
auto_sync_groups bool     NOT NULL DEFAULT false
get_user_info    bool     NOT NULL DEFAULT false
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

## Reconciliation Logic

On each SSO login (both new and returning users):

1. **Filter**: Apply `group_prefix`. If empty, all groups pass through.
2. **Find or create**: For each matching group name, do a case-insensitive lookup in the user's org. If not found, create the group.
3. **Add**: Insert into `group_members` (`ON CONFLICT DO NOTHING`).
4. **Track**: Insert into `sso_group_memberships` (`ON CONFLICT DO NOTHING`).
5. **Remove stale**: Query `sso_group_memberships` for memberships tracked under this provider but whose group names aren't in the current IDP group list. Delete those memberships.

**Key behaviors:**
- Groups are never deleted — only memberships are removed
- Manual memberships (no corresponding `sso_group_memberships` row) are never touched
- Errors are non-fatal — the login succeeds even if sync fails, errors are audit-logged
- Each SSO provider tracks its own memberships independently via `provider_id`

## Admin Override

If an admin manually removes a user from a group that was provisioned via SSO, the **next login re-adds them** (because the IDP still lists them and the `sso_group_memberships` row still exists). SSO is authoritative for its provisioned memberships.

If an admin **adds** a user to a group that isn't in the IDP's list, the sync never removes them (because there's no `sso_group_memberships` row for that membership).

## Group Renames

If the IDP renames a group, the old hnb group persists with stale memberships and a new group is created. There is no rename tracking — the old group must be cleaned up manually. This is a known limitation.

## Development: Testing with Keycloak

The dev stack includes a pre-configured Keycloak instance (port 5557):

```bash
docker compose -f docker-compose.dev.yml up -d    # Keycloak starts automatically
```

### SSO provider configuration (Admin UI):

| Field | Value |
|---|---|
| Name | `Keycloak Dev` |
| Client ID | `hnb-dev` |
| Client Secret | `hnb-dev-keycloak-secret` |
| Discovery URL | `http://localhost:5557/realms/hnb-dev` |
| Scopes | *(leave empty)* |
| Groups Claim | `groups` |
| Group Prefix | `hnb-` |
| Auto-sync Groups | ✅ |
| Call UserInfo | ✅ |

### Test users:

| User | Password | Groups |
|---|---|---|
| alice@hnb-dev.test | alice123 | hnb-analysts, all-employees |
| bob@hnb-dev.test | bob123 | hnb-engineering |
| charlie@hnb-dev.test | charlie123 | all-employees |
| dave@hnb-dev.test | dave123 | hnb-engineering, all-employees |
| eve@hnb-dev.test | eve123 | hnb-analysts |

Login as `alice` — you'll be auto-added to `hnb-analysts` and `hnb-engineering`. The `all-employees` group is filtered by the `hnb-` prefix.

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
| `internal/api/oidc_handlers_test.go` | 13 | OIDC Exchange with groups, UserInfo fallback, full callback + group sync, edge cases (empty, case-insensitive, stale) |
| `internal/api/sso_group_sync_test.go` | 3 | Group creation, prefix filter, manual membership preservation |
| `internal/sso/sso_test.go` | 7 | Provider CRUD round-trip with new fields |

All tests hit a real PostgreSQL database (no mocks).

## Audit Events

New audit event types:

| Event | When |
|---|---|
| `group.sso.create` | Auto-creating a group from an IDP group claim |
| `group.sso.add_member` | Adding user to a group via SSO sync |
| `group.sso.remove_member` | Removing user from a group via SSO sync |
| `group.sso.error` | Group reconciliation failure (non-fatal) |

## Migration

Migration `073_sso_group_provisioning.sql` adds the new columns and table. Migrations run automatically on server startup.

## Cleaning Up

To remove all SSO-provisioned data:

```sql
DELETE FROM sso_group_memberships;
-- Groups and memberships remain intact; only SSO tracking is removed.
-- Next SSO login will re-provision all memberships fresh.
```
