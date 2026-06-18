# Permission Audit v2 — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a comprehensive Go test suite that validates all 150+ API endpoints against every permission scenario, catching IDOR and authorization bypass vulnerabilities.

**Architecture:** Reuse existing `setupTestServer`/`testJWT` infra. One shared fixture file creates 2 orgs, 7 users, groups, and standard resources. One test file per resource type tests all permission combinations. Tests are table-driven for maintainability.

**Tech Stack:** Go testing, `net/http/httptest`, `testify/assert`, real Postgres + Redis (via `task test`)

---

## Key Findings to Verify

The following potential issues were identified during code review and MUST be confirmed by tests:

| # | Issue | Location | Severity |
|---|-------|----------|----------|
| F1 | Schedule CRUD has no permission check (only org scoping) | `schedule_handlers.go:30-286` | **HIGH** |
| F2 | Cell execution checks `connector:use` but NOT `notebook:run` | `execute_handlers.go:33-109` | **HIGH** |
| F3 | New resources created by non-admin won't be accessible to creator | `notebook_handlers.go:32` | **MEDIUM** |
| F4 | `adminModeFromContext` defaults to `true` on nil/missing context | `middleware.go:20-30` | **LOW** |
| F5 | `no_access` role not enforced anywhere | `permissions.go:192-207` | **LOW** |
| F6 | Removed user's JWT remains valid until expiry | No middleware rechecks membership | **LOW** |
| F7 | Schedule list/create only checks org scoping, not notebook permission | `schedule_handlers.go:30-109` | **HIGH** |
| F8 | Cell version history / snapshots — verify notebook:view check | `cell_history.go:101-262` | **MEDIUM** |
| F9 | Attachment upload/download — verify notebook:edit check | `attachment_handlers.go:13-183` | **MEDIUM** |
| F10 | Dashboard sharing (POST /dashboards/{id}/share) — verify dashboard:share | `dashboard_handlers.go:569` | **MEDIUM** |
| F11 | Agent session creation — verify agent:view on parent agent | `agent_handlers.go:349` | **MEDIUM** |
| F12 | Public dashboard access by token — verify token-only, no JWT bypass | `dashboard_handlers.go:601` | **MEDIUM** |

---

### Task 1: Shared Fixtures

**Files:**
- Create: `internal/api/permissions_audit_test.go`

**Step 1: Write the fixture setup**

```go
package api

import (
    "context"
    "fmt"
    "net/http/httptest"
    "testing"
    "time"
    
    "github.com/stretchr/testify/require"
)

type auditFixtures struct {
    srv        *Server
    tokens     map[string]string   // userKey -> JWT
    userIDs    map[string]string   // userKey -> UUID
    OrgA       orgFixtures
    OrgB       orgFixtures
}

type orgFixtures struct {
    OrgID      string
    GroupIDs   map[string]string
    Notebooks  resourceSet
    Folders    resourceSet
    Connectors resourceSet
    Dashboards resourceSet
    Agents     resourceSet
    ModelConfigs resourceSet
    Skills     resourceSet
    MCPServers resourceSet
}

type resourceSet struct {
    NoACL      string   // resource ID, no ACL entries — deny-by-default test
    UserACL    string   // resource ID, user:aliceA -> [view]
    GroupACL   string   // resource ID, group:engineers -> [view,edit]
    EveryoneACL string  // resource ID, org_role:everyone -> [view]
}

func setupAuditTest(t *testing.T) *auditFixtures {
    t.Helper()
    srv := setupTestServer(t)
    ctx := context.Background()
    f := &auditFixtures{
        srv:    srv,
        tokens: make(map[string]string),
        userIDs: make(map[string]string),
    }
    
    // 1. Create org A
    adminAToken := registerAndGetToken(t, srv,
        fmt.Sprintf("admin-a-%d@test.com", time.Now().UnixNano()),
        "Org A")
    f.tokens["adminA"] = adminAToken
    f.userIDs["adminA"] = extractUserID(t, srv, adminAToken)
    f.OrgA.OrgID = extractOrgID(t, srv, adminAToken)
    
    // 2. Create users aliceA, bobA, carolA in Org A (via SQL)
    // ... (see existing patterns from testhelpers_test.go)
    
    // 3. Create org B with adminB, eveB
    // ...
    
    // 4. Create groups: engineers (bobA member)
    // ...
    
    // 5. Create resources of each type with and without ACLs
    // ...
    
    // 6. Create admin-mode-off variants
    f.tokens["adminAModeOff"] = f.tokens["adminA"]  // same JWT, different header
    
    return f
}
```

The fixture must:
- Create 2 orgs with separate resources
- Create 7 users with known IDs and tokens
- Create 1 group (`engineers`) with `bobA` as member
- Create 4 variants of each resource type (no ACL, user ACL, group ACL, everyone ACL)
- Create helper: `f.Request(userKey, method, path, body)` that returns an authenticated request
- Create helper: `f.Do(userKey, method, path, body)` that returns `(code, body)`

**Step 2: Run compile check**

Run: `cd internal/api && go vet ./...`
Expected: No errors (may need to iteratively fix compilation issues)

**Step 3: Write helper functions**

Add to `permissions_audit_test.go`:

```go
func (f *auditFixtures) Request(t *testing.T, userKey, method, path string, body any) *http.Request {
    req := httptest.NewRequest(method, path, bodyReader(body))
    req.Header.Set("Authorization", "Bearer "+f.tokens[userKey])
    if strings.HasSuffix(userKey, "ModeOff") {
        req.Header.Set("X-HNB-Admin-Mode", "false")
    }
    return req
}

func (f *auditFixtures) Do(t *testing.T, userKey, method, path string, body any) (int, string) {
    rec := httptest.NewRecorder()
    f.srv.ServeHTTP(rec, f.Request(t, userKey, method, path, body))
    return rec.Code, rec.Body.String()
}
```

**Step 4: Run compile check**

Run: `cd internal/api && go vet ./...`
Expected: No errors

---

### Task 2: Middleware Tests

**Files:**
- Create: `internal/api/permissions_audit_middleware_test.go`

**Step 1: Write tests for requirePermission middleware**

Test that the middleware function itself works correctly by testing a known route:

```go
func TestRequirePermission_Middleware(t *testing.T) {
    f := setupAuditTest(t)
    
    // Pick a route protected by requirePermission: GET /connectors/{id}
    // Test all 9 scenarios from the matrix
    
    t.Run("admin bypass", func(t *testing.T) {
        code, _ := f.Do(t, "adminA", "GET", "/api/v1/connectors/"+f.OrgA.Connectors.NoACL, nil)
        assert.Equal(t, 200, code)
    })
    // ... more cases
}
```

**Step 2: Write tests for RequireRole("admin") middleware**

```go
func TestRequireRole_Admin_Middleware(t *testing.T) {
    f := setupAuditTest(t)
    
    // Pick a route protected by RequireRole("admin"): POST /api/v1/groups
    // adminA → 201, aliceA → 403, adminB → 403
}
```

**Step 3: Write tests for RequirePlatformAdmin middleware**

```go
func TestRequirePlatformAdmin_Middleware(t *testing.T) {
    f := setupAuditTest(t)
    // Test /api/v1/admin/orgs
}
```

**Step 4: Run tests**

Run: `cd internal/api && go test -run 'TestRequirePermission_Middleware|TestRequireRole_Admin_Middleware|TestRequirePlatformAdmin_Middleware' -v -count=1`
Expected: Tests pass

---

### Task 3: Schedule Permission Tests (HIGH priority — known vulnerability)

**Files:**
- Create: `internal/api/permissions_audit_schedule_test.go`

**Step 1: Write schedule CRUD permission tests**

Test that `aliceA` (no permission on notebook) CAN still create/list/get/delete/update schedules (expected: should be BLOCKED but current code allows it).

```go
func TestSchedule_Create_NoNotebookPermission(t *testing.T) {
    f := setupAuditTest(t)
    // aliceA has no ACL on NotebookNoACL
    code, body := f.Do(t, "aliceA", "POST",
        "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/schedules",
        map[string]string{"cron_expression": "0 0 * * *"})
    // EXPECTED TO BE 403 (vulnerability: this will likely be 201)
    t.Logf("Schedule create by unauthorized user: %d %s", code, body)
}
```

**Step 2: Write cross-org schedule tests**

Test that `eveB` (Org B) cannot create/list/get/delete/update schedules on Org A notebooks.

```go
func TestSchedule_CrossOrg(t *testing.T) {
    f := setupAuditTest(t)
    code, _ := f.Do(t, "eveB", "POST",
        "/api/v1/notebooks/"+f.OrgA.Notebooks.NoACL+"/schedules",
        map[string]string{"cron_expression": "0 0 * * *"})
    assert.Equal(t, 403, code, "cross-org schedule create should be forbidden")
}
```

**Step 3: Run tests**

Run: `cd internal/api && go test -run 'TestSchedule_' -v -count=1`
Expected: Tests reveal missing permission checks

---

### Task 4: Cell Execution Permission Tests

**Files:**
- Create: `internal/api/permissions_audit_execute_test.go`

**Step 1: Write cell execution permission tests**

Test that `aliceA` (who has `connector:use` but NOT `notebook:run`) can execute cells.

```go
func TestExecuteCell_NoNotebookRunPermission(t *testing.T) {
    f := setupAuditTest(t)
    // Create a cell in NotebookNoACL
    // Grant aliceA connector:use on a connector
    // Execute cell as aliceA — should fail but may succeed
}
```

**Step 2: Write cross-org execution test**

```go
func TestExecuteCell_CrossOrg(t *testing.T) {
    f := setupAuditTest(t)
    // eveB tries to execute a cell in Org A notebook
}
```

**Step 3: Run tests**

Run: `cd internal/api && go test -run 'TestExecuteCell_' -v -count=1`

---

### Task 5: Notebook & Cell Permission Tests

**Files:**
- Create: `internal/api/permissions_audit_notebook_test.go`

**Step 1: Write notebook CRUD permission tests**

Test all notebook endpoints with the full matrix:
- `GET /notebooks` (list)
- `GET /notebooks/{id}` (get)
- `POST /notebooks` (create)
- `PUT /notebooks/{id}` (edit)
- `DELETE /notebooks/{id}` (delete)
- `GET /notebooks/{id}/permissions`
- `GET /notebooks/{id}/export`
- `POST /notebooks/import`

Each tested as: adminA, adminAModeOff, aliceA, bobA, carolA, adminB, eveB

**Step 2: Write cell CRUD permission tests**

- `POST /notebooks/{nb_id}/cells` (create)
- `PUT /notebooks/{nb_id}/cells/{cell_id}` (update)
- `DELETE /notebooks/{nb_id}/cells/{cell_id}` (delete)
- `POST /notebooks/{nb_id}/cells/{cell_id}/duplicate`

**Step 3: Write cell history/snapshot tests**

- `GET /notebooks/{nb_id}/cells/{cell_id}/versions`
- `POST /notebooks/{nb_id}/cells/{cell_id}/versions/{vid}/restore`
- `POST /notebooks/{nb_id}/snapshots`
- `GET /notebooks/{nb_id}/snapshots`
- `POST /notebooks/{nb_id}/snapshots/{sid}/restore`

**Step 4: Run tests**

Run: `cd internal/api && go test -run 'TestNotebook_|TestCell_' -v -count=1`

---

### Task 6: Folder Permission Tests

**Files:**
- Create: `internal/api/permissions_audit_folder_test.go`

**Step 1: Write folder CRUD tests**

- `GET /folders` (list root)
- `GET /folders/{id}` (get contents)
- `POST /folders` (create)
- `PUT /folders/{id}` (edit)
- `DELETE /folders/{id}` (delete)
- `GET /folders/{id}/ancestors`
- `POST /users/me/home`

**Step 2: Write folder inheritance tests**

- Grant ACL on parent folder
- Verify child resources inherit
- Verify resources outside hierarchy don't inherit

**Step 3: Run tests**

Run: `cd internal/api && go test -run 'TestFolder_' -v -count=1`

---

### Task 7: Connector Permission Tests

**Files:**
- Create: `internal/api/permissions_audit_connector_test.go`

**Step 1: Write connector tests**

- `GET /connectors` (list — any auth, in-handler filter by permission)
- `GET /connectors/{id}` (view — requirePermission middleware)
- `POST /connectors` (admin-only)
- `PUT /connectors/{id}` (admin-only)
- `DELETE /connectors/{id}` (admin-only)
- `POST /connectors/test` (any auth)
- `POST /connectors/{id}/test` (any auth — verify checks connector:use or not)
- `GET /connectors/{id}/schema` (any auth)
- `GET /connectors/{id}/databases` (any auth)
- `PUT /connectors/{id}/default` (admin-only)

**Step 2: Run tests**

Run: `cd internal/api && go test -run 'TestConnector_' -v -count=1`

---

### Task 8: Dashboard Permission Tests

**Files:**
- Create: `internal/api/permissions_audit_dashboard_test.go`

**Step 1: Write dashboard tests**

- `GET /dashboards` (list)
- `GET /dashboards/{id}` (get — in-handler permission check)
- `POST /dashboards` (create)
- `PUT /dashboards/{id}` (edit — requirePermission)
- `DELETE /dashboards/{id}` (delete — requirePermission)
- `POST /dashboards/{id}/widgets` (add widget)
- `PUT /dashboards/{id}/widgets/{wid}` (update widget)
- `DELETE /dashboards/{id}/widgets/{wid}` (delete widget)
- `POST /dashboards/{id}/share` (share — verify dashboard:share check)
- `GET /dashboards/{id}/permissions`
- `GET /public/dashboards/{token}` (public, token-scoped)

**Step 2: Run tests**

Run: `cd internal/api && go test -run 'TestDashboard_' -v -count=1`

---

### Task 9: Agent Permission Tests

**Files:**
- Create: `internal/api/permissions_audit_agent_test.go`

**Step 1: Write agent tests**

- `GET /agents` (list)
- `GET /agents/{id}` (get)
- `POST /agents` (create)
- `PUT /agents/{id}` (edit — requirePermission)
- `DELETE /agents/{id}` (delete — requirePermission)
- `POST /agents/{id}/session` (create session — F11)
- `GET /agents/{id}/sessions` (list sessions)
- `GET /sessions/{sid}` (get session)
- `GET /sessions/{sid}/messages` (get messages)
- `PATCH /sessions/{sid}/title` (update title)
- `GET /agents/stats` (admin-only)
- `GET /agents/{id}/stats` (admin-only)

**Step 2: Run tests**

Run: `cd internal/api && go test -run 'TestAgent_' -v -count=1`

---

### Task 10: Model Config, Skill, MCP Server Tests

**Files:**
- Create: `internal/api/permissions_audit_modelconfig_test.go`
- Create: `internal/api/permissions_audit_skill_test.go`
- Create: `internal/api/permissions_audit_mcp_test.go`

**Step 1: Write model config tests**

- `GET /model-configs` (list)
- `GET /model-configs/{id}` (view — requirePermission)
- `POST /model-configs` (create)
- `PUT /model-configs/{id}` (edit — requirePermission)
- `DELETE /model-configs/{id}` (delete — requirePermission)
- `POST /model-configs/{id}/test` (test config)

**Step 2: Write skill tests**

- `GET /skills` (list)
- `GET /skills/{id}` (view — requirePermission)
- `POST /skills` (create — admin-only)
- `PUT /skills/{id}` (edit — requirePermission)
- `DELETE /skills/{id}` (delete — requirePermission)

**Step 3: Write MCP server tests**

- `GET /mcp-servers` (list)
- `GET /mcp-servers/{id}` (get)
- `POST /mcp-servers` (create — admin-only)
- `PUT /mcp-servers/{id}` (update — admin-only)
- `DELETE /mcp-servers/{id}` (delete — admin-only)
- `POST /mcp-servers/{id}/test` (test)

**Step 4: Run tests**

Run: `cd internal/api && go test -run 'TestModelConfig_|TestSkill_|TestMCPServer_' -v -count=1`

---

### Task 11: Attachment Permission Tests

**Files:**
- Create: `internal/api/permissions_audit_attachment_test.go`

**Step 1: Write attachment tests**

- `POST /notebooks/{nb_id}/attachments` (upload — verify notebook:edit check)
- `GET /notebooks/{nb_id}/attachments` (list)
- `GET /attachments/{id}` (download)
- `DELETE /attachments/{id}` (delete)

**Step 2: Run tests**

Run: `cd internal/api && go test -run 'TestAttachment_' -v -count=1`

---

### Task 12: Admin Routes Tests

**Files:**
- Create: `internal/api/permissions_audit_admin_test.go`

**Step 1: Write platform admin tests**

- `GET /api/v1/admin/orgs`
- `GET /api/v1/admin/users`
- `PUT /api/v1/admin/users/{id}`
- `GET /api/v1/admin/sso/providers`
- `POST /api/v1/admin/sso/providers`
- `PUT /api/v1/admin/sso/providers/{id}`
- `DELETE /api/v1/admin/sso/providers/{id}`
- `POST /api/v1/admin/sso/providers/{id}/test`

**Step 2: Write org admin management tests**

- Members: list, invite, update role, remove
- Audit log (GET /api/v1/audit)
- MOTD CRUD
- Templates CRUD
- SSO admin routes
- Org-level MCP server CRUD

**Step 3: Run tests**

Run: `cd internal/api && go test -run 'TestAdmin_|TestOrgAdmin_' -v -count=1`

---

### Task 13: Special Endpoint Tests

**Files:**
- Create: `internal/api/permissions_audit_special_test.go`

**Step 1: Write public endpoint tests**

- `GET /health` — no auth, should always work
- `GET /swagger.json` — no auth
- `GET /docs` — no auth
- `POST /api/v1/auth/login` — no auth
- `POST /api/v1/auth/register` — no auth
- `GET /api/v1/auth/oidc/{provider}` — no auth
- `GET /api/v1/auth/sso-providers` — no auth
- `GET /api/v1/public/motd` — no auth

**Step 2: Write authenticated-only (no permission check) endpoint tests**

- `GET /api/v1/users/me`
- `PUT /api/v1/users/me`
- `GET /api/v1/home`
- `GET /api/v1/recent`
- `GET /api/v1/templates`
- `GET /api/v1/groups`
- `GET /api/v1/groups/{id}/members`
- `GET /api/v1/members`
- `GET /api/v1/motd`

**Step 3: Write WebSocket endpoint tests** (difficult — may need manual verification)

- Verify auth is required for WebSocket connections

**Step 4: Run tests**

Run: `cd internal/api && go test -run 'TestSpecial_|TestPublic_|TestWebSocket_' -v -count=1`

---

### Task 14: Run Full Test Suite & Compile Report

**Step 1: Run all permission audit tests**

Run: `cd internal/api && go test -run 'TestRequire|TestSchedule_|TestExecuteCell_|TestNotebook_|TestCell_|TestFolder_|TestConnector_|TestDashboard_|TestAgent_|TestModelConfig_|TestSkill_|TestMCPServer_|TestAttachment_|TestAdmin_|TestSpecial_|TestPublic_' -v -count=1 2>&1 | tee /tmp/permission-audit-results.txt`

**Step 2: Parse results and categorize findings**

Categorize each test result:
- ✅ PASS (expected behavior confirmed)
- ❌ FAIL (expected behavior NOT confirmed — potential vulnerability)
- ⚠️ BUG CONFIRMED (test expected failure and got it = bug confirmed)

**Step 3: Write vulnerability report**

Write `docs/plans/2026-06-17-permission-audit-report.md` with:
- Executive summary
- Confirmed vulnerabilities (test evidence with code references)
- Test results per endpoint
- Severity ratings
- Remediation recommendations

---

## Execution Order

| Task | Description | Dependencies | Est. Size |
|------|-------------|--------------|-----------|
| 1 | Shared fixtures | None | Large |
| 2 | Middleware tests | Task 1 | Small |
| 3 | Schedule tests (HIGH priority) | Task 1 | Medium |
| 4 | Cell execution tests (HIGH priority) | Task 1 | Medium |
| 5 | Notebook & cell tests | Task 1 | Large |
| 6 | Folder tests | Task 1 | Medium |
| 7 | Connector tests | Task 1 | Medium |
| 8 | Dashboard tests | Task 1 | Medium |
| 9 | Agent tests | Task 1 | Medium |
| 10 | Model config / skill / MCP tests | Task 1 | Medium |
| 11 | Attachment tests | Task 1 | Small |
| 12 | Admin route tests | Task 1 | Medium |
| 13 | Special endpoint tests | Task 1 | Small |
| 14 | Report compilation | Tasks 2-13 | Small |
