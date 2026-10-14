package chaccess

import (
	"context"
	"errors"
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
	switch m {
	case PrivilegeModeLegacy:
		return []Privilege{AccessSelect}
	case PrivilegeModeFull:
		return append([]Privilege(nil), privilegeOrder...)
	default:
		// DetectPrivilegeMode only returns the two defined modes and callers
		// must propagate its error; an unknown mode is an internal invariant
		// violation, so fail loudly instead of silently picking a bundle.
		panic("Bundle: unknown privilege mode " + string(m))
	}
}

// GrantPrivilege is one (object, privilege) pair: the unit of the ClickHouse
// DDL diff. Grant itself stays the object-level key — tables, views, and
// dictionaries share the name-based grant model.
type GrantPrivilege struct {
	Grant
	Access Privilege
}

// DetectPrivilegeMode probes the provisioner connection for granular
// dictionary privileges (dictGet, SHOW DICTIONARIES).
//
// The direct read of system.privileges is authoritative and works for
// least-privilege provisioners: system.columns is access-filtered, so a
// provisioner without SELECT on system.privileges would wrongly see zero
// privilege columns and a metadata-first probe would misclassify modern
// servers as legacy. Only when the direct read fails does the existence
// probe decide: zero visible columns means a pre-granular server (the table
// does not exist) and legacy mode; anything else propagates the error so a
// transient outage cannot silently flip modes.
func DetectPrivilegeMode(ctx context.Context, conn clickhouse.Conn) (PrivilegeMode, error) {
	var dictGet, showDictionaries uint64
	err := conn.QueryRow(ctx, `
		SELECT countIf(toString(privilege) = 'dictGet'),
		       countIf(toString(privilege) = 'SHOW DICTIONARIES')
		FROM system.privileges`).Scan(&dictGet, &showDictionaries)
	if err == nil {
		return modeFromProbe(true, dictGet > 0, showDictionaries > 0), nil
	}
	readErr := err

	var visible uint64
	metaErr := conn.QueryRow(ctx, `
		SELECT count() FROM system.columns
		WHERE database = 'system' AND table = 'privileges' AND name = 'privilege'`).Scan(&visible)
	switch {
	case metaErr != nil:
		return "", errors.Join(
			fmt.Errorf("probe system.privileges: %w", readErr),
			fmt.Errorf("probe system.privileges existence: %w", metaErr))
	case visible == 0:
		return PrivilegeModeLegacy, nil
	default:
		return "", fmt.Errorf("probe system.privileges: %w", readErr)
	}
}

// modeFromProbe maps privilege-catalog observations to a mode. Both
// dictionary privileges must be present for the modern bundle.
func modeFromProbe(hasPrivilegesTable, hasDictGet, hasShowDictionaries bool) PrivilegeMode {
	if hasPrivilegesTable && hasDictGet && hasShowDictionaries {
		return PrivilegeModeFull
	}
	return PrivilegeModeLegacy
}
