# Wave 5: MOTD Implementation Report (Item 23)

**Status:** ✅ COMPLETE
**Branch:** feat/all-improvements-2026-06-09

## What was implemented

### Database Migration
- `internal/database/migrations/057_motd_messages.sql` — creates `motd_messages` table with org_id, title, content, priority, visibility, pages, show_on_login, created_by, expires_at columns + index on org_id

### Backend
- `internal/api/motd_handlers.go` — 4 handlers:
  - `handleListMOTD` — GET /api/v1/motd (any authenticated user, filtered by org, excludes expired)
  - `handleCreateMOTD` — POST /api/v1/admin/motd (admin only)
  - `handleUpdateMOTD` — PUT /api/v1/admin/motd/{id} (admin only)
  - `handleDeleteMOTD` — DELETE /api/v1/admin/motd/{id} (admin only)
- Routes registered in `internal/api/router.go`

### Frontend: MOTD Banner (AppShell)
- `web/src/components/AppShell.tsx`:
  - Fetches active MOTDs on mount via GET /api/v1/motd
  - Renders dismissable banners at top of main content area
  - Dismiss state stored in localStorage with 24h auto-expiry
  - Supports line breaks via \n → <br/> conversion
  - Multiple MOTDs stacked by priority

### Frontend: Admin Management (AdminPage)
- `web/src/pages/AdminPage.tsx`:
  - New "MOTD" tab added
  - List of existing MOTDs with edit/delete buttons
  - Form with: title, content (textarea), priority (number), visibility (select), show_on_login (checkbox), expires_at (datetime-local)
  - Create and update mutations with error handling

## Validation
- `go build ./...` — ✅ clean
- `npx tsc --noEmit` — ✅ clean
- All changes committed

## Files changed
- `internal/database/migrations/057_motd_messages.sql` (new)
- `internal/api/motd_handlers.go` (new)
- `internal/api/router.go` (4 routes added)
- `web/src/components/AppShell.tsx` (MOTD banner + dismiss logic)
- `web/src/pages/AdminPage.tsx` (MOTD management tab)

## Commits
- `67c15a4` — feat: admin MOTD system with dismissable banners (item 23)
- (migration, handler, router, AppShell changes landed in `493d0b8` from parallel agent)
