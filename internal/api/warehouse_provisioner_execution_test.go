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
