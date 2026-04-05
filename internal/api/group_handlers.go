package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/models"
)

type createGroupRequest struct {
	Name string `json:"name"`
}

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
	writeJSON(w, http.StatusCreated, g)
}

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
	writeJSON(w, http.StatusOK, g)
}

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
	w.WriteHeader(http.StatusNoContent)
}

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
	writeJSON(w, http.StatusCreated, map[string]string{"user_id": req.UserID})
}

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
	w.WriteHeader(http.StatusNoContent)
}
