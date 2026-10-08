package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// errInvalidSessionShare marks a share entry rejected as invalid input.
// Handlers map errors wrapping it to 400; any other normalization error is a
// server-side failure and maps to 500. It exists so DB outages are never
// reported as client mistakes (and pgx/SQL text never leaks to callers).
var errInvalidSessionShare = errors.New("invalid session share")

// maxSessionShares bounds the share list accepted per request. The cap is
// enforced before any per-entry database work so an oversized request cannot
// amplify into one query per entry.
const maxSessionShares = 100

// sessionOwnerActions is the full-access action set written to a session
// owner's ACL row at creation and re-upserted by the PUT handler. It is the
// single Go source of truth for the owner entry; both call sites bind it as a
// query parameter rather than spelling the array out in SQL.
var sessionOwnerActions = []string{"view", "edit", "share", "delete", "admin"}

// shareQueryer is the query surface share normalization needs. Both
// *pgxpool.Pool and pgx.Tx satisfy it, so the same validation runs inside the
// PUT transaction (against the locked session) and against the pool on session
// create.
type shareQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// writeSessionShareError maps a share-normalization error to the session
// create/update response: 400 with a stable message for invalid input, 500 with
// a generic message (and a server log) for anything else.
func writeSessionShareError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidSessionShare) {
		writeError(w, http.StatusBadRequest, "invalid shares")
		return
	}
	slog.Error("session share validation failed", "error", err)
	writeError(w, http.StatusInternalServerError, "failed to validate shares")
}

// normalizeSessionShareEntries validates and normalizes a session share list for
// userID in orgID, running its membership lookups through q. Subjects must
// belong to the caller's org and sharing is read-only: a non-owner subject's
// actions must be exactly ["view"] (an omitted list defaults to view).
// Duplicate subjects collapse, and entries naming the owner are dropped because
// the owner entry already carries full access.
//
// pending_user entries name someone who is not a member yet: the email is
// validated and lowercased here (the same canonical form every pending table
// uses) and staged in pending_acl_entries at insert time. An email that already
// belongs to an org member is converted to a real user share instead — a
// staged row for a member could never materialize (they have already joined),
// mirroring handlePutACL's conversion. The read-only rule applies unchanged, so
// a staged session share can only ever materialize as view.
//
// Invalid input wraps errInvalidSessionShare; database failures are wrapped
// without it so writeSessionShareError can map them to 500.
func (s *Server) normalizeSessionShareEntries(ctx context.Context, q shareQueryer, userID, orgID string, entries []aclEntryInput) ([]aclEntryInput, error) {
	if len(entries) > maxSessionShares {
		return nil, fmt.Errorf("%w: at most %d shares are allowed", errInvalidSessionShare, maxSessionShares)
	}

	// Resolve pending emails that already belong to org members up front so the
	// conversion below participates in the main pass's owner-drop and
	// duplicate-collapse logic. Invalid emails are skipped here and rejected by
	// the main pass.
	pendingEmails := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.SubjectType == "pending_user" {
			if email, ok := normalizePendingEmail(e.SubjectID); ok {
				pendingEmails = append(pendingEmails, email)
			}
		}
	}
	var memberByEmail map[string]string
	if len(pendingEmails) > 0 {
		var err error
		memberByEmail, err = s.lookupPendingShareMembers(ctx, q, orgID, pendingEmails)
		if err != nil {
			return nil, fmt.Errorf("validate pending share emails: %w", err)
		}
	}

	normalized := make([]aclEntryInput, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	var userIDs, groupIDs []string

	for _, e := range entries {
		switch e.SubjectType {
		case "user":
			if !isValidUUID(e.SubjectID) {
				return nil, fmt.Errorf("%w: invalid share user", errInvalidSessionShare)
			}
			if e.SubjectID == userID {
				continue
			}
			userIDs = append(userIDs, e.SubjectID)
		case "group":
			if !isValidUUID(e.SubjectID) {
				return nil, fmt.Errorf("%w: invalid share group", errInvalidSessionShare)
			}
			groupIDs = append(groupIDs, e.SubjectID)
		case "pending_user":
			// Not a member yet: validate the email here and stage the share in
			// pending_acl_entries at insert time. Read-only applies unchanged.
			// An email that already belongs to an org member converts to a real
			// user share: a staged row for a member could never materialize.
			email, ok := normalizePendingEmail(e.SubjectID)
			if !ok {
				return nil, fmt.Errorf("%w: invalid share email", errInvalidSessionShare)
			}
			if memberID, isMember := memberByEmail[email]; isMember {
				if memberID == userID {
					continue
				}
				e.SubjectType = "user"
				e.SubjectID = memberID
				userIDs = append(userIDs, memberID)
			} else {
				e.SubjectID = email
			}
		case "org_role":
			if e.SubjectID != "everyone" {
				return nil, fmt.Errorf(`%w: org_role shares must use subject_id "everyone"`, errInvalidSessionShare)
			}
		default:
			return nil, fmt.Errorf("%w: invalid share subject_type", errInvalidSessionShare)
		}

		actions := e.Actions
		if len(actions) == 0 {
			actions = []string{"view"}
		}
		if len(actions) != 1 || actions[0] != "view" {
			return nil, fmt.Errorf(`%w: shared sessions are read-only: actions must be ["view"]`, errInvalidSessionShare)
		}

		key := e.SubjectType + ":" + e.SubjectID
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, aclEntryInput{
			SubjectType: e.SubjectType,
			SubjectID:   e.SubjectID,
			Actions:     actions,
		})
	}

	// Membership lookups are batched so a full share list costs at most two
	// queries instead of one per entry.
	if len(userIDs) > 0 {
		found, err := s.lookupShareSubjectIDs(ctx, q,
			`SELECT user_id FROM org_members WHERE org_id = $1 AND user_id = ANY($2::uuid[])`,
			orgID, userIDs)
		if err != nil {
			return nil, fmt.Errorf("validate share users: %w", err)
		}
		for _, id := range userIDs {
			if !found[id] {
				return nil, fmt.Errorf("%w: share user is not a member of this organization", errInvalidSessionShare)
			}
		}
	}
	if len(groupIDs) > 0 {
		found, err := s.lookupShareSubjectIDs(ctx, q,
			`SELECT id FROM groups WHERE org_id = $1 AND id = ANY($2::uuid[])`,
			orgID, groupIDs)
		if err != nil {
			return nil, fmt.Errorf("validate share groups: %w", err)
		}
		for _, id := range groupIDs {
			if !found[id] {
				return nil, fmt.Errorf("%w: share group not found in this organization", errInvalidSessionShare)
			}
		}
	}

	return normalized, nil
}

// lookupShareSubjectIDs runs a single-column lookup scoped by orgID with the
// candidate IDs passed as a uuid array in $2, and returns the matching IDs as a
// set. Query errors are returned unwrapped so callers can classify them.
func (s *Server) lookupShareSubjectIDs(ctx context.Context, q shareQueryer, query, orgID string, ids []string) (map[string]bool, error) {
	rows, err := q.Query(ctx, query, orgID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	found := make(map[string]bool, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		found[id] = true
	}
	return found, rows.Err()
}

// lookupPendingShareMembers resolves pending share emails that already belong
// to org members to their user IDs, keyed by lowercased email. Emails without
// an account in this org are absent from the result and stay staged, mirroring
// the design's cross-org rule (they materialize only if and when they join).
// Query errors are returned unwrapped so callers can classify them.
func (s *Server) lookupPendingShareMembers(ctx context.Context, q shareQueryer, orgID string, emails []string) (map[string]string, error) {
	rows, err := q.Query(ctx, `
		SELECT lower(u.email), u.id
		FROM users u
		JOIN org_members om ON om.user_id = u.id AND om.org_id = $1
		WHERE lower(u.email) = ANY($2::text[])
	`, orgID, emails)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	found := make(map[string]string, len(emails))
	for rows.Next() {
		var email, id string
		if err := rows.Scan(&email, &id); err != nil {
			return nil, err
		}
		found[email] = id
	}
	return found, rows.Err()
}

// insertSessionACLEntries inserts one agent_session ACL row per normalized
// share through tx. pending_user shares are staged in pending_acl_entries
// instead, keyed by lowercased email, and materialize as view-only rows when
// the person first joins. ON CONFLICT DO NOTHING preserves any pre-existing
// real row — in particular the owner's full-access entry, so a share naming
// the owner can never downgrade it.
func insertSessionACLEntries(ctx context.Context, tx pgx.Tx, orgID, sessionID, createdBy string, shares []aclEntryInput) error {
	for _, share := range shares {
		if share.SubjectType == "pending_user" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO pending_acl_entries (org_id, resource_type, resource_id, email, actions, created_by)
				VALUES ($1, 'agent_session', $2::uuid, $3, $4, $5)
				ON CONFLICT (resource_type, resource_id, lower(email)) DO UPDATE
				SET actions = (SELECT ARRAY(SELECT DISTINCT unnest(pending_acl_entries.actions || EXCLUDED.actions)))
			`, orgID, sessionID, share.SubjectID, share.Actions, createdBy); err != nil {
				return fmt.Errorf("insert pending session share: %w", err)
			}
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
			VALUES ($1, 'agent_session', $2::uuid, $3, $4, $5)
			ON CONFLICT (resource_type, resource_id, subject_type, subject_id) DO NOTHING
		`, orgID, sessionID, share.SubjectType, share.SubjectID, share.Actions); err != nil {
			return fmt.Errorf("insert session share: %w", err)
		}
	}
	return nil
}
