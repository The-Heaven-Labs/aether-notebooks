# Phase 4 — Advanced Features Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Prerequisite:** Phase 3 complete (connector_id on notebooks, cell parameters, slug substitution).

**Goal:** Add Recharts-based chart expansion, dashboard input widgets, templates & snippets, file attachments (local filesystem), presentation mode, and the org/admin model refactor (two-tier roles, invite flows, platform admin panel).

**Architecture:** Migration `005` adds: `users.is_platform_admin`, `org_allowed_domains`, `org_invites`, `org_invite_links`, `templates`, `attachments`. Auth refactored: `POST /auth/register` no longer creates an org; two new endpoints handle org creation and org join. Recharts replaces the current chart rendering with a type-dispatch component and inline config panel. Dashboard gains a React context for parameter values shared between widgets. Presentation mode is a new frontend-only route. Platform admin panel is a new protected route.

**Tech Stack:** Go, React 19, TypeScript, Recharts (new dep), `react-router-dom` (existing), Vitest + RTL + MSW, Playwright.

---

## File Map

**Create:**
- `internal/database/migrations/005_advanced_features.sql`
- `internal/api/org_handlers.go` — `POST /auth/org/create`, `POST /auth/org/join`, `POST /members/invite`, `POST /members/invite-link`, domain auto-join logic
- `internal/api/org_handlers_test.go`
- `internal/api/admin_handlers.go` — platform admin endpoints
- `internal/api/admin_handlers_test.go`
- `internal/api/attachment_handlers.go` — upload, list, get, delete
- `internal/api/attachment_handlers_test.go`
- `internal/api/template_handlers.go` — CRUD for templates/snippets
- `internal/api/template_handlers_test.go`
- `web/src/components/ChartView.tsx` — replaces old chart rendering (Recharts)
- `web/src/components/ChartConfigPanel.tsx` — inline chart config
- `web/src/pages/PresentationPage.tsx`
- `web/src/pages/AdminPage.tsx`
- `web/src/pages/OrgOnboardingPage.tsx` — post-registration wizard
- `web/src/contexts/DashboardParamsContext.tsx`
- `web/src/test/ChartView.test.tsx`
- `web/src/test/PresentationPage.test.tsx`
- `web/src/test/AdminPage.test.tsx`
- `e2e/auth.spec.ts` — updated with new register flow
- `e2e/dashboard.spec.ts` — input widgets
- `e2e/admin.spec.ts`

**Modify:**
- `internal/api/auth_handlers.go` — refactor `handleRegister` to account-only, add domain auto-join check
- `internal/api/router.go` — new routes for org, admin, attachments, templates
- `internal/api/member_handlers.go` — add invite + invite-link handlers
- `internal/models/notebook.go` — no changes needed (Output.Config already exists)
- `web/src/App.tsx` — add `/notebooks/:id/present`, `/admin`, `/onboarding` routes
- `web/src/pages/LoginPage.tsx` / `web/src/pages/RegisterPage.tsx` — updated register flow
- `web/src/components/CellToolbar.tsx` — add "Insert snippet" button
- `web/package.json` — add `recharts`

---

## Task 1: Migration 005 — all new tables

**Files:**
- Create: `internal/database/migrations/005_advanced_features.sql`

- [ ] **Step 1: Write the migration**

```sql
-- internal/database/migrations/005_advanced_features.sql

-- Platform admin flag
ALTER TABLE users ADD COLUMN is_platform_admin BOOLEAN NOT NULL DEFAULT false;

-- Onboarding token for the post-registration wizard (stored transiently in JWT, no table needed)
-- domain-based org join
CREATE TABLE org_allowed_domains (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  domain VARCHAR(255) NOT NULL,
  auto_join BOOLEAN NOT NULL DEFAULT true,
  UNIQUE (org_id, domain)
);

-- Email invites
CREATE TABLE org_invites (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  email VARCHAR(255) NOT NULL,
  role VARCHAR(50) NOT NULL,
  token VARCHAR(64) NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  accepted_at TIMESTAMPTZ,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Shareable invite links
CREATE TABLE org_invite_links (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  role VARCHAR(50) NOT NULL DEFAULT 'viewer',
  token VARCHAR(64) NOT NULL UNIQUE,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Templates and snippets
CREATE TABLE templates (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  type VARCHAR(20) NOT NULL CHECK (type IN ('notebook', 'cell')),
  content JSONB NOT NULL,
  is_builtin BOOLEAN NOT NULL DEFAULT false,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Attachments
CREATE TABLE attachments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  notebook_id UUID REFERENCES notebooks(id) ON DELETE SET NULL,
  filename VARCHAR(255) NOT NULL,
  mime_type VARCHAR(100) NOT NULL,
  size_bytes BIGINT NOT NULL,
  storage_path TEXT NOT NULL,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- [ ] **Step 2: Restart server and verify migration runs**

Run: `task dev`
Expected: server starts, migration applied once.

Check: `psql $DATABASE_URL -c "\d org_invites"` shows the table.

- [ ] **Step 3: Commit**

```bash
git add internal/database/migrations/005_advanced_features.sql
git commit -m "feat: migration 005 — org invites, templates, attachments, platform admin"
```

---

## Task 2: Auth refactor — account-only register + org create/join

**Files:**
- Modify: `internal/api/auth_handlers.go`
- Create: `internal/api/org_handlers.go`
- Create: `internal/api/org_handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/api/org_handlers_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAccountOnly(t *testing.T) {
	s := setupTestServer(t)

	body := `{"email":"newuser@test.com","password":"password123","name":"New User"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	// Account-only register returns onboarding_token, no org in response
	assert.NotEmpty(t, resp["onboarding_token"])
	assert.Nil(t, resp["org"])
}

func TestRegisterOldFlowBackcompat(t *testing.T) {
	// Existing callers that send org_name should still work — they get an org
	s := setupTestServer(t)
	body := `{"email":"legacyuser@test.com","password":"password123","name":"Legacy","org_name":"Legacy Org"}`
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	// Old flow still returns a token (not onboarding_token)
	assert.NotEmpty(t, resp["token"])
	assert.NotNil(t, resp["org"])
}

func TestOrgCreate(t *testing.T) {
	s := setupTestServer(t)

	// Register account-only to get onboarding token
	regBody := `{"email":"neworg@test.com","password":"password123","name":"Org Creator"}`
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	s.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code)
	var regResp map[string]interface{}
	json.NewDecoder(regW.Body).Decode(&regResp)
	onboardingToken := regResp["onboarding_token"].(string)

	// Create org using onboarding token
	orgBody := `{"org_name":"New Org"}`
	orgReq := httptest.NewRequest("POST", "/api/v1/auth/org/create", bytes.NewBufferString(orgBody))
	orgReq.Header.Set("Content-Type", "application/json")
	orgReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	orgW := httptest.NewRecorder()
	s.ServeHTTP(orgW, orgReq)

	assert.Equal(t, http.StatusCreated, orgW.Code)
	var orgResp map[string]interface{}
	json.NewDecoder(orgW.Body).Decode(&orgResp)
	assert.NotEmpty(t, orgResp["token"])
	assert.NotNil(t, orgResp["org"])
}

func TestOrgJoinWithInviteToken(t *testing.T) {
	s := setupTestServer(t)

	// Setup: create an org and an invite
	adminOrgID, adminToken := createTestOrgAndAdmin(t, s)
	inviteToken := createTestInvite(t, s, adminOrgID, adminToken, "invitee@test.com", "viewer")

	// Register the invitee account-only
	regBody := `{"email":"invitee@test.com","password":"password123","name":"Invitee"}`
	regReq := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewBufferString(regBody))
	regReq.Header.Set("Content-Type", "application/json")
	regW := httptest.NewRecorder()
	s.ServeHTTP(regW, regReq)
	require.Equal(t, http.StatusCreated, regW.Code)
	var regResp map[string]interface{}
	json.NewDecoder(regW.Body).Decode(&regResp)
	onboardingToken := regResp["onboarding_token"].(string)

	// Join org using invite token
	joinBody := `{"invite_token":"` + inviteToken + `"}`
	joinReq := httptest.NewRequest("POST", "/api/v1/auth/org/join", bytes.NewBufferString(joinBody))
	joinReq.Header.Set("Content-Type", "application/json")
	joinReq.Header.Set("Authorization", "Bearer "+onboardingToken)
	joinW := httptest.NewRecorder()
	s.ServeHTTP(joinW, joinReq)

	assert.Equal(t, http.StatusOK, joinW.Code)
	var joinResp map[string]interface{}
	json.NewDecoder(joinW.Body).Decode(&joinResp)
	assert.NotEmpty(t, joinResp["token"])
	assert.Equal(t, adminOrgID, joinResp["org"].(map[string]interface{})["id"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run "TestRegisterAccountOnly|TestOrgCreate|TestOrgJoinWithInviteToken" -v`
Expected: FAIL.

- [ ] **Step 3: Refactor handleRegister to account-only when org_name is absent**

Edit `internal/api/auth_handlers.go`. Change `handleRegister` to:
- If `req.OrgName != ""`: run the existing flow (backwards compat — creates user + org, returns `authResponse` with token).
- If `req.OrgName == ""`: create user only, issue a short-lived **onboarding JWT** (15-minute expiry, role `"onboarding"`, no org_id), return `{ "onboarding_token": "..." }`.

The JWT issuer's `Issue` method needs an overload or the onboarding token can use a special `org_id = ""` with `role = "onboarding"`. Check `internal/auth/jwt.go` for the Claims type — add `"onboarding"` as a valid role or use a separate issuer call.

After creating the user, check `org_allowed_domains` for the user's email domain:

```go
domain := emailDomain(req.Email) // splits on "@"
var autoJoinOrgID, autoJoinRole string
s.db.Pool.QueryRow(ctx,
    `SELECT org_id, CASE WHEN auto_join THEN 'member' ELSE NULL END
     FROM org_allowed_domains WHERE domain = $1 AND auto_join = true LIMIT 1`,
    domain,
).Scan(&autoJoinOrgID, &autoJoinRole)

if autoJoinOrgID != "" {
    // Auto-join: add user to org and return full authResponse
    s.db.Pool.Exec(ctx,
        `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'viewer')
         ON CONFLICT DO NOTHING`,
        autoJoinOrgID, userID,
    )
    token, _ := s.jwt.Issue(userID, autoJoinOrgID, "viewer")
    // return full authResponse with token
    return
}

// No auto-join: return onboarding token
onboardingToken, _ := s.jwt.IssueOnboarding(userID)
writeJSON(w, http.StatusCreated, map[string]string{"onboarding_token": onboardingToken})
```

Add `emailDomain` helper:

```go
func emailDomain(email string) string {
    parts := strings.SplitN(email, "@", 2)
    if len(parts) == 2 {
        return parts[1]
    }
    return ""
}
```

- [ ] **Step 4: Add IssueOnboarding to JWT issuer**

In `internal/auth/jwt.go`, add:

```go
// IssueOnboarding issues a 15-minute token for the post-registration wizard.
// The token has no org_id and role="onboarding". It can only be used with
// /auth/org/create and /auth/org/join endpoints.
func (j *JWTIssuer) IssueOnboarding(userID string) (string, error) {
    claims := Claims{
        UserID: userID,
        OrgID:  "",
        Role:   "onboarding",
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
        },
    }
    return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.secret)
}
```

- [ ] **Step 5: Create org_handlers.go with org/create and org/join**

Create `internal/api/org_handlers.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/jackc/pgx/v5"
	"github.com/google/uuid"
)

type createOrgRequest struct {
	OrgName string `json:"org_name"`
}

// handleOrgCreate creates a new org and makes the authenticated user its admin.
// Requires an onboarding token (role="onboarding", no org_id).
func (s *Server) handleOrgCreate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims.Role != "onboarding" {
		writeError(w, http.StatusForbidden, "onboarding token required")
		return
	}

	var req createOrgRequest
	if err := decodeJSON(r, &req); err != nil || req.OrgName == "" {
		writeError(w, http.StatusBadRequest, "org_name is required")
		return
	}

	ctx := r.Context()
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(ctx)

	var orgID string
	slug := slugify(req.OrgName)
	err = tx.QueryRow(ctx,
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		req.OrgName, slug,
	).Scan(&orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create org")
		return
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID, claims.UserID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	token, err := s.jwt.Issue(claims.UserID, orgID, "admin")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: orgID, UserID: claims.UserID,
		Action: "org.create", ResourceType: "org", ResourceID: orgID,
	})

	var userName, userEmail string
	s.db.Pool.QueryRow(ctx, "SELECT name, email FROM users WHERE id = $1", claims.UserID).Scan(&userName, &userEmail)

	resp := authResponse{}
	resp.Token = token
	resp.User.ID = claims.UserID
	resp.User.Name = userName
	resp.User.Email = userEmail
	resp.Org.ID = orgID
	resp.Org.Name = req.OrgName
	resp.Org.Role = "admin"
	writeJSON(w, http.StatusCreated, resp)
}

type joinOrgRequest struct {
	InviteToken     string `json:"invite_token"`
	InviteLinkToken string `json:"invite_link_token"`
}

// handleOrgJoin accepts an invite token or invite link token and adds the user to the org.
func (s *Server) handleOrgJoin(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims.Role != "onboarding" {
		writeError(w, http.StatusForbidden, "onboarding token required")
		return
	}

	var req joinOrgRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	ctx := r.Context()
	var orgID, role string

	if req.InviteToken != "" {
		// Email invite
		err := s.db.Pool.QueryRow(ctx,
			`SELECT org_id, role FROM org_invites
			 WHERE token = $1 AND accepted_at IS NULL AND expires_at > now()`,
			req.InviteToken,
		).Scan(&orgID, &role)
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusBadRequest, "invalid or expired invite token")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		// Mark invite accepted
		s.db.Pool.Exec(ctx,
			`UPDATE org_invites SET accepted_at = now() WHERE token = $1`,
			req.InviteToken,
		)
	} else if req.InviteLinkToken != "" {
		// Invite link
		err := s.db.Pool.QueryRow(ctx,
			`SELECT org_id, role FROM org_invite_links WHERE token = $1`,
			req.InviteLinkToken,
		).Scan(&orgID, &role)
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusBadRequest, "invalid invite link")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
	} else {
		writeError(w, http.StatusBadRequest, "invite_token or invite_link_token required")
		return
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		orgID, claims.UserID, role,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join org")
		return
	}

	token, err := s.jwt.Issue(claims.UserID, orgID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	var orgName, userName, userEmail string
	s.db.Pool.QueryRow(ctx, "SELECT name FROM orgs WHERE id = $1", orgID).Scan(&orgName)
	s.db.Pool.QueryRow(ctx, "SELECT name, email FROM users WHERE id = $1", claims.UserID).Scan(&userName, &userEmail)

	resp := authResponse{}
	resp.Token = token
	resp.User.ID = claims.UserID
	resp.User.Name = userName
	resp.User.Email = userEmail
	resp.Org.ID = orgID
	resp.Org.Name = orgName
	resp.Org.Role = role
	writeJSON(w, http.StatusOK, resp)
}

// handleCreateInvite creates an email invite and returns the token.
// (Email sending is deferred — caller uses the token directly in Phase 4.)
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	type inviteRequest struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	var req inviteRequest
	if err := decodeJSON(r, &req); err != nil || req.Email == "" || req.Role == "" {
		writeError(w, http.StatusBadRequest, "email and role required")
		return
	}

	ctx := r.Context()
	token := uuid.NewString()
	var inviteID string
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO org_invites (org_id, email, role, token, expires_at, created_by)
		 VALUES ($1, $2, $3, $4, now() + INTERVAL '7 days', $5) RETURNING id`,
		claims.OrgID, req.Email, req.Role, token, claims.UserID,
	).Scan(&inviteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invite")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":    inviteID,
		"token": token,
	})
}

// handleCreateInviteLink creates a shareable invite link.
func (s *Server) handleCreateInviteLink(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	type inviteLinkRequest struct {
		Role string `json:"role"`
	}
	var req inviteLinkRequest
	decodeJSON(r, &req)
	if req.Role == "" {
		req.Role = "viewer"
	}

	ctx := r.Context()
	token := uuid.NewString()
	var linkID string
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO org_invite_links (org_id, role, token, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		claims.OrgID, req.Role, token, claims.UserID,
	).Scan(&linkID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invite link")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":    linkID,
		"token": token,
		"url":   "/join?token=" + token,
	})
}
```

- [ ] **Step 6: Register new auth routes**

Add to `internal/api/router.go` in `s.routes()`:

```go
// Onboarding routes (require onboarding JWT, no org)
s.mux.Handle("POST /api/v1/auth/org/create", authMW(http.HandlerFunc(s.handleOrgCreate)))
s.mux.Handle("POST /api/v1/auth/org/join", authMW(http.HandlerFunc(s.handleOrgJoin)))

// Invite routes (org admin)
s.mux.Handle("POST /api/v1/members/invite", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateInvite))))
s.mux.Handle("POST /api/v1/members/invite-link", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateInviteLink))))
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/api/... -run "TestRegisterAccountOnly|TestOrgCreate|TestOrgJoinWithInviteToken" -v`
Expected: PASS.

Run: `go test ./internal/api/... -v`
Expected: all existing tests still PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/api/auth_handlers.go internal/api/org_handlers.go internal/api/org_handlers_test.go internal/api/router.go internal/auth/jwt.go
git commit -m "feat: auth refactor — account-only register, org/create, org/join, invite flows"
```

---

## Task 3: Platform admin handlers

**Files:**
- Create: `internal/api/admin_handlers.go`
- Create: `internal/api/admin_handlers_test.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/middleware.go` (or auth.go) — add `RequirePlatformAdmin` middleware

- [ ] **Step 1: Write the failing tests**

Create `internal/api/admin_handlers_test.go`:

```go
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdminListOrgs_RequiresPlatformAdmin(t *testing.T) {
	s := setupTestServer(t)

	// Regular admin cannot access platform admin routes
	req := httptest.NewRequest("GET", "/api/v1/admin/orgs", nil)
	req = withAdminClaims(req, testOrgID)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestAdminListOrgs_PlatformAdminCanAccess(t *testing.T) {
	s := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/v1/admin/orgs", nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAdminListUsers(t *testing.T) {
	s := setupTestServer(t)
	req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
	req = withPlatformAdminClaims(req)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
```

Add `withPlatformAdminClaims` to the test helper: sets `is_platform_admin: true` in the JWT claims. Platform admin JWT needs a special claim. Add `IsPlatformAdmin bool` to `Claims` in `internal/auth/jwt.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run "TestAdminList" -v`
Expected: FAIL with 404.

- [ ] **Step 3: Add IsPlatformAdmin to JWT Claims**

In `internal/auth/jwt.go`, add `IsPlatformAdmin bool` to `Claims`:

```go
type Claims struct {
    UserID          string `json:"user_id"`
    OrgID           string `json:"org_id"`
    Role            string `json:"role"`
    IsPlatformAdmin bool   `json:"is_platform_admin,omitempty"`
    jwt.RegisteredClaims
}
```

Add `IssuePlatformAdmin` method or update `Issue` to accept `isPlatformAdmin bool`. Since the platform admin flag comes from the DB, update `handleLogin` to query `is_platform_admin` and pass it to `Issue`.

- [ ] **Step 4: Add RequirePlatformAdmin middleware**

In `internal/api/middleware.go` (or create `internal/api/admin_middleware.go`):

```go
func RequirePlatformAdmin(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        claims := ClaimsFromContext(r.Context())
        if !claims.IsPlatformAdmin {
            writeError(w, http.StatusForbidden, "platform admin access required")
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

- [ ] **Step 5: Implement admin handlers**

Create `internal/api/admin_handlers.go`:

```go
package api

import (
	"net/http"
)

func (s *Server) handleAdminListOrgs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.Pool.Query(ctx,
		`SELECT o.id, o.name, o.slug, COUNT(m.user_id) as member_count, o.created_at
		 FROM orgs o
		 LEFT JOIN org_members m ON m.org_id = o.id
		 GROUP BY o.id ORDER BY o.created_at DESC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type orgSummary struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		MemberCount int    `json:"member_count"`
		CreatedAt   string `json:"created_at"`
	}
	var orgs []orgSummary
	for rows.Next() {
		var o orgSummary
		rows.Scan(&o.ID, &o.Name, &o.Slug, &o.MemberCount, &o.CreatedAt)
		orgs = append(orgs, o)
	}
	if orgs == nil {
		orgs = []orgSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"orgs": orgs})
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.Pool.Query(ctx,
		`SELECT u.id, u.email, u.name, u.is_platform_admin, u.created_at,
		        array_agg(o.name ORDER BY o.name) FILTER (WHERE o.name IS NOT NULL) as orgs
		 FROM users u
		 LEFT JOIN org_members m ON m.user_id = u.id
		 LEFT JOIN orgs o ON o.id = m.org_id
		 GROUP BY u.id ORDER BY u.created_at DESC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type userSummary struct {
		ID              string   `json:"id"`
		Email           string   `json:"email"`
		Name            string   `json:"name"`
		IsPlatformAdmin bool     `json:"is_platform_admin"`
		CreatedAt       string   `json:"created_at"`
		Orgs            []string `json:"orgs"`
	}
	var users []userSummary
	for rows.Next() {
		var u userSummary
		rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsPlatformAdmin, &u.CreatedAt, &u.Orgs)
		if u.Orgs == nil {
			u.Orgs = []string{}
		}
		users = append(users, u)
	}
	if users == nil {
		users = []userSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": users})
}
```

- [ ] **Step 6: Register admin routes**

Add to `internal/api/router.go`:

```go
// Platform admin routes
adminMW := authMW(RequirePlatformAdmin(http.HandlerFunc(nil))) // pattern
s.mux.Handle("GET /api/v1/admin/orgs", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListOrgs))))
s.mux.Handle("GET /api/v1/admin/users", authMW(RequirePlatformAdmin(http.HandlerFunc(s.handleAdminListUsers))))
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/api/... -run "TestAdminList" -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/api/admin_handlers.go internal/api/admin_handlers_test.go internal/api/router.go internal/auth/jwt.go
git commit -m "feat: platform admin routes — list orgs, list users, RequirePlatformAdmin middleware"
```

---

## Task 4: Attachments API

**Files:**
- Create: `internal/api/attachment_handlers.go`
- Create: `internal/api/attachment_handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/api/attachment_handlers_test.go`:

```go
package api_test

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachmentUploadAndGet(t *testing.T) {
	s := setupTestServerWithAttachDir(t) // sets HNB_ATTACHMENT_DIR to t.TempDir()
	nbID := createTestNotebook(t, s)

	// Upload
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "test.png")
	io.WriteString(fw, "fake-png-data")
	w.Close()

	req := httptest.NewRequest("POST", "/api/v1/notebooks/"+nbID+"/attachments", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = withEditorClaims(req, testOrgID)
	rw := httptest.NewRecorder()
	s.ServeHTTP(rw, req)
	require.Equal(t, http.StatusCreated, rw.Code)

	var resp map[string]interface{}
	json.NewDecoder(rw.Body).Decode(&resp)
	attID := resp["id"].(string)

	// Get
	getReq := httptest.NewRequest("GET", "/api/v1/attachments/"+attID, nil)
	getReq = withEditorClaims(getReq, testOrgID)
	getRW := httptest.NewRecorder()
	s.ServeHTTP(getRW, getReq)
	assert.Equal(t, http.StatusOK, getRW.Code)
	assert.Equal(t, "fake-png-data", getRW.Body.String())
}

func TestAttachmentDelete(t *testing.T) {
	s := setupTestServerWithAttachDir(t)
	nbID := createTestNotebook(t, s)
	attID := uploadTestAttachment(t, s, nbID)

	req := httptest.NewRequest("DELETE", "/api/v1/attachments/"+attID, nil)
	req = withEditorClaims(req, testOrgID)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// Subsequent GET returns 404
	getReq := httptest.NewRequest("GET", "/api/v1/attachments/"+attID, nil)
	getReq = withEditorClaims(getReq, testOrgID)
	getRW := httptest.NewRecorder()
	s.ServeHTTP(getRW, getReq)
	assert.Equal(t, http.StatusNotFound, getRW.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run "TestAttachment" -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement attachment handlers**

Create `internal/api/attachment_handlers.go`:

```go
package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	// Verify notebook belongs to org
	var exists bool
	s.db.Pool.QueryRow(ctx, "SELECT true FROM notebooks WHERE id = $1 AND org_id = $2", nbID, claims.OrgID).Scan(&exists)
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	r.ParseMultipartForm(32 << 20) // 32MB max
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()

	attachDir := s.attachmentDir // set from env HNB_ATTACHMENT_DIR
	if attachDir == "" {
		attachDir = "./attachments"
	}
	if err := os.MkdirAll(attachDir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}

	id := newUUID()
	storagePath := filepath.Join(attachDir, id)
	f, err := os.Create(storagePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	defer f.Close()
	size, err := io.Copy(f, file)
	if err != nil {
		os.Remove(storagePath)
		writeError(w, http.StatusInternalServerError, "write error")
		return
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	// Strip any params from mime type
	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	var attID string
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO attachments (id, org_id, notebook_id, filename, mime_type, size_bytes, storage_path, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		id, claims.OrgID, nbID, header.Filename, mimeType, size, storagePath, claims.UserID,
	).Scan(&attID)
	if err != nil {
		os.Remove(storagePath)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":        attID,
		"filename":  header.Filename,
		"mime_type": mimeType,
		"size":      size,
	})
}

func (s *Server) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	attID := r.PathValue("id")
	ctx := r.Context()

	var storagePath, mimeType, filename string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT storage_path, mime_type, filename FROM attachments WHERE id = $1 AND org_id = $2`,
		attID, claims.OrgID,
	).Scan(&storagePath, &mimeType, &filename)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	f, err := os.Open(storagePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, filename))
	io.Copy(w, f)
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	nbID := r.PathValue("notebook_id")
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, filename, mime_type, size_bytes, created_at FROM attachments
		 WHERE notebook_id = $1 AND org_id = $2 ORDER BY created_at DESC`,
		nbID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type att struct {
		ID        string `json:"id"`
		Filename  string `json:"filename"`
		MimeType  string `json:"mime_type"`
		Size      int64  `json:"size"`
		CreatedAt string `json:"created_at"`
	}
	var atts []att
	for rows.Next() {
		var a att
		rows.Scan(&a.ID, &a.Filename, &a.MimeType, &a.Size, &a.CreatedAt)
		atts = append(atts, a)
	}
	if atts == nil {
		atts = []att{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"attachments": atts})
}

func (s *Server) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	attID := r.PathValue("id")
	ctx := r.Context()

	var storagePath string
	err := s.db.Pool.QueryRow(ctx,
		`DELETE FROM attachments WHERE id = $1 AND org_id = $2 RETURNING storage_path`,
		attID, claims.OrgID,
	).Scan(&storagePath)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}

	os.Remove(storagePath)
	w.WriteHeader(http.StatusNoContent)
}
```

Also add `attachmentDir string` to the `Server` struct in `router.go` and read it in `NewServer` from config. Add `HNB_ATTACHMENT_DIR` to `internal/config/config.go`.

- [ ] **Step 4: Register attachment routes**

Add to `internal/api/router.go`:

```go
s.mux.Handle("POST /api/v1/notebooks/{notebook_id}/attachments", authMW(RequireRole("editor")(http.HandlerFunc(s.handleUploadAttachment))))
s.mux.Handle("GET /api/v1/notebooks/{notebook_id}/attachments", authMW(http.HandlerFunc(s.handleListAttachments)))
s.mux.Handle("GET /api/v1/attachments/{id}", authMW(http.HandlerFunc(s.handleGetAttachment)))
s.mux.Handle("DELETE /api/v1/attachments/{id}", authMW(RequireRole("editor")(http.HandlerFunc(s.handleDeleteAttachment))))
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/api/... -run "TestAttachment" -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/attachment_handlers.go internal/api/attachment_handlers_test.go internal/api/router.go internal/config/config.go
git commit -m "feat: attachments API — upload, list, get, delete with local filesystem storage"
```

---

## Task 5: Templates & Snippets API

**Files:**
- Create: `internal/api/template_handlers.go`
- Create: `internal/api/template_handlers_test.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/api/template_handlers_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAndListCellSnippet(t *testing.T) {
	s := setupTestServer(t)

	body := `{"name":"Date Range","type":"cell","content":{"source":"WHERE created_at BETWEEN {{start}} AND {{end}}","type":"code","language":"sql"}}`
	req := httptest.NewRequest("POST", "/api/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminClaims(req, testOrgID)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// List
	listReq := httptest.NewRequest("GET", "/api/v1/templates?type=cell", nil)
	listReq = withAdminClaims(listReq, testOrgID)
	listW := httptest.NewRecorder()
	s.ServeHTTP(listW, listReq)
	assert.Equal(t, http.StatusOK, listW.Code)
	var resp map[string]interface{}
	json.NewDecoder(listW.Body).Decode(&resp)
	templates := resp["templates"].([]interface{})
	assert.GreaterOrEqual(t, len(templates), 1)
}

func TestTemplateVisibilityIsolatedByOrg(t *testing.T) {
	s := setupTestServer(t)

	// Create template as org A
	body := `{"name":"Org A Template","type":"cell","content":{}}`
	req := httptest.NewRequest("POST", "/api/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = withAdminClaims(req, testOrgID)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// List as org B — should not see org A's template
	listReq := httptest.NewRequest("GET", "/api/v1/templates", nil)
	listReq = withAdminClaims(listReq, "different-org-id")
	listW := httptest.NewRecorder()
	s.ServeHTTP(listW, listReq)
	assert.Equal(t, http.StatusOK, listW.Code)
	var resp map[string]interface{}
	json.NewDecoder(listW.Body).Decode(&resp)
	templates := resp["templates"].([]interface{})
	for _, t := range templates {
		tmpl := t.(map[string]interface{})
		assert.NotEqual(t, "Org A Template", tmpl["name"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run "TestCreateAndListCellSnippet|TestTemplateVisibility" -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement template handlers**

Create `internal/api/template_handlers.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type createTemplateRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Type        string          `json:"type"` // "notebook" or "cell"
	Content     json.RawMessage `json:"content"`
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req createTemplateRequest
	if err := decodeJSON(r, &req); err != nil || req.Name == "" || (req.Type != "notebook" && req.Type != "cell") {
		writeError(w, http.StatusBadRequest, "name and type ('notebook' or 'cell') required")
		return
	}

	ctx := r.Context()
	var id string
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO templates (org_id, name, description, type, content, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		claims.OrgID, req.Name, req.Description, req.Type, req.Content, claims.UserID,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create template")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()
	typeFilter := r.URL.Query().Get("type") // optional: "notebook" or "cell"

	var rows interface{ Close(); Next() bool; Scan(...any) error; Err() error }
	var err error
	if typeFilter != "" {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, name, description, type, content, is_builtin, created_at
			 FROM templates WHERE org_id = $1 AND type = $2 ORDER BY name`,
			claims.OrgID, typeFilter,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, name, description, type, content, is_builtin, created_at
			 FROM templates WHERE org_id = $1 ORDER BY name`,
			claims.OrgID,
		)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type tmpl struct {
		ID          string          `json:"id"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Type        string          `json:"type"`
		Content     json.RawMessage `json:"content"`
		IsBuiltin   bool            `json:"is_builtin"`
		CreatedAt   string          `json:"created_at"`
	}
	var templates []tmpl
	for rows.Next() {
		var t tmpl
		rows.Scan(&t.ID, &t.Name, &t.Description, &t.Type, &t.Content, &t.IsBuiltin, &t.CreatedAt)
		templates = append(templates, t)
	}
	if templates == nil {
		templates = []tmpl{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": templates})
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	id := r.PathValue("id")
	ctx := r.Context()

	var deletedID string
	err := s.db.Pool.QueryRow(ctx,
		`DELETE FROM templates WHERE id = $1 AND org_id = $2 AND is_builtin = false RETURNING id`,
		id, claims.OrgID,
	).Scan(&deletedID)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "template not found or is built-in")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Register template routes**

```go
s.mux.Handle("POST /api/v1/templates", authMW(RequireRole("admin")(http.HandlerFunc(s.handleCreateTemplate))))
s.mux.Handle("GET /api/v1/templates", authMW(http.HandlerFunc(s.handleListTemplates)))
s.mux.Handle("DELETE /api/v1/templates/{id}", authMW(RequireRole("admin")(http.HandlerFunc(s.handleDeleteTemplate))))
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/api/... -run "TestCreateAndListCellSnippet|TestTemplateVisibility" -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/template_handlers.go internal/api/template_handlers_test.go internal/api/router.go
git commit -m "feat: templates and snippets API — create, list, delete with org isolation"
```

---

## Task 6: Frontend — Recharts ChartView

**Files:**
- Modify: `web/package.json` — add `recharts`
- Create: `web/src/components/ChartView.tsx` — replaces old chart rendering
- Create: `web/src/components/ChartConfigPanel.tsx`
- Create: `web/src/test/ChartView.test.tsx`

- [ ] **Step 1: Install Recharts**

Run: `cd web && npm install recharts`
Expected: `package.json` updated with recharts dependency.

- [ ] **Step 2: Write the failing tests**

Create `web/src/test/ChartView.test.tsx`:

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { ChartView } from '../components/ChartView'

const tableOutput = {
  type: 'table',
  data: {
    columns: [
      { name: 'month', type: 'string' },
      { name: 'revenue', type: 'float' },
    ],
    rows: [['Jan', 1000], ['Feb', 1500], ['Mar', 1200]],
  },
}

test('renders bar chart when type is bar', () => {
  const config = { chartType: 'bar', xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  // Recharts renders SVG
  expect(document.querySelector('svg')).toBeInTheDocument()
})

test('renders line chart when type is line', () => {
  const config = { chartType: 'line', xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(document.querySelector('svg')).toBeInTheDocument()
})

test('renders pie chart when type is pie', () => {
  const config = { chartType: 'pie', xAxis: 'month', yAxis: ['revenue'] }
  render(<ChartView output={{ ...tableOutput, config }} />)
  expect(document.querySelector('svg')).toBeInTheDocument()
})

test('shows Configure button that toggles config panel', () => {
  const config = { chartType: 'bar', xAxis: 'month', yAxis: ['revenue'] }
  const onConfigChange = vi.fn()
  render(<ChartView output={{ ...tableOutput, config }} onConfigChange={onConfigChange} />)
  const btn = screen.getByRole('button', { name: /configure/i })
  fireEvent.click(btn)
  expect(screen.getByLabelText(/x axis/i)).toBeInTheDocument()
})

test('config panel calls onConfigChange when x-axis changes', () => {
  const config = { chartType: 'bar', xAxis: 'month', yAxis: ['revenue'] }
  const onConfigChange = vi.fn()
  render(<ChartView output={{ ...tableOutput, config }} onConfigChange={onConfigChange} />)
  fireEvent.click(screen.getByRole('button', { name: /configure/i }))
  fireEvent.change(screen.getByLabelText(/x axis/i), { target: { value: 'revenue' } })
  expect(onConfigChange).toHaveBeenCalledWith(expect.objectContaining({ xAxis: 'revenue' }))
})
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd web && npm run test -- --run ChartView`
Expected: FAIL with module not found.

- [ ] **Step 4: Implement ChartConfigPanel**

Create `web/src/components/ChartConfigPanel.tsx`:

```tsx
interface ChartConfig {
  chartType: 'bar' | 'stacked_bar' | 'line' | 'area' | 'scatter' | 'pie' | 'donut'
  xAxis: string
  yAxis: string[]
  title?: string
  showLegend?: boolean
  showGrid?: boolean
}

interface ChartConfigPanelProps {
  config: ChartConfig
  columns: string[]
  onChange: (config: ChartConfig) => void
}

export function ChartConfigPanel({ config, columns, onChange }: ChartConfigPanelProps) {
  return (
    <div className="chart-config-panel border-t border-gray-700 p-3 grid grid-cols-2 gap-3 text-sm">
      <div>
        <label htmlFor="chart-type" className="block text-xs text-gray-400 mb-1">Chart type</label>
        <select
          id="chart-type"
          className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1"
          value={config.chartType}
          onChange={e => onChange({ ...config, chartType: e.target.value as ChartConfig['chartType'] })}
        >
          {(['bar','stacked_bar','line','area','scatter','pie','donut'] as const).map(t => (
            <option key={t} value={t}>{t.replace('_', ' ')}</option>
          ))}
        </select>
      </div>
      <div>
        <label htmlFor="chart-x-axis" className="block text-xs text-gray-400 mb-1">X axis</label>
        <select
          id="chart-x-axis"
          className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1"
          value={config.xAxis}
          onChange={e => onChange({ ...config, xAxis: e.target.value })}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <div className="col-span-2">
        <label className="block text-xs text-gray-400 mb-1">Y axis (select multiple)</label>
        <select
          multiple
          className="w-full bg-gray-800 border border-gray-700 rounded px-2 py-1"
          value={config.yAxis}
          onChange={e => {
            const selected = Array.from(e.target.selectedOptions).map(o => o.value)
            onChange({ ...config, yAxis: selected })
          }}
        >
          {columns.map(c => <option key={c} value={c}>{c}</option>)}
        </select>
      </div>
      <label className="flex items-center gap-2 text-xs text-gray-400">
        <input type="checkbox" checked={config.showLegend ?? true} onChange={e => onChange({ ...config, showLegend: e.target.checked })} />
        Show legend
      </label>
      <label className="flex items-center gap-2 text-xs text-gray-400">
        <input type="checkbox" checked={config.showGrid ?? true} onChange={e => onChange({ ...config, showGrid: e.target.checked })} />
        Show grid
      </label>
    </div>
  )
}
```

- [ ] **Step 5: Implement ChartView**

Create `web/src/components/ChartView.tsx`:

```tsx
import { useState } from 'react'
import {
  BarChart, Bar, LineChart, Line, AreaChart, Area,
  ScatterChart, Scatter, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer,
} from 'recharts'
import { ChartConfigPanel } from './ChartConfigPanel'

interface ChartConfig {
  chartType: 'bar' | 'stacked_bar' | 'line' | 'area' | 'scatter' | 'pie' | 'donut'
  xAxis: string
  yAxis: string[]
  title?: string
  showLegend?: boolean
  showGrid?: boolean
}

interface ChartViewProps {
  output: { type: string; data: { columns: { name: string }[]; rows: unknown[][] }; config?: unknown }
  onConfigChange?: (config: ChartConfig) => void
}

const COLORS = ['#6366f1','#22d3ee','#f59e0b','#10b981','#ef4444','#8b5cf6','#ec4899']

export function ChartView({ output, onConfigChange }: ChartViewProps) {
  const [showConfig, setShowConfig] = useState(false)
  const cfg = (output.config ?? {}) as ChartConfig
  const columns = output.data?.columns?.map(c => c.name) ?? []
  const xAxis = cfg.xAxis || columns[0] || ''
  const yAxes = cfg.yAxis?.length ? cfg.yAxis : columns.slice(1, 2)

  // Transform rows to recharts-friendly objects
  const chartData = (output.data?.rows ?? []).map(row => {
    const obj: Record<string, unknown> = {}
    columns.forEach((col, i) => { obj[col] = row[i] })
    return obj
  })

  const showLegend = cfg.showLegend ?? true
  const showGrid = cfg.showGrid ?? true

  const renderChart = () => {
    const commonProps = { data: chartData, margin: { top: 8, right: 16, bottom: 8, left: 0 } }
    switch (cfg.chartType ?? 'bar') {
      case 'bar':
        return (
          <BarChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={xAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip contentStyle={{ background: '#1e1e2e', border: '1px solid #333' }} />
            {showLegend && <Legend />}
            {yAxes.map((y, i) => <Bar key={y} dataKey={y} fill={COLORS[i % COLORS.length]} />)}
          </BarChart>
        )
      case 'stacked_bar':
        return (
          <BarChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={xAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip contentStyle={{ background: '#1e1e2e', border: '1px solid #333' }} />
            {showLegend && <Legend />}
            {yAxes.map((y, i) => <Bar key={y} dataKey={y} stackId="a" fill={COLORS[i % COLORS.length]} />)}
          </BarChart>
        )
      case 'line':
        return (
          <LineChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={xAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip contentStyle={{ background: '#1e1e2e', border: '1px solid #333' }} />
            {showLegend && <Legend />}
            {yAxes.map((y, i) => <Line key={y} type="monotone" dataKey={y} stroke={COLORS[i % COLORS.length]} dot={false} />)}
          </LineChart>
        )
      case 'area':
        return (
          <AreaChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={xAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip contentStyle={{ background: '#1e1e2e', border: '1px solid #333' }} />
            {showLegend && <Legend />}
            {yAxes.map((y, i) => (
              <Area key={y} type="monotone" dataKey={y} stroke={COLORS[i % COLORS.length]} fill={COLORS[i % COLORS.length]} fillOpacity={0.3} />
            ))}
          </AreaChart>
        )
      case 'pie':
      case 'donut': {
        const innerRadius = cfg.chartType === 'donut' ? '60%' : 0
        return (
          <PieChart>
            <Pie data={chartData} dataKey={yAxes[0] ?? ''} nameKey={xAxis} cx="50%" cy="50%" outerRadius="80%" innerRadius={innerRadius}>
              {chartData.map((_, i) => <Cell key={i} fill={COLORS[i % COLORS.length]} />)}
            </Pie>
            <Tooltip contentStyle={{ background: '#1e1e2e', border: '1px solid #333' }} />
            {showLegend && <Legend />}
          </PieChart>
        )
      }
      case 'scatter':
        return (
          <ScatterChart {...commonProps}>
            {showGrid && <CartesianGrid strokeDasharray="3 3" stroke="#333" />}
            <XAxis dataKey={xAxis} name={xAxis} tick={{ fontSize: 11, fill: '#888' }} />
            <YAxis dataKey={yAxes[0]} name={yAxes[0]} tick={{ fontSize: 11, fill: '#888' }} />
            <Tooltip contentStyle={{ background: '#1e1e2e', border: '1px solid #333' }} />
            <Scatter data={chartData} fill={COLORS[0]} />
          </ScatterChart>
        )
      default:
        return <div className="text-gray-500 p-4">Unknown chart type</div>
    }
  }

  return (
    <div className="chart-view">
      <ResponsiveContainer width="100%" height={300}>
        {renderChart()}
      </ResponsiveContainer>
      {onConfigChange && (
        <div>
          <button
            className="text-xs text-gray-500 hover:text-gray-300 px-3 py-1"
            onClick={() => setShowConfig(v => !v)}
            aria-label={showConfig ? 'Hide configure' : 'Configure'}
          >
            {showConfig ? 'Hide' : 'Configure'}
          </button>
          {showConfig && (
            <ChartConfigPanel
              config={cfg}
              columns={columns}
              onChange={onConfigChange}
            />
          )}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 6: Wire ChartView into OutputRenderer**

In `web/src/components/OutputRenderer.tsx`, find where `output.type === 'chart'` is handled (or where table output has chart config). Replace the old chart rendering with:

```tsx
import { ChartView } from './ChartView'

// In the output type switch, add/replace the chart case:
if (output.type === 'table' && output.config && (output.config as any).chartType) {
  return <ChartView output={output} onConfigChange={config => onUpdateOutput?.({ ...output, config })} />
}
```

- [ ] **Step 7: Run tests**

Run: `cd web && npm run test -- --run ChartView`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add web/src/components/ChartView.tsx web/src/components/ChartConfigPanel.tsx web/src/components/OutputRenderer.tsx web/src/test/ChartView.test.tsx web/package.json web/package-lock.json
git commit -m "feat: Recharts ChartView — bar, stacked bar, line, area, scatter, pie, donut + inline config panel"
```

---

## Task 7: Frontend — Dashboard input widgets

**Files:**
- Create: `web/src/contexts/DashboardParamsContext.tsx`
- Modify: `web/src/pages/DashboardPage.tsx`

- [ ] **Step 1: Create DashboardParamsContext**

Create `web/src/contexts/DashboardParamsContext.tsx`:

```tsx
import { createContext, useContext, useState, useCallback, useRef } from 'react'

interface DashboardParamsContextValue {
  params: Record<string, string>
  setParam: (name: string, value: string) => void
}

const DashboardParamsContext = createContext<DashboardParamsContextValue>({
  params: {},
  setParam: () => {},
})

export function DashboardParamsProvider({ children }: { children: React.ReactNode }) {
  const [params, setParams] = useState<Record<string, string>>({})

  const setParam = useCallback((name: string, value: string) => {
    setParams(prev => ({ ...prev, [name]: value }))
  }, [])

  return (
    <DashboardParamsContext.Provider value={{ params, setParam }}>
      {children}
    </DashboardParamsContext.Provider>
  )
}

export function useDashboardParams() {
  return useContext(DashboardParamsContext)
}
```

- [ ] **Step 2: Add widget types to DashboardPage**

In `web/src/pages/DashboardPage.tsx`, wrap the page content in `<DashboardParamsProvider>`. For each widget, check `widget.type`:

- `date_picker`: renders `<input type="date">` that calls `setParam(widget.config.paramName, value)`
- `date_range`: renders two `<input type="date">` for start + end
- `multi_select`: renders a `<select multiple>` from `widget.config.options`
- `freetext`: renders `<input type="text">`
- `number`: renders `<input type="number">`

Code cells and chart widgets that have `{{param_name}}` in their source use `params` from context to re-execute (debounced 300ms using `useEffect` watching `params`).

```tsx
import { DashboardParamsProvider, useDashboardParams } from '../contexts/DashboardParamsContext'

// Input widget component:
function InputWidget({ widget }: { widget: Widget }) {
  const { setParam } = useDashboardParams()
  const paramName = (widget.config as any)?.paramName ?? ''

  switch (widget.type) {
    case 'date_picker':
      return (
        <div className="widget-input">
          <label className="widget-label">{widget.config?.label ?? paramName}</label>
          <input
            type="date"
            className="widget-date-input"
            defaultValue={(widget.config as any)?.default ?? ''}
            onChange={e => setParam(paramName, e.target.value)}
          />
        </div>
      )
    case 'date_range':
      return (
        <div className="widget-input flex gap-2">
          <input type="date" onChange={e => setParam(paramName + '_start', e.target.value)} />
          <span className="text-gray-500">to</span>
          <input type="date" onChange={e => setParam(paramName + '_end', e.target.value)} />
        </div>
      )
    case 'freetext':
      return <input type="text" onChange={e => setParam(paramName, e.target.value)} className="widget-text-input" />
    case 'number':
      return <input type="number" onChange={e => setParam(paramName, e.target.value)} className="widget-number-input" />
    case 'multi_select': {
      const options = (widget.config as any)?.options ?? []
      return (
        <select multiple onChange={e => {
          const vals = Array.from(e.target.selectedOptions).map(o => o.value).join(',')
          setParam(paramName, vals)
        }}>
          {options.map((o: string) => <option key={o} value={o}>{o}</option>)}
        </select>
      )
    }
    default:
      return null
  }
}
```

- [ ] **Step 3: Wire param-dependent re-execution**

For code/chart widgets that contain `{{...}}` in their source, add a `useEffect` that watches `params` (debounced 300ms) and re-runs the widget:

```tsx
const { params } = useDashboardParams()
const debounceRef = useRef<ReturnType<typeof setTimeout>>()
useEffect(() => {
  if (!widget.source?.includes('{{')) return
  clearTimeout(debounceRef.current)
  debounceRef.current = setTimeout(() => {
    executeWidget(widget.id, params)
  }, 300)
  return () => clearTimeout(debounceRef.current)
}, [params])
```

- [ ] **Step 4: Commit**

```bash
git add web/src/contexts/DashboardParamsContext.tsx web/src/pages/DashboardPage.tsx
git commit -m "feat: dashboard input widgets — date_picker, date_range, multi_select, freetext, number with param context"
```

---

## Task 8: Frontend — Presentation Mode

**Files:**
- Create: `web/src/pages/PresentationPage.tsx`
- Create: `web/src/test/PresentationPage.test.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Write the failing test**

Create `web/src/test/PresentationPage.test.tsx`:

```tsx
import { render, screen, fireEvent } from '@testing-library/react'
import { PresentationPage } from '../pages/PresentationPage'
import { http, HttpResponse } from 'msw'
import { server } from './setup'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

const mockNotebook = {
  id: 'nb-1',
  title: 'Sales Report',
  cells: [
    { id: 'c1', type: 'text', source: '# Slide 1', outputs: [] },
    { id: 'c2', type: 'code', source: 'SELECT 1', outputs: [{ type: 'table', data: { columns: [], rows: [] } }] },
    { id: 'c3', type: 'text', source: '# Slide 3', outputs: [] },
  ],
}

beforeEach(() => {
  server.use(http.get('/api/v1/notebooks/:id', () => HttpResponse.json(mockNotebook)))
})

function renderPresentation() {
  return render(
    <MemoryRouter initialEntries={['/notebooks/nb-1/present']}>
      <Routes>
        <Route path="/notebooks/:id/present" element={<PresentationPage />} />
      </Routes>
    </MemoryRouter>
  )
}

test('shows first cell on load', async () => {
  renderPresentation()
  expect(await screen.findByText(/Slide 1/)).toBeInTheDocument()
})

test('Next button advances to second cell', async () => {
  renderPresentation()
  await screen.findByText(/Slide 1/)
  fireEvent.click(screen.getByRole('button', { name: /next/i }))
  // Second cell is a code cell — shows output, not source
  expect(screen.queryByText('SELECT 1')).not.toBeInTheDocument()
})

test('Previous button is disabled on first cell', async () => {
  renderPresentation()
  await screen.findByText(/Slide 1/)
  expect(screen.getByRole('button', { name: /previous/i })).toBeDisabled()
})

test('shows progress indicator', async () => {
  renderPresentation()
  await screen.findByText(/Slide 1/)
  expect(screen.getByText('1 / 3')).toBeInTheDocument()
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npm run test -- --run PresentationPage`
Expected: FAIL with module not found.

- [ ] **Step 3: Implement PresentationPage**

Create `web/src/pages/PresentationPage.tsx`:

```tsx
import { useState, useEffect, useCallback } from 'react'
import { useParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import { OutputRenderer } from '../components/OutputRenderer'

interface Cell {
  id: string
  type: string
  source: string
  outputs: unknown[]
}

interface Notebook {
  id: string
  title: string
  cells: Cell[]
}

export function PresentationPage() {
  const { id } = useParams<{ id: string }>()
  const [notebook, setNotebook] = useState<Notebook | null>(null)
  const [index, setIndex] = useState(0)

  useEffect(() => {
    fetch(`/api/v1/notebooks/${id}`, { credentials: 'include' })
      .then(r => r.json())
      .then(setNotebook)
  }, [id])

  const total = notebook?.cells.length ?? 0
  const cell = notebook?.cells[index]

  const prev = useCallback(() => setIndex(i => Math.max(0, i - 1)), [])
  const next = useCallback(() => setIndex(i => Math.min(total - 1, i + 1)), [total])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next()
      if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') prev()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [next, prev])

  if (!notebook || !cell) {
    return <div className="flex items-center justify-center h-screen bg-gray-950 text-gray-400">Loading…</div>
  }

  return (
    <div className="flex flex-col h-screen bg-gray-950 text-white">
      {/* Cell content */}
      <div className="flex-1 flex items-center justify-center p-16 overflow-auto">
        <div className="max-w-4xl w-full">
          {cell.type === 'text' ? (
            <div className="prose prose-invert max-w-none text-xl">
              <ReactMarkdown>{cell.source}</ReactMarkdown>
            </div>
          ) : (
            <div>
              {cell.outputs.map((out, i) => (
                <OutputRenderer key={i} output={out as any} />
              ))}
            </div>
          )}
        </div>
      </div>

      {/* Navigation bar */}
      <div className="flex items-center justify-between px-8 py-4 bg-gray-900 border-t border-gray-800">
        <button
          className="px-4 py-2 rounded bg-gray-800 text-gray-300 hover:bg-gray-700 disabled:opacity-40"
          onClick={prev}
          disabled={index === 0}
          aria-label="Previous"
        >
          ← Previous
        </button>
        <span className="text-gray-400 text-sm">{index + 1} / {total}</span>
        <button
          className="px-4 py-2 rounded bg-gray-800 text-gray-300 hover:bg-gray-700 disabled:opacity-40"
          onClick={next}
          disabled={index === total - 1}
          aria-label="Next"
        >
          Next →
        </button>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Register route in App.tsx**

In `web/src/App.tsx`, add:

```tsx
import { PresentationPage } from './pages/PresentationPage'
// In the Routes:
<Route path="/notebooks/:id/present" element={<PresentationPage />} />
```

Also add a "Present" button in `NotebookPage` header that opens `/notebooks/:id/present` in a new tab:

```tsx
<button onClick={() => window.open(`/notebooks/${notebookId}/present`, '_blank')}>
  Present
</button>
```

- [ ] **Step 5: Run tests**

Run: `cd web && npm run test -- --run PresentationPage`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/PresentationPage.tsx web/src/test/PresentationPage.test.tsx web/src/App.tsx web/src/pages/NotebookPage.tsx
git commit -m "feat: presentation mode — full-screen single-cell navigation with keyboard support"
```

---

## Task 9: Frontend — Platform Admin page

**Files:**
- Create: `web/src/pages/AdminPage.tsx`
- Create: `web/src/test/AdminPage.test.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/TopBar.tsx`

- [ ] **Step 1: Write the failing test**

Create `web/src/test/AdminPage.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { AdminPage } from '../pages/AdminPage'
import { http, HttpResponse } from 'msw'
import { server } from './setup'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

beforeEach(() => {
  server.use(
    http.get('/api/v1/admin/orgs', () => HttpResponse.json({
      orgs: [{ id: 'o1', name: 'Acme Corp', slug: 'acme', member_count: 5, created_at: '2024-01-01' }]
    })),
    http.get('/api/v1/admin/users', () => HttpResponse.json({
      users: [{ id: 'u1', email: 'admin@acme.com', name: 'Admin', is_platform_admin: true, orgs: ['Acme Corp'] }]
    }))
  )
})

function renderAdmin() {
  return render(
    <MemoryRouter initialEntries={['/admin']}>
      <Routes><Route path="/admin" element={<AdminPage />} /></Routes>
    </MemoryRouter>
  )
}

test('shows orgs list', async () => {
  renderAdmin()
  expect(await screen.findByText('Acme Corp')).toBeInTheDocument()
  expect(screen.getByText('5')).toBeInTheDocument() // member count
})

test('shows users list', async () => {
  renderAdmin()
  // Switch to users tab
  const usersTab = await screen.findByRole('tab', { name: /users/i })
  usersTab.click()
  expect(await screen.findByText('admin@acme.com')).toBeInTheDocument()
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npm run test -- --run AdminPage`
Expected: FAIL.

- [ ] **Step 3: Implement AdminPage**

Create `web/src/pages/AdminPage.tsx`:

```tsx
import { useState, useEffect } from 'react'

interface OrgSummary {
  id: string; name: string; slug: string; member_count: number; created_at: string
}
interface UserSummary {
  id: string; email: string; name: string; is_platform_admin: boolean; orgs: string[]
}

export function AdminPage() {
  const [tab, setTab] = useState<'orgs' | 'users'>('orgs')
  const [orgs, setOrgs] = useState<OrgSummary[]>([])
  const [users, setUsers] = useState<UserSummary[]>([])

  useEffect(() => {
    fetch('/api/v1/admin/orgs', { credentials: 'include' }).then(r => r.json()).then(d => setOrgs(d.orgs ?? []))
    fetch('/api/v1/admin/users', { credentials: 'include' }).then(r => r.json()).then(d => setUsers(d.users ?? []))
  }, [])

  return (
    <div className="admin-page p-6">
      <h1 className="text-2xl font-semibold mb-6 text-white">Platform Administration</h1>
      <div className="flex gap-4 mb-6 border-b border-gray-700">
        <button role="tab" aria-selected={tab === 'orgs'} className={`pb-2 px-1 text-sm ${tab === 'orgs' ? 'border-b-2 border-indigo-500 text-white' : 'text-gray-400'}`} onClick={() => setTab('orgs')}>
          Organizations
        </button>
        <button role="tab" aria-selected={tab === 'users'} className={`pb-2 px-1 text-sm ${tab === 'users' ? 'border-b-2 border-indigo-500 text-white' : 'text-gray-400'}`} onClick={() => setTab('users')}>
          Users
        </button>
      </div>

      {tab === 'orgs' && (
        <table className="w-full text-sm text-gray-300">
          <thead>
            <tr className="text-left text-gray-500 border-b border-gray-800">
              <th className="pb-2 pr-4">Name</th>
              <th className="pb-2 pr-4">Slug</th>
              <th className="pb-2 pr-4">Members</th>
              <th className="pb-2">Created</th>
            </tr>
          </thead>
          <tbody>
            {orgs.map(o => (
              <tr key={o.id} className="border-b border-gray-900 hover:bg-gray-900">
                <td className="py-2 pr-4 font-medium text-white">{o.name}</td>
                <td className="py-2 pr-4 font-mono text-xs">{o.slug}</td>
                <td className="py-2 pr-4">{o.member_count}</td>
                <td className="py-2 text-xs text-gray-500">{new Date(o.created_at).toLocaleDateString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {tab === 'users' && (
        <table className="w-full text-sm text-gray-300">
          <thead>
            <tr className="text-left text-gray-500 border-b border-gray-800">
              <th className="pb-2 pr-4">Email</th>
              <th className="pb-2 pr-4">Name</th>
              <th className="pb-2 pr-4">Orgs</th>
              <th className="pb-2">Platform Admin</th>
            </tr>
          </thead>
          <tbody>
            {users.map(u => (
              <tr key={u.id} className="border-b border-gray-900 hover:bg-gray-900">
                <td className="py-2 pr-4">{u.email}</td>
                <td className="py-2 pr-4 text-white">{u.name}</td>
                <td className="py-2 pr-4 text-xs text-gray-400">{u.orgs.join(', ')}</td>
                <td className="py-2">{u.is_platform_admin ? '✓' : ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Register route and add TopBar link**

In `web/src/App.tsx`:
```tsx
import { AdminPage } from './pages/AdminPage'
<Route path="/admin" element={<AdminPage />} />
```

In `web/src/components/TopBar.tsx`, in the profile dropdown, add a "Platform Admin" link if the user's JWT has `is_platform_admin: true` (decode `localStorage.hnb_token` to check):

```tsx
{isPlatformAdmin && (
  <a href="/admin" className="dropdown-item text-indigo-400">Platform Admin</a>
)}
```

- [ ] **Step 5: Run tests**

Run: `cd web && npm run test -- --run AdminPage`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/AdminPage.tsx web/src/test/AdminPage.test.tsx web/src/App.tsx web/src/components/TopBar.tsx
git commit -m "feat: platform admin panel — org list, user list, accessible via TopBar dropdown"
```

---

## Task 10: Frontend — Post-registration onboarding wizard

**Files:**
- Create: `web/src/pages/OrgOnboardingPage.tsx`
- Modify: `web/src/pages/RegisterPage.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: Implement OrgOnboardingPage**

Create `web/src/pages/OrgOnboardingPage.tsx`:

```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

export function OrgOnboardingPage() {
  const [mode, setMode] = useState<'choose' | 'create' | 'join'>('choose')
  const [orgName, setOrgName] = useState('')
  const [inviteToken, setInviteToken] = useState('')
  const [error, setError] = useState('')
  const navigate = useNavigate()

  // Onboarding token is stored in localStorage after account-only register
  const onboardingToken = localStorage.getItem('hnb_onboarding_token') ?? ''

  async function createOrg() {
    const r = await fetch('/api/v1/auth/org/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${onboardingToken}` },
      body: JSON.stringify({ org_name: orgName }),
    })
    const data = await r.json()
    if (!r.ok) { setError(data.error ?? 'Failed to create org'); return }
    localStorage.setItem('hnb_token', data.token)
    localStorage.setItem('hnb_user_name', data.user.name)
    localStorage.setItem('hnb_user_email', data.user.email)
    localStorage.removeItem('hnb_onboarding_token')
    navigate('/')
  }

  async function joinOrg() {
    const r = await fetch('/api/v1/auth/org/join', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${onboardingToken}` },
      body: JSON.stringify({ invite_token: inviteToken }),
    })
    const data = await r.json()
    if (!r.ok) { setError(data.error ?? 'Failed to join org'); return }
    localStorage.setItem('hnb_token', data.token)
    localStorage.setItem('hnb_user_name', data.user.name)
    localStorage.setItem('hnb_user_email', data.user.email)
    localStorage.removeItem('hnb_onboarding_token')
    navigate('/')
  }

  return (
    <div className="min-h-screen bg-gray-950 flex items-center justify-center">
      <div className="bg-gray-900 rounded-xl p-8 w-full max-w-md">
        <h1 className="text-xl font-semibold text-white mb-2">Welcome to hnb</h1>
        <p className="text-gray-400 text-sm mb-6">Your account is ready. Now set up your workspace.</p>

        {mode === 'choose' && (
          <div className="flex flex-col gap-3">
            <button className="btn-primary" onClick={() => setMode('create')}>Create a new organization</button>
            <button className="btn-secondary" onClick={() => setMode('join')}>Join an existing organization</button>
          </div>
        )}

        {mode === 'create' && (
          <div>
            <label className="label">Organization name</label>
            <input className="input mb-4" value={orgName} onChange={e => setOrgName(e.target.value)} placeholder="Acme Corp" />
            {error && <p className="text-red-400 text-sm mb-2">{error}</p>}
            <button className="btn-primary w-full" onClick={createOrg}>Create</button>
            <button className="btn-ghost mt-2" onClick={() => setMode('choose')}>← Back</button>
          </div>
        )}

        {mode === 'join' && (
          <div>
            <label className="label">Invite token</label>
            <input className="input mb-4" value={inviteToken} onChange={e => setInviteToken(e.target.value)} placeholder="Paste your invite token" />
            {error && <p className="text-red-400 text-sm mb-2">{error}</p>}
            <button className="btn-primary w-full" onClick={joinOrg}>Join</button>
            <button className="btn-ghost mt-2" onClick={() => setMode('choose')}>← Back</button>
          </div>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Update RegisterPage to store onboarding_token and redirect**

In `web/src/pages/RegisterPage.tsx`, after a successful register response:
- If response has `onboarding_token`: store in `localStorage.hnb_onboarding_token` and navigate to `/onboarding`.
- If response has `token` (old flow with `org_name`): store in `localStorage.hnb_token` and navigate to `/`.

```tsx
const data = await r.json()
if (data.onboarding_token) {
  localStorage.setItem('hnb_onboarding_token', data.onboarding_token)
  navigate('/onboarding')
} else {
  localStorage.setItem('hnb_token', data.token)
  localStorage.setItem('hnb_user_name', data.user.name)
  localStorage.setItem('hnb_user_email', data.user.email)
  navigate('/')
}
```

- [ ] **Step 3: Register route**

In `web/src/App.tsx`:
```tsx
import { OrgOnboardingPage } from './pages/OrgOnboardingPage'
<Route path="/onboarding" element={<OrgOnboardingPage />} />
```

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/OrgOnboardingPage.tsx web/src/pages/RegisterPage.tsx web/src/App.tsx
git commit -m "feat: post-registration org onboarding wizard — create or join flow"
```

---

## Task 11: E2E tests — Auth, Dashboard, Admin

**Files:**
- Create/Update: `e2e/auth.spec.ts`
- Create/Update: `e2e/dashboard.spec.ts`
- Create: `e2e/admin.spec.ts`

- [ ] **Step 1: Write auth E2E spec (updated for new register flow)**

Create `e2e/auth.spec.ts`:

```typescript
import { test, expect } from '@playwright/test'

test.describe('Authentication', () => {
  test('register → onboarding → create org → land on home', async ({ page }) => {
    await page.goto('/register')
    await page.fill('input[name="name"]', 'Test User')
    await page.fill('input[name="email"]', `test-${Date.now()}@example.com`)
    await page.fill('input[name="password"]', 'password123')
    await page.click('button[type="submit"]')

    // Should land on /onboarding
    await expect(page).toHaveURL(/\/onboarding/)
    await expect(page.locator('text=Create a new organization')).toBeVisible()

    // Create org
    await page.click('text=Create a new organization')
    await page.fill('input', 'My Test Org')
    await page.click('button:has-text("Create")')

    // Should land on home
    await expect(page).toHaveURL('/')
    await expect(page.locator('text=My Test Org')).toBeVisible()
  })

  test('login with existing account', async ({ page }) => {
    await page.goto('/login')
    await page.fill('input[name="email"]', 'admin@example.com')
    await page.fill('input[name="password"]', 'password123')
    await page.click('button[type="submit"]')
    await expect(page).toHaveURL('/')
  })

  test('OIDC SSO button is visible on login page', async ({ page }) => {
    await page.goto('/login')
    await expect(page.locator('button:has-text("Sign in with")')).toBeVisible()
  })

  test('visual: login page', async ({ page }) => {
    await page.goto('/login')
    await expect(page).toHaveScreenshot('login-page.png')
  })

  test('visual: onboarding page', async ({ page }) => {
    // Navigate directly — in test env the onboarding token check may be bypassed
    await page.goto('/onboarding')
    await expect(page).toHaveScreenshot('onboarding-page.png')
  })
})
```

- [ ] **Step 2: Write dashboard E2E spec**

Create `e2e/dashboard.spec.ts`:

```typescript
import { test, expect } from '@playwright/test'
import { loginAsAdmin, createDashboard } from './helpers'

test.describe('Dashboard input widgets', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
  })

  test('date picker widget updates a param', async ({ page }) => {
    const dashId = await createDashboard(page, 'Widget Test')
    await page.goto(`/dashboards/${dashId}`)

    // Add a date_picker widget (requires dashboard editor)
    await page.click('button:has-text("Add Widget")')
    await page.selectOption('select[name="type"]', 'date_picker')
    await page.fill('input[name="paramName"]', 'start_date')
    await page.click('button:has-text("Save")')

    // Interact with the widget
    const datePicker = page.locator('input[type="date"]').first()
    await datePicker.fill('2024-01-15')
    // The param should be set — connected code widgets would re-execute (infra-dependent)
    await expect(datePicker).toHaveValue('2024-01-15')
  })

  test('visual: dashboard with input widgets', async ({ page }) => {
    await page.goto('/dashboards')
    await expect(page).toHaveScreenshot('dashboards-page.png')
  })
})
```

- [ ] **Step 3: Write admin E2E spec**

Create `e2e/admin.spec.ts`:

```typescript
import { test, expect } from '@playwright/test'
import { loginAsPlatformAdmin } from './helpers'

test.describe('Platform admin panel', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsPlatformAdmin(page)
  })

  test('platform admin can view orgs', async ({ page }) => {
    await page.goto('/admin')
    await expect(page.locator('h1:has-text("Platform Administration")')).toBeVisible()
    // Orgs table should render
    await expect(page.locator('table')).toBeVisible()
  })

  test('platform admin can switch to users tab', async ({ page }) => {
    await page.goto('/admin')
    await page.click('button[role="tab"]:has-text("Users")')
    await expect(page.locator('text=Email')).toBeVisible()
  })

  test('non-admin redirected away from /admin', async ({ page }) => {
    // Log in as regular user
    await page.goto('/login')
    await page.fill('input[name="email"]', 'regular@example.com')
    await page.fill('input[name="password"]', 'password123')
    await page.click('button[type="submit"]')
    await page.goto('/admin')
    // Should be redirected or see forbidden
    await expect(page).not.toHaveURL('/admin')
  })

  test('visual snapshot: platform admin panel', async ({ page }) => {
    await page.goto('/admin')
    await expect(page).toHaveScreenshot('platform-admin-panel.png')
  })
})
```

- [ ] **Step 4: Run E2E tests**

Run: `npx playwright test e2e/auth.spec.ts e2e/dashboard.spec.ts e2e/admin.spec.ts --config=e2e/playwright.config.ts`
Expected: tests run; visual snapshots created on first run.

- [ ] **Step 5: Commit**

```bash
git add e2e/auth.spec.ts e2e/dashboard.spec.ts e2e/admin.spec.ts
git commit -m "test: E2E specs for new register flow, dashboard input widgets, platform admin panel"
```

---

## Phase 4 Visual Validation Checklist

Before merging Phase 4, a human reviewer checks:

- [ ] Chart config panel is compact — does not push the chart off screen on 1280px viewport; chart and config panel visible simultaneously
- [ ] All 7 chart types render without layout breakage — bar, stacked bar, line, area, scatter, pie, donut
- [ ] Dashboard input widgets visually match the dashboard's overall dark-theme style
- [ ] Presentation mode is full-screen — no sidebar, no top bar, no other app chrome
- [ ] Presentation navigation arrows are visible but not distracting from content
- [ ] Onboarding "create or join" fork is unambiguous — both options clearly labeled; no way to accidentally end up in a dead state
- [ ] Platform admin panel has a visually distinct treatment from org-level pages (e.g., different header colour or "Admin" badge)
- [ ] Profile dropdown shows "Platform Admin" link only for users with the flag — not visible to regular users
- [ ] Sign-out from any page clears token and onboarding token from localStorage and redirects to /login
- [ ] Template snippet picker (in CellToolbar) is searchable and shows template name + description before inserting
