package api

import (
	"net/http"
	"strings"

	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
)

type createGroupRequest struct {
	Name string `json:"name"`
}

// @Summary List groups
// @Description List all groups in the organization
// @Tags groups
// @Produce json
// @Success 200 {array} object
// @Failure 401 {object} map[string]string
// @Security BearerAuth
// @Router /groups [get]
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var query string
	var args []any

	if r.URL.Query().Get("member") == "me" {
		query = `SELECT g.id, g.org_id, g.name, g.created_at, COUNT(gm2.user_id) AS member_count
                 FROM groups g
                 JOIN group_members gm ON gm.group_id = g.id AND gm.user_id = $2
                 LEFT JOIN group_members gm2 ON gm2.group_id = g.id
                 WHERE g.org_id = $1
                 GROUP BY g.id
                 ORDER BY g.name`
		args = []any{claims.OrgID, claims.UserID}
	} else {
		query = `SELECT g.id, g.org_id, g.name, g.created_at, COUNT(gm.user_id) AS member_count
                 FROM groups g
                 LEFT JOIN group_members gm ON gm.group_id = g.id
                 WHERE g.org_id = $1
                 GROUP BY g.id
                 ORDER BY g.name`
		args = []any{claims.OrgID}
	}

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var groups []models.Group
	for rows.Next() {
		var g models.Group
		if err := rows.Scan(&g.ID, &g.OrgID, &g.Name, &g.CreatedAt, &g.MemberCount); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		groups = append(groups, g)
	}
	if groups == nil {
		groups = []models.Group{}
	}
	writeJSON(w, http.StatusOK, groups)
}

// @Summary Create a group
// @Description Create a new group
// @Tags groups
// @Accept json
// @Produce json
// @Param request body object true "Group details"
// @Success 201 {object} object
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /groups [post]
func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	var req createGroupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.EqualFold(req.Name, "everyone") {
		writeError(w, http.StatusBadRequest, "\"everyone\" is a reserved group name")
		return
	}

	var g models.Group
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO groups (org_id, name) VALUES ($1, $2)
		 RETURNING id, org_id, name, created_at`,
		claims.OrgID, req.Name,
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insert failed")
		return
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "group.create", ResourceType: "group", ResourceID: g.ID, ResourceName: g.Name,
	})
	writeJSON(w, http.StatusCreated, g)
}

// @Summary Update a group
// @Description Update a group's name
// @Tags groups
// @Accept json
// @Produce json
// @Param id path string true "Group ID"
// @Param request body object true "Group updates"
// @Success 200 {object} object
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /groups/{id} [put]
func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()

	var req createGroupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.EqualFold(req.Name, "everyone") {
		writeError(w, http.StatusBadRequest, "\"everyone\" is a reserved group name")
		return
	}

	var g models.Group
	err := s.db.Pool.QueryRow(ctx,
		`UPDATE groups SET name=$1 WHERE id=$2 AND org_id=$3
		 RETURNING id, org_id, name, created_at`,
		req.Name, groupID, claims.OrgID,
	).Scan(&g.ID, &g.OrgID, &g.Name, &g.CreatedAt)
	if err != nil {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "group.update", ResourceType: "group", ResourceID: groupID, ResourceName: req.Name,
	})
	writeJSON(w, http.StatusOK, g)
}

// @Summary Delete a group
// @Description Delete a group
// @Tags groups
// @Param id path string true "Group ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /groups/{id} [delete]
func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()

	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM groups WHERE id=$1 AND org_id=$2`,
		groupID, claims.OrgID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "group.delete", ResourceType: "group", ResourceID: groupID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary List group members
// @Description List all members of a group
// @Tags groups
// @Produce json
// @Param id path string true "Group ID"
// @Success 200 {array} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /groups/{id}/members [get]
func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()

	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND org_id=$2)`,
		groupID, claims.OrgID,
	).Scan(&exists)
	if err != nil || !exists {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT u.id, u.email, u.name
		 FROM group_members gm
		 JOIN users u ON u.id = gm.user_id
		 WHERE gm.group_id = $1
		 ORDER BY u.name`,
		groupID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var members []models.GroupMember
	for rows.Next() {
		var m models.GroupMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		members = append(members, m)
	}
	if members == nil {
		members = []models.GroupMember{}
	}
	writeJSON(w, http.StatusOK, members)
}

type addGroupMemberRequest struct {
	UserID string `json:"user_id"`
}

// @Summary Add group member
// @Description Add a user to a group
// @Tags groups
// @Accept json
// @Produce json
// @Param id path string true "Group ID"
// @Param request body object true "Member details"
// @Success 201
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /groups/{id}/members [post]
func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	ctx := r.Context()

	var req addGroupMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND org_id=$2)`,
		groupID, claims.OrgID,
	).Scan(&exists)
	if err != nil || !exists {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`,
		groupID, req.UserID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insert failed")
		return
	}

	var memberEmail string
	s.db.Pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, req.UserID).Scan(&memberEmail)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "group.member.add", ResourceType: "group", ResourceID: groupID,
		Metadata: map[string]any{
			"user_id": req.UserID,
			"email":   memberEmail,
		},
	})
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": req.UserID})
}

// @Summary Remove group member
// @Description Remove a user from a group
// @Tags groups
// @Param id path string true "Group ID"
// @Param user_id path string true "User ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /groups/{id}/members/{user_id} [delete]
func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	groupID := r.PathValue("id")
	userID := r.PathValue("user_id")
	ctx := r.Context()

	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM groups WHERE id=$1 AND org_id=$2)`,
		groupID, claims.OrgID,
	).Scan(&exists)
	if err != nil || !exists {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}

	_, err = s.db.Pool.Exec(ctx,
		`DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`,
		groupID, userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed")
		return
	}

	var memberEmail string
	s.db.Pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&memberEmail)

	s.audit.Log(ctx, audit.Entry{
		OrgID: claims.OrgID, UserID: claims.UserID,
		Action: "group.member.remove", ResourceType: "group", ResourceID: groupID,
		Metadata: map[string]any{
			"user_id": userID,
			"email":   memberEmail,
		},
	})
	w.WriteHeader(http.StatusNoContent)
}
