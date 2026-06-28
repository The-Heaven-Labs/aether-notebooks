# Subdomain-Based Multi-Tenancy

Enable org-level subdomains (`org1.aether.test` → Org 1, `org2.aether.test` → Org 2) so that a single platform SSO provider works across all orgs, first-login provisioning routes to the correct org, and the org context is unambiguous on every request.

## 1. Org Resolution Middleware

A new middleware runs before auth on every non-public route. It extracts the subdomain from the `Host` header, looks up the org by slug in the `orgs` table, and sets the org ID in the request context.

```
org1.aether.test  → subdomain="org1",  look up orgs WHERE slug='org1'
aether.test        → no subdomain, default behavior (login page, etc.)
```

- The `orgs.slug` column already exists, computed via `slugify()` on org creation (e.g., "Acme Corp" → `acme-corp`). Each org slug must be unique and DNS-safe to function as a subdomain.
- The middleware runs **before** `AuthMiddleware` so unauthenticated routes (SSO probe, OIDC login/callback) also have the org context.
- Routes that don't need an org (health, swagger, login page) skip the middleware or accept a missing org gracefully.
- The resolved org ID is stored in the request context alongside the JWT claims. If a request has both (subdomain + JWT), the JWT's org claim is validated against the subdomain-resolved org. Mismatches return 403.

**Implementation sketch:**

```go
type subdomainKey struct{}
func SubdomainMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            host := strings.Split(r.Host, ":")[0] // strip port
            parts := strings.SplitN(host, ".", 2)
            if len(parts) == 2 && parts[1] != "" && parts[0] != "www" {
                var orgID string
                err := pool.QueryRow(r.Context(),
                    `SELECT id FROM orgs WHERE slug = $1`, parts[0],
                ).Scan(&orgID)
                if err == nil {
                    ctx := context.WithValue(r.Context(), subdomainKey, orgID)
                    next.ServeHTTP(w, r.WithContext(ctx))
                    return
                }
                // Unknown subdomain → 404
                writeError(w, http.StatusNotFound, "unknown organization")
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

A helper `OrgIDFromContext(ctx)` returns the subdomain org or falls back to `claims.OrgID`.

## 2. SSO Probe — Org-Scoped

The probe endpoint `GET /api/v1/auth/sso-providers?email=...` is currently unauthenticated and returns all enabled providers matching the email domain. With the subdomain middleware, the resolved org is available in the context.

**New behavior:**

- If an org is resolved from the subdomain → return only providers enabled **for that org**:
  - Org-level providers (`scope='org'` with matching `org_id`)
  - Platform providers (`scope='platform'`) that the org has opted into via `org_platform_providers`
- If no org (bare domain `aether.test`) → return empty list (login page shows only password form)

**Query:**

```sql
-- Org-scoped providers
SELECT sp.id, sp.name, sp.provider_type
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
  )
```

This is the key fix for the bug: platform providers only appear for orgs that have explicitly enabled them.

## 3. OIDC Callback — Org from Host

The OIDC callback handler currently creates a new org for first-time users. With subdomains:

- The org is already resolved from the subdomain via middleware.
- **First-time user**: provision the user into the resolved org (no new org created).
- **Existing user**: verify membership in the resolved org; if the SSO provider is platform-scoped, auto-join if the user's domain matches `allowed_domains` and the org has the provider enabled.
- The `redirect_uri` sent to Keycloak includes the full subdomain URL so the callback arrives at the correct host.

**Keycloak redirect URI pattern** (already supported): `http://*.localhost:8080/api/v1/auth/oidc/*/callback`

This means:
- `http://org1.localhost:8080/api/v1/auth/oidc/{id}/callback` → resolves Org 1
- `http://org2.localhost:8080/api/v1/auth/oidc/{id}/callback` → resolves Org 2
- One SSO provider, no duplicate config

## 4. Internal SSO Group Sync

Groups are synced independently per org using the same SSO provider claims. When a user's groups are reconciled (in `handleOIDCCallback`):

```go
if dbProvider.AutoSyncGroups && len(claims.Groups) > 0 {
    SyncSSOGroups(ctx, s.db.Pool, s.audit, dbProvider, orgID, userID, claims.Groups)
}
```

The `orgID` is now the subdomain-resolved org. Group records are created in that org if they don't exist. The same group name ("aether-analysts") maps to different ACLs in different orgs.

## 5. Cookie / Auth Scope

JWT tokens carry `org_id` as they do today (in the `oid` claim). No token format changes. The subdomain middleware validates that the token's org matches the subdomain-resolved org if both are present. This prevents a user with an Org 1 token from accessing Org 2's subdomain.

The frontend should include the subdomain in API requests naturally (the browser sends the `Host` header with the subdomain). Vite dev server needs to handle subdomain requests — this works if Vite listens on `0.0.0.0:5173` and the dev `Host` header is forwarded.

## 6. Frontend Changes

- **Login page**: Detect subdomain from `window.location.hostname`. The probe call `GET /api/v1/auth/sso-providers?email=...` automatically picks up the subdomain from the Host header — no frontend changes needed for the probe.
- **Post-auth redirects**: After login, redirect back to the subdomain URL the user came from (already works since the browser stays on the subdomain).
- **Org switcher**: Still available for users with multiple org memberships — navigating to a different org changes the subdomain.

## 7. Dev Environment

Add to `/etc/hosts`:

```
127.0.0.1  aether.test
127.0.0.1  org1.aether.test org2.aether.test
```

The Vite dev server and Go API already listen on `0.0.0.0` so subdomain requests reach them. Keycloak's redirect URI already uses a wildcard pattern (`http://*localhost:8080/...`).

## Files Changed

| File | Change |
|---|---|
| `internal/api/middleware.go` | Add `SubdomainMiddleware` + `OrgIDFromContext` |
| `internal/api/router.go` | Wire middleware on non-public routes; extract org setter pattern |
| `internal/api/sso_probe_handlers.go` | Scope probe query by org from context |
| `internal/api/oidc_handlers.go` | Use subdomain org in callback (no new org creation) |
| `internal/sso/sso.go` | Add `ListProvidersByDomainForOrg` |
| `web/src/pages/LoginPage.tsx` | Minor — ensure probe calls work with subdomain |
| `docker-compose.dev.yml` | No changes (already listens on 0.0.0.0) |
