package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/auth"
	"github.com/heavenlabs/hnb/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	OrgName  string `json:"org_name"`
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

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
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

	// Legacy / backcompat flow: org_name provided → create user + org atomically
	if req.OrgName != "" {
		tx, err := s.db.Pool.Begin(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		defer tx.Rollback(ctx)

		var userID string
		err = tx.QueryRow(ctx,
			`INSERT INTO users (email, password_hash, name, email_verified, is_platform_admin)
			 VALUES ($1, $2, $3, FALSE, $4) RETURNING id`,
			req.Email, hash, req.Name, isPlatformAdmin,
		).Scan(&userID)
		if err != nil {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}

		var orgID string
		slug := slugify(req.OrgName)
		err = tx.QueryRow(ctx,
			`SELECT id FROM orgs WHERE slug = $1`,
			slug,
		).Scan(&orgID)
		if err == pgx.ErrNoRows {
			err = tx.QueryRow(ctx,
				`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
				req.OrgName, slug,
			).Scan(&orgID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create organization")
				return
			}
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to check organization")
			return
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin') ON CONFLICT DO NOTHING`,
			orgID, userID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add member")
			return
		}

		if err := createHomeFolder(ctx, tx, orgID, userID, req.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create home folder")
			return
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO groups (org_id, name) VALUES ($1, 'Everyone') ON CONFLICT DO NOTHING`,
			orgID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create everyone group")
			return
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id)
			 SELECT g.id, $1 FROM groups g WHERE g.org_id = $2 AND g.name = 'Everyone'
			 ON CONFLICT (group_id, user_id) DO NOTHING`,
			userID, orgID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add user to everyone group")
			return
		}

		if err := tx.Commit(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit")
			return
		}

		token, err := s.jwt.IssueFull(userID, orgID, "admin", isPlatformAdmin)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue token")
			return
		}

		s.audit.Log(ctx, audit.Entry{
			OrgID: orgID, UserID: userID,
			Action: "user.register", ResourceType: "user", ResourceID: userID,
		})

		resp := authResponse{}
		resp.Token = token
		resp.User.ID = userID
		resp.User.Email = req.Email
		resp.User.Name = req.Name
		resp.Org.ID = orgID
		resp.Org.Name = req.OrgName
		resp.Org.Role = "admin"

		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// New account-only flow: create user, no org yet
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

	// Check for domain-based auto-join
	domain := emailDomain(req.Email)
	var autoJoinOrgID, autoJoinRole string
	err = s.db.Pool.QueryRow(ctx,
		`SELECT org_id, 'viewer' FROM org_allowed_domains WHERE domain = $1 AND auto_join = true LIMIT 1`,
		domain,
	).Scan(&autoJoinOrgID, &autoJoinRole)
	if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}

	if autoJoinOrgID != "" {
		autoJoinTx, txErr := s.db.Pool.Begin(ctx)
		if txErr != nil {
			// Log the error but fall through to issue an onboarding token instead
			fmt.Printf("auto-join begin tx failed: %v\n", txErr)
			autoJoinOrgID = ""
		} else {
			if _, execErr := autoJoinTx.Exec(ctx,
				`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'viewer') ON CONFLICT DO NOTHING`,
				autoJoinOrgID, userID,
			); execErr != nil {
				autoJoinTx.Rollback(ctx)
				fmt.Printf("auto-join org_members insert failed: %v\n", execErr)
				autoJoinOrgID = ""
			} else {
				var autoJoinUserName string
				autoJoinTx.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, userID).Scan(&autoJoinUserName)
				if hmErr := createHomeFolder(ctx, autoJoinTx, autoJoinOrgID, userID, autoJoinUserName); hmErr != nil {
					autoJoinTx.Rollback(ctx)
					fmt.Printf("auto-join createHomeFolder failed: %v\n", hmErr)
					autoJoinOrgID = ""
				} else if _, gErr := autoJoinTx.Exec(ctx,
					`INSERT INTO group_members (group_id, user_id)
					 SELECT g.id, $1 FROM groups g WHERE g.org_id = $2 AND g.name = 'Everyone'
					 ON CONFLICT (group_id, user_id) DO NOTHING`,
					userID, autoJoinOrgID,
				); gErr != nil {
					autoJoinTx.Rollback(ctx)
					fmt.Printf("auto-join group_members insert failed: %v\n", gErr)
					autoJoinOrgID = ""
				} else if commitErr := autoJoinTx.Commit(ctx); commitErr != nil {
					fmt.Printf("auto-join commit failed: %v\n", commitErr)
					autoJoinOrgID = ""
				}
			}
		}
	}

	if autoJoinOrgID != "" {
		token, err := s.jwt.IssueFull(userID, autoJoinOrgID, "viewer", isPlatformAdmin)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue token")
			return
		}
		var orgName string
		s.db.Pool.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, autoJoinOrgID).Scan(&orgName)

		s.audit.Log(ctx, audit.Entry{
			OrgID: autoJoinOrgID, UserID: userID,
			Action: "user.register", ResourceType: "user", ResourceID: userID,
		})

		resp := authResponse{}
		resp.Token = token
		resp.User.ID = userID
		resp.User.Email = req.Email
		resp.User.Name = req.Name
		resp.Org.ID = autoJoinOrgID
		resp.Org.Name = orgName
		resp.Org.Role = "viewer"

		writeJSON(w, http.StatusCreated, resp)
		return
	}

	onboardingToken, err := s.jwt.IssueOnboarding(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
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
			log.Printf("warning: failed to create home folder for user %s: %v", userID, err)
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
	var folderID string
	err := q.QueryRow(ctx,
		`INSERT INTO folders (org_id, name, is_home, owner_id, created_by)
		 VALUES ($1, $2, true, $3, $3)
		 RETURNING id`,
		orgID, userName+"'s Home", userID,
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
