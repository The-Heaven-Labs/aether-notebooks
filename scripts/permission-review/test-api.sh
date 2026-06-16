#!/usr/bin/env bash
#
# Permission Review — API Tests (Tasks 2-7, final)
#
set -euo pipefail

API="${HNB_API_URL:-http://localhost:8080}"
source /tmp/permission-review-state.env

PASS=0
FAIL=0
GAPS=""
RESULTS=""

check() {
  local test_name="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    echo "  ✓ $test_name ($actual)"
    PASS=$((PASS+1))
    RESULTS="$RESULTS\n| $test_name | $expected | $actual | PASS |"
  else
    echo "  ✗ $test_name (expected $expected, got $actual)"
    FAIL=$((FAIL+1))
    RESULTS="$RESULTS\n| $test_name | $expected | $actual | FAIL |"
  fi
}

gap() {
  GAPS="$GAPS\n- **$1**: $2"
  echo "  ⚠ GAP: $1 — $2"
}

code() {
  curl -s -o /dev/null -w "%{http_code}" "$@"
}

echo "=== Permission Review: API Tests (Tasks 2-7) ==="
echo ""

# ══════════════════════════════════════════════════════════════
# TASK 2: Baseline — Admin Bypass + Deny-by-Default
# ══════════════════════════════════════════════════════════════
echo "━━━ Task 2: Baseline — Admin Bypass + Deny-by-Default ━━━"

echo ""
echo "Admin access (should all be 200):"
check "Admin → GET notebook" 200 $(code -H "Authorization: Bearer $ADMIN_TOKEN" "$API/api/v1/notebooks/$NOTEBOOK_ID")
check "Admin → GET dashboard" 200 $(code -H "Authorization: Bearer $ADMIN_TOKEN" "$API/api/v1/dashboards/$DASHBOARD_ID")
check "Admin → GET agent" 200 $(code -H "Authorization: Bearer $ADMIN_TOKEN" "$API/api/v1/agents/$AGENT_ID")

echo ""
echo "Editor access without ACL (should all be 403):"
check "Editor → GET notebook (no ACL)" 403 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/notebooks/$NOTEBOOK_ID")
check "Editor → GET dashboard (no ACL)" 403 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/dashboards/$DASHBOARD_ID")
check "Editor → GET agent (no ACL)" 403 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/agents/$AGENT_ID")

echo ""
echo "Viewer access without ACL (should all be 403):"
check "Viewer → GET notebook (no ACL)" 403 $(code -H "Authorization: Bearer $VIEWER_TOKEN" "$API/api/v1/notebooks/$NOTEBOOK_ID")
check "Viewer → GET dashboard (no ACL)" 403 $(code -H "Authorization: Bearer $VIEWER_TOKEN" "$API/api/v1/dashboards/$DASHBOARD_ID")
check "Viewer → GET agent (no ACL)" 403 $(code -H "Authorization: Bearer $VIEWER_TOKEN" "$API/api/v1/agents/$AGENT_ID")

echo ""
echo "Resources without individual GET endpoints (list-only):"
echo "  connectors: GET /{id} not registered → test via list"
echo "  model_configs: GET /{id} not registered → test via list"
echo "  skills: GET /{id} not registered → test via list"
echo "  mcp_servers: GET /{id} has bug (no folder_id column) → test via list"

# ══════════════════════════════════════════════════════════════
# TASK 3: Explicit User ACL
# ══════════════════════════════════════════════════════════════
echo ""
echo "━━━ Task 3: Explicit User ACL ━━━"

echo ""
echo "Notebook: Grant editor 'view'..."
curl -sf -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"entries\":[{\"subject_type\":\"user\",\"subject_id\":\"$EDITOR_USER_ID\",\"actions\":[\"view\"]}]}" \
  "$API/api/v1/acl/notebook/$NOTEBOOK_ID" > /dev/null

check "Editor → GET notebook (view)" 200 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/notebooks/$NOTEBOOK_ID")

# Test edit: editor has only "view", PUT should be blocked
PUT_CODE=$(code -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" -H "Content-Type: application/json" -d '{"title":"hack"}' "$API/api/v1/notebooks/$NOTEBOOK_ID")
check "Editor → PUT notebook (no edit)" 403 "$PUT_CODE"
if [ "$PUT_CODE" = "200" ]; then
  gap "Notebook PUT ACL bypass" "PUT /notebooks/{id} uses RequireRole('editor') only — no per-resource ACL. Editor with 'view' can update."
fi

check "Viewer → GET notebook (no ACL)" 403 $(code -H "Authorization: Bearer $VIEWER_TOKEN" "$API/api/v1/notebooks/$NOTEBOOK_ID")

echo ""
echo "Dashboard: Grant editor 'view'..."
curl -sf -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"entries\":[{\"subject_type\":\"user\",\"subject_id\":\"$EDITOR_USER_ID\",\"actions\":[\"view\"]}]}" \
  "$API/api/v1/acl/dashboard/$DASHBOARD_ID" > /dev/null

check "Editor → GET dashboard (view)" 200 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/dashboards/$DASHBOARD_ID")

# Test edit: editor has only "view", PUT should be blocked
DASH_PUT_CODE=$(code -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" -H "Content-Type: application/json" -d '{"title":"hack"}' "$API/api/v1/dashboards/$DASHBOARD_ID")
check "Editor → PUT dashboard (no edit)" 403 "$DASH_PUT_CODE"
if [ "$DASH_PUT_CODE" = "200" ]; then
  gap "Dashboard PUT ACL bypass" "PUT /dashboards/{id} uses RequireRole('editor') only — no per-resource ACL. Editor with 'view' can update."
fi

check "Viewer → GET dashboard (no ACL)" 403 $(code -H "Authorization: Bearer $VIEWER_TOKEN" "$API/api/v1/dashboards/$DASHBOARD_ID")

echo ""
echo "Agent: Grant editor 'view'..."
curl -sf -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"entries\":[{\"subject_type\":\"user\",\"subject_id\":\"$EDITOR_USER_ID\",\"actions\":[\"view\"]}]}" \
  "$API/api/v1/acl/agent/$AGENT_ID" > /dev/null

check "Editor → GET agent (view)" 200 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/agents/$AGENT_ID")
check "Viewer → GET agent (no ACL)" 403 $(code -H "Authorization: Bearer $VIEWER_TOKEN" "$API/api/v1/agents/$AGENT_ID")

# Test agent edit (uses requirePermission — should be blocked)
AGENT_PUT_CODE=$(code -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" -H "Content-Type: application/json" -d '{"name":"hack"}' "$API/api/v1/agents/$AGENT_ID")
check "Editor → PUT agent (no edit, has requirePermission)" 403 "$AGENT_PUT_CODE"

# ══════════════════════════════════════════════════════════════
# TASK 4: Group-Based ACL
# ══════════════════════════════════════════════════════════════
echo ""
echo "━━━ Task 4: Group-Based ACL ━━━"

echo ""
echo "Creating group 'review-group' and adding editor..."
# Try to create group (may already exist from previous run)
GROUP_RESP=$(curl -s -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"review-group"}' \
  "$API/api/v1/groups")
GROUP_ID=$(echo "$GROUP_RESP" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "")

# If creation failed, look up existing group
if [ -z "$GROUP_ID" ]; then
  GROUP_ID=$(curl -s -H "Authorization: Bearer $ADMIN_TOKEN" "$API/api/v1/groups" | python3 -c "
import sys,json
groups = json.load(sys.stdin)
for g in groups:
    if g['name'] == 'review-group':
        print(g['id'])
        break
" 2>/dev/null || echo "")
fi
echo "  Group ID: $GROUP_ID"

# Add editor to group (ignore error if already member)
curl -s -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"user_id\":\"$EDITOR_USER_ID\"}" \
  "$API/api/v1/groups/$GROUP_ID/members" > /dev/null 2>&1 || true
echo "  Editor added to group"

echo ""
echo "Granting group 'view,edit' on dashboard..."
curl -sf -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"entries\":[{\"subject_type\":\"group\",\"subject_id\":\"$GROUP_ID\",\"actions\":[\"view\",\"edit\"]}]}" \
  "$API/api/v1/acl/dashboard/$DASHBOARD_ID" > /dev/null

check "Editor (group member) → GET dashboard" 200 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/dashboards/$DASHBOARD_ID")
check "Viewer (non-member) → GET dashboard" 403 $(code -H "Authorization: Bearer $VIEWER_TOKEN" "$API/api/v1/dashboards/$DASHBOARD_ID")

# ══════════════════════════════════════════════════════════════
# TASK 5: org_role ACL
# ══════════════════════════════════════════════════════════════
echo ""
echo "━━━ Task 5: org_role ACL ━━━"

echo ""
echo "Granting org_role:everyone 'view' on agent..."
curl -sf -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"entries":[{"subject_type":"org_role","subject_id":"everyone","actions":["view"]}]}' \
  "$API/api/v1/acl/agent/$AGENT_ID" > /dev/null

check "Editor → GET agent (everyone view)" 200 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/agents/$AGENT_ID")
check "Viewer → GET agent (everyone view)" 200 $(code -H "Authorization: Bearer $VIEWER_TOKEN" "$API/api/v1/agents/$AGENT_ID")

# ══════════════════════════════════════════════════════════════
# TASK 6: Folder Inheritance
# ══════════════════════════════════════════════════════════════
echo ""
echo "━━━ Task 6: Folder Inheritance ━━━"

echo ""
echo "Creating folder hierarchy..."
PARENT=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Parent Folder"}' \
  "$API/api/v1/folders" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Parent: $PARENT"

CHILD=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"Child Folder\",\"parent_id\":\"$PARENT\"}" \
  "$API/api/v1/folders" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Child: $CHILD"

NOTEBOOK_IN_CHILD=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"title\":\"Notebook in Child\",\"folder_id\":\"$CHILD\"}" \
  "$API/api/v1/notebooks" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Notebook in child: $NOTEBOOK_IN_CHILD"

NOTEBOOK_OUTSIDE=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Notebook Outside"}' \
  "$API/api/v1/notebooks" | python3 -c "import sys,json; print(json.load(sys.stdin)['id'])")
echo "  Notebook outside: $NOTEBOOK_OUTSIDE"

echo ""
echo "Granting editor 'view' on parent folder only..."
curl -sf -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"entries\":[{\"subject_type\":\"user\",\"subject_id\":\"$EDITOR_USER_ID\",\"actions\":[\"view\"]}]}" \
  "$API/api/v1/acl/folder/$PARENT" > /dev/null

echo "Inheritance test:"
check "Editor → GET notebook in child (inherited view)" 200 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/notebooks/$NOTEBOOK_IN_CHILD")
check "Editor → GET notebook outside (no ACL)" 403 $(code -H "Authorization: Bearer $EDITOR_TOKEN" "$API/api/v1/notebooks/$NOTEBOOK_OUTSIDE")

# ══════════════════════════════════════════════════════════════
# TASK 7: ACL Management (share/manage)
# ══════════════════════════════════════════════════════════════
echo ""
echo "━━━ Task 7: ACL Management (share/manage) ━━━"

echo ""
echo "Granting editor 'view,edit,share' on agent..."
curl -sf -X PUT -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"entries\":[{\"subject_type\":\"user\",\"subject_id\":\"$EDITOR_USER_ID\",\"actions\":[\"view\",\"edit\",\"share\"]}]}" \
  "$API/api/v1/acl/agent/$AGENT_ID" > /dev/null

echo "Editor ACL management (has share on agent):"
check "Editor → PUT ACL on agent (has share)" 200 $(code -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" -H "Content-Type: application/json" -d "{\"entries\":[{\"subject_type\":\"user\",\"subject_id\":\"$VIEWER_USER_ID\",\"actions\":[\"view\"]}]}" "$API/api/v1/acl/agent/$AGENT_ID")

echo ""
echo "Editor ACL management (no share on notebook):"
check "Editor → PUT ACL on notebook (no share)" 403 $(code -X PUT -H "Authorization: Bearer $EDITOR_TOKEN" -H "Content-Type: application/json" -d "{\"entries\":[{\"subject_type\":\"user\",\"subject_id\":\"$VIEWER_USER_ID\",\"actions\":[\"view\"]}]}" "$API/api/v1/acl/notebook/$NOTEBOOK_ID")

# ══════════════════════════════════════════════════════════════
# Summary
# ══════════════════════════════════════════════════════════════
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "=== API Tests Complete: $PASS passed, $FAIL failed ==="
echo ""

if [ -n "$GAPS" ]; then
  echo "━━━ GAPS FOUND ━━━"
  echo -e "$GAPS"
  echo ""
fi

# Save results
echo -e "$RESULTS" > /tmp/permission-review-results.md
echo -e "$GAPS" > /tmp/permission-review-gaps.md
