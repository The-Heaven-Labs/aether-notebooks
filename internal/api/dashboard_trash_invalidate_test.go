package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDashboardTrashPublishesInvalidate pins the trash path's relay fan-out:
// DELETE /dashboards/{id} soft-deletes the row and publishes an invalidation
// notice (reason "trashed") on the dashboard's channel so every relay replica
// disconnects viewers and drops any in-memory copy of the trashed document.
func TestDashboardTrashPublishesInvalidate(t *testing.T) {
	t.Setenv("AETHER_RATE_LIMIT_REGISTER", "500")
	srv := setupTestServer(t)
	token := registerAndGetToken(t, srv,
		fmt.Sprintf("dash-trash-invalidate-%d@example.com", time.Now().UnixNano()), "Dash Trash Invalidate Org")
	dashID := createDashWithSettings(t, srv, token, nil)

	ctx := context.Background()
	// The channel name is part of the wire contract with the relay
	// (relay/src/dashboardDocs.ts); it is pinned literally here so a rename on
	// either side fails this test.
	channel := "aether:dashboard-doc-invalidate:" + dashID

	sub := srv.Cache.Client().Subscribe(ctx, channel)
	t.Cleanup(func() { sub.Close() })

	// Readiness handshake: a publish issued before the subscription is
	// registered with Redis would be silently dropped.
	require.Eventually(t, func() bool {
		counts, err := srv.Cache.Client().PubSubNumSub(ctx, channel).Result()
		if err != nil {
			return false
		}
		return counts[channel] >= 1
	}, 5*time.Second, 10*time.Millisecond, "no subscriber attached to %s", channel)

	code, _ := doRequest(t, srv, token, "DELETE", "/api/v1/dashboards/"+dashID, nil)
	require.Equal(t, http.StatusNoContent, code)

	select {
	case msg := <-sub.Channel():
		require.Equal(t, channel, msg.Channel)
		require.JSONEq(t, `{"reason":"trashed"}`, msg.Payload)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the trash invalidation notice")
	}

	// The row is soft-deleted, not gone: the notice is about the trashed
	// state, and the purge publishes a separate "purged" notice later.
	var deletedAt *time.Time
	require.NoError(t, srv.DB().Pool.QueryRow(ctx,
		`SELECT deleted_at FROM dashboards WHERE id = $1`, dashID).Scan(&deletedAt))
	require.NotNil(t, deletedAt)
}
