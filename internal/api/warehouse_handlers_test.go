package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// warehouseEnqueueRecorder captures sync enqueues so CRUD tests observe
// scheduling without running a reconcile against ClickHouse.
type warehouseEnqueueRecorder struct {
	mu  sync.Mutex
	ids []uuid.UUID
}

func (r *warehouseEnqueueRecorder) Enqueue(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
}

func (r *warehouseEnqueueRecorder) contains(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, got := range r.ids {
		if got == id {
			return true
		}
	}
	return false
}

func warehouseEnqueues(t *testing.T, s *Server) *warehouseEnqueueRecorder {
	t.Helper()
	rec, ok := s.warehouseSync.(*warehouseEnqueueRecorder)
	require.True(t, ok, "test server must use the enqueue recorder")
	return rec
}

// seedWarehouseOrgAdmin creates an org with one admin user and returns its ID
// and a token. Rows are cleaned up before the shared server/database close.
func seedWarehouseOrgAdmin(t *testing.T, s *Server) (orgID, userID uuid.UUID, token string) {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()
	orgID, userID = uuid.New(), uuid.New()

	_, err := s.db.Pool.Exec(ctx, `INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
		orgID.String(), "Warehouse Handler Org", "wh-handler-"+suffix)
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), "wh-handler-"+suffix+"@test.local", "Warehouse Handler Admin")
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
		orgID.String(), userID.String())
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM orgs WHERE id = $1`, orgID.String()); err != nil {
			t.Logf("cleanup org: %v", err)
		}
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID.String()); err != nil {
			t.Logf("cleanup user: %v", err)
		}
	})

	token, err = s.jwt.Issue(userID.String(), orgID.String(), "admin")
	require.NoError(t, err)
	return orgID, userID, token
}

// setupTestServerWithOrgAdmin builds an isolated Server against the test
// database seeded with one org and its admin. The production sync worker is
// replaced by a recorder so CRUD tests never touch ClickHouse.
func setupTestServerWithOrgAdmin(t *testing.T) (*Server, uuid.UUID, string) {
	t.Helper()
	s, _ := newWarehouseSyncTestServer(t)
	s.SetWarehouseSyncerForTest(&warehouseEnqueueRecorder{})
	orgID, _, token := seedWarehouseOrgAdmin(t, s)
	return s, orgID, token
}

// seedWarehouseOrgMemberToken adds a non-admin member to an existing org.
func seedWarehouseOrgMemberToken(t *testing.T, s *Server, orgID uuid.UUID, role string) string {
	t.Helper()
	ctx := context.Background()
	suffix := uuid.NewString()
	userID := uuid.New()

	_, err := s.db.Pool.Exec(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(), "wh-member-"+suffix+"@test.local", "Warehouse Handler Member")
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`,
		orgID.String(), userID.String(), role)
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM users WHERE id = $1`, userID.String()); err != nil {
			t.Logf("cleanup member: %v", err)
		}
	})

	token, err := s.jwt.Issue(userID.String(), orgID.String(), role)
	require.NoError(t, err)
	return token
}

// seedWarehouseConnector inserts a ClickHouse connector row for link tests.
// Nothing decrypts or dials the stored config in these tests.
func seedWarehouseConnector(t *testing.T, s *Server, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted)
		VALUES ($1, $2, $3, 'clickhouse', $4)`,
		id.String(), orgID.String(), name, []byte("{}"))
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := s.db.Pool.Exec(cleanupCtx, `DELETE FROM connectors WHERE id = $1`, id.String()); err != nil {
			t.Logf("cleanup connector: %v", err)
		}
	})
	return id
}

// warehouseOrgAdminUserID returns the first member of the org (the seeded
// admin) for preference rows.
func warehouseOrgAdminUserID(t *testing.T, s *Server, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var userID string
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT user_id FROM org_members WHERE org_id = $1 ORDER BY created_at ASC LIMIT 1`,
		orgID.String()).Scan(&userID))
	parsed, err := uuid.Parse(userID)
	require.NoError(t, err)
	return parsed
}

func connectorWarehouse(t *testing.T, s *Server, connectorID uuid.UUID) *string {
	t.Helper()
	var warehouseID *string
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT warehouse_id FROM connectors WHERE id = $1`, connectorID.String()).Scan(&warehouseID))
	return warehouseID
}

func countWarehouseGrants(t *testing.T, s *Server, warehouseID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM warehouse_table_grants WHERE warehouse_id = $1`, warehouseID.String()).Scan(&n))
	return n
}

func countWarehousePreferences(t *testing.T, s *Server, warehouseID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM warehouse_service_preferences WHERE warehouse_id = $1`, warehouseID.String()).Scan(&n))
	return n
}

// warehouseAPIRequest drives the full HTTP stack, including auth middleware.
// Admin mode is set so admin-only ACL bypasses behave like the UI's.
func warehouseAPIRequest(t *testing.T, s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AETHER-Admin-Mode", "true")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func createWarehouseViaAPI(t *testing.T, s *Server, token, name string) uuid.UUID {
	t.Helper()
	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", token, map[string]any{"name": name})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var wh warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wh))
	id, err := uuid.Parse(wh.ID)
	require.NoError(t, err)
	return id
}

func listWarehousesViaAPI(t *testing.T, s *Server, token string) []warehouseJSON {
	t.Helper()
	rec := warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses", token, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list []warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	return list
}

func updateWarehouseViaAPI(t *testing.T, s *Server, token string, warehouseID uuid.UUID, patch map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return warehouseAPIRequest(t, s, http.MethodPut, "/api/v1/warehouses/"+warehouseID.String(), token, patch)
}

func deleteWarehouseViaAPI(t *testing.T, s *Server, token string, warehouseID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	return warehouseAPIRequest(t, s, http.MethodDelete, "/api/v1/warehouses/"+warehouseID.String(), token, nil)
}

func TestWarehouseCRUD(t *testing.T) {
	s, orgID, admin := setupTestServerWithOrgAdmin(t)

	wh := createWarehouseViaAPI(t, s, admin, "DWH Prod")

	list := listWarehousesViaAPI(t, s, admin)
	require.Len(t, list, 1)
	require.Equal(t, wh.String(), list[0].ID)
	require.Equal(t, orgID.String(), list[0].OrgID)
	require.Equal(t, "DWH Prod", list[0].Name)
	require.Equal(t, "pending", list[0].SyncStatus)
	require.Nil(t, list[0].ProvisionerConnectorID)

	dup := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{"name": "DWH Prod"})
	require.Equal(t, http.StatusConflict, dup.Code, dup.Body.String())

	blank := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{"name": "   "})
	require.Equal(t, http.StatusBadRequest, blank.Code)

	update := updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"name": "DWH Prod 2"})
	require.Equal(t, http.StatusOK, update.Code, update.Body.String())
	var updated warehouseJSON
	require.NoError(t, json.Unmarshal(update.Body.Bytes(), &updated))
	require.Equal(t, "DWH Prod 2", updated.Name)

	get := warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses/"+wh.String(), admin, nil)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	var got warehouseJSON
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &got))
	require.Equal(t, "DWH Prod 2", got.Name)

	deleteRec := deleteWarehouseViaAPI(t, s, admin, wh)
	require.Equal(t, http.StatusNoContent, deleteRec.Code, deleteRec.Body.String())

	require.Empty(t, listWarehousesViaAPI(t, s, admin))
	require.Equal(t, http.StatusNotFound,
		warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses/"+wh.String(), admin, nil).Code)

	missing := uuid.New()
	require.Equal(t, http.StatusNotFound,
		updateWarehouseViaAPI(t, s, admin, missing, map[string]any{"name": "X"}).Code)
	require.Equal(t, http.StatusNotFound, deleteWarehouseViaAPI(t, s, admin, missing).Code)
}

func TestWarehouseRoutesRequireOrgAdmin(t *testing.T) {
	s, orgID, admin := setupTestServerWithOrgAdmin(t)
	wh := createWarehouseViaAPI(t, s, admin, "Admin Only WH")
	member := seedWarehouseOrgMemberToken(t, s, orgID, "non-admin")

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/warehouses", nil},
		{http.MethodPost, "/api/v1/warehouses", map[string]any{"name": "Denied"}},
		{http.MethodGet, "/api/v1/warehouses/" + wh.String(), nil},
		{http.MethodPut, "/api/v1/warehouses/" + wh.String(), map[string]any{"name": "Denied"}},
		{http.MethodDelete, "/api/v1/warehouses/" + wh.String(), nil},
		{http.MethodPut, "/api/v1/warehouses/" + wh.String() + "/provisioner", map[string]any{}},
		{http.MethodPut, "/api/v1/connectors/" + uuid.NewString() + "/warehouse", map[string]any{"warehouse_id": wh.String()}},
	} {
		rec := warehouseAPIRequest(t, s, tc.method, tc.path, member, tc.body)
		require.Equal(t, http.StatusForbidden, rec.Code, "%s %s: %s", tc.method, tc.path, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/warehouses", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWarehouseListScopedToOrg(t *testing.T) {
	s, _, adminA := setupTestServerWithOrgAdmin(t)
	_, _, adminB := seedWarehouseOrgAdmin(t, s)

	whA := createWarehouseViaAPI(t, s, adminA, "Scoped Warehouse A")
	whB := createWarehouseViaAPI(t, s, adminB, "Scoped Warehouse B")

	listA := listWarehousesViaAPI(t, s, adminA)
	require.Len(t, listA, 1)
	require.Equal(t, whA.String(), listA[0].ID)

	require.Equal(t, http.StatusNotFound,
		warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses/"+whB.String(), adminA, nil).Code)
	require.Equal(t, http.StatusNotFound,
		updateWarehouseViaAPI(t, s, adminA, whB, map[string]any{"name": "Stolen"}).Code)
	require.Equal(t, http.StatusNotFound, deleteWarehouseViaAPI(t, s, adminA, whB).Code)
}

func TestWarehouseProvisionerLifecycle(t *testing.T) {
	s, orgID, admin := setupTestServerWithOrgAdmin(t)
	wh := createWarehouseViaAPI(t, s, admin, "Provisioner WH")
	linked := seedWarehouseConnector(t, s, orgID, "Linked Service")
	unlinked := seedWarehouseConnector(t, s, orgID, "Unlinked Service")

	// A connector that is not part of the warehouse cannot provision it.
	rec := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": linked.String()})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// Link first, then designate.
	link := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+linked.String()+"/warehouse", admin,
		map[string]any{"warehouse_id": wh.String()})
	require.Equal(t, http.StatusOK, link.Code, link.Body.String())
	require.True(t, warehouseEnqueues(t, s).contains(wh), "linking a connector must enqueue the warehouse")

	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": linked.String()})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var updated warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &updated))
	require.NotNil(t, updated.ProvisionerConnectorID)
	require.Equal(t, linked.String(), *updated.ProvisionerConnectorID)
	require.Equal(t, "pending", updated.SyncStatus)
	require.True(t, warehouseEnqueues(t, s).contains(wh), "setting a provisioner must enqueue a sync")

	// An unlinked connector in the same org is still rejected.
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": unlinked.String()})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// A connector from another org is rejected.
	orgB, _, _ := seedWarehouseOrgAdmin(t, s)
	otherOrgConnector := seedWarehouseConnector(t, s, orgB, "Other Org Service")
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": otherOrgConnector.String()})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// The warehouse update endpoint accepts the same field.
	rec = updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"provisioner_connector_id": linked.String()})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Clearing the provisioner is allowed.
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": nil})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var cleared warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &cleared))
	require.Nil(t, cleared.ProvisionerConnectorID)
}

func TestWarehouseCreateAdoptsProvisioner(t *testing.T) {
	s, orgID, admin := setupTestServerWithOrgAdmin(t)
	conn := seedWarehouseConnector(t, s, orgID, "Create Adopt Service")

	rec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "Adopt WH",
		"provisioner_connector_id": conn.String(),
	})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var wh warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wh))
	require.NotNil(t, wh.ProvisionerConnectorID)
	require.Equal(t, conn.String(), *wh.ProvisionerConnectorID)

	// The connector is adopted into the new warehouse and synced.
	require.Equal(t, wh.ID, *connectorWarehouse(t, s, conn))
	warehouseUUID, err := uuid.Parse(wh.ID)
	require.NoError(t, err)
	require.True(t, warehouseEnqueues(t, s).contains(warehouseUUID))

	// A connector already linked to another warehouse cannot be adopted.
	rec = warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "Second WH",
		"provisioner_connector_id": conn.String(),
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// A connector from another org cannot be adopted either.
	orgB, _, _ := seedWarehouseOrgAdmin(t, s)
	otherOrgConnector := seedWarehouseConnector(t, s, orgB, "Other Org Adopt Service")
	rec = warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "Third WH",
		"provisioner_connector_id": otherOrgConnector.String(),
	})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestConnectorWarehouseLinkCrossOrgRejected(t *testing.T) {
	s, orgA, adminA := setupTestServerWithOrgAdmin(t)
	orgB, _, adminB := seedWarehouseOrgAdmin(t, s)

	connA := seedWarehouseConnector(t, s, orgA, "Cross Org Connector A")
	connB := seedWarehouseConnector(t, s, orgB, "Cross Org Connector B")
	whA := createWarehouseViaAPI(t, s, adminA, "Cross Org WH A")
	whB := createWarehouseViaAPI(t, s, adminB, "Cross Org WH B")

	// A warehouse from another org is rejected and the link stays unset.
	rec := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+connA.String()+"/warehouse", adminA,
		map[string]any{"warehouse_id": whB.String()})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Nil(t, connectorWarehouse(t, s, connA))

	// A connector from another org is invisible to this org's admin.
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+connB.String()+"/warehouse", adminA,
		map[string]any{"warehouse_id": whA.String()})
	require.Equal(t, http.StatusNotFound, rec.Code)

	// An unknown warehouse ID is rejected.
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+connA.String()+"/warehouse", adminA,
		map[string]any{"warehouse_id": uuid.NewString()})
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWarehouseDeleteRefusesLinkedConnectors(t *testing.T) {
	s, orgID, admin := setupTestServerWithOrgAdmin(t)
	wh := createWarehouseViaAPI(t, s, admin, "Delete WH")
	conn := seedWarehouseConnector(t, s, orgID, "Delete Service")

	link := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+conn.String()+"/warehouse", admin,
		map[string]any{"warehouse_id": wh.String()})
	require.Equal(t, http.StatusOK, link.Code, link.Body.String())

	userID := warehouseOrgAdminUserID(t, s, orgID)
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'everyone', 'everyone', 'analytics', 'events')`,
		orgID.String(), wh.String())
	require.NoError(t, err)
	_, err = s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouse_service_preferences (user_id, warehouse_id, connector_id)
		VALUES ($1, $2, $3)`,
		userID.String(), wh.String(), conn.String())
	require.NoError(t, err)

	// Unconfirmed delete is refused while connectors are linked.
	rec := deleteWarehouseViaAPI(t, s, admin, wh)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	var exists bool
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM warehouses WHERE id = $1)`, wh.String()).Scan(&exists))
	require.True(t, exists)
	require.Equal(t, wh.String(), *connectorWarehouse(t, s, conn))

	// force=true unlinks the connectors and cascades grants and preferences.
	forced := warehouseAPIRequest(t, s, http.MethodDelete,
		"/api/v1/warehouses/"+wh.String()+"?force=true", admin, nil)
	require.Equal(t, http.StatusNoContent, forced.Code, forced.Body.String())
	require.Nil(t, connectorWarehouse(t, s, conn))
	require.Zero(t, countWarehouseGrants(t, s, wh))
	require.Zero(t, countWarehousePreferences(t, s, wh))
}

func TestConnectorDeleteRemovesWarehousePreference(t *testing.T) {
	s, orgID, admin := setupTestServerWithOrgAdmin(t)
	wh := createWarehouseViaAPI(t, s, admin, "Connector Delete WH")
	conn := seedWarehouseConnector(t, s, orgID, "Connector Delete Service")

	link := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+conn.String()+"/warehouse", admin,
		map[string]any{"warehouse_id": wh.String()})
	require.Equal(t, http.StatusOK, link.Code, link.Body.String())

	userID := warehouseOrgAdminUserID(t, s, orgID)
	preferWarehouseService(t, s, userID, wh, conn)
	require.Equal(t, 1, countWarehousePreferences(t, s, wh))

	// The connector list exposes the link so clients can render the managed
	// vs shared-credential mode.
	listRec := warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/connectors", admin, nil)
	require.Equal(t, http.StatusOK, listRec.Code, listRec.Body.String())
	var connectors []map[string]any
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &connectors))
	var linked map[string]any
	for _, c := range connectors {
		if c["id"] == conn.String() {
			linked = c
			break
		}
	}
	require.NotNil(t, linked, "linked connector must appear in the list")
	require.Equal(t, wh.String(), linked["warehouse_id"])

	// Connectors are soft-deleted, so the handler must clear preferences
	// explicitly; the FK cascade never fires.
	del := warehouseAPIRequest(t, s, http.MethodDelete, "/api/v1/connectors/"+conn.String(), admin, nil)
	require.Equal(t, http.StatusNoContent, del.Code, del.Body.String())
	require.Zero(t, countWarehousePreferences(t, s, wh))
}
