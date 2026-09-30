// Package api provides HTTP handlers, middleware, and routing for the Aether API server.
// Handlers are organized by resource type (notebooks, cells, connectors, etc.)
// and use net/http ServeMux with no external framework.
package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/models"
)

type aclEntryInput struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   string   `json:"subject_id"`
	Actions     []string `json:"actions"`
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// @Summary Get ACL
// @Description Get access control list for a resource
// @Tags permissions
// @Produce json
// @Param resource_type path string true "Resource type"
// @Param resource_id path string true "Resource ID"
// @Success 200 {array} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /acl/{resource_type}/{resource_id} [get]
func (s *Server) handleGetACL(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	resourceType := r.PathValue("resource_type")
	resourceID := r.PathValue("resource_id")
	ctx := r.Context()

	if resourceType == "agent_session" {
		// Sessions have no unconditional org-admin bypass: an org admin needs
		// admin mode, exactly like every other session route.
		allowed, err := s.checkSessionPermission(ctx, claims.UserID, claims.OrgID, claims.Role, resourceID, "view")
		if err != nil || !allowed {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	} else if claims.Role != "admin" {
		allowed, err := s.checkPermission(ctx, claims.UserID, claims.OrgID, claims.Role, resourceType, resourceID, "view")
		if err != nil || !allowed {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, org_id, resource_type, resource_id::text, subject_type, subject_id, actions, created_at
         FROM acl_entries
         WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3
         ORDER BY subject_type, subject_id`,
		resourceType, resourceID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	entries, err := scanACLEntries(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}
	if entries == nil {
		entries = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

// scanACLEntries drains an acl_entries query into models.ACLEntry rows.
func scanACLEntries(rows pgx.Rows) ([]models.ACLEntry, error) {
	defer rows.Close()
	var entries []models.ACLEntry
	for rows.Next() {
		var e models.ACLEntry
		if err := rows.Scan(&e.ID, &e.OrgID, &e.ResourceType, &e.ResourceID,
			&e.SubjectType, &e.SubjectID, &e.Actions, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// aclAuditDiff compares previous and replacement ACL entries by subject and
// returns the acl.revoked / acl.updated / acl.granted events describing the
// change.
func aclAuditDiff(userID, orgID, resourceType, resourceID string, oldEntries, newEntries []models.ACLEntry) []audit.Entry {
	var auditEvents []audit.Entry
	for _, old := range oldEntries {
		found := false
		for _, newEntry := range newEntries {
			if old.SubjectType == newEntry.SubjectType && old.SubjectID == newEntry.SubjectID {
				found = true
				if !slicesEqual(old.Actions, newEntry.Actions) {
					auditEvents = append(auditEvents, audit.Entry{
						OrgID:        orgID,
						UserID:       userID,
						Action:       "acl.updated",
						ResourceType: resourceType,
						ResourceID:   resourceID,
						Metadata: map[string]any{
							"subject_type": newEntry.SubjectType,
							"subject_id":   newEntry.SubjectID,
							"old_actions":  old.Actions,
							"new_actions":  newEntry.Actions,
						},
					})
				}
				break
			}
		}
		if !found {
			auditEvents = append(auditEvents, audit.Entry{
				OrgID:        orgID,
				UserID:       userID,
				Action:       "acl.revoked",
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Metadata: map[string]any{
					"subject_type": old.SubjectType,
					"subject_id":   old.SubjectID,
					"actions":      old.Actions,
				},
			})
		}
	}
	for _, newEntry := range newEntries {
		found := false
		for _, old := range oldEntries {
			if old.SubjectType == newEntry.SubjectType && old.SubjectID == newEntry.SubjectID {
				found = true
				break
			}
		}
		if !found {
			auditEvents = append(auditEvents, audit.Entry{
				OrgID:        orgID,
				UserID:       userID,
				Action:       "acl.granted",
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Metadata: map[string]any{
					"subject_type": newEntry.SubjectType,
					"subject_id":   newEntry.SubjectID,
					"actions":      newEntry.Actions,
				},
			})
		}
	}
	return auditEvents
}

// @Summary Update ACL
// @Description Update access control list for a resource
// @Tags permissions
// @Accept json
// @Produce json
// @Param resource_type path string true "Resource type"
// @Param resource_id path string true "Resource ID"
// @Param request body object true "ACL entries"
// @Success 200
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /acl/{resource_type}/{resource_id} [put]
func (s *Server) handlePutACL(w http.ResponseWriter, r *http.Request) {
	resourceType := r.PathValue("resource_type")
	resourceID := r.PathValue("resource_id")

	if resourceType == "agent_session" {
		s.handlePutSessionACL(w, r, resourceID)
		return
	}

	claims := ClaimsFromContext(r.Context())

	// Org admins always have ACL management rights; others need "manage" (folders) or "share".
	if claims.Role != "admin" {
		requiredAction := "share"
		if resourceType == "folder" {
			requiredAction = "manage"
		}
		allowed, err := s.checkPermission(r.Context(), claims.UserID, claims.OrgID, claims.Role, resourceType, resourceID, requiredAction)
		if err != nil || !allowed {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	var req struct {
		Entries []aclEntryInput `json:"entries"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := r.Context()
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(ctx)

	// Capture existing entries before deleting (for audit comparison)
	existingRows, err := tx.Query(ctx,
		`SELECT subject_type, subject_id, actions FROM acl_entries
         WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, claims.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query existing ACL")
		return
	}
	var oldEntries []models.ACLEntry
	for existingRows.Next() {
		var e models.ACLEntry
		if err := existingRows.Scan(&e.SubjectType, &e.SubjectID, &e.Actions); err != nil {
			existingRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		oldEntries = append(oldEntries, e)
	}
	existingRows.Close()

	// Delete all existing entries for this resource in this org
	if _, err := tx.Exec(ctx,
		`DELETE FROM acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, claims.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear ACL")
		return
	}

	// Insert new entries, skipping invalid ones
	var inserted []models.ACLEntry
	for _, e := range req.Entries {
		if e.SubjectType == "" || e.SubjectID == "" || len(e.Actions) == 0 {
			continue
		}
		var entry models.ACLEntry
		err := tx.QueryRow(ctx,
			`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
             VALUES ($1, $2, $3::uuid, $4, $5, $6)
             RETURNING id, org_id, resource_type, resource_id::text, subject_type, subject_id, actions, created_at`,
			claims.OrgID, resourceType, resourceID, e.SubjectType, e.SubjectID, e.Actions,
		).Scan(&entry.ID, &entry.OrgID, &entry.ResourceType, &entry.ResourceID,
			&entry.SubjectType, &entry.SubjectID, &entry.Actions, &entry.CreatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to insert ACL entry")
			return
		}
		inserted = append(inserted, entry)
	}

	// Compare old vs new to build audit events
	auditEvents := aclAuditDiff(claims.UserID, claims.OrgID, resourceType, resourceID, oldEntries, inserted)

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	// Log audit events after successful commit
	for _, e := range auditEvents {
		if err := s.audit.Log(ctx, e); err != nil {
			// Log error but don't fail the request since ACL was updated successfully
			continue
		}
	}

	if inserted == nil {
		inserted = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, inserted)
}

// handlePutSessionACL implements ACL writes for agent_session resources. Only
// the owner or an org admin in admin mode may write (checkSessionPermission
// with "share" honors both); org admins without admin mode are ordinary
// members here. Entries are validated as same-org read-only shares and replace
// the previous non-owner entries; the owner's full-access entry is upserted
// last, so a replace-style PUT can never lock the owner out.
func (s *Server) handlePutSessionACL(w http.ResponseWriter, r *http.Request, sessionID string) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	allowed, err := s.checkSessionPermission(ctx, claims.UserID, claims.OrgID, claims.Role, sessionID, "share")
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Entries []aclEntryInput `json:"entries"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Session ACL rows belong to the agent's org; resolve that org and the
	// owner (whose entry is preserved) before validating the shares.
	var ownerID, sessionOrgID string
	err = s.db.Pool.QueryRow(ctx, `
		SELECT s.user_id, a.org_id
		FROM agent_sessions s
		JOIN agents a ON a.id = s.agent_id
		WHERE s.id = $1
	`, sessionID).Scan(&ownerID, &sessionOrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load session")
		return
	}

	// The normalizer drops entries naming the owner and rejects anything that
	// is not a same-org, view-only share: invalid input maps to 400 and
	// database failures map to 500.
	shares, err := s.normalizeSessionShareEntries(ctx, ownerID, sessionOrgID, req.Entries)
	if err != nil {
		writeSessionShareError(w, err)
		return
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(ctx)

	// Capture the previous non-owner entries for the audit diff. The preserved
	// owner row is excluded so it never surfaces as revoked.
	var oldEntries []models.ACLEntry
	oldRows, err := tx.Query(ctx, `
		SELECT subject_type, subject_id, actions FROM acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = $1::uuid AND org_id = $2
		  AND NOT (subject_type = 'user' AND subject_id = $3)
	`, sessionID, sessionOrgID, ownerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query existing ACL")
		return
	}
	for oldRows.Next() {
		var e models.ACLEntry
		if err := oldRows.Scan(&e.SubjectType, &e.SubjectID, &e.Actions); err != nil {
			oldRows.Close()
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		oldEntries = append(oldEntries, e)
	}
	oldRows.Close()

	// Replace only the non-owner entries; the owner row is untouched here.
	if _, err := tx.Exec(ctx, `
		DELETE FROM acl_entries
		WHERE resource_type = 'agent_session' AND resource_id = $1::uuid AND org_id = $2
		  AND NOT (subject_type = 'user' AND subject_id = $3)
	`, sessionID, sessionOrgID, ownerID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear ACL")
		return
	}

	if err := insertSessionACLEntries(ctx, tx, sessionOrgID, sessionID, shares); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert ACL entry")
		return
	}

	// Upsert the owner entry last: recreate it for legacy sessions that lack
	// one and restore full access if it was ever downgraded.
	if _, err := tx.Exec(ctx, `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'agent_session', $2::uuid, 'user', $3, ARRAY['view','edit','share','delete','admin'])
		ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		DO UPDATE SET actions = EXCLUDED.actions
	`, sessionOrgID, sessionID, ownerID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to preserve owner ACL entry")
		return
	}

	newEntries := make([]models.ACLEntry, 0, len(shares))
	for _, share := range shares {
		newEntries = append(newEntries, models.ACLEntry{
			SubjectType: share.SubjectType,
			SubjectID:   share.SubjectID,
			Actions:     share.Actions,
		})
	}
	// Attribute the audit to the session's org: that is where the ACL rows
	// live even when the owner writes with a token for another org.
	auditEvents := aclAuditDiff(claims.UserID, sessionOrgID, "agent_session", sessionID, oldEntries, newEntries)

	// Return the resource's resulting ACL state, owner entry included.
	resultRows, err := tx.Query(ctx,
		`SELECT id, org_id, resource_type, resource_id::text, subject_type, subject_id, actions, created_at
         FROM acl_entries
         WHERE resource_type = 'agent_session' AND resource_id = $1::uuid AND org_id = $2
         ORDER BY subject_type, subject_id`,
		sessionID, sessionOrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ACL")
		return
	}
	entries, err := scanACLEntries(resultRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ACL")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	for _, e := range auditEvents {
		if err := s.audit.Log(ctx, e); err != nil {
			// Log error but don't fail the request since the ACL was updated.
			continue
		}
	}

	if entries == nil {
		entries = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}
