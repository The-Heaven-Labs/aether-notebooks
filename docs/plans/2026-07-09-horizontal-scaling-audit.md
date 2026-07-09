# Horizontal Scaling & Kubernetes Readiness Audit

**Date:** 2026-07-09
**Scope:** Analysis of the Aether codebase for running multiple replicas behind a load balancer in Kubernetes.

## Architecture Overview

```
                  ┌─────────────┐
                  │  Load       │
                  │  Balancer   │
                  └──────┬──────┘
                         │
         ┌───────────────┼───────────────┐
         │               │               │
    ┌────▼────┐    ┌────▼────┐    ┌────▼────┐
    │ API Pod │    │ API Pod │    │ API Pod │  ← Go API server
    │   #1    │    │   #2    │    │   #3    │
    └────┬────┘    └────┬────┘    └────┬────┘
         │              │              │
         └──────┬───────┴───────┬──────┘
                │               │
         ┌──────▼──────┐  ┌─────▼─────┐
         │  Postgres   │  │   Redis   │
         │  (shared)   │  │  (shared) │
         └─────────────┘  └───────────┘

    ┌─────────────┐    ┌─────────────┐
    │  Relay Pod  │    │  Relay Pod  │  ← Hocuspocus/Node
    │   #1        │    │   #2        │
    └─────────────┘    └─────────────┘
         │                    │
         └──────────┬─────────┘
                    │
            ┌───────▼───────┐
            │ Redis Pub/Sub │
            │ (Yjs sync)    │
            └───────────────┘
```

## 1. WebSocket State — Critical

### Problem
The WebSocket Hub (`internal/api/ws.go:39-47`) stores room membership as `map[string]map[*wsConn]bool` — purely in-memory. Broadcasting only reaches connections on the same pod. Agent streaming (`internal/agent/stream.go`) is also in-memory with a rolling 500-event buffer.

If user A connects to pod 1 and user B connects to pod 2:
- Notebook cell broadcasts don't cross pods
- Agent chat messages from pod 1 are invisible to pod 2
- A WebSocket reconnect to a different pod loses all in-flight state

### Fix
Replace `Hub.rooms` with Redis Pub/Sub channels:
- On room join: `SUBSCRIBE notebook:{id}` and `PUBLISH notebook:{id}:join {user}`
- On broadcast: `PUBLISH notebook:{id}:event {payload}` — all pods receive it
- Agent streams: use Redis streams instead of in-memory channel list with `[]chan any`

## 2. In-Memory Agent State — Critical

### Problem
The agent engine (`internal/agent/engine.go`) stores per-session metadata in `sync.Map`:
- `reasoningEffort` — selected reasoning level
- `pageContextMap` — current page/notebook context
- `sessionModelConfig` — model overrides
- `toolConfirmPending` — pending tool confirmations
- `runningCells` (`ws.go:42`) — executing cell tracking

All lost when a WebSocket reconnects to a different pod.

### Fix
Move these to Redis hashes keyed by session ID:
- `HSET agent:session:{id} reasoning_effort low`
- `HSET agent:session:{id} page_context {...}`
- TTL-based expiry (e.g., 1 hour after last access)

## 3. Database Migrations — High

### Problem
`internal/database/migrate.go` applies migrations at startup with no distributed locking. If two pods start simultaneously:

```
Pod A: BEGIN; APPLY migration 5; INSERT schema_version 5; COMMIT
Pod B: BEGIN; APPLY migration 5; INSERT schema_version 5; COMMIT  ← duplicate
```

Migrations are inside transactions and the SQL is designed to be idempotent, but the version check (`SELECT version FROM schema_migrations WHERE version = $1`) has a race window between the check and the insert.

### Fix
Wrap migration with a Postgres advisory lock:

```sql
SELECT pg_advisory_lock(989898);  -- magic app ID
-- ... run pending migrations ...
SELECT pg_advisory_unlock(989898);
```

Or: run migrations as a Kubernetes init container/job before the deployment rollout.

## 4. Scheduler — High

### Problem
`internal/scheduler/scheduler.go` ticks every minute on every pod. All pods query `SELECT ... FROM schedules WHERE enabled AND next_run_at <= NOW()` and all execute the same notebooks. Duplicate executions guaranteed.

### Fix
Use `SELECT ... FOR UPDATE SKIP LOCKED` when claiming a schedule row:

```sql
UPDATE schedules
SET next_run_at = next_run_at + interval '1 minute'
WHERE id = (
  SELECT id FROM schedules
  WHERE enabled AND next_run_at <= NOW()
  ORDER BY next_run_at
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
RETURNING *;
```

This ensures only one pod claims each schedule. The `SKIP LOCKED` (Postgres 9.5+) skips rows already locked by another pod.

For the midnight rollup/cleanup tasks, use a similar locking approach or a Postgres advisory lock.

## 5. Yjs/Relay — High

### Problem
The Hocuspocus relay (`relay/src/index.ts`) uses no cross-instance sync extension. Each relay pod is fully isolated. Two users editing the same notebook on different relay pods don't see real-time changes. Document state eventually converges on save to Postgres, but awareness/cursors are lost.

### Fix
Add `@hocuspocus/extension-redis`:

```typescript
import { Redis } from '@hocuspocus/extension-redis'

const server = new Server({
  extensions: [
    new Redis({
      host: process.env.REDIS_HOST || 'redis',
      port: parseInt(process.env.REDIS_PORT || '6379'),
    }),
  ],
  // ...
})
```

This syncs document updates and awareness across all relay instances via Redis Pub/Sub.

## 6. File Storage — Medium

### Problem
`AETHER_STORAGE_BACKEND=local` writes attachments to a local filesystem path (`AETHER_ATTACHMENT_DIR`, default `./attachments`). Not shared across pods.

`internal/storage/local.go` uses `os.Rename` which is atomic only within the same filesystem — workable on NFS but with consistency caveats.

### Fix
For K8s, use `AETHER_STORAGE_BACKEND=s3`. The S3 backend is already implemented and tested (used in the dev Docker Compose with Garage). If local storage is required, mount a `ReadWriteMany` PVC at the attachment path.

## 7. Health Checks — Low

### Problem
`GET /health` (`internal/api/router.go:431`) always returns `{"status": "ok"}` regardless of database or Redis connectivity. No separate liveness vs readiness probes.

### Fix
- `GET /healthz` (liveness): lightweight process check (current behavior)
- `GET /readyz` (readiness): verifies DB (`SELECT 1`), Redis (`PING`), and returns 503 if dependencies are down

## 8. Rate Limiting — Low

### Problem
Rate limiting only exists for the SSO probe endpoint (`internal/api/sso_probe_handlers.go:39-55`), using Redis `INCR`. No rate limiting on auth/login, registration, or agent execution endpoints.

### Fix
Add a Redis-based rate limiting middleware using a token bucket or sliding window pattern.

## Already Good

| Component | Why it scales |
|---|---|
| JWT Auth | Stateless, no server-side session |
| API tokens | Validated against shared Postgres |
| Agent sessions (`agent/session.go`) | Stored in Postgres |
| Internal routes (`/internal/yjs/*`) | Stateless, shared Postgres |
| S3 storage | Shared object store |
| Redis cache | Already shared (single Redis instance) |
