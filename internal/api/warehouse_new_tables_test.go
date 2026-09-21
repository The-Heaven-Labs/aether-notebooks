package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
)

// insertSchemaSnapshot records one catalog observation with an explicit
// first_seen_at so inbox cutoffs can be tested deterministically. Rows cascade
// away with the connector cleanup in seedWarehouseConnector.
func insertSchemaSnapshot(t *testing.T, s *Server, connectorID uuid.UUID, database, table string, firstSeen time.Time) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO schema_snapshots
			(connector_id, database_name, table_name, first_seen_at, last_seen_at)
		VALUES ($1, $2, $3, $4, now())`,
		connectorID.String(), database, table, firstSeen)
	require.NoError(t, err)
}

// insertWarehouseGrantAt writes a grant with a controlled created_at, which is
// what the new-tables handler treats as the last grant review.
func insertWarehouseGrantAt(t *testing.T, s *Server, orgID, warehouseID uuid.UUID, subjectType, subjectID, database, table string, createdAt time.Time) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		orgID.String(), warehouseID.String(), subjectType, subjectID, database, table, createdAt)
	require.NoError(t, err)
}

// grantOrgRoleEveryoneUse grants `use` on a connector to every org member via
// the org_role:everyone ACL subject.
func grantOrgRoleEveryoneUse(t *testing.T, s *Server, orgID, connectorID uuid.UUID) {
	t.Helper()
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'connector', $2::uuid, 'org_role', 'everyone', ARRAY['use'])`,
		orgID.String(), connectorID.String())
	require.NoError(t, err)
}

func listNewTablesViaAPI(t *testing.T, s *Server, token string, warehouseID uuid.UUID, since string) warehouseNewTablesJSON {
	t.Helper()
	path := "/api/v1/warehouses/" + warehouseID.String() + "/new-tables"
	if since != "" {
		path += "?since=" + since
	}
	rec := warehouseAPIRequest(t, s, http.MethodGet, path, token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp warehouseNewTablesJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func TestWarehouseNewTables(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "New Tables WH")

	conn := seedWarehouseConnector(t, s, orgID, "New Tables Service")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)
	postgresConn := seedPostgresConnector(t, s, orgID, "New Tables Postgres")
	// A postgres connector can never be linked to a warehouse, but a snapshot
	// row under its ID must not leak into the ClickHouse inbox either.
	require.Equal(t, http.StatusBadRequest, linkConnectorViaAPI(t, s, admin, postgresConn, &wh).Code)

	now := time.Now().UTC()
	// The default review cutoff is the max of warehouse creation and the most
	// recent grant, so age the warehouse past the grant below.
	_, err := s.db.Pool.Exec(context.Background(),
		`UPDATE warehouses SET created_at = $1 WHERE id = $2`,
		now.Add(-4*time.Hour), wh.String())
	require.NoError(t, err)
	insertSchemaSnapshot(t, s, conn, "analytics", "events", now.Add(-3*time.Hour))
	insertSchemaSnapshot(t, s, conn, "analytics", "old_table", now.Add(-2*time.Hour))
	insertSchemaSnapshot(t, s, conn, "raw", "clicks", now.Add(-30*time.Minute))
	insertSchemaSnapshot(t, s, conn, "raw", "logs", now.Add(-10*time.Minute))
	insertSchemaSnapshot(t, s, postgresConn, "public", "users", now.Add(-5*time.Minute))

	// The most recent grant is the default review cutoff, and granted tables
	// are excluded even when observed after it.
	insertWarehouseGrantAt(t, s, orgID, wh, "everyone", "everyone", "raw", "logs", now.Add(-time.Hour))

	review := now.Add(-time.Hour)
	resp := listNewTablesViaAPI(t, s, admin, wh, "")
	require.Equal(t, wh.String(), resp.WarehouseID)
	require.WithinDuration(t, review, resp.Since, 2*time.Second)
	require.Len(t, resp.Tables, 1)
	require.Equal(t, "raw", resp.Tables[0].Database)
	require.Equal(t, "clicks", resp.Tables[0].Table)
	require.WithinDuration(t, now.Add(-30*time.Minute), resp.Tables[0].FirstSeenAt, 2*time.Second)

	// An explicit earlier cutoff reopens the review window; granted tables and
	// other connector types stay out.
	resp = listNewTablesViaAPI(t, s, admin, wh, now.Add(-4*time.Hour).Format(time.RFC3339))
	require.Len(t, resp.Tables, 3)
	got := map[string]bool{}
	for _, table := range resp.Tables {
		got[table.Database+"."+table.Table] = true
	}
	require.True(t, got["analytics.events"])
	require.True(t, got["analytics.old_table"])
	require.True(t, got["raw.clicks"])
	require.False(t, got["raw.logs"], "granted tables are excluded from the inbox")
	require.False(t, got["public.users"], "non-clickhouse connector snapshots are ignored")

	// A cutoff after every observation returns an empty inbox.
	resp = listNewTablesViaAPI(t, s, admin, wh, now.Add(time.Hour).Format(time.RFC3339))
	require.Empty(t, resp.Tables)

	bad := warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+wh.String()+"/new-tables?since=not-a-time", admin, nil)
	require.Equal(t, http.StatusBadRequest, bad.Code)
}

func TestWarehouseNewTablesUnknownWarehouse(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)

	rec := warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+uuid.NewString()+"/new-tables", admin, nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWarehouseNewTablesRequiresAdmin(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "New Tables Members WH")
	member := seedWarehouseOrgMemberToken(t, s, orgID, "editor")

	rec := warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+wh.String()+"/new-tables", member, nil)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRecordSchemaSnapshotPreservesFirstSeen(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, _ := seedWarehouseOrgAdmin(t, s)
	conn := seedWarehouseConnector(t, s, orgID, "Snapshot Service")

	ctx := context.Background()
	firstSeen := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	require.NoError(t, s.recordSchemaSnapshot(ctx, conn, []chaccess.CatalogTable{
		{Database: "analytics", Table: "events"},
	}))
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE schema_snapshots SET first_seen_at = $1 WHERE connector_id = $2`,
		firstSeen, conn.String())
	require.NoError(t, err)

	// Invalid names are dropped; the re-observed table keeps its first sighting.
	require.NoError(t, s.recordSchemaSnapshot(ctx, conn, []chaccess.CatalogTable{
		{Database: "analytics", Table: "events"},
		{Database: "analytics", Table: "bad name"},
	}))

	var gotFirst, gotLast time.Time
	require.NoError(t, s.db.Pool.QueryRow(ctx, `
		SELECT first_seen_at, last_seen_at FROM schema_snapshots
		WHERE connector_id = $1 AND database_name = 'analytics' AND table_name = 'events'`,
		conn.String()).Scan(&gotFirst, &gotLast))
	require.WithinDuration(t, firstSeen, gotFirst, time.Millisecond)
	require.True(t, gotLast.After(firstSeen), "last_seen_at should refresh on observation")

	var count int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1`, conn.String()).Scan(&count))
	require.Equal(t, 1, count, "unrepresentable names must not be cached")
}

func getWarehouseValidation(t *testing.T, s *Server, token string, warehouseID uuid.UUID) warehouseValidationJSON {
	t.Helper()
	rec := warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+warehouseID.String()+"/validation", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp warehouseValidationJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

// validationSubjectByID finds one warning subject or fails.
func validationSubjectByID(t *testing.T, subjects []warehouseValidationSubjectJSON, subjectID string) warehouseValidationSubjectJSON {
	t.Helper()
	for _, subject := range subjects {
		if subject.SubjectID == subjectID {
			return subject
		}
	}
	t.Fatalf("subject %s not found in %+v", subjectID, subjects)
	return warehouseValidationSubjectJSON{}
}

func TestWarehouseValidationWarnings(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Validation WH")
	conn := seedWarehouseConnector(t, s, orgID, "Validation Service")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)

	// A group with grants but no service access is "granted but unusable".
	groupNoService := seedGrantGroup(t, s, orgID, "Validation No Service")
	require.Equal(t, http.StatusCreated,
		createGrantViaAPI(t, s, admin, wh, grantBody("group", groupNoService.String(), "analytics", "events")).Code)

	// A group and a user with service access but no grants can connect and
	// always fail.
	groupNoTables := seedGrantGroup(t, s, orgID, "Validation Service Only")
	grantGroupConnectorUse(t, s, orgID, groupNoTables, conn)
	userNoTables, _ := seedGrantOrgMember(t, s, orgID, "editor")
	grantConnectorUse(t, s, orgID, userNoTables, conn)

	// A member who reaches tables through the group is not warned: grants are
	// a union, not per-subject.
	userViaGroup, _ := seedGrantOrgMember(t, s, orgID, "editor")
	grantConnectorUse(t, s, orgID, userViaGroup, conn)
	_, err := s.db.Pool.Exec(context.Background(),
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		groupNoService.String(), userViaGroup.String())
	require.NoError(t, err)

	// A fully configured user appears in neither list.
	userWithBoth, _ := seedGrantOrgMember(t, s, orgID, "editor")
	grantConnectorUse(t, s, orgID, userWithBoth, conn)
	require.Equal(t, http.StatusCreated,
		createGrantViaAPI(t, s, admin, wh, grantBody("user", userWithBoth.String(), "raw", "clicks")).Code)

	resp := getWarehouseValidation(t, s, admin, wh)

	require.Len(t, resp.TablesWithoutService, 1)
	noService := validationSubjectByID(t, resp.TablesWithoutService, groupNoService.String())
	require.Equal(t, "group", noService.SubjectType)
	require.Equal(t, []string{"analytics.events"}, noService.Tables)

	require.Len(t, resp.ServicesWithoutTables, 2)
	svcOnly := validationSubjectByID(t, resp.ServicesWithoutTables, groupNoTables.String())
	require.Equal(t, "group", svcOnly.SubjectType)
	require.Equal(t, []string{"Validation Service"}, svcOnly.Services)
	userOnly := validationSubjectByID(t, resp.ServicesWithoutTables, userNoTables.String())
	require.Equal(t, "user", userOnly.SubjectType)
	require.Equal(t, []string{"Validation Service"}, userOnly.Services)

	// The union paths and the fully configured user stay out of both lists.
	for _, list := range [][]warehouseValidationSubjectJSON{resp.TablesWithoutService, resp.ServicesWithoutTables} {
		for _, subject := range list {
			require.NotEqual(t, userViaGroup.String(), subject.SubjectID)
			require.NotEqual(t, userWithBoth.String(), subject.SubjectID)
		}
	}

	// Granting a table clears the service-without-tables warning.
	require.Equal(t, http.StatusCreated,
		createGrantViaAPI(t, s, admin, wh, grantBody("user", userNoTables.String(), "analytics", "events")).Code)
	resp = getWarehouseValidation(t, s, admin, wh)
	require.Len(t, resp.ServicesWithoutTables, 1)
	require.Equal(t, groupNoTables.String(), resp.ServicesWithoutTables[0].SubjectID)
}

func TestWarehouseValidationEveryoneServiceWithoutTables(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Validation Everyone WH")
	conn := seedWarehouseConnector(t, s, orgID, "Validation Everyone Service")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)
	grantOrgRoleEveryoneUse(t, s, orgID, conn)

	resp := getWarehouseValidation(t, s, admin, wh)
	require.Len(t, resp.ServicesWithoutTables, 1)
	require.Equal(t, "everyone", resp.ServicesWithoutTables[0].SubjectType)
	require.Equal(t, "everyone", resp.ServicesWithoutTables[0].SubjectID)
	require.Equal(t, "Everyone", resp.ServicesWithoutTables[0].SubjectName)

	// An everyone table grant satisfies the warning.
	require.Equal(t, http.StatusCreated,
		createGrantViaAPI(t, s, admin, wh, grantBody("everyone", "", "analytics", "events")).Code)
	resp = getWarehouseValidation(t, s, admin, wh)
	require.Empty(t, resp.ServicesWithoutTables)
	require.Empty(t, resp.TablesWithoutService)
}

func TestWarehouseValidationRequiresAdmin(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Validation Members WH")
	member := seedWarehouseOrgMemberToken(t, s, orgID, "non-admin")

	rec := warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+wh.String()+"/validation", member, nil)
	require.Equal(t, http.StatusForbidden, rec.Code)
}
