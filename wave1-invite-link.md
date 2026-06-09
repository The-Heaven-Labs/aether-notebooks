# Wave 1 — Item 1: Invite Link Fix

**Status:** ✅ Complete
**Commit:** fa21fce
**Branch:** feat/all-improvements-2026-06-09

## Changes Made

### Bug 1: Wrong API URL in frontend
**File:** `web/src/pages/MembersPage.tsx` (line 71)
- Changed `/api/v1/organizations/invite-link` → `/api/v1/members/invite-link`
- The route is registered under `/api/v1/members/invite-link` in router.go

### Bug 2: Backend returns relative URL
**File:** `internal/api/org_handlers.go` (line 356)
- Changed `fmt.Sprintf("/join?token=%s", token)` → `fmt.Sprintf("%s/join?token=%s", s.frontendURL, token)`
- Uses the existing `s.frontendURL` field (set via `SetFrontendURL()`) to produce an absolute URL

### Bug 3: No /join route in frontend
**New file:** `web/src/pages/JoinPage.tsx`
- Reads `?token=` from URL query params
- Checks if user is authenticated (redirects to login if not)
- Calls `POST /api/v1/auth/org/join` with `{ invite_link_token: token }`
- On success: calls `loginWithToken(result.token)` and navigates to `/`
- On error: shows error message with link back to login

**File:** `web/src/App.tsx`
- Added import: `import { JoinPage } from './pages/JoinPage'`
- Added route: `<Route path="/join" element={<JoinPage />} />` (after `/login` route)

## Notes
- The `useAuth` hook exposes `loginWithToken(token: string)` (not `login(token)`), which was used correctly
- The JoinPage is NOT wrapped in ProtectedRoute since it needs to handle both authenticated and unauthenticated users
- The `/join` route is placed before the catch-all `/` route for proper React Router matching
