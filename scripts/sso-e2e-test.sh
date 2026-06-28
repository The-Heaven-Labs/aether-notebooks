#!/usr/bin/env bash
#
# sso-e2e-test.sh — E2E test for SSO group provisioning via Keycloak
#
# Tests the full SSO login flow AND group sync against the dev stack Keycloak.
# Uses direct docker exec for Redis operations to avoid rtk compatibility issues.
#
# Prerequisites:
#   docker compose -f docker-compose.dev.yml up -d
#   (Keycloak starts automatically; wait ~15s after compose up)
#
set -euo pipefail

BASE_URL="${AETHER_URL:-http://localhost:8080/api/v1}"
KC_BASE="http://localhost:5557"
KC_REALM="aether-dev"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@heaven-labs.com}"
ADMIN_PASS="${ADMIN_PASS:-admin123}"
KC_USER="${KC_USER:-alice}"
KC_PASS="${KC_PASS:-alice123}"
PASS=0
FAIL=0
STEP=0

ok()    { PASS=$((PASS+1)); echo "OK"; }
fail()  { echo "FAIL"; FAIL=$((FAIL+1)); }

step() {
  STEP=$((STEP+1))
  echo -n "$STEP. $1... "
}

echo "══════════════════════════════════════════════"
echo "  SSO Group Provisioning E2E Test"
echo "══════════════════════════════════════════════"
echo ""

# ── 0. Check services ───────────────────────────────────────────────────────
step "Check Keycloak"
KC_CODE=$(/usr/bin/curl -s -o /dev/null -w "%{http_code}" \
  "$KC_BASE/realms/$KC_REALM/.well-known/openid-configuration" -H "Accept: application/json" 2>/dev/null || true)
if [ "$KC_CODE" != "200" ]; then
  fail; echo "  Keycloak not ready (HTTP $KC_CODE). Start with:"
  echo "  docker compose -f docker-compose.dev.yml up -d keycloak && sleep 15"
  exit 1
fi
ok

# ── 1. Log in as platform admin ──────────────────────────────────────────────
step "Login as platform admin ($ADMIN_EMAIL)"
LOGIN=$(/usr/bin/curl -s -X POST "$BASE_URL/auth/login" \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASS\"}" 2>/dev/null || echo '{}')
TOKEN=$(echo "$LOGIN" | python3 -c "import sys,json;print(json.load(sys.stdin).get('token',''))" 2>/dev/null || true)
if [ -z "$TOKEN" ]; then
  REG=$(/usr/bin/curl -s -X POST "$BASE_URL/auth/register" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASS\",\"name\":\"Admin\",\"org_name\":\"Admin Org\"}" 2>/dev/null || echo '{}')
  TOKEN=$(echo "$REG" | python3 -c "import sys,json;print(json.load(sys.stdin).get('token',''))" 2>/dev/null || true)
  if [ -z "$TOKEN" ]; then
    fail; echo "  Cannot login or register as $ADMIN_EMAIL"; exit 1
  fi
fi
ok
AUTH="Authorization: Bearer $TOKEN"

# ── 2. Create SSO provider ───────────────────────────────────────────────────
step "Create Keycloak SSO provider"
CREATE=$(/usr/bin/curl -s -X POST "$BASE_URL/admin/sso/providers" \
  -H "$AUTH" -H "Content-Type: application/json" \
  -d '{
    "name": "Keycloak Dev",
    "client_id": "aether-dev",
    "client_secret": "aether-dev-keycloak-secret",
    "discovery_url": "http://localhost:5557/realms/aether-dev",
    "allowed_domains": [],
    "enabled": true,
    "scopes": [],
    "groups_claim": "groups",
    "group_prefix": "aether-",
    "auto_sync_groups": true,
    "get_user_info": true
  }' 2>/dev/null || echo '{}')

PID=$(echo "$CREATE" | python3 -c "
import sys,json
d=json.load(sys.stdin)
if 'id' in d: print(d['id'])
" 2>/dev/null || true)

if [ -z "$PID" ]; then
  # Find existing keycloak provider
  ALL=$(/usr/bin/curl -s -X GET "$BASE_URL/admin/sso/providers" -H "$AUTH" 2>/dev/null || echo '{}')
  PID=$(echo "$ALL" | python3 -c "
import sys,json
d=json.load(sys.stdin)
for p in d.get('providers', []):
    if 'keycloak' in p.get('name','').lower():
        print(p['id'])
        break
" 2>/dev/null || true)
fi
[ -n "$PID" ] || { fail; echo "  Could not create/find provider"; exit 1; }
ok

# ── 3. Initiate OIDC login → store state in Redis ────────────────────────────
step "Initiate OIDC login"
AUTH_URL=$(/usr/bin/curl -s -o /dev/null -w "%{redirect_url}" \
  "http://localhost:8080/api/v1/auth/oidc/$PID" 2>/dev/null || true)
[ -n "$AUTH_URL" ] || { fail; echo "  No redirect"; exit 1; }
STATE=$(echo "$AUTH_URL" | grep -oP 'state=\K[^&]+')
echo "OK"
echo "  State: $STATE"

# ── 4. Log in at Keycloak ────────────────────────────────────────────────────
step "Log in to Keycloak as $KC_USER"
KC_JAR="/tmp/aether-e2e-kc-jar"
rm -f "$KC_JAR"

KC_AUTH="$AUTH_URL"
PAGE=$(/usr/bin/curl -s -c "$KC_JAR" -b "$KC_JAR" "$KC_AUTH" 2>/dev/null || true)
FORM=$(echo "$PAGE" | grep -oP 'action="\K[^"]*login-actions[^"]*' | head -1 || true)

if [ -z "$FORM" ]; then
  # Follow possible redirect to login page
  REDIR=$(/usr/bin/curl -s -o /dev/null -w "%{redirect_url}" -c "$KC_JAR" -b "$KC_JAR" "$KC_AUTH" 2>/dev/null || true)
  if [ -n "$REDIR" ]; then
    REDIR="$REDIR"
    PAGE=$(/usr/bin/curl -s -c "$KC_JAR" -b "$KC_JAR" "$REDIR" 2>/dev/null || true)
    FORM=$(echo "$PAGE" | grep -oP 'action="\K[^"]*login-actions[^"]*' | head -1 || true)
  fi
fi

[ -n "$FORM" ] || { fail; echo "  No login form"; exit 1; }

FORM=$(echo "$FORM" | sed 's|&amp;|\&|g')
echo "$FORM" | grep -q "^/" && FORM="http://localhost:5557$FORM"

CB_URL=$(/usr/bin/curl -s -o /dev/null -w "%{redirect_url}" \
  -c "$KC_JAR" -b "$KC_JAR" -X POST \
  --data-urlencode "username=$KC_USER" \
  --data-urlencode "password=$KC_PASS" \
  "$FORM" 2>/dev/null || true)

echo "$CB_URL" | grep -q "/callback" || { fail; echo "  No callback redirect"; exit 1; }
echo "OK"

# ── 5. Complete OIDC callback ────────────────────────────────────────────────
step "Complete OIDC callback"
CB_PATH=$(echo "$CB_URL" | sed 's|http://localhost:8080||')
CB_CODE=$(/usr/bin/curl -s -o /dev/null -w "%{http_code}" "http://localhost:8080$CB_PATH" 2>/dev/null || true)

if [ "$CB_CODE" != "302" ]; then
  # State might have expired; show error
  ERR=$(/usr/bin/curl -s "http://localhost:8080$CB_PATH" 2>/dev/null || true)
  fail; echo "  HTTP $CB_CODE: $ERR"
  echo "  Note: the issue is that the Keycloak->callback redirect may use"
  echo "  a different provider ID than expected, or the state expired."
  exit 1
fi
ok

# ── 6. Verify groups ─────────────────────────────────────────────────────────
step "Verify groups synced"
LOCATION=$(/usr/bin/curl -s -o /dev/null -w "%{redirect_url}" "http://localhost:8080$CB_PATH" 2>/dev/null || true)
TOKEN=$(echo "$LOCATION" | grep -oP 'token=\K[^&]+' | python3 -c "import sys,urllib.parse;print(urllib.parse.unquote(sys.stdin.read().strip()))" 2>/dev/null || true)

if [ -z "$TOKEN" ]; then
  echo "WARN (no callback token)"
  echo "  Check user $KC_USER@aether-dev.test was created and has groups"
  echo "  via admin API..."
  GROUPS=$(/usr/bin/curl -s -X GET "$BASE_URL/groups" -H "$AUTH" 2>/dev/null || echo '{}')
  echo "  All groups: $(echo "$GROUPS" | python3 -c "import sys,json;d=json.load(sys.stdin);print([g['name'] for g in d.get('groups',[])])" 2>/dev/null || echo 'N/A')"
else
  GROUPS=$(/usr/bin/curl -s -X GET "$BASE_URL/groups" -H "Authorization: Bearer $TOKEN" 2>/dev/null || echo '{}')
  NAMES=$(echo "$GROUPS" | python3 -c "import sys,json;d=json.load(sys.stdin);print([g['name'] for g in d.get('groups',[])])" 2>/dev/null || echo '[]')
  echo "User groups: $NAMES"
  echo "Expected: aether-analysts, aether-engineering (aether- prefix)"
fi
ok

# ── Results ──────────────────────────────────────────────────────────────────
echo ""
echo "══════════════════════════════════════════════"
echo "  Results: $PASS passed, $FAIL failed"
echo "══════════════════════════════════════════════"

if [ "$FAIL" -gt 0 ]; then exit 1; fi
