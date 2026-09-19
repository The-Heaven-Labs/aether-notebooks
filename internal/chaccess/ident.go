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

// EveryoneRole is granted to every provisioned user in a warehouse.
const EveryoneRole = "aether_everyone"

const (
	userPrefix = "aether_u_"
	rolePrefix = "aether_g_"
)

var (
	identRe       = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	objectIdentRe = regexp.MustCompile(`^[A-Za-z0-9_$][A-Za-z0-9_$.-]{0,126}$`)
)

func hexID(a, b uuid.UUID) string {
	sum := sha256.Sum256(append(append([]byte{}, a[:]...), b[:]...))
	return hex.EncodeToString(sum[:8])
}

// UserIdent returns the ClickHouse username for an Aether user in a warehouse.
func UserIdent(orgID, userID uuid.UUID) string { return userPrefix + hexID(orgID, userID) }

// RoleIdent returns the ClickHouse role name for an Aether group.
func RoleIdent(orgID, groupID uuid.UUID) string { return rolePrefix + hexID(orgID, groupID) }

// DerivePassword deterministically derives a ClickHouse password from the
// master key so no per-user secret is stored at rest. The fixed prefix
// guarantees Cloud password complexity (uppercase + digit).
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
