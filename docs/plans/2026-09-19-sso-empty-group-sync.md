# SSO Empty Group Sync + Prefix Stripping + Group Display Names Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let an OIDC provider authoritatively sync zero groups (removing stale SSO-managed memberships), store group names without the IdP namespace prefix, and let admins attach a human-friendly display label to any group.

**Architecture:** Two opt-in boolean columns on `sso_providers` (`sync_empty_groups`, `strip_group_prefix`). `SyncSSOGroups` resolves IdP names (filter + optional strip), and when the resolved set is empty with `sync_empty_groups` on, it proceeds to stale-removal instead of returning early. `OIDCClaims.GroupsUnavailable` guards against wiping memberships when the configured UserInfo source fails. A nullable `groups.display_name` carries an admin-owned label rendered by the UI, never used for matching. All flags/columns default to preserving existing behavior (`false` / `NULL`).

**Tech Stack:** Go 1.x (net/http, pgx/v5), PostgreSQL migrations (Flyway naming), React + TypeScript frontend, swag for Swagger.

**Design doc:** `docs/plans/2026-09-19-sso-empty-group-sync-design.md`

---

## Before You Start

The repo workflow requires a feature branch from `main` (never commit to `main`). The current working tree may have unrelated changes — check `git status` and stash/move them if needed.

```bash
git fetch origin
git checkout -b feat/sso-empty-group-sync origin/main
```

Migration numbers in this plan (V115, V116) assume `V114` is the highest shipped
file. Check before starting and renumber to the next free values if later
migrations have landed:

```bash
ls internal/database/migrations/ | sort -V | tail -3
```

Start test infrastructure once:

```bash
task infra:up
```

All Go tests hit a real PostgreSQL database (`postgres://aether:aether_dev@localhost:5432/aether` by default).

---

### Task 1: Migration V115 + Provider model/CRUD

**Files:**
- Create: `internal/database/migrations/V115__sso_group_sync_options.sql`
- Modify: `internal/sso/sso.go`
- Test: `internal/sso/sso_test.go` (extend `TestCreateProviderWithGroupSettings`, around line 201)

**Step 1: Write the failing test**

In `internal/sso/sso_test.go`, extend the input and assertions in `TestCreateProviderWithGroupSettings`:

```go
	input := sso.Provider{
		Scope:            "platform",
		Name:             "groups-test",
		ProviderType:     "oidc",
		ClientID:         "groups-client",
		ClientSecret:     "groups-secret",
		DiscoveryURL:     "https://accounts.example.com/.well-known/openid-configuration",
		AllowedDomains:   []string{},
		Enabled:          true,
		Scopes:           []string{"openid", "profile", "email", "groups"},
		GroupsClaim:      "custom_groups",
		GroupPrefix:      "aether-",
		AutoSyncGroups:   true,
		GetUserInfo:      true,
		SyncEmptyGroups:  true,
		StripGroupPrefix: true,
	}
```

Add assertions after the existing ones (both `created` and `got` blocks):

```go
	assert.True(t, created.SyncEmptyGroups)
	assert.True(t, created.StripGroupPrefix)
	// ...existing got assertions...
	assert.True(t, got.SyncEmptyGroups)
	assert.True(t, got.StripGroupPrefix)
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/sso/ -run TestCreateProviderWithGroupSettings -v`
Expected: FAIL — `unknown field 'SyncEmptyGroups' in struct literal` (compile error).

**Step 3: Create the migration**

`internal/database/migrations/V115__sso_group_sync_options.sql`:

```sql
ALTER TABLE sso_providers
  ADD COLUMN sync_empty_groups  bool NOT NULL DEFAULT false,
  ADD COLUMN strip_group_prefix bool NOT NULL DEFAULT false;
```

**Step 4: Update `internal/sso/sso.go`**

Add fields to `Provider` after `GetUserInfo`:

```go
	SyncEmptyGroups  bool     `json:"sync_empty_groups"`
	StripGroupPrefix bool     `json:"strip_group_prefix"`
```

Update `scanProvider` and `collectProviders` scan lists (both) to:

```go
		&p.ID, &p.Scope, &p.OrgID, &p.Name, &p.ProviderType,
		&p.ClientID, &encSecret, &p.DiscoveryURL,
		&p.AllowedDomains, &p.Enabled, &p.Scopes, &p.GroupsClaim, &p.GroupPrefix, &p.AutoSyncGroups, &p.GetUserInfo,
		&p.SyncEmptyGroups, &p.StripGroupPrefix,
		&p.ProvisioningMode, &p.DefaultRole,
		&p.CreatedAt, &p.UpdatedAt,
```

Update `selectProviderCols`:

```go
const selectProviderCols = `id, scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, scopes, groups_claim, group_prefix, auto_sync_groups, get_user_info, sync_empty_groups, strip_group_prefix, provisioning_mode, default_role, created_at, updated_at`
```

Update `CreateProvider` SQL (16 → 18 params):

```go
	row := pool.QueryRow(ctx,
		`INSERT INTO sso_providers (scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, scopes, groups_claim, group_prefix, auto_sync_groups, get_user_info, sync_empty_groups, strip_group_prefix, provisioning_mode, default_role)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		 RETURNING `+selectProviderCols,
		p.Scope, p.OrgID, p.Name, p.ProviderType, p.ClientID, encSecret, p.DiscoveryURL, p.AllowedDomains, p.Enabled,
		p.Scopes, p.GroupsClaim, p.GroupPrefix, p.AutoSyncGroups, p.GetUserInfo, p.SyncEmptyGroups, p.StripGroupPrefix,
		p.ProvisioningMode, p.DefaultRole,
	)
```

Update `UpdateProvider` SQL (14 → 16 params):

```go
	row := pool.QueryRow(ctx,
		`UPDATE sso_providers
		 SET name=$1, client_id=$2, client_secret_enc=$3, discovery_url=$4, allowed_domains=$5, enabled=$6,
		     scopes=$7, groups_claim=$8, group_prefix=$9, auto_sync_groups=$10, get_user_info=$11,
		     sync_empty_groups=$12, strip_group_prefix=$13,
		     provisioning_mode=$14, default_role=$15,
		     updated_at=now()
		 WHERE id=$16
		 RETURNING `+selectProviderCols,
		p.Name, p.ClientID, encSecret, p.DiscoveryURL, p.AllowedDomains, p.Enabled,
		p.Scopes, p.GroupsClaim, p.GroupPrefix, p.AutoSyncGroups, p.GetUserInfo, p.SyncEmptyGroups, p.StripGroupPrefix,
		p.ProvisioningMode, p.DefaultRole, p.ID,
	)
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/sso/ -v`
Expected: PASS (all tests in the package).

**Step 6: Commit**

```bash
git add internal/database/migrations/V115__sso_group_sync_options.sql internal/sso/sso.go internal/sso/sso_test.go
git commit -m "feat(sso): add sync_empty_groups and strip_group_prefix provider options"
```

---

### Task 2: API request/response fields + org handlers

**Files:**
- Modify: `internal/api/sso_admin_handlers.go`
- Modify: `internal/api/sso_org_handlers.go` (two `sso.Provider` literals: create ~line 102, update ~line 188)
- Test: `internal/api/sso_admin_handlers_test.go` (extend `TestPlatformAdminSSOProviderCRUD`)

**Step 1: Write the failing test**

In `TestPlatformAdminSSOProviderCRUD`, add to `createBody`:

```go
		"auto_sync_groups":   true,
		"get_user_info":      true,
		"sync_empty_groups":  true,
		"strip_group_prefix": true,
```

Add assertions after the existing `created` assertions:

```go
	assert.True(t, created["auto_sync_groups"].(bool))
	assert.True(t, created["get_user_info"].(bool))
	assert.True(t, created["sync_empty_groups"].(bool))
	assert.True(t, created["strip_group_prefix"].(bool))
```

Also add `"sync_empty_groups": true, "strip_group_prefix": true` to `updateBody` and assert them on `updated`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestPlatformAdminSSOProviderCRUD -v`
Expected: FAIL — `created["sync_empty_groups"]` is nil / assertion failure.

**Step 3: Update `internal/api/sso_admin_handlers.go`**

Add to `providerResponse` after `GetUserInfo`:

```go
	SyncEmptyGroups  bool      `json:"sync_empty_groups"`
	StripGroupPrefix bool      `json:"strip_group_prefix"`
```

Add to `providerToResponse` after `GetUserInfo: p.GetUserInfo,`:

```go
		SyncEmptyGroups:  p.SyncEmptyGroups,
		StripGroupPrefix: p.StripGroupPrefix,
```

Add to `ssoProviderRequest` after `GetUserInfo`:

```go
	SyncEmptyGroups  bool     `json:"sync_empty_groups"`
	StripGroupPrefix bool     `json:"strip_group_prefix"`
```

Add to the `sso.Provider` literals in `handleAdminCreateSSOProvider` and `handleAdminUpdateSSOProvider` after `GetUserInfo: req.GetUserInfo,`:

```go
		SyncEmptyGroups:  req.SyncEmptyGroups,
		StripGroupPrefix: req.StripGroupPrefix,
```

**Step 4: Update `internal/api/sso_org_handlers.go`**

In both `handleOrgCreateSSOProvider` and `handleOrgUpdateSSOProvider`, add to the `sso.Provider` literal after `GetUserInfo: req.GetUserInfo,`:

```go
		SyncEmptyGroups:  req.SyncEmptyGroups,
		StripGroupPrefix: req.StripGroupPrefix,
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestPlatformAdminSSOProviderCRUD|TestOrgSSOProvider' -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add internal/api/sso_admin_handlers.go internal/api/sso_org_handlers.go internal/api/sso_admin_handlers_test.go
git commit -m "feat(api): expose sync_empty_groups and strip_group_prefix on SSO providers"
```

---

### Task 3: `FindOrCreateGroup` returns `created`

**Files:**
- Modify: `internal/api/sso_group_sync.go`
- Modify: `internal/api/oidc_handlers_test.go` (call sites at ~lines 265, 297, 322)
- Test: `internal/api/oidc_handlers_test.go` (`TestFindOrCreateGroup_Existing`, `TestFindOrCreateGroup_CaseInsensitive`)

**Step 1: Update tests to the new signature (failing)**

In `TestFindOrCreateGroup_Existing`:

```go
	foundID, created, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "my-group")
	require.NoError(t, err)
	assert.Equal(t, existingID, foundID, "should return existing group ID")
	assert.False(t, created, "existing group should report created=false")
```

In `TestFindOrCreateGroup_CaseInsensitive`:

```go
	foundID, created, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "mygroup")
	require.NoError(t, err)
	assert.Equal(t, existingID, foundID, "should find existing group case-insensitively")
	assert.False(t, created)
```

In `TestSyncSSOGroups_CaseInsensitiveMatching`:

```go
	foundID, created, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "engineering")
	require.NoError(t, err)
	assert.Equal(t, existingID, foundID, "should find existing group case-insensitively")
	assert.False(t, created)
```

Add a new test that a fresh name reports `created=true`:

```go
func TestFindOrCreateGroup_Creates(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	id, created, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "brand-new")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.True(t, created, "new group should report created=true")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestFindOrCreateGroup' -v`
Expected: FAIL — `assignment mismatch: 2 variables but api.FindOrCreateGroup returns 3 values` (once implemented) / compile error before that.

**Step 3: Change the signature in `internal/api/sso_group_sync.go`**

```go
func FindOrCreateGroup(ctx context.Context, pool *pgxpool.Pool, orgID, name string) (string, bool, error) {
	var id string
	err := pool.QueryRow(ctx,
		`SELECT id FROM groups WHERE org_id=$1 AND LOWER(name)=LOWER($2)`,
		orgID, name,
	).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if err != pgx.ErrNoRows {
		return "", false, fmt.Errorf("lookup group: %w", err)
	}

	err = pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`,
		orgID, name,
	).Scan(&id)
	if err != nil {
		return "", false, fmt.Errorf("create group: %w", err)
	}

	return id, true, nil
}
```

Update the call inside `SyncSSOGroups` to `groupID, _, err := FindOrCreateGroup(...)` (temporarily; Task 4 uses the flag for auditing).

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestFindOrCreateGroup|TestSyncSSOGroups_CaseInsensitiveMatching' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/sso_group_sync.go internal/api/oidc_handlers_test.go
git commit -m "refactor(api): report whether FindOrCreateGroup created a group"
```

---

### Task 4: Sync semantics — strip prefix, authoritative empty, case-insensitive stale, audit events

**Files:**
- Modify: `internal/api/sso_group_sync.go`
- Test: `internal/api/sso_group_sync_test.go`
- Test: `internal/api/oidc_handlers_test.go` (`TestSyncSSOGroups_EmptyGroups` rework)

**Step 1: Write the failing tests**

Rework `TestSyncSSOGroups_EmptyGroups` in `internal/api/oidc_handlers_test.go`:

```go
func TestSyncSSOGroups_EmptyGroups(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	var userID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("empty-%d@test.com", time.Now().UnixNano()), "Empty",
	).Scan(&userID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, userID,
	)
	require.NoError(t, err)

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, testMasterKey, sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           "empty-test",
		ProviderType:   "oidc",
		ClientID:       "test-client",
		ClientSecret:   "test-secret",
		DiscoveryURL:   "https://example.com/",
		AllowedDomains: []string{},
		Scopes:         []string{},
		Enabled:        true,
		AutoSyncGroups: true,
	})
	require.NoError(t, err)

	logger := audit.NewLogger(s.DB())

	// Seed an SSO-managed membership.
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"engineering"})

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID,
	).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Flag off (default): empty groups is a no-op.
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, nil)
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "empty groups should be a no-op when sync_empty_groups is off")

	// Flag on: empty groups removes all SSO-managed memberships.
	provider.SyncEmptyGroups = true
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, nil)

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "empty groups should remove SSO-managed memberships")

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sso_group_memberships WHERE provider_id=$1 AND user_id=$2`,
		provider.ID, userID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "tracking rows should be removed")

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM groups WHERE org_id=$1`, orgID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "groups are never deleted")
}
```

Add to `internal/api/sso_group_sync_test.go`:

```go
func TestSyncSSOGroups_StripGroupPrefix(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	var userID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("strip-%d@test.com", time.Now().UnixNano()), "Strip",
	).Scan(&userID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, userID,
	)
	require.NoError(t, err)

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, testMasterKey, sso.Provider{
		Scope:            "org",
		OrgID:            &orgID,
		Name:             "strip-test",
		ProviderType:     "oidc",
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		DiscoveryURL:     "https://example.com/",
		AllowedDomains:   []string{},
		Scopes:           []string{},
		Enabled:          true,
		AutoSyncGroups:   true,
		GroupPrefix:      "Aether Notebooks: ",
		StripGroupPrefix: true,
	})
	require.NoError(t, err)

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{
		"Aether Notebooks: Area Name",
		"Aether Notebooks: Engineering",
		"all-employees",
		"Aether Notebooks: ",
	})

	rows, err := s.DB().Pool.Query(ctx,
		`SELECT g.name FROM group_members gm
		 JOIN groups g ON g.id = gm.group_id
		 WHERE gm.user_id=$1 ORDER BY g.name`,
		userID,
	)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	assert.Equal(t, []string{"Area Name", "Engineering"}, names,
		"prefix should be stripped, unrelated and empty names dropped")
}

func TestSyncSSOGroups_StaleCaseInsensitive(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	var userID string
	err = s.DB().Pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("case-%d@test.com", time.Now().UnixNano()), "Case",
	).Scan(&userID)
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, userID,
	)
	require.NoError(t, err)

	provider, err := sso.CreateProvider(ctx, s.DB().Pool, testMasterKey, sso.Provider{
		Scope:          "org",
		OrgID:          &orgID,
		Name:           "case-test",
		ProviderType:   "oidc",
		ClientID:       "test-client",
		ClientSecret:   "test-secret",
		DiscoveryURL:   "https://example.com/",
		AllowedDomains: []string{},
		Scopes:         []string{},
		Enabled:        true,
		AutoSyncGroups: true,
	})
	require.NoError(t, err)

	logger := audit.NewLogger(s.DB())

	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"Engineering"})

	// Different casing must not churn the membership.
	api.SyncSSOGroups(ctx, s.DB().Pool, logger, provider, orgID, userID, []string{"engineering"})

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "casing change should not remove the membership")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestSyncSSOGroups_(EmptyGroups|StripGroupPrefix|StaleCaseInsensitive)' -v`
Expected: FAIL — empty-flag-on keeps membership, stripping stores prefixed names, case churn removes membership.

**Step 3: Implement in `internal/api/sso_group_sync.go`**

Replace the whole file with:

```go
package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/sso"
)

// resolveSSOGroupNames filters IdP group names by the provider prefix and
// optionally strips the prefix. Names that become empty are dropped.
func resolveSSOGroupNames(provider sso.Provider, idpGroups []string) []string {
	var resolved []string
	for _, g := range idpGroups {
		if provider.GroupPrefix != "" && !strings.HasPrefix(g, provider.GroupPrefix) {
			continue
		}
		name := g
		if provider.StripGroupPrefix {
			name = strings.TrimSpace(strings.TrimPrefix(g, provider.GroupPrefix))
		}
		if name == "" {
			continue
		}
		resolved = append(resolved, name)
	}
	return resolved
}

func logGroupSyncError(ctx context.Context, logger *audit.Logger, orgID, userID, groupID, groupName string, err error) {
	if logger == nil {
		return
	}
	logger.Log(ctx, audit.Entry{
		OrgID:        orgID,
		Action:       "group.sso.error",
		ResourceType: "group",
		ResourceID:   groupID,
		ResourceName: groupName,
		Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
	})
}

func logGroupSyncEvent(ctx context.Context, logger *audit.Logger, action, orgID, userID, groupID, groupName string) {
	if logger == nil {
		return
	}
	logger.Log(ctx, audit.Entry{
		OrgID:        orgID,
		Action:       action,
		ResourceType: "group",
		ResourceID:   groupID,
		ResourceName: groupName,
		Metadata:     map[string]any{"user_id": userID},
	})
}

// SyncSSOGroups reconciles the user's Aether group memberships against the
// IdP-reported groups. When provider.SyncEmptyGroups is true, an empty IdP
// group list is authoritative and removes all SSO-managed memberships.
func SyncSSOGroups(ctx context.Context, pool *pgxpool.Pool, logger *audit.Logger, provider sso.Provider, orgID, userID string, idpGroups []string) {
	resolved := resolveSSOGroupNames(provider, idpGroups)
	if len(resolved) == 0 && !provider.SyncEmptyGroups {
		return
	}

	for _, groupName := range resolved {
		groupID, created, err := FindOrCreateGroup(ctx, pool, orgID, groupName)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, "", groupName, err)
			continue
		}
		if created {
			logGroupSyncEvent(ctx, logger, "group.sso.create", orgID, userID, groupID, groupName)
		}

		tag, err := pool.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			groupID, userID,
		)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, groupID, groupName, err)
			continue
		}
		if tag.RowsAffected() > 0 {
			logGroupSyncEvent(ctx, logger, "group.sso.add_member", orgID, userID, groupID, groupName)
		}

		_, err = pool.Exec(ctx,
			`INSERT INTO sso_group_memberships (provider_id, group_id, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			provider.ID, groupID, userID,
		)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, groupID, groupName, err)
			continue
		}
	}

	staleGroups, err := FindStaleSSOGroups(ctx, pool, provider.ID, userID, resolved)
	if err != nil {
		logGroupSyncError(ctx, logger, orgID, userID, "", "", err)
		return
	}

	for _, groupID := range staleGroups {
		_, err := pool.Exec(ctx,
			`DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`,
			groupID, userID,
		)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, groupID, "", err)
			continue
		}

		_, err = pool.Exec(ctx,
			`DELETE FROM sso_group_memberships WHERE provider_id=$1 AND group_id=$2 AND user_id=$3`,
			provider.ID, groupID, userID,
		)
		if err != nil {
			logGroupSyncError(ctx, logger, orgID, userID, groupID, "", err)
			continue
		}

		logGroupSyncEvent(ctx, logger, "group.sso.remove_member", orgID, userID, groupID, "")
	}
}

func FindOrCreateGroup(ctx context.Context, pool *pgxpool.Pool, orgID, name string) (string, bool, error) {
	var id string
	err := pool.QueryRow(ctx,
		`SELECT id FROM groups WHERE org_id=$1 AND LOWER(name)=LOWER($2)`,
		orgID, name,
	).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if err != pgx.ErrNoRows {
		return "", false, fmt.Errorf("lookup group: %w", err)
	}

	err = pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`,
		orgID, name,
	).Scan(&id)
	if err != nil {
		return "", false, fmt.Errorf("create group: %w", err)
	}

	return id, true, nil
}

func FindStaleSSOGroups(ctx context.Context, pool *pgxpool.Pool, providerID, userID string, currentGroups []string) ([]string, error) {
	lowered := make([]string, len(currentGroups))
	for i, g := range currentGroups {
		lowered[i] = strings.ToLower(g)
	}

	rows, err := pool.Query(ctx,
		`SELECT sgm.group_id
		 FROM sso_group_memberships sgm
		 JOIN groups g ON g.id = sgm.group_id
		 WHERE sgm.provider_id = $1 AND sgm.user_id = $2
		 AND LOWER(g.name) != ALL($3)`,
		providerID, userID, lowered,
	)
	if err != nil {
		return nil, fmt.Errorf("find stale groups: %w", err)
	}
	defer rows.Close()

	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale group: %w", err)
		}
		stale = append(stale, id)
	}
	return stale, rows.Err()
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestSyncSSOGroups|TestFindOrCreateGroup|TestFindStaleSSOGroups' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/sso_group_sync.go internal/api/sso_group_sync_test.go internal/api/oidc_handlers_test.go
git commit -m "feat(sso): authoritative empty group sync, prefix stripping, audit events"
```

---

### Task 5: `OIDCClaims.GroupsUnavailable` guard

**Files:**
- Modify: `internal/auth/oidc.go`
- Test: `internal/api/oidc_handlers_test.go` (test server + new tests)

**Step 1: Write the failing tests**

Add a failure switch to the test server (`internal/api/oidc_handlers_test.go`):

```go
type testOIDCServer struct {
	// ...existing fields...
	failUserInfo bool
}
```

At the top of `handleUserInfo`:

```go
	if s.failUserInfo {
		http.Error(w, "userinfo unavailable", http.StatusInternalServerError)
		return
	}
```

Add tests:

```go
func TestOIDCExchangeUserInfoFailure(t *testing.T) {
	email := fmt.Sprintf("uifail-%d@test.com", time.Now().UnixNano())
	srv := newTestOIDCServer(t, "user-uifail", email, "UI Fail", nil, false)
	srv.failUserInfo = true

	provider, err := auth.NewGenericOIDCProvider(context.Background(),
		"test", srv.baseURL, "test-client-id", "test-client-secret",
		"http://localhost/callback",
		[]string{"openid", "profile", "email", "groups"},
		"groups", true)
	require.NoError(t, err)

	claims, err := provider.Exchange(context.Background(), "test-code")
	require.NoError(t, err)

	assert.True(t, claims.GroupsUnavailable,
		"failed UserInfo with no ID token groups should mark groups unavailable")
	assert.Empty(t, claims.Groups)
}

func TestOIDCExchangeUserInfoFailureWithIDTokenGroups(t *testing.T) {
	email := fmt.Sprintf("uifail2-%d@test.com", time.Now().UnixNano())
	srv := newTestOIDCServer(t, "user-uifail2", email, "UI Fail 2",
		[]string{"aether-x"}, true)
	srv.failUserInfo = true

	provider, err := auth.NewGenericOIDCProvider(context.Background(),
		"test", srv.baseURL, "test-client-id", "test-client-secret",
		"http://localhost/callback",
		[]string{"openid", "profile", "email", "groups"},
		"groups", true)
	require.NoError(t, err)

	claims, err := provider.Exchange(context.Background(), "test-code")
	require.NoError(t, err)

	assert.False(t, claims.GroupsUnavailable,
		"ID token groups should be used when UserInfo fails")
	assert.Equal(t, []string{"aether-x"}, claims.Groups)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestOIDCExchangeUserInfoFailure' -v`
Expected: FAIL — `claims.GroupsUnavailable` undefined (compile error).

**Step 3: Implement in `internal/auth/oidc.go`**

Add to `OIDCClaims`:

```go
type OIDCClaims struct {
	Subject           string
	Email             string
	Name              string
	Groups            []string
	GroupsUnavailable bool // true when the configured groups source (UserInfo) failed and no fallback groups exist
}
```

Replace the `getUserInfo` block in `Exchange`:

```go
	if p.getUserInfo && p.oidcProvider != nil {
		userInfoOK := false
		userInfo, err := p.oidcProvider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err == nil {
			var uiClaims map[string]any
			if err := userInfo.Claims(&uiClaims); err == nil {
				userInfoOK = true
				if groupsRaw, ok := uiClaims[p.groupsClaim]; ok {
					if groupsArr, ok := groupsRaw.([]any); ok {
						var uiGroups []string
						for _, g := range groupsArr {
							if s, ok := g.(string); ok {
								uiGroups = append(uiGroups, s)
							}
						}
						if len(uiGroups) > 0 {
							claims.Groups = uiGroups
						}
					}
				}
			}
		}
		if !userInfoOK && len(claims.Groups) == 0 {
			claims.GroupsUnavailable = true
		}
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestOIDCExchange' -v`
Expected: PASS (all Exchange tests, including existing ones).

**Step 5: Commit**

```bash
git add internal/auth/oidc.go internal/api/oidc_handlers_test.go
git commit -m "feat(auth): flag unavailable groups source when UserInfo fails"
```

---

### Task 6: Callback wiring + integration tests

**Files:**
- Modify: `internal/api/oidc_handlers.go:522-525`
- Test: `internal/api/oidc_handlers_test.go` (two new integration tests)

**Step 1: Write the failing tests**

Add to `internal/api/oidc_handlers_test.go`:

```go
func TestFullOIDCCallbackEmptyGroupsAuthoritative(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("emptyfull-%d@example.com", ts)

	oidcSrv := newTestOIDCServer(t, "empty-full-user", email, "Empty Full",
		[]string{"aether-analysts"}, true)

	dbProvider := sso.Provider{
		Scope:           "platform",
		Name:            "Empty Full OIDC",
		ProviderType:    "oidc",
		ClientID:        "test-client-id",
		ClientSecret:    "test-secret",
		DiscoveryURL:    oidcSrv.baseURL,
		AllowedDomains:  []string{},
		Scopes:          []string{"openid", "profile", "email", "groups"},
		Enabled:         true,
		AutoSyncGroups:  true,
		SyncEmptyGroups: true,
		GroupsClaim:     "groups",
		GroupPrefix:     "aether-",
	}
	created, err := sso.CreateProvider(ctx, s.DB().Pool, s.MasterKey(), dbProvider)
	require.NoError(t, err)

	callback := func() {
		state := fmt.Sprintf("empty-state-%d", time.Now().UnixNano())
		_, err := s.Cache.Client().SetNX(ctx, "oidc:state:"+state, "1", 10*time.Minute).Result()
		require.NoError(t, err)
		req := httptest.NewRequest("GET",
			fmt.Sprintf("/api/v1/auth/oidc/%s/callback?code=test-code&state=%s", created.ID, state), nil)
		req.Host = "localhost"
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		require.Equal(t, http.StatusFound, rec.Code, "callback body: %s", rec.Body.String())
	}

	callback()

	var userID string
	err = s.DB().Pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&userID)
	require.NoError(t, err)

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count, "first login should sync one group")

	// IdP now reports no groups at all (Keycloak omits the claim).
	oidcSrv.groups = nil
	callback()

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "empty groups should remove SSO-managed memberships")

	var tracked int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM sso_group_memberships WHERE user_id=$1`, userID).Scan(&tracked)
	require.NoError(t, err)
	assert.Equal(t, 0, tracked, "tracking rows should be removed")
}

func TestFullOIDCCallbackUserInfoFailureSkipsGroupSync(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	ts := time.Now().UnixNano()
	email := fmt.Sprintf("skipfull-%d@example.com", ts)

	oidcSrv := newTestOIDCServer(t, "skip-full-user", email, "Skip Full",
		[]string{"aether-analysts"}, false)

	dbProvider := sso.Provider{
		Scope:           "platform",
		Name:            "Skip Full OIDC",
		ProviderType:    "oidc",
		ClientID:        "test-client-id",
		ClientSecret:    "test-secret",
		DiscoveryURL:    oidcSrv.baseURL,
		AllowedDomains:  []string{},
		Scopes:          []string{"openid", "profile", "email", "groups"},
		Enabled:         true,
		AutoSyncGroups:  true,
		SyncEmptyGroups: true,
		GroupsClaim:     "groups",
		GroupPrefix:     "aether-",
		GetUserInfo:     true,
	}
	created, err := sso.CreateProvider(ctx, s.DB().Pool, s.MasterKey(), dbProvider)
	require.NoError(t, err)

	callback := func() {
		state := fmt.Sprintf("skip-state-%d", time.Now().UnixNano())
		_, err := s.Cache.Client().SetNX(ctx, "oidc:state:"+state, "1", 10*time.Minute).Result()
		require.NoError(t, err)
		req := httptest.NewRequest("GET",
			fmt.Sprintf("/api/v1/auth/oidc/%s/callback?code=test-code&state=%s", created.ID, state), nil)
		req.Host = "localhost"
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		require.Equal(t, http.StatusFound, rec.Code, "callback body: %s", rec.Body.String())
	}

	callback() // groups only in UserInfo → membership created

	var userID string
	err = s.DB().Pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, email).Scan(&userID)
	require.NoError(t, err)

	var count int
	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// UserInfo breaks and ID token has no groups → sync must be skipped, not wipe.
	oidcSrv.failUserInfo = true
	callback()

	err = s.DB().Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM group_members WHERE user_id=$1`, userID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "UserInfo failure must not wipe memberships")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestFullOIDCCallback(EmptyGroupsAuthoritative|UserInfoFailureSkipsGroupSync)' -v`
Expected: FAIL — empty callback leaves the membership; skip callback wipes it (because the gate ignores `GroupsUnavailable`).

**Step 3: Implement in `internal/api/oidc_handlers.go`**

Replace lines 522-525:

```go
	// Reconcile group membership via SSO
	if dbProvider.AutoSyncGroups {
		if claims.GroupsUnavailable {
			s.audit.Log(ctx, audit.Entry{
				OrgID: orgID, UserID: userID,
				Action: "group.sso.error", ResourceType: "group",
				Metadata: map[string]any{
					"error": "groups source unavailable (UserInfo failed); skipping group sync",
				},
			})
		} else {
			SyncSSOGroups(ctx, s.db.Pool, s.audit, dbProvider, orgID, userID, claims.Groups)
		}
	}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestFullOIDCCallback|TestSyncSSOGroups|TestOIDCExchange' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/api/oidc_handlers.go internal/api/oidc_handlers_test.go
git commit -m "feat(sso): reconcile empty group claims and skip sync on UserInfo failure"
```

---

### Task 7: Frontend types + provider forms

**Files:**
- Modify: `web/src/types/index.ts` (~line 298)
- Modify: `web/src/pages/AdminPage.tsx`
- Modify: `web/src/pages/OrgSettingsPage.tsx`

**Step 1: Update the shared type**

In `web/src/types/index.ts`, add to `SSOProvider` after `get_user_info`:

```ts
  sync_empty_groups: boolean
  strip_group_prefix: boolean
```

**Step 2: Update `web/src/pages/AdminPage.tsx`**

Add to `ProviderFormValues` after `get_user_info`:

```ts
  sync_empty_groups: boolean
  strip_group_prefix: boolean
```

Add to `emptyForm` after `get_user_info: false,`:

```ts
  sync_empty_groups: false,
  strip_group_prefix: false,
```

Add to `providerToForm` after `get_user_info: p.get_user_info ?? false,`:

```ts
    sync_empty_groups: p.sync_empty_groups ?? false,
    strip_group_prefix: p.strip_group_prefix ?? false,
```

Update the boolean field handler in `set`:

```ts
      setValues(v => ({ ...v, [field]: field === 'enabled' || field === 'auto_sync_groups' || field === 'get_user_info' || field === 'sync_empty_groups' || field === 'strip_group_prefix' ? (e.target as HTMLInputElement).checked : e.target.value }))
```

Add checkboxes after the "Call UserInfo Endpoint" label:

```tsx
        <label style={{ ...formStyles.label, flexDirection: 'row', alignItems: 'center', gap: 8 }}>
          <input type="checkbox" checked={values.sync_empty_groups} onChange={set('sync_empty_groups')} />
          Sync Empty Groups
        </label>
        <label style={{ ...formStyles.label, flexDirection: 'row', alignItems: 'center', gap: 8 }}>
          <input type="checkbox" checked={values.strip_group_prefix} onChange={set('strip_group_prefix')} />
          Strip Group Prefix
        </label>
```

Add to both `handleCreate` and `handleUpdate` bodies after `get_user_info: values.get_user_info,`:

```ts
      sync_empty_groups: values.sync_empty_groups,
      strip_group_prefix: values.strip_group_prefix,
```

**Step 3: Apply the identical changes to `web/src/pages/OrgSettingsPage.tsx`**

Same fields, defaults, `providerToForm`, `set` handler, checkboxes, and payloads as in Step 2.

**Step 4: Verify TypeScript compiles**

Run: `cd web && npx tsc --noEmit`
Expected: no errors.

**Step 5: Run the frontend build**

Run: `cd web && npm run build`
Expected: build succeeds.

**Step 6: Commit**

```bash
git add web/src/types/index.ts web/src/pages/AdminPage.tsx web/src/pages/OrgSettingsPage.tsx
git commit -m "feat(web): expose SSO empty-group sync and prefix-strip options"
```

---

### Task 8: Group display names

`groups.name` stays the sync identity; `display_name` is an admin-owned label
that the UI renders instead. It never participates in matching, uniqueness, or
permission resolution, and SSO sync never writes it. See design doc §4.

**Files:**
- Create: `internal/database/migrations/V116__group_display_names.sql`
- Modify: `internal/models/group.go`
- Modify: `internal/api/group_handlers.go`
- Test: `internal/api/group_handlers_test.go` (extend `TestGroupCRUD`), `internal/api/sso_group_sync_test.go`
- Modify: `web/src/types/index.ts`
- Create: `web/src/utils/groupLabel.ts`, `web/src/utils/groupLabel.test.ts`
- Modify: `web/src/pages/GroupsPage.tsx`
- Modify: `web/src/components/PermissionsPanel.tsx`, `web/src/components/WarehouseTableGrants.tsx`, `web/src/components/NewTablesInbox.tsx`
- Test: `web/src/test/GroupsPage.test.tsx`

**Step 1: Write the failing API test**

At the end of `TestGroupCRUD` in `internal/api/group_handlers_test.go`, add:

```go
	// Display names: set on update, rendered alongside the identity name
	labelBody, _ := json.Marshal(map[string]any{"name": "Analytics", "display_name": "Data Analysts Infra"})
	labelReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(labelBody))
	labelReq.Header.Set("Content-Type", "application/json")
	labelReq.Header.Set("Authorization", "Bearer "+token)
	labelRec := httptest.NewRecorder()
	srv.ServeHTTP(labelRec, labelReq)
	if labelRec.Code != http.StatusOK {
		t.Fatalf("set display name: expected 200, got %d: %s", labelRec.Code, labelRec.Body.String())
	}
	var labeled map[string]any
	json.NewDecoder(labelRec.Body).Decode(&labeled)
	if labeled["display_name"] != "Data Analysts Infra" {
		t.Fatalf("expected display_name to be set, got %v", labeled["display_name"])
	}
	if labeled["name"] != "Analytics" {
		t.Fatalf("expected name unchanged, got %v", labeled["name"])
	}

	// Label-only update keeps the name
	clearBody, _ := json.Marshal(map[string]any{"display_name": ""})
	clearReq := httptest.NewRequest("PUT", "/api/v1/groups/"+groupID, bytes.NewReader(clearBody))
	clearReq.Header.Set("Content-Type", "application/json")
	clearReq.Header.Set("Authorization", "Bearer "+token)
	clearRec := httptest.NewRecorder()
	srv.ServeHTTP(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear display name: expected 200, got %d: %s", clearRec.Code, clearRec.Body.String())
	}
	var cleared map[string]any
	json.NewDecoder(clearRec.Body).Decode(&cleared)
	if cleared["display_name"] != nil {
		t.Fatalf("expected cleared display_name null, got %v", cleared["display_name"])
	}
	if cleared["name"] != "Analytics" {
		t.Fatalf("expected name kept on label-only update, got %v", cleared["name"])
	}
```

In `internal/api/sso_group_sync_test.go`, add a regression test that a
sync-created group's label survives re-sync:

```go
func TestFindOrCreateGroup_PreservesDisplayName(t *testing.T) {
	s := setupTestServer(t)
	ctx := context.Background()

	slug := fmt.Sprintf("test-org-%d", time.Now().UnixNano())
	var orgID string
	err := s.DB().Pool.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		slug, slug,
	).Scan(&orgID)
	require.NoError(t, err)

	groupID, _, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "aether-analysts")
	require.NoError(t, err)

	_, err = s.DB().Pool.Exec(ctx, `UPDATE groups SET display_name=$1 WHERE id=$2`, "Data Analysts", groupID)
	require.NoError(t, err)

	again, created, err := api.FindOrCreateGroup(ctx, s.DB().Pool, orgID, "AETHER-ANALYSTS")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, groupID, again)

	var label *string
	require.NoError(t, s.DB().Pool.QueryRow(ctx, `SELECT display_name FROM groups WHERE id=$1`, groupID).Scan(&label))
	require.NotNil(t, label)
	require.Equal(t, "Data Analysts", *label)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestGroupCRUD|TestFindOrCreateGroup' -timeout 3m`
Expected: FAIL — `display_name` missing from the response / column does not exist.

**Step 3: Create the migration**

`internal/database/migrations/V116__group_display_names.sql`:

```sql
ALTER TABLE groups ADD COLUMN display_name text;
```

**Step 4: Extend the model**

In `internal/models/group.go`, add to `Group` after `Name`:

```go
	DisplayName *string   `json:"display_name"`
```

**Step 5: Update `internal/api/group_handlers.go`**

Split the request types:

```go
type createGroupRequest struct {
	Name        string  `json:"name"`
	DisplayName *string `json:"display_name"`
}

type updateGroupRequest struct {
	Name        *string `json:"name"`
	DisplayName *string `json:"display_name"`
}

// normalizeDisplayName trims a label and maps blank to nil (SQL NULL).
func normalizeDisplayName(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
```

Both list queries (the `member=me` branch and the org-wide branch) select
`g.display_name`, scan it into `&g.DisplayName`, and order by the rendered
label:

```sql
	SELECT g.id, g.org_id, g.name, g.display_name, g.created_at, COUNT(...) AS member_count
	...
	ORDER BY COALESCE(g.display_name, g.name)
```

`handleCreateGroup`: keep the name validation, insert the normalized label:

```go
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name, display_name) VALUES ($1, $2, $3)
		 RETURNING id, org_id, name, display_name, created_at`,
		claims.OrgID, req.Name, normalizeDisplayName(req.DisplayName),
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.DisplayName, &g.CreatedAt)
```

`handleUpdateGroup`: replace the required-name decode with the tri-state
update. An omitted `name` keeps the current name; an omitted `display_name`
keeps the label, while an explicit blank clears it. Reject any label on the
canonical `Everyone` group:

```go
	var req updateGroupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if strings.EqualFold(name, "everyone") {
			writeError(w, http.StatusBadRequest, "\"everyone\" is a reserved group name")
			return
		}
		req.Name = &name
	}
	if isEveryone && req.DisplayName != nil {
		writeError(w, http.StatusBadRequest, "the \"Everyone\" group cannot have a display name")
		return
	}

	display := ""
	if req.DisplayName != nil {
		display = strings.TrimSpace(*req.DisplayName)
	}
	err = s.db.Pool.QueryRow(ctx,
		`UPDATE groups
		    SET name = COALESCE($1, name),
		        display_name = CASE WHEN $2 THEN NULLIF($3, '') ELSE display_name END
		  WHERE id=$4 AND org_id=$5
		  RETURNING id, org_id, name, display_name, created_at`,
		req.Name, req.DisplayName != nil, display, groupID, claims.OrgID,
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.DisplayName, &g.CreatedAt)
```

Audit entries keep using `name` as `ResourceName`; add
`Metadata: map[string]any{"display_name": ...}` to `group.update` so label edits
are visible in the audit log.

`FindOrCreateGroup` in `internal/api/sso_group_sync.go` needs **no change** —
its insert omits `display_name` (NULL) and its lookup only selects `id`, so an
existing label is never read or written. The Step 1 test pins that.

**Step 6: Run the Go tests**

Run: `go test ./internal/api/ -run 'TestGroup|TestFindOrCreateGroup|TestSyncSSOGroups' -timeout 3m`
Expected: PASS.

**Step 7: Frontend type and shared helper**

In `web/src/types/index.ts`, extend `Group`:

```ts
  display_name: string | null
```

Create `web/src/utils/groupLabel.ts`:

```ts
export interface GroupLike {
  name: string
  display_name?: string | null
}

/** Renders the admin-owned label, falling back to the sync identity name. */
export function groupLabel(group: GroupLike): string {
  const label = group.display_name?.trim()
  return label ? label : group.name
}
```

Create `web/src/utils/groupLabel.test.ts` covering: label present, label blank,
`display_name: null`, and `display_name` absent.

**Step 8: Render the label in the UI**

- `web/src/pages/GroupsPage.tsx`
  - Add `newGroupDisplayName` and `renameDisplayValue` state next to the
    existing `newGroupName` / `renameValue`.
  - Create dialog: an optional "Display name" input (placeholder: the name);
    send `display_name: newGroupDisplayName.trim()` in the create payload.
  - `handleRename` seeds `renameDisplayValue` with `group.display_name ?? ''`;
    `handleRenameSubmit` sends `{ name, display_name: renameDisplayValue.trim() }`
    (blank clears the label).
  - Render `groupLabel(group)` in the group row and the delete confirmation;
    add `title={group.name}` so the underlying identity is visible on hover.
  - The `Everyone` row keeps its existing hardcoded rendering and hides the
    rename/label controls.
- Replace group-name render sites with `groupLabel(...)` (keep `group.name` for
  identity logic like the `/^everyone$/i` test):
  - `web/src/components/PermissionsPanel.tsx` (`subject label`, option list)
  - `web/src/components/WarehouseTableGrants.tsx` (name map, `optgroup`)
  - `web/src/components/NewTablesInbox.tsx` (name map)
  Verify with: `rg -n "groupLabel|g\.name|group\.name" web/src`.

**Step 9: Extend `web/src/test/GroupsPage.test.tsx`**

- Listing renders the label when `display_name` is set and the name otherwise.
- Creating with a display name sends it in the POST payload.
- Renaming with a display name sends `{ name, display_name }` in the PUT
  payload; clearing it sends `display_name: ""`.
- The `Everyone` group exposes no label control.

**Step 10: Run frontend checks**

```bash
cd web && npx tsc --noEmit && npx vitest run --project=default && npm run build
```

Expected: all pass.

**Step 11: Commit**

```bash
git add internal/database/migrations/V116__group_display_names.sql internal/models/group.go internal/api/group_handlers.go internal/api/group_handlers_test.go internal/api/sso_group_sync_test.go web/src/types/index.ts web/src/utils/groupLabel.ts web/src/utils/groupLabel.test.ts web/src/pages/GroupsPage.tsx web/src/components/PermissionsPanel.tsx web/src/components/WarehouseTableGrants.tsx web/src/components/NewTablesInbox.tsx web/src/test/GroupsPage.test.tsx
git commit -m "feat(groups): add admin-owned display names"
```

---

### Task 9: Swagger, docs, full verification

**Files:**
- Modify: `internal/api/docs/` (generated)
- Modify: `docs/SSO_GROUP_PROVISIONING.md`

**Step 1: Regenerate Swagger**

Run: `swag init -g cmd/aether-server/main.go -o internal/api/docs`
Expected: `docs/swagger.json` and `docs/swagger.yaml` updated.

**Step 2: Update `docs/SSO_GROUP_PROVISIONING.md`**

- Add rows to the "Provider Configuration" table:

```markdown
| `sync_empty_groups` | `boolean` | `false` | When enabled, an absent/empty groups claim is authoritative: all SSO-managed memberships for that user are removed. Warning: Keycloak omits the claim entirely when a user has zero groups, so a removed or misconfigured group mapper is indistinguishable from "no groups". |
| `strip_group_prefix` | `boolean` | `false` | When enabled and `group_prefix` is set, the prefix is removed from stored/displayed group names (`Aether Notebooks: Area` → `Area`). Filtering still uses the prefix. |
```

- Update the reconciliation steps to note prefix stripping and empty-claim handling.
- Add a paragraph explaining the Keycloak omission behavior and the `get_user_info` failure guard (`GroupsUnavailable` skips sync).
- Add a "Display names" paragraph: sync creates groups with no label; admins can label any group; the label is rendered instead of the name but never used for matching, so it survives re-sync and an IdP rename still creates a new (unlabeled) group.
- Update the "Audit Events" table: `group.sso.create`, `group.sso.add_member`, `group.sso.remove_member` are now actually emitted.

**Step 3: Run full verification**

```bash
task check
cd web && npx tsc --noEmit && npm run build
cd relay && npm run build
```

Expected: all pass.

**Step 4: Commit**

```bash
git add internal/api/docs docs/SSO_GROUP_PROVISIONING.md
git commit -m "docs(sso): document empty-group sync and prefix stripping"
```

---

## Done Criteria

- `task check` passes.
- Frontend and relay builds pass.
- Provider with `sync_empty_groups=true` removes all SSO-managed memberships when the IdP reports zero groups, preserving manual memberships and never deleting group rows.
- Provider with `strip_group_prefix=true` stores `Aether Notebooks: Area Name` as `Area Name`.
- Providers with both flags `false` behave exactly as before.
- A failing UserInfo source with no ID-token groups skips reconciliation instead of wiping.
- Any group (`Everyone` excepted) can carry an admin-set display name; it is rendered in the UI, survives re-sync, and never affects matching, uniqueness, or permission resolution. `aether-notebooks-data-analysts-infra` can display as `Data Analysts Infra`.
- `PUT /groups/{id}` accepts `name` and/or `display_name`; omitted fields keep their current values, blank clears the label, and `Everyone` rejects a label with 400.
