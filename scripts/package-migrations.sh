#!/bin/bash
set -euo pipefail
VERSION="${GORELEASER_CURRENT_TAG:-$(git describe --tags --abbrev=0 2>/dev/null || echo '0.0.0')}"
mkdir -p dist
cd internal/database/migrations
tar czf "../../../dist/aether-migrations-${VERSION}.tar.gz" \
  --transform="s|^|migrations/|" \
  *.sql
