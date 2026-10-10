package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

// dashboardFingerprintUnmanaged is the constant access fingerprint for
// connectors whose execution does not depend on the viewer's warehouse table
// grants: unmanaged connectors execute with their shared stored credential, as
// does every connector while warehouse management is disabled.
const dashboardFingerprintUnmanaged = "unmanaged"

// dashboardFingerprintPublic is the fingerprint for public-token runs: the
// token scope in the cache key already discriminates them.
const dashboardFingerprintPublic = "public"

// dashboardQueryComputeHook is a test seam: when non-nil it replaces the real
// query computation under the single flight.
var dashboardQueryComputeHook func(*Server, context.Context, dashboardQueryParams) (*dashboardQueryResponse, error)

// dashboardAccessFingerprint returns a stable hash of the viewer's effective
// data access for a served connector. Managed ClickHouse connectors hash the
// effective table-grant set (user + groups + Everyone, org-membership gated);
// everything else executes with a shared stored credential, so the fingerprint
// is constant. "Managed" is inferred from warehouse_id != nil: a non-ClickHouse
// connector linked to a warehouse still executes with its stored credential, so
// grant-hashing it only under-shares cache entries (the safe direction). Errors
// are the caller's signal to fall back to a per-viewer key.
func (s *Server) dashboardAccessFingerprint(ctx context.Context, orgID, userID string, warehouseID *string) (string, error) {
	if warehouseID == nil || !s.warehouseManagementEnabled() {
		return dashboardFingerprintUnmanaged, nil
	}
	wid, err := uuid.Parse(*warehouseID)
	if err != nil {
		return "", err
	}
	keys, _, err := s.loadEffectiveWarehouseGrants(ctx, wid, orgID, userID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, k := range keys { // already sorted by (database, table)
		// Length-prefixing each field makes the encoding injective regardless
		// of name contents, so distinct grant sets cannot collide.
		fmt.Fprintf(&b, "%d:%s:%d:%s\n", len(k.Database), k.Database, len(k.Table), k.Table)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}

// dashboardQueryAccessFingerprint resolves the cache-sharing fingerprint for
// one run. Public-token runs keep the token scope as their discriminator;
// authenticated runs return a per-user key whenever the viewer could not
// execute the query or the effective-access fingerprint cannot be computed
// (fail closed — caching continues, sharing stops); only a successfully
// resolved fingerprint is shared.
func (s *Server) dashboardQueryAccessFingerprint(ctx context.Context, p dashboardQueryParams) string {
	if p.CacheScope != "" {
		return dashboardFingerprintPublic
	}
	perUser := "user:" + p.Identity.UserID
	// A viewer without `use` on the served connector is denied by openQuery on
	// the miss path; a shared key would let a cache hit return data ahead of
	// that denial.
	useOK, err := s.checkPermission(ctx, p.Identity.UserID, p.OrgID, p.Identity.Role, "connector", p.ConnectorID, "use")
	if err != nil || !useOK {
		return perUser
	}
	computed, err := s.dashboardAccessFingerprint(ctx, p.OrgID, p.Identity.UserID, p.WarehouseID)
	if err != nil {
		slog.Debug("dashboard access fingerprint unavailable; using a per-user cache key",
			"org_id", p.OrgID, "user_id", p.Identity.UserID, "connector_id", p.ConnectorID, "error", err)
		return perUser
	}
	return computed
}
