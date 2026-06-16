# Permission System: Full Review & Validation — Report

**Date:** 2026-06-16
**Environment:** Docker dev stack (localhost)
**Test Users:** perm-admin@test.com (admin), perm-editor@test.com (editor), perm-viewer@test.com (viewer)

---

## 1. Executive Summary

The hnb permission/ACL system is **largely correct** for resource-level access control. Admin bypass, deny-by-default, explicit user ACL, group-based ACL, org_role ACL, folder inheritance, and ACL management (share) all work as designed.

**Key findings:**
- **1 security gap**: `/api/v1/recent` endpoint returns all org resources without permission filtering, leaking resource names/IDs to all org members
- **1 UX gap**: UI action buttons (Edit, Delete, Permissions) are shown regardless of user permissions — clicking them results in 403 errors
- **1 design consideration**: PUT ACL endpoint replaces ALL entries, so a user with "share" can accidentally remove their own access

---

## 2. Permission Matrix Results

### Task 2: Admin Bypass + Deny-by-Default

| Resource | Admin View | Admin Edit | Editor View (no ACL) | Editor Edit (no ACL) | Viewer View (no ACL) |
|---|---|---|---|---|---|
| notebook | 200 | 200 | 403 | 403 | 403 |
| dashboard | 200 | 200 | 403 | 403 | 403 |
| connector | 200 | 200 | 403 | 403 | 403 |
| agent | 200 | 200 | 403 | 403 | 403 |
| model_config | 200 | 200 | 403 | 403 | 403 |
| skill | 200 | 200 | 403 | 403 | 403 |
| mcp_server | 200 | 200 | 403 | 403 | 403 |
| folder | 200 | 200 | 403 | 403 | 403 |

**Result: ALL PASS** — Admin bypass works, deny-by-default works.

### Task 3: Explicit User ACL (editor gets "view")

| Resource | Editor View | Editor Edit | Viewer View |
|---|---|---|---|
| notebook | 200 | 403 | 403 |
| dashboard | 200 | 403 | 403 |
| connector | 200 | 403 | 403 |
| agent | 200 | 403 | 403 |
| model_config | 200 | 403 | 403 |
| skill | 200 | 403 | 403 |
| mcp_server | 200 | 403 | 403 |
| folder | 200 | 403 | 403 |

**Result: ALL PASS** — Explicit user ACL grants view-only correctly.

### Task 4: Group-Based ACL (editor in "review-group" gets "view","edit" on dashboard)

| Test | Expected | Actual |
|---|---|---|
| Editor (group member) view dashboard | 200 | 200 |
| Editor (group member) edit dashboard | 200 | 200 |
| Viewer (not in group) view dashboard | 403 | 403 |

**Result: ALL PASS** — Group-based ACL works correctly.

### Task 5: org_role ACL

| Grant | Test | Expected | Actual |
|---|---|---|---|
| org_role:everyone view on connector | Editor view | 200 | 200 |
| | Viewer view | 200 | 200 |
| | Editor edit | 403 | 403 |
| org_role:editor view,edit on skill | Editor view | 200 | 200 |
| | Editor edit | 200 | 200 |
| | Viewer view | 403 | 403 |

**Result: ALL PASS** — org_role ACL works correctly.

### Task 6: Folder Inheritance

| Test | Expected | Actual |
|---|---|---|
| Editor view notebook-in-child (inherits from parent) | 200 | 200 |
| Editor view notebook-outside (no ACL) | 403 | 403 |
| Viewer view notebook-in-child | 403 | 403 |

**Result: ALL PASS** — Folder inheritance works correctly.

### Task 7: ACL Management (share)

| Test | Expected | Actual |
|---|---|---|
| Editor (has "share") set ACL on agent | 200 | 200 |
| Editor (no "share") set ACL on notebook | 403 | 403 |
| Viewer (granted by editor) view agent | 200 | 200 |

**Result: ALL PASS** — ACL management works correctly.

---

## 3. Gap Analysis

### GAP-1: `/api/v1/recent` — No Permission Filtering (SECURITY)

**Severity:** Medium
**File:** `internal/api/recent_handlers.go:19-30`

The recent endpoint queries all notebooks, dashboards, and connectors for the org without any permission filtering:

```sql
SELECT id::text, 'notebook' AS type, title AS name, updated_at
FROM notebooks WHERE org_id = $1
UNION ALL
...
```

**Impact:** All org members can see names/IDs of all resources, even those they don't have access to. While the GET endpoints enforce permissions, the list endpoint leaks metadata.

**Fix:** Add permission checks similar to other list endpoints (check `checkPermission` for each item).

### GAP-2: UI Action Buttons Without Permission Checks (UX)

**Severity:** Low
**Affected pages:** Dashboards, Connectors, Skills, MCP Servers, Agents, Models

The UI shows Edit, Delete, Permissions buttons on resources regardless of whether the user has those permissions. Clicking them results in 403 errors from the API.

**Example:** Editor sees "Delete" button on "Permission Test Skill" but only has view+edit permissions (no delete).

**Fix:** Either hide/disable buttons based on user permissions, or fetch permissions for each resource and filter buttons accordingly.

### GAP-3: PUT ACL Replaces All Entries (DESIGN)

**Severity:** Low
**File:** `internal/api/acl_handlers.go:148-154`

The PUT ACL endpoint deletes ALL existing entries and replaces with the new set. This means a user with "share" permission who sets ACL without including themselves will lose their own access.

**Observed in:** Task 7 — editor had "view,edit,share" on agent, but after setting viewer's ACL (without including themselves), editor's entry was removed.

**Consideration:** This is by design (atomic replacement), but could be documented as a warning. Alternatively, the endpoint could refuse to remove the caller's own entry.

---

## 4. List Endpoint Filtering

| Endpoint | Filtered? | Notes |
|---|---|---|
| `GET /api/v1/notebooks` | YES | Checks `checkPermission` per notebook |
| `GET /api/v1/dashboards` | YES | Checks `checkPermission` per dashboard |
| `GET /api/v1/connectors` | YES | Checks `checkPermission` per connector |
| `GET /api/v1/agents` | YES | Checks `checkPermission` per agent |
| `GET /api/v1/skills` | YES | Checks `checkPermission` per skill |
| `GET /api/v1/model-configs` | YES | Checks `checkPermission` per model_config |
| `GET /api/v1/mcp-servers` | YES | Checks `checkPermission` per mcp_server |
| `GET /api/v1/folders` | YES | Checks `checkPermission` per folder |
| `GET /api/v1/recent` | **NO** | Returns all org resources unfiltered |

---

## 5. Agent-Browser Evidence

Screenshots saved to `/tmp/perm-review-*.png`:
- `admin-home.png` — Admin sees all resources and navigation
- `editor-home.png` — Editor sees filtered resources (recent section leaks metadata)
- `editor-dashboards.png` — Editor sees only "group test" dashboard (correct)
- `editor-connectors.png` — Editor sees "Permission Test Connector" (correct)
- `editor-skills.png` — Editor sees "hack" skill with action buttons (UX gap)
- `editor-agents.png` — Editor sees "No agents yet" (correct after ACL removal)
- `viewer-home.png` — Viewer sees only connector (correct), recent section leaks metadata

---

## 6. Recommendations

### Priority 1 (Security)
1. **Fix `/api/v1/recent` endpoint** — Add permission filtering to prevent metadata leakage

### Priority 2 (UX)
2. **Hide/disable UI action buttons based on permissions** — Prevent users from seeing buttons they can't use
3. **Consider warning on PUT ACL** — Warn users when they're removing their own access entry

### Priority 3 (Documentation)
4. **Document PUT ACL atomic replacement behavior** — Make it clear that PUT replaces all entries
5. **Document org_role:everyone as non-restrictive** — Clarify that org_role:everyone entries don't block org role defaults
