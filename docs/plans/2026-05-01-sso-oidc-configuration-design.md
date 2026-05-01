# SSO / OIDC Configuration — Design

**Date:** 2026-05-01
**Status:** Approved

## Problem

OIDC providers are currently configured at server startup via environment variables and stored in `Server.oidcProviders`. There is no UI to add, remove, or configure providers at runtime. Platform admins and org admins have no way to manage SSO without a server restart and config change.

## Goals

- Platform admins can define a catalog of platform-wide SSO providers
- Org admins can enable platform providers for their org (inherited as-is) or add their own
- Multiple providers per org are supported
- Org admins can control whether password login remains available when SSO is configured
- Login page is email-first: probes for SSO providers by email domain, shows buttons accordingly
- Multi-pod Kubernetes deployment is the target; no in-memory shared state

## Non-Goals

- Org admins cannot override settings inherited from platform providers
- No SAML support (OIDC only for now)
- No per-user SSO enforcement (org-level policy only)

---

## Data Model

### `sso_providers` table

| column | type | notes |
|---|---|---|
| `id` | `uuid` | PK, default `gen_random_uuid()` |
| `scope` | `text` | `'platform'` or `'org'` |
| `org_id` | `uuid` | NULL for platform providers; FK → `orgs` for org providers |
| `name` | `text` | Display name shown on login page (e.g. "Acme Okta") |
| `provider_type` | `text` | `'oidc'` (extensible) |
| `client_id` | `text` | |
| `client_secret_enc` | `text` | AES-encrypted with master key (same pattern as connectors) |
| `discovery_url` | `text` | OIDC discovery endpoint |
| `allowed_domains` | `text[]` | Email domains this provider covers (e.g. `{acme.com}`) |
| `enabled` | `bool` | Platform admin can globally disable a platform provider |
| `created_at` | `timestamptz` | |
| `updated_at` | `timestamptz` | |

### `org_platform_providers` table

Tracks which platform providers each org has enabled.

| column | type | notes |
|---|---|---|
| `org_id` | `uuid` | FK → `orgs` |
| `provider_id` | `uuid` | FK → `sso_providers` where `scope = 'platform'` |
| PK | `(org_id, provider_id)` | |

### `orgs` table — new column

- `sso_password_login bool DEFAULT true` — whether password login remains available when SSO is configured for the org

---

## Permission Model

| Actor | Scope | Allowed operations |
|---|---|---|
| Platform admin (`is_platform_admin = true`) | Platform providers | Full CRUD + enable/disable |
| Org admin (`role = 'admin'`) | Org providers | Full CRUD |
| Org admin | Platform providers | Enable/disable for their org; read-only on config |
| Org admin | Login settings | Update `sso_password_login` |
| Anyone (unauthenticated) | Probe endpoint | Read active provider names by email domain (rate limited) |

---

## API Routes

### Platform admin (`RequirePlatformAdmin`)

```
GET    /api/v1/admin/sso/providers
POST   /api/v1/admin/sso/providers
PUT    /api/v1/admin/sso/providers/{id}
DELETE /api/v1/admin/sso/providers/{id}
```

### Org admin (`RequireRole("admin")`)

```
GET    /api/v1/sso/providers                          list org's own + enabled platform providers
POST   /api/v1/sso/providers                          create org provider
PUT    /api/v1/sso/providers/{id}                     update org provider
DELETE /api/v1/sso/providers/{id}                     delete org provider
GET    /api/v1/sso/platform-providers                 list available platform providers
POST   /api/v1/sso/platform-providers/{id}/enable     enable platform provider for this org
DELETE /api/v1/sso/platform-providers/{id}/enable     disable platform provider for this org
PUT    /api/v1/sso/settings                           update sso_password_login
```

### Unauthenticated

```
GET    /api/v1/auth/sso-providers?email=…
```

Returns `[ { id, name, provider_type } ]` for all active providers whose `allowed_domains` matches the email domain. Never returns secrets or discovery URLs. Rate limited to 20 req/min per IP.

The existing OIDC redirect/callback routes remain:
```
GET    /api/v1/auth/oidc/{provider_id}
GET    /api/v1/auth/oidc/{provider_id}/callback
```

These now load provider config from the DB (via Redis cache) instead of startup config.

---

## Email Enumeration Mitigation

The probe endpoint matches on **email domain**, not user existence:

- `alice@company.com` → look up providers with `company.com` in `allowed_domains`
- Unknown domain → return empty provider list; login page falls back to password form
- Registered vs unregistered email at a known domain → indistinguishable from the probe response
- Auth failure at login time always returns generic "invalid credentials"
- Consistent response timing (small fixed floor) prevents timing-based inference

---

## Redis Integration

Redis (`HNB_REDIS_URL`) becomes a required dependency. Server fails fast at startup if Redis is unavailable.

| Purpose | Key | TTL |
|---|---|---|
| OAuth2 state tokens | `oidc:state:{token}` | 10 min |
| SSO provider cache | `sso:providers:{org_id}` | 60s |
| Rate limit buckets | `ratelimit:sso-probe:{ip}` | 1 min sliding window |

**OAuth2 state:** `globalStateStore` (current in-memory `sync.Map`) is replaced with Redis. `SET NX EX 600` on issue; `GETDEL` on consume. Fixes multi-pod correctness bug where callback could hit a different pod than the one that issued the state token.

**Provider cache:** populated on probe cache miss, invalidated on any provider create/update/delete/enable/disable.

**Rate limiting:** `INCR` + `EXPIRE` sliding window on the probe endpoint only. Returns `429` above 20 req/min per IP.

**Library:** `github.com/redis/go-redis/v9`

---

## Frontend Changes

### Login page

Two-step flow:

1. Email field + "Continue" button
2. Frontend calls probe endpoint with email
   - SSO providers found + password allowed → SSO buttons above divider, password form below
   - SSO providers found + password blocked → SSO buttons only
   - No SSO providers → password form only (no visual change from today)

SSO button shows provider `name`. Clicking redirects to `/api/v1/auth/oidc/{provider_id}`.

### Platform admin panel (`/admin`)

New "SSO Providers" tab alongside existing Orgs and Users tabs. Platform admins can create, edit, enable/disable, and delete platform-wide providers.

### Org settings (`/settings`)

New page, org-scoped, requires `admin` role. Two sections:

- **Platform providers** — list of available platform providers with enable/disable toggle
- **Custom providers** — CRUD for org's own providers
- **Login settings** — "Allow password login when SSO is configured" toggle

Link to `/settings` appears in the TopBar profile dropdown for org admins.

---

## Testing

- **OAuth2 state round-trip** — two server instances sharing one test Redis; state issued on instance A consumed on instance B
- **Probe endpoint** — domain match, unknown domain, rate limit (429 after threshold)
- **Permission enforcement** — non-admin returns 403 on all SSO management routes
- **Provider CRUD** — standard API integration tests using `setupTestServer` extended with test Redis
- **Email enumeration** — assert probe response is identical for registered vs unregistered email at a known domain
