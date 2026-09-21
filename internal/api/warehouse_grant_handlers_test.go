package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
)

// seedGrantOrgMember adds a non-admin member to an existing org and returns
// the user ID plus a token. Grants name subjects by canonical UUID, so tests
// need the ID, not just the token.
func seedGrantOrgMember(t *testing.T, s *Server, orgID uuid.UUID, role string) (uuid.UUID, string) {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()
	userID := uuid.New()

	_, err := s.db.Pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), "wh-grant-"+suffix+"@test.local", "Grant Member")
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`,
		orgID.String(), userID.String(), role)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID.String()); err != nil {
			t.Logf("cleanup grant member: %v", err)
		}
	})

	token, err := s.jwt.Issue(userID.String(), orgID.String(), role)
	require.NoError(t, err)
	return userID, token
}

// seedGrantGroup creates an org group with the given members. The org cleanup
// cascades the group and its membership rows.
func seedGrantGroup(t *testing.T, s *Server, orgID uuid.UUID, name string, members ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	groupID := uuid.New()

	_, err := s.db.Pool.Exec(ctx, `INSERT INTO groups (id, org_id, name) VALUES ($1, $2, $3)`,
		groupID.String(), orgID.String(), name)
	require.NoError(t, err)
	for _, member := range members {
		_, err = s.db.Pool.Exec(ctx, `INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
			groupID.String(), member.String())
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM groups WHERE id = $1`, groupID.String()); err != nil {
			t.Logf("cleanup grant group: %v", err)
		}
	})
	return groupID
}

// seedGrantEveryoneGroup creates the org's implicit Everyone group. Orgs
// seeded directly via SQL (as the test fixtures do) do not get one, but
// permissions.go treats every member as belonging to it.
func seedGrantEveryoneGroup(t *testing.T, s *Server, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	groupID := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO groups (id, org_id, name) VALUES ($1, $2, 'Everyone')`,
		groupID.String(), orgID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM groups WHERE id = $1`, groupID.String()); err != nil {
			t.Logf("cleanup everyone group: %v", err)
		}
	})
	return groupID
}

func grantBody(subjectType, subjectID, database, table string) map[string]any {
	return map[string]any{
		"subject_type": subjectType,
		"subject_id":   subjectID,
		"database":     database,
		"table":        table,
	}
}

func createGrantViaAPI(t *testing.T, s *Server, token string, warehouseID uuid.UUID, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return warehouseAPIRequest(t, s, http.MethodPost,
		"/api/v1/warehouses/"+warehouseID.String()+"/grants", token, body)
}

func listGrantsViaAPI(t *testing.T, s *Server, token string, warehouseID uuid.UUID) []warehouseGrantJSON {
	t.Helper()
	rec := warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+warehouseID.String()+"/grants", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var grants []warehouseGrantJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &grants))
	return grants
}

func deleteGrantViaAPI(t *testing.T, s *Server, token string, warehouseID, grantID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	return warehouseAPIRequest(t, s, http.MethodDelete,
		"/api/v1/warehouses/"+warehouseID.String()+"/grants/"+grantID.String(), token, nil)
}

func effectiveAccessViaAPI(t *testing.T, s *Server, token string, warehouseID uuid.UUID, query string) *httptest.ResponseRecorder {
	t.Helper()
	return warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+warehouseID.String()+"/effective-access"+query, token, nil)
}

func decodeEffectiveAccess(t *testing.T, rec *httptest.ResponseRecorder) warehouseEffectiveAccessJSON {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var access warehouseEffectiveAccessJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &access))
	return access
}

// putPreferenceViaAPI sends a preference for a connector UUID, or null to
// clear when connectorID is nil.
func putPreferenceViaAPI(t *testing.T, s *Server, token string, warehouseID uuid.UUID, connectorID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	var value any
	if connectorID != nil {
		value = connectorID.String()
	}
	return warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+warehouseID.String()+"/preference", token,
		map[string]any{"connector_id": value})
}

func warehousePreference(t *testing.T, s *Server, userID, warehouseID uuid.UUID) *string {
	t.Helper()
	var connectorID *string
	err := s.db.Pool.QueryRow(context.Background(), `
		SELECT connector_id FROM warehouse_service_preferences
		WHERE user_id = $1 AND warehouse_id = $2`,
		userID.String(), warehouseID.String()).Scan(&connectorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	require.NoError(t, err)
	return connectorID
}

// warehouseGrantAuditMeta returns the newest audit metadata for a grant
// mutation, keyed by the grant UUID embedded in the metadata.
func warehouseGrantAuditMeta(t *testing.T, s *Server, action, grantID string) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, s.db.Pool.QueryRow(context.Background(), `
		SELECT metadata FROM audit_logs
		WHERE action = $1 AND metadata->>'grant_id' = $2
		ORDER BY id DESC LIMIT 1`, action, grantID).Scan(&raw))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	return meta
}

func TestGrantCRUDAndEffectiveAccess(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Grant CRUD WH")

	connA := seedWarehouseConnector(t, s, orgID, "Grant Service A")
	connB := seedWarehouseConnector(t, s, orgID, "Grant Service B")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connA, &wh).Code)
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connB, &wh).Code)

	memberID, member := seedGrantOrgMember(t, s, orgID, "non-admin")
	groupID := seedGrantGroup(t, s, orgID, "Grant Analysts", memberID)

	directBody := grantBody("user", memberID.String(), "analytics", "events")

	// A direct grant to a subject with no service access is saved with a
	// warning, and the mutation reaches the sync scheduler and the audit log.
	rec.reset()
	directRec := createGrantViaAPI(t, s, admin, wh, directBody)
	require.Equal(t, http.StatusCreated, directRec.Code, directRec.Body.String())
	var direct warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(directRec.Body.Bytes(), &direct))
	require.Equal(t, "no_service_access", direct.Warning)
	require.NotEmpty(t, direct.ID)
	require.Equal(t, wh.String(), direct.WarehouseID)
	require.Equal(t, orgID.String(), direct.OrgID)
	require.Equal(t, "user", direct.SubjectType)
	require.Equal(t, memberID.String(), direct.SubjectID)
	require.Equal(t, "analytics", direct.Database)
	require.Equal(t, "events", direct.Table)
	require.NotNil(t, direct.CreatedBy)
	require.Equal(t, 1, countWarehouseGrants(t, s, wh))
	require.True(t, rec.contains(wh), "a committed grant must enqueue a sync")

	createMeta := warehouseGrantAuditMeta(t, s, "warehouse.grant.create", direct.ID)
	require.Equal(t, "user", createMeta["subject_type"])
	require.Equal(t, memberID.String(), createMeta["subject_id"])
	require.Equal(t, "analytics", createMeta["database"])
	require.Equal(t, "events", createMeta["table"])

	// Group and everyone subjects round-trip with the canonical subject IDs.
	groupRec := createGrantViaAPI(t, s, admin, wh, grantBody("group", groupID.String(), "analytics", "clicks"))
	require.Equal(t, http.StatusCreated, groupRec.Code, groupRec.Body.String())
	var groupGrant warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(groupRec.Body.Bytes(), &groupGrant))
	require.Equal(t, "no_service_access", groupGrant.Warning)

	everyoneRec := createGrantViaAPI(t, s, admin, wh, grantBody("everyone", "", "public", "reference"))
	require.Equal(t, http.StatusCreated, everyoneRec.Code, everyoneRec.Body.String())
	var everyoneGrant warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(everyoneRec.Body.Bytes(), &everyoneGrant))
	require.Equal(t, "everyone", everyoneGrant.SubjectType)
	require.Equal(t, "everyone", everyoneGrant.SubjectID)
	require.Equal(t, "Everyone", everyoneGrant.SubjectName)
	require.Equal(t, 3, countWarehouseGrants(t, s, wh))

	// A replayed duplicate is idempotent: 200, same row, no extra grant.
	dupRec := createGrantViaAPI(t, s, admin, wh, directBody)
	require.Equal(t, http.StatusOK, dupRec.Code, dupRec.Body.String())
	var dup warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(dupRec.Body.Bytes(), &dup))
	require.Equal(t, direct.ID, dup.ID)
	require.Equal(t, 3, countWarehouseGrants(t, s, wh))

	// Once the member's group may use a service, the warning clears.
	grantGroupConnectorUse(t, s, orgID, groupID, connA)
	afterUseRec := createGrantViaAPI(t, s, admin, wh, directBody)
	require.Equal(t, http.StatusOK, afterUseRec.Code, afterUseRec.Body.String())
	var afterUse warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(afterUseRec.Body.Bytes(), &afterUse))
	require.Empty(t, afterUse.Warning)

	// The list resolves subject labels without exposing anything else.
	grants := listGrantsViaAPI(t, s, admin, wh)
	require.Len(t, grants, 3)
	byType := map[string]warehouseGrantJSON{}
	for _, g := range grants {
		byType[g.SubjectType] = g
	}
	require.Equal(t, "Grant Member", byType["user"].SubjectName)
	require.Equal(t, memberID.String(), byType["user"].SubjectID)
	require.Equal(t, "Grant Analysts", byType["group"].SubjectName)
	require.Equal(t, groupID.String(), byType["group"].SubjectID)
	require.Equal(t, "Everyone", byType["everyone"].SubjectName)

	// Effective access: union of direct + group + everyone, with the CH
	// identities the sync worker provisions.
	ea := decodeEffectiveAccess(t, effectiveAccessViaAPI(t, s, admin, wh, "?user_id="+memberID.String()))
	require.Equal(t, memberID.String(), ea.UserID)
	require.Equal(t, wh.String(), ea.WarehouseID)
	require.Equal(t, chaccess.UserIdent(wh, orgID, memberID), ea.CHUser)
	require.ElementsMatch(t, []string{
		chaccess.RoleIdent(wh, orgID, groupID),
		chaccess.EveryoneRole(wh),
	}, ea.CHRoles)
	require.ElementsMatch(t, []warehouseEffectiveTableJSON{
		{Database: "analytics", Table: "events"},
		{Database: "analytics", Table: "clicks"},
		{Database: "public", Table: "reference"},
	}, ea.Tables)
	require.Len(t, ea.Services, 1)
	require.Equal(t, connA.String(), ea.Services[0].ConnectorID)
	require.False(t, ea.Services[0].Preferred)
	require.Nil(t, ea.PreferredConnectorID)

	// A member may inspect themselves (with or without an explicit ID) but no
	// one else.
	selfEA := decodeEffectiveAccess(t, effectiveAccessViaAPI(t, s, member, wh, ""))
	require.ElementsMatch(t, ea.Tables, selfEA.Tables)
	require.ElementsMatch(t, ea.CHRoles, selfEA.CHRoles)
	require.Equal(t,
		http.StatusForbidden,
		effectiveAccessViaAPI(t, s, member, wh, "?user_id="+uuid.NewString()).Code)

	// Deleting a grant removes exactly that row and enqueues a reconcile.
	rec.reset()
	delRec := deleteGrantViaAPI(t, s, admin, wh, uuid.MustParse(groupGrant.ID))
	require.Equal(t, http.StatusNoContent, delRec.Code, delRec.Body.String())
	require.Equal(t, 2, countWarehouseGrants(t, s, wh))
	require.True(t, rec.contains(wh), "a delete must enqueue a sync")

	deleteMeta := warehouseGrantAuditMeta(t, s, "warehouse.grant.delete", groupGrant.ID)
	require.Equal(t, "group", deleteMeta["subject_type"])
	require.Equal(t, groupID.String(), deleteMeta["subject_id"])
	require.Equal(t, "analytics", deleteMeta["database"])
	require.Equal(t, "clicks", deleteMeta["table"])

	// Replays and cross-warehouse IDs are 404, never a cross-warehouse delete.
	require.Equal(t, http.StatusNotFound, deleteGrantViaAPI(t, s, admin, wh, uuid.MustParse(groupGrant.ID)).Code)
	whOther := createWarehouseViaAPI(t, s, admin, "Grant CRUD WH Other")
	require.Equal(t, http.StatusNotFound, deleteGrantViaAPI(t, s, admin, whOther, uuid.MustParse(direct.ID)).Code)
	require.Equal(t, 2, countWarehouseGrants(t, s, wh))

	// Malformed IDs are not found, never a cast error.
	require.Equal(t, http.StatusNotFound, warehouseAPIRequest(t, s, http.MethodDelete,
		"/api/v1/warehouses/"+wh.String()+"/grants/not-a-uuid", admin, nil).Code)
	require.Equal(t, http.StatusNotFound, warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/not-a-uuid/grants", admin, nil).Code)
}

func TestWarehouseGrantValidation(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Grant Validation WH")
	memberID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")

	orgB, _, _ := seedWarehouseOrgAdmin(t, s)
	foreignUser, _ := seedGrantOrgMember(t, s, orgB, "non-admin")
	foreignGroup := seedGrantGroup(t, s, orgB, "Foreign Group")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"wildcard table", grantBody("user", memberID.String(), "analytics", "*")},
		{"wildcard table prefix", grantBody("user", memberID.String(), "analytics", "events*")},
		{"wildcard database", grantBody("user", memberID.String(), "analytics*", "events")},
		{"qualified wildcard", grantBody("user", memberID.String(), "analytics", "db.*")},
		{"blank table", grantBody("user", memberID.String(), "analytics", "")},
		{"blank database", grantBody("user", memberID.String(), "", "events")},
		{"unknown subject type", grantBody("org_role", "everyone", "analytics", "events")},
		{"malformed user subject", grantBody("user", "not-a-uuid", "analytics", "events")},
		{"unknown user subject", grantBody("user", uuid.NewString(), "analytics", "events")},
		{"cross-org user subject", grantBody("user", foreignUser.String(), "analytics", "events")},
		{"malformed group subject", grantBody("group", "not-a-uuid", "analytics", "events")},
		{"unknown group subject", grantBody("group", uuid.NewString(), "analytics", "events")},
		{"cross-org group subject", grantBody("group", foreignGroup.String(), "analytics", "events")},
		{"everyone with foreign subject id", grantBody("everyone", memberID.String(), "analytics", "events")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := createGrantViaAPI(t, s, admin, wh, tc.body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Zero(t, countWarehouseGrants(t, s, wh), "a rejected grant must not be stored")
		})
	}

	// The whitelist accepts the catalog charset it was built for: dots,
	// hyphens, dollar signs, and mixed case are not wildcards.
	okRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("user", memberID.String(), "my-db.v2", "Events.2024-Q1$"))
	require.Equal(t, http.StatusCreated, okRec.Code, okRec.Body.String())
	require.Equal(t, 1, countWarehouseGrants(t, s, wh), "only the accepted grant exists")
}

// TestWarehouseGrantWarningHonorsFolderServiceAccess pins the warning check to
// the same ACL ancestor walk execution uses: `use` granted on a connector's
// folder must clear the warning for the group and its members.
func TestWarehouseGrantWarningHonorsFolderServiceAccess(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, adminID, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Grant Folder Warning WH")
	conn := seedWarehouseConnector(t, s, orgID, "Grant Folder Service")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)
	memberID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")
	groupID := seedGrantGroup(t, s, orgID, "Grant Folder Group", memberID)

	folderID := uuid.New()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO folders (id, org_id, name, created_by) VALUES ($1, $2, $3, $4)`,
		folderID.String(), orgID.String(), "Grant Folder", adminID.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM folders WHERE id = $1`, folderID.String()); err != nil {
			t.Logf("cleanup folder: %v", err)
		}
	})
	_, err = s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET folder_id = $1 WHERE id = $2`, folderID.String(), conn.String())
	require.NoError(t, err)

	userBody := grantBody("user", memberID.String(), "analytics", "events")
	beforeUser := createGrantViaAPI(t, s, admin, wh, userBody)
	require.Equal(t, http.StatusCreated, beforeUser.Code, beforeUser.Body.String())
	var beforeUserGrant warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(beforeUser.Body.Bytes(), &beforeUserGrant))
	require.Equal(t, "no_service_access", beforeUserGrant.Warning)

	groupBody := grantBody("group", groupID.String(), "analytics", "clicks")
	beforeGroup := createGrantViaAPI(t, s, admin, wh, groupBody)
	require.Equal(t, http.StatusCreated, beforeGroup.Code, beforeGroup.Body.String())
	var beforeGroupGrant warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(beforeGroup.Body.Bytes(), &beforeGroupGrant))
	require.Equal(t, "no_service_access", beforeGroupGrant.Warning)

	// A `use` ACL on the connector's folder reaches both the group subject and
	// (through membership) the user subject.
	_, err = s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'folder', $2::uuid, 'group', $3, ARRAY['use'])`,
		orgID.String(), folderID.String(), groupID.String())
	require.NoError(t, err)

	afterUser := createGrantViaAPI(t, s, admin, wh, userBody)
	require.Equal(t, http.StatusOK, afterUser.Code, afterUser.Body.String())
	var afterUserGrant warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(afterUser.Body.Bytes(), &afterUserGrant))
	require.Empty(t, afterUserGrant.Warning, "folder-inherited `use` must clear the user warning")

	afterGroup := createGrantViaAPI(t, s, admin, wh, groupBody)
	require.Equal(t, http.StatusOK, afterGroup.Code, afterGroup.Body.String())
	var afterGroupGrant warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(afterGroup.Body.Bytes(), &afterGroupGrant))
	require.Empty(t, afterGroupGrant.Warning, "folder-inherited `use` must clear the group warning")
}

// TestWarehouseGrantWarningHonorsEveryoneGroupServiceAccess pins the warning
// check to permissions.go's implicit Everyone membership: a `use` ACL on the
// org's Everyone group must clear the warning for user, group, and everyone
// grant subjects alike.
func TestWarehouseGrantWarningHonorsEveryoneGroupServiceAccess(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Grant Everyone Warning WH")
	conn := seedWarehouseConnector(t, s, orgID, "Grant Everyone Service")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)
	memberID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")
	groupID := seedGrantGroup(t, s, orgID, "Grant Everyone Group", memberID)
	everyoneID := seedGrantEveryoneGroup(t, s, orgID)

	subjects := []struct {
		subjectType string
		subjectID   string
	}{
		{"user", memberID.String()},
		{"group", groupID.String()},
		{"everyone", ""},
	}
	for _, subject := range subjects {
		rec := createGrantViaAPI(t, s, admin, wh,
			grantBody(subject.subjectType, subject.subjectID, "analytics", "events"))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var created warehouseGrantCreateJSON
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		require.Equal(t, "no_service_access", created.Warning, "subject_type=%s", subject.subjectType)
	}

	// `use` on the implicit Everyone group reaches every member, so it must
	// clear the warning for all three subject forms.
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'connector', $2::uuid, 'group', $3, ARRAY['use'])`,
		orgID.String(), conn.String(), everyoneID.String())
	require.NoError(t, err)

	for _, subject := range subjects {
		rec := createGrantViaAPI(t, s, admin, wh,
			grantBody(subject.subjectType, subject.subjectID, "analytics", "events"))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var replayed warehouseGrantCreateJSON
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &replayed))
		require.Empty(t, replayed.Warning,
			"Everyone-group `use` must clear the warning for subject_type=%s", subject.subjectType)
	}
}

// TestWarehouseGrantEffectiveAccessZeroGrantShape pins the response contract
// for a member with no effective grants: no ClickHouse identity fields (they
// are not provisioned), empty unions, and a null preference.
func TestWarehouseGrantEffectiveAccessZeroGrantShape(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Grant Zero Shape WH")
	conn := seedWarehouseConnector(t, s, orgID, "Grant Zero Shape Service")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)
	_, member := seedGrantOrgMember(t, s, orgID, "non-admin")

	rec := effectiveAccessViaAPI(t, s, member, wh, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	require.NotContains(t, raw, "ch_user", "a zero-grant subject must not look provisioned")
	require.NotContains(t, raw, "ch_roles")
	require.JSONEq(t, `[]`, string(raw["tables"]))
	require.JSONEq(t, `[]`, string(raw["services"]))
	require.JSONEq(t, `null`, string(raw["preferred_connector_id"]))

	var access warehouseEffectiveAccessJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &access))
	require.Empty(t, access.CHUser)
	require.Empty(t, access.CHRoles)
}

// TestWarehouseGrantEffectiveAccessIgnoresStalePreference covers a stored
// preference whose connector is no longer an allowed service: routing already
// ignores it, so effective access must not report it either.
func TestWarehouseGrantEffectiveAccessIgnoresStalePreference(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Grant Stale Preference WH")
	connA := seedWarehouseConnector(t, s, orgID, "Grant Stale Service A")
	connB := seedWarehouseConnector(t, s, orgID, "Grant Stale Service B")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connA, &wh).Code)
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connB, &wh).Code)

	memberID, member := seedGrantOrgMember(t, s, orgID, "non-admin")
	grantConnectorUse(t, s, orgID, memberID, connA)
	grantConnectorUse(t, s, orgID, memberID, connB)
	require.Equal(t, http.StatusOK, putPreferenceViaAPI(t, s, member, wh, &connA).Code)

	// Revoke the preferred service without the handler's preference cleanup:
	// the row stays behind as a stale preference.
	_, err := s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET deleted_at = now() WHERE id = $1`, connA.String())
	require.NoError(t, err)
	require.Equal(t, connA.String(), *warehousePreference(t, s, memberID, wh))

	access := decodeEffectiveAccess(t, effectiveAccessViaAPI(t, s, member, wh, ""))
	require.Nil(t, access.PreferredConnectorID, "a stale preference must not be reported")
	require.Len(t, access.Services, 1)
	require.Equal(t, connB.String(), access.Services[0].ConnectorID)
	require.False(t, access.Services[0].Preferred)
}

// TestWarehouseGrantForeignKeyViolationMapping pins the race guard: a grant
// INSERT whose warehouse vanished mid-request fails the composite FK
// (SQLSTATE 23503) and must map to the same 404 as the existence pre-check.
func TestWarehouseGrantForeignKeyViolationMapping(t *testing.T) {
	require.True(t, isForeignKeyViolation(&pgconn.PgError{Code: "23503"}))
	require.False(t, isForeignKeyViolation(&pgconn.PgError{Code: "23505"}))
	require.False(t, isForeignKeyViolation(errors.New("not a pg error")))
}

func TestWarehouseGrantCrossOrgIsolation(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, adminA := seedWarehouseOrgAdmin(t, s)
	orgB, _, adminB := seedWarehouseOrgAdmin(t, s)

	whA := createWarehouseViaAPI(t, s, adminA, "Grant Isolation WH A")
	whB := createWarehouseViaAPI(t, s, adminB, "Grant Isolation WH B")
	memberB, _ := seedGrantOrgMember(t, s, orgB, "non-admin")
	groupB := seedGrantGroup(t, s, orgB, "Grant Isolation Group B")

	grantRec := createGrantViaAPI(t, s, adminA, whA, grantBody("user", memberB.String(), "analytics", "events"))
	require.Equal(t, http.StatusBadRequest, grantRec.Code, grantRec.Body.String())
	groupRec := createGrantViaAPI(t, s, adminA, whA, grantBody("group", groupB.String(), "analytics", "events"))
	require.Equal(t, http.StatusBadRequest, groupRec.Code, groupRec.Body.String())
	require.Zero(t, countWarehouseGrants(t, s, whA))

	// A foreign warehouse is invisible to every grant route.
	require.Equal(t, http.StatusNotFound, warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+whB.String()+"/grants", adminA, nil).Code)
	require.Equal(t, http.StatusNotFound, createGrantViaAPI(t, s, adminA, whB,
		grantBody("user", memberB.String(), "analytics", "events")).Code)
	require.Equal(t, http.StatusNotFound, deleteGrantViaAPI(t, s, adminA, whB, uuid.New()).Code)
	require.Equal(t, http.StatusNotFound, effectiveAccessViaAPI(t, s, adminA, whB, "").Code)
	require.Equal(t, http.StatusNotFound, putPreferenceViaAPI(t, s, adminA, whB, nil).Code)

	// A user outside the warehouse's org has no effective access to report.
	require.Equal(t, http.StatusNotFound,
		effectiveAccessViaAPI(t, s, adminA, whA, "?user_id="+memberB.String()).Code)
}

func TestWarehouseGrantRoutesRequireOrgAdmin(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Grant Guard WH")
	memberID, member := seedGrantOrgMember(t, s, orgID, "non-admin")
	conn := seedWarehouseConnector(t, s, orgID, "Grant Guard Service")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)

	grantRec := createGrantViaAPI(t, s, admin, wh, grantBody("user", memberID.String(), "analytics", "events"))
	require.Equal(t, http.StatusCreated, grantRec.Code, grantRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(grantRec.Body.Bytes(), &created))

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/warehouses/" + wh.String() + "/grants", nil},
		{http.MethodPost, "/api/v1/warehouses/" + wh.String() + "/grants",
			grantBody("user", memberID.String(), "analytics", "clicks")},
		{http.MethodDelete, "/api/v1/warehouses/" + wh.String() + "/grants/" + created.ID, nil},
	} {
		rec := warehouseAPIRequest(t, s, tc.method, tc.path, member, tc.body)
		require.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", tc.method, tc.path, rec.Body.String())
	}

	// Effective access and the routing preference are self-service.
	require.Equal(t, http.StatusOK, effectiveAccessViaAPI(t, s, member, wh, "").Code)
	grantConnectorUse(t, s, orgID, memberID, conn)
	require.Equal(t, http.StatusOK, putPreferenceViaAPI(t, s, member, wh, &conn).Code)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/warehouses/"+wh.String()+"/grants", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPreferenceRequiresServiceAccess(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	orgID, adminID, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Preference WH")

	connA := seedWarehouseConnector(t, s, orgID, "Preference Service A")
	connB := seedWarehouseConnector(t, s, orgID, "Preference Service B")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connA, &wh).Code)
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connB, &wh).Code)
	memberID, member := seedGrantOrgMember(t, s, orgID, "non-admin")

	// Without `use` the preference is refused and nothing is stored.
	rec.reset()
	noUse := putPreferenceViaAPI(t, s, member, wh, &connA)
	require.Equal(t, http.StatusForbidden, noUse.Code, noUse.Body.String())
	require.Nil(t, warehousePreference(t, s, memberID, wh))
	require.False(t, rec.contains(wh), "a rejected preference must not enqueue")

	// With `use` the preference sticks and schedules a reconcile.
	grantConnectorUse(t, s, orgID, memberID, connA)
	set := putPreferenceViaAPI(t, s, member, wh, &connA)
	require.Equal(t, http.StatusOK, set.Code, set.Body.String())
	require.Equal(t, connA.String(), *warehousePreference(t, s, memberID, wh))
	require.True(t, rec.contains(wh), "a saved preference must enqueue a sync")

	// A service the member cannot use is refused and the stored value stands.
	rec.reset()
	denied := putPreferenceViaAPI(t, s, member, wh, &connB)
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	require.Equal(t, connA.String(), *warehousePreference(t, s, memberID, wh))
	require.False(t, rec.contains(wh))

	// A connector in another warehouse, a soft-deleted connector, and an
	// unmanaged connector are all 400 regardless of the member's `use`.
	whOther := createWarehouseViaAPI(t, s, admin, "Preference WH Other")
	connOther := seedWarehouseConnector(t, s, orgID, "Preference Service Other")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connOther, &whOther).Code)
	grantConnectorUse(t, s, orgID, memberID, connOther)
	require.Equal(t, http.StatusBadRequest, putPreferenceViaAPI(t, s, member, wh, &connOther).Code)

	unmanaged := seedWarehouseConnector(t, s, orgID, "Preference Service Unmanaged")
	grantConnectorUse(t, s, orgID, memberID, unmanaged)
	require.Equal(t, http.StatusBadRequest, putPreferenceViaAPI(t, s, member, wh, &unmanaged).Code)

	deletedConn := seedWarehouseConnector(t, s, orgID, "Preference Service Deleted")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, deletedConn, &wh).Code)
	grantConnectorUse(t, s, orgID, memberID, deletedConn)
	_, err := s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET deleted_at = now() WHERE id = $1`, deletedConn.String())
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, putPreferenceViaAPI(t, s, member, wh, &deletedConn).Code)
	require.Equal(t, connA.String(), *warehousePreference(t, s, memberID, wh))

	// Unknown and malformed connectors, and a missing key, are 400.
	require.Equal(t, http.StatusBadRequest, putPreferenceViaAPI(t, s, member, wh, &uuid.UUID{}).Code)
	require.Equal(t, http.StatusBadRequest, warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/preference", member,
		map[string]any{"connector_id": "not-a-uuid"}).Code)
	require.Equal(t, http.StatusBadRequest, warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/preference", member, map[string]any{}).Code)

	// A foreign warehouse is 404 before any connector validation.
	_, _, adminB := seedWarehouseOrgAdmin(t, s)
	whB := createWarehouseViaAPI(t, s, adminB, "Preference WH B")
	require.Equal(t, http.StatusNotFound, putPreferenceViaAPI(t, s, admin, whB, &connB).Code)

	// An explicit null clears the preference (idempotently).
	clear := putPreferenceViaAPI(t, s, member, wh, nil)
	require.Equal(t, http.StatusOK, clear.Code, clear.Body.String())
	require.Nil(t, warehousePreference(t, s, memberID, wh))
	require.Equal(t, http.StatusOK, putPreferenceViaAPI(t, s, member, wh, nil).Code)

	// The stored preference is reflected by effective access.
	require.Equal(t, http.StatusOK, putPreferenceViaAPI(t, s, member, wh, &connA).Code)
	ea := decodeEffectiveAccess(t, effectiveAccessViaAPI(t, s, member, wh, ""))
	require.NotNil(t, ea.PreferredConnectorID)
	require.Equal(t, connA.String(), *ea.PreferredConnectorID)
	require.Len(t, ea.Services, 1)
	require.Equal(t, connA.String(), ea.Services[0].ConnectorID)
	require.True(t, ea.Services[0].Preferred)

	// An org admin in admin mode may route to a service without an explicit
	// connector ACL, matching execution's admin bypass.
	require.Equal(t, http.StatusOK, putPreferenceViaAPI(t, s, admin, wh, &connB).Code)
	require.Equal(t, connB.String(), *warehousePreference(t, s, adminID, wh))

	// Mutations are audited with the chosen connector.
	var raw []byte
	require.NoError(t, s.db.Pool.QueryRow(context.Background(), `
		SELECT metadata FROM audit_logs
		WHERE action = 'warehouse.preference.set' AND resource_id = $1
		ORDER BY id DESC LIMIT 1`, wh.String()).Scan(&raw))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	require.Equal(t, connB.String(), meta["connector_id"])
}
