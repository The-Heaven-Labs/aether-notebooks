// Package chaccess derives ClickHouse access identities and computes the
// desired ClickHouse user/role/grant state for an Aether warehouse.
package chaccess

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

var (
	identRe       = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	objectIdentRe = regexp.MustCompile(`^[A-Za-z0-9_$][A-Za-z0-9_$.-]{0,126}$`)
)

func hexID(ids ...uuid.UUID) string {
	sum := sha256.New()
	for _, id := range ids {
		sum.Write(id[:])
	}
	return hex.EncodeToString(sum.Sum(nil)[:8])
}

// IdentifierPrefix returns the prefix shared by every ClickHouse identity
// this package generates for a warehouse. Catalog queries (system.users,
// system.roles, system.grants) filter on it so multiple warehouses sharing
// one ClickHouse service never reconcile each other's entities.
func IdentifierPrefix(warehouseID uuid.UUID) string {
	return "aether_" + warehouseID.String()[:8] + "_"
}

// UserIdent returns the ClickHouse username for an Aether user in a
// warehouse. The name carries the warehouse discriminator as a prefix for
// scoping; the hash input (warehouse ‖ org ‖ user) is the collision
// protection.
func UserIdent(warehouseID, orgID, userID uuid.UUID) string {
	return IdentifierPrefix(warehouseID) + "u_" + hexID(warehouseID, orgID, userID)
}

// RoleIdent returns the ClickHouse role name for an Aether group in a
// warehouse. The hash input is warehouse ‖ org ‖ group, matching UserIdent.
func RoleIdent(warehouseID, orgID, groupID uuid.UUID) string {
	return IdentifierPrefix(warehouseID) + "g_" + hexID(warehouseID, orgID, groupID)
}

// EveryoneRole returns the role granted to every provisioned user in a
// warehouse.
func EveryoneRole(warehouseID uuid.UUID) string {
	return IdentifierPrefix(warehouseID) + "everyone"
}

// DerivePassword deterministically derives a ClickHouse password from the
// server's derived master key so no per-user secret is stored at rest. The
// fixed "Ae1_" prefix guarantees Cloud password complexity (uppercase +
// digit).
//
// masterKey is the output of crypto.DeriveKey(cfg.MasterKey), not the raw
// AETHER_MASTER_KEY env value. The "aether-ch-pw/v1" info string, the 24-byte
// output length, and the "Ae1_" prefix together form a versioned domain:
// changing any of them would re-key every provisioned user, so such a change
// must bump the info string to a new version (e.g. "aether-ch-pw/v2").
func DerivePassword(masterKey []byte, warehouseID, userID uuid.UUID) string {
	info := append([]byte("aether-ch-pw/v1:"+warehouseID.String()+":"), userID[:]...)
	key, err := hkdf.Key(sha256.New, masterKey, nil, string(info), 24)
	if err != nil {
		panic(err) // only fails on invalid hash/key parameters
	}
	return "Ae1_" + base64.RawURLEncoding.EncodeToString(key)
}

// QuoteIdent validates and backtick-quotes a generated ClickHouse identity
// name (user/role). These are always Aether-generated, so the charset is
// strict.
func QuoteIdent(name string) (string, error) {
	if !identRe.MatchString(name) {
		return "", fmt.Errorf("invalid clickhouse identifier %q", name)
	}
	return "`" + name + "`", nil
}

// QuoteObjectIdent validates and backtick-quotes a ClickHouse database/table
// name. Names come from the live catalog and may contain uppercase, dots,
// hyphens, or dollar signs; anything that could break out of backtick quoting
// (backtick, backslash, whitespace, control chars) is rejected.
func QuoteObjectIdent(name string) (string, error) {
	if !objectIdentRe.MatchString(name) {
		return "", fmt.Errorf("invalid clickhouse object name %q", name)
	}
	return "`" + name + "`", nil
}
