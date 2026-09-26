package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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

	body := `{"allow_provisioner_execution": true}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/warehouses/"+fx.warehouseID.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var wh warehouseJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wh))
	require.True(t, wh.AllowProvisionerExecution)

	var stored bool
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT allow_provisioner_execution FROM warehouses WHERE id = $1`,
		fx.warehouseID.String()).Scan(&stored))
	require.True(t, stored, "the toggle must persist")

	var metaJSON []byte
	require.NoError(t, s.db.Pool.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE action = 'warehouse.update' AND resource_id = $1
		ORDER BY id DESC LIMIT 1`,
		fx.warehouseID.String()).Scan(&metaJSON))
	var meta map[string]any
	require.NoError(t, json.Unmarshal(metaJSON, &meta))
	require.Equal(t, true, meta["allow_provisioner_execution"])

	// An empty body is still rejected.
	emptyReq := httptest.NewRequest(http.MethodPut,
		"/api/v1/warehouses/"+fx.warehouseID.String(), strings.NewReader(`{}`))
	emptyReq.Header.Set("Content-Type", "application/json")
	emptyReq.Header.Set("Authorization", "Bearer "+token)
	emptyRec := httptest.NewRecorder()
	s.ServeHTTP(emptyRec, emptyReq)
	require.Equal(t, http.StatusBadRequest, emptyRec.Code, emptyRec.Body.String())
}
