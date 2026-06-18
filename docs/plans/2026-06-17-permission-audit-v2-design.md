# Permission System: Comprehensive Audit v2

**Date**: 2026-06-17
**Scope**: Full IDOR + permission audit via automated Go tests. Supersedes `2026-06-15-permission-review-*` (which used curl + agent-browser and had significant coverage gaps).

---

## Background

The June 15 review covered baseline admin bypass, deny-by-default, explicit user ACL, group ACL, org_role ACL, folder inheritance, and ACL management. It found:
- Admin bypass ✅, deny-by-default ✅, user/group ACL ✅
- `org_role:editor/viewer` ACL entries non-functional (deprecated by design)
- UI sidebar shows unauthorized items ⚠️

**Gaps in previous review:**
| Gap | Impact |
|-----|--------|
| No cross-org testing | Misses IDOR between orgs |
| No Go tests | No regression suite |
| No cell CRUD/execution testing | Missing permission checks in cell/schedule endpoints |
| No attachment/snapshot/history testing | Same |
| No platform admin route testing | Platform admin routes untested |
| No WebSocket/internal Yjs testing | Risk endpoints skipped |
| No schedule CRUD testing | All 5 schedule endpoints have zero permission checks |
| No admin-mode-header edge case testing | `X-HNB-Admin-Mode: false` behavior untested |
| No test for no-ACL-on-create fallout | Whether creators can access own resources |

---

## Test Infrastructure

File: `internal/api/permissions_audit_test.go` (shared fixtures)
Resource-specific tests in separate files.

### Users

| ID | Org | Role | Subject | Purpose |
|----|-----|------|---------|---------|
| `adminA` | Org A | `admin` | N/A | Baseline — admin bypass |
| `aliceA` | Org A | `member` | Direct `user` ACL | ACL subject tests |
| `bobA` | Org A | `member` | `engineers` group member | Group ACL tests |
| `carolA` | Org A | `member` | None | Negative testing (deny-by-default) |
| `adminB` | Org B | `admin` | N/A | Cross-org IDOR |
| `eveB` | Org B | `member` | None | Cross-org IDOR |
| `platAdmin` | Platform | `admin` (platform) | N/A | Platform admin routes |

### Groups

| Group | Org | Members |
|-------|-----|---------|
| `engineers` | Org A | bobA |
| `Everyone` | Org A (implicit) | All members |

### Resources per org

For each resource type, create:
- **Resource with no ACL** (tests deny-by-default for non-admin)
- **Resource with user ACL** `aliceA:view` (tests explicit user grant)
- **Resource with group ACL** `engineers:view,edit` (tests group grant)
- **Resource with everyone ACL** `org_role:everyone:view` (tests everyone grant)

### Admin mode variants for adminA

- `adminA` with default headers (admin mode ON — bypass enabled)
- `adminA` with `X-HNB-Admin-Mode: false` (admin mode OFF — subject to ACL)

---

## Test Matrix

### A. Middleware-Protected Endpoints (requirePermission)

Test the middleware itself once, then verify each route registration. For each:

| Scenario | Expected |
|----------|----------|
| adminA (mode ON) → action | 200 (bypass) |
| adminA (mode OFF, no ACL) | 403 |
| aliceA (user ACL, action granted) | 200 |
| aliceA (user ACL, action NOT granted) | 403 |
| bobA (group ACL, action granted) | 200 |
| bobA (group ACL, action NOT granted) | 403 |
| carolA (no ACL) | 403 |
| adminB (cross-org) | 403/404 |
| eveB (cross-org) | 403/404 |

Routes covered:
- `DELETE /notebooks/{id}` (notebook, delete)
- `PUT /notebooks/{id}` (notebook, edit)
- `PUT /dashboards/{id}` (dashboard, edit)
- `DELETE /dashboards/{id}` (dashboard, delete)
- `GET /connectors/{id}` (connector, view) — **Note: LIST connectors is NOT middleware-protected**
- `GET /folders/{id}` (folder, view)
- `PUT /folders/{id}` (folder, edit)
- `DELETE /folders/{id}` (folder, delete)
- `PUT /agents/{id}` (agent, edit)
- `DELETE /agents/{id}` (agent, delete)
- `GET /model-configs/{id}` (model_config, view)
- `PUT /model-configs/{id}` (model_config, edit)
- `DELETE /model-configs/{id}` (model_config, delete)
- `GET /skills/{id}` (skill, view)
- `PUT /skills/{id}` (skill, edit)
- `DELETE /skills/{id}` (skill, delete)

### B. Org-Admin-Only Endpoints (RequireRole("admin"))

Test middleware once. For each:

| Scenario | Expected |
|----------|----------|
| adminA → action | 200 |
| aliceA → action | 403 |
| adminB → action | 403 (cross-org not admin in this org) |
| carolA → action | 403 |

### C. Platform-Admin-Only Endpoints (RequirePlatformAdmin)

| Scenario | Expected |
|----------|----------|
| platAdmin → action | 200 |
| adminA → action | 403 |
| adminB → action | 403 |

### D. Authenticated-Only Endpoints (IDOR Risk Zone)

These have **no permission middleware** — test for missing checks:

| Scenario | Expected |
|----------|----------|
| adminA → own org resource | 200 |
| aliceA → same org resource | 200 (or 403 depending on handler) |
| adminB → Org A resource | 403/404 (cross-org isolation) |
| eveB → Org A resource | 403/404 (cross-org isolation) |

#### Endpoints with in-handler checkPermission (verify correct)
- `POST /notebooks` — verify org scoping
- `POST /notebooks/{notebook_id}/cells` — checks `notebook:create` ✅ (verify)
- `PUT /notebooks/{notebook_id}/cells/{cell_id}` — checks `notebook:edit` ✅ (verify)
- `DELETE /notebooks/{notebook_id}/cells/{cell_id}` — checks `notebook:edit` ✅ (verify)
- `POST /notebooks/{notebook_id}/cells/{cell_id}/duplicate` — checks `notebook:create` ✅ (verify)
- `GET /notebooks/{id}` — checks `notebook:view` in handler ✅ (verify)
- `GET /connectors` — checks `connector:view` per result ✅ (verify)
- `GET /dashboards/{id}` — checks `dashboard:view` in handler ✅ (verify)
- Cell version history — checks `notebook:view` ✅ (verify)
- Attachments — checks `notebook:edit` ✅ (verify)

#### Endpoints with MISSING or incomplete checks (CRITICAL)
- `POST /notebooks/{notebook_id}/schedules` — **NO permission check** beyond org scoping ⚠️
- `GET /notebooks/{notebook_id}/schedules` — **NO permission check** beyond org scoping ⚠️
- `GET /schedules/{id}` — **NO permission check** beyond org scoping ⚠️
- `DELETE /schedules/{id}` — **NO permission check** beyond org scoping ⚠️
- `PUT /schedules/{id}` — **NO permission check** beyond org scoping ⚠️
- `POST /notebooks/{notebook_id}/cells/{cell_id}/execute` — checks `connector:use` but **NOT `notebook:run`** ⚠️

### E. Special Endpoints

#### WebSocket
- `GET /ws/notebooks/{id}` — verify auth + permission check
- `GET /ws/agents/{session_id}` — verify auth + permission check

#### Internal Yjs
- `GET /internal/yjs/{notebook_id}` — unauthenticated, verify it doesn't expose data
- `PUT /internal/yjs/{notebook_id}` — unauthenticated, verify it restricts writes

#### Public
- `GET /public/dashboards/{token}` — verify token-scoped access
- `GET /public/motd` — verify public access (no auth needed)

---

## Test File Organization

```
permissions_audit_test.go           — Fixtures: setupOrgs(), createUsers(), createGroups(),
                                       createResource(), user tokens, shared helpers
permissions_audit_middleware_test.go — Tests for requirePermission, RequireRole,
                                       RequirePlatformAdmin middleware patterns
permissions_audit_notebook_test.go   — Notebook + cell CRUD permissions
permissions_audit_execute_test.go    — Cell execution permissions
permissions_audit_schedule_test.go   — Schedule CRUD permissions (high-risk)
permissions_audit_folder_test.go     — Folder permissions + inheritance
permissions_audit_connector_test.go  — Connector permissions
permissions_audit_dashboard_test.go  — Dashboard permissions
permissions_audit_agent_test.go      — Agent permissions
permissions_audit_modelconfig_test.go — Model config permissions
permissions_audit_skill_test.go      — Skill permissions
permissions_audit_mcp_test.go        — MCP server permissions
permissions_audit_attachment_test.go — Attachment permissions
permissions_audit_admin_test.go      — Platform admin + org admin management routes
permissions_audit_special_test.go    — WebSocket, internal Yjs, public endpoints
```

---

## Test Pattern Template

```go
func TestNotebook_Permissions(t *testing.T) {
    srv, fixtures := setupAuditTest(t)
    nbID := fixtures.OrgAResources.NotebookNoACL

    cases := []struct {
        name     string
        user     string   // fixture user ID key
        action   string   // what to attempt
        method   string
        path     string
        body     any
        wantCode int
    }{
        {"admin view", "adminA", "view", "GET", "/api/v1/notebooks/" + nbID, nil, 200},
        {"admin mode-off view", "adminAModeOff", "view", "GET", "/api/v1/notebooks/" + nbID, nil, 403},
        {"alice view no acl", "aliceA", "view", "GET", "/api/v1/notebooks/" + nbID, nil, 403},
        {"cross-org view", "adminB", "view", "GET", "/api/v1/notebooks/" + nbID, nil, 403},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            token := fixtures.Token(tc.user)
            req := httptest.NewRequest(tc.method, tc.path, bodyReader(tc.body))
            req.Header.Set("Authorization", "Bearer "+token)
            if strings.HasSuffix(tc.user, "ModeOff") {
                req.Header.Set("X-HNB-Admin-Mode", "false")
            }
            rec := httptest.NewRecorder()
            srv.ServeHTTP(rec, req)
            assert.Equal(t, tc.wantCode, rec.Code)
        })
    }
}
```

---

## Expected Vulnerability Categories

| # | Category | Examples Found |
|---|----------|---------------|
| 1 | Missing permission check | Schedules (5 endpoints), execution (missing `notebook:run`) |
| 2 | Cross-org information disclosure | Any endpoint scoped by org_id in query but not rejecting cross-org tokens |
| 3 | Creator access denial | New resources without ACL seeding → creator can't access own resource |
| 4 | Admin bypass edge cases | `X-HNB-Admin-Mode` header behavior, nil context |
| 5 | Inconsistent list filtering | Some list endpoints filter by permission, others don't |
| 6 | Public/internal endpoint exposure | Yjs internal endpoints without auth |

---

## Deliverables

1. **Test files** — one per resource type, all runnable via `go test ./internal/api/...`
2. **Test report** — test output showing pass/fail per scenario
3. **Vulnerability report** — documenting each confirmed issue with code location and severity
