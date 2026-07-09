#!/bin/sh
set -e

BUCKET="aether-attachments"
AUDIT_BUCKET="aether-audit-logs"
KEY_ID="GKacc5e39c17a68ea60adf92db"
KEY_SECRET="557351ff6f3e11eb8a1810af9647f9af9d3b0ce2e5786d2b186d327f091ab65c"
GARAGE_VERSION="v1.0.1"

echo "Waiting for Garage..."
until curl -sf http://aether-garage:3903/health >/dev/null 2>&1; do
  sleep 1
done

GARAGE_URL="https://garagehq.deuxfleurs.fr/_releases/${GARAGE_VERSION}/x86_64-unknown-linux-musl/garage"
echo "Downloading Garage CLI..."
curl -sfL "$GARAGE_URL" -o /garage
chmod +x /garage

STATUS=$(curl -sf -H "Authorization: Bearer dev-admin-token" http://aether-garage:3903/v1/status)
LAYOUT_VERSION=$(echo "$STATUS" | grep -o 'layoutVersion.*' | cut -d: -f2 | tr -d ' ,')

if [ "$LAYOUT_VERSION" = "0" ] || [ -z "$LAYOUT_VERSION" ]; then
  echo "Configuring cluster layout..."
  NODE_ID=$(echo "$STATUS" | grep -o '"node".*' | cut -d'"' -f4)
  /garage -c /etc/garage/config.toml layout assign --tag aether-dev -z dc1 -c 100G "$NODE_ID"
  /garage -c /etc/garage/config.toml layout apply --version 1
  echo "Layout configured."
else
  echo "Layout already configured (version $LAYOUT_VERSION)."
fi

echo "Creating buckets via admin API..."
curl -sf -X POST -H "Authorization: Bearer dev-admin-token" \
  "http://aether-garage:3903/bucket?globalAlias=$BUCKET" 2>/dev/null || true
curl -sf -X POST -H "Authorization: Bearer dev-admin-token" \
  "http://aether-garage:3903/bucket?globalAlias=$AUDIT_BUCKET" 2>/dev/null || true

curl -sf -X POST -H "Authorization: Bearer dev-admin-token" \
  "http://aether-garage:3903/key?import=true" \
  -d "importKeyId=$KEY_ID&secretKey=$KEY_SECRET&name=aether-dev-key" 2>/dev/null || true

echo ""
echo "Garage ready — bucket=$BUCKET audit_bucket=$AUDIT_BUCKET key=$KEY_ID"
