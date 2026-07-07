package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/models"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	OrgID    string `json:"org_id,omitempty"` // optional, uses first org if empty
}

type authResponse struct {
	Token string `json:"token"`
	User  struct {
		ID              string `json:"id"`
		Email           string `json:"email"`
		Name            string `json:"name"`
		IsPlatformAdmin bool   `json:"is_platform_admin,omitempty"`
	} `json:"user"`
	Org struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"org"`
}

// @Summary Register a new user
// @Description Create a new user account
// @Tags auth
// @Accept json
// @Produce json
// @Param request body registerRequest true "Registration details"
// @Success 201 {object} authResponse
// @Failure 400 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Router /auth/register [post]
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.disableRegistration {
		writeError(w, http.StatusForbidden, "registration is disabled — contact your administrator")
		return
	}

	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email == "" || req.Password == "" || req.Name == "" {
		writeError(w, http.StatusBadRequest, "email, password, and name are required")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	ctx := r.Context()
	isPlatformAdmin := s.platformAdminEmail != "" && req.Email == s.platformAdminEmail

	var userID string
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, email_verified, is_platform_admin)
		 VALUES ($1, $2, $3, FALSE, $4) RETURNING id`,
		req.Email, hash, req.Name, isPlatformAdmin,
	).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusConflict, "email already registered")
		return
	}

	// Resolve target org: subdomain takes priority, then domain auto-join
	targetOrgID := OrgIDFromContext(ctx)
	if targetOrgID == "" {
		domain := emailDomain(req.Email)
		s.db.Pool.QueryRow(ctx,
			`SELECT org_id FROM org_allowed_domains WHERE domain = $1 AND auto_join = true LIMIT 1`,
			domain,
		).Scan(&targetOrgID)
	}

	if targetOrgID != "" {
		// Check if registration is enabled for this org
		var regEnabled bool
		s.db.Pool.QueryRow(ctx,
			`SELECT registration_enabled FROM orgs WHERE id=$1`, targetOrgID,
		).Scan(&regEnabled)
		if !regEnabled {
			writeError(w, http.StatusForbidden, "registration is disabled for this organization")
			return
		}

		tx, txErr := s.db.Pool.Begin(ctx)
		if txErr == nil {
			if _, execErr := tx.Exec(ctx,
				`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'viewer') ON CONFLICT DO NOTHING`,
				targetOrgID, userID,
			); execErr == nil {
				var userName string
				tx.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, userID).Scan(&userName)
				createHomeFolder(ctx, tx, targetOrgID, userID, userName)
				tx.Exec(ctx, `INSERT INTO groups (org_id, name) VALUES ($1, 'Everyone') ON CONFLICT DO NOTHING`, targetOrgID)
				tx.Exec(ctx, `INSERT INTO group_members (group_id, user_id) SELECT g.id, $1 FROM groups g WHERE g.org_id = $2 AND g.name = 'Everyone' ON CONFLICT (group_id, user_id) DO NOTHING`, userID, targetOrgID)
				tx.Commit(ctx)
			} else {
				tx.Rollback(ctx)
			}
		}

		var orgName string
		s.db.Pool.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, targetOrgID).Scan(&orgName)

		token, tErr := s.jwt.IssueFull(userID, targetOrgID, "non-admin", isPlatformAdmin)
		if tErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue token")
			return
		}

		s.audit.Log(ctx, audit.Entry{
			OrgID: targetOrgID, UserID: userID,
			Action: "user.register", ResourceType: "user", ResourceID: userID,
		})

		resp := authResponse{}
		resp.Token = token
		resp.User.ID = userID
		resp.User.Email = req.Email
		resp.User.Name = req.Name
		resp.User.IsPlatformAdmin = isPlatformAdmin
		resp.Org.ID = targetOrgID
		resp.Org.Name = orgName
		resp.Org.Role = "non-admin"
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// No org to join — issue onboarding token
	onboardingToken, err := s.jwt.IssueOnboarding(userID, isPlatformAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue onboarding token")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		UserID: userID,
		Action: "user.register", ResourceType: "user", ResourceID: userID,
	})

	writeJSON(w, http.StatusCreated, map[string]string{"onboarding_token": onboardingToken})
}

func emailDomain(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// @Summary Login with email and password
// @Description Authenticate a user and return a JWT token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body loginRequest true "Login credentials"
// @Success 200 {object} authResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /auth/login [post]
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()

	var userID, passwordHash, name string
	var isPlatformAdmin bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, password_hash, name, is_platform_admin FROM users WHERE email = $1`,
		req.Email,
	).Scan(&userID, &passwordHash, &name, &isPlatformAdmin)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if !auth.VerifyPassword(req.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Get org membership
	orgID, orgName, role, err := s.getUserOrg(ctx, userID, req.OrgID)
	if err != nil {
		writeError(w, http.StatusForbidden, "no organization membership found")
		return
	}

	// Auto-create home folder if it doesn't exist for this org membership
	var folderID string
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT id FROM folders WHERE owner_id = $1 AND is_home = true LIMIT 1`,
		userID,
	).Scan(&folderID); err == pgx.ErrNoRows {
		if err := createHomeFolder(ctx, s.db.Pool, orgID, userID, name); err != nil {
			// Non-fatal: log but don't block login
			slog.Warn("failed to create home folder", "user_id", userID, "error", err)
		}
	}

	token, err := s.jwt.IssueFull(userID, orgID, role, isPlatformAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: orgID, UserID: userID,
		Action: "user.login", ResourceType: "user", ResourceID: userID,
		Metadata: map[string]any{"ip": clientIP(r)},
	})

	resp := authResponse{}
	resp.Token = token
	resp.User.ID = userID
	resp.User.Email = req.Email
	resp.User.Name = name
	resp.User.IsPlatformAdmin = isPlatformAdmin
	resp.Org.ID = orgID
	resp.Org.Name = orgName
	resp.Org.Role = role

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) getUserOrg(ctx context.Context, userID, preferredOrgID string) (orgID, orgName, role string, err error) {
	if preferredOrgID != "" {
		err = s.db.Pool.QueryRow(ctx,
			`SELECT o.id, o.name, om.role FROM orgs o
			 JOIN org_members om ON om.org_id = o.id
			 WHERE om.user_id = $1 AND o.id = $2`,
			userID, preferredOrgID,
		).Scan(&orgID, &orgName, &role)
	} else {
		err = s.db.Pool.QueryRow(ctx,
			`SELECT o.id, o.name, om.role FROM orgs o
			 JOIN org_members om ON om.org_id = o.id
			 WHERE om.user_id = $1
			 ORDER BY om.created_at ASC LIMIT 1`,
			userID,
		).Scan(&orgID, &orgName, &role)
	}
	if err != nil {
		return "", "", "", fmt.Errorf("no membership: %w", err)
	}
	return
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx, allowing createHomeFolder
// to be called inside or outside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// createHomeFolder inserts a home folder for userID in orgID and seeds its ACL entry.
// userName is used to generate the folder name (e.g. "Alice's Home").
func createHomeFolder(ctx context.Context, q querier, orgID, userID, userName string) error {
	// Check if home folder already exists for this user in this org
	var existingID string
	err := q.QueryRow(ctx,
		`SELECT id FROM folders WHERE org_id = $1 AND owner_id = $2 AND is_home = true LIMIT 1`,
		orgID, userID,
	).Scan(&existingID)
	if err == nil {
		// Home folder already exists, return its ID
		return nil
	}
	if err != pgx.ErrNoRows {
		return fmt.Errorf("check existing home folder: %w", err)
	}

	// Get user email for folder name
	var userEmail string
	err = q.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&userEmail)
	if err != nil {
		return fmt.Errorf("get user email: %w", err)
	}

	// Create new home folder using email as the name
	var folderID string
	err = q.QueryRow(ctx,
		`INSERT INTO folders (org_id, name, is_home, owner_id, created_by)
		 VALUES ($1, $2, true, $3, $3)
		 RETURNING id`,
		orgID, userEmail, userID,
	).Scan(&folderID)
	if err != nil {
		return fmt.Errorf("create home folder: %w", err)
	}
	_, err = q.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, 'user', $3, ARRAY['view','create','edit','manage','delete'])`,
		orgID, folderID, userID,
	)
	if err != nil {
		return fmt.Errorf("seed home folder ACL: %w", err)
	}
	return nil
}

// clientIP returns the best-guess client IP from the request, respecting
// X-Forwarded-For and X-Real-IP headers set by reverse proxies.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For may be a comma-separated list; take the first entry.
		if idx := strings.IndexByte(xff, ','); idx >= 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func slugify(name string) string {
	slug := ""
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			slug += string(c)
		} else if c >= 'A' && c <= 'Z' {
			slug += string(c + 32)
		} else if c == ' ' {
			slug += "-"
		}
	}
	return slug
}

// @Summary Get current user
// @Description Get the currently authenticated user's information
// @Tags users
// @Produce json
// @Success 200 {object} models.User
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /users/me [get]
func (s *Server) handleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var u models.User
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, email, name, is_platform_admin, status, theme, created_at, updated_at FROM users WHERE id = $1`,
		claims.UserID,
	).Scan(&u.ID, &u.Email, &u.Name, &u.IsPlatformAdmin, &u.Status, &u.Theme, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch user")
		return
	}

	writeJSON(w, http.StatusOK, u)
}

// @Summary Update current user
// @Description Update the currently authenticated user's information
// @Tags users
// @Accept json
// @Produce json
// @Param request body object true "User updates"
// @Success 200 {object} models.User
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /users/me [put]
func (s *Server) handleUpdateCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	var req struct {
		Name   *string `json:"name,omitempty"`
		Status *string `json:"status,omitempty"`
		Theme  *string `json:"theme,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Theme != nil && *req.Theme != "light" && *req.Theme != "dark" {
		writeError(w, http.StatusBadRequest, "theme must be 'light' or 'dark'")
		return
	}

	setClauses := []string{}
	args := []any{}
	i := 1
	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name=$%d", i))
		args = append(args, *req.Name)
		i++
	}
	if req.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status=$%d", i))
		args = append(args, *req.Status)
		i++
	}
	if req.Theme != nil {
		setClauses = append(setClauses, fmt.Sprintf("theme=$%d", i))
		args = append(args, *req.Theme)
		i++
	}
	if len(setClauses) == 0 {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	setClauses = append(setClauses, "updated_at=NOW()")
	args = append(args, claims.UserID)

	query := fmt.Sprintf(
		`UPDATE users SET %s WHERE id=$%d
		 RETURNING id, email, name, email_verified, is_platform_admin, status, theme, created_at, updated_at`,
		strings.Join(setClauses, ", "), i,
	)
	var u models.User
	row := s.db.Pool.QueryRow(r.Context(), query, args...)
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.EmailVerified, &u.IsPlatformAdmin,
		&u.Status, &u.Theme, &u.CreatedAt, &u.UpdatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}
	writeJSON(w, http.StatusOK, u)
}
