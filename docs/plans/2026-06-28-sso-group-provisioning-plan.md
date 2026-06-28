# SSO Group Provisioning Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Auto-provision users into hnb groups based on OIDC claims (groups from ID token and/or UserInfo), with prefix filtering and full bidirectional sync on each login.

**Architecture:** Extend `sso_providers` table with group-sync config columns, add `sso_group_memberships` tracking table, extend `GenericOIDCProvider.Exchange` to optionally parse groups from ID token + UserInfo endpoint, and add reconciliation logic in the OIDC callback handler.

**Tech Stack:** Go (pgx, go-oidc, oauth2), PostgreSQL, React/TypeScript

---

### Task 1: DB Migration — sso_providers columns + sso_group_memberships table

**Files:**
- Create: `internal/database/migrations/073_sso_group_provisioning.sql`

**Step 1: Write the migration**

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

**Step 2: Run migrations to verify they apply cleanly**

Run: `task db:reset && task test` (migrations run on server startup, tests also run migrations)
Expected: migrations apply without error

**Step 3: Commit**

```bash
git add internal/database/migrations/073_sso_group_provisioning.sql
git commit -m "feat(db): add sso group provisioning columns and tracking table"
```

---

### Task 2: Extend Provider struct + CRUD in sso.go

**Files:**
- Modify: `internal/sso/sso.go` — Provider struct, scanProvider, CreateProvider, UpdateProvider, SQL queries
- Test: `internal/sso/sso_test.go` — verify round-trip of new fields

**Step 1: Read current sso.go to understand existing patterns**

Read: `internal/sso/sso.go` (already read above)

**Step 2: Extend the Provider struct**

Add these fields to the `Provider` struct (after `UpdatedAt`):
```go
Scopes         []string `json:"scopes"`
GroupsClaim    string   `json:"groups_claim"`
GroupPrefix    string   `json:"group_prefix"`
AutoSyncGroups bool     `json:"auto_sync_groups"`
GetUserInfo    bool     `json:"get_user_info"`
```

**Step 3: Update scanProvider — add new columns to row.Scan**

Add the 5 new fields to `scanProvider`'s `row.Scan` call, matching the order in the SELECT:
```go
&p.Scopes, &p.GroupsClaim, &p.GroupPrefix, &p.AutoSyncGroups, &p.GetUserInfo,
```

**Step 4: Update selectProviderCols**

Change to:
```go
const selectProviderCols = `id, scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, scopes, groups_claim, group_prefix, auto_sync_groups, get_user_info, created_at, updated_at`
```

**Step 5: Update CreateProvider INSERT**

Add new columns to INSERT:
```sql
INSERT INTO sso_providers (scope, org_id, name, provider_type, client_id, client_secret_enc, discovery_url, allowed_domains, enabled, scopes, groups_claim, group_prefix, auto_sync_groups, get_user_info)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
```

Update row args to pass the new fields:
```go
p.Scope, p.OrgID, p.Name, p.ProviderType, p.ClientID, encSecret, p.DiscoveryURL, p.AllowedDomains, p.Enabled,
p.Scopes, p.GroupsClaim, p.GroupPrefix, p.AutoSyncGroups, p.GetUserInfo,
```

**Step 6: Update UpdateProvider SET clause**

Add new columns:
```sql
SET name=$1, client_id=$2, client_secret_enc=$3, discovery_url=$4, allowed_domains=$5, enabled=$6,
    scopes=$7, groups_claim=$8, group_prefix=$9, auto_sync_groups=$10, get_user_info=$11,
    updated_at=now()
WHERE id=$12
```

**Step 7: Update collectProviders**

Add the 5 new fields to the `rows.Scan` call.

**Step 8: Write test for new fields round-trip**

Add to `sso_test.go`:

```go
func TestCreateProviderWithGroupSettings(t *testing.T) {
    db := setupTestDB(t)
    ctx := context.Background()

    input := sso.Provider{
        Scope:          "platform",
        Name:           "groups-test",
        ProviderType:   "oidc",
        ClientID:       "groups-client",
        ClientSecret:   "groups-secret",
        DiscoveryURL:   "https://accounts.example.com/.well-known/openid-configuration",
        AllowedDomains: []string{},
        Enabled:        true,
        Scopes:         []string{"openid", "profile", "email", "groups"},
        GroupsClaim:    "custom_groups",
        GroupPrefix:    "hnb-",
        AutoSyncGroups: true,
        GetUserInfo:    true,
    }

    created, err := sso.CreateProvider(ctx, db.Pool, testMasterKey, input)
    require.NoError(t, err)
    t.Cleanup(func() {
        db.Pool.Exec(ctx, `DELETE FROM sso_providers WHERE id=$1`, created.ID)
    })

    assert.Equal(t, input.Scopes, created.Scopes)
    assert.Equal(t, input.GroupsClaim, created.GroupsClaim)
    assert.Equal(t, input.GroupPrefix, created.GroupPrefix)
    assert.Equal(t, input.AutoSyncGroups, created.AutoSyncGroups)
    assert.Equal(t, input.GetUserInfo, created.GetUserInfo)

    got, err := sso.GetProvider(ctx, db.Pool, testMasterKey, created.ID)
    require.NoError(t, err)
    assert.Equal(t, input.Scopes, got.Scopes)
    assert.Equal(t, input.GroupsClaim, got.GroupsClaim)
    assert.Equal(t, input.GroupPrefix, got.GroupPrefix)
    assert.Equal(t, input.AutoSyncGroups, got.AutoSyncGroups)
    assert.Equal(t, input.GetUserInfo, got.GetUserInfo)
}
```

**Step 9: Run tests**

Run: `task test:v -run "TestCreateProviderWithGroupSettings|TestCreateAndGetProvider|TestUpdateProvider|TestListPlatformProviders|TestDeleteProvider|TestEnableDisablePlatformProvider|TestListEnabledProvidersForOrg"`
Expected: all pass

**Step 10: Commit**

```bash
git add internal/sso/sso.go internal/sso/sso_test.go
git commit -m "feat(sso): extend Provider struct with group sync fields"
```

---

### Task 3: Extend OIDC auth — Groups claim + UserInfo support

**Files:**
- Modify: `internal/auth/oidc.go` — OIDCClaims, GenericOIDCProvider, Exchange
- Test: `internal/auth/oidc_test.go`

**Step 1: Read current oidc.go**

Already read above.

**Step 2: Extend OIDCClaims**

```go
type OIDCClaims struct {
    Subject string
    Email   string
    Name    string
    Groups  []string
}
```

**Step 3: Extend GenericOIDCProvider**

```go
type GenericOIDCProvider struct {
    name        string
    verifier    *oidc.IDTokenVerifier
    oauth       oauth2.Config
    groupsClaim string
    getUserInfo bool
}
```

**Step 4: Update NewGenericOIDCProvider**

Change signature to:
```go
func NewGenericOIDCProvider(ctx context.Context, name, issuerURL, clientID, clientSecret, redirectURL string, scopes []string, groupsClaim string, getUserInfo bool) (*GenericOIDCProvider, error)
```

Set the new fields:
```go
groupsClaim: groupsClaim,
getUserInfo: getUserInfo,
```

**Step 5: Update Exchange to parse groups**

```go
func (p *GenericOIDCProvider) Exchange(ctx context.Context, code string) (*OIDCClaims, error) {
    token, err := p.oauth.Exchange(ctx, code)
    if err != nil {
        return nil, fmt.Errorf("exchange: %w", err)
    }

    rawIDToken, ok := token.Extra("id_token").(string)
    if !ok {
        return nil, fmt.Errorf("no id_token in response")
    }

    idToken, err := p.verifier.Verify(ctx, rawIDToken)
    if err != nil {
        return nil, fmt.Errorf("verify: %w", err)
    }

    // Parse all raw claims from the ID token
    var rawClaims map[string]any
    if err := idToken.Claims(&rawClaims); err != nil {
        return nil, fmt.Errorf("parse claims: %w", err)
    }

    claims := &OIDCClaims{
        Subject: idToken.Subject,
    }

    if email, ok := rawClaims["email"].(string); ok {
        claims.Email = email
    }
    if name, ok := rawClaims["name"].(string); ok {
        claims.Name = name
    }
    if groupsRaw, ok := rawClaims[p.groupsClaim]; ok {
        if groupsArr, ok := groupsRaw.([]any); ok {
            for _, g := range groupsArr {
                if s, ok := g.(string); ok {
                    claims.Groups = append(claims.Groups, s)
                }
            }
        }
    }

    // Optionally fetch UserInfo for additional claims
    if p.getUserInfo {
        userInfoClaims, err := p.fetchUserInfo(ctx, token.AccessToken)
        if err == nil {
            // Merge groups from UserInfo (overrides ID token groups)
            if groupsRaw, ok := userInfoClaims[p.groupsClaim]; ok {
                if groupsArr, ok := groupsRaw.([]any); ok {
                    claims.Groups = nil
                    for _, g := range groupsArr {
                        if s, ok := g.(string); ok {
                            claims.Groups = append(claims.Groups, s)
                        }
                    }
                }
            }
        }
    }

    return claims, nil
}

func (p *GenericOIDCProvider) fetchUserInfo(ctx context.Context, accessToken string) (map[string]any, error) {
    // Parse the UserInfo endpoint from the OIDC discovery
    // We don't store the provider discovery result, so re-fetch the provider
    provider, err := oidc.NewProvider(ctx, p.oauth.Endpoint.TokenURL[:strings.LastIndex(p.oauth.Endpoint.TokenURL, "/")])
    // Actually, use the UserInfo endpoint from oauth2 config if available
    // The oauth2.Config doesn't store UserInfo URL, so we need a different approach
    // Use the oidc package's UserInfo method
    
    userInfo, err := p.verifier.Verify(ctx, rawIDToken) // no, this doesn't work
    // Correct approach:
    return nil, fmt.Errorf("not implemented yet")
}
```

Wait — actually, the UserInfo endpoint is available from the `oidc.Provider` but we don't keep a reference to it. Let me reconsider.

The better approach: store the `*oidc.Provider` on `GenericOIDCProvider` so we can call `provider.UserInfo(ctx, oauth2.StaticTokenSource(accessToken))`.

```go
type GenericOIDCProvider struct {
    name        string
    verifier    *oidc.IDTokenVerifier
    oauth       oauth2.Config
    oidcProvider *oidc.Provider   // new — for UserInfo endpoint
    groupsClaim string
    getUserInfo bool
}
```

```go
func (p *GenericOIDCProvider) Exchange(ctx context.Context, code string) (*OIDCClaims, error) {
    // ... existing code up to parsing groups from ID token ...

    // Optionally fetch UserInfo for additional claims
    if p.getUserInfo && p.oidcProvider != nil {
        userInfo, err := p.oidcProvider.UserInfo(ctx, oauth2.StaticTokenSource(oauth2.Token{AccessToken: token.AccessToken}))
        if err == nil {
            var uiClaims map[string]any
            if err := userInfo.Claims(&uiClaims); err == nil {
                if groupsRaw, ok := uiClaims[p.groupsClaim]; ok {
                    if groupsArr, ok := groupsRaw.([]any); ok {
                        // UserInfo groups take precedence
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
    }

    return claims, nil
}
```

**Step 6: Update NewGenericOIDCProvider to store oidcProvider**

```go
func NewGenericOIDCProvider(ctx context.Context, name, issuerURL, clientID, clientSecret, redirectURL string, scopes []string, groupsClaim string, getUserInfo bool) (*GenericOIDCProvider, error) {
    provider, err := oidc.NewProvider(ctx, issuerURL)
    if err != nil {
        return nil, fmt.Errorf("oidc discovery: %w", err)
    }

    if len(scopes) == 0 {
        scopes = []string{oidc.ScopeOpenID, "profile", "email"}
    }

    return &GenericOIDCProvider{
        name:         name,
        verifier:     provider.Verifier(&oidc.Config{ClientID: clientID}),
        oauth: oauth2.Config{
            ClientID:     clientID,
            ClientSecret: clientSecret,
            Endpoint:     provider.Endpoint(),
            RedirectURL:  redirectURL,
            Scopes:       scopes,
        },
        oidcProvider: provider,
        groupsClaim:  groupsClaim,
        getUserInfo:  getUserInfo,
    }, nil
}
```

**Step 7: Write test**

Add compile-time check for groups:
```go
func TestOIDCClaimsHasGroups(t *testing.T) {
    c := auth.OIDCClaims{Subject: "s", Email: "e@e.com", Name: "n", Groups: []string{"a", "b"}}
    assert.Equal(t, []string{"a", "b"}, c.Groups)
}
```

**Step 8: Run tests**

Run: `task test:v -run "TestOIDC"`
Expected: PASS

**Step 9: Commit**

```bash
git add internal/auth/oidc.go internal/auth/oidc_test.go
git commit -m "feat(auth): add groups claim parsing and UserInfo support to OIDC Exchange"
```

---

### Task 4: Group reconciliation helper function

**Files:**
- Create: `internal/api/sso_group_sync.go`
- Test: `internal/api/sso_group_sync_test.go`

**Step 1: Write the reconciliation function**

```go
package api

import (
    "context"
    "fmt"
    "strings"

    "github.com/heavenlabs/hnb/internal/audit"
    "github.com/heavenlabs/hnb/internal/sso"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

// syncSSOGroups reconciles user group membership against IDP group claims.
// It auto-creates groups (scoped to orgID), adds the user to matching groups,
// and removes the user from SSO-provisioned groups no longer in the IDP list.
// Errors are non-fatal — the login still proceeds.
func syncSSOGroups(ctx context.Context, pool *pgxpool.Pool, logger *audit.Logger, provider sso.Provider, orgID, userID string, idpGroups []string) {
    // 1. Filter by prefix
    var filtered []string
    for _, g := range idpGroups {
        if provider.GroupPrefix == "" || strings.HasPrefix(g, provider.GroupPrefix) {
            filtered = append(filtered, g)
        }
    }
    if len(filtered) == 0 {
        return
    }

    // 2. For each group: find-or-create, add user
    for _, groupName := range filtered {
        groupID, err := findOrCreateGroup(ctx, pool, orgID, groupName)
        if err != nil {
            logger.Log(ctx, audit.Entry{
                OrgID:        orgID,
                Action:       "group.sso.error",
                ResourceType: "group",
                ResourceName: groupName,
                Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
            })
            continue
        }

        // Add user to group
        _, err = pool.Exec(ctx,
            `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
            groupID, userID,
        )
        if err != nil {
            logger.Log(ctx, audit.Entry{
                OrgID:        orgID,
                Action:       "group.sso.error",
                ResourceType: "group",
                ResourceID:   groupID,
                ResourceName: groupName,
                Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
            })
            continue
        }

        // Record SSO membership
        _, err = pool.Exec(ctx,
            `INSERT INTO sso_group_memberships (provider_id, group_id, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
            provider.ID, groupID, userID,
        )
        if err != nil {
            logger.Log(ctx, audit.Entry{
                OrgID:        orgID,
                Action:       "group.sso.error",
                ResourceType: "group",
                ResourceID:   groupID,
                ResourceName: groupName,
                Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
            })
            continue
        }
    }

    // 3. Remove stale SSO-provisioned memberships
    staleGroups, err := findStaleSSOGroups(ctx, pool, provider.ID, userID, filtered)
    if err != nil {
        logger.Log(ctx, audit.Entry{
            OrgID:        orgID,
            Action:       "group.sso.error",
            ResourceType: "group",
            Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
        })
        return
    }

    for _, groupID := range staleGroups {
        _, err := pool.Exec(ctx,
            `DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`,
            groupID, userID,
        )
        if err != nil {
            logger.Log(ctx, audit.Entry{
                OrgID:        orgID,
                Action:       "group.sso.error",
                ResourceType: "group",
                ResourceID:   groupID,
                Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
            })
            continue
        }

        _, err = pool.Exec(ctx,
            `DELETE FROM sso_group_memberships WHERE provider_id=$1 AND group_id=$2 AND user_id=$3`,
            provider.ID, groupID, userID,
        )
        if err != nil {
            logger.Log(ctx, audit.Entry{
                OrgID:        orgID,
                Action:       "group.sso.error",
                ResourceType: "group",
                ResourceID:   groupID,
                Metadata:     map[string]any{"error": err.Error(), "user_id": userID},
            })
            continue
        }
    }
}

// findOrCreateGroup finds a group by name (case-insensitive) in the org, or creates it.
func findOrCreateGroup(ctx context.Context, pool *pgxpool.Pool, orgID, name string) (string, error) {
    // Try case-insensitive lookup first
    var id string
    err := pool.QueryRow(ctx,
        `SELECT id FROM groups WHERE org_id=$1 AND LOWER(name)=LOWER($2)`,
        orgID, name,
    ).Scan(&id)
    if err == nil {
        return id, nil
    }
    if err != pgx.ErrNoRows {
        return "", fmt.Errorf("lookup group: %w", err)
    }

    // Create the group
    err = pool.QueryRow(ctx,
        `INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`,
        orgID, name,
    ).Scan(&id)
    if err != nil {
        return "", fmt.Errorf("create group: %w", err)
    }

    return id, nil
}

// findStaleSSOGroups returns group_ids that were SSO-provisioned but are no longer in the IDP group list.
func findStaleSSOGroups(ctx context.Context, pool *pgxpool.Pool, providerID, userID string, currentGroups []string) ([]string, error) {
    rows, err := pool.Query(ctx,
        `SELECT sgm.group_id
         FROM sso_group_memberships sgm
         JOIN groups g ON g.id = sgm.group_id
         WHERE sgm.provider_id = $1 AND sgm.user_id = $2
         AND g.name != ALL($3)`,
        providerID, userID, currentGroups,
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

**Step 2: Write the test**

Create `internal/api/sso_group_sync_test.go`:

```go
package api_test

import (
    "context"
    "testing"

    "github.com/heavenlabs/hnb/internal/audit"
    "github.com/heavenlabs/hnb/internal/sso"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSyncSSOGroups(t *testing.T) {
    s := setupTestServer(t)
    ctx := context.Background()

    // Create a test user and org
    userID := createTestUser(t, s.db.Pool, "groupsync@test.com")
    orgID := createTestOrgForUser(t, s.db.Pool, userID)

    // Create an SSO provider
    provider, err := sso.CreateProvider(ctx, s.db.Pool, testMasterKey, sso.Provider{
        Scope:          "org",
        OrgID:          &orgID,
        Name:           "sync-test",
        ProviderType:   "oidc",
        ClientID:       "test-client",
        ClientSecret:   "test-secret",
        DiscoveryURL:   "https://example.com/.well-known/openid-configuration",
        AllowedDomains: []string{},
        Enabled:        true,
        AutoSyncGroups: true,
    })
    require.NoError(t, err)

    logger := audit.NewLogger(s.db)

    // Sync with two groups
    syncSSOGroups(ctx, s.db.Pool, logger, provider, orgID, userID, []string{"engineering", "analysts"})

    // Verify memberships
    var count int
    err = s.db.Pool.QueryRow(ctx,
        `SELECT COUNT(*) FROM group_members gm
         JOIN groups g ON g.id = gm.group_id
         WHERE gm.user_id=$1 AND g.org_id=$2`,
        userID, orgID,
    ).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 2, count)

    // Verify SSO tracking
    err = s.db.Pool.QueryRow(ctx,
        `SELECT COUNT(*) FROM sso_group_memberships WHERE provider_id=$1 AND user_id=$2`,
        provider.ID, userID,
    ).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 2, count)

    // Sync with only one group — should remove the other
    syncSSOGroups(ctx, s.db.Pool, logger, provider, orgID, userID, []string{"engineering"})

    err = s.db.Pool.QueryRow(ctx,
        `SELECT COUNT(*) FROM group_members gm
         JOIN groups g ON g.id = gm.group_id
         WHERE gm.user_id=$1 AND g.org_id=$2`,
        userID, orgID,
    ).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 1, count)
}

func TestSyncSSOGroupsPrefixFilter(t *testing.T) {
    s := setupTestServer(t)
    ctx := context.Background()

    userID := createTestUser(t, s.db.Pool, "prefix@test.com")
    orgID := createTestOrgForUser(t, s.db.Pool, userID)

    provider, err := sso.CreateProvider(ctx, s.db.Pool, testMasterKey, sso.Provider{
        Scope:          "org",
        OrgID:          &orgID,
        Name:           "prefix-test",
        ProviderType:   "oidc",
        ClientID:       "test-client",
        ClientSecret:   "test-secret",
        DiscoveryURL:   "https://example.com/.well-known/openid-configuration",
        AllowedDomains: []string{},
        Enabled:        true,
        AutoSyncGroups: true,
        GroupPrefix:    "hnb-",
    })
    require.NoError(t, err)

    logger := audit.NewLogger(s.db)

    // Mix of prefixed and non-prefixed groups
    syncSSOGroups(ctx, s.db.Pool, logger, provider, orgID, userID,
        []string{"hnb-engineering", "hnb-analysts", "all-employees", "system-admins"})

    var count int
    err = s.db.Pool.QueryRow(ctx,
        `SELECT COUNT(*) FROM group_members gm
         JOIN groups g ON g.id = gm.group_id
         WHERE gm.user_id=$1 AND g.org_id=$2`,
        userID, orgID,
    ).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 2, count, "only hnb- prefixed groups should be synced")
}

func TestSyncSSOGroupsPreservesManualMemberships(t *testing.T) {
    s := setupTestServer(t)
    ctx := context.Background()

    userID := createTestUser(t, s.db.Pool, "manual@test.com")
    orgID := createTestOrgForUser(t, s.db.Pool, userID)

    provider, err := sso.CreateProvider(ctx, s.db.Pool, testMasterKey, sso.Provider{
        Scope:          "org",
        OrgID:          &orgID,
        Name:           "manual-test",
        ProviderType:   "oidc",
        ClientID:       "test-client",
        ClientSecret:   "test-secret",
        DiscoveryURL:   "https://example.com/.well-known/openid-configuration",
        AllowedDomains: []string{},
        Enabled:        true,
        AutoSyncGroups: true,
    })
    require.NoError(t, err)

    // Manually add user to a group (no sso_group_memberships row)
    var groupID string
    err = s.db.Pool.QueryRow(ctx,
        `INSERT INTO groups (org_id, name) VALUES ($1, 'manual-group') RETURNING id`,
        orgID,
    ).Scan(&groupID)
    require.NoError(t, err)

    _, err = s.db.Pool.Exec(ctx,
        `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
        groupID, userID,
    )
    require.NoError(t, err)

    logger := audit.NewLogger(s.db)

    // Sync with IDP groups that don't include the manual group
    syncSSOGroups(ctx, s.db.Pool, logger, provider, orgID, userID, []string{"engineering"})

    // Manual membership should still exist
    var count int
    err = s.db.Pool.QueryRow(ctx,
        `SELECT COUNT(*) FROM group_members WHERE group_id=$1 AND user_id=$2`,
        groupID, userID,
    ).Scan(&count)
    require.NoError(t, err)
    assert.Equal(t, 1, count, "manual membership should be preserved")
}
```

Note: `createTestUser` and `createTestOrgForUser` may already exist in `testhelpers_test.go` or need to be added.

**Step 3: Run tests**

Run: `task test:v -run "TestSyncSSOGroups"`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/api/sso_group_sync.go internal/api/sso_group_sync_test.go
git commit -m "feat(api): add SSO group reconciliation helper"
```

---

### Task 5: Wire reconciliation into OIDC callback handler

**Files:**
- Modify: `internal/api/oidc_handlers.go` — update `handleOIDCLogin`, `handleOIDCCallback`

**Step 1: Update handleOIDCLogin to pass scopes**

```go
provider, err := auth.NewGenericOIDCProvider(ctx, dbProvider.Name, dbProvider.DiscoveryURL,
    dbProvider.ClientID, dbProvider.ClientSecret, s.callbackURL(r, providerID),
    dbProvider.Scopes, dbProvider.GroupsClaim, dbProvider.GetUserInfo,
)
```

**Step 2: Update handleOIDCCallback**

Update `NewGenericOIDCProvider` call (same as above).

After the user lookup/creation block (after `role = "admin"` or the existing user branch), add:

```go
// Group reconciliation via SSO
if dbProvider.AutoSyncGroups && len(claims.Groups) > 0 {
    syncSSOGroups(ctx, s.db.Pool, s.audit, dbProvider, orgID, userID, claims.Groups)
}
```

This runs for both new and returning users.

**Step 3: Run tests**

Run: `task test:v -run "TestOIDC"`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/api/oidc_handlers.go
git commit -m "feat(api): wire SSO group reconciliation into OIDC login flow"
```

---

### Task 6: Update SSO provider request/response types

**Files:**
- Modify: `internal/api/sso_admin_handlers.go` — providerResponse, providerToResponse, ssoProviderRequest
- Modify: `internal/api/sso_org_handlers.go` — platformProviderResponse

**Step 1: Update providerResponse struct**

```go
type providerResponse struct {
    ID             string    `json:"id"`
    Scope          string    `json:"scope"`
    Name           string    `json:"name"`
    ProviderType   string    `json:"provider_type"`
    ClientID       string    `json:"client_id"`
    DiscoveryURL   string    `json:"discovery_url"`
    AllowedDomains []string  `json:"allowed_domains"`
    Enabled        bool      `json:"enabled"`
    Scopes         []string  `json:"scopes"`
    GroupsClaim    string    `json:"groups_claim"`
    GroupPrefix    string    `json:"group_prefix"`
    AutoSyncGroups bool      `json:"auto_sync_groups"`
    GetUserInfo    bool      `json:"get_user_info"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

**Step 2: Update providerToResponse**

```go
return providerResponse{
    ID:             p.ID,
    Scope:          p.Scope,
    Name:           p.Name,
    ProviderType:   p.ProviderType,
    ClientID:       p.ClientID,
    DiscoveryURL:   p.DiscoveryURL,
    AllowedDomains: domains,
    Enabled:        p.Enabled,
    Scopes:         p.Scopes,
    GroupsClaim:    p.GroupsClaim,
    GroupPrefix:    p.GroupPrefix,
    AutoSyncGroups: p.AutoSyncGroups,
    GetUserInfo:    p.GetUserInfo,
    CreatedAt:      p.CreatedAt,
    UpdatedAt:      p.UpdatedAt,
}
```

**Step 3: Update ssoProviderRequest struct**

```go
type ssoProviderRequest struct {
    Name           string   `json:"name"`
    ClientID       string   `json:"client_id"`
    ClientSecret   string   `json:"client_secret"`
    DiscoveryURL   string   `json:"discovery_url"`
    AllowedDomains []string `json:"allowed_domains"`
    Enabled        bool     `json:"enabled"`
    Scopes         []string `json:"scopes"`
    GroupsClaim    string   `json:"groups_claim"`
    GroupPrefix    string   `json:"group_prefix"`
    AutoSyncGroups bool     `json:"auto_sync_groups"`
    GetUserInfo    bool     `json:"get_user_info"`
}
```

**Step 4: Apply new fields in create/update handlers**

In `handleAdminCreateSSOProvider` and `handleAdminUpdateSSOProvider`, set the new fields on the `sso.Provider`:

```go
p.Scopes = req.Scopes
p.GroupsClaim = req.GroupsClaim
p.GroupPrefix = req.GroupPrefix
p.AutoSyncGroups = req.AutoSyncGroups
p.GetUserInfo = req.GetUserInfo
```

**Step 5: Update platformProviderResponse in sso_org_handlers.go**

```go
type platformProviderResponse struct {
    ID             string    `json:"id"`
    Scope          string    `json:"scope"`
    Name           string    `json:"name"`
    ProviderType   string    `json:"provider_type"`
    ClientID       string    `json:"client_id"`
    DiscoveryURL   string    `json:"discovery_url"`
    AllowedDomains []string  `json:"allowed_domains"`
    Enabled        bool      `json:"enabled"`
    EnabledForOrg  bool      `json:"enabled_for_org"`
    Scopes         []string  `json:"scopes"`
    GroupsClaim    string    `json:"groups_claim"`
    GroupPrefix    string    `json:"group_prefix"`
    AutoSyncGroups bool      `json:"auto_sync_groups"`
    GetUserInfo    bool      `json:"get_user_info"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
}
```

And update the inline struct literal where `platformProviderResponse` is constructed (around line 233-245):

```go
resp[i] = platformProviderResponse{
    ID:             p.ID,
    Scope:          p.Scope,
    Name:           p.Name,
    ProviderType:   p.ProviderType,
    ClientID:       p.ClientID,
    DiscoveryURL:   p.DiscoveryURL,
    AllowedDomains: domains(p.AllowedDomains),
    Enabled:        p.Enabled,
    EnabledForOrg:  enabledSet[p.ID],
    Scopes:         p.Scopes,
    GroupsClaim:    p.GroupsClaim,
    GroupPrefix:    p.GroupPrefix,
    AutoSyncGroups: p.AutoSyncGroups,
    GetUserInfo:    p.GetUserInfo,
    CreatedAt:      p.CreatedAt,
    UpdatedAt:      p.UpdatedAt,
}
```

**Step 6: Ensure org-level create/update handlers pass new fields**

In `handleOrgCreateSSOProvider`, add:
```go
p.Scopes = req.Scopes
p.GroupsClaim = req.GroupsClaim
p.GroupPrefix = req.GroupPrefix
p.AutoSyncGroups = req.AutoSyncGroups
p.GetUserInfo = req.GetUserInfo
```

Same in `handleOrgUpdateSSOProvider`.

**Step 7: Run tests**

Run: `task test`
Expected: all pass

**Step 8: Commit**

```bash
git add internal/api/sso_admin_handlers.go internal/api/sso_org_handlers.go
git commit -m "feat(api): add group sync fields to SSO provider request/response types"
```

---

### Task 7: Update SSO provider test for round-trip with new fields

Already covered in Task 2 Step 8 (the existing `TestCreateProviderWithGroupSettings` test). Verify it still passes.

**Step 1: Run tests**

Run: `task test:v -run "TestCreateProviderWithGroupSettings"`
Expected: PASS

---

### Task 8: Frontend — Update TypeScript types

**Files:**
- Modify: `web/src/types/index.ts`

**Step 1: Update SSOProvider interface**

```typescript
export interface SSOProvider {
  id: string
  scope: string
  name: string
  provider_type: string
  client_id: string
  discovery_url: string
  allowed_domains: string[]
  enabled: boolean
  scopes: string[]
  groups_claim: string
  group_prefix: string
  auto_sync_groups: boolean
  get_user_info: boolean
  created_at: string
  updated_at: string
}
```

**Step 2: Update PlatformSSOProvider (if it exists)**

Check if there's a `PlatformSSOProvider` type and update it the same way.

**Step 3: Commit**

```bash
git add web/src/types/index.ts
git commit -m "feat(web): add group sync fields to SSOProvider type"
```

---

### Task 9: Frontend — Update SSO provider form in AdminPage

**Files:**
- Modify: `web/src/pages/AdminPage.tsx`

**Step 1: Extend ProviderFormValues**

```typescript
interface ProviderFormValues {
  name: string
  client_id: string
  client_secret: string
  discovery_url: string
  allowed_domains: string
  enabled: boolean
  scopes: string
  groups_claim: string
  group_prefix: string
  auto_sync_groups: boolean
  get_user_info: boolean
}
```

**Step 2: Update emptyForm**

```typescript
const emptyForm: ProviderFormValues = {
  name: '',
  client_id: '',
  client_secret: '',
  discovery_url: '',
  allowed_domains: '',
  enabled: true,
  scopes: '',
  groups_claim: 'groups',
  group_prefix: '',
  auto_sync_groups: false,
  get_user_info: false,
}
```

**Step 3: Update providerToForm**

```typescript
function providerToForm(p: SSOProvider): ProviderFormValues {
  return {
    name: p.name,
    client_id: p.client_id,
    client_secret: '',
    discovery_url: p.discovery_url,
    allowed_domains: (p.allowed_domains ?? []).join(', '),
    enabled: p.enabled,
    scopes: (p.scopes ?? []).join(', '),
    groups_claim: p.groups_claim ?? 'groups',
    group_prefix: p.group_prefix ?? '',
    auto_sync_groups: p.auto_sync_groups ?? false,
    get_user_info: p.get_user_info ?? false,
  }
}
```

**Step 4: Update handleCreate and handleUpdate to send new fields**

In `handleCreate`:
```typescript
function handleCreate(values: ProviderFormValues) {
  const body: Record<string, unknown> = {
    name: values.name,
    client_id: values.client_id,
    client_secret: values.client_secret,
    discovery_url: values.discovery_url,
    allowed_domains: values.allowed_domains.split(',').map(s => s.trim()).filter(Boolean),
    enabled: values.enabled,
    scopes: values.scopes.split(',').map(s => s.trim()).filter(Boolean),
    groups_claim: values.groups_claim,
    group_prefix: values.group_prefix,
    auto_sync_groups: values.auto_sync_groups,
    get_user_info: values.get_user_info,
  }
  createProvider.mutate(body)
}
```

Same pattern in `handleUpdate`.

**Step 5: Add new form fields to ProviderForm component**

Add after the "Enabled" checkbox (around line 111):

```tsx
<label style={formStyles.label}>
  Scopes <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(comma-separated)</span>
  <input style={formStyles.input} value={values.scopes} onChange={set('scopes')} placeholder="openid, profile, email, groups" />
</label>
<label style={formStyles.label}>
  Groups Claim
  <input style={formStyles.input} value={values.groups_claim} onChange={set('groups_claim')} placeholder="groups" />
</label>
<label style={formStyles.label}>
  Group Prefix <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(only sync groups starting with this)</span>
  <input style={formStyles.input} value={values.group_prefix} onChange={set('group_prefix')} placeholder="hnb-" />
</label>
<label style={{ ...formStyles.label, flexDirection: 'row', alignItems: 'center', gap: 8 }}>
  <input type="checkbox" checked={values.auto_sync_groups} onChange={set('auto_sync_groups')} />
  Auto-sync Groups
</label>
<label style={{ ...formStyles.label, flexDirection: 'row', alignItems: 'center', gap: 8 }}>
  <input type="checkbox" checked={values.get_user_info} onChange={set('get_user_info')} />
  Call UserInfo Endpoint
</label>
```

**Step 6: Run TypeScript check**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

**Step 7: Commit**

```bash
git add web/src/pages/AdminPage.tsx
git commit -m "feat(web): add group sync form fields to admin SSO provider form"
```

---

### Task 10: Frontend — Update SSO provider form in OrgSettingsPage

**Files:**
- Modify: `web/src/pages/OrgSettingsPage.tsx`

**Step 1: Read the OrgSettingsPage to find the SSO provider form**

Read the file to find the equivalent form component and update it with the same new fields as AdminPage.tsx.

The changes mirror Task 9: update `ProviderFormValues` type, `emptyForm`, `providerToForm`, `handleCreate`, `handleUpdate`, and add the new form fields.

**Step 2: Run TypeScript check**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add web/src/pages/OrgSettingsPage.tsx
git commit -m "feat(web): add group sync form fields to org SSO provider form"
```

---

### Task 11: Verify full build and test

**Step 1: Run all Go tests**

Run: `task test`
Expected: all pass

**Step 2: Build the Go server**

Run: `task build`
Expected: builds cleanly

**Step 3: Build the frontend**

Run: `task build:web`
Expected: builds cleanly

**Step 4: Run TypeScript check**

Run: `cd web && npx tsc --noEmit`
Expected: No errors

**Step 5: Commit any remaining changes**

```bash
git add -A
git commit -m "chore: finalize SSO group provisioning implementation"
```
