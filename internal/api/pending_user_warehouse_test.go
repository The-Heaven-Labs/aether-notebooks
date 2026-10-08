package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPendingWarehouseGrantCreate(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Grant WH")
	rec.reset()

	email := "Future.User@Example.com"
	createRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", email, "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	require.Equal(t, "pending_user", created.SubjectType)
	require.Equal(t, "future.user@example.com", created.SubjectID)
	require.Equal(t, "future.user@example.com", created.SubjectEmail)
	require.Empty(t, created.Warning, "pending subjects are never warned about service access")

	ctx := context.Background()
	var realCount, pendingCount int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&realCount))
	require.Zero(t, realCount, "staging must not write a real grant")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&pendingCount))
	require.Equal(t, 1, pendingCount)
	require.False(t, rec.contains(wh), "staging must not enqueue a warehouse sync")

	// Idempotent replay returns the same staged row with 200.
	replayRec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", "future.user@example.COM", "analytics", "events"))
	require.Equal(t, http.StatusOK, replayRec.Code, replayRec.Body.String())
	var replay warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(replayRec.Body.Bytes(), &replay))
	require.Equal(t, created.ID, replay.ID)

	// Audit carries the pending marker.
	meta := warehouseGrantAuditMeta(t, s, "warehouse.grant.create", created.ID)
	require.Equal(t, true, meta["pending"])
}

func TestPendingWarehouseGrantRejectsInvalidEmail(t *testing.T) {
	s, _ := warehouseHandlersServer(t)
	_, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Invalid WH")

	for _, email := range []string{"", "future@", "@example.com", "two@@example.com"} {
		rec := createGrantViaAPI(t, s, admin, wh, grantBody("pending_user", email, "analytics", "events"))
		require.Equal(t, http.StatusBadRequest, rec.Code, "email %q must be rejected", email)
	}
}

// TestPendingWarehouseGrantConvertsExistingMember pins the member-conversion
// amendment: a pending_user request whose email already belongs to an org
// member becomes a real user grant, never staged (a staged row for a member
// could never materialize), mirroring handleAddPendingGroupMembers and the
// ACL pending path.
func TestPendingWarehouseGrantConvertsExistingMember(t *testing.T) {
	s, rec := warehouseHandlersServer(t)
	orgID, _, admin := seedWarehouseOrgAdmin(t, s)
	wh := createWarehouseViaAPI(t, s, admin, "Pending Convert WH")
	memberID, _ := seedGrantOrgMember(t, s, orgID, "non-admin")

	ctx := context.Background()
	var memberEmail string
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, memberID.String()).Scan(&memberEmail))
	rec.reset()

	// Uppercase input proves the membership lookup is case-insensitive.
	createRec := createGrantViaAPI(t, s, admin, wh,
		grantBody("pending_user", strings.ToUpper(memberEmail), "analytics", "events"))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())
	var created warehouseGrantCreateJSON
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &created))
	require.Equal(t, "user", created.SubjectType)
	require.Equal(t, memberID.String(), created.SubjectID)
	require.Equal(t, memberEmail, created.SubjectEmail)
	// The converted member is a real subject: the ordinary service-access
	// warning still applies, exactly as for a direct user grant.
	require.Equal(t, "no_service_access", created.Warning)

	var staged, real int
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_warehouse_table_grants WHERE warehouse_id = $1`, wh.String()).Scan(&staged))
	require.Zero(t, staged, "an existing member's email must not be staged")
	require.NoError(t, s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM warehouse_table_grants
		 WHERE warehouse_id = $1 AND subject_type = 'user' AND subject_id = $2`,
		wh.String(), memberID.String()).Scan(&real))
	require.Equal(t, 1, real)
	require.True(t, rec.contains(wh), "a converted member is a real grant and must enqueue a sync")

	meta := warehouseGrantAuditMeta(t, s, "warehouse.grant.create", created.ID)
	require.Equal(t, "user", meta["subject_type"])
	require.NotEqual(t, true, meta["pending"])
}
