# Deny-by-Default Permissions — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the org-role fallback permission model with explicit deny-by-default ACL, add audit logging, and improve the PermissionsPanel UI with inheritance indicators and action descriptions.

**Architecture:** Changes span three layers: (1) Go backend — `checkPermission` logic, migration, ACL handlers; (2) Database — one new migration; (3) React frontend — PermissionsPanel UI enhancements.

**Tech Stack:** Go (net/http), PostgreSQL, React, TanStack Query, ClickHouse audit logging.

---

## Task 1: Add migration — create ACL entries for existing resources

**Files:**
- Create: `internal/database/migrations/018_deny_by_default_acls.sql`
- Modify: `internal/database/migrations/run.go` (or whatever applies migrations)

**Step 1: Write migration SQL**

```sql
-- Migration: create ACL entries for all existing resources that have none.
-- Idempotent: uses ON CONFLICT DO NOTHING.

-- Notebooks without ACL: grant creator all actions + org_role:admin all actions
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT n.org_id, 'notebook', n.id, 'user', n.created_by::text,
       ARRAY['view','run','edit','share','delete']
FROM notebooks n
WHERE NOT EXISTS (
  SELECT 1 FROM acl_entries a
  WHERE a.resource_type = 'notebook' AND a.resource_id = n.id
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT n.org_id, 'notebook', n.id, 'org_role', 'admin',
       ARRAY['view','run','edit','share','delete']
FROM notebooks n
WHERE NOT EXISTS (
  SELECT 1 FROM acl_entries a
  WHERE a.resource_type = 'notebook' AND a.resource_id = n.id
    AND a.subject_type = 'org_role' AND a.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Connectors without ACL: grant creator all actions + org_role:admin all actions
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT c.org_id, 'connector', c.id, 'user', c.created_by::text,
       ARRAY['view','use','edit','share','delete']
FROM connectors c
WHERE NOT EXISTS (
  SELECT 1 FROM acl_entries a
  WHERE a.resource_type = 'connector' AND a.resource_id = c.id
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT c.org_id, 'connector', c.id, 'org_role', 'admin',
       ARRAY['view','use','edit','share','delete']
FROM connectors c
WHERE NOT EXISTS (
  SELECT 1 FROM acl_entries a
  WHERE a.resource_type = 'connector' AND a.resource_id = c.id
    AND a.subject_type = 'org_role' AND a.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Dashboards without ACL (dashboards don't have created_by, use org admin as fallback)
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT d.org_id, 'dashboard', d.id, 'org_role', 'admin',
       ARRAY['view','edit','share','delete']
FROM dashboards d
WHERE NOT EXISTS (
  SELECT 1 FROM acl_entries a
  WHERE a.resource_type = 'dashboard' AND a.resource_id = d.id
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

-- Folders without ACL: grant creator all actions + org_role:admin all actions
-- (skip home folders which already have ACL per migration 010)
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT f.org_id, 'folder', f.id, 'user', f.created_by::text,
       ARRAY['view','create','edit','manage','share','delete']
FROM folders f
WHERE f.is_home = false
  AND NOT EXISTS (
  SELECT 1 FROM acl_entries a
  WHERE a.resource_type = 'folder' AND a.resource_id = f.id
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;

INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT f.org_id, 'folder', f.id, 'org_role', 'admin',
       ARRAY['view','create','edit','manage','share','delete']
FROM folders f
WHERE f.is_home = false
  AND NOT EXISTS (
  SELECT 1 FROM acl_entries a
  WHERE a.resource_type = 'folder' AND a.resource_id = f.id
    AND a.subject_type = 'org_role' AND a.subject_id = 'admin'
)
ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING;
```

**Step 2: Verify migration runs cleanly**

Run: `task infra:up && task test:api` (or equivalent)
Expected: All tests pass including any new tests for the migration.

**Step 3: Commit**
```bash
git add internal/database/migrations/018_deny_by_default_acls.sql
git commit -m "migrate: add ACL entries for existing resources (deny-by-default)"
```

---

## Task 2: Modify checkPermission — remove org-role fallback, add org admin bypass

**Files:**
- Modify: `internal/api/permissions.go:1-257`

**Step 1: Write the failing test**

In `internal/api/permissions_test.go`, add tests that verify:
- Org admin bypasses all ACL checks (no ACL entry needed)
- Non-admin with no matching ACL is denied even if their org role would have granted it
- Existing ACL grants still work for creators

```go
func TestCheckPermission_OrgAdminBypass(t *testing.T) {
    // setupTestServer(t) gives admin role; verify admin can access any resource
    // even without ACL entries
}

func TestCheckPermission_DenyByDefault(t *testing.T) {
    // A viewer with no ACL entry should be denied access to a notebook
    // (previously would have been allowed via viewer org-role fallback)
}
```

Run: `task test:api` — expect new tests to fail (org-role fallback still in place).

**Step 2: Update checkPermission logic**

Remove `orgRoleActions` and `everyoneRoleActions` fallbacks. Replace the final resolution step:

Old (lines ~193-201):
```go
// No ACL matched user → use org role defaults
if actions, ok := orgRoleActions[orgRole]; ok {
    return actions[action], nil
}
// Special "everyone" pseudo-role fallback
if everyoneRoleActions[action] {
    return true, nil
}
return false, nil
```

New:
```go
// No ACL matched user → DENY (deny-by-default)
return false, nil
```

Add org admin bypass at the start of `checkPermission`:
```go
// Org admins always have full access to everything
if orgRole == "admin" {
    return true, nil
}
```

**Step 3: Run tests**

Run: `task test:api` — expect new tests to pass, existing tests still pass (migration covers existing resources).

**Step 4: Commit**
```bash
git add internal/api/permissions.go
git commit -m "feat: remove org-role fallback, add admin bypass (deny-by-default)"
```

---

## Task 3: Add audit logging for ACL operations

**Files:**
- Modify: `internal/api/acl_handlers.go:57-129`
- Modify: `internal/audit/audit.go:12-24` (Entry struct already has Metadata; no struct change needed)

**Step 1: Add audit events for ACL changes in handlePutACL**

In `handlePutACL`, after a successful ACL update, log the diff. For each entry that changed:
- Detect if it's a grant (new entry), revoke (removed from previous), or update (actions changed)
- Log `acl.granted`, `acl.revoked`, `acl.updated` accordingly

```go
// After tx.Commit(ctx), before writeJSON:
for _, e := range req.Entries {
    s.audit.Log(ctx, audit.Entry{
        OrgID:        claims.OrgID,
        UserID:       claims.UserID,
        Action:       "acl.updated", // or "acl.granted" / "acl.revoked"
        ResourceType: resourceType,
        ResourceID:   resourceID,
        Metadata: map[string]any{
            "subject_type": e.SubjectType,
            "subject_id":   e.SubjectID,
            "actions":      e.Actions,
        },
    })
}
```

Also log when ACL entries are removed (during the DELETE before insert):
```go
// Collect old entries before DELETE
var oldEntries []struct {
    SubjectType string
    SubjectID   string
    Actions     []string
}
rows, _ := s.db.Pool.Query(ctx, `SELECT subject_type, subject_id, actions
    FROM acl_entries WHERE resource_type=$1 AND resource_id=$2::uuid AND org_id=$3`,
    resourceType, resourceID, claims.OrgID)
// ... scan into oldEntries
// Then after commit, log each as revoked if not re-added
```

**Step 2: Run tests**

Run: `task test:api` — audit logging changes should not break any existing tests.

**Step 3: Commit**
```bash
git add internal/api/acl_handlers.go
git commit -m "feat: add audit events for ACL changes (acl.granted/revoked/updated)"
```

---

## Task 4: Update PermissionsPanel — inheritance indicators and action descriptions

**Files:**
- Modify: `web/src/components/PermissionsPanel.tsx`

**Step 1: Add action descriptions**

Add a `ACTION_DESCRIPTIONS` map:

```typescript
const ACTION_DESCRIPTIONS: Record<ResourceType, Record<string, string>> = {
  connector: {
    view:   'See connector name, type, host, and status',
    use:    'Run SQL queries against the connector',
    edit:   'Edit connector configuration and credentials',
    share:  'Share this connector with others',
    delete: 'Delete the connector permanently',
  },
  notebook: {
    view:   'See notebook content and cell outputs',
    run:    'Execute notebook cells',
    edit:   'Edit cell contents and notebook settings',
    share:  'Share this notebook with others',
    delete: 'Delete the notebook permanently',
  },
  dashboard: {
    view:   'View the dashboard',
    edit:   'Edit dashboard layout and content',
    share:  'Share this dashboard with others',
    delete: 'Delete the dashboard permanently',
  },
  folder: {
    view:   'See the folder and its contents',
    create: 'Create sub-folders and items',
    edit:   'Rename or restructure the folder',
    manage: 'Manage folder-level permissions',
    share:  'Share this folder with others',
    delete: 'Delete the folder permanently',
  },
}
```

**Step 2: Show description on hover or as tooltip in action labels**

In the checkbox label rendering (around line 261-270), add a `title` attribute to each action label:

```typescript
<span style={styles.actionLabel} title={ACTION_DESCRIPTIONS[resourceType][action]}>
  {action}
</span>
```

**Step 3: Add inherited vs direct indicator**

Each ACL entry row needs to show if it's direct or inherited. This requires:
- Fetching parent folder ACL entries when `parentFolderId` is set
- Tagging each entry as `inherited: boolean`
- Rendering inherited entries in a separate section with a "Inherited from [folder name]" label
- Making inherited entries read-only (no remove/modify buttons)

```typescript
// In the component, after fetching aclData and parentAcl:
// Merge and tag entries
const allEntries = [
  ...(aclData ?? []).map(e => ({ ...e, inherited: false })),
  ...(parentAcl ?? []).map(e => ({ ...e, inherited: true })),
]

// In inherited section, show folder name:
const parentFolderName = parentFolders?.[permissionsTarget.parentFolderId]?.name ?? 'Parent folder'
```

**Step 4: Show a divider between direct and inherited entries**

```typescript
{/* Inherited entries section */}
{inheritedEntries.length > 0 && (
  <div style={styles.inheritedSection}>
    <div style={styles.inheritedHeader}>
      <span>Inherited from <strong>{parentFolderName}</strong></span>
      <span style={styles.inheritedBadge}>read only</span>
    </div>
    {inheritedEntries.map(entry => (
      <div style={{ ...styles.entryRow, opacity: 0.6 }}>
        {/* same rendering as direct entries but without remove/edit controls */}
      </div>
    ))}
  </div>
)}
```

**Step 5: Run typecheck and build**

Run: `rtk tsc --noEmit && rtk npm run build` — expect clean build.

**Step 6: Commit**
```bash
git add web/src/components/PermissionsPanel.tsx
git commit -m "feat: add action descriptions and inheritance indicators to PermissionsPanel"
```

---

## Task 5: Ensure Everyone group membership is maintained for all org members

**Files:**
- Modify: `internal/api/auth_handlers.go` (user registration/org-join)
- Or existing group seed logic if already handled

**Step 1: Check existing user-join flow**

Find where users are added to orgs (likely `handleAddOrgMember` or user registration).

**Step 2: Add Everyone group auto-join**

When a user joins an org, automatically add them to the `Everyone` group:

```go
// After user is successfully added to org_members
_, err = tx.Exec(ctx,
  `INSERT INTO group_members (group_id, user_id)
   SELECT g.id, $1 FROM groups g WHERE g.org_id = $2 AND g.name = 'Everyone'
   ON CONFLICT (group_id, user_id) DO NOTHING`,
  userID, orgID)
if err != nil {
    return err
}
```

**Step 3: Commit**
```bash
git add internal/api/auth_handlers.go  # or wherever org join is handled
git commit -m "feat: auto-add new org members to Everyone group"
```

---

## Task 6: End-to-end verification

**Step 1: Run full test suite**

Run: `task check` (fmt + vet + tidy) then `task test`
Expected: All tests pass, including new and existing.

**Step 2: Manual smoke test**

1. Create a new org with a new user
2. As that user, verify they see no notebooks/connectors by default
3. As org admin, grant the user access to a resource
4. Verify user can now see/access it

**Step 3: Commit final state**
```bash
git push
```

---

## Execution Order

Tasks 1 → 2 → 3 → 4 → 5 → 6. Each must pass its tests before moving to the next.

> **Two execution options:**
> **1. Subagent-Driven (this session)** — I dispatch fresh subagent per task, review between tasks, fast iteration
> **2. Parallel Session (separate)** — Open new session with executing-plans, batch execution with checkpoints
> Which approach?