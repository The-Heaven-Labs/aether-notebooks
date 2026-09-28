package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// schemaTableSet asserts the response is a 200 and converts its schema body
// into a database.table lookup set.
func schemaTableSet(t *testing.T, rec *httptest.ResponseRecorder) map[string]bool {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var schema executor.SchemaInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schema))
	got := map[string]bool{}
	for _, table := range schema.Tables {
		got[table.Schema+"."+table.Name] = true
	}
	return got
}

// schemaHiddenTables extracts the schema endpoint's hidden_tables count; an
// absent field decodes to zero.
func schemaHiddenTables(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		HiddenTables int `json:"hidden_tables"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.HiddenTables
}

func TestCompileAndMatchHiddenPatterns(t *testing.T) {
	patterns := compileHiddenPatterns([]string{`^analytics\._tmp`, `_scratch$`, `^scratchy$`, `(`})
	require.Len(t, patterns, 3, "an invalid stored pattern is skipped")

	require.True(t, matchesHiddenPattern(patterns, "analytics", "_tmp_123"))
	require.True(t, matchesHiddenPattern(patterns, "raw", "my_scratch"))
	require.False(t, matchesHiddenPattern(patterns, "analytics", "events"))
	require.True(t, matchesHiddenPattern(patterns, "", "scratchy"),
		"an empty database matches the table name alone")
}

func TestFilterVisibleSchemaTables(t *testing.T) {
	tables := []executor.TableInfo{
		{Schema: "analytics", Name: "events"},
		{Schema: "analytics", Name: "_tmp_scratch"},
		{Schema: "raw", Name: "clicks"},
	}
	patterns := compileHiddenPatterns([]string{`_tmp`})

	// Admin view: patterns drop matched ungranted tables.
	got := filterVisibleSchemaTables(tables, patterns, nil, map[tableKey]struct{}{})
	require.Equal(t, []executor.TableInfo{{Schema: "analytics", Name: "events"}, {Schema: "raw", Name: "clicks"}}, got)

	// Granted tables survive a pattern match.
	protected := map[tableKey]struct{}{{Database: "analytics", Table: "_tmp_scratch"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, nil, protected)
	require.Len(t, got, 3)

	// An effective-grant allowlist wins over everything else.
	allowed := map[tableKey]struct{}{{Database: "raw", Table: "clicks"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, allowed, allowed)
	require.Equal(t, []executor.TableInfo{{Schema: "raw", Name: "clicks"}}, got)

	// A pattern-matched granted table stays visible while it is protected:
	// patterns can never hide granted access.
	grantedMatched := map[tableKey]struct{}{{Database: "analytics", Table: "_tmp_scratch"}: {}}
	got = filterVisibleSchemaTables(tables, patterns, grantedMatched, grantedMatched)
	require.Equal(t, []executor.TableInfo{{Schema: "analytics", Name: "_tmp_scratch"}}, got)

	// allowed and patternProtected are distinct parameters: an allowed
	// pattern-matched table is dropped when patternProtected is empty...
	got = filterVisibleSchemaTables(tables, patterns, grantedMatched, map[tableKey]struct{}{})
	require.Empty(t, got)

	// ...while an allowed table that matches no pattern survives an empty
	// patternProtected.
	got = filterVisibleSchemaTables(tables, patterns, allowed, map[tableKey]struct{}{})
	require.Equal(t, []executor.TableInfo{{Schema: "raw", Name: "clicks"}}, got)
}

func TestFilterHiddenCatalogTables(t *testing.T) {
	tables := []chaccess.CatalogTable{
		{Database: "analytics", Table: "events"},
		{Database: "analytics", Table: "_tmp_scratch"},
	}

	require.Equal(t, tables, filterHiddenCatalogTables(nil, tables),
		"no patterns returns the input unchanged")

	patterns := compileHiddenPatterns([]string{`_tmp`})
	require.Equal(t, []chaccess.CatalogTable{{Database: "analytics", Table: "events"}},
		filterHiddenCatalogTables(patterns, tables),
		"matched tables are dropped")
}

func TestConnectorSchemaHiddenPatterns(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()

	database := "aether_vis_" + uuid.NewString()[:8]
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quotedDB))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = fx.conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB)
	})
	for _, table := range []string{"events", "_tmp_scratch", "daily_revenue"} {
		quoted, err := chaccess.QuoteObjectIdent(table)
		require.NoError(t, err)
		require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quoted+
			" (id UInt64) ENGINE = MergeTree ORDER BY id"))
	}

	token, err := fx.s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)
	// Scope the read to this test's database so the hidden count is stable
	// against any other tables on the shared ClickHouse instance.
	schemaURL := "/api/v1/connectors/" + fx.connectorID.String() + "/schema?database=" + database

	// No patterns yet: nothing is hidden and matched-looking tables stay visible.
	rec := warehouseAPIRequest(t, fx.s, http.MethodGet, schemaURL, token, nil)
	got := schemaTableSet(t, rec)
	require.True(t, got[database+"._tmp_scratch"], "no patterns set: nothing is hidden")
	require.Zero(t, schemaHiddenTables(t, rec), "no patterns set: hidden_tables is absent/zero")

	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`_tmp`}, fx.warehouseID.String())
	require.NoError(t, err)
	// Drop the row the pre-pattern read wrote so the snapshot assertion below
	// proves the filtered read does not re-snapshot hidden tables.
	_, err = fx.s.db.Pool.Exec(ctx,
		`DELETE FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2`,
		fx.connectorID.String(), database)
	require.NoError(t, err)

	rec = warehouseAPIRequest(t, fx.s, http.MethodGet, schemaURL, token, nil)
	got = schemaTableSet(t, rec)
	require.True(t, got[database+".events"], "unmatched tables survive")
	require.True(t, got[database+".daily_revenue"])
	require.False(t, got[database+"._tmp_scratch"], "pattern-matched ungranted tables are hidden")
	require.Equal(t, 1, schemaHiddenTables(t, rec), "the response reports the hidden table count")

	// The schema-read snapshot is pattern-filtered too.
	var hiddenRows int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2 AND table_name = '_tmp_scratch'`,
		fx.connectorID.String(), database).Scan(&hiddenRows))
	require.Zero(t, hiddenRows)

	// ...and the surviving table was actually written: the filtered catalog is
	// a real snapshot, not an empty no-op.
	var visibleRows int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2 AND table_name = 'events'`,
		fx.connectorID.String(), database).Scan(&visibleRows))
	require.Equal(t, 1, visibleRows)

	// A granted table that also matches stays visible and is not "hidden".
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'everyone', 'everyone', $3, '_tmp_scratch')`,
		fx.orgID.String(), fx.warehouseID.String(), database)
	require.NoError(t, err)

	rec = warehouseAPIRequest(t, fx.s, http.MethodGet, schemaURL, token, nil)
	got = schemaTableSet(t, rec)
	require.True(t, got[database+"._tmp_scratch"], "granted tables stay visible even when matched")
	require.Zero(t, schemaHiddenTables(t, rec), "granted matches are not hidden")
}

// With the kill switch off, a non-admin viewing a warehouse-linked service
// connector must still see a granted table that matches a hidden pattern:
// patterns hide only ungranted tables, never existing access. setupWarehouseFixture
// builds a dedicated server, so flipping the kill switch is safe here.
func TestConnectorSchemaHiddenPatternsKillSwitchOff(t *testing.T) {
	fx := setupWarehouseFixture(t)
	defer fx.s.SetCHTablePermissions(true)
	fx.s.SetCHTablePermissions(false)
	ctx := context.Background()

	database := "aether_visoff_" + uuid.NewString()[:8]
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quotedDB))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = fx.conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB)
	})
	quotedTable, err := chaccess.QuoteObjectIdent("_tmp_scratch")
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quotedTable+
		" (id UInt64) ENGINE = MergeTree ORDER BY id"))

	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`_tmp`}, fx.warehouseID.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, $4, '_tmp_scratch')`,
		fx.orgID.String(), fx.warehouseID.String(), fx.userID.String(), database)
	require.NoError(t, err)

	serviceID := insertClickHouseService(t, fx.s, fx.orgID, fx.connectorID, "Visibility Off Service", &fx.warehouseID)
	grantConnectorUse(t, fx.s, fx.orgID, fx.userID, serviceID)

	token, err := fx.s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "non-admin")
	require.NoError(t, err)
	got := schemaTableSet(t, warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+serviceID.String()+"/schema", token, nil))
	require.True(t, got[database+"._tmp_scratch"],
		"a granted pattern-matched table must stay visible with the kill switch off")
}

// visibilityFixture links a non-provisioner ClickHouse service to the fixture
// warehouse, seeds the given database with events/daily_revenue/clicks/secret
// tables, and returns the service connector ID.
func visibilityFixture(t *testing.T, fx *warehouseSyncFixture, database string) (serviceID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	serviceID = insertClickHouseService(t, fx.s, fx.orgID, fx.connectorID, "Visibility Service", &fx.warehouseID)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = fx.s.db.Pool.Exec(cleanupCtx, `DELETE FROM connectors WHERE id = $1`, serviceID.String())
	})
	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quotedDB))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = fx.conn.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDB)
	})
	for _, table := range []string{"events", "daily_revenue", "clicks", "secret"} {
		quoted, err := chaccess.QuoteObjectIdent(table)
		require.NoError(t, err)
		require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quoted+
			" (id UInt64) ENGINE = MergeTree ORDER BY id"))
	}
	return serviceID
}

func TestConnectorSchemaPerUserGrantFilter(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()
	database := "aether_vis_" + uuid.NewString()[:8]
	serviceID := visibilityFixture(t, fx, database)

	memberID, memberToken := seedGrantOrgMember(t, fx.s, fx.orgID, "editor")
	grantConnectorUse(t, fx.s, fx.orgID, memberID, serviceID)
	_, err := fx.s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		fx.groupID.String(), memberID.String())
	require.NoError(t, err)

	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES
			($1, $2, 'user', $3, $4, 'events'),
			($1, $2, 'everyone', 'everyone', $4, 'clicks')`,
		fx.orgID.String(), fx.warehouseID.String(), memberID.String(), database)
	require.NoError(t, err)
	// The fixture's group grant (analytics.daily_revenue) is in another
	// database; add one in the visibility database to prove group inheritance.
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'group', $3, $4, 'daily_revenue')`,
		fx.orgID.String(), fx.warehouseID.String(), fx.groupID.String(), database)
	require.NoError(t, err)

	schemaFor := func(t *testing.T, token string) map[string]bool {
		t.Helper()
		return schemaTableSet(t, warehouseAPIRequest(t, fx.s, http.MethodGet,
			"/api/v1/connectors/"+serviceID.String()+"/schema", token, nil))
	}

	// Non-admin sees direct + group + everyone grants only.
	got := schemaFor(t, memberToken)
	require.True(t, got[database+".events"], "direct grant")
	require.True(t, got[database+".daily_revenue"], "group grant")
	require.True(t, got[database+".clicks"], "everyone grant")
	require.False(t, got[database+".secret"], "ungranted tables are hidden")

	// A member with service access but no direct/group grants still sees the
	// everyone grant: the effective set is exactly {clicks}.
	noGrantID, noGrantToken := seedGrantOrgMember(t, fx.s, fx.orgID, "editor")
	grantConnectorUse(t, fx.s, fx.orgID, noGrantID, serviceID)
	got = schemaFor(t, noGrantToken)
	require.True(t, got[database+".clicks"], "everyone grants reach every member")
	require.False(t, got[database+".events"])
	require.False(t, got[database+".secret"])
	require.Len(t, got, 1, "the member sees exactly the everyone grant")

	// Admin bypasses the per-user filter.
	adminToken, err := fx.s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)
	got = schemaFor(t, adminToken)
	require.True(t, got[database+".events"])
	require.True(t, got[database+".secret"])

	// A granted table that matches a hidden pattern stays visible to its
	// grantee with the kill switch on: patterns can never hide access.
	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`secret`}, fx.warehouseID.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, $4, 'secret')`,
		fx.orgID.String(), fx.warehouseID.String(), memberID.String(), database)
	require.NoError(t, err)
	got = schemaFor(t, memberToken)
	require.True(t, got[database+".secret"],
		"a granted pattern-matched table stays visible with the kill switch on")

	// With the kill switch off, execution uses the stored credential, so the
	// per-user filter must not understate access. (Dedicated server: safe.)
	fx.s.SetCHTablePermissions(false)
	defer fx.s.SetCHTablePermissions(true)
	got = schemaFor(t, memberToken)
	require.True(t, got[database+".secret"], "kill switch off disables per-user filtering")
}

// Removing a user from the org must revoke effective grants even while the
// user still holds a valid JWT and a connector use ACL: resolution requires
// current org membership, matching execution. The schema request itself still
// succeeds, with an empty table set.
func TestConnectorSchemaRemovedMemberSeesNothing(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()
	database := "aether_visrm_" + uuid.NewString()[:8]
	serviceID := visibilityFixture(t, fx, database)

	memberID, memberToken := seedGrantOrgMember(t, fx.s, fx.orgID, "editor")
	grantConnectorUse(t, fx.s, fx.orgID, memberID, serviceID)
	_, err := fx.s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		fx.groupID.String(), memberID.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES
			($1, $2, 'user', $3, $4, 'events'),
			($1, $2, 'everyone', 'everyone', $4, 'clicks')`,
		fx.orgID.String(), fx.warehouseID.String(), memberID.String(), database)
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'group', $3, $4, 'daily_revenue')`,
		fx.orgID.String(), fx.warehouseID.String(), fx.groupID.String(), database)
	require.NoError(t, err)

	got := schemaTableSet(t, warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+serviceID.String()+"/schema", memberToken, nil))
	require.True(t, got[database+".events"], "direct grant while a member")
	require.True(t, got[database+".daily_revenue"], "group grant while a member")
	require.True(t, got[database+".clicks"], "everyone grant while a member")

	// The membership row goes away; the token and the connector use ACL stay.
	_, err = fx.s.db.Pool.Exec(ctx,
		`DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`,
		fx.orgID.String(), memberID.String())
	require.NoError(t, err)

	rec := warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+serviceID.String()+"/schema", memberToken, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got = schemaTableSet(t, rec)
	require.False(t, got[database+".events"], "a removed member loses direct grants")
	require.False(t, got[database+".daily_revenue"], "a removed member loses group grants")
	require.False(t, got[database+".clicks"], "a removed member loses everyone grants")
	require.Empty(t, got, "a removed member sees no tables")
}

// A schema read must snapshot the warehouse-wide catalog, never one viewer's
// subset: a pattern-matched table granted to another subject stays recorded in
// schema_snapshots even though the non-admin caller's response omits it.
func TestConnectorSchemaSnapshotKeepsOtherSubjectGrants(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()
	database := "aether_vissn_" + uuid.NewString()[:8]
	serviceID := visibilityFixture(t, fx, database)

	_, err := fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`_tmp`}, fx.warehouseID.String())
	require.NoError(t, err)

	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	quotedTable, err := chaccess.QuoteObjectIdent("_tmp_shared")
	require.NoError(t, err)
	require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quotedTable+
		" (id UInt64) ENGINE = MergeTree ORDER BY id"))

	// The pattern-matched table is granted to a different org member: not the
	// caller, not a group the caller is in, not everyone.
	otherID, _ := seedGrantOrgMember(t, fx.s, fx.orgID, "editor")
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, $4, '_tmp_shared')`,
		fx.orgID.String(), fx.warehouseID.String(), otherID.String(), database)
	require.NoError(t, err)

	memberID, memberToken := seedGrantOrgMember(t, fx.s, fx.orgID, "editor")
	grantConnectorUse(t, fx.s, fx.orgID, memberID, serviceID)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, $4, 'events')`,
		fx.orgID.String(), fx.warehouseID.String(), memberID.String(), database)
	require.NoError(t, err)

	rec := warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+serviceID.String()+"/schema", memberToken, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var schema executor.SchemaInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schema))
	got := map[string]bool{}
	for _, table := range schema.Tables {
		got[table.Schema+"."+table.Name] = true
	}
	require.True(t, got[database+".events"], "the caller's own grant stays visible")
	require.False(t, got[database+"._tmp_shared"],
		"another subject's grant is not the caller's to see")

	var snapshotRows int
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_snapshots WHERE connector_id = $1 AND database_name = $2 AND table_name = '_tmp_shared'`,
		serviceID.String(), database).Scan(&snapshotRows))
	require.Equal(t, 1, snapshotRows,
		"the snapshot must record the warehouse-wide catalog before per-user filtering")
}

// Warehouse grants and hidden patterns are org-scoped: a viewer in one org
// must never see another org's grants, and one org's patterns must never hide
// tables from another org's connector.
func TestConnectorSchemaCrossOrgIsolation(t *testing.T) {
	fx := setupWarehouseFixture(t)
	ctx := context.Background()
	database := "aether_visx_" + uuid.NewString()[:8]
	serviceA := visibilityFixture(t, fx, database)

	quotedDB, err := chaccess.QuoteObjectIdent(database)
	require.NoError(t, err)
	// One plain name granted to org B, one pattern-matched name left ungranted
	// (so a leaked org A pattern would hide it for org B), and one name for the
	// cross-org group-membership probe.
	for _, table := range []string{"orgb_only", "_tmp_orgb", "orgb_group_leak"} {
		quoted, err := chaccess.QuoteObjectIdent(table)
		require.NoError(t, err)
		require.NoError(t, fx.conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+quotedDB+"."+quoted+
			" (id UInt64) ENGINE = MergeTree ORDER BY id"))
	}

	// Org A: a pattern plus a member with service access and a direct grant.
	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET hidden_table_patterns = $1 WHERE id = $2`,
		[]string{`_tmp`}, fx.warehouseID.String())
	require.NoError(t, err)
	memberA, tokenA := seedGrantOrgMember(t, fx.s, fx.orgID, "editor")
	grantConnectorUse(t, fx.s, fx.orgID, memberA, serviceA)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'user', $3, $4, 'events')`,
		fx.orgID.String(), fx.warehouseID.String(), memberA.String(), database)
	require.NoError(t, err)

	// Org B: its own warehouse, provisioner, service, and grant. The service
	// copies org A's encrypted ClickHouse config; only the org/warehouse scope
	// differs.
	orgB, _, adminB := seedWarehouseOrgAdmin(t, fx.s)
	provisionerB := insertClickHouseService(t, fx.s, orgB, fx.connectorID, "Cross-Org Provisioner B", nil)
	warehouseB := uuid.New()
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouses (id, org_id, name, provisioner_connector_id)
		VALUES ($1, $2, $3, $4)`,
		warehouseB.String(), orgB.String(), "Cross-Org Warehouse B", provisionerB.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE connectors SET warehouse_id = $1 WHERE id = $2`,
		warehouseB.String(), provisionerB.String())
	require.NoError(t, err)
	serviceB := insertClickHouseService(t, fx.s, orgB, fx.connectorID, "Cross-Org Service B", &warehouseB)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'everyone', 'everyone', $3, 'orgb_only')`,
		orgB.String(), warehouseB.String(), database)
	require.NoError(t, err)

	// Even a cross-org group membership row plus a raw grant naming that
	// foreign group (the API rejects both) must not import access: the
	// resolver joins memberships through org groups.
	groupB := seedGrantGroup(t, fx.s, orgB, "Cross-Org Group B")
	_, err = fx.s.db.Pool.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)`,
		groupB.String(), memberA.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx, `
		INSERT INTO warehouse_table_grants (org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'group', $3, $4, 'orgb_group_leak')`,
		fx.orgID.String(), fx.warehouseID.String(), groupB.String(), database)
	require.NoError(t, err)

	// Org A's non-admin sees only org A's effective grants; org B's grant is
	// invisible even though the table lives in the same ClickHouse database.
	rec := warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+serviceA.String()+"/schema", tokenA, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var schemaA executor.SchemaInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schemaA))
	gotA := map[string]bool{}
	for _, table := range schemaA.Tables {
		gotA[table.Schema+"."+table.Name] = true
	}
	require.True(t, gotA[database+".events"], "org A's own grant is visible")
	require.False(t, gotA[database+".orgb_only"], "org B's grant must not reach org A")
	require.False(t, gotA[database+"._tmp_orgb"])
	require.False(t, gotA[database+".orgb_group_leak"],
		"a foreign group's grant must not resolve through a cross-org membership row")

	// Org A's hidden pattern must not affect org B's connector: read as an org
	// B admin (no per-user filter) and require the ungranted pattern-matched
	// table to be visible.
	rec = warehouseAPIRequest(t, fx.s, http.MethodGet,
		"/api/v1/connectors/"+serviceB.String()+"/schema", adminB, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var schemaB executor.SchemaInfo
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &schemaB))
	gotB := map[string]bool{}
	for _, table := range schemaB.Tables {
		gotB[table.Schema+"."+table.Name] = true
	}
	require.True(t, gotB[database+"._tmp_orgb"],
		"org A's hidden pattern must not hide org B's tables")
	require.True(t, gotB[database+".orgb_only"])
}
