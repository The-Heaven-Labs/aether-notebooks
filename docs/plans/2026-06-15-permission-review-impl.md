# Permission System: Full Review & Validation — Implementation Plan

> **For Claude:** This is a testing/validation plan, not a coding plan. No code changes. Use skills: agent-browser for UI validation.

**Goal:** End-to-end validation of the hnb permission/ACL system across all 8 resource types, documenting all gaps via agent-browser testing.

**Architecture:** Use curl to set up test state (org, users, resources, ACL entries), then agent-browser to login as each user and verify access/denial behavior in the UI.

**Tech Stack:** Docker compose (dev stack), Go API (curl), React frontend (agent-browser)

---

### Task 1: Infrastructure Setup + Seed Test Data

**Files:**
- Create: `scripts/permission-review/setup.sh`
- Reference: `internal/api/permissions.go`
- Reference: `internal/models/acl.go`

**Step 1: Start dev stack**

```bash
docker compose -f docker-compose.dev.yml up -d
docker compose -f docker-compose.dev.yml ps
```
Expected: All services (api, web, relay, postgres, redis, clickhouse, opensearch) running.

**Step 2: Write and run seed script**

Create `scripts/permission-review/setup.sh` that:
1. Registers 3 users via `POST /api/v1/auth/register`:
   - `perm-admin@test.com` / `test123` (becomes org admin of its own org)
   - `perm-editor@test.com` / `test123`
   - `perm-viewer@test.com` / `test123`
2. Logs in as admin, creates org "Permission Test Org"
3. Invites + joins editor/user via invite link
4. Creates one of each resource type
5. Stores all IDs + auth tokens in a state file
6. Verifies availability of all endpoints (health check)

Run: `./scripts/permission-review/setup.sh`
Expected: Exit 0, state file created at `/tmp/permission-review-state.env`

**Step 3: Verify state**

```bash
source /tmp/permission-review-state.env
echo "Admin token: ${ADMIN_TOKEN:0:20}..."
echo "Editor token: ${EDITOR_TOKEN:0:20}..."
echo "Viewer token: ${VIEWER_TOKEN:0:20}..."
echo "Org ID: $ORG_ID"
echo "Notebook ID: $NOTEBOOK_ID"
echo "Agent ID: $AGENT_ID"
echo "Folder ID: $FOLDER_ID"
```
Expected: All values populated

---

### Task 2: Baseline — Admin Bypass + Deny-by-Default

**Files:**
- None (API-only testing)

**Step 1: Verify admin can access all resources**

For each resource type, test view/edit/delete as admin:
```bash
source /tmp/permission-review-state.env

# Admin view notebook
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:8080/api/v1/notebooks/$NOTEBOOK_ID"

# Admin view agent
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:8080/api/v1/agents/$AGENT_ID"

# ... repeat for all resource types
```
Expected: All return 200

**Step 2: Verify editor/viewer get 403 on resources without ACL**

Test each resource type as editor and viewer:
```bash
# Editor tries to view notebook (no ACL entry)
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $EDITOR_TOKEN" \
  "http://localhost:8080/api/v1/notebooks/$NOTEBOOK_ID"
```
Expected: 403 for all (except agents/model_configs/skill/mcp_server where creator auto-ACL was seeded — admin is creator so those also return 403 for editor/viewer)

**Step 3: Document results in matrix**

Record pass/fail in a temp results file.

---

### Task 3: Explicit User ACL

**Step 1: Grant ACL via API**

```bash
source /tmp/permission-review-state.env

# Grant User B (editor) "view" on the notebook
curl -s -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"user","subject_id":"'$EDITOR_USER_ID'","actions":["view"]}]}' \
  "http://localhost:8080/api/v1/acl/notebook/$NOTEBOOK_ID"
```
Expected: 200

**Step 2: Verify editor can now view, but not edit**

```bash
# Editor view notebook — should be 200
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $EDITOR_TOKEN" \
  "http://localhost:8080/api/v1/notebooks/$NOTEBOOK_ID"

# Editor edit notebook — should be 403
curl -s -o /dev/null -w "%{http_code}" -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"hack"}' \
  "http://localhost:8080/api/v1/notebooks/$NOTEBOOK_ID"
```
Expected: 200 then 403

**Step 3: Verify viewer still gets 403**

```bash
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $VIEWER_TOKEN" \
  "http://localhost:8080/api/v1/notebooks/$NOTEBOOK_ID"
```
Expected: 403

**Step 4: Repeat for all 8 resource types**

---

### Task 4: Group-Based ACL

**Step 1: Create group and add editor**

```bash
# Create group "review-group"
GROUP_RESPONSE=$(curl -s -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"review-group"}' \
  "http://localhost:8080/api/v1/groups")
GROUP_ID=$(echo $GROUP_RESPONSE | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

# Add editor to group
curl -s -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:8080/api/v1/groups/$GROUP_ID/members" \
  -d "{\"user_id\":\"$EDITOR_USER_ID\"}"
```
Expected: 200 for both

**Step 2: Grant ACL to group on dashboard**

```bash
curl -s -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"group","subject_id":"'$GROUP_ID'","actions":["view","edit"]}]}' \
  "http://localhost:8080/api/v1/acl/dashboard/$DASHBOARD_ID"
```
Expected: 200

**Step 3: Verify editor (group member) can view/edit, viewer (not member) can't**

```bash
# Editor view — 200
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $EDITOR_TOKEN" \
  "http://localhost:8080/api/v1/dashboards/$DASHBOARD_ID"

# Editor edit — 200 (has edit in group ACL)
curl -s -o /dev/null -w "%{http_code}" -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"hack"}' \
  "http://localhost:8080/api/v1/dashboards/$DASHBOARD_ID"

# Viewer view — 403
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $VIEWER_TOKEN" \
  "http://localhost:8080/api/v1/dashboards/$DASHBOARD_ID"
```
Expected: 200, 200, 403

---

### Task 5: org_role ACL (editor, viewer, everyone)

**Step 1: Grant `org_role:everyone` `view` on connector**

```bash
curl -s -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"org_role","subject_id":"everyone","actions":["view"]}]}' \
  "http://localhost:8080/api/v1/acl/connector/$CONNECTOR_ID"
```
Expected: 200

**Step 2: Verify both editor and viewer can view**

```bash
# Editor view — 200
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $EDITOR_TOKEN" \
  "http://localhost:8080/api/v1/connectors/$CONNECTOR_ID"

# Viewer view — 200
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $VIEWER_TOKEN" \
  "http://localhost:8080/api/v1/connectors/$CONNECTOR_ID"

# Editor edit — 403 (only view granted)
curl -s -o /dev/null -w "%{http_code}" -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"hack"}' \
  "http://localhost:8080/api/v1/connectors/$CONNECTOR_ID"
```
Expected: 200, 200, 403

**Step 3: Grant `org_role:editor` `view,edit` on skill**

```bash
curl -s -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"org_role","subject_id":"editor","actions":["view","edit"]}]}' \
  "http://localhost:8080/api/v1/acl/skill/$SKILL_ID"
```
Expected: 200

**Step 4: Verify editor can view/edit, viewer can't**

```bash
# Editor view — 200
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $EDITOR_TOKEN" \
  "http://localhost:8080/api/v1/skills/$SKILL_ID"

# Viewer view — 403
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $VIEWER_TOKEN" \
  "http://localhost:8080/api/v1/skills/$SKILL_ID"
```
Expected: 200, 403

---

### Task 6: Folder Inheritance

**Step 1: Create folder hierarchy**

```bash
# Create parent folder
PARENT=$(curl -s -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Parent Folder"}' \
  "http://localhost:8080/api/v1/folders" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

# Create child folder inside parent
CHILD=$(curl -s -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"Child Folder\",\"parent_id\":\"$PARENT\"}" \
  "http://localhost:8080/api/v1/folders" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

# Create notebook in child folder
NOTEBOOK_IN_CHILD=$(curl -s -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"Notebook in Child\",\"folder_id\":\"$CHILD\"}" \
  "http://localhost:8080/api/v1/notebooks" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")

# Create notebook NOT in hierarchy
NOTEBOOK_OUTSIDE=$(curl -s -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Notebook Outside"}' \
  "http://localhost:8080/api/v1/notebooks" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
```

**Step 2: Grant `view` to editor on parent folder only**

```bash
curl -s -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"user","subject_id":"'$EDITOR_USER_ID'","actions":["view"]}]}' \
  "http://localhost:8080/api/v1/acl/folder/$PARENT"
```
Expected: 200

**Step 3: Verify editor can view notebook-in-child (inherited) but not notebook-outside**

```bash
# Notebook in child (inherits via parent) — 200
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $EDITOR_TOKEN" \
  "http://localhost:8080/api/v1/notebooks/$NOTEBOOK_IN_CHILD"

# Notebook outside (no ACL) — 403
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $EDITOR_TOKEN" \
  "http://localhost:8080/api/v1/notebooks/$NOTEBOOK_OUTSIDE"
```
Expected: 200, 403

---

### Task 7: ACL Management ("share" / "manage")

**Step 1: Grant editor "share" on agent**

```bash
curl -s -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"user","subject_id":"'$EDITOR_USER_ID'","actions":["view","edit","share"]}]}' \
  "http://localhost:8080/api/v1/acl/agent/$AGENT_ID"
```
Expected: 200

**Step 2: Verify editor can now modify ACL on that agent**

```bash
# Editor sets ACL on agent
curl -s -o /dev/null -w "%{http_code}" -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"user","subject_id":"'$VIEWER_USER_ID'","actions":["view"]}]}' \
  "http://localhost:8080/api/v1/acl/agent/$AGENT_ID"
```
Expected: 200 (editor has "share" on agent)

**Step 3: Verify editor CANNOT modify ACL on notebook (no share)**

```bash
curl -s -o /dev/null -w "%{http_code}" -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"user","subject_id":"'$VIEWER_USER_ID'","actions":["view"]}]}' \
  "http://localhost:8080/api/v1/acl/notebook/$NOTEBOOK_ID"
```
Expected: 403

---

### Task 8: UI Validation via Agent-Browser

**Step 1: Login as admin and verify access in UI**

```bash
# Open browser to login page
# ... agent-browser commands
```
Use agent-browser to:
1. Login as admin user
2. Navigate to each resource type page
3. Verify all resources visible and editable
4. Take screenshot

**Step 2: Login as editor and verify scoped access**

1. Login as editor
2. Verify only resources with ACL grants are visible
3. Try to access a restricted resource directly via URL
4. Take screenshot of 403 page

**Step 3: Login as viewer and verify minimal access**

1. Login as viewer
2. Verify only `org_role:everyone` resources visible
3. Attempt to access restricted resources
4. Take screenshot

---

### Task 9: List Endpoint Filtering

**Step 1: Test that list endpoints respect permissions**

```bash
# Admin lists all notebooks
ADMIN_COUNT=$(curl -s -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:8080/api/v1/notebooks" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d))")

# Editor lists notebooks (should see fewer if filtering works)
EDITOR_COUNT=$(curl -s -H "Authorization: Bearer $EDITOR_TOKEN" \
  "http://localhost:8080/api/v1/notebooks" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d))")

echo "Admin sees: $ADMIN_COUNT notebooks, Editor sees: $EDITOR_COUNT notebooks"
```

This reveals whether list endpoints filter by permission or return all.

---

### Task 10: Compile Final Report

**Report sections:**
1. **Executive Summary** — key findings, severity, risk
2. **Permission Matrix Results** — pass/fail table
3. **Gap Analysis** — from static code review
4. **List Endpoint Findings** — which endpoints filter
5. **Agent-Browser Evidence** — screenshots linked
6. **Recommendations** — prioritized

Save to `docs/plans/2026-06-15-permission-review-report.md`
