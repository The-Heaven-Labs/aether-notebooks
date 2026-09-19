package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/models"
)

// reconcileWarehouse computes the desired ClickHouse access state for a
// warehouse and applies it through the provisioner connector. It is
// idempotent: a run with no Aether-side changes emits no DDL, and
// warehouses.applied_master_fp drives password re-keying after a master-key
// rotation.
//
// The sync worker wraps calls in a panic recovery, so a chaccess invariant
// violation cannot take down the API server.
func (s *Server) reconcileWarehouse(ctx context.Context, warehouseID uuid.UUID) error {
	var (
		orgID         uuid.UUID
		provisionerID *uuid.UUID
		appliedFP     *string
	)
	err := s.db.Pool.QueryRow(ctx, `
		SELECT org_id, provisioner_connector_id, applied_master_fp
		FROM warehouses WHERE id = $1`, warehouseID.String(),
	).Scan(&orgID, &provisionerID, &appliedFP)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("reconcile warehouse %s: not found", warehouseID)
	}
	if err != nil {
		return fmt.Errorf("reconcile warehouse %s: load warehouse: %w", warehouseID, err)
	}
	if provisionerID == nil {
		return s.failWarehouseSync(ctx, warehouseID,
			fmt.Errorf("warehouse %s has no provisioner connector", warehouseID))
	}

	// The provisioner must belong to this warehouse: a connector linked to a
	// different warehouse (or to no warehouse at all) would adopt another
	// warehouse's credential namespace.
	// NOTE: the "provisioner must be read-write" requirement is not
	// enforceable at load time — connectors carry no service-type/RW metadata
	// yet (deferred with that work). A read-only credential fails at DDL
	// execution time instead.
	var encrypted []byte
	var connectorWarehouseID *uuid.UUID
	err = s.db.Pool.QueryRow(ctx, `
		SELECT config_encrypted, warehouse_id FROM connectors
		WHERE id = $1 AND org_id = $2 AND type = 'clickhouse' AND deleted_at IS NULL`,
		provisionerID.String(), orgID.String(),
	).Scan(&encrypted, &connectorWarehouseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.failWarehouseSync(ctx, warehouseID, fmt.Errorf(
			"provisioner connector %s is missing, soft-deleted, or not a clickhouse connector", provisionerID))
	}
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID,
			fmt.Errorf("load provisioner connector: %w", err))
	}
	if connectorWarehouseID == nil || *connectorWarehouseID != warehouseID {
		return s.failWarehouseSync(ctx, warehouseID, fmt.Errorf(
			"provisioner connector %s does not belong to warehouse %s", provisionerID, warehouseID))
	}

	plain, err := crypto.Decrypt(encrypted, s.masterKey)
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID,
			fmt.Errorf("decrypt provisioner config: %w", err))
	}
	var cfg models.ConnectorConfig
	if err := json.Unmarshal(plain, &cfg); err != nil {
		return s.failWarehouseSync(ctx, warehouseID,
			fmt.Errorf("parse provisioner config: %w", err))
	}

	if err := s.setWarehouseSyncStatus(ctx, warehouseID, "syncing", "", ""); err != nil {
		return fmt.Errorf("reconcile warehouse %s: %w", warehouseID, err)
	}

	// Provisioning uses its own short-lived connection: the executor's
	// per-query path is for user SQL, not DDL.
	conn, err := openWarehouseProvisionerConn(ctx, cfg)
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID, fmt.Errorf("connect provisioner: %w", err))
	}
	defer conn.Close()

	desired, err := s.loadWarehouseDesiredState(ctx, warehouseID, orgID)
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID, err)
	}

	actual, err := chaccess.LoadActual(ctx, conn, warehouseID)
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID,
			fmt.Errorf("load clickhouse actual state: %w", err))
	}

	fingerprint := chaccess.Fingerprint(string(s.masterKey))
	actual.ForcePasswordReset = appliedFP == nil || *appliedFP != fingerprint

	// Fail closed: wildcards defeat table-level least privilege and foreign
	// role wiring / grant options are outside Aether's model. Never mark
	// ready while they exist; remediation is manual in this iteration.
	if actual.HasWildcard() || len(actual.Unexpected) > 0 {
		syncErr := fmt.Errorf("warehouse %s clickhouse drift requires manual remediation: %s",
			warehouseID, describeWarehouseDrift(actual))
		if err := s.setWarehouseSyncStatus(ctx, warehouseID, "error", syncErr.Error(), ""); err != nil {
			return errors.Join(syncErr, err)
		}
		s.auditWarehouseDrift(ctx, orgID, warehouseID, nil, actual)
		return syncErr
	}

	stmts, skipped := chaccess.Statements(desired, actual)
	if len(skipped) > 0 {
		s.auditWarehouseDrift(ctx, orgID, warehouseID, skipped, chaccess.ActualState{})
	}
	for _, stmt := range stmts {
		if err := conn.Exec(ctx, stmt); err != nil {
			return s.failWarehouseSync(ctx, warehouseID, fmt.Errorf("execute %q: %w", stmt, err))
		}
	}

	if err := s.setWarehouseSyncStatus(ctx, warehouseID, "ready", "", fingerprint); err != nil {
		return fmt.Errorf("reconcile warehouse %s: %w", warehouseID, err)
	}

	if err := s.audit.Log(ctx, audit.Entry{
		OrgID:        orgID.String(),
		Action:       "warehouse.sync",
		ResourceType: "warehouse",
		ResourceID:   warehouseID.String(),
		Metadata: map[string]any{
			"warehouse_id": warehouseID.String(),
			"statements":   len(stmts),
			"users":        len(desired.Users),
			"roles":        len(desired.Roles),
		},
	}); err != nil {
		slog.Warn("warehouse sync audit failed", "warehouse_id", warehouseID, "error", err)
	}
	return nil
}

// loadWarehouseDesiredState gathers org-scoped inputs for chaccess.Compute.
// Grants are re-validated against the org's members and groups so a subject
// from another org can never be provisioned (defense in depth against a
// write-path gap); malformed subject IDs grant nothing.
func (s *Server) loadWarehouseDesiredState(ctx context.Context, warehouseID, orgID uuid.UUID) (chaccess.DesiredState, error) {
	memberIDs := map[uuid.UUID]struct{}{}
	var users []chaccess.UserSpec
	memberRows, err := s.db.Pool.Query(ctx, `
		SELECT u.id FROM org_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1`, orgID.String())
	if err != nil {
		return chaccess.DesiredState{}, fmt.Errorf("load org members: %w", err)
	}
	for memberRows.Next() {
		var id uuid.UUID
		if err := memberRows.Scan(&id); err != nil {
			memberRows.Close()
			return chaccess.DesiredState{}, fmt.Errorf("scan org member: %w", err)
		}
		memberIDs[id] = struct{}{}
		users = append(users, chaccess.UserSpec{ID: id})
	}
	memberRows.Close()
	if err := memberRows.Err(); err != nil {
		return chaccess.DesiredState{}, fmt.Errorf("read org members: %w", err)
	}

	orgGroups := map[uuid.UUID]struct{}{}
	groupRows, err := s.db.Pool.Query(ctx, `SELECT id FROM groups WHERE org_id = $1`, orgID.String())
	if err != nil {
		return chaccess.DesiredState{}, fmt.Errorf("load org groups: %w", err)
	}
	for groupRows.Next() {
		var id uuid.UUID
		if err := groupRows.Scan(&id); err != nil {
			groupRows.Close()
			return chaccess.DesiredState{}, fmt.Errorf("scan org group: %w", err)
		}
		orgGroups[id] = struct{}{}
	}
	groupRows.Close()
	if err := groupRows.Err(); err != nil {
		return chaccess.DesiredState{}, fmt.Errorf("read org groups: %w", err)
	}

	grantRows, err := s.db.Pool.Query(ctx, `
		SELECT subject_type, subject_id, database_name, table_name
		FROM warehouse_table_grants
		WHERE warehouse_id = $1 AND org_id = $2`,
		warehouseID.String(), orgID.String())
	if err != nil {
		return chaccess.DesiredState{}, fmt.Errorf("load warehouse grants: %w", err)
	}
	var grants []chaccess.SubjectGrant
	for grantRows.Next() {
		var g chaccess.SubjectGrant
		if err := grantRows.Scan(&g.SubjectType, &g.SubjectID, &g.Database, &g.Table); err != nil {
			grantRows.Close()
			return chaccess.DesiredState{}, fmt.Errorf("scan warehouse grant: %w", err)
		}
		switch g.SubjectType {
		case "user":
			id, err := uuid.Parse(g.SubjectID)
			if err != nil {
				continue
			}
			if _, ok := memberIDs[id]; !ok {
				continue
			}
		case "group":
			id, err := uuid.Parse(g.SubjectID)
			if err != nil {
				continue
			}
			if _, ok := orgGroups[id]; !ok {
				continue
			}
		case "everyone":
			// Applies to any org member; nothing to validate.
		default:
			continue
		}
		grants = append(grants, g)
	}
	grantRows.Close()
	if err := grantRows.Err(); err != nil {
		return chaccess.DesiredState{}, fmt.Errorf("read warehouse grants: %w", err)
	}

	// Memberships are restricted to org members and to groups that hold a
	// grant in this warehouse; anything else cannot affect desired state.
	memberships := map[uuid.UUID][]uuid.UUID{}
	membershipRows, err := s.db.Pool.Query(ctx, `
		SELECT gm.user_id, gm.group_id
		FROM group_members gm
		JOIN org_members m ON m.user_id = gm.user_id AND m.org_id = $1
		JOIN groups g ON g.id = gm.group_id AND g.org_id = $1
		WHERE EXISTS (
			SELECT 1 FROM warehouse_table_grants wtg
			WHERE wtg.warehouse_id = $2 AND wtg.org_id = $1
			  AND wtg.subject_type = 'group'
			  AND wtg.subject_id = gm.group_id::text
		)`, orgID.String(), warehouseID.String())
	if err != nil {
		return chaccess.DesiredState{}, fmt.Errorf("load group memberships: %w", err)
	}
	for membershipRows.Next() {
		var userID, groupID uuid.UUID
		if err := membershipRows.Scan(&userID, &groupID); err != nil {
			membershipRows.Close()
			return chaccess.DesiredState{}, fmt.Errorf("scan group membership: %w", err)
		}
		memberships[userID] = append(memberships[userID], groupID)
	}
	membershipRows.Close()
	if err := membershipRows.Err(); err != nil {
		return chaccess.DesiredState{}, fmt.Errorf("read group memberships: %w", err)
	}

	// s.masterKey is already crypto.DeriveKey's output (see NewServer), which
	// is exactly what chaccess.DerivePassword expects.
	return chaccess.Compute(orgID, warehouseID, s.masterKey, grants, memberships, users), nil
}

// setWarehouseSyncStatus records a sync state transition. syncErr clears
// sync_error when empty. appliedFP, when non-empty, is stored as the
// master-key fingerprint applied by a successful run; last_synced_at only
// advances on the ready transition.
func (s *Server) setWarehouseSyncStatus(ctx context.Context, warehouseID uuid.UUID, status, syncErr, appliedFP string) error {
	var errText, fp *string
	if syncErr != "" {
		errText = &syncErr
	}
	if appliedFP != "" {
		fp = &appliedFP
	}
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE warehouses
		SET sync_status = $2,
		    sync_error = $3,
		    applied_master_fp = COALESCE($4, applied_master_fp),
		    last_synced_at = CASE WHEN $2 = 'ready' THEN now() ELSE last_synced_at END,
		    updated_at = now()
		WHERE id = $1`,
		warehouseID.String(), status, errText, fp)
	if err != nil {
		return fmt.Errorf("set warehouse %s sync status %q: %w", warehouseID, status, err)
	}
	return nil
}

// failWarehouseSync records the error status and returns the original cause.
// A failed status write is joined so the caller sees both.
func (s *Server) failWarehouseSync(ctx context.Context, warehouseID uuid.UUID, cause error) error {
	if err := s.setWarehouseSyncStatus(ctx, warehouseID, "error", cause.Error(), ""); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// auditWarehouseDrift records detected drift (unquotable catalog names,
// wildcard grants, or unexpected role wiring). Audit failures are logged and
// never abort the reconcile.
func (s *Server) auditWarehouseDrift(ctx context.Context, orgID, warehouseID uuid.UUID, skipped []string, actual chaccess.ActualState) {
	meta := map[string]any{"warehouse_id": warehouseID.String()}
	if len(skipped) > 0 {
		meta["skipped"] = skipped
	}
	if len(actual.Wildcards) > 0 {
		wildcards := make([]string, 0, len(actual.Wildcards))
		for _, w := range actual.Wildcards {
			wildcards = append(wildcards, fmt.Sprintf("%s %s", w.Subject, w.Scope))
		}
		meta["wildcards"] = wildcards
	}
	if len(actual.Unexpected) > 0 {
		meta["unexpected"] = actual.Unexpected
	}
	if err := s.audit.Log(ctx, audit.Entry{
		OrgID:        orgID.String(),
		Action:       "warehouse.drift",
		ResourceType: "warehouse",
		ResourceID:   warehouseID.String(),
		Metadata:     meta,
	}); err != nil {
		slog.Warn("warehouse drift audit failed", "warehouse_id", warehouseID, "error", err)
	}
}

// describeWarehouseDrift renders wildcards as "subject scope" and unexpected
// entries verbatim for sync_error.
func describeWarehouseDrift(actual chaccess.ActualState) string {
	parts := make([]string, 0, len(actual.Wildcards)+len(actual.Unexpected))
	for _, w := range actual.Wildcards {
		parts = append(parts, fmt.Sprintf("wildcard grant %s %s", w.Subject, w.Scope))
	}
	for _, u := range actual.Unexpected {
		parts = append(parts, "unexpected "+u)
	}
	return strings.Join(parts, "; ")
}

// openWarehouseProvisionerConn opens a short-lived ClickHouse connection from
// a decrypted connector config for provisioning DDL. It follows the executor's
// connection shape (native protocol, port 9000 default, TLS per ssl_mode) but
// is deliberately not built on the executor's per-query path.
func openWarehouseProvisionerConn(ctx context.Context, cfg models.ConnectorConfig) (clickhouse.Conn, error) {
	port := cfg.Port
	if port == 0 {
		port = 9000
	}
	opts := &clickhouse.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, port)},
		Auth: clickhouse.Auth{
			Username: cfg.User,
			Password: cfg.Password,
		},
		Protocol: clickhouse.Native,
	}
	if cfg.Database != "" {
		opts.Auth.Database = cfg.Database
	}
	if cfg.SSLMode == "require" || cfg.SSLMode == "verify-full" {
		opts.TLS = &tls.Config{
			InsecureSkipVerify: cfg.SSLMode == "require",
		}
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return conn, nil
}
