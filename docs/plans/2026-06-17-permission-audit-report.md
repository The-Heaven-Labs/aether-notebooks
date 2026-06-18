# Permission Audit v2 — Vulnerability Report

**Date**: 2026-06-17
**Method**: Automated Go test suite against real Postgres database, 2 orgs, 7 users, 40 test resources
**Tests Run**: 505 (all passing — documenting both success and vulnerability cases)

---

## Executive Summary

| Severity | Count | Issues |
|----------|-------|--------|
| **CRITICAL** | 2 | Schedule CRUD (5 endpoints), cell execution missing `notebook:run` check |
| **HIGH** | 5 | Agent session creation, attachment list/delete, model-config test, export notebook |
| **MEDIUM** | 4 | Folder ancestors, notebook permissions cross-org, dashboard permissions cross-org, MCP server test |
| **LOW** | 3 | `X-HNB-Admin-Mode` defaults to `true`, `no_access` role not enforced, admin bypass is cross-org |
| **INFO** | 2 | `org_role:editor/viewer` deprecated, no ACL seeding on resource creation |

---

## Critical Vulnerabilities

### C1: Schedule CRUD — No Permission Checks (5 endpoints)

**Files**: `schedule_handlers.go:30-286`
**Tests**: `permissions_audit_schedule_test.go` (13 tests)

**Issue**: All 5 schedule endpoints only scope queries by `org_id` via SQL. They never call `checkPermission()` to verify the user has `notebook:edit` or `notebook:run` on the associated notebook.

| Endpoint | What aliceA (no notebook ACL) can do |
|----------|--------------------------------------|
| `POST /notebooks/{id}/schedules` | Create schedule on any notebook in org |
| `GET /notebooks/{id}/schedules` | List schedules on any notebook in org |
| `GET /schedules/{id}` | View any schedule in org |
| `PUT /schedules/{id}` | Modify any schedule in org |
| `DELETE /schedules/{id}` | Delete any schedule in org |

**Impact**: Any org member can create, view, modify, or delete scheduled notebook executions on any notebook in their org, regardless of notebook-level ACLs.

---

### C2: Cell Execution Missing `notebook:run` Check

**File**: `execute_handlers.go:33-109`
**Test**: `permissions_audit_execute_test.go`

**Issue**: `handleExecuteCell` checks `connector:use` permission but NOT `notebook:run`. A user with access to a connector can execute cells in any notebook that uses that connector, even without `notebook:run` permission.

```
aliceA (no ACL on notebook) with connector:use permission → cell executes successfully (200)
```

**Impact**: Users can execute SQL against data sources via notebooks they shouldn't be able to run.

---

## High Vulnerabilities

### H1: Agent Session Creation — No Permission Check

**File**: `agent_handlers.go:349` (`handleCreateSession`)
**Test**: `permissions_audit_agent_test.go`

**Issue**: `POST /api/v1/agents/{id}/session` is `authMW` only. `handleCreateSession` never calls `checkPermission` on the agent. Any authenticated org member can create AI agent sessions on any agent in their org.

```
aliceA (no ACL on agent) → agent session created (201)
```

**Impact**: Unauthorized AI agent usage, potential cost exposure.

---

### H2: Attachment List — No Notebook Permission Check

**File**: `attachment_handlers.go:141` (`handleListAttachments`)
**Test**: `permissions_audit_attachment_test.go`

**Issue**: Lists all attachments for a notebook. Only scoped by `org_id` via SQL join, no `checkPermission` for `notebook:view`.

```
aliceA (no ACL on notebook) → lists all attachments (200)
```

**Impact**: Information disclosure — users can see filenames, upload dates, and file sizes of attachments in notebooks they can't access.

---

### H3: Attachment Delete — No Notebook Permission Check

**File**: `attachment_handlers.go:183` (`handleDeleteAttachment`)
**Test**: `permissions_audit_attachment_test.go`

**Issue**: Deletes an attachment. Only scoped by `org_id` via SQL join on the attachment → notebook chain, no `checkPermission` for `notebook:edit`.

```
aliceA (no ACL on notebook) → deletes attachment (200)
```

**Impact**: Data destruction — users can delete files from notebooks they shouldn't be able to edit.

---

### H4: Model Config Test — No Permission Check

**File**: `model_config_handlers.go:247` (`handleTest`)
**Test**: `permissions_audit_modelconfig_test.go`

**Issue**: `POST /api/v1/model-configs/{id}/test` is `authMW` only. No `requirePermission` or `checkPermission` call. Any org member can test any model config.

```
aliceA (no ACL on model config) → test call goes through (502 due to bad key, but call attempted)
```

**Impact**: Unauthorized LLM API calls via model config testing, potential cost exposure.

---

### H5: Notebook Export — No Permission Check

**File**: `notebook_handlers.go:440` (`handleExportNotebook`)
**Test**: `permissions_audit_notebook_test.go`

**Issue**: `GET /notebooks/{id}/export` has no permission check. Only scoped by `org_id` via SQL. Any org member can export any notebook.

```
aliceA (no ACL on notebook) → exports notebook with full content (200)
```

**Impact**: Data exfiltration — users can download notebook contents (queries, results, markdown) for notebooks they shouldn't access.

---

## Medium Vulnerabilities

### M1: Folder Ancestors — No Permission Check

**File**: `folder_handlers.go:701` (`handleGetFolderAncestors`)
**Test**: `permissions_audit_folder_test.go`

**Issue**: `GET /folders/{id}/ancestors` has no permission check. Only scoped by `org_id`.

```
aliceA (no ACL on folder) → sees ancestor chain (200)
```

**Impact**: Information disclosure — users can map the folder hierarchy structure.

---

### M2: Cross-Org Admin Bypass — `RequireRole("admin")` Not Org-Scoped

**Files**: Multiple — `middleware.go:141` (`RequireRole`)
**Tests**: Multiple (connector, MCP, agent stats, dashboard permissions)

**Issue**: `RequireRole("admin")` middleware only checks `claims.Role == "admin"`. It doesn't verify the user is admin of the **resource's** org. adminB (admin of Org B) passes `RequireRole` checks on Org A resources. The handlers return 404 for CRUD operations (org-scoped SQL), but some info-disclosure endpoints return data.

**Confirmed disclosures**:
- `GET /api/v1/dashboards/{id}/permissions` — adminB gets permission flags for Org A dashboard (200)
- `GET /api/v1/notebooks/{id}/permissions` — adminB gets permission state for Org A notebook (200)

**Impact**: Cross-org information disclosure of ACL state for dashboards and notebooks.

---

### M3: MCP Server Test — No Permission Check

**File**: `mcp_server_handlers.go:256` (`handleTestMCPServer`)
**Test**: `permissions_audit_mcp_test.go`

**Issue**: `POST /api/v1/mcp-servers/{id}/test` is `authMW` only. No `RequireRole("admin")` check despite being an admin-only CRUD resource. MCP server create/update/delete are admin-only, but test is open to any authenticated user.

```
aliceA (no ACL on MCP server) → test call succeeds (200)
```

**Impact**: Any org member can trigger MCP server testing, which may execute arbitrary commands on the MCP server host.

---

## Low Vulnerabilities

### L1: `X-HNB-Admin-Mode: false` Works but Default is `true`

**File**: `middleware.go:20-30` (`adminModeFromContext`)
**Test**: `permissions_audit_middleware_test.go`

**Issue**: `adminModeFromContext` returns `true` (admin mode ON) when the context key is missing or invalid, or when `X-HNB-Admin-Mode` header is absent or any value other than `"false"`. Tested and confirmed:

```
adminA with default headers → bypass ON (200 on NoACL)
adminA with X-HNB-Admin-Mode: false → bypass OFF (403 on NoACL)
adminA with X-HNB-Admin-Mode: false + EveryoneACL → 200 (still in org)
```

**Risk**: Low, since the normal flow always sets the context key. But nil context or missing key accidentally enables admin bypass.

---

### L2: `no_access` Role Not Enforced

**File**: `permissions.go:192-207` (`matchesUser`)
**Tests**: None created (role not used in test fixtures)

**Issue**: The `no_access` role is defined in `models/org.go:33` and the DB schema but is never checked anywhere. A user with `no_access` role who somehow has an ACL entry can still access resources.

---

### L3: Admin Bypass Is Cross-Org

**File**: `permissions.go:64-66`
**Tests**: Multiple

**Issue**: `checkPermission` checks `orgRole == "admin"` without verifying which org the user is admin of. adminB (Org B admin) passes the bypass check on Org A resources. The handler-level org scoping in SQL prevents data access in most cases, but info-disclosure endpoints leak data.

---

## Informational Findings

### I1: `org_role:editor`/`org_role:viewer` Deprecated

**File**: `permissions.go:203-204`
**Evidence**: Previous audit (June 15) confirmed these are non-functional.

**Issue**: ACL entries with `subject_type: 'org_role'` and `subject_id: 'editor'` or `'viewer'` are silently ignored. Only `subject_id: 'everyone'` is matched.

---

### I2: No ACL Seeding on Resource Creation

**Files**: `notebook_handlers.go:32`, `folder_handlers.go:768`, `dashboard_handlers.go:47`, `connector_handlers.go:36`

**Issue**: New resources created by non-admin users will be inaccessible to their creators unless an ancestor folder grants access. In the current test suite, adminA creates all resources (bypass always works), so this issue wasn't triggered, but it's a design flaw.

---

## Endpoints Working Correctly

| Category | Endpoints | Notes |
|----------|-----------|-------|
| **Middleware-gated CRUD** | All `requirePermission` routes (16) | Notebook edit/delete, folder view/edit/delete, dashboard edit/delete, connector view, agent edit/delete, model_config view/edit/delete, skill view/edit/delete |
| **Platform admin routes** | All 8 `/api/v1/admin/*` routes | Properly gated by `RequirePlatformAdmin` |
| **In-handler checks** | Notebook get, cell CRUD, connector schema/test | All check `checkPermission` correctly |
| **Folder inheritance** | Parent→child→notebook chain | Works correctly |
| **Group ACLs** | Group-based permission subjects | bobA in engineers group gets access |
| **Cross-org isolation** | Most CRUD handlers | Return 404 via org-scoped SQL |
| **Public endpoints** | Health, swagger, login, register, etc. | Work without auth |
| **Auth enforcement** | All authenticated endpoints | Return 401 without valid token |

---

## Severity Assessment Summary

```
CRITICAL: Schedule CRUD (5 endpoints) + cell execution → immediate fix needed
HIGH:     Agent sessions, attachments (2), model-config test, export → fix this week
MEDIUM:   Folder ancestors, cross-org info disclosure (2), MCP test → fix next sprint
LOW:      Admin mode defaults, no_access role, cross-org bypass → backlog
INFO:     Deprecated roles, no ACL seeding on create → document
```

---

## Remediation Recommendations

### Immediate (Critical)

1. **Schedule handlers**: Add `checkPermission(ctx, ..., "notebook", nbID, "edit")` to create/update/delete/list schedule handlers.
2. **Cell execution**: Add `checkPermission(ctx, ..., "notebook", nbID, "run")` to `handleExecuteCell`.

### This Week (High)

3. **Agent session create**: Add `checkPermission(ctx, ..., "agent", agentID, "view")` to `handleCreateSession`.
4. **Attachment list**: Add `checkPermission(ctx, ..., "notebook", nbID, "view")` to `handleListAttachments`.
5. **Attachment delete**: Add `checkPermission(ctx, ..., "notebook", nbID, "edit")` to `handleDeleteAttachment`.
6. **Model config test**: Add `requirePermission("model_config", "id", "edit")` middleware or `checkPermission` in handler.
7. **Notebook export**: Add `checkPermission(ctx, ..., "notebook", nbID, "view")` to `handleExportNotebook`.

### Next Sprint (Medium)

8. **Folder ancestors**: Add `requirePermission("folder", "id", "view")` to `handleGetFolderAncestors`.
9. **MCP server test**: Add `RequireRole("admin")` middleware.
10. **Dashboard/notebook permissions endpoints**: Add org-ownership check before returning permission state.
11. **Scope `RequireRole` middleware**: Consider adding `claims.OrgID == resourceOrgID` check.

---

## Test File Inventory

| File | Tests | Size |
|------|-------|------|
| `permissions_audit_test.go` | Fixtures | 14K |
| `permissions_audit_middleware_test.go` | 20 | 4K |
| `permissions_audit_schedule_test.go` | 19 | 7K |
| `permissions_audit_execute_test.go` | — | 5K |
| `permissions_audit_notebook_test.go` | — | 19K |
| `permissions_audit_folder_test.go` | — | 13K |
| `permissions_audit_connector_test.go` | 40 | 10K |
| `permissions_audit_dashboard_test.go` | 35 | 14K |
| `permissions_audit_agent_test.go` | 39 | 14K |
| `permissions_audit_modelconfig_test.go` | 26 | 9K |
| `permissions_audit_skill_test.go` | 22 | 7K |
| `permissions_audit_mcp_test.go` | 22 | 7K |
| `permissions_audit_attachment_test.go` | 20 | 8K |
| `permissions_audit_admin_test.go` | 14 | 3K |
| `permissions_audit_special_test.go` | 50 | 9K |
| **Total** | **~505** | **154K** |
