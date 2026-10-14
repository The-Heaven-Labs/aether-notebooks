package chaccess

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Privilege is a ClickHouse access type Aether manages as a read grant.
// Tables and views ignore dictGet/SHOW DICTIONARIES; dictionaries require
// them (modern ClickHouse denies dictGet-family functions without dictGet).
type Privilege string

const (
	AccessSelect           Privilege = "SELECT"
	AccessDictGet          Privilege = "dictGet"
	AccessShowDictionaries Privilege = "SHOW DICTIONARIES"
)

// privilegeOrder is the deterministic order privileges are emitted in a
// GRANT/REVOKE statement. It is also the full (modern) read bundle.
var privilegeOrder = []Privilege{AccessSelect, AccessDictGet, AccessShowDictionaries}

// PrivilegeMode selects the read bundle a warehouse server supports. Granular
// dictionary privileges (dictGet) exist from ClickHouse 23.x onward;
// pre-granular servers reject them, so Aether falls back to SELECT-only, which
// was the dictionary read privilege there.
type PrivilegeMode string

const (
	// PrivilegeModeFull grants SELECT, dictGet, SHOW DICTIONARIES per object.
	PrivilegeModeFull PrivilegeMode = "full"
	// PrivilegeModeLegacy grants SELECT only (pre-granular servers).
	PrivilegeModeLegacy PrivilegeMode = "legacy"
)

// Bundle returns the privileges granted for every managed object under mode.
// The returned slice is an independent copy; callers must not mutate it.
func (m PrivilegeMode) Bundle() []Privilege {
	if m == PrivilegeModeLegacy {
		return []Privilege{AccessSelect}
	}
	return append([]Privilege(nil), privilegeOrder...)
}

// GrantPrivilege is one (object, privilege) pair: the unit of the ClickHouse
// DDL diff. Grant itself stays the object-level key — tables, views, and
// dictionaries share the name-based grant model.
type GrantPrivilege struct {
	Grant
	Access Privilege
}

// DetectPrivilegeMode probes the provisioner connection for granular
// dictionary privileges. A probe failure other than "table/rows missing"
// returns an error so a transient outage cannot silently flip modes and
// trigger grant/revoke churn.
func DetectPrivilegeMode(ctx context.Context, conn clickhouse.Conn) (PrivilegeMode, error) {
	var n uint64
	err := conn.QueryRow(ctx, `
		SELECT count() FROM system.columns
		WHERE database = 'system' AND table = 'privileges' AND name = 'privilege'`).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("probe system.privileges: %w", err)
	}
	if n == 0 {
		return modeFromProbe(false, 0), nil
	}
	err = conn.QueryRow(ctx, `
		SELECT count() FROM system.privileges
		WHERE toString(privilege) IN ('dictGet', 'SHOW DICTIONARIES')`).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("probe dictionary privileges: %w", err)
	}
	return modeFromProbe(true, n), nil
}

// modeFromProbe maps the privilege-catalog probe result to a mode. Both
// dictGet and SHOW DICTIONARIES must exist; anything else is the legacy
// SELECT-only world.
func modeFromProbe(hasPrivilegesTable bool, matching uint64) PrivilegeMode {
	if !hasPrivilegesTable || matching < 2 {
		return PrivilegeModeLegacy
	}
	return PrivilegeModeFull
}
