package api

import (
	"net/http"
	"time"

	"github.com/heavenlabs/hnb/internal/audit"
)

type memberResponse struct {
	UserID   string    `json:"user_id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// @Summary List members
// @Description List all members of the current organization
// @Tags members
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /members [get]
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	rows, err := s.db.Pool.Query(ctx,
		`SELECT u.id, u.email, u.name, om.role, om.created_at
		 FROM org_members om
		 JOIN users u ON u.id = om.user_id
		 WHERE om.org_id = $1
		 ORDER BY om.created_at ASC`,
		claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var members []memberResponse
	for rows.Next() {
		var m memberResponse
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		members = append(members, m)
	}
	if members == nil {
		members = []memberResponse{}
	}
	writeJSON(w, http.StatusOK, members)
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleUpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	targetUserID := r.PathValue("user_id")

	var req updateRoleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	validRoles := map[string]bool{"admin": true, "editor": true, "viewer": true}
	if !validRoles[req.Role] {
		writeError(w, http.StatusBadRequest, "role must be admin, editor, or viewer")
		return
	}
	if targetUserID == claims.UserID {
		writeError(w, http.StatusBadRequest, "cannot change your own role")
		return
	}

	ctx := r.Context()
	result, err := s.db.Pool.Exec(ctx,
		`UPDATE org_members SET role = $1 WHERE org_id = $2 AND user_id = $3`,
		req.Role, claims.OrgID, targetUserID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "member.update_role", ResourceType: "member", ResourceID: targetUserID,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	targetUserID := r.PathValue("user_id")

	if targetUserID == claims.UserID {
		writeError(w, http.StatusBadRequest, "cannot remove yourself")
		return
	}

	ctx := r.Context()
	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`,
		claims.OrgID, targetUserID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "member not found")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "member.remove", ResourceType: "member", ResourceID: targetUserID,
	})
	w.WriteHeader(http.StatusNoContent)
}

type inviteMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// @Summary Invite a member
// @Description Invite a new member to the organization
// @Tags members
// @Accept json
// @Produce json
// @Param request body object true "Invitation details"
// @Success 201
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /members/invite [post]
func (s *Server) handleInviteMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	var req inviteMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	validRoles := map[string]bool{"admin": true, "editor": true, "viewer": true}
	if !validRoles[req.Role] {
		writeError(w, http.StatusBadRequest, "role must be admin, editor, or viewer")
		return
	}

	ctx := r.Context()
	var userID string
	err := s.db.Pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, req.Email).Scan(&userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found — they must register first")
		return
	}

	// ON CONFLICT upserts role if member already exists (re-invite = role update).
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		 ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		claims.OrgID, userID, req.Role,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add member")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "member.invite", ResourceType: "member", ResourceID: userID,
	})
	w.WriteHeader(http.StatusNoContent)
}
