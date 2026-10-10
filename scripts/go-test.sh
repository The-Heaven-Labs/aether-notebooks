#!/usr/bin/env bash
#
# Runs Go tests against a disposable, per-worktree test database.
#
# Why: tests hit a real Postgres (no mocks), but the shared dev database
# accumulates every org/user/ACL row ever created by a test run — by the time
# it reaches millions of ACL rows, a single test costs ~10s and the API
# package no longer fits its 3-minute budget. CI is fast because its Postgres
# container starts empty for every job; this script gives local runs the same
# starting point while keeping the dev data untouched.
#
# The test database name is derived from the worktree directory, so parallel
# worktrees never share a database. Redis DB 1 is used and flushed for the
# same reason (rate-limit counters and cache keys stop leaking between runs).
#
# Environment overrides:
#   AETHER_TEST_DB        database name (default: aether_test_<worktree dir>)
#   AETHER_TEST_DB_RESET  0 to keep the existing database (e.g. test:watch)
#
# Usage: scripts/go-test.sh [go test args...]
set -euo pipefail

if [ ! -f docker-compose.dev.yml ]; then
  echo "go-test.sh: run from the repository root" >&2
  exit 1
fi

DB_NAME="${AETHER_TEST_DB:-aether_test_$(basename "$PWD")}"
RESET="${AETHER_TEST_DB_RESET:-1}"

if [ "$RESET" = "1" ]; then
  echo "go-test.sh: resetting test database $DB_NAME"
  docker compose -f docker-compose.dev.yml exec -T aether-postgres \
    psql -U aether -d postgres -v ON_ERROR_STOP=1 \
    -c "DROP DATABASE IF EXISTS \"$DB_NAME\" WITH (FORCE);"
  docker compose -f docker-compose.dev.yml exec -T aether-postgres \
    psql -U aether -d postgres -v ON_ERROR_STOP=1 \
    -c "CREATE DATABASE \"$DB_NAME\";"
  docker compose -f docker-compose.dev.yml exec -T aether-redis \
    redis-cli -n 1 FLUSHDB >/dev/null
fi

export AETHER_DATABASE_URL="postgres://aether:aether_dev@localhost:5432/$DB_NAME?sslmode=disable"
export AETHER_REDIS_URL="redis://localhost:6379/1"
# CI sets these; without them the dev limiter (5/min) turns long runs into 429s.
export AETHER_RATE_LIMIT_REGISTER="${AETHER_RATE_LIMIT_REGISTER:-500}"
export AETHER_RATE_LIMIT_LOGIN="${AETHER_RATE_LIMIT_LOGIN:-500}"

# -p 1 matches CI's heavy-package runs: the api package alone takes ~160s of its
# 3-minute budget, and running packages concurrently pushes it over the limit.
exec go test -timeout 3m -p 1 "$@"
