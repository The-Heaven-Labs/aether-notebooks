# Deny-by-Default Permission Model

**Date:** 2026-05-18
**Status:** Approved
**Owner:** Engineering

---

## 1. Principle

**Deny by default.** No user can access any resource unless an ACL entry explicitly grants access. This applies to all resource types: connectors, notebooks, dashboards, and folders.

This is the security/privacy model — similar to how Kubernetes RBAC works (principle of least privilege by default).

---

## 2. Permission Model

### 2.1 Owner

The creator of a resource is the owner. Owners have full permissions (`view`, `run`, `edit`, `share`, `delete`, `use`, `manage`) and can grant any subset of those to others, including making someone else an owner/manager of that resource.

### 2.2 Org Admins

Any org admin has full permissions on every resource in the org, without needing explicit ACL entries. This is like Kubernetes' `cluster-admin`. They can also manage ACLs on any resource.

*(Future: a toggleable "admin-as-user" mode where admins can opt to act as regular users. Out of scope for this design.)*

### 2.3 Everyone Group

A system group that:
- Contains **every org member** automatically (managed by the system, not manually)
- Cannot be deleted
- Behaves like a normal group — ACLs can grant it any level of access
- Is included in the members/group selector in the PermissionsPanel

Resources that should be broadly accessible (e.g., company-wide dashboards, shared connectors) get explicit grants to the `Everyone` group. New users automatically see resources with grants to `Everyone`.

### 2.4 Custom Groups

Users can belong to zero or more custom groups. Group membership is managed by org admins. Permissions cascade: if a user is in a group and that group has an ACL entry, the user inherits those permissions.

### 2.5 Permission Actions by Resource Type

| Action | Connector | Notebook | Dashboard | Folder |
|--------|-----------|----------|-----------|--------|
| view | View connector config & status | View cell contents | View dashboard | See folder & contents |
| run / use | Execute queries | Run cells | — | — |
| edit | Edit connector config | Edit cells & metadata | Edit dashboard layout | Rename / restructure |
| share | Share connector | Share notebook | Share dashboard | Share folder |
| delete | Delete connector | Delete notebook | Delete dashboard | Delete folder |
| manage | — | — | — | Manage folder ACLs |
| create | — | — | — | Create sub-folders/items |

---

## 3. ACL Resolution

Simplified permission check:

1. Is user an org admin → grant all actions
2. Find ACL entries on the resource matching the user:
   - Direct user entry (`subject_type: user`)
   - Group entries (`subject_type: group`) where user is a member
   - `org_role:everyone` entry (the Everyone group)
3. Any matching entry that includes the requested action → grant
4. No match found → **DENY**

### Folder Inheritance

Folder ACL entries cascade to all sub-resources (notebooks, connectors, dashboards, sub-folders). The recursive CTE walk remains — ancestor folder entries are collected and considered alongside direct resource entries.

Within a single resource's ACL entries, resolution order: **user → group → org_role** (user entries take precedence over group entries, group over org_role).

**Removed:** The org-role fallback (where `editor` role got `view, run, edit, use` by default) is eliminated. If no ACL applies to a user, they are denied.

---

## 4. Audit Trail

All access-related events are logged to ClickHouse (existing `audit` package).

### Events to Log

| Event | Trigger | Fields |
|-------|---------|--------|
| `acl.granted` | ACL entry created or updated | `org_id`, `user_id`, `resource_type`, `resource_id`, `subject_type`, `subject_id`, `actions[]` |
| `acl.revoked` | ACL entry removed | `org_id`, `user_id`, `resource_type`, `resource_id`, `subject_type`, `subject_id` |
| `acl.updated` | ACL entry actions changed | `org_id`, `user_id`, `resource_type`, `resource_id`, `subject_type`, `subject_id`, `old_actions[]`, `new_actions[]` |
| `permission.checked` | Permission check performed (optional, high volume) | `org_id`, `user_id`, `resource_type`, `resource_id`, `action`, `result` |
| `resource.accessed` | User accessed a resource (view/run/edit) | `org_id`, `user_id`, `resource_type`, `resource_id`, `action` |

The `permission.checked` event should be sampled or rate-limited to avoid audit log explosion.

---

## 5. Migration

When this feature is deployed, a **one-time migration** runs at server startup:

For every resource (connector, notebook, dashboard, folder) that has **no ACL entries**:
1. Add ACL entry: subject = `user:{creator_id}`, actions = all available for that resource type
2. Add ACL entry: subject = `org_role:admin`, actions = all available for that resource type

This migration must be **idempotent** — running it multiple times must not create duplicate ACL entries.

After migration, the org-role fallback is **removed** from `checkPermission`. All permission checks go through explicit ACL only.

---

## 6. UI: Permissions Panel

### 6.1 Entry Point

Every resource type (connector, notebook, dashboard, folder) has a "Permissions" option accessible via a context menu (the "⋯" button on each item in file lists, and now also on connectors page).

### 6.2 Permission Source Indicator

Each ACL entry row shows whether it is **direct** or **inherited**:

- **Direct**: The entry is set directly on this resource. Shown with a label or icon (e.g., a filled indicator).
- **Inherited**: The entry comes from an ancestor folder. Shown with a different label/icon (e.g., an arrow or "inherited" badge), and displayed in a separate section or visually differentiated.

Users can see exactly where each permission came from.

### 6.3 Permission Descriptions

Each action has a human-readable description shown on hover or inline in the PermissionsPanel:

| Resource | Action | Description |
|----------|--------|-------------|
| Connector | view | See connector name, type, host, and status |
| Connector | use | Run SQL queries against the connector |
| Connector | edit | Edit connector configuration and credentials |
| Connector | share | Share this connector with others |
| Connector | delete | Delete the connector permanently |
| Notebook | view | See notebook content and cell outputs |
| Notebook | run | Execute notebook cells |
| Notebook | edit | Edit cell contents and notebook settings |
| Notebook | share | Share this notebook with others |
| Notebook | delete | Delete the notebook permanently |
| Dashboard | view | View the dashboard |
| Dashboard | edit | Edit dashboard layout and content |
| Dashboard | share | Share this dashboard with others |
| Dashboard | delete | Delete the dashboard permanently |
| Folder | view | See the folder and its contents |
| Folder | create | Create sub-folders and items |
| Folder | edit | Rename or restructure the folder |
| Folder | manage | Manage folder-level permissions |
| Folder | share | Share this folder with others |
| Folder | delete | Delete the folder permanently |

### 6.4 Presets

Preset buttons (`none`, `viewer`, `editor`, `admin`) continue to work as before, mapping to the appropriate action sets per resource type.

### 6.5 Inheritance Section

The PermissionsPanel for a resource that has a `folder_id` shows a section at the top:
- **"Inherited from [folder name]"** — lists each inherited ACL entry
- These are **read-only** in this view (to modify, user must go to the parent folder)
- A link/button to navigate to the parent folder's permissions panel

---

## 7. New Users

When a user joins an org:
- They are automatically a member of the `Everyone` group
- They can access any resource that grants permissions to `Everyone` (at whatever level is granted)
- They can be added to custom groups by org admins, gaining those group-based permissions

No special onboarding migration needed — the `Everyone` group handles this.

---

## 8. API Changes

No changes to the ACL data model. Changes to `checkPermission` logic:
- Remove org-role fallback
- Add org admin bypass (full access without ACL)

New audit events added via existing `audit.Log()` calls.

---

## 9. Backward Compatibility

After migration:
- Existing resources owned by users remain accessible to those users (creator ACL added)
- Existing org admins retain full access (admin ACL added)
- Users who had access only via org-role fallback (e.g., viewers who could see everything) now see only resources with explicit grants or `Everyone` group grants — this is the intended breaking change
- The PermissionsPanel shows both direct and inherited entries clearly

---

## 10. Future Considerations (Out of Scope)

- Admin "act as user" mode (opt-in reduced privilege for admins)
- Service accounts / machine tokens
- Time-limited ACL grants (e.g., "access expires in 7 days")
- Approval workflow for access requests