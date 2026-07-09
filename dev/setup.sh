#!/bin/sh
set -e


API_URL="http://api:8080"

echo "Waiting for API..."
until curl -sf "$API_URL/health" >/dev/null 2>&1; do
  sleep 5
done
echo "API ready."

cd /app

# Configure CLI
go run ./cmd/aether config set api-url "$API_URL"
go run ./cmd/aether config set api-url "$API_URL"

# Register admin user (ok if already exists)
echo "Registering admin user..."
go run ./cmd/aether register --email admin@heaven-labs.com --password admin123 --name "Admin" 2>/dev/null || true

# Login
echo "Logging in..."
go run ./cmd/aether login --email admin@heaven-labs.com --password admin123

# Create Heaven Labs org (ok if already exists)
echo "Creating Heaven Labs org..."
go run ./cmd/aether admin orgs create --name "Heaven Labs" --slug heaven-labs 2>/dev/null || true

# Re-login to get token for the new org
go run ./cmd/aether login --email admin@heaven-labs.com --password admin123

# Create connectors (ok if already exists)
echo "Creating Postgres connector..."
go run ./cmd/aether connectors create \
  --name "Postgres (Dev)" \
  --type postgres \
  --config '{"host":"aether-postgres","port":5432,"user":"aether","password":"aether_dev","database":"aether","ssl_mode":"disable"}' 2>/dev/null || true

echo "Creating ClickHouse connector..."
go run ./cmd/aether connectors create \
  --name "ClickHouse (Dev)" \
  --type clickhouse \
  --config '{"host":"aether-clickhouse","port":9000,"user":"dev","password":"dev","database":"default"}' 2>/dev/null || true

echo "Creating OpenSearch connector..."
go run ./cmd/aether connectors create \
  --name "OpenSearch (Dev)" \
  --type opensearch \
  --config '{"host":"aether-opensearch","port":9200,"user":"","password":"","scheme":"http"}' 2>/dev/null || true

# Create additional test users
echo "Creating test users..."
go run ./cmd/aether register --email nova@heaven-labs.com --password nova123 --name "Nova" 2>/dev/null || true
go run ./cmd/aether register --email sol@heaven-labs.com --password sol123 --name "Sol" 2>/dev/null || true

# Add test users to Heaven Labs org via API
TOKEN=$(cat "$HOME/.aether/credentials.json" 2>/dev/null | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
if [ -n "$TOKEN" ]; then
  for USER_EMAIL in nova@heaven-labs.com sol@heaven-labs.com; do
    USER_ID=$(curl -sf -H "Authorization: Bearer $TOKEN" "http://api:8080/api/v1/admin/users?search=$USER_EMAIL" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
    if [ -n "$USER_ID" ]; then
      curl -sf -X POST -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" \
        "http://api:8080/api/v1/members" \
        -d "{\"user_id\":\"$USER_ID\",\"role\":\"editor\"}" >/dev/null 2>&1 || true
      echo "  Added $USER_EMAIL to org"
    fi
  done
fi

echo "Dev setup complete."
