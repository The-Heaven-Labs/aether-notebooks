# Permission System: Full Review & Validation

**Date**: 2026-06-15
**Scope**: Audit-only — validate behavior end-to-end, document all gaps, no code changes.
**Approach**: Matrix-based validation with agent-browser testing.

---

## Resource Inventory

| Resource | DB Table | Actions | Auto-ACL on Create? | Folder Inheritance |
|---|---|---|---|---|
| `folder` | `folders` | `view, create, edit, manage, share, delete` | No (home folder only) | Parent-based walk |
| `notebook` | `notebooks` | `view, run, edit, share, delete` | No | Yes |
| `connector` | `connectors` | `view, use, edit, share, delete` | No | Yes |
| `dashboard` | `dashboards` | `view, run, edit, share, delete` | No | Yes |
| `agent` | `agents` | `view, edit, delete` | Yes (creator) | Yes |
| `model_config` | `model_configs` | `view, edit, delete` | Yes (creator) | Yes |
| `skill` | `skills` | `view, edit, delete` | Yes (creator) | Yes |
| `mcp_server` | `mcp_servers` | `view, edit, delete` | Yes (creator) | Yes |

**Permission subjects**: `user` (explicit UUID), `group` (custom group + Everyone group), `org_role` (`admin`, `editor`, `viewer`, `everyone`)

---

## Test Environment

- 1 org
- User A (admin) — org creator
- User B (editor) — no explicit ACL on anything initially
- User C (viewer) — no explicit ACL on anything initially

### Resource Creation Strategy

Since notebooks/connectors/dashboards/folders don't auto-create ACL entries, resources created by User A will have **no ACL entries** (admin bypass is the only access path). For agents/model_configs/skill/mcp_server, an ACL entry is auto-created for the creator (User A).

---

## Test Categories

| # | Category | Description |
|---|---|---|
| A | Admin bypass | Admin can view/edit/delete any resource regardless of ACL |
| B | Deny-by-default | No ACL entry = nobody except admin can access |
| C | Explicit user ACL | Grant `view` on a resource to User B, verify scoped access |
| D | Group-based ACL | Grant ACL to a group, verify member gets access |
| E | org_role ACL | Grant ACL to `org_role:editor`, verify editors but not viewers |
| F | Everyone ACL | Grant ACL to `org_role:everyone`, verify all org members |
| G | Folder inheritance | Grant ACL on a folder, verify child resources inherit |
| H | Restrictive entries | Explicit non-matching entry doesn't cascade to everyone |
| I | ACL "share" action | Who can modify ACL entries (test "share" and "manage") |
| J | List filtering | Verify list endpoints respect permissions (or don't) |

---

## Permission Matrix

For each resource type, test the following combinations:

### Baseline (no ACL entries on resource)

| Role | Action | Expected |
|---|---|---|
| Admin | view/edit/delete | ✅ Granted (admin bypass) |
| Editor | view | ❌ Denied (no ACL) |
| Viewer | view | ❌ Denied (no ACL) |

### Explicit user ACL (`user:UserB` with `view`)

| User | Action | Expected |
|---|---|---|
| User B | view | ✅ Granted (direct user entry) |
| User B | edit | ❌ Denied (no `edit` in actions) |
| User B | delete | ❌ Denied |
| User C | view | ❌ Denied (not in any matching ACL) |

### Group-based ACL (`group:MyGroup` with `view,edit`)

| User | Action | Expected |
|---|---|---|
| User B (in group) | view | ✅ Granted |
| User B (in group) | edit | ✅ Granted |
| User C (not in group) | view | ❌ Denied |

### org_role:everyone ACL

| User | Action | Expected |
|---|---|---|
| User B | view | ✅ Granted (everyone matches all) |
| User C | view | ✅ Granted (everyone matches all) |
| User B | edit | ❌ Denied (only `view` granted) |

### org_role:editor ACL

| User | Action | Expected |
|---|---|---|
| User B (editor) | view | ✅ Granted |
| User C (viewer) | view | ❌ Denied (role mismatch) |

### Folder inheritance

| Scenario | Action | Expected |
|---|---|---|
| ACL `view` on folder → notebook inside | User B views notebook | ✅ Inherited |
| ACL `view` on folder → notebook outside | User B views notebook | ❌ Not inherited |
| ACL `view` on parent → sub-folder → notebook | User B views notebook | ✅ Inherited through chain |

### ACL management ("share" / "manage")

| Who | Can set ACL on resource? | Expected |
|---|---|---|
| Admin | ✅ | Granted (admin bypass) |
| User with `share` action | ✅ | Explicit `share` on resource |
| User with `manage` on folder | ✅ | For folders via "manage" action |
| User without ACL | ❌ | Denied |

---

## Gap Analysis (Static Code Review)

Issues to document (not fix):

1. **Notebook/connector/dashboard creation doesn't seed ACL** — creator gets no automatic ACL entry
2. **`handleDeleteNotebook` has no ACL check** — only `RequireRole("editor")` at the router level
3. **`handleUpdateNotebook` has no explicit ACL check** — relies on `RequireRole("editor")` only
4. **Notebook/dashboard list endpoints don't filter by permission** — return all org resources
5. **Two permission models coexist** — Go `checkPermission` + SQL `permissionCheckSQL` in folder handlers
6. **Connector create/update/delete only checked by `RequireRole("admin")`** — no ACL check

---

## Test Procedure

1. Start dev stack: `docker compose -f docker-compose.dev.yml up -d`
2. Register users and create org via API (curl)
3. Create test resources as admin
4. For each test scenario:
   a. Set up required ACL entries via API
   b. Login as target user via agent-browser
   c. Navigate and attempt the action
   d. Capture screenshot + result
5. Compile results into report

---

## Deliverable

A single comprehensive markdown report containing:
- Exec summary of findings
- Permission matrix results (pass/fail per scenario)
- Static gap analysis with severity ratings
- Agent-browser evidence (screenshots in report)
- Prioritized recommendations
