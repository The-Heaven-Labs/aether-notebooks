package api

// White-box coverage for how ancestor folder ACL grants expand onto the items
// inside them. Folder ACLs expose a coarse five-action vocabulary
// (view/create/edit/manage/delete) while items support actions the folder
// cannot name directly (notebook run/share, connector and tool use, dashboard
// share/view_with_data). A full folder grant is defined to confer every action
// on every resource it contains; the expansion lives in permissions.go's
// folderActionImplications and this file pins it. checkPermission is
// unexported, so these tests drive the resolver directly rather than through
// an HTTP handler.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// setupFolderInheritance wires one org with an owner and a member, both with
// org membership rows only (no implicit permissions).
func setupFolderInheritance(t *testing.T, label string) (s *Server, orgID, owner, member uuid.UUID) {
	t.Helper()
	s = newSessionPermissionTestServer(t)
	orgID = insertSessionPermOrg(t, s, label)
	owner = insertSessionPermUser(t, s, "owner")
	member = insertSessionPermUser(t, s, "member")
	addSessionPermMember(t, s, orgID, owner, "editor")
	addSessionPermMember(t, s, orgID, member, "editor")
	return s, orgID, owner, member
}

// insertInheritanceFolder inserts a folder owned by ownerID; parentID nil
// creates a root folder.
func insertInheritanceFolder(t *testing.T, s *Server, orgID, owner uuid.UUID, parentID *uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var parent any
	if parentID != nil {
		parent = parentID.String()
	}
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO folders (id, org_id, parent_id, name, created_by) VALUES ($1, $2, $3, $4, $5)`,
		id.String(), orgID.String(), parent, name, owner.String())
	require.NoError(t, err)
	return id
}

// insertInheritanceNotebook inserts a notebook owned by ownerID inside folderID.
func insertInheritanceNotebook(t *testing.T, s *Server, orgID, owner uuid.UUID, folderID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO notebooks (id, org_id, title, created_by, folder_id) VALUES ($1, $2, $3, $4, $5)`,
		id.String(), orgID.String(), "Inheritance Notebook", owner.String(), folderID.String())
	require.NoError(t, err)
	return id
}

// insertInheritanceConnector inserts a connector inside folderID. Only the
// folder linkage matters for permission resolution.
func insertInheritanceConnector(t *testing.T, s *Server, orgID, owner uuid.UUID, folderID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO connectors (id, org_id, name, type, config_encrypted, created_by, folder_id)
		 VALUES ($1, $2, $3, 'postgres', $4, $5, $6)`,
		id.String(), orgID.String(), "Inheritance Connection "+id.String(), []byte("x"), owner.String(), folderID.String())
	require.NoError(t, err)
	return id
}

// insertInheritanceDashboard inserts a dashboard inside folderID.
func insertInheritanceDashboard(t *testing.T, s *Server, orgID, owner uuid.UUID, folderID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO dashboards (id, org_id, title, created_by, folder_id) VALUES ($1, $2, $3, $4, $5)`,
		id.String(), orgID.String(), "Inheritance Dashboard", owner.String(), folderID.String())
	require.NoError(t, err)
	return id
}

// insertInheritanceTool inserts a tool inside folderID.
func insertInheritanceTool(t *testing.T, s *Server, orgID, owner uuid.UUID, folderID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO tools (id, org_id, name, type, folder_id, created_by) VALUES ($1, $2, $3, 'webhook', $4, $5)`,
		id.String(), orgID.String(), "Inheritance Tool "+id.String(), folderID.String(), owner.String())
	require.NoError(t, err)
	return id
}

// grantFolderACL upserts one folder ACL entry for the given subject.
func grantFolderACL(t *testing.T, s *Server, orgID, folderID uuid.UUID, subjectType, subjectID string, actions []string) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, $3, $4, $5)
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		 DO UPDATE SET actions = EXCLUDED.actions`,
		orgID.String(), folderID.String(), subjectType, subjectID, actions)
	require.NoError(t, err)
}

// requireAction asserts the resolver decision for one resource action.
func requireAction(t *testing.T, s *Server, orgID, user uuid.UUID, want bool, resourceType string, resourceID uuid.UUID, action string) {
	t.Helper()
	allowed, err := s.checkPermission(context.Background(), user.String(), orgID.String(), "editor", resourceType, resourceID.String(), action)
	require.NoError(t, err)
	require.Equal(t, want, allowed, "%s action %q for resource %s by user %s", resourceType, action, resourceID, user)
}

// TestFolderFullGrantCoversEveryNotebookAction pins the reported bug: a
// subject holding the full folder action set can do everything to a notebook
// created inside the folder, including executing its cells.
func TestFolderFullGrantCoversEveryNotebookAction(t *testing.T) {
	s, orgID, owner, member := setupFolderInheritance(t, "full-notebook")
	folderID := insertInheritanceFolder(t, s, orgID, owner, nil, "Full Notebook Folder")
	notebookID := insertInheritanceNotebook(t, s, orgID, owner, folderID)

	grantFolderACL(t, s, orgID, folderID, "user", member.String(), []string{"view", "create", "edit", "manage", "delete"})

	for _, action := range []string{"view", "run", "edit", "share", "delete", "create"} {
		requireAction(t, s, orgID, member, true, "notebook", notebookID, action)
	}
}

// TestFolderFullGrantCoversEveryItemAction pins the same principle for the
// other resource types: connectors and tools inherit use, dashboards inherit
// view_with_data, and access-control actions come through manage.
func TestFolderFullGrantCoversEveryItemAction(t *testing.T) {
	s, orgID, owner, member := setupFolderInheritance(t, "full-items")
	folderID := insertInheritanceFolder(t, s, orgID, owner, nil, "Full Items Folder")
	connID := insertInheritanceConnector(t, s, orgID, owner, folderID)
	dashID := insertInheritanceDashboard(t, s, orgID, owner, folderID)
	toolID := insertInheritanceTool(t, s, orgID, owner, folderID)

	grantFolderACL(t, s, orgID, folderID, "user", member.String(), []string{"view", "create", "edit", "manage", "delete"})

	for _, action := range []string{"view", "use", "edit", "share", "delete"} {
		requireAction(t, s, orgID, member, true, "connector", connID, action)
	}
	for _, action := range []string{"view", "edit", "share", "delete", "view_with_data"} {
		requireAction(t, s, orgID, member, true, "dashboard", dashID, action)
	}
	for _, action := range []string{"view", "use", "edit", "delete"} {
		requireAction(t, s, orgID, member, true, "tool", toolID, action)
	}
}

// TestFolderPartialGrantsStayWithinTheirRung pins that each folder action
// expands exactly its rung and nothing above it.
func TestFolderPartialGrantsStayWithinTheirRung(t *testing.T) {
	s, orgID, owner, member := setupFolderInheritance(t, "partial-rungs")
	folderID := insertInheritanceFolder(t, s, orgID, owner, nil, "Partial Rung Folder")
	notebookID := insertInheritanceNotebook(t, s, orgID, owner, folderID)
	connID := insertInheritanceConnector(t, s, orgID, owner, folderID)
	dashID := insertInheritanceDashboard(t, s, orgID, owner, folderID)

	t.Run("view only", func(t *testing.T) {
		grantFolderACL(t, s, orgID, folderID, "user", member.String(), []string{"view"})
		requireAction(t, s, orgID, member, true, "notebook", notebookID, "view")
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "run")
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "edit")
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "share")
		requireAction(t, s, orgID, member, false, "connector", connID, "use")
		requireAction(t, s, orgID, member, false, "dashboard", dashID, "view_with_data")
	})

	t.Run("create only", func(t *testing.T) {
		grantFolderACL(t, s, orgID, folderID, "user", member.String(), []string{"view", "create"})
		requireAction(t, s, orgID, member, true, "notebook", notebookID, "create")
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "run")
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "edit")
	})

	t.Run("edit only", func(t *testing.T) {
		grantFolderACL(t, s, orgID, folderID, "user", member.String(), []string{"view", "edit"})
		requireAction(t, s, orgID, member, true, "notebook", notebookID, "run")
		requireAction(t, s, orgID, member, true, "notebook", notebookID, "edit")
		requireAction(t, s, orgID, member, true, "connector", connID, "use")
		requireAction(t, s, orgID, member, true, "dashboard", dashID, "view_with_data")
		// Access control and deletion stay out of the edit rung.
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "share")
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "delete")
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "create")
	})

	t.Run("manage only", func(t *testing.T) {
		grantFolderACL(t, s, orgID, folderID, "user", member.String(), []string{"view", "manage"})
		requireAction(t, s, orgID, member, true, "notebook", notebookID, "share")
		requireAction(t, s, orgID, member, true, "connector", connID, "share")
		requireAction(t, s, orgID, member, true, "dashboard", dashID, "share")
		requireAction(t, s, orgID, member, false, "notebook", notebookID, "run")
		requireAction(t, s, orgID, member, false, "connector", connID, "use")
	})
}

// TestFolderExpansionTravelsAncestorsAndSubjects pins that the expansion
// applies through the whole ancestor chain and to group / Everyone entries.
func TestFolderExpansionTravelsAncestorsAndSubjects(t *testing.T) {
	s, orgID, owner, member := setupFolderInheritance(t, "expansion-subjects")
	ctx := context.Background()
	rootID := insertInheritanceFolder(t, s, orgID, owner, nil, "Expansion Root")
	childID := insertInheritanceFolder(t, s, orgID, owner, &rootID, "Expansion Child")
	notebookID := insertInheritanceNotebook(t, s, orgID, owner, childID)

	// A direct user entry on the grandparent folder.
	grantFolderACL(t, s, orgID, rootID, "user", member.String(), []string{"view", "edit"})
	requireAction(t, s, orgID, member, true, "notebook", notebookID, "run")

	// A group entry on the root folder.
	groupID := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
		groupID.String(), orgID.String(), "Expansion Group")
	require.NoError(t, err)
	groupMemberID := insertSessionPermUser(t, s, "group-member")
	addSessionPermMember(t, s, orgID, groupMemberID, "editor")
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		groupID.String(), groupMemberID.String())
	require.NoError(t, err)
	grantFolderACL(t, s, orgID, rootID, "group", groupID.String(), []string{"view", "edit"})
	requireAction(t, s, orgID, groupMemberID, true, "notebook", notebookID, "run")

	// An Everyone entry on the parent folder.
	everyoneMemberID := insertSessionPermUser(t, s, "everyone-member")
	addSessionPermMember(t, s, orgID, everyoneMemberID, "editor")
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, 'folder', $2::uuid, 'org_role', 'everyone', $3)`,
		orgID.String(), childID.String(), []string{"view", "edit"})
	require.NoError(t, err)
	requireAction(t, s, orgID, everyoneMemberID, true, "notebook", notebookID, "run")
}

// TestFolderExpansionIsAncestorOnly pins the boundary: the expansion belongs
// to folder ancestors, so an entry on the notebook itself keeps its exact
// action list while a folder grant still reaches past a restrictive direct
// entry (the resolver is additive).
func TestFolderExpansionIsAncestorOnly(t *testing.T) {
	s, orgID, owner, member := setupFolderInheritance(t, "expansion-boundary")
	folderID := insertInheritanceFolder(t, s, orgID, owner, nil, "Expansion Boundary")
	notebookID := insertInheritanceNotebook(t, s, orgID, owner, folderID)

	// A direct notebook edit grant must not imply run or share.
	grantSessionPermACL(t, s, orgID, "notebook", notebookID, member, []string{"view", "edit"})
	requireAction(t, s, orgID, member, false, "notebook", notebookID, "run")
	requireAction(t, s, orgID, member, false, "notebook", notebookID, "share")

	// The folder grant is additive: it grants run beside the restrictive
	// direct entry, exactly as same-named folder grants already do.
	grantFolderACL(t, s, orgID, folderID, "user", member.String(), []string{"view", "edit"})
	requireAction(t, s, orgID, member, true, "notebook", notebookID, "run")
}

// TestFolderExpansionDoesNotLeakToFolders pins that a folder grant of edit
// cannot fabricate a run action on child folders (folders have no run).
func TestFolderExpansionDoesNotLeakToFolders(t *testing.T) {
	s, orgID, owner, member := setupFolderInheritance(t, "expansion-folder-scope")
	parentID := insertInheritanceFolder(t, s, orgID, owner, nil, "Expansion Folder Parent")
	childID := insertInheritanceFolder(t, s, orgID, owner, &parentID, "Expansion Folder Child")

	grantFolderACL(t, s, orgID, parentID, "user", member.String(), []string{"view", "edit"})
	requireAction(t, s, orgID, member, true, "folder", childID, "view")
	requireAction(t, s, orgID, member, false, "folder", childID, "run")
}
