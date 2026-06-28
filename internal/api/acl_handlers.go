// Package api provides HTTP handlers, middleware, and routing for the hnb API server.
// Handlers are organized by resource type (notebooks, cells, connectors, etc.)
// and use net/http ServeMux with no external framework.
package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/audit"
	"github.com/heavenlabs/hnb/internal/models"
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

	if claims.Role != "admin" {
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
	defer rows.Close()

	var entries []models.ACLEntry
	for rows.Next() {
		var e models.ACLEntry
		if err := rows.Scan(&e.ID, &e.OrgID, &e.ResourceType, &e.ResourceID,
			&e.SubjectType, &e.SubjectID, &e.Actions, &e.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan failed")
			return
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
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
	claims := ClaimsFromContext(r.Context())
	resourceType := r.PathValue("resource_type")
	resourceID := r.PathValue("resource_id")

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
	var auditEvents []audit.Entry
	for _, old := range oldEntries {
		found := false
		for _, new := range inserted {
			if old.SubjectType == new.SubjectType && old.SubjectID == new.SubjectID {
				found = true
				if !slicesEqual(old.Actions, new.Actions) {
					auditEvents = append(auditEvents, audit.Entry{
						OrgID:        claims.OrgID,
						UserID:       claims.UserID,
						Action:       "acl.updated",
						ResourceType: resourceType,
						ResourceID:   resourceID,
						Metadata: map[string]any{
							"subject_type": new.SubjectType,
							"subject_id":   new.SubjectID,
							"old_actions":  old.Actions,
							"new_actions":  new.Actions,
						},
					})
				}
				break
			}
		}
		if !found {
			auditEvents = append(auditEvents, audit.Entry{
				OrgID:        claims.OrgID,
				UserID:       claims.UserID,
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
	for _, new := range inserted {
		found := false
		for _, old := range oldEntries {
			if old.SubjectType == new.SubjectType && old.SubjectID == new.SubjectID {
				found = true
				break
			}
		}
		if !found {
			auditEvents = append(auditEvents, audit.Entry{
				OrgID:        claims.OrgID,
				UserID:       claims.UserID,
				Action:       "acl.granted",
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Metadata: map[string]any{
					"subject_type": new.SubjectType,
					"subject_id":   new.SubjectID,
					"actions":      new.Actions,
				},
			})
		}
	}

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
