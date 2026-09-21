package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
)

type addPendingGroupMembersRequest struct {
	Emails []string `json:"emails"`
}

// pendingMemberSkip reports an email that was not staged, with the reason the
// UI can surface to the admin.
type pendingMemberSkip struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// pendingGroupMember is a staged membership row returned by the list endpoint.
type pendingGroupMember struct {
	Email     string    `json:"email"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// @Summary Add pending group members
// @Description Stage group memberships by email for people who do not have an
// @Description account yet. Existing org members are added directly; emails that
// @Description already belong to a member of the group are skipped.
// @Tags groups
// @Accept json
// @Produce json
// @Param id path string true "Group ID"
// @Param request body object true "Emails to stage"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /groups/{id}/pending-members [post]
func (s *Server) handleAddPendingGroupMembers(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()

	groupName, err := s.lookupGroupName(ctx, groupID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	if strings.EqualFold(groupName, "everyone") {
		writeError(w, http.StatusBadRequest, "cannot pre-provision members for the \"Everyone\" group — all org members are automatically included")
		return
	}

	var req addPendingGroupMembersRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Emails) == 0 {
		writeError(w, http.StatusBadRequest, "emails is required")
		return
	}

	added := 0
	directAdd := false
	skipped := []pendingMemberSkip{}
	seen := make(map[string]bool, len(req.Emails))

	for _, raw := range req.Emails {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" || !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
			skipped = append(skipped, pendingMemberSkip{Email: strings.TrimSpace(raw), Reason: "invalid"})
			continue
		}
		if seen[email] {
			skipped = append(skipped, pendingMemberSkip{Email: email, Reason: "already_pending"})
			continue
		}
		seen[email] = true

		// Existing org member? Add them directly rather than staging a row that
		// would never be materialized (they have already appeared in the org).
		var userID string
		err := s.db.Pool.QueryRow(ctx,
			`SELECT u.id FROM users u
			 JOIN org_members om ON om.user_id = u.id AND om.org_id = $1
			 WHERE lower(u.email) = $2
			 LIMIT 1`, claims.OrgID, email).Scan(&userID)
		if err == nil {
			var alreadyInGroup bool
			s.db.Pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)`,
				groupID, userID).Scan(&alreadyInGroup)
			if alreadyInGroup {
				skipped = append(skipped, pendingMemberSkip{Email: email, Reason: "already_member"})
				continue
			}
			if _, err := s.db.Pool.Exec(ctx,
				`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				groupID, userID); err != nil {
				writeError(w, http.StatusInternalServerError, "insert failed")
				return
			}
			directAdd = true
			added++
			s.audit.Log(ctx, audit.Entry{
				OrgID: claims.OrgID, UserID: claims.UserID,
				Action: "group.member.add", ResourceType: "group", ResourceID: groupID, ResourceName: groupName,
				Metadata: map[string]any{"user_id": userID, "email": email},
			})
			continue
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}

		tag, err := s.db.Pool.Exec(ctx,
			`INSERT INTO pending_group_members (org_id, group_id, email, created_by)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (group_id, lower(email)) DO NOTHING`,
			claims.OrgID, groupID, email, claims.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "insert failed")
			return
		}
		if tag.RowsAffected() == 0 {
			skipped = append(skipped, pendingMemberSkip{Email: email, Reason: "already_pending"})
			continue
		}
		added++
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "group.pending_member.add", ResourceType: "group", ResourceID: groupID, ResourceName: groupName,
			Metadata: map[string]any{"email": email, "group_id": groupID, "group_name": groupName},
		})
	}

	// Direct adds commit as they happen; enqueue once for the whole request
	// (the sync worker coalesces anyway, but one call keeps the trigger cheap
	// when an admin pastes a long list).
	if directAdd {
		s.enqueueWarehouseSyncForGroup(ctx, groupID)
	}

	writeJSON(w, http.StatusOK, map[string]any{"added": added, "skipped": skipped})
}

// @Summary List pending group members
// @Description List memberships staged by email that have not yet been
// @Description materialized (the person has no account in this org yet).
// @Tags groups
// @Produce json
// @Param id path string true "Group ID"
// @Success 200 {array} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /groups/{id}/pending-members [get]
func (s *Server) handleListPendingGroupMembers(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()

	if _, err := s.lookupGroupName(ctx, groupID, claims.OrgID); err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT email, COALESCE(created_by::text, ''), created_at
		 FROM pending_group_members
		 WHERE group_id = $1 AND org_id = $2
		 ORDER BY email`,
		groupID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	pending := []pendingGroupMember{}
	for rows.Next() {
		var p pendingGroupMember
		if err := rows.Scan(&p.Email, &p.CreatedBy, &p.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		pending = append(pending, p)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, pending)
}

// @Summary Remove pending group member
// @Description Remove a membership staged by email (case-insensitive match).
// @Tags groups
// @Param id path string true "Group ID"
// @Param email path string true "Email"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /groups/{id}/pending-members/{email} [delete]
func (s *Server) handleRemovePendingGroupMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	email := r.PathValue("email")
	ctx := r.Context()

	groupName, err := s.lookupGroupName(ctx, groupID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM pending_group_members
		 WHERE group_id = $1 AND org_id = $2 AND lower(email) = lower($3)`,
		groupID, claims.OrgID, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if tag.RowsAffected() > 0 {
		s.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "group.pending_member.remove", ResourceType: "group", ResourceID: groupID, ResourceName: groupName,
			Metadata: map[string]any{"email": strings.ToLower(email), "group_id": groupID, "group_name": groupName},
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// lookupGroupName returns a group's name when it belongs to orgID.
func (s *Server) lookupGroupName(ctx context.Context, groupID, orgID string) (string, error) {
	var name string
	err := s.db.Pool.QueryRow(ctx,
		`SELECT name FROM groups WHERE id = $1 AND org_id = $2`,
		groupID, orgID,
	).Scan(&name)
	return name, err
}
