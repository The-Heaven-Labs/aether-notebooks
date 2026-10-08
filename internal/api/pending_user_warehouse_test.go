package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPendingWarehouseGrantCreate(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Grant WH")
	rec.reset()

	email := "Future.User@Example.com"
	createRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", email, "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	require.Equal(t, "pending_user", created.SubjectType)
	require.Equal(t, "future.user@example.com", created.SubjectID)
	require.Equal(t, "future.user@example.com", created.SubjectEmail)
	require.Empty(t, created.Warning, "pending subjects are never warned about service access")

	ctx := context.Background()
	var realCount, pendingCount int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&realCount))
	require.Zero(t, realCount, "staging must not write a real grant")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&pendingCount))
	require.Equal(t, 1, pendingCount)
	require.False(t, rec.contains(wh), "staging must not enqueue a warehouse sync")

	// Idempotent replay returns the same staged row with 200.
	replayRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", "future.user@example.COM", "analytics", "events"))
	require.Equal(t, http.StatusOK, replayRec.Code, replayRec.Body.String())
	var replay warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(replayRec.Body.Bytes(), &replay))
	require.Equal(t, created.ID, replay.ID)

	// Audit carries the pending marker.
	meta := warehouseGrantAuditMeta(t, s, "warehouse.grant.create", created.ID)
	require.Equal(t, true, meta["pending"])
}

func TestPendingWarehouseGrantRejectsInvalidEmail(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Invalid WH")

	for _, email := range []string{"", "future@", "@example.com", "two@@example.com"} {
		rec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", email, "analytics", "events"))
		require.Equal(t, http.StatusBadRequest, rec.Code, "email %q must be rejected", email)
	}
}

// TestPendingWarehouseGrantConvertsExistingMember pins the member-conversion
// amendment: a pending_user request whose email already belongs to an org
// member becomes a real user grant, never staged (a staged row for a member
// could never materialize), mirroring handleAddPendingGroupMembers and the
// ACL pending path.
func TestPendingWarehouseGrantConvertsExistingMember(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Convert WH")
	memberID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")

	ctx := context.Background()
	var memberEmail string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, memberID.String()).Scan(&memberEmail))
	rec.reset()

	// Uppercase input proves the membership lookup is case-insensitive.
	createRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("pending_user", strings.ToUpper(memberEmail), "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	require.Equal(t, "user", created.SubjectType)
	require.Equal(t, memberID.String(), created.SubjectID)
	require.Equal(t, memberEmail, created.SubjectEmail)
	// The converted member is a real subject: the ordinary service-access
	// warning still applies, exactly as for a direct user grant.
	require.Equal(t, "no_service_access", created.Warning)

	var staged, real int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&staged))
	require.Zero(t, staged, "an existing member's email must not be staged")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants
		 WHERE warehouse_id = $1 AND subject_type = 'user' AND subject_id = $2`,
		wh.String(), memberID.String()).Scan(&real))
	require.Equal(t, 1, real)
	require.True(t, rec.contains(wh), "a converted member is a real grant and must enqueue a sync")

	meta := warehouseGrantAuditMeta(t, s, "warehouse.grant.create", created.ID)
	require.Equal(t, "user", meta["subject_type"])
	require.NotEqual(t, true, meta["pending"])
}

func TestPendingWarehouseGrantListAndDelete(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending List WH")

	// A real grant (Everyone) plus a staged one; the list unions both, staged last.
	realRec := createGrantViaAPI(t, s, admin, wh, grantBody("everyone", "everyone", "raw", "clicks"))
	require.Equal(t, http.StatusCreated, realRec.Code, realRec.Body.String())
	createRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", "future@example.com", "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	rec.reset()

	grants := listGrantsViaAPI(t, s, admin, wh)
	require.Len(t, grants, 2)
	require.Equal(t, "everyone", grants[0].SubjectType)
	require.Equal(t, "pending_user", grants[1].SubjectType)
	require.Equal(t, created.ID, grants[1].ID)
	require.Equal(t, "future@example.com", grants[1].SubjectEmail)

	// Delete removes the staged row from its table and 404s on replay.
	require.Equal(t, http.StatusNoContent,
		deleteGrantViaAPI(t, s, admin, wh, mustParseUUID(t, created.ID)).Code)
	require.Equal(t, http.StatusNotFound,
		deleteGrantViaAPI(t, s, admin, wh, mustParseUUID(t, created.ID)).Code)

	grants = listGrantsViaAPI(t, s, admin, wh)
	require.Len(t, grants, 1)

	var pending int
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&pending))
	require.Zero(t, pending)
	require.False(t, rec.contains(wh), "deleting a staged grant must not enqueue a warehouse sync")

	// The delete audit carries the pending marker.
	meta := warehouseGrantAuditMeta(t, s, "warehouse.grant.delete", created.ID)
	require.Equal(t, true, meta["pending"])
}

func TestWarehouseValidationIgnoresPendingGrants(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Validation WH")

	createRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", "future@example.com", "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	valRec := warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+wh.String()+"/validation", admin, nil)
	require.Equal(t, http.StatusOK, valRec.Code, valRec.Body.String())
	var v warehouseValidationJSON
	require.NoError(t, json.Unmarshal(valRec.Body.Bytes(), &v))
	require.Empty(t, v.TablesWithoutService, "staged grants must not produce validation warnings")
	require.Empty(t, v.ServicesWithoutTables)
}

// mustParseUUID fails the test on an invalid grant ID returned by the API.
func mustParseUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(raw)
	require.NoError(t, err)
	return parsed
}

func TestPendingWarehouseGrantRecreateAfterDelete(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Recreate WH")

	createRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("pending_user", "recreate@example.com", "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var first warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &first))

	require.Equal(t, http.StatusNoContent,
		deleteGrantViaAPI(t, s, admin, wh, mustParseUUID(t, first.ID)).Code)

	recreateRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("pending_user", "recreate@example.com", "analytics", "events"))
	require.Equal(t, http.StatusCreated, recreateRec.Code, recreateRec.Body.String())
	var second warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(recreateRec.Body.Bytes(), &second))
	require.NotEqual(t, first.ID, second.ID, "a recreated staged grant gets a new id")
}

func TestPendingWarehouseGrantConvertReplaysExistingRealGrant(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Convert Replay WH")
	memberID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")

	ctx := context.Background()
	var memberEmail string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, memberID.String()).Scan(&memberEmail))

	directRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("user", memberID.String(), "analytics", "events"))
	require.Equal(t, http.StatusCreated, directRec.Code, directRec.Body.String())
	var direct warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(directRec.Body.Bytes(), &direct))

	convertRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("pending_user", memberEmail, "analytics", "events"))
	require.Equal(t, http.StatusOK, convertRec.Code, convertRec.Body.String())
	var replay warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(convertRec.Body.Bytes(), &replay))
	require.Equal(t, direct.ID, replay.ID, "conversion replays the existing real grant")
	require.Equal(t, "user", replay.SubjectType)

	var pending, real int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&pending))
	require.Zero(t, pending, "conversion must not stage a row")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants
		 WHERE warehouse_id = $1 AND subject_type = 'user' AND subject_id = $2`,
		wh.String(), memberID.String()).Scan(&real))
	require.Equal(t, 1, real, "no duplicate real row")
}

func TestPendingWarehouseGrantRegisteredNonMemberStaysStaged(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Outsider WH")

	// A registered user who never joined this org has no membership row, so
	// the pending grant cannot materialize yet and must stay staged.
	ctx := context.Background()
	outsiderID := uuid.New()
	outsiderEmail := "wh-outsider-" + uuid.NewString() + "@test.local"
	_, err := s.db.Pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		outsiderID.String(), outsiderEmail, "Warehouse Outsider")
	require.NoError(t, err)
	t.Cleanup(func() {
		if _, err := s.db.Pool.Exec(context.Background(),
			`DELETE FROM users WHERE id = $1`, outsiderID.String()); err != nil {
			t.Logf("cleanup outsider: %v", err)
		}
	})

	createRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("pending_user", strings.ToUpper(outsiderEmail), "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	require.Equal(t, "pending_user", created.SubjectType)
	require.Equal(t, outsiderEmail, created.SubjectID, "the staged email is lowercased")

	var real int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&real))
	require.Zero(t, real, "a non-member's grant stays staged")
}

// TestPendingWarehouseGrantConvertConsumesStagedRows pins the self-healing
// conversion: a staged row for an email that has since become an org member
// (the join/create race) is materialized by the conversion transaction instead
// of staying stranded as pending_user forever.
func TestPendingWarehouseGrantConvertConsumesStagedRows(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Self Heal WH")
	memberID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")

	ctx := context.Background()
	var memberEmail string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, memberID.String()).Scan(&memberEmail))

	// Stage the grant directly, simulating a staging write that landed after
	// the join materializer's snapshot.
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO pending_warehouse_table_grants
			(org_id, warehouse_id, email, database_name, table_name)
		VALUES ($1, $2, $3, 'analytics', 'events')`,
		orgID.String(), wh.String(), memberEmail)
	require.NoError(t, err)

	convertRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("pending_user", memberEmail, "analytics", "events"))
	require.Equal(t, http.StatusOK, convertRec.Code, convertRec.Body.String())
	var converted warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(convertRec.Body.Bytes(), &converted))
	require.Equal(t, "user", converted.SubjectType)
	require.Equal(t, memberID.String(), converted.SubjectID)

	var pending, real int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&pending))
	require.Zero(t, pending, "the staged row must be consumed by the conversion")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants
		 WHERE warehouse_id = $1 AND subject_type = 'user' AND subject_id = $2`,
		wh.String(), memberID.String()).Scan(&real))
	require.Equal(t, 1, real)
}

func TestPendingWarehouseGrantDeleteScopedToWarehouse(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	whA := createWarehouseViaAPI(t, s, admin, "Pending Scoped A")
	whB := createWarehouseViaAPI(t, s, admin, "Pending Scoped B")

	createRec := createGrantViaAPI(t, s, admin, whA,
		grantBody("pending_user", "scoped@example.com", "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))

	require.Equal(t, http.StatusNotFound,
		deleteGrantViaAPI(t, s, admin, whB, mustParseUUID(t, created.ID)).Code)

	// The staged row survives the cross-warehouse delete attempt.
	var pending int
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE id = $1`, created.ID).Scan(&pending))
	require.Equal(t, 1, pending)
}
