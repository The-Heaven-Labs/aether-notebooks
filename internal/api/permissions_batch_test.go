package api

// White-box parity coverage for the batched permission resolver. resolveActions
// must return, for every requested resource and action, exactly what
// checkPermission returns — across subjects (direct user, group, Everyone, a
// foreign group), folder depths (resource, parent, grandparent), resource
// types, admin mode on/off, cross-org ids, and empty/unknown/duplicate id
// sets. permIdentity and resolveActions are unexported, so the tests live in
// package api and drive the resolver directly; the database helpers come from
// permissions_internal_test.go and permissions_folder_inheritance_test.go.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// batchResolverFixture is the shared world the parity matrix runs against: one
// org with an owner, an org admin, a member (who belongs to a group), and a
// stranger, plus a root → parent → child folder tree with resources ACL'd at
// every depth and a second org whose resources must never resolve.
type batchResolverFixture struct {
	srv        *Server
	orgID      uuid.UUID
	crossOrgID uuid.UUID

	ownerID        uuid.UUID
	memberID       uuid.UUID
	strangerID     uuid.UUID
	adminID        uuid.UUID
	groupID        uuid.UUID
	foreignGroupID uuid.UUID

	rootID   uuid.UUID
	parentID uuid.UUID
	childID  uuid.UUID

	notebookDirectID     uuid.UUID // direct user view in child
	notebookRootDirectID uuid.UUID // direct user view at root: no ancestor grants apply
	notebookFolderlessID uuid.UUID // folder_id NULL: direct grants only, no ancestry walk
	notebookGroupID      uuid.UUID // direct group edit in parent
	notebookEveryoneID   uuid.UUID // Everyone view at root + restrictive direct user entry
	notebookShareID      uuid.UUID // direct share (implies view) in child
	notebookUnknownID    uuid.UUID // never inserted
	notebookCrossOrgID   uuid.UUID

	connectorID         uuid.UUID // direct user use in child
	connectorUnknownID  uuid.UUID
	connectorCrossOrgID uuid.UUID

	dashboardID         uuid.UUID // no direct ACL: view through the child folder grant
	dashboardUnknownID  uuid.UUID
	dashboardCrossOrgID uuid.UUID

	folderUnknownID  uuid.UUID
	folderCrossOrgID uuid.UUID
}

// batchClaims builds the claims permIdentity reads; groups and admin mode are
// resolved by permIdentity from the database and the context.
func batchClaims(fx *batchResolverFixture, userID uuid.UUID, role string) *auth.Claims {
	return &auth.Claims{UserID: userID.String(), OrgID: fx.orgID.String(), Role: role}
}

// grantBatchACL upserts one ACL entry for any subject on any resource type.
func grantBatchACL(t *testing.T, s *Server, orgID uuid.UUID, resourceType string, resourceID uuid.UUID, subjectType, subjectID string, actions []string) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, $2, $3::uuid, $4, $5, $6)
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		 DO UPDATE SET actions = EXCLUDED.actions`,
		orgID.String(), resourceType, resourceID.String(), subjectType, subjectID, actions)
	require.NoError(t, err)
}

// insertBatchFolderlessNotebook inserts a notebook with folder_id NULL: the
// resolver branch where seed[id] is nil and only direct ACLs apply.
func insertBatchFolderlessNotebook(t *testing.T, s *Server, orgID, owner uuid.UUID, title string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, $3, $4)`,
		id.String(), orgID.String(), title, owner.String())
	require.NoError(t, err)
	return id
}

// setupBatchResolverFixture builds the shared world described on
// batchResolverFixture: one org with four members, a three-deep folder tree,
// resources with direct, group, Everyone, and folder-inherited grants, and a
// second org holding cross-org ids.
func setupBatchResolverFixture(t *testing.T) *batchResolverFixture {
	t.Helper()
	ctx := context.Background()
	fx := &batchResolverFixture{}

	fx.srv = newSessionPermissionTestServer(t)

	fx.orgID = insertSessionPermOrg(t, fx.srv, "batch-parity")
	fx.ownerID = insertSessionPermUser(t, fx.srv, "batch-owner")
	fx.memberID = insertSessionPermUser(t, fx.srv, "batch-member")
	fx.strangerID = insertSessionPermUser(t, fx.srv, "batch-stranger")
	fx.adminID = insertSessionPermUser(t, fx.srv, "batch-admin")
	addSessionPermMember(t, fx.srv, fx.orgID, fx.ownerID, "editor")
	addSessionPermMember(t, fx.srv, fx.orgID, fx.memberID, "editor")
	addSessionPermMember(t, fx.srv, fx.orgID, fx.strangerID, "editor")
	addSessionPermMember(t, fx.srv, fx.orgID, fx.adminID, "admin")

	// The member belongs to one group; a second, foreign group exists so a
	// same-depth non-matching group entry is part of the matrix.
	fx.groupID = uuid.New()
	fx.foreignGroupID = uuid.New()
	for _, groupID := range []uuid.UUID{fx.groupID, fx.foreignGroupID} {
		_, err := fx.srv.db.Pool.Exec(ctx,
			`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
			groupID.String(), fx.orgID.String(), "Batch Group "+groupID.String())
		require.NoError(t, err)
	}
	_, err := fx.srv.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		fx.groupID.String(), fx.memberID.String())
	require.NoError(t, err)

	// Folder tree root → parent → child, with member grants at two depths:
	// child view is depth 0 for the child's items, parent edit is depth 1 (and
	// still a name match for edit at any depth).
	fx.rootID = insertInheritanceFolder(t, fx.srv, fx.orgID, fx.ownerID, nil, "Batch Root")
	fx.parentID = insertInheritanceFolder(t, fx.srv, fx.orgID, fx.ownerID, &fx.rootID, "Batch Parent")
	fx.childID = insertInheritanceFolder(t, fx.srv, fx.orgID, fx.ownerID, &fx.parentID, "Batch Child")
	grantBatchACL(t, fx.srv, fx.orgID, "folder", fx.childID, "user", fx.memberID.String(), []string{"view"})
	grantBatchACL(t, fx.srv, fx.orgID, "folder", fx.parentID, "user", fx.memberID.String(), []string{"edit"})

	// Notebooks: a direct user grant, a group grant, an Everyone grant over a
	// restrictive direct entry (plus a foreign group entry that never matches),
	// a share grant (implies view), and ids that must stay all-false (unknown,
	// cross-org).
	fx.notebookDirectID = insertInheritanceNotebook(t, fx.srv, fx.orgID, fx.ownerID, fx.childID)
	grantBatchACL(t, fx.srv, fx.orgID, "notebook", fx.notebookDirectID, "user", fx.memberID.String(), []string{"view"})

	fx.notebookRootDirectID = insertInheritanceNotebook(t, fx.srv, fx.orgID, fx.ownerID, fx.rootID)
	grantBatchACL(t, fx.srv, fx.orgID, "notebook", fx.notebookRootDirectID, "user", fx.memberID.String(), []string{"view"})

	fx.notebookGroupID = insertInheritanceNotebook(t, fx.srv, fx.orgID, fx.ownerID, fx.parentID)
	grantBatchACL(t, fx.srv, fx.orgID, "notebook", fx.notebookGroupID, "group", fx.groupID.String(), []string{"edit"})

	fx.notebookEveryoneID = insertInheritanceNotebook(t, fx.srv, fx.orgID, fx.ownerID, fx.rootID)
	grantBatchACL(t, fx.srv, fx.orgID, "notebook", fx.notebookEveryoneID, "org_role", "everyone", []string{"view"})
	grantBatchACL(t, fx.srv, fx.orgID, "notebook", fx.notebookEveryoneID, "user", fx.memberID.String(), []string{"nonexistent"})
	grantBatchACL(t, fx.srv, fx.orgID, "notebook", fx.notebookEveryoneID, "group", fx.foreignGroupID.String(), []string{"edit"})

	fx.notebookShareID = insertInheritanceNotebook(t, fx.srv, fx.orgID, fx.ownerID, fx.childID)
	grantBatchACL(t, fx.srv, fx.orgID, "notebook", fx.notebookShareID, "user", fx.memberID.String(), []string{"share"})

	// A notebook with no folder exercises the NULL folder_id branch: no
	// ancestry walk exists, so only the direct entry can apply.
	fx.notebookFolderlessID = insertBatchFolderlessNotebook(t, fx.srv, fx.orgID, fx.ownerID, "Batch Folderless Notebook")
	grantBatchACL(t, fx.srv, fx.orgID, "notebook", fx.notebookFolderlessID, "user", fx.memberID.String(), []string{"view"})

	fx.notebookUnknownID = uuid.New()

	// Connector and dashboard: a direct use grant and a folder-inherited view.
	fx.connectorID = insertInheritanceConnector(t, fx.srv, fx.orgID, fx.ownerID, fx.childID)
	grantBatchACL(t, fx.srv, fx.orgID, "connector", fx.connectorID, "user", fx.memberID.String(), []string{"use"})
	fx.connectorUnknownID = uuid.New()

	fx.dashboardID = insertInheritanceDashboard(t, fx.srv, fx.orgID, fx.ownerID, fx.childID)
	fx.dashboardUnknownID = uuid.New()

	fx.folderUnknownID = uuid.New()

	// Second org: everything here must resolve false for first-org callers,
	// even an org admin in admin mode (ids outside the org are not scoped).
	fx.crossOrgID = insertSessionPermOrg(t, fx.srv, "batch-cross")
	crossOwnerID := insertSessionPermUser(t, fx.srv, "batch-cross-owner")
	crossFolderID := insertInheritanceFolder(t, fx.srv, fx.crossOrgID, crossOwnerID, nil, "Batch Cross Folder")
	fx.folderCrossOrgID = crossFolderID
	fx.notebookCrossOrgID = insertInheritanceNotebook(t, fx.srv, fx.crossOrgID, crossOwnerID, crossFolderID)
	fx.connectorCrossOrgID = insertInheritanceConnector(t, fx.srv, fx.crossOrgID, crossOwnerID, crossFolderID)
	fx.dashboardCrossOrgID = insertInheritanceDashboard(t, fx.srv, fx.crossOrgID, crossOwnerID, crossFolderID)

	return fx
}

// batchResolverMatrix is one resource type's parity matrix: the ids to resolve
// (including unknown, cross-org, and a duplicate id), the actions to compare,
// and the out-of-org ids the admin-mode subtest asserts stay denied.
type batchResolverMatrix struct {
	resourceType string
	ids          []string
	actions      []string
	unknownID    string
	crossOrgID   string
}

// batchResolverMatrices returns the four per-type matrices the fixture covers.
func batchResolverMatrices(fx *batchResolverFixture) []batchResolverMatrix {
	return []batchResolverMatrix{
		{
			resourceType: "notebook",
			ids: []string{
				fx.notebookDirectID.String(),
				fx.notebookRootDirectID.String(),
				fx.notebookFolderlessID.String(),
				fx.notebookGroupID.String(),
				fx.notebookEveryoneID.String(),
				fx.notebookShareID.String(),
				fx.notebookUnknownID.String(),
				fx.notebookCrossOrgID.String(),
				fx.notebookDirectID.String(), // duplicate: deduped internally
			},
			actions:    []string{"view", "edit", "delete", "share", "run", "use"},
			unknownID:  fx.notebookUnknownID.String(),
			crossOrgID: fx.notebookCrossOrgID.String(),
		},
		{
			resourceType: "connector",
			ids: []string{
				fx.connectorID.String(),
				fx.connectorUnknownID.String(),
				fx.connectorCrossOrgID.String(),
			},
			actions:    []string{"view", "edit", "delete", "share", "run", "use"},
			unknownID:  fx.connectorUnknownID.String(),
			crossOrgID: fx.connectorCrossOrgID.String(),
		},
		{
			resourceType: "dashboard",
			ids: []string{
				fx.dashboardID.String(),
				fx.dashboardUnknownID.String(),
				fx.dashboardCrossOrgID.String(),
			},
			actions:    []string{"view", "edit", "delete", "share", "run", "use", "view_with_data"},
			unknownID:  fx.dashboardUnknownID.String(),
			crossOrgID: fx.dashboardCrossOrgID.String(),
		},
		{
			resourceType: "folder",
			ids: []string{
				fx.rootID.String(),
				fx.parentID.String(),
				fx.childID.String(),
				fx.folderUnknownID.String(),
				fx.folderCrossOrgID.String(),
			},
			actions:    []string{"view", "edit", "delete", "share", "run", "use", "create"},
			unknownID:  fx.folderUnknownID.String(),
			crossOrgID: fx.folderCrossOrgID.String(),
		},
	}
}

// requireBatchParity compares resolveActions with checkPermission cell by cell.
// It also pins the contract that the result carries an entry for every
// requested id (deduped), and returns the batched result so callers can add
// explicit expectations.
func requireBatchParity(t *testing.T, ctx context.Context, fx *batchResolverFixture, ident permIdentity, resourceType string, ids, actions []string) map[string]map[string]bool {
	t.Helper()

	got, err := fx.srv.resolveActions(ctx, ident, resourceType, ids, actions)
	require.NoError(t, err)

	distinct := make(map[string]bool, len(ids))
	for _, id := range ids {
		distinct[id] = true
	}
	require.Len(t, got, len(distinct), "the result must carry one entry per distinct requested id")
	for id := range distinct {
		_, present := got[id]
		require.Truef(t, present, "%s %s must have a result entry", resourceType, id)
		for _, action := range actions {
			want, err := fx.srv.checkPermission(ctx, ident.UserID, ident.OrgID, ident.OrgRole, resourceType, id, action)
			require.NoError(t, err)
			require.Equalf(t, want, actionAllowed(got, id, action),
				"%s %s action %q for user %s: resolveActions=%v checkPermission=%v",
				resourceType, id, action, ident.UserID, actionAllowed(got, id, action), want)
		}
	}
	return got
}

func TestResolveActionsParity(t *testing.T) {
	fx := setupBatchResolverFixture(t)
	ctx := context.Background()

	identities := []struct {
		name   string
		claims *auth.Claims
	}{
		{"member", batchClaims(fx, fx.memberID, "editor")},
		{"owner", batchClaims(fx, fx.ownerID, "editor")},
		{"stranger", batchClaims(fx, fx.strangerID, "editor")},
		{"admin-without-admin-mode", batchClaims(fx, fx.adminID, "admin")},
	}

	for _, identity := range identities {
		ident, err := fx.srv.permIdentity(ctx, identity.claims)
		require.NoError(t, err)
		require.Equal(t, identity.claims.UserID, ident.UserID)
		require.Equal(t, identity.claims.OrgID, ident.OrgID)
		require.Equal(t, identity.claims.Role, ident.OrgRole)
		require.False(t, ident.AdminMode, "admin mode must default to off")

		for _, matrix := range batchResolverMatrices(fx) {
			t.Run(identity.name+"/"+matrix.resourceType, func(t *testing.T) {
				requireBatchParity(t, ctx, fx, ident, matrix.resourceType, matrix.ids, matrix.actions)
			})
		}
	}

	// The member's resolved identity carries the explicit group (not the
	// foreign one, which never matches).
	memberIdent, err := fx.srv.permIdentity(ctx, batchClaims(fx, fx.memberID, "editor"))
	require.NoError(t, err)
	require.Contains(t, memberIdent.GroupIDs, fx.groupID.String())
	require.NotContains(t, memberIdent.GroupIDs, fx.foreignGroupID.String())

	// Admin mode on: every org resource passes every requested action, and
	// ids outside the org stay all-false. Both edges are pinned against
	// checkPermission cell by cell, whose bypass compares the resource's org
	// to the caller's.
	adminCtx := executor.WithAdminMode(ctx, true)
	adminIdent, err := fx.srv.permIdentity(adminCtx, batchClaims(fx, fx.adminID, "admin"))
	require.NoError(t, err)
	require.True(t, adminIdent.AdminMode)
	require.Equal(t, "admin", adminIdent.OrgRole)

	for _, matrix := range batchResolverMatrices(fx) {
		matrix := matrix
		t.Run("admin-mode/"+matrix.resourceType, func(t *testing.T) {
			got := requireBatchParity(t, adminCtx, fx, adminIdent, matrix.resourceType, matrix.ids, matrix.actions)
			require.False(t, actionAllowed(got, matrix.unknownID, "view"),
				"admin mode must not fabricate access to unknown ids")
			require.False(t, actionAllowed(got, matrix.crossOrgID, "view"),
				"admin mode must not fabricate access across orgs")
		})
	}
}

// TestResolveActionsSemantics pins a handful of concrete verdicts on top of
// the parity matrix, so a shared regression in both resolvers (which parity
// alone cannot see) still fails here.
func TestResolveActionsSemantics(t *testing.T) {
	fx := setupBatchResolverFixture(t)
	ctx := context.Background()
	ident, err := fx.srv.permIdentity(ctx, batchClaims(fx, fx.memberID, "editor"))
	require.NoError(t, err)

	notebookActions := []string{"view", "edit", "delete", "share", "run", "use"}
	notebooks, err := fx.srv.resolveActions(ctx, ident, "notebook", []string{
		fx.notebookDirectID.String(),
		fx.notebookRootDirectID.String(),
		fx.notebookFolderlessID.String(),
		fx.notebookGroupID.String(),
		fx.notebookEveryoneID.String(),
		fx.notebookShareID.String(),
	}, notebookActions)
	require.NoError(t, err)

	direct := notebooks[fx.notebookDirectID.String()]
	require.True(t, direct["view"])
	require.True(t, direct["run"], "the grandparent folder's edit expands to run at depth 1")
	require.False(t, direct["delete"])
	require.False(t, direct["share"])

	// A direct-only notebook at the root keeps its exact action list: view
	// alone grants nothing else.
	rootDirect := notebooks[fx.notebookRootDirectID.String()]
	require.True(t, rootDirect["view"])
	for _, action := range []string{"edit", "delete", "share", "run", "use"} {
		require.Falsef(t, rootDirect[action], "a direct view grant must not imply %s", action)
	}

	// A folderless notebook (folder_id NULL) has no ancestry to walk: the
	// direct entry is the only source, so it stays view-only too.
	folderless := notebooks[fx.notebookFolderlessID.String()]
	require.True(t, folderless["view"])
	for _, action := range []string{"edit", "delete", "share", "run", "use"} {
		require.Falsef(t, folderless[action], "a folderless notebook must resolve from direct entries only (saw %s)", action)
	}

	group := notebooks[fx.notebookGroupID.String()]
	require.True(t, group["view"], "view is implied by the group's edit")
	require.True(t, group["edit"])
	require.True(t, group["run"], "the notebook's parent folder edit expands to run at depth 0")

	everyone := notebooks[fx.notebookEveryoneID.String()]
	require.True(t, everyone["view"], "the Everyone grant wins over a restrictive direct entry")
	require.False(t, everyone["edit"], "neither the restrictive direct entry nor the foreign group contributes")

	share := notebooks[fx.notebookShareID.String()]
	require.True(t, share["share"])
	require.True(t, share["view"], "share implies view")
	require.False(t, share["delete"])

	folders, err := fx.srv.resolveActions(ctx, ident, "folder", []string{
		fx.rootID.String(), fx.parentID.String(), fx.childID.String(),
	}, []string{"view", "edit", "delete", "share", "run", "use", "create"})
	require.NoError(t, err)
	require.True(t, folders[fx.childID.String()]["view"])
	require.True(t, folders[fx.parentID.String()]["edit"], "the parent folder's direct edit grant applies to itself")
	require.True(t, folders[fx.parentID.String()]["view"], "view is implied by edit")
	require.True(t, folders[fx.childID.String()]["edit"], "the ancestor folder's edit grant is a name match for its children")
	require.False(t, folders[fx.rootID.String()]["view"], "a folder with no entries anywhere stays invisible")
	require.False(t, folders[fx.childID.String()]["run"], "folders are excluded from the expansion rungs")
	require.False(t, folders[fx.childID.String()]["delete"])

	dashboards, err := fx.srv.resolveActions(ctx, ident, "dashboard", []string{
		fx.dashboardID.String(),
	}, []string{"view", "edit", "delete", "share", "run", "use", "view_with_data"})
	require.NoError(t, err)
	dashboard := dashboards[fx.dashboardID.String()]
	require.True(t, dashboard["view"], "the child folder's view reaches the dashboard")
	require.True(t, dashboard["view_with_data"], "the grandparent folder's edit expands to view_with_data at depth 1")
	require.False(t, dashboard["share"], "no folder manage grant exists")

	connectors, err := fx.srv.resolveActions(ctx, ident, "connector", []string{
		fx.connectorID.String(),
	}, []string{"view", "edit", "delete", "share", "run", "use"})
	require.NoError(t, err)
	connector := connectors[fx.connectorID.String()]
	require.True(t, connector["use"])
	require.True(t, connector["view"], "use implies view")
	require.False(t, connector["delete"])

	// The stranger matches no subject anywhere except the Everyone grant,
	// which deliberately reaches every org member by design.
	stranger, err := fx.srv.permIdentity(ctx, batchClaims(fx, fx.strangerID, "editor"))
	require.NoError(t, err)
	for _, matrix := range batchResolverMatrices(fx) {
		got, err := fx.srv.resolveActions(ctx, stranger, matrix.resourceType, matrix.ids, matrix.actions)
		require.NoError(t, err)
		for id, entry := range got {
			for action, allowed := range entry {
				if id == fx.notebookEveryoneID.String() && action == "view" {
					require.True(t, allowed, "the Everyone grant must reach every org member")
					continue
				}
				require.Falsef(t, allowed, "stranger must not hold %s %s %s", matrix.resourceType, id, action)
			}
		}
	}
}

// TestResolveActionsContract pins the resolver's API contract: an entry for
// every requested id (deduped), empty input handling, fail-closed unknown
// resource types, and the admin-mode edges.
func TestResolveActionsContract(t *testing.T) {
	fx := setupBatchResolverFixture(t)
	ctx := context.Background()
	ident, err := fx.srv.permIdentity(ctx, batchClaims(fx, fx.memberID, "editor"))
	require.NoError(t, err)

	t.Run("empty id list returns an empty map", func(t *testing.T) {
		got, err := fx.srv.resolveActions(ctx, ident, "notebook", nil, []string{"view"})
		require.NoError(t, err)
		require.Empty(t, got)

		got, err = fx.srv.resolveActions(ctx, ident, "notebook", []string{}, []string{"view"})
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("unknown and cross-org ids are present and all-false", func(t *testing.T) {
		ids := []string{fx.notebookUnknownID.String(), fx.notebookCrossOrgID.String()}
		got, err := fx.srv.resolveActions(ctx, ident, "notebook", ids, []string{"view", "edit", "share"})
		require.NoError(t, err)
		require.Len(t, got, len(ids))
		for _, id := range ids {
			entry, present := got[id]
			require.Truef(t, present, "id %s must have an entry", id)
			for action, allowed := range entry {
				require.Falsef(t, allowed, "id %s action %s must be false", id, action)
			}
		}
	})

	t.Run("duplicate ids resolve like a deduped list", func(t *testing.T) {
		actions := []string{"view", "edit"}
		deduped, err := fx.srv.resolveActions(ctx, ident, "notebook",
			[]string{fx.notebookDirectID.String()}, actions)
		require.NoError(t, err)
		withDuplicates, err := fx.srv.resolveActions(ctx, ident, "notebook",
			[]string{fx.notebookDirectID.String(), fx.notebookDirectID.String(), fx.notebookUnknownID.String(), fx.notebookDirectID.String()},
			actions)
		require.NoError(t, err)
		require.Equal(t, deduped[fx.notebookDirectID.String()], withDuplicates[fx.notebookDirectID.String()])
		require.Len(t, withDuplicates, 2)
	})

	t.Run("unknown resource type fails closed", func(t *testing.T) {
		_, err := fx.srv.resolveActions(ctx, ident, "widget", []string{fx.notebookDirectID.String()}, []string{"view"})
		require.Error(t, err)
	})

	t.Run("orphaned ACL rows without a resource are ignored", func(t *testing.T) {
		// resource_id carries no foreign key, so a direct ACL row can outlive
		// its resource. checkPermission would consult that row; resolveActions
		// scopes by resource existence and deliberately stays fail-closed.
		orphanID := uuid.New()
		grantBatchACL(t, fx.srv, fx.orgID, "notebook", orphanID, "user", fx.memberID.String(), []string{"view"})

		got, err := fx.srv.resolveActions(ctx, ident, "notebook", []string{orphanID.String()}, []string{"view"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.False(t, actionAllowed(got, orphanID.String(), "view"))
	})

	t.Run("admin mode grants org resources and denies ids outside the org", func(t *testing.T) {
		adminCtx := executor.WithAdminMode(ctx, true)
		adminIdent, err := fx.srv.permIdentity(adminCtx, batchClaims(fx, fx.adminID, "admin"))
		require.NoError(t, err)
		require.True(t, adminIdent.AdminMode)

		actions := []string{"view", "edit", "delete", "share", "run", "use"}
		orgNotebooks := []string{
			fx.notebookDirectID.String(),
			fx.notebookGroupID.String(),
			fx.notebookEveryoneID.String(),
			fx.notebookShareID.String(),
		}
		got, err := fx.srv.resolveActions(adminCtx, adminIdent, "notebook", orgNotebooks, actions)
		require.NoError(t, err)
		for _, id := range orgNotebooks {
			// Admin mode must agree with the single check, not just be true.
			want, err := fx.srv.checkPermission(adminCtx, adminIdent.UserID, adminIdent.OrgID, adminIdent.OrgRole, "notebook", id, "edit")
			require.NoError(t, err)
			require.True(t, want)
			for _, action := range actions {
				require.Truef(t, actionAllowed(got, id, action), "admin mode must grant notebook %s %s", action, id)
			}
		}

		outside, err := fx.srv.resolveActions(adminCtx, adminIdent, "notebook",
			[]string{fx.notebookUnknownID.String(), fx.notebookCrossOrgID.String()}, actions)
		require.NoError(t, err)
		require.False(t, actionAllowed(outside, fx.notebookUnknownID.String(), "view"))
		require.False(t, actionAllowed(outside, fx.notebookCrossOrgID.String(), "view"))

		orgFolders, err := fx.srv.resolveActions(adminCtx, adminIdent, "folder",
			[]string{fx.rootID.String(), fx.parentID.String(), fx.childID.String()}, actions)
		require.NoError(t, err)
		for _, id := range []string{fx.rootID.String(), fx.parentID.String(), fx.childID.String()} {
			require.Truef(t, actionAllowed(orgFolders, id, "view"), "admin mode must grant folder %s", id)
		}
	})
}
