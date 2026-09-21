package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/chaccess"
)

// executeWarehouseFixture seeds a ready, reconciled warehouse plus two managed
// service connectors and one unmanaged ClickHouse connector. Reconciliation
// provisions the per-user identity for the fixture user with a direct grant on
// analytics.events and a group grant on analytics.daily_revenue (see
// seedWarehouseFixtureRows), which the execution tests exercise.
type executeWarehouseFixture struct {
	s           *Server
	key         []byte
	orgID       uuid.UUID
	userID      uuid.UUID
	warehouseID uuid.UUID
	connA       uuid.UUID
	connB       uuid.UUID
	unmanagedID uuid.UUID
}

// setupExecuteWarehouseFixture skips the test when the dev ClickHouse service
// is unreachable. The probe runs before the shared server is built so skipped
// tests stay fast.
func setupExecuteWarehouseFixture(t *testing.T) *executeWarehouseFixture {
	t.Helper()
	ctx := context.Background()

	requireClickHouseReachable(t)
	s, key := sharedWarehouseTestServer(t)
	seed := setupWarehouseFixtureWithServer(t, s, key)
	require.NoError(t, s.reconcileWarehouse(ctx, seed.warehouseID))
	// Run before the fixture's ClickHouse cleanup (LIFO), so no idle pooled
	// connection outlives the identity it authenticated as.
	t.Cleanup(func() { s.connPool.CloseAll() })

	return &executeWarehouseFixture{
		s:           s,
		key:         key,
		orgID:       seed.orgID,
		userID:      seed.userID,
		warehouseID: seed.warehouseID,
		connA:       insertClickHouseService(t, s, seed.orgID, seed.connectorID, "Execute Service A", &seed.warehouseID),
		connB:       insertClickHouseService(t, s, seed.orgID, seed.connectorID, "Execute Service B", &seed.warehouseID),
		unmanagedID: insertClickHouseService(t, s, seed.orgID, seed.connectorID, "Execute Unmanaged", nil),
	}
}

func (fx *executeWarehouseFixture) grantConnectorUse(t *testing.T, connectorID uuid.UUID) {
	t.Helper()
	grantConnectorUse(t, fx.s, fx.orgID, fx.userID, connectorID)
}

func (fx *executeWarehouseFixture) grantNotebookRun(t *testing.T, notebookID uuid.UUID) {
	t.Helper()
	grantNotebookRun(t, fx.s, fx.orgID, fx.userID, notebookID)
}

// seedExecuteWarehouseCell inserts a notebook and one SQL cell directly, since
// the warehouse fixtures live in package api and cannot use the api_test HTTP
// helpers.
func seedExecuteWarehouseCell(t *testing.T, s *Server, orgID, userID, connectorID uuid.UUID, source string, limit *int) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	notebookID := uuid.New()
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO notebooks (id, org_id, title, created_by)
		VALUES ($1, $2, $3, $4)`,
		notebookID.String(), orgID.String(), "Execute Warehouse Notebook", userID.String())
	require.NoError(t, err)

	cellID := uuid.New()
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO cells (id, notebook_id, position, type, language, connector_id, source, "limit")
		VALUES ($1, $2, 0, 'code', 'sql', $3, $4, $5)`,
		cellID.String(), notebookID.String(), connectorID.String(), source, limit)
	require.NoError(t, err)

	// Run before the fixture cleanup (LIFO): notebooks.created_by references
	// the fixture user, so the notebook must go before the user does.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM notebooks WHERE id = $1`, notebookID.String()); err != nil {
			t.Logf("cleanup notebook: %v", err)
		}
	})

	return notebookID, cellID
}

// executeWarehouseCell drives the full HTTP handler, including auth middleware,
// with a token for the fixture user.
func executeWarehouseCell(t *testing.T, s *Server, userID, orgID, notebookID, cellID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	return executeWarehouseCellBody(t, s, userID, orgID, notebookID, cellID, "")
}

// executeWarehouseCellBody is executeWarehouseCell with an explicit JSON body,
// used for request options such as pinned execution. An empty body sends none.
func executeWarehouseCellBody(t *testing.T, s *Server, userID, orgID, notebookID, cellID uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, err := s.jwt.Issue(userID.String(), orgID.String(), "admin")
	require.NoError(t, err)

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/notebooks/"+notebookID.String()+"/cells/"+cellID.String()+"/execute", reader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

type executeOutputs struct {
	Outputs []struct {
		Type string `json:"type"`
		Data struct {
			Columns []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"data"`
	} `json:"outputs"`
	// Routing is present only for warehouse-routed runs: the service and
	// warehouse that actually served the query.
	Routing *struct {
		WarehouseID   string `json:"warehouse_id"`
		WarehouseName string `json:"warehouse_name"`
		ConnectorID   string `json:"connector_id"`
		ConnectorName string `json:"connector_name"`
		CHUser        string `json:"ch_user"`
	} `json:"routing"`
}

func decodeExecuteOutputs(t *testing.T, rec *httptest.ResponseRecorder) executeOutputs {
	t.Helper()
	var out executeOutputs
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	require.NotEmpty(t, out.Outputs)
	return out
}

func TestExecuteCellUsesPerUserIdentity(t *testing.T) {
	ctx := context.Background()
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)

	// analytics.events is granted to the user directly, so the run must
	// succeed as the warehouse-scoped identity rather than the stored
	// provisioner credential.
	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT currentUser() AS ch_user, count() AS events FROM analytics.events", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.Len(t, out.Outputs[0].Data.Rows, 1)
	userIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID)
	require.Equal(t, userIdent, out.Outputs[0].Data.Rows[0][0])
	require.NotEmpty(t, out.Outputs[0].Data.Rows[0][1], "granted table must return rows")

	// The response reports the service and warehouse that served the run.
	require.NotNil(t, out.Routing, "warehouse-routed runs must include routing metadata")
	require.Equal(t, fx.warehouseID.String(), out.Routing.WarehouseID)
	require.Equal(t, "Warehouse Sync WH", out.Routing.WarehouseName)
	require.Equal(t, fx.connA.String(), out.Routing.ConnectorID)
	require.Equal(t, "Execute Service A", out.Routing.ConnectorName)
	require.Equal(t, userIdent, out.Routing.CHUser)

	// The audit entry records the routed warehouse, the service actually
	// dialed, and the per-user identity.
	var metaJSON []byte
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE org_id = $1 AND action = 'cell.execute' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`,
		fx.orgID.String(), cellID.String()).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	require.Equal(t, fx.warehouseID.String(), meta["warehouse_id"])
	require.Equal(t, fx.connA.String(), meta["connector_id"])
	require.Equal(t, userIdent, meta["ch_user"])
	executionID, _ := meta["execution_id"].(string)
	require.NotEmpty(t, executionID, "cell.execute audit must carry the execution id")

	// The query itself must be tagged in ClickHouse so system.query_log rows
	// join back to the audit entry. query_log flushes asynchronously, so flush
	// and poll instead of asserting once.
	provConn, err := openWarehouseProvisionerConn(ctx, warehouseSyncTestClickHouseConfig())
	require.NoError(t, err)
	defer provConn.Close()
	require.Eventually(t, func() bool {
		if err := provConn.Exec(ctx, "SYSTEM FLUSH LOGS"); err != nil {
			return false
		}
		var count uint64
		if err := provConn.QueryRow(ctx,
			`SELECT count() FROM system.query_log WHERE log_comment = $1`,
			"aether:"+executionID).Scan(&count); err != nil {
			return false
		}
		return count > 0
	}, 10*time.Second, 250*time.Millisecond,
		"system.query_log must contain the tagged query for execution %s", executionID)
}

func TestExecuteCellWarehouseTableDenied(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)

	// analytics.users exists in the dev dataset but is not granted to the
	// user's warehouse identity.
	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT count() FROM analytics.users", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	body := strings.ToLower(rec.Body.String())
	require.True(t,
		strings.Contains(body, "not enough privileges") || strings.Contains(body, "access_denied"),
		"expected a ClickHouse access-denied message, got: %s", rec.Body.String())
}

func TestExecuteCellUnmanagedClickHouseUsesLegacyCredential(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.unmanagedID)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.unmanagedID,
		"SELECT currentUser() AS ch_user", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.Len(t, out.Outputs[0].Data.Rows, 1)
	require.Equal(t, "dev", out.Outputs[0].Data.Rows[0][0],
		"an unmanaged connector must keep using its stored credential")
	require.Nil(t, out.Routing, "legacy runs must not report warehouse routing")
}

// Services in a warehouse are interchangeable: a collaborator holding `use`
// only on Service B must be able to run a cell wired to Service A, because
// routing sends the query to B. The connector-level pre-check must not gate
// managed connectors; resolveExecutionTarget enforces service access.
func TestExecuteCellRoutesByServiceAccessNotCellConnector(t *testing.T) {
	ctx := context.Background()
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connB)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT currentUser() AS ch_user", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.Equal(t, chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID),
		out.Outputs[0].Data.Rows[0][0], "the run must execute as the warehouse identity")
	require.NotNil(t, out.Routing)
	require.Equal(t, fx.connB.String(), out.Routing.ConnectorID,
		"the run must route to the only service the user may use")

	var metaJSON []byte
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE org_id = $1 AND action = 'cell.execute' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`,
		fx.orgID.String(), cellID.String()).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	require.Equal(t, fx.connB.String(), meta["connector_id"],
		"audit must record the service actually dialed")
}

// Without `use` on any service in the warehouse, managed routing fails closed
// even though handleExecuteCell no longer gates on the cell's connector.
func TestExecuteCellManagedNoServiceAccessDenied(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT 1", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "no permitted service in warehouse")
}

// Unmanaged connectors keep the connector-level `use` pre-check: access to a
// service in some warehouse must not leak into running an unmanaged connector.
func TestExecuteCellUnmanagedConnectorStillRequiresUse(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connB)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.unmanagedID,
		"SELECT currentUser()", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "permission to use this connector")
}

// With the kill switch off, a connector linked to a ready warehouse must still
// execute through its stored credential rather than the per-user identity, so
// an operator can roll back without unlinking warehouses. It runs on a
// dedicated server with the switch off, never toggling the shared fixture.
func TestKillSwitchOffRoutesManagedConnectorLegacy(t *testing.T) {
	requireClickHouseReachable(t)
	s, key := newKillSwitchTestServer(t)
	seed := seedWarehouseFixtureRows(t, s, key)
	connA := insertClickHouseService(t, s, seed.orgID, seed.connectorID, "Kill Switch Service", &seed.warehouseID)
	grantConnectorUse(t, s, seed.orgID, seed.userID, connA)

	nbID, cellID := seedExecuteWarehouseCell(t, s, seed.orgID, seed.userID, connA,
		"SELECT currentUser() AS ch_user", nil)
	grantNotebookRun(t, s, seed.orgID, seed.userID, nbID)

	rec := executeWarehouseCell(t, s, seed.userID, seed.orgID, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.Len(t, out.Outputs[0].Data.Rows, 1)
	require.Equal(t, "dev", out.Outputs[0].Data.Rows[0][0],
		"kill switch off must fall back to the connector's stored credential")

	// The legacy run must not claim a warehouse identity in the audit trail.
	var metaJSON []byte
	require.NoError(t, s.db.Pool.QueryRow(context.Background(), `
		SELECT metadata FROM audit_logs
		WHERE org_id = $1 AND action = 'cell.execute' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`,
		seed.orgID.String(), cellID.String()).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	_, hasWarehouse := meta["warehouse_id"]
	_, hasCHUser := meta["ch_user"]
	require.False(t, hasWarehouse, "legacy run must not record a warehouse_id")
	require.False(t, hasCHUser, "legacy run must not record a per-user ch_user")
	require.Equal(t, connA.String(), meta["connector_id"])
}

func TestExecuteCellNonClickHouseConnectorKeepsLegacyPath(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	// Linked to the warehouse on purpose: the type gate, not the warehouse
	// link, must decide the execution path.
	pgID := insertPostgresConnector(t, fx.s, fx.key, fx.orgID, "Execute Postgres", &fx.warehouseID)
	fx.grantConnectorUse(t, pgID)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, pgID,
		"SELECT 1 AS result", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.Len(t, out.Outputs[0].Data.Rows, 1)
	require.Equal(t, float64(1), out.Outputs[0].Data.Rows[0][0])
}

func TestExecuteCellServiceChoiceRequired(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)
	fx.grantConnectorUse(t, fx.connB)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA, "SELECT 1", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	var body struct {
		Error       string `json:"error"`
		WarehouseID string `json:"warehouse_id"`
		Services    []struct {
			ConnectorID string `json:"connector_id"`
			Name        string `json:"name"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "service_choice_required", body.Error)
	require.Equal(t, fx.warehouseID.String(), body.WarehouseID,
		"the 409 must name the warehouse so the choice can be stored")
	require.Len(t, body.Services, 2)
	names := map[string]string{}
	for _, svc := range body.Services {
		names[svc.ConnectorID] = svc.Name
	}
	require.Equal(t, "Execute Service A", names[fx.connA.String()])
	require.Equal(t, "Execute Service B", names[fx.connB.String()])
}

// A pinned run dials the cell's connector directly and ignores a stored
// preference, so it never hits the ambiguous-routing 409.
func TestExecuteCellPinnedConnectorBypassesPreference(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)
	fx.grantConnectorUse(t, fx.connB)
	preferWarehouseService(t, fx.s, fx.userID, fx.warehouseID, fx.connB)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT currentUser() AS ch_user", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCellBody(t, fx.s, fx.userID, fx.orgID, nbID, cellID, `{"pinned":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.NotNil(t, out.Routing)
	require.Equal(t, fx.connA.String(), out.Routing.ConnectorID,
		"a pinned run must dial the requested service, not the preferred one")
	require.Equal(t, chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID), out.Routing.CHUser)
}

func TestExecuteCellWarehouseNotReady(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE warehouses SET sync_status = 'pending' WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA, "SELECT 1", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
}

func TestExecuteCellManagedConnectorNotFound(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET deleted_at = now() WHERE id = $1`, fx.connA.String())
	require.NoError(t, err)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA, "SELECT 1", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestExecuteCellWarehouseAppliesCellLimit(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)

	limit := 1
	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT event_type FROM analytics.events", &limit)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.Len(t, out.Outputs[0].Data.Rows, 1, "the cell LIMIT must still be applied on the pooled path")
}

func TestExecuteCellSoftDeletedLegacyConnectorNotFound(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	pgID := insertPostgresConnector(t, fx.s, fx.key, fx.orgID, "Execute Postgres Deleted", nil)
	fx.grantConnectorUse(t, pgID)
	_, err := fx.s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET deleted_at = now() WHERE id = $1`, pgID.String())
	require.NoError(t, err)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, pgID, "SELECT 1", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestExecuteCellPreferenceRoutesAndLogsRoutedService(t *testing.T) {
	ctx := context.Background()
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)
	fx.grantConnectorUse(t, fx.connB)
	preferWarehouseService(t, fx.s, fx.userID, fx.warehouseID, fx.connB)

	// The cell requests Service A but the preference routes it to Service B.
	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT currentUser() AS ch_user", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.NotNil(t, out.Routing)
	require.Equal(t, fx.connB.String(), out.Routing.ConnectorID,
		"the response must report the routed service")

	var metaJSON []byte
	require.NoError(t, fx.s.db.Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE org_id = $1 AND action = 'cell.execute' AND resource_id = $2
		ORDER BY id DESC LIMIT 1`,
		fx.orgID.String(), cellID.String()).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	require.Equal(t, fx.connB.String(), meta["connector_id"], "audit must record the routed service")

	// The execution log is written asynchronously; it must record the routed
	// service, not the cell's requested connector.
	require.Eventually(t, func() bool {
		var logged string
		err := fx.s.db.Pool.QueryRow(ctx,
			`SELECT connector_id::text FROM cell_execution_logs WHERE cell_id = $1`,
			cellID.String()).Scan(&logged)
		return err == nil && logged == fx.connB.String()
	}, 5*time.Second, 50*time.Millisecond,
		"cell_execution_logs must record the routed connector")
}

func TestExecuteCellAppliesRoutedServiceLimits(t *testing.T) {
	ctx := context.Background()
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)
	fx.grantConnectorUse(t, fx.connB)
	preferWarehouseService(t, fx.s, fx.userID, fx.warehouseID, fx.connB)

	// The requested connector allows 1 row; the routed service allows 2. The
	// routed service's cap must win.
	_, err := fx.s.db.Pool.Exec(ctx,
		`UPDATE connectors SET max_rows = 1 WHERE id = $1`, fx.connA.String())
	require.NoError(t, err)
	_, err = fx.s.db.Pool.Exec(ctx,
		`UPDATE connectors SET max_rows = 2 WHERE id = $1`, fx.connB.String())
	require.NoError(t, err)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT event_type FROM analytics.events LIMIT 5", nil)
	fx.grantNotebookRun(t, nbID)

	rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	out := decodeExecuteOutputs(t, rec)
	require.Len(t, out.Outputs[0].Data.Rows, 2, "the routed service's max_rows must apply")
}

// MCP sessions build their own ToolContext: it must carry the warehouse
// execution hooks and resolve identities as the authenticated owner.
func TestMCPToolContextUsesOwnerIdentity(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)

	probe := &agent.ToolDef{Type: "function"}
	probe.Function.Name = "probe_warehouse_identity"
	probe.Function.Parameters = `{"type":"object","properties":{}}`
	probe.Handler = func(_ json.RawMessage, tc *agent.ToolContext) (any, error) {
		if tc.ResolveTarget == nil || tc.ConnPool == nil || tc.CheckPermissionFunc == nil {
			return nil, fmt.Errorf("MCP tool context is missing warehouse execution hooks")
		}
		userID, err := uuid.Parse(tc.UserID)
		if err != nil {
			return nil, fmt.Errorf("tool context user id: %w", err)
		}
		target, err := tc.ResolveTarget(tc.Context, userID, fx.connA, false)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ch_user": target.CHUser}, nil
	}
	fx.s.RegisterToolForTest(probe)
	fx.s.SetMCPToolAllowedForTest("probe_warehouse_identity", true)
	t.Cleanup(func() { fx.s.SetMCPToolAllowedForTest("probe_warehouse_identity", false) })

	claims := &auth.Claims{UserID: fx.userID.String(), OrgID: fx.orgID.String(), Role: "admin"}
	params, err := json.Marshal(map[string]any{"name": "probe_warehouse_identity", "arguments": map[string]any{}})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", nil)
	rec := httptest.NewRecorder()
	fx.s.handleMCPToolsCall(rec, mcpJSONRPCRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "tools/call", Params: params}, claims, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// The resolver must derive the per-user identity for the claims owner.
	require.Contains(t, rec.Body.String(), chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID),
		"MCP must resolve the authenticated owner's warehouse identity")
}

func TestExecuteCellReusesPooledLease(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	fx.grantConnectorUse(t, fx.connA)

	nbID, cellID := seedExecuteWarehouseCell(t, fx.s, fx.orgID, fx.userID, fx.connA,
		"SELECT currentUser() AS ch_user", nil)
	fx.grantNotebookRun(t, nbID)

	for run := 1; run <= 2; run++ {
		rec := executeWarehouseCell(t, fx.s, fx.userID, fx.orgID, nbID, cellID)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, 1, fx.s.connPool.Len(),
			"run %d must reuse the lease pooled for this identity", run)
	}
}
