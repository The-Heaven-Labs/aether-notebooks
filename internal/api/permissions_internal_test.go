package api

// White-box tests for the session permission primitive. They live in package
// api because checkSessionPermission and resourceOrgID are unexported; the
// external api_test helpers cannot reach them. The setup mirrors
// testhelpers_test.go:86-103 with the exported constructors and seeds rows
// directly with SQL, using a fresh org per test for isolation.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/database"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// newSessionPermissionTestServer wires a real database, JWT issuer, and audit
// logger into a Server. Redis is deliberately nil: these tests never touch the
// stream or cache paths.
func newSessionPermissionTestServer(t *testing.T) *Server {
	t.Helper()
	dsn := os.Getenv("AETHER_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://aether:aether_dev@localhost:5432/aether?sslmode=disable"
	}
	db, err := database.Connect(context.Background(), dsn, "")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background()))
	srv := NewServer(db,
		auth.NewJWTIssuer("test-secret", 15*time.Minute),
		audit.NewLogger(db),
		crypto.DeriveKey("session-permission-test-master-key"),
		nil,
	)
	t.Cleanup(func() {
		srv.Close()
		db.Close()
	})
	return srv
}

func insertSessionPermOrg(t *testing.T, s *Server, label string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		id.String(), "Session Perm "+label, "session-perm-"+id.String())
	require.NoError(t, err)
	return id
}

func insertSessionPermUser(t *testing.T, s *Server, label string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		id.String(), id.String()+"-"+label+"@example.com", label)
	require.NoError(t, err)
	return id
}

func addSessionPermMember(t *testing.T, s *Server, orgID, userID uuid.UUID, role string) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`,
		orgID.String(), userID.String(), role)
	require.NoError(t, err)
}

// seedSessionPermAgentAndSession inserts an agent and a session owned by
// ownerID, creating and attaching a fresh notebook when withNotebook is true.
// It returns the agent and notebook IDs (notebook is zero when none was
// created) plus the session ID. No ACL entry is written: owner access must come
// from the fallback.
func seedSessionPermAgentAndSession(t *testing.T, s *Server, orgID, ownerID uuid.UUID, withNotebook, inherit bool) (agentID, notebookID, sessionID uuid.UUID) {
	t.Helper()

	var nb *uuid.UUID
	if withNotebook {
		notebookID = uuid.New()
		_, err := s.db.Pool.Exec(context.Background(),
			`INSERT INTO notebooks (id, org_id, title, created_by) VALUES ($1, $2, $3, $4)`,
			notebookID.String(), orgID.String(), "Session Perm Notebook", ownerID.String())
		require.NoError(t, err)
		nb = &notebookID
	}

	agentID, sessionID = seedSessionPermSessionForNotebook(t, s, orgID, ownerID, nb, inherit)
	return agentID, notebookID, sessionID
}

// seedSessionPermSessionForNotebook inserts an agent and a session owned by
// ownerID attached to notebookID (nil leaves the session notebookless), with
// share_with_notebook_viewers set to inherit. It returns both IDs. No ACL entry
// is written: owner access must come from the fallback. Tests use it to attach
// sibling sessions that differ only in the inherit flag to one notebook.
func seedSessionPermSessionForNotebook(t *testing.T, s *Server, orgID, ownerID uuid.UUID, notebookID *uuid.UUID, inherit bool) (agentID, sessionID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	agentID = uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO agents (id, org_id, name, created_by) VALUES ($1, $2, $3, $4)`,
		agentID.String(), orgID.String(), "Session Perm Agent", ownerID.String())
	require.NoError(t, err)

	var nb any
	if notebookID != nil {
		nb = notebookID.String()
	}

	sessionID = uuid.New()
	_, err = s.db.Pool.Exec(ctx,
		`INSERT INTO agent_sessions (id, agent_id, notebook_id, user_id, share_with_notebook_viewers)
		 VALUES ($1, $2, $3, $4, $5)`,
		sessionID.String(), agentID.String(), nb, ownerID.String(), inherit)
	require.NoError(t, err)
	return agentID, sessionID
}

func grantSessionPermACL(t *testing.T, s *Server, orgID uuid.UUID, resourceType string, resourceID, subjectID uuid.UUID, actions []string) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		 VALUES ($1, $2, $3::uuid, 'user', $4, $5)
		 ON CONFLICT (resource_type, resource_id, subject_type, subject_id)
		 DO UPDATE SET actions = EXCLUDED.actions`,
		orgID.String(), resourceType, resourceID.String(), subjectID.String(), actions)
	require.NoError(t, err)
}

func revokeSessionPermACL(t *testing.T, s *Server, resourceType string, resourceID, subjectID uuid.UUID) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(),
		`DELETE FROM acl_entries
		 WHERE resource_type = $1 AND resource_id = $2::uuid AND subject_type = 'user' AND subject_id = $3`,
		resourceType, resourceID.String(), subjectID.String())
	require.NoError(t, err)
}

func TestCheckSessionPermissionOwnerFallback(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "owner-fallback")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	_, _, sessionID := seedSessionPermAgentAndSession(t, s, orgID, ownerID, true, false)

	// No owner ACL entry exists (the session was inserted after migration);
	// the fallback alone must grant every action.
	var aclCount int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM acl_entries WHERE resource_type = 'agent_session' AND resource_id = $1`,
		sessionID.String()).Scan(&aclCount))
	require.Zero(t, aclCount)

	for _, action := range []string{"view", "edit", "share", "delete", "admin"} {
		allowed, err := s.checkSessionPermission(ctx, ownerID.String(), orgID.String(), "editor", sessionID.String(), action)
		require.NoError(t, err)
		require.True(t, allowed, "owner fallback should grant %q with no ACL row", action)
	}
}

func TestCheckSessionPermissionAdminModeBypass(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "admin-bypass")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	adminID := insertSessionPermUser(t, s, "admin")
	addSessionPermMember(t, s, orgID, adminID, "admin")
	_, _, sessionID := seedSessionPermAgentAndSession(t, s, orgID, ownerID, true, false)

	// A foreign session with no ACL: an org admin without admin mode is just
	// another member.
	allowed, err := s.checkSessionPermission(ctx, adminID.String(), orgID.String(), "admin", sessionID.String(), "view")
	require.NoError(t, err)
	require.False(t, allowed, "org admin without admin mode must not view a foreign session")

	allowed, err = s.checkSessionPermission(ctx, adminID.String(), orgID.String(), "admin", sessionID.String(), "edit")
	require.NoError(t, err)
	require.False(t, allowed, "org admin without admin mode must not edit a foreign session")

	// Admin mode unlocks the ACL bypass for the agent_session resource type.
	adminCtx := executor.WithAdminMode(ctx, true)
	for _, action := range []string{"view", "edit", "share", "delete"} {
		allowed, err = s.checkSessionPermission(adminCtx, adminID.String(), orgID.String(), "admin", sessionID.String(), action)
		require.NoError(t, err)
		require.True(t, allowed, "org admin in admin mode should pass %q", action)
	}
}

func TestCheckSessionPermissionNotebookInheritance(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "inheritance")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	viewerID := insertSessionPermUser(t, s, "viewer")
	addSessionPermMember(t, s, orgID, viewerID, "editor")

	_, notebookID, sessionID := seedSessionPermAgentAndSession(t, s, orgID, ownerID, true, true)

	// Without notebook view the inherit flag grants nothing.
	allowed, err := s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", sessionID.String(), "view")
	require.NoError(t, err)
	require.False(t, allowed, "inherit flag alone must not grant view")

	grantSessionPermACL(t, s, orgID, "notebook", notebookID, viewerID, []string{"view"})

	allowed, err = s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", sessionID.String(), "view")
	require.NoError(t, err)
	require.True(t, allowed, "notebook viewer should inherit session view")

	// Inheritance is view-only.
	allowed, err = s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", sessionID.String(), "edit")
	require.NoError(t, err)
	require.False(t, allowed, "notebook inheritance must never grant edit")

	// Revocation is live: dropping the notebook grant denies immediately.
	revokeSessionPermACL(t, s, "notebook", notebookID, viewerID)
	allowed, err = s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", sessionID.String(), "view")
	require.NoError(t, err)
	require.False(t, allowed, "removing the notebook grant must revoke inheritance immediately")

	// The inherit flag is the only difference between the next two sessions:
	// both hang off notebookID, which the viewer can view. The flagged session
	// is visible through inheritance, while flipping the flag off must deny
	// that same notebook viewer with no other variable changed.
	grantSessionPermACL(t, s, orgID, "notebook", notebookID, viewerID, []string{"view"})
	_, flaggedSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, &notebookID, true)
	allowed, err = s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", flaggedSessionID.String(), "view")
	require.NoError(t, err)
	require.True(t, allowed, "session with the inherit flag on must be visible to notebook viewers")

	_, flaglessSessionID := seedSessionPermSessionForNotebook(t, s, orgID, ownerID, &notebookID, false)
	allowed, err = s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", flaglessSessionID.String(), "view")
	require.NoError(t, err)
	require.False(t, allowed, "session with the inherit flag off must not be visible to notebook viewers")

	// The flag alone is not enough: without a notebook there is nothing to
	// inherit from.
	_, _, notebooklessSessionID := seedSessionPermAgentAndSession(t, s, orgID, ownerID, false, true)
	allowed, err = s.checkSessionPermission(ctx, viewerID.String(), orgID.String(), "editor", notebooklessSessionID.String(), "view")
	require.NoError(t, err)
	require.False(t, allowed, "inherit flag without a notebook must not grant view")
}

func TestCheckSessionPermissionDirectACLIsReadOnly(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "direct-acl")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")
	otherID := insertSessionPermUser(t, s, "other")
	addSessionPermMember(t, s, orgID, otherID, "editor")
	adminID := insertSessionPermUser(t, s, "admin")
	addSessionPermMember(t, s, orgID, adminID, "admin")
	_, _, sessionID := seedSessionPermAgentAndSession(t, s, orgID, ownerID, false, false)

	// A direct view share grants view.
	grantSessionPermACL(t, s, orgID, "agent_session", sessionID, otherID, []string{"view"})
	allowed, err := s.checkSessionPermission(ctx, otherID.String(), orgID.String(), "editor", sessionID.String(), "view")
	require.NoError(t, err)
	require.True(t, allowed, "direct agent_session view share should pass")

	// Non-owner sessions are read-only: an edit grant on a non-owner subject
	// is ignored by the permission check.
	grantSessionPermACL(t, s, orgID, "agent_session", sessionID, otherID, []string{"view", "edit"})
	allowed, err = s.checkSessionPermission(ctx, otherID.String(), orgID.String(), "editor", sessionID.String(), "edit")
	require.NoError(t, err)
	require.False(t, allowed, "edit granted to a non-owner must be ignored")

	// ...unless the caller is an org admin in admin mode.
	adminCtx := executor.WithAdminMode(ctx, true)
	allowed, err = s.checkSessionPermission(adminCtx, adminID.String(), orgID.String(), "admin", sessionID.String(), "edit")
	require.NoError(t, err)
	require.True(t, allowed, "admin mode should grant edit to a non-owner")
}

func TestCheckSessionPermissionMissingSession(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "missing")
	userID := insertSessionPermUser(t, s, "user")
	addSessionPermMember(t, s, orgID, userID, "editor")

	allowed, err := s.checkSessionPermission(ctx, userID.String(), orgID.String(), "editor", uuid.New().String(), "view")
	require.NoError(t, err)
	require.False(t, allowed, "an unknown session must deny without an error")
}

func TestResourceOrgIDResolvesFolderNotebookAndSession(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()
	orgID := insertSessionPermOrg(t, s, "resource-org")
	ownerID := insertSessionPermUser(t, s, "owner")
	addSessionPermMember(t, s, orgID, ownerID, "editor")

	folderID := uuid.New()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO folders (id, org_id, name, created_by) VALUES ($1, $2, $3, $4)`,
		folderID.String(), orgID.String(), "Session Perm Folder", ownerID.String())
	require.NoError(t, err)

	_, notebookID, sessionID := seedSessionPermAgentAndSession(t, s, orgID, ownerID, true, false)

	got, err := s.resourceOrgID(ctx, "folder", folderID.String())
	require.NoError(t, err)
	require.Equal(t, orgID.String(), got)

	got, err = s.resourceOrgID(ctx, "notebook", notebookID.String())
	require.NoError(t, err)
	require.Equal(t, orgID.String(), got)

	got, err = s.resourceOrgID(ctx, "agent_session", sessionID.String())
	require.NoError(t, err)
	require.Equal(t, orgID.String(), got)
}

func TestResourceOrgIDUnknownTypeErrors(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()

	_, err := s.resourceOrgID(ctx, "widget", uuid.New().String())
	require.Error(t, err, "unknown resource types must fail closed")
}

func TestResourceOrgIDMissingResourceIsEmpty(t *testing.T) {
	s := newSessionPermissionTestServer(t)
	ctx := context.Background()

	got, err := s.resourceOrgID(ctx, "notebook", uuid.New().String())
	require.NoError(t, err)
	require.Empty(t, got)

	got, err = s.resourceOrgID(ctx, "agent_session", uuid.New().String())
	require.NoError(t, err)
	require.Empty(t, got)
}
