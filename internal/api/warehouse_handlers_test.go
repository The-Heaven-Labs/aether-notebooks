package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/the-heaven-labs/aether/internal/chaccess"
)

// warehouseEnqueueEvent is one sync scheduling request; immediate records an
// EnqueueNow (debounce-skipping) request.
type warehouseEnqueueEvent struct {
	id        uuid.UUID
	immediate bool
}

// warehouseEnqueueRecorder captures sync enqueues so CRUD tests observe
// scheduling without running a reconcile against ClickHouse.
type warehouseEnqueueRecorder struct {
	mu     sync.Mutex
	events []warehouseEnqueueEvent
}

func (r *warehouseEnqueueRecorder) Enqueue(id uuid.UUID) {
	r.record(id, false)
}

func (r *warehouseEnqueueRecorder) EnqueueNow(id uuid.UUID) {
	r.record(id, true)
}

func (r *warehouseEnqueueRecorder) record(id uuid.UUID, immediate bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, warehouseEnqueueEvent{id: id, immediate: immediate})
}

func (r *warehouseEnqueueRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
}

func (r *warehouseEnqueueRecorder) contains(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.id == id {
			return true
		}
	}
	return false
}

func (r *warehouseEnqueueRecorder) containsImmediate(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.id == id && e.immediate {
			return true
		}
	}
	return false
}

// warehouseHandlersServer returns the process-wide test server with the
// production sync worker replaced by a recorder. Reusing the shared server
// avoids paying connect + migrate + server construction per test; every test
// seeds random org/user/connector IDs so state stays isolated.
func warehouseHandlersServer(t *testing.T) (*Server, *warehouseEnqueueRecorder) {
	t.Helper()
	s, _ := sharedWarehouseTestServer(t)
	rec := &warehouseEnqueueRecorder{}
	s.SetWarehouseSyncerForTest(rec)
	return s, rec
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

func seedWarehouseConnectorOfType(t *testing.T, s *Server, orgID uuid.UUID, name, connType string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO connectors (id, org_id, name, type, config_encrypted)
		VALUES ($1, $2, $3, $4, $5)`,
		id.String(), orgID.String(), name, connType, []byte("{}"))
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

// seedWarehouseConnector inserts a ClickHouse connector row for link tests.
// Nothing decrypts or dials the stored config in these tests.
func seedWarehouseConnector(t *testing.T, s *Server, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	return seedWarehouseConnectorOfType(t, s, orgID, name, "clickhouse")
}

func seedPostgresConnector(t *testing.T, s *Server, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	return seedWarehouseConnectorOfType(t, s, orgID, name, "postgres")
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

func connectorDeleted(t *testing.T, s *Server, connectorID uuid.UUID) bool {
	t.Helper()
	var deleted bool
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM connectors WHERE id = $1`, connectorID.String()).Scan(&deleted))
	return deleted
}

func warehouseProvisioner(t *testing.T, s *Server, warehouseID uuid.UUID) *string {
	t.Helper()
	var provisioner *string
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT provisioner_connector_id FROM warehouses WHERE id = $1`, warehouseID.String()).Scan(&provisioner))
	return provisioner
}

func warehouseSyncStatus(t *testing.T, s *Server, warehouseID uuid.UUID) string {
	t.Helper()
	var status string
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT sync_status FROM warehouses WHERE id = $1`, warehouseID.String()).Scan(&status))
	return status
}

func warehouseExists(t *testing.T, s *Server, warehouseID uuid.UUID) bool {
	t.Helper()
	var exists bool
	require.NoError(t, s.db.Pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM warehouses WHERE id = $1)`, warehouseID.String()).Scan(&exists))
	return exists
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

func linkConnectorViaAPI(t *testing.T, s *Server, token string, connectorID uuid.UUID, warehouseID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	var body map[string]any
	if warehouseID != nil {
		body = map[string]any{"warehouse_id": warehouseID.String()}
	} else {
		body = map[string]any{"warehouse_id": nil}
	}
	return warehouseAPIRequest(t, s, http.MethodPut, "/api/v1/connectors/"+connectorID.String()+"/warehouse", token, body)
}

func TestWarehouseCRUD(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)

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

	tooLong := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin,
		map[string]any{"name": strings.Repeat("a", 256)})
	require.Equal(t, http.StatusBadRequest, tooLong.Code)

	update := updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"name": "DWH Prod 2"})
	require.Equal(t, http.StatusOK, update.Code, update.Body.String())
	var updated warehouseJSON
	require.NoError(t, json.Unmarshal(update.Body.Bytes(), &updated))
	require.Equal(t, "DWH Prod 2", updated.Name)

	// Null semantics: absent field = no change, empty body = 400, null name = 400.
	require.Equal(t, http.StatusBadRequest, updateWarehouseViaAPI(t, s, admin, wh, map[string]any{}).Code)
	require.Equal(t, http.StatusBadRequest, updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"name": nil}).Code)
	require.Equal(t, http.StatusBadRequest, updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"unrelated": "x"}).Code)

	get := warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses/"+wh.String(), admin, nil)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	var got warehouseJSON
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &got))
	require.Equal(t, "DWH Prod 2", got.Name)

	// Malformed IDs are not found, never a Postgres cast error.
	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/warehouses/not-a-uuid", nil},
		{http.MethodPut, "/api/v1/warehouses/not-a-uuid", map[string]any{"name": "x"}},
		{http.MethodDelete, "/api/v1/warehouses/not-a-uuid", nil},
		{http.MethodPut, "/api/v1/warehouses/not-a-uuid/provisioner", map[string]any{"connector_id": nil}},
	} {
		rec := warehouseAPIRequest(t, s, tc.method, tc.path, admin, tc.body)
		require.Equal(t, http.StatusNotFound, rec.Code, "%s %s: %s", tc.method, tc.path, rec.Body.String())
	}

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
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
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
	s, _ := warehouseHandlersServer(t)
	_, _, adminA := seedWarehouseOrgAdmin(t, s)
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

// TestWarehouseDeleteCrossOrgWithLinkedConnectors is the regression for a
// cross-org leak: deleting another org's warehouse must 404 before any linked
// connector count is computed, and must not leak that count or 409.
func TestWarehouseDeleteCrossOrgWithLinkedConnectors(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, adminA := seedWarehouseOrgAdmin(t, s)
	orgB, _, adminB := seedWarehouseOrgAdmin(t, s)

	connB := seedWarehouseConnector(t, s, orgB, "Cross Org Regression Service")
	whB := createWarehouseViaAPI(t, s, adminB, "Cross Org Regression WH")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, adminB, connB, &whB).Code)

	rec := deleteWarehouseViaAPI(t, s, adminA, whB)
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "connector_count")

	require.True(t, warehouseExists(t, s, whB), "cross-org delete must not touch the warehouse")
	require.Equal(t, whB.String(), *connectorWarehouse(t, s, connB))
}

func TestWarehouseProvisionerLifecycle(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Provisioner WH")
	linked := seedWarehouseConnector(t, s, orgID, "Linked Service")

	// A connector that is not part of the warehouse cannot provision it.
	rec400 := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": linked.String()})
	require.Equal(t, http.StatusBadRequest, rec400.Code, rec400.Body.String())

	// Missing key and empty string are rejected; absent is not "clear".
	require.Equal(t, http.StatusBadRequest, warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin, map[string]any{}).Code)
	require.Equal(t, http.StatusBadRequest, warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": ""}).Code)

	// Link first, then designate.
	link := linkConnectorViaAPI(t, s, admin, linked, &wh)
	require.Equal(t, http.StatusOK, link.Code, link.Body.String())
	require.True(t, rec.contains(wh), "linking a connector must enqueue the warehouse")

	rec.reset()
	set := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": strings.ToUpper(linked.String())})
	require.Equal(t, http.StatusOK, set.Code, set.Body.String())
	var updated warehouseJSON
	require.NoError(t, json.Unmarshal(set.Body.Bytes(), &updated))
	require.NotNil(t, updated.ProvisionerConnectorID)
	require.Equal(t, linked.String(), *updated.ProvisionerConnectorID, "canonical lowercase UUID is stored")
	require.Equal(t, "pending", updated.SyncStatus)
	require.True(t, rec.contains(wh), "setting a provisioner must enqueue a sync")

	// A rejected update must not reach the sync path.
	rec.reset()
	require.Equal(t, http.StatusBadRequest,
		updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"unrelated": "x"}).Code)
	require.False(t, rec.contains(wh), "a rejected update must not enqueue")

	// The warehouse update endpoint accepts the same field.
	require.Equal(t, http.StatusOK,
		updateWarehouseViaAPI(t, s, admin, wh, map[string]any{"provisioner_connector_id": linked.String()}).Code)

	// Clearing the provisioner is allowed.
	clear := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": nil})
	require.Equal(t, http.StatusOK, clear.Code, clear.Body.String())
	var cleared warehouseJSON
	require.NoError(t, json.Unmarshal(clear.Body.Bytes(), &cleared))
	require.Nil(t, cleared.ProvisionerConnectorID)
	require.Nil(t, warehouseProvisioner(t, s, wh))
}

func TestWarehouseProvisionerValidation(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Provisioner Validation WH")

	postgres := seedPostgresConnector(t, s, orgID, "Postgres Service")
	linkedClickHouse := seedWarehouseConnector(t, s, orgID, "Linked CH")

	// Linking a non-ClickHouse connector is rejected.
	linkPG := linkConnectorViaAPI(t, s, admin, postgres, &wh)
	require.Equal(t, http.StatusBadRequest, linkPG.Code, linkPG.Body.String())
	require.Nil(t, connectorWarehouse(t, s, postgres))

	// A non-ClickHouse connector can never be a provisioner, linked or not.
	rec := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": postgres.String()})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "clickhouse")

	// A connector from another org is rejected.
	orgB, _, _ := seedWarehouseOrgAdmin(t, s)
	otherOrgConnector := seedWarehouseConnector(t, s, orgB, "Other Org Service")
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": otherOrgConnector.String()})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// GET only lists ClickHouse services; a legacy cross-linked Postgres row
	// (bypassing the write path) is filtered out.
	linkCH := linkConnectorViaAPI(t, s, admin, linkedClickHouse, &wh)
	require.Equal(t, http.StatusOK, linkCH.Code, linkCH.Body.String())
	_, err := s.db.Pool.Exec(context.Background(),
		`UPDATE connectors SET warehouse_id = $1 WHERE id = $2`, wh.String(), postgres.String())
	require.NoError(t, err)
	get := warehouseAPIRequest(t, s, http.MethodGet, "/api/v1/warehouses/"+wh.String(), admin, nil)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	var got warehouseJSON
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &got))
	require.Len(t, got.Connectors, 1, "only the ClickHouse service may be listed")
	require.Equal(t, "clickhouse", got.Connectors[0].Type)
	require.Equal(t, linkedClickHouse.String(), got.Connectors[0].ID)

	// The linked ClickHouse connector can be designated.
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": linkedClickHouse.String()})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestWarehouseCreateAdoptsProvisioner(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	conn := seedWarehouseConnector(t, s, orgID, "Create Adopt Service")

	// Uppercase UUIDs are accepted and echoed canonically.
	rec201 := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "Adopt WH",
		"provisioner_connector_id": strings.ToUpper(conn.String()),
	})
	require.Equal(t, http.StatusCreated, rec201.Code, rec201.Body.String())
	var wh warehouseJSON
	require.NoError(t, json.Unmarshal(rec201.Body.Bytes(), &wh))
	require.NotNil(t, wh.ProvisionerConnectorID)
	require.Equal(t, conn.String(), *wh.ProvisionerConnectorID)
	require.Equal(t, wh.ID, *connectorWarehouse(t, s, conn))
	warehouseUUID, err := uuid.Parse(wh.ID)
	require.NoError(t, err)
	require.True(t, rec.contains(warehouseUUID))

	// A connector already linked to another warehouse cannot be adopted, and
	// the failed create rolls back (no orphan warehouse row).
	recBad := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "Second WH",
		"provisioner_connector_id": conn.String(),
	})
	require.Equal(t, http.StatusBadRequest, recBad.Code, recBad.Body.String())
	require.Len(t, listWarehousesViaAPI(t, s, admin), 1, "a failed adoption must not leave a warehouse behind")

	// A non-ClickHouse connector cannot be adopted.
	postgres := seedPostgresConnector(t, s, orgID, "Adopt Postgres")
	recPG := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "Third WH",
		"provisioner_connector_id": postgres.String(),
	})
	require.Equal(t, http.StatusBadRequest, recPG.Code, recPG.Body.String())
	require.Len(t, listWarehousesViaAPI(t, s, admin), 1)

	// A connector from another org cannot be adopted.
	orgB, _, _ := seedWarehouseOrgAdmin(t, s)
	otherOrgConnector := seedWarehouseConnector(t, s, orgB, "Other Org Adopt Service")
	recCross := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "Fourth WH",
		"provisioner_connector_id": otherOrgConnector.String(),
	})
	require.Equal(t, http.StatusBadRequest, recCross.Code, recCross.Body.String())

	// Explicit null is the same as absent: no provisioner, no adoption.
	recNull := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "No Provisioner WH",
		"provisioner_connector_id": nil,
	})
	require.Equal(t, http.StatusCreated, recNull.Code, recNull.Body.String())
	var bare warehouseJSON
	require.NoError(t, json.Unmarshal(recNull.Body.Bytes(), &bare))
	require.Nil(t, bare.ProvisionerConnectorID)
}

func TestConnectorWarehouseLinkValidation(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgA, _, adminA := seedWarehouseOrgAdmin(t, s)
	orgB, _, adminB := seedWarehouseOrgAdmin(t, s)

	connA := seedWarehouseConnector(t, s, orgA, "Cross Org Connector A")
	connB := seedWarehouseConnector(t, s, orgB, "Cross Org Connector B")
	whA := createWarehouseViaAPI(t, s, adminA, "Cross Org WH A")
	whB := createWarehouseViaAPI(t, s, adminB, "Cross Org WH B")

	// A warehouse from another org is rejected and the link stays unset.
	rec := linkConnectorViaAPI(t, s, adminA, connA, &whB)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Nil(t, connectorWarehouse(t, s, connA))

	// A connector from another org is invisible to this org's admin.
	rec = linkConnectorViaAPI(t, s, adminA, connB, &whA)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Absent field = no change: an empty body is a 200 that leaves the link
	// unset, while unknown/malformed values are rejected.
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+connA.String()+"/warehouse", adminA, map[string]any{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Nil(t, connectorWarehouse(t, s, connA))
	rec = linkConnectorViaAPI(t, s, adminA, connA, ptrUUID(uuid.New()))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+connA.String()+"/warehouse", adminA, map[string]any{"warehouse_id": "nope"})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	// Link and unlink; an empty body keeps an existing link.
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, adminA, connA, &whA).Code)
	require.Equal(t, whA.String(), *connectorWarehouse(t, s, connA))
	rec = warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/connectors/"+connA.String()+"/warehouse", adminA, map[string]any{})
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, whA.String(), *connectorWarehouse(t, s, connA))
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, adminA, connA, nil).Code)
	require.Nil(t, connectorWarehouse(t, s, connA))
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }

func TestProvisionerMoveOrphansOldWarehouse(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	orgID, userID, admin := seedWarehouseOrgAdmin(t, s)

	whA := createWarehouseViaAPI(t, s, admin, "Orphan Move A")
	connP := seedWarehouseConnector(t, s, orgID, "Move Provisioner")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connP, &whA).Code)
	setProvisioner := warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+whA.String()+"/provisioner", admin,
		map[string]any{"connector_id": connP.String()})
	require.Equal(t, http.StatusOK, setProvisioner.Code, setProvisioner.Body.String())

	// A preference tied to the old warehouse becomes unsatisfiable on move.
	preferWarehouseService(t, s, userID, whA, connP)
	require.Equal(t, 1, countWarehousePreferences(t, s, whA))

	whB := createWarehouseViaAPI(t, s, admin, "Orphan Move B")
	rec.reset()

	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connP, &whB).Code)

	require.Nil(t, warehouseProvisioner(t, s, whA), "moving a provisioner must orphan the old warehouse")
	require.Equal(t, "pending", warehouseSyncStatus(t, s, whA))
	require.True(t, rec.containsImmediate(whA), "the orphaned warehouse must be enqueued immediately")
	require.Zero(t, countWarehousePreferences(t, s, whA), "stale preferences must not survive the move")
	require.Equal(t, whB.String(), *connectorWarehouse(t, s, connP))
	require.Nil(t, warehouseProvisioner(t, s, whB), "the new warehouse has no provisioner yet")
}

func TestProvisionerSoftDeleteOrphansWarehouse(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	orgID, userID, admin := seedWarehouseOrgAdmin(t, s)

	wh := createWarehouseViaAPI(t, s, admin, "Orphan Delete WH")
	connP := seedWarehouseConnector(t, s, orgID, "Delete Provisioner")
	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connP, &wh).Code)
	require.Equal(t, http.StatusOK, warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": connP.String()}).Code)
	preferWarehouseService(t, s, userID, wh, connP)
	rec.reset()

	del := warehouseAPIRequest(t, s, http.MethodDelete, "/api/v1/connectors/"+connP.String(), admin, nil)
	require.Equal(t, http.StatusNoContent, del.Code, del.Body.String())

	require.True(t, connectorDeleted(t, s, connP))
	require.Nil(t, warehouseProvisioner(t, s, wh), "soft-deleting a provisioner must orphan the warehouse")
	require.Equal(t, "pending", warehouseSyncStatus(t, s, wh))
	require.True(t, rec.containsImmediate(wh), "the orphaned warehouse must be enqueued immediately")
	require.Zero(t, countWarehousePreferences(t, s, wh))
}

func TestWarehouseDeleteRefusesLinkedConnectors(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Delete WH")
	conn := seedWarehouseConnector(t, s, orgID, "Delete Service")

	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)

	userID := warehouseOrgAdminUserID(t, s, orgID)
	_, err := s.db.Pool.Exec(context.Background(), `
		INSERT INTO warehouse_table_grants
			(org_id, warehouse_id, subject_type, subject_id, database_name, table_name)
		VALUES ($1, $2, 'everyone', 'everyone', 'analytics', 'events')`,
		orgID.String(), wh.String())
	require.NoError(t, err)
	preferWarehouseService(t, s, userID, wh, conn)

	// Unconfirmed delete is refused while connectors are linked.
	rec := deleteWarehouseViaAPI(t, s, admin, wh)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "force=true")
	require.Contains(t, rec.Body.String(), "identities are revoked")
	require.True(t, warehouseExists(t, s, wh))
	require.Equal(t, wh.String(), *connectorWarehouse(t, s, conn))

	// force=true unlinks the connectors and cascades grants and preferences.
	forced := warehouseAPIRequest(t, s, http.MethodDelete,
		"/api/v1/warehouses/"+wh.String()+"?force=true", admin, nil)
	require.Equal(t, http.StatusNoContent, forced.Code, forced.Body.String())
	require.False(t, warehouseExists(t, s, wh))
	require.Nil(t, connectorWarehouse(t, s, conn))
	require.Zero(t, countWarehouseGrants(t, s, wh))
	require.Zero(t, countWarehousePreferences(t, s, wh))
}

func TestConnectorDeleteRemovesWarehousePreference(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Connector Delete WH")
	conn := seedWarehouseConnector(t, s, orgID, "Connector Delete Service")

	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, conn, &wh).Code)
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

func TestWarehouseAuditMetadata(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	conn := seedWarehouseConnector(t, s, orgID, "Audit Provisioner")

	createRec := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/warehouses", admin, map[string]any{
		"name":                     "Audit WH",
		"provisioner_connector_id": conn.String(),
	})
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var wh warehouseJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &wh))

	createMeta := warehouseAuditMetadata(t, s, "warehouse.create", wh.ID)
	require.Equal(t, "Audit WH", createMeta["name"])
	require.Equal(t, conn.String(), createMeta["provisioner_connector_id"])

	require.Equal(t, http.StatusOK, updateWarehouseViaAPI(t, s, admin, uuid.MustParse(wh.ID),
		map[string]any{"name": "Audit WH 2"}).Code)
	updateMeta := warehouseAuditMetadata(t, s, "warehouse.update", wh.ID)
	require.Equal(t, "Audit WH 2", updateMeta["name"])
	require.Equal(t, "Audit WH", updateMeta["previous_name"])

	// Clear the provisioner first so the delete does not attempt identity
	// cleanup against the stub connector config.
	require.Equal(t, http.StatusOK, warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.ID+"/provisioner", admin,
		map[string]any{"connector_id": nil}).Code)
	require.Equal(t, http.StatusNoContent,
		warehouseAPIRequest(t, s, http.MethodDelete,
			"/api/v1/warehouses/"+wh.ID+"?force=true", admin, nil).Code)
	deleteMeta := warehouseAuditMetadata(t, s, "warehouse.delete", wh.ID)
	require.Equal(t, true, deleteMeta["force"])
	require.EqualValues(t, 1, deleteMeta["connector_count"])
}

func warehouseAuditMetadata(t *testing.T, s *Server, action, resourceID string) map[string]any {
	t.Helper()
	var raw []byte
	require.NoError(t, s.db.Pool.QueryRow(context.Background(), `
		SELECT metadata FROM audit_logs
		WHERE action = $1 AND resource_id = $2
		ORDER BY id DESC LIMIT 1`, action, resourceID).Scan(&raw))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	return meta
}

// TestWarehouseDeleteRevokesIdentities drives a real ClickHouse provision and
// then deletes the warehouse: the managed users/roles must be dropped before
// the Aether row disappears.
func TestWarehouseDeleteRevokesIdentities(t *testing.T) {
	ctx := context.Background()
	s, key := sharedWarehouseTestServer(t)
	s.SetWarehouseSyncerForTest(&warehouseEnqueueRecorder{})
	fx := setupWarehouseFixtureWithServer(t, s, key)
	token, err := s.jwt.Issue(fx.userID.String(), fx.orgID.String(), "admin")
	require.NoError(t, err)

	// Provision the warehouse's namespace so the delete has identities to
	// revoke, not an empty no-op.
	require.NoError(t, s.reconcileWarehouse(ctx, fx.warehouseID))
	userIdent := chaccess.UserIdent(fx.warehouseID, fx.orgID, fx.userID)
	roleIdent := chaccess.RoleIdent(fx.warehouseID, fx.orgID, fx.groupID)
	requireClickHouseUserExists(t, fx.conn, userIdent)
	requireClickHouseRoleExists(t, fx.conn, roleIdent)

	rec := warehouseAPIRequest(t, s, http.MethodDelete,
		"/api/v1/warehouses/"+fx.warehouseID.String()+"?force=true", token, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	require.False(t, warehouseExists(t, s, fx.warehouseID))
	requireNoPrefixedEntities(t, fx.conn, chaccess.IdentifierPrefix(fx.warehouseID))
}

// TestWarehouseDeleteCleanupFailureKeepsWarehouse verifies the fail-closed
// path: when the provisioner is unreachable, the delete is a 503 and the
// warehouse row is untouched.
func TestWarehouseDeleteCleanupFailureKeepsWarehouse(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Cleanup Failure WH")

	createConn := warehouseAPIRequest(t, s, http.MethodPost, "/api/v1/connectors", admin, map[string]any{
		"name": "Unreachable Provisioner",
		"type": "clickhouse",
		"config": map[string]any{
			"host": "127.0.0.1", "port": 1, "user": "x", "password": "y",
		},
	})
	require.Equal(t, http.StatusCreated, createConn.Code, createConn.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(createConn.Body.Bytes(), &created))
	connID := uuid.MustParse(created["id"].(string))
	t.Cleanup(func() {
		_, _ = s.db.Pool.Exec(context.Background(), `DELETE FROM connectors WHERE id = $1`, connID.String())
	})

	require.Equal(t, http.StatusOK, linkConnectorViaAPI(t, s, admin, connID, &wh).Code)
	require.Equal(t, http.StatusOK, warehouseAPIRequest(t, s, http.MethodPut,
		"/api/v1/warehouses/"+wh.String()+"/provisioner", admin,
		map[string]any{"connector_id": connID.String()}).Code)

	rec := warehouseAPIRequest(t, s, http.MethodDelete,
		"/api/v1/warehouses/"+wh.String()+"?force=true", admin, nil)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "identity cleanup failed")
	require.True(t, warehouseExists(t, s, wh), "a failed identity cleanup must not delete the warehouse")
	require.Equal(t, connID.String(), *warehouseProvisioner(t, s, wh))
}
