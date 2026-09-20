package api

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Warehouse sync triggers. These helpers schedule ClickHouse access
// reconciliation after a membership mutation has committed. They are
// best-effort: a lookup failure is logged and swallowed, because managing
// memberships must never fail due to sync bookkeeping. Enqueues coalesce per
// warehouse and a reconcile with no changes emits no DDL, so over-enqueuing is
// cheap.

// enqueueWarehouseSyncForOrg enqueues every warehouse in the org. It is the
// coarse fallback for mutations after which the affected warehouses cannot be
// resolved precisely — org membership removal and user deletion destroy the
// user rows that identify their direct, group, and everyone relationships.
func (s *Server) enqueueWarehouseSyncForOrg(ctx context.Context, orgID string) {
	if s.warehouseSync == nil {
		return
	}
	oid, err := uuid.Parse(orgID)
	if err != nil {
		slog.Warn("warehouse sync enqueue skipped", "scope", "org", "org_id", orgID, "error", err)
		return
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id FROM warehouses WHERE org_id = $1`, oid.String())
	if err != nil {
		slog.Warn("warehouse sync enqueue lookup failed", "scope", "org", "org_id", orgID, "error", err)
		return
	}
	defer rows.Close()
	s.enqueueWarehouseRows(rows, "org", orgID)
}

// enqueueWarehouseSyncForGroup enqueues the warehouses whose desired state
// depends on the group's membership: precisely those holding a grant for the
// group. Membership only affects provisioning through a group grant, so
// warehouses without one cannot change. Orphaned grants for a deleted group
// still resolve, which is exactly what makes role removal possible.
func (s *Server) enqueueWarehouseSyncForGroup(ctx context.Context, groupID string) {
	if s.warehouseSync == nil {
		return
	}
	gid, err := uuid.Parse(groupID)
	if err != nil {
		slog.Warn("warehouse sync enqueue skipped", "scope", "group", "group_id", groupID, "error", err)
		return
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT warehouse_id
		FROM warehouse_table_grants
		WHERE subject_type = 'group' AND subject_id = $1`, gid.String())
	if err != nil {
		slog.Warn("warehouse sync enqueue lookup failed", "scope", "group", "group_id", groupID, "error", err)
		return
	}
	defer rows.Close()
	s.enqueueWarehouseRows(rows, "group", groupID)
}

// enqueueWarehouseSyncForUser enqueues the warehouses where the user's
// effective access can change: warehouses with a direct grant for the user,
// warehouses whose granting groups include the user, and warehouses with an
// everyone grant in an org the user belongs to. It must run after the
// membership mutation commits, so the user's org and group rows are visible.
// The everyone clause is what provisions a newly joined member; without it,
// joining an org with an everyone-granted warehouse would go unsynced.
func (s *Server) enqueueWarehouseSyncForUser(ctx context.Context, userID string) {
	if s.warehouseSync == nil {
		return
	}
	uid, err := uuid.Parse(userID)
	if err != nil {
		slog.Warn("warehouse sync enqueue skipped", "scope", "user", "user_id", userID, "error", err)
		return
	}
	// $1 stays text (grants.subject_id is text); $2/$3 are uuid parameters so
	// one parameter type is never inferred from both a text and a uuid column.
	rows, err := s.db.Pool.Query(ctx, `
		SELECT DISTINCT wtg.warehouse_id
		FROM warehouse_table_grants wtg
		WHERE (wtg.subject_type = 'user' AND wtg.subject_id = $1)
		   OR (wtg.subject_type = 'group' AND wtg.subject_id IN (
		         SELECT gm.group_id::text FROM group_members gm WHERE gm.user_id = $2
		      ))
		   OR (wtg.subject_type = 'everyone' AND wtg.org_id IN (
		         SELECT om.org_id FROM org_members om WHERE om.user_id = $3
		      ))`, uid.String(), uid.String(), uid.String())
	if err != nil {
		slog.Warn("warehouse sync enqueue lookup failed", "scope", "user", "user_id", userID, "error", err)
		return
	}
	defer rows.Close()
	s.enqueueWarehouseRows(rows, "user", userID)
}

// enqueueWarehouseRows drains a warehouse_id result set into the sync worker,
// logging (not returning) scan and iteration failures.
func (s *Server) enqueueWarehouseRows(rows pgx.Rows, scope, id string) {
	for rows.Next() {
		var warehouseID uuid.UUID
		if err := rows.Scan(&warehouseID); err != nil {
			slog.Warn("warehouse sync enqueue scan failed", "scope", scope, "id", id, "error", err)
			return
		}
		s.warehouseSync.Enqueue(warehouseID)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("warehouse sync enqueue read failed", "scope", scope, "id", id, "error", err)
	}
}
