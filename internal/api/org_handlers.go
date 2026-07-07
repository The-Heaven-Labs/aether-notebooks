package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/audit"
)

type orgCreateRequest struct {
	OrgName string `json:"org_name"`
}

type orgJoinRequest struct {
	InviteToken     string `json:"invite_token"`
	InviteLinkToken string `json:"invite_link_token"`
}

type createInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type createInviteLinkRequest struct {
	Role string `json:"role"`
}

// @Summary Create organization
// @Description Create a new organization (requires onboarding token)
// @Tags orgs
// @Accept json
// @Produce json
// @Param request body object true "Organization details"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /auth/org/create [post]
func (s *Server) handleOrgCreate(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil || claims.Role != "onboarding" {
		writeError(w, http.StatusForbidden, "onboarding token required")
		return
	}

	var req orgCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.OrgName == "" {
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
		writeError(w, http.StatusInternalServerError, "failed to create organization")
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

	var uName string
	if err := tx.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, claims.UserID).Scan(&uName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch user name")
		return
	}
	if err := createHomeFolder(ctx, tx, orgID, claims.UserID, uName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create home folder")
		return
	}

	// Ensure Everyone group exists for this org
	if _, err := tx.Exec(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'Everyone') ON CONFLICT DO NOTHING`,
		orgID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create Everyone group")
		return
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id)
		 SELECT g.id, $1 FROM groups g WHERE g.org_id = $2 AND g.name = 'Everyone'
		 ON CONFLICT (group_id, user_id) DO NOTHING`,
		claims.UserID, orgID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add user to Everyone group")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	agent.SeedBuiltinTools(ctx, s.db.Pool, orgID)

	var isPlatformAdmin bool
	s.db.Pool.QueryRow(ctx, `SELECT is_platform_admin FROM users WHERE id = $1`, claims.UserID).Scan(&isPlatformAdmin)
	token, err := s.jwt.IssueFull(claims.UserID, orgID, "admin", isPlatformAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	// Fetch user info for response
	var email, name string
	s.db.Pool.QueryRow(ctx, `SELECT email, name FROM users WHERE id = $1`, claims.UserID).Scan(&email, &name)

	s.audit.Log(ctx, audit.Entry{
		OrgID: orgID, UserID: claims.UserID,
		Action: "org.create", ResourceType: "org", ResourceID: orgID,
	})

	resp := authResponse{}
	resp.Token = token
	resp.User.ID = claims.UserID
	resp.User.Email = email
	resp.User.Name = name
	resp.User.IsPlatformAdmin = isPlatformAdmin
	resp.Org.ID = orgID
	resp.Org.Name = req.OrgName
	resp.Org.Role = "admin"

	writeJSON(w, http.StatusCreated, resp)
}

// @Summary Join organization
// @Description Join an organization via invite token or invite link token (requires onboarding token)
// @Tags orgs
// @Accept json
// @Produce json
// @Param request body object true "Invite details"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Security BearerAuth
// @Router /auth/org/join [post]
func (s *Server) handleOrgJoin(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil || claims.Role != "onboarding" {
		writeError(w, http.StatusForbidden, "onboarding token required")
		return
	}

	var req orgJoinRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	var orgID, role string

	var inviteMeta map[string]any

	if req.InviteToken != "" {
		// Validate invite from org_invites (Fix 3: also fetch email column)
		var inviteEmail, inviteID string
		err := s.db.Pool.QueryRow(ctx,
			`SELECT org_id, role, email, id FROM org_invites WHERE token = $1 AND accepted_at IS NULL AND expires_at > now()`,
			req.InviteToken,
		).Scan(&orgID, &role, &inviteEmail, &inviteID)
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusBadRequest, "invalid, expired, or already used invite token")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}

		// Fix 3: Look up the user's email and compare to the invite's email
		var userEmail string
		err = s.db.Pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, claims.UserID).Scan(&userEmail)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database error")
			return
		}
		if userEmail != inviteEmail {
			writeError(w, http.StatusForbidden, "this invite was issued to a different email address")
			return
		}

		// Fix 4: Atomically mark invite as accepted; detect TOCTOU race via RowsAffected
		tag, err := s.db.Pool.Exec(ctx,
			`UPDATE org_invites SET accepted_at = now() WHERE token = $1 AND accepted_at IS NULL`,
			req.InviteToken,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to accept invite")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusConflict, "invite already used")
			return
		}

		inviteMeta = map[string]any{
			"invite_id": inviteID,
			"role":      role,
			"email":     inviteEmail,
		}
	} else if req.InviteLinkToken != "" {
		// Validate invite link from org_invite_links
		var linkID string
		err := s.db.Pool.QueryRow(ctx,
			`SELECT org_id, role, id FROM org_invite_links WHERE token = $1`,
			req.InviteLinkToken,
		).Scan(&orgID, &role, &linkID)
		if err != nil {
			writeError(w, http.StatusNotFound, "invalid invite link token")
			return
		}

		inviteMeta = map[string]any{
			"invite_link_id": linkID,
			"role":           role,
		}
	} else {
		writeError(w, http.StatusBadRequest, "invite_token or invite_link_token is required")
		return
	}

	// Add user to org and create home folder in a single transaction
	joinTx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer joinTx.Rollback(ctx)

	_, err = joinTx.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		orgID, claims.UserID, role,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join organization")
		return
	}

	var joinUserName string
	joinTx.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, claims.UserID).Scan(&joinUserName)
	if err := createHomeFolder(ctx, joinTx, orgID, claims.UserID, joinUserName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create home folder")
		return
	}

	// Ensure Everyone group exists and add user
	if _, err := joinTx.Exec(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, 'Everyone') ON CONFLICT DO NOTHING`,
		orgID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create Everyone group")
		return
	}

	if _, err := joinTx.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id)
		 SELECT g.id, $1 FROM groups g WHERE g.org_id = $2 AND g.name = 'Everyone'
		 ON CONFLICT (group_id, user_id) DO NOTHING`,
		claims.UserID, orgID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add user to Everyone group")
		return
	}

	if err := joinTx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	var isPlatformAdmin bool
	s.db.Pool.QueryRow(ctx, `SELECT is_platform_admin FROM users WHERE id = $1`, claims.UserID).Scan(&isPlatformAdmin)
	token, err := s.jwt.IssueFull(claims.UserID, orgID, role, isPlatformAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	// Fetch user and org info for response
	var email, userName, orgName string
	s.db.Pool.QueryRow(ctx, `SELECT email, name FROM users WHERE id = $1`, claims.UserID).Scan(&email, &userName)
	s.db.Pool.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, orgID).Scan(&orgName)

	s.audit.Log(ctx, audit.Entry{
		OrgID: orgID, UserID: claims.UserID,
		Action: "org.join", ResourceType: "org", ResourceID: orgID,
		Metadata: inviteMeta,
	})

	resp := authResponse{}
	resp.Token = token
	resp.User.ID = claims.UserID
	resp.User.Email = email
	resp.User.Name = userName
	resp.User.IsPlatformAdmin = isPlatformAdmin
	resp.Org.ID = orgID
	resp.Org.Name = orgName
	resp.Org.Role = role

	writeJSON(w, http.StatusOK, resp)
}

// @Summary Create email invite
// @Description Create an email invite for a specific user (org admin only)
// @Tags orgs
// @Accept json
// @Produce json
// @Param request body object true "Invite details"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /members/invite [post]
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var req createInviteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if req.Role == "" {
		req.Role = "admin"
	}
	if req.Role != "admin" {
		writeError(w, http.StatusBadRequest, "role must be admin")
		return
	}

	token, err := generateSecureToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	ctx := r.Context()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	var inviteID string
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO org_invites (org_id, email, role, token, expires_at, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		claims.OrgID, req.Email, req.Role, token, expiresAt, claims.UserID,
	).Scan(&inviteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invite")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "invite.create", ResourceType: "invite", ResourceID: inviteID,
		Metadata: map[string]any{
			"email": req.Email,
			"role":  req.Role,
		},
	})

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":    inviteID,
		"token": token,
	})
}

// @Summary Create invite link
// @Description Create a shareable invite link (org admin only)
// @Tags orgs
// @Accept json
// @Produce json
// @Param request body object true "Invite link details"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /members/invite-link [post]
func (s *Server) handleCreateInviteLink(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var invitationsEnabled bool
	s.db.Pool.QueryRow(r.Context(),
		`SELECT invitations_enabled FROM orgs WHERE id=$1`, claims.OrgID,
	).Scan(&invitationsEnabled)
	if !invitationsEnabled {
		writeError(w, http.StatusForbidden, "invitations are disabled for this organization")
		return
	}

	var req createInviteLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Role == "" {
		req.Role = "non-admin"
	}
	if req.Role != "admin" && req.Role != "non-admin" {
		writeError(w, http.StatusBadRequest, "role must be admin or non-admin")
		return
	}

	token, err := generateSecureToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	ctx := r.Context()

	var linkID string
	err = s.db.Pool.QueryRow(ctx,
		`INSERT INTO org_invite_links (org_id, role, token, created_by)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		claims.OrgID, req.Role, token, claims.UserID,
	).Scan(&linkID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invite link")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "invite_link.create", ResourceType: "invite_link", ResourceID: linkID,
		Metadata: map[string]any{
			"role": req.Role,
		},
	})

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":    linkID,
		"token": token,
		"url":   fmt.Sprintf("%s/join?token=%s", s.frontendURL, token),
	})
}

// @Summary Get org sharing settings
// @Description Get the public sharing settings for the organization
// @Tags orgs
// @Produce json
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /org/sharing [get]
func (s *Server) handleGetOrgSharingSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var enabled bool
	err := s.db.Pool.QueryRow(r.Context(),
		`SELECT public_sharing_enabled FROM orgs WHERE id=$1`, claims.OrgID,
	).Scan(&enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"public_sharing_enabled": enabled})
}

// @Summary Update org sharing settings
// @Description Update the public sharing settings for the organization
// @Tags orgs
// @Accept json
// @Produce json
// @Param request body object true "Sharing settings"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /org/sharing [put]
func (s *Server) handleUpdateOrgSharingSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Enabled bool `json:"public_sharing_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	_, err := s.db.Pool.Exec(r.Context(),
		`UPDATE orgs SET public_sharing_enabled=$1 WHERE id=$2`,
		req.Enabled, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"public_sharing_enabled": req.Enabled})
}

// @Summary Get org registration settings
// @Description Get the registration settings for the organization
// @Tags orgs
// @Produce json
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /org/registration [get]
func (s *Server) handleGetOrgRegistrationSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var enabled bool
	err := s.db.Pool.QueryRow(r.Context(),
		`SELECT registration_enabled FROM orgs WHERE id=$1`, claims.OrgID,
	).Scan(&enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"registration_enabled": enabled})
}

// @Summary Update org registration settings
// @Description Update the registration settings for the organization
// @Tags orgs
// @Accept json
// @Produce json
// @Param request body object true "Registration settings"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /org/registration [put]
func (s *Server) handleUpdateOrgRegistrationSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Enabled bool `json:"registration_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	_, err := s.db.Pool.Exec(r.Context(),
		`UPDATE orgs SET registration_enabled=$1 WHERE id=$2`,
		req.Enabled, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"registration_enabled": req.Enabled})
}

// @Summary Get org invitation settings
// @Description Get the invitation settings for the organization
// @Tags orgs
// @Produce json
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /org/invitations [get]
func (s *Server) handleGetOrgInvitationSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var enabled bool
	err := s.db.Pool.QueryRow(r.Context(),
		`SELECT invitations_enabled FROM orgs WHERE id=$1`, claims.OrgID,
	).Scan(&enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"invitations_enabled": enabled})
}

// @Summary Update org invitation settings
// @Description Update the invitation settings for the organization
// @Tags orgs
// @Accept json
// @Produce json
// @Param request body object true "Invitation settings"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /org/invitations [put]
func (s *Server) handleUpdateOrgInvitationSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Enabled bool `json:"invitations_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	_, err := s.db.Pool.Exec(r.Context(),
		`UPDATE orgs SET invitations_enabled=$1 WHERE id=$2`,
		req.Enabled, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"invitations_enabled": req.Enabled})
}

// @Summary Get org data export settings
// @Description Get whether CSV/JSON data export is enabled for the organization
// @Tags orgs
// @Produce json
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /org/data-export [get]
func (s *Server) handleGetOrgDataExportSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var enabled bool
	err := s.db.Pool.QueryRow(r.Context(),
		`SELECT data_export_enabled FROM orgs WHERE id=$1`, claims.OrgID,
	).Scan(&enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"data_export_enabled": enabled})
}

// @Summary Update org data export settings
// @Description Update whether CSV/JSON data export is enabled for the organization
// @Tags orgs
// @Accept json
// @Produce json
// @Param request body object true "Data export settings"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /org/data-export [put]
func (s *Server) handleUpdateOrgDataExportSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req struct {
		Enabled bool `json:"data_export_enabled"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	_, err := s.db.Pool.Exec(r.Context(),
		`UPDATE orgs SET data_export_enabled=$1 WHERE id=$2`,
		req.Enabled, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"data_export_enabled": req.Enabled})
}

// generateSecureToken returns a hex-encoded random token of the given byte length.
func generateSecureToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
