#!/usr/bin/env bash
set -uo pipefail

# Permission Review — Seed Script
# Creates test users, org, and resources for permission validation.

API="http://localhost:8080/api/v1"
STATE_FILE="/tmp/permission-review-state.env"

log() { echo "[setup] $*"; }
fail() { echo "[setup] FAILED: $*" >&2; exit 1; }

# Safely extract a JSON field from stdin. Returns empty string on failure.
json_field() {
  python3 -c "import sys,json; d=json.load(sys.stdin); print(d$1)" 2>/dev/null || true
}

# ── Health check ──────────────────────────────────────────────────────
log "Checking API health..."
HEALTH=$(curl -s http://localhost:8080/health)
echo "$HEALTH" | grep -q '"ok"' || fail "API not healthy: $HEALTH"
log "API is healthy."

# ── Register admin (with org) ────────────────────────────────────────
log "Registering admin user..."
ADMIN_RESP=$(curl -s -X POST "$API/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"perm-admin@test.com","password":"test123","name":"Permission Admin","org_name":"Permission Test Org"}')

ADMIN_TOKEN=$(echo "$ADMIN_RESP" | json_field "['token']")
ADMIN_USER_ID=$(echo "$ADMIN_RESP" | json_field "['user']['id']")
ORG_ID=$(echo "$ADMIN_RESP" | json_field "['org']['id']")
ADMIN_ROLE=$(echo "$ADMIN_RESP" | json_field "['org']['role']")

[ -z "$ADMIN_TOKEN" ] && fail "Admin registration failed: $ADMIN_RESP"
log "Admin registered: id=$ADMIN_USER_ID org=$ORG_ID role=$ADMIN_ROLE"

# ── Register editor (no org) ─────────────────────────────────────────
log "Registering editor user..."
EDITOR_RESP=$(curl -s -X POST "$API/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"perm-editor@test.com","password":"test123","name":"Permission Editor"}')

EDITOR_ONBOARDING=$(echo "$EDITOR_RESP" | json_field "['onboarding_token']")
[ -z "$EDITOR_ONBOARDING" ] && fail "Editor registration failed (no onboarding_token): $EDITOR_RESP"
log "Editor registered (onboarding token obtained)."

# ── Register viewer (no org) ─────────────────────────────────────────
log "Registering viewer user..."
VIEWER_RESP=$(curl -s -X POST "$API/auth/register" \
  -H "Content-Type: application/json" \
  -d '{"email":"perm-viewer@test.com","password":"test123","name":"Permission Viewer"}')

VIEWER_ONBOARDING=$(echo "$VIEWER_RESP" | json_field "['onboarding_token']")
[ -z "$VIEWER_ONBOARDING" ] && fail "Viewer registration failed (no onboarding_token): $VIEWER_RESP"
log "Viewer registered (onboarding token obtained)."

# ── Create invite links ──────────────────────────────────────────────
log "Creating invite link for editor..."
EDITOR_INVITE=$(curl -s -X POST "$API/members/invite-link" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"editor"}')
EDITOR_INVITE_TOKEN=$(echo "$EDITOR_INVITE" | json_field "['token']")
[ -z "$EDITOR_INVITE_TOKEN" ] && fail "Editor invite link creation failed: $EDITOR_INVITE"
log "Editor invite token: ${EDITOR_INVITE_TOKEN:0:16}..."

log "Creating invite link for viewer..."
VIEWER_INVITE=$(curl -s -X POST "$API/members/invite-link" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"viewer"}')
VIEWER_INVITE_TOKEN=$(echo "$VIEWER_INVITE" | json_field "['token']")
[ -z "$VIEWER_INVITE_TOKEN" ] && fail "Viewer invite link creation failed: $VIEWER_INVITE"
log "Viewer invite token: ${VIEWER_INVITE_TOKEN:0:16}..."

# ── Editor joins org ─────────────────────────────────────────────────
log "Editor joining org..."
EDITOR_JOIN_RESP=$(curl -s -X POST "$API/auth/org/join" \
  -H "Authorization: Bearer $EDITOR_ONBOARDING" \
  -H "Content-Type: application/json" \
  -d "{\"invite_link_token\":\"$EDITOR_INVITE_TOKEN\"}")

EDITOR_TOKEN=$(echo "$EDITOR_JOIN_RESP" | json_field "['token']")
EDITOR_USER_ID=$(echo "$EDITOR_JOIN_RESP" | json_field "['user']['id']")
[ -z "$EDITOR_TOKEN" ] && fail "Editor join failed: $EDITOR_JOIN_RESP"
log "Editor joined: id=$EDITOR_USER_ID"

# ── Viewer joins org ─────────────────────────────────────────────────
log "Viewer joining org..."
VIEWER_JOIN_RESP=$(curl -s -X POST "$API/auth/org/join" \
  -H "Authorization: Bearer $VIEWER_ONBOARDING" \
  -H "Content-Type: application/json" \
  -d "{\"invite_link_token\":\"$VIEWER_INVITE_TOKEN\"}")

VIEWER_TOKEN=$(echo "$VIEWER_JOIN_RESP" | json_field "['token']")
VIEWER_USER_ID=$(echo "$VIEWER_JOIN_RESP" | json_field "['user']['id']")
[ -z "$VIEWER_TOKEN" ] && fail "Viewer join failed: $VIEWER_JOIN_RESP"
log "Viewer joined: id=$VIEWER_USER_ID"

# ── Create resources ─────────────────────────────────────────────────
log "Creating resources..."

# Notebook
NB_RESP=$(curl -s -X POST "$API/notebooks" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Permission Test Notebook"}')
NOTEBOOK_ID=$(echo "$NB_RESP" | json_field "['id']")
[ -z "$NOTEBOOK_ID" ] && fail "Notebook creation failed: $NB_RESP"
log "Notebook: $NOTEBOOK_ID"

# Dashboard
DASH_RESP=$(curl -s -X POST "$API/dashboards" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Permission Test Dashboard"}')
DASHBOARD_ID=$(echo "$DASH_RESP" | json_field "['id']")
[ -z "$DASHBOARD_ID" ] && fail "Dashboard creation failed: $DASH_RESP"
log "Dashboard: $DASHBOARD_ID"

# Connector (postgres)
CONN_RESP=$(curl -s -X POST "$API/connectors" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Connector","type":"postgres","config":{"host":"hnb-postgres","port":5432,"user":"hnb","password":"hnb_dev","database":"hnb"}}')
CONNECTOR_ID=$(echo "$CONN_RESP" | json_field "['id']")
[ -z "$CONNECTOR_ID" ] && fail "Connector creation failed: $CONN_RESP"
log "Connector: $CONNECTOR_ID"

# Agent
AGENT_RESP=$(curl -s -X POST "$API/agents" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Agent","system_prompt":"You are a test agent."}')
AGENT_ID=$(echo "$AGENT_RESP" | json_field "['id']")
[ -z "$AGENT_ID" ] && fail "Agent creation failed: $AGENT_RESP"
log "Agent: $AGENT_ID"

# Skill
SKILL_RESP=$(curl -s -X POST "$API/skills" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Skill","description":"A test skill."}')
SKILL_ID=$(echo "$SKILL_RESP" | json_field "['id']")
[ -z "$SKILL_ID" ] && fail "Skill creation failed: $SKILL_RESP"
log "Skill: $SKILL_ID"

# Model Config
MC_RESP=$(curl -s -X POST "$API/model-configs" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Model","provider":"openai","model":"gpt-4","api_key":"sk-test"}')
MODEL_CONFIG_ID=$(echo "$MC_RESP" | json_field "['id']")
[ -z "$MODEL_CONFIG_ID" ] && fail "Model config creation failed: $MC_RESP"
log "Model Config: $MODEL_CONFIG_ID"

# Folder
FOLDER_RESP=$(curl -s -X POST "$API/folders" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Permission Test Folder"}')
FOLDER_ID=$(echo "$FOLDER_RESP" | json_field "['id']")
[ -z "$FOLDER_ID" ] && fail "Folder creation failed: $FOLDER_RESP"
log "Folder: $FOLDER_ID"

# ── Write state file ─────────────────────────────────────────────────
log "Writing state file to $STATE_FILE"
cat > "$STATE_FILE" <<EOF
# Permission Review State — auto-generated by setup.sh
export ADMIN_TOKEN="$ADMIN_TOKEN"
export ADMIN_USER_ID="$ADMIN_USER_ID"
export EDITOR_TOKEN="$EDITOR_TOKEN"
export EDITOR_USER_ID="$EDITOR_USER_ID"
export VIEWER_TOKEN="$VIEWER_TOKEN"
export VIEWER_USER_ID="$VIEWER_USER_ID"
export ORG_ID="$ORG_ID"
export NOTEBOOK_ID="$NOTEBOOK_ID"
export DASHBOARD_ID="$DASHBOARD_ID"
export CONNECTOR_ID="$CONNECTOR_ID"
export AGENT_ID="$AGENT_ID"
export SKILL_ID="$SKILL_ID"
export MODEL_CONFIG_ID="$MODEL_CONFIG_ID"
export FOLDER_ID="$FOLDER_ID"
EOF

log "============================================"
log "Setup complete! Source the state file:"
log "  source $STATE_FILE"
log "============================================"
log ""
log "Users:"
log "  Admin:   perm-admin@test.com / test123 (role=admin)"
log "  Editor:  perm-editor@test.com / test123 (role=editor)"
log "  Viewer:  perm-viewer@test.com / test123 (role=viewer)"
log ""
log "Resources:"
log "  Notebook:     $NOTEBOOK_ID"
log "  Dashboard:    $DASHBOARD_ID"
log "  Connector:    $CONNECTOR_ID"
log "  Agent:        $AGENT_ID"
log "  Skill:        $SKILL_ID"
log "  Model Config: $MODEL_CONFIG_ID"
log "  Folder:       $FOLDER_ID"
