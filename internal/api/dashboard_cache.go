package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// dashboardFingerprintUnmanaged is the constant access fingerprint for
// connectors whose execution does not depend on the viewer's warehouse table
// grants: unmanaged connectors execute with their shared stored credential, as
// does every connector while warehouse management is disabled.
const dashboardFingerprintUnmanaged = "unmanaged"

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
