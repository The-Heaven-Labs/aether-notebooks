# Platform Admin Bootstrap Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow operators to designate a platform admin via `HNB_PLATFORM_ADMIN_EMAIL` env var, and let existing platform admins promote/demote others via API + UI.

**Architecture:** Env var read into `Config`; passed to `Server` via a setter; applied at startup (UPDATE existing user) and at registration (INSERT with flag set). A new `PUT /api/v1/admin/users/:id` endpoint handles ongoing management, guarded by `RequirePlatformAdmin` and a self-demotion guard. The Admin UI Users tab gets a per-row toggle.

**Tech Stack:** Go (`net/http`, `pgx/v5`), React + `@tanstack/react-query`, existing `api.put()` client helper.

---

### Task 1: Add `HNB_PLATFORM_ADMIN_EMAIL` to config and wire into Server

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/api/router.go`

**Step 1: Add field to Config**

In `internal/config/config.go`, add to `Config` struct and `Load()`:

```go
type Config struct {
    Port               string
    DatabaseURL        string
    RedisURL           string
    MasterKey          string
    JWTSecret          string
    AttachmentDir      string
    PlatformAdminEmail string   // add this
}

func Load() (*Config, error) {
    cfg := &Config{
        // existing fields unchanged
        PlatformAdminEmail: os.Getenv("HNB_PLATFORM_ADMIN_EMAIL"),  // add this
    }
    // existing validation unchanged
    return cfg, nil
}
```

**Step 2: Add field and setter to Server**

In `internal/api/router.go`, add to the `Server` struct and a new exported setter:

```go
type Server struct {
    db                 *database.DB
    jwt                *auth.JWTIssuer
    audit              *audit.Logger
    masterKey          []byte
    hub                *Hub
    mux                *http.ServeMux
    attachmentDir      string
    Cache              *cache.Cache
    platformAdminEmail string   // add this
}

// SetPlatformAdminEmail configures which email gets platform admin on registration.
func (s *Server) SetPlatformAdminEmail(email string) {
    s.platformAdminEmail = email
}
```

`NewServer` signature does NOT change — call the setter separately from `main.go`.

**Step 3: Run tests to confirm nothing broke**

```bash
cd internal/api && go test ./... -count=1
```

Expected: all existing tests pass.

**Step 4: Commit**

```bash
git add internal/config/config.go internal/api/router.go
git commit -m "feat: add HNB_PLATFORM_ADMIN_EMAIL config and Server field"
```

---

### Task 2: Seed platform admin at startup

**Files:**
- Modify: `cmd/hnb-server/main.go`
- Test: `internal/api/admin_handlers_test.go`

**Step 1: Write the failing test**

Add to `internal/api/admin_handlers_test.go`. This test directly exercises the SQL logic:

```go
func TestSeedPlatformAdmin(t *testing.T) {
    s := setupTestServer(t)
    ctx := context.Background()

    email := fmt.Sprintf("admin-%d@example.com", time.Now().UnixNano())

    // User doesn't exist yet: seed is a no-op
    _, err := s.DB().Pool.Exec(ctx,
        `UPDATE users SET is_platform_admin=true WHERE email=$1`, email)
    assert.NoError(t, err) // UPDATE with no matching rows is not an error

    // Create the user
    var userID string
    err = s.DB().Pool.QueryRow(ctx,
        `INSERT INTO users (email, password_hash, name, email_verified)
         VALUES ($1, 'x', 'Test', false) RETURNING id`, email).Scan(&userID)
    require.NoError(t, err)

    // Seed: user now exists, should be promoted
    _, err = s.DB().Pool.Exec(ctx,
        `UPDATE users SET is_platform_admin=true WHERE email=$1`, email)
    require.NoError(t, err)

    var isPlatformAdmin bool
    err = s.DB().Pool.QueryRow(ctx,
        `SELECT is_platform_admin FROM users WHERE id=$1`, userID).Scan(&isPlatformAdmin)
    require.NoError(t, err)
    assert.True(t, isPlatformAdmin)
}
```

Add imports needed: `"context"`, `"fmt"`, `"time"`, `"github.com/stretchr/testify/require"`.

**Step 2: Run test to verify it passes** (this test exercises the DB directly, should pass immediately)

```bash
cd internal/api && go test -run TestSeedPlatformAdmin -v
```

Expected: PASS — confirms the UPDATE pattern is correct.

**Step 3: Wire into main.go**

In `cmd/hnb-server/main.go`, after `db.Migrate(ctx)` and before building the HTTP server, add:

```go
// Seed platform admin from env if configured
if cfg.PlatformAdminEmail != "" {
    if _, err := db.Pool.Exec(ctx,
        `UPDATE users SET is_platform_admin=true WHERE email=$1`,
        cfg.PlatformAdminEmail,
    ); err != nil {
        log.Printf("warning: failed to seed platform admin: %v", err)
    } else {
        log.Printf("platform admin seeded for %s", cfg.PlatformAdminEmail)
    }
}
```

Then after building `srv`, call the setter:

```go
srv := api.NewServer(db, jwtIssuer, auditLogger, masterKey, redisCache)
srv.SetPlatformAdminEmail(cfg.PlatformAdminEmail)  // add this
srv.SetAttachmentDir(cfg.AttachmentDir)
```

**Step 4: Build to confirm it compiles**

```bash
go build ./cmd/hnb-server/
```

Expected: no errors.

**Step 5: Commit**

```bash
git add cmd/hnb-server/main.go internal/api/admin_handlers_test.go
git commit -m "feat: seed platform admin at startup from HNB_PLATFORM_ADMIN_EMAIL"
```

---

### Task 3: Promote at registration

**Files:**
- Modify: `internal/api/auth_handlers.go`
- Test: `internal/api/auth_handlers_test.go`

**Step 1: Write the failing test**

Add to `internal/api/auth_handlers_test.go`:

```go
func TestRegister_PlatformAdminEmail(t *testing.T) {
    s := setupTestServer(t)
    ts := time.Now().UnixNano()
    email := fmt.Sprintf("padmin-%d@example.com", ts)

    s.SetPlatformAdminEmail(email)

    // Register with the designated platform admin email
    body, _ := json.Marshal(map[string]string{
        "email":    email,
        "password": "pass1234",
        "name":     "Platform Admin",
        "org_name": fmt.Sprintf("Org %d", ts),
    })
    req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    s.ServeHTTP(rec, req)
    require.Equal(t, http.StatusCreated, rec.Code)

    // Verify is_platform_admin is set in the DB
    var isPlatformAdmin bool
    err := s.DB().Pool.QueryRow(context.Background(),
        `SELECT is_platform_admin FROM users WHERE email=$1`, email,
    ).Scan(&isPlatformAdmin)
    require.NoError(t, err)
    assert.True(t, isPlatformAdmin, "registered user with matching email should be platform admin")
}
```

**Step 2: Run test to verify it fails**

```bash
cd internal/api && go test -run TestRegister_PlatformAdminEmail -v
```

Expected: FAIL — `is_platform_admin` is false.

**Step 3: Implement promotion in both registration flows**

In `internal/api/auth_handlers.go`, the registration handler (`handleRegister`) has two INSERT paths. In both, change the INSERT to set `is_platform_admin` when the email matches.

**Flow 1 — with org_name (around line 77):**

```go
isPlatformAdmin := s.platformAdminEmail != "" && req.Email == s.platformAdminEmail
err = tx.QueryRow(ctx,
    `INSERT INTO users (email, password_hash, name, email_verified, is_platform_admin)
     VALUES ($1, $2, $3, FALSE, $4) RETURNING id`,
    req.Email, hash, req.Name, isPlatformAdmin,
).Scan(&userID)
```

**Flow 2 — without org_name (around line 151):**

```go
isPlatformAdmin := s.platformAdminEmail != "" && req.Email == s.platformAdminEmail
err = s.db.Pool.QueryRow(ctx,
    `INSERT INTO users (email, password_hash, name, email_verified, is_platform_admin)
     VALUES ($1, $2, $3, FALSE, $4) RETURNING id`,
    req.Email, hash, req.Name, isPlatformAdmin,
).Scan(&userID)
```

**Step 4: Run test to verify it passes**

```bash
cd internal/api && go test -run TestRegister_PlatformAdminEmail -v
```

Expected: PASS.

**Step 5: Run full test suite**

```bash
cd internal/api && go test ./... -count=1
```

Expected: all pass.

**Step 6: Commit**

```bash
git add internal/api/auth_handlers.go internal/api/auth_handlers_test.go
git commit -m "feat: promote user to platform admin on registration when email matches config"
```

---

### Task 4: `PUT /api/v1/admin/users/:id` endpoint

**Files:**
- Modify: `internal/api/admin_handlers.go`
- Modify: `internal/api/router.go`
- Test: `internal/api/admin_handlers_test.go`

**Step 1: Write the failing tests**

Add to `internal/api/admin_handlers_test.go`:

```go
func TestAdminUpdateUser_Promote(t *testing.T) {
    s := setupTestServer(t)
    ctx := context.Background()

    // Create a regular user to promote
    var targetID string
    err := s.DB().Pool.QueryRow(ctx,
        `INSERT INTO users (email, password_hash, name, email_verified)
         VALUES ('target@example.com', 'x', 'Target', false) RETURNING id`,
    ).Scan(&targetID)
    require.NoError(t, err)

    body, _ := json.Marshal(map[string]bool{"is_platform_admin": true})
    req := httptest.NewRequest("PUT", "/api/v1/admin/users/"+targetID, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req = withPlatformAdminClaims(req)
    w := httptest.NewRecorder()
    s.ServeHTTP(w, req)
    assert.Equal(t, http.StatusOK, w.Code)

    var isPlatformAdmin bool
    s.DB().Pool.QueryRow(ctx,
        `SELECT is_platform_admin FROM users WHERE id=$1`, targetID,
    ).Scan(&isPlatformAdmin)
    assert.True(t, isPlatformAdmin)
}

func TestAdminUpdateUser_Demote(t *testing.T) {
    s := setupTestServer(t)
    ctx := context.Background()

    var targetID string
    err := s.DB().Pool.QueryRow(ctx,
        `INSERT INTO users (email, password_hash, name, email_verified, is_platform_admin)
         VALUES ('demote@example.com', 'x', 'Demote', false, true) RETURNING id`,
    ).Scan(&targetID)
    require.NoError(t, err)

    body, _ := json.Marshal(map[string]bool{"is_platform_admin": false})
    req := httptest.NewRequest("PUT", "/api/v1/admin/users/"+targetID, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req = withPlatformAdminClaims(req)
    w := httptest.NewRecorder()
    s.ServeHTTP(w, req)
    assert.Equal(t, http.StatusOK, w.Code)

    var isPlatformAdmin bool
    s.DB().Pool.QueryRow(ctx,
        `SELECT is_platform_admin FROM users WHERE id=$1`, targetID,
    ).Scan(&isPlatformAdmin)
    assert.False(t, isPlatformAdmin)
}

func TestAdminUpdateUser_SelfDemotionBlocked(t *testing.T) {
    s := setupTestServer(t)

    // withPlatformAdminClaims uses testUserID — attempt to demote self
    body, _ := json.Marshal(map[string]bool{"is_platform_admin": false})
    req := httptest.NewRequest("PUT", "/api/v1/admin/users/"+testUserID, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req = withPlatformAdminClaims(req)
    w := httptest.NewRecorder()
    s.ServeHTTP(w, req)
    assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminUpdateUser_RequiresPlatformAdmin(t *testing.T) {
    s := setupTestServer(t)

    body, _ := json.Marshal(map[string]bool{"is_platform_admin": true})
    req := httptest.NewRequest("PUT", "/api/v1/admin/users/"+testUserID, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req = withAdminClaims(req, testOrgID)
    w := httptest.NewRecorder()
    s.ServeHTTP(w, req)
    assert.Equal(t, http.StatusForbidden, w.Code)
}
```

Check what `testUserID` is in testhelpers_test.go — use that constant for the self-demotion test.

**Step 2: Run tests to verify they fail**

```bash
cd internal/api && go test -run "TestAdminUpdateUser" -v
```

Expected: FAIL — 404 (route not registered yet).

**Step 3: Implement the handler**

Add to `internal/api/admin_handlers.go`:

```go
func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
    targetID := r.PathValue("id")
    if targetID == "" {
        writeError(w, http.StatusBadRequest, "missing user id")
        return
    }

    claims, ok := claimsFromContext(r.Context())
    if !ok {
        writeError(w, http.StatusUnauthorized, "unauthorized")
        return
    }

    var req struct {
        IsPlatformAdmin bool `json:"is_platform_admin"`
    }
    if err := decodeJSON(r, &req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    // Prevent self-demotion
    if claims.UserID == targetID && !req.IsPlatformAdmin {
        writeError(w, http.StatusBadRequest, "cannot remove your own platform admin status")
        return
    }

    _, err := s.db.Pool.Exec(r.Context(),
        `UPDATE users SET is_platform_admin=$1 WHERE id=$2`,
        req.IsPlatformAdmin, targetID,
    )
    if err != nil {
        writeError(w, http.StatusInternalServerError, "failed to update user")
        return
    }

    writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

**Step 4: Register the route**

In `internal/api/router.go`, inside `s.routes()`, alongside the other admin routes:

```go
s.mux.Handle("PUT /api/v1/admin/users/{id}", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminUpdateUser))))
```

**Step 5: Run tests to verify they pass**

```bash
cd internal/api && go test -run "TestAdminUpdateUser" -v
```

Expected: all 4 tests PASS.

**Step 6: Run full test suite**

```bash
cd internal/api && go test ./... -count=1
```

Expected: all pass.

**Step 7: Commit**

```bash
git add internal/api/admin_handlers.go internal/api/router.go internal/api/admin_handlers_test.go
git commit -m "feat: PUT /api/v1/admin/users/:id for platform admin promotion/demotion"
```

---

### Task 5: Platform admin toggle in Admin UI Users tab

**Files:**
- Modify: `web/src/pages/AdminPage.tsx`

The Users tab currently fetches with a plain `fetch` in a `useEffect` and stores results in local `users` state. Keep that pattern — add a `useMutation` (React Query is already imported) for the toggle, and update local state on success.

**Step 1: Add the mutation and toggle button**

In `AdminPage.tsx`, inside the `AdminPage` component, add after the existing state declarations:

```tsx
const togglePlatformAdmin = useMutation({
  mutationFn: ({ id, isPlatformAdmin }: { id: string; isPlatformAdmin: boolean }) =>
    api.put(`/api/v1/admin/users/${id}`, { is_platform_admin: isPlatformAdmin }),
  onSuccess: (_data, { id, isPlatformAdmin }) => {
    setUsers(prev => prev.map(u => u.id === id ? { ...u, is_platform_admin: isPlatformAdmin } : u))
  },
})
```

The `User` interface already has `is_platform_admin: boolean` (line 11). The mutation needs the `useQueryClient` import — but since it doesn't use a query cache, no `queryClient` needed. Just `useMutation` which is already imported.

In the Users tab table, add a "Platform Admin" column header and a toggle cell per row. Replace the existing read-only cell:

```tsx
// Replace the existing "Platform Admin" column (which just shows Yes/No text):
<th style={styles.th}>Platform Admin</th>
// ...
<td style={styles.td}>
  <button
    style={{
      padding: '2px 10px',
      fontSize: 12,
      cursor: 'pointer',
      borderRadius: 4,
      border: '1px solid var(--border)',
      background: u.is_platform_admin ? 'var(--accent)' : 'transparent',
      color: u.is_platform_admin ? '#fff' : 'var(--text-muted)',
    }}
    disabled={togglePlatformAdmin.isPending}
    onClick={() => togglePlatformAdmin.mutate({ id: u.id, isPlatformAdmin: !u.is_platform_admin })}
  >
    {u.is_platform_admin ? 'Admin' : 'User'}
  </button>
</td>
```

**Step 2: Verify TypeScript is clean**

```bash
cd web && npx tsc --noEmit
```

Expected: `TypeScript compilation completed` with no errors.

**Step 3: Commit**

```bash
git add web/src/pages/AdminPage.tsx
git commit -m "feat: platform admin toggle in admin users tab"
```
