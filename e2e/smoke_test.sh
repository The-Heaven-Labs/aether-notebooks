#!/bin/bash
set -euo pipefail

API="${AETHER_API_URL:-http://localhost:8080}"
EMAIL="smoke-$(date +%s)@example.com"
PASSWORD="smoke-pass-123"

echo "=== Aether Smoke Test ==="
echo "API: $API"

# 1. Health check
echo ""
echo "1. Health check..."
STATUS=$(curl -sf "$API/health" | grep -c '"status":"ok"')
[ "$STATUS" -ge 1 ] && echo "   PASS" || (echo "   FAIL: health check failed"; exit 1)

# 2. Register account-only (onboarding flow)
echo ""
echo "2. Register user..."
REGISTER=$(curl -sf -X POST "$API/api/v1/auth/register" \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"name\":\"Smoke Test\"}")
ONBOARDING_TOKEN=$(echo "$REGISTER" | grep -o '"onboarding_token":"[^"]*"' | cut -d'"' -f4)
[ -n "$ONBOARDING_TOKEN" ] && echo "   PASS (onboarding token received)" || (echo "   FAIL: no onboarding token"; exit 1)

# 3. Create org via onboarding token
echo ""
echo "3. Create organization..."
ORG=$(curl -sf -X POST "$API/api/v1/auth/org/create" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ONBOARDING_TOKEN" \
  -d "{\"org_name\":\"Smoke Org $(date +%s)\"}")
TOKEN=$(echo "$ORG" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
[ -n "$TOKEN" ] && echo "   PASS (token received)" || (echo "   FAIL: no token"; exit 1)

# 4. Create connector
echo ""
echo "4. Create connector..."
CONNECTOR=$(curl -sf -X POST "$API/api/v1/connectors" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"Smoke DB","type":"postgres","config":{"host":"localhost","port":5432,"user":"aether","password":"aether_dev","database":"aether"}}')
CONNECTOR_ID=$(echo "$CONNECTOR" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$CONNECTOR_ID" ] && echo "   PASS (connector: $CONNECTOR_ID)" || (echo "   FAIL: no connector id"; exit 1)

# 5. Create notebook
echo ""
echo "5. Create notebook..."
NOTEBOOK=$(curl -sf -X POST "$API/api/v1/notebooks" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"Smoke Notebook"}')
NB_ID=$(echo "$NOTEBOOK" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$NB_ID" ] && echo "   PASS (notebook: $NB_ID)" || (echo "   FAIL: no notebook id"; exit 1)

# 6. Create cell
echo ""
echo "6. Create cell..."
CELL=$(curl -sf -X POST "$API/api/v1/notebooks/$NB_ID/cells" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d "{\"type\":\"code\",\"language\":\"sql\",\"source\":\"SELECT 1 AS result\",\"connector_id\":\"$CONNECTOR_ID\"}")
CELL_ID=$(echo "$CELL" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$CELL_ID" ] && echo "   PASS (cell: $CELL_ID)" || (echo "   FAIL: no cell id"; exit 1)

# 7. Execute cell
echo ""
echo "7. Execute cell..."
EXEC=$(curl -sf -X POST "$API/api/v1/notebooks/$NB_ID/cells/$CELL_ID/execute" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{}')
HAS_OUTPUT=$(echo "$EXEC" | grep -c '"outputs"')
[ "$HAS_OUTPUT" -ge 1 ] && echo "   PASS (outputs received)" || (echo "   FAIL: no outputs in response"; exit 1)

# 8. Create dashboard
echo ""
echo "8. Create dashboard..."
DASH=$(curl -sf -X POST "$API/api/v1/dashboards" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"Smoke Dashboard"}')
DASH_ID=$(echo "$DASH" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -n "$DASH_ID" ] && echo "   PASS (dashboard: $DASH_ID)" || (echo "   FAIL: no dashboard id"; exit 1)

# 9. Share dashboard
echo ""
echo "9. Share dashboard..."
SHARE=$(curl -sf -X POST "$API/api/v1/dashboards/$DASH_ID/share" \
  -H "Authorization: Bearer $TOKEN")
PUBLIC_TOKEN=$(echo "$SHARE" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
[ -n "$PUBLIC_TOKEN" ] && echo "   PASS (public token received)" || (echo "   FAIL: no public token"; exit 1)

# 10. Access public dashboard
echo ""
echo "10. Public dashboard access..."
PUB=$(curl -sf "$API/api/v1/public/$PUBLIC_TOKEN")
[ -n "$PUB" ] && echo "   PASS" || (echo "   FAIL: public dashboard not accessible"; exit 1)

echo ""
echo "=== All smoke tests PASSED ==="
