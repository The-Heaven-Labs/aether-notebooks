# Item 22: Personal Access Tokens — Implementation Report

## Status: ✅ COMPLETE

## What was implemented

### Database Migration
- **File:** `internal/database/migrations/055_api_tokens.sql`
- Creates `api_tokens` table with: id, user_id, org_id, name, token_hash (bcrypt), last_used_at, expires_at, created_at
- Indexes on user_id and token_hash

### Backend: Token CRUD API
- **File:** `internal/api/token_handlers.go` (new)
- `POST /api/v1/tokens` — Create token (returns raw token ONCE, stores bcrypt hash)
- `GET /api/v1/tokens` — List user's tokens (metadata only, no hash)
- `DELETE /api/v1/tokens/{id}` — Revoke token (scoped to current user)
- Token format: `hnb_tok_` + 32 random bytes hex-encoded

### Auth Middleware Enhancement
- **File:** `internal/api/middleware.go` (modified)
- `AuthMiddleware` now accepts a `*pgxpool.Pool` parameter
- Detects tokens starting with `hnb_tok_` and validates via bcrypt comparison
- Sets Claims context (UserID, OrgID, Role) from the token's org/user
- Updates `last_used_at` in background goroutine
- Falls through to JWT validation for non-API tokens

### Route Registration
- **File:** `internal/api/router.go` (modified)
- Updated `AuthMiddleware(s.jwt)` → `AuthMiddleware(s.jwt, s.db.Pool)`
- Added 3 token routes in User routes section

### Frontend: ProfilePage UI
- **File:** `web/src/pages/ProfilePage.tsx` (modified)
- "Personal Access Tokens" section at bottom of profile page
- "Create Token" button → inline form with name input
- After creation: shows token ONCE with copy button and warning banner
- Token list: name, created date, last used, expires, revoke button
- Confirmation dialog before revoking

## Files Changed
| File | Action |
|------|--------|
| `internal/database/migrations/055_api_tokens.sql` | Created |
| `internal/api/token_handlers.go` | Created |
| `internal/api/middleware.go` | Modified (API token validation) |
| `internal/api/middleware_test.go` | Modified (pass nil pool) |
| `internal/api/router.go` | Modified (routes + pool param) |
| `web/src/pages/ProfilePage.tsx` | Modified (full token UI) |

## Validation
- `go build ./...` ✅
- `go vet ./...` ✅
- `npx tsc --noEmit` ✅

## Commit
`30c7492 feat: personal access tokens - full CRUD UI in ProfilePage (item 22)`
