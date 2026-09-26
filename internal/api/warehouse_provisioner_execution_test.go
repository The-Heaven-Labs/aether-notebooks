package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The provisioner execution override defaults to off and is part of the
// warehouse read model the admin UI toggles.
func TestWarehouseDefaultsAllowProvisionerExecutionFalse(t *testing.T) {
	s, key := newWarehouseSyncTestServer(t)
	fx := seedWarehouseFixtureRows(t, s, key)

	wh, err := s.loadWarehouseForOrg(context.Background(), fx.orgID.String(), fx.warehouseID)
	require.NoError(t, err)
	require.False(t, wh.AllowProvisionerExecution)
}
