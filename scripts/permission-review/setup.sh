#!/usr/bin/env bash
set -euo pipefail

API="http://localhost:8080/api/v1"
BASE="http://localhost:8080"
STATE="/tmp/permission-review-state.env"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

fail() { echo -e "${RED}FAIL: $1${NC}" >&2; exit 1; }
ok()   { echo -e "${GREEN}OK: $1${NC}"; }

jv() { python3 -c "import sys,json; d=json.load(sys.stdin); print(d$1)" <<< "$2" 2>/dev/null; }

echo "=== Health Check ==="
HTTP=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/health")
[ "$HTTP" = "200" ] || fail "API not healthy (HTTP $HTTP)"
ok "API healthy"

echo ""
echo "=== Register Admin ==="
ADMIN_RESP=$(curl -s -X POST "$API/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"perm-admin@test.com","password":"test123","name":"Perm Admin","org_name":"Permission Test Org"}')
ADMIN_TOKEN=$(jv "['token']" "$ADMIN_RESP")
ADMIN_USER_ID=$(jv "['user']['id']" "$ADMIN_RESP")
ORG_ID=$(jv "['org']['id']" "$ADMIN_RESP")
ADMIN_ROLE=$(jv "['org']['role']" "$ADMIN_RESP")

[ -n "$ADMIN_TOKEN" ] || fail "Admin registration failed: $ADMIN_RESP"
[ "$ADMIN_ROLE" = "admin" ] || fail "Admin role is '$ADMIN_ROLE', expected 'admin'"
ok "Admin registered (user=$ADMIN_USER_ID, org=$ORG_ID)"

echo ""
echo "=== Register Editor (no org) ==="
EDITOR_RESP=$(curl -s -X POST "$API/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"perm-editor@test.com","password":"test123","name":"Perm Editor"}')
EDITOR_ONBOARD=$(jv "['onboarding_token']" "$EDITOR_RESP")
[ -n "$EDITOR_ONBOARD" ] || fail "Editor registration failed: $EDITOR_RESP"
ok "Editor registered (onboarding token received)"

echo ""
echo "=== Register Viewer (no org) ==="
VIEWER_RESP=$(curl -s -X POST "$API/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"perm-viewer@test.com","password":"test123","name":"Perm Viewer"}')
VIEWER_ONBOARD=$(jv "['onboarding_token']" "$VIEWER_RESP")
[ -n "$VIEWER_ONBOARD" ] || fail "Viewer registration failed: $VIEWER_RESP"
ok "Viewer registered (onboarding token received)"

echo ""
echo "=== Create Invites ==="
EDITOR_INVITE=$(curl -s -X POST "$API/members/invite" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"perm-editor@test.com","role":"editor"}')
EDITOR_INVITE_TOKEN=$(jv "['token']" "$EDITOR_INVITE")
[ -n "$EDITOR_INVITE_TOKEN" ] || fail "Editor invite failed: $EDITOR_INVITE"
ok "Editor invite created"

VIEWER_INVITE=$(curl -s -X POST "$API/members/invite" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"perm-viewer@test.com","role":"viewer"}')
VIEWER_INVITE_TOKEN=$(jv "['token']" "$VIEWER_INVITE")
[ -n "$VIEWER_INVITE_TOKEN" ] || fail "Viewer invite failed: $VIEWER_INVITE"
ok "Viewer invite created"

echo ""
echo "=== Editor Joins Org ==="
EDITOR_JOIN=$(curl -s -X POST "$API/auth/org/join" \
  -H "Authorization: Bearer $EDITOR_ONBOARD" \
  -H "Content-Type: application/json" \
  -d "{\"invite_token\":\"$EDITOR_INVITE_TOKEN\"}")
EDITOR_TOKEN=$(jv "['token']" "$EDITOR_JOIN")
EDITOR_USER_ID=$(jv "['user']['id']" "$EDITOR_JOIN")
[ -n "$EDITOR_TOKEN" ] || fail "Editor join failed: $EDITOR_JOIN"
ok "Editor joined (user=$EDITOR_USER_ID)"

echo ""
echo "=== Viewer Joins Org ==="
VIEWER_JOIN=$(curl -s -X POST "$API/auth/org/join" \
  -H "Authorization: Bearer $VIEWER_ONBOARD" \
  -H "Content-Type: application/json" \
  -d "{\"invite_token\":\"$VIEWER_INVITE_TOKEN\"}")
VIEWER_TOKEN=$(jv "['token']" "$VIEWER_JOIN")
VIEWER_USER_ID=$(jv "['user']['id']" "$VIEWER_JOIN")
[ -n "$VIEWER_TOKEN" ] || fail "Viewer join failed: $VIEWER_JOIN"
ok "Viewer joined (user=$VIEWER_USER_ID)"

echo ""
echo "=== Create Test Resources (as admin) ==="

NB_RESP=$(curl -s -X POST "$API/notebooks" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Permission Test Notebook"}')
NOTEBOOK_ID=$(jv "['id']" "$NB_RESP")
[ -n "$NOTEBOOK_ID" ] || fail "Notebook creation failed: $NB_RESP"
ok "Notebook created ($NOTEBOOK_ID)"

DASH_RESP=$(curl -s -X POST "$API/dashboards" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Permission Test Dashboard"}')
DASHBOARD_ID=$(jv "['id']" "$DASH_RESP")
[ -n "$DASHBOARD_ID" ] || fail "Dashboard creation failed: $DASH_RESP"
ok "Dashboard created ($DASHBOARD_ID)"

CONN_RESP=$(curl -s -X POST "$API/connectors" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Connector","type":"postgres","host":"localhost","port":5432,"database":"test","user":"test","password":"test"}')
CONNECTOR_ID=$(jv "['id']" "$CONN_RESP")
[ -n "$CONNECTOR_ID" ] || fail "Connector creation failed: $CONN_RESP"
ok "Connector created ($CONNECTOR_ID)"

AGENT_RESP=$(curl -s -X POST "$API/agents" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Agent","description":"test agent","system_prompt":"You are a test."}')
AGENT_ID=$(jv "['id']" "$AGENT_RESP")
[ -n "$AGENT_ID" ] || fail "Agent creation failed: $AGENT_RESP"
ok "Agent created ($AGENT_ID)"

MC_RESP=$(curl -s -X POST "$API/model-configs" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Model Config","provider":"openai","base_url":"https://api.openai.com/v1","model":"gpt-4","api_key":"sk-test-key","context_window":128000}')
MODEL_CONFIG_ID=$(jv "['id']" "$MC_RESP")
[ -n "$MODEL_CONFIG_ID" ] || fail "Model config creation failed: $MC_RESP"
ok "Model config created ($MODEL_CONFIG_ID)"

SKILL_RESP=$(curl -s -X POST "$API/skills" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Skill","description":"test skill","system_prompt":"You are a test skill."}')
SKILL_ID=$(jv "['id']" "$SKILL_RESP")
[ -n "$SKILL_ID" ] || fail "Skill creation failed: $SKILL_RESP"
ok "Skill created ($SKILL_ID)"

MCP_RESP=$(curl -s -X POST "$API/mcp-servers" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test MCP","type":"stdio","command":"echo","args":["hello"]}')
MCP_SERVER_ID=$(jv "['id']" "$MCP_RESP")
[ -n "$MCP_SERVER_ID" ] || fail "MCP server creation failed: $MCP_RESP"
ok "MCP server created ($MCP_SERVER_ID)"

FOLDER_RESP=$(curl -s -X POST "$API/folders" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Folder"}')
FOLDER_ID=$(jv "['id']" "$FOLDER_RESP")
[ -n "$FOLDER_ID" ] || fail "Folder creation failed: $FOLDER_RESP"
ok "Folder created ($FOLDER_ID)"

echo ""
echo "=== Writing State File ==="
cat > "$STATE" << EOF
ADMIN_TOKEN=$ADMIN_TOKEN
ADMIN_USER_ID=$ADMIN_USER_ID
EDITOR_TOKEN=$EDITOR_TOKEN
EDITOR_USER_ID=$EDITOR_USER_ID
VIEWER_TOKEN=$VIEWER_TOKEN
VIEWER_USER_ID=$VIEWER_USER_ID
ORG_ID=$ORG_ID
NOTEBOOK_ID=$NOTEBOOK_ID
DASHBOARD_ID=$DASHBOARD_ID
CONNECTOR_ID=$CONNECTOR_ID
AGENT_ID=$AGENT_ID
MODEL_CONFIG_ID=$MODEL_CONFIG_ID
SKILL_ID=$SKILL_ID
MCP_SERVER_ID=$MCP_SERVER_ID
FOLDER_ID=$FOLDER_ID
EOF

ok "State saved to $STATE"
echo ""
echo "=== Setup Complete ==="
