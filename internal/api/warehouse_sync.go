package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/chaccess"
	"github.com/the-heaven-labs/aether/internal/config"
	"github.com/the-heaven-labs/aether/internal/crypto"
	"github.com/the-heaven-labs/aether/internal/models"
)

// passwordLiteralRe matches a ClickHouse password literal ("BY '<secret>'").
// Derived passwords never contain quotes (chaccess rejects them), so a
// non-greedy quoted run cannot over-match.
var passwordLiteralRe = regexp.MustCompile(`(?i)\bBY\s*'[^']*'`)

// redactSecrets removes password literals from text that may reach sync_error
// or logs. Defense in depth: reconcile builds its own error strings without
// statement text, and driver messages do not echo statements today.
func redactSecrets(s string) string {
	return passwordLiteralRe.ReplaceAllString(s, "BY '<redacted>'")
}

// warehouseSyncTimeout bounds one reconcile run, including the provisioner
// connection and statement execution.
const warehouseSyncTimeout = 5 * time.Minute

// maxWarehouseReconcileStartupJitter caps the random delay applied before the
// first enqueue-all so replicas started together do not contend on the same
// warehouse advisory locks. The actual jitter is also bounded by a tenth of
// the configured interval, keeping short test intervals fast.
const maxWarehouseReconcileStartupJitter = 5 * time.Second

// warehouseReconcileJitterFn computes the startup jitter for an interval. It is
// a package var so tests can force deterministic (zero) jitter.
var warehouseReconcileJitterFn = warehouseReconcileJitter

// warehouseSyncLockKeySQL derives a stable, cross-process advisory-lock key
// from a warehouse UUID; hashtextextended yields a bigint acceptable to
// pg_try_advisory_lock(bigint).
const warehouseSyncLockKeySQL = `hashtextextended($1::text, 0)`

// reconcileWarehouse computes the desired ClickHouse access state for a
// warehouse and applies it through the provisioner connector. It is
// idempotent: a run with no Aether-side changes emits no DDL, and
// warehouses.applied_master_fp drives password re-keying after a master-key
// rotation.
//
// The sync worker wraps calls in a panic recovery; the recover here only
// records an honest sync_status so a panic cannot leave the warehouse stuck
// in 'syncing'. The original panic is re-raised for the worker's stack log.
func (s *Server) reconcileWarehouse(ctx context.Context, warehouseID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, warehouseSyncTimeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			panicErr := fmt.Errorf("reconcile warehouse %s panic: %v", warehouseID, r)
			// The run context may be cancelled or nearly done; use a short
			// detached context so the status write can still land.
			statusCtx, statusCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer statusCancel()
			if err := s.setWarehouseSyncStatus(statusCtx, warehouseID, "error", panicErr.Error(), ""); err != nil {
				slog.Warn("warehouse sync panic status update failed", "warehouse_id", warehouseID, "error", err)
			}
			panic(r)
		}
	}()

	hdr, err := s.loadWarehouseHeader(ctx, warehouseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("reconcile warehouse %s: %w", warehouseID, err)
	}
	if err != nil {
		return fmt.Errorf("reconcile warehouse %s: load warehouse: %w", warehouseID, err)
	}

	// Cross-process single-flight: one reconcile per warehouse at a time.
	// The lock is held on a dedicated pooled connection for the whole run.
	lockConn, locked, err := s.acquireWarehouseSyncLock(ctx, warehouseID)
	if err != nil {
		return fmt.Errorf("reconcile warehouse %s: %w", warehouseID, err)
	}
	if !locked {
		s.auditWarehouseSyncSkipped(ctx, hdr.orgID, warehouseID)
		return nil
	}
	defer releaseWarehouseSyncLock(lockConn, warehouseID)

	cfg, err := s.loadProvisionerConfig(ctx, warehouseID, hdr.orgID, hdr.provisionerID)
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID, err)
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

	desired, err := s.loadWarehouseDesiredState(ctx, warehouseID, hdr.orgID)
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID, err)
	}

	actual, err := chaccess.LoadActual(ctx, conn, warehouseID)
	if err != nil {
		return s.failWarehouseSync(ctx, warehouseID,
			fmt.Errorf("load clickhouse actual state: %w", err))
	}

	fingerprint := chaccess.Fingerprint(string(s.masterKey))
	actual.ForcePasswordReset = hdr.appliedFP == nil || *hdr.appliedFP != fingerprint

	// Diff before applying so drift is reported from the same observation the
	// plan is built from. A first provision legitimately reports everything as
	// missing, so only warehouses that were provisioned before alert.
	report := compareWarehouseState(warehouseID, desired, actual)
	prevProvisioned := hdr.appliedFP != nil || hdr.syncStatus == "ready"

	// Fail closed: wildcards defeat table-level least privilege and foreign
	// role wiring / grant options are outside Aether's model. Never mark
	// ready while they exist; remediation is manual in this iteration.
	if actual.HasWildcard() || len(actual.Unexpected) > 0 {
		syncErr := fmt.Errorf("warehouse %s clickhouse drift requires manual remediation: %s",
			warehouseID, describeWarehouseDrift(actual))
		if err := s.setWarehouseSyncStatus(ctx, warehouseID, "error", syncErr.Error(), ""); err != nil {
			return errors.Join(syncErr, err)
		}
		// An unchanged fail-closed state is one alert, not one per tick.
		if hdr.syncStatus != "error" || hdr.syncError == nil || *hdr.syncError != syncErr.Error() {
			s.auditWarehouseDriftReport(ctx, hdr.orgID, warehouseID, report, nil)
		}
		return syncErr
	}

	stmts, skipped := chaccess.Statements(desired, actual)
	auditReport := report
	if !prevProvisioned {
		auditReport = DriftReport{WarehouseID: warehouseID}
	}
	if !auditReport.IsEmpty() || len(skipped) > 0 {
		s.auditWarehouseDriftReport(ctx, hdr.orgID, warehouseID, auditReport, skipped)
	}
	// Never embed the statement text: CREATE/ALTER USER statements carry the
	// derived ClickHouse password, and the error reaches sync_error, worker
	// logs, and the admin UI. redactSecrets is belt-and-braces in case a
	// driver error echoes the statement.
	for i, stmt := range stmts {
		if execErr := conn.Exec(ctx, stmt); execErr != nil {
			return s.failWarehouseSync(ctx, warehouseID, fmt.Errorf(
				"execute statement %d of %d: %s", i+1, len(stmts), redactSecrets(execErr.Error())))
		}
	}

	if err := s.setWarehouseSyncStatus(ctx, warehouseID, "ready", "", fingerprint); err != nil {
		return fmt.Errorf("reconcile warehouse %s: %w", warehouseID, err)
	}

	// A clean no-op run writes no heartbeat: without the gate every ready
	// warehouse would produce an audit row on every reconcile tick.
	if len(stmts) > 0 || hdr.syncStatus != "ready" {
		if err := s.audit.Log(ctx, audit.Entry{
			OrgID:        hdr.orgID.String(),
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
	}
	return nil
}

// warehouseHeader is the warehouse row state shared by reconcile and drift
// detection: the owning org, provisioner connector ID (nil when unset), the
// master-key fingerprint recorded by the last successful run, and the stored
// sync status/error used to suppress repeated alerts.
type warehouseHeader struct {
	orgID         uuid.UUID
	provisionerID *uuid.UUID
	appliedFP     *string
	syncStatus    string
	syncError     *string
}

// loadWarehouseHeader reads the per-warehouse header state. pgx.ErrNoRows is
// returned unwrapped so callers can distinguish a deleted warehouse from a
// lookup failure.
func (s *Server) loadWarehouseHeader(ctx context.Context, warehouseID uuid.UUID) (warehouseHeader, error) {
	var h warehouseHeader
	err := s.db.Pool.QueryRow(ctx, `
		SELECT org_id, provisioner_connector_id, applied_master_fp, sync_status, sync_error
		FROM warehouses WHERE id = $1`, warehouseID.String(),
	).Scan(&h.orgID, &h.provisionerID, &h.appliedFP, &h.syncStatus, &h.syncError)
	if err != nil {
		return warehouseHeader{}, err
	}
	return h, nil
}

// loadProvisionerConfig loads, validates, and decrypts a warehouse's
// provisioner connector config. Error strings are stable because reconcile
// embeds them in sync_error; callers decide whether to record them.
//
// The provisioner must belong to this warehouse: a connector linked to a
// different warehouse (or to no warehouse at all) would adopt another
// warehouse's credential namespace.
// NOTE: the "provisioner must be read-write" requirement is not enforceable at
// load time — connectors carry no service-type/RW metadata yet (deferred with
// that work). A read-only credential fails at DDL execution time instead.
func (s *Server) loadProvisionerConfig(ctx context.Context, warehouseID, orgID uuid.UUID, provisionerID *uuid.UUID) (models.ConnectorConfig, error) {
	if provisionerID == nil {
		return models.ConnectorConfig{}, fmt.Errorf("warehouse %s has no provisioner connector", warehouseID)
	}
	var encrypted []byte
	var connectorWarehouseID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `
		SELECT config_encrypted, warehouse_id FROM connectors
		WHERE id = $1 AND org_id = $2 AND type = 'clickhouse' AND deleted_at IS NULL`,
		provisionerID.String(), orgID.String(),
	).Scan(&encrypted, &connectorWarehouseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.ConnectorConfig{}, fmt.Errorf(
			"provisioner connector %s is missing, soft-deleted, or not a clickhouse connector", provisionerID)
	}
	if err != nil {
		return models.ConnectorConfig{}, fmt.Errorf("load provisioner connector: %w", err)
	}
	if connectorWarehouseID == nil || *connectorWarehouseID != warehouseID {
		return models.ConnectorConfig{}, fmt.Errorf(
			"provisioner connector %s does not belong to warehouse %s", provisionerID, warehouseID)
	}

	plain, err := crypto.Decrypt(encrypted, s.masterKey)
	if err != nil {
		return models.ConnectorConfig{}, fmt.Errorf("decrypt provisioner config: %w", err)
	}
	var cfg models.ConnectorConfig
	if err := json.Unmarshal(plain, &cfg); err != nil {
		return models.ConnectorConfig{}, fmt.Errorf("parse provisioner config: %w", err)
	}
	return cfg, nil
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

// DriftReport describes how a warehouse's actual ClickHouse access state
// differs from desired. All slices are sorted and deduplicated; an empty
// report means desired and actual agree. It is informational: reconcile
// applies the plan, drift detection only reports.
type DriftReport struct {
	WarehouseID uuid.UUID
	// UnexpectedGrants are actual SELECT grants absent from desired state,
	// rendered "db.table for <subject>".
	UnexpectedGrants []string
	// MissingGrants are desired SELECT grants absent from actual state,
	// rendered "db.table for <subject>".
	MissingGrants []string
	// UnexpectedUsers are users in the warehouse namespace that desired state
	// does not know about.
	UnexpectedUsers []string
	// UnexpectedRoles are roles in the warehouse namespace that desired state
	// does not know about.
	UnexpectedRoles []string
	// MissingUsers are desired users absent from actual state entirely (e.g.
	// access is group/everyone-only, so no grant of their own is missing).
	MissingUsers []string
	// MissingRoles are desired roles absent from actual state entirely.
	MissingRoles []string
	// MissingRoleMemberships are desired roles not granted to an existing
	// desired user, rendered "role <role> for <user>".
	MissingRoleMemberships []string
	// Wildcards are actual wildcard grants (subject + scope). They defeat
	// table-level least privilege and make reconcile fail closed.
	Wildcards []chaccess.WildcardGrant
	// Unexpected mirrors chaccess.ActualState.Unexpected: non-SELECT grants,
	// foreign role wiring, and grants held WITH GRANT OPTION.
	Unexpected []string
	// DefaultRolesNotAll are desired users whose roles are provisioned but
	// whose default role is not "all"; the next reconcile resets it.
	DefaultRolesNotAll []string
}

// IsEmpty reports whether desired and actual state agree on everything the
// report covers.
func (r DriftReport) IsEmpty() bool {
	return len(r.UnexpectedGrants) == 0 && len(r.MissingGrants) == 0 &&
		len(r.UnexpectedUsers) == 0 && len(r.UnexpectedRoles) == 0 &&
		len(r.MissingUsers) == 0 && len(r.MissingRoles) == 0 &&
		len(r.MissingRoleMemberships) == 0 &&
		len(r.Wildcards) == 0 && len(r.Unexpected) == 0 &&
		len(r.DefaultRolesNotAll) == 0
}

// detectWarehouseDrift compares a warehouse's desired state against the
// actual ClickHouse state without applying any DDL. It uses the same
// provisioner connection as reconcile and audits a non-empty report as
// warehouse.drift. Reconcile remains the auto-healing path; detection exists
// so operators can observe drift without waiting for a mutation.
func (s *Server) detectWarehouseDrift(ctx context.Context, warehouseID uuid.UUID) (DriftReport, error) {
	ctx, cancel := context.WithTimeout(ctx, warehouseSyncTimeout)
	defer cancel()

	hdr, err := s.loadWarehouseHeader(ctx, warehouseID)
	if err != nil {
		return DriftReport{}, fmt.Errorf("detect warehouse %s drift: %w", warehouseID, err)
	}
	cfg, err := s.loadProvisionerConfig(ctx, warehouseID, hdr.orgID, hdr.provisionerID)
	if err != nil {
		return DriftReport{}, fmt.Errorf("detect warehouse %s drift: %w", warehouseID, err)
	}

	conn, err := openWarehouseProvisionerConn(ctx, cfg)
	if err != nil {
		return DriftReport{}, fmt.Errorf("detect warehouse %s drift: connect provisioner: %w", warehouseID, err)
	}
	defer conn.Close()

	desired, err := s.loadWarehouseDesiredState(ctx, warehouseID, hdr.orgID)
	if err != nil {
		return DriftReport{}, fmt.Errorf("detect warehouse %s drift: %w", warehouseID, err)
	}
	actual, err := chaccess.LoadActual(ctx, conn, warehouseID)
	if err != nil {
		return DriftReport{}, fmt.Errorf("detect warehouse %s drift: load clickhouse actual state: %w", warehouseID, err)
	}

	report := compareWarehouseState(warehouseID, desired, actual)
	if !report.IsEmpty() {
		s.auditWarehouseDriftReport(ctx, hdr.orgID, warehouseID, report, nil)
	}
	return report, nil
}

// errWarehouseSyncInProgress reports that another reconcile holds the
// per-warehouse sync lock; callers should treat it as retryable.
var errWarehouseSyncInProgress = errors.New("warehouse sync in progress")

// errWarehouseProvisionerNeeded reports that a warehouse has provisioned
// ClickHouse identities but no provisioner connector, so the identities
// cannot be revoked. The caller must fail closed and tell the admin to assign
// a provisioner.
var errWarehouseProvisionerNeeded = errors.New(
	"warehouse has provisioned ClickHouse identities but no provisioner connector; assign a provisioner to revoke them before deleting")

// withWarehouseSyncLock runs fn while holding the per-warehouse reconcile
// lock. It fails closed when a reconcile already holds the lock instead of
// waiting, so callers can surface a retryable error.
func (s *Server) withWarehouseSyncLock(ctx context.Context, warehouseID uuid.UUID, fn func(context.Context) error) error {
	lockConn, locked, err := s.acquireWarehouseSyncLock(ctx, warehouseID)
	if err != nil {
		return err
	}
	if !locked {
		return fmt.Errorf("%w: warehouse %s", errWarehouseSyncInProgress, warehouseID)
	}
	defer releaseWarehouseSyncLock(lockConn, warehouseID)
	return fn(ctx)
}

// dropWarehouseIdentitiesLocked revokes every ClickHouse identity in a
// warehouse's namespace by computing an empty desired state against the
// observed actual state; the orphan-drop ordering (users before roles) handles
// dependencies. Callers must hold the per-warehouse sync lock (the delete path
// holds it across this drop and the row delete) so a concurrent reconcile
// cannot recreate identities mid-drop. It returns whether any drop statements
// ran (so callers can decide whether a refused delete needs a restoring
// reconcile) and fails closed when the provisioner is missing/unreachable,
// any statement fails, or identities remain afterwards. The warehouse row is
// not touched here.
func (s *Server) dropWarehouseIdentitiesLocked(ctx context.Context, warehouseID uuid.UUID) (bool, error) {
	hdr, err := s.loadWarehouseHeader(ctx, warehouseID)
	if err != nil {
		return false, fmt.Errorf("load warehouse header: %w", err)
	}
	if hdr.provisionerID == nil {
		// applied_master_fp proves a provisioner once provisioned this
		// warehouse. Without one the identities cannot be revoked, so the
		// delete must fail closed instead of orphaning them.
		if hdr.appliedFP != nil {
			return false, errWarehouseProvisionerNeeded
		}
		return false, nil
	}
	cfg, err := s.loadProvisionerConfig(ctx, warehouseID, hdr.orgID, hdr.provisionerID)
	if err != nil {
		return false, err
	}

	conn, err := openWarehouseProvisionerConn(ctx, cfg)
	if err != nil {
		return false, fmt.Errorf("connect provisioner: %w", err)
	}
	defer conn.Close()

	actual, err := chaccess.LoadActual(ctx, conn, warehouseID)
	if err != nil {
		return false, fmt.Errorf("load clickhouse actual state: %w", err)
	}
	stmts, skipped := chaccess.Statements(chaccess.DesiredState{}, actual)
	for i, stmt := range stmts {
		if execErr := conn.Exec(ctx, stmt); execErr != nil {
			return len(stmts) > 0, fmt.Errorf("execute drop statement %d of %d: %s",
				i+1, len(stmts), redactSecrets(execErr.Error()))
		}
	}
	if len(skipped) > 0 {
		return len(stmts) > 0, fmt.Errorf("cannot revoke %d unquotable identity name(s): %s",
			len(skipped), strings.Join(skipped, ", "))
	}

	// Verify rather than assume: DROP ... IF EXISTS plus the prefix filter is
	// the only thing between a deleted row and orphaned identities, so any
	// identity still visible (an unquotable name, a concurrent server-side
	// change) must fail the cleanup instead of surviving it silently.
	remaining, err := chaccess.LoadActual(ctx, conn, warehouseID)
	if err != nil {
		return len(stmts) > 0, fmt.Errorf("verify clickhouse identities removed: %w", err)
	}
	if len(remaining.Users) > 0 || len(remaining.Roles) > 0 {
		return len(stmts) > 0, fmt.Errorf("warehouse %s still has %d user(s) and %d role(s) after cleanup",
			warehouseID, len(remaining.Users), len(remaining.Roles))
	}

	meta := map[string]any{
		"warehouse_id": warehouseID.String(),
		"statements":   len(stmts),
		"users":        len(actual.Users),
		"roles":        len(actual.Roles),
	}
	if err := s.audit.Log(ctx, audit.Entry{
		OrgID:        hdr.orgID.String(),
		Action:       "warehouse.identities.cleanup",
		ResourceType: "warehouse",
		ResourceID:   warehouseID.String(),
		Metadata:     meta,
	}); err != nil {
		slog.Warn("warehouse identity cleanup audit failed", "warehouse_id", warehouseID, "error", err)
	}
	return len(stmts) > 0, nil
}

// compareWarehouseState diffs desired against actual. It is pure so it can be
// unit-tested without ClickHouse.
func compareWarehouseState(warehouseID uuid.UUID, desired chaccess.DesiredState, actual chaccess.ActualState) DriftReport {
	report := DriftReport{WarehouseID: warehouseID}

	for ident, grants := range actual.Roles {
		want := map[chaccess.Grant]struct{}{}
		if rs, ok := desired.Roles[ident]; ok {
			want = rs.Grants
		}
		for g := range grants {
			if _, ok := want[g]; !ok {
				report.UnexpectedGrants = append(report.UnexpectedGrants, grantDriftLabel(g, ident))
			}
		}
		if _, ok := desired.Roles[ident]; !ok {
			report.UnexpectedRoles = append(report.UnexpectedRoles, ident)
		}
	}
	for ident, user := range actual.Users {
		want := map[chaccess.Grant]struct{}{}
		if us, ok := desired.Users[ident]; ok {
			want = us.DirectGrants
		}
		for g := range user.DirectGrants {
			if _, ok := want[g]; !ok {
				report.UnexpectedGrants = append(report.UnexpectedGrants, grantDriftLabel(g, ident))
			}
		}
		if _, ok := desired.Users[ident]; !ok {
			report.UnexpectedUsers = append(report.UnexpectedUsers, ident)
		}
	}

	for ident, rs := range desired.Roles {
		have, exists := actual.Roles[ident]
		if !exists {
			report.MissingRoles = append(report.MissingRoles, ident)
		}
		for g := range rs.Grants {
			if _, ok := have[g]; !ok {
				report.MissingGrants = append(report.MissingGrants, grantDriftLabel(g, ident))
			}
		}
	}
	for ident, us := range desired.Users {
		user, exists := actual.Users[ident]
		if !exists {
			// The whole identity is missing; its memberships and direct
			// grants are covered by MissingUsers + MissingGrants, not by
			// per-membership entries.
			report.MissingUsers = append(report.MissingUsers, ident)
		} else {
			for _, role := range us.Roles {
				if _, ok := user.Roles[role]; !ok {
					report.MissingRoleMemberships = append(report.MissingRoleMemberships,
						roleMembershipDriftLabel(role, ident))
				}
			}
		}
		for g := range us.DirectGrants {
			if _, ok := user.DirectGrants[g]; !ok {
				report.MissingGrants = append(report.MissingGrants, grantDriftLabel(g, ident))
			}
		}
		// Mirrors the reconcile condition for SET DEFAULT ROLE ALL: only a
		// user that exists with desired roles can have default roles wrong.
		if exists && len(us.Roles) > 0 && !user.DefaultRolesAll {
			report.DefaultRolesNotAll = append(report.DefaultRolesNotAll, ident)
		}
	}

	report.Wildcards = append([]chaccess.WildcardGrant(nil), actual.Wildcards...)
	report.Unexpected = append([]string(nil), actual.Unexpected...)
	report.UnexpectedGrants = sortDedupe(report.UnexpectedGrants)
	report.MissingGrants = sortDedupe(report.MissingGrants)
	report.UnexpectedUsers = sortDedupe(report.UnexpectedUsers)
	report.UnexpectedRoles = sortDedupe(report.UnexpectedRoles)
	report.MissingUsers = sortDedupe(report.MissingUsers)
	report.MissingRoles = sortDedupe(report.MissingRoles)
	report.MissingRoleMemberships = sortDedupe(report.MissingRoleMemberships)
	report.DefaultRolesNotAll = sortDedupe(report.DefaultRolesNotAll)
	return report
}

// grantDriftLabel renders one grant and its subject for a drift report.
func grantDriftLabel(g chaccess.Grant, subject string) string {
	return fmt.Sprintf("%s.%s for %s", g.Database, g.Table, subject)
}

// roleMembershipDriftLabel renders a missing role membership for a report.
func roleMembershipDriftLabel(role, user string) string {
	return fmt.Sprintf("role %s for %s", role, user)
}

// sortDedupe returns a sorted, deduplicated copy of in (nil for empty input).
func sortDedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(in))
	for _, s := range in {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// setWarehouseSyncStatus records a sync state transition. syncErr clears
// sync_error when empty. appliedFP, when non-empty, is stored as the
// master-key fingerprint applied by a successful run; last_synced_at only
// advances on the ready transition.
func (s *Server) setWarehouseSyncStatus(ctx context.Context, warehouseID uuid.UUID, status, syncErr, appliedFP string) error {
	var errText, fp *string
	if syncErr != "" {
		syncErr = redactSecrets(syncErr)
		errText = &syncErr
	}
	if appliedFP != "" {
		fp = &appliedFP
	}
	tag, err := s.db.Pool.Exec(ctx, `
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
	if tag.RowsAffected() == 0 {
		// The warehouse was hard-deleted mid-run; surface it instead of
		// pretending the transition landed.
		return fmt.Errorf("set warehouse %s sync status %q: warehouse not found", warehouseID, status)
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

// acquireWarehouseSyncLock takes a session-scoped Postgres advisory lock so
// only one reconcile per warehouse runs at a time, even across API replicas.
// It returns (nil, false, nil) when another session already holds the lock;
// the returned connection must be passed to releaseWarehouseSyncLock.
//
// The lock lives on a dedicated non-pool connection: a reconcile run needs
// several pooled connections for its own DB work, and holding one of them
// for the whole run would self-deadlock once concurrent reconciles reach the
// pool's MaxConns.
func (s *Server) acquireWarehouseSyncLock(ctx context.Context, warehouseID uuid.UUID) (*pgx.Conn, bool, error) {
	conn, err := pgx.Connect(ctx, s.db.Pool.Config().ConnString())
	if err != nil {
		return nil, false, fmt.Errorf("connect for warehouse sync lock: %w", err)
	}
	var locked bool
	if err := conn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock(`+warehouseSyncLockKeySQL+`)`, warehouseID.String(),
	).Scan(&locked); err != nil {
		conn.Close(context.Background())
		return nil, false, fmt.Errorf("acquire warehouse sync lock: %w", err)
	}
	if !locked {
		conn.Close(context.Background())
		return nil, false, nil
	}
	return conn, true, nil
}

// releaseWarehouseSyncLock unlocks and closes the dedicated connection.
// Closing releases the session-scoped lock even if the explicit unlock fails.
func releaseWarehouseSyncLock(conn *pgx.Conn, warehouseID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.Exec(ctx,
		`SELECT pg_advisory_unlock(`+warehouseSyncLockKeySQL+`)`, warehouseID.String(),
	); err != nil {
		slog.Warn("warehouse sync lock release failed; closing connection drops it", "warehouse_id", warehouseID, "error", err)
	}
	conn.Close(context.Background())
}

// auditWarehouseSyncSkipped records that a reconcile was skipped because
// another process holds the warehouse lock.
func (s *Server) auditWarehouseSyncSkipped(ctx context.Context, orgID, warehouseID uuid.UUID) {
	if err := s.audit.Log(ctx, audit.Entry{
		OrgID:        orgID.String(),
		Action:       "warehouse.sync.skipped",
		ResourceType: "warehouse",
		ResourceID:   warehouseID.String(),
		Metadata: map[string]any{
			"warehouse_id": warehouseID.String(),
			"reason":       "another reconcile holds the warehouse lock",
		},
	}); err != nil {
		slog.Warn("warehouse sync skip audit failed", "warehouse_id", warehouseID, "error", err)
	}
}

// auditWarehouseDriftReport records a drift report (or skipped catalog names)
// as warehouse.drift. It is the single drift audit path for reconcile and
// detectWarehouseDrift; audit failures are logged and never abort the caller.
func (s *Server) auditWarehouseDriftReport(ctx context.Context, orgID, warehouseID uuid.UUID, report DriftReport, skipped []string) {
	if report.IsEmpty() && len(skipped) == 0 {
		return
	}
	meta := map[string]any{"warehouse_id": warehouseID.String()}
	if len(skipped) > 0 {
		meta["skipped"] = skipped
	}
	if len(report.UnexpectedGrants) > 0 {
		meta["unexpected_grants"] = report.UnexpectedGrants
	}
	if len(report.MissingGrants) > 0 {
		meta["missing_grants"] = report.MissingGrants
	}
	if len(report.UnexpectedUsers) > 0 {
		meta["unexpected_users"] = report.UnexpectedUsers
	}
	if len(report.UnexpectedRoles) > 0 {
		meta["unexpected_roles"] = report.UnexpectedRoles
	}
	if len(report.MissingUsers) > 0 {
		meta["missing_users"] = report.MissingUsers
	}
	if len(report.MissingRoles) > 0 {
		meta["missing_roles"] = report.MissingRoles
	}
	if len(report.MissingRoleMemberships) > 0 {
		meta["missing_role_memberships"] = report.MissingRoleMemberships
	}
	if len(report.Wildcards) > 0 {
		meta["wildcards"] = wildcardLabels(report.Wildcards)
	}
	if len(report.Unexpected) > 0 {
		meta["unexpected"] = report.Unexpected
	}
	if len(report.DefaultRolesNotAll) > 0 {
		meta["default_roles_not_all"] = report.DefaultRolesNotAll
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

// wildcardLabels renders wildcards as "subject scope" for audit metadata.
func wildcardLabels(wildcards []chaccess.WildcardGrant) []string {
	out := make([]string, 0, len(wildcards))
	for _, w := range wildcards {
		out = append(out, fmt.Sprintf("%s %s", w.Subject, w.Scope))
	}
	return out
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

// startWarehouseReconcileLoop starts the periodic catch-up enqueue. Every
// warehouse is enqueued once after a small jittered delay, then again on each
// interval; enqueues are non-blocking and coalesce in the sync service. The
// jitter spreads replicas started together so they do not contend on the same
// warehouse advisory locks. The loop stops when ctx is cancelled or Server.Close
// runs.
func (s *Server) startWarehouseReconcileLoop(ctx context.Context) {
	if s.warehouseSync == nil {
		return
	}
	interval := s.warehouseReconcileInterval
	if interval <= 0 {
		// Defensive fallback for a Server built without NewServer; the value
		// comes from config.DefaultWarehouseReconcileInterval.
		interval = config.DefaultWarehouseReconcileInterval
	}
	s.warehouseLoop.start(ctx, func(loopCtx context.Context) {
		if jitter := warehouseReconcileJitterFn(interval); jitter > 0 {
			select {
			case <-time.After(jitter):
			case <-loopCtx.Done():
				return
			}
		}
		s.enqueueAllWarehouses(loopCtx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				s.enqueueAllWarehouses(loopCtx)
			}
		}
	})
}

// warehouseReconcileJitter returns a random startup delay in [0, jitterMax).
// The cap is a tenth of the interval, so a short test interval does not make
// the first enqueue slow, and never more than 5s.
func warehouseReconcileJitter(interval time.Duration) time.Duration {
	jitterMax := interval / 10
	if jitterMax > maxWarehouseReconcileStartupJitter {
		jitterMax = maxWarehouseReconcileStartupJitter
	}
	if jitterMax <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(jitterMax)))
}

// enqueueAllWarehouses enqueues every warehouse with a provisioner for
// reconciliation. Warehouses without one cannot sync; assigning a provisioner
// is covered by the membership/CRUD triggers. Lookup failures are logged and
// swallowed: the next tick retries, and sync bookkeeping must never take down
// the loop. A cancelled context (shutdown) is not logged as a failure.
func (s *Server) enqueueAllWarehouses(ctx context.Context) {
	if s.warehouseSync == nil {
		return
	}
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id FROM warehouses WHERE provisioner_connector_id IS NOT NULL`)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("warehouse reconcile loop: list warehouses", "error", err)
		}
		return
	}
	defer rows.Close()
	for rows.Next() {
		var warehouseID uuid.UUID
		if err := rows.Scan(&warehouseID); err != nil {
			if ctx.Err() == nil {
				slog.Warn("warehouse reconcile loop: scan warehouse", "error", err)
			}
			return
		}
		s.warehouseSync.Enqueue(warehouseID)
	}
	if err := rows.Err(); err != nil && ctx.Err() == nil {
		slog.Warn("warehouse reconcile loop: read warehouses", "error", err)
	}
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
