package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/the-heaven-labs/aether/internal/audit"
	"github.com/the-heaven-labs/aether/internal/auth"
	"github.com/the-heaven-labs/aether/internal/chaccess"
)

// warehouseProvisionerTestStep is one phase of the provisioner self-test.
type warehouseProvisionerTestStep struct {
	Name  string  `json:"name"`
	OK    bool    `json:"ok"`
	Error *string `json:"error,omitempty"`
}

// warehouseProvisionerTestResult is the POST /warehouses/{id}/test response.
type warehouseProvisionerTestResult struct {
	OK    bool                           `json:"ok"`
	Error *string                        `json:"error,omitempty"`
	Steps []warehouseProvisionerTestStep `json:"steps"`
}

// testWarehouseOperationTimeout bounds the whole provisioner self-test,
// including connection, DDL, verification, and cleanup.
const testWarehouseOperationTimeout = 90 * time.Second

// @Summary Test a warehouse provisioner
// @Description Run a non-mutating end-to-end check of the warehouse's ClickHouse provisioner: connect, create a throwaway user, grant the mode's read bundle on up to three catalog tables, verify the grants are visible, drop the user, and verify no identity or grant remains.
// @Tags warehouses
// @Produce json
// @Param id path string true "Warehouse ID"
// @Success 200 {object} object
// @Failure 404 {object} map[string]string
// @Security BearerAuth
// @Router /warehouses/{id}/test [post]
func (s *Server) handleTestWarehouseProvisioner(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	ctx := r.Context()

	warehouseUUID, ok := parsePathUUID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}

	// The endpoint works whether or not AETHER_CH_TABLE_PERMISSIONS is enabled:
	// it validates the provisioner credential itself by connecting and running
	// DDL directly; reconcile semantics do not matter here.
	wh, err := s.loadWarehouseForOrg(ctx, claims.OrgID, warehouseUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "warehouse not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, testWarehouseOperationTimeout)
	defer cancel()

	result := s.runWarehouseProvisionerTest(ctx, claims, warehouseUUID, wh)

	meta := map[string]any{
		"warehouse_id": warehouseUUID.String(),
		"ok":           result.OK,
	}
	if result.Error != nil {
		meta["error"] = *result.Error
	}
	// Audit must land even if the request context was cancelled or timed out;
	// use a short detached context so the entry is not lost.
	auditCtx, auditCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
	defer auditCancel()
	if err := s.audit.Log(auditCtx, audit.Entry{
		OrgID:        claims.OrgID,
		UserID:       claims.UserID,
		Action:       "warehouse.provisioner_test",
		ResourceType: "warehouse",
		ResourceID:   warehouseUUID.String(),
		Metadata:     meta,
	}); err != nil {
		slog.Warn("warehouse provisioner test audit failed", "warehouse_id", warehouseUUID, "error", err)
	}

	writeJSON(w, http.StatusOK, result)
}

// runWarehouseProvisionerTest performs the actual self-test under the
// per-warehouse sync lock so it cannot interleave with a reconcile.
func (s *Server) runWarehouseProvisionerTest(ctx context.Context, claims *auth.Claims, warehouseID uuid.UUID, wh warehouseJSON) warehouseProvisionerTestResult {
	result := warehouseProvisionerTestResult{Steps: make([]warehouseProvisionerTestStep, 0, 5)}

	hdr, err := s.loadWarehouseHeader(ctx, warehouseID)
	if err != nil {
		return s.failProvisionerTest(result, "connect", fmt.Errorf("load warehouse: %w", err))
	}

	cfg, err := s.loadProvisionerConfig(ctx, warehouseID, hdr.orgID, hdr.provisionerID)
	if err != nil {
		return s.failProvisionerTest(result, "connect", err)
	}

	lockCtx, lockCancel := context.WithTimeout(ctx, testWarehouseOperationTimeout)
	defer lockCancel()

	var (
		testIdent  string
		cleanupErr error
		cleanupRan bool
	)
	runErr := s.withWarehouseSyncLock(lockCtx, warehouseID, func(lockCtx context.Context) error {
		conn, err := openWarehouseProvisionerConn(lockCtx, cfg)
		if err != nil {
			return s.stepError(&result, "connect", err)
		}
		defer conn.Close()

		mode, err := chaccess.DetectPrivilegeMode(lockCtx, conn)
		if err != nil {
			return s.stepError(&result, "connect", fmt.Errorf("detect clickhouse privilege mode: %w", err))
		}

		tables, err := chaccess.LoadCatalogTables(lockCtx, conn)
		if err != nil {
			return s.stepError(&result, "connect", fmt.Errorf("load catalog tables: %w", err))
		}
		tables = chaccess.SanitizeCatalogTables(tables)
		if len(tables) > 3 {
			tables = tables[:3]
		}

		result.Steps = append(result.Steps, warehouseProvisionerTestStep{Name: "connect", OK: true})

		suffix, err := randomHex(16)
		if err != nil {
			return s.stepError(&result, "create user", fmt.Errorf("generate test suffix: %w", err))
		}
		testIdent = chaccess.IdentifierPrefix(warehouseID) + "test_" + suffix
		password, err := randomHex(32)
		if err != nil {
			return s.stepError(&result, "create user", fmt.Errorf("generate test password: %w", err))
		}

		quotedIdent, err := chaccess.QuoteIdent(testIdent)
		if err != nil {
			return s.stepError(&result, "create user", fmt.Errorf("quote test identity: %w", err))
		}

		// Ensure the throwaway identity is removed and verified gone even if a
		// later step fails or the request context is cancelled. The cleanup
		// runs in a detached context and reports its own result.
		defer func() {
			if testIdent == "" {
				return
			}
			cleanupRan = true
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(lockCtx), 30*time.Second)
			defer cleanupCancel()
			cleanupErr = s.verifyTestIdentityRemoved(cleanupCtx, conn, testIdent)
		}()

		createStmt := fmt.Sprintf(
			"CREATE USER IF NOT EXISTS %s IDENTIFIED WITH sha256_password BY '%s' GRANTEES NONE",
			quotedIdent, password)
		if err := conn.Exec(lockCtx, createStmt); err != nil {
			return s.stepError(&result, "create user", fmt.Errorf("create test user: %w", err))
		}
		result.Steps = append(result.Steps, warehouseProvisionerTestStep{Name: "create user", OK: true})

		for _, tbl := range tables {
			qdb, err1 := chaccess.QuoteObjectIdent(tbl.Database)
			qtbl, err2 := chaccess.QuoteObjectIdent(tbl.Table)
			if err1 != nil || err2 != nil {
				// SanitizeCatalogTables already filters these, but skip defensively.
				continue
			}
			privs := mode.Bundle()
			privNames := make([]string, len(privs))
			for i, p := range privs {
				privNames[i] = string(p)
			}
			grantStmt := fmt.Sprintf("GRANT %s ON %s.%s TO %s",
				strings.Join(privNames, ", "), qdb, qtbl, quotedIdent)
			if err := conn.Exec(lockCtx, grantStmt); err != nil {
				return s.stepError(&result, "grant access", fmt.Errorf("grant on %s.%s: %w", tbl.Database, tbl.Table, err))
			}
		}
		result.Steps = append(result.Steps, warehouseProvisionerTestStep{Name: "grant access", OK: true})

		exists, err := s.testIdentityExists(lockCtx, conn, testIdent)
		if err != nil {
			return s.stepError(&result, "verify grants", fmt.Errorf("verify user exists: %w", err))
		}
		if !exists {
			return s.stepError(&result, "verify grants", errors.New("test user is not visible after creation"))
		}
		for _, tbl := range tables {
			visible, err := s.testGrantVisible(lockCtx, conn, testIdent, tbl.Database, tbl.Table)
			if err != nil {
				return s.stepError(&result, "verify grants", fmt.Errorf("verify grant on %s.%s: %w", tbl.Database, tbl.Table, err))
			}
			if !visible {
				return s.stepError(&result, "verify grants", fmt.Errorf("grant on %s.%s is not visible", tbl.Database, tbl.Table))
			}
		}
		result.Steps = append(result.Steps, warehouseProvisionerTestStep{Name: "verify grants", OK: true})

		// Normal-path drop and verification are handled by the deferred cleanup
		// so the same verification runs on every path.
		return nil
	})

	if cleanupRan {
		if cleanupErr != nil {
			msg := cleanupErr.Error()
			result.Steps = append(result.Steps, warehouseProvisionerTestStep{Name: "remove test user", OK: false, Error: &msg})
		} else {
			result.Steps = append(result.Steps, warehouseProvisionerTestStep{Name: "remove test user", OK: true})
		}
	}

	if errors.Is(runErr, errWarehouseSyncInProgress) {
		return s.failProvisionerTest(result, "connect", runErr)
	}
	if runErr != nil {
		if cleanupErr != nil {
			combined := fmt.Sprintf("%s; cleanup failed: %s", runErr.Error(), cleanupErr.Error())
			return s.failProvisionerTestWithResult(result, combined)
		}
		// Surface the first failed step's error as the overall error.
		for _, step := range result.Steps {
			if !step.OK && step.Error != nil {
				return s.failProvisionerTestWithResult(result, *step.Error)
			}
		}
		return s.failProvisionerTestWithResult(result, runErr.Error())
	}

	if cleanupErr != nil {
		msg := cleanupErr.Error()
		return s.failProvisionerTestWithResult(result, msg)
	}

	result.OK = true
	return result
}

// stepError records a failed step and returns an error so the lock callback
// exits. The returned error is the same as the step error so callers can
// short-circuit with return s.stepError(...).
func (s *Server) stepError(result *warehouseProvisionerTestResult, name string, err error) error {
	msg := err.Error()
	result.Steps = append(result.Steps, warehouseProvisionerTestStep{Name: name, OK: false, Error: &msg})
	return err
}

// failProvisionerTest returns a failed result with a single named step and an
// overall error message.
func (s *Server) failProvisionerTest(result warehouseProvisionerTestResult, stepName string, err error) warehouseProvisionerTestResult {
	msg := err.Error()
	result.Steps = append(result.Steps, warehouseProvisionerTestStep{Name: stepName, OK: false, Error: &msg})
	result.Error = &msg
	return result
}

// failProvisionerTestWithResult sets the overall error on an already-populated
// failed result.
func (s *Server) failProvisionerTestWithResult(result warehouseProvisionerTestResult, msg string) warehouseProvisionerTestResult {
	result.Error = &msg
	return result
}

// testIdentityExists reports whether a user with the given name is visible in
// system.users.
func (s *Server) testIdentityExists(ctx context.Context, conn clickhouse.Conn, name string) (bool, error) {
	var n uint64
	if err := conn.QueryRow(ctx, "SELECT count() FROM system.users WHERE name = ?", name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// testGrantVisible reports whether at least one grant row is visible for the
// user on the given database/table.
func (s *Server) testGrantVisible(ctx context.Context, conn clickhouse.Conn, userName, database, table string) (bool, error) {
	var n uint64
	if err := conn.QueryRow(ctx,
		"SELECT count() FROM system.grants WHERE user_name = ? AND database = ? AND table = ?",
		userName, database, table).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// testGrantsRemain reports whether any grant rows remain for the user.
func (s *Server) testGrantsRemain(ctx context.Context, conn clickhouse.Conn, userName string) (bool, error) {
	var n uint64
	if err := conn.QueryRow(ctx,
		"SELECT count() FROM system.grants WHERE user_name = ?", userName).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// verifyTestIdentityRemoved drops the throwaway test user and verifies that
// both the user and any grants for it are gone. It returns an error if the
// drop fails or if anything remains.
func (s *Server) verifyTestIdentityRemoved(ctx context.Context, conn clickhouse.Conn, ident string) error {
	if err := s.dropTestIdentity(ctx, conn, ident); err != nil {
		return fmt.Errorf("drop test user: %w", err)
	}
	exists, err := s.testIdentityExists(ctx, conn, ident)
	if err != nil {
		return fmt.Errorf("verify user removed: %w", err)
	}
	if exists {
		return errors.New("test user still exists after drop")
	}
	grantsRemain, err := s.testGrantsRemain(ctx, conn, ident)
	if err != nil {
		return fmt.Errorf("verify grants removed: %w", err)
	}
	if grantsRemain {
		return errors.New("grants for test user still remain after drop")
	}
	return nil
}

// dropTestIdentity removes the throwaway test user.
func (s *Server) dropTestIdentity(ctx context.Context, conn clickhouse.Conn, ident string) error {
	quoted, err := chaccess.QuoteIdent(ident)
	if err != nil {
		return err
	}
	return conn.Exec(ctx, fmt.Sprintf("DROP USER IF EXISTS %s", quoted))
}

// randomHex returns a lowercase hex string using n random bytes.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
