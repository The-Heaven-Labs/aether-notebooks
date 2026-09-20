package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/models"
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
// is unreachable (setupWarehouseFixtureWithServer handles that check).
func setupExecuteWarehouseFixture(t *testing.T) *executeWarehouseFixture {
	t.Helper()
	ctx := context.Background()

	s, key := newWarehouseSyncTestServer(t)
	seed := setupWarehouseFixtureWithServer(t, s, key)
	require.NoError(t, s.reconcileWarehouse(ctx, seed.warehouseID))
	// Run before the fixture's ClickHouse cleanup (LIFO), so no idle pooled
	// connection outlives the identity it authenticated as.
	t.Cleanup(func() { s.connPool.CloseAll() })

	var encrypted []byte
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT config_encrypted FROM connectors WHERE id = $1`,
		seed.connectorID.String()).Scan(&encrypted))

	insertClickHouse := func(name string, warehouseID *uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		var wh any
		if warehouseID != nil {
			wh = warehouseID.String()
		}
		_, err := s.db.Pool.Exec(ctx, `
			INSERT INTO connectors (id, org_id, name, type, config_encrypted, warehouse_id)
			VALUES ($1, $2, $3, 'clickhouse', $4, $5)`,
			id.String(), seed.orgID.String(), name, encrypted, wh)
		require.NoError(t, err)
		return id
	}

	return &executeWarehouseFixture{
		s:           s,
		key:         key,
		orgID:       seed.orgID,
		userID:      seed.userID,
		warehouseID: seed.warehouseID,
		connA:       insertClickHouse("Execute Service A", &seed.warehouseID),
		connB:       insertClickHouse("Execute Service B", &seed.warehouseID),
		unmanagedID: insertClickHouse("Execute Unmanaged", nil),
	}
}

func (fx *executeWarehouseFixture) grantConnectorUse(t *testing.T, connectorID uuid.UUID) {
	t.Helper()
	_, err := fx.s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'connector', $2::uuid, 'user', $3, ARRAY['view','use'])`,
		fx.orgID.String(), connectorID.String(), fx.userID.String())
	require.NoError(t, err)
}

func (fx *executeWarehouseFixture) grantNotebookRun(t *testing.T, notebookID uuid.UUID) {
	t.Helper()
	_, err := fx.s.db.Pool.Exec(context.Background(), `
		INSERT INTO acl_entries (org_id, resource_type, resource_id, subject_type, subject_id, actions)
		VALUES ($1, 'notebook', $2::uuid, 'user', $3, ARRAY['view','run'])`,
		fx.orgID.String(), notebookID.String(), fx.userID.String())
	require.NoError(t, err)
}

// insertPostgresConnector adds a legacy connector. It is linked to the
// warehouse on purpose in the non-ClickHouse test so the type gate, not the
// warehouse link, decides the execution path.
func (fx *executeWarehouseFixture) insertPostgresConnector(t *testing.T, name string, warehouseID *uuid.UUID) uuid.UUID {
	t.Helper()
	cfg := models.ConnectorConfig{
		Host: "localhost", Port: 5432, User: "aether", Password: "aether_dev", Database: "aether",
	}
	plain, err := json.Marshal(cfg)
	require.NoError(t, err)
	encrypted, err := crypto.Encrypt(plain, fx.key)
	require.NoError(t, err)

	id := uuid.New()
	var wh any
	if warehouseID != nil {
		wh = warehouseID.String()
	}
	_, err = fx.s.db.Pool.Exec(context.Background(), `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted, warehouse_id)
		VALUES ($1, $2, $3, 'postgres', $4, $5)`,
		id.String(), fx.orgID.String(), name, encrypted, wh)
	require.NoError(t, err)
	return id
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
	token, err := s.jwt.Issue(userID.String(), orgID.String(), "admin")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/notebooks/"+notebookID.String()+"/cells/"+cellID.String()+"/execute", nil)
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
}

func TestExecuteCellNonClickHouseConnectorKeepsLegacyPath(t *testing.T) {
	fx := setupExecuteWarehouseFixture(t)
	pgID := fx.insertPostgresConnector(t, "Execute Postgres", &fx.warehouseID)
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
		Error    string `json:"error"`
		Services []struct {
			ConnectorID string `json:"connector_id"`
			Name        string `json:"name"`
		} `json:"services"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "service_choice_required", body.Error)
	require.Len(t, body.Services, 2)
	names := map[string]string{}
	for _, svc := range body.Services {
		names[svc.ConnectorID] = svc.Name
	}
	require.Equal(t, "Execute Service A", names[fx.connA.String()])
	require.Equal(t, "Execute Service B", names[fx.connB.String()])
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
