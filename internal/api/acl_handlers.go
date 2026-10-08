// Package api provides HTTP handlers, middleware, and routing for the Aether API server.
// Handlers are organized by resource type (notebooks, cells, connectors, etc.)
// and use net/http ServeMux with no external framework.
package api

import (
	"context"
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

// unionActions merges two action lists preserving first-seen order.
func unionActions(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, action := range list {
			if !seen[action] {
				seen[action] = true
				out = append(out, action)
			}
		}
	}
	return out
}

// aclAuditMetadata labels one audit subject; staged pending subjects carry
// pending: true so consumers can tell them from real entries.
func aclAuditMetadata(subjectType, subjectID string) map[string]any {
	meta := map[string]any{
		"subject_type": subjectType,
		"subject_id":   subjectID,
	}
	if subjectType == "pending_user" {
		meta["pending"] = true
	}
	return meta
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

	// Session ACL rows live in the agent's org, which may differ from the
	// token's org for the owner, so the scan must use the session's org to see
	// what the PUT handler writes.
	scanOrgID := claims.OrgID
	if resourceType == "agent_session" {
		// Sessions have no unconditional org-admin bypass: an org admin needs
		// admin mode, exactly like every other session route.
		allowed, err := s.checkSessionPermission(ctx, claims.UserID, claims.OrgID, claims.Role, resourceID, "view")
		if err != nil || !allowed {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		sessionOrgID, err := s.resourceOrgID(ctx, "agent_session", resourceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load session")
			return
		}
		if sessionOrgID == "" {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		scanOrgID = sessionOrgID
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
		resourceType, resourceID, scanOrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	entries, err := scanACLEntries(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}

	// Staged rows follow the real entries: same visibility rules, additional
	// pending_user subject type.
	pendingEntries, err := queryPendingACLEntries(ctx, s.db.Pool, scanOrgID, resourceType, resourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	entries = append(entries, pendingEntries...)

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

// pendingQueryer is the query surface staged-row loading needs; both
// *pgxpool.Pool and pgx.Tx satisfy it.
type pendingQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// pendingACLSelect loads staged rows for a resource. Emails are stored
// lowercased by every writer and are synthesized as the subject_id.
const pendingACLSelect = `
	SELECT id, org_id, resource_type, resource_id::text, email, actions, created_at
	FROM pending_acl_entries
	WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3
	ORDER BY lower(email)`

// queryPendingACLEntries returns the staged rows for a resource through q as
// models.ACLEntry rows with subject_type "pending_user" and pending: true. The
// row ID is the pending row's UUID so clients can key and remove it.
func queryPendingACLEntries(ctx context.Context, q pendingQueryer, orgID, resourceType, resourceID string) ([]models.ACLEntry, error) {
	rows, err := q.Query(ctx, pendingACLSelect, resourceType, resourceID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []models.ACLEntry
	for rows.Next() {
		var e models.ACLEntry
		if err := rows.Scan(&e.ID, &e.OrgID, &e.ResourceType, &e.ResourceID,
			&e.SubjectID, &e.Actions, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.SubjectType = "pending_user"
		e.Pending = true
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// aclAuditDiff compares previous and replacement ACL entries by subject and
// returns the acl.revoked / acl.updated / acl.granted events describing the
// change. Pending subjects are compared like any other subject so replacing a
// staged row audits a revoke/grant pair.
func aclAuditDiff(userID, orgID, resourceType, resourceID string, oldEntries, newEntries []models.ACLEntry) []audit.Entry {
	var auditEvents []audit.Entry
	for _, old := range oldEntries {
		found := false
		for _, newEntry := range newEntries {
			if old.SubjectType == newEntry.SubjectType && old.SubjectID == newEntry.SubjectID {
				found = true
				if !slicesEqual(old.Actions, newEntry.Actions) {
					meta := aclAuditMetadata(newEntry.SubjectType, newEntry.SubjectID)
					meta["old_actions"] = old.Actions
					meta["new_actions"] = newEntry.Actions
					auditEvents = append(auditEvents, audit.Entry{
						OrgID:        orgID,
						UserID:       userID,
						Action:       "acl.updated",
						ResourceType: resourceType,
						ResourceID:   resourceID,
						Metadata:     meta,
					})
				}
				break
			}
		}
		if !found {
			meta := aclAuditMetadata(old.SubjectType, old.SubjectID)
			meta["actions"] = old.Actions
			auditEvents = append(auditEvents, audit.Entry{
				OrgID:        orgID,
				UserID:       userID,
				Action:       "acl.revoked",
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Metadata:     meta,
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
			meta := aclAuditMetadata(newEntry.SubjectType, newEntry.SubjectID)
			meta["actions"] = newEntry.Actions
			auditEvents = append(auditEvents, audit.Entry{
				OrgID:        orgID,
				UserID:       userID,
				Action:       "acl.granted",
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Metadata:     meta,
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

	// Capture existing entries before deleting (for audit comparison). Staged
	// pending rows participate so a replace that drops them audits a revoke.
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
	if err := existingRows.Err(); err != nil {
		existingRows.Close()
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
	}
	existingRows.Close()

	pendingOld, err := queryPendingACLEntries(ctx, tx, claims.OrgID, resourceType, resourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query existing ACL")
		return
	}
	oldEntries = append(oldEntries, pendingOld...)

	// Replace semantics apply to both sources: a PUT that omits staged rows
	// removes them, exactly like omitting a real entry.
	//
	// Delete staged rows first. Materialization (applyPendingACL's single
	// statement) locks pending_acl_entries before acl_entries; taking the
	// locks in the same order here prevents a lock-order deadlock with a
	// concurrent join materializing the same email.
	if _, err := tx.Exec(ctx,
		`DELETE FROM pending_acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, claims.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear pending ACL")
		return
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM acl_entries WHERE resource_type = $1 AND resource_id = $2::uuid AND org_id = $3`,
		resourceType, resourceID, claims.OrgID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear ACL")
		return
	}

	// Insert new entries, skipping invalid ones. pending_user entries are
	// validated, lowercased, deduped, and routed to pending_acl_entries; an
	// email that already belongs to an org member becomes a real user entry
	// instead (a staged row for a member could never materialize).
	type pendingInsert struct {
		email   string
		actions []string
	}
	var inserted []models.ACLEntry
	var pendingInserts []pendingInsert
	pendingIndex := map[string]int{}
	// userIndex tracks inserted user entries so an explicit entry and a
	// converted pending email naming the same member merge into one entry
	// (the acl_entries unique constraint forbids two).
	userIndex := map[string]int{}
	insertUserEntry := func(userID string, actions []string) error {
		if idx, seen := userIndex[userID]; seen {
			merged := unionActions(inserted[idx].Actions, actions)
			if !slicesEqual(merged, inserted[idx].Actions) {
				if _, err := tx.Exec(ctx, `UPDATE acl_entries SET actions = $1 WHERE id = $2`, merged, inserted[idx].ID); err != nil {
					return err
				}
				inserted[idx].Actions = merged
			}
			return nil
		}
		var entry models.ACLEntry
		err := tx.QueryRow(ctx,
			`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
             VALUES ($1, $2, $3::uuid, $4, $5, $6)
             ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
             DO UPDATE SET actions = (SELECT ARRAY(SELECT DISTINCT unnest(acl_entries.actions || EXCLUDED.actions) ORDER BY 1))
             RETURNING id, org_id, resource_type, resource_id::text, subject_type, subject_id, actions, created_at`,
			claims.OrgID, resourceType, resourceID, "user", userID, actions,
		).Scan(&entry.ID, &entry.OrgID, &entry.ResourceType, &entry.ResourceID,
			&entry.SubjectType, &entry.SubjectID, &entry.Actions, &entry.CreatedAt)
		if err != nil {
			return err
		}
		userIndex[userID] = len(inserted)
		inserted = append(inserted, entry)
		return nil
	}

	for _, e := range req.Entries {
		if e.SubjectType == "" || e.SubjectID == "" || len(e.Actions) == 0 {
			continue
		}
		// Unknown subject types would violate acl_entries' check constraint
		// and turn into a 500; skip them like any other invalid entry.
		switch e.SubjectType {
		case "user", "group", "org_role", "pending_user":
		default:
			continue
		}
		if e.SubjectType == "pending_user" {
			email, ok := normalizePendingEmail(e.SubjectID)
			if !ok {
				continue
			}
			// Existing org member? Add them as a real user entry rather than
			// staging a row that would never be materialized (they have
			// already appeared in the org), mirroring
			// handleAddPendingGroupMembers.
			var memberID string
			err := tx.QueryRow(ctx, `
				SELECT u.id FROM users u
				JOIN org_members om ON om.user_id = u.id AND om.org_id = $1
				WHERE lower(u.email) = $2
				LIMIT 1`, claims.OrgID, email).Scan(&memberID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, "failed to resolve pending email")
				return
			}
			if err == nil {
				if err := insertUserEntry(memberID, e.Actions); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to insert ACL entry")
					return
				}
				continue
			}
			if idx, seen := pendingIndex[email]; seen {
				pendingInserts[idx].actions = unionActions(pendingInserts[idx].actions, e.Actions)
				continue
			}
			pendingIndex[email] = len(pendingInserts)
			pendingInserts = append(pendingInserts, pendingInsert{email: email, actions: unionActions(nil, e.Actions)})
			continue
		}
		if e.SubjectType == "user" {
			if err := insertUserEntry(e.SubjectID, e.Actions); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to insert ACL entry")
				return
			}
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
	for _, p := range pendingInserts {
		var entry models.ACLEntry
		err := tx.QueryRow(ctx, `
			INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions, created_by)
			VALUES ($1, $2, $3::uuid, $4, $5, $6)
			ON CONFLICT (resource_type, resource_id, lower(email)) DO UPDATE
			SET actions = (SELECT ARRAY(SELECT DISTINCT unnest(pending_acl_entries.actions || EXCLUDED.actions)))
			RETURNING id, org_id, resource_type, resource_id::text, email, actions, created_at`,
			claims.OrgID, resourceType, resourceID, p.email, p.actions, claims.UserID,
		).Scan(&entry.ID, &entry.OrgID, &entry.ResourceType, &entry.ResourceID,
			&entry.SubjectID, &entry.Actions, &entry.CreatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to insert pending ACL entry")
			return
		}
		entry.SubjectType = "pending_user"
		entry.Pending = true
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
// the owner or an org admin in admin mode may write; org admins without admin
// mode are ordinary members here. The session row is locked before any
// acl_entries work (agent_sessions -> acl_entries, matching create/delete), so
// concurrent PUTs serialize and a session delete cannot interleave to leave
// orphan ACL rows. The owner and org used to validate and mutate come from that
// locked row, and a missing session is a 404. Entries are validated as same-org
// read-only shares and replace the previous non-owner entries; the owner's
// full-access entry is upserted last, so a replace-style PUT can never lock the
// owner out.
func (s *Server) handlePutSessionACL(w http.ResponseWriter, r *http.Request, sessionID string) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database error")
		return
	}
	defer tx.Rollback(ctx)

	// Lock and resolve the session before touching acl_entries. Locking only
	// the session row keeps the transaction from touching agents rows and
	// holds the lock order the create/delete paths rely on.
	var ownerID, sessionOrgID string
	err = tx.QueryRow(ctx, `
		SELECT s.user_id, a.org_id
		FROM agent_sessions s
		JOIN agents a ON a.id = s.agent_id
		WHERE s.id = $1
		FOR UPDATE OF s
	`, sessionID).Scan(&ownerID, &sessionOrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load session")
		return
	}

	// Only the owner or an org admin in admin mode may write. This mirrors
	// checkSessionPermission's "share" rule against the row already locked and
	// resolved above, so no second pool connection is needed while the
	// transaction holds the lock.
	if claims.UserID != ownerID &&
		!(claims.Role == "admin" && adminModeFromContext(ctx) && claims.OrgID == sessionOrgID) {
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

	// The normalizer drops entries naming the owner and rejects anything that
	// is not a same-org, view-only share: invalid input maps to 400 and
	// database failures map to 500. Running it against the transaction keeps
	// the membership checks in the same snapshot as the locked session.
	shares, err := s.normalizeSessionShareEntries(ctx, tx, ownerID, sessionOrgID, req.Entries)
	if err != nil {
		writeSessionShareError(w, err)
		return
	}

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
	if err := oldRows.Err(); err != nil {
		oldRows.Close()
		writeError(w, http.StatusInternalServerError, "scan failed")
		return
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
		VALUES ($1, 'agent_session', $2::uuid, 'user', $3, $4)
		ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		DO UPDATE SET actions = EXCLUDED.actions
	`, sessionOrgID, sessionID, ownerID, sessionOwnerActions); err != nil {
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
