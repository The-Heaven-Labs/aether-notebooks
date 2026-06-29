# Subdomain Multi-Tenancy Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable org-level subdomains (`org1.aether.test` → Org 1) so a single platform SSO provider works across orgs and first-login provisioning routes correctly.

**Architecture:** A middleware resolves the org from the `Host` header subdomain before auth. SSO probe and OIDC callback use the resolved org to scope providers and provision new users into the correct org.

**Tech Stack:** Go (net/http), pgx, Keycloak OIDC, React

**Reference design:** `docs/plans/2026-06-28-subdomain-multitenancy-design.md`

---

### Task 1: Add `OrgIDFromContext` helper + `subdomainKey`

**Files:**
- Modify: `internal/api/middleware.go:14-18`

**Step 1: Add subdomainKey and OrgIDFromContext**

Add after the existing context keys:

```go
const subdomainKey contextKey = "subdomain_org"
```

Add at the end of the file (before package closing):

```go
// OrgIDFromContext returns the org ID resolved from the subdomain, or falls
// back to the org ID in the JWT claims. Returns empty string if neither is available.
func OrgIDFromContext(ctx context.Context) string {
    if v := ctx.Value(subdomainKey); v != nil {
        if id, ok := v.(string); ok && id != "" {
            return id
        }
    }
    if claims := ClaimsFromContext(ctx); claims != nil {
        return claims.OrgID
    }
    return ""
}
```

**Step 2: Run vet**

Run: `go vet ./internal/api/...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/api/middleware.go
git commit -m "feat: add OrgIDFromContext helper for subdomain-based org resolution"
```

---

### Task 2: Add `SubdomainMiddleware`

**Files:**
- Modify: `internal/api/middleware.go`

**Step 1: Add the middleware before AuthMiddleware**

```go
func SubdomainMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            host := strings.Split(r.Host, ":")[0]
            parts := strings.SplitN(host, ".", 2)
            // Must have exactly two parts (e.g. "org1.aether.test"),
            // second part must be non-empty, first must not be "www" or "localhost".
            if len(parts) == 2 && parts[1] != "" && parts[0] != "www" && parts[0] != "localhost" {
                var orgID string
                err := pool.QueryRow(r.Context(),
                    `SELECT id FROM orgs WHERE slug = $1`, parts[0],
                ).Scan(&orgID)
                if err == nil {
                    ctx := context.WithValue(r.Context(), subdomainKey, orgID)
                    next.ServeHTTP(w, r.WithContext(ctx))
                    return
                }
                writeError(w, http.StatusNotFound, "unknown organization")
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

**Step 2: Run vet**

Run: `go vet ./internal/api/...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/api/middleware.go
git commit -m "feat: add SubdomainMiddleware to resolve org from Host header"
```

---

### Task 3: Wire SubdomainMiddleware in router

**Files:**
- Modify: `internal/api/router.go`

**Step 1: Wire middleware on the mux before authMW**

In `routes()`, add the subdomain middleware as a wrapper on the mux. Since Go 1.22's `net/http` mux doesn't have a global middleware chain, wrap the entire mux in a handler:

Add this near the top of `routes()`:

```go
// Wrap the entire mux with subdomain resolution (runs before auth on every request).
// Public routes (health, login, OIDC) also benefit from the subdomain context.
subdomainMW := SubdomainMiddleware(s.db.Pool)
```

Then wrap `s.mux` for all routes:

At the end of `routes()`, replace `s.mux.ServeHTTP` via a wrapper. Actually, the cleanest approach: wrap the mux when serving:

```go
// In ServeHTTP (add this method or modify existing):
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    subdomainMW := SubdomainMiddleware(s.db.Pool)
    subdomainMW(s.mux).ServeHTTP(w, r)
}
```

Wait — `ServeHTTP` is already defined (line 56). Replace its body:

```go
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    sm := SubdomainMiddleware(s.db.Pool)
    sm(s.mux).ServeHTTP(w, r)
}
```

Create `subdomainMW` once at Server creation (in `NewServer`) instead of per-request to avoid creating a new pool connection each time:

In `NewServer` (router.go:37-54):

```go
func NewServer(...) *Server {
    s := &Server{
        ...
    }
    ...
    s.subdomainMW = SubdomainMiddleware(s.db.Pool) // store as a field
    s.routes()
    return s
}
```

Add `subdomainMW func(http.Handler) http.Handler` to the Server struct.

Then `ServeHTTP` becomes:

```go
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    s.subdomainMW(s.mux).ServeHTTP(w, r)
}
```

**Step 2: Run vet**

Run: `go vet ./internal/api/...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/api/router.go
git commit -m "feat: wire SubdomainMiddleware into request pipeline"
```

---

### Task 4: Add org-scoped provider query

**Files:**
- Modify: `internal/sso/sso.go`

**Step 1: Add `ListProvidersByDomainForOrg`**

Add after `ListProvidersByDomain`:

```go
// ListProvidersByDomainForOrg returns providers for a specific org whose allowed_domains contains domain.
// Includes both org-scoped providers and platform providers enabled for that org.
func ListProvidersByDomainForOrg(ctx context.Context, pool *pgxpool.Pool, domain, orgID string) ([]ProbeResult, error) {
    rows, err := pool.Query(ctx,
        `SELECT sp.id, sp.name, sp.provider_type
         FROM sso_providers sp
         WHERE sp.enabled = true
           AND $1 = ANY(sp.allowed_domains)
           AND (
             (sp.scope = 'org' AND sp.org_id = $2)
             OR
             (sp.scope = 'platform' AND EXISTS (
               SELECT 1 FROM org_platform_providers opp
               WHERE opp.provider_id = sp.id AND opp.org_id = $2
             ))
           )`,
        domain, orgID,
    )
    if err != nil {
        return nil, fmt.Errorf("list providers by domain for org: %w", err)
    }
    defer rows.Close()

    var results []ProbeResult
    for rows.Next() {
        var r ProbeResult
        if err := rows.Scan(&r.ID, &r.Name, &r.ProviderType); err != nil {
            return nil, fmt.Errorf("scan probe result: %w", err)
        }
        results = append(results, r)
    }
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("rows error: %w", err)
    }
    if results == nil {
        results = []ProbeResult{}
    }
    return results, nil
}
```

**Step 2: Run vet**

Run: `go vet ./internal/sso/...`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/sso/sso.go
git commit -m "feat: add ListProvidersByDomainForOrg for org-scoped SSO probe"
```

---

### Task 5: Update SSO probe handler to use org context

**Files:**
- Modify: `internal/api/sso_probe_handlers.go`

**Step 1: Import `OrgIDFromContext`** (same package, no import needed)

**Step 2: Update handleSSOProbe to scope by org**

After the domain extraction (line 32), add:

```go
orgID := OrgIDFromContext(ctx)
var providers []sso.ProbeResult
if orgID != "" {
    providers, err = sso.ListProvidersByDomainForOrg(ctx, s.db.Pool, domain, orgID)
} else {
    providers, err = sso.ListProvidersByDomain(ctx, s.db.Pool, domain)
}
```

Currently it calls `sso.ListProvidersByDomain(ctx, s.db.Pool, domain)` — replace that line with the conditional.

**Step 3: Run vet**

Run: `go vet ./internal/api/...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/api/sso_probe_handlers.go
git commit -m "fix: scope SSO probe to org context when subdomain is present"
```

---

### Task 6: Update OIDC callback to use subdomain org

**Files:**
- Modify: `internal/api/oidc_handlers.go`

**Step 1: In handleOIDCCallback, resolve org from subdomain**

Currently lines 169-233 look up the existing user by email and create a new org if not found. With subdomains, the org is already resolved. Add near the top of the callback handler (after loading the dbProvider):

```go
// Resolve org from subdomain (if any)
subdomainOrgID := OrgIDFromContext(ctx)
```

Then modify the existing user lookup to handle org context:

When `err == pgx.ErrNoRows` (new user) and `subdomainOrgID != ""`:

```go
if err == pgx.ErrNoRows {
    if subdomainOrgID != "" {
        // New user — provision into the subdomain-resolved org
        tx, txErr := s.db.Pool.Begin(ctx)
        if txErr != nil {
            writeError(w, http.StatusInternalServerError, "database error")
            return
        }
        defer tx.Rollback(ctx)

        displayName := claims.Name
        if displayName == "" {
            displayName = claims.Email
        }

        txErr = tx.QueryRow(ctx,
            `INSERT INTO users (email, name, email_verified) VALUES ($1, $2, TRUE) RETURNING id`,
            claims.Email, displayName,
        ).Scan(&userID)
        if txErr != nil {
            writeError(w, http.StatusInternalServerError, "failed to create user")
            return
        }

        // Use the subdomain org, don't create a new one
        orgID = subdomainOrgID

        _, txErr = tx.Exec(ctx,
            `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`,
            orgID, userID,
        )
        if txErr != nil {
            writeError(w, http.StatusInternalServerError, "failed to add member")
            return
        }

        if txErr = createHomeFolder(ctx, tx, orgID, userID, displayName); txErr != nil {
            writeError(w, http.StatusInternalServerError, "failed to create home folder")
            return
        }

        if txErr = tx.Commit(ctx); txErr != nil {
            writeError(w, http.StatusInternalServerError, "failed to commit")
            return
        }

        role = "member"
    } else {
        // No subdomain — create a new org (existing behavior)
        // ... keep the existing code for creating a new org
    }
}
```

**Step 2: Handle existing user with subdomain**

After the existing user lookup succeeds, if `subdomainOrgID` is set and differs from `claims.OrgID`, the token may be for a different org. In this case, verify membership in the subdomain org and issue a new token:

```go
} else if err != nil {
    // ...
}

// If subdomain is present and differs from the token's org,
// re-issue a token for the subdomain org
if subdomainOrgID != "" && subdomainOrgID != orgID {
    // Verify user is a member of the subdomain org
    var subdomainRole string
    err = s.db.Pool.QueryRow(ctx,
        `SELECT role FROM org_members WHERE org_id = $1 AND user_id = $2`,
        subdomainOrgID, userID,
    ).Scan(&subdomainRole)
    if err == nil {
        orgID = subdomainOrgID
        role = subdomainRole
    }
    // If not a member, keep the original org (user accesses their own org)
}
```

**Step 3: Run vet**

Run: `go vet ./internal/api/...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/api/oidc_handlers.go
git commit -m "feat: route OIDC callback users to subdomain org instead of creating new org"
```

---

### Task 7: Update tests

**Files:**
- Modify: `internal/api/oidc_handlers_test.go`

Update tests that exercise the OIDC callback to verify the subdomain org behavior. The existing tests for OIDC callback should still pass for the no-subdomain case. Add new test cases:

- User visits `org1.aether.test` with a new email → provisioned into Org 1
- User visits bare domain with a new email → new org created (existing behavior preserved)
- Existing user visits `org1.aether.test` with a token for Org 2 → re-issued token for Org 1 (if member)

The test server helper `setupTestServer` has `SubdomainMiddleware` wired — tests that don't set a specific Host header will hit the bare domain path (existing behavior preserved).

**Step 1: Add subdomain test cases**

In `oidc_handlers_test.go`, add tests:

```go
func TestOIDCCallbackWithSubdomain(t *testing.T) {
    ts, ctx := setupTestServer(t)
    defer ts.Close()

    // Set up a provider and org
    org := createTestOrg(t, ctx, ts)
    provider := createTestSSOProvider(t, ctx, ts, "platform", org.ID)

    // Simulate callback with subdomain Host header
    req := ts.Request("GET", "/api/v1/auth/oidc/"+provider.ID+"/callback?code=test&state=test", nil)
    req.Host = org.Slug + ".aether.test"

    // ... exchange mock, verify user is in org
}
```

**Step 2: Run tests**

Run: `go test ./internal/api/... -run TestOIDC -v -count=1`
Expected: PASS (or skip if infra unavailable)

**Step 3: Commit**

```bash
git add internal/api/oidc_handlers_test.go
git commit -m "test: add OIDC callback subdomain routing tests"
```

---

### Task 8: Dev environment setup

**Files:**
- Modify: `README.md` or `CLAUDE.md`

**Step 1: Document /etc/hosts entries**

Add to the dev setup section in `CLAUDE.md`:

```
Add to /etc/hosts for local testing:

127.0.0.1  aether.test
127.0.0.1  org1.aether.test org2.aether.test
```

**Step 2: Test end-to-end**

1. Start the dev stack: `docker compose -f docker-compose.dev.yml up -d`
2. Create an org with slug `org1` (via API or UI)
3. Enable the platform SSO provider for Org 1 (via API or UI)
4. Visit `http://org1.aether.test:5173` — login page should work
5. Enter `alice@aether-dev.test` — should see Keycloak SSO button
6. Log in via Keycloak — should be provisioned into Org 1

**Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add subdomain dev setup instructions"
```
