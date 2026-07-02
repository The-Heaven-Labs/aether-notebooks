#!/bin/sh
set -e

GARAGE_HOST="http://aether-garage:3903"
TOKEN="dev-admin-token"
BUCKET="aether-attachments"
KEY_NAME="aether-dev-key"
ACCESS_KEY="GKacc5e39c17a68ea60adf92db"
SECRET_KEY="557351ff6f3e11eb8a1810af9647f9af9d3b0ce2e5786d2b186d327f091ab65c"

# Install garage CLI
echo "Installing garage CLI..."
ARCH="x86_64"
[ "$(uname -m)" = "aarch64" ] && ARCH="aarch64"
curl -sL "https://garagehq.deuxfleurs.fr/_releases/v1.0.1/${ARCH}-unknown-linux-musl/garage" -o /usr/local/bin/garage 2>/dev/null || \
  curl -sL "https://garagehq.deuxfleurs.fr/_releases/v1.0.1/${ARCH}-unknown-linux-musc/garage" -o /usr/local/bin/garage
chmod +x /usr/local/bin/garage

echo "Waiting for Garage..."
until curl -sf "$GARAGE_HOST/health" >/dev/null 2>&1; do
  sleep 1
done

NODE_ID=$(garage --host "$GARAGE_HOST" --admin-token "$TOKEN" status 2>/dev/null | tail -n +5 | head -1 | awk '{print $1}')
echo "Node ID: $NODE_ID"

# Configure layout (idempotent)
if ! garage --host "$GARAGE_HOST" --admin-token "$TOKEN" layout show 2>/dev/null | grep -q "ROLES"; then
  echo "Configuring layout..."
  garage --host "$GARAGE_HOST" --admin-token "$TOKEN" layout assign -z dc1 -c 10G "$NODE_ID"
  garage --host "$GARAGE_HOST" --admin-token "$TOKEN" layout apply --version 1
fi

# Create bucket (idempotent)
garage --host "$GARAGE_HOST" --admin-token "$TOKEN" bucket info "$BUCKET" >/dev/null 2>&1 || \
  garage --host "$GARAGE_HOST" --admin-token "$TOKEN" bucket create "$BUCKET"

# Import key with known credentials (idempotent)
garage --host "$GARAGE_HOST" --admin-token "$TOKEN" key info "$ACCESS_KEY" >/dev/null 2>&1 || \
  garage --host "$GARAGE_HOST" --admin-token "$TOKEN" key import --yes "$ACCESS_KEY" "$SECRET_KEY" --name "$KEY_NAME"

# Allow key on bucket
garage --host "$GARAGE_HOST" --admin-token "$TOKEN" bucket allow "$BUCKET" --key "$ACCESS_KEY" --owner --read --write 2>/dev/null || true

echo ""
echo "Garage ready — bucket=$BUCKET key=$ACCESS_KEY"
