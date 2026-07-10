# Horizontal Scaling Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Aether horizontally scalable for Kubernetes deployment — multiple API and relay replicas behind a load balancer.

**Architecture:** Replace per-pod in-memory state with Redis-backed shared state. Add distributed locking for migrations and scheduler. Add Redis Pub/Sub for cross-pod WebSocket broadcasting and Yjs document sync.

**Tech Stack:** Go, Redis Pub/Sub, Postgres advisory locks, `@hocuspocus/extension-redis`

---

### Task 1: Redis Pub/Sub for WebSocket Hub

**Files:**
- Modify: `internal/api/ws.go`
- Modify: `internal/cache/cache.go` (expose Redis pub/sub if needed)

**The problem:** `Hub.rooms` is `map[string]map[*wsConn]bool` — purely per-pod. Broadcasts (`Hub.Broadcast`) only reach connections on the same pod.

**The fix:** Replace `Hub.rooms with` Redis Pub/Sub. On room join, `SUBSCRIBE` to a channel per notebook. On broadcast, `PUBLISH` to that channel. All pods receive the message and forward to their local connections.

**Step 1: Add Redis Pub/Sub to the Hub struct**

```go
type Hub struct {
    rooms      map[string]map[*wsConn]bool
    mu         sync.RWMutex
    redis      *redis.Client
    pubsub     *redis.PubSub
    stopCh     chan struct{}
}
```

The `redis` and `pubsub` fields are the new additions. `pubsub` is a Redis Pub/Sub connection that receives messages from other pods.

**Step 2: Initialize Redis Pub/Sub in NewHub**

```go
func NewHub(rdb *redis.Client) *Hub {
    h := &Hub{
        rooms:  make(map[string]map[*wsConn]bool),
        redis:  rdb,
        pubsub: rdb.Subscribe(context.Background()),
        stopCh: make(chan struct{}),
    }
    go h.redisListener()
    return h
}
```

**Step 3: Implement redisListener goroutine**

```go
func (h *Hub) redisListener() {
    ch := h.pubsub.Channel()
    for {
        select {
        case msg := <-ch:
            // msg.Channel is "ws:{room}"
            // msg.Payload is JSON-encoded broadcast message
            room := strings.TrimPrefix(msg.Channel, "ws:")
            h.mu.RLock()
            conns := h.rooms[room]
            h.mu.RUnlock()
            for conn := range conns {
                select {
                case conn.send <- []byte(msg.Payload):
                default:
                    close(conn.send)
                    delete(conns, conn)
                }
            }
        case <-h.stopCh:
            return
        }
    }
}
```

**Step 4: Modify Broadcast to publish to Redis**

```go
func (h *Hub) Broadcast(room string, msg []byte) {
    if h.redis != nil {
        h.redis.Publish(context.Background(), "ws:"+room, string(msg))
    }
    // still send to local connections too
    h.mu.RLock()
    conns := h.rooms[room]
    h.mu.RUnlock()
    for conn := range conns {
        select {
        case conn.send <- msg:
        default:
            close(conn.send)
            delete(conns, conn)
        }
    }
}
```

Note: the local broadcast is kept for efficiency (avoid a Redis round-trip for local messages). The Redis publish ensures cross-pod delivery.

**Step 5: Handle room join/subscribe**

On `JoinRoom`, also subscribe to the Redis channel:

```go
func (h *Hub) JoinRoom(room string, conn *wsConn) {
    h.mu.Lock()
    if h.rooms[room] == nil {
        h.rooms[room] = make(map[*wsConn]bool)
        if h.redis != nil {
            h.pubsub.Subscribe(context.Background(), "ws:"+room)
        }
    }
    h.rooms[room][conn] = true
    h.mu.Unlock()
}
```

**Step 6: Handle leave/unsubscribe**

```go
func (h *Hub) LeaveRoom(room string, conn *wsConn) {
    h.mu.Lock()
    if conns := h.rooms[room]; conns != nil {
        delete(conns, conn)
        if len(conns) == 0 {
            delete(h.rooms, room)
            if h.redis != nil {
                h.pubsub.Unsubscribe(context.Background(), "ws:"+room)
            }
        }
    }
    h.mu.Unlock()
}
```

**Step 7: Wire Redis into NewHub call site**

Find where `NewHub` is called (likely `cmd/aether-server/main.go` or `internal/api/router.go`) and pass the Redis client.

**Step 8: Test**

Run: `task test:api` — verify existing WebSocket tests pass.

**Step 9: Commit**

```bash
git add internal/api/ws.go internal/cache/
git commit -m "feat: add Redis Pub/Sub for cross-pod WebSocket broadcasting"
```

---

### Task 2: Redis-Backed Agent Session State

**Files:**
- Modify: `internal/agent/engine.go`
- Modify: `internal/api/agent_ws.go`
- Modify: `internal/api/router.go`

**The problem:** Agent engine stores per-session metadata in `sync.Map` fields: `reasoningEffort`, `pageContextMap`, `sessionModelConfig`, `toolConfirmPending`. These are lost when a WebSocket reconnects to a different pod.

**The fix:** Store session metadata in Redis hashes with TTL. Read from Redis on each operation, falling back to the in-memory `sync.Map` (as a write-through cache).

**Step 1: Define Redis key helpers**

```go
func sessionKey(id string) string     { return "agent:session:" + id }
func reasoningKey(id string) string    { return sessionKey(id) + ":reasoning_effort" }
func pageCtxKey(id string) string      { return sessionKey(id) + ":page_context" }
func modelCfgKey(id string) string     { return sessionKey(id) + ":model_config" }
func toolConfirmKey(id string) string  { return sessionKey(id) + ":tool_confirm" }
```

**Step 2: Add Redis client to Engine**

```go
type Engine struct {
    redis                  *redis.Client
    reasoningEffort        sync.Map  // fallback cache
    toolConfirmPending     sync.Map
    pageContextMap         sync.Map
    sessionModelConfig     sync.Map
    mu                     sync.Mutex
}
```

**Step 3: Write Redis-backed getters/setters**

For each `sync.Map` access, implement a function that:
1. Reads from Redis
2. Falls back to `sync.Map` if Redis miss
3. Writes through to Redis on set

```go
func (e *Engine) GetReasoningEffort(sessionID string) string {
    if e.redis != nil {
        val, err := e.redis.Get(context.Background(), reasoningKey(sessionID)).Result()
        if err == nil {
            return val
        }
    }
    if v, ok := e.reasoningEffort.Load(sessionID); ok {
        return v.(string)
    }
    return ""
}

func (e *Engine) SetReasoningEffort(sessionID, effort string) {
    e.reasoningEffort.Store(sessionID, effort)
    if e.redis != nil {
        e.redis.Set(context.Background(), reasoningKey(sessionID), effort, time.Hour)
    }
}
```

**Step 4: Replace all direct `sync.Map` accesses with getter/setter functions**

Search for all `engine.reasoningEffort.Load(...)`, `engine.reasoningEffort.Store(...)`, and similar patterns in `internal/agent/engine.go` and `internal/api/agent_ws.go`. Replace each with the corresponding function call.

**Step 5: Wire Redis client into Engine constructor**

Find where `NewEngine` is called and pass the Redis client.

**Step 6: Test**

Run: `task test:api`

**Step 7: Commit**

```bash
git commit -m "feat: move agent session metadata to Redis for cross-pod portability"
```

---

### Task 3: Database Migration Locking

**Files:**
- Modify: `internal/database/migrate.go`

**The problem:** No distributed locking. Two pods starting simultaneously can race applying the same migration.

**The fix:** Wrap migration execution with `pg_advisory_lock`.

**Step 1: Acquire advisory lock before running migrations**

```go
const migrationLockID = 989898

func (m *Migrator) Run(ctx context.Context, conn *pgxpool.Pool) error {
    // Acquire distributed lock
    if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
        return fmt.Errorf("acquire migration lock: %w", err)
    }
    defer conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationLockID)

    // ... existing migration logic ...
}
```

**Step 2: Release lock on exit (defer handles this)**

**Step 3: Test**

Run: `task test:api` — migration tests should still pass.

**Step 4: Commit**

```bash
git commit -m "feat: add pg_advisory_lock around database migrations"
```

---

### Task 4: Scheduler with FOR UPDATE SKIP LOCKED

**Files:**
- Modify: `internal/scheduler/scheduler.go`

**The problem:** Every pod ticks every minute and all execute the same due schedules. Duplicate runs guaranteed.

**The fix:** Use `SELECT ... FOR UPDATE SKIP LOCKED` to claim a schedule row exclusively.

**Step 1: Replace the due-schedule query**

Old:
```sql
SELECT * FROM schedules WHERE enabled AND next_run_at <= NOW()
```

New:
```go
const claimQuery = `
    UPDATE schedules
    SET next_run_at = next_run_at + INTERVAL '1 minute'
    WHERE id = (
        SELECT id FROM schedules
        WHERE enabled AND next_run_at <= NOW()
        ORDER BY next_run_at
        FOR UPDATE SKIP LOCKED
        LIMIT 1
    )
    RETURNING *`
```

**Step 2: Add advisory lock for midnight tasks**

Wrap `runAgentStatsRollup` and `purgeTrash` with a Postgres advisory lock so only one pod runs them:

```go
func (s *Scheduler) runLocked(name string, lockID int64, fn func() error) error {
    _, err := s.db.Exec("SELECT pg_advisory_lock($1)", lockID)
    if err != nil {
        return err
    }
    defer s.db.Exec("SELECT pg_advisory_unlock($1)", lockID)
    return fn()
}
```

**Step 3: Test**

Run: `task test:api`

**Step 4: Commit**

```bash
git commit -m "fix: prevent duplicate scheduled executions with SELECT FOR UPDATE SKIP LOCKED"
```

---

### Task 5: Hocuspocus Redis Extension

**Files:**
- Modify: `relay/package.json`
- Modify: `relay/src/index.ts`
- Modify: `docker-compose.dev.yml` (relay env vars)

**The problem:** Multiple relay pods don't sync document updates. Users on different pods editing the same notebook don't see changes in real time.

**The fix:** Add `@hocuspocus/extension-redis` to sync document updates across relay instances.

**Step 1: Install the Redis extension**

```bash
cd relay && npm install @hocuspocus/extension-redis
```

**Step 2: Add the extension to Hocuspocus server**

```typescript
import { Server } from '@hocuspocus/server'
import { Redis } from '@hocuspocus/extension-redis'

const server = Server.configure({
  extensions: [
    new Redis({
      host: process.env.REDIS_HOST || 'localhost',
      port: parseInt(process.env.REDIS_PORT || '6379'),
    }),
  ],
  // ... existing config ...
})
```

**Step 3: Add `REDIS_HOST` and `REDIS_PORT` to relay environment**

In `docker-compose.dev.yml`:
```yaml
relay:
  environment:
    REDIS_HOST: aether-redis
    REDIS_PORT: "6379"
```

**Step 4: Test**

Run relay locally and verify it connects to Redis without errors:
```bash
cd relay && npm run build && node dist/index.js
```

**Step 5: Commit**

```bash
git commit -m "feat: add @hocuspocus/extension-redis for cross-relay Yjs sync"
```

---

### Task 6: Health Check Enhancement

**Files:**
- Modify: `internal/api/router.go`

**The problem:** `GET /health` returns 200 even when DB or Redis is down. No separate liveness vs readiness.

**The fix:** Add `GET /readyz` endpoint that checks DB and Redis connectivity. Keep `GET /healthz` as a simple process liveness check.

**Step 1: Add readiness check**

```go
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()

    checks := map[string]string{}

    if err := s.db.Ping(ctx); err != nil {
        checks["database"] = "unreachable"
    } else {
        checks["database"] = "ok"
    }

    if s.redis != nil {
        if err := s.redis.Ping(ctx).Err(); err != nil {
            checks["redis"] = "unreachable"
        } else {
            checks["redis"] = "ok"
        }
    }

    status := http.StatusOK
    for _, v := range checks {
        if v != "ok" {
            status = http.StatusServiceUnavailable
            break
        }
    }

    writeJSON(w, status, map[string]any{
        "status": status == http.StatusOK,
        "checks": checks,
    })
}
```

**Step 2: Register routes**

```go
s.mux.HandleFunc("GET /healthz", s.handleHealth)  // liveness
s.mux.HandleFunc("GET /readyz", s.handleReadyz)    // readiness
```

Keep the existing `GET /health` for backward compatibility.

**Step 3: Test**

Run: `task test:api`

**Step 4: Commit**

```bash
git commit -m "feat: add /readyz endpoint with DB and Redis health checks"
```

---

### Task 7: Redis-Based Rate Limiting

**Files:**
- Create: `internal/api/ratelimit.go`
- Modify: `internal/api/middleware.go`
- Modify: `internal/api/router.go`

**The problem:** Rate limiting only exists for the SSO probe endpoint.

**The fix:** Add a generic Redis-based rate limiting middleware using a sliding window counter.

**Step 1: Create rate limiter middleware**

```go
// RateLimit returns a middleware that limits requests per key (IP, user, etc.)
func (s *Server) rateLimit(keyFunc func(r *http.Request) string, limit int, window time.Duration) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            key := "ratelimit:" + keyFunc(r) + ":" + r.URL.Path
            count, err := s.redis.Incr(r.Context(), key).Result()
            if err != nil {
                next.ServeHTTP(w, r)
                return
            }
            if count == 1 {
                s.redis.Expire(r.Context(), key, window)
            }
            w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
            w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(max(0, limit-int(count))))

            if count > int64(limit) {
                http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

**Step 2: Apply to auth endpoints**

In `router.go`, wrap auth handlers:
```go
loginMW := s.rateLimit(func(r *http.Request) string {
    return r.RemoteAddr
}, 10, time.Minute)

s.mux.Handle("POST /api/v1/auth/login", loginMW(http.HandlerFunc(s.handleLogin)))
```

**Step 3: Migrate existing SSO probe rate limiting to use the new middleware**

Replace the inline Redis logic in `sso_probe_handlers.go` with the new middleware.

**Step 4: Test**

Run: `task test:api`

**Step 5: Commit**

```bash
git commit -m "feat: add Redis-based rate limiting middleware for auth endpoints"
```
