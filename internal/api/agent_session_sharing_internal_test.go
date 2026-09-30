package api

// White-box session-sharing tests. The create-with-inheritance test asserts a
// session created with share_with_notebook_viewers is readable by a notebook
// viewer immediately after the 201; the normalization tests pin the share-list
// validation (dedup, owner drop, cap, error classification) without going
// through the HTTP layer. The external api_test package cannot reach these
// helpers, so the tests live in package api and reuse the session-permission
// helpers from permissions_internal_test.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/executor"
)

func TestCreateSessionInheritanceVisibleToNotebookViewer(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "create-inherit")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "admin")
	viewerID := insertSessionPermUser(t, s, "viewer")
	addSessionPermMember(t, s, orgID, viewerID, "editor")

	notebookID := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, $3, $4)`,
		notebookID.String(), orgID.String(), "Inherit Notebook", ownerID.String())
	require.NoError(t, err)
	grantSessionPermACL(t, s, orgID, "notebook", notebookID, viewerID, []string{"view"})

	agentID := uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO agents (id, org_id, name, created_by) VALUES ($1, $2, $3, $4)`,
		agentID.String(), orgID.String(), "Inherit Agent", ownerID.String())
	require.NoError(t, err)

	token, err := s.jwt.Issue(ownerID.String(), orgID.String(), "admin")
	require.NoError(t, err)

	body, err := json.Marshal(map[string]any{
		"notebook_id":                 notebookID.String(),
		"share_with_notebook_viewers": true,
	})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/api/v1/agents/"+agentID.String()+"/session", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	sessionID, _ := resp["session_id"].(string)
	require.NotEmpty(t, sessionID)

	// Immediately readable by a notebook viewer, with no follow-up ACL write.
	allowed, err := s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", sessionID, "view")
	require.NoError(t, err)
	require.True(t, allowed, "notebook viewer must read the just-created inherited session")

	// Inheritance remains view-only.
	allowed, err = s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", sessionID, "edit")
	require.NoError(t, err)
	require.False(t, allowed)
}

// TestNormalizeSessionShareEntriesDedupAndOwnerDrop pins the normalization
// result directly, independently of the acl_entries ON CONFLICT clause: the
// owner is dropped even when named with actions, duplicates collapse to a
// single entry, and the output keeps first-seen order.
func TestNormalizeSessionShareEntriesDedupAndOwnerDrop(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "normalize")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	memberID := insertSessionPermUser(t, s, "member")
	addSessionPermMember(t, s, orgID, memberID, "editor")

	groupID := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
		groupID.String(), orgID.String(), "Normalize Group")
	require.NoError(t, err)

	got, err := s.normalizeSessionShareEntries(ctx, ownerID.String(), orgID.String(), []aclEntryInput{
		{SubjectType: "user", SubjectID: memberID.String()},
		{SubjectType: "user", SubjectID: ownerID.String(), Actions: []string{"edit"}},
		{SubjectType: "user", SubjectID: memberID.String(), Actions: []string{"view"}},
		{SubjectType: "org_role", SubjectID: "everyone"},
		{SubjectType: "group", SubjectID: groupID.String(), Actions: []string{"view"}},
	})
	require.NoError(t, err)
	require.Equal(t, []aclEntryInput{
		{SubjectType: "user", SubjectID: memberID.String(), Actions: []string{"view"}},
		{SubjectType: "org_role", SubjectID: "everyone", Actions: []string{"view"}},
		{SubjectType: "group", SubjectID: groupID.String(), Actions: []string{"view"}},
	}, got, "owner entries must be dropped and duplicates collapsed before any insert")
}

// TestNormalizeSessionShareEntriesCap asserts the cap is enforced before the
// per-entry work: entries naming the owner are otherwise silently dropped, so
// only the cap can make this list fail.
func TestNormalizeSessionShareEntriesCap(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	ownerID := uuid.NewString()

	entries := make([]aclEntryInput, maxSessionShares+1)
	for i := range entries {
		entries[i] = aclEntryInput{SubjectType: "user", SubjectID: ownerID, Actions: []string{"view"}}
	}

	_, err := s.normalizeSessionShareEntries(ctx, ownerID, uuid.NewString(), entries)
	require.Error(t, err)
	require.ErrorIs(t, err, errInvalidSessionShare, "oversized share lists must be rejected as invalid input")
}

// grantSessionPermGroupACL upserts one group-subject ACL row; the user-subject
// helper in permissions_internal_test.go cannot express group grants.
func grantSessionPermGroupACL(t *testing.T, s *Server, orgID uuid.UUID, resourceType string, resourceID, subjectID uuid.UUID, actions []string) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, $2, $3::uuid, 'group', $4, $5)
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		 DO UPDATE SET actions = EXCLUDED.actions`,
		orgID.String(), resourceType, resourceID.String(), subjectID.String(), actions)
	require.NoError(t, err)
}

// TestFilterVisibleSessionsMatchesPerRowChecks pins the batched visibility
// helper against the per-row checkSessionPermission primitive for every access
// path (owner, direct user, custom group, Everyone group, everyone org_role,
// notebook inheritance, admin mode) so an optimized query and the authoritative
// check can never diverge silently.
func TestFilterVisibleSessionsMatchesPerRowChecks(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "filter")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	directID := insertSessionPermUser(t, s, "direct")
	addSessionPermMember(t, s, orgID, directID, "editor")
	groupMemberID := insertSessionPermUser(t, s, "group-member")
	addSessionPermMember(t, s, orgID, groupMemberID, "editor")
	inheritorID := insertSessionPermUser(t, s, "inheritor")
	addSessionPermMember(t, s, orgID, inheritorID, "editor")
	unrelatedID := insertSessionPermUser(t, s, "unrelated")
	addSessionPermMember(t, s, orgID, unrelatedID, "editor")
	adminID := insertSessionPermUser(t, s, "admin")
	addSessionPermMember(t, s, orgID, adminID, "admin")

	groupID := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
		groupID.String(), orgID.String(), "Filter Group")
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		groupID.String(), groupMemberID.String())
	require.NoError(t, err)

	// The org was seeded with raw SQL, so no Everyone group exists yet; create
	// the canonical group and resolve its ID by name exactly the way
	// callerGroupIDs and checkPermission do.
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO groups (org_id, name, source) VALUES ($1, 'Everyone', 'system')`,
		orgID.String())
	require.NoError(t, err)
	var everyoneGroupID string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT id FROM groups WHERE org_id = $1 AND name = 'Everyone'`,
		orgID.String()).Scan(&everyoneGroupID))

	notebookID := uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, $3, $4)`,
		notebookID.String(), orgID.String(), "Filter Notebook", ownerID.String())
	require.NoError(t, err)
	grantSessionPermACL(t, s, orgID, "notebook", notebookID, inheritorID, []string{"view"})

	// A second notebook inherits view from its parent folder and has no direct
	// notebook ACL: the batched path must fall back to checkPermission's
	// ancestor walk to see it.
	folderID := uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO folders (id, org_id, name, created_by) VALUES ($1, $2, $3, $4)`,
		folderID.String(), orgID.String(), "Filter Folder", ownerID.String())
	require.NoError(t, err)
	grantSessionPermACL(t, s, orgID, "folder", folderID, inheritorID, []string{"view"})

	folderNotebookID := uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO notebooks (id, org_id, folder_id, title, created_by) VALUES ($1, $2, $3, $4, $5)`,
		folderNotebookID.String(), orgID.String(), folderID.String(), "Filter Folder Notebook", ownerID.String())
	require.NoError(t, err)

	_, ownedSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)
	_, directSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)
	grantSessionPermACL(t, s, orgID, "agent_session", directSessionID, directID, []string{"view"})
	_, groupSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)
	grantSessionPermGroupACL(t, s, orgID, "agent_session", groupSessionID, groupID, []string{"view"})
	_, everyoneGroupSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'agent_session', $2::uuid, 'group', $3, ARRAY['view'])`,
		orgID.String(), everyoneGroupSessionID.String(), everyoneGroupID)
	require.NoError(t, err)
	_, everyoneSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'agent_session', $2::uuid, 'org_role', 'everyone', ARRAY['view'])`,
		orgID.String(), everyoneSessionID.String())
	require.NoError(t, err)
	nb := notebookID
	_, inheritedSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, &nb, true)
	_, mixedSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, &nb, true)
	grantSessionPermACL(t, s, orgID, "agent_session", mixedSessionID, directID, []string{"view"})
	folderNB := folderNotebookID
	_, folderInheritedSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, &folderNB, true)
	_, privateSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nil, false)

	notebookIDCopy := notebookID.String()
	folderNotebookIDCopy := folderNotebookID.String()
	candidates := []listSessionRow{
		{ID: ownedSessionID.String(), UserID: ownerID.String()},
		{ID: directSessionID.String(), UserID: ownerID.String()},
		{ID: groupSessionID.String(), UserID: ownerID.String()},
		{ID: everyoneGroupSessionID.String(), UserID: ownerID.String()},
		{ID: everyoneSessionID.String(), UserID: ownerID.String()},
		{ID: inheritedSessionID.String(), UserID: ownerID.String(), NotebookID: &notebookIDCopy, Inherit: true},
		{ID: mixedSessionID.String(), UserID: ownerID.String(), NotebookID: &notebookIDCopy, Inherit: true},
		{ID: folderInheritedSessionID.String(), UserID: ownerID.String(), NotebookID: &folderNotebookIDCopy, Inherit: true},
		{ID: privateSessionID.String(), UserID: ownerID.String()},
	}

	callers := []struct {
		name      string
		userID    uuid.UUID
		role      string
		adminMode bool
	}{
		{"owner", ownerID, "editor", false},
		{"direct share", directID, "editor", false},
		{"group member", groupMemberID, "editor", false},
		{"notebook inheritor", inheritorID, "editor", false},
		{"unrelated member", unrelatedID, "editor", false},
		{"admin without admin mode", adminID, "admin", false},
		{"admin with admin mode", adminID, "admin", true},
	}

	for _, caller := range callers {
		t.Run(caller.name, func(t *testing.T) {
			callerCtx := ctx
			if caller.adminMode {
				callerCtx = executor.WithAdminMode(ctx, true)
			}

			expectedVisible := map[string]bool{}
			expectedEdit := map[string]bool{}
			for _, candidate := range candidates {
				allowed, err := s.checkSessionPermission(callerCtx, caller.userID.String(), orgID.String(), caller.role, candidate.ID, "view")
				require.NoError(t, err)
				expectedVisible[candidate.ID] = allowed

				editable, err := s.checkSessionPermission(callerCtx, caller.userID.String(), orgID.String(), caller.role, candidate.ID, "edit")
				require.NoError(t, err)
				expectedEdit[candidate.ID] = editable
			}

			groupIDs, err := s.callerGroupIDs(callerCtx, caller.userID.String(), orgID.String())
			require.NoError(t, err)
			got, err := s.filterVisibleSessions(callerCtx, caller.userID.String(), orgID.String(), caller.role, groupIDs, candidates)
			require.NoError(t, err)

			gotIDs := make([]string, 0, len(got))
			expectedIDs := make([]string, 0, len(expectedVisible))
			for id, visible := range expectedVisible {
				if visible {
					expectedIDs = append(expectedIDs, id)
				}
			}
			for _, row := range got {
				gotIDs = append(gotIDs, row.ID)
				require.Equal(t, expectedEdit[row.ID], row.CanEdit,
					"can_edit must match the per-row edit check for %s", row.ID)
				require.True(t, expectedVisible[row.ID],
					"filter returned %s but the per-row view check denies it", row.ID)
			}
			require.ElementsMatch(t, expectedIDs, gotIDs,
				"batched visibility must match the per-row check")
		})
	}
}

// TestMergeSessionRowsDedupOrderAndCap pins mergeSessionRows: duplicates across
// the two pages collapse to the first-seen row, the result is newest-first, and
// the cap truncates the oldest rows.
func TestMergeSessionRowsDedupOrderAndCap(t *testing.T) {
	now := time.Now()
	row := func(id string, age time.Duration) listSessionRow {
		return listSessionRow{ID: id, CreatedAt: now.Add(-age)}
	}

	primary := []listSessionRow{row("a", 3*time.Hour), row("b", time.Hour)}
	secondary := []listSessionRow{row("b", 2*time.Hour), row("c", 4*time.Hour)}

	merged := mergeSessionRows(primary, secondary, 10)
	require.Len(t, merged, 3, "the duplicate b row must collapse")
	require.Equal(t, []string{"b", "a", "c"}, []string{merged[0].ID, merged[1].ID, merged[2].ID},
		"newest first, primary page first on ties")
	require.Equal(t, now.Add(-time.Hour), merged[0].CreatedAt, "the first-seen b row wins")

	capped := mergeSessionRows(primary, secondary, 2)
	require.Equal(t, []string{"b", "a"}, []string{capped[0].ID, capped[1].ID},
		"the cap keeps the newest rows")
}

// TestSessionShareDatabaseFailureMapsToServerError proves a database outage
// during share validation is classified as a server error, never as a 400.
func TestSessionShareDatabaseFailureMapsToServerError(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.normalizeSessionShareEntries(ctx, uuid.NewString(), uuid.NewString(), []aclEntryInput{
		{SubjectType: "user", SubjectID: uuid.NewString(), Actions: []string{"view"}},
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, errInvalidSessionShare, "a database failure must not be classified as invalid input")

	rec := httptest.NewRecorder()
	writeSessionShareError(rec, err)
	require.Equal(t, http.StatusInternalServerError, rec.Code,
		"a database failure must map to 500: %s", rec.Body.String())
}
