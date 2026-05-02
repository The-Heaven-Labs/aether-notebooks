# Platform Admin Bootstrap Design

**Goal:** Provide a zero-friction path for operators to designate the first platform admin on a new hnb instance, and allow existing platform admins to manage others.

**Architecture:** Env var seeding at startup/registration for bootstrap; API + UI for ongoing management.

---

## Problem

`is_platform_admin` defaults to `false` and nothing in the codebase ever sets it to `true`. On a fresh instance, the only path is direct SQL — unacceptable UX for operators deploying hnb for multiple orgs on a shared instance.

## Design

### Bootstrap: `HNB_PLATFORM_ADMIN_EMAIL`

Add an optional env var `HNB_PLATFORM_ADMIN_EMAIL`. When set, the server ensures that email address is a platform admin via two mechanisms:

1. **At startup** (after migrations): `UPDATE users SET is_platform_admin=true WHERE email=$1`. No-op if the user hasn't registered yet.
2. **At registration**: if the new user's email matches the configured address, set `is_platform_admin=true` in the INSERT (or via an immediate UPDATE after insert).

No new migration needed — `is_platform_admin` already exists (migration 005). No separate "pending" storage — the config value is the source of truth.

### Ongoing management: platform admin can promote/demote others

Once bootstrapped, platform admins manage others through the Admin UI.

**API:** `PUT /api/v1/admin/users/:id` — accepts `{ is_platform_admin: bool }`, guarded by `RequirePlatformAdmin`. Supports both promote and demote.

**Self-demotion guard:** If `claims.UserID == targetID`, return 400. Prevents accidental lockout.

**UI:** The Users tab in `AdminPage.tsx` gets a "Platform Admin" toggle per row that calls the endpoint.

---

## Components

| Component | Change |
|---|---|
| `internal/config/config.go` | Add `PlatformAdminEmail string`, read from `HNB_PLATFORM_ADMIN_EMAIL` (optional) |
| `cmd/hnb-server/main.go` | After `db.Migrate()`, call `seedPlatformAdmin(ctx, db, cfg.PlatformAdminEmail)` if non-empty |
| `internal/api/router.go` | Add `platformAdminEmail string` to `Server`; register `PUT /api/v1/admin/users/:id` |
| `internal/api/auth_handlers.go` | In `handleRegister`, if email matches `s.platformAdminEmail`, set `is_platform_admin=true` |
| `internal/api/admin_handlers.go` | Add `handleAdminUpdateUser` — validates input, guards self-demotion, runs UPDATE |
| `web/src/pages/AdminPage.tsx` | Users tab: add platform admin toggle per row |

---

## What This Does Not Do

- **Demote via env var:** The env var only promotes, never revokes. Revocation is done through the UI or direct SQL.
- **Multiple bootstrap emails:** One email is sufficient for bootstrap. Additional admins are added through the UI.
- **Auto-promote first registered user:** Rejected — unsafe in multi-tenant deployments where an org user could register before the operator.
