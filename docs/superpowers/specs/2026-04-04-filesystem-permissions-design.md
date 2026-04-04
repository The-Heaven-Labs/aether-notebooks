# Filesystem + Permissions Design

**Date:** 2026-04-04
**Items:** IMPROVEMENTS.md #5 (Filesystem structure) and #6 (Robust permission system)
**Status:** Approved

---

## Overview

Two tightly coupled features:

1. **Filesystem** — folders with arbitrary nesting (folder-in-folder). All resource types (notebooks, connectors, dashboards) live inside folders or at root. Each user gets a personal home folder that is private by default.

2. **Permissions** — fine-grained per-resource ACLs with inheritance through the folder tree. Custom groups as first-class subjects. More-specific entries override less-specific ones. Fallback to org role only when no ACL exists anywhere in the chain.

---

## Database Schema

### Migration 009 — Folders

```sql
CREATE TABLE folders (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  parent_id UUID REFERENCES folders(id) ON DELETE CASCADE,  -- NULL = root
  name TEXT NOT NULL,
  is_home BOOLEAN NOT NULL DEFAULT false,
  owner_id UUID REFERENCES users(id) ON DELETE CASCADE,     -- set for home folders
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (org_id, parent_id, name)
);

CREATE INDEX idx_folders_org ON folders (org_id);
CREATE INDEX idx_folders_parent ON folders (parent_id);

-- Add folder_id to all resource tables (NULL = root level)
ALTER TABLE notebooks  ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;
ALTER TABLE connectors ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;
ALTER TABLE dashboards ADD COLUMN folder_id UUID REFERENCES folders(id) ON DELETE SET NULL;

-- Back-fill home folders for all existing users
INSERT INTO folders (org_id, name, is_home, owner_id, created_by)
SELECT om.org_id, u.name || '''s Home', true, u.id, u.id
FROM users u
JOIN org_members om ON om.user_id = u.id;
```

### Migration 010 — Groups + ACL

```sql
CREATE TABLE groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (org_id, name)
);

CREATE TABLE group_members (
  group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (group_id, user_id)
);

CREATE TABLE acl_entries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  resource_type TEXT NOT NULL CHECK (resource_type IN ('folder','notebook','connector','dashboard')),
  resource_id UUID NOT NULL,
  subject_type TEXT NOT NULL CHECK (subject_type IN ('user','group','org_role')),
  subject_id TEXT NOT NULL,  -- user UUID, group UUID, or role name ('viewer','editor','admin')
  actions TEXT[] NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (resource_type, resource_id, subject_type, subject_id)
);

CREATE INDEX idx_acl_resource ON acl_entries (resource_type, resource_id);
CREATE INDEX idx_acl_subject ON acl_entries (subject_type, subject_id);

-- Seed ACL entries for all existing home folders
INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
SELECT f.org_id, 'folder', f.id, 'user', f.owner_id::text,
       ARRAY['view','create','edit','manage','delete']
FROM folders f WHERE f.is_home = true;
```

---

## Permission Actions Per Resource Type

| Resource    | Actions |
|-------------|---------|
| `folder`    | `view`, `create` (add items inside), `edit` (rename/move), `manage` (set permissions), `delete` |
| `notebook`  | `view`, `run`, `edit`, `share`, `delete` |
| `connector` | `view`, `use`, `edit`, `share`, `delete` |
| `dashboard` | `view`, `edit`, `share`, `delete` |

---

## Permission Resolution Algorithm

To check if user U has action A on resource R (type T):

```
1. Collect user's group memberships → group_ids[]

2. Determine resource's folder_id (NULL if at root)

3. Build ancestor folder chain via recursive CTE:
   WITH RECURSIVE ancestors AS (
     SELECT id, parent_id, 0 AS depth FROM folders WHERE id = <folder_id>
     UNION ALL
     SELECT f.id, f.parent_id, a.depth + 1
     FROM folders f JOIN ancestors a ON f.id = a.parent_id
   )

4. Collect ACL candidates ordered by specificity (most specific first):
   a. Entries on resource R itself          (depth = -1, most specific)
   b. Entries on immediate parent folder    (depth = 0)
   c. Entries on grandparent folder         (depth = 1)
   ... continuing up the tree ...
   Within the same depth: user entry beats group entry beats org_role entry

5. Take the first (most specific) entry that matches user U:
   - matches if subject_type='user'     AND subject_id = U.id
   - matches if subject_type='group'    AND subject_id in group_ids[]
   - matches if subject_type='org_role' AND subject_id = U.org_role

6. If a matching entry is found:
   → ALLOW if A is in entry.actions[], DENY otherwise

7. If NO matching entry found AND no ACL entries exist anywhere in the chain:
   → Fall back to org role defaults:
     viewer  → [view]
     editor  → [view, run, edit, use]
     admin   → all actions

8. If NO matching entry found AND ACL entries DO exist in the chain:
   → DENY (ACL is the complete policy; org role is not consulted)
```

### Home Folder Behavior

Home folders are seeded with a single ACL entry at creation granting the owner all actions. Because ACL entries exist, the org-role fallback is suppressed for all resources inside — only explicit grants apply. Other users cannot see the home folder's contents unless the owner (or an admin) adds an ACL entry for them.

---

## API Endpoints

### Folders

```
GET    /api/v1/folders                     List root contents (folders + resources at root)
GET    /api/v1/folders/:id                 Get folder contents (child folders + resources)
GET    /api/v1/folders/:id/ancestors       Breadcrumb path from root to :id
POST   /api/v1/folders                     Create folder { name, parent_id? }
PUT    /api/v1/folders/:id                 Rename or move { name?, parent_id? }
DELETE /api/v1/folders/:id                 Delete (fails if non-empty, ?force=true cascades)
```

Resource move — extend existing update endpoints to accept `folder_id`:
```
PUT /api/v1/notebooks/:id    accepts { folder_id? }
PUT /api/v1/connectors/:id   accepts { folder_id? }
PUT /api/v1/dashboards/:id   accepts { folder_id? }
```

### Groups

```
GET    /api/v1/groups                      List org groups (with member count)
POST   /api/v1/groups                      Create group { name }
PUT    /api/v1/groups/:id                  Rename { name }
DELETE /api/v1/groups/:id                  Delete group
POST   /api/v1/groups/:id/members          Add member { user_id }
DELETE /api/v1/groups/:id/members/:user_id Remove member
```

### ACL

```
GET /api/v1/acl/:resource_type/:resource_id    Get all ACL entries for a resource
PUT /api/v1/acl/:resource_type/:resource_id    Replace full ACL { entries: [{subject_type, subject_id, actions[]}] }
```

### Permission Middleware

New `requirePermission(resourceType, action string)` middleware helper:
- Resolves effective permissions using the algorithm above
- Returns 403 if DENY, passes through if ALLOW
- Used on all folder/resource routes
- Existing `requireRole` stays for org-level admin-only routes (groups management, org settings)

---

## Backend Changes

### New files
- `internal/api/folder_handlers.go`
- `internal/api/group_handlers.go`
- `internal/api/acl_handlers.go`
- `internal/models/folder.go`
- `internal/models/group.go`
- `internal/models/acl.go`

### Modified files
- `internal/api/router.go` — register new routes
- `internal/api/middleware.go` — add `requirePermission` helper
- `internal/api/notebook_handlers.go` — filter by folder, accept folder_id on create/update, gate with requirePermission
- `internal/api/connector_handlers.go` — same
- `internal/api/dashboard_handlers.go` — same
- `internal/api/auth_handlers.go` — create home folder on registration
- `internal/models/notebook.go` — add `FolderID` field
- `internal/models/connector.go` — add `FolderID` field
- `internal/models/dashboard.go` — add `FolderID` field
- `internal/database/migrations/009_folders.sql` — new
- `internal/database/migrations/010_groups_acl.sql` — new

---

## Frontend

### File Browser (HomePage.tsx)

`HomePage.tsx` becomes a file browser controlled by `?folder=<uuid>` query param (absent = root).

Layout:
```
[Breadcrumb: Home / Engineering / Analytics]
[New Folder]  [New Notebook]  [New Dashboard]  ...

Folders:
  📁 Analytics      📁 Reports      📁 Marketing

Items:
  📓 Q1 Report      🔌 Prod DB      📊 Sales Dashboard
```

Each item has a `⋯` context menu: Rename, Move to..., Permissions, Delete.

"Move to..." opens a folder picker modal that shows the folder tree.

### Permissions Panel

A slide-over drawer accessible from the `⋯` menu on any item. Shows:

- Header: resource name + type
- Inheritance note: "Inheriting N permissions from parent folder"
- ACL entries list: avatar + name/group + action checkboxes (view, run, edit, share, delete)
- Add entry row: user/group search box + action checkboxes + Add button
- Remove button (×) per entry

### Groups Page (`/groups`)

New page listing org groups. All members can view; only admins can create/edit/delete groups.

Each group row expands to show members with an "Add member" search and × remove buttons.

Sidebar link: "Groups" (visible to all members, admin badge if they have edit access).

### New Route

```tsx
<Route path="/groups" element={<ProtectedRoute><GroupsPage /></ProtectedRoute>} />
```

---

## Key Decisions

| Decision | Choice | Reason |
|---|---|---|
| Tree storage | Adjacency list (`parent_id`) | Simple, Postgres recursive CTEs handle depth well |
| Permission computation | Walk tree at query time | No cache invalidation complexity; scale is not a concern |
| ACL override rule | Most specific (deepest) wins | Matches user mental model of "closer = stronger" |
| Org-role fallback | Suppressed when any ACL exists in chain | Prevents org roles from leaking into private folders |
| Home folder | Auto-created on registration, seeded with owner ACL | Private by default, no extra UX step required |
| Groups | Custom, independent of org roles | Maximum flexibility; org roles are just the fallback |
