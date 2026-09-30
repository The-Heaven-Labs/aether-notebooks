package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
)

const (
	// agentSessionListLimit keeps the historical agent-list page size.
	agentSessionListLimit = 50
	// sharedSessionListLimit bounds each shared-with-me candidate scan.
	sharedSessionListLimit = 100
)

// listSessionRow is one candidate session row shared by the listing handlers.
// CanEdit is filled by filterVisibleSessions for the returned rows.
type listSessionRow struct {
	ID           string
	AgentID      string
	NotebookID   *string
	UserID       string
	OwnerEmail   string
	MaxTurns     int
	Title        *string
	EndedAt      *time.Time
	CreatedAt    time.Time
	FirstMessage string
	MessageCount int
	Inherit      bool
	CanEdit      bool
}

// sessionListSelect is the shared SELECT prefix for session listing queries.
// Callers append their own joins, WHERE clause, and LIMIT; the scan order must
// stay in sync with scanListSessionRows.
const sessionListSelect = `
	SELECT s.id, s.agent_id, s.notebook_id, s.user_id, u.email, s.max_turns, s.ended_at, s.title, s.created_at,
		s.share_with_notebook_viewers,
		COALESCE(
			(SELECT content FROM agent_messages WHERE session_id = s.id AND role = 'user' ORDER BY created_at ASC LIMIT 1),
			''
		) AS first_message,
		COALESCE(
			(SELECT COUNT(*) FROM agent_messages WHERE session_id = s.id),
			0
		) AS message_count
	FROM agent_sessions s
	JOIN users u ON u.id = s.user_id
`

// scanListSessionRows drains a session listing query into candidate rows.
func scanListSessionRows(rows pgx.Rows) ([]listSessionRow, error) {
	defer rows.Close()
	out := make([]listSessionRow, 0)
	for rows.Next() {
		var row listSessionRow
		if err := rows.Scan(&row.ID, &row.AgentID, &row.NotebookID, &row.UserID, &row.OwnerEmail,
			&row.MaxTurns, &row.EndedAt, &row.Title, &row.CreatedAt, &row.Inherit,
			&row.FirstMessage, &row.MessageCount); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// sessionListResponse renders candidate rows for the wire. shared is true for
// every row the caller does not own; can_edit comes from the visibility filter.
func sessionListResponse(rows []listSessionRow, callerID string) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		notebookID := ""
		if row.NotebookID != nil {
			notebookID = *row.NotebookID
		}
		out = append(out, map[string]any{
			"id":                          row.ID,
			"agent_id":                    row.AgentID,
			"notebook_id":                 notebookID,
			"user_id":                     row.UserID,
			"owner_email":                 row.OwnerEmail,
			"max_turns":                   row.MaxTurns,
			"ended_at":                    row.EndedAt,
			"title":                       row.Title,
			"created_at":                  row.CreatedAt,
			"first_message":               row.FirstMessage,
			"message_count":               row.MessageCount,
			"shared":                      row.UserID != callerID,
			"can_edit":                    row.CanEdit,
			"share_with_notebook_viewers": row.Inherit,
		})
	}
	return out
}

// viewerGroupIDs returns the caller's group memberships plus their org's
// Everyone group, mirroring how checkPermission builds its subject set.
func (s *Server) viewerGroupIDs(ctx context.Context, userID, orgID string) ([]string, error) {
	groupIDs := []string{}
	rows, err := s.db.Pool.Query(ctx, `SELECT group_id FROM group_members WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("viewer group query: %w", err)
	}
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan viewer group: %w", err)
		}
		groupIDs = append(groupIDs, gid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("viewer group rows: %w", err)
	}

	everyoneRows, err := s.db.Pool.Query(ctx, `SELECT id FROM groups WHERE org_id = $1 AND name = 'Everyone'`, orgID)
	if err != nil {
		return nil, fmt.Errorf("everyone group query: %w", err)
	}
	for everyoneRows.Next() {
		var gid string
		if err := everyoneRows.Scan(&gid); err != nil {
			everyoneRows.Close()
			return nil, fmt.Errorf("scan everyone group: %w", err)
		}
		groupIDs = append(groupIDs, gid)
	}
	everyoneRows.Close()
	if err := everyoneRows.Err(); err != nil {
		return nil, fmt.Errorf("everyone group rows: %w", err)
	}
	return groupIDs, nil
}

// aclSubjectGrantsView reports whether one ACL entry matches the caller and
// grants view, applying the same action-implication rule as checkPermission.
func aclSubjectGrantsView(subjectType, subjectID string, actions []string, userID string, groupIDs []string) bool {
	matched := false
	switch subjectType {
	case "user":
		matched = subjectID == userID
	case "group":
		matched = slices.Contains(groupIDs, subjectID)
	case "org_role":
		matched = subjectID == "everyone"
	}
	if !matched {
		return false
	}
	for _, action := range actions {
		if action == "view" || isViewImpliedBy(action) {
			return true
		}
	}
	return false
}

// filterVisibleSessions filters a bounded candidate page (<= ~100 rows) to the
// sessions the caller can view, matching checkSessionPermission without its
// per-row N+1 cost: owner fallback, one batched direct-ACL lookup (user,
// groups, everyone), the admin-mode bypass, and notebook inheritance with the
// notebook check memoized per distinct notebook. It fills CanEdit: true only
// for owners and admin mode, since non-owner ACLs can never hold edit.
func (s *Server) filterVisibleSessions(ctx context.Context, userID, orgID, orgRole string, candidates []listSessionRow) ([]listSessionRow, error) {
	visible := make([]listSessionRow, 0, len(candidates))
	if len(candidates) == 0 {
		return visible, nil
	}

	// Admin-mode bypass assumes org-scoped candidates (all current callers);
	// unlike checkPermission's resourceOrgID equality, it applies no org check,
	// so a future non-org-scoped caller must add one.
	adminMode := orgRole == "admin" && adminModeFromContext(ctx)

	groupIDs, err := s.viewerGroupIDs(ctx, userID, orgID)
	if err != nil {
		return nil, fmt.Errorf("load viewer groups: %w", err)
	}

	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}

	directView := make(map[string]bool, len(candidates))
	rows, err := s.db.Pool.Query(ctx, `
		SELECT resource_id::text, subject_type, subject_id, actions
		FROM acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = ANY($1::uuid[]) AND org_id = $2
	`, ids, orgID)
	if err != nil {
		return nil, fmt.Errorf("load session ACLs: %w", err)
	}
	for rows.Next() {
		var sessionID, subjectType, subjectID string
		var actions []string
		if err := rows.Scan(&sessionID, &subjectType, &subjectID, &actions); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan session ACL: %w", err)
		}
		if directView[sessionID] {
			continue
		}
		if aclSubjectGrantsView(subjectType, subjectID, actions, userID, groupIDs) {
			directView[sessionID] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session ACLs: %w", err)
	}

	notebookView := make(map[string]bool)
	for _, candidate := range candidates {
		canView := adminMode || candidate.UserID == userID || directView[candidate.ID]
		if !canView && candidate.Inherit && candidate.NotebookID != nil {
			nbID := *candidate.NotebookID
			cached, ok := notebookView[nbID]
			if !ok {
				cached, err = s.checkPermission(ctx, userID, orgID, orgRole, "notebook", nbID, "view")
				if err != nil {
					return nil, fmt.Errorf("check notebook view: %w", err)
				}
				notebookView[nbID] = cached
			}
			canView = cached
		}
		if !canView {
			continue
		}
		candidate.CanEdit = adminMode || candidate.UserID == userID
		visible = append(visible, candidate)
	}
	return visible, nil
}

// mergeSessionRows dedupes two candidate pages by session ID, keeps the most
// recent rows first, and truncates to limit.
func mergeSessionRows(primary, secondary []listSessionRow, limit int) []listSessionRow {
	seen := make(map[string]bool, len(primary)+len(secondary))
	merged := make([]listSessionRow, 0, len(primary)+len(secondary))
	for _, page := range [][]listSessionRow{primary, secondary} {
		for _, row := range page {
			if seen[row.ID] {
				continue
			}
			seen[row.ID] = true
			merged = append(merged, row)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].CreatedAt.After(merged[j].CreatedAt)
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

// @Summary List sessions attached to a notebook
// @Description List sessions attached to a notebook that the caller can view
// @Tags agents
// @Produce json
// @Param id path string true "Notebook ID"
// @Success 200 {array} object
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /notebooks/{id}/sessions [get]
func (h *agentHandlers) handleListNotebookSessions(w http.ResponseWriter, r *http.Request) {
	notebookID := r.PathValue("id")
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	if !isValidUUID(notebookID) {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	var exists bool
	if err := h.server.db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM notebooks WHERE id = $1 AND org_id = $2)`,
		notebookID, claims.OrgID).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load notebook")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "notebook not found")
		return
	}

	allowed, err := h.server.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, "notebook", notebookID, "view")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}

	rows, err := h.server.db.Pool.Query(ctx, sessionListSelect+`
		JOIN agents a ON a.id = s.agent_id
		WHERE a.org_id = $1
			AND s.notebook_id = $2::uuid
			AND s.id IN (SELECT DISTINCT session_id FROM agent_messages)
		ORDER BY s.created_at DESC LIMIT $3
	`, claims.OrgID, notebookID, agentSessionListLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	candidates, err := scanListSessionRows(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	visible, err := h.server.filterVisibleSessions(ctx, claims.UserID, claims.OrgID, claims.Role, candidates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	writeJSON(w, http.StatusOK, sessionListResponse(visible, claims.UserID))
}

// @Summary List sessions shared with the caller
// @Description List non-owned sessions the caller can view: direct shares (user, group, Everyone) and notebook-viewer inheritance
// @Tags agents
// @Produce json
// @Success 200 {array} object
// @Security BearerAuth
// @Router /sessions/shared [get]
func (h *agentHandlers) handleListSharedSessions(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	groupIDs, err := h.server.viewerGroupIDs(ctx, claims.UserID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	directRows, err := h.querySessionRows(ctx, sessionListSelect+`
		JOIN agents a ON a.id = s.agent_id
		WHERE a.org_id = $1
			AND s.user_id::text <> $2
			AND s.id IN (SELECT DISTINCT session_id FROM agent_messages)
			AND EXISTS (
				SELECT 1 FROM acl_entries ae
				WHERE ae.org_id = $1
					AND ae.resource_type = 'agent_session'
					AND ae.resource_id = s.id
					AND (
						(ae.subject_type = 'user' AND ae.subject_id = $2)
						OR (ae.subject_type = 'group' AND ae.subject_id = ANY($3::text[]))
						OR (ae.subject_type = 'org_role' AND ae.subject_id = 'everyone')
					)
			)
		ORDER BY s.created_at DESC LIMIT $4
	`, claims.OrgID, claims.UserID, groupIDs, sharedSessionListLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	inheritRows, err := h.querySessionRows(ctx, sessionListSelect+`
		JOIN agents a ON a.id = s.agent_id
		WHERE a.org_id = $1
			AND s.user_id <> $2
			AND s.share_with_notebook_viewers = TRUE
			AND s.notebook_id IS NOT NULL
			AND s.id IN (SELECT DISTINCT session_id FROM agent_messages)
		ORDER BY s.created_at DESC LIMIT $3
	`, claims.OrgID, claims.UserID, sharedSessionListLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	candidates := mergeSessionRows(directRows, inheritRows, sharedSessionListLimit)
	visible, err := h.server.filterVisibleSessions(ctx, claims.UserID, claims.OrgID, claims.Role, candidates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	writeJSON(w, http.StatusOK, sessionListResponse(visible, claims.UserID))
}

// querySessionRows runs one session listing query into candidate rows.
func (h *agentHandlers) querySessionRows(ctx context.Context, query string, args ...any) ([]listSessionRow, error) {
	rows, err := h.server.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanListSessionRows(rows)
}

// @Summary Update a session
// @Description Update a session's title (owner/edit) or notebook-viewer inheritance flag (owner/share)
// @Tags agents
// @Accept json
// @Produce json
// @Param session_id path string true "Session ID"
// @Param request body object true "Session update"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /sessions/{session_id} [patch]
func (h *agentHandlers) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	if !isValidUUID(sessionID) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	var (
		notebookID *string
		inherit    bool
		title      *string
	)
	err := h.server.db.Pool.QueryRow(ctx, `
		SELECT notebook_id, share_with_notebook_viewers, title
		FROM agent_sessions WHERE id = $1
	`, sessionID).Scan(&notebookID, &inherit, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load session")
		return
	}

	var req struct {
		Title                    *string `json:"title"`
		ShareWithNotebookViewers *bool   `json:"share_with_notebook_viewers"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Title != nil && len(*req.Title) > 50 {
		writeError(w, http.StatusBadRequest, "title must be 50 characters or less")
		return
	}

	if req.Title != nil {
		allowed, err := h.server.checkSessionPermission(ctx, claims.UserID, claims.OrgID, claims.Role, sessionID, "edit")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
	}

	if req.ShareWithNotebookViewers != nil {
		allowed, err := h.server.checkSessionPermission(ctx, claims.UserID, claims.OrgID, claims.Role, sessionID, "share")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		if !allowed {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		if *req.ShareWithNotebookViewers && notebookID == nil {
			writeError(w, http.StatusBadRequest, "share_with_notebook_viewers requires a notebook")
			return
		}
	}

	if req.Title != nil {
		if err := h.server.agentEngine.SessionStore().UpdateTitle(ctx, sessionID, req.Title); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		title = req.Title
		h.server.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "agent_session.update_title", ResourceType: "agent_session", ResourceID: sessionID,
		})
	}

	if req.ShareWithNotebookViewers != nil {
		if _, err := h.server.db.Pool.Exec(ctx,
			`UPDATE agent_sessions SET share_with_notebook_viewers = $1 WHERE id = $2`,
			*req.ShareWithNotebookViewers, sessionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update sharing")
			return
		}
		inherit = *req.ShareWithNotebookViewers
		h.server.audit.Log(ctx, audit.Entry{
			OrgID: claims.OrgID, UserID: claims.UserID,
			Action: "agent_session.update_sharing", ResourceType: "agent_session", ResourceID: sessionID,
			Metadata: map[string]any{"share_with_notebook_viewers": inherit},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"title":                       title,
		"share_with_notebook_viewers": inherit,
	})
}
