package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/agent"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/executor"
)

// The provisioner execution override defaults to off and is part of the
// warehouse read model the admin UI will toggle.
func TestWarehouseDefaultsAllowProvisionerExecutionFalse(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	wh, err := s.loadWarehouseForOrg(context.Background(), fx.orgID.String(), fx.warehouseID)
	require.NoError(t, err)
	require.False(t, wh.AllowProvisionerExecution)
}

func TestUpdateWarehouseTogglesAllowProvisionerExecution(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut,
			"/api/v1/warehouses/"+fx.warehouseID.String(), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	storedFlag := func() bool {
		var stored bool
		require.NoError(t, s.db.Pool.QueryRow(ctx,
			`SELECT allow_provisioner_execution FROM warehouses WHERE id = $1`,
			fx.warehouseID.String()).Scan(&stored))
		return stored
	}
	latestAuditMeta := func() map[string]any {
		var metaJSON []byte
		require.NoError(t, s.db.Pool.QueryRow(ctx, `
			SELECT metadata FROM audit_logs
			WHERE action = 'warehouse.update' AND resource_id = $1
			ORDER BY id DESC LIMIT 1`,
			fx.warehouseID.String()).Scan(&metaJSON))
		var meta map[string]any
		require.NoError(t, json.Unmarshal(metaJSON, &meta))
		return meta
	}

	rec := put(`{"allow_provisioner_execution": true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var wh warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wh))
	require.True(t, wh.AllowProvisionerExecution)
	require.True(t, storedFlag(), "the toggle must persist")
	require.Equal(t, true, latestAuditMeta()["allow_provisioner_execution"])

	rec = put(`{"allow_provisioner_execution": false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wh))
	require.False(t, wh.AllowProvisionerExecution)
	require.False(t, storedFlag())
	meta := latestAuditMeta()
	require.Equal(t, false, meta["allow_provisioner_execution"])
	require.Equal(t, true, meta["previous_allow_provisioner_execution"])

	require.Equal(t, http.StatusOK, put(`{"allow_provisioner_execution": true}`).Code)
	require.True(t, storedFlag())

	// A name-only update leaves the flag unchanged.
	rec = put(`{"name": "Renamed Warehouse"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.True(t, storedFlag(), "an absent toggle must leave the value unchanged")

	// An empty body is still rejected.
	require.Equal(t, http.StatusBadRequest, put(`{}`).Code)
}

// A provisioner connector must fail closed for user execution, and must never
// be reported as unmanaged (which would mean the stored-credential path).
func TestResolveExecutionTargetBlocksProvisionerByDefault(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	_, err := s.resolveExecutionTarget(context.Background(), fx.userID, fx.connectorID, false)
	require.ErrorIs(t, err, executor.ErrProvisionerNotExecutable)
	require.NotErrorIs(t, err, executor.ErrUnmanagedConnector)
}

// With the override on and managed mode enabled, the provisioner resolves as a
// normal managed service: per-user identity, never the stored credential.
func TestResolveExecutionTargetAllowsProvisionerWhenEnabled(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET sync_status = 'ready', allow_provisioner_execution = true WHERE id = $1`,
		fx.warehouseID.String())
	require.NoError(t, err)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	target, err := s.resolveExecutionTarget(ctx, fx.userID, fx.connectorID, false)
	require.NoError(t, err)
	require.Equal(t, fx.connectorID, target.ConnectorID)
	require.Equal(t, chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID), target.CHUser)
	require.NotEqual(t, "dev", target.Config.Password,
		"the provisioner's stored credential must never reach execution")
}

// The override cannot unblock the kill-switch-off stored-credential fallback.
func TestResolveExecutionTargetBlocksProvisionerWhenKillSwitchOff(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	s.SetCHTablePermissions(false)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`,
		fx.warehouseID.String())
	require.NoError(t, err)

	_, err = s.resolveExecutionTarget(ctx, fx.userID, fx.connectorID, false)
	require.ErrorIs(t, err, executor.ErrProvisionerNotExecutable)
	require.NotErrorIs(t, err, executor.ErrUnmanagedConnector)
}

// Even with the override on, a provisioner whose warehouse link is missing
// (an integrity violation no API path can produce) must fail closed rather
// than fall through to the stored-credential path.
func TestResolveExecutionTargetBlocksProvisionerWithMissingWarehouseLink(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	_, err := s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET sync_status = 'ready', allow_provisioner_execution = true WHERE id = $1`,
		fx.warehouseID.String())
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx,
		`UPDATE connectors SET warehouse_id = NULL WHERE id = $1`, fx.connectorID.String())
	require.NoError(t, err)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	_, err = s.resolveExecutionTarget(ctx, fx.userID, fx.connectorID, false)
	require.ErrorIs(t, err, executor.ErrProvisionerNotExecutable)
	require.NotErrorIs(t, err, executor.ErrUnmanagedConnector)
}

func TestExecuteCellOnProvisionerReturns403(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	// No grantConnectorUse: with the kill switch on, a managed ClickHouse
	// connector skips the connector-level `use` pre-check in the handler.
	nbID, cellID := seedExecuteWarehouseCell(t, s, fx.orgID, fx.userID, fx.connectorID,
		"SELECT currentUser()", nil)
	grantNotebookRun(t, s, fx.orgID, fx.userID, nbID)

	rec := executeWarehouseCell(t, s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	body := strings.ToLower(rec.Body.String())
	require.Contains(t, body, "provisioner")
	require.Contains(t, body, "cannot run queries")
}

// With the kill switch off the handler would normally fall back to the stored
// credential. The provisioner config points at an unreachable host, so a
// fallback would surface as a connection/gateway error instead of the block.
func TestExecuteCellOnProvisionerKillSwitchOffReturns403(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	s.SetCHTablePermissions(false)
	fx := seedWarehouseFixtureRows(t, s, key)
	pointProvisionerAtUnreachableHost(t, s, key, fx.connectorID)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	nbID, cellID := seedExecuteWarehouseCell(t, s, fx.orgID, fx.userID, fx.connectorID,
		"SELECT currentUser()", nil)
	grantNotebookRun(t, s, fx.orgID, fx.userID, nbID)

	rec := executeWarehouseCell(t, s, fx.userID, fx.orgID, nbID, cellID)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	body := strings.ToLower(rec.Body.String())
	require.Contains(t, body, "provisioner")
	require.Contains(t, body, "cannot run queries")
}

// The agent execute_sql tool shares resolveExecutionTarget; it must surface a
// provisioner-specific, actionable error and never open the stored credential.
func TestAgentExecuteSQLBlocksProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)
	ctx := context.Background()

	def, ok := s.agentEngine.GetRegistry().Get("execute_sql")
	require.True(t, ok, "execute_sql must be registered")
	args, err := json.Marshal(map[string]any{
		"connector_id": fx.connectorID.String(),
		"query":        "SELECT currentUser()",
	})
	require.NoError(t, err)

	tc := &agent.ToolContext{
		Context:             ctx,
		UserID:              fx.userID.String(),
		OrgID:               fx.orgID.String(),
		OrgRole:             "admin",
		DB:                  s.db.Pool,
		MasterKey:           s.masterKey,
		ResolveTarget:       s.resolveExecutionTarget,
		ConnPool:            s.connPool,
		CheckPermissionFunc: s.checkPermission,
	}
	_, err = def.Execute(args, tc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is the warehouse provisioner and cannot run queries")
}

func connectorIntrospectionRequest(t *testing.T, s *Server, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestProvisionerIntrospectionBlockedForNonAdmin(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "non-admin")
	require.NoError(t, err)
	base := "/api/v1/connectors/" + fx.connectorID.String()

	rec := connectorIntrospectionRequest(t, s, http.MethodGet, base+"/schema", token)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "provisioner")

	rec = connectorIntrospectionRequest(t, s, http.MethodGet, base+"/databases", token)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "provisioner")

	rec = connectorIntrospectionRequest(t, s, http.MethodPost, base+"/test", token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"ok":false`)
}

// Org admins keep introspection so the warehouse grant-management table
// browser keeps working when the provisioner is the only linked connector.
// The connector config points at an unreachable host: getting past the guard
// surfaces as a gateway error, not a 403.
func TestProvisionerIntrospectionAllowedForOrgAdmin(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	pointProvisionerAtUnreachableHost(t, s, key, fx.connectorID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)
	base := "/api/v1/connectors/" + fx.connectorID.String()

	rec := connectorIntrospectionRequest(t, s, http.MethodGet, base+"/schema", token)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())

	rec = connectorIntrospectionRequest(t, s, http.MethodGet, base+"/databases", token)
	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())

	rec = connectorIntrospectionRequest(t, s, http.MethodPost, base+"/test", token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"ok":false`)
}

func TestListWarehouseServicesExcludesBlockedProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()
	serviceID := insertClickHouseService(t, s, fx.orgID, fx.connectorID, "Listed Service", &fx.warehouseID)

	services, err := s.listWarehouseServices(ctx, fx.warehouseID, fx.orgID)
	require.NoError(t, err)
	require.Len(t, services, 1)
	require.Equal(t, serviceID, services[0].id)

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)
	services, err = s.listWarehouseServices(ctx, fx.warehouseID, fx.orgID)
	require.NoError(t, err)
	require.Len(t, services, 2)
	require.ElementsMatch(t, []uuid.UUID{serviceID, fx.connectorID},
		[]uuid.UUID{services[0].id, services[1].id})
}

func TestSetWarehousePreferenceRejectsBlockedProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	rec := putPreferenceViaAPI(t, s, token, fx.warehouseID, &fx.connectorID)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Nil(t, warehousePreference(t, s, fx.userID, fx.warehouseID),
		"a rejected preference must not be stored")

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)
	rec = putPreferenceViaAPI(t, s, token, fx.warehouseID, &fx.connectorID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	preferred := warehousePreference(t, s, fx.userID, fx.warehouseID)
	require.NotNil(t, preferred)
	require.Equal(t, fx.connectorID.String(), *preferred)
}

// A subject whose only `use` grant is the blocked provisioner must not be
// reported as having service access: grant creation warns and the validation
// endpoint flags the unusable table grants.
func TestGrantWarningAndValidationFlagBlockedProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	// The fixture pre-seeds user and group table grants. Start from a clean
	// slate so the created grant is a fresh insert and the group's grant does
	// not add a second "granted but unusable" subject to the validation list.
	_, err := s.db.Pool.Exec(ctx,
		`DELETE FROM warehouse_table_grants WHERE warehouse_id = $1`, fx.warehouseID.String())
	require.NoError(t, err)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	rec := createGrantViaAPI(t, s, token, fx.warehouseID,
		grantBody("user", fx.userID.String(), "analytics", "events"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, "no_service_access", created.Warning,
		"the blocked provisioner must not count as service access")

	rec = warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+fx.warehouseID.String()+"/validation", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var validation warehouseValidationJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &validation))
	require.Len(t, validation.TablesWithoutService, 1)
	require.Equal(t, fx.userID.String(), validation.TablesWithoutService[0].SubjectID)

	// With the override on, the provisioner is a usable service again.
	_, err = s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)

	rec = createGrantViaAPI(t, s, token, fx.warehouseID,
		grantBody("user", fx.userID.String(), "analytics", "daily_revenue"))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var afterOverride warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &afterOverride))
	require.Empty(t, afterOverride.Warning, "with the override on the provisioner is a service")

	rec = warehouseAPIRequest(t, s, http.MethodGet,
		"/api/v1/warehouses/"+fx.warehouseID.String()+"/validation", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &validation))
	require.Empty(t, validation.TablesWithoutService)
}

type listedConnector struct {
	ID            string `json:"id"`
	CanUse        bool   `json:"can_use"`
	IsProvisioner bool   `json:"is_provisioner"`
}

func TestListConnectorsMarksBlockedProvisioner(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	listConnectors := func(tok string) []listedConnector {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/connectors", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var out []listedConnector
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		return out
	}

	find := func(rows []listedConnector) *listedConnector {
		for i := range rows {
			if rows[i].ID == fx.connectorID.String() {
				return &rows[i]
			}
		}
		return nil
	}

	row := find(listConnectors(token))
	require.NotNil(t, row)
	require.True(t, row.IsProvisioner)
	require.False(t, row.CanUse, "a blocked provisioner must not be offered as usable")

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)

	row = find(listConnectors(token))
	require.NotNil(t, row)
	require.True(t, row.IsProvisioner)
	require.True(t, row.CanUse, "the override restores managed usability")

	// No connector view ⇒ the provisioner must not even be disclosed.
	_, memberToken := seedGrantOrgMember(t, s, fx.orgID, "non-admin")
	require.Nil(t, find(listConnectors(memberToken)))
}

// listConnectorsForOrg lists connectors over the API and keys them by ID.
func listConnectorsForOrg(t *testing.T, s *Server, token string) map[string]listedConnector {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connectors", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var rows []listedConnector
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rows))
	byID := make(map[string]listedConnector, len(rows))
	for _, c := range rows {
		byID[c.ID] = c
	}
	return byID
}

// The kill switch off alone blocks a provisioner even when the warehouse
// override is on, while a warehouse-linked non-provisioner keeps its
// ACL-derived can_use: the isProvisioner=false short-circuit must preserve
// normal managed behavior.
func TestListConnectorsKillSwitchOffBlocksOnlyProvisioners(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	s.SetCHTablePermissions(false)
	fx := seedWarehouseFixtureRows(t, s, key)
	ctx := context.Background()

	plainID := insertClickHouseService(t, s, fx.orgID, fx.connectorID, "Plain Service", &fx.warehouseID)
	grantConnectorUse(t, s, fx.orgID, fx.userID, fx.connectorID)
	grantConnectorUse(t, s, fx.orgID, fx.userID, plainID)

	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	rows := listConnectorsForOrg(t, s, token)
	require.Contains(t, rows, plainID.String())
	require.False(t, rows[plainID.String()].IsProvisioner)
	require.True(t, rows[plainID.String()].CanUse, "a warehouse-linked non-provisioner keeps its ACL use")
	require.Contains(t, rows, fx.connectorID.String())
	require.False(t, rows[fx.connectorID.String()].CanUse)

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE warehouses SET allow_provisioner_execution = true WHERE id = $1`, fx.warehouseID.String())
	require.NoError(t, err)

	rows = listConnectorsForOrg(t, s, token)
	require.Contains(t, rows, fx.connectorID.String())
	require.True(t, rows[fx.connectorID.String()].IsProvisioner)
	require.False(t, rows[fx.connectorID.String()].CanUse,
		"the kill switch must keep the provisioner unusable even with the override on")
	require.Contains(t, rows, plainID.String())
	require.True(t, rows[plainID.String()].CanUse)
}
