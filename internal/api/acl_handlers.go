package api

import (
	"net/http"

	"github.com/heavenlabs/hnb/internal/models"
)

type aclEntryInput struct {
	SubjectType string   `json:"subject_type"`
	SubjectID   string   `json:"subject_id"`
	Actions     []string `json:"actions"`
}

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

	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "commit failed")
		return
	}

	if inserted == nil {
		inserted = []models.ACLEntry{}
	}
	writeJSON(w, http.StatusOK, inserted)
}
