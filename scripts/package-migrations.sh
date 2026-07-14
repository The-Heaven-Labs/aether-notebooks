#!/bin/bash
set -euo pipefail
VERSION="${GORELEASER_CURRENT_TAG:-$(git describe --tags --abbrev=0 2>/dev/null || echo '0.0.0')}"
OUTDIR="${GITHUB_WORKSPACE:-$(git rev-parse --show-toplevel)}"
cd internal/database/migrations
tar czf "${OUTDIR}/aether-migrations-${VERSION}.tar.gz" \
  --transform="s|^|migrations/|" \
  *.sql
