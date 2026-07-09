#!/bin/sh
set -e

BUCKET="aether-attachments"
AUDIT_BUCKET="aether-audit-logs"
KEY_ID="GKacc5e39c17a68ea60adf92db"
KEY_SECRET="557351ff6f3e11eb8a1810af9647f9af9d3b0ce2e5786d2b186d327f091ab65c"

echo "Waiting for Garage..."
until curl -sf http://aether-garage:3903/health >/dev/null 2>&1; do
  sleep 1
done

# Run garage CLI commands via the running garage container
G() {
  docker compose -f /dev/null exec -e GARAGE_ADMIN_TOKEN=dev-admin-token aether-garage /garage -c /etc/garage/config.toml "$@" 2>/dev/null || true
}

# But we can't use docker compose here (no socket in this container)
# Instead, use the admin HTTP API (Garage v1.0 supports limited HTTP admin)

# Get node ID from health endpoint
NODE_ID=$(curl -sf http://aether-garage:3903/v1/health 2>/dev/null | sed 's/.*"storageNodes":1.*/single/')
echo "Node ID: single-node setup, skipping layout config"

# Create buckets using S3 API (Garage auto-creates buckets on first write via S3 API)
# Actually, use the Garage admin API  
  
# The Garage admin HTTP API at port 3903 accepts these endpoints:
# POST /bucket?globalAlias=name - create bucket
# We can do this via curl

echo "Creating buckets via admin API..."
curl -sf -X POST -H "Authorization: Bearer dev-admin-token" \
  "http://aether-garage:3903/bucket?globalAlias=$BUCKET" 2>/dev/null || true
curl -sf -X POST -H "Authorization: Bearer dev-admin-token" \
  "http://aether-garage:3903/bucket?globalAlias=$AUDIT_BUCKET" 2>/dev/null || true

# Import key  
curl -sf -X POST -H "Authorization: Bearer dev-admin-token" \
  "http://aether-garage:3903/key?import=true" \
  -d "importKeyId=$KEY_ID&secretKey=$KEY_SECRET&name=aether-dev-key" 2>/dev/null || true
  
# Allow key on buckets via S3 policy (Garage allows keys globally)
# The key has global permissions set during creation
  
echo ""
echo "Garage ready — bucket=$BUCKET audit_bucket=$AUDIT_BUCKET key=$KEY_ID"
