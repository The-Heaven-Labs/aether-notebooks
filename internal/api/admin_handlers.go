package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/audit"
)

// @Summary List all organizations
// @Description Returns all organizations with member counts
// @Tags admin
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/admin/orgs [get]
func (s *Server) handleAdminListOrgs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit := 50
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = o
	}
	search := q.Get("search")

	var total int
	countQuery := `SELECT COUNT(*) FROM orgs o`
	countArgs := []any{}
	if search != "" {
		countQuery += ` WHERE o.name ILIKE $1 OR o.slug ILIKE $1`
		countArgs = append(countArgs, "%"+search+"%")
	}
	s.db.Pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total)

	var whereClause string
	var args []any
	if search != "" {
		whereClause = ` WHERE o.name ILIKE $1 OR o.slug ILIKE $1`
		args = append(args, "%"+search+"%")
	}

	query := fmt.Sprintf(
		`SELECT o.id, o.name, o.slug, COUNT(m.user_id) as member_count, o.created_at
         FROM orgs o
         LEFT JOIN org_members m ON m.org_id = o.id
         %s
         GROUP BY o.id ORDER BY o.created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, len(args)+1, len(args)+2,
	)
	args = append(args, limit, offset)

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type orgSummary struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Slug        string    `json:"slug"`
		MemberCount int       `json:"member_count"`
		CreatedAt   time.Time `json:"created_at"`
	}
	var orgs []orgSummary
	for rows.Next() {
		var o orgSummary
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.MemberCount, &o.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		orgs = append(orgs, o)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query iteration failed")
		return
	}
	if orgs == nil {
		orgs = []orgSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"orgs": orgs, "total": total})
}

// @Summary List all users
// @Description Returns all users across all organizations
// @Tags admin
// @Produce json
// @Success 200 {object} map[string]any
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/admin/users [get]
func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	limit := 50
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o >= 0 {
		offset = o
	}
	search := q.Get("search")

	var total int
	countArgs := []any{}
	countQuery := `SELECT COUNT(*) FROM users u`
	if search != "" {
		countQuery += ` WHERE u.email ILIKE $1 OR u.name ILIKE $1`
		countArgs = append(countArgs, "%"+search+"%")
	}
	s.db.Pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total)

	var whereClause string
	var args []any
	if search != "" {
		whereClause = ` WHERE u.email ILIKE $1 OR u.name ILIKE $1`
		args = append(args, "%"+search+"%")
	}

	query := fmt.Sprintf(
		`SELECT u.id, u.email, u.name, u.is_platform_admin, u.created_at,
                COALESCE(array_agg(o.name ORDER BY o.name) FILTER (WHERE o.name IS NOT NULL), ARRAY[]::text[]) as orgs
         FROM users u
         LEFT JOIN org_members m ON m.user_id = u.id
         LEFT JOIN orgs o ON o.id = m.org_id
         %s
         GROUP BY u.id ORDER BY u.created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, len(args)+1, len(args)+2,
	)
	args = append(args, limit, offset)

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	type userSummary struct {
		ID              string    `json:"id"`
		Email           string    `json:"email"`
		Name            string    `json:"name"`
		IsPlatformAdmin bool      `json:"is_platform_admin"`
		CreatedAt       time.Time `json:"created_at"`
		Orgs            []string  `json:"orgs"`
	}
	var users []userSummary
	for rows.Next() {
		var u userSummary
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsPlatformAdmin, &u.CreatedAt, &u.Orgs); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		if u.Orgs == nil {
			u.Orgs = []string{}
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "query iteration failed")
		return
	}
	if users == nil {
		users = []userSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": users, "total": total})
}

// @Summary Update user
// @Description Update a user's platform admin status
// @Tags admin
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param request body object true "Update payload"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/admin/users/{id} [put]
func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	if targetID == "" {
		writeError(w, http.StatusBadRequest, "missing user id")
		return
	}

	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req struct {
		IsPlatformAdmin bool `json:"is_platform_admin"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Prevent self-demotion only. Self-promotion is a no-op for an existing platform admin
	// and is allowed to avoid a special case.
	if claims.UserID == targetID && !req.IsPlatformAdmin {
		writeError(w, http.StatusBadRequest, "cannot remove your own platform admin status")
		return
	}

	tag, err := s.db.Pool.Exec(r.Context(),
		`UPDATE users SET is_platform_admin=$1 WHERE id=$2`,
		req.IsPlatformAdmin, targetID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	s.audit.Log(r.Context(), audit.Entry{
		UserID: claims.UserID,
		Action: "user.platform_admin.update", ResourceType: "user", ResourceID: targetID,
		Metadata: map[string]any{"is_platform_admin": req.IsPlatformAdmin},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type adminCreateOrgRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// @Summary Create organization
// @Description Create a new organization as a platform admin
// @Tags admin
// @Accept json
// @Produce json
// @Param request body adminCreateOrgRequest true "Organization details"
// @Success 201 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/admin/orgs [post]
func (s *Server) handleAdminCreateOrg(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())

	var req adminCreateOrgRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}

	var orgID string
	err := s.db.Pool.QueryRow(r.Context(),
		`INSERT INTO orgs (name, slug) VALUES ($1, $2) RETURNING id`,
		req.Name, req.Slug,
	).Scan(&orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}

	// Add creating user as org admin
	if _, err := s.db.Pool.Exec(r.Context(),
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`, orgID, claims.UserID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add org admin")
		return
	}

	// Create home folder for the admin
	var uName string
	s.db.Pool.QueryRow(r.Context(), `SELECT name FROM users WHERE id = $1`, claims.UserID).Scan(&uName)
	createHomeFolder(r.Context(), s.db.Pool, orgID, claims.UserID, uName)

	// Ensure Everyone group
	s.db.Pool.Exec(r.Context(),
		`INSERT INTO groups (org_id, name) VALUES ($1, 'Everyone') ON CONFLICT DO NOTHING`, orgID,
	)
	s.db.Pool.Exec(r.Context(),
		`INSERT INTO group_members (group_id, user_id)
		 SELECT g.id, $1 FROM groups g WHERE g.org_id = $2 AND g.name = 'Everyone'
		 ON CONFLICT (group_id, user_id) DO NOTHING`, claims.UserID, orgID,
	)

	agent.SeedBuiltinTools(r.Context(), s.db.Pool, orgID)

	s.audit.Log(r.Context(), audit.Entry{
		OrgID: orgID, UserID: claims.UserID,
		Action: "org.create", ResourceType: "org", ResourceID: orgID,
	})

	writeJSON(w, http.StatusCreated, map[string]string{"id": orgID, "name": req.Name, "slug": req.Slug})
}

// @Summary Delete user
// @Description Permanently delete a user account. Removes the user from all orgs and
// @Description groups, deletes their home folders and personal data, and reassigns
// @Description ownership of org-shared resources (notebooks, dashboards, agents, etc.)
// @Description to the platform admin performing the deletion.
// @Tags admin
// @Produce json
// @Param id path string true "User ID"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/admin/users/{id} [delete]
func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	if targetID == "" {
		writeError(w, http.StatusBadRequest, "missing user id")
		return
	}

	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Prevent self-deletion (same guard as self-demotion in handleAdminUpdateUser).
	if claims.UserID == targetID {
		writeError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}

	ctx := r.Context()
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	var email string
	if err := tx.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, targetID).Scan(&email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	// Remove the user as an ACL subject (their home folder and resource grants).
	if _, err := tx.Exec(ctx,
		`DELETE FROM acl_entries WHERE subject_type = 'user' AND subject_id = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up ACLs")
		return
	}

	// Delete the user's folders (home folders + any folders they own). Child
	// folders are removed via parent_id ON DELETE CASCADE and resource
	// folder_id references fall back to NULL.
	if _, err := tx.Exec(ctx,
		`DELETE FROM folders WHERE owner_id = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete folders")
		return
	}

	// Reassign org-shared content to the platform admin so it survives the
	// user deletion. created_by is a historical reference with no permission
	// semantics, so reassignment is safe even across orgs.
	reassign := []string{
		`UPDATE folders SET created_by = $2 WHERE created_by = $1`,
		`UPDATE notebooks SET created_by = $2 WHERE created_by = $1`,
		`UPDATE dashboards SET created_by = $2 WHERE created_by = $1`,
		`UPDATE agents SET created_by = $2 WHERE created_by = $1`,
		`UPDATE model_configs SET created_by = $2 WHERE created_by = $1`,
		`UPDATE skills SET created_by = $2 WHERE created_by = $1`,
		`UPDATE mcp_servers SET created_by = $2 WHERE created_by = $1`,
		`UPDATE tools SET created_by = $2 WHERE created_by = $1`,
		`UPDATE agent_versions SET changed_by = $2 WHERE changed_by = $1`,
	}
	for _, q := range reassign {
		if _, err := tx.Exec(ctx, q, targetID, claims.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reassign resources")
			return
		}
	}

	// Null out the optional created_by references on org-shared resources.
	if _, err := tx.Exec(ctx,
		`UPDATE templates SET created_by = NULL WHERE created_by = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up templates")
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE connectors SET created_by = NULL WHERE created_by = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up connectors")
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE cell_versions SET created_by = NULL WHERE created_by = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up cell versions")
		return
	}
	if _, err := tx.Exec(ctx,
		`UPDATE motd_messages SET created_by = NULL WHERE created_by = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clean up MOTD messages")
		return
	}

	// Delete the user's personal/credential data.
	if _, err := tx.Exec(ctx,
		`DELETE FROM api_tokens WHERE user_id = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete API tokens")
		return
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM agent_sessions WHERE user_id = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent sessions")
		return
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM agent_stats_daily WHERE user_id = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete agent stats")
		return
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM attachments WHERE created_by = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete attachments")
		return
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM org_invites WHERE created_by = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete invites")
		return
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM org_invite_links WHERE created_by = $1`, targetID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete invite links")
		return
	}

	// Delete the user last. Remaining references (org_members, group_members,
	// sso_group_memberships, public_tokens) cascade; audit_logs set user_id NULL.
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, targetID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		UserID: claims.UserID,
		Action: "user.delete", ResourceType: "user", ResourceID: targetID,
		Metadata: map[string]any{"email": email},
	})
	w.WriteHeader(http.StatusNoContent)
}

// @Summary Delete organization
// @Description Permanently delete an organization and all of its data (notebooks,
// @Description folders, groups, members, connectors, tools, MCP servers, dashboards,
// @Description SSO bindings, audit logs, etc.). This is irreversible.
// @Tags admin
// @Produce json
// @Param id path string true "Organization ID"
// @Success 204
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Security BearerAuth
// @Router /api/v1/admin/orgs/{id} [delete]
func (s *Server) handleAdminDeleteOrg(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("id")
	if orgID == "" {
		writeError(w, http.StatusBadRequest, "missing org id")
		return
	}

	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	ctx := r.Context()
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(ctx)

	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, orgID).Scan(&name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "organization not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load organization")
		return
	}

	// These two tables reference orgs without ON DELETE CASCADE; clean them up
	// before deleting the org row. Everything else cascades.
	if _, err := tx.Exec(ctx, `DELETE FROM api_tokens WHERE org_id = $1`, orgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete API tokens")
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM motd_messages WHERE org_id = $1`, orgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete MOTD messages")
		return
	}

	if _, err := tx.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, orgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete organization")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	s.audit.Log(ctx, audit.Entry{
		UserID: claims.UserID,
		Action: "org.delete", ResourceType: "org", ResourceID: orgID,
		Metadata: map[string]any{"name": name},
	})
	w.WriteHeader(http.StatusNoContent)
}
