# Permission System Review — Validation Report

> **Date:** 2026-06-16
> **Method:** API-level testing (curl) + UI testing (agent-browser)
> **Test users:** perm-admin (admin), perm-editor (editor), perm-viewer (viewer)

---

## Executive Summary

The permission system is **mostly functional** with **2 critical issues** and **1 UI-level issue**:

| Severity | Issue | Status |
|----------|-------|--------|
| **CRITICAL** | `org_role:editor` and `org_role:viewer` ACL entries are non-functional | Confirmed |
| **CRITICAL** | `org_role:everyone` works correctly (baseline) | OK |
| **MEDIUM** | UI sidebar tree shows items user lacks permission to access | Confirmed |

**The core ACL engine (user, group, folder inheritance) works correctly.** The only broken feature is `org_role`-based permissioning for individual roles (`editor`, `viewer`), which aligns with the codebase's deprecation of this feature.

---

## Permission Matrix Results

### Task 2: Admin Bypass + Deny-by-Default ✅

| Resource | Admin GET | Admin PUT | Editor GET | Editor PUT | Viewer GET |
|----------|-----------|-----------|------------|------------|------------|
| Notebook | 200 ✅ | 200 ✅ | 403 ✅ | 403 ✅ | 403 ✅ |
| Dashboard | 200 ✅ | 200 ✅ | 403 ✅ | 403 ✅ | 403 ✅ |
| Connector | 200 ✅ | — | 403 ✅ | — | 403 ✅ |
| Agent | 200 ✅ | 200 ✅ | 403 ✅ | 403 ✅ | 403 ✅ |
| Skill | 200 ✅ | 200 ✅ | 403 ✅ | 403 ✅ | 403 ✅ |
| Model Config | 200 ✅ | 200 ✅ | 403 ✅ | 403 ✅ | 403 ✅ |
| Folder | 200 ✅ | 200 ✅ | 403 ✅ | 403 ✅ | 403 ✅ |

**Result:** Admin bypass works. Deny-by-default works for all resource types.

### Task 3: Explicit User ACL ✅

Granting `user:editor` → `["view"]` on each resource type:

| Resource | Editor GET | Editor PUT | Viewer GET |
|----------|------------|------------|------------|
| Notebook | 200 ✅ | 403 ✅ | 403 ✅ |
| Dashboard | 200 ✅ | 403 ✅ | 403 ✅ |
| Connector | 200 ✅ | — | 403 ✅ |
| Agent | 200 ✅ | 403 ✅ | 403 ✅ |
| Skill | 200 ✅ | 403 ✅ | 403 ✅ |
| Model Config | 200 ✅ | 403 ✅ | 403 ✅ |
| Folder | 200 ✅ | 403 ✅ | 403 ✅ |

Upgrading to `["view","edit"]` correctly allows editing (200 on PUT).

### Task 4: Group-Based ACL ✅

- Created group `review-group`, added editor as member
- Granted `group:review-group` → `["view","edit"]` on dashboard
- **Editor (member):** GET 200 ✅, PUT 200 ✅
- **Viewer (non-member):** GET 403 ✅

### Task 5: org_role ACL ❌ CRITICAL

| ACL Entry | Editor GET | Viewer GET |
|-----------|------------|------------|
| `org_role:everyone` → `["view"]` on connector | 200 ✅ | 200 ✅ |
| `org_role:editor` → `["view","edit"]` on skill | **403 ❌** | 403 ✅ (expected) |
| `org_role:viewer` → `["view"]` on model-config | 403 ✅ (expected) | **403 ❌** |

**Root cause:** `matchesUser()` in `internal/api/permissions.go:204` only matches `subjectID == "everyone"`. Individual role values (`editor`, `viewer`) are explicitly commented as **deprecated** and never match.

**Impact:** Any ACL entry using `org_role:editor` or `org_role:viewer` is silently ignored. Users with these ACL entries are denied access.

### Task 6: Folder Inheritance ✅

- Created parent → child folder hierarchy
- Notebook in child folder inherits `user:editor` → `["view"]` from parent
- **Editor GET notebook-in-child:** 200 ✅ (inherited)
- **Editor GET notebook-outside:** 403 ✅ (no ACL)
- **Viewer GET notebook-in-child:** 403 ✅ (no ACL for viewer)

### Task 7: ACL Management (share/manage) ✅

- Granted editor `["view","edit","share"]` on agent
- **Editor PUT ACL on agent (has share):** 200 ✅
- **Editor PUT ACL on notebook (no share):** 403 ✅
- **Viewer GET ACL on agent (has view):** 200 ✅

### Task 9: List Endpoint Filtering ✅

| Resource | Admin | Editor | Viewer |
|----------|-------|--------|--------|
| Notebooks | 3 | 2 | 0 |
| Dashboards | 1 | 1 | 0 |
| Agents | 1 | 1 | 0 |
| Skills | 1 | 0 | 0 |
| Model Configs | 1 | 0 | 0 |
| Connectors | 1 | 1 | 1 |

**Note:** Viewer sees 1 connector because `org_role:everyone` grants view access.

---

## UI Validation Findings (Task 8)

### Sidebar Tree Shows Unauthorized Items ⚠️

The sidebar file tree renders **all items in the folder hierarchy** regardless of the user's permissions. Both editor and viewer see:
- Notebooks they cannot access
- Dashboards they cannot access
- Folders they cannot access

When clicking a restricted item:
- The page navigates to the resource URL
- The main content area shows an empty/error state (no crash)
- No explicit "403 Forbidden" message is displayed in the UI

**Impact:** Users can see resource names they shouldn't know about, even though they can't access the content.

---

## Gap Analysis

### Code-Level Issues

1. **`org_role` individual role matching is deprecated** (`permissions.go:203-204`)
   - Only `org_role:everyone` is recognized
   - `org_role:editor` and `org_role:viewer` are silently ignored
   - This is by design but may break existing ACL entries in the database

2. **Migration 062 backfilled `org_role:admin` entries** that are effectively dead code
   - These entries exist in the database but are never matched by `matchesUser()`
   - Admin users bypass ACL entirely, so these entries have no effect

3. **UI sidebar tree does not filter by permissions**
   - The folder tree component fetches all items without permission checks
   - This is a frontend-level issue, not a backend permission issue

### Recommendations

1. **If `org_role:editor`/`org_role:viewer` should work:** Re-add individual role matching in `matchesUser()`:
   ```go
   case "org_role":
       if c.subjectID == "everyone" {
           return true
       }
       return c.subjectID == orgRole
   ```

2. **If `org_role:editor`/`org_role:viewer` should NOT work:** Clean up any existing ACL entries in the database that use these subject IDs, and document this limitation.

3. **Fix UI sidebar filtering:** Add permission checks to the folder tree component so users only see items they have access to.

---

## Evidence

Screenshots saved to `/tmp/permission-review-*.png`:
- `permission-review-admin.png` — Admin view (all resources visible)
- `permission-review-editor.png` — Editor view (shows unauthorized items in sidebar)
- `permission-review-viewer.png` — Viewer view (shows unauthorized items in sidebar)
- `permission-review-viewer-notebook.png` — Viewer accessing restricted notebook (empty state)
