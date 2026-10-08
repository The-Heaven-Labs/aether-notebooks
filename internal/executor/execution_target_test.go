package executor

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestServiceAccessDeniedErrorWrapsSentinel(t *testing.T) {
	wh := uuid.New()
	e := &ServiceAccessDeniedError{
		WarehouseID: wh,
		Allowed:     []ServiceChoice{{ConnectorID: uuid.New(), Name: "Service A"}},
	}
	require.ErrorIs(t, e, ErrServiceAccessDenied)
	require.Contains(t, e.Error(), "Service A")
	require.Equal(t, wh, e.WarehouseID)
	require.Len(t, e.Allowed, 1)
}
